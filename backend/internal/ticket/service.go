package ticket

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// codeAttempts bounds retries when a freshly drawn code collides with an existing
// one. With ~1.1e15 combinations a single retry is already implausible; the bound
// stops a broken entropy source from spinning forever.
const codeAttempts = 5

// notFoundMessage is deliberately identical for unknown and malformed codes, so
// the public lookup cannot be used to probe the code format.
const notFoundMessage = "No ticket matches that code."

// AttendeeProvider is the contract this domain needs from the order domain,
// declared here by its consumer (ARCHITECTURE.md §3.2). One ticket is issued per
// attendee, and attendees are the order domain's data.
type AttendeeProvider interface {
	AttendeeIDsForOrder(ctx context.Context, orderID uuid.UUID) ([]uuid.UUID, error)
}

// Service implements ticket issuance and the door-side validation flow.
type Service struct {
	pool      db.Beginner
	repo      *Repository
	attendees AttendeeProvider
	log       *logger.Logger
}

// NewService builds the ticket service.
//
// attendees may be nil for callers that only use the read-side flows (validate,
// mark used, public lookup); IssueTicketsForOrder requires it.
func NewService(pool db.Beginner, repo *Repository, attendees AttendeeProvider, log *logger.Logger) *Service {
	return &Service{pool: pool, repo: repo, attendees: attendees, log: log}
}

// IssueTicketsForOrder generates the tickets for a freshly paid order: exactly one
// per attendee (constitution, Critical Data Flow Rules). It is the entry point the
// payment webhook's post-payment goroutine calls.
func (s *Service) IssueTicketsForOrder(ctx context.Context, orderID uuid.UUID) error {
	if s.attendees == nil {
		return errors.New("ticket: no attendee provider configured")
	}

	attendeeIDs, err := s.attendees.AttendeeIDsForOrder(ctx, orderID)
	if err != nil {
		return err
	}

	_, err = s.GenerateForOrder(ctx, orderID, attendeeIDs)
	return err
}

// GenerateForOrder issues exactly one ticket per attendee, in a single
// transaction.
//
// It is idempotent: payment providers retry notifications, so an order that
// already has tickets keeps them rather than gaining a second set. Only the unique
// ticket_code is persisted — no QR image or URL is stored anywhere (spec FR-022).
func (s *Service) GenerateForOrder(ctx context.Context, orderID uuid.UUID, attendeeIDs []uuid.UUID) ([]Record, error) {
	existing, err := s.repo.ListByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		s.log.InfoContext(ctx, "tickets already issued for order; skipping generation",
			"order_id", orderID.String(), "ticket_count", len(existing))
		return existing, nil
	}
	if len(attendeeIDs) == 0 {
		return []Record{}, nil
	}

	err = db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		for _, attendeeID := range attendeeIDs {
			if err := s.createOneTicket(ctx, tx, orderID, attendeeID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		// A concurrent generation may have won the race; if tickets now exist, that
		// is the correct final state and this call simply has nothing to add.
		if issued, listErr := s.repo.ListByOrderID(ctx, orderID); listErr == nil && len(issued) > 0 {
			return issued, nil
		}
		return nil, err
	}

	issued, err := s.repo.ListByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "tickets issued", "order_id", orderID.String(), "ticket_count", len(issued))
	return issued, nil
}

func (s *Service) createOneTicket(ctx context.Context, tx pgx.Tx, orderID, attendeeID uuid.UUID) error {
	for range codeAttempts {
		code, err := GenerateCode()
		if err != nil {
			return err
		}

		err = s.repo.CreateTicket(ctx, tx, code, orderID, attendeeID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, ErrCodeTaken) {
			return err
		}
		// A unique violation aborts the surrounding transaction in Postgres, so the
		// retry cannot continue inside it. Surfacing the error lets the caller start
		// a clean transaction.
		return fmt.Errorf("ticket code collision: %w", err)
	}
	return errors.New("ticket: could not allocate a unique code")
}

// Validate resolves a code to VALID / ALREADY_USED / INVALID.
//
// It is strictly read-only. At a door the same code is routinely scanned more than
// once before an admin decides to admit anyone, and marking a ticket used is
// irreversible — so consuming a ticket needs its own explicit call.
func (s *Service) Validate(ctx context.Context, rawCode string) (ValidationResult, error) {
	code := NormalizeCode(rawCode)

	detail, err := s.repo.GetDetailByCode(ctx, code)
	if errors.Is(err, ErrNotFound) {
		// An unknown code is a normal answer at a door, not a server error.
		return ValidationResult{Result: ResultInvalid, TicketCode: code}, nil
	}
	if err != nil {
		return ValidationResult{}, err
	}

	result := ResultInvalid
	switch detail.Status {
	case StatusActive:
		result = ResultValid
	case StatusUsed:
		result = ResultAlreadyUsed
	case StatusRevoked:
		// A revoked ticket must not be admitted, and the door has no separate
		// action for it, so it reads as invalid.
		result = ResultInvalid
	}

	if result == ResultInvalid {
		return ValidationResult{Result: ResultInvalid, TicketCode: code}, nil
	}

	return ValidationResult{
		Result:         result,
		TicketCode:     detail.TicketCode,
		AttendeeName:   &detail.AttendeeName,
		TicketTypeName: &detail.TicketTypeName,
		EventName:      &detail.EventName,
	}, nil
}

// MarkUsed performs the irreversible ACTIVE -> USED transition.
//
// The distinction between 404 and 409 comes from the guarded update's row count
// plus a status read, so a ticket that exists but is not admissible is reported as
// a conflict rather than as missing.
func (s *Service) MarkUsed(ctx context.Context, rawCode string) (MarkUsedResponse, error) {
	code := NormalizeCode(rawCode)

	applied, err := s.repo.MarkUsed(ctx, code)
	if err != nil {
		return MarkUsedResponse{}, err
	}
	if applied {
		s.log.InfoContext(ctx, "ticket marked used", "ticket_code", code)
		return MarkUsedResponse{TicketCode: code, Status: StatusUsed}, nil
	}

	status, err := s.repo.GetStatusByCode(ctx, code)
	if errors.Is(err, ErrNotFound) {
		return MarkUsedResponse{}, apperr.NotFound(apperr.CodeTicketNotFound, notFoundMessage)
	}
	if err != nil {
		return MarkUsedResponse{}, err
	}

	return MarkUsedResponse{}, apperr.Conflict(apperr.CodeAlreadyUsed,
		fmt.Sprintf("This ticket cannot be admitted: its status is %s.", status))
}

// LookupPublic serves the unauthenticated ticket lookup, returning only the fields
// spec FR-016 allows: no attendee email, no buyer data, no order number, no
// pricing, and no QR image.
func (s *Service) LookupPublic(ctx context.Context, rawCode string) (PublicTicket, error) {
	code := NormalizeCode(rawCode)

	detail, err := s.repo.GetDetailByCode(ctx, code)
	if errors.Is(err, ErrNotFound) {
		return PublicTicket{}, apperr.NotFound(apperr.CodeTicketNotFound, notFoundMessage)
	}
	if err != nil {
		return PublicTicket{}, err
	}

	return PublicTicket{
		TicketCode:   detail.TicketCode,
		Status:       detail.Status,
		EventName:    detail.EventName,
		AttendeeName: detail.AttendeeName,
	}, nil
}

package event

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// Service holds the event domain's business rules.
//
// It also implements the quota contract the order domain depends on. That contract
// is declared by its consumer as order.EventProvider (ARCHITECTURE.md §3.2 shows
// the interface living with the consumer), so the two domains share a shape rather
// than an import edge — nothing in internal/order imports this package.
//
// The quota semantics that contract carries are worth stating once, here, because
// every caller depends on them: ticket_types.quota is the REMAINING quota, not an
// original allocation. Checkout decrements it and cancel/expire/deny/failure
// restore it (spec FR-018). SCHEMA.md defines no quota_total column, so there is
// never a total to reconcile against.
type Service struct {
	pool   db.Beginner
	repo   *Repository
	orders OrderChecker
	log    *logger.Logger
}

// NewService builds the event service.
//
// orders may be nil for callers that only use the guest-facing catalog reads and
// the quota contract; the admin delete guards and sold counts require it.
func NewService(pool db.Beginner, repo *Repository, orders OrderChecker, log *logger.Logger) *Service {
	return &Service{pool: pool, repo: repo, orders: orders, log: log}
}

// ListPublishedEvents returns the guest-facing catalog.
func (s *Service) ListPublishedEvents(ctx context.Context) ([]EventSummary, error) {
	events, err := s.repo.ListPublishedEvents(ctx)
	if err != nil {
		return nil, err
	}
	return events, nil
}

// GetPublishedEventBySlug returns one published event with its ticket types.
// Unpublished events are reported as not found so drafts stay invisible to guests.
func (s *Service) GetPublishedEventBySlug(ctx context.Context, slug string) (EventDetail, error) {
	detail, err := s.repo.GetPublishedEventBySlug(ctx, slug)
	if errors.Is(err, ErrNotFound) {
		return EventDetail{}, apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	}
	if err != nil {
		return EventDetail{}, err
	}

	rows, err := s.repo.ListTicketTypesByEventID(ctx, detail.ID)
	if err != nil {
		return EventDetail{}, err
	}

	detail.TicketTypes = make([]TicketTypeSummary, 0, len(rows))
	for _, row := range rows {
		detail.TicketTypes = append(detail.TicketTypes, TicketTypeSummary{
			ID:             row.ID,
			Name:           row.Name,
			Price:          money.From(row.Price),
			QuotaRemaining: row.Quota,
			SalesStart:     row.SalesStart,
			SalesEnd:       row.SalesEnd,
		})
	}
	return detail, nil
}

// --- Quota contract (consumed by the order domain) ------------------------

// CheckAndDeductQuota atomically reserves qty seats on a ticket type inside the
// caller's transaction, returning an insufficient-quota error when the remaining
// quota cannot cover the request.
func (s *Service) CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	return s.repo.CheckAndDeductQuota(ctx, tx, ticketTypeID, qty)
}

// RestoreQuota returns qty seats to a ticket type inside the caller's transaction.
func (s *Service) RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	return s.repo.RestoreQuota(ctx, tx, ticketTypeID, qty)
}

// GetTicketTypeForCheckout returns the current server-side price and sales window
// for a ticket type, so checkout never trusts a client-supplied total. It reads
// inside the caller's transaction.
func (s *Service) GetTicketTypeForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (TicketTypeRow, error) {
	row, err := s.repo.GetTicketTypeByID(ctx, tx, id)
	if errors.Is(err, ErrNotFound) {
		return TicketTypeRow{}, apperr.BadRequest(apperr.CodeTicketTypeNotFound,
			fmt.Sprintf("Ticket type %s does not exist.", id))
	}
	if err != nil {
		return TicketTypeRow{}, err
	}
	return row, nil
}

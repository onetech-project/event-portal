package ticket

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/manjo/ticketing/backend/internal/ticket/ticketsql"
)

// Sentinel errors this domain reports to its service layer.
var (
	// ErrNotFound reports that no ticket matched the code.
	ErrNotFound = errors.New("ticket: not found")
	// ErrCodeTaken reports a unique-violation on tickets.ticket_code, which the
	// generator answers by drawing a fresh code.
	ErrCodeTaken = errors.New("ticket: code already taken")
)

const uniqueViolation = "23505"

// Repository is the only place in the codebase that talks to the tickets table.
type Repository struct {
	queries *ticketsql.Queries
}

// NewRepository builds a repository over a pool or any other DBTX.
func NewRepository(dbtx ticketsql.DBTX) *Repository {
	return &Repository{queries: ticketsql.New(dbtx)}
}

// CreateTicket inserts one ACTIVE ticket inside the caller's transaction, so a
// partially-issued order can never be committed.
func (r *Repository) CreateTicket(ctx context.Context, tx pgx.Tx, code string, orderID, attendeeID uuid.UUID) error {
	_, err := r.queries.WithTx(tx).CreateTicket(ctx, ticketsql.CreateTicketParams{
		TicketCode: code,
		OrderID:    orderID,
		AttendeeID: attendeeID,
	})
	if isUniqueViolation(err) {
		return ErrCodeTaken
	}
	if err != nil {
		return fmt.Errorf("create ticket: %w", err)
	}
	return nil
}

// GetDetailByCode looks a ticket up by its exact, already-normalized code.
//
// The predicate is a plain equality so idx_tickets_ticket_code is used. Callers
// must normalize their input first (see NormalizeCode) — the column is never
// wrapped in UPPER(), which would defeat that index.
func (r *Repository) GetDetailByCode(ctx context.Context, code string) (Detail, error) {
	row, err := r.queries.GetTicketDetailByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, fmt.Errorf("get ticket detail: %w", err)
	}

	return Detail{
		TicketCode:     row.TicketCode,
		Status:         row.Status,
		AttendeeName:   row.AttendeeName,
		TicketTypeName: row.TicketTypeName,
		EventName:      row.EventName,
	}, nil
}

// GetStatusByCode returns a ticket's current status.
func (r *Repository) GetStatusByCode(ctx context.Context, code string) (string, error) {
	status, err := r.queries.GetTicketStatusByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get ticket status: %w", err)
	}
	return status, nil
}

// MarkUsed performs the ACTIVE -> USED transition and reports whether it applied.
//
// It is a single guarded UPDATE, so two admins scanning the same ticket at the
// same moment cannot both be told they admitted it. A false result means the
// ticket was not ACTIVE (already used, revoked, or nonexistent); the caller
// distinguishes those by reading the status.
func (r *Repository) MarkUsed(ctx context.Context, code string) (bool, error) {
	affected, err := r.queries.MarkTicketUsed(ctx, code)
	if err != nil {
		return false, fmt.Errorf("mark ticket used: %w", err)
	}
	return affected > 0, nil
}

// ListByOrderID returns every ticket issued for an order, used when rendering (or
// re-rendering) the buyer's PDF.
func (r *Repository) ListByOrderID(ctx context.Context, orderID uuid.UUID) ([]Record, error) {
	rows, err := r.queries.ListTicketsByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list tickets: %w", err)
	}

	out := make([]Record, 0, len(rows))
	for _, row := range rows {
		out = append(out, Record{
			ID:         row.ID,
			TicketCode: row.TicketCode,
			AttendeeID: row.AttendeeID,
			Status:     row.Status,
		})
	}
	return out, nil
}

// ListDetailsByOrderID returns everything the ticket PDF prints for an order, in
// one query. Used for the initial post-payment delivery and for every admin
// resend, which is why it reads the stored codes rather than generating any.
func (r *Repository) ListDetailsByOrderID(ctx context.Context, orderID uuid.UUID) ([]FullDetail, error) {
	rows, err := r.queries.ListTicketDetailsByOrderID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("list ticket details: %w", err)
	}

	out := make([]FullDetail, 0, len(rows))
	for _, row := range rows {
		out = append(out, FullDetail{
			TicketCode:     row.TicketCode,
			Status:         row.Status,
			AttendeeName:   row.AttendeeName,
			TicketTypeName: row.TicketTypeName,
			EventName:      row.EventName,
			Venue:          row.Venue,
			StartDate:      row.StartDate,
		})
	}
	return out, nil
}

// CountByOrderID reports how many tickets an order already has, letting the
// post-payment path skip regeneration for an order that was already processed.
func (r *Repository) CountByOrderID(ctx context.Context, orderID uuid.UUID) (int, error) {
	total, err := r.queries.CountTicketsByOrderID(ctx, orderID)
	if err != nil {
		return 0, fmt.Errorf("count tickets: %w", err)
	}
	return int(total), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

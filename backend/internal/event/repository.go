package event

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/internal/event/eventsql"
)

// Sentinel errors the service layer maps onto HTTP responses. Keeping them here
// means callers never have to reason about pgx or SQL specifics.
var (
	// ErrNotFound reports that no row matched.
	ErrNotFound = errors.New("event: not found")
	// ErrInsufficientQuota reports that the guarded deduction matched no row —
	// either the remaining quota is too low or the ticket type does not exist.
	// Both are the same answer to a buyer, and neither is a server fault.
	ErrInsufficientQuota = errors.New("event: insufficient quota")
	// ErrSlugTaken reports a unique-violation on events.slug.
	ErrSlugTaken = errors.New("event: slug already taken")
)

// TicketTypeRow is the internal (non-wire) view of a ticket type used by services
// in this domain and by the quota checks checkout relies on.
type TicketTypeRow struct {
	ID      uuid.UUID
	EventID uuid.UUID
	Name    string
	// Description is the admin-authored note the booking card shows in place of
	// the standard non-refundable wording. Nil or blank falls back to it.
	Description *string
	Price       decimal.Decimal
	Quota       int32
	// SalesStart/SalesEnd bound when this type may be BOUGHT.
	SalesStart time.Time
	SalesEnd   time.Time
	// EventStart/EventEnd bound when a ticket of this type ADMITS its holder
	// (spec 015). EventStart is the instant admission opens, not showtime.
	// Independent of the sales window in both directions.
	EventStart time.Time
	EventEnd   time.Time
}

// Repository is the only place in the codebase that talks to events/ticket_types.
type Repository struct {
	queries *eventsql.Queries
}

// NewRepository builds a repository over a pool or any other DBTX.
func NewRepository(dbtx eventsql.DBTX) *Repository {
	return &Repository{queries: eventsql.New(dbtx)}
}

// WithTx returns a repository whose statements join the caller's transaction.
func (r *Repository) WithTx(tx pgx.Tx) *Repository {
	return &Repository{queries: r.queries.WithTx(tx)}
}

// --- Guest-facing reads ---------------------------------------------------

// ListPublishedEvents returns the catalog visible to guests.
func (r *Repository) ListPublishedEvents(ctx context.Context) ([]EventSummary, error) {
	rows, err := r.queries.ListPublishedEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list published events: %w", err)
	}

	out := make([]EventSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, EventSummary{
			ID:        row.ID,
			Name:      row.Name,
			Slug:      row.Slug,
			Venue:     row.Venue,
			Address:   row.Address,
			StartDate: row.StartDate,
			EndDate:   row.EndDate,
			BannerURL: row.BannerUrl,
		})
	}
	return out, nil
}

// GetPublishedEventBySlug returns a single published event. Draft and completed
// events are indistinguishable from missing ones to a guest.
func (r *Repository) GetPublishedEventBySlug(ctx context.Context, slug string) (EventDetail, error) {
	row, err := r.queries.GetPublishedEventBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return EventDetail{}, ErrNotFound
	}
	if err != nil {
		return EventDetail{}, fmt.Errorf("get published event by slug: %w", err)
	}

	return EventDetail{
		ID:          row.ID,
		Name:        row.Name,
		Slug:        row.Slug,
		Description: row.Description,
		Venue:       row.Venue,
		Address:     row.Address,
		StartDate:   row.StartDate,
		EndDate:     row.EndDate,
		BannerURL:   row.BannerUrl,
		Scale:       row.Scale,
	}, nil
}

// ListTicketTypesByEventID returns an event's purchasable options.
func (r *Repository) ListTicketTypesByEventID(ctx context.Context, eventID uuid.UUID) ([]TicketTypeRow, error) {
	rows, err := r.queries.ListTicketTypesByEventID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list ticket types: %w", err)
	}

	out := make([]TicketTypeRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, TicketTypeRow{
			ID:          row.ID,
			EventID:     row.EventID,
			Name:        row.Name,
			Description: row.Description,
			Price:       row.Price,
			Quota:       row.Quota,
			SalesStart:  row.SalesStart,
			SalesEnd:    row.SalesEnd,
			EventStart:  row.EventStart,
			EventEnd:    row.EventEnd,
		})
	}
	return out, nil
}

// GetTicketTypeByID returns one ticket type, used by checkout to validate the
// sales window and to price line items from current server-side data.
//
// tx may be nil for a standalone read. When checkout passes its transaction, the
// read joins it — both for a consistent snapshot and because borrowing a second
// pool connection while holding one can deadlock under concurrency.
func (r *Repository) GetTicketTypeByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (TicketTypeRow, error) {
	queries := r.queries
	if tx != nil {
		queries = queries.WithTx(tx)
	}

	row, err := queries.GetTicketTypeByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return TicketTypeRow{}, ErrNotFound
	}
	if err != nil {
		return TicketTypeRow{}, fmt.Errorf("get ticket type: %w", err)
	}

	return TicketTypeRow{
		ID:          row.ID,
		EventID:     row.EventID,
		Name:        row.Name,
		Description: row.Description,
		Price:       row.Price,
		Quota:       row.Quota,
		SalesStart:  row.SalesStart,
		SalesEnd:    row.SalesEnd,
		EventStart:  row.EventStart,
		EventEnd:    row.EventEnd,
	}, nil
}

// --- Quota (the EventProvider contract) -----------------------------------

// CheckAndDeductQuota atomically reserves qty seats inside the caller's
// transaction. The guarded UPDATE is what prevents overselling: concurrent buyers
// serialize on the row lock, and a buyer who arrives after the quota is exhausted
// matches no row rather than reading a stale count (Constitution Principle IV).
func (r *Repository) CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	_, err := r.queries.WithTx(tx).CheckAndDeductQuota(ctx, eventsql.CheckAndDeductQuotaParams{
		Qty: qty,
		ID:  ticketTypeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrInsufficientQuota
	}
	if err != nil {
		return fmt.Errorf("deduct quota: %w", err)
	}
	return nil
}

// RestoreQuota returns qty seats to the pool inside the caller's transaction. It
// is the mirror of CheckAndDeductQuota, used by the checkout compensation path and
// by every webhook status that cancels or expires an order.
func (r *Repository) RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	_, err := r.queries.WithTx(tx).RestoreQuota(ctx, eventsql.RestoreQuotaParams{
		Qty: qty,
		ID:  ticketTypeID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("restore quota: %w", err)
	}
	return nil
}

// EventHasTerms reports whether an event has an authored Terms & Conditions
// document. Booking is refused without one (spec 008, 409001).
func (r *Repository) EventHasTerms(ctx context.Context, eventID uuid.UUID) (bool, error) {
	has, err := r.queries.EventHasTerms(ctx, eventID)
	if err != nil {
		return false, fmt.Errorf("check event terms: %w", err)
	}
	return has, nil
}

// TermsRow is the current Terms & Conditions document of one event.
type TermsRow struct {
	ID        uuid.UUID
	Content   string
	UpdatedAt *time.Time
}

// GetEventTermsByEventID returns the event's current terms document, or
// ErrNotFound when none has been authored.
func (r *Repository) GetEventTermsByEventID(ctx context.Context, eventID uuid.UUID) (TermsRow, error) {
	row, err := r.queries.GetEventTermsByEventID(ctx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return TermsRow{}, ErrNotFound
	}
	if err != nil {
		return TermsRow{}, fmt.Errorf("get event terms: %w", err)
	}
	return TermsRow{ID: row.ID, Content: row.Content, UpdatedAt: row.UpdatedAt}, nil
}

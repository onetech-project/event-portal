package event

import (
	"context"
	"errors"
	"fmt"

	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/internal/event/eventsql"
)

const uniqueViolation = "23505"

type timeValue = time.Time

// AdminEventParams carries the server-validated fields for an event write.
type AdminEventParams struct {
	Name        string
	Slug        string
	Description *string
	Venue       string
	Address     string
	StartDate   timeValue
	EndDate     timeValue
	BannerURL   *string
	Status      string
}

// AdminTicketTypeParams carries the server-validated fields for a ticket-type
// write. Quota is the absolute remaining quota to store.
type AdminTicketTypeParams struct {
	EventID    uuid.UUID
	Name       string
	Price      decimal.Decimal
	Quota      int32
	SalesStart timeValue
	SalesEnd   timeValue
}

// --- Admin event reads ----------------------------------------------------

// ListEvents returns every event regardless of status.
func (r *Repository) ListEvents(ctx context.Context) ([]EventAdminView, error) {
	rows, err := r.queries.ListEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	out := make([]EventAdminView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toAdminView(row))
	}
	return out, nil
}

// GetEventByID returns one event regardless of status.
func (r *Repository) GetEventByID(ctx context.Context, id uuid.UUID) (EventAdminView, error) {
	row, err := r.queries.GetEventByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return EventAdminView{}, ErrNotFound
	}
	if err != nil {
		return EventAdminView{}, fmt.Errorf("get event: %w", err)
	}
	return toAdminView(row), nil
}

// --- Admin event writes ---------------------------------------------------

// CreateEvent inserts an event, reporting a slug collision as ErrSlugTaken rather
// than a raw constraint violation.
func (r *Repository) CreateEvent(ctx context.Context, p AdminEventParams) (EventAdminView, error) {
	row, err := r.queries.CreateEvent(ctx, eventsql.CreateEventParams{
		Name:        p.Name,
		Slug:        p.Slug,
		Description: p.Description,
		Venue:       p.Venue,
		Address:     p.Address,
		StartDate:   p.StartDate,
		EndDate:     p.EndDate,
		BannerUrl:   p.BannerURL,
		Status:      p.Status,
	})
	if isUniqueViolation(err) {
		return EventAdminView{}, ErrSlugTaken
	}
	if err != nil {
		return EventAdminView{}, fmt.Errorf("create event: %w", err)
	}
	return toAdminView(row), nil
}

// UpdateEvent replaces an event's fields.
func (r *Repository) UpdateEvent(ctx context.Context, id uuid.UUID, p AdminEventParams) (EventAdminView, error) {
	row, err := r.queries.UpdateEvent(ctx, eventsql.UpdateEventParams{
		ID:          id,
		Name:        p.Name,
		Slug:        p.Slug,
		Description: p.Description,
		Venue:       p.Venue,
		Address:     p.Address,
		StartDate:   p.StartDate,
		EndDate:     p.EndDate,
		BannerUrl:   p.BannerURL,
		Status:      p.Status,
	})
	if isUniqueViolation(err) {
		return EventAdminView{}, ErrSlugTaken
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return EventAdminView{}, ErrNotFound
	}
	if err != nil {
		return EventAdminView{}, fmt.Errorf("update event: %w", err)
	}
	return toAdminView(row), nil
}

// ListTicketTypeIDsByEventID returns an event's ticket type ids. Deleting an event
// resolves them here — from this domain's own table — and hands them to the
// OrderChecker, so neither domain has to read the other's tables.
func (r *Repository) ListTicketTypeIDsByEventID(ctx context.Context, tx pgx.Tx, eventID uuid.UUID) ([]uuid.UUID, error) {
	ids, err := r.withTx(tx).ListTicketTypeIDsByEventID(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list ticket type ids: %w", err)
	}
	return ids, nil
}

// TicketTypeNamesByIDs resolves ticket type ids to their display names in one
// query. The order domain uses it to label attendee rows without JOINing across a
// domain boundary.
func (r *Repository) TicketTypeNamesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	names := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}

	rows, err := r.queries.ListTicketTypeNamesByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list ticket type names: %w", err)
	}
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
}

// TicketTypeDisplayRecord labels one ticket type with the event it belongs to.
type TicketTypeDisplayRecord struct {
	TicketTypeName string
	EventName      string
	EventSlug      string
}

// TicketTypeDisplaysByIDs resolves ticket type ids to their display labels and
// owning event in one query. The order domain uses it to title a guest's order
// page without JOINing ticket_types/events itself — tables it does not own.
func (r *Repository) TicketTypeDisplaysByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]TicketTypeDisplayRecord, error) {
	displays := make(map[uuid.UUID]TicketTypeDisplayRecord, len(ids))
	if len(ids) == 0 {
		return displays, nil
	}

	rows, err := r.queries.ListTicketTypeDisplaysByIDs(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list ticket type displays: %w", err)
	}
	for _, row := range rows {
		displays[row.ID] = TicketTypeDisplayRecord{
			TicketTypeName: row.Name,
			EventName:      row.EventName,
			EventSlug:      row.EventSlug,
		}
	}
	return displays, nil
}

// DeleteTicketTypesByEventID removes an event's ticket types inside the caller's
// transaction. ticket_types.event_id is ON DELETE RESTRICT, so this must happen
// before the event row itself can be deleted.
func (r *Repository) DeleteTicketTypesByEventID(ctx context.Context, tx pgx.Tx, eventID uuid.UUID) error {
	if _, err := r.withTx(tx).DeleteTicketTypesByEventID(ctx, eventID); err != nil {
		return fmt.Errorf("delete ticket types: %w", err)
	}
	return nil
}

// DeleteEvent removes the event row inside the caller's transaction, reporting
// whether it existed.
func (r *Repository) DeleteEvent(ctx context.Context, tx pgx.Tx, eventID uuid.UUID) (bool, error) {
	affected, err := r.withTx(tx).DeleteEvent(ctx, eventID)
	if err != nil {
		return false, fmt.Errorf("delete event: %w", err)
	}
	return affected > 0, nil
}

// --- Admin ticket-type reads and writes -----------------------------------

// ListTicketTypesAdmin returns one event's ticket types.
func (r *Repository) ListTicketTypesAdmin(ctx context.Context, eventID uuid.UUID) ([]TicketTypeRow, error) {
	rows, err := r.queries.ListTicketTypesAdmin(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("list ticket types: %w", err)
	}

	out := make([]TicketTypeRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, TicketTypeRow{
			ID: row.ID, EventID: row.EventID, Name: row.Name, Price: row.Price,
			Quota: row.Quota, SalesStart: row.SalesStart, SalesEnd: row.SalesEnd,
		})
	}
	return out, nil
}

// GetTicketTypeAdmin returns one ticket type.
func (r *Repository) GetTicketTypeAdmin(ctx context.Context, id uuid.UUID) (TicketTypeRow, error) {
	row, err := r.queries.GetTicketTypeAdmin(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return TicketTypeRow{}, ErrNotFound
	}
	if err != nil {
		return TicketTypeRow{}, fmt.Errorf("get ticket type: %w", err)
	}
	return TicketTypeRow{
		ID: row.ID, EventID: row.EventID, Name: row.Name, Price: row.Price,
		Quota: row.Quota, SalesStart: row.SalesStart, SalesEnd: row.SalesEnd,
	}, nil
}

// CreateTicketType inserts a ticket type.
func (r *Repository) CreateTicketType(ctx context.Context, p AdminTicketTypeParams) (TicketTypeRow, error) {
	row, err := r.queries.CreateTicketType(ctx, eventsql.CreateTicketTypeParams{
		EventID:    p.EventID,
		Name:       p.Name,
		Price:      p.Price,
		Quota:      p.Quota,
		SalesStart: p.SalesStart,
		SalesEnd:   p.SalesEnd,
	})
	if err != nil {
		return TicketTypeRow{}, fmt.Errorf("create ticket type: %w", err)
	}
	return TicketTypeRow{
		ID: row.ID, EventID: row.EventID, Name: row.Name, Price: row.Price,
		Quota: row.Quota, SalesStart: row.SalesStart, SalesEnd: row.SalesEnd,
	}, nil
}

// UpdateTicketType replaces a ticket type's fields.
//
// Quota is set ABSOLUTELY to the submitted remaining quota — past sales are never
// re-subtracted from it. Treating this column as an original total and deriving a
// remainder would let an admin edit silently create tickets that were never
// allocated (constitution, Critical Data Flow Rules).
func (r *Repository) UpdateTicketType(ctx context.Context, id uuid.UUID, p AdminTicketTypeParams) (TicketTypeRow, error) {
	row, err := r.queries.UpdateTicketType(ctx, eventsql.UpdateTicketTypeParams{
		ID:         id,
		Name:       p.Name,
		Price:      p.Price,
		Quota:      p.Quota,
		SalesStart: p.SalesStart,
		SalesEnd:   p.SalesEnd,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return TicketTypeRow{}, ErrNotFound
	}
	if err != nil {
		return TicketTypeRow{}, fmt.Errorf("update ticket type: %w", err)
	}
	return TicketTypeRow{
		ID: row.ID, EventID: row.EventID, Name: row.Name, Price: row.Price,
		Quota: row.Quota, SalesStart: row.SalesStart, SalesEnd: row.SalesEnd,
	}, nil
}

// DeleteTicketType removes one ticket type inside the caller's transaction,
// reporting whether it existed.
func (r *Repository) DeleteTicketType(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	affected, err := r.withTx(tx).DeleteTicketType(ctx, id)
	if err != nil {
		return false, fmt.Errorf("delete ticket type: %w", err)
	}
	return affected > 0, nil
}

func (r *Repository) withTx(tx pgx.Tx) *eventsql.Queries {
	if tx == nil {
		return r.queries
	}
	return r.queries.WithTx(tx)
}

func toAdminView(row eventsql.Event) EventAdminView {
	return EventAdminView{
		ID:          row.ID,
		Name:        row.Name,
		Slug:        row.Slug,
		Description: row.Description,
		Venue:       row.Venue,
		Address:     row.Address,
		StartDate:   row.StartDate,
		EndDate:     row.EndDate,
		BannerURL:   row.BannerUrl,
		Status:      row.Status,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

package event

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// --- Events ---------------------------------------------------------------

// ListEvents returns every event, in any status, for the admin dashboard.
func (s *Service) ListEvents(ctx context.Context) ([]EventAdminView, error) {
	return s.repo.ListEvents(ctx)
}

// GetEventDetail returns one event with its ticket types and their derived sold
// counts.
func (s *Service) GetEventDetail(ctx context.Context, id uuid.UUID) (EventAdminDetail, error) {
	view, err := s.repo.GetEventByID(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return EventAdminDetail{}, apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	}
	if err != nil {
		return EventAdminDetail{}, err
	}

	types, err := s.ListTicketTypes(ctx, id)
	if err != nil {
		return EventAdminDetail{}, err
	}
	return EventAdminDetail{EventAdminView: view, TicketTypes: types}, nil
}

// CreateEvent validates and inserts an event.
func (s *Service) CreateEvent(ctx context.Context, req EventRequest) (EventAdminView, error) {
	if err := req.Validate(); err != nil {
		return EventAdminView{}, err
	}

	created, err := s.repo.CreateEvent(ctx, toEventParams(req))
	if errors.Is(err, ErrSlugTaken) {
		return EventAdminView{}, slugTakenError(req.Slug)
	}
	if err != nil {
		return EventAdminView{}, err
	}
	return created, nil
}

// UpdateEvent validates and replaces an event's fields.
func (s *Service) UpdateEvent(ctx context.Context, id uuid.UUID, req EventRequest) (EventAdminView, error) {
	if err := req.Validate(); err != nil {
		return EventAdminView{}, err
	}

	updated, err := s.repo.UpdateEvent(ctx, id, toEventParams(req))
	switch {
	case errors.Is(err, ErrSlugTaken):
		return EventAdminView{}, slugTakenError(req.Slug)
	case errors.Is(err, ErrNotFound):
		return EventAdminView{}, apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	case err != nil:
		return EventAdminView{}, err
	}
	return updated, nil
}

// DeleteEvent removes an event and all of its ticket types in one transaction.
//
// The sequence matters and cannot be collapsed: ticket_types.event_id is
// ON DELETE RESTRICT, so the ticket types must go first, and the order guard must
// run inside the same transaction or an order could be placed between the check
// and the delete. If anything is referenced, the whole transaction rolls back and
// nothing is deleted.
func (s *Service) DeleteEvent(ctx context.Context, id uuid.UUID) error {
	if s.orders == nil {
		return errors.New("event: no order checker configured")
	}

	var found bool

	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		ticketTypeIDs, err := s.repo.ListTicketTypeIDsByEventID(ctx, tx, id)
		if err != nil {
			return err
		}

		hasOrders, err := s.orders.HasOrdersForTicketTypes(ctx, tx, ticketTypeIDs)
		if err != nil {
			return err
		}
		if hasOrders {
			return apperr.BadRequest(apperr.CodeEventHasOrders,
				"This event cannot be deleted because one of its ticket types has been ordered.")
		}

		if err := s.repo.DeleteTicketTypesByEventID(ctx, tx, id); err != nil {
			return err
		}

		found, err = s.repo.DeleteEvent(ctx, tx, id)
		return err
	})
	if err != nil {
		return err
	}
	if !found {
		return apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	}
	return nil
}

// --- Lookups consumed by the order domain ---------------------------------

// TicketTypeNames resolves ticket type ids to display names, so the admin order
// views can label rows without reading this domain's tables directly.
func (s *Service) TicketTypeNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	return s.repo.TicketTypeNamesByIDs(ctx, ids)
}

// TicketTypeDisplays resolves ticket type ids to their display labels and owning
// event, so the guest-facing order page can name what was bought and which event
// it belongs to without reading this domain's tables directly.
func (s *Service) TicketTypeDisplays(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]TicketTypeDisplayRecord, error) {
	return s.repo.TicketTypeDisplaysByIDs(ctx, ids)
}

// TicketTypeIDsForEvent resolves an event to its ticket type ids, letting the
// order domain filter its own tables by event without JOINing across the boundary.
func (s *Service) TicketTypeIDsForEvent(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error) {
	return s.repo.ListTicketTypeIDsByEventID(ctx, nil, eventID)
}

// --- Ticket types ---------------------------------------------------------

// ListTicketTypes returns one event's ticket types with their derived sold counts.
func (s *Service) ListTicketTypes(ctx context.Context, eventID uuid.UUID) ([]TicketTypeAdminView, error) {
	rows, err := s.repo.ListTicketTypesAdmin(ctx, eventID)
	if err != nil {
		return nil, err
	}
	return s.withSoldCounts(ctx, rows)
}

// GetTicketType returns one ticket type with its derived sold count.
func (s *Service) GetTicketType(ctx context.Context, id uuid.UUID) (TicketTypeAdminView, error) {
	row, err := s.repo.GetTicketTypeAdmin(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return TicketTypeAdminView{}, ticketTypeNotFound()
	}
	if err != nil {
		return TicketTypeAdminView{}, err
	}

	views, err := s.withSoldCounts(ctx, []TicketTypeRow{row})
	if err != nil {
		return TicketTypeAdminView{}, err
	}
	return views[0], nil
}

// CreateTicketType validates and inserts a ticket type under an existing event.
func (s *Service) CreateTicketType(ctx context.Context, req TicketTypeRequest) (TicketTypeAdminView, error) {
	if err := req.Validate(true); err != nil {
		return TicketTypeAdminView{}, err
	}

	// Checked explicitly so an unknown event_id is a clear 400 rather than a raw
	// foreign-key violation.
	if _, err := s.repo.GetEventByID(ctx, req.EventID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return TicketTypeAdminView{}, apperr.BadRequest(apperr.CodeEventNotFound,
				fmt.Sprintf("Event %s does not exist.", req.EventID))
		}
		return TicketTypeAdminView{}, err
	}

	created, err := s.repo.CreateTicketType(ctx, AdminTicketTypeParams{
		EventID:    req.EventID,
		Name:       req.Name,
		Price:      req.Price.Decimal(),
		Quota:      req.Quota,
		SalesStart: req.SalesStart,
		SalesEnd:   req.SalesEnd,
	})
	if err != nil {
		return TicketTypeAdminView{}, err
	}

	// A freshly created ticket type has no sales yet, so its sold count is zero
	// without needing a query.
	return toTicketTypeView(created, 0), nil
}

// UpdateTicketType validates and replaces a ticket type's fields.
//
// event_id is immutable and ignored if sent. The submitted quota replaces the
// remaining quota absolutely.
func (s *Service) UpdateTicketType(ctx context.Context, id uuid.UUID, req TicketTypeRequest) (TicketTypeAdminView, error) {
	if err := req.Validate(false); err != nil {
		return TicketTypeAdminView{}, err
	}

	updated, err := s.repo.UpdateTicketType(ctx, id, AdminTicketTypeParams{
		Name:       req.Name,
		Price:      req.Price.Decimal(),
		Quota:      req.Quota,
		SalesStart: req.SalesStart,
		SalesEnd:   req.SalesEnd,
	})
	if errors.Is(err, ErrNotFound) {
		return TicketTypeAdminView{}, ticketTypeNotFound()
	}
	if err != nil {
		return TicketTypeAdminView{}, err
	}

	views, err := s.withSoldCounts(ctx, []TicketTypeRow{updated})
	if err != nil {
		return TicketTypeAdminView{}, err
	}
	return views[0], nil
}

// DeleteTicketType removes a ticket type, guarded inside the same transaction as
// the delete so no order can appear between the check and the DELETE.
func (s *Service) DeleteTicketType(ctx context.Context, id uuid.UUID) error {
	if s.orders == nil {
		return errors.New("event: no order checker configured")
	}

	var found bool

	err := db.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		hasOrders, err := s.orders.HasOrdersForTicketType(ctx, tx, id)
		if err != nil {
			return err
		}
		if hasOrders {
			return apperr.BadRequest(apperr.CodeTicketTypeHasOrders,
				"This ticket type cannot be deleted because it has been ordered.")
		}

		found, err = s.repo.DeleteTicketType(ctx, tx, id)
		return err
	})
	if err != nil {
		return err
	}
	if !found {
		return ticketTypeNotFound()
	}
	return nil
}

// withSoldCounts attaches the derived sold count to a page of ticket types using
// one batched query, never an N+1 loop and never a cross-domain JOIN.
func (s *Service) withSoldCounts(ctx context.Context, rows []TicketTypeRow) ([]TicketTypeAdminView, error) {
	views := make([]TicketTypeAdminView, 0, len(rows))
	if len(rows) == 0 {
		return views, nil
	}

	sold := map[uuid.UUID]int{}
	if s.orders != nil {
		ids := make([]uuid.UUID, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}

		var err error
		sold, err = s.orders.SoldCountByTicketType(ctx, ids)
		if err != nil {
			return nil, err
		}
	}

	for _, row := range rows {
		views = append(views, toTicketTypeView(row, sold[row.ID]))
	}
	return views, nil
}

func toTicketTypeView(row TicketTypeRow, sold int) TicketTypeAdminView {
	return TicketTypeAdminView{
		ID:         row.ID,
		EventID:    row.EventID,
		Name:       row.Name,
		Price:      money.From(row.Price),
		Quota:      row.Quota,
		Sold:       sold,
		SalesStart: row.SalesStart,
		SalesEnd:   row.SalesEnd,
	}
}

func toEventParams(req EventRequest) AdminEventParams {
	return AdminEventParams{
		Name:        req.Name,
		Slug:        req.Slug,
		Description: req.Description,
		Venue:       req.Venue,
		Address:     req.Address,
		StartDate:   req.StartDate,
		EndDate:     req.EndDate,
		BannerURL:   req.BannerURL,
		Status:      req.Status,
	}
}

func slugTakenError(slug string) error {
	return apperr.BadRequest(apperr.CodeSlugNotUnique,
		fmt.Sprintf("The slug %q is already used by another event.", slug))
}

func ticketTypeNotFound() error {
	return apperr.NotFound(apperr.CodeTicketTypeNotFound, "Ticket type not found.")
}

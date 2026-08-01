package order

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/internal/order/ordersql"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// TicketTypeDisplay is everything the order views need to label a line: the
// ticket type's own name plus the event it belongs to. The order domain holds
// only the ticket type id, and both other values live in tables the event domain
// owns.
type TicketTypeDisplay struct {
	TicketTypeName string
	EventName      string
	EventSlug      string
}

// EventLookup is the contract the order read views need from the event domain,
// declared here by its consumer (ARCHITECTURE.md §3.2).
//
// It exists so these views can label and filter rows by event without JOINing
// orders/attendees against ticket_types — tables the event domain owns
// (Constitution Principle II).
type EventLookup interface {
	// TicketTypeNames resolves ticket type ids to display names, batched.
	TicketTypeNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
	// TicketTypeIDsForEvent resolves an event to its ticket type ids, which this
	// domain then matches against its own foreign keys.
	TicketTypeIDsForEvent(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error)
	// TicketTypeDisplays resolves ticket type ids to their display labels and
	// owning event, batched — one lookup per page, never one per row.
	TicketTypeDisplays(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]TicketTypeDisplay, error)
}

// OrderFilter narrows the admin order list. A nil field means "no filter".
type OrderFilter struct {
	Status  *string
	EventID *uuid.UUID
}

// AttendeeFilter narrows the admin attendee list. A nil field means "no filter".
type AttendeeFilter struct {
	OrderID *uuid.UUID
	EventID *uuid.UUID
}

// AdminService serves the read-only admin views over orders and attendees.
type AdminService struct {
	repo   *Repository
	events EventLookup
}

// NewAdminService builds the admin read service.
func NewAdminService(repo *Repository, events EventLookup) *AdminService {
	return &AdminService{repo: repo, events: events}
}

// ListOrders returns orders matching the filter, newest first.
func (s *AdminService) ListOrders(ctx context.Context, filter OrderFilter) ([]OrderSummary, error) {
	ticketTypeIDs, scoped, err := s.ticketTypeScope(ctx, filter.EventID)
	if err != nil {
		return nil, err
	}
	if scoped && len(ticketTypeIDs) == 0 {
		// The event has no ticket types, so no order can reference it.
		return []OrderSummary{}, nil
	}

	rows, err := s.repo.queries.ListOrdersAdmin(ctx, ordersql.ListOrdersAdminParams{
		Status:        filter.Status,
		TicketTypeIds: ticketTypeIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list orders: %w", err)
	}

	out := make([]OrderSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, OrderSummary{
			ID:          row.ID,
			OrderNumber: row.OrderNumber,
			BuyerName:   row.BuyerName,
			BuyerEmail:  row.BuyerEmail,
			Status:      row.Status,
			TotalAmount: money.From(row.TotalAmount),
			CreatedAt:   row.CreatedAt,
		})
	}
	return out, nil
}

// ListAttendees returns attendees matching the filter, with their ticket type
// names resolved through the event domain in one batched lookup.
func (s *AdminService) ListAttendees(ctx context.Context, filter AttendeeFilter) ([]AttendeeSummary, error) {
	ticketTypeIDs, scoped, err := s.ticketTypeScope(ctx, filter.EventID)
	if err != nil {
		return nil, err
	}
	if scoped && len(ticketTypeIDs) == 0 {
		return []AttendeeSummary{}, nil
	}

	rows, err := s.repo.queries.ListAttendeesAdmin(ctx, ordersql.ListAttendeesAdminParams{
		OrderID:       toNullUUID(filter.OrderID),
		TicketTypeIds: ticketTypeIDs,
	})
	if err != nil {
		return nil, fmt.Errorf("list attendees: %w", err)
	}

	names, err := s.ticketTypeNames(ctx, rows)
	if err != nil {
		return nil, err
	}

	out := make([]AttendeeSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, AttendeeSummary{
			Name:           row.Name,
			Email:          row.Email,
			TicketTypeName: names[row.TicketTypeID],
			OrderNumber:    row.OrderNumber,
		})
	}
	return out, nil
}

// ticketTypeScope translates an optional event filter into the ticket type ids
// this domain can match against its own columns. The bool reports whether an
// event filter was requested at all, so "no filter" stays distinct from "an event
// with no ticket types".
func (s *AdminService) ticketTypeScope(ctx context.Context, eventID *uuid.UUID) ([]uuid.UUID, bool, error) {
	if eventID == nil {
		return nil, false, nil
	}

	ids, err := s.events.TicketTypeIDsForEvent(ctx, *eventID)
	if err != nil {
		return nil, true, err
	}
	return ids, true, nil
}

func (s *AdminService) ticketTypeNames(ctx context.Context, rows []ordersql.ListAttendeesAdminRow) (map[uuid.UUID]string, error) {
	if len(rows) == 0 {
		return map[uuid.UUID]string{}, nil
	}

	// One lookup for the whole page, deduplicated — never one per row.
	seen := make(map[uuid.UUID]struct{}, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.TicketTypeID]; ok {
			continue
		}
		seen[row.TicketTypeID] = struct{}{}
		ids = append(ids, row.TicketTypeID)
	}

	return s.events.TicketTypeNames(ctx, ids)
}

func toNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

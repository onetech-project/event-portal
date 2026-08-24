package order

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/internal/order/ordersql"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// TicketTypeDisplay is everything the order views need to label a line: the
// ticket type's own name plus the event it belongs to. The order domain holds
// only the ticket type id, and both other values live in tables the event domain
// owns.
type TicketTypeDisplay struct {
	TicketTypeName string
	// Description is the line's own admin-authored note, printed after the
	// admission date on the receipt's product sub-line (spec 016 FR-011).
	Description    string
	EventName      string
	EventSlug      string
	EventVenue     string
	EventAddress   string
	EventStartDate time.Time
	EventEndDate   time.Time
	// AdmissionStarts are the days this line admits on — one for a ticket type.
	// Distinct from the event's dates above: a multi-day event's Day 1 and Day 2
	// passes share an event but admit on different days, and an order line must
	// name its own (spec 015).
	AdmissionStarts []time.Time
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
	// PackageDisplays does the same for package ids. An order made entirely of
	// bundles has no ticket type on any of its lines, so without this those lines
	// would render nameless.
	PackageDisplays(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]PackageDisplay, error)
}

// PackageDisplay labels a package line: the package's own name plus the event it
// belongs to, mirroring TicketTypeDisplay.
type PackageDisplay struct {
	PackageName string
	// Description mirrors TicketTypeDisplay.Description for a bundle line.
	Description    string
	EventName      string
	EventSlug      string
	EventVenue     string
	EventAddress   string
	EventStartDate time.Time
	EventEndDate   time.Time
	// Every distinct day the bundle admits on, derived from its constituents
	// (spec 015 FR-021a). A single span cannot express Day 1 + Day 2.
	AdmissionStarts []time.Time
}

// OrderFilter narrows the admin order list. A nil field means "no filter".
//
// Page is the slice of the filtered result to return (spec 021). It is applied
// after the filters, so the reported total means "matching what was asked for",
// not "in the table".
type OrderFilter struct {
	Status  *string
	EventID *uuid.UUID
	// Search is a partial, case-insensitive match on order number, buyer name or
	// buyer email (spec 022 FR-053). Nil means no filter; it is NOT the same as
	// the empty string, which would match everything and is normalised away.
	Search *string
	Page   httpx.PageRequest
}

// AttendeeFilter narrows the admin attendee list. A nil field means "no filter".
type AttendeeFilter struct {
	OrderID *uuid.UUID
	EventID *uuid.UUID
	// Search matches a PARTIAL attendee name or email, or an order number
	// (spec 022 FR-053). This is the operator's recovery route for an undelivered
	// registration: the registrant is never shown the address their ticket went
	// to, so a lookup demanding the exact address is not a recovery route.
	Search *string
	Page   httpx.PageRequest
}

// paging renders a filter's page as the cache-key component. Kept here so the
// two admin lists cannot drift apart on how they build it.
func paging(p httpx.PageRequest) cache.Paging {
	return cache.Paging{Page: p.Page, Size: p.Size}
}

// AdminService serves the read-only admin views over orders and attendees.
type AdminService struct {
	repo   *Repository
	events EventLookup
	cache  cache.Lists
}

// NewAdminService builds the admin read service. The cache starts as a no-op so
// a service built without WithCache reads the database on every call, exactly as
// it did before the cache existed.
func NewAdminService(repo *Repository, events EventLookup) *AdminService {
	return &AdminService{repo: repo, events: events, cache: cache.NoOp{}}
}

// WithCache installs the list cache (Constitution Principle VII).
func (s *AdminService) WithCache(c cache.Lists) *AdminService {
	if c != nil {
		s.cache = c
	}
	return s
}

// ListOrders returns one page of orders matching the filter, newest first.
//
// Cached per filter combination AND per page, all sharing the single orders
// scope — so one order changing status invalidates every variant that could
// contain it with one INCR, rather than requiring the writer to know which
// filters or which pages are warm (FR-010, spec 021).
//
// The whole page is cached, count included, so a hit reports the same total the
// database would have rather than pairing this page's rows with some other
// read's count.
func (s *AdminService) ListOrders(ctx context.Context, filter OrderFilter) (httpx.Page[OrderSummary], error) {
	filter.Page = filter.Page.Normalize()
	key := cache.OrdersAdminKey(filter.Status, filter.EventID, filter.Search, paging(filter.Page))
	return cache.Through(ctx, s.cache, key,
		func(ctx context.Context) (httpx.Page[OrderSummary], error) {
			return s.listOrders(ctx, filter)
		})
}

func (s *AdminService) listOrders(ctx context.Context, filter OrderFilter) (httpx.Page[OrderSummary], error) {
	ticketTypeIDs, scoped, err := s.ticketTypeScope(ctx, filter.EventID)
	if err != nil {
		return httpx.Page[OrderSummary]{}, err
	}
	if scoped && len(ticketTypeIDs) == 0 {
		// The event has no ticket types, so no order can reference it. An empty
		// page with total 0 — never an unfiltered count.
		return httpx.EmptyPage[OrderSummary](filter.Page), nil
	}

	// Count first: an out-of-range page must resolve to the last one, and a page
	// read past the end comes back empty with nothing to clamp against.
	total, err := s.repo.queries.CountOrdersAdmin(ctx, ordersql.CountOrdersAdminParams{
		Status:        filter.Status,
		TicketTypeIds: ticketTypeIDs,
		Search:        filter.Search,
	})
	if err != nil {
		return httpx.Page[OrderSummary]{}, fmt.Errorf("count orders: %w", err)
	}
	page := filter.Page.ClampTo(total)
	if total == 0 {
		return httpx.EmptyPage[OrderSummary](filter.Page), nil
	}

	rows, err := s.repo.queries.ListOrdersAdmin(ctx, ordersql.ListOrdersAdminParams{
		Status:        filter.Status,
		TicketTypeIds: ticketTypeIDs,
		Search:        filter.Search,
		RowLimit:      int32(page.Limit()),
		RowOffset:     int32(page.Offset()),
	})
	if err != nil {
		return httpx.Page[OrderSummary]{}, fmt.Errorf("list orders: %w", err)
	}

	out := make([]OrderSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, OrderSummary{
			ID:             row.ID,
			OrderNumber:    row.OrderNumber,
			BuyerName:      strv(row.BuyerName),
			BuyerEmail:     strv(row.BuyerEmail),
			Status:         row.Status,
			TotalAmount:    money.From(row.TotalAmount),
			IsRegistration: row.IsRegistration,
			CreatedAt:      row.CreatedAt,
		})
	}
	return httpx.NewPage(out, page, total), nil
}

// ListAttendees returns one page of attendees matching the filter, with their
// ticket type names resolved through the event domain in one batched lookup.
//
// Paging makes that lookup strictly cheaper than it was: it now resolves the
// names on one page rather than on every attendee in the system.
func (s *AdminService) ListAttendees(ctx context.Context, filter AttendeeFilter) (httpx.Page[AttendeeSummary], error) {
	filter.Page = filter.Page.Normalize()
	key := cache.AttendeesAdminKey(filter.OrderID, filter.EventID, filter.Search, paging(filter.Page))
	return cache.Through(ctx, s.cache, key,
		func(ctx context.Context) (httpx.Page[AttendeeSummary], error) {
			return s.listAttendees(ctx, filter)
		})
}

func (s *AdminService) listAttendees(ctx context.Context, filter AttendeeFilter) (httpx.Page[AttendeeSummary], error) {
	ticketTypeIDs, scoped, err := s.ticketTypeScope(ctx, filter.EventID)
	if err != nil {
		return httpx.Page[AttendeeSummary]{}, err
	}
	if scoped && len(ticketTypeIDs) == 0 {
		return httpx.EmptyPage[AttendeeSummary](filter.Page), nil
	}

	total, err := s.repo.queries.CountAttendeesAdmin(ctx, ordersql.CountAttendeesAdminParams{
		OrderID:       toNullUUID(filter.OrderID),
		TicketTypeIds: ticketTypeIDs,
		Search:        filter.Search,
	})
	if err != nil {
		return httpx.Page[AttendeeSummary]{}, fmt.Errorf("count attendees: %w", err)
	}
	page := filter.Page.ClampTo(total)
	if total == 0 {
		return httpx.EmptyPage[AttendeeSummary](filter.Page), nil
	}

	rows, err := s.repo.queries.ListAttendeesAdmin(ctx, ordersql.ListAttendeesAdminParams{
		OrderID:       toNullUUID(filter.OrderID),
		TicketTypeIds: ticketTypeIDs,
		Search:        filter.Search,
		RowLimit:      int32(page.Limit()),
		RowOffset:     int32(page.Offset()),
	})
	if err != nil {
		return httpx.Page[AttendeeSummary]{}, fmt.Errorf("list attendees: %w", err)
	}

	names, err := s.ticketTypeNames(ctx, rows)
	if err != nil {
		return httpx.Page[AttendeeSummary]{}, err
	}

	out := make([]AttendeeSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, AttendeeSummary{
			Name:           strv(row.Name),
			Email:          strv(row.Email),
			TicketTypeName: names[row.TicketTypeID],
			OrderNumber:    row.OrderNumber,
			IsRegistration: row.IsRegistration,
		})
	}
	return httpx.NewPage(out, page, total), nil
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

// --- Fee master administration (clarified 2026-08-05) -----------------------

// Fees returns every fee master row for the admin panel.
// Fees returns one page of the fee master rows.
//
// Deliberately uncached, and it must stay that way: `fees` is not in the cache's
// closed family registry, and Constitution Principle VII's surface list would
// need amending before it could be (spec 021 research R5).
func (s *AdminService) Fees(ctx context.Context, page httpx.PageRequest) (httpx.Page[FeeAdminView], error) {
	page = page.Normalize()
	total, err := s.repo.CountFees(ctx)
	if err != nil {
		return httpx.Page[FeeAdminView]{}, err
	}
	if total == 0 {
		return httpx.EmptyPage[FeeAdminView](page), nil
	}
	page = page.ClampTo(total)

	rows, err := s.repo.ListFees(ctx, page)
	if err != nil {
		return httpx.Page[FeeAdminView]{}, err
	}
	out := make([]FeeAdminView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toFeeAdminView(row))
	}
	return httpx.NewPage(out, page, total), nil
}

// CreateFee adds a fee master row. It affects only future bookings — existing
// orders keep the lines frozen at their booking time.
func (s *AdminService) CreateFee(ctx context.Context, req FeeRequest) (FeeAdminView, error) {
	if err := req.Validate(); err != nil {
		return FeeAdminView{}, err
	}
	row, err := s.repo.CreateFee(ctx, strings.TrimSpace(req.Name), req.FeeType, req.Value.Decimal(), req.Position, req.IsActive)
	if errors.Is(err, ErrFeeNameTaken) {
		return FeeAdminView{}, apperr.Conflict(apperr.CodeValidation, "A fee with this name already exists.")
	}
	if err != nil {
		return FeeAdminView{}, err
	}
	return toFeeAdminView(row), nil
}

// UpdateFee rewrites a fee master row; future bookings pick the change up.
func (s *AdminService) UpdateFee(ctx context.Context, id uuid.UUID, req FeeRequest) (FeeAdminView, error) {
	if err := req.Validate(); err != nil {
		return FeeAdminView{}, err
	}
	row, err := s.repo.UpdateFee(ctx, id, strings.TrimSpace(req.Name), req.FeeType, req.Value.Decimal(), req.Position, req.IsActive)
	if errors.Is(err, ErrNotFound) {
		return FeeAdminView{}, apperr.NotFound(apperr.CodeValidation, "Fee not found.")
	}
	if errors.Is(err, ErrFeeNameTaken) {
		return FeeAdminView{}, apperr.Conflict(apperr.CodeValidation, "A fee with this name already exists.")
	}
	if err != nil {
		return FeeAdminView{}, err
	}
	return toFeeAdminView(row), nil
}

// DeleteFee removes a fee master row; orders keep their frozen snapshots.
func (s *AdminService) DeleteFee(ctx context.Context, id uuid.UUID) error {
	err := s.repo.DeleteFee(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return apperr.NotFound(apperr.CodeValidation, "Fee not found.")
	}
	return err
}

func toFeeAdminView(row FeeRow) FeeAdminView {
	return FeeAdminView{
		ID: row.ID, Name: row.Name, FeeType: row.FeeType,
		Value: money.From(row.Value), Position: row.Position, IsActive: row.IsActive,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

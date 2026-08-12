package order_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
)

// eventLookupAdapter mirrors what cmd/api wires: the order domain labels and
// filters its rows through the event domain's contract instead of JOINing across
// the boundary.
type eventLookupAdapter struct{ svc *event.Service }

func (a eventLookupAdapter) TicketTypeNames(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	return a.svc.TicketTypeNames(ctx, ids)
}

func (a eventLookupAdapter) TicketTypeIDsForEvent(ctx context.Context, eventID uuid.UUID) ([]uuid.UUID, error) {
	return a.svc.TicketTypeIDsForEvent(ctx, eventID)
}

func (a eventLookupAdapter) TicketTypeDisplays(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]order.TicketTypeDisplay, error) {
	records, err := a.svc.TicketTypeDisplays(ctx, ids)
	if err != nil {
		return nil, err
	}

	displays := make(map[uuid.UUID]order.TicketTypeDisplay, len(records))
	for id, record := range records {
		// A faithful copy of every field, matching cmd/api/adapters.go. A double
		// that silently drops fields agrees with an implementation that never
		// populated them.
		displays[id] = order.TicketTypeDisplay{
			TicketTypeName:  record.TicketTypeName,
			EventName:       record.EventName,
			EventSlug:       record.EventSlug,
			EventVenue:      record.EventVenue,
			EventAddress:    record.EventAddress,
			EventStartDate:  record.EventStartDate,
			EventEndDate:    record.EventEndDate,
			AdmissionStarts: record.AdmissionStarts,
		}
	}
	return displays, nil
}

func (a eventLookupAdapter) PackageDisplays(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]order.PackageDisplay, error) {
	records, err := a.svc.PackageDisplays(ctx, ids)
	if err != nil {
		return nil, err
	}
	displays := make(map[uuid.UUID]order.PackageDisplay, len(records))
	for id, record := range records {
		displays[id] = order.PackageDisplay{
			PackageName:     record.PackageName,
			EventName:       record.EventName,
			EventSlug:       record.EventSlug,
			EventVenue:      record.EventVenue,
			EventAddress:    record.EventAddress,
			EventStartDate:  record.EventStartDate,
			EventEndDate:    record.EventEndDate,
			AdmissionStarts: record.AdmissionStarts,
		}
	}
	return displays, nil
}

type adminOrderFixture struct {
	svc   *order.AdminService
	pool  *testsupport.Pool
	event testsupport.Event
	other testsupport.Event
	vip   testsupport.TicketType
	reg   testsupport.TicketType
}

func newAdminOrderFixture(t *testing.T) adminOrderFixture {
	t.Helper()
	pool := testsupport.RequirePool(t)

	events := event.NewService(pool, event.NewRepository(pool), order.NewRepository(pool), testsupport.DiscardLogger())
	svc := order.NewAdminService(order.NewRepository(pool), eventLookupAdapter{svc: events})

	main := testsupport.SeedEvent(t, pool, "main-event", "PUBLISHED")
	other := testsupport.SeedEvent(t, pool, "other-event", "PUBLISHED")

	return adminOrderFixture{
		svc:   svc,
		pool:  pool,
		event: main,
		other: other,
		reg:   testsupport.SeedTicketType(t, pool, main.ID, "Regular", "150000.00", 10),
		vip:   testsupport.SeedTicketType(t, pool, main.ID, "VIP", "500000.00", 5),
	}
}

// --- Orders ---------------------------------------------------------------

func TestAdminListOrdersReturnsTheContractFields(t *testing.T) {
	f := newAdminOrderFixture(t)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-ADM1", "PAID")
	testsupport.SeedOrderItem(t, f.pool, ord.ID, f.reg.ID, 2)

	orders, err := f.svc.ListOrders(context.Background(), order.OrderFilter{})

	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "ORD-ADM1", orders[0].OrderNumber)
	assert.Equal(t, "PAID", orders[0].Status)
	assert.Equal(t, "Test Buyer", orders[0].BuyerName)
	assert.Equal(t, "buyer@example.com", orders[0].BuyerEmail)
	assert.Equal(t, "250000.00", orders[0].TotalAmount.String())
	assert.NotNil(t, orders[0].CreatedAt)
}

func TestAdminListOrdersReturnsAnEmptySliceNotNil(t *testing.T) {
	f := newAdminOrderFixture(t)

	orders, err := f.svc.ListOrders(context.Background(), order.OrderFilter{})

	require.NoError(t, err)
	assert.NotNil(t, orders, "an empty list must serialize as [] not null")
	assert.Empty(t, orders)
}

func TestAdminListOrdersFiltersByStatus(t *testing.T) {
	f := newAdminOrderFixture(t)
	testsupport.SeedOrder(t, f.pool, "ORD-PAID", "PAID")
	testsupport.SeedOrder(t, f.pool, "ORD-PEND", "PENDING")
	testsupport.SeedOrder(t, f.pool, "ORD-CANC", "CANCELLED")

	paid := "PAID"
	orders, err := f.svc.ListOrders(context.Background(), order.OrderFilter{Status: &paid})

	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "ORD-PAID", orders[0].OrderNumber)
}

func TestAdminListOrdersFiltersByEvent(t *testing.T) {
	f := newAdminOrderFixture(t)
	otherType := testsupport.SeedTicketType(t, f.pool, f.other.ID, "Other", "100000.00", 10)

	mine := testsupport.SeedOrder(t, f.pool, "ORD-MINE", "PAID")
	testsupport.SeedOrderItem(t, f.pool, mine.ID, f.reg.ID, 1)

	theirs := testsupport.SeedOrder(t, f.pool, "ORD-THEIRS", "PAID")
	testsupport.SeedOrderItem(t, f.pool, theirs.ID, otherType.ID, 1)

	orders, err := f.svc.ListOrders(context.Background(), order.OrderFilter{EventID: &f.event.ID})

	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "ORD-MINE", orders[0].OrderNumber)
}

func TestAdminListOrdersCombinesFilters(t *testing.T) {
	f := newAdminOrderFixture(t)

	paidOrder := testsupport.SeedOrder(t, f.pool, "ORD-CP", "PAID")
	testsupport.SeedOrderItem(t, f.pool, paidOrder.ID, f.reg.ID, 1)

	pendingOrder := testsupport.SeedOrder(t, f.pool, "ORD-CQ", "PENDING")
	testsupport.SeedOrderItem(t, f.pool, pendingOrder.ID, f.reg.ID, 1)

	paid := "PAID"
	orders, err := f.svc.ListOrders(context.Background(),
		order.OrderFilter{Status: &paid, EventID: &f.event.ID})

	require.NoError(t, err)
	require.Len(t, orders, 1)
	assert.Equal(t, "ORD-CP", orders[0].OrderNumber)
}

func TestAdminListOrdersForAnEventWithNoTicketTypesReturnsNothing(t *testing.T) {
	f := newAdminOrderFixture(t)
	empty := testsupport.SeedEvent(t, f.pool, "no-types", "PUBLISHED")
	ord := testsupport.SeedOrder(t, f.pool, "ORD-X", "PAID")
	testsupport.SeedOrderItem(t, f.pool, ord.ID, f.reg.ID, 1)

	orders, err := f.svc.ListOrders(context.Background(), order.OrderFilter{EventID: &empty.ID})

	require.NoError(t, err)
	assert.Empty(t, orders)
}

// --- Attendees ------------------------------------------------------------

func TestAdminListAttendeesResolvesTheTicketTypeName(t *testing.T) {
	f := newAdminOrderFixture(t)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-ATT", "PAID")
	testsupport.SeedAttendee(t, f.pool, ord.ID, f.reg.ID, "Andi", "andi@example.com")
	testsupport.SeedAttendee(t, f.pool, ord.ID, f.vip.ID, "Sari", "sari@example.com")

	attendees, err := f.svc.ListAttendees(context.Background(), order.AttendeeFilter{})

	require.NoError(t, err)
	require.Len(t, attendees, 2)

	byName := map[string]order.AttendeeSummary{}
	for _, a := range attendees {
		byName[a.Name] = a
	}

	assert.Equal(t, "Regular", byName["Andi"].TicketTypeName)
	assert.Equal(t, "VIP", byName["Sari"].TicketTypeName)
	assert.Equal(t, "ORD-ATT", byName["Andi"].OrderNumber)
	assert.Equal(t, "andi@example.com", byName["Andi"].Email)
}

func TestAdminListAttendeesFiltersByOrder(t *testing.T) {
	f := newAdminOrderFixture(t)
	first := testsupport.SeedOrder(t, f.pool, "ORD-A1", "PAID")
	second := testsupport.SeedOrder(t, f.pool, "ORD-A2", "PAID")
	testsupport.SeedAttendee(t, f.pool, first.ID, f.reg.ID, "Andi", "andi@example.com")
	testsupport.SeedAttendee(t, f.pool, second.ID, f.reg.ID, "Sari", "sari@example.com")

	attendees, err := f.svc.ListAttendees(context.Background(),
		order.AttendeeFilter{OrderID: &first.ID})

	require.NoError(t, err)
	require.Len(t, attendees, 1)
	assert.Equal(t, "Andi", attendees[0].Name)
}

func TestAdminListAttendeesFiltersByEvent(t *testing.T) {
	f := newAdminOrderFixture(t)
	otherType := testsupport.SeedTicketType(t, f.pool, f.other.ID, "Other", "100000.00", 10)

	ord := testsupport.SeedOrder(t, f.pool, "ORD-EV", "PAID")
	testsupport.SeedAttendee(t, f.pool, ord.ID, f.reg.ID, "Mine", "mine@example.com")
	testsupport.SeedAttendee(t, f.pool, ord.ID, otherType.ID, "Theirs", "theirs@example.com")

	attendees, err := f.svc.ListAttendees(context.Background(),
		order.AttendeeFilter{EventID: &f.event.ID})

	require.NoError(t, err)
	require.Len(t, attendees, 1)
	assert.Equal(t, "Mine", attendees[0].Name)
}

func TestAdminListAttendeesReturnsAnEmptySliceNotNil(t *testing.T) {
	f := newAdminOrderFixture(t)

	attendees, err := f.svc.ListAttendees(context.Background(), order.AttendeeFilter{})

	require.NoError(t, err)
	assert.NotNil(t, attendees)
	assert.Empty(t, attendees)
}

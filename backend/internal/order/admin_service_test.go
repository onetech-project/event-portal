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

	page, err := f.svc.ListOrders(context.Background(), order.OrderFilter{})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "ORD-ADM1", page.Items[0].OrderNumber)
	assert.Equal(t, "PAID", page.Items[0].Status)
	assert.Equal(t, "Test Buyer", page.Items[0].BuyerName)
	assert.Equal(t, "buyer@example.com", page.Items[0].BuyerEmail)
	assert.Equal(t, "250000.00", page.Items[0].TotalAmount.String())
	assert.NotNil(t, page.Items[0].CreatedAt)
}

func TestAdminListOrdersReturnsAnEmptySliceNotNil(t *testing.T) {
	f := newAdminOrderFixture(t)

	page, err := f.svc.ListOrders(context.Background(), order.OrderFilter{})

	require.NoError(t, err)
	assert.NotNil(t, page.Items, "an empty list must serialize as [] not null")
	assert.Empty(t, page.Items)
}

func TestAdminListOrdersFiltersByStatus(t *testing.T) {
	f := newAdminOrderFixture(t)
	testsupport.SeedOrder(t, f.pool, "ORD-PAID", "PAID")
	testsupport.SeedOrder(t, f.pool, "ORD-PEND", "PENDING")
	testsupport.SeedOrder(t, f.pool, "ORD-CANC", "CANCELLED")

	paid := "PAID"
	page, err := f.svc.ListOrders(context.Background(), order.OrderFilter{Status: &paid})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "ORD-PAID", page.Items[0].OrderNumber)
}

func TestAdminListOrdersFiltersByEvent(t *testing.T) {
	f := newAdminOrderFixture(t)
	otherType := testsupport.SeedTicketType(t, f.pool, f.other.ID, "Other", "100000.00", 10)

	mine := testsupport.SeedOrder(t, f.pool, "ORD-MINE", "PAID")
	testsupport.SeedOrderItem(t, f.pool, mine.ID, f.reg.ID, 1)

	theirs := testsupport.SeedOrder(t, f.pool, "ORD-THEIRS", "PAID")
	testsupport.SeedOrderItem(t, f.pool, theirs.ID, otherType.ID, 1)

	page, err := f.svc.ListOrders(context.Background(), order.OrderFilter{EventID: &f.event.ID})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "ORD-MINE", page.Items[0].OrderNumber)
}

func TestAdminListOrdersCombinesFilters(t *testing.T) {
	f := newAdminOrderFixture(t)

	paidOrder := testsupport.SeedOrder(t, f.pool, "ORD-CP", "PAID")
	testsupport.SeedOrderItem(t, f.pool, paidOrder.ID, f.reg.ID, 1)

	pendingOrder := testsupport.SeedOrder(t, f.pool, "ORD-CQ", "PENDING")
	testsupport.SeedOrderItem(t, f.pool, pendingOrder.ID, f.reg.ID, 1)

	paid := "PAID"
	page, err := f.svc.ListOrders(context.Background(),
		order.OrderFilter{Status: &paid, EventID: &f.event.ID})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "ORD-CP", page.Items[0].OrderNumber)
}

func TestAdminListOrdersForAnEventWithNoTicketTypesReturnsNothing(t *testing.T) {
	f := newAdminOrderFixture(t)
	empty := testsupport.SeedEvent(t, f.pool, "no-types", "PUBLISHED")
	ord := testsupport.SeedOrder(t, f.pool, "ORD-X", "PAID")
	testsupport.SeedOrderItem(t, f.pool, ord.ID, f.reg.ID, 1)

	page, err := f.svc.ListOrders(context.Background(), order.OrderFilter{EventID: &empty.ID})

	require.NoError(t, err)
	assert.Empty(t, page.Items)
}

// --- Attendees ------------------------------------------------------------

func TestAdminListAttendeesResolvesTheTicketTypeName(t *testing.T) {
	f := newAdminOrderFixture(t)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-ATT", "PAID")
	testsupport.SeedAttendee(t, f.pool, ord.ID, f.reg.ID, "Andi", "andi@example.com")
	testsupport.SeedAttendee(t, f.pool, ord.ID, f.vip.ID, "Sari", "sari@example.com")

	page, err := f.svc.ListAttendees(context.Background(), order.AttendeeFilter{})

	require.NoError(t, err)
	require.Len(t, page.Items, 2)

	byName := map[string]order.AttendeeSummary{}
	for _, a := range page.Items {
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

	page, err := f.svc.ListAttendees(context.Background(),
		order.AttendeeFilter{OrderID: &first.ID})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "Andi", page.Items[0].Name)
}

func TestAdminListAttendeesFiltersByEvent(t *testing.T) {
	f := newAdminOrderFixture(t)
	otherType := testsupport.SeedTicketType(t, f.pool, f.other.ID, "Other", "100000.00", 10)

	ord := testsupport.SeedOrder(t, f.pool, "ORD-EV", "PAID")
	testsupport.SeedAttendee(t, f.pool, ord.ID, f.reg.ID, "Mine", "mine@example.com")
	testsupport.SeedAttendee(t, f.pool, ord.ID, otherType.ID, "Theirs", "theirs@example.com")

	page, err := f.svc.ListAttendees(context.Background(),
		order.AttendeeFilter{EventID: &f.event.ID})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "Mine", page.Items[0].Name)
}

func TestAdminListAttendeesReturnsAnEmptySliceNotNil(t *testing.T) {
	f := newAdminOrderFixture(t)

	page, err := f.svc.ListAttendees(context.Background(), order.AttendeeFilter{})

	require.NoError(t, err)
	assert.NotNil(t, page.Items)
	assert.Empty(t, page.Items)
}

// --- Spec 022 US4: registrations are ordinary records here -------------------

// A registration arranged through the REAL registration path, then read back
// through the admin lists. Arranged rather than seeded because a direct INSERT
// would not exercise the write path under test and, per AGENTS.md, would make
// the setup lie about what the system actually produces.
func newAdminRegistrationFixture(t *testing.T) (adminOrderFixture, registrationFixture) {
	t.Helper()
	rf := newRegistrationFixture(t, 5)
	require.NoError(t, rf.svc.RegisterFree(context.Background(), rf.ticket.ID, rf.request()))
	rf.svc.WaitForRegistrationFulfillment()

	events := event.NewService(rf.pool, event.NewRepository(rf.pool),
		order.NewRepository(rf.pool), testsupport.DiscardLogger())
	admin := order.NewAdminService(order.NewRepository(rf.pool), eventLookupAdapter{svc: events})

	return adminOrderFixture{svc: admin, pool: rf.pool, event: rf.event}, rf
}

// FR-033 / US4 scenario 1. Since spec 022, PAID no longer implies money moved: a
// registration is written directly at PAID with a zero total. An admin surface
// that reads the status alone will report a free registration as revenue, so the
// origin has to be legible on the row itself rather than inferred.
func TestAdminListOrdersMarksARegistrationAsSuchWithAZeroTotal(t *testing.T) {
	f, _ := newAdminRegistrationFixture(t)

	page, err := f.svc.ListOrders(context.Background(), order.OrderFilter{})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	row := page.Items[0]

	assert.Equal(t, "PAID", row.Status)
	assert.True(t, row.IsRegistration,
		"a status of PAID is not enough to tell a registration from a sale")
	assert.Equal(t, "0.00", row.TotalAmount.String())
}

// The other half of the same rule: a PURCHASE must not be labelled a
// registration. Asserting only the positive would pass against a field hardcoded
// true.
func TestAdminListOrdersDoesNotMarkAPurchaseAsARegistration(t *testing.T) {
	f := newAdminOrderFixture(t)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-BOUGHT", "PAID")
	testsupport.SeedOrderItem(t, f.pool, ord.ID, f.reg.ID, 1)

	page, err := f.svc.ListOrders(context.Background(), order.OrderFilter{})

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	assert.False(t, page.Items[0].IsRegistration)
}

// FR-053, and the reason it exists: the registrant is never shown the address
// their e-ticket went to (FR-039), so an operator helping someone who never
// received it has a NAME and at best a guess at the address. A lookup demanding
// the exact address is not a recovery route.
func TestOperatorCanFindARegistrationByPartialNameOrEmail(t *testing.T) {
	f, _ := newAdminRegistrationFixture(t)

	for _, term := range []string{"Halo", "halo", "example.com", "REGISTRANT"} {
		t.Run(term, func(t *testing.T) {
			search := term
			page, err := f.svc.ListAttendees(context.Background(),
				order.AttendeeFilter{Search: &search})

			require.NoError(t, err)
			require.Len(t, page.Items, 1, "a partial, case-insensitive match must find the registrant")
			assert.Equal(t, "Halo Registrant", page.Items[0].Name)
			assert.True(t, page.Items[0].IsRegistration,
				"and the operator must be able to see it has no receipt to resend")
		})
	}
}

func TestOperatorSearchExcludesNonMatchingRegistrants(t *testing.T) {
	f, _ := newAdminRegistrationFixture(t)

	absent := "nobody-by-that-name"
	page, err := f.svc.ListAttendees(context.Background(), order.AttendeeFilter{Search: &absent})

	require.NoError(t, err)
	assert.Empty(t, page.Items)
	assert.Equal(t, int64(0), page.Total, "and the count must agree with the rows, not with an unfiltered read")
}

// The count and the rows come from two queries whose WHERE clauses must stay
// identical (order.sql says so beside them). A search applied to one and not the
// other shows an operator "3 results" above a single row, or pages them into
// emptiness.
func TestOperatorSearchFiltersTheCountAndTheRowsTogether(t *testing.T) {
	f, _ := newAdminRegistrationFixture(t)

	search := "Halo"
	page, err := f.svc.ListOrders(context.Background(), order.OrderFilter{Search: &search})

	require.NoError(t, err)
	assert.Equal(t, int64(len(page.Items)), page.Total,
		"the paired count query must carry the same filter as the row query")
}

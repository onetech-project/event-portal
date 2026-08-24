package order_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// The admin lists are filtered by status and event, so several cached variants
// can hold the same order at once. FR-010 requires that a change to that order
// reach every one of them — which is the property the generation counter buys,
// and the property these tests pin down.

type adminCacheFixture struct {
	pool   *testsupport.Pool
	admin  *order.AdminService
	orders *order.Service
	events *event.Service
	cache  *cache.Redis
}

func newAdminCacheFixture(t *testing.T) adminCacheFixture {
	t.Helper()

	pool := testsupport.RequirePool(t)
	srv := miniredis.RunT(t)

	c, err := cache.NewRedis("redis://"+srv.Addr()+"/0", time.Minute,
		testsupport.DiscardLogger(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	repo := order.NewRepository(pool)
	events := event.NewService(pool, event.NewRepository(pool), repo,
		testsupport.DiscardLogger()).WithCache(c)

	gw := &fakeGateway{
		url:       "https://pay.example.com/session",
		qrString:  "00020101021226620014COM.EXAMPLE.QRIS",
		expiresAt: time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second),
	}

	return adminCacheFixture{
		pool:  pool,
		admin: order.NewAdminService(repo, eventLookupAdapter{svc: events}).WithCache(c),
		orders: order.NewService(pool, repo, eventProviderAdapter{svc: events}, gw,
			testsupport.DiscardLogger()).WithCache(c),
		events: events,
		cache:  c,
	}
}

// firstPage is the page a bare OrderFilter{} normalises to, and therefore the
// entry those reads warm.
var firstPage = cache.Paging{Page: 1, Size: httpx.DefaultPageSize}

func (f adminCacheFixture) orderVariantCached(t *testing.T, status *string, eventID *uuid.UUID) bool {
	t.Helper()
	_, ok, err := f.cache.Get(context.Background(), cache.OrdersAdminKey(status, eventID, nil, firstPage))
	require.NoError(t, err)
	return ok
}

func TestAdminOrderListIsCachedPerFilterCombination(t *testing.T) {
	f := newAdminCacheFixture(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, f.pool, "admin-cached", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, f.pool, "ORD-ADMIN-1", "PENDING")
	testsupport.SeedOrderItem(t, f.pool, ord.ID, tt.ID, 2)

	first, err := f.admin.ListOrders(ctx, order.OrderFilter{})
	require.NoError(t, err)
	require.Len(t, first.Items, 1)

	second, err := f.admin.ListOrders(ctx, order.OrderFilter{})
	require.NoError(t, err)
	require.Equal(t, first, second, "the cached read must equal the uncached one")

	// A cache-free admin service must produce exactly the same thing (FR-005).
	uncached := order.NewAdminService(order.NewRepository(f.pool),
		eventLookupAdapter{svc: f.events})
	direct, err := uncached.ListOrders(ctx, order.OrderFilter{})
	require.NoError(t, err)
	require.Equal(t, direct, second)
}

// Distinct filters must not collide: each combination is its own entry.
func TestAdminOrderFilterVariantsAreCachedSeparately(t *testing.T) {
	f := newAdminCacheFixture(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, f.pool, "variants", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	pending := testsupport.SeedOrder(t, f.pool, "ORD-PENDING", "PENDING")
	testsupport.SeedOrderItem(t, f.pool, pending.ID, tt.ID, 1)
	paid := testsupport.SeedOrder(t, f.pool, "ORD-PAID", "PAID")
	testsupport.SeedOrderItem(t, f.pool, paid.ID, tt.ID, 1)

	statusPaid := "PAID"

	all, err := f.admin.ListOrders(ctx, order.OrderFilter{})
	require.NoError(t, err)
	require.Len(t, all.Items, 2)

	onlyPaid, err := f.admin.ListOrders(ctx, order.OrderFilter{Status: &statusPaid})
	require.NoError(t, err)
	require.Len(t, onlyPaid.Items, 1, "the status filter must not be served the unfiltered entry")
	require.Equal(t, "ORD-PAID", onlyPaid.Items[0].OrderNumber)
}

// FR-010: one order changing must invalidate EVERY warm variant that could
// contain it, not just the one the writer happened to think of.
func TestOneOrderChangeInvalidatesEveryWarmFilterVariant(t *testing.T) {
	f := newAdminCacheFixture(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, f.pool, "fanout", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "Terms apply.")

	statusPending := "PENDING"
	statusPaid := "PAID"

	// Warm four distinct variants plus an attendee variant.
	variants := []struct {
		status  *string
		eventID *uuid.UUID
	}{
		{nil, nil},
		{&statusPending, nil},
		{&statusPaid, nil},
		{nil, &ev.ID},
		{&statusPending, &ev.ID},
	}
	for _, v := range variants {
		_, err := f.admin.ListOrders(ctx, order.OrderFilter{Status: v.status, EventID: v.eventID})
		require.NoError(t, err)
	}
	_, err := f.admin.ListAttendees(ctx, order.AttendeeFilter{EventID: &ev.ID})
	require.NoError(t, err)

	for _, v := range variants {
		require.True(t, f.orderVariantCached(t, v.status, v.eventID), "precondition: all variants warm")
	}

	// A single booking creates one order.
	_, err = f.orders.Book(ctx, order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: 1}},
	})
	require.NoError(t, err)

	for _, v := range variants {
		require.False(t, f.orderVariantCached(t, v.status, v.eventID),
			"every variant that could contain the new order must be invalidated")
	}

	// Attendee lists share the same scope and go with them.
	_, ok, err := f.cache.Get(ctx, cache.AttendeesAdminKey(nil, &ev.ID, nil, firstPage))
	require.NoError(t, err)
	require.False(t, ok, "attendee variants share the orders scope")

	// And the next read of the unfiltered variant shows the new order.
	after, err := f.admin.ListOrders(ctx, order.OrderFilter{})
	require.NoError(t, err)
	require.Len(t, after.Items, 1)
}

// Booking writes attendee slots, so the admin attendee list must refresh
// (spec US3 scenario 4).
func TestBookingRefreshesTheAdminAttendeeList(t *testing.T) {
	f := newAdminCacheFixture(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, f.pool, "attendees", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "Terms apply.")

	warm, err := f.admin.ListAttendees(ctx, order.AttendeeFilter{})
	require.NoError(t, err)
	require.Empty(t, warm.Items)

	_, err = f.orders.Book(ctx, order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: 2}},
	})
	require.NoError(t, err)

	after, err := f.admin.ListAttendees(ctx, order.AttendeeFilter{})
	require.NoError(t, err)
	require.Len(t, after.Items, 2, "the two new attendee slots must be visible immediately")
}

// An empty result is a legitimate cached value: an event with no orders must not
// re-query on every admin page load.
func TestEmptyAdminListIsCachedNotRepeatedlyQueried(t *testing.T) {
	f := newAdminCacheFixture(t)
	ctx := context.Background()

	first, err := f.admin.ListOrders(ctx, order.OrderFilter{})
	require.NoError(t, err)
	require.Empty(t, first.Items)

	require.True(t, f.orderVariantCached(t, nil, nil),
		"an empty list is a value worth caching, not a miss to repeat")

	second, err := f.admin.ListOrders(ctx, order.OrderFilter{})
	require.NoError(t, err)
	require.Empty(t, second.Items)
	require.NotNil(t, second.Items, "an empty list must not come back as null")
}

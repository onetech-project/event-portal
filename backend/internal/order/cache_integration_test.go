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
)

// The correctness-critical story: a cached ticket list carries live remaining
// quota, so every quota movement has to reach it. These tests drive real bookings
// and real restores through the real services, then assert BOTH that the next
// read is fresh AND that it was a cache miss — a content-only assertion would
// pass even with the cache bypassed entirely, which is the bug worth catching.

type quotaCacheFixture struct {
	pool   *testsupport.Pool
	orders *order.Service
	events *event.Service
	cache  *cache.Redis
	redis  *miniredis.Miniredis
}

func newQuotaCacheFixture(t *testing.T) quotaCacheFixture {
	t.Helper()

	pool := testsupport.RequirePool(t)
	srv := miniredis.RunT(t)

	c, err := cache.NewRedis("redis://"+srv.Addr()+"/0", time.Minute,
		testsupport.DiscardLogger(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	events := event.NewService(pool, event.NewRepository(pool), order.NewRepository(pool),
		testsupport.DiscardLogger()).WithCache(c)

	gw := &fakeGateway{
		url:       "https://pay.example.com/session",
		qrString:  "00020101021226620014COM.EXAMPLE.QRIS",
		expiresAt: time.Now().Add(15 * time.Minute).UTC().Truncate(time.Second),
	}

	orders := order.NewService(pool, order.NewRepository(pool),
		eventProviderAdapter{svc: events}, gw, testsupport.DiscardLogger()).WithCache(c)

	return quotaCacheFixture{pool: pool, orders: orders, events: events, cache: c, redis: srv}
}

// ticketListCached reports whether the event's ticket list is currently readable
// from cache. False after an invalidation, true after a warm read.
func (f quotaCacheFixture) ticketListCached(t *testing.T, eventID uuid.UUID) bool {
	t.Helper()
	_, ok, err := f.cache.Get(context.Background(), cache.TicketTypesPublicKey(eventID))
	require.NoError(t, err)
	return ok
}

func TestBookingInvalidatesTheEventTicketList(t *testing.T) {
	f := newQuotaCacheFixture(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, f.pool, "quota-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "Terms apply.")

	warm, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)
	require.Len(t, warm, 1)
	require.EqualValues(t, 10, warm[0].QuotaRemaining)
	require.True(t, f.ticketListCached(t, ev.ID), "precondition: warm")

	_, err = f.orders.Book(ctx, order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: 3}},
	})
	require.NoError(t, err)

	require.False(t, f.ticketListCached(t, ev.ID),
		"booking moved quota, so the cached ticket list must have been invalidated")

	after, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)
	require.EqualValues(t, 7, after[0].QuotaRemaining,
		"the very next read must show the decremented quota")
}

// The cache must never decide a sale. Even reading a stale figure, the booking
// outcome comes from the row-locked UPDATE (FR-012).
func TestCacheNeverAuthorisesASaleBeyondQuota(t *testing.T) {
	f := newQuotaCacheFixture(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, f.pool, "scarce-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 2)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "Terms apply.")

	// Warm the list showing 2 remaining.
	warm, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)
	require.EqualValues(t, 2, warm[0].QuotaRemaining)

	// Take both.
	_, err = f.orders.Book(ctx, order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: 2}},
	})
	require.NoError(t, err)

	// Re-warm so a cached figure exists again, then try to oversell.
	_, err = f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)

	_, err = f.orders.Book(ctx, order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: 1}},
	})
	require.Error(t, err, "the database, not the cache, decides — and it says no")

	remaining, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)
	require.EqualValues(t, 0, remaining[0].QuotaRemaining, "quota never goes negative")
}

// One event's quota movement must not disturb another event's cached lists
// (FR-009, spec US2 scenario 4).
func TestBookingOneEventLeavesAnotherEventsCacheIntact(t *testing.T) {
	f := newQuotaCacheFixture(t)
	ctx := context.Background()

	evA := testsupport.SeedEvent(t, f.pool, "event-a", "PUBLISHED")
	ttA := testsupport.SeedTicketType(t, f.pool, evA.ID, "Regular", "150000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, evA.ID, "Terms apply.")

	evB := testsupport.SeedEvent(t, f.pool, "event-b", "PUBLISHED")
	testsupport.SeedTicketType(t, f.pool, evB.ID, "Regular", "150000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, evB.ID, "Terms apply.")

	_, err := f.events.TicketTypesForEventSlug(ctx, evA.Slug)
	require.NoError(t, err)
	_, err = f.events.TicketTypesForEventSlug(ctx, evB.Slug)
	require.NoError(t, err)
	require.True(t, f.ticketListCached(t, evA.ID))
	require.True(t, f.ticketListCached(t, evB.ID))

	_, err = f.orders.Book(ctx, order.BookRequest{
		EventID: evA.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &ttA.ID, Quantity: 1}},
	})
	require.NoError(t, err)

	require.False(t, f.ticketListCached(t, evA.ID), "the booked event's list is invalidated")
	require.True(t, f.ticketListCached(t, evB.ID),
		"an unrelated event's warm list must survive — scoping is the point")
}

// A booking that fails on insufficient quota rolls back, so nothing changed and
// nothing may be invalidated (FR-007).
func TestFailedBookingDoesNotInvalidate(t *testing.T) {
	f := newQuotaCacheFixture(t)
	ctx := context.Background()

	ev := testsupport.SeedEvent(t, f.pool, "tiny-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 1)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "Terms apply.")

	_, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)
	require.True(t, f.ticketListCached(t, ev.ID))

	_, err = f.orders.Book(ctx, order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: 5}},
	})
	require.Error(t, err)

	require.True(t, f.ticketListCached(t, ev.ID),
		"the transaction rolled back, so the cached list is still correct")
}

// SC-002 at scale for this family: many write-then-read cycles, zero stale reads.
func TestRepeatedBookingsNeverServeStaleQuota(t *testing.T) {
	f := newQuotaCacheFixture(t)
	ctx := context.Background()

	const cycles = 40
	ev := testsupport.SeedEvent(t, f.pool, "cycle-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", cycles)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "Terms apply.")

	for i := 0; i < cycles; i++ {
		before, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
		require.NoError(t, err)
		want := int32(cycles - i)
		require.EqualValues(t, want, before[0].QuotaRemaining, "cycle %d read a stale quota", i)

		_, err = f.orders.Book(ctx, order.BookRequest{
			EventID: ev.ID,
			Items:   []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: 1}},
		})
		require.NoError(t, err)
	}

	final, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)
	require.EqualValues(t, 0, final[0].QuotaRemaining)
}

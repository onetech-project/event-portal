package payment_test

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/cache"
)

// Quota restored by a webhook or the sweeper must reach the guest-facing ticket
// list, and the payment domain cannot name the event on its own: `orders` has no
// event_id column, so it resolves ticket types → events through the
// EventScopeLookup seam. These tests cover that path end to end.

type paymentCacheFixture struct {
	svc     *payment.Service
	gateway *stubGateway
	events  *event.Service
	pool    *testsupport.Pool
	cache   *cache.Redis
	redis   *miniredis.Miniredis
	orderID uuid.UUID
	eventID uuid.UUID
	slug    string
}

func newPaymentCacheFixture(t *testing.T) paymentCacheFixture {
	t.Helper()
	pool := testsupport.RequirePool(t)
	srv := miniredis.RunT(t)

	c, err := cache.NewRedis("redis://"+srv.Addr()+"/0", time.Minute,
		testsupport.DiscardLogger(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ev := testsupport.SeedEvent(t, pool, "restore-night", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 7)
	ord := testsupport.SeedOrder(t, pool, "ORD-RESTORE", "PENDING")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 3)
	testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Budi", "budi@example.com")

	events := event.NewService(pool, event.NewRepository(pool), order.NewRepository(pool),
		testsupport.DiscardLogger()).WithCache(c)

	fulfiller := &spyFulfiller{}
	gw := &stubGateway{}
	svc := payment.NewService(
		pool,
		payment.NewRepository(pool),
		gw,
		orderAdapter{repo: order.NewRepository(pool)},
		quotaAdapter{svc: events},
		fulfiller,
		fulfiller,
		testsupport.DiscardLogger(),
	).WithCache(c, events) // events satisfies payment.EventScopeLookup

	return paymentCacheFixture{
		svc: svc, gateway: gw, events: events, pool: pool, cache: c, redis: srv,
		orderID: ord.ID, eventID: ev.ID, slug: ev.Slug,
	}
}

// settle drives a real settlement notification through the service, exactly as
// the provider would.
func (f paymentCacheFixture) settle(t *testing.T, ctx context.Context) {
	t.Helper()
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-RESTORE",
		TransactionID:     "tx-cache-1",
		TransactionStatus: "settlement",
		PaymentType:       "qris",
		RawPayload:        []byte(`{"transaction_status":"settlement"}`),
	}
	require.NoError(t, f.svc.HandleNotification(ctx, "midtrans", f.gateway.result.RawPayload, ""))
	f.svc.WaitForFulfillment()
}

func (f paymentCacheFixture) ticketListCached(t *testing.T) bool {
	t.Helper()
	_, ok, err := f.cache.Get(context.Background(), cache.TicketTypesPublicKey(f.eventID))
	require.NoError(t, err)
	return ok
}

func (f paymentCacheFixture) ordersListCached(t *testing.T) bool {
	t.Helper()
	_, ok, err := f.cache.Get(context.Background(), cache.OrdersAdminKey(nil, nil))
	require.NoError(t, err)
	return ok
}

// The core of US2's second half: an expiring order returns its quota, and the
// guest refreshing the event page sees it immediately.
func TestExpiryRestoresQuotaAndInvalidatesTheTicketList(t *testing.T) {
	f := newPaymentCacheFixture(t)
	ctx := context.Background()

	// Put the order past its deadline so the sweeper picks it up.
	_, err := f.pool.Exec(ctx,
		`UPDATE orders SET payment_expires_at = now() - interval '1 minute' WHERE id = $1`, f.orderID)
	require.NoError(t, err)

	warm, err := f.events.TicketTypesForEventSlug(ctx, f.slug)
	require.NoError(t, err)
	require.EqualValues(t, 7, warm[0].QuotaRemaining, "precondition: 3 of 10 are held")
	require.True(t, f.ticketListCached(t))

	n, err := f.svc.ExpireDueOrders(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.False(t, f.ticketListCached(t),
		"restored quota must invalidate the event's ticket list, resolved through EventScopeLookup")

	after, err := f.events.TicketTypesForEventSlug(ctx, f.slug)
	require.NoError(t, err)
	require.EqualValues(t, 10, after[0].QuotaRemaining,
		"the next read must show the restored quota")
}

// A settlement moves the order but restores no quota, so only the orders scope
// is bumped — the event's ticket list is still correct and must stay warm.
func TestSettlementInvalidatesOrdersButNotTheTicketList(t *testing.T) {
	f := newPaymentCacheFixture(t)
	ctx := context.Background()

	_, err := f.events.TicketTypesForEventSlug(ctx, f.slug)
	require.NoError(t, err)
	require.True(t, f.ticketListCached(t))

	// Warm the admin order list too.
	_, _, err = f.cache.Get(ctx, cache.OrdersAdminKey(nil, nil))
	require.NoError(t, err)
	require.NoError(t, f.cache.Set(ctx, cache.OrdersAdminKey(nil, nil), []byte(`[]`)))
	require.True(t, f.ordersListCached(t))

	f.settle(t, ctx)

	require.False(t, f.ordersListCached(t), "the order changed status")
	require.True(t, f.ticketListCached(t),
		"no quota moved, so the ticket list is still correct and must not be discarded")
}

// Providers retry. A replayed notification against an already-PAID order writes
// nothing, so it must invalidate nothing — otherwise every duplicate delivery
// would throw away a warm cache.
func TestReplayedNotificationDoesNotInvalidate(t *testing.T) {
	f := newPaymentCacheFixture(t)
	ctx := context.Background()

	f.settle(t, ctx)

	// Warm both lists after the first, legitimate transition.
	_, err := f.events.TicketTypesForEventSlug(ctx, f.slug)
	require.NoError(t, err)
	require.NoError(t, f.cache.Set(ctx, cache.OrdersAdminKey(nil, nil), []byte(`[]`)))
	require.True(t, f.ticketListCached(t))
	require.True(t, f.ordersListCached(t))

	// The retry.
	f.settle(t, ctx)

	require.True(t, f.ordersListCached(t),
		"an idempotent no-op wrote nothing and must discard nothing")
	require.True(t, f.ticketListCached(t))
}

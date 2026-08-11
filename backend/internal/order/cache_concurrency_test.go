package order_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
)

// SC-007: no availability figure ever shown to a guest may exceed the true
// remaining quota, under real concurrency.
//
// This is the test that would catch the design going wrong. The cached number is
// a display value; the sale is decided by the row-locked UPDATE inside the
// booking transaction. If those two ever swapped roles — if a cached figure were
// allowed to authorise a booking — this test oversells and fails.
func TestConcurrentBookingsNeverOversellOrOverstate(t *testing.T) {
	f := newQuotaCacheFixture(t)
	ctx := context.Background()

	const (
		quota    = 50
		bookers  = 200
		perOrder = 1
	)

	ev := testsupport.SeedEvent(t, f.pool, "stampede", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", quota)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "Terms apply.")

	// Warm the list so every booker races against a cache that is being
	// invalidated underneath them.
	_, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)

	var (
		succeeded  atomic.Int64
		overstated atomic.Int64
		wg         sync.WaitGroup
	)

	for i := 0; i < bookers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Every booker reads the list first, exactly as the UI does.
			listed, listErr := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
			if listErr == nil && len(listed) == 1 && listed[0].QuotaRemaining > quota {
				// A figure above the original allocation could only come from a
				// cache serving something the database never said.
				overstated.Add(1)
			}

			_, bookErr := f.orders.Book(ctx, order.BookRequest{
				EventID: ev.ID,
				Items:   []order.CheckoutItem{{TicketTypeID: &tt.ID, Quantity: perOrder}},
			})
			if bookErr == nil {
				succeeded.Add(1)
			}
		}()
	}
	wg.Wait()

	require.Zero(t, overstated.Load(),
		"no guest may ever be shown more tickets than were ever allocated")

	require.EqualValues(t, quota, succeeded.Load(),
		"exactly the allocated quota may be sold — no more (oversell) and no fewer (lost sale)")

	// And the database agrees, which is the only opinion that counts.
	var remaining int32
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT quota FROM ticket_types WHERE id = $1`, tt.ID).Scan(&remaining))
	require.Zero(t, remaining, "quota lands exactly at zero and never goes negative")

	// After the dust settles the cached list agrees with the database.
	final, err := f.events.TicketTypesForEventSlug(ctx, ev.Slug)
	require.NoError(t, err)
	require.EqualValues(t, 0, final[0].QuotaRemaining,
		"the last invalidation wins; the cache converges on the truth")
}

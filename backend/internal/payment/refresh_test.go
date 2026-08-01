package payment_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
)

// setDeadline puts the fixture order's payment deadline at a chosen offset from
// now, so expiry can be exercised without waiting one out.
func setDeadline(t *testing.T, f webhookFixture, offset time.Duration) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET payment_expires_at = now() + $2::interval WHERE id = $1`,
		f.orderID, offset.String())
	require.NoError(t, err)
}

// statusFromProvider makes the provider's status read say something specific,
// which is the whole point of reconciliation: it can differ from what our own
// database currently believes.
func statusFromProvider(f webhookFixture, transactionStatus, fraudStatus string) {
	f.gateway.statusResult = &payment.WebhookResult{
		OrderNumber:       "ORD-WEBHOOK",
		TransactionID:     "tx-refresh",
		TransactionStatus: transactionStatus,
		FraudStatus:       fraudStatus,
		PaymentType:       "qris",
		RawPayload:        []byte(`{"transaction_status":"` + transactionStatus + `"}`),
	}
}

// --- Reconciliation -------------------------------------------------------

func TestRefreshStatusAppliesASettlementTheWebhookNeverDelivered(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, 15*time.Minute)
	statusFromProvider(f, "settlement", "")

	result, err := f.svc.RefreshStatus(context.Background(), "ORD-WEBHOOK")
	f.svc.WaitForFulfillment()

	require.NoError(t, err)
	assert.Equal(t, "PAID", result.Status)
	assert.True(t, result.Changed, "the guest must be told their payment landed")
	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued, "a reconciliation fulfils exactly like a webhook")
	assert.Equal(t, 1, emailed)
}

func TestRefreshStatusReportsNoChangeWhilePaymentIsStillPending(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, 15*time.Minute)
	statusFromProvider(f, "pending", "")

	result, err := f.svc.RefreshStatus(context.Background(), "ORD-WEBHOOK")

	require.NoError(t, err)
	assert.Equal(t, "PENDING", result.Status)
	assert.False(t, result.Changed, "the button must be able to say 'still waiting'")
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

// Every provider answer is recorded, including one that changes nothing: the
// payments table is the audit trail for "what did we know, and when".
func TestRefreshStatusRecordsAnAuditRow(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, 15*time.Minute)
	statusFromProvider(f, "pending", "")

	_, err := f.svc.RefreshStatus(context.Background(), "ORD-WEBHOOK")
	require.NoError(t, err)

	var rows int
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM payments WHERE order_id = $1`, f.orderID).Scan(&rows))
	assert.Equal(t, 1, rows)
}

// The point of sharing the webhook's transition: pressing the button twice, or
// pressing it after the webhook already landed, must not issue a second set of
// tickets or send a second email.
func TestRefreshStatusIsIdempotentAcrossRepeatedCalls(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, 15*time.Minute)
	statusFromProvider(f, "settlement", "")
	ctx := context.Background()

	first, err := f.svc.RefreshStatus(ctx, "ORD-WEBHOOK")
	require.NoError(t, err)
	second, err := f.svc.RefreshStatus(ctx, "ORD-WEBHOOK")
	require.NoError(t, err)
	f.svc.WaitForFulfillment()

	assert.True(t, first.Changed)
	assert.False(t, second.Changed, "nothing changed the second time")
	assert.Equal(t, "PAID", second.Status)

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued, "exactly one set of tickets per order")
	assert.Equal(t, 1, emailed, "exactly one confirmation email per order")
}

func TestRefreshStatusOnAnAlreadyPaidOrderNeverCallsTheProvider(t *testing.T) {
	f := newWebhookFixture(t)
	require.NoError(t, f.notify(t, "settlement", ""))
	f.gateway.statusErr = errors.New("the provider must not be called")

	result, err := f.svc.RefreshStatus(context.Background(), "ORD-WEBHOOK")

	require.NoError(t, err)
	assert.Equal(t, "PAID", result.Status)
	assert.False(t, result.Changed)
}

func TestRefreshStatusSurfacesAnUnreachableProvider(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, 15*time.Minute)
	f.gateway.statusErr = errors.New("connection refused")

	_, err := f.svc.RefreshStatus(context.Background(), "ORD-WEBHOOK")

	require.Error(t, err)
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID),
		"a provider we cannot reach must leave the order untouched")
}

func TestRefreshStatusReportsAnUnknownOrder(t *testing.T) {
	f := newWebhookFixture(t)

	_, err := f.svc.RefreshStatus(context.Background(), "ORD-NOPE")

	assert.ErrorIs(t, err, payment.ErrOrderNotFound)
}

// An order we hold that the provider has never heard of is not "no such order":
// the guest is looking at it. Reporting it as not-found would tell them their
// own order does not exist.
func TestRefreshStatusDistinguishesAnOrderTheProviderDoesNotKnow(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, 15*time.Minute)
	f.gateway.statusErr = payment.ErrOrderNotFound

	_, err := f.svc.RefreshStatus(context.Background(), "ORD-WEBHOOK")

	assert.ErrorIs(t, err, payment.ErrProviderHasNoRecord)
	assert.NotErrorIs(t, err, payment.ErrOrderNotFound)
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

// Past the deadline the answer is "expired" regardless of what the provider
// says, and the guest gets it immediately rather than at the next sweep.
func TestRefreshStatusExpiresAnOrderPastItsDeadline(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, -1*time.Minute)
	f.gateway.statusErr = errors.New("the provider must not be consulted for a lapsed order")

	result, err := f.svc.RefreshStatus(context.Background(), "ORD-WEBHOOK")

	require.NoError(t, err)
	assert.Equal(t, "EXPIRED", result.Status)
	assert.True(t, result.Changed)
	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketID),
		"the 3 reserved seats go back on sale")
}

// --- The expiry sweep -----------------------------------------------------

func TestExpireDueOrdersReleasesQuotaForLapsedOrders(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, -1*time.Minute)

	expired, err := f.svc.ExpireDueOrders(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 1, expired)
	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketID))
}

func TestExpireDueOrdersLeavesOrdersInsideTheirWindowAlone(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, 5*time.Minute)

	expired, err := f.svc.ExpireDueOrders(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, expired)
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketID))
}

func TestExpireDueOrdersIgnoresOrdersThatAreAlreadyFinal(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, -1*time.Minute)
	require.NoError(t, f.notify(t, "settlement", ""))

	expired, err := f.svc.ExpireDueOrders(context.Background())

	require.NoError(t, err)
	assert.Equal(t, 0, expired, "a paid order is not expired just because its code lapsed")
	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

func TestExpireDueOrdersIsANoOpWhenNothingIsDue(t *testing.T) {
	f := newWebhookFixture(t)

	expired, err := f.svc.ExpireDueOrders(context.Background())

	require.NoError(t, err)
	assert.Zero(t, expired)
}

// The guarded transition is what makes overlapping paths safe. Two sweeps at
// once must restore the seats once in total, not twice.
func TestConcurrentSweepsRestoreQuotaExactlyOnce(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, -1*time.Minute)

	const sweeps = 8
	var wg sync.WaitGroup
	counts := make([]int, sweeps)
	errs := make([]error, sweeps)
	for i := range sweeps {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counts[i], errs[i] = f.svc.ExpireDueOrders(context.Background())
		}()
	}
	wg.Wait()

	total := 0
	for i := range sweeps {
		require.NoError(t, errs[i])
		total += counts[i]
	}

	assert.Equal(t, 1, total, "exactly one sweep may apply the transition")
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketID),
		"quota must not be restored twice")
}

// A sweep and a provider `expire` notification arriving together are the same
// race as two sweeps, through the same guard.
func TestASweepRacingAnExpireWebhookRestoresQuotaOnce(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, -1*time.Minute)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = f.svc.ExpireDueOrders(context.Background())
	}()
	go func() {
		defer wg.Done()
		_ = f.notify(t, "expire", "")
	}()
	wg.Wait()

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketID))
}

// --- Late success for a settled order (spec US4 scenario 5) ---------------

// The order is NOT flipped back to paid: the seats have already gone back on
// sale and may have been resold. It must be logged loudly instead so an admin
// can reconcile it by hand.
func TestASuccessForAnAlreadyExpiredOrderDoesNotResurrectIt(t *testing.T) {
	f := newWebhookFixture(t)
	setDeadline(t, f, -1*time.Minute)
	_, err := f.svc.ExpireDueOrders(context.Background())
	require.NoError(t, err)

	require.NoError(t, f.notify(t, "settlement", ""))

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketID),
		"quota released on expiry must not be re-deducted")

	issued, emailed := f.fulfiller.counts()
	assert.Zero(t, issued, "no tickets for seats we no longer hold")
	assert.Zero(t, emailed)
}

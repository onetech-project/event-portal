package payment_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pgauto/cdtc/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
)

// --- Settle adapters -------------------------------------------------------

// reserverAdapter mirrors cmd/api: taking quota back is a capability only the
// settle path needs, and the event domain's refusal is translated onto the
// payment domain's own so it reads as "these seats are gone".
type reserverAdapter struct{ svc *event.Service }

func (a reserverAdapter) ReserveQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	err := a.svc.CheckAndDeductQuota(ctx, tx, ticketTypeID, qty)
	if err != nil && err.Error() == event.ErrInsufficientQuota.Error() {
		return payment.ErrQuotaUnavailable
	}
	return err
}

func (a reserverAdapter) RemainingQuota(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]payment.TicketTypeQuota, error) {
	records, err := a.svc.TicketTypeQuotas(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]payment.TicketTypeQuota, len(records))
	for id, rec := range records {
		out[id] = payment.TicketTypeQuota{Name: rec.Name, Remaining: rec.Remaining}
	}
	return out, nil
}

// SettleExpired binds both statuses here exactly as the composition root does,
// which is what makes FR-019d unreachable rather than merely unasked-for.
func (a orderAdapter) SettleExpired(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) (bool, error) {
	return a.repo.UpdateOrderStatusFrom(ctx, tx, orderID, "EXPIRED", "PAID")
}

func (a orderAdapter) OrderByID(ctx context.Context, orderID uuid.UUID) (payment.OrderRef, error) {
	rec, err := a.repo.GetOrderByID(ctx, orderID)
	if err != nil {
		return payment.OrderRef{}, payment.ErrOrderNotFound
	}
	return payment.OrderRef{
		ID:               rec.ID,
		OrderNumber:      rec.OrderNumber,
		Status:           rec.Status,
		PaymentExpiresAt: rec.PaymentExpiresAt,
	}, nil
}

// --- Settle after expiry (T107–T110, FR-019/FR-019b) ----------------------

// The whole recovery flow in one test: an order stranded by a lost notification,
// its seats given back by the sweeper, settled by the gateway redelivering the
// notification it lost. Nothing here asserts a payment — the gateway does.
func TestARedeliveredCompletionSettlesAnExpiredOrderAndRetakesItsSeats(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Expired))
	require.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"the expiry returned the seats")

	require.NoError(t, f.notify(t, status.Completed))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"settling takes the seats back so the remaining count stays truthful")
	assert.Equal(t, 1, f.markerCount(t, payment.MarkerSettledAfterExpiry))

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued, "through the same path a first-time notification uses")
	assert.Equal(t, 1, emailed)
}

// The marker answers the question an oversold event provokes: why does this
// ticket type's issued count exceed the allocation it was given?
func TestSettlingRecordsThePriorStatusAndTheLinesRetaken(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, status.Expired))
	require.NoError(t, f.notify(t, status.Completed))

	records, err := f.svc.OrderNotifications(ctx, f.orderID)
	require.NoError(t, err)

	var found bool
	for _, r := range records {
		if r.Status != payment.MarkerSettledAfterExpiry {
			continue
		}
		found = true
		assert.True(t, r.IsMarker)
		assert.Contains(t, string(r.RawPayload), "EXPIRED", "what the order was when it arrived")
		assert.Contains(t, string(r.RawPayload), f.ticketIDs[0].String(), "and which seats came back out")
	}
	assert.True(t, found, "a settle out of expiry is not routine traffic and must leave a record")
}

// A package order holds seats in every constituent ticket type. All of them have
// to be re-taken, or the count lies about a type nobody thought to check.
func TestSettlingABundleOrderRetakesEveryConstituent(t *testing.T) {
	f := newBundleWebhookFixture(t)

	require.NoError(t, f.notifyBundle(t, status.Expired))
	for _, id := range f.ticketIDs {
		require.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, id))
	}

	require.NoError(t, f.notifyBundle(t, status.Completed))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	for _, id := range f.ticketIDs {
		assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, id),
			"every constituent gives its seats back up")
	}
}

// --- Refusal on a shortfall (T108, FR-019c) -------------------------------

// The seats were resold while the order sat expired. Settling would oversell, so
// nothing moves — and the answer is 200-with-a-body rather than a refusal,
// because a retry would fail identically and burn the gateway's whole budget.
func TestSettlingIsRefusedWhenTheSeatsHaveBeenResold(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, status.Expired))

	// Somebody else buys every returned seat.
	_, err := f.pool.Exec(ctx, `UPDATE ticket_types SET quota = 0 WHERE id = $1`, f.ticketIDs[0])
	require.NoError(t, err)

	err = f.notify(t, status.Completed)

	var refused *payment.SettleRefusedError
	require.ErrorAs(t, err, &refused, "the caller must be able to answer 200 with the reason")
	assert.Equal(t, "ORD-WEBHOOK", refused.OrderNumber)

	require.Len(t, refused.Shortfalls, 1, "one ticket type cannot cover its hold")
	assert.Equal(t, f.ticketIDs[0], refused.Shortfalls[0].TicketTypeID)
	assert.Equal(t, int32(3), refused.Shortfalls[0].Required)
	assert.Equal(t, int32(0), refused.Shortfalls[0].Remaining)
	assert.Equal(t, "Regular", refused.Shortfalls[0].TicketTypeName,
		"named, because an operator has to find it in the editor")

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID),
		"a refused settle leaves the order exactly where it was")
	assert.Equal(t, int32(0), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"and moves no quota — not even into the negative the CHECK would reject")
	assert.Equal(t, 1, f.markerCount(t, payment.MarkerSettleRefusedNoQuota))

	issued, emailed := f.fulfiller.counts()
	assert.Zero(t, issued, "nothing may be issued against seats somebody else holds")
	assert.Zero(t, emailed)
}

// A partial settle is never an outcome. With one constituent short, the whole
// order stays put — including the constituent that could have been covered.
func TestABundleShortInOneConstituentSettlesNothing(t *testing.T) {
	f := newBundleWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notifyBundle(t, status.Expired))
	_, err := f.pool.Exec(ctx, `UPDATE ticket_types SET quota = 1 WHERE id = $1`, f.ticketIDs[1])
	require.NoError(t, err)

	err = f.notifyBundle(t, status.Completed)

	var refused *payment.SettleRefusedError
	require.ErrorAs(t, err, &refused)
	require.Len(t, refused.Shortfalls, 1, "only the short one is reported; the other needs no top-up")
	assert.Equal(t, f.ticketIDs[1], refused.Shortfalls[0].TicketTypeID)
	assert.Equal(t, int32(1), refused.Shortfalls[0].Remaining)

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"the constituent that COULD have been covered keeps its seats in the pool — "+
			"half an order is not something this system can issue tickets for")
}

// The operator's loop: refused, quota topped up, resend requested again.
func TestToppingUpQuotaLetsTheNextResendSettle(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, status.Expired))
	_, err := f.pool.Exec(ctx, `UPDATE ticket_types SET quota = 0 WHERE id = $1`, f.ticketIDs[0])
	require.NoError(t, err)

	var refused *payment.SettleRefusedError
	require.ErrorAs(t, f.notify(t, status.Completed), &refused)

	// Ops adds exactly the shortfall the refusal named.
	_, err = f.pool.Exec(ctx, `UPDATE ticket_types SET quota = $2 WHERE id = $1`,
		f.ticketIDs[0], refused.Shortfalls[0].Required)
	require.NoError(t, err)

	require.NoError(t, f.notify(t, status.Completed))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(0), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"the added seats went straight to the order they were added for")

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued)
	assert.Equal(t, 1, emailed)
}

// --- Idempotency and concurrency (T110, FR-012d, US6 scenario 5) -----------

// A resend is just one more delivery, so the idempotency that makes replays safe
// is the same idempotency that makes recovery safe. There is no separate replay
// path to get right.
func TestReplayingASettledNotificationChangesNothingFurther(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Expired))
	require.NoError(t, f.notify(t, status.Completed))
	require.NoError(t, f.notify(t, status.Completed))
	require.NoError(t, f.notify(t, status.Completed))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"the seats are deducted once however many deliveries arrive")
	assert.Equal(t, 1, f.markerCount(t, payment.MarkerSettledAfterExpiry))

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued, "no second set of tickets")
	assert.Equal(t, 1, emailed, "no second email")
}

// Ops presses resend twice in quick succession. The guarded UPDATE is what
// separates them: one transition applies, the other finds nothing to do.
func TestTwoConcurrentRedeliveriesSettleExactlyOnce(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Expired))

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := notification("ORD-WEBHOOK", status.Completed)
			f.gateway.result = result
			//nolint:errcheck // the losing deliveries are no-ops; the assertions below are the point
			_ = f.svc.HandleNotification(context.Background(), "manjo", result.RawPayload, "token")
		}()
	}
	wg.Wait()
	f.svc.WaitForFulfillment()

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"four deliveries, one deduction — or the event is oversold by three seats")
	assert.Equal(t, 1, f.markerCount(t, payment.MarkerSettledAfterExpiry))

	issued, _ := f.fulfiller.counts()
	assert.Equal(t, 1, issued)
}

// --- Per-order history (FR-022c) ------------------------------------------

// Refused notifications appear too. They are often the whole explanation, and a
// history that only showed accepted ones would hide the reason.
func TestOrderNotificationsIncludeRefusedOnesAndMarkers(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Completed))
	require.ErrorIs(t, f.notify(t, status.Cancel), payment.ErrNotificationContradiction)

	records, err := f.svc.OrderNotifications(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, records, 3, "two payloads plus the marker raised about the second")

	var markers, payloads int
	for _, r := range records {
		if r.IsMarker {
			markers++
		} else {
			payloads++
			assert.NotEmpty(t, r.RawPayload, "staff match these against the gateway dashboard")
		}
	}
	assert.Equal(t, 1, markers)
	assert.Equal(t, 2, payloads)
}

// --- Holds versus remaining (T113, FR-022e) -------------------------------

// The figure an operator needs before requesting a resend, and the one number
// the rest of the admin surface cannot give them.
func TestOrderHoldsReportsWhatIsHeldAgainstWhatRemains(t *testing.T) {
	f := newWebhookFixture(t)

	holds, err := f.svc.OrderHolds(context.Background(), f.orderID)
	require.NoError(t, err)
	require.Len(t, holds, 1)

	assert.Equal(t, f.ticketIDs[0], holds[0].TicketTypeID)
	assert.Equal(t, "Regular", holds[0].TicketTypeName)
	assert.Equal(t, int32(3), holds[0].Held)
	assert.Equal(t, int32(7), holds[0].Remaining)
}

// After an expiry the seats are back in the pool, so `remaining` rises while the
// order still holds the same three. That gap is exactly what has to be closed
// before a resend can settle.
func TestOrderHoldsShowsTheGapAResendHasToClose(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, status.Expired))
	_, err := f.pool.Exec(ctx, `UPDATE ticket_types SET quota = 1 WHERE id = $1`, f.ticketIDs[0])
	require.NoError(t, err)

	holds, err := f.svc.OrderHolds(ctx, f.orderID)
	require.NoError(t, err)
	require.Len(t, holds, 1)

	assert.Equal(t, int32(3), holds[0].Held)
	assert.Equal(t, int32(1), holds[0].Remaining, "two short; the top-up is the difference")
}

// A bundle holds seats in several types at once, and every one of them has to
// cover its hold before the settle succeeds.
func TestOrderHoldsCoversEveryConstituentOfABundle(t *testing.T) {
	f := newBundleWebhookFixture(t)

	holds, err := f.svc.OrderHolds(context.Background(), f.orderID)
	require.NoError(t, err)
	assert.Len(t, holds, 2, "an operator topping up only the first would be refused again")
}

// The order-level value an operator reads off the payment view resolves out of
// this sequence: exactly one row carries the reference, so a client finds it
// without a second request and without a column that would be blank everywhere
// else (spec 017 FR-014, FR-015).
func TestOrderNotificationsCarryTheReferenceOnTheSessionOpenRowAlone(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.RecordSessionOpened(ctx, "ORD-WEBHOOK", payment.PaymentSession{
		ProviderRef: "A487336098162400838C", QRString: "qr",
		ExpiresAt: time.Now().Add(14 * time.Minute), ExpiryFromGateway: true,
	}))
	require.NoError(t, f.notify(t, status.Completed))

	records, err := f.svc.OrderNotifications(ctx, f.orderID)
	require.NoError(t, err)
	require.Len(t, records, 2, "the session-open marker and the settling payload")

	var carrying int
	for _, r := range records {
		if r.Status == payment.MarkerSessionOpened {
			assert.True(t, r.IsMarker, "our own statement, not something the gateway said")
			assert.Equal(t, "A487336098162400838C", r.ExtRefID)
			carrying++
			continue
		}
		assert.Empty(t, r.ExtRefID, "a callback does not carry the reference")
	}
	assert.Equal(t, 1, carrying, "stated once for the order, never repeated per row")
}

// An order that never reached checkout has no reference and no row to hold one.
// It must read as absent rather than as an error (spec 017 FR-018, SC-006).
func TestOrderNotificationsAreEmptyForAnOrderThatNeverOpenedASession(t *testing.T) {
	f := newWebhookFixture(t)

	records, err := f.svc.OrderNotifications(context.Background(), f.orderID)
	require.NoError(t, err)
	assert.Empty(t, records)
}

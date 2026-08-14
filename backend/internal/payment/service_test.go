package payment_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pgauto/cdtc/status"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
)

// --- Test doubles ---------------------------------------------------------

type stubGateway struct {
	result *payment.WebhookResult
	err    error
	// session/createErr/createCalls back the session-open tests.
	session     payment.PaymentSession
	createErr   error
	createCalls []payment.TransactionRequest
}

func (g *stubGateway) Name() string { return "manjo" }

func (g *stubGateway) CreateTransaction(_ context.Context, req payment.TransactionRequest) (payment.PaymentSession, error) {
	g.createCalls = append(g.createCalls, req)
	if g.createErr != nil {
		return payment.PaymentSession{}, g.createErr
	}
	if g.session.QRString == "" {
		return payment.PaymentSession{}, errors.New("no session configured")
	}
	return g.session, nil
}

func (g *stubGateway) VerifyWebhook([]byte, string) (*payment.WebhookResult, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.result, nil
}

// orderAdapter is the same shape cmd/api wires: it adapts the order domain onto
// the contract the payment domain declares.
type orderAdapter struct{ repo *order.Repository }

func (a orderAdapter) OrderByNumber(ctx context.Context, number string) (payment.OrderRef, error) {
	rec, err := a.repo.GetOrderByNumber(ctx, number)
	if errors.Is(err, order.ErrNotFound) {
		return payment.OrderRef{}, payment.ErrOrderNotFound
	}
	if err != nil {
		return payment.OrderRef{}, err
	}
	return payment.OrderRef{
		ID:               rec.ID,
		OrderNumber:      rec.OrderNumber,
		Status:           rec.Status,
		PaymentExpiresAt: rec.PaymentExpiresAt,
	}, nil
}

func (a orderAdapter) DueForExpiry(ctx context.Context, now time.Time, limit int32) ([]payment.OrderRef, error) {
	candidates, err := a.repo.ListOrdersDueForExpiry(ctx, now, limit)
	if err != nil {
		return nil, err
	}
	out := make([]payment.OrderRef, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, payment.OrderRef{
			ID:               candidate.ID,
			OrderNumber:      candidate.OrderNumber,
			Status:           candidate.Status,
			PaymentExpiresAt: candidate.PaymentExpiresAt,
		})
	}
	return out, nil
}

func (a orderAdapter) QuotaHolds(ctx context.Context, tx pgx.Tx, orderID uuid.UUID) ([]payment.QuotaHold, error) {
	items, err := a.repo.ListQuotaHoldsByOrderID(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	out := make([]payment.QuotaHold, 0, len(items))
	for _, item := range items {
		out = append(out, payment.QuotaHold{TicketTypeID: item.TicketTypeID, Quantity: item.Quantity})
	}
	return out, nil
}

func (a orderAdapter) UpdateStatusIfPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, orderStatus string) (bool, error) {
	return a.repo.UpdateOrderStatusIfPending(ctx, tx, orderID, orderStatus)
}

type quotaAdapter struct{ svc *event.Service }

func (a quotaAdapter) RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error {
	return a.svc.RestoreQuota(ctx, tx, ticketTypeID, qty)
}

type spyFulfiller struct {
	mu       sync.Mutex
	issued   []uuid.UUID
	emailed  []uuid.UUID
	issueErr error
}

func (f *spyFulfiller) IssueTicketsForOrder(_ context.Context, orderID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issueErr != nil {
		return f.issueErr
	}
	f.issued = append(f.issued, orderID)
	return nil
}

func (f *spyFulfiller) SendTicketEmail(_ context.Context, orderID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emailed = append(f.emailed, orderID)
	return nil
}

func (f *spyFulfiller) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.issued), len(f.emailed)
}

// --- Fixture --------------------------------------------------------------

type webhookFixture struct {
	svc       *payment.Service
	pool      *testsupport.Pool
	gateway   *stubGateway
	fulfiller *spyFulfiller
	orderID   uuid.UUID
	ticketIDs []uuid.UUID
}

// newWebhookFixture seeds a PENDING order for 3 tickets whose quota has already
// been deducted at checkout: quota 7 remaining out of an original 10.
func newWebhookFixture(t *testing.T) webhookFixture {
	t.Helper()
	pool := testsupport.RequirePool(t)

	ev := testsupport.SeedEvent(t, pool, "webhook-night", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "100000.00", 7)
	ord := testsupport.SeedOrder(t, pool, "ORD-WEBHOOK", "PENDING")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 3)
	testsupport.SeedAttendee(t, pool, ord.ID, tt.ID, "Budi", "budi@example.com")

	gw := &stubGateway{}
	fulfiller := &spyFulfiller{}

	svc := payment.NewService(
		pool,
		payment.NewRepository(pool),
		gw,
		orderAdapter{repo: order.NewRepository(pool)},
		quotaAdapter{svc: event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())},
		reserverAdapter{svc: event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())},
		orderAdapter{repo: order.NewRepository(pool)},
		fulfiller,
		fulfiller,
		testsupport.DiscardLogger(),
	)

	return webhookFixture{
		svc: svc, pool: pool, gateway: gw, fulfiller: fulfiller,
		orderID: ord.ID, ticketIDs: []uuid.UUID{tt.ID},
	}
}

// newBundleWebhookFixture seeds a PENDING order holding three package units
// across two constituents; each constituent was deducted from 10 down to 7 at
// checkout (3 units × quantity_per_unit 1 each).
func newBundleWebhookFixture(t *testing.T) webhookFixture {
	t.Helper()
	pool := testsupport.RequirePool(t)

	ev := testsupport.SeedEvent(t, pool, "webhook-bundle", "PUBLISHED")
	day1 := testsupport.SeedTicketType(t, pool, ev.ID, "Day 1", "30000.00", 7)
	day2 := testsupport.SeedTicketType(t, pool, ev.ID, "Day 2", "20000.00", 7)
	pkg := testsupport.SeedPackage(t, pool, ev.ID, "Day 1+2", "50000.00", true)
	testsupport.SeedPackageTicket(t, pool, pkg.ID, day1.ID, ev.ID, 1)
	testsupport.SeedPackageTicket(t, pool, pkg.ID, day2.ID, ev.ID, 1)
	ord := testsupport.SeedOrder(t, pool, "ORD-WEBHOOK-BUNDLE", "PENDING")
	// 3 package units × 1 seat per constituent = 3 deducted from each (10 → 7).
	testsupport.SeedOrderItemPackage(t, pool, ord.ID, pkg.ID, 3, decimal.NewFromInt(50000))
	for i := 0; i < 3; i++ {
		testsupport.SeedAttendeeWithPackage(t, pool, ord.ID, day1.ID, uuid.NullUUID{UUID: pkg.ID, Valid: true}, "Budi", "budi@example.com")
	}

	gw := &stubGateway{}
	fulfiller := &spyFulfiller{}

	svc := payment.NewService(
		pool,
		payment.NewRepository(pool),
		gw,
		orderAdapter{repo: order.NewRepository(pool)},
		quotaAdapter{svc: event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())},
		reserverAdapter{svc: event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())},
		orderAdapter{repo: order.NewRepository(pool)},
		fulfiller,
		fulfiller,
		testsupport.DiscardLogger(),
	)

	return webhookFixture{
		svc: svc, pool: pool, gateway: gw, fulfiller: fulfiller,
		orderID: ord.ID, ticketIDs: []uuid.UUID{day1.ID, day2.ID},
	}
}

// notification builds an authenticated result the way the real adapter would,
// including the deposit flag every genuine payment carries.
func notification(reference string, s status.Status) *payment.WebhookResult {
	return &payment.WebhookResult{
		OrderNumber:       reference,
		TransactionID:     "A48593" + s.String(),
		Status:            s,
		StatusPresent:     true,
		TransactionStatus: s.String(),
		PaymentType:       "qris",
		TransactionType:   "DEPOSIT",
		IsDeposit:         true,
		RawPayload:        []byte(fmt.Sprintf(`{"ri":%q,"s":%d,"tt":0}`, reference, s)),
	}
}

func (f webhookFixture) deliver(t *testing.T, result *payment.WebhookResult) error {
	t.Helper()
	f.gateway.result = result
	err := f.svc.HandleNotification(context.Background(), "manjo", result.RawPayload, "token")
	f.svc.WaitForFulfillment()
	return err
}

func (f webhookFixture) notify(t *testing.T, s status.Status) error {
	t.Helper()
	return f.deliver(t, notification("ORD-WEBHOOK", s))
}

func (f webhookFixture) notifyBundle(t *testing.T, s status.Status) error {
	t.Helper()
	return f.deliver(t, notification("ORD-WEBHOOK-BUNDLE", s))
}

// markerCount reports how many rows carry a given marker.
func (f webhookFixture) markerCount(t *testing.T, marker string) int {
	t.Helper()
	var count int
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM payments WHERE order_id = $1 AND status = $2`,
		f.orderID, marker).Scan(&count))
	return count
}

// --- Authentication -------------------------------------------------------

func TestCallbackRejectsAnUnauthenticatedNotification(t *testing.T) {
	f := newWebhookFixture(t)
	f.gateway.err = payment.ErrInvalidSignature

	err := f.svc.HandleNotification(context.Background(), "manjo", []byte(`{}`), "wrong")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID),
		"an unauthenticated notification must change nothing")
}

// --- Paid path ------------------------------------------------------------

func TestCompletedMarksTheOrderPaidWithoutTouchingQuota(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Completed))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"quota was already deducted at checkout and must not move again")
}

func TestPaidOrderTriggersTicketGenerationAndEmail(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Completed))

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued)
	assert.Equal(t, 1, emailed)
}

func TestFulfillmentIsSkippedWhenTicketGenerationFails(t *testing.T) {
	f := newWebhookFixture(t)
	f.fulfiller.issueErr = errors.New("db down")

	require.NoError(t, f.notify(t, status.Completed),
		"the gateway is still acknowledged; fulfilment is retried out of band")

	_, emailed := f.fulfiller.counts()
	assert.Zero(t, emailed, "no email may be sent for tickets that were never issued")
}

// --- Idempotency (Constitution Principle IV, FR-012d, SC-004) -------------

// The gateway retries any non-200 three times and delivers at least once, so an
// outcome must survive four deliveries. Ten is deliberate overkill.
func TestRepeatedDeliveryIssuesOneSetOfTicketsAndOneEmail(t *testing.T) {
	f := newWebhookFixture(t)

	for i := 0; i < 10; i++ {
		require.NoError(t, f.notify(t, status.Completed), "delivery %d", i+1)
	}

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued, "an already-PAID order must not be reprocessed")
	assert.Equal(t, 1, emailed, "the buyer must not receive a second email")
}

// A repeat of the recorded outcome is the NORMAL case under at-least-once
// delivery, so it must raise no signal at all — doing so would bury the genuine
// contradictions below in routine retry noise.
func TestRepeatedCompletedRaisesNoMarker(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Completed))
	require.NoError(t, f.notify(t, status.Completed))

	assert.Zero(t, f.markerCount(t, payment.MarkerDisputed))
	assert.Zero(t, f.markerCount(t, payment.MarkerSettledAfterExpiry))
}

func TestANotificationForAnAlreadyPaidOrderIsStillLogged(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, status.Completed))
	require.NoError(t, f.notify(t, status.Completed))

	var count int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM payments WHERE order_id = $1`, f.orderID).Scan(&count))
	assert.Equal(t, 2, count, "the audit trail records every notification received")
}

// --- Contradiction (FR-016b/c/d) ------------------------------------------

// PAID is terminal for every automated path. A notification saying the payment
// failed does not reverse it — the seats are gone and the buyer holds valid
// tickets — but it must reach a human, distinguishably from retry traffic.
func TestTerminalFailureAfterPaidLeavesTheOrderPaidAndRaisesADispute(t *testing.T) {
	for _, s := range []status.Status{status.Cancel, status.Reject, status.Expired} {
		t.Run(s.String(), func(t *testing.T) {
			f := newWebhookFixture(t)

			require.NoError(t, f.notify(t, status.Completed))
			require.ErrorIs(t, f.notify(t, s), payment.ErrNotificationContradiction,
				"a contradiction names itself in the response (FR-012g)")

			assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID),
				"nothing but an attributable staff action may leave PAID")
			assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
				"quota must not be returned for seats whose tickets are live")
			assert.Equal(t, 1, f.markerCount(t, payment.MarkerDisputed))
		})
	}
}

// The marker is written IN ADDITION to the audit row for the payload that
// triggered it, never instead of it (FR-018).
func TestADisputeKeepsTheAuditRowForTheContradictingPayload(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, status.Completed))
	require.ErrorIs(t, f.notify(t, status.Cancel), payment.ErrNotificationContradiction)

	var raw int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM payments WHERE order_id = $1 AND status = $2`,
		f.orderID, status.Cancel.String()).Scan(&raw))
	assert.Equal(t, 1, raw, "the contradicting notification keeps its own audit row")
	assert.Equal(t, 1, f.markerCount(t, payment.MarkerDisputed))
}

// --- A completion for a cancelled order (FR-019d) --------------------------

// Cancellation is the GATEWAY's verdict, or the consequence of a code that never
// reached the guest. Reversing it would contradict the source of payment truth
// this design rests on, so the order stands and a person decides about the money.
//
// The expired case is the exact opposite and lives in reconcile_test.go: expiry
// is OUR verdict, so a completion reverses it. That the two arrive here looking
// identical — a released order with money against it — is why the split is
// tested rather than assumed.
func TestCompletedForACancelledOrderIsNotRevived(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Cancel))
	require.ErrorIs(t, f.notify(t, status.Completed), payment.ErrNotificationOrderCancelled,
		"a completion for a cancelled order is reported, never silently dropped (FR-012g)")

	assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"the seats stay in the pool; nothing takes them back for an order that is not revived")

	issued, emailed := f.fulfiller.counts()
	assert.Zero(t, issued, "no ticket may be issued against a cancelled order")
	assert.Zero(t, emailed)
	assert.Zero(t, f.markerCount(t, payment.MarkerSettledAfterExpiry))
}

// No marker is written for this case, deliberately: the audit row for the
// completion payload is already on the order's own record, and that is where
// anyone looking the order up will find it.
func TestCompletedForACancelledOrderKeepsItsAuditRow(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, status.Cancel))
	require.ErrorIs(t, f.notify(t, status.Completed), payment.ErrNotificationOrderCancelled)

	var raw int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM payments WHERE order_id = $1 AND status = $2`,
		f.orderID, status.Completed.String()).Scan(&raw))
	assert.Equal(t, 1, raw, "the payment we refused to honour is still recorded in full")
}

// --- Quota-restoring outcomes (US3) ---------------------------------------

func TestCancelRestoresQuotaAndCancelsTheOrder(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Cancel))

	assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
}

func TestExpiredRestoresQuotaAndExpiresTheOrder(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Expired))

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
}

func TestRejectAndCancelBothCancelAndRestore(t *testing.T) {
	for _, s := range []status.Status{status.Reject, status.Cancel} {
		t.Run(s.String(), func(t *testing.T) {
			f := newWebhookFixture(t)

			require.NoError(t, f.notify(t, s))

			assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
			assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
		})
	}
}

// The status change and its quota restoration share one transaction, guarded on
// the order still being PENDING, so a replayed cancel cannot restore twice.
func TestReplayedCancelDoesNotRestoreQuotaTwice(t *testing.T) {
	f := newWebhookFixture(t)

	for i := 0; i < 4; i++ {
		require.NoError(t, f.notify(t, status.Cancel))
	}

	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"quota must be restored exactly once")
}

func TestCancelAfterPaidDoesNotRestoreQuota(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Completed))
	require.ErrorIs(t, f.notify(t, status.Cancel), payment.ErrNotificationContradiction)

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
}

// A sweep racing a notification still restores quota exactly once — both go
// through the same guarded transition.
func TestSweepRacingACancelRestoresQuotaOnce(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	// Put the order past its deadline so the sweeper is eligible too.
	_, err := f.pool.Exec(ctx,
		`UPDATE orders SET payment_expires_at = now() - interval '1 minute' WHERE id = $1`, f.orderID)
	require.NoError(t, err)

	require.NoError(t, f.notify(t, status.Cancel))
	_, err = f.svc.ExpireDueOrders(ctx)
	require.NoError(t, err)

	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"whichever path wins the guarded transition, the seats come back once")
}

// Cancelling a bundle order restores every constituent to its pre-checkout value
// — the release is driven by the package-expanded holds, not by a single flat
// count — and a replayed cancel cannot restore any of them twice.
func TestCancelRestoresEveryBundleConstituentExactlyOnce(t *testing.T) {
	f := newBundleWebhookFixture(t)

	require.NoError(t, f.notifyBundle(t, status.Cancel))
	require.NoError(t, f.notifyBundle(t, status.Cancel))

	assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	for _, ticketID := range f.ticketIDs {
		assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, ticketID),
			"each package constituent must be restored to its original quota exactly once")
	}
}

func TestExpiredRestoresEveryBundleConstituent(t *testing.T) {
	f := newBundleWebhookFixture(t)

	require.NoError(t, f.notifyBundle(t, status.Expired))

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	for _, ticketID := range f.ticketIDs {
		assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, ticketID),
			"an expiry signal restores every package constituent, not just one")
	}
}

// --- No-op outcomes -------------------------------------------------------

func TestPendingLeavesTheOrderAndQuotaUntouched(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, status.Pending))

	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))

	issued, _ := f.fulfiller.counts()
	assert.Zero(t, issued)
}

// Obscure is "undefined status from payment network or bank". It holds the order
// rather than guessing, and it is still acknowledged so the gateway stops.
func TestObscureHoldsTheOrderAndIsAcknowledged(t *testing.T) {
	f := newWebhookFixture(t)

	require.ErrorIs(t, f.notify(t, status.Obscure), payment.ErrNotificationUnknownStatus,
		"an indeterminate status is reported to the caller (FR-012g)")

	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"quota is held until a definitive notification arrives")
}

func TestAnUnrecognisedStatusChangesNothingButIsAcknowledged(t *testing.T) {
	f := newWebhookFixture(t)

	err := f.notify(t, status.Status(99))

	require.ErrorIs(t, err, payment.ErrNotificationUnknownStatus,
		"still a 200 to the gateway, but the caller is told what was wrong (FR-012g)")
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
}

// --- Refused notifications (FR-013, FR-020) -------------------------------

func TestANotificationForAnUnknownReferenceIsAcknowledged(t *testing.T) {
	f := newWebhookFixture(t)

	err := f.deliver(t, notification("ORD-DOES-NOT-EXIST", status.Completed))

	require.ErrorIs(t, err, payment.ErrNotificationUnknownOrder,
		"still acknowledged so the gateway stops retrying, but no longer silently (FR-012g)")
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

// A withdrawal is not a payment for one of our orders. It changes nothing, and
// retrying would deliver the same irrelevant fact three more times.
func TestANonDepositNotificationChangesNothingAndIsAcknowledged(t *testing.T) {
	f := newWebhookFixture(t)

	result := notification("ORD-WEBHOOK", status.Completed)
	result.IsDeposit = false
	result.TransactionType = "WITHDRAW"

	require.ErrorIs(t, f.deliver(t, result), payment.ErrNotificationNotDeposit,
		"a withdrawal on a deposit-only endpoint is reported (FR-012g)")

	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	issued, _ := f.fulfiller.counts()
	assert.Zero(t, issued)
}

// --- Duplicate reference (FR-007e) ----------------------------------------

// The gateway has issued a code this system will never hold, so the order can
// never be paid. Holding its seats to the deadline could not help anyone.
func TestReleaseDuplicateSessionFreesQuotaImmediatelyAndMarksTheOrder(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.svc.ReleaseDuplicateSession(
		context.Background(), "ORD-WEBHOOK", payment.ErrDuplicateReference))

	assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"seats come back now, not at a deadline that cannot end in a payment")
	assert.Equal(t, 1, f.markerCount(t, payment.MarkerSessionDuplicate))
}

// FR-007f: a payment that somehow arrives for an order released this way must be
// recorded and reach a person, and must NOT settle under FR-019.
//
// The release cancels the order rather than expiring it, and that is what makes
// the distinction load-bearing rather than academic: the order was released
// because no code ever reached the guest, so a payment against it did not come
// from this booking — the guest was told to start again and may hold a second
// order entirely.
func TestAPaymentArrivingAfterADuplicateReleaseDoesNotSettleTheOrder(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.ReleaseDuplicateSession(
		ctx, "ORD-WEBHOOK", payment.ErrDuplicateReference))
	require.ErrorIs(t, f.notify(t, status.Completed), payment.ErrNotificationOrderCancelled)

	assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Zero(t, f.markerCount(t, payment.MarkerSettledAfterExpiry))
	issued, _ := f.fulfiller.counts()
	assert.Zero(t, issued)

	// It is still findable: the payload sits on the order's own history beside the
	// SESSION_DUPLICATE marker that explains why the order was released at all.
	records, err := f.svc.OrderNotifications(ctx, f.orderID)
	require.NoError(t, err)

	var sawDuplicateMarker, sawCompletion bool
	for _, r := range records {
		switch {
		case r.Status == payment.MarkerSessionDuplicate:
			sawDuplicateMarker = true
		case r.Status == status.Completed.String():
			sawCompletion = true
		}
	}
	assert.True(t, sawDuplicateMarker, "why the order was released")
	assert.True(t, sawCompletion, "and the payment that arrived for it anyway")
}

// FR-016c. A contradiction has to be visible on the order's own record, not only
// in a log line — anyone who looks the order up should see it without being told
// to look. This is the coverage the withdrawn worklist used to carry.
func TestADisputeIsReadableOnTheOrdersOwnHistory(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, status.Completed))
	require.ErrorIs(t, f.notify(t, status.Cancel), payment.ErrNotificationContradiction)

	records, err := f.svc.OrderNotifications(ctx, f.orderID)
	require.NoError(t, err)

	var dispute, settlement bool
	for _, r := range records {
		switch r.Status {
		case payment.MarkerDisputed:
			dispute = true
			assert.True(t, r.IsMarker, "a conclusion of ours must not read as a gateway status")
			assert.Contains(t, string(r.RawPayload), "PAID",
				"the marker carries the status the order was in when the contradiction arrived")
		case status.Completed.String():
			settlement = true
		}
	}
	assert.True(t, dispute, "the contradiction")
	assert.True(t, settlement, "shown alongside the notification that settled the payment")
}

// --- Session open (spec 017) ----------------------------------------------

// The counterpart of ReleaseDuplicateSession above: the gateway agreed, so the
// reference it agreed under goes on the order's own record. It is the only
// moment that value exists — no callback carries it.
func TestRecordSessionOpenedWritesOneMarkerCarryingTheReference(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()
	deadline := time.Now().Add(14 * time.Minute).UTC().Truncate(time.Second)

	require.NoError(t, f.svc.RecordSessionOpened(ctx, "ORD-WEBHOOK", payment.PaymentSession{
		ProviderRef:       "A487336098162400838C",
		QRString:          "00020101021226610014COM.STUB.WWW",
		ExpiresAt:         deadline,
		ExpiryFromGateway: true,
	}))

	assert.Equal(t, 1, f.markerCount(t, payment.MarkerSessionOpened),
		"exactly one row per opened session")

	records, err := f.svc.OrderNotifications(ctx, f.orderID)
	require.NoError(t, err)
	require.Len(t, records, 1)

	row := records[0]
	assert.Equal(t, payment.MarkerSessionOpened, row.Status)
	assert.True(t, row.IsMarker, "our own statement must not read as a gateway status")
	assert.Equal(t, "A487336098162400838C", row.ExtRefID)
	assert.Equal(t, "qris", row.PaymentType,
		"the session-open row is where SettlementForOrder reads the instrument")
	assert.Equal(t, "ORD-WEBHOOK", row.TransactionID,
		"transaction_id is NOT NULL and the network id does not exist yet, so it falls "+
			"back to the order number rather than to a blank that would read as a real one")
	// Parsed rather than substring-matched: the column is JSONB, so Postgres
	// re-serialises the envelope and the byte layout is not ours to assert on.
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(row.RawPayload, &envelope))
	assert.Equal(t, true, envelope["expiry_from_gateway"],
		"whether the gateway agreed to the deadline reaches the order's record, not only the logs")
	assert.Equal(t, "A487336098162400838C", envelope["ext_ref_id"])
}

// The order is payable by the time this runs. An audit write that cannot land
// must not be able to take the session away from the guest.
func TestRecordSessionOpenedReportsAnUnknownOrderWithoutPanicking(t *testing.T) {
	f := newWebhookFixture(t)

	err := f.svc.RecordSessionOpened(context.Background(), "ORD-DOES-NOT-EXIST",
		payment.PaymentSession{ProviderRef: "A4873360", QRString: "qr"})

	require.Error(t, err, "the caller decides what to do; this reports rather than swallows")
	assert.Zero(t, f.markerCount(t, payment.MarkerSessionOpened))
}

// A gateway that opened a session but sent no reference still leaves a payable
// order. The gap is recorded, never fatal (spec 017 FR-004).
func TestRecordSessionOpenedAcceptsAnAbsentReference(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.RecordSessionOpened(ctx, "ORD-WEBHOOK", payment.PaymentSession{
		QRString: "00020101021226610014COM.STUB.WWW", ExpiresAt: time.Now().Add(time.Minute),
	}))

	assert.Equal(t, 1, f.markerCount(t, payment.MarkerSessionOpened))
	ref, err := f.svc.ExternalRefForOrder(ctx, f.orderID)
	require.NoError(t, err)
	assert.Empty(t, ref)
}

// Regression guard. SettlementForOrder walks the rows newest-first and takes the
// first non-empty payment_type as the receipt's instrument and the first
// Completed row as the settlement time. The SESSION_OPENED row is the OLDEST, so
// it must not win that walk — a receipt naming the wrong instrument is not a
// cosmetic defect.
func TestSettlementStillResolvesFromTheNotificationRowsWithASessionOpenRowPresent(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.svc.RecordSessionOpened(ctx, "ORD-WEBHOOK", payment.PaymentSession{
		ProviderRef: "A487336098162400838C", QRString: "qr",
		ExpiresAt: time.Now().Add(time.Minute), ExpiryFromGateway: true,
	}))
	require.NoError(t, f.notify(t, status.Completed))

	settlement, err := f.svc.SettlementForOrder(ctx, f.orderID)
	require.NoError(t, err)
	assert.Equal(t, "qris", settlement.Method)
	assert.False(t, settlement.PaidAt.IsZero(),
		"the settling notification still supplies the time; the session-open row has none")
}

func TestExternalRefForOrderAnswersFromTheRecordedSession(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	ref, err := f.svc.ExternalRefForOrder(ctx, f.orderID)
	require.NoError(t, err)
	assert.Empty(t, ref, "nothing opened yet")

	require.NoError(t, f.svc.RecordSessionOpened(ctx, "ORD-WEBHOOK", payment.PaymentSession{
		ProviderRef: "A487336098162400838C", QRString: "qr", ExpiresAt: time.Now().Add(time.Minute),
	}))

	ref, err = f.svc.ExternalRefForOrder(ctx, f.orderID)
	require.NoError(t, err)
	assert.Equal(t, "A487336098162400838C", ref)
}

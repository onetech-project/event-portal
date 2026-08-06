package payment_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	// statusResult/statusErr let a test make a provider status read differ from
	// what its webhook says — the case reconciliation exists for.
	statusResult *payment.WebhookResult
	statusErr    error
	// session/createErr/createCalls back the QR re-issue tests.
	session     payment.PaymentSession
	createErr   error
	createCalls []payment.TransactionRequest
}

func (g *stubGateway) Name() string { return "midtrans" }

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

func (g *stubGateway) FetchStatus(context.Context, string) (*payment.WebhookResult, error) {
	if g.statusErr != nil {
		return nil, g.statusErr
	}
	if g.statusResult != nil {
		return g.statusResult, nil
	}
	return g.result, g.err
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
		TotalAmount:      rec.TotalAmount,
		BuyerName:        rec.BuyerName,
		BuyerEmail:       rec.BuyerEmail,
		BuyerPhone:       rec.BuyerPhone,
		PaymentStarted:   rec.PaymentQRString != nil && *rec.PaymentQRString != "",
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

func (a orderAdapter) UpdateStatusIfPending(ctx context.Context, tx pgx.Tx, orderID uuid.UUID, status string) (bool, error) {
	return a.repo.UpdateOrderStatusIfPending(ctx, tx, orderID, status)
}

func (a orderAdapter) UpdatePaymentQR(ctx context.Context, orderID uuid.UUID, url, qrString string) (bool, error) {
	return a.repo.UpdatePaymentQRByID(ctx, orderID, url, qrString)
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
	pkg := testsupport.SeedPackage(t, pool, ev.ID, "Day 1+2", "50000.00", "ACTIVE")
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
		fulfiller,
		fulfiller,
		testsupport.DiscardLogger(),
	)

	return webhookFixture{
		svc: svc, pool: pool, gateway: gw, fulfiller: fulfiller,
		orderID: ord.ID, ticketIDs: []uuid.UUID{day1.ID, day2.ID},
	}
}

func (f webhookFixture) notifyBundle(t *testing.T, transactionStatus, fraudStatus string) error {
	t.Helper()
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-WEBHOOK-BUNDLE",
		TransactionID:     "tx-1",
		TransactionStatus: transactionStatus,
		FraudStatus:       fraudStatus,
		PaymentType:       "bank_transfer",
		RawPayload:        []byte(`{"transaction_status":"` + transactionStatus + `"}`),
	}
	err := f.svc.HandleNotification(context.Background(), "midtrans", f.gateway.result.RawPayload, "")
	f.svc.WaitForFulfillment()
	return err
}

func (f webhookFixture) notify(t *testing.T, transactionStatus, fraudStatus string) error {
	t.Helper()
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-WEBHOOK",
		TransactionID:     "tx-1",
		TransactionStatus: transactionStatus,
		FraudStatus:       fraudStatus,
		PaymentType:       "bank_transfer",
		RawPayload:        []byte(`{"transaction_status":"` + transactionStatus + `"}`),
	}
	err := f.svc.HandleNotification(context.Background(), "midtrans", f.gateway.result.RawPayload, "")
	f.svc.WaitForFulfillment()
	return err
}

// --- Authentication -------------------------------------------------------

func TestWebhookRejectsAnUnverifiedNotification(t *testing.T) {
	f := newWebhookFixture(t)
	f.gateway.err = payment.ErrInvalidSignature

	err := f.svc.HandleNotification(context.Background(), "midtrans", []byte(`{}`), "")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID),
		"an unverified notification must change nothing")
}

// --- Paid path ------------------------------------------------------------

func TestSettlementMarksTheOrderPaidWithoutTouchingQuota(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "settlement", ""))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"quota was already deducted at checkout and must not move again")
}

func TestCaptureWithAcceptMarksTheOrderPaid(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "capture", "accept"))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

func TestPaidOrderTriggersTicketGenerationAndEmail(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "settlement", ""))

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued)
	assert.Equal(t, 1, emailed)
}

func TestFulfillmentIsSkippedWhenTicketGenerationFails(t *testing.T) {
	f := newWebhookFixture(t)
	f.fulfiller.issueErr = errors.New("db down")

	require.NoError(t, f.notify(t, "settlement", ""),
		"the provider is still acknowledged; fulfillment is retried out of band")

	_, emailed := f.fulfiller.counts()
	assert.Zero(t, emailed, "no email may be sent for tickets that were never issued")
}

// --- Idempotency (Constitution Principle IV) ------------------------------

func TestReplayedSettlementIsANoOp(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "settlement", ""))
	require.NoError(t, f.notify(t, "settlement", ""))
	require.NoError(t, f.notify(t, "settlement", ""))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))

	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued, "an already-PAID order must not be reprocessed")
	assert.Equal(t, 1, emailed, "the buyer must not receive a second email")
}

func TestANotificationForAnAlreadyPaidOrderIsStillLogged(t *testing.T) {
	f := newWebhookFixture(t)
	ctx := context.Background()

	require.NoError(t, f.notify(t, "settlement", ""))
	require.NoError(t, f.notify(t, "settlement", ""))

	var count int
	require.NoError(t, f.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM payments WHERE order_id = $1`, f.orderID).Scan(&count))
	assert.Equal(t, 2, count, "the audit trail records every notification received")
}

// --- Quota-restoring outcomes ---------------------------------------------

func TestCancelRestoresQuotaAndCancelsTheOrder(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "cancel", ""))

	assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
}

func TestExpireRestoresQuotaAndExpiresTheOrder(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "expire", ""))

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
}

func TestDenyAndFailureBothCancelAndRestore(t *testing.T) {
	for _, status := range []string{"deny", "failure"} {
		t.Run(status, func(t *testing.T) {
			f := newWebhookFixture(t)

			require.NoError(t, f.notify(t, status, ""))

			assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
			assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
		})
	}
}

// The status change and its quota restoration share one transaction, guarded on
// the order still being PENDING, so a replayed cancel cannot restore twice.
func TestReplayedCancelDoesNotRestoreQuotaTwice(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "cancel", ""))
	require.NoError(t, f.notify(t, "cancel", ""))
	require.NoError(t, f.notify(t, "cancel", ""))

	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"quota must be restored exactly once")
}

func TestCancelAfterPaidDoesNotRestoreQuota(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "settlement", ""))
	require.NoError(t, f.notify(t, "cancel", ""))

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
}

// T051: cancelling a bundle order restores every constituent to its pre-checkout
// value — the release is driven by the package-expanded holds, not by a single
// flat count — and a replayed cancel cannot restore any of them twice.
func TestCancelRestoresEveryBundleConstituentExactlyOnce(t *testing.T) {
	f := newBundleWebhookFixture(t)

	require.NoError(t, f.notifyBundle(t, "cancel", ""))
	require.NoError(t, f.notifyBundle(t, "cancel", ""))

	assert.Equal(t, "CANCELLED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	for _, ticketID := range f.ticketIDs {
		assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, ticketID),
			"each package constituent must be restored to its original quota exactly once")
	}
}

func TestExpireRestoresEveryBundleConstituent(t *testing.T) {
	f := newBundleWebhookFixture(t)

	require.NoError(t, f.notifyBundle(t, "expire", ""))

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	for _, ticketID := range f.ticketIDs {
		assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, ticketID),
			"an expiry signal restores every package constituent, not just one")
	}
}

// --- No-op outcomes -------------------------------------------------------

func TestPendingLeavesTheOrderAndQuotaUntouched(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "pending", ""))

	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))

	issued, _ := f.fulfiller.counts()
	assert.Zero(t, issued)
}

func TestCaptureWithChallengeLeavesTheOrderPending(t *testing.T) {
	f := newWebhookFixture(t)

	require.NoError(t, f.notify(t, "capture", "challenge"))

	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"quota is held until a definitive notification arrives")
}

func TestAnUnrecognizedStatusChangesNothingButIsAcknowledged(t *testing.T) {
	f := newWebhookFixture(t)

	err := f.notify(t, "refund", "")

	require.NoError(t, err, "the provider is acknowledged so it stops retrying")
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, int32(7), testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]))
}

// --- Unknown order --------------------------------------------------------

func TestANotificationForAnUnknownOrderIsAcknowledged(t *testing.T) {
	f := newWebhookFixture(t)
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-DOES-NOT-EXIST",
		TransactionStatus: "settlement",
		RawPayload:        []byte(`{}`),
	}

	err := f.svc.HandleNotification(context.Background(), "midtrans", []byte(`{}`), "")

	require.NoError(t, err, "acknowledging stops the provider retrying a notification we can never process")
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

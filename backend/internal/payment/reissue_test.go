package payment_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// --- T023: QR re-issue under a suffixed reference ----------------------------

// startPayment stamps a live QR + future deadline on the fixture's order, as
// checkout would have.
func startPayment(t *testing.T, f webhookFixture) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		UPDATE orders SET payment_qr_string = 'QR-ORIGINAL',
		                  payment_url = 'https://provider.example/qr-1',
		                  payment_provider = 'midtrans',
		                  payment_expires_at = now() + interval '10 minutes'
		WHERE id = $1`, f.orderID)
	require.NoError(t, err)
}

func TestReissueQRUsesTheSuffixedReferenceAndKeepsTheDeadline(t *testing.T) {
	f := newWebhookFixture(t)
	startPayment(t, f)
	f.gateway.session = payment.PaymentSession{
		ProviderRef: "txn-r", QRString: "QR-FRESH",
		QRImageURL: "https://provider.example/qr-2",
		ExpiresAt:  time.Now().Add(15 * time.Minute),
	}

	var before time.Time
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT payment_expires_at FROM orders WHERE id = $1`, f.orderID).Scan(&before))

	reissued, err := f.svc.ReissueQR(context.Background(), "ORD-WEBHOOK")
	require.NoError(t, err)

	require.Len(t, f.gateway.createCalls, 1)
	assert.Equal(t, "ORD-WEBHOOK-R1", f.gateway.createCalls[0].OrderNumber,
		"first re-issue carries the -R1 suffix")
	assert.Equal(t, "QR-FRESH", reissued.QRString)

	// The stored payload swapped, the deadline did NOT move (FR-015).
	var qr string
	var after time.Time
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT payment_qr_string, payment_expires_at FROM orders WHERE id = $1`,
		f.orderID).Scan(&qr, &after))
	assert.Equal(t, "QR-FRESH", qr)
	assert.Equal(t, before, after, "re-issuing never extends the window")

	// A second re-issue increments the suffix — n comes from the audit rows.
	_, err = f.svc.ReissueQR(context.Background(), "ORD-WEBHOOK")
	require.NoError(t, err)
	require.Len(t, f.gateway.createCalls, 2)
	assert.Equal(t, "ORD-WEBHOOK-R2", f.gateway.createCalls[1].OrderNumber)
}

func TestReissueQRRefusesWhenPaymentNeverStarted(t *testing.T) {
	f := newWebhookFixture(t)
	// No startPayment: the order is held but has no QR.

	_, err := f.svc.ReissueQR(context.Background(), "ORD-WEBHOOK")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409005, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
	assert.Empty(t, f.gateway.createCalls)
}

func TestReissueQRLeavesTheOldQRWhenTheGatewayFails(t *testing.T) {
	f := newWebhookFixture(t)
	startPayment(t, f)
	f.gateway.createErr = errors.New("provider down")

	_, err := f.svc.ReissueQR(context.Background(), "ORD-WEBHOOK")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 502001, apperr.Numeric(appErr.HTTPStatus, appErr.Code))

	var qr string
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT payment_qr_string FROM orders WHERE id = $1`, f.orderID).Scan(&qr))
	assert.Equal(t, "QR-ORIGINAL", qr, "the old QR stays live client-side")
}

// --- Webhook suffix strip ----------------------------------------------------

func TestWebhookSettlesTheOrderThroughASuffixedReference(t *testing.T) {
	f := newWebhookFixture(t)
	startPayment(t, f)

	// The provider settles the RE-ISSUED session: order_id echoes the suffix.
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-WEBHOOK-R2",
		TransactionID:     "tx-r2",
		TransactionStatus: "settlement",
		PaymentType:       "qris",
		RawPayload:        []byte(`{"transaction_status":"settlement"}`),
	}
	require.NoError(t, f.svc.HandleNotification(context.Background(), "midtrans",
		f.gateway.result.RawPayload, ""))
	f.svc.WaitForFulfillment()

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID),
		"the -R2 suffix is stripped before order lookup")
}

// Paid-old-QR idempotency: the original session settles first, then a stale
// settlement for a re-issued session arrives — the second must be a no-op.
func TestSettlementForAReissuedSessionAfterPaidIsIdempotent(t *testing.T) {
	f := newWebhookFixture(t)
	startPayment(t, f)

	require.NoError(t, f.notify(t, "settlement", ""))
	require.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	quotaAfterFirst := testsupport.QuotaOf(t, f.pool, f.ticketIDs[0])

	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-WEBHOOK-R1",
		TransactionID:     "tx-r1",
		TransactionStatus: "settlement",
		PaymentType:       "qris",
		RawPayload:        []byte(`{"transaction_status":"settlement"}`),
	}
	require.NoError(t, f.svc.HandleNotification(context.Background(), "midtrans",
		f.gateway.result.RawPayload, ""))
	f.svc.WaitForFulfillment()

	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	assert.Equal(t, quotaAfterFirst, testsupport.QuotaOf(t, f.pool, f.ticketIDs[0]),
		"the second settlement must not touch quota")
}

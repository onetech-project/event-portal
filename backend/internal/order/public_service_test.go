package order_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// pendingOrder drives the live path (Book → RecordAgreement → CheckoutOrder
// against the fake gateway) and returns the order number, so each test starts
// from a real PENDING order with a real payment instruction.
func pendingOrder(t *testing.T, f checkoutFixture) string {
	t.Helper()
	orderNumber, slotIDs := bookAgreedOrder(t, f)
	_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, formsFor(slotIDs))
	require.NoError(t, err)
	return orderNumber
}

func setStatus(t *testing.T, f checkoutFixture, orderNumber, status string) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET status_id = (SELECT id FROM order_statuses WHERE name = $2) WHERE order_number = $1`, orderNumber, status)
	require.NoError(t, err)
}

// --- The QR payload -------------------------------------------------------

func TestPaymentQRPayloadReturnsTheStoredPayloadWhilePayable(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber := pendingOrder(t, f)

	payload, err := f.public.PaymentQRPayload(context.Background(), orderNumber)

	require.NoError(t, err)
	assert.Equal(t, "00020101021226620014COM.EXAMPLE.QRIS", payload)
}

// The image must vanish at the same instant the instruction does, or a stale
// page keeps showing a code that no longer works.
func TestPaymentQRPayloadIsNotFoundOnceTheOrderIsNotPayable(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber := pendingOrder(t, f)
	setStatus(t, f, orderNumber, "PAID")

	_, err := f.public.PaymentQRPayload(context.Background(), orderNumber)

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}

func TestPaymentQRPayloadIsNotFoundForAnUnknownOrder(t *testing.T) {
	f := newCheckoutFixture(t)

	_, err := f.public.PaymentQRPayload(context.Background(), "ORD-NOPE")

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}

// --- Reload stability (FR-013) --------------------------------------------

func TestReadingAnOrderRepeatedlyNeverChangesItsInstruction(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber := pendingOrder(t, f)
	ctx := context.Background()

	first, err := f.public.TicketOrderByNumber(ctx, orderNumber)
	require.NoError(t, err)
	second, err := f.public.TicketOrderByNumber(ctx, orderNumber)
	require.NoError(t, err)

	require.NotNil(t, first.Payment)
	require.NotNil(t, second.Payment)
	assert.Equal(t, first.Payment.ExpiresAt, second.Payment.ExpiresAt,
		"reopening the page must not start a second payment attempt")
	assert.Equal(t, first.Payment.QRImagePath, second.Payment.QRImagePath)
	assert.Equal(t, 1, f.gateway.callCount(), "reads must never call the provider")
}

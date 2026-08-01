package order_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// paidOrderNumber checks out one ticket and returns the resulting order number,
// so each test starts from a real PENDING order with a real payment instruction.
func pendingOrder(t *testing.T, f checkoutFixture) (string, testsupport.TicketType) {
	t.Helper()
	tt := f.seedSellableEvent(t, 10)

	resp, err := f.svc.Checkout(context.Background(), checkoutFor(tt, 2))
	require.NoError(t, err)
	return resp.OrderNumber, tt
}

func setStatus(t *testing.T, f checkoutFixture, orderNumber, status string) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET status = $2 WHERE order_number = $1`, orderNumber, status)
	require.NoError(t, err)
}

// --- What the guest sees --------------------------------------------------

func TestOrderByNumberDescribesTheOrderAndItsPaymentInstruction(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, _ := pendingOrder(t, f)

	detail, err := f.public.OrderByNumber(context.Background(), orderNumber)

	require.NoError(t, err)
	assert.Equal(t, orderNumber, detail.OrderNumber)
	assert.Equal(t, "PENDING", detail.Status)
	assert.Equal(t, "300000.00", detail.TotalAmount.String())
	assert.Equal(t, "Budi Santoso", detail.BuyerName)
	assert.Equal(t, "budi@example.com", detail.BuyerEmail)
	assert.NotNil(t, detail.CreatedAt)

	// Resolved through the event domain's contract, not a JOIN across the
	// boundary (Constitution Principle II).
	assert.Equal(t, "sellable", detail.Event.Slug)
	assert.NotEmpty(t, detail.Event.Name)

	require.Len(t, detail.Items, 1)
	assert.Equal(t, "Regular", detail.Items[0].TicketTypeName)
	assert.Equal(t, int32(2), detail.Items[0].Quantity)
	assert.Equal(t, "150000.00", detail.Items[0].UnitPrice.String())
	assert.Equal(t, "300000.00", detail.Items[0].Subtotal.String())

	require.NotNil(t, detail.Payment)
	assert.Equal(t, "QRIS", detail.Payment.Method)
	assert.Equal(t, "fakegw", detail.Payment.Provider)
	assert.Equal(t, "300000.00", detail.Payment.Amount.String())
	assert.True(t, detail.Payment.ExpiresAt.After(time.Now()))
	assert.Equal(t, "/api/v1/orders/"+orderNumber+"/qris.png", detail.Payment.QRImagePath)

	// The countdown is rendered against this, not the device clock (SC-005).
	assert.WithinDuration(t, time.Now(), detail.ServerTime, 5*time.Second)
}

// --- When the payment instruction must disappear (FR-014) -----------------

func TestOrderByNumberOmitsThePaymentInstructionOnceFinal(t *testing.T) {
	for _, status := range []string{"PAID", "CANCELLED", "EXPIRED"} {
		t.Run(status, func(t *testing.T) {
			f := newCheckoutFixture(t)
			orderNumber, _ := pendingOrder(t, f)
			setStatus(t, f, orderNumber, status)

			detail, err := f.public.OrderByNumber(context.Background(), orderNumber)

			require.NoError(t, err)
			assert.Equal(t, status, detail.Status)
			assert.Nil(t, detail.Payment, "a settled order must never show a payable code")
		})
	}
}

// The sweeper flips a lapsed order within its interval, but the read model must
// not show a live code in the meantime.
func TestOrderByNumberOmitsThePaymentInstructionPastTheDeadline(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, _ := pendingOrder(t, f)

	_, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET payment_expires_at = now() - interval '1 minute' WHERE order_number = $1`,
		orderNumber)
	require.NoError(t, err)

	detail, err := f.public.OrderByNumber(context.Background(), orderNumber)

	require.NoError(t, err)
	assert.Equal(t, "PENDING", detail.Status, "the status flip is the sweeper's job")
	assert.Nil(t, detail.Payment, "but the code is already dead, so it must not be shown")
}

// Orders that predate this feature — and any whose charge never recorded an
// instruction — have no payload to render.
func TestOrderByNumberOmitsThePaymentInstructionWhenNoneWasRecorded(t *testing.T) {
	f := newCheckoutFixture(t)
	seeded := testsupport.SeedOrder(t, f.pool, "ORD-LEGACY", "PENDING")

	detail, err := f.public.OrderByNumber(context.Background(), seeded.OrderNumber)

	require.NoError(t, err)
	assert.Nil(t, detail.Payment)
}

// --- What it must never expose (FR-022) -----------------------------------

func TestOrderByNumberExposesNothingBeyondTheGuestsOwnOrder(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, _ := pendingOrder(t, f)

	detail, err := f.public.OrderByNumber(context.Background(), orderNumber)
	require.NoError(t, err)

	raw, err := json.Marshal(detail)
	require.NoError(t, err)

	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))

	for _, forbidden := range []string{
		"ticket_code", "tickets", "attendees", "transaction_id",
		"payment_qr_string", "id", "email_sent",
	} {
		assert.NotContains(t, body, forbidden,
			"the public order view must not carry %q", forbidden)
	}
}

func TestOrderByNumberReportsNotFoundForAnUnknownOrder(t *testing.T) {
	f := newCheckoutFixture(t)

	_, err := f.public.OrderByNumber(context.Background(), "ORD-DOES-NOT-EXIST")

	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	assert.Equal(t, "Order not found.", appErr.Message)
}

// --- The QR payload -------------------------------------------------------

func TestPaymentQRPayloadReturnsTheStoredPayloadWhilePayable(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, _ := pendingOrder(t, f)

	payload, err := f.public.PaymentQRPayload(context.Background(), orderNumber)

	require.NoError(t, err)
	assert.Equal(t, "00020101021226620014COM.EXAMPLE.QRIS", payload)
}

// The image must vanish at the same instant the instruction does, or a stale
// page keeps showing a code that no longer works.
func TestPaymentQRPayloadIsNotFoundOnceTheOrderIsNotPayable(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, _ := pendingOrder(t, f)
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
	orderNumber, _ := pendingOrder(t, f)
	ctx := context.Background()

	first, err := f.public.OrderByNumber(ctx, orderNumber)
	require.NoError(t, err)
	second, err := f.public.OrderByNumber(ctx, orderNumber)
	require.NoError(t, err)

	require.NotNil(t, first.Payment)
	require.NotNil(t, second.Payment)
	assert.Equal(t, first.Payment.ExpiresAt, second.Payment.ExpiresAt,
		"reopening the page must not start a second payment attempt")
	assert.Equal(t, first.Payment.QRImagePath, second.Payment.QRImagePath)
	assert.Equal(t, 1, f.gateway.callCount(), "reads must never call the provider")
}

package order_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
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

// --- Per-line admission windows (spec 015) --------------------------------

// setEventWindow moves a ticket type's admission window. Ticket-type
// configuration is not order, ticket or payment state, so setting it directly is
// arrangement rather than a shortcut past the API.
func setEventWindow(t *testing.T, f checkoutFixture, ticketTypeID uuid.UUID, start, end time.Time) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(),
		`UPDATE ticket_types SET event_start = $2, event_end = $3 WHERE id = $1`,
		ticketTypeID, start, end)
	require.NoError(t, err)
}

// The defect this feature exists to fix: two lines of one order that admit on
// different days used to render the same event-level date.
func TestOrderLinesCarryTheirOwnAdmissionWindows(t *testing.T) {
	f := newCheckoutFixture(t)
	ctx := context.Background()

	ev, day1, day2, _ := f.seedBundleEvent(t, 5, 5)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	dayOneStart := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	dayTwoStart := dayOneStart.Add(24 * time.Hour)
	setEventWindow(t, f, day1.ID, dayOneStart, dayOneStart.Add(14*time.Hour))
	setEventWindow(t, f, day2.ID, dayTwoStart, dayTwoStart.Add(14*time.Hour))

	booked, err := f.svc.Book(ctx, order.BookRequest{
		EventID: ev.ID,
		Items: []order.CheckoutItem{
			{TicketTypeID: &day1.ID, Quantity: 1},
			{TicketTypeID: &day2.ID, Quantity: 1},
		},
	})
	require.NoError(t, err)

	detail, err := f.public.TicketOrderByNumber(ctx, booked.OrderID)
	require.NoError(t, err)
	require.Len(t, detail.Items, 2)

	starts := map[string][]time.Time{}
	for _, item := range detail.Items {
		require.NotNil(t, item.TicketTypeName)
		starts[*item.TicketTypeName] = item.AdmissionStarts
	}

	require.Len(t, starts["Day 1"], 1, "a ticket line admits on exactly one day")
	require.Len(t, starts["Day 2"], 1)
	assert.WithinDuration(t, dayOneStart, starts["Day 1"][0], time.Second)
	assert.WithinDuration(t, dayTwoStart, starts["Day 2"][0], time.Second)
	assert.False(t, starts["Day 1"][0].Equal(starts["Day 2"][0]),
		"two lines on different days must not render the same date")
}

// FR-012: the order's event block keeps naming the EVENT's dates. Only a line's
// date moved to the line.
func TestOrderEventBlockStillCarriesTheEventsOwnDates(t *testing.T) {
	f := newCheckoutFixture(t)
	ctx := context.Background()

	ev, day1, _, _ := f.seedBundleEvent(t, 5, 5)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	shifted := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second).Add(3 * time.Hour)
	setEventWindow(t, f, day1.ID, shifted, shifted.Add(2*time.Hour))

	booked, err := f.svc.Book(ctx, order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &day1.ID, Quantity: 1}},
	})
	require.NoError(t, err)

	detail, err := f.public.TicketOrderByNumber(ctx, booked.OrderID)
	require.NoError(t, err)

	require.Len(t, detail.Items[0].AdmissionStarts, 1)
	assert.False(t, detail.Event.StartDate.Equal(detail.Items[0].AdmissionStarts[0]),
		"the event block must not silently become the ticket's window")
	assert.WithinDuration(t, shifted, detail.Items[0].AdmissionStarts[0], time.Second)
}

// FR-021a: a bundle admits on every day its parts admit, and the line must name
// each of them. Collapsing them to one span was the defect — a Day 1 + Day 2
// bundle rendered as "Day 1" and the second day disappeared.
func TestPackageLineListsEveryConstituentAdmissionDay(t *testing.T) {
	f := newCheckoutFixture(t)
	ctx := context.Background()

	ev, day1, day2, pkg := f.seedBundleEvent(t, 5, 5)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	dayOneStart := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	dayOneEnd := dayOneStart.Add(14 * time.Hour)
	dayTwoStart := dayOneStart.Add(24 * time.Hour)
	setEventWindow(t, f, day1.ID, dayOneStart, dayOneEnd)
	setEventWindow(t, f, day2.ID, dayTwoStart, dayTwoStart.Add(14*time.Hour))

	booked, err := f.svc.Book(ctx, bundleBookFor(ev.ID, pkg.ID, 1))
	require.NoError(t, err)

	detail, err := f.public.TicketOrderByNumber(ctx, booked.OrderID)
	require.NoError(t, err)
	require.Len(t, detail.Items, 1)

	starts := detail.Items[0].AdmissionStarts
	require.Len(t, starts, 2, "a two-day bundle names two days, not one span")
	assert.WithinDuration(t, dayOneStart, starts[0], time.Second)
	assert.WithinDuration(t, dayTwoStart, starts[1], time.Second)
	assert.True(t, starts[0].Before(starts[1]), "ascending, so the display can range them")
}

// Duplicates are dropped at the source: two constituents admitting on the same
// instant are one day to the buyer.
func TestPackageLineDeduplicatesConstituentsSharingADay(t *testing.T) {
	f := newCheckoutFixture(t)
	ctx := context.Background()

	ev, day1, day2, pkg := f.seedBundleEvent(t, 5, 5)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	sameDay := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)
	setEventWindow(t, f, day1.ID, sameDay, sameDay.Add(6*time.Hour))
	setEventWindow(t, f, day2.ID, sameDay, sameDay.Add(6*time.Hour))

	booked, err := f.svc.Book(ctx, bundleBookFor(ev.ID, pkg.ID, 1))
	require.NoError(t, err)

	detail, err := f.public.TicketOrderByNumber(ctx, booked.OrderID)
	require.NoError(t, err)
	require.Len(t, detail.Items, 1)

	assert.Len(t, detail.Items[0].AdmissionStarts, 1,
		"two parts admitting at the same instant are one day")
}

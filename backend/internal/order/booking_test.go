package order_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// --- T015 Book / T016 RecordAgreement ---------------------------------------

func bookFor(eventID, ticketTypeID uuid.UUID, qty int32) order.BookRequest {
	return order.BookRequest{
		EventID: eventID,
		Items:   []order.CheckoutItem{{TicketTypeID: &ticketTypeID, Quantity: qty}},
	}
}

func quotaOf(t *testing.T, f checkoutFixture, ticketTypeID uuid.UUID) int32 {
	t.Helper()
	var quota int32
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT quota FROM ticket_types WHERE id = $1`, ticketTypeID).Scan(&quota))
	return quota
}

func TestBookCreatesAPendingHeldOrderWithEmptySlots(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "bookable", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	before := time.Now()
	resp, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 2))
	require.NoError(t, err)

	assert.Equal(t, "PENDING", resp.Status)
	assert.Equal(t, "300000.00", resp.TotalAmount.String(), "total is server-priced")
	// The hold deadline is ~1h out (default timer): inside (55m, 65m] of now.
	assert.WithinRange(t, resp.ExpiresAt,
		before.Add(55*time.Minute), time.Now().Add(65*time.Minute))

	// Buyer identity must not exist yet (Option B), and the payment fields are
	// untouched — no gateway was involved.
	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	assert.Nil(t, stored.BuyerName)
	assert.Nil(t, stored.BuyerEmail)
	assert.Nil(t, stored.PaymentQRString)
	assert.Nil(t, stored.TermsAgreedAt, "agreement is its own call")
	assert.Equal(t, 0, f.gateway.callCount(), "booking never talks to the gateway")

	// Two EMPTY slots bound to the ticket type.
	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	require.Len(t, slots, 2)
	for _, slot := range slots {
		assert.Equal(t, tt.ID, slot.TicketTypeID)
		assert.Nil(t, slot.Name)
		assert.Nil(t, slot.Email)
		assert.False(t, slot.PackageID.Valid)
		assert.Nil(t, slot.PackageUnit, "standalone slots carry no bundle unit")
	}

	assert.Equal(t, int32(8), quotaOf(t, f, tt.ID), "quota locked at booking")
}

func TestBookRefusesAnEventWithoutAuthoredTerms(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "no-terms", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 5)

	_, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)
	assert.Equal(t, 409001, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
	assert.Equal(t, int32(5), quotaOf(t, f, tt.ID), "no hold may be taken")
}

func TestBookRejectsMoreThanRemainingQuota(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "scarce", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 1)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	_, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 2))

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.CodeInsufficientQuota, appErr.Code)
	assert.Equal(t, int32(1), quotaOf(t, f, tt.ID))
}

func TestBookRejectsItemsBelongingToAnotherEvent(t *testing.T) {
	f := newCheckoutFixture(t)
	target := testsupport.SeedEvent(t, f.pool, "target-event", "PUBLISHED")
	testsupport.SeedEventTerms(t, f.pool, target.ID, "<p>terms</p>")
	other := testsupport.SeedEvent(t, f.pool, "other-event", "PUBLISHED")
	foreign := testsupport.SeedTicketType(t, f.pool, other.ID, "Foreign", "10000.00", 5)

	_, err := f.svc.Book(context.Background(), bookFor(target.ID, foreign.ID, 1))

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.CodeValidation, appErr.Code)
	assert.Equal(t, int32(5), quotaOf(t, f, foreign.ID))
}

func TestBookBundleCreatesSlotsPerConstituentWithPackageOrigin(t *testing.T) {
	f := newCheckoutFixture(t)
	ev, day1, day2, pkg := f.seedBundleEvent(t, 5, 5)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	pkgID := pkg.ID
	resp, err := f.svc.Book(context.Background(), order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{PackageID: &pkgID, Quantity: 2}},
	})
	require.NoError(t, err)
	assert.Equal(t, "100000.00", resp.TotalAmount.String(), "2 × bundle price")

	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	require.Len(t, slots, 4, "2 units × 2 constituents")

	perType := map[uuid.UUID]int{}
	perUnitType := map[int16]map[uuid.UUID]int{}
	for _, slot := range slots {
		require.True(t, slot.PackageID.Valid, "bundle slots carry their package origin")
		assert.Equal(t, pkg.ID, slot.PackageID.UUID)
		perType[slot.TicketTypeID]++
		// Spec 010: every bundle slot belongs to a purchased unit.
		require.NotNil(t, slot.PackageUnit, "bundle slots carry their unit ordinal")
		if perUnitType[*slot.PackageUnit] == nil {
			perUnitType[*slot.PackageUnit] = map[uuid.UUID]int{}
		}
		perUnitType[*slot.PackageUnit][slot.TicketTypeID]++
	}
	assert.Equal(t, 2, perType[day1.ID])
	assert.Equal(t, 2, perType[day2.ID])

	// Units are 1..quantity, each holding the per-unit composition — one Day 1
	// and one Day 2, never two of the same day in a unit (spec 010 US3).
	require.Len(t, perUnitType, 2, "2 purchased units")
	for _, unit := range []int16{1, 2} {
		assert.Equal(t, map[uuid.UUID]int{day1.ID: 1, day2.ID: 1}, perUnitType[unit],
			"unit %d holds exactly the per-unit composition", unit)
	}

	assert.Equal(t, int32(3), quotaOf(t, f, day1.ID))
	assert.Equal(t, int32(3), quotaOf(t, f, day2.ID))
}

// Spec 010 T010: a single purchased bundle unit stamps package_unit = 1 on all
// of its constituent slots.
func TestBookSingleBundleUnitStampsUnitOne(t *testing.T) {
	f := newCheckoutFixture(t)
	ev, day1, day2, pkg := f.seedBundleEvent(t, 5, 5)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	pkgID := pkg.ID
	resp, err := f.svc.Book(context.Background(), order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{PackageID: &pkgID, Quantity: 1}},
	})
	require.NoError(t, err)

	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	require.Len(t, slots, 2, "1 unit × 2 constituents")

	types := map[uuid.UUID]int{}
	for _, slot := range slots {
		require.NotNil(t, slot.PackageUnit)
		assert.Equal(t, int16(1), *slot.PackageUnit)
		types[slot.TicketTypeID]++
	}
	assert.Equal(t, map[uuid.UUID]int{day1.ID: 1, day2.ID: 1}, types)
}

// The concurrent last-ticket race: two guests agree at once; exactly one hold.
func TestConcurrentBooksCannotOversellTheLastTicket(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "last-ticket", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 1)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range results {
		if err == nil {
			succeeded++
		} else {
			var appErr *apperr.Error
			require.True(t, errors.As(err, &appErr))
			assert.Equal(t, apperr.CodeInsufficientQuota, appErr.Code)
		}
	}
	assert.Equal(t, 1, succeeded, "exactly one booking may take the last seat")
	assert.Equal(t, int32(0), quotaOf(t, f, tt.ID))
}

// --- T016 RecordAgreement ----------------------------------------------------

// bookHeldOrder books one ticket and returns the order number plus the current
// terms id the dialog would have displayed.
func bookHeldOrder(t *testing.T, f checkoutFixture) (string, uuid.UUID) {
	t.Helper()
	ev := testsupport.SeedEvent(t, f.pool, "agreeable", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 5)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	resp, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))
	require.NoError(t, err)
	return resp.OrderID, termsID
}

func TestRecordAgreementStampsTheOrderAndIsIdempotent(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, termsID := bookHeldOrder(t, f)

	req := order.AgreementRequest{Agreed: true, EventTermsID: termsID}
	require.NoError(t, f.svc.RecordAgreement(context.Background(), orderNumber, req))
	// Same click retried (network flake) must succeed identically.
	require.NoError(t, f.svc.RecordAgreement(context.Background(), orderNumber, req))

	stored, err := f.repo.GetOrderByNumber(context.Background(), orderNumber)
	require.NoError(t, err)
	require.NotNil(t, stored.TermsAgreedAt, "durable agreement record (FR-008)")
	require.True(t, stored.EventTermsID.Valid)
	assert.Equal(t, termsID, stored.EventTermsID.UUID)
}

func TestRecordAgreementRejectsWhenNotAgreed(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, termsID := bookHeldOrder(t, f)

	err := f.svc.RecordAgreement(context.Background(), orderNumber,
		order.AgreementRequest{Agreed: false, EventTermsID: termsID})

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400003, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
}

func TestRecordAgreementRejectsAStaleTermsID(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, _ := bookHeldOrder(t, f)

	err := f.svc.RecordAgreement(context.Background(), orderNumber,
		order.AgreementRequest{Agreed: true, EventTermsID: uuid.New()})

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 409002, apperr.Numeric(appErr.HTTPStatus, appErr.Code),
		"a changed document must send the dialog back to refetch")
}

func TestRecordAgreementRejectsAnUnknownOrder(t *testing.T) {
	f := newCheckoutFixture(t)

	err := f.svc.RecordAgreement(context.Background(), "ORD-20260101-DEADBEEF",
		order.AgreementRequest{Agreed: true, EventTermsID: uuid.New()})

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}

func TestRecordAgreementRejectsAnExpiredHold(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, termsID := bookHeldOrder(t, f)

	// Age the hold past its deadline.
	_, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET payment_expires_at = now() - interval '1 minute' WHERE order_number = $1`,
		orderNumber)
	require.NoError(t, err)

	agreeErr := f.svc.RecordAgreement(context.Background(), orderNumber,
		order.AgreementRequest{Agreed: true, EventTermsID: termsID})

	var appErr *apperr.Error
	require.True(t, errors.As(agreeErr, &appErr))
	assert.Equal(t, 410001, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
}

// Fees (clarified 2026-08-05): booking applies the active master rows to the
// subtotal and freezes the computed lines onto the order.
func TestBookAppliesActiveFeesAndFreezesTheBreakdown(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "feeable", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "35000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	testsupport.SeedFee(t, f.pool, "PPN", "PERCENT", "11.00", 1)
	testsupport.SeedFee(t, f.pool, "Admin Fee", "FIXED", "1200.00", 2)

	resp, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 3))
	require.NoError(t, err)

	// 105.000 subtotal + 11% PPN (11.550) + 1.200 admin = 117.750 (Figma 32-1366).
	assert.Equal(t, "117750.00", resp.TotalAmount.String())

	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	require.True(t, stored.Subtotal.Valid)
	assert.Equal(t, "105000.00", stored.Subtotal.Decimal.StringFixed(2))

	lines, err := f.repo.ListOrderFeesByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	require.Len(t, lines, 2)
	assert.Equal(t, "PPN (11%)", lines[0].Name, "percentage baked into the frozen name")
	assert.Equal(t, "11550.00", lines[0].Amount.StringFixed(2))
	assert.Equal(t, "Admin Fee", lines[1].Name)
	assert.Equal(t, "1200.00", lines[1].Amount.StringFixed(2))
}

// A fee edited after booking must not change what an existing order shows.
func TestBookedFeeLinesSurviveAMasterEdit(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "fee-frozen", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "100000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	testsupport.SeedFee(t, f.pool, "Admin Fee", "FIXED", "1000.00", 1)

	resp, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))
	require.NoError(t, err)

	_, err = f.pool.Exec(context.Background(), `UPDATE fees SET value = 9999`)
	require.NoError(t, err)

	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	lines, err := f.repo.ListOrderFeesByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	require.Len(t, lines, 1)
	assert.Equal(t, "1000.00", lines[1-1].Amount.StringFixed(2))
	assert.Equal(t, "101000.00", stored.TotalAmount.StringFixed(2))
}

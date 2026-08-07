package order_test

import (
	"context"
	"errors"
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

// --- T021: CheckoutOrder (Option B forms + payment in one call) --------------

// bookAgreedOrder books 2 tickets and records the agreement, returning the
// fixture-ready order number and its slot ids.
func bookAgreedOrder(t *testing.T, f checkoutFixture) (string, []uuid.UUID) {
	t.Helper()
	ev := testsupport.SeedEvent(t, f.pool, "payable", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	resp, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 2))
	require.NoError(t, err)
	require.NoError(t, f.svc.RecordAgreement(context.Background(), resp.OrderID,
		order.AgreementRequest{Agreed: true, EventTermsID: termsID}))

	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)

	ids := make([]uuid.UUID, 0, len(slots))
	for _, s := range slots {
		ids = append(ids, s.ID)
	}
	return resp.OrderID, ids
}

// formsFor fills every slot with the same visitor identity except gender, which
// alternates through the seeded master names. No buyer block (spec 011): the
// primary contact is derived server-side from the first canonical slot.
func formsFor(slotIDs []uuid.UUID) order.CheckoutFormsRequest {
	visitors := make([]order.CheckoutVisitor, 0, len(slotIDs))
	for i, id := range slotIDs {
		visitors = append(visitors, order.CheckoutVisitor{
			ID:     id,
			Name:   "Visitor",
			Email:  "visitor@example.com",
			Phone:  "081234567890",
			Dob:    "2000-01-31",
			Gender: []string{"FEMALE", "MALE"}[i%2],
		})
	}
	return order.CheckoutFormsRequest{Attendees: visitors}
}

func TestCheckoutOrderSavesFormsAndStartsPayment(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	resp, err := f.svc.CheckoutOrder(context.Background(), orderNumber, formsFor(slotIDs))
	require.NoError(t, err)

	assert.Equal(t, orderNumber, resp.OrderID)
	assert.Equal(t, f.gateway.qrString, resp.QRString)
	assert.Equal(t, "/api/v1/ticket/order/"+orderNumber+"/qris.png", resp.QRImageURL)
	assert.Equal(t, 7*60, resp.QRRefreshAfterSeconds, "default QR_REFRESH_AFTER is 7m")

	stored, err := f.repo.GetOrderByNumber(context.Background(), orderNumber)
	require.NoError(t, err)
	// The primary contact is derived from the topmost holder form (spec 011).
	require.NotNil(t, stored.BuyerName)
	assert.Equal(t, "Visitor", *stored.BuyerName)
	require.NotNil(t, stored.BuyerEmail)
	assert.Equal(t, "visitor@example.com", *stored.BuyerEmail)
	require.NotNil(t, stored.BuyerPhone)
	assert.Equal(t, "081234567890", *stored.BuyerPhone)
	require.NotNil(t, stored.PaymentQRString)

	// The gateway's customer is the same primary contact (FR-017).
	call := f.gateway.lastCall()
	assert.Equal(t, "Visitor", call.CustomerName)
	assert.Equal(t, "visitor@example.com", call.CustomerEmail)
	assert.Equal(t, "081234567890", call.CustomerPhone)

	// The 14-minute window replaced the 1-hour hold on the same column.
	require.NotNil(t, stored.PaymentExpiresAt)
	// timestamptz stores microseconds; allow the sub-µs round-trip loss.
	assert.WithinDuration(t, resp.ExpiresAt, *stored.PaymentExpiresAt, time.Millisecond)

	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	for _, slot := range slots {
		require.NotNil(t, slot.Name, "every slot is filled by checkout")
		require.NotNil(t, slot.Dob)
		assert.Equal(t, "2000-01-31", slot.Dob.Format("2006-01-02"))
	}
}

func TestCheckoutOrderRejectsBadFormsWithAFieldMap(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	bad := formsFor(slotIDs)
	bad.Attendees[0].Email = "not-an-email"
	bad.Attendees[0].Dob = "31-01-2000"
	bad.Attendees[1].Gender = "OTHER"

	_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, bad)

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400001, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
	fields, ok := appErr.Data.(map[string]string)
	require.True(t, ok, "400001 carries the field map as data")
	assert.Contains(t, fields, "attendees[0].email")
	assert.Contains(t, fields, "attendees[0].dob")
	assert.Contains(t, fields, "attendees[1].gender")

	// Nothing was saved and no session opened.
	stored, err := f.repo.GetOrderByNumber(context.Background(), orderNumber)
	require.NoError(t, err)
	assert.Nil(t, stored.BuyerName)
	assert.Equal(t, 0, f.gateway.callCount())
}

// Spec 011 (clarified 2026-08-07): the phone rule is 10-15 digits and nothing
// else, and every failure carries the exact shared message keyed to the
// offending form.
func TestCheckoutOrderRejectsMalformedPhonesWithTheExactMessage(t *testing.T) {
	for name, phone := range map[string]string{
		"nine digits":         "081234567",
		"sixteen digits":      "0812345678901234",
		"contains separators": "0812-3456-789",
		"contains letters":    "08123456789a",
		"leading plus":        "+628123456789",
		"empty":               "",
	} {
		t.Run(name, func(t *testing.T) {
			f := newCheckoutFixture(t)
			orderNumber, slotIDs := bookAgreedOrder(t, f)

			bad := formsFor(slotIDs)
			bad.Attendees[0].Phone = phone

			_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, bad)

			var appErr *apperr.Error
			require.True(t, errors.As(err, &appErr))
			assert.Equal(t, 400001, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
			fields, ok := appErr.Data.(map[string]string)
			require.True(t, ok, "400001 carries the field map as data")
			assert.Equal(t, "Enter a phone number of 10-15 digits.", fields["attendees[0].phone"])
			assert.Equal(t, 0, f.gateway.callCount())
		})
	}
}

// Both length boundaries are inclusive, and — the point of FR-006's "verbatim"
// rule — the local and international spellings of the SAME number are stored
// exactly as submitted rather than normalised into one another.
func TestCheckoutAcceptsThePhoneLengthBoundariesAndStoresItVerbatim(t *testing.T) {
	for name, phone := range map[string]string{
		"ten digits":         "0812345678",
		"fifteen digits":     "081234567890123",
		"local form":         "08123456789",
		"international form": "628123456789",
	} {
		t.Run(name, func(t *testing.T) {
			f := newCheckoutFixture(t)
			orderNumber, slotIDs := bookAgreedOrder(t, f)

			forms := formsFor(slotIDs)
			for i := range forms.Attendees {
				forms.Attendees[i].Phone = phone
			}

			_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, forms)
			require.NoError(t, err)

			stored, err := f.repo.GetOrderByNumber(context.Background(), orderNumber)
			require.NoError(t, err)
			require.NotNil(t, stored.BuyerPhone)
			assert.Equal(t, phone, *stored.BuyerPhone)
			assert.Equal(t, phone, f.gateway.lastCall().CustomerPhone)
		})
	}
}

// Spec 011: every attendee row stores a gender_id that resolves back to exactly
// the gender NAME its form submitted — checked against the database directly.
func TestCheckoutStoresAGenderIDResolvingToTheSubmittedName(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	forms := formsFor(slotIDs) // genders alternate FEMALE / MALE
	_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, forms)
	require.NoError(t, err)

	submitted := map[uuid.UUID]string{}
	for _, v := range forms.Attendees {
		submitted[v.ID] = v.Gender
	}

	stored, err := f.repo.GetOrderByNumber(context.Background(), orderNumber)
	require.NoError(t, err)
	rows, err := f.pool.Query(context.Background(), `
		SELECT a.id, g.name
		FROM attendees a
		JOIN genders g ON g.id = a.gender_id
		WHERE a.order_id = $1`, stored.ID)
	require.NoError(t, err)
	defer rows.Close()

	resolved := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var name string
		require.NoError(t, rows.Scan(&id, &name))
		resolved[id] = name
	}
	require.NoError(t, rows.Err())

	// The inner JOIN drops any NULL gender_id, so map equality proves both
	// non-NULL storage and correct name resolution for every slot.
	assert.Equal(t, submitted, resolved)
}

func TestCheckoutOrderRefusesWhenAgreementWasNeverRecorded(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "unagreed", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 5)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	resp, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))
	require.NoError(t, err)
	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)

	_, checkoutErr := f.svc.CheckoutOrder(context.Background(), resp.OrderID,
		formsFor([]uuid.UUID{slots[0].ID}))

	var appErr *apperr.Error
	require.True(t, errors.As(checkoutErr, &appErr))
	assert.Equal(t, 409003, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
	assert.Equal(t, 0, f.gateway.callCount())
}

func TestCheckoutOrderIsIdempotentOncePaymentStarted(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	first, err := f.svc.CheckoutOrder(context.Background(), orderNumber, formsFor(slotIDs))
	require.NoError(t, err)

	_, retryErr := f.svc.CheckoutOrder(context.Background(), orderNumber, formsFor(slotIDs))

	// 409004 whose data is the current QR payload — the client renders it
	// exactly as it would a fresh 200 (contracts/api.md call 8).
	var appErr *apperr.Error
	require.True(t, errors.As(retryErr, &appErr))
	assert.Equal(t, 409004, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
	payload, ok := appErr.Data.(order.CheckoutQRResponse)
	require.True(t, ok)
	assert.Equal(t, first.QRString, payload.QRString)

	assert.Equal(t, 1, f.gateway.callCount(), "no second provider session")
}

func TestCheckoutOrderKeepsFormsWhenTheGatewayFails(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)
	f.gateway.err = errors.New("provider down")

	_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, formsFor(slotIDs))

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 502001, apperr.Numeric(appErr.HTTPStatus, appErr.Code))

	// The order stays PENDING with its forms saved and its hold deadline
	// untouched — retry re-runs only the gateway leg. No compensation: the
	// quota stays held by the live order (booking-flow.md §3).
	stored, storedErr := f.repo.GetOrderByNumber(context.Background(), orderNumber)
	require.NoError(t, storedErr)
	assert.Equal(t, "PENDING", stored.Status)
	require.NotNil(t, stored.BuyerName)
	assert.Equal(t, "Visitor", *stored.BuyerName, "the derived primary contact survives the failure")
	assert.Nil(t, stored.PaymentQRString)

	// And the retry succeeds once the provider recovers.
	f.gateway.err = nil
	_, retryErr := f.svc.CheckoutOrder(context.Background(), orderNumber, formsFor(slotIDs))
	require.NoError(t, retryErr)
}

func TestCheckoutOrderRejectsAForeignSlotID(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	forged := formsFor(slotIDs)
	forged.Attendees[1].ID = uuid.New()

	_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, forged)

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	assert.Equal(t, 0, f.gateway.callCount())
}

// --- Spec 010: one visitor per bundle unit -----------------------------------

// bookAgreedBundleOrder books qty units of the 2-constituent bundle and records
// the agreement, returning the order number and its slots (unit-grouped order).
func bookAgreedBundleOrder(t *testing.T, f checkoutFixture, qty int32) (string, []order.AttendeeSlotRecord) {
	t.Helper()
	ev, _, _, pkg := f.seedBundleEvent(t, 10, 10)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	pkgID := pkg.ID
	resp, err := f.svc.Book(context.Background(), order.BookRequest{
		EventID: ev.ID,
		Items:   []order.CheckoutItem{{PackageID: &pkgID, Quantity: qty}},
	})
	require.NoError(t, err)
	require.NoError(t, f.svc.RecordAgreement(context.Background(), resp.OrderID,
		order.AgreementRequest{Agreed: true, EventTermsID: termsID}))

	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	return resp.OrderID, slots
}

// visitorNamed is one filled visitor form entry for a slot; identity varies by
// name+email, the rest stays constant.
func visitorNamed(id uuid.UUID, name, email string) order.CheckoutVisitor {
	return order.CheckoutVisitor{
		ID: id, Name: name, Email: email,
		Phone: "081234567890", Dob: "2000-01-31", Gender: "FEMALE",
	}
}

func bundleForms(visitors []order.CheckoutVisitor) order.CheckoutFormsRequest {
	return order.CheckoutFormsRequest{Attendees: visitors}
}

func TestCheckoutBundleUnitRejectsDivergentVisitorData(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slots := bookAgreedBundleOrder(t, f, 1)
	require.Len(t, slots, 2)

	// Same unit, two different visitors — the single-form contract is broken.
	forms := bundleForms([]order.CheckoutVisitor{
		visitorNamed(slots[0].ID, "Visitor", "visitor@example.com"),
		visitorNamed(slots[1].ID, "Someone Else", "other@example.com"),
	})
	_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, forms)

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, 400001, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
	fields, ok := appErr.Data.(map[string]string)
	require.True(t, ok, "400001 carries the field map as data")
	assert.Contains(t, fields, "attendees[1].name")
	assert.Contains(t, fields, "attendees[1].email")
	assert.Equal(t,
		"All tickets in the same bundle must use the same visitor information.",
		fields["attendees[1].email"])

	// Nothing was saved and no session opened.
	stored, err := f.repo.GetOrderByNumber(context.Background(), orderNumber)
	require.NoError(t, err)
	assert.Nil(t, stored.BuyerName)
	assert.Equal(t, 0, f.gateway.callCount())
}

func TestCheckoutBundleUnitAcceptsIdenticalVisitors(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slots := bookAgreedBundleOrder(t, f, 1)
	require.Len(t, slots, 2)

	forms := bundleForms([]order.CheckoutVisitor{
		visitorNamed(slots[0].ID, "Visitor", "visitor@example.com"),
		visitorNamed(slots[1].ID, "Visitor", "visitor@example.com"),
	})
	_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, forms)
	require.NoError(t, err)

	// Both of the unit's tickets store the one visitor (FR-003).
	stored, err := f.repo.GetOrderByNumber(context.Background(), orderNumber)
	require.NoError(t, err)
	saved, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	for _, slot := range saved {
		require.NotNil(t, slot.Name)
		assert.Equal(t, "Visitor", *slot.Name)
		require.NotNil(t, slot.Email)
		assert.Equal(t, "visitor@example.com", *slot.Email)
	}
	assert.Equal(t, 1, f.gateway.callCount())
}

func TestCheckoutBundleAllowsDifferentVisitorsAcrossUnits(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slots := bookAgreedBundleOrder(t, f, 2)
	require.Len(t, slots, 4, "2 units × 2 constituents")

	// One visitor per UNIT (spec 010 US3): unit 1 → Visitor One, unit 2 → Two.
	visitors := make([]order.CheckoutVisitor, 0, len(slots))
	emailByUnit := map[int16]string{1: "one@example.com", 2: "two@example.com"}
	nameByUnit := map[int16]string{1: "Visitor One", 2: "Visitor Two"}
	for _, slot := range slots {
		require.NotNil(t, slot.PackageUnit)
		visitors = append(visitors,
			visitorNamed(slot.ID, nameByUnit[*slot.PackageUnit], emailByUnit[*slot.PackageUnit]))
	}
	_, err := f.svc.CheckoutOrder(context.Background(), orderNumber, bundleForms(visitors))
	require.NoError(t, err)

	// Each unit's two tickets carry that unit's visitor.
	stored, err := f.repo.GetOrderByNumber(context.Background(), orderNumber)
	require.NoError(t, err)
	saved, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	for _, slot := range saved {
		require.NotNil(t, slot.PackageUnit)
		require.NotNil(t, slot.Email)
		assert.Equal(t, emailByUnit[*slot.PackageUnit], *slot.Email)
	}
}

func TestCheckoutMixedOrderKeepsStandaloneVisitorIndependent(t *testing.T) {
	f := newCheckoutFixture(t)
	ev, _, _, pkg := f.seedBundleEvent(t, 10, 10)
	solo := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	pkgID := pkg.ID
	soloID := solo.ID
	resp, err := f.svc.Book(context.Background(), order.BookRequest{
		EventID: ev.ID,
		Items: []order.CheckoutItem{
			{PackageID: &pkgID, Quantity: 1},
			{TicketTypeID: &soloID, Quantity: 1},
		},
	})
	require.NoError(t, err)
	require.NoError(t, f.svc.RecordAgreement(context.Background(), resp.OrderID,
		order.AgreementRequest{Agreed: true, EventTermsID: termsID}))

	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	require.Len(t, slots, 3, "2 bundle constituents + 1 standalone")

	// The bundle unit shares one visitor; the standalone ticket has its own —
	// only same-unit divergence is forbidden (spec 010 US2).
	visitors := make([]order.CheckoutVisitor, 0, len(slots))
	for _, slot := range slots {
		if slot.PackageID.Valid {
			visitors = append(visitors, visitorNamed(slot.ID, "Bundle Visitor", "bundle@example.com"))
		} else {
			visitors = append(visitors, visitorNamed(slot.ID, "Solo Visitor", "solo@example.com"))
		}
	}
	_, err = f.svc.CheckoutOrder(context.Background(), resp.OrderID, bundleForms(visitors))
	require.NoError(t, err)

	saved, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	for _, slot := range saved {
		require.NotNil(t, slot.Email)
		if slot.PackageID.Valid {
			assert.Equal(t, "bundle@example.com", *slot.Email)
			require.NotNil(t, slot.Name)
			assert.Equal(t, "Bundle Visitor", *slot.Name)
		} else {
			assert.Equal(t, "solo@example.com", *slot.Email)
			require.NotNil(t, slot.Name)
			assert.Equal(t, "Solo Visitor", *slot.Name)
		}
	}
}

// Spec 011: the primary contact is the visitor of the FIRST slot in canonical
// order (standalone slots sort before bundle slots) — never simply the first
// element of the client-controlled attendees array.
func TestCheckoutMixedOrderDerivesThePrimaryContactFromTheFirstCanonicalSlot(t *testing.T) {
	f := newCheckoutFixture(t)
	ev, _, _, pkg := f.seedBundleEvent(t, 10, 10)
	solo := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	pkgID := pkg.ID
	soloID := solo.ID
	resp, err := f.svc.Book(context.Background(), order.BookRequest{
		EventID: ev.ID,
		Items: []order.CheckoutItem{
			{PackageID: &pkgID, Quantity: 1},
			{TicketTypeID: &soloID, Quantity: 1},
		},
	})
	require.NoError(t, err)
	require.NoError(t, f.svc.RecordAgreement(context.Background(), resp.OrderID,
		order.AgreementRequest{Agreed: true, EventTermsID: termsID}))

	stored, err := f.repo.GetOrderByNumber(context.Background(), resp.OrderID)
	require.NoError(t, err)
	slots, err := f.repo.ListAttendeeSlotsByOrderID(context.Background(), stored.ID)
	require.NoError(t, err)
	require.Len(t, slots, 3, "2 bundle constituents + 1 standalone")
	require.False(t, slots[0].PackageID.Valid,
		"canonical order (package_id NULLS FIRST) puts the standalone slot first")

	// Submit the visitor forms in REVERSED canonical order, so the request
	// array leads with a bundle visitor.
	visitors := make([]order.CheckoutVisitor, 0, len(slots))
	for i := len(slots) - 1; i >= 0; i-- {
		slot := slots[i]
		if slot.PackageID.Valid {
			v := visitorNamed(slot.ID, "Bundle Visitor", "bundle@example.com")
			v.Phone = "089999999999"
			visitors = append(visitors, v)
		} else {
			v := visitorNamed(slot.ID, "Solo Visitor", "solo@example.com")
			v.Phone = "081111111111"
			visitors = append(visitors, v)
		}
	}
	require.NotEqual(t, slots[0].ID, visitors[0].ID, "the array's first element is not the first canonical slot")

	_, err = f.svc.CheckoutOrder(context.Background(), resp.OrderID,
		order.CheckoutFormsRequest{Attendees: visitors})
	require.NoError(t, err)

	// orders.buyer_* snapshots the standalone (first canonical) visitor, not
	// visitors[0] of the submitted array.
	var buyerName, buyerEmail, buyerPhone string
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT buyer_name, buyer_email, buyer_phone FROM orders WHERE id = $1`, stored.ID).
		Scan(&buyerName, &buyerEmail, &buyerPhone))
	assert.Equal(t, "Solo Visitor", buyerName)
	assert.Equal(t, "solo@example.com", buyerEmail)
	assert.Equal(t, "081111111111", buyerPhone)
	assert.NotEqual(t, visitors[0].Name, buyerName, "the request array's first element must not win")

	// The gateway received the same primary contact as its customer (FR-017).
	call := f.gateway.lastCall()
	assert.Equal(t, "Solo Visitor", call.CustomerName)
	assert.Equal(t, "solo@example.com", call.CustomerEmail)
	assert.Equal(t, "081111111111", call.CustomerPhone)
}

func TestCheckoutBundleExemptsLegacySlotsWithoutAUnit(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slots := bookAgreedBundleOrder(t, f, 1)
	require.Len(t, slots, 2)

	// Pre-010 order: bundle slots exist but carry no unit ordinal.
	_, err := f.pool.Exec(context.Background(),
		`UPDATE attendees SET package_unit = NULL
		 WHERE order_id = (SELECT id FROM orders WHERE order_number = $1)`, orderNumber)
	require.NoError(t, err)

	// Divergent visitors are the OLD contract — still accepted for such orders.
	forms := bundleForms([]order.CheckoutVisitor{
		visitorNamed(slots[0].ID, "Visitor", "visitor@example.com"),
		visitorNamed(slots[1].ID, "Someone Else", "other@example.com"),
	})
	_, checkoutErr := f.svc.CheckoutOrder(context.Background(), orderNumber, forms)
	require.NoError(t, checkoutErr)
	assert.Equal(t, 1, f.gateway.callCount())
}

func TestCheckoutOrderRefusesAnExpiredHold(t *testing.T) {
	f := newCheckoutFixture(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)
	_, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET payment_expires_at = now() - interval '1 minute' WHERE order_number = $1`,
		orderNumber)
	require.NoError(t, err)

	_, checkoutErr := f.svc.CheckoutOrder(context.Background(), orderNumber, formsFor(slotIDs))

	var appErr *apperr.Error
	require.True(t, errors.As(checkoutErr, &appErr))
	assert.Equal(t, 410001, apperr.Numeric(appErr.HTTPStatus, appErr.Code))
	assert.Equal(t, 0, f.gateway.callCount())
}

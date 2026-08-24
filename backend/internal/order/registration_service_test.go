package order_test

import (
	"context"
	"fmt"
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

// --- spec 022: free ticket registration -------------------------------------

// recordingFulfiller stands in for the ticket and notification domains, so these
// tests can assert that the post-commit work is DISPATCHED without dragging PDF
// rendering and SMTP into a transaction test.
type recordingFulfiller struct {
	mu       sync.Mutex
	issued   []uuid.UUID
	sent     []uuid.UUID
	issueErr error
}

func (r *recordingFulfiller) IssueTicketsForOrder(_ context.Context, orderID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.issueErr != nil {
		return r.issueErr
	}
	r.issued = append(r.issued, orderID)
	return nil
}

func (r *recordingFulfiller) SendTicketEmail(_ context.Context, orderID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, orderID)
	return nil
}

func (r *recordingFulfiller) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.issued), len(r.sent)
}

type registrationFixture struct {
	checkoutFixture
	fulfiller *recordingFulfiller
	event     testsupport.Event
	ticket    testsupport.TicketType
	terms     uuid.UUID
	updatedAt time.Time
}

func newRegistrationFixture(t *testing.T, quota int32) registrationFixture {
	t.Helper()
	f := newCheckoutFixture(t)
	fulfiller := &recordingFulfiller{}
	f.svc.WithFulfillment(fulfiller, fulfiller)

	ev := testsupport.SeedEvent(t, f.pool, "reg-event", "PUBLISHED")
	tt := testsupport.SeedRegistrationTicketType(t, f.pool, ev.ID, "Invitation Access", quota)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	return registrationFixture{
		checkoutFixture: f,
		fulfiller:       fulfiller,
		event:           ev,
		ticket:          tt,
		terms:           termsID,
		updatedAt:       termsUpdatedAt(t, f, ev.ID),
	}
}

func termsUpdatedAt(t *testing.T, f checkoutFixture, eventID uuid.UUID) time.Time {
	t.Helper()
	var at time.Time
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT updated_at FROM event_terms WHERE event_id = $1`, eventID).Scan(&at))
	return at
}

func (f registrationFixture) request() order.RegistrationRequest {
	return order.RegistrationRequest{
		Slug:                f.event.Slug,
		Name:                "Halo Registrant",
		Email:               "halo@example.com",
		Phone:               "628125567820",
		Dob:                 "1996-04-12",
		Gender:              "MALE",
		Agreed:              true,
		EventTermsUpdatedAt: f.updatedAt,
	}
}

func countRows(t *testing.T, f checkoutFixture, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, f.pool.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

// The happy path, asserted against the ROWS rather than the response — the
// response is deliberately just `{registered:true}`.
func TestRegisterFreeWritesAZeroTotalPaidOrder(t *testing.T) {
	f := newRegistrationFixture(t, 5)

	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()))

	var (
		status         string
		total          string
		subtotal       *string
		isRegistration bool
		buyerEmail     *string
		buyerName      *string
		buyerPhone     *string
		termsAgreedAt  *time.Time
		eventTermsID   *uuid.UUID
		expiresAt      *time.Time
		provider       *string
	)
	require.NoError(t, f.pool.QueryRow(context.Background(), `
		SELECT os.name, o.total_amount::text, o.subtotal::text,
		       EXISTS (SELECT 1 FROM order_items oi
		              JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		              WHERE oi.order_id = o.id AND NOT tt.is_visible),
		       o.buyer_email, o.buyer_name, o.buyer_phone,
		       o.terms_agreed_at, o.event_terms_id, o.payment_expires_at, o.payment_provider
		FROM orders o JOIN order_statuses os ON os.id = o.status_id
		WHERE EXISTS (SELECT 1 FROM order_items oi
		              JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		              WHERE oi.order_id = o.id AND NOT tt.is_visible)`).Scan(
		&status, &total, &subtotal, &isRegistration,
		&buyerEmail, &buyerName, &buyerPhone,
		&termsAgreedAt, &eventTermsID, &expiresAt, &provider))

	assert.Equal(t, "PAID", status, "a registration is created final")
	assert.Equal(t, "0.00", total)
	require.NotNil(t, subtotal)
	assert.Equal(t, "0.00", *subtotal)
	assert.True(t, isRegistration, "the marker is what keeps PAID's two origins apart")

	// buyer_email is the delivery address and is NOT optional: SendTicketEmail
	// refuses an empty snapshot, so a registration that left it NULL would issue
	// a ticket and then silently fail to deliver it — after the guest was told it
	// had been sent.
	require.NotNil(t, buyerEmail)
	assert.Equal(t, "halo@example.com", *buyerEmail)
	require.NotNil(t, buyerName)
	assert.Equal(t, "Halo Registrant", *buyerName)
	require.NotNil(t, buyerPhone)
	assert.Equal(t, "628125567820", *buyerPhone)

	// Stamped inline on the INSERT — RecordTermsAgreement is PENDING-guarded and
	// would have affected zero rows here.
	assert.NotNil(t, termsAgreedAt, "the agreement is part of the row")
	require.NotNil(t, eventTermsID)
	assert.Equal(t, f.terms, *eventTermsID)

	// Nothing that would make it look payable, or let a sweeper touch it.
	assert.Nil(t, expiresAt, "a registration is already final; nothing expires")
	assert.Nil(t, provider, "no gateway was involved")

	assert.Equal(t, 0, f.gateway.callCount(), "registration never talks to the gateway")
}

func TestRegisterFreeDeductsExactlyOnePlace(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()))
	assert.Equal(t, int32(4), quotaOf(t, f.checkoutFixture, f.ticket.ID))
}

func TestRegisterFreeWritesOneLineOneAttendeeAndNoFees(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()))

	var orderID uuid.UUID
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT o.id FROM orders o WHERE EXISTS (SELECT 1 FROM order_items oi
		              JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		              WHERE oi.order_id = o.id AND NOT tt.is_visible)`).Scan(&orderID))

	assert.Equal(t, 1, countRows(t, f.checkoutFixture,
		`SELECT count(*) FROM order_items WHERE order_id = $1 AND quantity = 1 AND price = 0`, orderID))
	// A zero-total order has no fees: fees are computed from a subtotal, and this
	// one is zero by construction (FR-027).
	assert.Equal(t, 0, countRows(t, f.checkoutFixture,
		`SELECT count(*) FROM order_fees WHERE order_id = $1`, orderID))
	assert.Equal(t, 0, countRows(t, f.checkoutFixture,
		`SELECT count(*) FROM payments WHERE order_id = $1`, orderID),
		"no payment session was ever opened")

	// The attendee is FILLED, not an empty slot: a registration has no held phase.
	var name, email, phone *string
	var genderID *int16
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT name, email, phone, gender_id FROM attendees WHERE order_id = $1`, orderID).
		Scan(&name, &email, &phone, &genderID))
	require.NotNil(t, name)
	assert.Equal(t, "Halo Registrant", *name)
	require.NotNil(t, email)
	assert.Equal(t, "halo@example.com", *email)
	assert.NotNil(t, genderID, "gender resolves through the master, as checkout does")
}

func TestRegisterFreeDispatchesIssuanceThenDelivery(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()))

	f.svc.WaitForRegistrationFulfillment()
	issued, sent := f.fulfiller.counts()
	assert.Equal(t, 1, issued)
	assert.Equal(t, 1, sent)
}

// Without a ticket there is nothing to email. Stopping is what keeps a guest from
// receiving an empty e-ticket document.
func TestRegisterFreeDoesNotDeliverWhenIssuanceFails(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	f.fulfiller.issueErr = fmt.Errorf("boom")

	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()))
	f.svc.WaitForRegistrationFulfillment()

	_, sent := f.fulfiller.counts()
	assert.Zero(t, sent, "no ticket, no email")
}

// --- FR-023: the duplicate-email rule ---------------------------------------

// Spec 022 FR-023: an address may register as many times as quota allows.
//
// This replaces three tests that enforced the opposite rule (refuse a repeat,
// match it case-insensitively, and do not let an unissued attendee row block).
// All three are gone with the rule. What is asserted instead is that the second
// registration is an ordinary registration in every respect — its own order, its
// own place taken — because "no rule" is only observable as a completed second
// write, not as the absence of an error.
func TestRegisterFreeAcceptsAnAddressThatAlreadyHoldsATicket(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()))
	f.svc.WaitForRegistrationFulfillment()
	issueTicketsFor(t, f.checkoutFixture)

	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()),
		"the same address registering again is not an error")
	f.svc.WaitForRegistrationFulfillment()

	assert.Equal(t, 2, countRows(t, f.checkoutFixture,
		`SELECT count(*) FROM orders o WHERE EXISTS (SELECT 1 FROM order_items oi
		              JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		              WHERE oi.order_id = o.id AND NOT tt.is_visible)`),
		"each submission is its own registration")
	assert.Equal(t, int32(3), quotaOf(t, f.checkoutFixture, f.ticket.ID),
		"and each takes its own place")
}

// Case is not a rule any more, but the address must still be STORED as typed and
// still reach delivery. This is the residue of the deleted case-insensitivity
// test, kept because the lowering it asserted about has to be gone from the write
// path, not merely unused by it.
func TestRegisterFreeStoresDifferentlyCasedAddressesIndependently(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()))

	shouty := f.request()
	shouty.Email = "HALO@Example.COM"
	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, shouty),
		"a differently-cased address is not a duplicate, because nothing is a duplicate")

	assert.Equal(t, 2, countRows(t, f.checkoutFixture,
		`SELECT count(*) FROM orders o WHERE EXISTS (SELECT 1 FROM order_items oi
		              JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		              WHERE oi.order_id = o.id AND NOT tt.is_visible)`))
}

// SC-002, INVERTED from what it once asserted, and kept for that reason.
//
// It used to prove that six concurrent submissions of one address produced
// exactly ONE ticket — the advisory lock was what made that hold. FR-023a removed
// the rule and the lock, so the correct outcome is now SIX. The test still earns
// its place, and arguably earns it more: it is the only thing proving that two
// submissions of one address do not CONTEND (a leftover lock would serialise them
// invisibly and still pass a count assertion) and that quota stays exact under
// concurrency with the address-scoped serialisation gone.
func TestConcurrentRegistrationsOfOneAddressAllSucceed(t *testing.T) {
	f := newRegistrationFixture(t, 10)

	const attempts = 6
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	start := make(chan struct{})

	for i := range attempts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request())
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		assert.NoErrorf(t, err, "attempt %d must not be refused on account of the others", i)
	}

	assert.Equal(t, attempts, countRows(t, f.checkoutFixture,
		`SELECT count(*) FROM orders o WHERE EXISTS (SELECT 1 FROM order_items oi
		              JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		              WHERE oi.order_id = o.id AND NOT tt.is_visible)`),
		"every concurrent submission is its own registration")
	assert.Equal(t, int32(10-attempts), quotaOf(t, f.checkoutFixture, f.ticket.ID),
		"quota is exact under concurrency — the quota row lock still does its job")
}

// --- the boundary: only registration-only types, only this event -------------

func TestRegisterFreeRefusesAPurchasableTicketType(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	paid := testsupport.SeedTicketType(t, f.pool, f.event.ID, "Regular", "150000.00", 10)

	err := f.svc.RegisterFree(context.Background(), paid.ID, f.request())
	requireCode(t, err, apperr.CodeTicketTypeNotFound)
	assert.Equal(t, 0, countRows(t, f.checkoutFixture, `SELECT count(*) FROM orders`),
		"a purchasable type must mint nothing")
	assert.Equal(t, int32(10), quotaOf(t, f.checkoutFixture, paid.ID))
}

func TestRegisterFreeRefusesATicketTypeFromAnotherEvent(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	other := testsupport.SeedEvent(t, f.pool, "other-event", "PUBLISHED")
	foreign := testsupport.SeedRegistrationTicketType(t, f.pool, other.ID, "Elsewhere", 5)

	requireCode(t, f.svc.RegisterFree(context.Background(), foreign.ID, f.request()),
		apperr.CodeTicketTypeNotFound)
}

func TestRegisterFreeRefusesAnUnpublishedEvent(t *testing.T) {
	f := newCheckoutFixture(t)
	fulfiller := &recordingFulfiller{}
	f.svc.WithFulfillment(fulfiller, fulfiller)

	draft := testsupport.SeedEvent(t, f.pool, "draft-event", "DRAFT")
	tt := testsupport.SeedRegistrationTicketType(t, f.pool, draft.ID, "Invitation", 5)
	testsupport.SeedEventTerms(t, f.pool, draft.ID, "<p>terms</p>")

	req := order.RegistrationRequest{
		Slug: draft.Slug, Name: "Halo", Email: "halo@example.com",
		Phone: "628125567820", Dob: "1996-04-12", Gender: "MALE",
		Agreed: true, EventTermsUpdatedAt: time.Now(),
	}
	requireCode(t, f.svc.RegisterFree(context.Background(), tt.ID, req),
		apperr.CodeTicketTypeNotFound)
}

// FR-012: every unavailable cause must be INDISTINGUISHABLE on the wire.
func TestRegistrationRefusalsAreIndistinguishable(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	other := testsupport.SeedEvent(t, f.pool, "other-event", "PUBLISHED")
	foreign := testsupport.SeedRegistrationTicketType(t, f.pool, other.ID, "Elsewhere", 5)
	paid := testsupport.SeedTicketType(t, f.pool, f.event.ID, "Regular", "150000.00", 10)

	messages := map[string]struct{}{}
	for _, id := range []uuid.UUID{uuid.New(), foreign.ID, paid.ID} {
		err := f.svc.RegisterFree(context.Background(), id, f.request())
		var appErr *apperr.Error
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, apperr.CodeTicketTypeNotFound, appErr.Code)
		messages[appErr.Message] = struct{}{}
	}
	assert.Len(t, messages, 1,
		"an unknown id, a foreign one and a purchasable one must read identically")
}

func TestRegisterFreeRefusesWhenQuotaIsExhausted(t *testing.T) {
	f := newRegistrationFixture(t, 0)
	requireCode(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()),
		apperr.CodeTicketTypeNotFound)
	assert.Equal(t, 0, countRows(t, f.checkoutFixture, `SELECT count(*) FROM orders`))
}

// --- FR-050: the terms version --------------------------------------------

// The check compares updated_at, NOT the row id — UpsertEventTerms overwrites in
// place and PRESERVES the id, so an id comparison could never fire here. This
// test fails against an id-based implementation, which is the point.
func TestRegisterFreeRefusesASupersededTermsVersion(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	stale := f.request()

	// Republish. Same row, same id, new updated_at.
	time.Sleep(1100 * time.Millisecond)
	newID := testsupport.SeedEventTerms(t, f.pool, f.event.ID, "<p>terms v2</p>")
	require.Equal(t, f.terms, newID, "an edit must preserve the id — that is why the id cannot be the version")

	requireCode(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, stale),
		apperr.CodeTermsChanged)
	assert.Equal(t, 0, countRows(t, f.checkoutFixture, `SELECT count(*) FROM orders`),
		"a stale acceptance writes nothing")
	assert.Equal(t, int32(5), quotaOf(t, f.checkoutFixture, f.ticket.ID))
}

func TestRegistrationPrerequisitesReportTheCurrentTermsVersion(t *testing.T) {
	f := newRegistrationFixture(t, 5)

	got, err := f.svc.RegistrationPrerequisites(context.Background(), f.event.Slug, f.ticket.ID)
	require.NoError(t, err)

	assert.Equal(t, f.ticket.ID, got.TicketTypeID)
	assert.Equal(t, f.event.ID, got.Event.ID)
	assert.Equal(t, f.terms, got.EventTermsID)
	assert.WithinDuration(t, f.updatedAt, got.EventTermsUpdatedAt, time.Second)
	assert.NotEmpty(t, got.Genders, "the form builds its options from the master list")
}

func TestRegistrationPrerequisitesRefuseAnEventWithoutTerms(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "termless", "PUBLISHED")
	tt := testsupport.SeedRegistrationTicketType(t, f.pool, ev.ID, "Invitation", 5)

	_, err := f.svc.RegistrationPrerequisites(context.Background(), ev.Slug, tt.ID)
	requireCode(t, err, apperr.CodeTermsMissing)
}

// --- FR-008: the purchase path refuses these types ---------------------------

func TestBookRefusesARegistrationOnlyTicketType(t *testing.T) {
	f := newRegistrationFixture(t, 5)

	_, err := f.svc.Book(context.Background(), bookFor(f.event.ID, f.ticket.ID, 1))
	requireCode(t, err, apperr.CodeTicketTypeNotOnSale)
	assert.Equal(t, int32(5), quotaOf(t, f.checkoutFixture, f.ticket.ID), "no quota moved")
}

// The seam covers availability too, not just booking. If this fails, the refusal
// has been placed in Book rather than in expandTicket, and the advisory check
// still calls a registration-only type buyable — putting the refusal AFTER the
// Terms & Conditions gate that spec 013 deliberately placed it before.
func TestAvailabilityRefusesARegistrationOnlyTicketType(t *testing.T) {
	f := newRegistrationFixture(t, 5)

	decision, err := f.svc.EvaluateAvailability(context.Background(), order.AvailabilityRequest{
		EventID: f.event.ID,
		Items:   []order.CheckoutItem{{TicketTypeID: &f.ticket.ID, Quantity: 1}},
	})
	require.NoError(t, err, "an availability refusal is a 200 with available:false")
	assert.False(t, decision.Available,
		"availability must not report a registration-only type as buyable")
}

// issueTicketsFor promotes every registration attendee to a real issued ticket,
// so the duplicate rule has something to find. Done through SQL rather than the
// ticket service because these tests do not own that domain — the rule under
// test is "does an issued ticket exist", not "how is one issued".
func issueTicketsFor(t *testing.T, f checkoutFixture) {
	t.Helper()
	_, err := f.pool.Exec(context.Background(), `
		INSERT INTO tickets (ticket_code, order_id, attendee_id, status)
		SELECT 'TEST-' || substr(a.id::text, 1, 8), a.order_id, a.id, 'ACTIVE'
		FROM attendees a
		JOIN orders o ON o.id = a.order_id
		WHERE EXISTS (SELECT 1 FROM order_items oi
		              JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		              WHERE oi.order_id = o.id AND NOT tt.is_visible)
		  AND NOT EXISTS (SELECT 1 FROM tickets t WHERE t.attendee_id = a.id)`)
	require.NoError(t, err)
}

func requireCode(t *testing.T, err error, want string) {
	t.Helper()
	require.Error(t, err)
	var appErr *apperr.Error
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, want, appErr.Code)
}

// --- spec 022 T053a: booking's terms-staleness check was DEAD ---------------

// A regression pinning the pre-existing defect this feature uncovered.
//
// `UpsertEventTerms` is `ON CONFLICT (event_id) DO UPDATE` on a UNIQUE event_id,
// so an admin edit overwrites the row IN PLACE and preserves its id. The check
// this endpoint shipped with compared IDS, so it could never fire for the very
// case its own doc comment described: "the document changed while you were
// reading it". A guest could agree to text they had never seen.
//
// This test fails against the id comparison and passes against the updated_at
// one. That is the whole point of it.
func TestRecordAgreementRefusesARepublishedDocument(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "stale-terms", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>original</p>")

	var readAt time.Time
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT updated_at FROM event_terms WHERE event_id = $1`, ev.ID).Scan(&readAt))

	booked, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))
	require.NoError(t, err)

	// The admin republishes while the guest is reading.
	time.Sleep(1100 * time.Millisecond)
	republishedID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>rewritten</p>")
	require.Equal(t, termsID, republishedID,
		"an edit preserves the row id — which is exactly why the id cannot be the version")

	err = f.svc.RecordAgreement(context.Background(), booked.OrderID, order.AgreementRequest{
		Agreed:              true,
		EventTermsID:        termsID,
		EventTermsUpdatedAt: &readAt,
	})
	requireCode(t, err, apperr.CodeTermsChanged)
}

// A client that has not been updated to send the version still works: it falls
// back to the id comparison, which is the pre-022 behaviour. A stale browser tab
// should keep functioning, not be refused outright.
func TestRecordAgreementStillAcceptsAClientThatSendsNoVersion(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "no-version", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	booked, err := f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))
	require.NoError(t, err)

	assert.NoError(t, f.svc.RecordAgreement(context.Background(), booked.OrderID,
		order.AgreementRequest{Agreed: true, EventTermsID: termsID}))
}

// The cost of deriving `is_registration` instead of storing it, pinned as a test
// so it is visible rather than only described in a comment.
//
// Constitution Principle IV v6.0.0 removed the stored marker at explicit
// direction: an order is registration-originated exactly when it carries a line
// for a ticket type that is not guest-visible. That makes the classification a
// function of CURRENT ticket-type state, not of what happened when the order was
// placed — so an admin who makes an invitation type purchasable again silently
// reclassifies every historical order that used it.
//
// This test asserts that reclassification HAPPENS. It is not a bug report; it is
// the accepted consequence, recorded here so that anyone who later finds it
// surprising can see it was chosen rather than overlooked — and so that anyone
// "fixing" it discovers they are reversing a governance decision.
//
// The practical blast radius: OrderForDelivery reads this, so a RESEND of an
// already-delivered registration would start rendering a receipt for an order
// that never had a payment.
func TestDerivedRegistrationFlagFlipsWhenTheTicketTypeBecomesPurchasable(t *testing.T) {
	f := newRegistrationFixture(t, 5)
	require.NoError(t, f.svc.RegisterFree(context.Background(), f.ticket.ID, f.request()))
	f.svc.WaitForRegistrationFulfillment()

	var orderID uuid.UUID
	require.NoError(t, f.pool.QueryRow(context.Background(),
		`SELECT o.id FROM orders o
		  WHERE EXISTS (SELECT 1 FROM order_items oi
		                JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		                WHERE oi.order_id = o.id AND NOT tt.is_visible)`).Scan(&orderID))

	before, err := f.repo.GetOrderByID(context.Background(), orderID)
	require.NoError(t, err)
	require.True(t, before.IsRegistration, "it was placed through the registration path")

	// The admin republishes the invitation type onto the purchase list.
	_, err = f.pool.Exec(context.Background(),
		`UPDATE ticket_types SET is_visible = TRUE WHERE id = $1`, f.ticket.ID)
	require.NoError(t, err)

	after, err := f.repo.GetOrderByID(context.Background(), orderID)
	require.NoError(t, err)
	assert.False(t, after.IsRegistration,
		"a derived classification follows current ticket-type state — the same order "+
			"now reads as a purchase, and a resend would render it a receipt")
}

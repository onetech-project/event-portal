package order_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// --- spec 013: the availability check in front of the Terms gate ------------

func availabilityFor(eventID uuid.UUID, items ...order.CheckoutItem) order.AvailabilityRequest {
	return order.AvailabilityRequest{EventID: eventID, Items: items}
}

func ticketItem(id uuid.UUID, qty int32) order.CheckoutItem {
	ttID := id
	return order.CheckoutItem{TicketTypeID: &ttID, Quantity: qty}
}

func packageItem(id uuid.UUID, qty int32) order.CheckoutItem {
	pkgID := id
	return order.CheckoutItem{PackageID: &pkgID, Quantity: qty}
}

// codesOf collapses a decision to its reason codes, for assertions that care
// about which refusals fired rather than their exact wording.
func codesOf(decision order.AvailabilityDecision) []string {
	codes := make([]string, 0, len(decision.Reasons))
	for _, reason := range decision.Reasons {
		codes = append(codes, reason.Code)
	}
	return codes
}

func TestAvailabilityAcceptsAPurchasableSelection(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "available", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	decision, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, ticketItem(tt.ID, 2)))
	require.NoError(t, err)

	assert.True(t, decision.Available)
	assert.Empty(t, decision.Reasons)

	// The whole point of the check: it reserves nothing.
	assert.Equal(t, int32(10), quotaOf(t, f, tt.ID), "checking must not hold quota")
}

func TestAvailabilityRefusesMoreThanRemainingQuota(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "scarce", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 1)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	decision, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, ticketItem(tt.ID, 3)))
	require.NoError(t, err)

	assert.False(t, decision.Available)
	require.Len(t, decision.Reasons, 1)
	assert.Equal(t, apperr.CodeInsufficientQuota, decision.Reasons[0].Code)
	require.NotNil(t, decision.Reasons[0].ItemIndex)
	assert.Equal(t, 0, *decision.Reasons[0].ItemIndex)
	require.NotNil(t, decision.Reasons[0].TicketTypeID)
	assert.Equal(t, tt.ID, *decision.Reasons[0].TicketTypeID)

	// Worded exactly as bookOnce words it, so meeting the same shortfall again
	// at Agree does not tell the guest a different story (FR-013).
	assert.Equal(t, "Only fewer than 3 ticket(s) remain.", decision.Reasons[0].Message)

	assert.Equal(t, int32(1), quotaOf(t, f, tt.ID), "a refusal holds nothing either")
}

func TestAvailabilityRefusesATicketOutsideItsSalesWindow(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "closed-window", "PUBLISHED")
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	closed := testsupport.SeedTicketTypeWindow(t, f.pool, ev.ID, "Early Bird", 10,
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))

	decision, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, ticketItem(closed.ID, 1)))
	require.NoError(t, err)

	assert.False(t, decision.Available)
	require.Len(t, decision.Reasons, 1)
	assert.Equal(t, apperr.CodeTicketTypeNotOnSale, decision.Reasons[0].Code)
	assert.Contains(t, decision.Reasons[0].Message, "Early Bird")
}

func TestAvailabilityRefusesATicketTypeThatDoesNotExist(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "gone", "PUBLISHED")
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	decision, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, ticketItem(uuid.New(), 1)))
	require.NoError(t, err)

	assert.False(t, decision.Available)
	assert.Equal(t, []string{apperr.CodeTicketTypeNotFound}, codesOf(decision))
}

func TestAvailabilityRefusesAnItemFromAnotherEvent(t *testing.T) {
	f := newCheckoutFixture(t)
	target := testsupport.SeedEvent(t, f.pool, "target", "PUBLISHED")
	testsupport.SeedEventTerms(t, f.pool, target.ID, "<p>terms</p>")
	other := testsupport.SeedEvent(t, f.pool, "other", "PUBLISHED")
	foreign := testsupport.SeedTicketType(t, f.pool, other.ID, "Foreign", "10000.00", 5)

	decision, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(target.ID, ticketItem(foreign.ID, 1)))
	require.NoError(t, err)

	assert.False(t, decision.Available)
	assert.Equal(t, []string{apperr.CodeValidation}, codesOf(decision))
}

// FR-006: the guest must not have to peel their selection apart one rejected
// line per attempt.
func TestAvailabilityReportsEveryOffendingLineNotJustTheFirst(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "two-problems", "PUBLISHED")
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")
	closed := testsupport.SeedTicketTypeWindow(t, f.pool, ev.ID, "Early Bird", 10,
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	scarce := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 1)

	decision, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, ticketItem(closed.ID, 1), ticketItem(scarce.ID, 5)))
	require.NoError(t, err)

	assert.False(t, decision.Available)
	assert.ElementsMatch(t,
		[]string{apperr.CodeTicketTypeNotOnSale, apperr.CodeInsufficientQuota},
		codesOf(decision),
		"both lines are judged; booking's fail-fast would have reported only the first")
}

// FR-005, and the oversell aggregateDemand exists to stop: either line alone
// fits, together they do not. Judging them in isolation would approve a
// selection the booking transaction then refuses.
func TestAvailabilityAggregatesDemandAcrossABundleAndAStandaloneTicket(t *testing.T) {
	f := newCheckoutFixture(t)
	ev, day1, _, pkg := f.seedBundleEvent(t, 1, 5)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	// Day 1 has exactly one seat. The bundle needs it, and so does the
	// standalone line — each is satisfiable alone.
	alone, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, ticketItem(day1.ID, 1)))
	require.NoError(t, err)
	require.True(t, alone.Available, "one Day 1 seat covers the standalone line")

	bundleAlone, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, packageItem(pkg.ID, 1)))
	require.NoError(t, err)
	require.True(t, bundleAlone.Available, "the same seat covers the bundle")

	together, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, packageItem(pkg.ID, 1), ticketItem(day1.ID, 1)))
	require.NoError(t, err)

	assert.False(t, together.Available, "two lines cannot both take the last Day 1 seat")

	// Both contributing lines are named, because the guest is looking at their
	// selection and has to know which rows to change.
	require.Len(t, together.Reasons, 2)
	for _, reason := range together.Reasons {
		assert.Equal(t, apperr.CodeInsufficientQuota, reason.Code)
		require.NotNil(t, reason.TicketTypeID)
		assert.Equal(t, day1.ID, *reason.TicketTypeID, "the scarce constituent is named")
	}
}

func TestAvailabilityRefusesAnEventWithNoAuthoredTerms(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "no-terms-yet", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)

	decision, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, ticketItem(tt.ID, 1)))
	require.NoError(t, err)

	assert.False(t, decision.Available)
	require.Len(t, decision.Reasons, 1)
	assert.Equal(t, apperr.CodeTermsMissing, decision.Reasons[0].Code)
	// Order-level, not attributable to a line: no line is at fault, and the
	// client must not highlight an arbitrary row for it.
	assert.Nil(t, decision.Reasons[0].ItemIndex, "TERMS_MISSING belongs to the order")
}

// A malformed request has no availability answer. It is an error, not a
// decision — otherwise a client could not tell "your ticket stopped selling"
// from "your request was nonsense".
func TestAvailabilityRejectsMalformedRequests(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "malformed", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 5)
	both := tt.ID

	cases := map[string]order.AvailabilityRequest{
		"no event":       availabilityFor(uuid.Nil, ticketItem(tt.ID, 1)),
		"no items":       availabilityFor(ev.ID),
		"zero quantity":  availabilityFor(ev.ID, ticketItem(tt.ID, 0)),
		"neither id set": {EventID: ev.ID, Items: []order.CheckoutItem{{Quantity: 1}}},
		"both ids set": {EventID: ev.ID, Items: []order.CheckoutItem{
			{TicketTypeID: &both, PackageID: &both, Quantity: 1},
		}},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := f.svc.EvaluateAvailability(context.Background(), req)
			require.Error(t, err)

			var appErr *apperr.Error
			require.True(t, errors.As(err, &appErr))
			assert.Equal(t, apperr.CodeValidation, appErr.Code)
		})
	}
}

// The race the check narrows but cannot close (FR-003, FR-011): a decision
// describes the instant it ran, and booking is still the authority afterwards.
func TestAvailabilityIsAdvisoryAndDoesNotBindTheBooking(t *testing.T) {
	f := newCheckoutFixture(t)
	ev := testsupport.SeedEvent(t, f.pool, "racy", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 1)
	testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	decision, err := f.svc.EvaluateAvailability(context.Background(),
		availabilityFor(ev.ID, ticketItem(tt.ID, 1)))
	require.NoError(t, err)
	require.True(t, decision.Available)

	// Somebody else takes the seat between the check and the Agree click.
	_, err = f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))
	require.NoError(t, err)

	// The earlier "available" answer buys nothing.
	_, err = f.svc.Book(context.Background(), bookFor(ev.ID, tt.ID, 1))
	require.Error(t, err)
	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, apperr.CodeInsufficientQuota, appErr.Code)
}

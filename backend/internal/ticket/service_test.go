package ticket_test

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

	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/internal/ticket"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

func newTicketService(t *testing.T, f ticketFixture) *ticket.Service {
	t.Helper()
	return ticket.NewService(f.pool, f.repo, nil, testsupport.DiscardLogger())
}

// --- Generation after payment ---------------------------------------------

func TestGenerateForOrderIssuesExactlyOneTicketPerAttendee(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	ctx := context.Background()

	second := testsupport.SeedAttendee(t, f.pool, f.orderID, f.ttype.ID, "Sari", "sari@example.com")
	third := testsupport.SeedAttendee(t, f.pool, f.orderID, f.ttype.ID, "Andi", "andi@example.com")

	issued, err := svc.GenerateForOrder(ctx, f.orderID, []uuid.UUID{f.attendee, second, third})

	require.NoError(t, err)
	assert.Len(t, issued, 3)

	stored, err := f.repo.ListByOrderID(ctx, f.orderID)
	require.NoError(t, err)
	assert.Len(t, stored, 3)

	codes := map[string]struct{}{}
	for _, tk := range stored {
		assert.Equal(t, ticket.StatusActive, tk.Status)
		assert.Len(t, tk.TicketCode, ticket.CodeLength)
		codes[tk.TicketCode] = struct{}{}
	}
	assert.Len(t, codes, 3, "every attendee gets a distinct code")
}

// The webhook can be delivered more than once, and the goroutine it launches must
// not mint a second set of tickets for an order that already has them.
func TestGenerateForOrderIsIdempotent(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	ctx := context.Background()

	first, err := svc.GenerateForOrder(ctx, f.orderID, []uuid.UUID{f.attendee})
	require.NoError(t, err)

	second, err := svc.GenerateForOrder(ctx, f.orderID, []uuid.UUID{f.attendee})
	require.NoError(t, err)

	assert.Equal(t, first[0].TicketCode, second[0].TicketCode,
		"a replayed notification must return the existing tickets, not new ones")

	stored, err := f.repo.ListByOrderID(ctx, f.orderID)
	require.NoError(t, err)
	assert.Len(t, stored, 1)
}

func TestConcurrentGenerationForTheSameOrderIssuesOneSet(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.GenerateForOrder(ctx, f.orderID, []uuid.UUID{f.attendee})
		}()
	}
	wg.Wait()

	stored, err := f.repo.ListByOrderID(ctx, f.orderID)
	require.NoError(t, err)
	assert.Len(t, stored, 1, "the attendee must end up with exactly one ticket")
}

func TestGenerateForOrderWithNoAttendeesIssuesNothing(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)

	issued, err := svc.GenerateForOrder(context.Background(), f.orderID, nil)

	require.NoError(t, err)
	assert.Empty(t, issued)
}

// --- Validate (read-only) -------------------------------------------------

func TestValidateReportsAnActiveTicketAsValid(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	testsupport.SeedTicket(t, f.pool, "VALID234AB", f.orderID, f.attendee, "ACTIVE")

	result, err := svc.Validate(context.Background(), "VALID234AB")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultValid, result.Result)
	assert.Equal(t, "VALID234AB", result.TicketCode)
	require.NotNil(t, result.AttendeeName)
	assert.Equal(t, "Budi Santoso", *result.AttendeeName)
	require.NotNil(t, result.TicketTypeName)
	assert.Equal(t, "Regular", *result.TicketTypeName)
	require.NotNil(t, result.EventName)
	assert.Equal(t, f.event.Name, *result.EventName)
}

func TestValidateReportsAUsedTicketAsAlreadyUsed(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	testsupport.SeedTicket(t, f.pool, "USED234ABC", f.orderID, f.attendee, "USED")

	result, err := svc.Validate(context.Background(), "USED234ABC")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultAlreadyUsed, result.Result)
	require.NotNil(t, result.AttendeeName, "the door staff still needs to see who this was")
}

func TestValidateReportsARevokedTicketAsInvalid(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	testsupport.SeedTicket(t, f.pool, "REVOK234AB", f.orderID, f.attendee, "REVOKED")

	result, err := svc.Validate(context.Background(), "REVOK234AB")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultInvalid, result.Result)
}

func TestValidateReportsAnUnknownCodeAsInvalidWithNoDetails(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)

	result, err := svc.Validate(context.Background(), "GHOST234AB")

	require.NoError(t, err, "an unknown code is a normal answer, not an error")
	assert.Equal(t, ticket.ResultInvalid, result.Result)
	assert.Nil(t, result.AttendeeName)
	assert.Nil(t, result.TicketTypeName)
	assert.Nil(t, result.EventName)
}

func TestValidateNormalizesTheInput(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	testsupport.SeedTicket(t, f.pool, "NORM234ABC", f.orderID, f.attendee, "ACTIVE")

	result, err := svc.Validate(context.Background(), "  norm234abc \n")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultValid, result.Result)
	assert.Equal(t, "NORM234ABC", result.TicketCode, "the response echoes the normalized code")
}

// validate must stay side-effect-free: at a door the same code is routinely
// scanned more than once before the admin decides to admit anyone.
func TestValidateNeverMutatesTheTicket(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	ctx := context.Background()
	testsupport.SeedTicket(t, f.pool, "PURE234ABC", f.orderID, f.attendee, "ACTIVE")

	for range 3 {
		result, err := svc.Validate(ctx, "PURE234ABC")
		require.NoError(t, err)
		assert.Equal(t, ticket.ResultValid, result.Result)
	}

	status, err := f.repo.GetStatusByCode(ctx, "PURE234ABC")
	require.NoError(t, err)
	assert.Equal(t, "ACTIVE", status)
}

// --- MarkUsed -------------------------------------------------------------

func TestMarkUsedConsumesAValidTicket(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	ctx := context.Background()
	testsupport.SeedTicket(t, f.pool, "ADMIT234AB", f.orderID, f.attendee, "ACTIVE")

	resp, err := svc.MarkUsed(ctx, " admit234ab ")

	require.NoError(t, err)
	assert.Equal(t, "ADMIT234AB", resp.TicketCode)
	assert.Equal(t, ticket.StatusUsed, resp.Status)

	result, err := svc.Validate(ctx, "ADMIT234AB")
	require.NoError(t, err)
	assert.Equal(t, ticket.ResultAlreadyUsed, result.Result, "re-validating now reports Already Used")
}

func TestMarkUsedReturnsAConflictForAnAlreadyUsedTicket(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	testsupport.SeedTicket(t, f.pool, "TWICE234AB", f.orderID, f.attendee, "USED")

	_, err := svc.MarkUsed(context.Background(), "TWICE234AB")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeAlreadyUsed, appErr.Code)
}

func TestMarkUsedReturnsAConflictForARevokedTicket(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	testsupport.SeedTicket(t, f.pool, "REVK234ABC", f.orderID, f.attendee, "REVOKED")

	_, err := svc.MarkUsed(context.Background(), "REVK234ABC")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)
}

func TestMarkUsedReturnsNotFoundForAnUnknownCode(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)

	_, err := svc.MarkUsed(context.Background(), "GHOST234AB")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeTicketNotFound, appErr.Code)
}

func TestConcurrentMarkUsedAdmitsExactlyOnce(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	ctx := context.Background()
	testsupport.SeedTicket(t, f.pool, "RACE234ABC", f.orderID, f.attendee, "ACTIVE")

	const scanners = 8
	var wg sync.WaitGroup
	errs := make([]error, scanners)
	for i := range scanners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = svc.MarkUsed(ctx, "RACE234ABC")
		}()
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	assert.Equal(t, 1, succeeded, "two admins scanning at once must not both admit the holder")
}

// --- Public lookup (spec FR-016) ------------------------------------------

func TestLookupPublicReturnsOnlyTheDisclosableFields(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)
	testsupport.SeedTicket(t, f.pool, "PUB234ABCD", f.orderID, f.attendee, "ACTIVE")

	got, err := svc.LookupPublic(context.Background(), " pub234abcd ")

	require.NoError(t, err)
	assert.Equal(t, "PUB234ABCD", got.TicketCode)
	assert.Equal(t, "ACTIVE", got.Status)
	assert.Equal(t, f.event.Name, got.EventName)
	assert.Equal(t, "Budi Santoso", got.AttendeeName)
}

func TestLookupPublicReturnsAFlat404ForAnUnknownCode(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)

	_, err := svc.LookupPublic(context.Background(), "GHOST234AB")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
}

// A malformed code must be indistinguishable from an unknown one, so the endpoint
// cannot be used to probe the code format.
func TestLookupPublicDoesNotDistinguishMalformedFromUnknown(t *testing.T) {
	f := newTicketFixture(t)
	svc := newTicketService(t, f)

	_, unknownErr := svc.LookupPublic(context.Background(), "GHOST234AB")
	_, malformedErr := svc.LookupPublic(context.Background(), "!!!")
	_, emptyErr := svc.LookupPublic(context.Background(), "   ")

	var a, b, c *apperr.Error
	require.True(t, errors.As(unknownErr, &a))
	require.True(t, errors.As(malformedErr, &b))
	require.True(t, errors.As(emptyErr, &c))

	assert.Equal(t, a.HTTPStatus, b.HTTPStatus)
	assert.Equal(t, a.Code, b.Code)
	assert.Equal(t, a.Message, b.Message)
	assert.Equal(t, a.Message, c.Message)
}

// --- Admission window at the gate (spec 015 US3) --------------------------

// ticketWindow moves the fixture ticket type's admission window and returns its
// bounds. Ticket-type configuration is not order, ticket or payment state.
func ticketWindow(t *testing.T, f ticketFixture, start, end time.Time) (time.Time, time.Time) {
	t.Helper()
	// Truncated to what Postgres actually stores. Without this the returned
	// bounds carry nanoseconds the column drops, and "validate at exactly the
	// end" would land a few hundred nanoseconds past the stored value.
	start = start.Truncate(time.Microsecond)
	end = end.Truncate(time.Microsecond)
	_, err := f.pool.Exec(context.Background(),
		`UPDATE ticket_types SET event_start = $2, event_end = $3 WHERE id = $1`,
		f.ttype.ID, start, end)
	require.NoError(t, err)
	return start, end
}

// serviceAt builds a service whose clock reads a fixed instant, so the boundary
// cases are exact rather than approximately-now.
func serviceAt(t *testing.T, f ticketFixture, at time.Time) *ticket.Service {
	t.Helper()
	return ticket.NewService(f.pool, f.repo, nil, testsupport.DiscardLogger()).
		WithClock(func() time.Time { return at })
}

func TestValidateAdmitsATicketInsideItsWindow(t *testing.T) {
	f := newTicketFixture(t)
	start, end := ticketWindow(t, f, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	testsupport.SeedTicket(t, f.pool, "INWINDOW01", f.orderID, f.attendee, "ACTIVE")

	got, err := serviceAt(t, f, start.Add(30*time.Minute)).
		Validate(context.Background(), "INWINDOW01")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultValid, got.Result)
	require.NotNil(t, got.EventStart)
	assert.WithinDuration(t, start, *got.EventStart, time.Second)
	assert.WithinDuration(t, end, *got.EventEnd, time.Second)
}

func TestValidateRefusesATicketBeforeItsWindowOpens(t *testing.T) {
	f := newTicketFixture(t)
	start, _ := ticketWindow(t, f, time.Now().Add(24*time.Hour), time.Now().Add(30*time.Hour))
	testsupport.SeedTicket(t, f.pool, "TOOEARLY01", f.orderID, f.attendee, "ACTIVE")

	got, err := serviceAt(t, f, start.Add(-time.Hour)).
		Validate(context.Background(), "TOOEARLY01")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultNotYetValid, got.Result)
	require.NotNil(t, got.EventStart, "the refusal names the day the ticket does apply to")
}

func TestValidateRefusesATicketAfterItsWindowCloses(t *testing.T) {
	f := newTicketFixture(t)
	_, end := ticketWindow(t, f, time.Now().Add(-30*time.Hour), time.Now().Add(-24*time.Hour))
	testsupport.SeedTicket(t, f.pool, "TOOLATE001", f.orderID, f.attendee, "ACTIVE")

	got, err := serviceAt(t, f, end.Add(time.Hour)).
		Validate(context.Background(), "TOOLATE001")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultExpired, got.Result)
	require.NotNil(t, got.EventEnd)
}

// FR-014: both endpoints are inclusive, and there is no grace period. These two
// are the whole difference between a workable gate and one that turns away
// everyone who arrives on the hour.
func TestValidateTreatsBothWindowEndpointsAsInclusive(t *testing.T) {
	f := newTicketFixture(t)
	start, end := ticketWindow(t, f, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	testsupport.SeedTicket(t, f.pool, "BOUNDARY01", f.orderID, f.attendee, "ACTIVE")
	ctx := context.Background()

	atStart, err := serviceAt(t, f, start).Validate(ctx, "BOUNDARY01")
	require.NoError(t, err)
	assert.Equal(t, ticket.ResultValid, atStart.Result, "exactly at the start is admitted")

	atEnd, err := serviceAt(t, f, end).Validate(ctx, "BOUNDARY01")
	require.NoError(t, err)
	assert.Equal(t, ticket.ResultValid, atEnd.Result, "exactly at the end is admitted")
}

func TestValidateAllowsNoGracePeriodBeforeTheWindow(t *testing.T) {
	f := newTicketFixture(t)
	start, _ := ticketWindow(t, f, time.Now().Add(time.Hour), time.Now().Add(2*time.Hour))
	testsupport.SeedTicket(t, f.pool, "NOGRACE001", f.orderID, f.attendee, "ACTIVE")

	got, err := serviceAt(t, f, start.Add(-time.Millisecond)).
		Validate(context.Background(), "NOGRACE001")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultNotYetValid, got.Result,
		"one moment early is refused — the event start is when admission opens, not showtime")
}

// FR-018: already-used beats out-of-window. Telling an admin the ticket was
// already admitted is more useful than telling them it is the wrong day.
func TestValidateReportsAlreadyUsedAheadOfTheWindow(t *testing.T) {
	f := newTicketFixture(t)
	start, _ := ticketWindow(t, f, time.Now().Add(24*time.Hour), time.Now().Add(30*time.Hour))
	testsupport.SeedTicket(t, f.pool, "USEDEARLY1", f.orderID, f.attendee, "USED")

	got, err := serviceAt(t, f, start.Add(-time.Hour)).
		Validate(context.Background(), "USEDEARLY1")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultAlreadyUsed, got.Result)
}

// FR-019: an unknown code still discloses nothing, the window included.
func TestValidateDisclosesNoWindowForAnUnknownCode(t *testing.T) {
	f := newTicketFixture(t)

	got, err := serviceAt(t, f, time.Now()).Validate(context.Background(), "NOSUCHCODE")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultInvalid, got.Result)
	assert.Nil(t, got.EventStart)
	assert.Nil(t, got.EventEnd)
}

func TestValidateReportsARevokedTicketAsInvalidRegardlessOfTheWindow(t *testing.T) {
	f := newTicketFixture(t)
	ticketWindow(t, f, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	testsupport.SeedTicket(t, f.pool, "REVOKED001", f.orderID, f.attendee, "REVOKED")

	got, err := serviceAt(t, f, time.Now()).Validate(context.Background(), "REVOKED001")

	require.NoError(t, err)
	assert.Equal(t, ticket.ResultInvalid, got.Result)
	assert.Nil(t, got.EventStart)
}

// FR-017: the UI hiding the button is not the enforcement.
func TestMarkUsedRefusesATicketOutsideItsWindow(t *testing.T) {
	f := newTicketFixture(t)
	start, _ := ticketWindow(t, f, time.Now().Add(24*time.Hour), time.Now().Add(30*time.Hour))
	testsupport.SeedTicket(t, f.pool, "NOADMIT001", f.orderID, f.attendee, "ACTIVE")
	ctx := context.Background()

	_, err := serviceAt(t, f, start.Add(-time.Hour)).MarkUsed(ctx, "NOADMIT001")

	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr))
	assert.Equal(t, http.StatusConflict, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeInvalidDateRange, appErr.Code)

	status, err := f.repo.GetStatusByCode(ctx, "NOADMIT001")
	require.NoError(t, err)
	assert.Equal(t, "ACTIVE", status, "a refused admit leaves the ticket admissible on the right day")
}

func TestMarkUsedStillAdmitsATicketInsideItsWindow(t *testing.T) {
	f := newTicketFixture(t)
	start, _ := ticketWindow(t, f, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	testsupport.SeedTicket(t, f.pool, "ADMITOK001", f.orderID, f.attendee, "ACTIVE")
	ctx := context.Background()

	got, err := serviceAt(t, f, start.Add(time.Minute)).MarkUsed(ctx, "ADMITOK001")

	require.NoError(t, err)
	assert.Equal(t, "USED", got.Status)
}

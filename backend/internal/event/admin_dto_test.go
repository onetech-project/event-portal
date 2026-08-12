package event_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/money"
)

func validEventRequest() event.EventRequest {
	return event.EventRequest{
		Name:      "Jazz Night 2026",
		Slug:      "jazz-night-2026",
		Venue:     "Balai Sarbini",
		Address:   "Jl. Jend. Sudirman",
		StartDate: time.Now().Add(30 * 24 * time.Hour),
		EndDate:   time.Now().Add(31 * 24 * time.Hour),
		Status:    event.StatusDraft,
	}
}

func validTicketTypeRequest() event.TicketTypeRequest {
	return event.TicketTypeRequest{
		EventID:    uuid.New(),
		Name:       "Regular",
		Price:      money.From(decimal.RequireFromString("150000.00")),
		Quota:      100,
		SalesStart: time.Now(),
		SalesEnd:   time.Now().Add(29 * 24 * time.Hour),
		// The admission window, independent of the sales window above (spec 015).
		// Deliberately an hour INSIDE the +30d/+31d span that validEventRequest and
		// testsupport.SeedEvent both use: those compute their bounds from a
		// different clock reading than this one, so an exactly-equal window would
		// land microseconds outside the parent and trip containment.
		EventStart: time.Now().Add(30*24*time.Hour + time.Hour),
		EventEnd:   time.Now().Add(31*24*time.Hour - time.Hour),
	}
}

func errCodeOf(t *testing.T, err error) string {
	t.Helper()
	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr), "expected an *apperr.Error, got %v", err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	return appErr.Code
}

func TestValidEventRequestPasses(t *testing.T) {
	require.NoError(t, validEventRequest().Validate())
}

func TestEventRequestRejectsMissingRequiredFields(t *testing.T) {
	tests := map[string]func(*event.EventRequest){
		"name":    func(r *event.EventRequest) { r.Name = "  " },
		"slug":    func(r *event.EventRequest) { r.Slug = "" },
		"venue":   func(r *event.EventRequest) { r.Venue = "" },
		"address": func(r *event.EventRequest) { r.Address = "" },
	}

	for field, mutate := range tests {
		t.Run(field, func(t *testing.T) {
			req := validEventRequest()
			mutate(&req)
			assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate()))
		})
	}
}

func TestEventRequestRejectsAMalformedSlug(t *testing.T) {
	for _, slug := range []string{"Jazz Night", "JAZZ-NIGHT", "jazz_night", "-jazz", "jazz--night", "jazz-"} {
		t.Run(slug, func(t *testing.T) {
			req := validEventRequest()
			req.Slug = slug
			assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate()))
		})
	}
}

func TestEventRequestRejectsAnEndDateBeforeItsStart(t *testing.T) {
	req := validEventRequest()
	req.EndDate = req.StartDate.Add(-time.Hour)

	assert.Equal(t, apperr.CodeInvalidDateRange, errCodeOf(t, req.Validate()))
}

func TestEventRequestAllowsASingleInstantEvent(t *testing.T) {
	req := validEventRequest()
	req.EndDate = req.StartDate

	require.NoError(t, req.Validate(), "end_date == start_date is a valid zero-length window")
}

func TestEventRequestRejectsAnUnknownStatus(t *testing.T) {
	req := validEventRequest()
	req.Status = "ARCHIVED"

	assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate()))
}

func TestEventRequestAcceptsEveryDeclaredStatus(t *testing.T) {
	for _, status := range []string{event.StatusDraft, event.StatusPublished, event.StatusCompleted} {
		req := validEventRequest()
		req.Status = status
		assert.NoError(t, req.Validate(), status)
	}
}

func TestEventRequestRejectsMissingDates(t *testing.T) {
	req := validEventRequest()
	req.StartDate = time.Time{}

	assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate()))
}

// --- Ticket type ----------------------------------------------------------

func TestValidTicketTypeRequestPasses(t *testing.T) {
	require.NoError(t, validTicketTypeRequest().Validate(true))
}

func TestTicketTypeRequestRequiresEventIDOnlyOnCreate(t *testing.T) {
	req := validTicketTypeRequest()
	req.EventID = uuid.Nil

	assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate(true)))
	assert.NoError(t, req.Validate(false), "event_id is immutable on update and ignored")
}

func TestTicketTypeRequestRejectsANegativeQuota(t *testing.T) {
	req := validTicketTypeRequest()
	req.Quota = -1

	assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate(true)))
}

// A zero remaining quota is legitimate — it means sold out, not invalid.
func TestTicketTypeRequestAllowsAZeroQuota(t *testing.T) {
	req := validTicketTypeRequest()
	req.Quota = 0

	require.NoError(t, req.Validate(true))
}

func TestTicketTypeRequestRejectsANegativePrice(t *testing.T) {
	req := validTicketTypeRequest()
	req.Price = money.From(decimal.RequireFromString("-1"))

	assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate(true)))
}

func TestTicketTypeRequestAllowsAFreeTicket(t *testing.T) {
	req := validTicketTypeRequest()
	req.Price = money.From(decimal.Zero)

	require.NoError(t, req.Validate(true))
}

func TestTicketTypeRequestRejectsAnInvertedSalesWindow(t *testing.T) {
	req := validTicketTypeRequest()
	req.SalesEnd = req.SalesStart.Add(-time.Hour)

	assert.Equal(t, apperr.CodeInvalidDateRange, errCodeOf(t, req.Validate(true)))
}

func TestTicketTypeRequestRejectsAMissingName(t *testing.T) {
	req := validTicketTypeRequest()
	req.Name = " "

	assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate(true)))
}

// --- Ticket type event window (spec 015) ----------------------------------

func TestTicketTypeRequestRequiresAnEventWindow(t *testing.T) {
	for field, mutate := range map[string]func(*event.TicketTypeRequest){
		"event_start": func(r *event.TicketTypeRequest) { r.EventStart = time.Time{} },
		"event_end":   func(r *event.TicketTypeRequest) { r.EventEnd = time.Time{} },
	} {
		t.Run(field, func(t *testing.T) {
			req := validTicketTypeRequest()
			mutate(&req)
			assert.Equal(t, apperr.CodeValidation, errCodeOf(t, req.Validate(true)))
		})
	}
}

func TestTicketTypeRequestRejectsAnInvertedEventWindow(t *testing.T) {
	req := validTicketTypeRequest()
	req.EventEnd = req.EventStart.Add(-time.Hour)

	assert.Equal(t, apperr.CodeInvalidDateRange, errCodeOf(t, req.Validate(true)))
}

// Equal endpoints are deliberately legal, matching the rule an event's own dates
// already follow (TestEventRequestAllowsASingleInstantEvent). Migration 0014
// backfills every pre-existing ticket type from its parent event, so a stricter
// rule here would have failed that backfill on any zero-length event.
func TestTicketTypeRequestAllowsASingleInstantEventWindow(t *testing.T) {
	req := validTicketTypeRequest()
	req.EventEnd = req.EventStart

	require.NoError(t, req.Validate(true))
}

// The two windows are independent in both directions (FR-003): a ticket may stay
// on sale after the day it admits to, and may go on sale long before it.
func TestTicketTypeRequestLeavesTheSalesAndEventWindowsUnrelated(t *testing.T) {
	req := validTicketTypeRequest()
	req.EventStart = time.Now().Add(24 * time.Hour)
	req.EventEnd = time.Now().Add(48 * time.Hour)
	req.SalesStart = time.Now().Add(72 * time.Hour)
	req.SalesEnd = time.Now().Add(96 * time.Hour)

	require.NoError(t, req.Validate(true), "sales entirely after the event window is not this layer's business")
}

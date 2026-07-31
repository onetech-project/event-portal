package ticket_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/internal/ticket"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func newPublicTicketAPI(t *testing.T) (*echo.Echo, ticketFixture) {
	t.Helper()
	f := newTicketFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	ticket.NewHandler(newTicketService(t, f)).RegisterPublicRoutes(e.Group("/api/v1"))
	return e, f
}

func getPath(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestPublicLookupReturnsExactlyTheContractFields(t *testing.T) {
	e, f := newPublicTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "LOOK234ABC", f.orderID, f.attendee, "ACTIVE")

	rec := getPath(t, e, "/api/v1/tickets/LOOK234ABC")

	require.Equal(t, http.StatusOK, rec.Code)
	body := objectOf(t, rec)

	assert.ElementsMatch(t,
		[]string{"ticket_code", "status", "event_name", "attendee_name"},
		keysOfObject(body),
		"this is the complete response body per contracts/api.md")
	assert.Equal(t, "LOOK234ABC", body["ticket_code"])
	assert.Equal(t, "ACTIVE", body["status"])
	assert.Equal(t, "Budi Santoso", body["attendee_name"])
}

// FR-016: the endpoint is unauthenticated, so it may disclose only what the code
// holder already knows.
func TestPublicLookupNeverLeaksEmailsOrOrderData(t *testing.T) {
	e, f := newPublicTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "PRIV234ABC", f.orderID, f.attendee, "ACTIVE")

	rec := getPath(t, e, "/api/v1/tickets/PRIV234ABC")

	raw := rec.Body.String()
	assert.NotContains(t, raw, "budi@example.com", "the attendee email must not be disclosed")
	assert.NotContains(t, raw, "ORD-TICKETS", "the order number must not be disclosed")
	assert.NotContains(t, raw, "qr_code_url")
	assert.NotContains(t, raw, "price")
}

func TestPublicLookupNormalizesTheCode(t *testing.T) {
	e, f := newPublicTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "CASE234ABC", f.orderID, f.attendee, "USED")

	rec := getPath(t, e, "/api/v1/tickets/case234abc")

	require.Equal(t, http.StatusOK, rec.Code)
	body := objectOf(t, rec)
	assert.Equal(t, "CASE234ABC", body["ticket_code"])
	assert.Equal(t, "USED", body["status"])
}

func TestPublicLookupReturns404ForAnUnknownCode(t *testing.T) {
	e, _ := newPublicTicketAPI(t)

	rec := getPath(t, e, "/api/v1/tickets/GHOST234AB")

	require.Equal(t, http.StatusNotFound, rec.Code)

	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, apperr.CodeTicketNotFound, body.ErrorCode)
}

// A malformed code and an unknown one must be indistinguishable, so the endpoint
// cannot be used to probe the code format.
func TestPublicLookupGivesAFlat404ForAMalformedCode(t *testing.T) {
	e, _ := newPublicTicketAPI(t)

	unknown := getPath(t, e, "/api/v1/tickets/GHOST234AB")
	malformed := getPath(t, e, "/api/v1/tickets/!!!short")

	assert.Equal(t, unknown.Code, malformed.Code)
	assert.JSONEq(t, unknown.Body.String(), malformed.Body.String())
}

func TestPublicLookupNeedsNoAuthorizationHeader(t *testing.T) {
	e, f := newPublicTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "OPEN234ABC", f.orderID, f.attendee, "ACTIVE")

	rec := getPath(t, e, "/api/v1/tickets/OPEN234ABC")

	assert.Equal(t, http.StatusOK, rec.Code)
}

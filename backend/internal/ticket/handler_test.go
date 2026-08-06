package ticket_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/internal/ticket"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func newTicketAPI(t *testing.T) (*echo.Echo, ticketFixture) {
	t.Helper()
	f := newTicketFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	// Registered without the JWT middleware: authentication has its own tests in
	// the admin package, and mounting it here would only add noise.
	ticket.NewHandler(newTicketService(t, f)).RegisterAdminRoutes(e.Group("/api/v1"))
	return e, f
}

func post(t *testing.T, e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func objectOf(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	return body
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Code
}

// --- Validate -------------------------------------------------------------

func TestValidateEndpointReturnsTheContractShapeForAValidTicket(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "DOOR234ABC", f.orderID, f.attendee, "ACTIVE")

	rec := post(t, e, "/api/v1/admin/tickets/validate", `{"code":"DOOR234ABC"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	body := objectOf(t, rec)

	assert.ElementsMatch(t,
		[]string{"result", "ticket_code", "attendee_name", "ticket_type_name", "event_name"},
		keysOfObject(body))
	assert.Equal(t, "VALID", body["result"])
	assert.Equal(t, "DOOR234ABC", body["ticket_code"])
	assert.Equal(t, "Budi Santoso", body["attendee_name"])
	assert.Equal(t, "Regular", body["ticket_type_name"])
}

func TestValidateEndpointReportsAnAlreadyUsedTicket(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "SPENT234AB", f.orderID, f.attendee, "USED")

	rec := post(t, e, "/api/v1/admin/tickets/validate", `{"code":"SPENT234AB"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "ALREADY_USED", objectOf(t, rec)["result"])
}

// An unknown code is a normal answer at a door, so it is a 200 with an INVALID
// result rather than a 404 the scanner UI would have to special-case.
func TestValidateEndpointReturns200WithInvalidForAnUnknownCode(t *testing.T) {
	e, _ := newTicketAPI(t)

	rec := post(t, e, "/api/v1/admin/tickets/validate", `{"code":"GHOST234AB"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	body := objectOf(t, rec)
	assert.Equal(t, "INVALID", body["result"])
	assert.Nil(t, body["attendee_name"], "an unknown code must disclose nothing")
	assert.Nil(t, body["event_name"])
}

func TestValidateEndpointAcceptsALowercasePaddedCode(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "SCAN234ABC", f.orderID, f.attendee, "ACTIVE")

	rec := post(t, e, "/api/v1/admin/tickets/validate", `{"code":"  scan234abc \n"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	body := objectOf(t, rec)
	assert.Equal(t, "VALID", body["result"])
	assert.Equal(t, "SCAN234ABC", body["ticket_code"])
}

func TestValidateEndpointRejectsAMissingCode(t *testing.T) {
	e, _ := newTicketAPI(t)

	rec := post(t, e, "/api/v1/admin/tickets/validate", `{}`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestValidateEndpointRejectsMalformedJSON(t *testing.T) {
	e, _ := newTicketAPI(t)

	rec := post(t, e, "/api/v1/admin/tickets/validate", `{"code":`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Scanning the same code repeatedly must never consume it.
func TestValidateEndpointIsSideEffectFree(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "REPEAT234A", f.orderID, f.attendee, "ACTIVE")

	for range 4 {
		rec := post(t, e, "/api/v1/admin/tickets/validate", `{"code":"REPEAT234A"}`)
		require.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "VALID", objectOf(t, rec)["result"])
	}
}

// --- Mark used ------------------------------------------------------------

func TestMarkUsedEndpointReturns200AndTheNewStatus(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "ENTER234AB", f.orderID, f.attendee, "ACTIVE")

	rec := post(t, e, "/api/v1/admin/tickets/ENTER234AB/use", "")

	require.Equal(t, http.StatusOK, rec.Code)
	body := objectOf(t, rec)
	assert.ElementsMatch(t, []string{"ticket_code", "status"}, keysOfObject(body))
	assert.Equal(t, "ENTER234AB", body["ticket_code"])
	assert.Equal(t, "USED", body["status"])
}

func TestMarkUsedEndpointNormalizesThePathSegment(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "LOWER234AB", f.orderID, f.attendee, "ACTIVE")

	rec := post(t, e, "/api/v1/admin/tickets/lower234ab/use", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "LOWER234AB", objectOf(t, rec)["ticket_code"])
}

func TestMarkUsedEndpointReturns409ForAnAlreadyUsedTicket(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "AGAIN234AB", f.orderID, f.attendee, "USED")

	rec := post(t, e, "/api/v1/admin/tickets/AGAIN234AB/use", "")

	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeAlreadyUsed), errorCode(t, rec))
}

func TestMarkUsedEndpointReturns409ForARevokedTicket(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "VOID234ABC", f.orderID, f.attendee, "REVOKED")

	rec := post(t, e, "/api/v1/admin/tickets/VOID234ABC/use", "")

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestMarkUsedEndpointReturns404ForAnUnknownCode(t *testing.T) {
	e, _ := newTicketAPI(t)

	rec := post(t, e, "/api/v1/admin/tickets/GHOST234AB/use", "")

	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeTicketNotFound), errorCode(t, rec))
}

// The full door loop: validate, admit, re-validate.
func TestValidateThenMarkUsedThenRevalidate(t *testing.T) {
	e, f := newTicketAPI(t)
	testsupport.SeedTicket(t, f.pool, "LOOP234ABC", f.orderID, f.attendee, "ACTIVE")

	before := post(t, e, "/api/v1/admin/tickets/validate", `{"code":"LOOP234ABC"}`)
	assert.Equal(t, "VALID", objectOf(t, before)["result"])

	used := post(t, e, "/api/v1/admin/tickets/LOOP234ABC/use", "")
	require.Equal(t, http.StatusOK, used.Code)

	after := post(t, e, "/api/v1/admin/tickets/validate", `{"code":"LOOP234ABC"}`)
	assert.Equal(t, "ALREADY_USED", objectOf(t, after)["result"])

	second := post(t, e, "/api/v1/admin/tickets/LOOP234ABC/use", "")
	assert.Equal(t, http.StatusConflict, second.Code, "marking used is irreversible and one-shot")
}

func keysOfObject(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

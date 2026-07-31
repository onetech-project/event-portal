package order_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func newCheckoutAPI(t *testing.T) (*echo.Echo, checkoutFixture) {
	t.Helper()
	f := newCheckoutFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	order.NewHandler(f.svc, testsupport.DiscardLogger()).RegisterPublicRoutes(e.Group("/api/v1"))
	return e, f
}

func postJSON(t *testing.T, e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func checkoutBody(ticketTypeID fmt.Stringer, quantity int) string {
	attendees := make([]string, 0, quantity)
	for i := range quantity {
		attendees = append(attendees, fmt.Sprintf(
			`{"ticket_type_id":"%s","name":"Attendee %d","email":"a%d@example.com"}`,
			ticketTypeID, i, i))
	}
	return fmt.Sprintf(`{
		"buyer_name":"Budi Santoso",
		"buyer_email":"budi@example.com",
		"buyer_phone":"+628123456789",
		"items":[{"ticket_type_id":"%s","quantity":%d}],
		"attendees":[%s]
	}`, ticketTypeID, quantity, strings.Join(attendees, ","))
}

func TestCheckoutEndpointReturns201WithTheContractShape(t *testing.T) {
	e, f := newCheckoutAPI(t)
	tt := f.seedSellableEvent(t, 10)

	rec := postJSON(t, e, "/api/v1/checkout", checkoutBody(tt.ID, 2))

	require.Equal(t, http.StatusCreated, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	assert.ElementsMatch(t,
		[]string{"order_number", "status", "total_amount", "payment_url"},
		keysOfMap(body))
	assert.Equal(t, "PENDING", body["status"])
	assert.Equal(t, "300000.00", body["total_amount"])
	assert.NotEmpty(t, body["payment_url"], "a 201 is only emitted once the payment URL exists")
}

func TestCheckoutEndpointReturns400ForAnAttendeeCountMismatch(t *testing.T) {
	e, f := newCheckoutAPI(t)
	tt := f.seedSellableEvent(t, 10)

	body := fmt.Sprintf(`{
		"buyer_name":"Budi","buyer_email":"budi@example.com","buyer_phone":"+62812",
		"items":[{"ticket_type_id":"%s","quantity":2}],
		"attendees":[{"ticket_type_id":"%s","name":"Only One","email":"one@example.com"}]
	}`, tt.ID, tt.ID)

	rec := postJSON(t, e, "/api/v1/checkout", body)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apperr.CodeAttendeeCountMismatch, errorCodeOf(t, rec))
}

func TestCheckoutEndpointReturns400ForInsufficientQuota(t *testing.T) {
	e, f := newCheckoutAPI(t)
	tt := f.seedSellableEvent(t, 1)

	rec := postJSON(t, e, "/api/v1/checkout", checkoutBody(tt.ID, 3))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apperr.CodeInsufficientQuota, errorCodeOf(t, rec))
}

// FR-021: the guest is told they may retry, and the server has already released
// the reservation before answering.
func TestCheckoutEndpointReturns502WhenPaymentInitiationFails(t *testing.T) {
	e, f := newCheckoutAPI(t)
	tt := f.seedSellableEvent(t, 10)
	f.gateway.err = errors.New("provider unreachable")

	rec := postJSON(t, e, "/api/v1/checkout", checkoutBody(tt.ID, 2))

	require.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Equal(t, apperr.CodePaymentInitiationFailed, errorCodeOf(t, rec))
	assert.Equal(t, int32(10), testsupport.QuotaOf(t, f.pool, tt.ID))
}

func TestCheckoutEndpointReturns400ForMalformedJSON(t *testing.T) {
	e, _ := newCheckoutAPI(t)

	rec := postJSON(t, e, "/api/v1/checkout", `{"buyer_name":`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apperr.CodeValidation, errorCodeOf(t, rec))
}

func TestCheckoutEndpointReturns400ForAMalformedTicketTypeID(t *testing.T) {
	e, _ := newCheckoutAPI(t)

	body := `{
		"buyer_name":"Budi","buyer_email":"budi@example.com","buyer_phone":"+62812",
		"items":[{"ticket_type_id":"not-a-uuid","quantity":1}],
		"attendees":[{"ticket_type_id":"not-a-uuid","name":"A","email":"a@example.com"}]
	}`

	rec := postJSON(t, e, "/api/v1/checkout", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// The error body must never echo internal detail back to a guest.
func TestCheckoutErrorBodyOnlyContainsTheContractFields(t *testing.T) {
	e, f := newCheckoutAPI(t)
	tt := f.seedSellableEvent(t, 1)

	rec := postJSON(t, e, "/api/v1/checkout", checkoutBody(tt.ID, 5))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.ElementsMatch(t, []string{"error_code", "message"}, keysOfMap(body))
}

func errorCodeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.ErrorCode
}

func keysOfMap(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

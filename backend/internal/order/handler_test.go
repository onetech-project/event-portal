package order_test

import (
	"encoding/json"
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
	h := order.NewHandler(f.svc, f.public, testsupport.DiscardLogger())
	h.RegisterPublicRoutes(e.Group("/api/v1"))
	// Unlimited here; the per-IP limiter is composition-root wiring.
	h.RegisterBookRoute(e.Group("/api/v1"))
	h.RegisterCheckoutRoutes(e.Group("/api/v1"))
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

// The pre-008 guest surface is gone, not aliased (spec clarification
// 2026-08-05): a client still calling the old paths gets a plain 404, because a
// silent alias would let two generations of clients drift apart unnoticed.
func TestOldGuestPathsAreGone(t *testing.T) {
	e, _ := newCheckoutAPI(t)

	rec := postJSON(t, e, "/api/v1/checkout", `{}`)
	assert.Equal(t, http.StatusNotFound, rec.Code, "POST /checkout was replaced by /ticket/book + /ticket/checkout/:order_id")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/ORD-20260731-ABCDEF", nil)
	get := httptest.NewRecorder()
	e.ServeHTTP(get, req)
	assert.Equal(t, http.StatusNotFound, get.Code, "GET /orders/:orderNumber was replaced by GET /ticket/order/:order_id")
}

func TestBookEndpointReturns400ForMalformedJSON(t *testing.T) {
	e, _ := newCheckoutAPI(t)

	rec := postJSON(t, e, "/api/v1/ticket/book", `{"event_id":`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeValidation), errorCodeOf(t, rec))
}

func TestBookEndpointReturns400ForAMalformedTicketTypeID(t *testing.T) {
	e, f := newCheckoutAPI(t)
	tt := f.seedSellableEvent(t, 10)

	body := fmt.Sprintf(`{
		"event_id":"%s",
		"items":[{"ticket_type_id":"not-a-uuid","quantity":1}]
	}`, tt.EventID)

	rec := postJSON(t, e, "/api/v1/ticket/book", body)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// The error body must never echo internal detail back to a guest.
func TestBookErrorBodyOnlyContainsTheContractFields(t *testing.T) {
	e, f := newCheckoutAPI(t)
	tt := f.seedSellableEvent(t, 1)
	// Authored terms, so the request gets past the 409001 pre-check and fails on
	// quota — the error under test.
	testsupport.SeedEventTerms(t, f.pool, tt.EventID, "<p>terms</p>")

	body := fmt.Sprintf(`{
		"event_id":"%s",
		"items":[{"ticket_type_id":"%s","quantity":5}]
	}`, tt.EventID, tt.ID)

	rec := postJSON(t, e, "/api/v1/ticket/book", body)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &parsed))
	assert.ElementsMatch(t, []string{"code", "message", "data"}, keysOfMap(parsed))
}

func errorCodeOf(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Code
}

func keysOfMap(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

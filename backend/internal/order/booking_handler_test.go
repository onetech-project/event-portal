package order_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// --- T017: booking endpoints -------------------------------------------------

func getPath(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// bookViaAPI seeds a sellable event with terms and books qty tickets through
// the endpoint, returning the response data plus seeded ids.
func bookViaAPI(t *testing.T, e *echo.Echo, f checkoutFixture, qty int) (map[string]any, testsupport.Event, string) {
	t.Helper()
	ev := testsupport.SeedEvent(t, f.pool, "api-bookable", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 10)
	termsID := testsupport.SeedEventTerms(t, f.pool, ev.ID, "<p>terms</p>")

	rec := postJSON(t, e, "/api/v1/ticket/book", fmt.Sprintf(
		`{"event_id":"%s","items":[{"ticket_type_id":"%s","quantity":%d}]}`, ev.ID, tt.ID, qty))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var data map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &data))
	return data, ev, termsID.String()
}

func TestBookEndpointReturns201WithTheContractShape(t *testing.T) {
	e, f := newCheckoutAPI(t)

	data, _, _ := bookViaAPI(t, e, f, 2)

	assert.ElementsMatch(t,
		[]string{"order_id", "status", "total_amount", "expires_at"},
		keysOfMap(data), "contracts/api.md call 5")
	assert.Equal(t, "PENDING", data["status"])
	assert.Equal(t, "300000.00", data["total_amount"])
	assert.NotEmpty(t, data["expires_at"])
}

func TestBookEndpointRefusesAnEventWithoutTermsWith409001(t *testing.T) {
	e, f := newCheckoutAPI(t)
	ev := testsupport.SeedEvent(t, f.pool, "terms-less", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, f.pool, ev.ID, "Regular", "150000.00", 5)

	rec := postJSON(t, e, "/api/v1/ticket/book", fmt.Sprintf(
		`{"event_id":"%s","items":[{"ticket_type_id":"%s","quantity":1}]}`, ev.ID, tt.ID))

	require.Equal(t, http.StatusConflict, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 409001, body.Code)
}

func TestAgreementEndpointStampsTheOrder(t *testing.T) {
	e, f := newCheckoutAPI(t)
	data, _, termsID := bookViaAPI(t, e, f, 1)
	orderID := data["order_id"].(string)

	rec := postJSON(t, e, "/api/v1/ticket/terms-condition/"+orderID,
		fmt.Sprintf(`{"agreed":true,"event_terms_id":"%s"}`, termsID))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"code":200000,"message":"Success","data":null}`, rec.Body.String())
}

func TestAgreementEndpointRejectsAStaleTermsIDWith409002(t *testing.T) {
	e, f := newCheckoutAPI(t)
	data, _, _ := bookViaAPI(t, e, f, 1)
	orderID := data["order_id"].(string)

	rec := postJSON(t, e, "/api/v1/ticket/terms-condition/"+orderID,
		`{"agreed":true,"event_terms_id":"11111111-2222-3333-4444-555555555555"}`)

	require.Equal(t, http.StatusConflict, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 409002, body.Code)
}

func TestTicketOrderEndpointReturnsSlotsAndScreenRoutingFields(t *testing.T) {
	e, f := newCheckoutAPI(t)
	data, _, termsID := bookViaAPI(t, e, f, 2)
	orderID := data["order_id"].(string)

	rec := getPath(t, e, "/api/v1/ticket/order/"+orderID)
	require.Equal(t, http.StatusOK, rec.Code)

	var detail map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &detail))
	assert.ElementsMatch(t,
		[]string{"order_id", "status", "total_amount", "subtotal", "fees",
			"expires_at", "terms_agreed_at",
			"payment_started", "event", "items", "slots", "server_time", "payment"},
		keysOfMap(detail), "spec 011 removed buyer_name/buyer_email from the guest read")

	assert.Equal(t, false, detail["payment_started"], "no gateway involvement at booking")
	assert.Nil(t, detail["terms_agreed_at"], "agreement not yet recorded")
	assert.Nil(t, detail["payment"])

	slots := detail["slots"].([]any)
	require.Len(t, slots, 2)
	first := slots[0].(map[string]any)
	assert.Equal(t, "Regular", first["ticket_type_name"])
	assert.Nil(t, first["name"], "details are null until checkout (Option B)")
	assert.Nil(t, first["package_name"], "standalone slot has no bundle origin")

	// After agreement, the stamp appears on the read.
	postJSON(t, e, "/api/v1/ticket/terms-condition/"+orderID,
		fmt.Sprintf(`{"agreed":true,"event_terms_id":"%s"}`, termsID))
	rec = getPath(t, e, "/api/v1/ticket/order/"+orderID)
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &detail))
	assert.NotNil(t, detail["terms_agreed_at"])
}

func TestTicketOrderEndpointReturns404ForAnUnknownOrder(t *testing.T) {
	e, _ := newCheckoutAPI(t)

	rec := getPath(t, e, "/api/v1/ticket/order/ORD-20260101-NOPE")

	require.Equal(t, http.StatusNotFound, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 404001, body.Code)
}

package event_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func newAdminAPI(t *testing.T) (*echo.Echo, *testsupport.Pool) {
	t.Helper()
	pool := testsupport.RequirePool(t)

	svc := event.NewService(pool, event.NewRepository(pool), order.NewRepository(pool), testsupport.DiscardLogger())

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	// Registered without the JWT middleware here: authentication is covered by the
	// admin package's own tests, and mounting it would only add noise.
	event.NewHandler(svc).RegisterAdminRoutes(e.Group("/api/v1"))
	return e, pool
}

func do(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func eventBody(slug string) string {
	start := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	end := time.Now().Add(31 * 24 * time.Hour).UTC().Format(time.RFC3339)
	return fmt.Sprintf(`{
		"name":"Jazz Night","slug":"%s","description":"An evening of jazz",
		"venue":"Balai Sarbini","address":"Jl. Sudirman",
		"start_date":"%s","end_date":"%s",
		"banner_url":"https://cdn.example.com/b.png","status":"PUBLISHED"
	}`, slug, start, end)
}

func ticketTypeBody(eventID uuid.UUID, quota int) string {
	start := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	end := time.Now().Add(29 * 24 * time.Hour).UTC().Format(time.RFC3339)
	return fmt.Sprintf(`{
		"event_id":"%s","name":"Regular","price":"150000.00","quota":%d,
		"sales_start":"%s","sales_end":"%s"
	}`, eventID, quota, start, end)
}

// ticketTypeBodyWithDescription is the same body carrying the admin-authored
// remark that replaces the booking card's standard non-refundable notice.
func ticketTypeBodyWithDescription(eventID uuid.UUID, quota int, description string) string {
	start := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	end := time.Now().Add(29 * 24 * time.Hour).UTC().Format(time.RFC3339)
	return fmt.Sprintf(`{
		"event_id":"%s","name":"Regular","description":%q,"price":"150000.00","quota":%d,
		"sales_start":"%s","sales_end":"%s"
	}`, eventID, description, quota, start, end)
}

func TestAdminTicketTypeRoundTripsItsDescription(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "jazz-night", "DRAFT")

	rec := do(t, e, http.MethodPost, "/api/v1/admin/ticket-types",
		ticketTypeBodyWithDescription(ev.ID, 42, "Includes entry 10:00-22:00. No re-entry."))

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, "Includes entry 10:00-22:00. No re-entry.",
		decodeObject(t, rec)["description"])
}

func TestAdminTicketTypeStoresABlankDescriptionAsNull(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "jazz-night", "DRAFT")

	// A submitted-but-empty textarea must not become "", or the booking card
	// would render a blank notice instead of the standard wording (FR-042).
	rec := do(t, e, http.MethodPost, "/api/v1/admin/ticket-types",
		ticketTypeBodyWithDescription(ev.ID, 42, "   "))

	require.Equal(t, http.StatusCreated, rec.Code)
	assert.Nil(t, decodeObject(t, rec)["description"])
}

func decodeObject(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	return body
}

func codeFromBody(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Code
}

// --- Events ---------------------------------------------------------------

func TestAdminCreateEventReturns201(t *testing.T) {
	e, _ := newAdminAPI(t)

	rec := do(t, e, http.MethodPost, "/api/v1/admin/events", eventBody("jazz-night"))

	require.Equal(t, http.StatusCreated, rec.Code)
	body := decodeObject(t, rec)
	assert.Equal(t, "jazz-night", body["slug"])
	assert.Equal(t, "PUBLISHED", body["status"])
	assert.NotEmpty(t, body["id"])
}

func TestAdminListEventsReturnsAnArray(t *testing.T) {
	e, pool := newAdminAPI(t)
	testsupport.SeedEvent(t, pool, "one", "DRAFT")
	testsupport.SeedEvent(t, pool, "two", "PUBLISHED")

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	assert.Len(t, body, 2)
}

func TestAdminGetEventIncludesItsTicketTypes(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "detailed", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 42)

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events/"+ev.ID.String(), "")

	require.Equal(t, http.StatusOK, rec.Code)
	body := decodeObject(t, rec)

	types, ok := body["ticket_types"].([]any)
	require.True(t, ok)
	require.Len(t, types, 1)

	first := types[0].(map[string]any)
	assert.ElementsMatch(t,
		[]string{"id", "event_id", "name", "description", "price", "quota", "sold",
			"sales_start", "sales_end"},
		keysOf(first))
	assert.InDelta(t, 42.0, first["quota"], 0.001)
	assert.InDelta(t, 0.0, first["sold"], 0.001)
}

func TestAdminGetEventReturns404ForAnUnknownID(t *testing.T) {
	e, _ := newAdminAPI(t)

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events/"+uuid.New().String(), "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAdminGetEventReturns400ForAMalformedID(t *testing.T) {
	e, _ := newAdminAPI(t)

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events/not-a-uuid", "")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminUpdateEventReturns200(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "editable", "DRAFT")

	rec := do(t, e, http.MethodPut, "/api/v1/admin/events/"+ev.ID.String(), eventBody("editable"))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "Jazz Night", decodeObject(t, rec)["name"])
}

func TestAdminCreateEventReturns400ForADuplicateSlug(t *testing.T) {
	e, pool := newAdminAPI(t)
	testsupport.SeedEvent(t, pool, "taken", "DRAFT")

	rec := do(t, e, http.MethodPost, "/api/v1/admin/events", eventBody("taken"))

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeSlugNotUnique), codeFromBody(t, rec))
}

func TestAdminCreateEventReturns400ForAnInvertedDateRange(t *testing.T) {
	e, _ := newAdminAPI(t)
	body := `{"name":"X","slug":"x","venue":"V","address":"A",
		"start_date":"2026-09-02T10:00:00Z","end_date":"2026-09-01T10:00:00Z","status":"DRAFT"}`

	rec := do(t, e, http.MethodPost, "/api/v1/admin/events", body)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeInvalidDateRange), codeFromBody(t, rec))
}

func TestAdminDeleteEventReturns204(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "removable", "DRAFT")
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	rec := do(t, e, http.MethodDelete, "/api/v1/admin/events/"+ev.ID.String(), "")

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestAdminDeleteEventReturns400WhenItHasOrders(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "sold-out-event", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-H1", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 1)

	rec := do(t, e, http.MethodDelete, "/api/v1/admin/events/"+ev.ID.String(), "")

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeEventHasOrders), codeFromBody(t, rec))
}

// --- Ticket types (flat routes per the locked PRD §1.5) -------------------

func TestAdminCreateTicketTypeReturns201WithZeroSold(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "host-event", "PUBLISHED")

	rec := do(t, e, http.MethodPost, "/api/v1/admin/ticket-types", ticketTypeBody(ev.ID, 100))

	require.Equal(t, http.StatusCreated, rec.Code)
	body := decodeObject(t, rec)
	assert.Equal(t, ev.ID.String(), body["event_id"], "event_id is a body field, not a path segment")
	assert.InDelta(t, 100.0, body["quota"], 0.001)
	assert.InDelta(t, 0.0, body["sold"], 0.001)
	assert.Equal(t, "150000.00", body["price"])
}

func TestAdminListTicketTypesRequiresAnEventID(t *testing.T) {
	e, _ := newAdminAPI(t)

	rec := do(t, e, http.MethodGet, "/api/v1/admin/ticket-types", "")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminListTicketTypesRejectsAMalformedEventID(t *testing.T) {
	e, _ := newAdminAPI(t)

	rec := do(t, e, http.MethodGet, "/api/v1/admin/ticket-types?event_id=nope", "")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminListTicketTypesScopesToTheEvent(t *testing.T) {
	e, pool := newAdminAPI(t)
	first := testsupport.SeedEvent(t, pool, "event-a", "PUBLISHED")
	second := testsupport.SeedEvent(t, pool, "event-b", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, first.ID, "A1", "100000.00", 10)
	testsupport.SeedTicketType(t, pool, first.ID, "A2", "200000.00", 10)
	testsupport.SeedTicketType(t, pool, second.ID, "B1", "300000.00", 10)

	rec := do(t, e, http.MethodGet, "/api/v1/admin/ticket-types?event_id="+first.ID.String(), "")

	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	assert.Len(t, body, 2)
}

// PRD §1.5 is LOCKED and lists ticket types as flat routes, never nested.
//
// Echo's :id parameter greedily captures trailing segments, so the nested path is
// absorbed by /admin/events/:id and rejected as a malformed UUID rather than 404.
// Either way the invariant that matters holds: nothing serves ticket-type data
// from a nested path.
func TestNoNestedTicketTypeRouteIsRegistered(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "nested-check", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	rec := do(t, e, http.MethodGet,
		fmt.Sprintf("/api/v1/admin/events/%s/ticket-types", ev.ID), "")

	assert.GreaterOrEqual(t, rec.Code, 400, "the nested path must be rejected")
	assert.Less(t, rec.Code, 500)
	assert.NotContains(t, rec.Body.String(), "sales_start",
		"no ticket-type payload may be served from a nested path")
}

func TestAdminUpdateTicketTypeReturns200(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "updatable", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	rec := do(t, e, http.MethodPut, "/api/v1/admin/ticket-types/"+tt.ID.String(),
		ticketTypeBody(ev.ID, 25))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.InDelta(t, 25.0, decodeObject(t, rec)["quota"], 0.001)
	assert.Equal(t, int32(25), testsupport.QuotaOf(t, pool, tt.ID))
}

func TestAdminUpdateTicketTypeReturns400ForANegativeQuota(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "neg-quota", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	rec := do(t, e, http.MethodPut, "/api/v1/admin/ticket-types/"+tt.ID.String(),
		ticketTypeBody(ev.ID, -5))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAdminGetTicketTypeReturns404ForAnUnknownID(t *testing.T) {
	e, _ := newAdminAPI(t)

	rec := do(t, e, http.MethodGet, "/api/v1/admin/ticket-types/"+uuid.New().String(), "")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestAdminDeleteTicketTypeReturns204(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "del-type", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)

	rec := do(t, e, http.MethodDelete, "/api/v1/admin/ticket-types/"+tt.ID.String(), "")

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestAdminDeleteTicketTypeReturns400WhenOrdered(t *testing.T) {
	e, pool := newAdminAPI(t)
	ev := testsupport.SeedEvent(t, pool, "ordered-type", "PUBLISHED")
	tt := testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 10)
	ord := testsupport.SeedOrder(t, pool, "ORD-H2", "PAID")
	testsupport.SeedOrderItem(t, pool, ord.ID, tt.ID, 2)

	rec := do(t, e, http.MethodDelete, "/api/v1/admin/ticket-types/"+tt.ID.String(), "")

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeTicketTypeHasOrders), codeFromBody(t, rec))
}

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
		"sales_start":"%s","sales_end":"%s",
		"event_start":"%s","event_end":"%s"
	}`, eventID, quota, start, end, eventWindowStart(), eventWindowEnd())
}

// eventWindowStart/End sit an hour inside the +30d/+31d span that SeedEvent uses,
// so the containment check (spec 015 FR-005) has room for the clock difference
// between the seeded row and this body.
func eventWindowStart() string {
	return time.Now().Add(30*24*time.Hour + time.Hour).UTC().Format(time.RFC3339)
}

func eventWindowEnd() string {
	return time.Now().Add(31*24*time.Hour - time.Hour).UTC().Format(time.RFC3339)
}

// ticketTypeBodyWithDescription is the same body carrying the admin-authored
// remark that replaces the booking card's standard non-refundable notice.
func ticketTypeBodyWithDescription(eventID uuid.UUID, quota int, description string) string {
	start := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	end := time.Now().Add(29 * 24 * time.Hour).UTC().Format(time.RFC3339)
	return fmt.Sprintf(`{
		"event_id":"%s","name":"Regular","description":%q,"price":"150000.00","quota":%d,
		"sales_start":"%s","sales_end":"%s",
		"event_start":"%s","event_end":"%s"
	}`, eventID, description, quota, start, end, eventWindowStart(), eventWindowEnd())
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

// The admin event list is paginated (spec 021), so `data` is a page object
// rather than a bare array.
type eventPageBody struct {
	Items      []map[string]any `json:"items"`
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"total_pages"`
}

func decodeEventPage(t *testing.T, rec *httptest.ResponseRecorder) eventPageBody {
	t.Helper()
	var body eventPageBody
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	return body
}

func TestAdminListEventsReturnsAPageWithItsTotal(t *testing.T) {
	e, pool := newAdminAPI(t)
	testsupport.SeedEvent(t, pool, "one", "DRAFT")
	testsupport.SeedEvent(t, pool, "two", "PUBLISHED")

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events", "")

	require.Equal(t, http.StatusOK, rec.Code)
	body := decodeEventPage(t, rec)
	assert.Len(t, body.Items, 2)
	assert.EqualValues(t, 2, body.Total)
	assert.Equal(t, 1, body.Page)
	assert.Equal(t, 20, body.PageSize)
	assert.Equal(t, 1, body.TotalPages)
}

func TestAdminListEventsBoundsThePageToWhatWasAsked(t *testing.T) {
	e, pool := newAdminAPI(t)
	for _, slug := range []string{"a", "b", "c"} {
		testsupport.SeedEvent(t, pool, slug, "PUBLISHED")
	}

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events?page=2&page_size=2", "")

	require.Equal(t, http.StatusOK, rec.Code)
	body := decodeEventPage(t, rec)
	assert.Len(t, body.Items, 1, "three events at two per page leaves one on page 2")
	assert.EqualValues(t, 3, body.Total)
	assert.Equal(t, 2, body.TotalPages)
}

func TestAdminListEventsClampsAPageBeyondTheEnd(t *testing.T) {
	// FR-013: a stale bookmark lands on the last page, not on an error and not on
	// an empty table.
	e, pool := newAdminAPI(t)
	for _, slug := range []string{"a", "b", "c"} {
		testsupport.SeedEvent(t, pool, slug, "PUBLISHED")
	}

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events?page=999&page_size=2", "")

	require.Equal(t, http.StatusOK, rec.Code)
	body := decodeEventPage(t, rec)
	assert.Equal(t, 2, body.Page, "the served page is the last one, not the one asked for")
	assert.Len(t, body.Items, 1)
}

func TestAdminListEventsAcceptsAnythingAPersonCouldType(t *testing.T) {
	// SC-007: no page or size value produces an error.
	e, pool := newAdminAPI(t)
	testsupport.SeedEvent(t, pool, "only", "PUBLISHED")

	for _, query := range []string{
		"?page=0", "?page=-3", "?page=abc", "?page=",
		"?page_size=0", "?page_size=abc", "?page_size=-1", "?page_size=5000",
		"?page=abc&page_size=abc",
	} {
		rec := do(t, e, http.MethodGet, "/api/v1/admin/events"+query, "")
		require.Equal(t, http.StatusOK, rec.Code, "query %q", query)

		body := decodeEventPage(t, rec)
		assert.GreaterOrEqual(t, body.Page, 1, "query %q", query)
		assert.LessOrEqual(t, body.PageSize, 100, "query %q must be capped", query)
	}
}

func TestAdminListEventsCapsAnOversizedPage(t *testing.T) {
	e, pool := newAdminAPI(t)
	testsupport.SeedEvent(t, pool, "only", "PUBLISHED")

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events?page_size=5000", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 100, decodeEventPage(t, rec).PageSize)
}

// --- Event options (spec 021) ---------------------------------------------
//
// The selector read behind the event filter on the admin order and attendee
// lists. It exists because those dropdowns cannot be fed from one page of the
// paginated event list without silently losing every event past the first page.

func TestAdminEventOptionsListsEveryEventNotJustAPage(t *testing.T) {
	e, pool := newAdminAPI(t)
	for _, slug := range []string{"a", "b", "c", "d", "e"} {
		testsupport.SeedEvent(t, pool, slug, "PUBLISHED")
	}

	// Even asking for a tiny page must not narrow this read — it does not page.
	rec := do(t, e, http.MethodGet, "/api/v1/admin/events/options?page_size=1&page=3", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	assert.Len(t, body, 5)
}

func TestAdminEventOptionsCarriesOnlyIdAndName(t *testing.T) {
	e, pool := newAdminAPI(t)
	testsupport.SeedEvent(t, pool, "solo", "DRAFT")

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events/options", "")

	require.Equal(t, http.StatusOK, rec.Code)
	var body []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	require.Len(t, body, 1)
	assert.ElementsMatch(t, []string{"id", "name"}, keysOf(body[0]))
}

func TestAdminEventOptionsIsNotShadowedByTheEventIdRoute(t *testing.T) {
	// Route order is load-bearing: registered after /admin/events/:id, "options"
	// parses as an event id and this answers 400 instead of listing anything.
	e, pool := newAdminAPI(t)
	testsupport.SeedEvent(t, pool, "solo", "PUBLISHED")

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events/options", "")

	require.Equal(t, http.StatusOK, rec.Code,
		"a 400 here means /admin/events/:id matched first")
}

func TestAdminEventOptionsReturnsAnEmptyArrayNotNull(t *testing.T) {
	e, _ := newAdminAPI(t)

	rec := do(t, e, http.MethodGet, "/api/v1/admin/events/options", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, string(testsupport.UnwrapData(t, rec.Body.Bytes())))
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
	// is_visible joins the admin projection in spec 022. Admin reads are
	// NOT filtered — containment applies to the purchase path, not to
	// administration (FR-009) — so the admin table can label these types.
	assert.ElementsMatch(t,
		[]string{"id", "event_id", "name", "description", "price", "quota", "sold",
			"sales_start", "sales_end", "event_start", "event_end", "is_visible"},
		keysOf(first))
	assert.Equal(t, true, first["is_visible"],
		"an ordinary ticket type is visible and purchasable by default — this is the "+
			"wire-level guard on the DEFAULT TRUE, and it fails loudly if the column's "+
			"default is ever copied back from the is_registration_only draft")
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

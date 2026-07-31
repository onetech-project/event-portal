package event_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

func newGuestAPI(t *testing.T) (*echo.Echo, *testsupport.Pool) {
	t.Helper()
	pool := testsupport.RequirePool(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(logger.NewWithWriter(&bytes.Buffer{}, logger.LevelError))
	event.NewHandler(event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())).RegisterPublicRoutes(e.Group("/api/v1"))
	return e, pool
}

func get(t *testing.T, e *echo.Echo, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestListEventsEndpointReturnsTheContractShape(t *testing.T) {
	e, pool := newGuestAPI(t)
	testsupport.SeedEvent(t, pool, "rock-fest", "PUBLISHED")
	testsupport.SeedEvent(t, pool, "draft-fest", "DRAFT")

	rec := get(t, e, "/api/v1/events")

	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body, 1)

	assert.ElementsMatch(t,
		[]string{"id", "name", "slug", "venue", "address", "start_date", "end_date", "banner_url"},
		keysOf(body[0]),
		"the list response must match contracts/api.md exactly")
	assert.Equal(t, "rock-fest", body[0]["slug"])
	assert.Equal(t, "https://cdn.example.com/banner.png", body[0]["banner_url"])
}

func TestListEventsReturnsAnEmptyJSONArrayWhenNoneArePublished(t *testing.T) {
	e, _ := newGuestAPI(t)

	rec := get(t, e, "/api/v1/events")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `[]`, rec.Body.String())
}

func TestEventDetailEndpointReturnsTicketTypesWithRemainingQuota(t *testing.T) {
	e, pool := newGuestAPI(t)
	ev := testsupport.SeedEvent(t, pool, "jazz-night", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 42)

	rec := get(t, e, "/api/v1/events/jazz-night")

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	assert.ElementsMatch(t,
		[]string{"id", "name", "slug", "description", "venue", "address",
			"start_date", "end_date", "banner_url", "ticket_types"},
		keysOf(body))

	types := body["ticket_types"].([]any)
	require.Len(t, types, 1)
	first := types[0].(map[string]any)
	assert.ElementsMatch(t,
		[]string{"id", "name", "price", "quota_remaining", "sales_start", "sales_end"},
		keysOf(first))
	assert.Equal(t, "150000.00", first["price"], "price is a decimal string per the contract")
	assert.InDelta(t, 42.0, first["quota_remaining"], 0.001)
}

func TestEventDetailEndpointReturns404ForAnUnpublishedEvent(t *testing.T) {
	e, pool := newGuestAPI(t)
	testsupport.SeedEvent(t, pool, "unreleased", "DRAFT")

	rec := get(t, e, "/api/v1/events/unreleased")

	require.Equal(t, http.StatusNotFound, rec.Code)

	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, apperr.CodeEventNotFound, body.ErrorCode)
}

func TestEventDetailEndpointReturns404ForAnUnknownSlug(t *testing.T) {
	e, _ := newGuestAPI(t)

	rec := get(t, e, "/api/v1/events/who-knows")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// The guest endpoints must never require authentication (Constitution VI).
func TestGuestEndpointsRequireNoAuthorizationHeader(t *testing.T) {
	e, pool := newGuestAPI(t)
	testsupport.SeedEvent(t, pool, "open-to-all", "PUBLISHED")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

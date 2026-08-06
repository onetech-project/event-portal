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

	rec := get(t, e, "/api/v1/event")

	require.Equal(t, http.StatusOK, rec.Code)

	var body []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
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

	rec := get(t, e, "/api/v1/event")

	assert.Equal(t, http.StatusOK, rec.Code)
	// data must be [] rather than null even when nothing is published — the
	// envelope wraps it, the guarantee itself is unchanged.
	assert.JSONEq(t, `{"code":200000,"message":"Success","data":[]}`, rec.Body.String())
}

func TestEventDetailIsContentOnly(t *testing.T) {
	e, pool := newGuestAPI(t)
	ev := testsupport.SeedEvent(t, pool, "jazz-night", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 42)
	testsupport.SeedEventTerms(t, pool, ev.ID, "<ol><li>No refunds.</li></ol>")

	rec := get(t, e, "/api/v1/event/jazz-night")

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))

	// Clarification 2026-08-05: no ticket or package data on the detail —
	// those live behind GET /ticket/:event_id and GET /packages/:event_id.
	assert.ElementsMatch(t,
		[]string{"id", "name", "slug", "description", "venue", "address",
			"start_date", "end_date", "banner_url", "scale",
			"activities", "guest_stars", "guidelines", "has_terms"},
		keysOf(body))
	assert.Equal(t, true, body["has_terms"], "authored terms enable Buy Ticket")
}

func TestTicketEndpointReturnsTypesWithRemainingQuota(t *testing.T) {
	e, pool := newGuestAPI(t)
	ev := testsupport.SeedEvent(t, pool, "jazz-night", "PUBLISHED")
	testsupport.SeedTicketType(t, pool, ev.ID, "Regular", "150000.00", 42)

	rec := get(t, e, "/api/v1/ticket/jazz-night")

	require.Equal(t, http.StatusOK, rec.Code)

	var types []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &types))
	require.Len(t, types, 1)
	first := types[0]
	assert.ElementsMatch(t,
		[]string{"id", "name", "description", "price", "quota_remaining", "sales_start", "sales_end"},
		keysOf(first))
	assert.Nil(t, first["description"],
		"an unset remark is null, so the card falls back to the standard notice")
	assert.Equal(t, "150000.00", first["price"], "price is a decimal string per the contract")
	assert.InDelta(t, 42.0, first["quota_remaining"], 0.001)
}

func TestPackagesEndpointReturnsEmptyArrayWhenNoneExist(t *testing.T) {
	e, pool := newGuestAPI(t)
	testsupport.SeedEvent(t, pool, "jazz-night", "PUBLISHED")

	rec := get(t, e, "/api/v1/packages/jazz-night")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"code":200000,"message":"Success","data":[]}`, rec.Body.String())
}

// --- T013: GET /ticket/terms-condition/:event_id ----------------------------

func TestTermsEndpointReturnsTheAuthoredDocument(t *testing.T) {
	e, pool := newGuestAPI(t)
	ev := testsupport.SeedEvent(t, pool, "with-terms", "PUBLISHED")
	termsID := testsupport.SeedEventTerms(t, pool, ev.ID, "<ol><li>No refunds.</li></ol>")

	rec := get(t, e, "/api/v1/ticket/terms-condition/with-terms")

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	assert.ElementsMatch(t, []string{"id", "content", "updated_at"}, keysOf(body))
	assert.Equal(t, termsID.String(), body["id"],
		"the dialog echoes this id back when recording agreement (409002 guard)")
	assert.Equal(t, "<ol><li>No refunds.</li></ol>", body["content"])
}

func TestTermsEndpointDistinguishesUnauthoredFromUnknownEvent(t *testing.T) {
	e, pool := newGuestAPI(t)
	testsupport.SeedEvent(t, pool, "no-terms-yet", "PUBLISHED")

	// A known event with no authored terms → 404002.
	rec := get(t, e, "/api/v1/ticket/terms-condition/no-terms-yet")
	require.Equal(t, http.StatusNotFound, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 404002, body.Code, "terms-not-authored is its own code")

	// An unknown event → plain 404001, so the dialog can word it differently.
	rec = get(t, e, "/api/v1/ticket/terms-condition/who-knows")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 404001, body.Code)
}

// The static "terms-condition" segment must never be swallowed by
// GET /ticket/:event_id's parameter (routing precedence, T011/T013).
func TestTermsPathIsNotShadowedByTheTicketListingParam(t *testing.T) {
	e, pool := newGuestAPI(t)
	ev := testsupport.SeedEvent(t, pool, "shadow-check", "PUBLISHED")
	testsupport.SeedEventTerms(t, pool, ev.ID, "<p>ok</p>")

	rec := get(t, e, "/api/v1/ticket/terms-condition/shadow-check")

	require.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	assert.Contains(t, body, "content",
		"must resolve to the terms handler, not the ticket-type listing")
}

func TestEventDetailEndpointReturns404ForAnUnpublishedEvent(t *testing.T) {
	e, pool := newGuestAPI(t)
	testsupport.SeedEvent(t, pool, "unreleased", "DRAFT")

	rec := get(t, e, "/api/v1/event/unreleased")

	require.Equal(t, http.StatusNotFound, rec.Code)

	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeEventNotFound), body.Code)
}

func TestEventDetailEndpointReturns404ForAnUnknownSlug(t *testing.T) {
	e, _ := newGuestAPI(t)

	rec := get(t, e, "/api/v1/event/who-knows")

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// The guest endpoints must never require authentication (Constitution VI).
func TestGuestEndpointsRequireNoAuthorizationHeader(t *testing.T) {
	e, pool := newGuestAPI(t)
	testsupport.SeedEvent(t, pool, "open-to-all", "PUBLISHED")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/event", nil)
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

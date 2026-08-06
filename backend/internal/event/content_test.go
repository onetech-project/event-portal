package event_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/event"
	"github.com/manjo/ticketing/backend/internal/event/iconkeys"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// --- T030/T031: admin CMS content surface ------------------------------------

// newAdminContentAPI mounts the admin content routes WITHOUT auth middleware —
// auth is composition-root wiring, exercised in the admin domain's own tests.
func newAdminContentAPI(t *testing.T) (*echo.Echo, *testsupport.Pool) {
	t.Helper()
	pool := testsupport.RequirePool(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(logger.NewWithWriter(&bytes.Buffer{}, logger.LevelError))
	event.NewHandler(event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())).
		RegisterAdminContentRoutes(e.Group("/api/v1"))
	return e, pool
}

func doJSON(t *testing.T, e *echo.Echo, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestActivityCRUDRoundTripsInPositionOrder(t *testing.T) {
	e, pool := newAdminContentAPI(t)
	ev := testsupport.SeedEvent(t, pool, "cms-event", "PUBLISHED")
	base := "/api/v1/admin/events/" + ev.ID.String() + "/activities"

	// Create out of order; the list must come back position-sorted.
	rec := doJSON(t, e, http.MethodPost, base,
		`{"title":"Food Bazaar","description":"Stalls all day.","icon":"utensils","position":2}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = doJSON(t, e, http.MethodPost, base,
		`{"title":"Live Music","description":"Main stage.","icon":"music","position":1}`)
	require.Equal(t, http.StatusCreated, rec.Code)
	var created map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &created))

	rec = doJSON(t, e, http.MethodGet, base, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &list))
	require.Len(t, list, 2)
	assert.Equal(t, "Live Music", list[0]["title"], "position-ordered")
	assert.Equal(t, "Food Bazaar", list[1]["title"])

	// Update, then delete, the second block.
	id := created["id"].(string)
	rec = doJSON(t, e, http.MethodPut, base+"/"+id,
		`{"title":"Acoustic Set","description":"Side stage.","icon":"mic","position":3}`)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = doJSON(t, e, http.MethodDelete, base+"/"+id, "")
	require.Equal(t, http.StatusOK, rec.Code)

	rec = doJSON(t, e, http.MethodGet, base, "")
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &list))
	require.Len(t, list, 1)
	assert.Equal(t, "Food Bazaar", list[0]["title"])
}

func TestUnknownIconKeyIsRefusedWith400004(t *testing.T) {
	e, pool := newAdminContentAPI(t)
	ev := testsupport.SeedEvent(t, pool, "icon-check", "PUBLISHED")

	rec := doJSON(t, e, http.MethodPost,
		"/api/v1/admin/events/"+ev.ID.String()+"/activities",
		`{"title":"X","description":"Y","icon":"definitely-not-real","position":0}`)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 400004, body.Code)
}

// Spec 009: the recognized catalog is the full generated lucide set, so keys
// outside the legacy 20 (here "anchor") must be accepted end-to-end.
func TestNonLegacyCatalogIconKeyIsAccepted(t *testing.T) {
	e, pool := newAdminContentAPI(t)
	ev := testsupport.SeedEvent(t, pool, "icon-catalog", "PUBLISHED")

	rec := doJSON(t, e, http.MethodPost,
		"/api/v1/admin/events/"+ev.ID.String()+"/activities",
		`{"title":"Harbor Tour","description":"By the docks.","icon":"anchor","position":1}`)

	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &created))
	assert.Equal(t, "anchor", created["icon"])
}

// Spec 009: the embedded catalog must be the real generated file (not a stub)
// and keep every legacy key valid so pre-feature rows still render (FR-007).
func TestEmbeddedIconCatalogCoversLegacyKeys(t *testing.T) {
	require.GreaterOrEqual(t, iconkeys.Count(), 1000,
		"icon_keys.txt looks truncated — regenerate with frontend/scripts/generate-icon-keys.mjs")

	legacy := []string{
		"music", "mic", "utensils", "cup-soda", "gamepad-2", "palette",
		"camera", "gift", "star", "heart", "ticket", "map-pin", "clock",
		"info", "shield", "ambulance", "car", "cigarette-off", "id-card", "ban",
	}
	for _, k := range legacy {
		assert.True(t, iconkeys.Valid(k), "legacy key %q missing from catalog", k)
	}
	assert.False(t, iconkeys.Valid("definitely-not-real"))
}

func TestGuestStarAndGuidelineCRUD(t *testing.T) {
	e, pool := newAdminContentAPI(t)
	ev := testsupport.SeedEvent(t, pool, "blocks-event", "PUBLISHED")
	prefix := "/api/v1/admin/events/" + ev.ID.String()

	rec := doJSON(t, e, http.MethodPost, prefix+"/guest-stars",
		`{"name":"DJ Nova","position":1}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = doJSON(t, e, http.MethodPost, prefix+"/guidelines",
		`{"description":"No smoking inside the venue.","icon":"cigarette-off","position":1}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	rec = doJSON(t, e, http.MethodGet, prefix+"/guest-stars", "")
	var stars []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &stars))
	require.Len(t, stars, 1)
	assert.Equal(t, "DJ Nova", stars[0]["name"])

	rec = doJSON(t, e, http.MethodGet, prefix+"/guidelines", "")
	var guides []map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &guides))
	require.Len(t, guides, 1)
	assert.Equal(t, "cigarette-off", guides[0]["icon"])
}

func TestContentRoutesAnswer404ForAnUnknownEvent(t *testing.T) {
	e, _ := newAdminContentAPI(t)

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/admin/events/11111111-2222-3333-4444-555555555555/activities", "")

	require.Equal(t, http.StatusNotFound, rec.Code)
}

// --- T031: terms upsert + sanitize-on-write ----------------------------------

func TestTermsUpsertSanitizesScriptOnWrite(t *testing.T) {
	e, pool := newAdminContentAPI(t)
	ev := testsupport.SeedEvent(t, pool, "terms-admin", "PUBLISHED")
	path := "/api/v1/admin/events/" + ev.ID.String() + "/terms"

	rec := doJSON(t, e, http.MethodPut, path,
		`{"content":"<ol><li>No refunds.</li></ol><script>alert('xss')</script>"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	content := body["content"].(string)
	assert.Contains(t, content, "<ol><li>No refunds.</li></ol>")
	assert.NotContains(t, content, "<script", "XSS stripped ON WRITE — nothing stored needs escaping")

	// The upsert overwrites in place: the id survives an edit.
	firstID := body["id"].(string)
	rec = doJSON(t, e, http.MethodPut, path, `{"content":"<p>Version two.</p>"}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	assert.Equal(t, firstID, body["id"])

	// And the admin read reflects it.
	rec = doJSON(t, e, http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, rec.Code)
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))
	assert.Equal(t, "<p>Version two.</p>", body["content"])
}

func TestTermsReadAnswers404002BeforeAuthoring(t *testing.T) {
	e, pool := newAdminContentAPI(t)
	ev := testsupport.SeedEvent(t, pool, "no-terms-admin", "PUBLISHED")

	rec := doJSON(t, e, http.MethodGet,
		"/api/v1/admin/events/"+ev.ID.String()+"/terms", "")

	require.Equal(t, http.StatusNotFound, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 404002, body.Code)
}

// --- T032: guest detail embeds the content blocks ----------------------------

func TestGuestDetailEmbedsPositionOrderedContentBlocks(t *testing.T) {
	e, pool := newGuestAPI(t)
	ev := testsupport.SeedEvent(t, pool, "full-detail", "PUBLISHED")
	testsupport.SeedEventTerms(t, pool, ev.ID, "<p>terms</p>")

	svc := event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())
	ctx := context.Background()
	_, err := svc.CreateActivity(ctx, ev.ID, event.ActivityRequest{
		Title: "Late Act", Description: "d", Position: 2})
	require.NoError(t, err)
	_, err = svc.CreateActivity(ctx, ev.ID, event.ActivityRequest{
		Title: "Early Act", Description: "d", Position: 1})
	require.NoError(t, err)
	_, err = svc.CreateGuestStar(ctx, ev.ID, event.GuestStarRequest{Name: "DJ Nova", Position: 1})
	require.NoError(t, err)
	_, err = svc.CreateGuideline(ctx, ev.ID, event.GuidelineRequest{
		Description: "No outside food.", Position: 1})
	require.NoError(t, err)

	rec := get(t, e, "/api/v1/event/full-detail")
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Activities []map[string]any `json:"activities"`
		GuestStars []map[string]any `json:"guest_stars"`
		Guidelines []map[string]any `json:"guidelines"`
	}
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))

	require.Len(t, body.Activities, 2)
	assert.Equal(t, "Early Act", body.Activities[0]["title"], "position-ordered")
	require.Len(t, body.GuestStars, 1)
	assert.Equal(t, "DJ Nova", body.GuestStars[0]["name"])
	require.Len(t, body.Guidelines, 1)
}

func TestGuestDetailContentBlocksAreEmptyArraysNotNull(t *testing.T) {
	e, pool := newGuestAPI(t)
	testsupport.SeedEvent(t, pool, "bare-detail", "PUBLISHED")

	rec := get(t, e, "/api/v1/event/bare-detail")
	require.Equal(t, http.StatusOK, rec.Code)

	raw := string(testsupport.UnwrapData(t, rec.Body.Bytes()))
	assert.Contains(t, raw, `"activities":[]`)
	assert.Contains(t, raw, `"guest_stars":[]`)
	assert.Contains(t, raw, `"guidelines":[]`)
}

// T031 also covers the event description: WYSIWYG HTML sanitized on write.
func TestEventDescriptionIsSanitizedOnCreate(t *testing.T) {
	pool := testsupport.RequirePool(t)
	svc := event.NewService(pool, event.NewRepository(pool), nil, testsupport.DiscardLogger())

	dirty := `<p>Great show.</p><script>alert('xss')</script><img src=x onerror=alert(1)>`
	created, err := svc.CreateEvent(context.Background(), event.EventRequest{
		Name: "Sanitized Fest", Slug: "sanitized-fest",
		Description: &dirty,
		Venue:       "Hall", Address: "Street 1",
		StartDate: time.Now().Add(24 * time.Hour),
		EndDate:   time.Now().Add(48 * time.Hour),
		Status:    "DRAFT",
	})
	require.NoError(t, err)
	require.NotNil(t, created.Description)
	assert.Contains(t, *created.Description, "<p>Great show.</p>")
	assert.NotContains(t, *created.Description, "<script")
	assert.NotContains(t, *created.Description, "onerror")
}

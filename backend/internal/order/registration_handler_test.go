package order_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// The registration endpoints, mounted WITHOUT the per-IP limiter — that is
// composition-root wiring, and throttling is exercised by its own suite.
func newRegistrationAPI(t *testing.T, quota int32) (*echo.Echo, registrationFixture) {
	t.Helper()
	f := newRegistrationFixture(t, quota)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	h := order.NewHandler(f.svc, f.public, testsupport.DiscardLogger())
	h.RegisterRegistrationReadRoute(e.Group("/api/v1"))
	h.RegisterRegistrationRoute(e.Group("/api/v1"))
	return e, f
}

// gender_id, not gender: the wire submits the master entry's IDENTIFIER since
// 2026-08-24 (spec 022 FR-022). Raw JSON on purpose — this is the level at which a
// DTO tag and the wire could drift apart unnoticed.
func registrationBody(f registrationFixture) string {
	return fmt.Sprintf(`{
		"slug":"%s",
		"name":"Halo Registrant",
		"email":"halo@example.com",
		"phone":"628125567820",
		"dob":"1996-04-12",
		"gender_id":%d,
		"agreed":true,
		"event_terms_updated_at":"%s"
	}`, f.event.Slug, f.maleGenderID, f.updatedAt.UTC().Format(time.RFC3339Nano))
}

// Spec 022 T050 / FR-012: SIX distinct reasons a registration is unavailable, and
// the caller must not be able to tell them apart.
//
// This is asserted at the HTTP layer deliberately, not only at the service layer.
// The service collapses them through refuseRegistration, but the handler adds two
// refusals of its own for a malformed id — and a status, a numeric code and a
// serialised message the service never sees. An oracle re-introduced at this layer
// would be invisible to the service-level test.
//
// What an oracle would give away: whether a UUID names a real ticket type, whether
// it belongs to this event, whether that event is published, and whether a type is
// free or paid — all from an unauthenticated endpoint, by guessing.
func TestRegistrationRefusalsAreIdenticalOverHTTP(t *testing.T) {
	e, f := newRegistrationAPI(t, 5)

	other := testsupport.SeedEvent(t, f.pool, "other-event", "PUBLISHED")
	foreign := testsupport.SeedRegistrationTicketType(t, f.pool, other.ID, "Elsewhere", 5)
	paid := testsupport.SeedTicketType(t, f.pool, f.event.ID, "Regular", "150000.00", 10)

	draft := testsupport.SeedEvent(t, f.pool, "draft-event", "DRAFT")
	unpublished := testsupport.SeedRegistrationTicketType(t, f.pool, draft.ID, "Hidden", 5)

	closed := testsupport.SeedTicketTypeWindow(t, f.pool, f.event.ID, "Closed", 5,
		time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour))
	_, err := f.pool.Exec(t.Context(),
		`UPDATE ticket_types SET is_visible = FALSE WHERE id = $1`, closed.ID)
	require.NoError(t, err)

	for _, c := range []struct {
		name string
		id   string
	}{
		{"an id naming nothing", uuid.New().String()},
		{"a malformed id", "not-a-uuid"},
		{"another event's registration type", foreign.ID.String()},
		{"a purchasable type on this event", paid.ID.String()},
		{"a type on an unpublished event", unpublished.ID.String()},
		{"a type whose window has closed", closed.ID.String()},
	} {
		t.Run(c.name, func(t *testing.T) {
			get := httptest.NewRequest(http.MethodGet,
				"/api/v1/ticket/register/"+c.id+"?slug="+f.event.Slug, nil)
			getRec := httptest.NewRecorder()
			e.ServeHTTP(getRec, get)

			postRec := postJSON(t, e, "/api/v1/ticket/register/"+c.id, registrationBody(f))

			for label, rec := range map[string]*httptest.ResponseRecorder{
				"GET": getRec, "POST": postRec,
			} {
				assert.Equalf(t, http.StatusNotFound, rec.Code, "%s status", label)
				assert.Equalf(t,
					apperr.Numeric(http.StatusNotFound, apperr.CodeTicketTypeNotFound),
					errorCodeOf(t, rec), "%s numeric code", label)
				assert.Equalf(t, registrationRefusalMessage(t, e, f),
					messageOf(t, rec), "%s message", label)
			}
		})
	}
}

// US3 scenario 2: the POST is not protected by the GET. A caller who never loaded
// the form — no prerequisites read, no page — is refused by the submit itself,
// and nothing is written.
func TestRegistrationSubmitRefusesACallerThatSkippedTheForm(t *testing.T) {
	e, f := newRegistrationAPI(t, 5)
	paid := testsupport.SeedTicketType(t, f.pool, f.event.ID, "Regular", "150000.00", 10)

	rec := postJSON(t, e, "/api/v1/ticket/register/"+paid.ID.String(), registrationBody(f))

	require.Equal(t, http.StatusNotFound, rec.Code)
	assert.Zero(t, countRows(t, f.checkoutFixture,
		`SELECT count(*) FROM orders o WHERE EXISTS (SELECT 1 FROM order_items oi
		              JOIN ticket_types tt ON tt.id = oi.ticket_type_id
		              WHERE oi.order_id = o.id AND NOT tt.is_visible)`),
		"a refused submit writes nothing")
	assert.Equal(t, int32(10), quotaOf(t, f.checkoutFixture, paid.ID),
		"and moves no quota")
}

// The refusal message every case must match: whatever a known-bad id produces.
func registrationRefusalMessage(t *testing.T, e *echo.Echo, f registrationFixture) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/ticket/register/"+uuid.New().String()+"?slug="+f.event.Slug, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return messageOf(t, rec)
}

func messageOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Message
}

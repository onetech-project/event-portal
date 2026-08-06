package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/admin"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func newAdminAPI(t *testing.T) (*echo.Echo, *testsupport.Pool) {
	t.Helper()
	pool := testsupport.RequirePool(t)

	issuer := admin.NewTokenIssuer(testSecret, time.Hour)
	svc := admin.NewService(admin.NewRepository(pool), issuer, testsupport.DiscardLogger())

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	admin.NewHandler(svc).RegisterRoutes(e.Group("/api/v1"))
	return e, pool
}

func postLogin(t *testing.T, e *echo.Echo, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/login", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestLoginEndpointReturnsTheContractShape(t *testing.T) {
	e, pool := newAdminAPI(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	rec := postLogin(t, e, `{"email":"admin@example.com","password":"`+adminPassword+`"}`)

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &body))

	assert.ElementsMatch(t, []string{"token", "expires_at"}, keysOf(body))
	assert.NotEmpty(t, body["token"])

	expiresAt, err := time.Parse(time.RFC3339, body["expires_at"].(string))
	require.NoError(t, err)
	assert.True(t, expiresAt.After(time.Now()))
}

func TestLoginEndpointReturns401ForBadCredentials(t *testing.T) {
	e, pool := newAdminAPI(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	rec := postLogin(t, e, `{"email":"admin@example.com","password":"wrong"}`)

	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeInvalidCredentials), body.Code)
}

// The response must never hint at whether the account exists or echo the
// submitted password back.
func TestLoginErrorBodyLeaksNothing(t *testing.T) {
	e, pool := newAdminAPI(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	rec := postLogin(t, e, `{"email":"nobody@example.com","password":"hunter2"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "hunter2")
	assert.NotContains(t, rec.Body.String(), "nobody@example.com")
	assert.NotContains(t, strings.ToLower(rec.Body.String()), "not found")
}

func TestLoginEndpointReturns400ForMalformedJSON(t *testing.T) {
	e, _ := newAdminAPI(t)

	rec := postLogin(t, e, `{"email":`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Login is the one /admin/* route that must be reachable without a token.
func TestLoginEndpointNeedsNoAuthorizationHeader(t *testing.T) {
	e, pool := newAdminAPI(t)
	seedAdmin(t, pool, "admin@example.com", adminPassword)

	rec := postLogin(t, e, `{"email":"admin@example.com","password":"`+adminPassword+`"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

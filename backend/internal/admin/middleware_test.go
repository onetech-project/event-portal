package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/admin"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

func protectedEcho(issuer *admin.TokenIssuer) (*echo.Echo, *uuid.UUID) {
	seen := &uuid.UUID{}
	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(logger.NewWithWriter(&bytes.Buffer{}, logger.LevelError))
	e.GET("/api/v1/admin/thing", func(c echo.Context) error {
		claims := admin.ClaimsFromContext(c)
		if claims != nil {
			*seen = claims.AdminID
		}
		return c.NoContent(http.StatusOK)
	}, admin.RequireAuth(issuer))
	return e, seen
}

func doRequest(e *echo.Echo, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/thing", nil)
	if authHeader != "" {
		req.Header.Set(echo.HeaderAuthorization, authHeader)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestRequireAuthAcceptsValidBearerTokenAndExposesClaims(t *testing.T) {
	issuer := admin.NewTokenIssuer(testSecret, time.Hour)
	id := uuid.New()
	token, _, err := issuer.Issue(id, "admin@example.com")
	require.NoError(t, err)

	e, seen := protectedEcho(issuer)
	rec := doRequest(e, "Bearer "+token)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, id, *seen)
}

func TestRequireAuthRejectsMissingHeader(t *testing.T) {
	e, _ := protectedEcho(admin.NewTokenIssuer(testSecret, time.Hour))

	rec := doRequest(e, "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeUnauthorized), body.Code)
}

func TestRequireAuthRejectsNonBearerScheme(t *testing.T) {
	e, _ := protectedEcho(admin.NewTokenIssuer(testSecret, time.Hour))

	rec := doRequest(e, "Basic YWRtaW46YWRtaW4=")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuthRejectsInvalidToken(t *testing.T) {
	e, _ := protectedEcho(admin.NewTokenIssuer(testSecret, time.Hour))

	rec := doRequest(e, "Bearer not.a.real.token")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestRequireAuthRejectsExpiredToken(t *testing.T) {
	expired, _, err := admin.NewTokenIssuer(testSecret, -time.Hour).Issue(uuid.New(), "a@b.c")
	require.NoError(t, err)

	e, _ := protectedEcho(admin.NewTokenIssuer(testSecret, time.Hour))
	rec := doRequest(e, "Bearer "+expired)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestBearerSchemeIsMatchedCaseInsensitively(t *testing.T) {
	issuer := admin.NewTokenIssuer(testSecret, time.Hour)
	token, _, err := issuer.Issue(uuid.New(), "admin@example.com")
	require.NoError(t, err)

	e, _ := protectedEcho(issuer)
	rec := doRequest(e, "bearer "+token)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestClaimsFromContextIsNilWhenUnauthenticated(t *testing.T) {
	e := echo.New()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/", nil), httptest.NewRecorder())

	assert.Nil(t, admin.ClaimsFromContext(c))
}

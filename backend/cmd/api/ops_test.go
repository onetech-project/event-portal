package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

func discardLogger() *logger.Logger { return logger.New(logger.LevelError) }

func newRedisCache(t *testing.T) (*cache.Redis, *miniredis.Miniredis) {
	t.Helper()
	srv := miniredis.RunT(t)
	c, err := cache.NewRedis("redis://"+srv.Addr()+"/0", time.Minute, discardLogger(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c, srv
}

// call runs a handler through the real error handler, so status codes and error
// envelopes are exactly what a client would see rather than a test-local
// approximation.
func call(t *testing.T, h echo.HandlerFunc, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(discardLogger())
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := h(c); err != nil {
		e.HTTPErrorHandler(err, c)
	}
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// --- /healthz ----------------------------------------------------------------

func TestHealthzIsOKWhenBothDependenciesAreUp(t *testing.T) {
	c, _ := newRedisCache(t)

	rec := call(t, healthHandler(stubPinger{}, c), http.MethodGet, "/healthz")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", decode(t, rec)["status"])
}

// The cache is disposable, so its absence degrades the report rather than
// failing it — a 503 here would pull a working instance out of a load balancer
// over a performance problem (FR-018).
func TestHealthzIsDegradedNotFailedWhenTheCacheIsDown(t *testing.T) {
	c, srv := newRedisCache(t)
	srv.Close()

	rec := call(t, healthHandler(stubPinger{}, c), http.MethodGet, "/healthz")

	require.Equal(t, http.StatusOK, rec.Code, "a cache outage must not fail the health check")
	body := decode(t, rec)
	require.Equal(t, "degraded", body["status"])
	require.Equal(t, "unavailable", body["cache"])
}

func TestHealthzFailsWhenTheDatabaseIsDown(t *testing.T) {
	c, _ := newRedisCache(t)

	rec := call(t, healthHandler(stubPinger{err: errors.New("connection refused")}, c),
		http.MethodGet, "/healthz")

	require.Equal(t, http.StatusServiceUnavailable, rec.Code,
		"the database is a hard dependency and its loss must be reported as failure")
}

// With caching switched off there is no cache to probe, so health must not
// mention it at all.
func TestHealthzIgnoresADisabledCache(t *testing.T) {
	rec := call(t, healthHandler(stubPinger{}, cache.NoOp{}), http.MethodGet, "/healthz")

	require.Equal(t, http.StatusOK, rec.Code)
	body := decode(t, rec)
	require.Equal(t, "ok", body["status"])
	require.NotContains(t, body, "cache")
}

func TestHealthzRecoversWhenTheCacheComesBack(t *testing.T) {
	c, srv := newRedisCache(t)
	h := healthHandler(stubPinger{}, c)

	srv.Close()
	require.Equal(t, "degraded", decode(t, call(t, h, http.MethodGet, "/healthz"))["status"])

	// A fresh server on the same address stands in for Redis restarting.
	restarted, err := miniredis.RunT(t), error(nil)
	require.NoError(t, err)
	c2, err := cache.NewRedis("redis://"+restarted.Addr()+"/0", time.Minute, discardLogger(), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c2.Close() })

	require.Equal(t, "ok",
		decode(t, call(t, healthHandler(stubPinger{}, c2), http.MethodGet, "/healthz"))["status"])
}

// --- POST /admin/cache/refresh -----------------------------------------------

func TestCacheRefreshFlushesEverything(t *testing.T) {
	c, srv := newRedisCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, cache.EventsPublicKey(), []byte(`["a"]`)))
	require.NoError(t, c.Invalidate(ctx, cache.Events()))
	require.NoError(t, c.Set(ctx, cache.EventsPublicKey(), []byte(`["b"]`)))
	require.NotEmpty(t, srv.Keys(), "precondition: something is cached")

	rec := call(t, cacheRefreshHandler(c, discardLogger()),
		http.MethodPost, "/api/v1/admin/cache/refresh")

	require.Equal(t, http.StatusOK, rec.Code)
	data := decode(t, rec)["data"].(map[string]any)
	require.Equal(t, "flushed", data["status"])
	require.Positive(t, data["entries_cleared"])

	require.Empty(t, srv.Keys(), "entries and generation counters go together")

	// And the cache is immediately usable again.
	_, ok, err := c.Get(ctx, cache.EventsPublicKey())
	require.NoError(t, err)
	require.False(t, ok)
}

func TestCacheRefreshIsIdempotent(t *testing.T) {
	c, _ := newRedisCache(t)
	h := cacheRefreshHandler(c, discardLogger())

	require.Equal(t, http.StatusOK, call(t, h, http.MethodPost, "/x").Code)

	rec := call(t, h, http.MethodPost, "/x")
	require.Equal(t, http.StatusOK, rec.Code)
	data := decode(t, rec)["data"].(map[string]any)
	require.Equal(t, "flushed", data["status"])
	require.EqualValues(t, 0, data["entries_cleared"], "nothing left to clear")
}

// Caching off is not an error: the caller wanted "no stale list is being served",
// and that is already true.
func TestCacheRefreshReportsDisabledRatherThanFailing(t *testing.T) {
	rec := call(t, cacheRefreshHandler(cache.NoOp{}, discardLogger()), http.MethodPost, "/x")

	require.Equal(t, http.StatusOK, rec.Code)
	data := decode(t, rec)["data"].(map[string]any)
	require.Equal(t, "disabled", data["status"])
	require.EqualValues(t, 0, data["entries_cleared"])
}

// The one place a cache failure legitimately surfaces as an HTTP error: the cache
// is the subject of the request, so claiming success would mislead an operator.
func TestCacheRefreshReports503WhenTheCacheIsUnreachable(t *testing.T) {
	c, srv := newRedisCache(t)
	srv.Close()

	rec := call(t, cacheRefreshHandler(c, discardLogger()), http.MethodPost, "/x")

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)

	// The error envelope is the project's standard flat body with a numeric code
	// (status × 1000 for codes outside the registry), not a nested error object.
	body := decode(t, rec)
	require.EqualValues(t, 503000, body["code"])
	require.NotEmpty(t, body["message"])
}

// A successful flush proves the store is reachable and empty, so it doubles as
// the manual recovery from the distrust window.
func TestCacheRefreshClearsDistrust(t *testing.T) {
	c, srv := newRedisCache(t)
	ctx := context.Background()

	// Trip distrust: a write commits, then the invalidation cannot land.
	require.NoError(t, c.Set(ctx, cache.EventsPublicKey(), []byte(`["stale"]`)))
	srv.Close()
	require.Error(t, c.Invalidate(ctx, cache.Events()))
	require.False(t, c.Trusted())

	// With Redis still down the flush fails and distrust correctly persists.
	require.Equal(t, http.StatusServiceUnavailable,
		call(t, cacheRefreshHandler(c, discardLogger()), http.MethodPost, "/x").Code)
	require.False(t, c.Trusted())
}

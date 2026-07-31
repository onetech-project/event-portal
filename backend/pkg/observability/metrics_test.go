package observability_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
	"github.com/manjo/ticketing/backend/pkg/observability"
)

func instrumentedEcho() (*echo.Echo, *prometheus.Registry) {
	registry := prometheus.NewRegistry()
	metrics := observability.NewMetrics(registry)

	e := echo.New()
	e.Use(metrics.Middleware())
	e.GET("/api/v1/events", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/api/v1/events/:slug", func(c echo.Context) error {
		if c.Param("slug") == "missing" {
			return echo.NewHTTPError(http.StatusNotFound)
		}
		return c.NoContent(http.StatusOK)
	})
	e.GET("/metrics", metrics.Handler())
	return e, registry
}

func call(e *echo.Echo, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func scrape(t *testing.T, e *echo.Echo) string {
	t.Helper()
	rec := call(e, http.MethodGet, "/metrics")
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func TestRequestsAreCounted(t *testing.T) {
	e, _ := instrumentedEcho()

	call(e, http.MethodGet, "/api/v1/events")
	call(e, http.MethodGet, "/api/v1/events")

	body := scrape(t, e)
	assert.Contains(t, body, "http_requests_total")
	assert.Contains(t, body, `path="/api/v1/events"`)
	assert.Contains(t, body, `method="GET"`)
	assert.Contains(t, body, `status="200"`)
}

// Labelling by the matched route rather than the raw URL is what keeps
// cardinality bounded: /tickets/:code is one series, not one per ticket ever
// looked up.
func TestPathLabelUsesTheRouteTemplateNotTheURL(t *testing.T) {
	e, _ := instrumentedEcho()

	call(e, http.MethodGet, "/api/v1/events/jazz-night")
	call(e, http.MethodGet, "/api/v1/events/rock-fest")

	body := scrape(t, e)
	assert.Contains(t, body, `path="/api/v1/events/:slug"`)
	assert.NotContains(t, body, "jazz-night")
	assert.NotContains(t, body, "rock-fest")
}

func TestErrorStatusesAreRecorded(t *testing.T) {
	e, _ := instrumentedEcho()

	call(e, http.MethodGet, "/api/v1/events/missing")

	assert.Contains(t, scrape(t, e), `status="404"`)
}

// Handlers in this codebase return *apperr.Error, not echo.HTTPError. Recording
// the response status before the error handler has run would file every one of
// them as a 200 and hide the entire error rate.
func TestApplicationErrorsAreRecordedWithTheirRealStatus(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := observability.NewMetrics(registry)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(logger.NewWithWriter(io.Discard, logger.LevelError))
	e.Use(metrics.Middleware())
	e.GET("/api/v1/events/:slug", func(echo.Context) error {
		return apperr.NotFound(apperr.CodeEventNotFound, "Event not found.")
	})
	e.POST("/api/v1/checkout", func(echo.Context) error {
		return apperr.BadRequest(apperr.CodeInsufficientQuota, "Only 1 left.")
	})
	e.GET("/metrics", metrics.Handler())

	call(e, http.MethodGet, "/api/v1/events/missing")
	call(e, http.MethodPost, "/api/v1/checkout")

	body := scrape(t, e)
	assert.Contains(t, body, `path="/api/v1/events/:slug",status="404"`)
	assert.Contains(t, body, `path="/api/v1/checkout",status="400"`)
	assert.NotContains(t, body, `path="/api/v1/events/:slug",status="200"`)
}

func TestDurationHistogramIsExposed(t *testing.T) {
	e, _ := instrumentedEcho()

	call(e, http.MethodGet, "/api/v1/events")

	body := scrape(t, e)
	assert.Contains(t, body, "http_request_duration_seconds_bucket")
	assert.Contains(t, body, "http_request_duration_seconds_sum")
	assert.Contains(t, body, "http_request_duration_seconds_count")
}

func TestInFlightGaugeIsExposed(t *testing.T) {
	e, _ := instrumentedEcho()

	call(e, http.MethodGet, "/api/v1/events")

	assert.Contains(t, scrape(t, e), "http_requests_in_flight")
}

// An unmatched request must not create a series per probed URL, or a scanner
// hitting random paths would blow up the metric's cardinality.
func TestUnmatchedRoutesCollapseIntoOneSeries(t *testing.T) {
	e, _ := instrumentedEcho()

	call(e, http.MethodGet, "/nope/one")
	call(e, http.MethodGet, "/nope/two")

	body := scrape(t, e)
	assert.NotContains(t, body, "/nope/one")
	assert.NotContains(t, body, "/nope/two")
}

// Scraping must not feed itself: /metrics traffic is noise in request stats.
func TestTheMetricsEndpointDoesNotCountItself(t *testing.T) {
	e, _ := instrumentedEcho()

	scrape(t, e)
	body := scrape(t, e)

	assert.NotContains(t, body, `path="/metrics"`)
}

func TestGoRuntimeMetricsAreRegistered(t *testing.T) {
	e, _ := instrumentedEcho()

	body := scrape(t, e)

	// Process and Go collectors give heap, GC, goroutine, and FD stats for free —
	// the first things worth looking at when the service misbehaves.
	assert.Contains(t, body, "go_goroutines")
	assert.Contains(t, body, "go_memstats_alloc_bytes")
}

func TestMetricsAreExposedInPrometheusTextFormat(t *testing.T) {
	e, _ := instrumentedEcho()
	call(e, http.MethodGet, "/api/v1/events")

	body := scrape(t, e)

	assert.True(t, strings.Contains(body, "# HELP http_requests_total"))
	assert.True(t, strings.Contains(body, "# TYPE http_requests_total counter"))
}

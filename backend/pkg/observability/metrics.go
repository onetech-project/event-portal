package observability

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// metricsPath is excluded from request stats: a scrape every 15s would otherwise
// dominate the request rate and skew every latency percentile.
const metricsPath = "/metrics"

// unmatchedRoute is the single label value used for requests that matched no
// route. Recording the raw URL instead would let anyone probing random paths
// create unbounded series.
const unmatchedRoute = "<unmatched>"

// Metrics holds the HTTP instruments and the registry they live in.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

// NewMetrics registers the HTTP instruments plus the Go runtime and process
// collectors on the given registry.
//
// The registry is passed in rather than using the global default so tests get a
// clean one per case and cannot interfere with each other.
func NewMetrics(registry *prometheus.Registry) *Metrics {
	m := &Metrics{
		registry: registry,
		requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total HTTP requests handled, by method, matched route, and status.",
			},
			[]string{"method", "path", "status"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name: "http_request_duration_seconds",
				Help: "HTTP request latency in seconds, by method and matched route.",
				// Buckets skew short: the checkout budget is well under a second,
				// and the top bucket still catches the gateway-bound outliers.
				Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "path"},
		),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "HTTP requests currently being served.",
		}),
	}

	registry.MustRegister(
		m.requests,
		m.duration,
		m.inFlight,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	return m
}

// Middleware records a counter, a latency histogram, and an in-flight gauge for
// every request.
func (m *Metrics) Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if c.Request().URL.Path == metricsPath {
				return next(c)
			}

			m.inFlight.Inc()
			start := time.Now()

			err := next(c)

			m.inFlight.Dec()

			// Read the route AFTER the handler: Echo only resolves it during
			// routing, so reading it earlier yields an empty string.
			route := c.Path()
			if route == "" {
				route = unmatchedRoute
			}

			status := statusOf(c, err)

			method := c.Request().Method
			m.requests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
			m.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())

			return err
		}
	}
}

// statusOf resolves the status a request will actually answer with.
//
// When a handler returns an error, Echo's error handler has not run yet, so the
// response still carries its default 200. Taking that at face value would file
// every rejected checkout and every 404 as a success and hide the error rate
// entirely — so the status is read from the error itself.
func statusOf(c echo.Context, err error) int {
	if err == nil {
		return c.Response().Status
	}

	// Handlers in this codebase return *apperr.Error; middleware and the router
	// return *echo.HTTPError. Both carry the status they intend.
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr.HTTPStatus
	}

	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Code
	}

	// An unrecognized error becomes an opaque 500, matching what the shared error
	// handler will render.
	return http.StatusInternalServerError
}

// Handler serves the Prometheus text exposition format.
func (m *Metrics) Handler() echo.HandlerFunc {
	h := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		Registry: m.registry,
	})
	return echo.WrapHandler(h)
}

package httpx_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

func newEchoWithHandler() *echo.Echo {
	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(logger.NewWithWriter(&bytes.Buffer{}, logger.LevelError))
	return e
}

func serve(t *testing.T, handler echo.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	e := newEchoWithHandler()
	e.GET("/x", handler)

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) apperr.Body {
	t.Helper()
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestAppErrorRendersItsCodeAndStatus(t *testing.T) {
	rec := serve(t, func(echo.Context) error {
		return apperr.BadRequest(apperr.CodeInsufficientQuota, "only 2 left")
	})

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeBody(t, rec)
	assert.Equal(t, 400002, body.Code)
	assert.Equal(t, "only 2 left", body.Message)
}

func TestWrappedAppErrorIsStillUnwrapped(t *testing.T) {
	rec := serve(t, func(echo.Context) error {
		return echo.NewHTTPError(http.StatusTeapot). // decoy, not returned
								SetInternal(apperr.NotFound(apperr.CodeTicketNotFound, "nope"))
	})

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, 404001, decodeBody(t, rec).Code)
}

// An unexpected error must never leak its text to the client.
func TestUnknownErrorBecomesOpaque500(t *testing.T) {
	rec := serve(t, func(echo.Context) error {
		return errors.New("pq: password authentication failed for user \"ticketing\"")
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	body := decodeBody(t, rec)
	assert.Equal(t, 500000, body.Code)
	assert.NotContains(t, rec.Body.String(), "password authentication failed")
}

func TestEchoNotFoundIsRenderedInTheSameShape(t *testing.T) {
	e := newEchoWithHandler()
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/does-not-exist", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, 404001, decodeBody(t, rec).Code)
}

// spec FR-020 / contracts: the rate limiter's 429 must use the same error envelope.
func TestEchoHTTPErrorStatusIsMappedToARateLimitCode(t *testing.T) {
	rec := serve(t, func(echo.Context) error {
		return echo.NewHTTPError(http.StatusTooManyRequests, "rate limit exceeded")
	})

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, 429001, decodeBody(t, rec).Code)
}

// Every rejected or failed request must be linkable to its trace. Logging
// without the request context drops trace_id and breaks the pivot from a Loki
// line to the Tempo trace — which is the whole reason both are running.
func TestErrorLogsCarryTheRequestsTraceID(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled,
	})

	var logs bytes.Buffer
	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(logger.NewWithWriter(&logs, logger.LevelDebug))
	e.GET("/x", func(c echo.Context) error {
		// Simulate the span the tracing middleware would have started.
		req := c.Request()
		c.SetRequest(req.WithContext(trace.ContextWithSpanContext(req.Context(), spanCtx)))
		return apperr.Internal(apperr.CodeInternal, "boom")
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), "4bf92f3577b34da6a3ce929d0e0e4736")
}

func TestResponseIsNotWrittenTwice(t *testing.T) {
	e := newEchoWithHandler()
	e.GET("/x", func(c echo.Context) error {
		_ = c.NoContent(http.StatusNoContent)
		return apperr.Internal(apperr.CodeInternal, "too late")
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Empty(t, rec.Body.String())
}

package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func limitedEcho(rate float64, burst int) *echo.Echo {
	e := newEchoWithHandler()
	g := e.Group("", httpx.RateLimitPerIP(rate, burst, time.Minute))
	g.GET("/tickets/:code", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	return e
}

func requestFrom(e *echo.Echo, ip string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/tickets/ABC234DEFG", nil)
	req.RemoteAddr = ip + ":54321"
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestRequestsWithinTheBurstAreAllowed(t *testing.T) {
	e := limitedEcho(1, 5)

	for i := range 5 {
		rec := requestFrom(e, "203.0.113.10")
		assert.Equal(t, http.StatusOK, rec.Code, "request %d should be allowed", i+1)
	}
}

// FR-020: the public lookup is rate limited so ticket-code enumeration is
// impractical.
func TestExceedingTheBurstReturns429(t *testing.T) {
	e := limitedEcho(1, 3)

	for range 3 {
		require.Equal(t, http.StatusOK, requestFrom(e, "203.0.113.20").Code)
	}

	rec := requestFrom(e, "203.0.113.20")

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Equal(t, apperr.CodeRateLimited, decodeBody(t, rec).ErrorCode)
}

// One abusive client must not lock everyone else out.
func TestTheLimitIsPerClientIP(t *testing.T) {
	e := limitedEcho(1, 2)

	for range 2 {
		require.Equal(t, http.StatusOK, requestFrom(e, "203.0.113.30").Code)
	}
	require.Equal(t, http.StatusTooManyRequests, requestFrom(e, "203.0.113.30").Code)

	assert.Equal(t, http.StatusOK, requestFrom(e, "198.51.100.7").Code,
		"a different client has its own budget")
}

func TestTheRateLimitErrorUsesTheStandardEnvelope(t *testing.T) {
	e := limitedEcho(1, 1)
	require.Equal(t, http.StatusOK, requestFrom(e, "203.0.113.40").Code)

	rec := requestFrom(e, "203.0.113.40")

	body := decodeBody(t, rec)
	assert.Equal(t, apperr.CodeRateLimited, body.ErrorCode)
	assert.NotEmpty(t, body.Message)
}

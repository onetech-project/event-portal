package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.Equal(t, 429001, decodeBody(t, rec).Code)
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
	assert.Equal(t, 429001, body.Code)
	assert.NotEmpty(t, body.Message)
}

// --- Client identity (spec 018 FR-002a, Constitution Principle IX) -----------

// requestFromWithHeader issues a request from a fixed socket address while
// claiming a different origin in X-Forwarded-For.
func requestFromWithHeader(e *echo.Echo, remoteIP, forwardedFor string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/tickets/ABC234DEFG", nil)
	req.RemoteAddr = remoteIP + ":54321"
	req.Header.Set(echo.HeaderXForwardedFor, forwardedFor)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// A per-client limit that any caller can shed by setting a header is not a
// limit. Echo's default RealIP() returns the first X-Forwarded-For value with no
// notion of which hop is trustworthy, so until the composition root installs an
// IPExtractor, one client rotating the header gets a fresh bucket per request —
// and every per-IP throttle in the system is decorative.
//
// Constitution Principle IX: "A per-client throttle MUST key on an identity the
// client cannot forge."
func TestForgedForwardedForCannotMintFreshBuckets(t *testing.T) {
	e := limitedEcho(1, 3)
	e.IPExtractor = echo.ExtractIPDirect()

	// Spend the burst from one socket, all under the same claimed origin.
	for i := range 3 {
		rec := requestFromWithHeader(e, "203.0.113.99", "10.0.0.1")
		require.Equal(t, http.StatusOK, rec.Code, "burst request %d should be allowed", i+1)
	}

	// Same socket, a new claimed origin every time. The bucket must already be
	// empty: the header is the caller's assertion, not the caller's identity.
	for i := range 5 {
		rec := requestFromWithHeader(e, "203.0.113.99", "10.0.0."+string(rune('2'+i)))
		assert.Equal(t, http.StatusTooManyRequests, rec.Code,
			"rotating X-Forwarded-For must not refill the bucket (attempt %d)", i+1)
	}
}

// The same proof without the extractor installed, pinning exactly what the
// default costs. This is the behaviour the composition root must not ship.
func TestEchoDefaultTrustsForwardedForFromAnyone(t *testing.T) {
	e := limitedEcho(1, 3) // no IPExtractor: echo's legacy fallback

	for i := range 10 {
		rec := requestFromWithHeader(e, "203.0.113.99", "10.0.0."+string(rune('1'+i)))
		require.Equal(t, http.StatusOK, rec.Code,
			"documents the defect: request %d bought a fresh bucket with a header", i+1)
	}
}

// --- Disabled surfaces (spec 018 FR-010, Constitution Principle IX) ----------

// A disabled surface refuses nothing, however hard it is driven. SC-004 asks for
// at least 100 consecutive requests without a throttle refusal.
func TestPassThroughRefusesNothing(t *testing.T) {
	e := newEchoWithHandler()
	e.IPExtractor = echo.ExtractIPDirect()
	g := e.Group("", httpx.PassThrough())
	g.GET("/tickets/:code", func(c echo.Context) error { return c.NoContent(http.StatusOK) })

	for i := range 100 {
		rec := requestFrom(e, "203.0.113.10")
		require.Equal(t, http.StatusOK, rec.Code, "request %d must not be throttled", i+1)
	}
}

// R3: substitution, not omission. Echo registers catch-all NotFound routes for
// any group carrying middleware, and cmd/api builds five groups on the same
// /api/v1 prefix — so dropping a disabled surface's group entirely would change
// which chain answers unmatched paths. Throttling on and off must be
// indistinguishable to a request that matches no route.
func TestUnmatchedPathAnswersIdenticallyInBothModes(t *testing.T) {
	build := func(throttled bool) int {
		e := newEchoWithHandler()
		e.IPExtractor = echo.ExtractIPDirect()
		for range 5 { // the five groups cmd/api mounts on one prefix
			mw := httpx.PassThrough()
			if throttled {
				mw = httpx.RateLimitPerIP(1, 5, time.Minute)
			}
			g := e.Group("/api/v1", mw)
			g.GET("/real", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
		}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
		req.RemoteAddr = "203.0.113.10:54321"
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, build(true), build(false),
		"an unmatched path must answer the same with throttling on and off")
}

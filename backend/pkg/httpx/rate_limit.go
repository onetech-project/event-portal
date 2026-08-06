package httpx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"golang.org/x/time/rate"
)

// RateLimitPerIP builds a per-client-IP token-bucket limiter.
//
// The store is in-memory: PRD.md §1.6 puts Redis out of scope, and for a
// single-instance MVP a process-local bucket is the right size of solution. It
// does mean each instance limits independently, which is the accepted trade-off
// documented alongside the scope decision.
//
// rate is the sustained requests per second per IP; burst is the short-term
// allowance above it. Idle buckets are evicted after expiresIn so memory does not
// grow with the number of distinct clients seen.
func RateLimitPerIP(requestsPerSecond float64, burst int, expiresIn time.Duration) echo.MiddlewareFunc {
	return RateLimitBy(func(c echo.Context) string { return c.RealIP() },
		requestsPerSecond, burst, expiresIn)
}

// RateLimitPerBodyField builds a limiter whose buckets are keyed by a string
// field in the request's JSON body rather than by the caller.
//
// Use it when the resource being protected belongs to someone other than the
// caller. The guest ticket-email resend is the case in point: the thing at risk
// is the buyer's inbox, so one bucket per order caps the mail a buyer can receive
// no matter how many callers ask for it — whereas a per-IP limit would let a
// handful of hosts flood one buyer while throttling nobody in particular.
//
// The body is buffered here and restored, so the handler still reads it
// normally. A request whose body is missing, unparseable, or lacks the field
// falls into a shared bucket rather than bypassing the limit.
func RateLimitPerBodyField(field string, requestsPerSecond float64, burst int, expiresIn time.Duration) echo.MiddlewareFunc {
	const contextKey = "httpx.rate_limit.body_field"

	limited := RateLimitBy(func(c echo.Context) string {
		if value, ok := c.Get(contextKey).(string); ok && value != "" {
			return value
		}
		return "\x00missing"
	}, requestsPerSecond, burst, expiresIn)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		inner := limited(next)
		return func(c echo.Context) error {
			c.Set(contextKey, bodyField(c.Request(), field))
			return inner(c)
		}
	}
}

// bodyField extracts a top-level string field from a JSON request body, leaving
// the body readable for the handler.
func bodyField(r *http.Request, field string) string {
	if r.Body == nil {
		return ""
	}
	buf, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(buf))
	if err != nil {
		return ""
	}

	var payload map[string]json.RawMessage
	if json.Unmarshal(buf, &payload) != nil {
		return ""
	}
	var value string
	if json.Unmarshal(payload[field], &value) != nil {
		return ""
	}
	return value
}

// RateLimitBy is the shared token-bucket construction behind the helpers above.
// key decides what a bucket belongs to.
func RateLimitBy(key func(echo.Context) string, requestsPerSecond float64, burst int, expiresIn time.Duration) echo.MiddlewareFunc {
	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      rate.Limit(requestsPerSecond),
				Burst:     burst,
				ExpiresIn: expiresIn,
			},
		),
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return key(c), nil
		},
	})
}

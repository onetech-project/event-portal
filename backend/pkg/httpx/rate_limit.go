package httpx

import (
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

// A body-field-keyed limiter used to live here, mounted on the guest ticket-email
// resend. It is gone (spec 012 FR-021k). Its documented fallback — "a request
// whose body is missing, unparseable, or lacks the field falls into a shared
// bucket" — meant one malformed request from any caller refused every other guest
// for the length of the window, which is a system-wide outage triggerable by a
// single bad client. Per-order keying now happens in the handler via
// httpx.Cooldown, which also reports the remaining time the middleware shape
// could not.

// PassThrough is the middleware a disabled throttle is replaced by.
//
// Constitution Principle IX requires substitution rather than skipping: no
// token-bucket store is allocated, so a disabled surface costs no memory and has
// no code path to a refusal. The middleware still EXISTS, because the group it
// belongs to must still be constructed — echo registers catch-all NotFound
// routes per group carrying middleware, and several groups share the /api/v1
// prefix, so dropping a group would change how unmatched paths are answered
// between throttling modes.
func PassThrough() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc { return next }
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

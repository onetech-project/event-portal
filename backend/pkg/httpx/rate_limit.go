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
	return middleware.RateLimiterWithConfig(middleware.RateLimiterConfig{
		Store: middleware.NewRateLimiterMemoryStoreWithConfig(
			middleware.RateLimiterMemoryStoreConfig{
				Rate:      rate.Limit(requestsPerSecond),
				Burst:     burst,
				ExpiresIn: expiresIn,
			},
		),
		IdentifierExtractor: func(c echo.Context) (string, error) {
			return c.RealIP(), nil
		},
	})
}

package main

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/internal/admin"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/cache"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// The two operational endpoints that belong to the process rather than to any
// domain, alongside /metrics. They live in named constructors rather than inline
// closures so they can be exercised without standing up the whole server.

// pinger is the subset of the pool health checking needs.
type pinger interface {
	Ping(ctx context.Context) error
}

var _ pinger = (*pgxpool.Pool)(nil)

const healthTimeout = 2 * time.Second

// healthHandler reports readiness.
//
// The database is a hard dependency: unreachable means 503, and a load balancer
// should take this instance out. The cache is not — it is a disposable
// accelerator (Constitution Principle VII), so with it gone every read falls back
// to Postgres and the product still works. That is reported as "degraded" on a
// 200, never a 503: pulling a healthy instance out of rotation over a performance
// problem would turn a slowdown into an outage.
func healthHandler(pool pinger, listCache cache.Lists) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), healthTimeout)
		defer cancel()

		if err := pool.Ping(ctx); err != nil {
			return apperr.Wrap(err, http.StatusServiceUnavailable, "DATABASE_UNAVAILABLE",
				"The database is not reachable.")
		}

		if listCache != nil && listCache.Enabled() {
			if err := listCache.Ping(ctx); err != nil {
				return c.JSON(http.StatusOK, map[string]string{
					"status": "degraded",
					"cache":  "unavailable",
				})
			}
		}
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	}
}

// cacheRefreshHandler discards every cached list, forcing the next read of each
// to rebuild from Postgres. The operator escape hatch: for a suspected
// inconsistency, after a manual database correction, or during an incident.
//
// It lives here rather than in a domain handler because the cache is shared
// infrastructure, not any domain's business — the same reason /healthz and
// /metrics are registered here. Putting it in internal/order would also have
// forced that domain to import internal/admin to attribute the flush, which
// Principle II exists to prevent.
func cacheRefreshHandler(listCache cache.Lists, log *logger.Logger) echo.HandlerFunc {
	return func(c echo.Context) error {
		if listCache == nil || !listCache.Enabled() {
			// Not an error. The caller wanted "no stale list is being served",
			// and with caching off that is already true.
			return httpx.Respond(c, http.StatusOK, map[string]any{
				"status":          "disabled",
				"entries_cleared": int64(0),
			})
		}

		cleared, err := listCache.FlushAll(c.Request().Context())
		if err != nil {
			// The one place a cache failure legitimately surfaces as an HTTP
			// error, and it does not contradict the fail-open rule: that rule
			// governs endpoints serving product data. Here the cache IS the
			// subject of the request, and reporting success when nothing was
			// flushed would mislead an operator mid-incident.
			return apperr.Wrap(err, http.StatusServiceUnavailable, "CACHE_UNAVAILABLE",
				"The cache is not reachable, so it could not be refreshed.")
		}

		// This discards state across every instance, so it should be attributable.
		identity := "unknown"
		if claims := admin.ClaimsFromContext(c); claims != nil {
			identity = claims.Email
		}
		log.Info("cache flushed by an administrator",
			"admin", identity, "entries_cleared", cleared)

		return httpx.Respond(c, http.StatusOK, map[string]any{
			"status":          "flushed",
			"entries_cleared": cleared,
		})
	}
}

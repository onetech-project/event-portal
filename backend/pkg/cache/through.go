package cache

import (
	"context"
	"encoding/json"

	"golang.org/x/sync/singleflight"

	"github.com/manjo/ticketing/backend/pkg/db"
)

// loaders collapses concurrent misses for the same key so a cold cache under
// load produces one database query per key per process rather than one per
// waiting request (FR-017). It is process-wide and keyed by the fully resolved
// cache key, which already includes the generation — so a request that arrives
// after an invalidation joins a new flight, never the old one's result.
var loaders singleflight.Group

// Through is the only way a domain should read through the cache. It owns miss
// handling, JSON round-tripping, miss collapsing, metrics, and the fail-open
// rule, so that no domain repeats that logic and no domain can get it subtly
// wrong.
//
// The load function is called with the caller's context and must return the
// value exactly as the uncached path would: Through does not transform it, so
// the cached and uncached responses are the same bytes (FR-005).
//
// A cache failure is never returned to the caller. Every error path here ends in
// "read the database and carry on" — the guest gets their list either way.
func Through[T any](ctx context.Context, c Lists, k Key, load func(context.Context) (T, error)) (T, error) {
	var zero T

	// A cache read from inside a transaction would hold a row lock across a
	// network round-trip. Fail loudly rather than quietly degrading throughput.
	if db.InTransaction(ctx) {
		return zero, ErrInTransaction
	}

	// Disabled, or distrusted after a failed invalidation: behave exactly as the
	// system did before this feature existed.
	if c == nil || !c.Enabled() || !c.Trusted() {
		if c != nil && c.Enabled() {
			metricsOf(c).bypass(k.Family)
		}
		return load(ctx)
	}

	if raw, ok, _ := c.Get(ctx, k); ok {
		var v T
		if err := json.Unmarshal(raw, &v); err == nil {
			return v, nil
		}
		// A stored value we cannot decode is worse than useless — most likely a
		// DTO shape that changed under a still-live key. Fall through to the
		// database rather than serving or trusting it.
		metricsOf(c).failed("decode")
	}

	entryKey := k.EntryPrefix()
	res, err, _ := loaders.Do(entryKey, func() (any, error) {
		v, err := load(ctx)
		if err != nil {
			return nil, err
		}
		// Best-effort write-back. A failure here costs the next reader a query;
		// it must never cost this one their response.
		if raw, mErr := json.Marshal(v); mErr == nil {
			_ = c.Set(ctx, k, raw)
		} else {
			metricsOf(c).failed("encode")
		}
		return v, nil
	})
	if err != nil {
		return zero, err
	}

	typed, ok := res.(T)
	if !ok {
		// Only reachable if two different types ever shared one key, which the
		// closed family registry prevents. Degrade to a direct read.
		return load(ctx)
	}
	return typed, nil
}

// metricsOf digs the metric sink out of a Lists implementation when it has one.
// Keeps Metrics off the interface: NoOp and test fakes should not have to carry
// a field they never use.
func metricsOf(c Lists) *Metrics {
	if r, ok := c.(*Redis); ok {
		return r.metrics
	}
	return nil
}

// InvalidateAfterCommit records scopes to be invalidated once the surrounding
// transaction commits. Outside a transaction it invalidates immediately.
//
// This is what makes the timing rule structural rather than a convention: a
// domain writes the same line whether or not it happens to be inside InTx, and
// a rollback discards the pending invalidation instead of applying it (FR-007).
func InvalidateAfterCommit(ctx context.Context, c Lists, scopes ...Scope) {
	if c == nil || len(scopes) == 0 {
		return
	}
	db.AfterCommit(ctx, func(ctx context.Context) {
		_ = c.Invalidate(ctx, scopes...)
	})
}

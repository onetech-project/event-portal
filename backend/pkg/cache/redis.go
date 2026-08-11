package cache

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"github.com/manjo/ticketing/backend/pkg/db"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// getScript fetches the scope's generation and the entry derived from it in a
// single round-trip. Doing it in two dependent GETs would also work and would
// still clear the latency budget, but a cached read is supposed to be cheap and
// there is no reason to pay two RTTs for it.
//
// A missing generation counter is read as 0 rather than as an error: a cold
// Redis is a cold cache, not a broken one.
var getScript = redis.NewScript(`
local g = redis.call('GET', KEYS[1])
if not g then g = '0' end
return redis.call('GET', KEYS[2] .. g)
`)

const (
	// invalidateRetryDelay is the pause before the single retry of a failed
	// invalidation. Short on purpose: the write has already committed and every
	// millisecond here is a millisecond of provably stale reads.
	invalidateRetryDelay = 50 * time.Millisecond

	// defaultDistrustCooldown is how long cached values are bypassed after an
	// invalidation failed outright. Long enough for a blip to pass, short enough
	// that a recovered Redis starts earning its keep again quickly.
	defaultDistrustCooldown = 30 * time.Second

	// pingTimeout bounds the health probe. /healthz must answer promptly whether
	// or not Redis does.
	pingTimeout = 500 * time.Millisecond

	// unreachableCooldown is how long the cache is skipped entirely after a
	// connection-level failure.
	//
	// Without this, a Redis outage costs EVERY request a failed dial before it
	// falls through to Postgres — measured at ~3.5s per request with the default
	// dial timeout and retry count, which turns "fail open" into an outage of its
	// own. One request pays the timeout and opens the breaker; the rest are free
	// until it closes and a single probe re-tests the store.
	unreachableCooldown = 5 * time.Second
)

// Redis is the go-redis-backed Lists implementation.
//
// Every method is written so that a Redis problem degrades throughput and never
// correctness: reads report a miss, writes are advisory, and a failed
// invalidation trips the distrust window rather than leaving provably stale
// entries in circulation.
type Redis struct {
	client  *redis.Client
	ttl     time.Duration
	log     *logger.Logger
	metrics *Metrics

	// distrustUntil is a unix-nano deadline, zero when trusted. Atomic because
	// it is written from whichever request happened to fail an invalidation and
	// read by every concurrent request.
	distrustUntil atomic.Int64
	cooldown      time.Duration

	// unreachableUntil is the circuit breaker for a store that will not answer.
	// Distinct from distrustUntil on purpose: distrust means "cached entries are
	// provably wrong", this means "the store is down". Both bypass the cache, but
	// conflating them would lose the difference between a correctness problem and
	// a connectivity one — and they recover on very different timescales.
	unreachableUntil atomic.Int64
}

// NewRedis dials the store and returns a cache. A dial failure is NOT an error:
// Principle VII requires the API to start and serve with Redis absent, so the
// caller gets a working cache object that fails open until Redis appears.
func NewRedis(url string, ttl time.Duration, log *logger.Logger, reg prometheus.Registerer) (*Redis, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	// Aggressive on purpose. Every millisecond spent discovering that Redis is
	// unavailable is added to a request that then still has to query Postgres, so
	// a slow failure is worse than no cache at all. Retries are off for the same
	// reason: go-redis defaults to 3, which multiplies the cost of an outage.
	opt.DialTimeout = 300 * time.Millisecond
	opt.ReadTimeout = 300 * time.Millisecond
	opt.WriteTimeout = 300 * time.Millisecond
	opt.MaxRetries = -1 // -1 disables retries; 0 would mean "use the default of 3"

	c := &Redis{
		client:   redis.NewClient(opt),
		ttl:      ttl,
		log:      log,
		metrics:  NewMetrics(reg),
		cooldown: defaultDistrustCooldown,
	}
	return c, nil
}

// Close releases the connection pool.
func (c *Redis) Close() error { return c.client.Close() }

// Enabled reports true: this is a real cache.
func (c *Redis) Enabled() bool { return true }

// Trusted reports whether cached values may be served right now. It is false
// during the distrust window that follows an invalidation failure — the one
// situation where entries are known-wrong and the only correct response is to
// stop reading them (FR-014).
func (c *Redis) Trusted() bool {
	until := c.distrustUntil.Load()
	if until == 0 {
		return true
	}
	if time.Now().UnixNano() >= until {
		// Self-clearing: the first reader past the deadline restores trust.
		if c.distrustUntil.CompareAndSwap(until, 0) {
			c.metrics.setDistrusted(false)
		}
		return true
	}
	return false
}

// reachable reports whether the store should be contacted at all right now.
func (c *Redis) reachable() bool {
	until := c.unreachableUntil.Load()
	if until == 0 {
		return true
	}
	if time.Now().UnixNano() >= until {
		// One request past the deadline re-probes; if the store is still down it
		// pays the timeout and re-opens the breaker for everyone else.
		c.unreachableUntil.CompareAndSwap(until, 0)
		return true
	}
	return false
}

// markUnreachable opens the breaker after a connection-level failure.
func (c *Redis) markUnreachable() {
	c.unreachableUntil.Store(time.Now().Add(unreachableCooldown).UnixNano())
}

func (c *Redis) distrust(reason string, scopes []Scope) {
	c.distrustUntil.Store(time.Now().Add(c.cooldown).UnixNano())
	c.metrics.setDistrusted(true)
	c.metrics.invalidationFailed()
	names := make([]string, 0, len(scopes))
	for _, s := range scopes {
		names = append(names, s.GenerationKey())
	}
	c.log.Error("cache invalidation failed after its write committed; bypassing the cache until it recovers",
		"reason", reason,
		"scopes", names,
		"cooldown", c.cooldown.String(),
	)
}

// Get returns the cached bytes for k, or a miss. It never reports a store
// failure as a caller-visible error: the caller's next move is a database read
// either way, and the failure is counted for operators instead.
func (c *Redis) Get(ctx context.Context, k Key) ([]byte, bool, error) {
	if db.InTransaction(ctx) {
		return nil, false, ErrInTransaction
	}

	if !c.reachable() {
		c.metrics.miss(k.Family)
		return nil, false, nil
	}

	raw, err := getScript.Run(ctx, c.client, []string{k.GenerationKey(), k.EntryPrefix()}).Text()
	switch {
	case err == nil:
		c.metrics.hit(k.Family)
		return []byte(raw), true, nil
	case errors.Is(err, redis.Nil):
		c.metrics.miss(k.Family)
		return nil, false, nil
	default:
		c.metrics.miss(k.Family)
		c.metrics.failed("get")
		c.markUnreachable()
		return nil, false, err
	}
}

// Set stores v under k's current generation with the configured TTL.
//
// Note the generation is read again here rather than carried over from the Get:
// if an invalidation landed in between, this write must go to the NEW key. The
// old one would be orphaned anyway, but writing to it would waste memory and
// briefly mislead anyone reading the keyspace.
func (c *Redis) Set(ctx context.Context, k Key, v []byte) error {
	if db.InTransaction(ctx) {
		return ErrInTransaction
	}

	if !c.reachable() {
		return nil
	}

	gen, err := c.generation(ctx, k.Scope)
	if err != nil {
		c.metrics.failed("set")
		c.markUnreachable()
		return err
	}
	if err := c.client.Set(ctx, k.EntryPrefix()+strconv.FormatInt(gen, 10), v, c.ttl).Err(); err != nil {
		c.metrics.failed("set")
		c.markUnreachable()
		return err
	}
	return nil
}

// generation reads a scope's counter, treating absence as 0.
func (c *Redis) generation(ctx context.Context, s Scope) (int64, error) {
	n, err := c.client.Get(ctx, s.GenerationKey()).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	return n, err
}

// Invalidate bumps every scope's generation, orphaning all entries derived from
// them. One INCR per scope, pipelined — which is what makes it safe to call on
// the hot booking path the instant its transaction commits.
//
// It MUST NOT be called inside a transaction, and says so rather than obliging.
func (c *Redis) Invalidate(ctx context.Context, scopes ...Scope) error {
	if len(scopes) == 0 {
		return nil
	}
	if db.InTransaction(ctx) {
		return ErrInTransaction
	}

	err := c.bump(ctx, scopes)
	if err == nil {
		for _, s := range scopes {
			c.metrics.invalidated(s)
		}
		return nil
	}

	// The write has already committed, so these entries are now wrong. One quick
	// retry, then stop serving from cache entirely rather than serving them.
	time.Sleep(invalidateRetryDelay)
	if retryErr := c.bump(ctx, scopes); retryErr == nil {
		for _, s := range scopes {
			c.metrics.invalidated(s)
		}
		c.log.Warn("cache invalidation succeeded on retry", "first_error", err.Error())
		return nil
	}

	c.metrics.failed("invalidate")
	c.distrust(err.Error(), scopes)
	return err
}

func (c *Redis) bump(ctx context.Context, scopes []Scope) error {
	pipe := c.client.Pipeline()
	for _, s := range scopes {
		pipe.Incr(ctx, s.GenerationKey())
	}
	_, err := pipe.Exec(ctx)
	return err
}

// FlushAll discards every entry AND every generation counter, returning how many
// keys existed beforehand.
//
// Both together, never one or the other: flushing entries alone is harmless, but
// flushing counters alone would reset generations to values whose orphaned
// entries still exist — resurrecting stale data. FLUSHDB avoids the question.
func (c *Redis) FlushAll(ctx context.Context) (int64, error) {
	if db.InTransaction(ctx) {
		return 0, ErrInTransaction
	}

	// Best-effort count; a failure here must not stop the flush.
	n, err := c.client.DBSize(ctx).Result()
	if err != nil {
		n = 0
	}
	if err := c.client.FlushDB(ctx).Err(); err != nil {
		c.metrics.failed("flush")
		return 0, err
	}
	// The store is now reachable and empty, so there is nothing left to distrust
	// and nothing to route around.
	c.distrustUntil.Store(0)
	c.unreachableUntil.Store(0)
	c.metrics.setDistrusted(false)
	return n, nil
}

// Ping reports reachability for /healthz, under its own short timeout so a hung
// Redis cannot hold up the health response.
func (c *Redis) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	return c.client.Ping(ctx).Err()
}

var _ Lists = (*Redis)(nil)

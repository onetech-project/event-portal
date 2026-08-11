package httpx

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Cooldown is a keyed token bucket that reports how long is left before the next
// take will succeed.
//
// It exists because the guest ticket-email resend has to tell the caller when to
// come back, on *both* answers: the acceptance carries the window it has just
// started, the refusal carries what is left of the one already running (spec 012
// FR-021j). Echo's rate-limiter middleware cannot do that — its store answers
// Allow(identifier) (bool, error) and nothing more, and a middleware runs before
// the handler has validated anything, so a request whose key cannot be read has
// already been counted by the time anyone notices (FR-021k).
//
// The store is in-memory for the same reason every other limiter here is: PRD.md
// §1.6 puts Redis out of scope, so each instance limits independently.
type Cooldown struct {
	limit    rate.Limit
	burst    int
	idleFor  time.Duration
	mu       sync.Mutex
	visitors map[string]*visitor
}

type visitor struct {
	limiter *rate.Limiter
	seenAt  time.Time
}

// NewCooldown builds a cooldown allowing requestsPerSecond sustained takes per
// key, with burst as the short-term allowance.
//
// idleFor is how long an untouched key is retained. It MUST be longer than the
// window itself (burst/requestsPerSecond) — evicting a key mid-cooldown forgives
// it silently, which is the one failure this type cannot report. The caller owns
// that invariant; cooldown_test.go asserts it for the values main.go wires.
func NewCooldown(requestsPerSecond float64, burst int, idleFor time.Duration) *Cooldown {
	return &Cooldown{
		limit:    rate.Limit(requestsPerSecond),
		burst:    burst,
		idleFor:  idleFor,
		visitors: make(map[string]*visitor),
	}
}

// Take spends one of key's allowance and reports whether it was there to spend,
// along with how long until the next take will succeed.
//
// retryAfter is meaningful on both answers. Allowed: the wait the caller has just
// created, which is the full window when burst is 1. Refused: what remains of the
// wait already running. It is zero only when another take would succeed
// immediately, which cannot happen right after an allowed take with burst 1.
func (c *Cooldown) Take(key string) (bool, time.Duration) {
	now := time.Now()

	c.mu.Lock()
	c.evictStale(now)

	v, ok := c.visitors[key]
	if !ok {
		v = &visitor{limiter: rate.NewLimiter(c.limit, c.burst)}
		c.visitors[key] = v
	}
	v.seenAt = now
	limiter := v.limiter
	c.mu.Unlock()

	allowed := limiter.AllowN(now, 1)

	// TokensAt is a pure read — no Reserve/Cancel dance, which would restore the
	// token only when no later reservation had intervened. Below one token, the
	// wait for the next is the shortfall divided by the refill rate.
	tokens := limiter.TokensAt(now)
	var retryAfter time.Duration
	if tokens < 1 && c.limit > 0 {
		retryAfter = time.Duration((1 - tokens) / float64(c.limit) * float64(time.Second))
	}

	return allowed, retryAfter
}

// evictStale drops keys nobody has touched for idleFor, so memory does not grow
// with the number of distinct keys ever seen. Callers hold c.mu.
func (c *Cooldown) evictStale(now time.Time) {
	for key, v := range c.visitors {
		if now.Sub(v.seenAt) > c.idleFor {
			delete(c.visitors, key)
		}
	}
}

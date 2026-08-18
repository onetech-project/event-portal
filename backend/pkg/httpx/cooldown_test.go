package httpx_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// The values the composition root wires for the guest ticket-email resend: one
// send per order per minute, no burst, keys retained for three minutes.
const (
	resendRate    = 1.0 / 60.0
	resendBurst   = 1
	resendIdleFor = 3 * time.Minute
)

// FR-021j: the acceptance carries the window it has just started, so the screen
// can begin counting down from a send rather than from a refusal.
func TestAnAllowedTakeReportsTheWindowItStarted(t *testing.T) {
	c := httpx.NewCooldown(resendRate, resendBurst, resendIdleFor)

	allowed, retryAfter := c.Take("ORD-1")

	require.True(t, allowed)
	assert.InDelta(t, time.Minute.Seconds(), retryAfter.Seconds(), 1,
		"a burst of 1 at one per minute leaves a full minute to wait")
}

// FR-021j: the refusal carries what is *left* of the window, not the window. A
// constant equal to the full window would send every guest back to the start of
// a wait they are already part-way through.
func TestARefusedTakeReportsTheRemainder(t *testing.T) {
	// One per second so the remainder moves measurably inside a test.
	c := httpx.NewCooldown(1, 1, time.Minute)
	require.True(t, mustTake(t, c, "ORD-1"))

	_, first := c.Take("ORD-1")
	time.Sleep(30 * time.Millisecond)
	_, second := c.Take("ORD-1")

	assert.Less(t, second, first, "the wait must shrink as the window elapses")
	assert.Positive(t, second)
}

func TestTakesWithinTheWindowAreRefused(t *testing.T) {
	c := httpx.NewCooldown(resendRate, resendBurst, resendIdleFor)
	require.True(t, mustTake(t, c, "ORD-1"))

	allowed, retryAfter := c.Take("ORD-1")

	assert.False(t, allowed)
	assert.Positive(t, retryAfter)
}

// The whole point of keying per order: this is what the withdrawn
// RateLimitPerBodyField got wrong, by dropping every request it could not key
// into one bucket shared by the entire system.
func TestOneKeysCooldownDoesNotReachAnother(t *testing.T) {
	c := httpx.NewCooldown(resendRate, resendBurst, resendIdleFor)
	require.True(t, mustTake(t, c, "ORD-1"))
	require.False(t, mustTake(t, c, "ORD-1"))

	allowed, _ := c.Take("ORD-2")

	assert.True(t, allowed, "a different order has its own window")
}

// Eviction forgives a key, so idleFor MUST outlast the window or a guest is
// handed a free resend by a housekeeping detail. Demonstrated here with an
// idleFor deliberately shorter than the window; main.go wires three minutes
// against a sixty-second window, which is the correct direction.
func TestEvictionForgivesAKeyWhenIdleForIsShorterThanTheWindow(t *testing.T) {
	c := httpx.NewCooldown(resendRate, resendBurst, 20*time.Millisecond)
	require.True(t, mustTake(t, c, "ORD-1"))
	require.False(t, mustTake(t, c, "ORD-1"))

	time.Sleep(40 * time.Millisecond)

	assert.True(t, mustTake(t, c, "ORD-1"),
		"the key was evicted mid-cooldown — the hazard idleFor > window prevents")
}

func TestTheWiredValuesKeepAKeyForLongerThanItsWindow(t *testing.T) {
	window := time.Duration(float64(resendBurst) / resendRate * float64(time.Second))

	assert.Greater(t, resendIdleFor, window,
		"idle eviction must not forgive a cooldown that is still running")
}

func mustTake(t *testing.T, c *httpx.Cooldown, key string) bool {
	t.Helper()
	allowed, _ := c.Take(key)
	return allowed
}

// --- Disabled cooldown (spec 018 FR-010, FR-012) -----------------------------

// With the resend throttle off, every take succeeds and none reports a wait. The
// zero is deliberate: the confirmation screen applies its own short debounce to
// it, which is a send-guard against a held key rather than this limit.
func TestDisabledCooldownAllowsEveryTakeAndNamesNoWait(t *testing.T) {
	c := httpx.NewDisabledCooldown()

	for i := range 100 {
		allowed, retryAfter := c.Take("ORD-1")
		require.True(t, allowed, "take %d must be allowed while the cooldown is off", i+1)
		assert.Zero(t, retryAfter, "a disabled cooldown imposes no wait")
	}
}

// FR-011: nothing accumulates while the throttle is off, so nothing can be
// charged against a caller when it comes back on. A disabled cooldown keeps no
// visitor state at all, which is what makes this true by construction rather
// than by an explicit reset step.
func TestDisabledCooldownAccumulatesNothing(t *testing.T) {
	off := httpx.NewDisabledCooldown()
	for range 50 {
		require.True(t, mustTake(t, off, "ORD-1"))
	}

	// A restart with throttling on builds a fresh cooldown; the earlier traffic
	// left nothing behind that could count against this key.
	on := httpx.NewCooldown(resendRate, resendBurst, resendIdleFor)
	assert.True(t, mustTake(t, on, "ORD-1"),
		"traffic sent while the throttle was off must not be charged afterwards")
}

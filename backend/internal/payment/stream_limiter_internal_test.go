package payment

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In-package because streamLimiter is unexported: the connection cap is an
// implementation detail of the SSE handler, not part of the domain's contract.

// Spec 018 FR-010: with the cap switched off, no stream is refused for being one
// too many. Note what that leaves exposed — this cap is the only bound on
// held-open connections per client, and each held stream also issues a periodic
// database re-read, so with it off both are unbounded.
func TestDisabledStreamLimiterAcceptsEveryConnection(t *testing.T) {
	l := newDisabledStreamLimiter()

	releases := make([]func(), 0, 100)
	for i := range 100 {
		release, ok := l.acquire("203.0.113.10")
		require.True(t, ok, "connection %d must be accepted while the cap is off", i+1)
		releases = append(releases, release)
	}
	for _, r := range releases {
		r() // releasing a no-op must not panic
	}
}

// The cap still binds when it is on, at exactly the configured ceiling.
func TestStreamLimiterRefusesAboveTheConfiguredCeiling(t *testing.T) {
	l := newStreamLimiter(3)

	for i := range 3 {
		_, ok := l.acquire("203.0.113.10")
		require.True(t, ok, "connection %d is within the ceiling", i+1)
	}
	_, ok := l.acquire("203.0.113.10")
	assert.False(t, ok, "the fourth concurrent stream must be refused")
}

// WithStreamCap is how the composition root hands the domain its ceiling
// (Constitution Principle II — the domain does not choose its own limits).
func TestWithStreamCapHonoursBothSwitchAndCeiling(t *testing.T) {
	h := (&Handler{}).WithStreamCap(true, 2)
	require.NotNil(t, h.streams)
	assert.False(t, h.streams.disabled)
	assert.Equal(t, 2, h.streams.limit)

	off := (&Handler{}).WithStreamCap(false, 2)
	assert.True(t, off.streams.disabled)
}

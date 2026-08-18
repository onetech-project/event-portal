package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/config"
)

// setRequired lives in config_test.go — same package, same purpose.

// FR-006: a deployment that configures nothing behaves exactly as the system did
// before any of these settings existed. Each expectation below is the literal
// constant this feature deleted from cmd/api/main.go and internal/payment.
func TestDefaultsReproduceThePreviousConstants(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	require.NoError(t, err)
	th := cfg.Throttle

	assert.True(t, th.Enabled, "throttling must default to on")
	assert.Equal(t, 3*time.Minute, th.IdleTTL, "was rateLimitWindow")

	// 0.33 is NOT one third. Reproducing today's behaviour means carrying the
	// literal across, so this asserts equality rather than a delta.
	assert.Equal(t, 0.33, th.Book.Rate, "was bookRate")
	assert.Equal(t, 5, th.Book.Burst, "was bookBurst")
	assert.Equal(t, 1.0, th.Availability.Rate, "was availabilityRate")
	assert.Equal(t, 10, th.Availability.Burst, "was availabilityBurst")
	assert.Equal(t, 5.0, th.TicketLookup.Rate, "was TICKET_LOOKUP_RATE_LIMIT's default")
	assert.Equal(t, 10, th.TicketLookup.Burst, "was TICKET_LOOKUP_BURST's default")
	assert.Equal(t, 0.2, th.Checkout.Rate, "was gatewayCallRate")
	assert.Equal(t, 3, th.Checkout.Burst, "was gatewayCallBurst")
	assert.Equal(t, 10, th.StatusStream.MaxConns, "was defaultStreamCap")

	// guestResendRate was 1.0/60.0. The window idiom must reproduce it exactly.
	assert.Equal(t, time.Minute, th.Resend.Window)
	assert.Equal(t, 1, th.Resend.Burst)
	assert.InDelta(t, 1.0/60.0, th.Resend.Rate(), 1e-12, "was guestResendRate")

	assert.Empty(t, cfg.TrustedProxyCIDRs,
		"the client address must come from the connection unless a proxy is declared")
}

// FR-009: the precedence between the two switches, stated once in Active().
func TestSwitchPrecedence(t *testing.T) {
	cases := []struct {
		name           string
		master, self   bool
		expectThrottle bool
	}{
		{"both on", true, true, true},
		{"master off wins over surface on", false, true, false},
		{"surface off with master on", true, false, false},
		{"both off", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := config.ThrottlePolicy{Enabled: c.self, Rate: 1, Burst: 1}
			assert.Equal(t, c.expectThrottle, p.Active(c.master))
		})
	}
}

func TestUnsetSwitchesMeanEnabled(t *testing.T) {
	setRequired(t)
	cfg, err := config.Load()
	require.NoError(t, err)

	// The state of every existing deployment on the day this ships.
	assert.True(t, cfg.Throttle.Enabled)
	assert.True(t, cfg.Throttle.Book.Active(cfg.Throttle.Enabled))
	assert.True(t, cfg.Throttle.StatusStream.Active(cfg.Throttle.Enabled))
}

// FR-006a: a deployment that already tuned the one configurable throttle keeps
// the values it set. Silently reverting it is the worst failure available here.
func TestLegacyTicketLookupNamesStillWork(t *testing.T) {
	t.Run("legacy alone is honoured and reported", func(t *testing.T) {
		setRequired(t)
		t.Setenv("TICKET_LOOKUP_RATE_LIMIT", "2.5")
		t.Setenv("TICKET_LOOKUP_BURST", "7")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 2.5, cfg.Throttle.TicketLookup.Rate)
		assert.Equal(t, 7, cfg.Throttle.TicketLookup.Burst)
		assert.Len(t, cfg.DeprecatedAliases, 2, "using a legacy name must be reported, not silent")
	})

	t.Run("new name wins when both are set", func(t *testing.T) {
		setRequired(t)
		t.Setenv("TICKET_LOOKUP_RATE_LIMIT", "2.5")
		t.Setenv("RATE_LIMIT_TICKET_LOOKUP_RATE", "9")

		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, 9.0, cfg.Throttle.TicketLookup.Rate)
		assert.Empty(t, cfg.DeprecatedAliases, "the new name was set, so nothing is deprecated")
	})
}

// FR-013, FR-014, FR-015: every documented failure shape is refused at startup,
// naming the setting.
func TestInvalidConfigurationRefusesStartup(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"unparseable rate", map[string]string{"RATE_LIMIT_BOOK_RATE": "fast"}, "RATE_LIMIT_BOOK_RATE"},
		{"negative rate", map[string]string{"RATE_LIMIT_BOOK_RATE": "-1"}, "must be positive"},
		{"zero rate never refills", map[string]string{"RATE_LIMIT_BOOK_RATE": "0"}, "must be positive"},
		{"zero burst refuses forever", map[string]string{"RATE_LIMIT_BOOK_BURST": "0"}, "at least 1"},
		{"zero stream ceiling", map[string]string{"RATE_LIMIT_STATUS_STREAM_MAX_CONNS": "0"}, "at least 1"},
		{"zero resend window", map[string]string{"RATE_LIMIT_RESEND_WINDOW": "0s"}, "must be positive"},
		{"retention below the longest window", map[string]string{"RATE_LIMIT_IDLE_TTL": "10s"}, "must exceed the longest window"},
		{"bad proxy cidr", map[string]string{"TRUSTED_PROXY_CIDRS": "not-a-cidr"}, "is not a CIDR"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setRequired(t)
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			_, err := config.Load()
			require.Error(t, err, "this configuration must not reach a serving system")
			assert.Contains(t, err.Error(), c.wantErr)
		})
	}
}

// FR-015a: the operator hitting the retention rule is usually raising a burst on
// purpose. Telling them only that they are wrong would block the exact tuning
// User Story 1 exists for, so the rejection names the value that would work.
func TestRetentionRejectionNamesTheFix(t *testing.T) {
	setRequired(t)
	t.Setenv("RATE_LIMIT_BOOK_BURST", "100") // 100/0.33 ≈ 303s, above the 3m default

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "set RATE_LIMIT_IDLE_TTL above",
		"the message must say what to set, not merely that the value is wrong")
}

// A disabled surface's numbers are never read, so refusing to start over them
// would make turning a throttle off harder than leaving it on.
func TestDisabledSurfaceSkipsValidation(t *testing.T) {
	setRequired(t)
	t.Setenv("RATE_LIMIT_BOOK_ENABLED", "false")
	t.Setenv("RATE_LIMIT_BOOK_RATE", "0")
	t.Setenv("RATE_LIMIT_BOOK_BURST", "0")

	_, err := config.Load()
	assert.NoError(t, err)
}

func TestMasterSwitchOffSkipsAllValidation(t *testing.T) {
	setRequired(t)
	t.Setenv("RATE_LIMIT_ENABLED", "false")
	t.Setenv("RATE_LIMIT_BOOK_RATE", "0")
	t.Setenv("RATE_LIMIT_IDLE_TTL", "1ns")

	_, err := config.Load()
	assert.NoError(t, err)
}

// Every problem in one pass. One restart must tell an operator everything that
// is wrong, not the first thing.
func TestAllProblemsReportedTogether(t *testing.T) {
	setRequired(t)
	t.Setenv("RATE_LIMIT_BOOK_RATE", "0")
	t.Setenv("RATE_LIMIT_CHECKOUT_BURST", "0")
	t.Setenv("RATE_LIMIT_STATUS_STREAM_MAX_CONNS", "0")

	_, err := config.Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RATE_LIMIT_BOOK_RATE")
	assert.Contains(t, err.Error(), "RATE_LIMIT_CHECKOUT_BURST")
	assert.Contains(t, err.Error(), "RATE_LIMIT_STATUS_STREAM_MAX_CONNS")
}

// An empty value means the documented default — consistent with every other
// setting this service reads — and must never be read as zero, which is the
// lockout shape.
func TestEmptyValueMeansDefaultNotZero(t *testing.T) {
	setRequired(t)
	t.Setenv("RATE_LIMIT_BOOK_RATE", "")
	t.Setenv("RATE_LIMIT_BOOK_BURST", "")

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Equal(t, 0.33, cfg.Throttle.Book.Rate)
	assert.Equal(t, 5, cfg.Throttle.Book.Burst)
}

// R9: the invariant generalises — retention must clear the longest window among
// every enabled surface, not just the resend's.
func TestRefillWindowMatchesTheShippedValues(t *testing.T) {
	setRequired(t)
	cfg, err := config.Load()
	require.NoError(t, err)

	for _, c := range []struct {
		name string
		p    config.ThrottlePolicy
	}{
		{"book", cfg.Throttle.Book},
		{"availability", cfg.Throttle.Availability},
		{"ticket lookup", cfg.Throttle.TicketLookup},
		{"checkout", cfg.Throttle.Checkout},
	} {
		assert.Less(t, c.p.RefillWindow(), cfg.Throttle.IdleTTL,
			"%s refills in %s, which must stay under the %s retention",
			c.name, c.p.RefillWindow(), cfg.Throttle.IdleTTL)
	}
	assert.Less(t, cfg.Throttle.Resend.Window, cfg.Throttle.IdleTTL)
}

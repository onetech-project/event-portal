package config

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// Throttle configuration (Constitution Principle IX).
//
// Every threshold here was a compiled-in constant until spec 018. The rules the
// principle imposes and this file implements: defaults reproduce the shipped
// values exactly, a master switch plus a per-surface switch with precedence
// stated once, and invalid configuration refuses startup naming the setting —
// and, where one exists, naming the value that would work.

// ThrottlePolicy configures one rate-shaped surface: a sustained allowance in
// requests per second, plus a short-term burst above it.
type ThrottlePolicy struct {
	Enabled bool
	Rate    float64
	Burst   int
}

// Active is the ONLY place the master/per-surface precedence is expressed, so it
// cannot be recombined differently at different call sites (spec 018 FR-009).
func (p ThrottlePolicy) Active(master bool) bool { return master && p.Enabled }

// RefillWindow is how long an emptied bucket takes to refill completely. It is
// the quantity the shared idle-retention must exceed: evicting a key before it
// refills silently forgives a caller who is still meant to be waiting.
func (p ThrottlePolicy) RefillWindow() time.Duration {
	if p.Rate <= 0 {
		return 0
	}
	return time.Duration(float64(p.Burst) / p.Rate * float64(time.Second))
}

// CooldownPolicy configures the guest ticket-email resend.
//
// Rate-shaped underneath, but configured as the wait itself: that duration is
// what a refusal reports and what the confirmation screen counts down, and
// "one per 0.0166 requests per second" is a value no operator can check by eye.
type CooldownPolicy struct {
	Enabled bool
	Window  time.Duration
	Burst   int
}

func (p CooldownPolicy) Active(master bool) bool { return master && p.Enabled }

// Rate converts the window into the requests-per-second the token bucket takes.
// Window=60s, Burst=1 reproduces the previous constant 1.0/60.0 exactly.
func (p CooldownPolicy) Rate() float64 {
	if p.Window <= 0 {
		return 0
	}
	return 1 / p.Window.Seconds()
}

// StreamPolicy configures the live payment-status connection cap.
//
// A ceiling on concurrent held-open connections, not a rate. It gets its own
// type so it can never be handed an allowance and a burst it has no meaning for
// (spec 018 FR-004), and it retains no idle state: the accounting is released
// when the connection closes, so the retention setting does not reach it.
type StreamPolicy struct {
	Enabled  bool
	MaxConns int
}

func (p StreamPolicy) Active(master bool) bool { return master && p.Enabled }

// ThrottleConfig is the whole subsystem, travelling as one value into main.go.
type ThrottleConfig struct {
	// Enabled is the master switch. False disables every surface below
	// regardless of its own switch.
	Enabled bool
	// IdleTTL is how long untouched per-key state is retained. ONE setting
	// shared by every surface that retains state, matching the single shared
	// constant it replaces.
	IdleTTL time.Duration

	Book         ThrottlePolicy
	Availability ThrottlePolicy
	TicketLookup ThrottlePolicy
	Checkout     ThrottlePolicy
	Resend       CooldownPolicy
	StatusStream StreamPolicy
}

// ratePolicies returns the four rate-shaped surfaces with the names their
// settings carry, for validation and for the startup report.
func (t ThrottleConfig) ratePolicies() []struct {
	name   string
	policy ThrottlePolicy
} {
	return []struct {
		name   string
		policy ThrottlePolicy
	}{
		{"RATE_LIMIT_BOOK", t.Book},
		{"RATE_LIMIT_AVAILABILITY", t.Availability},
		{"RATE_LIMIT_TICKET_LOOKUP", t.TicketLookup},
		{"RATE_LIMIT_CHECKOUT", t.Checkout},
	}
}

// throttle reads every throttle setting. Defaults are the constants that were
// compiled into cmd/api/main.go and internal/payment/stream.go before spec 018,
// carried across verbatim — 0.33 is 0.33, not one third.
func (l *loader) throttle() ThrottleConfig {
	return ThrottleConfig{
		Enabled: l.boolean("RATE_LIMIT_ENABLED", true),
		IdleTTL: l.duration("RATE_LIMIT_IDLE_TTL", 3*time.Minute),

		Book: ThrottlePolicy{
			Enabled: l.boolean("RATE_LIMIT_BOOK_ENABLED", true),
			Rate:    l.float("RATE_LIMIT_BOOK_RATE", 0.33),
			Burst:   l.integer("RATE_LIMIT_BOOK_BURST", 5),
		},
		Availability: ThrottlePolicy{
			Enabled: l.boolean("RATE_LIMIT_AVAILABILITY_ENABLED", true),
			Rate:    l.float("RATE_LIMIT_AVAILABILITY_RATE", 1.0),
			Burst:   l.integer("RATE_LIMIT_AVAILABILITY_BURST", 10),
		},
		// The one surface that was already configurable, so its legacy names
		// keep working (FR-006a).
		TicketLookup: ThrottlePolicy{
			Enabled: l.boolean("RATE_LIMIT_TICKET_LOOKUP_ENABLED", true),
			Rate:    l.floatAliased("RATE_LIMIT_TICKET_LOOKUP_RATE", "TICKET_LOOKUP_RATE_LIMIT", 5),
			Burst:   l.integerAliased("RATE_LIMIT_TICKET_LOOKUP_BURST", "TICKET_LOOKUP_BURST", 10),
		},
		Checkout: ThrottlePolicy{
			Enabled: l.boolean("RATE_LIMIT_CHECKOUT_ENABLED", true),
			Rate:    l.float("RATE_LIMIT_CHECKOUT_RATE", 0.2),
			Burst:   l.integer("RATE_LIMIT_CHECKOUT_BURST", 3),
		},
		Resend: CooldownPolicy{
			Enabled: l.boolean("RATE_LIMIT_RESEND_ENABLED", true),
			Window:  l.duration("RATE_LIMIT_RESEND_WINDOW", time.Minute),
			Burst:   l.integer("RATE_LIMIT_RESEND_BURST", 1),
		},
		StatusStream: StreamPolicy{
			Enabled:  l.boolean("RATE_LIMIT_STATUS_STREAM_ENABLED", true),
			MaxConns: l.integer("RATE_LIMIT_STATUS_STREAM_MAX_CONNS", 10),
		},
	}
}

// DeprecatedAliases records legacy setting names a deployment is still using, so
// startup can tell an operator to move without ignoring what they set.
type DeprecatedAlias struct{ Old, New string }

// floatAliased prefers the new name, falls back to the legacy one, and records
// the fallback. Silently ignoring a variable an operator has already set is the
// worst failure this feature can produce: they believe they configured a limit
// and did not.
func (l *loader) floatAliased(key, legacy string, def float64) float64 {
	if os.Getenv(key) == "" && os.Getenv(legacy) != "" {
		l.aliases = append(l.aliases, DeprecatedAlias{Old: legacy, New: key})
		return l.float(legacy, def)
	}
	return l.float(key, def)
}

func (l *loader) integerAliased(key, legacy string, def int) int {
	if os.Getenv(key) == "" && os.Getenv(legacy) != "" {
		l.aliases = append(l.aliases, DeprecatedAlias{Old: legacy, New: key})
		return l.integer(legacy, def)
	}
	return l.integer(key, def)
}

// cidrs parses a comma-separated CIDR list. Empty is the default and means the
// client address comes from the connection alone.
func (l *loader) cidrs(key string) []string {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(part); err != nil {
			l.errs = append(l.errs, fmt.Errorf(
				"%s entry %q is not a CIDR block such as 10.0.0.0/8", key, part))
			continue
		}
		out = append(out, part)
	}
	return out
}

// validateThrottle enforces the rules in specs/018 data-model.md (V1-V7).
//
// Only ENABLED surfaces are checked: refusing to start over a value nobody will
// read would make turning a throttle off harder than leaving it on, which is the
// opposite of what a kill switch is for.
func validateThrottle(t ThrottleConfig, errs *[]error) {
	if !t.Enabled {
		return
	}

	for _, s := range t.ratePolicies() {
		if !s.policy.Enabled {
			continue
		}
		// A bucket that never refills spends its burst and locks the client out
		// until the process restarts.
		if s.policy.Rate <= 0 {
			*errs = append(*errs, fmt.Errorf(
				"%s_RATE must be positive when %s_ENABLED is true, got %g",
				s.name, s.name, s.policy.Rate))
		}
		// x/time/rate grants only when n <= burst, so a burst of zero refuses
		// every request forever regardless of the allowance.
		if s.policy.Burst < 1 {
			*errs = append(*errs, fmt.Errorf(
				"%s_BURST must be at least 1, got %d — a burst of zero refuses every request forever",
				s.name, s.policy.Burst))
		}
	}

	if t.Resend.Enabled {
		if t.Resend.Window <= 0 {
			*errs = append(*errs, fmt.Errorf(
				"RATE_LIMIT_RESEND_WINDOW must be positive when RATE_LIMIT_RESEND_ENABLED is true, got %s",
				t.Resend.Window))
		}
		if t.Resend.Burst < 1 {
			*errs = append(*errs, fmt.Errorf(
				"RATE_LIMIT_RESEND_BURST must be at least 1, got %d", t.Resend.Burst))
		}
	}

	if t.StatusStream.Enabled && t.StatusStream.MaxConns < 1 {
		*errs = append(*errs, fmt.Errorf(
			"RATE_LIMIT_STATUS_STREAM_MAX_CONNS must be at least 1, got %d — a ceiling of zero accepts no stream at all",
			t.StatusStream.MaxConns))
	}

	if t.IdleTTL <= 0 {
		*errs = append(*errs, fmt.Errorf(
			"RATE_LIMIT_IDLE_TTL must be positive, got %s", t.IdleTTL))
		return
	}

	// One retention serves every surface, so it must clear the LONGEST window
	// among them. Shorter, and an exhausted key is evicted before it refills and
	// comes back with a full bucket — the limit appears to work while enforcing
	// nothing, which is the one failure mode none of the checks above can see.
	//
	// The message names the retention that would work, because the operator
	// hitting this is usually raising a burst deliberately (FR-015a).
	longest, worst := time.Duration(0), ""
	for _, s := range t.ratePolicies() {
		if !s.policy.Enabled || s.policy.Rate <= 0 || s.policy.Burst < 1 {
			continue
		}
		if w := s.policy.RefillWindow(); w > longest {
			longest, worst = w, s.name
		}
	}
	if t.Resend.Enabled && t.Resend.Window > longest {
		longest, worst = t.Resend.Window, "RATE_LIMIT_RESEND"
	}
	if longest > 0 && t.IdleTTL <= longest {
		*errs = append(*errs, fmt.Errorf(
			"RATE_LIMIT_IDLE_TTL (%s) must exceed the longest window it retains, "+
				"which is %s's %s — set RATE_LIMIT_IDLE_TTL above %s",
			t.IdleTTL, worst, longest.Round(time.Millisecond), longest.Round(time.Second)))
	}
}

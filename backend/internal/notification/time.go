package notification

import (
	"sync"
	"time"
)

// jakarta is the display zone for every time on every surface: the email body,
// the receipt, and the e-tickets (spec 016 FR-020a, FR-024b).
//
// It is HARDCODED rather than configured, deliberately. The rest of pkg/config
// exists for values an operator changes without an engineer; a display zone is
// not one of those, and a wrong value would silently misstate a financial
// document rather than fail loudly.
//
// Conversion is not optional decoration. pgx scans `timestamptz` into
// time.Local — TimestamptzCodec's ScanLocation is nil, so the binary path builds
// values with time.Unix() — and the runtime image sets no TZ, so time.Local is
// UTC in production and Asia/Jakarta on a developer's machine. Formatting
// without converting therefore produces host-dependent output, and for a
// calendar date it produces a WRONG date: a ticket admitting at 06:00 WIB is
// 23:00Z the previous day, so an unconverted receipt tells a Day 2 holder they
// admit on Day 1.
//
// Resolved once. The fallback exists so LoadLocation can never fail on the
// delivery path: the runtime image ships tzdata today, but nothing pins that,
// and a switch to a true scratch base would otherwise turn a missing zone
// database into an undeliverable email.
var jakarta = sync.OnceValue(func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Jakarta"); err == nil {
		return loc
	}
	// Indonesia has not observed DST since 1964 and WIB has been a fixed +07:00
	// throughout, so this renders identically to the loaded zone for every date
	// this system will ever print.
	return time.FixedZone("WIB", 7*60*60)
})

// inJakarta converts an instant to the display zone. Every formatter goes
// through it; none may call Format on a raw scanned value.
func inJakarta(t time.Time) time.Time { return t.In(jakarta()) }

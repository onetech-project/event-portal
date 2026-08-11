// Package config loads all runtime configuration from environment variables. It is
// a shared, non-domain utility and therefore lives under pkg/ (Constitution
// Principle I).
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// minPaymentExpiry is the floor the payment provider itself documents: below 15
// minutes its expiry scheduler stops expiring transactions reliably, which would
// leave a code payable after the countdown we showed the guest hit zero.
const minPaymentExpiry = 15 * time.Minute

// Config holds every setting the API needs. Values are read once at startup so a
// misconfigured deployment fails fast rather than at first request.
type Config struct {
	AppPort     string
	AppBaseURL  string
	FrontendURL string

	DatabaseURL string

	JWTSecret string
	JWTTTL    time.Duration

	MidtransServerKey    string
	MidtransClientKey    string
	MidtransIsProduction bool
	// MidtransBaseURL overrides the Core API endpoint. Left empty it is derived
	// from MidtransIsProduction; setting it lets local and CI runs point at a stub
	// gateway so the full purchase flow can be exercised without the network.
	MidtransBaseURL string

	// PaymentExpiry is how long a QRIS code stays payable. The provider's own
	// expiry scheduler is only reliable at 15 minutes or more, so anything shorter
	// is rejected outright rather than silently producing codes that outlive the
	// deadline we show the guest.
	PaymentExpiry time.Duration
	// PaymentSweepInterval is how often abandoned orders past their deadline are
	// expired and their quota returned. It bounds how stale quota can get when no
	// provider notification arrives.
	PaymentSweepInterval time.Duration

	// BookingHold is how long a booked order holds its seats before the guest
	// must start payment. Booking writes payment_expires_at = now()+BookingHold;
	// the same sweeper that expires unpaid payment windows reclaims lapsed holds.
	BookingHold time.Duration
	// PaymentWindow is the server-owned payment deadline stamped when the guest
	// continues to payment. It is deliberately shorter than PaymentExpiry (the
	// gateway-side QR validity): the sweeper expires the order first, and a
	// payment landing in the gap goes through the webhook-after-expiry
	// reconciliation path instead of silently succeeding.
	PaymentWindow time.Duration
	// QRRefreshAfter is when the client swaps in a fresh QR during the payment
	// window. Purely a frontend timer — served to the client in the checkout
	// response; no background job runs on it.
	QRRefreshAfter time.Duration

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string

	// TicketLookupRateLimit is the sustained per-IP request rate allowed on the
	// public GET /tickets/:code endpoint (spec FR-020); TicketLookupBurst is the
	// short-term allowance above it.
	TicketLookupRateLimit float64
	TicketLookupBurst     int

	// Observability.
	ServiceName    string
	ServiceVersion string
	Environment    string
	// OTLPEndpoint is the collector's gRPC address. Empty disables trace export;
	// the service still runs and still propagates incoming trace context.
	OTLPEndpoint string
	// TraceSampleRatio is the head-sampling probability, 0..1.
	TraceSampleRatio float64
	// MetricsEnabled exposes GET /metrics for Prometheus to scrape.
	MetricsEnabled bool

	// Cache (Constitution Principle VII). Redis is a read cache in front of the
	// list endpoints and nothing else: it is a soft dependency, and the API must
	// start and serve with it absent.
	//
	// RedisURL empty is treated exactly like CacheEnabled=false — there is no
	// third state where caching is on but has nowhere to go.
	RedisURL string
	// CacheEnabled is the kill switch. False returns the system to direct
	// database reads on every request with no other behavioural difference.
	CacheEnabled bool
	// CacheTTL bounds how long a cached entry can survive. It is a BACKSTOP for
	// an invalidation that was somehow missed, not the freshness mechanism —
	// correctness comes from invalidating on commit.
	CacheTTL time.Duration
}

// CacheActive reports whether the cache should be built at all. Both the empty
// URL and the explicit kill switch collapse to the same answer so callers never
// have to test two things.
func (c *Config) CacheActive() bool { return c.CacheEnabled && c.RedisURL != "" }

type loader struct {
	errs []error
}

func (l *loader) required(key string) string {
	v := os.Getenv(key)
	if v == "" {
		l.errs = append(l.errs, fmt.Errorf("%s is required but not set", key))
	}
	return v
}

func (l *loader) str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func (l *loader) integer(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s must be an integer, got %q", key, v))
		return def
	}
	return n
}

func (l *loader) float(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s must be a number, got %q", key, v))
		return def
	}
	return n
}

func (l *loader) boolean(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s must be a boolean, got %q", key, v))
		return def
	}
	return b
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.errs = append(l.errs, fmt.Errorf("%s must be a duration such as 12h, got %q", key, v))
		return def
	}
	return d
}

// Load reads configuration from the environment, reporting every problem it finds
// at once rather than failing on the first.
func Load() (*Config, error) {
	l := &loader{}

	cfg := &Config{
		AppPort:     l.str("APP_PORT", "8080"),
		AppBaseURL:  l.str("APP_BASE_URL", "http://localhost:8080"),
		FrontendURL: l.str("FRONTEND_URL", "http://localhost:3000"),

		DatabaseURL: l.required("DATABASE_URL"),

		JWTSecret: l.required("JWT_SECRET"),
		JWTTTL:    l.duration("JWT_TTL", 12*time.Hour),

		MidtransServerKey:    l.str("MIDTRANS_SERVER_KEY", ""),
		MidtransClientKey:    l.str("MIDTRANS_CLIENT_KEY", ""),
		MidtransIsProduction: l.boolean("MIDTRANS_IS_PRODUCTION", false),
		MidtransBaseURL:      l.str("MIDTRANS_BASE_URL", ""),

		PaymentExpiry:        l.duration("PAYMENT_EXPIRY", 15*time.Minute),
		PaymentSweepInterval: l.duration("PAYMENT_SWEEP_INTERVAL", 30*time.Second),

		BookingHold:    l.duration("BOOKING_HOLD", time.Hour),
		PaymentWindow:  l.duration("PAYMENT_WINDOW", 14*time.Minute),
		QRRefreshAfter: l.duration("QR_REFRESH_AFTER", 7*time.Minute),

		SMTPHost:     l.str("SMTP_HOST", "localhost"),
		SMTPPort:     l.integer("SMTP_PORT", 1025),
		SMTPUsername: l.str("SMTP_USERNAME", ""),
		SMTPPassword: l.str("SMTP_PASSWORD", ""),
		SMTPFrom:     l.str("SMTP_FROM", "tickets@example.com"),
		SMTPFromName: l.str("SMTP_FROM_NAME", "Event Ticketing"),

		TicketLookupRateLimit: l.float("TICKET_LOOKUP_RATE_LIMIT", 5),
		TicketLookupBurst:     l.integer("TICKET_LOOKUP_BURST", 10),

		ServiceName:    l.str("OTEL_SERVICE_NAME", "ticketing-api"),
		ServiceVersion: l.str("SERVICE_VERSION", "dev"),
		Environment:    l.str("ENVIRONMENT", "local"),
		OTLPEndpoint:   l.str("OTEL_EXPORTER_OTLP_ENDPOINT", ""),
		// 1.0 at MVP volume: a complete picture costs little and a sampled one
		// hides exactly the rare failure you went looking for.
		TraceSampleRatio: l.float("OTEL_TRACES_SAMPLER_ARG", 1.0),
		MetricsEnabled:   l.boolean("METRICS_ENABLED", true),

		RedisURL:     l.str("REDIS_URL", "redis://localhost:6379/0"),
		CacheEnabled: l.boolean("CACHE_ENABLED", true),
		CacheTTL:     l.duration("CACHE_TTL", 10*time.Minute),
	}

	// Checked after loading rather than inside the duration helper: a bad value
	// must be reported alongside every other configuration problem, not instead of
	// them.
	if cfg.PaymentExpiry < minPaymentExpiry {
		l.errs = append(l.errs, fmt.Errorf(
			"PAYMENT_EXPIRY must be at least %s (the payment provider's expiry scheduler is unreliable below that), got %s",
			minPaymentExpiry, cfg.PaymentExpiry))
	}
	if cfg.PaymentSweepInterval <= 0 {
		l.errs = append(l.errs, fmt.Errorf(
			"PAYMENT_SWEEP_INTERVAL must be positive, got %s", cfg.PaymentSweepInterval))
	}
	if cfg.BookingHold <= 0 {
		l.errs = append(l.errs, fmt.Errorf(
			"BOOKING_HOLD must be positive, got %s", cfg.BookingHold))
	}
	// A non-positive TTL would write entries that expire immediately or never —
	// the first silently disables the cache, the second removes the backstop that
	// bounds a missed invalidation. Only checked when the cache is actually on.
	if cfg.CacheActive() && cfg.CacheTTL <= 0 {
		l.errs = append(l.errs, fmt.Errorf(
			"CACHE_TTL must be positive, got %s", cfg.CacheTTL))
	}
	// The server deadline must sit strictly inside the gateway-side QR validity,
	// otherwise a QR could outlive the order it belongs to (research R2).
	if cfg.PaymentWindow <= 0 || cfg.PaymentWindow >= cfg.PaymentExpiry {
		l.errs = append(l.errs, fmt.Errorf(
			"PAYMENT_WINDOW must be positive and shorter than PAYMENT_EXPIRY (%s), got %s",
			cfg.PaymentExpiry, cfg.PaymentWindow))
	}
	// The refresh must land while the window is still open, or the client would
	// swap in a QR for an order the sweeper is about to expire.
	if cfg.QRRefreshAfter <= 0 || cfg.QRRefreshAfter >= cfg.PaymentWindow {
		l.errs = append(l.errs, fmt.Errorf(
			"QR_REFRESH_AFTER must be positive and shorter than PAYMENT_WINDOW (%s), got %s",
			cfg.PaymentWindow, cfg.QRRefreshAfter))
	}

	if len(l.errs) > 0 {
		msg := "invalid configuration:"
		for _, e := range l.errs {
			msg += "\n  - " + e.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return cfg, nil
}

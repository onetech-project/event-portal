// Package config loads all runtime configuration from environment variables. It is
// a shared, non-domain utility and therefore lives under pkg/ (Constitution
// Principle I).
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

// Config holds every setting the API needs. Values are read once at startup so a
// misconfigured deployment fails fast rather than at first request.
type Config struct {
	AppPort     string
	AppBaseURL  string
	FrontendURL string

	DatabaseURL string

	JWTSecret string
	JWTTTL    time.Duration

	// PGBaseURL is the payment gateway's address. Required, with no compiled-in
	// default and no environment selector (FR-026, FR-026a): the deployment says
	// where the gateway is, and nothing here invents one. Pointing it at a stub is
	// how the full purchase flow is exercised offline (FR-008a).
	PGBaseURL string
	// PGServerKey and PGClientKey travel in the session-open body as
	// ac.cr.{client_secret,client_id}. The gateway checks they are present, not
	// what they are, so they grant no privilege — but they must never reach a log,
	// an error message, or a guest-facing response (FR-029).
	PGServerKey string
	PGClientKey string
	// PGCallbackToken is the bearer token the gateway presents on the inbound
	// notification endpoint. This side issues it; a mismatch is the only case
	// where the callback answers non-200 (FR-012).
	PGCallbackToken string

	// PaymentSweepInterval is how often abandoned orders past their deadline are
	// expired and their quota returned. It bounds how stale quota can get when no
	// provider notification arrives.
	PaymentSweepInterval time.Duration

	// BookingHold is how long a booked order holds its seats before the guest
	// must start payment. Booking writes payment_expires_at = now()+BookingHold;
	// the same sweeper that expires unpaid payment windows reclaims lapsed holds.
	BookingHold time.Duration
	// PaymentWindow is no longer the payment deadline — the gateway returns that
	// and it is adopted verbatim (FR-009). Two uses survive: the fallback deadline
	// when no usable expiry came back (which also raises a signal, FR-009b), and
	// the expectation an adopted expiry is measured against so a materially
	// different one is noticed rather than silently accepted (FR-009d).
	PaymentWindow time.Duration

	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
	SMTPFromName string

	// Branding printed on the ticket email and both of its attachments (spec 016
	// FR-035). Platform-wide by design, NOT per-event: FR-036 forbids storing
	// branding against an event, so every order's documents carry these same
	// strings whichever event was bought.
	//
	// Every key has a working default, so the API starts and delivers correctly
	// with none of them set — the same stance the SMTP block above takes.
	BrandSiteName     string
	BrandSiteURL      string
	BrandSupportEmail string
	BrandLegalEntity  string
	BrandAttribution  string
	BrandCopyright    string
	// BrandLogoPath is an optional file embedded in the PDF header bands. Empty,
	// missing, or unreadable falls back to a text wordmark — a decorative asset
	// must never fail a delivery (spec 016 research R-005).
	BrandLogoPath string

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

		PGBaseURL:       l.required("PG_BASE_URL"),
		PGServerKey:     l.str("PG_SERVER_KEY", ""),
		PGClientKey:     l.str("PG_CLIENT_KEY", ""),
		PGCallbackToken: l.required("PG_CALLBACK_TOKEN"),

		PaymentSweepInterval: l.duration("PAYMENT_SWEEP_INTERVAL", 30*time.Second),

		BookingHold:   l.duration("BOOKING_HOLD", time.Hour),
		PaymentWindow: l.duration("PAYMENT_WINDOW", 15*time.Minute),

		SMTPHost:     l.str("SMTP_HOST", "localhost"),
		SMTPPort:     l.integer("SMTP_PORT", 1025),
		SMTPUsername: l.str("SMTP_USERNAME", ""),
		SMTPPassword: l.str("SMTP_PASSWORD", ""),
		SMTPFrom:     l.str("SMTP_FROM", "tickets@example.com"),
		SMTPFromName: l.str("SMTP_FROM_NAME", "Event Ticketing"),

		BrandSiteName:     l.str("BRAND_SITE_NAME", "JIVE"),
		BrandSiteURL:      l.str("BRAND_SITE_URL", "https://www.jive.co.id"),
		BrandSupportEmail: l.str("BRAND_SUPPORT_EMAIL", "help@manjo.com"),
		BrandLegalEntity:  l.str("BRAND_LEGAL_ENTITY", "PT Manjo Teknologi Indonesia"),
		BrandAttribution:  l.str("BRAND_ATTRIBUTION", "Powered By Manjo"),
		BrandCopyright:    l.str("BRAND_COPYRIGHT", "© 2026 manjo"),
		BrandLogoPath:     l.str("BRAND_LOGO_PATH", ""),

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
	//
	// The QR_REFRESH_AFTER < PAYMENT_WINDOW < PAYMENT_EXPIRY interdependency that
	// used to live here is deliberately DELETED rather than loosened (FR-030).
	// None of the three still means what it did: the first is no longer sent, the
	// second is no longer ours to decide, and the third has nothing to drive. A
	// check comparing values that have lost their old meaning is worse than no
	// check, because it still looks like it is protecting something.
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
	if cfg.PaymentWindow <= 0 {
		l.errs = append(l.errs, fmt.Errorf(
			"PAYMENT_WINDOW must be positive, got %s", cfg.PaymentWindow))
	}
	// A present-but-unusable address must stop the deployment too (FR-027).
	// Configuration is read only at startup, so there is no last-known-good to
	// fall back to at request time — the alternative to refusing here is failing
	// on the first guest's checkout.
	if cfg.PGBaseURL != "" {
		if err := validateBaseURL(cfg.PGBaseURL); err != nil {
			l.errs = append(l.errs, fmt.Errorf("PG_BASE_URL %w", err))
		}
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

// validateBaseURL reports why an address cannot be used as a gateway endpoint.
//
// url.Parse alone is far too permissive to be a check — it accepts "not a url"
// happily as a relative path — so the scheme and host are asserted explicitly.
// The message names the problem rather than just the variable, because the
// operator reading it at boot cannot see the value from the log line alone.
func validateBaseURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("is not a usable address: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("must be an http:// or https:// address, got %q", raw)
	}
	if parsed.Host == "" {
		return fmt.Errorf("has no host, got %q", raw)
	}
	return nil
}

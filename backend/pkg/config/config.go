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
	// MidtransBaseURL overrides the SNAP endpoint. Left empty it is derived from
	// MidtransIsProduction; setting it lets local and CI runs point at a stub
	// gateway so the full purchase flow can be exercised without the network.
	MidtransBaseURL string

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
}

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

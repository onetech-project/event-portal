package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/config"
)

func setRequired(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5433/ticketing")
	t.Setenv("JWT_SECRET", "a-secret-at-least-32-bytes-long!!")
	// The gateway address and the callback token are required with no default
	// (FR-026), so every test that expects a successful load must supply them.
	t.Setenv("PG_BASE_URL", "http://localhost:10327")
	t.Setenv("PG_CALLBACK_TOKEN", "a-callback-token")
}

func TestLoadReadsRequiredValues(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "postgres://u:p@localhost:5433/ticketing", cfg.DatabaseURL)
	assert.Equal(t, "a-secret-at-least-32-bytes-long!!", cfg.JWTSecret)
	assert.Equal(t, "http://localhost:10327", cfg.PGBaseURL)
	assert.Equal(t, "a-callback-token", cfg.PGCallbackToken)
}

func TestLoadFailsWhenRequiredValueIsMissing(t *testing.T) {
	setRequired(t)
	t.Setenv("DATABASE_URL", "")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

// US4 / FR-026. Absent means refuse to start, naming what is missing — not fall
// back to a compiled-in hostname, of which there are now none.
func TestLoadRefusesWithoutGatewayAddress(t *testing.T) {
	setRequired(t)
	t.Setenv("PG_BASE_URL", "")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PG_BASE_URL")
}

func TestLoadRefusesWithoutCallbackToken(t *testing.T) {
	setRequired(t)
	t.Setenv("PG_CALLBACK_TOKEN", "")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PG_CALLBACK_TOKEN")
}

// FR-027. Present but unusable must also stop the deployment: configuration is
// read once at startup, so there is no last-known-good to fall back to and the
// only alternative to refusing here is failing on the first guest's checkout.
func TestLoadRefusesUnusableGatewayAddress(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"not a url at all", "not a url"},
		{"no scheme", "localhost:10327"},
		{"wrong scheme", "ftp://gateway.example"},
		{"no host", "http://"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setRequired(t)
			t.Setenv("PG_BASE_URL", tc.value)

			_, err := config.Load()

			require.Error(t, err)
			assert.Contains(t, err.Error(), "PG_BASE_URL")
		})
	}
}

// Every problem is reported at once rather than one per restart — an operator
// fixing a bad deploy should not have to discover the faults serially.
func TestLoadReportsEveryProblemTogether(t *testing.T) {
	setRequired(t)
	t.Setenv("PG_BASE_URL", "")
	t.Setenv("PG_CALLBACK_TOKEN", "")
	t.Setenv("DATABASE_URL", "")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PG_BASE_URL")
	assert.Contains(t, err.Error(), "PG_CALLBACK_TOKEN")
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

func TestLoadAppliesDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.AppPort)
	assert.Equal(t, 12*time.Hour, cfg.JWTTTL)
	// spec FR-020: the public ticket lookup is rate limited per client IP.
	assert.Positive(t, cfg.Throttle.TicketLookup.Rate)
	assert.GreaterOrEqual(t, float64(cfg.Throttle.TicketLookup.Burst), cfg.Throttle.TicketLookup.Rate)
	assert.Equal(t, 30*time.Second, cfg.PaymentSweepInterval)
	assert.Equal(t, time.Hour, cfg.BookingHold)
	// No longer the deadline — the fallback basis and the expectation (FR-009b/d).
	assert.Equal(t, 15*time.Minute, cfg.PaymentWindow)
	// The credential pair is optional: the gateway checks presence, not privilege.
	assert.Empty(t, cfg.PGServerKey)
	assert.Empty(t, cfg.PGClientKey)
}

// FR-030. The QR_REFRESH_AFTER < PAYMENT_WINDOW < PAYMENT_EXPIRY ordering is
// gone, not loosened. A window longer than the retired PAYMENT_EXPIRY default
// must now load cleanly — if it still fails, a check is comparing values that no
// longer carry their old meaning.
func TestLoadNoLongerEnforcesTheRetiredTimingOrder(t *testing.T) {
	setRequired(t)
	t.Setenv("PAYMENT_WINDOW", "45m")
	t.Setenv("PAYMENT_EXPIRY", "15m")   // retired; must be ignored entirely
	t.Setenv("QR_REFRESH_AFTER", "40m") // retired; must be ignored entirely

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, 45*time.Minute, cfg.PaymentWindow)
}

func TestLoadRejectsNonPositiveBookingHold(t *testing.T) {
	setRequired(t)
	t.Setenv("BOOKING_HOLD", "0s")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "BOOKING_HOLD")
}

func TestLoadRejectsNonPositivePaymentWindow(t *testing.T) {
	setRequired(t)
	t.Setenv("PAYMENT_WINDOW", "0s")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PAYMENT_WINDOW")
}

func TestBookingTimersOverridableFromEnv(t *testing.T) {
	setRequired(t)
	t.Setenv("BOOKING_HOLD", "3m")
	t.Setenv("PAYMENT_WINDOW", "10m")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, 3*time.Minute, cfg.BookingHold)
	assert.Equal(t, 10*time.Minute, cfg.PaymentWindow)
}

func TestLoadOverridesDefaultsFromEnv(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_PORT", "9090")
	t.Setenv("JWT_TTL", "30m")
	t.Setenv("TICKET_LOOKUP_RATE_LIMIT", "2")
	t.Setenv("TICKET_LOOKUP_BURST", "7")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("PAYMENT_SWEEP_INTERVAL", "10s")
	t.Setenv("PG_SERVER_KEY", "server-key")
	t.Setenv("PG_CLIENT_KEY", "client-key")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "9090", cfg.AppPort)
	assert.Equal(t, 30*time.Minute, cfg.JWTTTL)
	// Set here under the LEGACY names, so this also pins that a deployment
	// already using them keeps working (spec 018 FR-006a).
	assert.InDelta(t, 2.0, cfg.Throttle.TicketLookup.Rate, 0.001)
	assert.Equal(t, 7, cfg.Throttle.TicketLookup.Burst)
	assert.Equal(t, 2525, cfg.SMTPPort)
	assert.Equal(t, 10*time.Second, cfg.PaymentSweepInterval)
	assert.Equal(t, "server-key", cfg.PGServerKey)
	assert.Equal(t, "client-key", cfg.PGClientKey)
}

func TestLoadRejectsNonPositiveSweepInterval(t *testing.T) {
	setRequired(t)
	t.Setenv("PAYMENT_SWEEP_INTERVAL", "0s")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PAYMENT_SWEEP_INTERVAL")
}

func TestObservabilityDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "ticketing-api", cfg.ServiceName)
	assert.Equal(t, "local", cfg.Environment)
	assert.True(t, cfg.MetricsEnabled, "metrics are cheap and on by default")
	assert.Empty(t, cfg.OTLPEndpoint, "trace export stays off until a collector is configured")
	assert.InDelta(t, 1.0, cfg.TraceSampleRatio, 0.001)
}

func TestObservabilityCanBeConfigured(t *testing.T) {
	setRequired(t)
	t.Setenv("OTEL_SERVICE_NAME", "ticketing-api-staging")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "alloy:4317")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	t.Setenv("ENVIRONMENT", "staging")
	t.Setenv("SERVICE_VERSION", "1.4.2")
	t.Setenv("METRICS_ENABLED", "false")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "ticketing-api-staging", cfg.ServiceName)
	assert.Equal(t, "alloy:4317", cfg.OTLPEndpoint)
	assert.InDelta(t, 0.25, cfg.TraceSampleRatio, 0.001)
	assert.Equal(t, "staging", cfg.Environment)
	assert.Equal(t, "1.4.2", cfg.ServiceVersion)
	assert.False(t, cfg.MetricsEnabled)
}

func TestLoadRejectsMalformedDuration(t *testing.T) {
	setRequired(t)
	t.Setenv("JWT_TTL", "not-a-duration")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_TTL")
}

func TestLoadRejectsMalformedInt(t *testing.T) {
	setRequired(t)
	t.Setenv("SMTP_PORT", "abc")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SMTP_PORT")
}

// Spec 016 FR-035: branding is platform-wide configuration. Every key defaults,
// so an operator who sets none of them still gets correctly branded documents —
// the same stance the SMTP block takes, and the reason delivery is not part of
// the deploy critical path.
func TestLoadDefaultsEveryBrandingValue(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "JIVE", cfg.BrandSiteName)
	assert.Equal(t, "https://www.jive-promotion.com/", cfg.BrandSiteURL)
	assert.Equal(t, "help@manjo.co.id", cfg.BrandSupportEmail)
	assert.Equal(t, "PT Manjo Teknologi Indonesia", cfg.BrandLegalEntity)
	assert.Equal(t, "Powered By Manjo", cfg.BrandAttribution)
	// BrandLogoPath is an OVERRIDE, so its default is empty. That no longer
	// means no logo: since spec 016 the mark is compiled into the binary
	// (assets.go), and an empty path selects the embedded asset rather than the
	// text wordmark. The wordmark is now only the unreadable-asset failure path.
	assert.Empty(t, cfg.BrandLogoPath)
}

func TestLoadOverridesBrandingFromEnvironment(t *testing.T) {
	setRequired(t)
	t.Setenv("BRAND_SITE_NAME", "OTHEREXPO")
	t.Setenv("BRAND_SUPPORT_EMAIL", "support@example.test")
	t.Setenv("BRAND_LOGO_PATH", "/srv/assets/logo.png")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "OTHEREXPO", cfg.BrandSiteName)
	assert.Equal(t, "support@example.test", cfg.BrandSupportEmail)
	assert.Equal(t, "/srv/assets/logo.png", cfg.BrandLogoPath)
	// Untouched keys keep their defaults rather than collapsing to empty.
	assert.Equal(t, "Powered By Manjo", cfg.BrandAttribution)
}

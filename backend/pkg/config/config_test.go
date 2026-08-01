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
}

func TestLoadReadsRequiredValues(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "postgres://u:p@localhost:5433/ticketing", cfg.DatabaseURL)
	assert.Equal(t, "a-secret-at-least-32-bytes-long!!", cfg.JWTSecret)
}

func TestLoadFailsWhenRequiredValueIsMissing(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "a-secret-at-least-32-bytes-long!!")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

func TestLoadAppliesDefaults(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "8080", cfg.AppPort)
	assert.Equal(t, 12*time.Hour, cfg.JWTTTL)
	assert.False(t, cfg.MidtransIsProduction, "sandbox is the default per PRD 1.2")
	// spec FR-020: the public ticket lookup is rate limited per client IP.
	assert.Positive(t, cfg.TicketLookupRateLimit)
	assert.GreaterOrEqual(t, float64(cfg.TicketLookupBurst), cfg.TicketLookupRateLimit)
	assert.Equal(t, 15*time.Minute, cfg.PaymentExpiry, "the provider's documented QRIS default")
	assert.Equal(t, 30*time.Second, cfg.PaymentSweepInterval)
}

func TestLoadOverridesDefaultsFromEnv(t *testing.T) {
	setRequired(t)
	t.Setenv("APP_PORT", "9090")
	t.Setenv("JWT_TTL", "30m")
	t.Setenv("MIDTRANS_IS_PRODUCTION", "true")
	t.Setenv("TICKET_LOOKUP_RATE_LIMIT", "2")
	t.Setenv("TICKET_LOOKUP_BURST", "7")
	t.Setenv("SMTP_PORT", "2525")
	t.Setenv("PAYMENT_EXPIRY", "30m")
	t.Setenv("PAYMENT_SWEEP_INTERVAL", "10s")

	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "9090", cfg.AppPort)
	assert.Equal(t, 30*time.Minute, cfg.JWTTTL)
	assert.True(t, cfg.MidtransIsProduction)
	assert.InDelta(t, 2.0, cfg.TicketLookupRateLimit, 0.001)
	assert.Equal(t, 7, cfg.TicketLookupBurst)
	assert.Equal(t, 2525, cfg.SMTPPort)
	assert.Equal(t, 30*time.Minute, cfg.PaymentExpiry)
	assert.Equal(t, 10*time.Second, cfg.PaymentSweepInterval)
}

// Below 15 minutes the provider stops expiring transactions reliably, so a code
// could still be payable after the countdown we showed the guest reached zero.
// Startup refuses rather than shipping that inconsistency.
func TestLoadRejectsPaymentExpiryBelowProviderFloor(t *testing.T) {
	setRequired(t)
	t.Setenv("PAYMENT_EXPIRY", "5m")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PAYMENT_EXPIRY")
	assert.Contains(t, err.Error(), "15m")
}

func TestLoadRejectsNonPositiveSweepInterval(t *testing.T) {
	setRequired(t)
	t.Setenv("PAYMENT_SWEEP_INTERVAL", "0s")

	_, err := config.Load()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "PAYMENT_SWEEP_INTERVAL")
}

// The SNAP endpoint is derived from the environment flag unless explicitly
// overridden, which is what lets a local run point at a stub gateway.
func TestMidtransBaseURLDefaultsToEmptyAndIsOverridable(t *testing.T) {
	setRequired(t)

	cfg, err := config.Load()
	require.NoError(t, err)
	assert.Empty(t, cfg.MidtransBaseURL)

	t.Setenv("MIDTRANS_BASE_URL", "http://localhost:9999")
	cfg, err = config.Load()
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:9999", cfg.MidtransBaseURL)
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

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/config"
)

// writeEnvFile creates a .env in a temp directory and makes it the working
// directory for the test, mirroring how the binary finds its own .env.
func writeEnvFile(t *testing.T, contents string) {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte(contents), 0o600))

	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(original) })
}

func TestLoadDotEnvPopulatesTheEnvironment(t *testing.T) {
	writeEnvFile(t, "APP_PORT=9191\nSMTP_FROM=from-dotenv@example.com\n")
	t.Setenv("APP_PORT", "")
	t.Setenv("SMTP_FROM", "")

	require.NoError(t, config.LoadDotEnv())

	assert.Equal(t, "9191", os.Getenv("APP_PORT"))
	assert.Equal(t, "from-dotenv@example.com", os.Getenv("SMTP_FROM"))
}

// A real environment variable beats the file. Containers and CI inject config
// that way, and a stale committed .env must never silently override it.
func TestDotEnvDoesNotOverrideAnExistingVariable(t *testing.T) {
	writeEnvFile(t, "APP_PORT=9191\n")
	t.Setenv("APP_PORT", "7777")

	require.NoError(t, config.LoadDotEnv())

	assert.Equal(t, "7777", os.Getenv("APP_PORT"))
}

// Running without a .env is the normal case in production, not an error.
func TestLoadDotEnvIsAnNoOpWhenTheFileIsAbsent(t *testing.T) {
	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(t.TempDir()))
	t.Cleanup(func() { _ = os.Chdir(original) })

	assert.NoError(t, config.LoadDotEnv())
}

func TestLoadDotEnvReportsAMalformedFile(t *testing.T) {
	writeEnvFile(t, "this is not a valid env line\n")

	err := config.LoadDotEnv()

	require.Error(t, err)
	assert.Contains(t, err.Error(), ".env")
}

func TestLoadDotEnvFeedsLoad(t *testing.T) {
	writeEnvFile(t, "DATABASE_URL=postgres://u:p@localhost:5433/db\n"+
		"JWT_SECRET=a-secret-that-is-long-enough-ok\n"+
		"PG_BASE_URL=http://localhost:10327\n"+
		"PG_CALLBACK_TOKEN=a-callback-token\n"+
		"TICKET_LOOKUP_RATE_LIMIT=3\n")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("JWT_SECRET", "")
	t.Setenv("PG_BASE_URL", "")
	t.Setenv("PG_CALLBACK_TOKEN", "")
	t.Setenv("TICKET_LOOKUP_RATE_LIMIT", "")

	require.NoError(t, config.LoadDotEnv())
	cfg, err := config.Load()

	require.NoError(t, err)
	assert.Equal(t, "postgres://u:p@localhost:5433/db", cfg.DatabaseURL)
	assert.InDelta(t, 3.0, cfg.TicketLookupRateLimit, 0.001)
}

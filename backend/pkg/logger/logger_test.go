package logger_test

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/logger"
)

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	return out
}

func TestLoggerEmitsStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelInfo)

	log.Info("checkout completed", "order_number", "ORD-1", "ticket_type_id", "tt-1")

	entry := decode(t, &buf)
	assert.Equal(t, "checkout completed", entry["msg"])
	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, "ORD-1", entry["order_number"])
	assert.Equal(t, "tt-1", entry["ticket_type_id"])
}

func TestLoggerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelWarn)

	log.Info("suppressed")

	assert.Empty(t, buf.String())
}

func TestWithAddsPersistentFields(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelInfo).With("provider", "manjo")

	log.Info("webhook received", "provider_status", "settlement")

	entry := decode(t, &buf)
	assert.Equal(t, "manjo", entry["provider"])
	assert.Equal(t, "settlement", entry["provider_status"])
}

func TestParseLevel(t *testing.T) {
	assert.Equal(t, logger.LevelDebug, logger.ParseLevel("debug"))
	assert.Equal(t, logger.LevelWarn, logger.ParseLevel("WARN"))
	assert.Equal(t, logger.LevelError, logger.ParseLevel("error"))
	assert.Equal(t, logger.LevelInfo, logger.ParseLevel("nonsense"), "unknown levels fall back to info")
}

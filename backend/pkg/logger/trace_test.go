package logger_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"

	"github.com/manjo/ticketing/backend/pkg/logger"
)

// spanContext builds a valid, sampled SpanContext without needing a real SDK.
func spanContext(t *testing.T) trace.SpanContext {
	t.Helper()

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	require.NoError(t, err)

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
}

// Correlating a log line with its trace is the whole point of running both:
// Grafana links from a Loki line to the Tempo trace through these two fields.
func TestTraceIDsAreAddedWhenTheContextCarriesASpan(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelInfo)

	ctx := trace.ContextWithSpanContext(context.Background(), spanContext(t))
	log.InfoContext(ctx, "checkout completed", "order_number", "ORD-1")

	entry := decode(t, &buf)
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", entry["trace_id"])
	assert.Equal(t, "00f067aa0ba902b7", entry["span_id"])
	assert.Equal(t, "ORD-1", entry["order_number"])
}

func TestNoTraceFieldsWithoutASpan(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelInfo)

	log.InfoContext(context.Background(), "no span here")

	entry := decode(t, &buf)
	assert.NotContains(t, entry, "trace_id")
	assert.NotContains(t, entry, "span_id")
}

// A non-context call must still work; it simply carries no correlation.
func TestPlainLoggingStillWorks(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelInfo)

	log.Info("plain message", "key", "value")

	entry := decode(t, &buf)
	assert.Equal(t, "plain message", entry["msg"])
	assert.Equal(t, "value", entry["key"])
	assert.NotContains(t, entry, "trace_id")
}

func TestTraceFieldsSurviveWith(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelInfo).With("provider", "midtrans")

	ctx := trace.ContextWithSpanContext(context.Background(), spanContext(t))
	log.InfoContext(ctx, "webhook received")

	entry := decode(t, &buf)
	assert.Equal(t, "midtrans", entry["provider"])
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", entry["trace_id"])
}

// An unsampled or invalid span context has no trace worth linking to.
func TestAnInvalidSpanContextAddsNothing(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelInfo)

	ctx := trace.ContextWithSpanContext(context.Background(), trace.SpanContext{})
	log.InfoContext(ctx, "no valid span")

	entry := decode(t, &buf)
	assert.NotContains(t, entry, "trace_id")
}

func TestServiceFieldsAreAttachedWhenConfigured(t *testing.T) {
	var buf bytes.Buffer
	log := logger.NewWithWriter(&buf, logger.LevelInfo).
		With("service_name", "ticketing-api", "environment", "local")

	log.Info("started")

	entry := decode(t, &buf)
	assert.Equal(t, "ticketing-api", entry["service_name"])
	assert.Equal(t, "local", entry["environment"])
}

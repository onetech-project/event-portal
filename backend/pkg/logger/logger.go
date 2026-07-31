// Package logger provides the shared structured logger used across every domain.
package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// Level aliases keep slog out of call sites that only need to pick a verbosity.
const (
	LevelDebug = slog.LevelDebug
	LevelInfo  = slog.LevelInfo
	LevelWarn  = slog.LevelWarn
	LevelError = slog.LevelError
)

// Logger is the structured logger handed to handlers and services.
type Logger = slog.Logger

// traceHandler stamps every record logged with a context onto its trace.
//
// This is what makes a log line and a trace two views of the same request:
// Grafana pivots from a Loki line to the Tempo trace using exactly these two
// fields, so emitting them is the difference between three separate tools and
// one connected picture.
type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, record slog.Record) error {
	if span := trace.SpanContextFromContext(ctx); span.IsValid() {
		record.AddAttrs(
			slog.String("trace_id", span.TraceID().String()),
			slog.String("span_id", span.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, record)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{Handler: h.Handler.WithGroup(name)}
}

// NewWithWriter builds a JSON logger writing to w. Used directly by tests.
func NewWithWriter(w io.Writer, level slog.Level) *Logger {
	return slog.New(traceHandler{
		Handler: slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level}),
	})
}

// New builds the process logger, writing JSON to stdout.
//
// JSON on stdout is deliberate: the container runtime captures it, Alloy tails it
// and ships it to Loki, and every field stays queryable without a parsing rule.
func New(level slog.Level) *Logger {
	return NewWithWriter(os.Stdout, level)
}

// ParseLevel maps a LOG_LEVEL string onto a level, defaulting to info for anything
// unrecognized so a typo degrades verbosity rather than silencing the process.
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

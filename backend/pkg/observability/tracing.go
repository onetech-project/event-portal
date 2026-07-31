// Package observability wires the three signals this service emits: traces over
// OTLP, Prometheus metrics over /metrics, and trace-correlated JSON logs on
// stdout (see pkg/logger).
//
// The collector is Grafana Alloy, which fans traces out to Tempo and tails
// container logs into Loki; Prometheus scrapes the metrics endpoint directly.
package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TracingConfig describes where traces go and how they are labelled.
type TracingConfig struct {
	// ServiceName identifies this service in Tempo. Required.
	ServiceName string
	// ServiceVersion is attached to every span for release correlation.
	ServiceVersion string
	// Environment separates local, staging, and production traces.
	Environment string
	// OTLPEndpoint is the collector's gRPC address (host:port). When empty,
	// tracing is disabled and a no-op provider is installed.
	OTLPEndpoint string
	// SampleRatio is the head-sampling probability, 0..1. At MVP volume 1.0 is
	// affordable and far more useful than a sample.
	SampleRatio float64
}

// Shutdown flushes and stops the exporter. Always call it, or spans buffered at
// exit are lost — which is precisely when you most want them.
type Shutdown func(context.Context) error

// InitTracing installs the global tracer provider and propagator.
//
// When no endpoint is configured it installs a no-op provider and returns a
// no-op shutdown, so the service runs identically without a collector — nothing
// in the request path has to check whether tracing is on.
func InitTracing(ctx context.Context, cfg TracingConfig) (Shutdown, error) {
	// The W3C propagator is installed either way, so an incoming traceparent is
	// still honoured (and forwarded) even when this service is not exporting.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if cfg.OTLPEndpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
		// Plaintext: the collector is a sidecar on a private network in this MVP.
		otlptracegrpc.WithTLSCredentials(insecure.NewCredentials()),
		otlptracegrpc.WithDialOption(grpc.WithBlock()),
		otlptracegrpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	res, err := BuildResource(cfg)
	if err != nil {
		return nil, err
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		// ParentBased keeps a trace whole: once an upstream service samples a
		// request, every downstream span for it is kept too.
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(cfg.SampleRatio),
		)),
	)
	otel.SetTracerProvider(provider)

	return func(shutdownCtx context.Context) error {
		if err := provider.Shutdown(shutdownCtx); err != nil {
			return errors.Join(errors.New("shutdown trace provider"), err)
		}
		return nil
	}, nil
}

// BuildResource assembles the attributes attached to every span.
//
// Exported so the merge can be tested directly: it fails at startup rather than
// at compile time when the semconv package here and the one the SDK's default
// resource uses disagree on their schema URL.
func BuildResource(cfg TracingConfig) (*resource.Resource, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("deployment.environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("build trace resource: %w", err)
	}
	return res, nil
}

// Tracer returns a named tracer for manual instrumentation.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

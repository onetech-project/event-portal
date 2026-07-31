package observability_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/manjo/ticketing/backend/pkg/observability"
)

func testConfig() observability.TracingConfig {
	return observability.TracingConfig{
		ServiceName:    "ticketing-api",
		ServiceVersion: "1.2.3",
		Environment:    "test",
		SampleRatio:    1,
	}
}

// The resource merges service attributes with the SDK's defaults, and those two
// must share a semconv schema URL — a mismatch fails at startup, not at compile
// time, so it is worth an explicit test.
func TestResourceBuildsWithoutASchemaConflict(t *testing.T) {
	res, err := observability.BuildResource(testConfig())

	require.NoError(t, err)

	attrs := map[string]string{}
	for _, attr := range res.Attributes() {
		attrs[string(attr.Key)] = attr.Value.Emit()
	}

	assert.Equal(t, "ticketing-api", attrs["service.name"])
	assert.Equal(t, "1.2.3", attrs["service.version"])
	assert.Equal(t, "test", attrs["deployment.environment"])
}

// Without a collector the service must still start and behave normally.
func TestInitTracingWithoutAnEndpointIsANoOp(t *testing.T) {
	shutdown, err := observability.InitTracing(context.Background(), testConfig())

	require.NoError(t, err)
	require.NotNil(t, shutdown)
	assert.NoError(t, shutdown(context.Background()))
}

// Incoming trace context is honoured even when this service exports nothing, so
// a request stays part of one trace across services.
func TestPropagatorIsInstalledEvenWhenTracingIsDisabled(t *testing.T) {
	_, err := observability.InitTracing(context.Background(), testConfig())
	require.NoError(t, err)

	carrier := propagation.MapCarrier{
		"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}
	ctx := otel.GetTextMapPropagator().Extract(context.Background(), carrier)

	span := spanContextFrom(ctx)
	assert.True(t, span.IsValid())
	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", span.TraceID().String())
}

func TestTracerIsAlwaysUsable(t *testing.T) {
	_, err := observability.InitTracing(context.Background(), testConfig())
	require.NoError(t, err)

	// Must not panic and must return a usable span even with no exporter.
	ctx, span := observability.Tracer("test").Start(context.Background(), "unit")
	defer span.End()

	assert.NotNil(t, ctx)
	assert.NotNil(t, span)
}

func spanContextFrom(ctx context.Context) trace.SpanContext {
	return trace.SpanContextFromContext(ctx)
}

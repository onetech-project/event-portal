package payment_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/pgauto/cdtc/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/httpx"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// --- T024: SSE checkout-status stream ----------------------------------------

func TestStreamHubFansOutToSubscribersOfTheSameOrder(t *testing.T) {
	hub := payment.NewStreamHub()
	a, cancelA := hub.Subscribe("ORD-1")
	b, cancelB := hub.Subscribe("ORD-1")
	other, cancelOther := hub.Subscribe("ORD-2")
	defer cancelA()
	defer cancelB()
	defer cancelOther()

	hub.Publish("ORD-1", payment.StatusEvent{OrderID: "ORD-1", Status: "PAID"})

	assert.Equal(t, "PAID", (<-a).Status)
	assert.Equal(t, "PAID", (<-b).Status)
	select {
	case ev := <-other:
		t.Fatalf("ORD-2 subscriber must not receive ORD-1's event, got %+v", ev)
	default:
	}
}

func TestStreamHubPublishNeverBlocksOnASlowReader(t *testing.T) {
	hub := payment.NewStreamHub()
	_, cancel := hub.Subscribe("ORD-1")
	defer cancel()

	done := make(chan struct{})
	go func() {
		for range 100 { // far beyond the channel buffer
			hub.Publish("ORD-1", payment.StatusEvent{OrderID: "ORD-1", Status: "PENDING"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish stalled on a reader that never drains")
	}
}

// streamAPI mounts the SSE route with millisecond timers over a real order.
func streamAPI(t *testing.T, f webhookFixture) *echo.Echo {
	t.Helper()
	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(logger.NewWithWriter(&bytes.Buffer{}, logger.LevelError))
	payment.NewHandler(f.svc, testsupport.DiscardLogger()).
		WithStreamIntervals(20*time.Millisecond, 30*time.Millisecond).
		RegisterStatusStream(e.Group("/api/v1"))
	return e
}

// streamRequest runs one SSE request until ctx expires or the server closes,
// returning everything that was written.
func streamRequest(t *testing.T, e *echo.Echo, ctx context.Context, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Body.String()
}

func TestStatusStreamSendsTheInitialSnapshot(t *testing.T) {
	f := newWebhookFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	body := streamRequest(t, streamAPI(t, f), ctx, "/api/v1/ticket/checkout/ORD-WEBHOOK/status")

	assert.Contains(t, body, `data: {`)
	assert.Contains(t, body, `"order_id":"ORD-WEBHOOK"`)
	assert.Contains(t, body, `"status":"PENDING"`)
}

func TestStatusStreamEmitsThePublishedTransitionAndCloses(t *testing.T) {
	f := newWebhookFixture(t)
	e := streamAPI(t, f)

	go func() {
		time.Sleep(30 * time.Millisecond)
		f.svc.Hub().Publish("ORD-WEBHOOK",
			payment.StatusEvent{OrderID: "ORD-WEBHOOK", Status: "PAID"})
	}()

	// No context timeout needed: a terminal status closes the stream itself.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	body := streamRequest(t, e, ctx, "/api/v1/ticket/checkout/ORD-WEBHOOK/status")

	assert.Contains(t, body, `"status":"PENDING"`, "snapshot first")
	assert.Contains(t, body, `"status":"PAID"`, "then the live transition")
}

func TestStatusStreamCatchesADriftedTransitionWithoutAPublish(t *testing.T) {
	f := newWebhookFixture(t)
	e := streamAPI(t, f)

	// The order flips in the database with NO hub publish — as if another
	// process handled the webhook.
	go func() {
		time.Sleep(30 * time.Millisecond)
		_, err := f.pool.Exec(context.Background(),
			`UPDATE orders SET status_id = (SELECT id FROM order_statuses WHERE name = 'EXPIRED') WHERE id = $1`, f.orderID)
		require.NoError(t, err)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	body := streamRequest(t, e, ctx, "/api/v1/ticket/checkout/ORD-WEBHOOK/status")

	assert.Contains(t, body, `"status":"EXPIRED"`, "the drift re-read catches it")
}

func TestStatusStreamClosesImmediatelyForASettledOrder(t *testing.T) {
	f := newWebhookFixture(t)
	require.NoError(t, f.notify(t, status.Completed))
	e := streamAPI(t, f)

	// No timeout: a terminal snapshot must close without waiting on anything.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	body := streamRequest(t, e, ctx, "/api/v1/ticket/checkout/ORD-WEBHOOK/status")

	assert.Contains(t, body, `"status":"PAID"`)
	assert.Less(t, time.Since(start), time.Second,
		"a settled order's stream closes after the snapshot")
	assert.Equal(t, 1, strings.Count(body, "data: "), "exactly the snapshot frame")
}

func TestStatusStreamAnswers404ForAnUnknownOrder(t *testing.T) {
	f := newWebhookFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ticket/checkout/ORD-NOPE/status", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	streamAPI(t, f).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"code":404001`, "pre-stream errors stay enveloped")
}

func TestWebhookTransitionReachesAnOpenStream(t *testing.T) {
	f := newWebhookFixture(t)
	e := streamAPI(t, f)

	go func() {
		time.Sleep(30 * time.Millisecond)
		// The real path: a settlement notification publishes to the hub.
		_ = f.notify(t, status.Completed)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	body := streamRequest(t, e, ctx, "/api/v1/ticket/checkout/ORD-WEBHOOK/status")

	assert.Contains(t, body, `"status":"PAID"`,
		"the webhook's applyOutcome publish reaches the open stream")
}

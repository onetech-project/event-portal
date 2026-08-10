package payment

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// The SSE checkout-status stream (GET /ticket/checkout/:order_id/status).
//
// Frames are raw SSE — NOT wrapped in the JSON envelope (clarification
// 2026-08-05: SSE frames and binary responses are the two exceptions). Each
// data frame is one StatusEvent; a comment frame every keepAliveInterval keeps
// intermediaries from cutting the idle connection; the server closes after a
// terminal status so a settled page holds no connection open.

// StatusEvent is one SSE data frame.
type StatusEvent struct {
	OrderID string `json:"order_id"`
	Status  string `json:"status"`
	// ExpiresAt rides along so a QR refresh window survives a reconnect.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// terminalStatus reports whether nothing can change after this status — the
// signal to close the stream.
func terminalStatus(status string) bool {
	return status == OrderStatusPaid || status == OrderStatusExpired || status == OrderStatusCancelled
}

// StreamHub fans order-status transitions out to the order's open streams.
//
// Publish never blocks: a subscriber that cannot keep up (buffer full) is
// skipped rather than allowed to stall the webhook path — the 15-second drift
// re-read in the handler catches whatever a skipped frame missed.
type StreamHub struct {
	mu   sync.Mutex
	subs map[string]map[chan StatusEvent]struct{}
}

// NewStreamHub builds an empty hub.
func NewStreamHub() *StreamHub {
	return &StreamHub{subs: map[string]map[chan StatusEvent]struct{}{}}
}

// Subscribe registers a listener for one order. The returned cancel must be
// called when the stream ends; it is idempotent.
func (h *StreamHub) Subscribe(orderNumber string) (<-chan StatusEvent, func()) {
	ch := make(chan StatusEvent, 4)

	h.mu.Lock()
	if h.subs[orderNumber] == nil {
		h.subs[orderNumber] = map[chan StatusEvent]struct{}{}
	}
	h.subs[orderNumber][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[orderNumber], ch)
			if len(h.subs[orderNumber]) == 0 {
				delete(h.subs, orderNumber)
			}
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

// Publish notifies every open stream of the order's new status.
func (h *StreamHub) Publish(orderNumber string, ev StatusEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[orderNumber] {
		select {
		case ch <- ev:
		default: // never stall a webhook on a slow reader
		}
	}
}

// streamLimiter caps concurrent SSE connections per IP, so one client cannot
// hold every server connection open (contracts/api.md, Rate limiting).
type streamLimiter struct {
	mu    sync.Mutex
	open  map[string]int
	limit int
}

func newStreamLimiter(limit int) *streamLimiter {
	return &streamLimiter{open: map[string]int{}, limit: limit}
}

// acquire reserves a slot for ip, reporting false at the cap. release must be
// called once per successful acquire.
func (l *streamLimiter) acquire(ip string) (release func(), ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.open[ip] >= l.limit {
		return nil, false
	}
	l.open[ip]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			l.open[ip]--
			if l.open[ip] <= 0 {
				delete(l.open, ip)
			}
			l.mu.Unlock()
		})
	}, true
}

// Defaults for the stream's timers (overridable on the handler for tests).
const (
	defaultKeepAliveInterval = 25 * time.Second
	defaultDriftReadInterval = 15 * time.Second
	// defaultStreamCap is the per-IP concurrent connection cap.
	defaultStreamCap = 10
)

// RegisterStatusStream mounts the SSE endpoint. It manages its own per-IP
// connection cap rather than a request-rate limit: the cost here is held-open
// connections, not calls per second.
func (h *Handler) RegisterStatusStream(g *echo.Group) {
	if h.streams == nil {
		h.streams = newStreamLimiter(defaultStreamCap)
	}
	g.GET("/ticket/checkout/:order_id/status", h.statusStream)
}

func (h *Handler) statusStream(c echo.Context) error {
	orderNumber := c.Param("order_id")

	release, ok := h.streams.acquire(c.RealIP())
	if !ok {
		return apperr.New(http.StatusTooManyRequests, apperr.CodeRateLimited,
			"Too many open status streams from this address.")
	}
	defer release()

	// The initial snapshot doubles as the existence check: a 404 here is a
	// normal enveloped error, only a live stream switches to SSE framing.
	ord, err := h.svc.orders.OrderByNumber(c.Request().Context(), orderNumber)
	if err != nil {
		return apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")
	}

	// Subscribe BEFORE the snapshot is written: a transition landing between
	// the two would otherwise be lost until the drift re-read.
	events, cancel := h.svc.hub.Subscribe(orderNumber)
	defer cancel()

	res := c.Response()
	res.Header().Set(echo.HeaderContentType, "text/event-stream")
	res.Header().Set("Cache-Control", "no-store")
	res.Header().Set("Connection", "keep-alive")
	res.WriteHeader(http.StatusOK)

	lastStatus := ord.Status
	if err := writeSSE(res, StatusEvent{
		OrderID: ord.OrderNumber, Status: ord.Status, ExpiresAt: ord.PaymentExpiresAt,
	}); err != nil {
		return nil // client already gone
	}
	if terminalStatus(lastStatus) {
		return nil
	}

	keepAlive := time.NewTicker(h.keepAliveInterval)
	defer keepAlive.Stop()
	// The drift re-read covers a transition whose publish this process never
	// saw — another instance handled the webhook, or a frame was dropped.
	drift := time.NewTicker(h.driftReadInterval)
	defer drift.Stop()

	ctx := c.Request().Context()
	for {
		select {
		case <-ctx.Done():
			return nil

		case ev := <-events:
			if err := writeSSE(res, ev); err != nil {
				return nil
			}
			lastStatus = ev.Status
			if terminalStatus(lastStatus) {
				return nil
			}

		case <-keepAlive.C:
			if err := writeKeepAlive(res); err != nil {
				return nil
			}

		case <-drift.C:
			current, err := h.svc.orders.OrderByNumber(ctx, orderNumber)
			if err != nil {
				continue // transient; the next tick retries
			}
			if current.Status == lastStatus {
				continue
			}
			if err := writeSSE(res, StatusEvent{
				OrderID: current.OrderNumber, Status: current.Status,
				ExpiresAt: current.PaymentExpiresAt,
			}); err != nil {
				return nil
			}
			lastStatus = current.Status
			if terminalStatus(lastStatus) {
				return nil
			}
		}
	}
}

// keepAliveEvent is the SSE event name of the liveness beat.
const keepAliveEvent = "keep-alive"

// writeKeepAlive emits the periodic beat as a NAMED event rather than as a
// comment frame.
//
// A comment (`: keep-alive`) nurses intermediaries just as well, and that is all
// it was ever asked to do. But the browser's EventSource discards comments
// without dispatching anything, so a client cannot tell a healthy idle stream
// from one an intermediary is holding open and forwarding nothing — which is the
// exact failure the client's liveness watchdog exists to catch (FR-021e). A
// named event is observable via addEventListener and, unlike an unnamed data
// frame, does NOT reach onmessage, so the status path needs no filtering.
func writeKeepAlive(res *echo.Response) error {
	if _, err := fmt.Fprintf(res, "event: %s\ndata: {}\n\n", keepAliveEvent); err != nil {
		return err
	}
	res.Flush()
	return nil
}

// writeSSE emits one data frame and flushes it to the socket.
func writeSSE(res *echo.Response, ev StatusEvent) error {
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(res, "data: %s\n\n", payload); err != nil {
		return err
	}
	res.Flush()
	return nil
}

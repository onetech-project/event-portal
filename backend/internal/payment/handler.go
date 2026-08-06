package payment

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"

	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// maxWebhookBody caps how much of a notification body is read. Provider
// notifications are a few kilobytes; the cap stops a malformed or hostile request
// from consuming unbounded memory.
const maxWebhookBody = 1 << 20

// signatureHeader is honoured for providers that sign in a header. Midtrans
// carries its signature in the body instead, and VerifyWebhook falls back to that.
const signatureHeader = "X-Signature"

// Handler exposes the payment webhook over HTTP.
type Handler struct {
	svc *Service
	log *logger.Logger
	// qrRefreshAfter is echoed to the client as qr_refresh_after_seconds so the
	// refresh timer stays server-owned (config QR_REFRESH_AFTER).
	qrRefreshAfter time.Duration

	// SSE stream plumbing (stream.go). Intervals are fields so tests can run
	// the loop in milliseconds.
	streams           *streamLimiter
	keepAliveInterval time.Duration
	driftReadInterval time.Duration
}

// NewHandler builds the payment HTTP handler.
func NewHandler(svc *Service, log *logger.Logger) *Handler {
	return &Handler{
		svc: svc, log: log, qrRefreshAfter: 7 * time.Minute,
		streams:           newStreamLimiter(defaultStreamCap),
		keepAliveInterval: defaultKeepAliveInterval,
		driftReadInterval: defaultDriftReadInterval,
	}
}

// WithQRRefreshAfter overrides the client refresh point from config.
func (h *Handler) WithQRRefreshAfter(d time.Duration) *Handler {
	h.qrRefreshAfter = d
	return h
}

// WithStreamIntervals overrides the SSE keep-alive and drift re-read timers —
// tests run the loop in milliseconds instead of tens of seconds.
func (h *Handler) WithStreamIntervals(keepAlive, driftRead time.Duration) *Handler {
	h.keepAliveInterval = keepAlive
	h.driftReadInterval = driftRead
	return h
}

// RegisterPublicRoutes mounts the webhook. It is called by the payment provider,
// not by a browser, and is authenticated by signature rather than by session.
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
	g.POST("/payment/webhook/:provider", h.webhook)
}

// RegisterRefreshRoute mounts the guest's "check payment status" endpoint.
//
// It is mounted separately from the webhook because it must sit behind a
// per-IP rate limit: unlike the polled read endpoint, every call here costs an
// outbound provider round-trip.
func (h *Handler) RegisterRefreshRoute(g *echo.Group) {
	g.POST("/ticket/order/:order_id/payment/refresh", h.refreshStatus)
	// The 7-minute QR re-issue (FR-015): also one provider round-trip per call,
	// so it shares this limited group (contracts/api.md, Rate limiting).
	g.POST("/ticket/checkout/:order_id/refresh-qr", h.reissueQR)
}

// reissueQRResponse mirrors the checkout call's data shape (contracts/api.md
// call 8b), so the payment screen renders both identically.
type reissueQRResponse struct {
	OrderID               string    `json:"order_id"`
	QRString              string    `json:"qr_string"`
	ExpiresAt             time.Time `json:"expires_at"`
	QRImageURL            string    `json:"qr_image_url"`
	QRRefreshAfterSeconds int       `json:"qr_refresh_after_seconds"`
}

func (h *Handler) reissueQR(c echo.Context) error {
	reissued, err := h.svc.ReissueQR(c.Request().Context(), c.Param("order_id"))
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, reissueQRResponse{
		OrderID:   reissued.OrderNumber,
		QRString:  reissued.QRString,
		ExpiresAt: reissued.ExpiresAt,
		// Same render route the checkout response names; the path is part of the
		// public contract, not an order-domain import.
		QRImageURL:            "/api/v1/ticket/order/" + reissued.OrderNumber + "/qris.png",
		QRRefreshAfterSeconds: int(h.qrRefreshAfter.Seconds()),
	})
}

// refreshResponse is what the guest's button gets back. `changed` is what lets
// the page say "payment confirmed" versus "still waiting" rather than nothing.
type refreshResponse struct {
	OrderNumber string    `json:"order_number"`
	Status      string    `json:"status"`
	Changed     bool      `json:"changed"`
	CheckedAt   time.Time `json:"checked_at"`
}

func (h *Handler) refreshStatus(c echo.Context) error {
	ctx := c.Request().Context()
	orderNumber := c.Param("order_id")

	result, err := h.svc.RefreshStatus(ctx, orderNumber)
	if errors.Is(err, ErrOrderNotFound) {
		return apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")
	}
	if errors.Is(err, ErrProviderHasNoRecord) {
		// The order exists; the provider simply has nothing to say about it. That
		// is a status we could not confirm, not a missing order — telling the
		// guest their own order does not exist would be wrong.
		return apperr.Wrap(err, http.StatusBadGateway, apperr.CodePaymentStatusUnavailable,
			"We could not confirm this payment with the provider. Please try again shortly.")
	}
	if err != nil {
		// The provider being unreachable is not the guest's problem and not a bug
		// in this system: the order is untouched, the page keeps polling, and the
		// button stays available.
		h.log.ErrorContext(ctx, "could not reconcile order status with the provider",
			"order_number", orderNumber, "error", err.Error())
		return apperr.Wrap(err, http.StatusBadGateway, apperr.CodePaymentStatusUnavailable,
			"We could not reach the payment provider just now. Your payment is unaffected — please try again shortly.")
	}

	return httpx.Respond(c, http.StatusOK, refreshResponse{
		OrderNumber: result.OrderNumber,
		Status:      result.Status,
		Changed:     result.Changed,
		CheckedAt:   time.Now().UTC(),
	})
}

func (h *Handler) webhook(c echo.Context) error {
	provider := c.Param("provider")
	if provider != h.svc.gateway.Name() {
		// Silently accepting a notification we cannot verify would be worse than
		// refusing it: nothing here can authenticate another provider's payload.
		h.log.WarnContext(c.Request().Context(), "notification for an unconfigured provider", "provider", provider)
		return apperr.NotFound(apperr.CodeNotFound, "Unknown payment provider.")
	}

	payload, err := io.ReadAll(io.LimitReader(c.Request().Body, maxWebhookBody))
	if err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The notification body could not be read.")
	}

	err = h.svc.HandleNotification(c.Request().Context(), provider, payload, c.Request().Header.Get(signatureHeader))
	if errors.Is(err, ErrInvalidSignature) {
		return apperr.Wrap(err, http.StatusUnauthorized, apperr.CodeInvalidSignature,
			"The notification signature could not be verified.")
	}
	if err != nil {
		return err
	}

	// Every authenticated notification is acknowledged immediately, before any
	// post-payment work runs (ARCHITECTURE.md §3.4).
	return c.NoContent(http.StatusOK)
}

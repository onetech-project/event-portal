package payment

import (
	"errors"
	"io"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"
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
}

// NewHandler builds the payment HTTP handler.
func NewHandler(svc *Service, log *logger.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// RegisterPublicRoutes mounts the webhook. It is called by the payment provider,
// not by a browser, and is authenticated by signature rather than by session.
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
	g.POST("/payment/webhook/:provider", h.webhook)
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

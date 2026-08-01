package order

import (
	"net/http"

	"github.com/labstack/echo/v4"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// qrImageSize is the rendered QR's edge in pixels. Large enough to scan from a
// phone held at arm's length, small enough to stay a few KB.
const qrImageSize = 512

// Handler exposes the order domain over HTTP.
type Handler struct {
	svc    *Service
	public *PublicService
	log    *logger.Logger
}

// NewHandler builds the order HTTP handler.
func NewHandler(svc *Service, public *PublicService, log *logger.Logger) *Handler {
	return &Handler{svc: svc, public: public, log: log}
}

// RegisterPublicRoutes mounts the guest purchase surface. No authentication is
// required anywhere in it (Constitution Principle VI).
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
	g.POST("/checkout", h.checkout)
	g.GET("/orders/:orderNumber", h.orderDetail)
	g.GET("/orders/:orderNumber/qris.png", h.qrImage)
}

func (h *Handler) checkout(c echo.Context) error {
	var req CheckoutRequest
	if err := c.Bind(&req); err != nil {
		// Bind failures are malformed JSON or a badly-typed field (for example a
		// ticket_type_id that is not a UUID) — a client problem, not a server one.
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}

	resp, err := h.svc.Checkout(c.Request().Context(), req)
	if err != nil {
		return err
	}

	h.log.InfoContext(c.Request().Context(), "checkout accepted", "order_number", resp.OrderNumber)
	return c.JSON(http.StatusCreated, resp)
}

// orderDetail serves the guest's own order page.
//
// This is polled every few seconds while an order is pending, so it must stay
// what it is: a couple of indexed reads, no provider call, no writes.
func (h *Handler) orderDetail(c echo.Context) error {
	detail, err := h.public.OrderByNumber(c.Request().Context(), c.Param("orderNumber"))
	if err != nil {
		return err
	}

	// Live payment status must never be served from a cache.
	c.Response().Header().Set("Cache-Control", "no-store")
	return c.JSON(http.StatusOK, detail)
}

// qrImage renders the order's QRIS payload as a PNG on demand.
//
// Nothing is stored: this MVP has no object storage, so the image is generated
// from orders.payment_qr_string per request, the same way ticket QR codes are
// generated at PDF render time (Constitution, Technology Stack Requirements).
func (h *Handler) qrImage(c echo.Context) error {
	payload, err := h.public.PaymentQRPayload(c.Request().Context(), c.Param("orderNumber"))
	if err != nil {
		return err
	}

	png, err := qrcode.Encode(payload, qrcode.Medium, qrImageSize)
	if err != nil {
		return err
	}

	// The payload cannot change while the order is payable, but a short TTL keeps
	// a code that has just expired from lingering in a browser cache.
	c.Response().Header().Set("Cache-Control", "private, max-age=60")
	return c.Blob(http.StatusOK, "image/png", png)
}

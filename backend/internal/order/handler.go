package order

import (
	"net/http"

	"github.com/labstack/echo/v4"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"

	"github.com/manjo/ticketing/backend/pkg/httpx"
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
//
// The static /ticket/... prefixes win over event's parameterized
// GET /ticket/:event_id (Echo routes static segments first).
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
	// Spec 008 booking surface. Book itself is mounted separately behind a
	// per-IP limiter (RegisterBookRoute) — it creates rows and holds quota. The
	// pre-008 paths (POST /checkout, GET /orders/:orderNumber) are gone, not
	// aliased (spec clarification 2026-08-05).
	g.POST("/ticket/terms-condition/:order_id", h.recordAgreement)
	g.GET("/ticket/order/:order_id", h.ticketOrderDetail)
	// The gender master list the registration forms build their options from
	// (clarified 2026-08-05).
	g.GET("/ticket/genders", h.genders)
	// The QR render moved into the /ticket/order namespace with the rest of the
	// 008 guest surface (binary, no envelope).
	g.GET("/ticket/order/:order_id/qris.png", h.qrImage)
}

// RegisterCheckoutRoutes mounts the payment-starting calls on the composition
// root's payment-refresh limiter group: each costs an outbound provider
// round-trip, so unlike the polled reads they are throttled per IP.
func (h *Handler) RegisterCheckoutRoutes(g *echo.Group) {
	g.POST("/ticket/checkout/:order_id", h.checkoutOrder)
}

func (h *Handler) checkoutOrder(c echo.Context) error {
	var req CheckoutFormsRequest
	if err := c.Bind(&req); err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}

	resp, err := h.svc.CheckoutOrder(c.Request().Context(), c.Param("order_id"), req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, resp)
}

// RegisterBookRoute mounts POST /ticket/book on its own group so the
// composition root can rate limit it per IP without throttling the polled
// order reads.
func (h *Handler) RegisterBookRoute(g *echo.Group) {
	g.POST("/ticket/book", h.book)
}

func (h *Handler) book(c echo.Context) error {
	var req BookRequest
	if err := c.Bind(&req); err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}

	resp, err := h.svc.Book(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusCreated, resp)
}

func (h *Handler) recordAgreement(c echo.Context) error {
	var req AgreementRequest
	if err := c.Bind(&req); err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}

	if err := h.svc.RecordAgreement(c.Request().Context(), c.Param("order_id"), req); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, nil)
}

// ticketOrderDetail serves the 008 guest order read. Polled while pending, so
// it stays a few indexed reads with no provider call and no writes.
func (h *Handler) genders(c echo.Context) error {
	options, err := h.svc.Genders(c.Request().Context())
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, options)
}

func (h *Handler) ticketOrderDetail(c echo.Context) error {
	detail, err := h.public.TicketOrderByNumber(c.Request().Context(), c.Param("order_id"))
	if err != nil {
		return err
	}
	c.Response().Header().Set("Cache-Control", "no-store")
	return httpx.Respond(c, http.StatusOK, detail)
}

// qrImage renders the order's QRIS payload as a PNG on demand.
//
// Nothing is stored: this MVP has no object storage, so the image is generated
// from orders.payment_qr_string per request, the same way ticket QR codes are
// generated at PDF render time (Constitution, Technology Stack Requirements).
func (h *Handler) qrImage(c echo.Context) error {
	payload, err := h.public.PaymentQRPayload(c.Request().Context(), c.Param("order_id"))
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

package notification

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"

	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// Handler exposes the admin resend action over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler builds the notification HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterAdminRoutes mounts the resend endpoint. The caller applies the JWT
// middleware to the group.
func (h *Handler) RegisterAdminRoutes(g *echo.Group) {
	g.POST("/admin/orders/:id/resend-email", h.resend)
}

// RegisterPublicRoutes mounts the guest-facing resend. The caller supplies the
// rate-limiting middleware, which is not optional: this endpoint sends mail
// without authentication, so an unbounded one is a way to flood a buyer's inbox.
func (h *Handler) RegisterPublicRoutes(g *echo.Group, mw ...echo.MiddlewareFunc) {
	g.POST("/ticket/resend-email", h.resendPublic, mw...)
}

// resendPublic re-sends a guest their own tickets, identified only by the order
// number in the body's order_id field.
//
// Two rules shape it. The body carries nothing but the order number — no address
// field is read, so no caller can redirect the mail to an address of their
// choosing; the destination is always the buyer's stored address (spec FR-024).
// And every outcome short of a rate-limit answers 202 with the same body, so the
// endpoint cannot be used to discover which order numbers exist (spec FR-026); a
// 404 here would be an enumeration oracle.
//
// The cost of that silence is that a mistyped number gets no correction. That is
// acceptable because the button is only ever reached from the guest's own
// confirmation screen, which fills the number in for them.
func (h *Handler) resendPublic(c echo.Context) error {
	ctx := c.Request().Context()

	// Every return below funnels through this, so no branch can accidentally
	// answer differently and give the outcome away. A malformed body lands here
	// too: a parse error is as silent as an unknown order.
	accepted := func() error {
		return httpx.Respond(c, http.StatusAccepted, PublicResendResponse{Message: PublicResendMessage})
	}

	var req PublicResendRequest
	if err := c.Bind(&req); err != nil || req.OrderID == "" {
		return accepted()
	}

	orderID, err := h.svc.orders.OrderIDByNumber(ctx, req.OrderID)
	if err != nil {
		// Includes "no such order". Swallowed on purpose — see above.
		return accepted()
	}

	// SendTicketEmail already refuses anything that is not PAID, so an unpaid or
	// cancelled order sends nothing and still answers identically.
	if _, err := h.svc.SendTicketEmail(ctx, orderID); err != nil {
		h.svc.log.WarnContext(ctx, "public ticket email resend failed",
			"order_number", req.OrderID, "error", err.Error())
		return accepted()
	}

	return accepted()
}

func (h *Handler) resend(c echo.Context) error {
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation, "id must be a UUID.")
	}

	ctx := c.Request().Context()

	// Read the order first so a missing order is a 404 before any rendering
	// work happens.
	if _, err := h.svc.orders.OrderForDelivery(ctx, orderID); err != nil {
		return err
	}

	// The recipient comes from the send itself rather than being re-derived here,
	// so what the admin is shown is exactly the address that was mailed.
	recipient, err := h.svc.SendTicketEmail(ctx, orderID)
	if err != nil {
		return err
	}

	return httpx.Respond(c, http.StatusOK, ResendResponse{
		Message: "Email resent",
		SentTo:  recipient,
	})
}

package notification

import (
	"math"
	"net/http"
	"time"

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
// cooldown, which is not optional: this endpoint sends mail without
// authentication, so an unbounded one is a way to flood a buyer's inbox.
//
// It is a dependency rather than middleware because the handler needs the number
// the cooldown holds — the remaining seconds go on the accepted answer as well as
// the refused one — and because the body must be understood before any allowance
// is spent, which is the one thing a middleware cannot arrange (FR-021j,
// FR-021k).
// The cooldown is closed over rather than stored on the Handler: the admin resend
// registered above is authenticated and unlimited, and a shared field would leave
// it holding a nil nobody needs.
func (h *Handler) RegisterPublicRoutes(g *echo.Group, cooldown *httpx.Cooldown) {
	g.POST("/ticket/resend-email", func(c echo.Context) error {
		return h.resendPublic(c, cooldown)
	})
}

// resendPublic re-sends a guest their own tickets, identified only by the order
// number in the body's order_id field.
//
// Three rules shape it, and the order of the steps below is the third one.
//
// The body carries nothing but the order number — no address field is read, so no
// caller can redirect the mail to an address of their choosing; the destination is
// always the buyer's stored address (spec 008 FR-024).
//
// Every outcome that names an order answers 202 with the same bytes, so the
// endpoint cannot be used to discover which order numbers exist (spec 008 FR-026);
// a 404 here would be an enumeration oracle. A body that names no order at all is
// outside that rule (FR-021l): refusing it discloses nothing, and answering it as
// "accepted" tells a caller their mail is on its way when nothing was sent.
//
// And the cooldown is consulted after the body and before the lookup. After,
// because a request nobody can key must cost nobody anything (FR-021k). Before,
// because every attempt that names an order spends the window whatever it finds
// there, or probing order numbers would be free (FR-021m).
func (h *Handler) resendPublic(c echo.Context, cooldown *httpx.Cooldown) error {
	ctx := c.Request().Context()

	var req PublicResendRequest
	if err := c.Bind(&req); err != nil || req.OrderID == "" {
		// No allowance is spent here and no bucket is touched. The endpoint holds
		// no shared bucket for requests it could not key: one of those makes a
		// single malformed request a refusal for every other guest.
		h.svc.log.InfoContext(ctx, "public ticket email resend rejected", "outcome", "body unreadable")
		return apperr.BadRequest(apperr.CodeValidation, "order_id is required.")
	}

	allowed, retryAfter := cooldown.Take(req.OrderID)
	seconds := wholeSeconds(retryAfter)
	if !allowed {
		h.svc.log.InfoContext(ctx, "public ticket email resend refused",
			"order_number", req.OrderID, "outcome", "cooldown", "retry_after_seconds", seconds)
		return apperr.New(http.StatusTooManyRequests, apperr.CodeRateLimited,
			PublicResendRateLimitedMessage).
			WithData(PublicRetryAfter{RetryAfterSeconds: seconds})
	}

	// Every return below funnels through this, so no branch can accidentally
	// answer differently and give the outcome away.
	accepted := func() error {
		return httpx.Respond(c, http.StatusAccepted, PublicResendResponse{
			Message:           PublicResendMessage,
			RetryAfterSeconds: seconds,
		})
	}

	orderID, err := h.svc.orders.OrderIDByNumber(ctx, req.OrderID)
	if err != nil {
		// Includes "no such order". Silent to the caller, never to the operator:
		// an outcome that leaves no trace cannot be told apart from a press that
		// never happened (FR-021n).
		h.svc.log.InfoContext(ctx, "public ticket email resend accepted",
			"order_number", req.OrderID, "outcome", "order unknown")
		return accepted()
	}

	// SendTicketEmail already refuses anything that is not PAID, so an unpaid or
	// cancelled order sends nothing and still answers identically.
	if _, err := h.svc.SendTicketEmail(ctx, orderID); err != nil {
		h.svc.log.WarnContext(ctx, "public ticket email resend accepted",
			"order_number", req.OrderID, "outcome", "send failed", "error", err.Error())
		return accepted()
	}

	h.svc.log.InfoContext(ctx, "public ticket email resend accepted",
		"order_number", req.OrderID, "outcome", "delivered")
	return accepted()
}

// wholeSeconds rounds a remaining wait up, so a caller told to wait n seconds is
// never refused again at n. Rounding down would hand every guest one guaranteed
// wasted press.
func wholeSeconds(d time.Duration) int {
	if d <= 0 {
		return 0
	}
	return int(math.Ceil(d.Seconds()))
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

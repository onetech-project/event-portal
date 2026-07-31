package notification

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
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

func (h *Handler) resend(c echo.Context) error {
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation, "id must be a UUID.")
	}

	ctx := c.Request().Context()

	// Read the order first so the response can report where the email went, and so
	// a missing order is a 404 before any rendering work happens.
	order, err := h.svc.orders.OrderForDelivery(ctx, orderID)
	if err != nil {
		return err
	}

	if err := h.svc.SendTicketEmail(ctx, orderID); err != nil {
		return err
	}

	return c.JSON(http.StatusOK, ResendResponse{
		Message: "Email resent",
		SentTo:  order.BuyerEmail,
	})
}

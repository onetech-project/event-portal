package order

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// AdminHandler exposes the read-only admin views. These live in the order domain
// because it owns the orders and attendees tables (specs/002 plan.md).
type AdminHandler struct {
	svc *AdminService
}

// NewAdminHandler builds the admin read handler.
func NewAdminHandler(svc *AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// RegisterAdminRoutes mounts the read-only views. The caller applies the JWT
// middleware to the group.
func (h *AdminHandler) RegisterAdminRoutes(g *echo.Group) {
	g.GET("/admin/orders", h.listOrders)
	g.GET("/admin/attendees", h.listAttendees)
}

func (h *AdminHandler) listOrders(c echo.Context) error {
	filter := OrderFilter{}

	if status := strings.TrimSpace(c.QueryParam("status")); status != "" {
		if !isKnownOrderStatus(status) {
			return apperr.BadRequest(apperr.CodeValidation,
				"status must be one of PENDING, PAID, CANCELLED, or EXPIRED.")
		}
		filter.Status = &status
	}

	eventID, err := optionalUUIDParam(c, "event_id")
	if err != nil {
		return err
	}
	filter.EventID = eventID

	orders, err := h.svc.ListOrders(c.Request().Context(), filter)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, orders)
}

func (h *AdminHandler) listAttendees(c echo.Context) error {
	orderID, err := optionalUUIDParam(c, "order_id")
	if err != nil {
		return err
	}
	eventID, err := optionalUUIDParam(c, "event_id")
	if err != nil {
		return err
	}

	attendees, err := h.svc.ListAttendees(c.Request().Context(),
		AttendeeFilter{OrderID: orderID, EventID: eventID})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, attendees)
}

func optionalUUIDParam(c echo.Context, name string) (*uuid.UUID, error) {
	raw := strings.TrimSpace(c.QueryParam(name))
	if raw == "" {
		return nil, nil
	}

	parsed, err := uuid.Parse(raw)
	if err != nil {
		return nil, apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			name+" must be a UUID.")
	}
	return &parsed, nil
}

func isKnownOrderStatus(status string) bool {
	switch status {
	case "PENDING", "PAID", "CANCELLED", "EXPIRED":
		return true
	default:
		return false
	}
}

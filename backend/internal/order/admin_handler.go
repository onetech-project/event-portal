package order

import (
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"

	"github.com/manjo/ticketing/backend/pkg/httpx"
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
	// Fee master CRUD (clarified 2026-08-05): what booking applies to the
	// subtotal on every new order.
	g.GET("/admin/fees", h.listFees)
	g.POST("/admin/fees", h.createFee)
	g.PUT("/admin/fees/:id", h.updateFee)
	g.DELETE("/admin/fees/:id", h.deleteFee)
}

func (h *AdminHandler) listFees(c echo.Context) error {
	fees, err := h.svc.Fees(c.Request().Context(), httpx.BindPage(c))
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, fees)
}

func (h *AdminHandler) createFee(c echo.Context) error {
	var req FeeRequest
	if err := c.Bind(&req); err != nil {
		return apperr.BadRequest(apperr.CodeValidation, "Invalid request body.")
	}
	fee, err := h.svc.CreateFee(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusCreated, fee)
}

func (h *AdminHandler) updateFee(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperr.BadRequest(apperr.CodeValidation, "id must be a valid UUID.")
	}
	var req FeeRequest
	if err := c.Bind(&req); err != nil {
		return apperr.BadRequest(apperr.CodeValidation, "Invalid request body.")
	}
	fee, err := h.svc.UpdateFee(c.Request().Context(), id, req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, fee)
}

func (h *AdminHandler) deleteFee(c echo.Context) error {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return apperr.BadRequest(apperr.CodeValidation, "id must be a valid UUID.")
	}
	if err := h.svc.DeleteFee(c.Request().Context(), id); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, map[string]string{"message": "Fee deleted."})
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
	// Paging is corrected rather than refused, unlike the filters above: a bad
	// status names something that does not exist, while a bad page names a
	// position that drifted (spec 021 research R8).
	filter.Page = httpx.BindPage(c)

	orders, err := h.svc.ListOrders(c.Request().Context(), filter)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, orders)
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
		AttendeeFilter{OrderID: orderID, EventID: eventID, Page: httpx.BindPage(c)})
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, attendees)
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

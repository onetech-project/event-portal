package event

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// RegisterAdminRoutes mounts the admin CRUD surface. The caller applies the JWT
// middleware to the group.
//
// Ticket-type routes are FLAT (/admin/ticket-types with event_id in the query or
// body), matching PRD.md §1.5, which is LOCKED. No nested
// /admin/events/:id/ticket-types route exists.
func (h *Handler) RegisterAdminRoutes(g *echo.Group) {
	g.GET("/admin/events", h.adminListEvents)
	g.POST("/admin/events", h.adminCreateEvent)
	g.GET("/admin/events/:id", h.adminGetEvent)
	g.PUT("/admin/events/:id", h.adminUpdateEvent)
	g.DELETE("/admin/events/:id", h.adminDeleteEvent)

	g.GET("/admin/ticket-types", h.adminListTicketTypes)
	g.POST("/admin/ticket-types", h.adminCreateTicketType)
	g.GET("/admin/ticket-types/:id", h.adminGetTicketType)
	g.PUT("/admin/ticket-types/:id", h.adminUpdateTicketType)
	g.DELETE("/admin/ticket-types/:id", h.adminDeleteTicketType)
}

// --- Events ---------------------------------------------------------------

func (h *Handler) adminListEvents(c echo.Context) error {
	events, err := h.svc.ListEvents(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, events)
}

func (h *Handler) adminCreateEvent(c echo.Context) error {
	req, err := bindEventRequest(c)
	if err != nil {
		return err
	}

	created, err := h.svc.CreateEvent(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, created)
}

func (h *Handler) adminGetEvent(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}

	detail, err := h.svc.GetEventDetail(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, detail)
}

func (h *Handler) adminUpdateEvent(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	req, err := bindEventRequest(c)
	if err != nil {
		return err
	}

	updated, err := h.svc.UpdateEvent(c.Request().Context(), id, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, updated)
}

func (h *Handler) adminDeleteEvent(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}

	if err := h.svc.DeleteEvent(c.Request().Context(), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Ticket types ---------------------------------------------------------

func (h *Handler) adminListTicketTypes(c echo.Context) error {
	raw := c.QueryParam("event_id")
	if raw == "" {
		return apperr.BadRequest(apperr.CodeValidation, "event_id is required.")
	}
	eventID, err := uuid.Parse(raw)
	if err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"event_id must be a UUID.")
	}

	types, err := h.svc.ListTicketTypes(c.Request().Context(), eventID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, types)
}

func (h *Handler) adminCreateTicketType(c echo.Context) error {
	req, err := bindTicketTypeRequest(c)
	if err != nil {
		return err
	}

	created, err := h.svc.CreateTicketType(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, created)
}

func (h *Handler) adminGetTicketType(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}

	got, err := h.svc.GetTicketType(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, got)
}

func (h *Handler) adminUpdateTicketType(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}
	req, err := bindTicketTypeRequest(c)
	if err != nil {
		return err
	}

	updated, err := h.svc.UpdateTicketType(c.Request().Context(), id, req)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, updated)
}

func (h *Handler) adminDeleteTicketType(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}

	if err := h.svc.DeleteTicketType(c.Request().Context(), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

// --- Binding helpers ------------------------------------------------------

func pathUUID(c echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			name+" must be a UUID.")
	}
	return id, nil
}

func bindEventRequest(c echo.Context) (EventRequest, error) {
	var req EventRequest
	if err := c.Bind(&req); err != nil {
		return EventRequest{}, apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}
	return req, nil
}

func bindTicketTypeRequest(c echo.Context) (TicketTypeRequest, error) {
	var req TicketTypeRequest
	if err := c.Bind(&req); err != nil {
		return TicketTypeRequest{}, apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}
	return req, nil
}

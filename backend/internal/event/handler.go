package event

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// Handler exposes the event domain over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler builds the event HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterPublicRoutes mounts the guest-facing catalog endpoints. They are
// deliberately unauthenticated (Constitution Principle VI).
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
	g.GET("/events", h.listEvents)
	g.GET("/events/:slug", h.getEventBySlug)
}

func (h *Handler) listEvents(c echo.Context) error {
	events, err := h.svc.ListPublishedEvents(c.Request().Context())
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, events)
}

func (h *Handler) getEventBySlug(c echo.Context) error {
	detail, err := h.svc.GetPublishedEventBySlug(c.Request().Context(), c.Param("slug"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, detail)
}

package event

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// Handler exposes the event domain over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler builds the event HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterPublicRoutes mounts the guest-facing catalog endpoints on the
// user-mandated paths (clarification 2026-08-05). They are deliberately
// unauthenticated (Constitution Principle VI).
//
// :id and :event_id both carry the event's public identifier — its slug.
// Detail is content-only; tickets and packages have their own endpoints so the
// selection page is the only thing that pays for live-quota reads.
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
	g.GET("/event", h.listEvents)
	g.GET("/event/:id", h.getEventBySlug)
	g.GET("/ticket/:event_id", h.listTicketTypes)
	g.GET("/packages/:event_id", h.listPackages)
	// Static "terms-condition" segment wins over :event_id (Echo routes static
	// before param), so an event slugged "terms-condition" cannot shadow it.
	g.GET("/ticket/terms-condition/:event_id", h.getTerms)
}

func (h *Handler) getTerms(c echo.Context) error {
	terms, err := h.svc.TermsForEventSlug(c.Request().Context(), c.Param("event_id"))
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, terms)
}

func (h *Handler) listEvents(c echo.Context) error {
	events, err := h.svc.ListPublishedEvents(c.Request().Context())
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, events)
}

func (h *Handler) getEventBySlug(c echo.Context) error {
	detail, err := h.svc.GetPublishedEventBySlug(c.Request().Context(), c.Param("id"))
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, detail)
}

func (h *Handler) listTicketTypes(c echo.Context) error {
	types, err := h.svc.TicketTypesForEventSlug(c.Request().Context(), c.Param("event_id"))
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, types)
}

func (h *Handler) listPackages(c echo.Context) error {
	packages, err := h.svc.PackagesForEventSlug(c.Request().Context(), c.Param("event_id"))
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, packages)
}

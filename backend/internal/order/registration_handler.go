package order

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// RegisterRegistrationReadRoute mounts the form's prerequisites read.
//
// Mounted on the UNTHROTTLED public group: it creates nothing, holds nothing and
// sends nothing, so it does not belong behind the submit's deliberately tight
// limiter — the same judgement RegisterAvailabilityRoute already documents for
// its own read-only endpoint. A guest who mistypes and reloads must not be
// throttled out of the page they are trying to reach.
//
// "register" is a static segment and Echo routes static before param, so this
// cannot be shadowed by the event domain's GET /ticket/:event_id — the same
// protection /ticket/terms-condition/:event_id already relies on.
func (h *Handler) RegisterRegistrationReadRoute(g *echo.Group) {
	g.GET("/ticket/register/:ticket_type_id", h.registrationPrereqs)
}

// RegisterRegistrationRoute mounts the submit on its own per-IP limiter group.
//
// Separate from the read because the cost is different in kind: this deducts
// quota AND sends real mail, on an unauthenticated surface (Principle IX).
func (h *Handler) RegisterRegistrationRoute(g *echo.Group) {
	g.POST("/ticket/register/:ticket_type_id", h.register)
}

func (h *Handler) registrationPrereqs(c echo.Context) error {
	ticketTypeID, err := uuid.Parse(c.Param("ticket_type_id"))
	if err != nil {
		// A malformed id is answered exactly as an unknown one. Anything else
		// would tell a caller which of their guesses were well-formed (FR-012).
		return apperr.NotFound(apperr.CodeTicketTypeNotFound, registrationRefusal)
	}

	prereqs, err := h.svc.RegistrationPrerequisites(c.Request().Context(),
		c.QueryParam("slug"), ticketTypeID)
	if err != nil {
		return err
	}
	// Live availability decided this response; a cached copy could offer a form
	// for a registration that has since closed.
	c.Response().Header().Set("Cache-Control", "no-store")
	return httpx.Respond(c, http.StatusOK, prereqs)
}

func (h *Handler) register(c echo.Context) error {
	ticketTypeID, err := uuid.Parse(c.Param("ticket_type_id"))
	if err != nil {
		return apperr.NotFound(apperr.CodeTicketTypeNotFound, registrationRefusal)
	}

	var req RegistrationRequest
	if err := c.Bind(&req); err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}

	// Every gate the GET applied is re-applied by the service on this path too.
	// Hiding a form is not enforcement: this must refuse a caller who never
	// loaded the page (spec 022 US3 scenario 2).
	if err := h.svc.RegisterFree(c.Request().Context(), ticketTypeID, req); err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusCreated, RegistrationResponse{Registered: true})
}

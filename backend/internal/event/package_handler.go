package event

import (
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"

	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// maxPackageBody bounds what the raw-body reads below will accept. A package
// carries a handful of fields and a short component list; anything larger is a
// mistake or an attack, and reading it unbounded would let one request occupy
// memory proportional to whatever the client chose to send.
const maxPackageBody = 1 << 20 // 1 MiB

// RegisterPackageRoutes mounts the admin package surface. The caller applies the
// JWT middleware to the group.
//
// Routes are FLAT (/admin/packages with event_id in the query or body), matching
// the convention PRD.md §1.5 locks in for ticket types. Composition has no
// independent identity, so it is carried inside the package body rather than
// given a sub-resource of its own.
func (h *Handler) RegisterPackageRoutes(g *echo.Group) {
	g.GET("/admin/packages", h.adminListPackages)
	g.POST("/admin/packages", h.adminCreatePackage)
	g.GET("/admin/packages/:id", h.adminGetPackage)
	g.PUT("/admin/packages/:id", h.adminUpdatePackage)
	g.DELETE("/admin/packages/:id", h.adminDeletePackage)
	g.GET("/admin/packages/:id/availability", h.adminPackageAvailability)
}

func (h *Handler) adminListPackages(c echo.Context) error {
	raw := c.QueryParam("event_id")
	if raw == "" {
		return apperr.BadRequest(apperr.CodeValidation, "event_id is required.")
	}
	eventID, err := uuid.Parse(raw)
	if err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"event_id must be a UUID.")
	}

	packages, err := h.svc.ListPackagesByEvent(c.Request().Context(), eventID)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, packages)
}

func (h *Handler) adminGetPackage(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}

	pkg, err := h.svc.GetPackageAdmin(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, pkg)
}

func (h *Handler) adminCreatePackage(c echo.Context) error {
	body, err := readPackageBody(c)
	if err != nil {
		return err
	}

	created, err := h.svc.CreatePackage(c.Request().Context(), body)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusCreated, created)
}

func (h *Handler) adminUpdatePackage(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}

	body, err := readPackageBody(c)
	if err != nil {
		return err
	}

	updated, err := h.svc.UpdatePackage(c.Request().Context(), id, body)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, updated)
}

func (h *Handler) adminDeletePackage(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}

	if err := h.svc.DeletePackage(c.Request().Context(), id); err != nil {
		return err
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) adminPackageAvailability(c echo.Context) error {
	id, err := pathUUID(c, "id")
	if err != nil {
		return err
	}

	view, err := h.svc.PackageAvailabilityDiagnostic(c.Request().Context(), id)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, view)
}

// readPackageBody hands the service the raw bytes rather than a bound struct.
//
// Binding first would silently drop any field the struct does not declare, which
// is exactly the wrong behaviour for a quota-like field: FR-036 requires such a
// body be REJECTED, not ignored. The service parses the raw JSON so that rule
// lives beside the rest of the validation and stays directly testable.
func readPackageBody(c echo.Context) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, maxPackageBody+1))
	if err != nil {
		return nil, apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be read.")
	}
	if len(body) > maxPackageBody {
		return nil, apperr.BadRequest(apperr.CodeValidation, "The request body is too large.")
	}
	return body, nil
}

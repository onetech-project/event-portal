package ticket

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// Handler exposes the ticket domain over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler builds the ticket HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterAdminRoutes mounts the door-side endpoints. The caller applies the JWT
// middleware to the group.
//
// POST /admin/tickets/:code/use is a deliberate, documented addition to PRD §1.5's
// locked API list (see specs/003 contracts/api.md): validate must stay
// side-effect-free because the same code is routinely scanned more than once at a
// door, so the irreversible ACTIVE -> USED transition needs its own endpoint.
func (h *Handler) RegisterAdminRoutes(g *echo.Group) {
	g.POST("/admin/tickets/validate", h.validate)
	g.POST("/admin/tickets/:code/use", h.markUsed)
}

// RegisterPublicRoutes mounts the unauthenticated ticket lookup. The caller
// applies the per-IP rate limiter to the group (spec FR-020).
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
	g.GET("/tickets/:code", h.lookup)
}

func (h *Handler) validate(c echo.Context) error {
	var req ValidateRequest
	if err := c.Bind(&req); err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}
	if strings.TrimSpace(req.Code) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "code is required.")
	}

	result, err := h.svc.Validate(c.Request().Context(), req.Code)
	if err != nil {
		return err
	}
	// Always 200: Valid, Already Used, and Invalid are all normal answers at a
	// door, and the scanner UI branches on `result`, not on the status code.
	return c.JSON(http.StatusOK, result)
}

func (h *Handler) markUsed(c echo.Context) error {
	resp, err := h.svc.MarkUsed(c.Request().Context(), c.Param("code"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resp)
}

func (h *Handler) lookup(c echo.Context) error {
	got, err := h.svc.LookupPublic(c.Request().Context(), c.Param("code"))
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, got)
}

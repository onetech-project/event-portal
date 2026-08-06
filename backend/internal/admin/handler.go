package admin

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"

	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// Handler exposes admin authentication over HTTP.
type Handler struct {
	svc *Service
}

// NewHandler builds the admin HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts the login endpoint. It is deliberately registered without
// the auth middleware — it is the one /admin/* route that issues the token every
// other one requires.
func (h *Handler) RegisterRoutes(g *echo.Group) {
	g.POST("/admin/login", h.login)
}

func (h *Handler) login(c echo.Context) error {
	var req LoginRequest
	if err := c.Bind(&req); err != nil {
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}

	resp, err := h.svc.Login(c.Request().Context(), req)
	if err != nil {
		return err
	}
	return httpx.Respond(c, http.StatusOK, resp)
}

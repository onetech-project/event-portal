package order

import (
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// Handler exposes the order domain over HTTP.
type Handler struct {
	svc *Service
	log *logger.Logger
}

// NewHandler builds the order HTTP handler.
func NewHandler(svc *Service, log *logger.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// RegisterPublicRoutes mounts the guest checkout endpoint. No authentication is
// required anywhere in the purchase flow (Constitution Principle VI).
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
	g.POST("/checkout", h.checkout)
}

func (h *Handler) checkout(c echo.Context) error {
	var req CheckoutRequest
	if err := c.Bind(&req); err != nil {
		// Bind failures are malformed JSON or a badly-typed field (for example a
		// ticket_type_id that is not a UUID) — a client problem, not a server one.
		return apperr.Wrap(err, http.StatusBadRequest, apperr.CodeValidation,
			"The request body could not be parsed.")
	}

	resp, err := h.svc.Checkout(c.Request().Context(), req)
	if err != nil {
		return err
	}

	h.log.InfoContext(c.Request().Context(), "checkout accepted", "order_number", resp.OrderNumber)
	return c.JSON(http.StatusCreated, resp)
}

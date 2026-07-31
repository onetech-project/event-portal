package admin

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// contextKeyName is the Echo context slot holding the verified claims. It is
// unexported so no other package can plant a forged value and impersonate an admin.
const contextKeyName = "admin.claims"

const bearerPrefix = "bearer "

// RequireAuth guards every /admin/* route except login. It verifies the bearer
// token and stashes the resulting claims on the request context.
func RequireAuth(issuer *TokenIssuer) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get(echo.HeaderAuthorization)
			if len(header) < len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
				return apperr.Unauthorized(apperr.CodeUnauthorized, "A bearer token is required.")
			}

			claims, err := issuer.Verify(strings.TrimSpace(header[len(bearerPrefix):]))
			if err != nil {
				return apperr.Wrap(err, http.StatusUnauthorized, apperr.CodeUnauthorized,
					"The access token is invalid or has expired.")
			}

			c.Set(contextKeyName, claims)
			return next(c)
		}
	}
}

// ClaimsFromContext returns the authenticated admin's claims, or nil if the request
// did not pass through RequireAuth.
func ClaimsFromContext(c echo.Context) *Claims {
	claims, ok := c.Get(contextKeyName).(*Claims)
	if !ok {
		return nil
	}
	return claims
}

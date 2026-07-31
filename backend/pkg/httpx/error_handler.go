// Package httpx holds transport-level helpers shared by every domain's handlers.
package httpx

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/logger"
)

// ErrorHandler renders every error in the single envelope the API contracts
// promise: {"error_code": "...", "message": "..."}. Unrecognized errors become an
// opaque 500 so internal detail (driver messages, SQL, credentials) never reaches a
// client; the full cause is logged instead.
func ErrorHandler(log *logger.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}

		appErr := toAppError(err)

		// Context-aware logging so every line carries the request's trace_id and
		// can be pivoted to its trace in Tempo.
		ctx := c.Request().Context()

		if appErr.HTTPStatus >= http.StatusInternalServerError {
			log.ErrorContext(ctx, "request failed",
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"status", appErr.HTTPStatus,
				"error_code", appErr.Code,
				"error", err.Error(),
			)
		} else {
			log.WarnContext(ctx, "request rejected",
				"method", c.Request().Method,
				"path", c.Request().URL.Path,
				"status", appErr.HTTPStatus,
				"error_code", appErr.Code,
			)
		}

		var writeErr error
		if c.Request().Method == http.MethodHead {
			writeErr = c.NoContent(appErr.HTTPStatus)
		} else {
			writeErr = c.JSON(appErr.HTTPStatus, appErr.Response())
		}
		if writeErr != nil {
			log.ErrorContext(ctx, "failed to write error response", "error", writeErr.Error())
		}
	}
}

func toAppError(err error) *apperr.Error {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr
	}

	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		return apperr.New(httpErr.Code, codeForStatus(httpErr.Code), messageForStatus(httpErr))
	}

	return apperr.Internal(apperr.CodeInternal, "An unexpected error occurred.")
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return apperr.CodeValidation
	case http.StatusUnauthorized:
		return apperr.CodeUnauthorized
	case http.StatusNotFound:
		return apperr.CodeNotFound
	case http.StatusTooManyRequests:
		return apperr.CodeRateLimited
	default:
		if status >= http.StatusInternalServerError {
			return apperr.CodeInternal
		}
		return apperr.CodeValidation
	}
}

func messageForStatus(httpErr *echo.HTTPError) string {
	if status := httpErr.Code; status >= http.StatusInternalServerError {
		return "An unexpected error occurred."
	}
	if msg, ok := httpErr.Message.(string); ok && msg != "" {
		return msg
	}
	return http.StatusText(httpErr.Code)
}

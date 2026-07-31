// Package apperr defines the single structured error type every domain returns to
// its HTTP handler, plus the machine-readable error codes the API contracts in
// specs/*/contracts/api.md promise to clients.
package apperr

import (
	"fmt"
	"net/http"
)

// Machine-readable error codes. These strings are part of the public API contract —
// clients branch on them, so they must not be renamed casually.
const (
	// Guest purchase flow (specs/001).
	CodeValidation              = "VALIDATION_ERROR"
	CodeInsufficientQuota       = "INSUFFICIENT_QUOTA"
	CodeAttendeeCountMismatch   = "ATTENDEE_COUNT_MISMATCH"
	CodeTicketTypeNotOnSale     = "TICKET_TYPE_NOT_ON_SALE"
	CodeTicketTypeNotFound      = "TICKET_TYPE_NOT_FOUND"
	CodeEventNotFound           = "EVENT_NOT_FOUND"
	CodePaymentInitiationFailed = "PAYMENT_INITIATION_FAILED"
	CodeTicketNotFound          = "TICKET_NOT_FOUND"
	CodeRateLimited             = "RATE_LIMITED"
	CodeInvalidSignature        = "INVALID_SIGNATURE"

	// Admin management (specs/002).
	CodeInvalidCredentials  = "INVALID_CREDENTIALS"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeSlugNotUnique       = "SLUG_NOT_UNIQUE"
	CodeInvalidDateRange    = "INVALID_DATE_RANGE"
	CodeEventHasOrders      = "EVENT_HAS_ORDERS"
	CodeTicketTypeHasOrders = "TICKET_TYPE_HAS_ORDERS"

	// Admin ticket validation (specs/003).
	CodeAlreadyUsed   = "ALREADY_USED"
	CodeOrderNotPaid  = "ORDER_NOT_PAID"
	CodeOrderNotFound = "ORDER_NOT_FOUND"

	// Transport-level fallbacks used by the shared HTTP error handler.
	CodeNotFound = "NOT_FOUND"
	CodeInternal = "INTERNAL_ERROR"
)

// Error is a domain error carrying everything the HTTP layer needs to render a
// response without re-deriving it from the error text.
type Error struct {
	// HTTPStatus is the status the handler should emit.
	HTTPStatus int
	// Code is the stable, machine-readable identifier clients branch on.
	Code string
	// Message is the human-readable explanation shown to the caller.
	Message string
	// Err is the underlying cause, kept for logging and errors.Is/As. It is never
	// serialized — internal detail must not leak to clients.
	Err error
}

// Body is the exact JSON shape returned for any error response.
type Body struct {
	ErrorCode string `json:"error_code"`
	Message   string `json:"message"`
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap exposes the cause so errors.Is/errors.As traverse into it.
func (e *Error) Unwrap() error { return e.Err }

// Response renders the client-facing body, deliberately omitting the cause.
func (e *Error) Response() Body {
	return Body{ErrorCode: e.Code, Message: e.Message}
}

// New builds an Error with no underlying cause.
func New(status int, code, message string) *Error {
	return &Error{HTTPStatus: status, Code: code, Message: message}
}

// Wrap builds an Error that keeps err as its cause.
func Wrap(err error, status int, code, message string) *Error {
	return &Error{HTTPStatus: status, Code: code, Message: message, Err: err}
}

func BadRequest(code, message string) *Error {
	return New(http.StatusBadRequest, code, message)
}

func Unauthorized(code, message string) *Error {
	return New(http.StatusUnauthorized, code, message)
}

func NotFound(code, message string) *Error {
	return New(http.StatusNotFound, code, message)
}

func Conflict(code, message string) *Error {
	return New(http.StatusConflict, code, message)
}

func Internal(code, message string) *Error {
	return New(http.StatusInternalServerError, code, message)
}

func BadGateway(code, message string) *Error {
	return New(http.StatusBadGateway, code, message)
}

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
	// CodePaymentSessionDuplicate reports that the gateway had already issued a
	// code for this order's reference and will not issue another.
	//
	// It is separate from CodePaymentInitiationFailed because the guest's
	// instruction differs: a generic failure is worth retrying, this one never is.
	// No call returns an existing code, so the order can never be paid — its seats
	// are released and the guest must start again (FR-007d, FR-007e).
	CodePaymentSessionDuplicate = "PAYMENT_SESSION_DUPLICATE"
	CodeTicketNotFound          = "TICKET_NOT_FOUND"
	CodeRateLimited             = "RATE_LIMITED"
	// CodeInvalidSignature reports a notification this system could not
	// authenticate. Under the Manjo contract that means a missing or wrong bearer
	// token; the code keeps its name because clients branch on it.
	CodeInvalidSignature = "INVALID_SIGNATURE"

	// CodeTicketsUnavailable reports a redelivered notification that could not
	// settle its order because the ticket type no longer covers the order's hold.
	//
	// It is the only code in this registry whose HTTP status is a success. FR-019c
	// requires it: a non-200 would spend the gateway's three retries on an attempt
	// guaranteed to fail identically, and the notification would be lost at the end
	// of it. The status line therefore cannot carry the refusal, so the envelope's
	// numeric sub-code does (FR-019e) — see Numeric.
	CodeTicketsUnavailable = "TICKETS_UNAVAILABLE"

	// The rest of the 200 band: notifications this system recorded but
	// deliberately did not act on, each answered 200 because a retry cannot change
	// any of them (FR-012c). They carry a code rather than a bare success so the
	// caller learns what the operator already learns from the signal (FR-012g).
	CodeNotificationOrderUnknown   = "NOTIFICATION_ORDER_UNKNOWN"
	CodeNotificationNotDeposit     = "NOTIFICATION_NOT_DEPOSIT"
	CodeNotificationStatusUnknown  = "NOTIFICATION_STATUS_UNKNOWN"
	CodeNotificationContradiction  = "NOTIFICATION_CONTRADICTS_PAID"
	CodeNotificationOrderCancelled = "NOTIFICATION_ORDER_CANCELLED"

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

	// Ticket package bundles (specs/005).
	CodePackageNotFound          = "PACKAGE_NOT_FOUND"
	CodePackageNotOnSale         = "PACKAGE_NOT_ON_SALE"
	CodePackageUnavailable       = "PACKAGE_UNAVAILABLE"
	CodePackageHasOrders         = "PACKAGE_HAS_ORDERS"
	CodePackageCompositionLocked = "PACKAGE_COMPOSITION_LOCKED"

	// End-to-end purchase flow (specs/008).
	CodeTermsMissing          = "TERMS_MISSING"
	CodeTermsChanged          = "TERMS_CHANGED"
	CodeTermsNotAccepted      = "TERMS_NOT_ACCEPTED"
	CodeTermsNotRecorded      = "TERMS_NOT_RECORDED"
	CodePaymentAlreadyStarted = "PAYMENT_ALREADY_STARTED"
	CodePaymentNotStarted     = "PAYMENT_NOT_STARTED"
	CodeOrderExpired          = "ORDER_EXPIRED"
	CodeUnknownIcon           = "UNKNOWN_ICON"

	// Transport-level fallbacks used by the shared HTTP error handler.
	CodeNotFound = "NOT_FOUND"
	CodeInternal = "INTERNAL_ERROR"
)

// Numeric maps a (status, string code) pair onto the numeric registry the 008
// contract promises: HTTP status × 1000 + sub-code. Codes outside the registry
// fall back to status × 1000, which stays inside the scheme without inventing
// unregistered sub-codes. TERMS_MISSING is status-dependent by design: 404002
// when reading absent terms, 409001 when booking is refused over them.
func Numeric(status int, code string) int {
	switch code {
	case CodeValidation, CodeAttendeeCountMismatch, CodeInvalidDateRange, CodeTicketTypeNotOnSale,
		CodePackageNotOnSale, CodePackageUnavailable:
		return 400001
	case CodeInsufficientQuota:
		return 400002
	case CodeTermsNotAccepted:
		return 400003
	case CodeUnknownIcon:
		return 400004
	case CodeUnauthorized, CodeInvalidCredentials, CodeInvalidSignature:
		return 401001
	case CodeNotFound, CodeEventNotFound, CodeTicketTypeNotFound, CodeTicketNotFound,
		CodeOrderNotFound, CodePackageNotFound:
		return 404001
	case CodeTermsMissing:
		if status == http.StatusNotFound {
			return 404002
		}
		return 409001
	case CodeTermsChanged:
		return 409002
	case CodeTermsNotRecorded:
		return 409003
	case CodePaymentAlreadyStarted:
		return 409004
	case CodePaymentNotStarted:
		return 409005
	case CodeOrderExpired:
		return 410001
	case CodeRateLimited:
		return 429001
	case CodeInternal:
		return 500000
	case CodePaymentInitiationFailed:
		return 502001
	// A duplicate reference is a conflict, not a gateway fault: the gateway
	// answered correctly and the answer is final.
	case CodePaymentSessionDuplicate:
		return 409006
	// The 200 band: outcomes that must not be retried and so cannot use a failing
	// status line. Registering each is load-bearing rather than tidy — the default
	// below is status*1000, so an unregistered code on a 200 renders 200000,
	// byte-identical to httpx.SuccessCode. It would then announce itself as a
	// success, and no test asserting on the status line would catch it, because
	// 200 is correct in every one of these cases by design (FR-019e, FR-012g).
	case CodeTicketsUnavailable:
		return 200001
	case CodeNotificationOrderUnknown:
		return 200002
	case CodeNotificationNotDeposit:
		return 200003
	case CodeNotificationStatusUnknown:
		return 200004
	case CodeNotificationContradiction:
		return 200005
	case CodeNotificationOrderCancelled:
		return 200006
	default:
		return status * 1000
	}
}

// Error is a domain error carrying everything the HTTP layer needs to render a
// response without re-deriving it from the error text.
type Error struct {
	// HTTPStatus is the status the handler should emit.
	HTTPStatus int
	// Code is the stable, machine-readable identifier clients branch on. It is
	// mapped to the numeric envelope code via Numeric at render time.
	Code string
	// Message is the human-readable explanation shown to the caller.
	Message string
	// Data is optional client-facing detail carried in the envelope's data
	// field: a validation field map, or the current QR payload on
	// PAYMENT_ALREADY_STARTED. Nil for most errors.
	Data any
	// Err is the underlying cause, kept for logging and errors.Is/As. It is never
	// serialized — internal detail must not leak to clients.
	Err error
}

// Body is the exact JSON shape returned for any error response — the same
// {code, message, data} envelope successes use (clarification 2026-08-05).
type Body struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
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
	return Body{Code: Numeric(e.HTTPStatus, e.Code), Message: e.Message, Data: e.Data}
}

// WithData returns a copy of the error carrying client-facing detail in the
// envelope's data field.
func (e *Error) WithData(data any) *Error {
	clone := *e
	clone.Data = data
	return &clone
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

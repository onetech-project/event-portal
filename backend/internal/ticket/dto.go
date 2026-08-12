package ticket

import (
	"time"

	"github.com/google/uuid"
)

// Validation results returned by the admin door flow.
//
// NotYetValid and Expired (spec 015) are separate values rather than one
// OUT_OF_WINDOW because the not-yet/passed decision is the SERVER's to make: a
// gate device's clock is the least trustworthy in the system, and letting the
// result card derive the wording by comparing the window to its own clock would
// hand that decision to it.
const (
	ResultValid       = "VALID"
	ResultAlreadyUsed = "ALREADY_USED"
	ResultInvalid     = "INVALID"
	ResultNotYetValid = "NOT_YET_VALID"
	ResultExpired     = "EXPIRED"
)

// Ticket statuses, matching the CHECK constraint on tickets.status.
const (
	StatusActive  = "ACTIVE"
	StatusUsed    = "USED"
	StatusRevoked = "REVOKED"
)

// Detail is the internal joined view of a ticket. It is never returned directly —
// each endpoint projects only the fields its own contract allows.
type Detail struct {
	TicketCode     string
	Status         string
	AttendeeName   string
	TicketTypeName string
	EventName      string
	// The admission window of the ticket type this ticket was issued from
	// (spec 015). Both endpoints inclusive; no grace period either side.
	EventStart time.Time
	EventEnd   time.Time
}

// FullDetail extends Detail with the event context printed on a ticket PDF.
// AttendeeEmail is the holder's delivery address (spec 011): the notification
// domain groups an order's tickets by it for per-holder emails. Empty for
// pre-008 rows whose slot was never filled.
type FullDetail struct {
	TicketCode     string
	Status         string
	AttendeeName   string
	AttendeeEmail  string
	TicketTypeName string
	EventName      string
	Venue          string
	// The admission window of the ticket TYPE this ticket was issued from, not
	// the parent event's dates (spec 015 FR-011). The venue above is still the
	// event's — only the date moved.
	EventStart time.Time
	EventEnd   time.Time
}

// Record is the internal view of a tickets row.
type Record struct {
	ID         uuid.UUID
	TicketCode string
	AttendeeID uuid.UUID
	Status     string
}

// PublicTicket is the complete body of GET /api/v1/tickets/:code.
//
// The endpoint is unauthenticated, so it discloses only what the code holder
// already knows. The attendee's email, all buyer data, the order number, and
// pricing are deliberately absent (spec FR-016). No QR image is returned either:
// tickets.qr_code_url is always NULL and QR images are rendered on demand at PDF
// render time (FR-022).
type PublicTicket struct {
	TicketCode   string `json:"ticket_code"`
	Status       string `json:"status"`
	EventName    string `json:"event_name"`
	AttendeeName string `json:"attendee_name"`
}

// ValidateRequest is the admin validate body (POST /api/v1/admin/tickets/validate).
type ValidateRequest struct {
	Code string `json:"code"`
}

// ValidationResult is the admin validate response. The three detail fields are
// null when the result is INVALID, so an unknown code discloses nothing.
type ValidationResult struct {
	Result         string  `json:"result"`
	TicketCode     string  `json:"ticket_code"`
	AttendeeName   *string `json:"attendee_name"`
	TicketTypeName *string `json:"ticket_type_name"`
	EventName      *string `json:"event_name"`
	// The window this ticket does apply to, so an out-of-window refusal can name
	// the right day rather than merely turning the holder away (spec 015 FR-016).
	// Nil on INVALID, which discloses nothing about an unknown code (FR-019).
	EventStart *time.Time `json:"event_start"`
	EventEnd   *time.Time `json:"event_end"`
}

// MarkUsedResponse is the body of POST /api/v1/admin/tickets/:code/use.
type MarkUsedResponse struct {
	TicketCode string `json:"ticket_code"`
	Status     string `json:"status"`
}

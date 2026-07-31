package ticket

import (
	"time"

	"github.com/google/uuid"
)

// Validation results returned by the admin door flow.
const (
	ResultValid       = "VALID"
	ResultAlreadyUsed = "ALREADY_USED"
	ResultInvalid     = "INVALID"
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
}

// FullDetail extends Detail with the event context printed on a ticket PDF.
type FullDetail struct {
	TicketCode     string
	Status         string
	AttendeeName   string
	TicketTypeName string
	EventName      string
	Venue          string
	StartDate      time.Time
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
}

// MarkUsedResponse is the body of POST /api/v1/admin/tickets/:code/use.
type MarkUsedResponse struct {
	TicketCode string `json:"ticket_code"`
	Status     string `json:"status"`
}

// Package order owns the orders, order_items, and attendees tables: guest
// checkout, the admin read-only views over sales, and the order-side checks other
// domains consume through interfaces.
package order

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/money"
)

// CheckoutItem is one requested line: either an individual ticket type or a
// package, never both. Exactly one of TicketTypeID / PackageID must be set,
// matching the order_items_line_kind_chk constraint.
type CheckoutItem struct {
	TicketTypeID *uuid.UUID `json:"ticket_type_id"`
	PackageID    *uuid.UUID `json:"package_id"`
	Quantity     int32      `json:"quantity"`
}

// BookRequest is the booking body (POST /ticket/book, spec 008): items only.
// No buyer or visitor identity yet — those arrive with checkout (Option B).
type BookRequest struct {
	EventID uuid.UUID      `json:"event_id"`
	Items   []CheckoutItem `json:"items"`
}

// Validate rejects what can be decided without I/O.
func (r BookRequest) Validate() error {
	if r.EventID == uuid.Nil {
		return apperr.BadRequest(apperr.CodeValidation, "event_id is required.")
	}
	_, err := validateItemLines(r.Items)
	return err
}

// BookResponse is the 201 body of POST /ticket/book. OrderID is the public
// order number — the identifier every later /ticket/... call carries.
type BookResponse struct {
	OrderID     string      `json:"order_id"`
	Status      string      `json:"status"`
	TotalAmount money.Money `json:"total_amount"`
	ExpiresAt   time.Time   `json:"expires_at"`
}

// AvailabilityRequest is the body of POST /ticket/availability (spec 013): the
// selection the guest is about to buy, asked about before the Terms &
// Conditions gate opens.
//
// Deliberately the same shape as BookRequest. The client sends one object to
// both calls, so the check cannot approve a selection booking never saw.
type AvailabilityRequest struct {
	EventID uuid.UUID      `json:"event_id"`
	Items   []CheckoutItem `json:"items"`
}

// Validate rejects what can be decided without I/O. A malformed request has no
// availability answer — it is a 400, not a decision.
func (r AvailabilityRequest) Validate() error {
	if r.EventID == uuid.Nil {
		return apperr.BadRequest(apperr.CodeValidation, "event_id is required.")
	}
	_, err := validateItemLines(r.Items)
	return err
}

// AvailabilityDecision is the 200 body of POST /ticket/availability: the
// server's answer about one selection at one instant.
//
// It is advisory and transient — nothing is reserved, nothing is stored, and a
// true answer confers no right to book. Between this decision and the guest's
// Agree click another buyer can take the last seat, which is why every refusal
// path inside bookOnce stays exactly where it is.
//
// "Not available" is a successful answer to a well-formed question, so it comes
// back 200 with envelope code 200000, not as an error.
type AvailabilityDecision struct {
	// Available is true if and only if Reasons is empty.
	Available bool `json:"available"`
	// Reasons carries EVERY refusal found, not the first. Originally so a guest
	// could be told everything at once; since the 2026-08-19 amendment they are
	// told one general sentence instead (FR-012), and this survives as the
	// diagnostic record FR-006 requires.
	Reasons []AvailabilityReason `json:"reasons"`
}

// AvailabilityReason is one refusal, addressed to the line that caused it.
type AvailabilityReason struct {
	// ItemIndex is the 0-based index into the request's items. Nil for an
	// order-level reason — today only TERMS_MISSING, which is a property of the
	// event rather than of any one line.
	ItemIndex *int `json:"item_index"`
	// TicketTypeID names the ticket type at fault. For a quota shortfall reached
	// through a bundle this is the CONSTITUENT, which is what explains the
	// refusal to a guest looking at a bundle that seemed available.
	TicketTypeID *uuid.UUID `json:"ticket_type_id"`
	// PackageID is set when the offending line was a bundle.
	PackageID *uuid.UUID `json:"package_id"`
	// Code is the STABLE STRING apperr code, never the numeric envelope code.
	// apperr.Numeric renders TICKET_TYPE_NOT_ON_SALE, PACKAGE_NOT_ON_SALE and
	// VALIDATION_ERROR all as 400001, so a client branching on numbers could not
	// tell "your ticket stopped selling" from "your request was malformed" —
	// which spec 013 still requires it to do. The reason changed with the
	// 2026-08-19 amendment and the need did not: the first two now fall in
	// FR-012's general-message bucket while VALIDATION_ERROR keeps its own
	// wording, so the client still has to separate them.
	Code string `json:"code"`
	// Message describes this refusal, produced by the same code that produces
	// booking's message for the condition. DIAGNOSTIC since the 2026-08-19
	// amendment: it is never rendered to a guest, who reads the fixed general
	// message of FR-012 (FR-006, FR-013).
	Message string `json:"message"`
}

// AgreementRequest is the record-agreement body
// (POST /ticket/terms-condition/:order_id). EventTermsID is the document id the
// dialog displayed, so the server can refuse a stale agreement (409002).
type AgreementRequest struct {
	Agreed       bool      `json:"agreed"`
	EventTermsID uuid.UUID `json:"event_terms_id"`
}

// PublicOrderEvent names the event an order belongs to. The slug is what lets an
// expired order offer a way back to buy again; venue, address, and dates feed
// the registration page's Order Summary event box (Figma 12-4456).
type PublicOrderEvent struct {
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	Venue     string    `json:"venue"`
	Address   string    `json:"address"`
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
}

// LineKind discriminates what an order line sold.
type LineKind string

const (
	LineKindTicket  LineKind = "ticket"
	LineKindPackage LineKind = "package"
)

// PublicOrderItem is one purchased line on the guest's own order page.
//
// Kind tells the client which name field to read: exactly one of TicketTypeName
// and PackageName is populated, mirroring the XOR on order_items. A bundle is
// shown whole, at the price it was sold for, rather than split into the ticket
// types it drew on.
type PublicOrderItem struct {
	Kind           LineKind    `json:"kind"`
	TicketTypeName *string     `json:"ticket_type_name"`
	PackageName    *string     `json:"package_name"`
	Quantity       int32       `json:"quantity"`
	UnitPrice      money.Money `json:"unit_price"`
	Subtotal       money.Money `json:"subtotal"`
	// AdmissionStarts are the days THIS line admits on, ascending: one for a
	// ticket line, one per distinct constituent for a bundle (spec 015 FR-021a).
	//
	// A list rather than a start/end pair, because a single value cannot express
	// a Day 1 + Day 2 bundle — collapsing one to its earliest date is the defect
	// this field exists to fix. There is no matching end: the display names days,
	// and a range across three or more closes on a START date (FR-021c), so an
	// end would only invite a consumer to read the wrong thing.
	//
	// They live on the line rather than on the order's event block because two
	// lines of one order can admit on different days — that is the whole point of
	// the feature — whereas Event below names the EVENT's own dates (FR-009).
	AdmissionStarts []time.Time `json:"admission_starts"`
	// Description is the line's own admin-authored note — the ticket type's or
	// the package's — already rendered on the booking card. Null when none was
	// written.
	//
	// Added by spec 016: the ticket email's receipt attachment prints it after
	// the admission date on each product row, and it has no other source. An
	// additive nullable field, so no existing consumer breaks.
	Description *string `json:"description"`
}

// PaymentInstruction is everything the guest needs in order to pay, and nothing
// else. It is present only while the order is actually payable — see
// PublicOrderDetail.Payment.
type PaymentInstruction struct {
	// Method is the payment instrument, not the provider: what the guest is
	// looking at is a QRIS code regardless of who acquires it.
	Method   string      `json:"method"`
	Provider string      `json:"provider"`
	Amount   money.Money `json:"amount"`
	// ExpiresAt is the server's deadline. Paired with PublicOrderDetail.ServerTime
	// it lets the client render a countdown that a wrong device clock cannot skew.
	ExpiresAt time.Time `json:"expires_at"`
	// QRImagePath is where the QR is rendered on demand. A path rather than a
	// full URL so the client stays origin-agnostic.
	QRImagePath string `json:"qr_image_path"`
}

// CheckoutVisitor is one filled visitor form, addressed to the slot it fills
// (id from GET /ticket/order/:order_id).
type CheckoutVisitor struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
	Phone string    `json:"phone"`
	// Dob is date-only, YYYY-MM-DD.
	Dob    string `json:"dob"`
	Gender string `json:"gender"`
}

// CheckoutFormsRequest is the Option B checkout body
// (POST /ticket/checkout/:order_id): the holder forms arrive with the same
// call that starts payment — nothing was persisted before it. There is no
// separate buyer block (spec 011): the topmost holder form doubles as the
// order's primary contact, derived server-side from canonical slot order.
// Unknown fields sent by stale clients (buyer_*) are ignored by binding.
type CheckoutFormsRequest struct {
	Attendees []CheckoutVisitor `json:"attendees"`
}

// GenderOption is one row of GET /ticket/genders — the gender master list the
// forms build their options from (clarified 2026-08-05).
type GenderOption struct {
	// Narrowed from a uuid string to a small integer by migration 0013
	// (spec 011 FR-025). Safe because no client reads it: checkout submits the
	// gender NAME, and the form's select is bound to the name too — verified
	// across the frontend, not assumed (contracts/schema-revision.md §4).
	ID   int16  `json:"id"`
	Name string `json:"name"`
}

// visitorDobFormat is the wire format for dates of birth.
const visitorDobFormat = "2006-01-02"

// visitorPhonePattern is the spec 011 phone rule (clarified 2026-08-07, floor
// raised 2026-08-13): 12-15 digits and nothing else. Length is the whole rule —
// no country code is required or implied, and the digits are counted on the
// value exactly as the guest typed it, with no prefix inspection. So
// `628123456789` passes while the same subscriber number written `08123456789`
// is eleven digits and does not. The message below is shared verbatim with the
// client-side validator so the same failure reads identically whichever side
// catches it.
//
// This governs writes only. Rows stored under either earlier rule stay valid at
// rest — `attendees.phone` is VARCHAR(50) with no CHECK, deliberately, because
// digits cannot be invented for a number already taken.
var visitorPhonePattern = regexp.MustCompile(`^[0-9]{12,15}$`)

const visitorPhoneMessage = "Enter a phone number of 12-15 digits."

// Validate rejects malformed forms with a 400001 whose data is a field→message
// map (contracts/api.md call 8), so the client can mark the exact inputs.
//
// validGenders maps each active gender master NAME to its row id (spec 011:
// the name is the wire value, gender_id is what checkout stores). Passed in by
// the service so every problem lands in ONE field map — a guest never fixes
// the email only to be told about the gender on the next attempt.
func (r CheckoutFormsRequest) Validate(validGenders map[string]int16) error {
	fields := map[string]string{}

	if len(r.Attendees) == 0 {
		fields["attendees"] = "Visitor details are required for every ticket."
	}

	seen := make(map[uuid.UUID]bool, len(r.Attendees))
	for i, v := range r.Attendees {
		key := func(name string) string { return fmt.Sprintf("attendees[%d].%s", i, name) }
		if v.ID == uuid.Nil {
			fields[key("id")] = "The slot id is required."
		} else if seen[v.ID] {
			fields[key("id")] = "This slot appears more than once."
		}
		seen[v.ID] = true
		if strings.TrimSpace(v.Name) == "" {
			fields[key("name")] = "Visitor name is required."
		}
		if !emailShaped(v.Email) {
			fields[key("email")] = "A valid email address is required."
		}
		if !visitorPhonePattern.MatchString(strings.TrimSpace(v.Phone)) {
			fields[key("phone")] = visitorPhoneMessage
		}
		if dob, err := time.Parse(visitorDobFormat, v.Dob); err != nil {
			fields[key("dob")] = "Date of birth must be YYYY-MM-DD."
		} else if dob.After(time.Now()) {
			fields[key("dob")] = "Date of birth cannot be in the future."
		}
		if _, ok := validGenders[v.Gender]; !ok {
			fields[key("gender")] = "Select a valid gender."
		}
	}

	if len(fields) > 0 {
		return apperr.BadRequest(apperr.CodeValidation,
			"Some fields are missing or invalid.").WithData(fields)
	}
	return nil
}

// CheckoutQRResponse is the 200 body of POST /ticket/checkout/:order_id:
// everything the payment screen needs to render and time the QR.
type CheckoutQRResponse struct {
	OrderID  string `json:"order_id"`
	QRString string `json:"qr_string"`
	// ExpiresAt is the gateway's own deadline, adopted verbatim — one order, one
	// code, one window. The mid-window refresh that used to shorten this is
	// withdrawn, so there is no qr_refresh_after_seconds to serve alongside it.
	ExpiresAt time.Time `json:"expires_at"`
	// QRImageURL is our own render route — the browser never talks to the
	// payment provider (FR-009).
	QRImageURL string `json:"qr_image_url"`
	// ExtRefID is the gateway's own reference for this payment session, carried so
	// a caller can match the order against the gateway's records without a second
	// request (spec 017 FR-008).
	//
	// Always present, empty when no reference exists, so a caller never has to
	// tell an absent field from an absent reference (FR-010). It is opaque: no
	// format is promised and nothing may be derived from it.
	//
	// Every answer that carries a payment payload carries this too — the freshly
	// opened session, the already-started refusal, and the lost-race answer — and
	// all three name the same session for the same order (FR-011).
	ExtRefID string `json:"ext_ref_id"`
}

// TicketOrderSlot is one attendee slot on the guest order read
// (GET /ticket/order/:order_id). Details are null until checkout saves the
// visitor forms (Option B); PackageName is null for standalone ticket slots.
// PackageID + PackageUnit identify the purchased bundle unit the slot belongs
// to (spec 010) — the client renders one visitor form per unit. PackageUnit is
// also null on bundle slots booked before spec 010 (one form per slot then).
type TicketOrderSlot struct {
	ID             uuid.UUID  `json:"id"`
	TicketTypeName string     `json:"ticket_type_name"`
	PackageName    *string    `json:"package_name"`
	PackageID      *uuid.UUID `json:"package_id"`
	PackageUnit    *int       `json:"package_unit"`
	Name           *string    `json:"name"`
	Email          *string    `json:"email"`
	Phone          *string    `json:"phone"`
	// Dob is date-only (YYYY-MM-DD), matching the checkout request format.
	Dob    *string `json:"dob"`
	Gender *string `json:"gender"`
}

// PublicOrderFee is one frozen fee line on the guest order read, exactly as
// booking computed it (name with any percentage baked in, e.g. "PPN (11%)").
type PublicOrderFee struct {
	Name   string      `json:"name"`
	Amount money.Money `json:"amount"`
}

// TicketOrderDetail is the guest order read the screens route on
// (GET /ticket/order/:order_id, contracts/api.md "Supporting reads").
type TicketOrderDetail struct {
	OrderID     string      `json:"order_id"`
	Status      string      `json:"status"`
	TotalAmount money.Money `json:"total_amount"`
	// Subtotal is the pre-fee sum of the lines (null on pre-fee orders); Fees
	// are the frozen breakdown between it and TotalAmount (Figma 32-1366).
	Subtotal *money.Money     `json:"subtotal"`
	Fees     []PublicOrderFee `json:"fees"`
	// ExpiresAt is the live deadline: the 1-hour hold before payment starts, the
	// 14-minute payment window after.
	ExpiresAt     *time.Time `json:"expires_at"`
	TermsAgreedAt *time.Time `json:"terms_agreed_at"`
	// PaymentStarted picks the screen: false = visitor forms, true = QR panel.
	PaymentStarted bool              `json:"payment_started"`
	Event          PublicOrderEvent  `json:"event"`
	Items          []PublicOrderItem `json:"items"`
	Slots          []TicketOrderSlot `json:"slots"`
	// ServerTime is this response's clock reading; countdowns offset from it.
	ServerTime time.Time `json:"server_time"`
	// Payment is non-nil only while the order is genuinely payable.
	Payment *PaymentInstruction `json:"payment"`
}

// OrderSummary is an admin read-only order row (GET /api/v1/admin/orders).
type OrderSummary struct {
	ID          uuid.UUID   `json:"id"`
	OrderNumber string      `json:"order_number"`
	BuyerName   string      `json:"buyer_name"`
	BuyerEmail  string      `json:"buyer_email"`
	Status      string      `json:"status"`
	TotalAmount money.Money `json:"total_amount"`
	CreatedAt   *time.Time  `json:"created_at"`
}

// AttendeeSummary is an admin read-only attendee row
// (GET /api/v1/admin/attendees).
type AttendeeSummary struct {
	Name           string `json:"name"`
	Email          string `json:"email"`
	TicketTypeName string `json:"ticket_type_name"`
	OrderNumber    string `json:"order_number"`
}

// validateItemLines checks the selected lines shared by booking and checkout:
// per-line quantities, and one line per ticket type or package — a repeated
// reference would make the attendee-to-line mapping ambiguous. Returns the
// per-reference quantity map for callers that reconcile attendees against it.
func validateItemLines(items []CheckoutItem) (map[string]int32, error) {
	if len(items) == 0 {
		return nil, apperr.BadRequest(apperr.CodeValidation, "At least one item must be selected.")
	}

	wanted := make(map[string]int32, len(items))
	for i, item := range items {
		hasTicket := item.TicketTypeID != nil
		hasPackage := item.PackageID != nil
		if hasTicket == hasPackage {
			return nil, apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("items[%d] must reference exactly one of ticket_type_id or package_id.", i))
		}
		if item.Quantity < 1 {
			return nil, apperr.BadRequest(apperr.CodeValidation, "items[].quantity must be at least 1.")
		}

		var key string
		if hasTicket {
			if *item.TicketTypeID == uuid.Nil {
				return nil, apperr.BadRequest(apperr.CodeValidation, "items[].ticket_type_id is required.")
			}
			key = "tt:" + item.TicketTypeID.String()
		} else {
			if *item.PackageID == uuid.Nil {
				return nil, apperr.BadRequest(apperr.CodeValidation, "items[].package_id is required.")
			}
			key = "pkg:" + item.PackageID.String()
		}
		if _, seen := wanted[key]; seen {
			return nil, apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("Item %s appears more than once; combine it into a single line.", key))
		}
		wanted[key] = item.Quantity
	}
	return wanted, nil
}

// emailShaped reports whether a value parses as an email address, for callers
// building field maps rather than one-error-at-a-time responses.
func emailShaped(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	_, err := mail.ParseAddress(trimmed)
	return err == nil
}

// FeeAdminView is one fee master row on the admin panel
// (GET /api/v1/admin/fees).
type FeeAdminView struct {
	ID uuid.UUID `json:"id"`
	// Name is the plain label ("PPN"); booking bakes a percentage into the
	// order's frozen line ("PPN (11%)") itself.
	Name string `json:"name"`
	// FeeType is PERCENT (value% of the subtotal) or FIXED (flat rupiah).
	FeeType   string      `json:"fee_type"`
	Value     money.Money `json:"value"`
	Position  int32       `json:"position"`
	IsActive  bool        `json:"is_active"`
	CreatedAt *time.Time  `json:"created_at"`
	UpdatedAt *time.Time  `json:"updated_at"`
}

// FeeRequest is the admin create/update body for a fee master row.
type FeeRequest struct {
	Name     string      `json:"name"`
	FeeType  string      `json:"fee_type"`
	Value    money.Money `json:"value"`
	Position int32       `json:"position"`
	IsActive bool        `json:"is_active"`
}

// Validate rejects a malformed fee body with a 400001 field map.
func (r FeeRequest) Validate() error {
	fields := map[string]string{}
	if strings.TrimSpace(r.Name) == "" {
		fields["name"] = "Fee name is required."
	}
	if r.FeeType != "PERCENT" && r.FeeType != "FIXED" {
		fields["fee_type"] = "Fee type must be PERCENT or FIXED."
	}
	if r.Value.Decimal().IsNegative() {
		fields["value"] = "Fee value cannot be negative."
	}
	if len(fields) > 0 {
		return apperr.BadRequest(apperr.CodeValidation,
			"Some fields are missing or invalid.").WithData(fields)
	}
	return nil
}

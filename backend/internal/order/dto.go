// Package order owns the orders, order_items, and attendees tables: guest
// checkout, the admin read-only views over sales, and the order-side checks other
// domains consume through interfaces.
package order

import (
	"fmt"
	"net/mail"
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

// CheckoutAttendee is one person the buyer is purchasing a ticket for. One ticket
// is generated per attendee once the order is paid.
//
// PackageID is non-nil only for attendees originating from a bundle purchase.
// TicketTypeID is always present: even bundle-derived attendees are bound to one
// specific ticket type, which is what keeps one pass per attendee true for
// packages.
type CheckoutAttendee struct {
	TicketTypeID uuid.UUID  `json:"ticket_type_id"`
	PackageID    *uuid.UUID `json:"package_id,omitempty"`
	Name         string     `json:"name"`
	Email        string     `json:"email"`
}

// CheckoutRequest is the guest checkout body (POST /api/v1/checkout).
//
// No total is accepted from the client: the server recomputes it from current
// ticket-type prices (contracts/api.md, FR-006).
type CheckoutRequest struct {
	BuyerName  string             `json:"buyer_name"`
	BuyerEmail string             `json:"buyer_email"`
	BuyerPhone string             `json:"buyer_phone"`
	Items      []CheckoutItem     `json:"items"`
	Attendees  []CheckoutAttendee `json:"attendees"`
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

// AgreementRequest is the record-agreement body
// (POST /ticket/terms-condition/:order_id). EventTermsID is the document id the
// dialog displayed, so the server can refuse a stale agreement (409002).
type AgreementRequest struct {
	Agreed       bool      `json:"agreed"`
	EventTermsID uuid.UUID `json:"event_terms_id"`
}

// OrderResponse is the 201 body returned once the payment session exists.
//
// PaymentURL is retained for compatibility and audit only: the guest is no
// longer sent anywhere, they are routed in-app to the order page, which renders
// the QR itself (spec FR-009).
type OrderResponse struct {
	OrderNumber string      `json:"order_number"`
	Status      string      `json:"status"`
	TotalAmount money.Money `json:"total_amount"`
	PaymentURL  string      `json:"payment_url"`
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

// PublicOrderDetail is the guest's own view of an order
// (GET /api/v1/orders/:orderNumber).
//
// It is unauthenticated and keyed only by order number, so what it omits matters
// as much as what it carries: no ticket codes, no attendee list, no provider
// transaction id, nothing about any other order (spec FR-022).
type PublicOrderDetail struct {
	OrderNumber string            `json:"order_number"`
	Status      string            `json:"status"`
	TotalAmount money.Money       `json:"total_amount"`
	BuyerName   string            `json:"buyer_name"`
	BuyerEmail  string            `json:"buyer_email"`
	CreatedAt   *time.Time        `json:"created_at"`
	Event       PublicOrderEvent  `json:"event"`
	Items       []PublicOrderItem `json:"items"`
	// ServerTime is this response's clock reading. The client offsets its own
	// clock by the difference before counting down (spec FR-012, SC-005).
	ServerTime time.Time `json:"server_time"`
	// Payment is nil unless the order is genuinely payable: PENDING, with an
	// instruction recorded, and not past its deadline. A client must render a QR
	// only when this is present (spec FR-014).
	Payment *PaymentInstruction `json:"payment"`
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
// (POST /ticket/checkout/:order_id): buyer + visitor forms arrive with the
// same call that starts payment — nothing was persisted before it.
type CheckoutFormsRequest struct {
	BuyerName  string `json:"buyer_name"`
	BuyerEmail string `json:"buyer_email"`
	BuyerPhone string `json:"buyer_phone"`
	// BuyerDob is date-only, YYYY-MM-DD — the buyer form collects the same
	// personal fields as a visitor card (Figma 12-4456).
	BuyerDob    string            `json:"buyer_dob"`
	BuyerGender string            `json:"buyer_gender"`
	Attendees   []CheckoutVisitor `json:"attendees"`
}

// GenderOption is one row of GET /ticket/genders — the gender master list the
// forms build their options from (clarified 2026-08-05).
type GenderOption struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// visitorDobFormat is the wire format for dates of birth.
const visitorDobFormat = "2006-01-02"

// Validate rejects malformed forms with a 400001 whose data is a field→message
// map (contracts/api.md call 8), so the client can mark the exact inputs.
//
// validGenders is the active gender master list (clarified 2026-08-05), passed
// in by the service so every problem lands in ONE field map — a guest never
// fixes the email only to be told about the gender on the next attempt.
func (r CheckoutFormsRequest) Validate(validGenders map[string]bool) error {
	fields := map[string]string{}

	if strings.TrimSpace(r.BuyerName) == "" {
		fields["buyer_name"] = "Buyer name is required."
	}
	if !emailShaped(r.BuyerEmail) {
		fields["buyer_email"] = "A valid email address is required."
	}
	if strings.TrimSpace(r.BuyerPhone) == "" {
		fields["buyer_phone"] = "Buyer phone is required."
	}
	if dob, err := time.Parse(visitorDobFormat, r.BuyerDob); err != nil {
		fields["buyer_dob"] = "Date of birth must be YYYY-MM-DD."
	} else if dob.After(time.Now()) {
		fields["buyer_dob"] = "Date of birth cannot be in the future."
	}
	if !validGenders[r.BuyerGender] {
		fields["buyer_gender"] = "Select a valid gender."
	}
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
		if strings.TrimSpace(v.Phone) == "" {
			fields[key("phone")] = "Visitor phone is required."
		}
		if dob, err := time.Parse(visitorDobFormat, v.Dob); err != nil {
			fields[key("dob")] = "Date of birth must be YYYY-MM-DD."
		} else if dob.After(time.Now()) {
			fields[key("dob")] = "Date of birth cannot be in the future."
		}
		if !validGenders[v.Gender] {
			fields[key("gender")] = "Select a valid gender."
		}
	}

	if len(fields) > 0 {
		return apperr.BadRequest(apperr.CodeValidation,
			"Some fields are missing or invalid.").WithData(fields)
	}
	return nil
}

// CheckoutQRResponse is the 200 body of POST /ticket/checkout/:order_id (and
// refresh-qr): everything the payment screen needs to render and time the QR.
type CheckoutQRResponse struct {
	OrderID   string    `json:"order_id"`
	QRString  string    `json:"qr_string"`
	ExpiresAt time.Time `json:"expires_at"`
	// QRImageURL is our own render route — the browser never talks to the
	// payment provider (FR-009).
	QRImageURL string `json:"qr_image_url"`
	// QRRefreshAfterSeconds is when the client swaps in a fresh QR (config
	// QR_REFRESH_AFTER, served so the timer is server-owned).
	QRRefreshAfterSeconds int `json:"qr_refresh_after_seconds"`
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
	Name           *string   `json:"name"`
	Email          *string   `json:"email"`
	Phone          *string   `json:"phone"`
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
	// Buyer identity is null until checkout saves the forms (Option B).
	BuyerName  *string `json:"buyer_name"`
	BuyerEmail *string `json:"buyer_email"`
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

// TotalQuantity sums every line's quantity. For ticket lines this is the number
// of tickets bought; for a package line it counts package units, not the attendee
// slots the package expands into (those depend on the composition and are
// validated server-side). It exists for reporting, not as the attendee count.
func (r CheckoutRequest) TotalQuantity() int32 {
	var total int32
	for _, item := range r.Items {
		total += item.Quantity
	}
	return total
}

// Validate checks everything that can be decided without touching the database.
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

// Cheap rejections happen here so an invalid request never opens a transaction or
// takes a quota row lock.
func (r CheckoutRequest) Validate() error {
	if strings.TrimSpace(r.BuyerName) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "buyer_name is required.")
	}
	if err := validateEmail(r.BuyerEmail, "buyer_email"); err != nil {
		return err
	}
	if strings.TrimSpace(r.BuyerPhone) == "" {
		return apperr.BadRequest(apperr.CodeValidation, "buyer_phone is required.")
	}
	wanted, err := validateItemLines(r.Items)
	if err != nil {
		return err
	}

	for i, attendee := range r.Attendees {
		if strings.TrimSpace(attendee.Name) == "" {
			return apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("attendees[%d].name is required.", i))
		}
		if err := validateEmail(attendee.Email, fmt.Sprintf("attendees[%d].email", i)); err != nil {
			return err
		}
		if attendee.TicketTypeID == uuid.Nil {
			return apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("attendees[%d].ticket_type_id is required.", i))
		}
	}

	// Ticket lines are fully decidable here: every attendee without a package
	// origin must land on an ordered ticket type, in exactly the ordered count.
	// Package attendee slots depend on the composition, which only the server can
	// see after reading the database, so their exact counts are validated in
	// service.go against the expansion (see validateAttendees).
	gotTicket := make(map[string]int32)
	for _, attendee := range r.Attendees {
		if attendee.PackageID != nil {
			continue
		}
		gotTicket["tt:"+attendee.TicketTypeID.String()]++
	}
	for ref, quantity := range wanted {
		if strings.HasPrefix(ref, "pkg:") {
			continue
		}
		if gotTicket[ref] != quantity {
			return apperr.BadRequest(apperr.CodeAttendeeCountMismatch,
				fmt.Sprintf("Reference %s ordered %d ticket(s) but %d attendee entr(ies) were provided.",
					ref, quantity, gotTicket[ref]))
		}
	}
	for ref := range gotTicket {
		if _, ordered := wanted[ref]; !ordered {
			return apperr.BadRequest(apperr.CodeAttendeeCountMismatch,
				fmt.Sprintf("An attendee references %s, which is not in the order.", ref))
		}
	}
	// Every package attendee must reference an ordered package. The per-ticket-type
	// breakdown of those slots is checked server-side after expansion.
	for _, attendee := range r.Attendees {
		if attendee.PackageID == nil {
			continue
		}
		if _, ordered := wanted["pkg:"+attendee.PackageID.String()]; !ordered {
			return apperr.BadRequest(apperr.CodeAttendeeCountMismatch,
				fmt.Sprintf("An attendee references package %s, which is not in the order.", attendee.PackageID))
		}
	}

	return nil
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

func validateEmail(value, field string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return apperr.BadRequest(apperr.CodeValidation, field+" is required.")
	}
	if _, err := mail.ParseAddress(trimmed); err != nil {
		return apperr.BadRequest(apperr.CodeValidation, field+" is not a valid email address.")
	}
	return nil
}

// FeeAdminView is one fee master row on the admin panel
// (GET /api/v1/admin/fees).
type FeeAdminView struct {
	ID uuid.UUID `json:"id"`
	// Name is the plain label ("PPN"); booking bakes a percentage into the
	// order's frozen line ("PPN (11%)") itself.
	Name string `json:"name"`
	// FeeType is PERCENT (value% of the subtotal) or FIXED (flat rupiah).
	FeeType  string      `json:"fee_type"`
	Value    money.Money `json:"value"`
	Position int32       `json:"position"`
	IsActive bool        `json:"is_active"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
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

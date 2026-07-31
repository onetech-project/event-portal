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

// CheckoutItem is one requested ticket-type line.
type CheckoutItem struct {
	TicketTypeID uuid.UUID `json:"ticket_type_id"`
	Quantity     int32     `json:"quantity"`
}

// CheckoutAttendee is one person the buyer is purchasing a ticket for. One ticket
// is generated per attendee once the order is paid.
type CheckoutAttendee struct {
	TicketTypeID uuid.UUID `json:"ticket_type_id"`
	Name         string    `json:"name"`
	Email        string    `json:"email"`
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

// OrderResponse is the 201 body returned once the payment URL exists.
type OrderResponse struct {
	OrderNumber string      `json:"order_number"`
	Status      string      `json:"status"`
	TotalAmount money.Money `json:"total_amount"`
	PaymentURL  string      `json:"payment_url"`
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

// TotalQuantity is the number of tickets the request asks for, which must equal
// the number of attendee entries.
func (r CheckoutRequest) TotalQuantity() int32 {
	var total int32
	for _, item := range r.Items {
		total += item.Quantity
	}
	return total
}

// Validate checks everything that can be decided without touching the database.
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
	if len(r.Items) == 0 {
		return apperr.BadRequest(apperr.CodeValidation, "At least one ticket type must be selected.")
	}

	// Per-line quantities, and one line per ticket type: a repeated ticket type
	// would make the attendee-to-line mapping below ambiguous.
	wanted := make(map[uuid.UUID]int32, len(r.Items))
	for _, item := range r.Items {
		if item.TicketTypeID == uuid.Nil {
			return apperr.BadRequest(apperr.CodeValidation, "items[].ticket_type_id is required.")
		}
		if item.Quantity < 1 {
			return apperr.BadRequest(apperr.CodeValidation, "items[].quantity must be at least 1.")
		}
		if _, seen := wanted[item.TicketTypeID]; seen {
			return apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("Ticket type %s appears more than once; combine it into a single line.", item.TicketTypeID))
		}
		wanted[item.TicketTypeID] = item.Quantity
	}

	for i, attendee := range r.Attendees {
		if strings.TrimSpace(attendee.Name) == "" {
			return apperr.BadRequest(apperr.CodeValidation,
				fmt.Sprintf("attendees[%d].name is required.", i))
		}
		if err := validateEmail(attendee.Email, fmt.Sprintf("attendees[%d].email", i)); err != nil {
			return err
		}
	}

	if total := r.TotalQuantity(); int(total) != len(r.Attendees) {
		return apperr.BadRequest(apperr.CodeAttendeeCountMismatch,
			fmt.Sprintf("Expected %d attendee entries to match the %d tickets ordered, got %d.",
				total, total, len(r.Attendees)))
	}

	// The grand total matching is not enough — each ticket type's attendee count
	// must match its own quantity, or tickets would be issued for the wrong type.
	got := make(map[uuid.UUID]int32, len(wanted))
	for _, attendee := range r.Attendees {
		got[attendee.TicketTypeID]++
	}
	for ticketTypeID, quantity := range wanted {
		if got[ticketTypeID] != quantity {
			return apperr.BadRequest(apperr.CodeAttendeeCountMismatch,
				fmt.Sprintf("Ticket type %s ordered %d ticket(s) but %d attendee entr(ies) were provided.",
					ticketTypeID, quantity, got[ticketTypeID]))
		}
	}
	for ticketTypeID := range got {
		if _, ordered := wanted[ticketTypeID]; !ordered {
			return apperr.BadRequest(apperr.CodeAttendeeCountMismatch,
				fmt.Sprintf("An attendee references ticket type %s, which is not in the order.", ticketTypeID))
		}
	}

	return nil
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

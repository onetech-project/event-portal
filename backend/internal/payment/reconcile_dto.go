package payment

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Wire shapes for the two admin read views an operator consults before asking
// the gateway to resend a notification. They exist so no sqlc struct and no
// internal domain type crosses the HTTP boundary (Constitution Principle III) —
// the payments table's column names are an implementation detail, and `status`
// meaning two different things depending on the row is exactly the kind of
// detail a client should not have to know.
//
// There is deliberately no request shape here. Both views are reads: nothing on
// this surface records that a payment happened (FR-022d).

// NotificationResponse is one row of
// GET /admin/payment/order/:order_id/notifications.
type NotificationResponse struct {
	ID            uuid.UUID `json:"id"`
	Provider      string    `json:"provider"`
	TransactionID string    `json:"transaction_id"`
	Status        string    `json:"status"`
	// IsMarker separates this system's own conclusions from what the gateway
	// actually said. Both live in the same column; conflating them on screen
	// would let a marker be read as a gateway status.
	IsMarker    bool            `json:"is_marker"`
	PaymentType string          `json:"payment_type"`
	RawPayload  json.RawMessage `json:"raw_payload"`
	ReceivedAt  time.Time       `json:"received_at"`
}

// OrderHoldResponse is one row of GET /admin/payment/order/:order_id/holds:
// what the order holds of a ticket type against what that type has left.
//
// Both numbers are shown because only the pair is actionable — the top-up an
// operator needs is the difference, and `remaining` alone looks reassuring right
// up until it is smaller than `held`.
type OrderHoldResponse struct {
	TicketTypeID   uuid.UUID `json:"ticket_type_id"`
	TicketTypeName string    `json:"ticket_type_name"`
	Held           int32     `json:"held"`
	Remaining      int32     `json:"remaining"`
}

// SettleRefusedResponse is the body of a 200 answering a redelivered
// notification that could not settle its order (FR-019c).
//
// The status code is 200 because a retry would fail identically and the gateway
// would exhaust its budget on it. The body is what carries the refusal — for our
// own record and for an operator reading it, since the gateway does not read
// response bodies at all.
type SettleRefusedResponse struct {
	OrderNumber string `json:"order_number"`
	Error       string `json:"error"`
	Message     string `json:"message"`
	// Shortfall names each ticket type that cannot cover the order's hold, and by
	// how much, so the top-up can be sized without guessing.
	Shortfall []QuotaShortfallResponse `json:"shortfall"`
}

// QuotaShortfallResponse is one ticket type short of covering an order's hold.
type QuotaShortfallResponse struct {
	TicketTypeID   uuid.UUID `json:"ticket_type_id"`
	TicketTypeName string    `json:"ticket_type_name"`
	Required       int32     `json:"required"`
	Remaining      int32     `json:"remaining"`
}

func toNotificationResponse(records []NotificationRecord) []NotificationResponse {
	out := make([]NotificationResponse, 0, len(records))
	for _, r := range records {
		out = append(out, NotificationResponse{
			ID:            r.ID,
			Provider:      r.Provider,
			TransactionID: r.TransactionID,
			Status:        r.Status,
			IsMarker:      r.IsMarker,
			PaymentType:   r.PaymentType,
			RawPayload:    r.RawPayload,
			ReceivedAt:    r.ReceivedAt.UTC(),
		})
	}
	return out
}

func toOrderHoldResponse(holds []OrderHold) []OrderHoldResponse {
	out := make([]OrderHoldResponse, 0, len(holds))
	for _, h := range holds {
		out = append(out, OrderHoldResponse{
			TicketTypeID:   h.TicketTypeID,
			TicketTypeName: h.TicketTypeName,
			Held:           h.Held,
			Remaining:      h.Remaining,
		})
	}
	return out
}

func toSettleRefusedResponse(err *SettleRefusedError) SettleRefusedResponse {
	short := make([]QuotaShortfallResponse, 0, len(err.Shortfalls))
	for _, s := range err.Shortfalls {
		short = append(short, QuotaShortfallResponse{
			TicketTypeID:   s.TicketTypeID,
			TicketTypeName: s.TicketTypeName,
			Required:       s.Required,
			Remaining:      s.Remaining,
		})
	}
	return SettleRefusedResponse{
		OrderNumber: err.OrderNumber,
		Error:       "TICKETS_UNAVAILABLE",
		Message:     "The tickets for this order are no longer available, so the payment could not be settled. Add quota and request the notification again.",
		Shortfall:   short,
	}
}

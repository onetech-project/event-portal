package order

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ErrInsufficientQuota reports that a ticket type could not cover the requested
// quantity. Providers translate their own error onto this one.
var ErrInsufficientQuota = errors.New("order: insufficient quota")

// TicketTypeInfo is the slice of a ticket type checkout needs: the authoritative
// price and the sales window.
type TicketTypeInfo struct {
	ID         uuid.UUID
	EventID    uuid.UUID
	Name       string
	Price      decimal.Decimal
	SalesStart time.Time
	SalesEnd   time.Time
}

// EventProvider is the contract checkout needs from the event domain, declared
// here by its consumer exactly as ARCHITECTURE.md §3.2 prescribes. Nothing in this
// package imports internal/event; the composition root adapts one onto the other,
// so the two domains stay separable (Constitution Principle II).
//
// ticket_types.quota is the REMAINING quota: CheckAndDeductQuota decrements it and
// RestoreQuota adds back. There is no stored total to reconcile against.
type EventProvider interface {
	// TicketTypeForCheckout returns current server-side pricing and sales window.
	//
	// It takes the caller's transaction rather than borrowing its own connection.
	// That is not just for snapshot consistency: a checkout that holds a
	// transaction and then asks the pool for a second connection can exhaust the
	// pool and deadlock once concurrent buyers outnumber it.
	TicketTypeForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (TicketTypeInfo, error)
	// CheckAndDeductQuota atomically reserves qty seats inside the caller's
	// transaction, returning ErrInsufficientQuota when it cannot.
	CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error
	// RestoreQuota releases qty seats inside the caller's transaction.
	RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error
}

// PaymentItem is one line shown on the provider's payment page.
type PaymentItem struct {
	ID       string
	Name     string
	Price    decimal.Decimal
	Quantity int32
}

// PaymentRequest is what checkout hands the payment gateway.
type PaymentRequest struct {
	OrderNumber   string
	GrossAmount   decimal.Decimal
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	Items         []PaymentItem
}

// PaymentGateway is the contract checkout needs from the payment domain, again
// declared by its consumer. Swapping providers therefore never touches this
// package (Constitution Principle V).
type PaymentGateway interface {
	// Name identifies the provider, persisted on the order.
	Name() string
	// CreateTransaction opens a payment session and returns the guest-facing URL.
	CreateTransaction(ctx context.Context, req PaymentRequest) (string, error)
}

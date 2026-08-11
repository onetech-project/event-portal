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

// ErrNoTerms reports that an event has no authored Terms & Conditions document.
// Providers translate their own not-found onto this one.
var ErrNoTerms = errors.New("order: event has no terms")

// ErrGatewaySessionDuplicate reports that the gateway had already issued a code
// for this order's reference and refused to issue another. The adapter
// translates the payment domain's own sentinel onto this one.
//
// It is separate from a generic session-open failure because the two need
// opposite handling. A generic failure leaves the order payable and worth
// retrying; this one proves a code exists that this system will never hold, so
// the order can never be paid and the guest must start again.
var ErrGatewaySessionDuplicate = errors.New("order: gateway already issued a code for this reference")

// EventTermsInfo is the slice of a terms document the booking flow needs: the
// identity to stamp on the order when agreement is recorded.
type EventTermsInfo struct {
	ID      uuid.UUID
	EventID uuid.UUID
}

// TicketTypeInfo is the slice of a ticket type checkout needs: the authoritative
// price and the sales window.
type TicketTypeInfo struct {
	ID         uuid.UUID
	EventID    uuid.UUID
	Name       string
	Price      decimal.Decimal
	SalesStart time.Time
	SalesEnd   time.Time
	// QuotaRemaining is ticket_types.quota — REMAINING seats, never an original
	// allocation — read under NO lock, for the advisory availability check only.
	//
	// It must never gate a sale. Booking's authority over quota is the atomic,
	// row-locked UPDATE in CheckAndDeductQuota (Constitution Principle IV);
	// Principle VII says the same of any non-authoritative availability figure:
	// it "MUST NOT gate, authorize, or short-circuit a sale". Reading this field
	// inside bookOnce as a cheap pre-check would reintroduce exactly the oversell
	// that lock exists to prevent, and would do it silently — the happy path
	// would look identical. TestConcurrentBookingCannotOversell guards it.
	QuotaRemaining int32
}

// PackageInfo is the slice of a bundle checkout needs: the authoritative price,
// sales window, and full composition. Composition must arrive fully expanded so
// checkout can aggregate demand without a second lookup.
type PackageInfo struct {
	ID         uuid.UUID
	EventID    uuid.UUID
	Name       string
	Price      decimal.Decimal
	SalesStart time.Time
	SalesEnd   time.Time
	Components []PackageComponentInfo
}

// PackageComponentInfo is one constituent of a bundle: how many of the
// ticket-type one package unit consumes, plus its own sales window so the
// checkout can reject a package whose constituent is outside its window.
type PackageComponentInfo struct {
	TicketTypeID uuid.UUID
	Quantity     int32
	SalesStart   time.Time
	SalesEnd     time.Time
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
	// PackageForCheckout returns the authoritative price, sales window, and full
	// composition of a package inside the caller's transaction. The caller is
	// responsible for rejecting packages whose status or composition makes them
	// unpurchasable.
	PackageForCheckout(ctx context.Context, tx pgx.Tx, id uuid.UUID) (PackageInfo, error)
	// CheckAndDeductQuota atomically reserves qty seats inside the caller's
	// transaction, returning ErrInsufficientQuota when it cannot.
	CheckAndDeductQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error
	// RestoreQuota releases qty seats inside the caller's transaction.
	RestoreQuota(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID, qty int32) error
	// CurrentTerms returns the event's current Terms & Conditions identity, or
	// ErrNoTerms when none is authored. Booking refuses events without terms
	// (409001), and agreement recording compares the id the guest saw against
	// this current one (409002).
	CurrentTerms(ctx context.Context, eventID uuid.UUID) (EventTermsInfo, error)
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

// PaymentSession is what the gateway hands back once a payment session exists.
//
// Mirrored here rather than imported so the order domain never depends on the
// payment package (Constitution Principle II); the composition root translates
// between the two shapes.
type PaymentSession struct {
	// ProviderRef is the provider's transaction id, kept for support.
	ProviderRef string
	// QRString is the raw QRIS payload the guest's banking app scans. The image
	// is rendered from it on demand, never stored.
	QRString string
	// QRImageURL is the provider-hosted image of the same payload, persisted as
	// an audit trail rather than as somewhere to send the guest.
	QRImageURL string
	// ExpiresAt is the gateway's own deadline, which becomes the order's
	// payment_expires_at and from there the guest's countdown. It is always set —
	// the adapter substitutes a fallback when the gateway returned nothing usable
	// — so checkout never has to decide what a missing deadline means.
	ExpiresAt time.Time
	// ExpiryFromGateway is false when ExpiresAt is that fallback. Checkout records
	// it so a deadline the gateway never agreed to is visible in the order's own
	// logs, not only in the adapter's.
	ExpiryFromGateway bool
	// RedirectURL is empty for QRIS; it exists for a gateway that can only
	// redirect.
	RedirectURL string
}

// PaymentGateway is the contract checkout needs from the payment domain, again
// declared by its consumer. Swapping providers therefore never touches this
// package (Constitution Principle V).
type PaymentGateway interface {
	// Name identifies the provider, persisted on the order.
	Name() string
	// CreateTransaction opens a payment session and returns what the guest needs
	// in order to pay it.
	CreateTransaction(ctx context.Context, req PaymentRequest) (PaymentSession, error)
}

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

// ErrRegistrationTargetMissing reports that a free-registration link resolves to
// nothing a guest may register for (spec 022).
//
// ONE sentinel for several distinct causes — unknown event, unpublished event,
// unknown ticket type, ticket type belonging to a different event — because
// FR-012 forbids disclosing which. Splitting it into precise sentinels would put
// the distinction one careless handler away from the wire, so the collapse
// happens here, at the boundary, rather than being re-decided per call site.
var ErrRegistrationTargetMissing = errors.New("order: no such registration target")

// ErrGatewaySessionDuplicate reports that the gateway had already issued a code
// for this order's reference and refused to issue another. The adapter
// translates the payment domain's own sentinel onto this one.
//
// It is separate from a generic session-open failure because the two need
// opposite handling. A generic failure leaves the order payable and worth
// retrying; this one proves a code exists that this system will never hold, so
// the order can never be paid and the guest must start again.
var ErrGatewaySessionDuplicate = errors.New("order: gateway already issued a code for this reference")

// EventTermsInfo is the slice of a terms document the agreement flows need.
//
// TWO fields identify a document, and they are not interchangeable. ID says
// WHICH document — that is what gets stamped on the order. UpdatedAt says WHICH
// VERSION, and it is the only one of the two that can detect an edit.
//
// That distinction is load-bearing rather than pedantic. `UpsertEventTerms` is
// `ON CONFLICT (event_id) DO UPDATE SET content = ..., updated_at = now()` and
// `event_terms.event_id` is UNIQUE, so an admin edit OVERWRITES IN PLACE and
// preserves the row id. A staleness check comparing ids therefore cannot fire
// for the case it exists to catch — which is exactly the defect spec 022 found
// in RecordAgreement and fixed on both surfaces.
type EventTermsInfo struct {
	ID        uuid.UUID
	EventID   uuid.UUID
	UpdatedAt time.Time
}

// RegistrationTarget is the ticket type a free registration is issuing from,
// resolved within a guest-visible event (spec 022).
//
// IsVisible arrives as data rather than as a refusal so THIS domain
// decides the refusal — every failure on that surface must be indistinguishable
// (FR-012), and that is only guaranteeable in one place.
type RegistrationTarget struct {
	TicketTypeID   uuid.UUID
	TicketTypeName string
	EventID        uuid.UUID
	EventName      string
	EventSlug      string
	IsVisible      bool
	SalesStart     time.Time
	SalesEnd       time.Time
	QuotaRemaining int32
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
	// IsVisible means this type is obtained by REGISTERING, not by
	// buying (spec 022 FR-008). Every purchase path — book, checkout,
	// availability, and package expansion — resolves a ticket type through
	// TicketTypeForCheckout, so refusing it in expandTicket refuses it in all
	// four at once. Refusing it only in Book would leave the advisory
	// availability check answering "buyable", which would put the refusal AFTER
	// the Terms & Conditions gate that spec 013 deliberately placed it before.
	IsVisible bool
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
	// RegistrationTargetBySlug resolves a ticket type inside a guest-visible
	// event for the free-registration path, returning ErrRegistrationTargetMissing
	// when the event is not visible, the type does not exist, or they do not
	// belong together. The caller collapses all three onto one refusal.
	RegistrationTargetBySlug(ctx context.Context, slug string, ticketTypeID uuid.UUID) (RegistrationTarget, error)
}

// TicketIssuer generates one ticket per attendee once an order is final.
//
// Declared HERE rather than imported from internal/payment, which declares an
// identical pair. A domain must not import another domain (Principle II), so
// the duplication is the rule working, not a smell — the composition root
// satisfies both from the same ticket service.
type TicketIssuer interface {
	IssueTicketsForOrder(ctx context.Context, orderID uuid.UUID) error
}

// TicketDeliverer emails the holder their ticket.
type TicketDeliverer interface {
	SendTicketEmail(ctx context.Context, orderID uuid.UUID) error
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

// PaymentRecords is what checkout needs to read back about a payment it has
// already started — declared by its consumer, like everything else this package
// needs from outside it (Constitution Principle II).
//
// Read-only, and deliberately so. Recording the session belongs to the payment
// domain and happens in the composition root at the moment the gateway answers;
// this package never asks for that write, only for what it produced.
//
// It exists because two of checkout's three payload-bearing answers are rebuilt
// from what was stored rather than from a live gateway answer — the
// already-started refusal, which returns before any gateway call, and the answer
// served to a checkout that lost the stamping race, whose own session is not the
// one the order holds. Both must still name the session the guest is paying.
type PaymentRecords interface {
	// ExternalRefForOrder returns the gateway's own reference for this order's
	// payment session, or the empty string when none was recorded. Absent is a
	// normal answer, not an error: an order may never have opened a session, and
	// orders predating the record have none to find.
	ExternalRefForOrder(ctx context.Context, orderID uuid.UUID) (string, error)
}

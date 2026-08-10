package payment

import (
	"time"

	"github.com/pgauto/cdtc/status"
	"github.com/shopspring/decimal"
)

// The payment domain's data-transfer shapes.
//
// The webhook endpoint itself answers with a bare 200 (or a 401 rendered by the
// shared error envelope), so it has no success body. The types below are the
// domain's boundary shapes in the sense ARCHITECTURE.md §3.5 means: they are what
// crosses the Gateway interface, normalized so no provider-specific struct leaks
// into the order domain or into a handler.

// TransactionItem is one line the provider shows on its payment page.
type TransactionItem struct {
	ID       string
	Name     string
	Price    decimal.Decimal
	Quantity int32
}

// TransactionRequest is everything a gateway needs to open a payment session.
type TransactionRequest struct {
	OrderNumber   string
	GrossAmount   decimal.Decimal
	CustomerName  string
	CustomerEmail string
	CustomerPhone string
	Items         []TransactionItem
}

// PaymentSession is what a gateway hands back after opening a payment session.
//
// It is deliberately wider than the single URL the SNAP redirect flow needed: a
// QRIS charge yields a payload the guest scans and a deadline it stops working
// at, neither of which a bare URL can carry. Every field is provider-neutral, so
// the order domain still learns nothing about who is processing the payment
// (Constitution Principle V).
type PaymentSession struct {
	// ProviderRef is the provider's own transaction id, kept for support.
	ProviderRef string
	// QRString is the raw QRIS payload. The QR image is rendered from it on
	// demand — this MVP stores no files.
	QRString string
	// QRImageURL is the provider-hosted image of the same payload, retained as an
	// audit trail and fallback. It is not a page the guest is sent to.
	QRImageURL string
	// ExpiresAt is when the payment stops being accepted. It is the gateway's own
	// deadline, adopted verbatim whatever its length (FR-009, FR-009c) — not a
	// figure this system computed and hoped the gateway would honour. It is the
	// source of the guest's countdown and of the sweeper's decision.
	//
	// It is always set: when the gateway returned nothing usable the adapter
	// substitutes a fallback and raises a signal (FR-009b), so callers never have
	// to decide what an absent deadline means.
	ExpiresAt time.Time
	// ExpiryFromGateway is false when ExpiresAt is the fallback rather than the
	// gateway's own value. Callers that persist the deadline record which it was,
	// because a fallback deadline is one the gateway never agreed to and the two
	// sides can disagree about when the code stops working.
	ExpiryFromGateway bool
	// RedirectURL is empty for QRIS. It exists so a gateway that can only
	// redirect still fits this interface without another shape change.
	RedirectURL string
}

// WebhookResult is an authenticated gateway notification, normalized across
// gateways.
type WebhookResult struct {
	// OrderNumber is the reference the gateway echoes back. It is our order
	// number unmodified — there is no suffix to strip (FR-010b).
	OrderNumber string
	// TransactionID is the gateway's network transaction id (`nti`).
	TransactionID string
	// Status is the gateway's status enum, which MapProviderStatus turns into an
	// order outcome.
	Status status.Status
	// StatusPresent distinguishes an explicitly-sent status from an absent one.
	//
	// It exists because Pending is the enum's zero value, so a notification that
	// omits the field decodes as "pending" rather than erroring. The outcome is
	// the same either way — both change nothing — but the audit record must not
	// claim the gateway said "pending" when it said nothing at all (FR-018).
	StatusPresent bool
	// TransactionStatus is the status's own name, stored raw in payments.status so
	// Reject and Cancel stay distinguishable after both fold into CANCELLED.
	TransactionStatus string
	// PaymentType is the method, e.g. "qris".
	PaymentType string
	// TransactionType is the gateway's transaction type by name, for the audit
	// record. IsDeposit is the only thing acted on.
	TransactionType string
	// IsDeposit reports whether this notification is about money coming in. A
	// withdrawal changes no order of ours (FR-020).
	IsDeposit bool
	// RawPayload is the complete body exactly as it arrived.
	RawPayload []byte
}

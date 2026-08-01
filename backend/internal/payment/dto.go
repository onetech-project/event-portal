package payment

import (
	"time"

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
	// ExpiresAt is when the payment stops being accepted, as the provider
	// computed it. It is the source of the guest's countdown.
	ExpiresAt time.Time
	// RedirectURL is empty for QRIS. It exists so a gateway that can only
	// redirect still fits this interface without another shape change.
	RedirectURL string
}

// WebhookResult is a verified provider notification, normalized across gateways.
//
// TransactionStatus and FraudStatus stay in the provider's own vocabulary — the
// mapping onto order statuses is MapProviderStatus's job, and payments.status
// stores the raw value so deny and failure remain distinguishable.
type WebhookResult struct {
	OrderNumber       string
	TransactionID     string
	TransactionStatus string
	FraudStatus       string
	PaymentType       string
	RawPayload        []byte
}

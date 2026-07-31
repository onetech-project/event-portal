package payment

import "github.com/shopspring/decimal"

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

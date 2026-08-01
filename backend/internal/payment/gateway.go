package payment

import (
	"context"
	"errors"
)

// ErrInvalidSignature reports a notification whose signature does not verify. The
// webhook handler turns this into a 401 and processes nothing.
var ErrInvalidSignature = errors.New("payment: invalid webhook signature")

// Gateway is the payment-provider abstraction required by ARCHITECTURE.md §3.5 and
// Constitution Principle V. The order domain depends only on this interface, so a
// provider can be swapped without touching checkout.
type Gateway interface {
	// Name identifies the provider, stored on the order and the payment log.
	Name() string
	// CreateTransaction opens a payment session and returns what the guest needs
	// to pay it. It performs a network call and MUST NOT be invoked inside a
	// database transaction (Constitution Principle IV).
	CreateTransaction(ctx context.Context, req TransactionRequest) (PaymentSession, error)
	// VerifyWebhook authenticates a raw notification payload and normalizes it.
	VerifyWebhook(payload []byte, signature string) (*WebhookResult, error)
	// FetchStatus reads the provider's authoritative status for an order.
	//
	// It exists because a notification can be delayed, lost, or — in local
	// development — undeliverable to a machine the provider cannot reach. The
	// result is normalized onto the same shape a notification produces so both
	// feed the identical status-mapping and transition path, which is what keeps
	// reconciliation idempotent.
	FetchStatus(ctx context.Context, orderNumber string) (*WebhookResult, error)
}

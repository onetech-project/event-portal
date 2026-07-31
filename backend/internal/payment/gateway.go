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
	// CreateTransaction opens a payment session and returns the URL to send the
	// guest to. It performs a network call and MUST NOT be invoked inside a
	// database transaction (Constitution Principle IV).
	CreateTransaction(ctx context.Context, req TransactionRequest) (string, error)
	// VerifyWebhook authenticates a raw notification payload and normalizes it.
	VerifyWebhook(payload []byte, signature string) (*WebhookResult, error)
}

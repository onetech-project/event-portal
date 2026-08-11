package payment

import (
	"context"
	"errors"
)

// ErrInvalidSignature reports a notification this system could not authenticate.
// The callback handler turns it into a non-200 and processes nothing.
//
// The name predates the Manjo contract, which carries no signature — the gateway
// presents a bearer token instead. It is kept because the wire-level error code
// `INVALID_SIGNATURE` is part of the published API contract and clients branch on
// it; only its meaning narrows, from "the body's digest did not verify" to "the
// presented credential did not match".
var ErrInvalidSignature = errors.New("payment: notification could not be authenticated")

// ErrDuplicateReference reports that the gateway has already issued a code for
// this reference and will not issue another.
//
// It is deliberately not folded into a generic session-open failure. A generic
// failure leaves the order payable on a retry; this one proves a live code exists
// that this system will never hold — no call returns an existing code — so the
// order can never be paid and its seats must be released now rather than at a
// deadline that cannot end in a payment (FR-007d, FR-007e).
var ErrDuplicateReference = errors.New("payment: gateway already issued a code for this reference")

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
	//
	// token is the credential the caller presented — for Manjo, the bearer token
	// from the Authorization header. It is compared in constant time, because a
	// byte-by-byte early return would leak the expected value a character at a
	// time to anyone able to time the endpoint (FR-012a).
	VerifyWebhook(payload []byte, token string) (*WebhookResult, error)
}

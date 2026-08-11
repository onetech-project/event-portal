package notification

// ResendResponse is the body of POST /api/v1/admin/orders/:id/resend-email.
//
// It echoes the address the tickets went to — the order's buyer, the sole
// recipient (spec 011 FR-012, constitution v3.0.0) — so an admin can confirm at
// a glance that support reached the right person.
type ResendResponse struct {
	Message string `json:"message"`
	SentTo  string `json:"sent_to"`
}

// PublicResendRequest is the body of POST /api/v1/ticket/resend-email. The order
// number is the only field read; anything else a caller sends — an email address
// in particular — is ignored, so the destination stays the buyer's stored
// address (spec FR-024).
type PublicResendRequest struct {
	OrderID string `json:"order_id"`
}

// PublicResendResponse is the body of POST /api/v1/ticket/resend-email.
//
// Deliberately narrower than ResendResponse: this endpoint is unauthenticated, so
// it names neither the recipient nor whether the order exists. Every outcome that
// is not a rate-limit returns this same body, which is what stops the endpoint
// from being used to probe which order numbers are real (spec FR-026).
//
// RetryAfterSeconds is safe to carry alongside that silence precisely because it
// is identical across every accepted outcome: it describes the cooldown, which
// every attempt spends alike (spec 012 FR-021m), not what the attempt found. It
// is what the confirmation screen counts down from, so the wait is never guessed
// locally (FR-021j).
type PublicResendResponse struct {
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retry_after_seconds"`
}

// PublicRetryAfter is the detail body carried by the 429, in the envelope's data
// slot — the same place PAYMENT_ALREADY_STARTED puts the current QR payload.
//
// A header would have been the other option and was rejected: a cross-origin page
// cannot read one that is not named in Access-Control-Expose-Headers, and the
// envelope already has a place for per-error detail.
type PublicRetryAfter struct {
	RetryAfterSeconds int `json:"retry_after_seconds"`
}

// PublicResendMessage is the single sentence that endpoint ever returns. It is a
// constant because the non-disclosure property depends on every path returning
// exactly the same bytes.
const PublicResendMessage = "If that order exists, its ticket email has been sent again."

// PublicResendRateLimitedMessage is what a caller inside the cooldown is told.
// It reads as a wait rather than as a failure (FR-021q): the two have opposite
// remedies, and a guest who cannot tell them apart stops trying.
const PublicResendRateLimitedMessage = "That email was just sent. Please wait before asking again."

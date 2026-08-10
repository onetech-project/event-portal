package payment

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pgauto/cdtc/callback"
	"github.com/pgauto/cdtc/method"
	"github.com/pgauto/cdtc/paynet"
	"github.com/pgauto/cdtc/status"

	"github.com/manjo/ticketing/backend/pkg/logger"
)

// sessionOpenPath is where a payment session is opened.
const sessionOpenPath = "/v1/manjo/transaction/incoming"

// maxReferenceLength is the gateway's cap on the transaction reference. Order
// numbers are 19 characters, so this is headroom rather than a constraint — but
// it is enforced by refusing, never by truncating: a truncated reference would
// come back on the callback matching no order, turning a caught misconfiguration
// into a payment nobody can attribute (FR-010a).
const maxReferenceLength = 25

// Session-open retry budget. A guest is watching a spinner, so this is
// deliberately NOT the gateway's own ten-second pacing — correct for a
// background callback, unacceptable in a checkout (FR-007c).
const (
	sessionOpenAttempts = 3
	sessionOpenBudget   = 20 * time.Second
)

// sessionOpenBackoff is the pause before each retry. Short and fixed: the point
// is to ride out a blip, not to wait out an outage.
var sessionOpenBackoff = []time.Duration{300 * time.Millisecond, 800 * time.Millisecond}

// sessionRemark is the human-readable remark carried on the session. It surfaces
// inside the QRIS payload at tag 62→08, which is length-constrained, so it stays
// a short constant rather than embedding the order number — the reference field
// already carries that, and the gateway echoes it back on every notification.
const sessionRemark = "Payment for goods"

// ManjoConfig is everything the gateway adapter needs.
type ManjoConfig struct {
	// BaseURL is the gateway's address. Required — there is no default.
	BaseURL string
	// ServerKey and ClientKey travel in the request body as
	// ac.cr.{client_secret,client_id}. The gateway checks they are present, not
	// what they are, so they grant no privilege — but they must never reach a log,
	// an error message, or a guest-facing response (FR-029, FR-002a).
	ServerKey string
	ClientKey string
	// CallbackToken is the bearer token inbound notifications must present.
	CallbackToken string
	// ExpectedWindow is how long a payment code is expected to stay payable. It is
	// NOT sent to the gateway and it is NOT a cap: an adopted expiry is used
	// whatever its length (FR-009c). It has exactly two uses — the fallback
	// deadline when nothing usable came back (FR-009b), and the yardstick an
	// adopted expiry is measured against so a materially different one is noticed
	// rather than silently accepted (FR-009d).
	ExpectedWindow time.Duration
	// HTTPClient is injectable so the request shape, the response parsing, and the
	// token check are all testable without reaching the network — which is also
	// what lets the whole purchase flow run against a stub (FR-008a).
	HTTPClient *http.Client
	Log        *logger.Logger
	// now is injectable so expiry handling can be exercised at a fixed instant.
	now func() time.Time
}

// ManjoGateway speaks the Manjo REST contract directly. No vendor SDK is
// involved — none exists, and none is wanted: the wire shapes come from the
// shared contract module so there is one source of truth per shape.
type ManjoGateway struct {
	cfg ManjoConfig
}

// NewManjoGateway builds the gateway.
func NewManjoGateway(cfg ManjoConfig) *ManjoGateway {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Log == nil {
		cfg.Log = logger.New(logger.ParseLevel("info"))
	}
	if cfg.now == nil {
		cfg.now = time.Now
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	return &ManjoGateway{cfg: cfg}
}

// Name identifies this provider on orders and payment log rows.
func (g *ManjoGateway) Name() string { return "manjo" }

// credential is the vestigial key pair the gateway validates the presence of.
//
// It is a local shape rather than a contract-module one because the module types
// it as `any` (paynet.Account.Credential): the field's contents are the payment
// network's business, not the contract's.
type credential struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// CreateTransaction opens a QRIS payment session and returns the payload the
// guest scans plus the deadline the gateway will enforce.
func (g *ManjoGateway) CreateTransaction(ctx context.Context, req TransactionRequest) (PaymentSession, error) {
	if len(req.OrderNumber) > maxReferenceLength {
		return PaymentSession{}, fmt.Errorf(
			"manjo: reference %q is %d characters, over the gateway's %d limit",
			req.OrderNumber, len(req.OrderNumber), maxReferenceLength)
	}
	if req.OrderNumber == "" {
		return PaymentSession{}, errors.New("manjo: session open needs a reference")
	}

	// Exactly the six required fields plus the optional payer name. `mp` is
	// omitted — the QR method payload is an empty struct — and no expiry is
	// requested at all: the gateway applies and reports its own (FR-002c).
	//
	// The amount is the order's frozen total sent unchanged. Recomputing fees at
	// request time would let the figure in the code disagree with the one the
	// guest agreed to (FR-002b).
	payload := paynet.TransactionRequest[method.Method]{
		RefID:     req.OrderNumber,
		Amount:    req.GrossAmount.InexactFloat64(),
		Currency:  "IDR",
		Method:    method.QR,
		Remarks:   sessionRemark,
		PayorName: req.CustomerName,
		NetworkAccount: paynet.Account{
			Credential: credential{
				ClientID:     g.cfg.ClientKey,
				ClientSecret: g.cfg.ServerKey,
			},
		},
	}

	raw, httpStatus, err := g.openSessionWithRetry(ctx, payload, req.OrderNumber)
	if err != nil {
		return PaymentSession{}, err
	}

	return g.parseSession(ctx, raw, httpStatus, req.OrderNumber)
}

// openSessionWithRetry performs the session open, retrying only faults that a
// retry can actually fix.
//
// Reusing the reference unchanged is what makes retrying safe (FR-007b): the
// gateway refuses a reference it has already issued a code for, so a retry can
// never produce a second live code for one order. That also makes a *timeout*
// retryable — the worst case is a duplicate-reference reply, which is a definite
// answer rather than a second charge.
func (g *ManjoGateway) openSessionWithRetry(
	ctx context.Context,
	payload paynet.TransactionRequest[method.Method],
	reference string,
) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, sessionOpenBudget)
	defer cancel()

	var lastErr error
	for attempt := 1; attempt <= sessionOpenAttempts; attempt++ {
		raw, httpStatus, err := g.post(ctx, sessionOpenPath, payload)
		if err == nil && httpStatus < 500 {
			return raw, httpStatus, nil
		}

		if err != nil {
			lastErr = err
		} else {
			// A 5xx is the gateway telling us it did not handle the request. It is
			// retried on the same footing as a network fault, and for the same
			// reason: the duplicate-reference rule makes a second attempt safe even
			// if the first did land.
			lastErr = fmt.Errorf("manjo: session open returned %d", httpStatus)
		}

		if attempt == sessionOpenAttempts {
			break
		}
		// Stop early rather than sleep into a budget that has already run out —
		// waiting to fail is the one thing worse than failing.
		pause := sessionOpenBackoff[attempt-1]
		select {
		case <-ctx.Done():
			return nil, 0, fmt.Errorf("manjo: session open budget exhausted after %d attempts: %w", attempt, lastErr)
		case <-time.After(pause):
		}

		g.cfg.Log.WarnContext(ctx, "retrying payment session open under the same reference",
			"reference", reference, "attempt", attempt+1, "of", sessionOpenAttempts, "error", lastErr.Error())
	}

	return nil, 0, fmt.Errorf("manjo: session open failed after %d attempts: %w", sessionOpenAttempts, lastErr)
}

// parseSession turns the gateway's answer into a PaymentSession.
func (g *ManjoGateway) parseSession(ctx context.Context, raw []byte, httpStatus int, reference string) (PaymentSession, error) {
	var parsed paynet.TransactionResponse[method.Method]
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return PaymentSession{}, fmt.Errorf("manjo: decode session response (http %d): %w", httpStatus, err)
	}

	// Only `s: true` is a session. Anything else is a failed open, whatever the
	// HTTP status said (FR-003).
	if !parsed.Success {
		detail := errorText(parsed.Error)
		if isDuplicateReference(detail) {
			// Terminal, and distinct from a generic failure: a code exists for this
			// reference that this system will never hold, because no call returns an
			// existing one. Retrying cannot help and neither can waiting (FR-007d).
			return PaymentSession{}, fmt.Errorf("manjo: %s: %w", detail, ErrDuplicateReference)
		}
		return PaymentSession{}, fmt.Errorf("manjo: session open refused (http %d): %s", httpStatus, detail)
	}

	qr, expiryErr := decodeQRResponse(parsed.Data.MethodResponse)
	if qr.RawQR == "" {
		// Without a payload there is nothing to render, and showing the guest a
		// payment page with no code is worse than failing the checkout: failing
		// releases their seats so they can retry (FR-006).
		return PaymentSession{}, errors.New("manjo: session response carried no QR payload")
	}

	expiresAt, fromGateway := g.resolveExpiry(ctx, qr.ExpiredAt, expiryErr, reference)

	return PaymentSession{
		ProviderRef:       parsed.Data.ExternalRefID,
		QRString:          qr.RawQR,
		QRImageURL:        qr.URLQR,
		ExpiresAt:         expiresAt,
		ExpiryFromGateway: fromGateway,
	}, nil
}

// resolveExpiry decides the deadline and says whether the gateway supplied it.
//
// Three ways the gateway's value can be unusable, and they must stay
// distinguishable in the logs even though they share an outcome:
//
//   - absent — the zero time. Note this is NOT an error and NOT a null: the
//     contract types the field as a timestamp, so an omitted `qr_ea` decodes to a
//     perfectly valid year-1 value. Treated as an ordinary expiry it would read
//     as "already expired", which routes to the right fallback for the wrong
//     reason and loses the signal entirely — the operator would never learn the
//     gateway stopped sending it (FR-009b).
//   - unreadable — the value failed to parse. The deadline is affected; whether
//     the guest can pay is not (FR-009e).
//   - already past — a real value, but one no guest could ever pay against.
//
// A usable value is adopted whatever its length. It is never capped and never
// substituted: the gateway is the one enforcing it, so a shorter deadline of our
// own would strand payments in the gap and a longer one would show a countdown
// that outlives the code (FR-009c).
func (g *ManjoGateway) resolveExpiry(ctx context.Context, expiredAt time.Time, expiryErr error, reference string) (time.Time, bool) {
	fallback := g.cfg.now().Add(g.cfg.ExpectedWindow)

	switch {
	case expiryErr != nil:
		g.cfg.Log.ErrorContext(ctx, "gateway expiry could not be read; falling back to the configured window and serving the code anyway",
			"reference", reference, "error", expiryErr.Error(), "fallback_expires_at", fallback.UTC())
		return fallback, false

	case expiredAt.IsZero():
		g.cfg.Log.ErrorContext(ctx, "gateway returned no expiry; falling back to the configured window",
			"reference", reference, "fallback_expires_at", fallback.UTC())
		return fallback, false

	case !expiredAt.After(g.cfg.now()):
		g.cfg.Log.ErrorContext(ctx, "gateway returned an expiry that has already passed; falling back to the configured window",
			"reference", reference, "gateway_expires_at", expiredAt.UTC(), "fallback_expires_at", fallback.UTC())
		return fallback, false
	}

	// Adopted — but a length far from what this deployment expects is worth
	// knowing about, because it means the gateway's configuration and ours have
	// drifted apart. Signalled, not corrected (FR-009d).
	if g.cfg.ExpectedWindow > 0 {
		remaining := expiredAt.Sub(g.cfg.now())
		if remaining > 2*g.cfg.ExpectedWindow || remaining*2 < g.cfg.ExpectedWindow {
			g.cfg.Log.WarnContext(ctx, "gateway expiry differs materially from the configured expectation; adopting it unchanged",
				"reference", reference,
				"gateway_expires_at", expiredAt.UTC(),
				"gateway_window", remaining.String(),
				"expected_window", g.cfg.ExpectedWindow.String())
		}
	}

	return expiredAt, true
}

// decodeQRResponse reads the method response, keeping a bad timestamp from
// costing the guest their code.
//
// The contract module's QRResponse types `qr_ea` as a timestamp, so an
// unparseable value fails the decode of the whole struct. Whether the other
// fields survive that depends on encoding/json's error-accumulation behaviour —
// precisely the kind of implicit dependency not to build a payment path on. So
// the retry is explicit: drop the offending key and decode again. The returned
// error describes the expiry alone; the QR fields are either present or the
// response genuinely had none.
func decodeQRResponse(raw any) (method.QRResponse, error) {
	if qr, err := asQRResponse(raw); err == nil {
		return qr, nil
	} else {
		fields, ok := raw.(map[string]any)
		if !ok {
			return method.QRResponse{}, err
		}
		stripped := make(map[string]any, len(fields))
		for k, v := range fields {
			if k == "qr_ea" {
				continue
			}
			stripped[k] = v
		}
		qr, retryErr := asQRResponse(stripped)
		if retryErr != nil {
			// Not the timestamp after all — the whole shape is wrong.
			return method.QRResponse{}, retryErr
		}
		return qr, err
	}
}

func asQRResponse(raw any) (method.QRResponse, error) {
	decoded, err := method.QR.Response(raw)
	if err != nil {
		return method.QRResponse{}, err
	}
	qr, ok := decoded.(*method.QRResponse)
	if !ok || qr == nil {
		return method.QRResponse{}, fmt.Errorf("manjo: method response was %T, not a QR response", decoded)
	}
	return *qr, nil
}

// duplicateReferenceMarkers are the error strings that identify the gateway's
// refusal to issue a second code for a reference.
//
// Matching on text is not what anyone would choose — the contract types the
// error as `any`, so there is no code to switch on — and every entry here is a
// guess at wording rather than an observation. **We do not know what this
// gateway calls a duplicate reference.** That is stated plainly because the
// alternative was tried and was much worse.
//
// `MERCHANT_NOT_AVAILABLE` was on this list and has been removed. It was added
// on the strength of one experiment — replaying a reference returned it, while a
// fresh reference before and after both succeeded — and read as "the name is
// misleading, the merchant is available and the reference is taken". That
// inference was wrong. The string means exactly what it says: the gateway had no
// available merchant to route to. Merchant availability is scored and
// volume-capped on the gateway's side, so it changes between calls, which is
// what made the experiment look conclusive when it was not.
//
// The cost of the mistake was not the "seats released a little early" this
// comment used to claim. A merchant outage made **every** checkout terminal:
// each guest's order was cancelled on the spot and they were told to book again,
// which failed identically, forever. Getting this wrong in this direction takes
// the shop offline; getting it wrong in the other direction holds one order's
// seats until its deadline. Prefer the second.
//
// So an unrecognised refusal now falls through to the generic failure path,
// where the order stays PENDING with its quota held and the guest can retry
// (FR-007). A genuine duplicate that goes undetected loses only FR-007e's prompt
// release — the seats come back on the deadline instead.
var duplicateReferenceMarkers = []string{
	"duplicate",
	"already exist",
	"already registered",
	"ref id exist",
}

func isDuplicateReference(detail string) bool {
	lowered := strings.ToLower(detail)
	for _, marker := range duplicateReferenceMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// errorText renders the gateway's untyped error field for a log line. It is
// never shown to a guest.
func errorText(v any) string {
	if v == nil {
		return "no error detail"
	}
	if s, ok := v.(string); ok {
		return s
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(encoded)
}

// post performs one session-open call and returns the raw body and HTTP status.
func (g *ManjoGateway) post(ctx context.Context, path string, payload any) ([]byte, int, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("manjo: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.BaseURL+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, 0, fmt.Errorf("manjo: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := g.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		// The error text can name the URL but never the body, which is where the
		// credential travels (FR-029).
		return nil, 0, fmt.Errorf("manjo: call %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxWebhookBody))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("manjo: read %s response: %w", path, err)
	}
	return raw, resp.StatusCode, nil
}

// VerifyWebhook authenticates an inbound notification and normalizes it.
//
// The gateway presents a bearer token and nothing else — the notification body
// carries no signature, no digest, and no field that could authenticate it — so
// the token is the whole mechanism. A body this system cannot parse is a body it
// cannot authenticate, and is refused on the same footing as a bad token.
func (g *ManjoGateway) VerifyWebhook(payload []byte, token string) (*WebhookResult, error) {
	// Constant-time, and unconditional: comparing lengths first, or returning
	// early on the first differing byte, would leak the expected token a character
	// at a time to anyone able to time this endpoint (FR-012a).
	if subtle.ConstantTimeCompare([]byte(token), []byte(g.cfg.CallbackToken)) != 1 {
		return nil, ErrInvalidSignature
	}
	// An empty configured token would otherwise authenticate an empty presented
	// one. Configuration refuses to start without it, so this is a second line of
	// defence rather than the only one.
	if g.cfg.CallbackToken == "" {
		return nil, fmt.Errorf("manjo: no callback token configured: %w", ErrInvalidSignature)
	}

	var notification callback.Request
	if err := json.Unmarshal(payload, &notification); err != nil {
		return nil, fmt.Errorf("manjo: decode notification: %w: %w", err, ErrInvalidSignature)
	}
	if notification.RefID == "" {
		return nil, fmt.Errorf("manjo: notification carries no reference: %w", ErrInvalidSignature)
	}

	statusPresent := hasStatusField(payload)

	return &WebhookResult{
		OrderNumber:       notification.RefID,
		TransactionID:     notification.NetTrxID,
		Status:            notification.Status,
		StatusPresent:     statusPresent,
		TransactionStatus: statusName(notification.Status, statusPresent),
		PaymentType:       "qris",
		TransactionType:   notification.TrxType.String(),
		IsDeposit:         notification.TrxType == callback.DEPOSIT,
		RawPayload:        payload,
	}, nil
}

// hasStatusField reports whether the notification actually carried a status.
//
// Pending is the enum's zero value, so a body that omits `s` decodes as "pending"
// rather than erroring. The order outcome is the same either way — both change
// nothing — but the audit record must not claim the gateway said "pending" when
// it said nothing at all (FR-018).
func hasStatusField(payload []byte) bool {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return false
	}
	value, present := fields["s"]
	return present && string(value) != "null"
}

// statusName renders a status for the audit record, distinguishing an absent
// value from an explicitly-sent one.
func statusName(s status.Status, present bool) string {
	if !present {
		return "ABSENT"
	}
	return s.String()
}

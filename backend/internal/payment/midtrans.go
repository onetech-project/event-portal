package payment

import (
	"bytes"
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// Sandbox and production Core API endpoints. The concrete one is chosen at wiring
// time from config so nothing else in the codebase knows which environment is
// active.
//
// Note the host: Core API lives on api.*, not the app.* host the old SNAP
// redirect used. Pointing this at app.* fails with a confusing 404.
const (
	CoreAPISandboxBaseURL    = "https://api.sandbox.midtrans.com"
	CoreAPIProductionBaseURL = "https://api.midtrans.com"
)

const (
	chargePath = "/v2/charge"
	// qrisAcquirer decides which network acquires the payment. GoPay issues a
	// standard interoperable QRIS payload, so any QRIS-capable app can scan it;
	// airpay_shopee would narrow that to Shopee's own apps.
	qrisAcquirer = "gopay"
)

// MidtransGateway is the Midtrans Core API implementation of Gateway.
//
// It speaks the HTTP API directly rather than through the vendor SDK so the base
// URL and HTTP client are injectable, which is what makes the request shape, the
// response parsing, and the signature check testable without reaching the
// network.
type MidtransGateway struct {
	serverKey  string
	baseURL    string
	httpClient *http.Client
	// paymentExpiry is how long an issued QRIS code stays payable. It is sent to
	// the provider so both sides agree on the deadline rather than each computing
	// their own.
	paymentExpiry time.Duration
}

// NewMidtransGateway builds the gateway. baseURL selects sandbox or production.
func NewMidtransGateway(serverKey, baseURL string, paymentExpiry time.Duration, httpClient *http.Client) *MidtransGateway {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &MidtransGateway{
		serverKey:     serverKey,
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		httpClient:    httpClient,
		paymentExpiry: paymentExpiry,
	}
}

// authHeader is HTTP Basic with the server key as username and no password,
// which is how every Core API endpoint authenticates.
func (g *MidtransGateway) authHeader() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(g.serverKey+":"))
}

// Name identifies this provider on orders and payment log rows.
func (g *MidtransGateway) Name() string { return "midtrans" }

type chargeRequest struct {
	PaymentType        string             `json:"payment_type"`
	TransactionDetails transactionDetails `json:"transaction_details"`
	CustomerDetails    customerDetails    `json:"customer_details"`
	ItemDetails        []itemDetail       `json:"item_details,omitempty"`
	QRIS               qrisDetails        `json:"qris"`
	CustomExpiry       customExpiry       `json:"custom_expiry"`
}

type transactionDetails struct {
	OrderID     string `json:"order_id"`
	GrossAmount int64  `json:"gross_amount"`
}

type customerDetails struct {
	FirstName string `json:"first_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

type itemDetail struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Price    int64  `json:"price"`
	Quantity int32  `json:"quantity"`
}

type qrisDetails struct {
	Acquirer string `json:"acquirer"`
}

type customExpiry struct {
	ExpiryDuration int    `json:"expiry_duration"`
	Unit           string `json:"unit"`
}

type chargeAction struct {
	Name   string `json:"name"`
	Method string `json:"method"`
	URL    string `json:"url"`
}

type chargeResponse struct {
	StatusCode        string         `json:"status_code"`
	StatusMessage     string         `json:"status_message"`
	TransactionID     string         `json:"transaction_id"`
	OrderID           string         `json:"order_id"`
	GrossAmount       string         `json:"gross_amount"`
	TransactionStatus string         `json:"transaction_status"`
	FraudStatus       string         `json:"fraud_status"`
	PaymentType       string         `json:"payment_type"`
	QRString          string         `json:"qr_string"`
	ExpiryTime        string         `json:"expiry_time"`
	Actions           []chargeAction `json:"actions"`
	ValidationMessage []string       `json:"validation_messages"`
}

// qrCodeAction returns the URL of the provider-hosted QR image, if it offered one.
func (r chargeResponse) qrCodeAction() string {
	for _, action := range r.Actions {
		if action.Name == "generate-qr-code" {
			return action.URL
		}
	}
	return ""
}

// CreateTransaction opens a QRIS payment session and returns the payload the
// guest scans plus the deadline it stops working at.
func (g *MidtransGateway) CreateTransaction(ctx context.Context, req TransactionRequest) (PaymentSession, error) {
	// Midtrans requires whole rupiah for IDR, and requires item prices to sum to
	// gross_amount. Truncating both consistently keeps that invariant.
	items := make([]itemDetail, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, itemDetail{
			ID:       item.ID,
			Name:     truncateRunes(item.Name, 50), // the API rejects longer names
			Price:    toRupiah(item.Price),
			Quantity: item.Quantity,
		})
	}

	payload := chargeRequest{
		PaymentType: "qris",
		TransactionDetails: transactionDetails{
			OrderID:     req.OrderNumber,
			GrossAmount: toRupiah(req.GrossAmount),
		},
		CustomerDetails: customerDetails{
			FirstName: req.CustomerName,
			Email:     req.CustomerEmail,
			Phone:     req.CustomerPhone,
		},
		ItemDetails: items,
		QRIS:        qrisDetails{Acquirer: qrisAcquirer},
		CustomExpiry: customExpiry{
			// Minutes because the provider's scheduler works in whole minutes and
			// documents 15 as its reliable floor; config enforces that floor.
			ExpiryDuration: int(g.paymentExpiry.Minutes()),
			Unit:           "minute",
		},
	}

	raw, status, err := g.do(ctx, http.MethodPost, chargePath, payload)
	if err != nil {
		return PaymentSession{}, err
	}

	var parsed chargeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return PaymentSession{}, fmt.Errorf("midtrans: decode charge response (status %d): %w", status, err)
	}
	if status < 200 || status >= 300 {
		return PaymentSession{}, fmt.Errorf("midtrans: charge returned %d: %s %s",
			status, parsed.StatusMessage, strings.Join(parsed.ValidationMessage, "; "))
	}
	if parsed.QRString == "" {
		// Without the payload there is nothing to render, and sending the guest to
		// a page with no code is worse than failing checkout: the compensating
		// transaction releases their seats so they can retry.
		return PaymentSession{}, errors.New("midtrans: charge response contained no qr_string")
	}

	expiresAt, err := parseExpiryTime(parsed.ExpiryTime)
	if err != nil {
		return PaymentSession{}, err
	}

	return PaymentSession{
		ProviderRef: parsed.TransactionID,
		QRString:    parsed.QRString,
		QRImageURL:  parsed.qrCodeAction(),
		ExpiresAt:   expiresAt,
	}, nil
}

type statusResponse struct {
	// StatusCode is Midtrans's own logical code, carried in the body as a string.
	// It does not always match the HTTP status: an unknown transaction comes back
	// as HTTP 200 with "404" in here, so trusting the HTTP status alone reads that
	// as a valid answer with an empty transaction_status.
	StatusCode        string `json:"status_code"`
	TransactionID     string `json:"transaction_id"`
	OrderID           string `json:"order_id"`
	TransactionStatus string `json:"transaction_status"`
	FraudStatus       string `json:"fraud_status"`
	PaymentType       string `json:"payment_type"`
	StatusMessage     string `json:"status_message"`
}

// FetchStatus reads the provider's authoritative status for an order.
//
// The result is deliberately the same shape a verified notification produces, so
// the caller runs it through the identical status mapping and transition path —
// which is what makes a reconciliation exactly as idempotent as a webhook.
func (g *MidtransGateway) FetchStatus(ctx context.Context, orderNumber string) (*WebhookResult, error) {
	// Order numbers are generated by this system from an alphanumeric alphabet,
	// but escaping keeps a future format change from silently building a broken
	// path.
	path := "/v2/" + url.PathEscape(orderNumber) + "/status"

	raw, status, err := g.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var parsed statusResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("midtrans: decode status response (status %d): %w", status, err)
	}
	// The provider has no record of this order: nothing to reconcile, and not an
	// error the caller can act on differently. Checked against both codes because
	// Midtrans reports this as HTTP 200 with "404" in the body.
	if status == http.StatusNotFound || parsed.StatusCode == "404" {
		return nil, ErrOrderNotFound
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("midtrans: status returned %d: %s", status, parsed.StatusMessage)
	}
	// Anything else non-2xx in the body is a real failure — an auth problem, say.
	// Falling through would hand the caller an empty transaction_status, which
	// reads as "unrecognized status" and hides the cause.
	if parsed.StatusCode != "" && !strings.HasPrefix(parsed.StatusCode, "2") {
		return nil, fmt.Errorf("midtrans: status returned code %s: %s", parsed.StatusCode, parsed.StatusMessage)
	}

	return &WebhookResult{
		OrderNumber:       parsed.OrderID,
		TransactionID:     parsed.TransactionID,
		TransactionStatus: parsed.TransactionStatus,
		FraudStatus:       parsed.FraudStatus,
		PaymentType:       parsed.PaymentType,
		RawPayload:        raw,
	}, nil
}

// do performs one authenticated Core API call and returns the raw body and HTTP
// status. Both endpoints share the auth, headers, and body-size bound.
func (g *MidtransGateway) do(ctx context.Context, method, path string, payload any) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, fmt.Errorf("midtrans: encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, g.baseURL+path, body)
	if err != nil {
		return nil, 0, fmt.Errorf("midtrans: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", g.authHeader())

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("midtrans: call %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("midtrans: read %s response: %w", path, err)
	}
	return raw, resp.StatusCode, nil
}

// parseExpiryTime reads the provider's expiry_time, which is formatted in
// Jakarta local time with no zone marker. Parsing it as UTC would shift the
// guest's countdown by seven hours.
func parseExpiryTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, errors.New("midtrans: charge response contained no expiry_time")
	}

	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, jakarta)
	if err != nil {
		return time.Time{}, fmt.Errorf("midtrans: parse expiry_time %q: %w", value, err)
	}
	return parsed.UTC(), nil
}

// jakarta is the zone Midtrans reports timestamps in (WIB, UTC+7). It is built
// from a fixed offset rather than the tzdata name so the parse cannot fail on a
// container image without zone files.
var jakarta = time.FixedZone("WIB", 7*60*60)

type midtransNotification struct {
	OrderID           string `json:"order_id"`
	StatusCode        string `json:"status_code"`
	GrossAmount       string `json:"gross_amount"`
	SignatureKey      string `json:"signature_key"`
	TransactionID     string `json:"transaction_id"`
	TransactionStatus string `json:"transaction_status"`
	FraudStatus       string `json:"fraud_status"`
	PaymentType       string `json:"payment_type"`
}

// VerifyWebhook authenticates a Midtrans notification.
//
// Midtrans carries its signature inside the JSON body as signature_key rather than
// in a header, so the signature argument required by the Gateway interface is
// accepted as an override for providers that do use a header, and ignored when
// empty. The digest covers order_id, status_code, and gross_amount, so a replayed
// notification with any of those altered fails to verify.
func (g *MidtransGateway) VerifyWebhook(payload []byte, signature string) (*WebhookResult, error) {
	// Every failure below is reported as ErrInvalidSignature, including an
	// unparseable body: a payload we cannot read is a payload we cannot
	// authenticate, and the handler must answer 401 rather than 500 — a 5xx makes
	// the provider retry a notification that can never succeed.
	var notification midtransNotification
	if err := json.Unmarshal(payload, &notification); err != nil {
		return nil, fmt.Errorf("midtrans: decode notification: %w: %w", err, ErrInvalidSignature)
	}
	if notification.OrderID == "" {
		return nil, fmt.Errorf("midtrans: notification has no order_id: %w", ErrInvalidSignature)
	}

	provided := signature
	if provided == "" {
		provided = notification.SignatureKey
	}
	if provided == "" {
		return nil, ErrInvalidSignature
	}

	sum := sha512.Sum512([]byte(
		notification.OrderID + notification.StatusCode + notification.GrossAmount + g.serverKey))
	expected := hex.EncodeToString(sum[:])

	// Constant-time comparison: a timing oracle here would let an attacker
	// discover a valid signature byte by byte.
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(provided)), []byte(expected)) != 1 {
		return nil, ErrInvalidSignature
	}

	return &WebhookResult{
		OrderNumber:       notification.OrderID,
		TransactionID:     notification.TransactionID,
		TransactionStatus: notification.TransactionStatus,
		FraudStatus:       notification.FraudStatus,
		PaymentType:       notification.PaymentType,
		RawPayload:        payload,
	}, nil
}

// toRupiah truncates a decimal amount to whole rupiah, which is the only form
// Midtrans accepts for IDR.
func toRupiah(amount decimal.Decimal) int64 {
	return amount.Truncate(0).IntPart()
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}

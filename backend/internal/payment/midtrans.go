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
	"strings"

	"github.com/shopspring/decimal"
)

// Sandbox and production SNAP endpoints. The concrete one is chosen at wiring time
// from config so nothing else in the codebase knows which environment is active.
const (
	SnapSandboxBaseURL    = "https://app.sandbox.midtrans.com"
	SnapProductionBaseURL = "https://app.midtrans.com"
)

const snapTransactionsPath = "/snap/v1/transactions"

// MidtransGateway is the Midtrans SNAP implementation of Gateway.
//
// It speaks the SNAP HTTP API directly rather than through the vendor SDK so the
// base URL and HTTP client are injectable, which is what makes the request shape
// and the signature check testable without reaching the network.
type MidtransGateway struct {
	serverKey  string
	baseURL    string
	httpClient *http.Client
}

// NewMidtransGateway builds the gateway. baseURL selects sandbox or production.
func NewMidtransGateway(serverKey, baseURL string, httpClient *http.Client) *MidtransGateway {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &MidtransGateway{
		serverKey:  serverKey,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: httpClient,
	}
}

// Name identifies this provider on orders and payment log rows.
func (g *MidtransGateway) Name() string { return "midtrans" }

type snapRequest struct {
	TransactionDetails snapTransactionDetails `json:"transaction_details"`
	CustomerDetails    snapCustomerDetails    `json:"customer_details"`
	ItemDetails        []snapItemDetail       `json:"item_details,omitempty"`
}

type snapTransactionDetails struct {
	OrderID     string `json:"order_id"`
	GrossAmount int64  `json:"gross_amount"`
}

type snapCustomerDetails struct {
	FirstName string `json:"first_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

type snapItemDetail struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Price    int64  `json:"price"`
	Quantity int32  `json:"quantity"`
}

type snapResponse struct {
	Token         string   `json:"token"`
	RedirectURL   string   `json:"redirect_url"`
	ErrorMessages []string `json:"error_messages"`
}

// CreateTransaction opens a SNAP payment session and returns its redirect URL.
func (g *MidtransGateway) CreateTransaction(ctx context.Context, req TransactionRequest) (string, error) {
	// Midtrans requires whole rupiah for IDR, and requires item prices to sum to
	// gross_amount. Truncating both consistently keeps that invariant.
	items := make([]snapItemDetail, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, snapItemDetail{
			ID:       item.ID,
			Name:     truncateRunes(item.Name, 50), // SNAP rejects longer names
			Price:    toRupiah(item.Price),
			Quantity: item.Quantity,
		})
	}

	payload := snapRequest{
		TransactionDetails: snapTransactionDetails{
			OrderID:     req.OrderNumber,
			GrossAmount: toRupiah(req.GrossAmount),
		},
		CustomerDetails: snapCustomerDetails{
			FirstName: req.CustomerName,
			Email:     req.CustomerEmail,
			Phone:     req.CustomerPhone,
		},
		ItemDetails: items,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("midtrans: encode request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+snapTransactionsPath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("midtrans: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(g.serverKey+":")))

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("midtrans: call SNAP: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("midtrans: read SNAP response: %w", err)
	}

	var parsed snapResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("midtrans: decode SNAP response (status %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("midtrans: SNAP returned %d: %s",
			resp.StatusCode, strings.Join(parsed.ErrorMessages, "; "))
	}
	if parsed.RedirectURL == "" {
		return "", errors.New("midtrans: SNAP response contained no redirect_url")
	}
	return parsed.RedirectURL, nil
}

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

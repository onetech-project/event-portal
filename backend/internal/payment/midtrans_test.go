package payment_test

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/payment"
)

const serverKey = "SB-Mid-server-TESTKEY"

func midtransSignature(orderID, statusCode, grossAmount, key string) string {
	sum := sha512.Sum512([]byte(orderID + statusCode + grossAmount + key))
	return hex.EncodeToString(sum[:])
}

func notification(t *testing.T, overrides map[string]any) []byte {
	t.Helper()
	body := map[string]any{
		"order_id":           "ORD-20260731-ABCDEF",
		"status_code":        "200",
		"gross_amount":       "300000.00",
		"transaction_status": "settlement",
		"transaction_id":     "tx-123",
		"payment_type":       "bank_transfer",
		"fraud_status":       "accept",
	}
	for k, v := range overrides {
		body[k] = v
	}
	if _, ok := body["signature_key"]; !ok {
		body["signature_key"] = midtransSignature(
			body["order_id"].(string), body["status_code"].(string),
			body["gross_amount"].(string), serverKey)
	}
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	return raw
}

// --- CreateTransaction ----------------------------------------------------

func TestCreateTransactionReturnsTheRedirectURL(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"snap-token","redirect_url":"https://pay.example.com/snap-token"}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, server.Client())

	url, err := gw.CreateTransaction(context.Background(), payment.TransactionRequest{
		OrderNumber:   "ORD-20260731-ABCDEF",
		GrossAmount:   decimal.RequireFromString("300000.00"),
		CustomerName:  "Budi Santoso",
		CustomerEmail: "budi@example.com",
		CustomerPhone: "+628123456789",
		Items: []payment.TransactionItem{
			{ID: "tt-1", Name: "Regular", Price: decimal.RequireFromString("150000.00"), Quantity: 2},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "https://pay.example.com/snap-token", url)

	assert.True(t, len(gotAuth) > 6 && gotAuth[:6] == "Basic ", "SNAP uses HTTP Basic with the server key")

	details := gotBody["transaction_details"].(map[string]any)
	assert.Equal(t, "ORD-20260731-ABCDEF", details["order_id"])
	assert.InDelta(t, 300000.0, details["gross_amount"], 0.001,
		"IDR amounts are sent as whole rupiah")

	items := gotBody["item_details"].([]any)
	require.Len(t, items, 1)
	assert.InDelta(t, 150000.0, items[0].(map[string]any)["price"], 0.001)
}

func TestCreateTransactionFailsOnAGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error_messages":["Access denied"]}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, server.Client())

	_, err := gw.CreateTransaction(context.Background(), payment.TransactionRequest{
		OrderNumber: "ORD-1", GrossAmount: decimal.NewFromInt(1000),
	})

	require.Error(t, err)
}

func TestCreateTransactionFailsWhenNoRedirectURLIsReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":"snap-token"}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, server.Client())

	_, err := gw.CreateTransaction(context.Background(), payment.TransactionRequest{
		OrderNumber: "ORD-1", GrossAmount: decimal.NewFromInt(1000),
	})

	require.Error(t, err, "a 201 with no redirect_url cannot be handed to a guest")
}

// --- VerifyWebhook --------------------------------------------------------

func TestVerifyWebhookAcceptsACorrectlySignedNotification(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	result, err := gw.VerifyWebhook(notification(t, nil), "")

	require.NoError(t, err)
	assert.Equal(t, "ORD-20260731-ABCDEF", result.OrderNumber)
	assert.Equal(t, "settlement", result.TransactionStatus)
	assert.Equal(t, "accept", result.FraudStatus)
	assert.Equal(t, "tx-123", result.TransactionID)
	assert.Equal(t, "bank_transfer", result.PaymentType)
	assert.NotEmpty(t, result.RawPayload, "the full payload is persisted for audit")
}

func TestVerifyWebhookRejectsAForgedSignature(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	_, err := gw.VerifyWebhook(notification(t, map[string]any{"signature_key": "deadbeef"}), "")

	require.Error(t, err)
	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

// The signature covers gross_amount, so an attacker cannot replay a real
// notification with the amount changed.
func TestVerifyWebhookRejectsATamperedAmount(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	raw := notification(t, nil)
	var body map[string]any
	require.NoError(t, json.Unmarshal(raw, &body))
	body["gross_amount"] = "1.00"
	tampered, err := json.Marshal(body)
	require.NoError(t, err)

	_, err = gw.VerifyWebhook(tampered, "")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhookRejectsASignatureFromAnotherMerchantsKey(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	raw := notification(t, map[string]any{
		"signature_key": midtransSignature("ORD-20260731-ABCDEF", "200", "300000.00", "SOMEONE-ELSES-KEY"),
	})

	_, err := gw.VerifyWebhook(raw, "")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

// A payload that cannot even be parsed has certainly not been authenticated, so
// it must reach the handler as a signature failure (401) rather than as an
// unexpected server error (500) that a provider would keep retrying.
func TestVerifyWebhookRejectsMalformedJSONAsUnauthenticated(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	_, err := gw.VerifyWebhook([]byte(`{not json`), "")

	require.Error(t, err)
	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhookRejectsAMissingOrderIDAsUnauthenticated(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	raw := notification(t, map[string]any{"order_id": ""})

	_, err := gw.VerifyWebhook(raw, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhookRejectsAnEmptyBodyAsUnauthenticated(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	_, err := gw.VerifyWebhook(nil, "")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhookPreservesTheRawStatusForEveryTerminalOutcome(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	for _, status := range []string{"deny", "failure", "cancel", "expire"} {
		t.Run(status, func(t *testing.T) {
			result, err := gw.VerifyWebhook(notification(t, map[string]any{
				"transaction_status": status,
			}), "")

			require.NoError(t, err)
			assert.Equal(t, status, result.TransactionStatus,
				"deny and failure must stay distinguishable even though both cancel the order")
		})
	}
}

func TestGatewayNameIdentifiesTheProvider(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	assert.Equal(t, "midtrans", gw.Name())
}

// The gateway must satisfy the abstraction the order domain depends on, so a
// different provider can be swapped in without touching that domain
// (Constitution Principle V).
func TestMidtransSatisfiesTheGatewayInterface(t *testing.T) {
	var gw payment.Gateway = payment.NewMidtransGateway(serverKey, "https://unused", http.DefaultClient)

	assert.NotNil(t, gw)
	fmt.Fprint(io.Discard, gw.Name())
}

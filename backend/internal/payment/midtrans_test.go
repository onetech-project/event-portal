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
	"strings"
	"testing"
	"time"

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

// --- CreateTransaction (QRIS charge) -------------------------------------

const chargeResponseBody = `{
  "status_code": "201",
  "status_message": "QRIS transaction is created",
  "transaction_id": "0d8178e1-c6c7-4ab4-81a6-893be9d924ab",
  "order_id": "ORD-20260731-ABCDEF",
  "gross_amount": "300000.00",
  "transaction_status": "pending",
  "payment_type": "qris",
  "expiry_time": "2026-07-31 10:15:00",
  "qr_string": "00020101021226620014COM.GO-JEK.WWW01199360091434905263034021",
  "actions": [
    {"name": "generate-qr-code", "method": "GET",
     "url": "https://api.sandbox.midtrans.com/v2/qris/0d8178e1/qr-code"}
  ]
}`

func TestCreateTransactionChargesQRISAndReturnsTheScannablePayload(t *testing.T) {
	var gotBody map[string]any
	var gotAuth, gotPath, gotMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(chargeResponseBody))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	session, err := gw.CreateTransaction(context.Background(), payment.TransactionRequest{
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

	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "/v2/charge", gotPath, "QRIS is charged through the Core API, not SNAP")
	assert.True(t, strings.HasPrefix(gotAuth, "Basic "), "the Core API uses HTTP Basic with the server key")

	assert.Equal(t, "qris", gotBody["payment_type"])
	assert.Equal(t, "gopay", gotBody["qris"].(map[string]any)["acquirer"],
		"gopay acquiring issues an interoperable QRIS payload any app can scan")

	expiry := gotBody["custom_expiry"].(map[string]any)
	assert.InDelta(t, 15.0, expiry["expiry_duration"], 0.001)
	assert.Equal(t, "minute", expiry["unit"])

	details := gotBody["transaction_details"].(map[string]any)
	assert.Equal(t, "ORD-20260731-ABCDEF", details["order_id"])
	assert.InDelta(t, 300000.0, details["gross_amount"], 0.001,
		"IDR amounts are sent as whole rupiah")

	items := gotBody["item_details"].([]any)
	require.Len(t, items, 1)
	assert.InDelta(t, 150000.0, items[0].(map[string]any)["price"], 0.001)

	// What the guest's page actually needs.
	assert.Equal(t, "00020101021226620014COM.GO-JEK.WWW01199360091434905263034021", session.QRString)
	assert.Equal(t, "https://api.sandbox.midtrans.com/v2/qris/0d8178e1/qr-code", session.QRImageURL)
	assert.Equal(t, "0d8178e1-c6c7-4ab4-81a6-893be9d924ab", session.ProviderRef)
	assert.Empty(t, session.RedirectURL, "QRIS never sends the guest anywhere")
}

// expiry_time comes back in Jakarta local time with no zone marker. Reading it
// as UTC would put the deadline seven hours out and expire every code on sight.
func TestCreateTransactionReadsExpiryTimeAsJakartaLocalTime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(chargeResponseBody))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	session, err := gw.CreateTransaction(context.Background(), payment.TransactionRequest{
		OrderNumber: "ORD-1", GrossAmount: decimal.NewFromInt(1000),
	})

	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 7, 31, 3, 15, 0, 0, time.UTC), session.ExpiresAt.UTC(),
		"10:15 WIB is 03:15 UTC")
}

func TestCreateTransactionFailsOnAGatewayError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status_message":"Access denied","validation_messages":["bad key"]}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	_, err := gw.CreateTransaction(context.Background(), payment.TransactionRequest{
		OrderNumber: "ORD-1", GrossAmount: decimal.NewFromInt(1000),
	})

	require.Error(t, err)
}

// A charge with no payload leaves nothing to render. Failing here is what makes
// checkout compensate and hand the seats back, instead of parking the guest on a
// page with a blank code.
func TestCreateTransactionFailsWhenNoQRStringIsReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"transaction_id":"tx-1","expiry_time":"2026-07-31 10:15:00"}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	_, err := gw.CreateTransaction(context.Background(), payment.TransactionRequest{
		OrderNumber: "ORD-1", GrossAmount: decimal.NewFromInt(1000),
	})

	require.Error(t, err)
}

func TestCreateTransactionFailsWhenNoExpiryTimeIsReturned(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"transaction_id":"tx-1","qr_string":"0002010102122662"}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	_, err := gw.CreateTransaction(context.Background(), payment.TransactionRequest{
		OrderNumber: "ORD-1", GrossAmount: decimal.NewFromInt(1000),
	})

	require.Error(t, err, "without a deadline there is no countdown and no expiry")
}

// --- FetchStatus ----------------------------------------------------------

func TestFetchStatusReadsTheProvidersAuthoritativeStatus(t *testing.T) {
	var gotPath, gotMethod, gotAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status_code": "200",
			"transaction_id": "tx-9",
			"order_id": "ORD-20260731-ABCDEF",
			"transaction_status": "settlement",
			"fraud_status": "accept",
			"payment_type": "qris"
		}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	result, err := gw.FetchStatus(context.Background(), "ORD-20260731-ABCDEF")

	require.NoError(t, err)
	assert.Equal(t, http.MethodGet, gotMethod)
	assert.Equal(t, "/v2/ORD-20260731-ABCDEF/status", gotPath)
	assert.True(t, strings.HasPrefix(gotAuth, "Basic "))

	// Normalized onto the same shape a notification produces, so both feed the
	// identical status mapping and transition path.
	assert.Equal(t, "ORD-20260731-ABCDEF", result.OrderNumber)
	assert.Equal(t, "tx-9", result.TransactionID)
	assert.Equal(t, "settlement", result.TransactionStatus)
	assert.Equal(t, "accept", result.FraudStatus)
	assert.Equal(t, "qris", result.PaymentType)
	assert.NotEmpty(t, result.RawPayload, "the raw answer is kept for the audit log")
}

func TestFetchStatusReportsAnOrderTheProviderDoesNotKnow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status_code":"404","status_message":"Transaction doesn't exist."}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	_, err := gw.FetchStatus(context.Background(), "ORD-GHOST")

	assert.ErrorIs(t, err, payment.ErrOrderNotFound)
}

// Midtrans answers an unknown transaction with HTTP 200 and "404" in the body.
// Trusting the HTTP status alone yields an empty transaction_status, which the
// caller then logs as an unrecognized provider status — hiding the real cause.
func TestFetchStatusReadsTheBodyStatusCodeNotJustTheHTTPStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status_code":"404","status_message":"Transaction doesn't exist.","id":"x"}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	_, err := gw.FetchStatus(context.Background(), "ORD-GHOST")

	assert.ErrorIs(t, err, payment.ErrOrderNotFound)
}

func TestFetchStatusFailsOnANonSuccessBodyStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status_code":"401","status_message":"Unauthorized"}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	_, err := gw.FetchStatus(context.Background(), "ORD-1")

	require.Error(t, err)
	assert.NotErrorIs(t, err, payment.ErrOrderNotFound)
}

func TestFetchStatusFailsOnAProviderError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status_message":"Access denied"}`))
	}))
	defer server.Close()

	gw := payment.NewMidtransGateway(serverKey, server.URL, 15*time.Minute, server.Client())

	_, err := gw.FetchStatus(context.Background(), "ORD-1")

	require.Error(t, err)
	assert.NotErrorIs(t, err, payment.ErrOrderNotFound)
}

// --- VerifyWebhook --------------------------------------------------------

func TestVerifyWebhookAcceptsACorrectlySignedNotification(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

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
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

	_, err := gw.VerifyWebhook(notification(t, map[string]any{"signature_key": "deadbeef"}), "")

	require.Error(t, err)
	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

// The signature covers gross_amount, so an attacker cannot replay a real
// notification with the amount changed.
func TestVerifyWebhookRejectsATamperedAmount(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

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
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

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
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

	_, err := gw.VerifyWebhook([]byte(`{not json`), "")

	require.Error(t, err)
	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhookRejectsAMissingOrderIDAsUnauthenticated(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

	raw := notification(t, map[string]any{"order_id": ""})

	_, err := gw.VerifyWebhook(raw, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhookRejectsAnEmptyBodyAsUnauthenticated(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

	_, err := gw.VerifyWebhook(nil, "")

	assert.ErrorIs(t, err, payment.ErrInvalidSignature)
}

func TestVerifyWebhookPreservesTheRawStatusForEveryTerminalOutcome(t *testing.T) {
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

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
	gw := payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

	assert.Equal(t, "midtrans", gw.Name())
}

// The gateway must satisfy the abstraction the order domain depends on, so a
// different provider can be swapped in without touching that domain
// (Constitution Principle V).
func TestMidtransSatisfiesTheGatewayInterface(t *testing.T) {
	var gw payment.Gateway = payment.NewMidtransGateway(serverKey, "https://unused", 15*time.Minute, http.DefaultClient)

	assert.NotNil(t, gw)
	fmt.Fprint(io.Discard, gw.Name())
}

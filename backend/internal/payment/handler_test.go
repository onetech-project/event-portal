package payment_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func newWebhookAPI(t *testing.T) (*echo.Echo, webhookFixture) {
	t.Helper()
	f := newWebhookFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	payment.NewHandler(f.svc, testsupport.DiscardLogger()).RegisterPublicRoutes(e.Group("/api/v1"))
	return e, f
}

func postWebhook(t *testing.T, e *echo.Echo, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestWebhookEndpointAcknowledgesAVerifiedNotification(t *testing.T) {
	e, f := newWebhookAPI(t)
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-WEBHOOK",
		TransactionStatus: "settlement",
		RawPayload:        []byte(`{"transaction_status":"settlement"}`),
	}

	rec := postWebhook(t, e, "/api/v1/payment/webhook/midtrans", `{"transaction_status":"settlement"}`)
	f.svc.WaitForFulfillment()

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

func TestWebhookEndpointReturns401ForAnInvalidSignature(t *testing.T) {
	e, f := newWebhookAPI(t)
	f.gateway.err = payment.ErrInvalidSignature

	rec := postWebhook(t, e, "/api/v1/payment/webhook/midtrans", `{}`)

	require.Equal(t, http.StatusUnauthorized, rec.Code)

	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeInvalidSignature), body.Code)
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

// A no-op status still gets a 200, or the provider will keep retrying it.
func TestWebhookEndpointAcknowledgesANoOpStatus(t *testing.T) {
	e, f := newWebhookAPI(t)
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-WEBHOOK",
		TransactionStatus: "pending",
		RawPayload:        []byte(`{"transaction_status":"pending"}`),
	}

	rec := postWebhook(t, e, "/api/v1/payment/webhook/midtrans", `{"transaction_status":"pending"}`)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

func TestWebhookEndpointAcknowledgesAnUnknownOrder(t *testing.T) {
	e, f := newWebhookAPI(t)
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-NOT-OURS",
		TransactionStatus: "settlement",
		RawPayload:        []byte(`{}`),
	}

	rec := postWebhook(t, e, "/api/v1/payment/webhook/midtrans", `{}`)

	assert.Equal(t, http.StatusOK, rec.Code)
}

// The webhook must answer before post-payment work finishes (ARCHITECTURE §3.4).
func TestWebhookEndpointRespondsWithoutWaitingForFulfillment(t *testing.T) {
	e, f := newWebhookAPI(t)
	f.gateway.result = &payment.WebhookResult{
		OrderNumber:       "ORD-WEBHOOK",
		TransactionStatus: "settlement",
		RawPayload:        []byte(`{}`),
	}

	rec := postWebhook(t, e, "/api/v1/payment/webhook/midtrans", `{}`)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Only after the response has been written does the work complete.
	f.svc.WaitForFulfillment()
	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued)
	assert.Equal(t, 1, emailed)
}

func TestWebhookEndpointRejectsAnUnknownProvider(t *testing.T) {
	e, _ := newWebhookAPI(t)

	rec := postWebhook(t, e, "/api/v1/payment/webhook/stripe", `{}`)

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"only providers this deployment is configured for are accepted")
}

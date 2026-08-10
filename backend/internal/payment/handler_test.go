package payment_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/pgauto/cdtc/status"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/payment"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func newCallbackAPI(t *testing.T) (*echo.Echo, webhookFixture) {
	t.Helper()
	f := newWebhookFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	// Mounted on the Echo instance, not a group: the path is the gateway's to
	// choose and it is not under /api/v1.
	payment.NewHandler(f.svc, testsupport.DiscardLogger()).RegisterCallbackRoute(e)
	return e, f
}

func postCallback(t *testing.T, e *echo.Echo, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, payment.CallbackPath, strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.Header.Set(echo.HeaderAuthorization, "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestCallbackEndpointAcknowledgesAnAuthenticatedNotification(t *testing.T) {
	e, f := newCallbackAPI(t)
	f.gateway.result = notification("ORD-WEBHOOK", status.Completed)

	rec := postCallback(t, e, `{"ri":"ORD-WEBHOOK","s":5,"tt":0}`, "the-token")
	f.svc.WaitForFulfillment()

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "PAID", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

// The one case where a non-200 is correct: everything else the gateway would
// simply retry three more times to no purpose.
func TestCallbackEndpointRefusesAMissingOrWrongToken(t *testing.T) {
	for _, tc := range []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"wrong token", "not-the-token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, f := newCallbackAPI(t)
			f.gateway.err = payment.ErrInvalidSignature

			rec := postCallback(t, e, `{"ri":"ORD-WEBHOOK","s":5,"tt":0}`, tc.token)

			require.Equal(t, http.StatusUnauthorized, rec.Code)

			var body apperr.Body
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, apperr.Numeric(rec.Code, apperr.CodeInvalidSignature), body.Code)
			assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID),
				"a refused notification must change nothing")
		})
	}
}

// Deliberate no-ops still get a 200. The gateway retries any other answer three
// times, ten seconds apart, with no dead-letter — so refusing something a retry
// cannot fix costs four deliveries and still loses the notification.
func TestCallbackEndpointAcknowledgesDeliberateNoOps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *payment.WebhookResult
	}{
		{"pending status", notification("ORD-WEBHOOK", status.Pending)},
		{"unrecognised status", notification("ORD-WEBHOOK", status.Status(99))},
		{"unknown reference", notification("ORD-NOT-OURS", status.Completed)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, f := newCallbackAPI(t)
			f.gateway.result = tc.result

			rec := postCallback(t, e, string(tc.result.RawPayload), "the-token")

			assert.Equal(t, http.StatusOK, rec.Code)
			assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
		})
	}
}

func TestCallbackEndpointAcknowledgesANonDeposit(t *testing.T) {
	e, f := newCallbackAPI(t)
	result := notification("ORD-WEBHOOK", status.Completed)
	result.IsDeposit = false
	result.TransactionType = "WITHDRAW"
	f.gateway.result = result

	rec := postCallback(t, e, `{"ri":"ORD-WEBHOOK","s":5,"tt":1}`, "the-token")

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

// The callback must answer before post-payment work finishes (ARCHITECTURE §3.4).
// Under this gateway that is load-bearing rather than merely tidy: the budget is
// five seconds, and ticket generation plus an SMTP round-trip can exceed it.
func TestCallbackEndpointRespondsWithoutWaitingForFulfilment(t *testing.T) {
	e, f := newCallbackAPI(t)
	f.gateway.result = notification("ORD-WEBHOOK", status.Completed)

	rec := postCallback(t, e, `{"ri":"ORD-WEBHOOK","s":5,"tt":0}`, "the-token")
	assert.Equal(t, http.StatusOK, rec.Code)

	// Only after the response has been written does the work complete.
	f.svc.WaitForFulfillment()
	issued, emailed := f.fulfiller.counts()
	assert.Equal(t, 1, issued)
	assert.Equal(t, 1, emailed)
}

// FR-019c. A redelivered notification whose seats have been resold is refused —
// but with 200, not a failure code. A non-200 would spend the gateway's three
// retries on an attempt guaranteed to fail identically, and the notification
// would be lost at the end of it. The body carries the reason; the gateway does
// not read it, but our record and the operator who asked for the resend do.
func TestCallbackEndpointAnswers200WithAReasonWhenTheSeatsAreGone(t *testing.T) {
	e, f := newCallbackAPI(t)
	ctx := context.Background()

	f.gateway.result = notification("ORD-WEBHOOK", status.Expired)
	require.Equal(t, http.StatusOK,
		postCallback(t, e, `{"ri":"ORD-WEBHOOK","s":3,"tt":0}`, "the-token").Code)

	_, err := f.pool.Exec(ctx, `UPDATE ticket_types SET quota = 0 WHERE id = $1`, f.ticketIDs[0])
	require.NoError(t, err)

	f.gateway.result = notification("ORD-WEBHOOK", status.Completed)
	rec := postCallback(t, e, `{"ri":"ORD-WEBHOOK","s":5,"tt":0}`, "the-token")

	require.Equal(t, http.StatusOK, rec.Code,
		"anything else and the gateway burns its budget on a certainty")

	var body payment.SettleRefusedResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "TICKETS_UNAVAILABLE", body.Error)
	assert.Equal(t, "ORD-WEBHOOK", body.OrderNumber)
	require.Len(t, body.Shortfall, 1)
	assert.Equal(t, int32(3), body.Shortfall[0].Required)
	assert.Equal(t, int32(0), body.Shortfall[0].Remaining)
	assert.Equal(t, "Regular", body.Shortfall[0].TicketTypeName)

	assert.Equal(t, "EXPIRED", testsupport.OrderStatusOf(t, f.pool, f.orderID))
	issued, _ := f.fulfiller.counts()
	assert.Zero(t, issued)
}

// The admin surface offers reads and nothing else. There is no route by which a
// person can record that a payment happened (FR-022d, FR-022h).
func TestAdminPaymentRoutesAreReadsOnly(t *testing.T) {
	f := newWebhookFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	api := e.Group("/api/v1")
	payment.NewHandler(f.svc, testsupport.DiscardLogger()).RegisterAdminRoutes(api)

	orderID := f.orderID.String()
	for _, tc := range []struct {
		name, method, path string
		want               int
	}{
		{"notification history", http.MethodGet, "/api/v1/admin/payment/order/" + orderID + "/notifications", http.StatusOK},
		{"holds versus remaining", http.MethodGet, "/api/v1/admin/payment/order/" + orderID + "/holds", http.StatusOK},
		{"withdrawn worklist", http.MethodGet, "/api/v1/admin/payment/reconciliation", http.StatusNotFound},
		{"withdrawn manual confirmation", http.MethodPost, "/api/v1/admin/payment/reconciliation/" + orderID + "/confirm", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			assert.Equal(t, tc.want, rec.Code)
		})
	}
}

// A body larger than the cap must not be read into memory whole. The handler
// still answers rather than hanging, which is what keeps the gateway's five
// second budget from being spent on a hostile request.
func TestCallbackEndpointBoundsTheBodyItReads(t *testing.T) {
	e, f := newCallbackAPI(t)
	f.gateway.err = payment.ErrInvalidSignature

	oversized := `{"ri":"ORD-WEBHOOK","pad":"` + strings.Repeat("x", 2<<20) + `"}`
	rec := postCallback(t, e, oversized, "the-token")

	assert.NotEqual(t, 0, rec.Code, "the handler must answer rather than hang")
	assert.Equal(t, "PENDING", testsupport.OrderStatusOf(t, f.pool, f.orderID))
}

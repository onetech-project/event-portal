package notification_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/notification"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

// The guest resend is unauthenticated, so these tests are as much about what the
// endpoint refuses to reveal as about what it does.

const publicResendWindow = time.Minute

// newPublicResendAPI mounts the guest route with the same per-order limiter the
// composition root applies. limitBurst of 0 leaves the limiter off, which keeps
// the disclosure tests from tripping over a 429 they did not mean to test.
func newPublicResendAPI(t *testing.T, limitBurst int) (*echo.Echo, deliveryFixture) {
	t.Helper()
	f := newDeliveryFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())

	g := e.Group("/api/v1")
	if limitBurst > 0 {
		g = e.Group("/api/v1",
			httpx.RateLimitPerBodyField("order_id", 1.0/60.0, limitBurst, publicResendWindow))
	}
	notification.NewHandler(f.svc).RegisterPublicRoutes(g)

	return e, f
}

func publicResend(t *testing.T, e *echo.Echo, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/ticket/resend-email", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func resendBody(orderNumber string) string {
	return fmt.Sprintf(`{"order_id":%q}`, orderNumber)
}

func TestPublicResendSendsOneEmailToTheStoredBuyerAddress(t *testing.T) {
	e, f := newPublicResendAPI(t, 0)

	rec := publicResend(t, e, resendBody(f.orders.order.OrderNumber))

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, f.mailer.sent, 1)
	assert.Equal(t, "budi@example.com", f.mailer.sent[0].To)
}

// FR-024: the destination is never the caller's to choose. The body carries the
// order number and nothing else is read from it.
func TestPublicResendIgnoresAnAddressSuppliedInTheBody(t *testing.T) {
	e, f := newPublicResendAPI(t, 0)

	rec := publicResend(t, e, fmt.Sprintf(
		`{"order_id":%q,"email":"attacker@example.com","to":"attacker@example.com"}`,
		f.orders.order.OrderNumber))

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, f.mailer.sent, 1)
	assert.Equal(t, "budi@example.com", f.mailer.sent[0].To,
		"an unauthenticated endpoint that mails a caller-supplied address is an open relay")
}

// FR-026: a 404 here would let anyone probe which order numbers are real.
func TestPublicResendAnswersIdenticallyForAnUnknownOrder(t *testing.T) {
	known, knownFixture := newPublicResendAPI(t, 0)
	unknown, unknownFixture := newPublicResendAPI(t, 0)
	unknownFixture.orders.byNumberErr = apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")

	knownRec := publicResend(t, known, resendBody(knownFixture.orders.order.OrderNumber))
	unknownRec := publicResend(t, unknown, resendBody("ORD-00000000-DEADBEEF"))

	assert.Equal(t, knownRec.Code, unknownRec.Code)
	assert.Equal(t, knownRec.Body.String(), unknownRec.Body.String(),
		"the two responses must be byte-identical or the endpoint is an enumeration oracle")
	assert.Empty(t, unknownFixture.mailer.sent)
}

// A body the endpoint cannot read is as silent as an unknown order — a 400
// would distinguish "you spoke wrongly" from "that order does not exist" for
// free probing.
func TestPublicResendAnswersIdenticallyForAMalformedBody(t *testing.T) {
	e, f := newPublicResendAPI(t, 0)

	for _, body := range []string{"", "{", `{"order_id":42}`, `{"wrong_field":"x"}`} {
		rec := publicResend(t, e, body)
		assert.Equal(t, http.StatusAccepted, rec.Code)
		assert.Equal(t, notification.PublicResendMessage, decodeMessage(t, rec))
	}
	assert.Empty(t, f.mailer.sent)
}

func TestPublicResendBodyNamesNeitherRecipientNorOrder(t *testing.T) {
	e, f := newPublicResendAPI(t, 0)

	rec := publicResend(t, e, resendBody(f.orders.order.OrderNumber))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "resend payload rides the envelope's data field")
	assert.ElementsMatch(t, []string{"message"}, keysOfBody(data),
		"unlike the admin resend, this one must not echo sent_to")
	assert.NotContains(t, rec.Body.String(), "budi@example.com")
	assert.NotContains(t, rec.Body.String(), f.orders.order.OrderNumber)
}

// A non-paid order has no tickets to send, and must not say so either.
func TestPublicResendSendsNothingForANonPaidOrderButAnswersTheSame(t *testing.T) {
	for _, status := range []string{"PENDING", "CANCELLED", "EXPIRED"} {
		t.Run(status, func(t *testing.T) {
			e, f := newPublicResendAPI(t, 0)
			f.orders.order.Status = status

			rec := publicResend(t, e, resendBody(f.orders.order.OrderNumber))

			assert.Equal(t, http.StatusAccepted, rec.Code)
			assert.Equal(t, notification.PublicResendMessage, decodeMessage(t, rec))
			assert.Empty(t, f.mailer.sent)
		})
	}
}

// A delivery failure is the server's problem, not the guest's, and it must not
// become a signal either.
func TestPublicResendStillAnswers202WhenDeliveryFails(t *testing.T) {
	e, f := newPublicResendAPI(t, 0)
	f.mailer.err = assertAnError{}

	rec := publicResend(t, e, resendBody(f.orders.order.OrderNumber))

	assert.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, notification.PublicResendMessage, decodeMessage(t, rec))
}

// FR-025: the buyer's inbox is the resource being protected, so the bucket is
// keyed by the order_id in the body.
func TestPublicResendRateLimitsRepeatRequestsForTheSameOrder(t *testing.T) {
	e, f := newPublicResendAPI(t, 1)
	body := resendBody(f.orders.order.OrderNumber)

	require.Equal(t, http.StatusAccepted, publicResend(t, e, body).Code)

	rec := publicResend(t, e, body)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Len(t, f.mailer.sent, 1, "the throttled request must not have sent a second email")
}

func TestPublicResendLimitsPerOrderNotGlobally(t *testing.T) {
	e, f := newPublicResendAPI(t, 1)

	require.Equal(t, http.StatusAccepted,
		publicResend(t, e, resendBody(f.orders.order.OrderNumber)).Code)

	// A different order draws from its own bucket. Keying the limiter on the
	// caller instead would let one buyer's resend lock out everyone else's.
	rec := publicResend(t, e, resendBody("ORD-20260801-OTHER123"))

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

// A request that omits order_id cannot buy itself a fresh bucket per attempt —
// all such requests share one.
func TestPublicResendRequestsWithoutAnOrderIDShareOneBucket(t *testing.T) {
	e, _ := newPublicResendAPI(t, 1)

	require.Equal(t, http.StatusAccepted, publicResend(t, e, "{}").Code)

	rec := publicResend(t, e, `{"other":"field"}`)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
}

func decodeMessage(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Data notification.PublicResendResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Data.Message
}

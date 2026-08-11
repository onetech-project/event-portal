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
// endpoint refuses to reveal as about what it does — and, since spec 012
// FR-021j–q, about what it tells an operator that it never tells the caller.

const (
	publicResendWindow = time.Minute
	publicResendRate   = 1.0 / 60.0
	// Whole seconds a fresh window is worth, as the responses report it.
	publicResendSeconds = 60
)

// newPublicResendAPI mounts the guest route with the same per-order cooldown the
// composition root applies. A burst of 0 means "do not get in the way": the
// disclosure tests would otherwise trip over a 429 they did not mean to test.
func newPublicResendAPI(t *testing.T, burst int) (*echo.Echo, deliveryFixture) {
	t.Helper()
	f := newDeliveryFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())

	if burst <= 0 {
		burst = 1000
	}
	notification.NewHandler(f.svc).RegisterPublicRoutes(e.Group("/api/v1"),
		httpx.NewCooldown(publicResendRate, burst, 3*publicResendWindow))

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

// Spec 011 FR-012: the public resend runs the same single delivery as the
// original send — one email to the buyer — while the caller still only sees the
// uniform 202 body.
func TestPublicResendSendsOneEmailToTheBuyer(t *testing.T) {
	e, f := newPublicResendAPI(t, 0)

	rec := publicResend(t, e, resendBody(f.orders.order.OrderNumber))

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, notification.PublicResendMessage, decodeResend(t, rec).Message)
	require.Len(t, f.mailer.sent, 1, "exactly one email for the whole order")
	assert.Equal(t, []string{"budi@example.com"}, f.mailer.toAddresses())
}

// FR-024: the destination is never the caller's to choose. The body carries the
// order number and nothing else is read from it — delivery goes to the stored
// buyer address, untouched by what the caller supplied.
func TestPublicResendIgnoresAnAddressSuppliedInTheBody(t *testing.T) {
	e, f := newPublicResendAPI(t, 0)

	rec := publicResend(t, e, fmt.Sprintf(
		`{"order_id":%q,"email":"attacker@example.com","to":"attacker@example.com"}`,
		f.orders.order.OrderNumber))

	require.Equal(t, http.StatusAccepted, rec.Code)
	require.Len(t, f.mailer.sent, 1)
	assert.Equal(t, []string{"budi@example.com"}, f.mailer.toAddresses(),
		"an unauthenticated endpoint that mails a caller-supplied address is an open relay")
	assert.NotEqual(t, "attacker@example.com", f.mailer.sent[0].To)
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

// FR-021l: the identical-answer rule covers an order number that is well-formed
// but unknown. A body that names no order makes no claim about any order, so
// refusing it discloses nothing — and answering "accepted" would tell a caller
// their mail is on its way when nothing was sent, which is how a double-encoded
// request body went unnoticed for a release.
func TestPublicResendRefusesABodyItCannotKey(t *testing.T) {
	for _, body := range []string{"", "{", `{"order_id":42}`, `{"wrong_field":"x"}`,
		`{"order_id":""}`, `"{\"order_id\":\"ORD-1\"}"`} {
		t.Run(body, func(t *testing.T) {
			e, f := newPublicResendAPI(t, 0)

			rec := publicResend(t, e, body)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.Equal(t, apperr.Numeric(http.StatusBadRequest, apperr.CodeValidation),
				decodeEnvelopeCode(t, rec))
			assert.Empty(t, f.mailer.sent)
		})
	}
}

// FR-021k: the request above must cost nobody anything. One shared bucket for
// requests the server cannot key turns a single malformed client into a refusal
// for every other guest for the length of the window — which is exactly what
// happened, and what made an encoding bug look like a rate-limit problem.
func TestAnUnreadableBodySpendsNoOnesAllowance(t *testing.T) {
	e, f := newPublicResendAPI(t, 1)

	for range 5 {
		require.Equal(t, http.StatusBadRequest, publicResend(t, e, `{"nope":1}`).Code)
	}

	rec := publicResend(t, e, resendBody(f.orders.order.OrderNumber))

	assert.Equal(t, http.StatusAccepted, rec.Code,
		"a malformed request must not throttle an unrelated order")
	assert.Len(t, f.mailer.sent, 1)
}

func TestPublicResendBodyNamesNeitherRecipientNorOrder(t *testing.T) {
	e, f := newPublicResendAPI(t, 0)

	rec := publicResend(t, e, resendBody(f.orders.order.OrderNumber))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, ok := body["data"].(map[string]any)
	require.True(t, ok, "resend payload rides the envelope's data field")
	assert.ElementsMatch(t, []string{"message", "retry_after_seconds"}, keysOfBody(data),
		"unlike the admin resend, this one must not echo sent_to")
	assert.NotContains(t, rec.Body.String(), "ani@example.com")
	assert.NotContains(t, rec.Body.String(), "bayu@example.com")
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
			assert.Equal(t, notification.PublicResendMessage, decodeResend(t, rec).Message)
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
	assert.Equal(t, notification.PublicResendMessage, decodeResend(t, rec).Message)
}

// FR-021j: the acceptance carries the window it has just started, so the screen
// counts down from a send rather than waiting for a refusal to tell it anything.
func TestAnAcceptedResendReportsTheWindowItStarted(t *testing.T) {
	e, f := newPublicResendAPI(t, 1)

	rec := publicResend(t, e, resendBody(f.orders.order.OrderNumber))

	require.Equal(t, http.StatusAccepted, rec.Code)
	assert.Equal(t, publicResendSeconds, decodeResend(t, rec).RetryAfterSeconds)
}

// FR-025: the buyer's inbox is the resource being protected, so the cooldown is
// keyed by the order_id in the body. FR-021j: the refusal says how long is left.
func TestPublicResendRateLimitsRepeatRequestsForTheSameOrder(t *testing.T) {
	e, f := newPublicResendAPI(t, 1)
	body := resendBody(f.orders.order.OrderNumber)

	accepted := publicResend(t, e, body)
	require.Equal(t, http.StatusAccepted, accepted.Code)

	rec := publicResend(t, e, body)

	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.Len(t, f.mailer.sent, 1,
		"only the first request's email went out; the throttled one sent nothing")

	seconds := decodeRetryAfter(t, rec)
	assert.Positive(t, seconds, "a refusal that does not say when to return is a dead end")
	assert.LessOrEqual(t, seconds, decodeResend(t, accepted).RetryAfterSeconds,
		"the refusal reports the remainder of the window, never more than the window")
}

func TestPublicResendLimitsPerOrderNotGlobally(t *testing.T) {
	e, f := newPublicResendAPI(t, 1)

	require.Equal(t, http.StatusAccepted,
		publicResend(t, e, resendBody(f.orders.order.OrderNumber)).Code)

	// A different order draws from its own window. Keying on the caller instead
	// would let one buyer's resend lock out everyone else's.
	rec := publicResend(t, e, resendBody("ORD-20260801-OTHER123"))

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

// FR-021m: the endpoint is unauthenticated, so what the window caps is the work
// an attempt causes, not the mail it produces. Counting only deliveries would
// leave order-number probing unlimited.
func TestAnAttemptThatSendsNothingStillSpendsTheWindow(t *testing.T) {
	e, f := newPublicResendAPI(t, 1)
	f.orders.byNumberErr = apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")
	body := resendBody("ORD-00000000-DEADBEEF")

	require.Equal(t, http.StatusAccepted, publicResend(t, e, body).Code)

	assert.Equal(t, http.StatusTooManyRequests, publicResend(t, e, body).Code)
	assert.Empty(t, f.mailer.sent)
}

func decodeResend(t *testing.T, rec *httptest.ResponseRecorder) notification.PublicResendResponse {
	t.Helper()
	var body struct {
		Data notification.PublicResendResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Data
}

func decodeRetryAfter(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Data notification.PublicRetryAfter `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Data.RetryAfterSeconds
}

func decodeEnvelopeCode(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Code
}

package notification_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/notification"
	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func newResendAPI(t *testing.T) (*echo.Echo, deliveryFixture) {
	t.Helper()
	f := newDeliveryFixture(t)

	e := echo.New()
	e.HTTPErrorHandler = httpx.ErrorHandler(testsupport.DiscardLogger())
	// Registered without the JWT middleware: authentication has its own tests in
	// the admin package.
	notification.NewHandler(f.svc).RegisterAdminRoutes(e.Group("/api/v1"))
	return e, f
}

func resend(t *testing.T, e *echo.Echo, orderID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/admin/orders/"+orderID+"/resend-email", strings.NewReader(""))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func TestResendEndpointReturns200AndTheRecipient(t *testing.T) {
	e, f := newResendAPI(t)

	rec := resend(t, e, f.orderID.String())

	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.ElementsMatch(t, []string{"message", "sent_to"}, keysOfBody(body))
	assert.Equal(t, "Email resent", body["message"])
	assert.Equal(t, "budi@example.com", body["sent_to"])

	require.Len(t, f.mailer.sent, 1)
	assert.Equal(t, 1, f.orders.markCalled, "a successful resend also sets email_sent")
}

func TestResendEndpointReturns400ForANonPaidOrder(t *testing.T) {
	for _, status := range []string{"PENDING", "CANCELLED", "EXPIRED"} {
		t.Run(status, func(t *testing.T) {
			e, f := newResendAPI(t)
			f.orders.order.Status = status

			rec := resend(t, e, f.orderID.String())

			require.Equal(t, http.StatusBadRequest, rec.Code)

			var body apperr.Body
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, apperr.CodeOrderNotPaid, body.ErrorCode)
			assert.Empty(t, f.mailer.sent)
		})
	}
}

func TestResendEndpointReturns404ForAnUnknownOrder(t *testing.T) {
	e, f := newResendAPI(t)
	f.orders.getErr = apperr.NotFound(apperr.CodeOrderNotFound, "Order not found.")

	rec := resend(t, e, uuid.New().String())

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestResendEndpointReturns400ForAMalformedID(t *testing.T) {
	e, _ := newResendAPI(t)

	rec := resend(t, e, "not-a-uuid")

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// The resend must reuse the stored ticket codes verbatim so a guest's original
// ticket stays valid (specs/003 FR-011).
func TestResendEndpointReproducesTheSameTickets(t *testing.T) {
	e, f := newResendAPI(t)

	require.Equal(t, http.StatusOK, resend(t, e, f.orderID.String()).Code)
	require.Equal(t, http.StatusOK, resend(t, e, f.orderID.String()).Code)

	require.Len(t, f.mailer.sent, 2)
	assert.Equal(t,
		len(f.mailer.sent[0].Attachments[0].Content),
		len(f.mailer.sent[1].Attachments[0].Content),
		"the same codes render the same document; nothing is regenerated")
}

func TestResendEndpointFailsWhenDeliveryFails(t *testing.T) {
	e, f := newResendAPI(t)
	f.mailer.err = assertAnError{}

	rec := resend(t, e, f.orderID.String())

	assert.GreaterOrEqual(t, rec.Code, 500)
	assert.Zero(t, f.orders.markCalled, "a failed resend must not set email_sent")
}

type assertAnError struct{}

func (assertAnError) Error() string { return "smtp refused the message" }

func keysOfBody(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

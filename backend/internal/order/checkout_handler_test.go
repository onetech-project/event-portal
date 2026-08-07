package order_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/testsupport"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// --- T022: POST /ticket/checkout/:order_id + moved QR image ------------------

func checkoutFormsBody(slotIDs []uuid.UUID) string {
	visitors := make([]string, 0, len(slotIDs))
	for _, id := range slotIDs {
		visitors = append(visitors, fmt.Sprintf(
			`{"id":"%s","name":"Visitor","email":"v@example.com","phone":"081234567890","dob":"2000-01-31","gender":"FEMALE"}`, id))
	}
	return fmt.Sprintf(`{"attendees":[%s]}`, strings.Join(visitors, ","))
}

func TestCheckoutEndpointReturnsTheQRContractShape(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var data map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &data))
	assert.ElementsMatch(t,
		[]string{"order_id", "qr_string", "expires_at", "qr_image_url", "qr_refresh_after_seconds"},
		keysOfMap(data), "contracts/api.md call 8")
	assert.Equal(t, "/api/v1/ticket/order/"+orderNumber+"/qris.png", data["qr_image_url"])
	assert.InDelta(t, 420, data["qr_refresh_after_seconds"], 0.001)
}

func TestCheckoutEndpointReturns409004WithTheCurrentPayloadOnRetry(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)
	require.Equal(t, http.StatusOK,
		postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs)).Code)

	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs))

	require.Equal(t, http.StatusConflict, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 409004, body.Code)
	payload, ok := body.Data.(map[string]any)
	require.True(t, ok, "409004 data is the current QR payload")
	assert.Equal(t, f.gateway.qrString, payload["qr_string"])
}

func TestCheckoutEndpointReturns400001FieldMap(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	bad := strings.Replace(checkoutFormsBody(slotIDs), "v@example.com", "nope", 1)
	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, bad)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, 400001, body.Code)
	fields, ok := body.Data.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, fields, "attendees[0].email")
}

// Spec 011: a stale client may still send the removed buyer_* block; binding
// ignores unknown fields, so the request is accepted as if it were clean.
func TestCheckoutEndpointIgnoresStaleBuyerFields(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	stale := `{
		"buyer_name":"Siti Rahayu",
		"buyer_email":"siti@example.com",
		"buyer_phone":"+628123456789",
		"buyer_dob":"1995-05-05",
		"buyer_gender":"FEMALE",` + strings.TrimPrefix(checkoutFormsBody(slotIDs), "{")

	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, stale)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestQRImageServesAtTheTicketOrderPath(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)
	require.Equal(t, http.StatusOK,
		postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs)).Code)

	rec := getPath(t, e, "/api/v1/ticket/order/"+orderNumber+"/qris.png")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.NotEmpty(t, rec.Body.Bytes(), "binary body, no envelope")
}

func TestQRImageDisappearsForAnUnpayableOrder(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)
	require.Equal(t, http.StatusOK,
		postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs)).Code)
	_, err := f.pool.Exec(context.Background(),
		`UPDATE orders SET payment_expires_at = now() - interval '1 second' WHERE order_number = $1`,
		orderNumber)
	require.NoError(t, err)

	rec := getPath(t, e, "/api/v1/ticket/order/"+orderNumber+"/qris.png")

	assert.Equal(t, http.StatusNotFound, rec.Code,
		"a page left open past the deadline cannot keep showing a live-looking code")
}

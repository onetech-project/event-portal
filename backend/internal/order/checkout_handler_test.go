package order_test

import (
	"context"
	"encoding/json"
	"errors"
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
	// qr_refresh_after_seconds is gone: one order, one code, one window (FR-010).
	// ext_ref_id joined it in spec 017 — the gateway's own reference for the
	// session, carried so a caller can match the order against the gateway's
	// records without a second request.
	assert.ElementsMatch(t,
		[]string{"order_id", "qr_string", "expires_at", "qr_image_url", "ext_ref_id"},
		keysOfMap(data), "specs/017-payment-ext-ref-id/contracts/api.md")
	assert.Equal(t, "/api/v1/ticket/order/"+orderNumber+"/qris.png", data["qr_image_url"])
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

// --- External reference (spec 017) ----------------------------------------

func TestCheckoutReturnsTheGatewaysExternalReference(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var data map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &data))
	assert.Equal(t, "txn-"+orderNumber, data["ext_ref_id"],
		"the reference the gateway issued for this session, verbatim")
}

// FR-011. The retry never reaches the gateway — it returns at the already-started
// guard — so this answer is rebuilt from what was stored. It must still name the
// same session, or a caller that retried would be handed a different identifier
// for one payment.
func TestCheckoutRetryCarriesTheSameExternalReference(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	first := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs))
	require.Equal(t, http.StatusOK, first.Code)
	var opened map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, first.Body.Bytes()), &opened))

	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs))

	require.Equal(t, http.StatusConflict, rec.Code)
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	payload, ok := body.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, opened["ext_ref_id"], payload["ext_ref_id"],
		"one session, one reference, however many times checkout is called")
	assert.NotEmpty(t, payload["ext_ref_id"])
	assert.Equal(t, 1, f.gateway.callCount(), "the retry opened no second session")
}

// FR-010. A gateway that opens a session without supplying a reference costs
// traceability, never the guest's ability to pay. The field is present and empty
// rather than absent, so a caller never distinguishes the two.
func TestCheckoutSucceedsWhenTheGatewaySuppliesNoExternalReference(t *testing.T) {
	e, f := newCheckoutAPI(t)
	f.gateway.providerRef = noProviderRef
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var data map[string]any
	require.NoError(t, json.Unmarshal(testsupport.UnwrapData(t, rec.Body.Bytes()), &data))
	ref, present := data["ext_ref_id"]
	assert.True(t, present, "present-and-empty, never absent")
	assert.Equal(t, "", ref)
	assert.NotEmpty(t, data["qr_string"], "and the guest still has something to scan")
}

// A failure to read the reference must not turn a payable order into an error.
// It is a support identifier; the guest's code is not contingent on it.
func TestCheckoutStillAnswersWhenTheReferenceCannotBeRead(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)
	require.Equal(t, http.StatusOK,
		postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs)).Code)

	f.records.err = errors.New("payments unreachable")
	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, checkoutFormsBody(slotIDs))

	require.Equal(t, http.StatusConflict, rec.Code, "the already-started refusal, not a 5xx")
	var body apperr.Body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	payload, ok := body.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "", payload["ext_ref_id"])
	assert.Equal(t, f.gateway.qrString, payload["qr_string"],
		"the payment payload is unaffected by a missing support identifier")
}

// FR-013. Nothing is added to a path that carries no payment payload.
func TestCheckoutFailuresCarryNoExternalReference(t *testing.T) {
	e, f := newCheckoutAPI(t)
	orderNumber, slotIDs := bookAgreedOrder(t, f)

	stale := checkoutFormsBody(slotIDs[:0])
	rec := postJSON(t, e, "/api/v1/ticket/checkout/"+orderNumber, stale)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	assert.NotContains(t, rec.Body.String(), "ext_ref_id")
}

package apperr_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/apperr"
	"github.com/manjo/ticketing/backend/pkg/httpx"
)

func TestNewCarriesCodeMessageAndStatus(t *testing.T) {
	err := apperr.New(http.StatusBadRequest, apperr.CodeInsufficientQuota, "not enough tickets left")

	assert.Equal(t, apperr.CodeInsufficientQuota, err.Code)
	assert.Equal(t, "not enough tickets left", err.Message)
	assert.Equal(t, http.StatusBadRequest, err.HTTPStatus)
	assert.Contains(t, err.Error(), apperr.CodeInsufficientQuota)
	assert.Contains(t, err.Error(), "not enough tickets left")
}

func TestWrapPreservesCauseForErrorsIs(t *testing.T) {
	cause := errors.New("boom")

	err := apperr.Wrap(cause, http.StatusBadGateway, apperr.CodePaymentInitiationFailed, "gateway down")

	assert.ErrorIs(t, err, cause)
	assert.Equal(t, http.StatusBadGateway, err.HTTPStatus)
}

func TestAsExtractsAppError(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), apperr.NotFound(apperr.CodeTicketNotFound, "no such ticket"))

	var appErr *apperr.Error
	require.True(t, errors.As(wrapped, &appErr))
	assert.Equal(t, http.StatusNotFound, appErr.HTTPStatus)
	assert.Equal(t, apperr.CodeTicketNotFound, appErr.Code)
}

func TestConstructorsUseTheExpectedStatuses(t *testing.T) {
	tests := []struct {
		name string
		err  *apperr.Error
		want int
	}{
		{"bad request", apperr.BadRequest("X", "m"), http.StatusBadRequest},
		{"unauthorized", apperr.Unauthorized("X", "m"), http.StatusUnauthorized},
		{"not found", apperr.NotFound("X", "m"), http.StatusNotFound},
		{"conflict", apperr.Conflict("X", "m"), http.StatusConflict},
		{"internal", apperr.Internal("X", "m"), http.StatusInternalServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.err.HTTPStatus)
		})
	}
}

// The wire shape is the {code, message, data} envelope (specs/008): a numeric
// code from the registry, a human-readable message, and optional detail data.
func TestResponseShapeIsNumericEnvelope(t *testing.T) {
	body := apperr.BadRequest(apperr.CodeAttendeeCountMismatch, "attendee count mismatch").Response()

	assert.Equal(t, 400001, body.Code)
	assert.Equal(t, "attendee count mismatch", body.Message)
	assert.Nil(t, body.Data)
}

// Registry spot checks (specs/008/contracts/api.md).
func TestNumericRegistry(t *testing.T) {
	tests := []struct {
		status int
		code   string
		want   int
	}{
		{http.StatusBadRequest, apperr.CodeValidation, 400001},
		{http.StatusBadRequest, apperr.CodeInsufficientQuota, 400002},
		{http.StatusBadRequest, apperr.CodeTermsNotAccepted, 400003},
		{http.StatusBadRequest, apperr.CodeUnknownIcon, 400004},
		{http.StatusUnauthorized, apperr.CodeUnauthorized, 401001},
		{http.StatusNotFound, apperr.CodeOrderNotFound, 404001},
		{http.StatusNotFound, apperr.CodeTermsMissing, 404002},
		{http.StatusConflict, apperr.CodeTermsMissing, 409001},
		{http.StatusConflict, apperr.CodeTermsChanged, 409002},
		{http.StatusConflict, apperr.CodeTermsNotRecorded, 409003},
		{http.StatusConflict, apperr.CodePaymentAlreadyStarted, 409004},
		{http.StatusConflict, apperr.CodePaymentNotStarted, 409005},
		{http.StatusGone, apperr.CodeOrderExpired, 410001},
		{http.StatusTooManyRequests, apperr.CodeRateLimited, 429001},
		{http.StatusInternalServerError, apperr.CodeInternal, 500000},
		{http.StatusBadGateway, apperr.CodePaymentInitiationFailed, 502001},
		{http.StatusConflict, apperr.CodePaymentSessionDuplicate, 409006},
		// The 200 band: outcomes a retry cannot change, so the status line stays a
		// success and the sub-code carries the meaning (specs/012 FR-019e, FR-012g).
		{http.StatusOK, apperr.CodeTicketsUnavailable, 200001},
		{http.StatusOK, apperr.CodeNotificationOrderUnknown, 200002},
		{http.StatusOK, apperr.CodeNotificationNotDeposit, 200003},
		{http.StatusOK, apperr.CodeNotificationStatusUnknown, 200004},
		{http.StatusOK, apperr.CodeNotificationContradiction, 200005},
		{http.StatusOK, apperr.CodeNotificationOrderCancelled, 200006},
		// Unregistered codes fall back to status*1000, staying in the scheme.
		{http.StatusConflict, apperr.CodeEventHasOrders, 409000},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			assert.Equal(t, tc.want, apperr.Numeric(tc.status, tc.code))
		})
	}
}

// An unregistered code on a 200 renders exactly httpx.SuccessCode.
//
// This is the trap TICKETS_UNAVAILABLE exists to sit outside of, and the reason
// it must be *registered* rather than left to the fallback. Numeric's default is
// status*1000, so any future 200-band code added without a registry arm would
// announce itself as a success — on a status line that is 200 in both cases by
// design (specs/012 FR-019c), so nothing asserting on the status would notice.
// If this assertion ever fails, the fallback changed and that guarantee moved.
func TestUnregisteredCodeOnOKCollidesWithSuccess(t *testing.T) {
	assert.Equal(t, httpx.SuccessCode, apperr.Numeric(http.StatusOK, "ANYTHING_UNREGISTERED"),
		"the fallback collides with success by construction — this is why 200-band codes must be registered")

	assert.NotEqual(t, apperr.Numeric(http.StatusOK, "ANYTHING_UNREGISTERED"),
		apperr.Numeric(http.StatusOK, apperr.CodeTicketsUnavailable),
		"a settle refusal must never render the same code as an acknowledgement")
}

// PAYMENT_ALREADY_STARTED carries the current QR payload so a retry is safe.
func TestWithDataRidesTheEnvelope(t *testing.T) {
	payload := map[string]string{"qr_string": "00020101…"}

	body := apperr.Conflict(apperr.CodePaymentAlreadyStarted, "payment already started").WithData(payload).Response()

	assert.Equal(t, 409004, body.Code)
	assert.Equal(t, payload, body.Data)
}

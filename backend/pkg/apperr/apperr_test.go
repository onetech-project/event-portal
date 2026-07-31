package apperr_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/apperr"
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

// The wire shape is fixed by every contracts/api.md in specs/: a machine-readable
// error_code plus a human-readable message, and nothing else.
func TestResponseShapeIsErrorCodeAndMessage(t *testing.T) {
	body := apperr.BadRequest(apperr.CodeAttendeeCountMismatch, "attendee count mismatch").Response()

	assert.Equal(t, apperr.CodeAttendeeCountMismatch, body.ErrorCode)
	assert.Equal(t, "attendee count mismatch", body.Message)
}

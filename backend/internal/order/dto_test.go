package order_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
	"github.com/manjo/ticketing/backend/pkg/apperr"
)

var (
	ttRegular = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ttVIP     = uuid.MustParse("22222222-2222-2222-2222-222222222222")
)

func validRequest() order.CheckoutRequest {
	return order.CheckoutRequest{
		BuyerName:  "Budi Santoso",
		BuyerEmail: "budi@example.com",
		BuyerPhone: "+628123456789",
		Items: []order.CheckoutItem{
			{TicketTypeID: ttRegular, Quantity: 2},
		},
		Attendees: []order.CheckoutAttendee{
			{TicketTypeID: ttRegular, Name: "Budi", Email: "budi@example.com"},
			{TicketTypeID: ttRegular, Name: "Siti", Email: "siti@example.com"},
		},
	}
}

func codeOf(t *testing.T, err error) string {
	t.Helper()
	var appErr *apperr.Error
	require.True(t, errors.As(err, &appErr), "expected an *apperr.Error, got %v", err)
	assert.Equal(t, http.StatusBadRequest, appErr.HTTPStatus)
	return appErr.Code
}

func TestValidRequestPasses(t *testing.T) {
	require.NoError(t, validRequest().Validate())
}

func TestValidatePassesForMultipleTicketTypes(t *testing.T) {
	req := validRequest()
	req.Items = append(req.Items, order.CheckoutItem{TicketTypeID: ttVIP, Quantity: 1})
	req.Attendees = append(req.Attendees,
		order.CheckoutAttendee{TicketTypeID: ttVIP, Name: "Andi", Email: "andi@example.com"})

	require.NoError(t, req.Validate())
}

func TestValidateRejectsMissingBuyerFields(t *testing.T) {
	for _, field := range []string{"name", "email", "phone"} {
		t.Run(field, func(t *testing.T) {
			req := validRequest()
			switch field {
			case "name":
				req.BuyerName = "  "
			case "email":
				req.BuyerEmail = ""
			case "phone":
				req.BuyerPhone = ""
			}

			assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
		})
	}
}

func TestValidateRejectsMalformedBuyerEmail(t *testing.T) {
	req := validRequest()
	req.BuyerEmail = "not-an-email"

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

func TestValidateRejectsEmptyItems(t *testing.T) {
	req := validRequest()
	req.Items = nil
	req.Attendees = nil

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

func TestValidateRejectsNonPositiveQuantity(t *testing.T) {
	req := validRequest()
	req.Items = []order.CheckoutItem{{TicketTypeID: ttRegular, Quantity: 0}}
	req.Attendees = nil

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

func TestValidateRejectsDuplicateTicketTypeLines(t *testing.T) {
	req := validRequest()
	req.Items = []order.CheckoutItem{
		{TicketTypeID: ttRegular, Quantity: 1},
		{TicketTypeID: ttRegular, Quantity: 1},
	}

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()),
		"a repeated ticket type makes the per-line attendee mapping ambiguous")
}

// FR-003/004: sum(items[].quantity) must equal len(attendees).
func TestValidateRejectsTooFewAttendees(t *testing.T) {
	req := validRequest()
	req.Attendees = req.Attendees[:1]

	assert.Equal(t, apperr.CodeAttendeeCountMismatch, codeOf(t, req.Validate()))
}

func TestValidateRejectsTooManyAttendees(t *testing.T) {
	req := validRequest()
	req.Attendees = append(req.Attendees,
		order.CheckoutAttendee{TicketTypeID: ttRegular, Name: "Extra", Email: "extra@example.com"})

	assert.Equal(t, apperr.CodeAttendeeCountMismatch, codeOf(t, req.Validate()))
}

// The per-ticket-type breakdown must match too, not just the grand total.
func TestValidateRejectsCorrectTotalButWrongPerTicketTypeSplit(t *testing.T) {
	req := order.CheckoutRequest{
		BuyerName:  "Budi",
		BuyerEmail: "budi@example.com",
		BuyerPhone: "+628123456789",
		Items: []order.CheckoutItem{
			{TicketTypeID: ttRegular, Quantity: 1},
			{TicketTypeID: ttVIP, Quantity: 1},
		},
		Attendees: []order.CheckoutAttendee{
			{TicketTypeID: ttRegular, Name: "A", Email: "a@example.com"},
			{TicketTypeID: ttRegular, Name: "B", Email: "b@example.com"},
		},
	}

	assert.Equal(t, apperr.CodeAttendeeCountMismatch, codeOf(t, req.Validate()))
}

func TestValidateRejectsAttendeeForAnUnorderedTicketType(t *testing.T) {
	req := validRequest()
	req.Attendees[1].TicketTypeID = ttVIP

	assert.Equal(t, apperr.CodeAttendeeCountMismatch, codeOf(t, req.Validate()))
}

func TestValidateRejectsAttendeeWithMissingNameOrEmail(t *testing.T) {
	t.Run("name", func(t *testing.T) {
		req := validRequest()
		req.Attendees[0].Name = " "
		assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
	})
	t.Run("email", func(t *testing.T) {
		req := validRequest()
		req.Attendees[0].Email = "nope"
		assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
	})
}

func TestValidateRejectsNilTicketTypeID(t *testing.T) {
	req := validRequest()
	req.Items[0].TicketTypeID = uuid.Nil

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

func TestTotalQuantitySumsLineItems(t *testing.T) {
	req := validRequest()
	req.Items = append(req.Items, order.CheckoutItem{TicketTypeID: ttVIP, Quantity: 3})

	assert.Equal(t, int32(5), req.TotalQuantity())
}

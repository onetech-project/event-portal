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
	evBookable = uuid.MustParse("00000000-0000-0000-0000-0000000000ee")
	ttRegular  = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	ttVIP      = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	pkgBundle  = uuid.MustParse("33333333-3333-3333-3333-333333333333")
)

func ptr(u uuid.UUID) *uuid.UUID { return &u }

func validBookRequest() order.BookRequest {
	return order.BookRequest{
		EventID: evBookable,
		Items: []order.CheckoutItem{
			{TicketTypeID: ptr(ttRegular), Quantity: 2},
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

func TestValidBookRequestPasses(t *testing.T) {
	require.NoError(t, validBookRequest().Validate())
}

func TestBookValidatePassesForMixedTicketAndPackageLines(t *testing.T) {
	req := validBookRequest()
	req.Items = append(req.Items,
		order.CheckoutItem{TicketTypeID: ptr(ttVIP), Quantity: 1},
		order.CheckoutItem{PackageID: ptr(pkgBundle), Quantity: 1})

	require.NoError(t, req.Validate())
}

func TestBookValidateRejectsAMissingEventID(t *testing.T) {
	req := validBookRequest()
	req.EventID = uuid.Nil

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

func TestBookValidateRejectsEmptyItems(t *testing.T) {
	req := validBookRequest()
	req.Items = nil

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

func TestBookValidateRejectsNonPositiveQuantity(t *testing.T) {
	for name, qty := range map[string]int32{"zero": 0, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			req := validBookRequest()
			req.Items = []order.CheckoutItem{{TicketTypeID: ptr(ttRegular), Quantity: qty}}

			assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
		})
	}
}

func TestBookValidateRejectsDuplicateTicketTypeLines(t *testing.T) {
	req := validBookRequest()
	req.Items = []order.CheckoutItem{
		{TicketTypeID: ptr(ttRegular), Quantity: 1},
		{TicketTypeID: ptr(ttRegular), Quantity: 1},
	}

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()),
		"a repeated ticket type makes the per-line slot mapping ambiguous")
}

func TestBookValidateRejectsDuplicatePackageLines(t *testing.T) {
	req := validBookRequest()
	req.Items = []order.CheckoutItem{
		{PackageID: ptr(pkgBundle), Quantity: 1},
		{PackageID: ptr(pkgBundle), Quantity: 2},
	}

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()),
		"a repeated package must be combined into a single line")
}

func TestBookValidateRejectsItemWithBothTicketTypeAndPackage(t *testing.T) {
	req := validBookRequest()
	req.Items = []order.CheckoutItem{
		{TicketTypeID: ptr(ttRegular), PackageID: ptr(pkgBundle), Quantity: 1},
	}

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()),
		"an item must reference exactly one of ticket_type_id or package_id")
}

func TestBookValidateRejectsItemWithNeitherTicketTypeNorPackage(t *testing.T) {
	req := validBookRequest()
	req.Items = []order.CheckoutItem{{Quantity: 1}}

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

func TestBookValidateRejectsNilTicketTypeID(t *testing.T) {
	req := validBookRequest()
	req.Items = []order.CheckoutItem{{TicketTypeID: &uuid.Nil, Quantity: 1}}

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

func TestBookValidateRejectsNilPackageID(t *testing.T) {
	req := validBookRequest()
	req.Items = []order.CheckoutItem{{PackageID: &uuid.Nil, Quantity: 1}}

	assert.Equal(t, apperr.CodeValidation, codeOf(t, req.Validate()))
}

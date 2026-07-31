package money_test

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/pkg/money"
)

func TestMarshalsAsAFixedScaleDecimalString(t *testing.T) {
	// The driver hands back NUMERIC(12,2) with trailing zeros stripped; the wire
	// format must not inherit that inconsistency.
	raw, err := json.Marshal(money.From(decimal.RequireFromString("175500")))

	require.NoError(t, err)
	assert.JSONEq(t, `"175500.00"`, string(raw))
}

func TestMarshalKeepsExistingFractionalDigits(t *testing.T) {
	raw, err := json.Marshal(money.From(decimal.RequireFromString("99.50")))

	require.NoError(t, err)
	assert.JSONEq(t, `"99.50"`, string(raw))
}

func TestMarshalZero(t *testing.T) {
	raw, err := json.Marshal(money.From(decimal.Zero))

	require.NoError(t, err)
	assert.JSONEq(t, `"0.00"`, string(raw))
}

func TestUnmarshalAcceptsAString(t *testing.T) {
	var m money.Money
	require.NoError(t, json.Unmarshal([]byte(`"1250.75"`), &m))

	assert.True(t, m.Decimal().Equal(decimal.RequireFromString("1250.75")))
}

func TestUnmarshalAcceptsANumber(t *testing.T) {
	var m money.Money
	require.NoError(t, json.Unmarshal([]byte(`1250.75`), &m))

	assert.True(t, m.Decimal().Equal(decimal.RequireFromString("1250.75")))
}

func TestUnmarshalRejectsGarbage(t *testing.T) {
	var m money.Money
	require.Error(t, json.Unmarshal([]byte(`"abc"`), &m))
}

func TestRoundTripThroughAStruct(t *testing.T) {
	type payload struct {
		Total money.Money `json:"total_amount"`
	}

	raw, err := json.Marshal(payload{Total: money.From(decimal.RequireFromString("250000"))})
	require.NoError(t, err)
	assert.JSONEq(t, `{"total_amount":"250000.00"}`, string(raw))

	var back payload
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.True(t, back.Total.Decimal().Equal(decimal.RequireFromString("250000")))
}

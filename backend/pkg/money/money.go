// Package money wraps decimal amounts so every monetary field on the wire uses one
// consistent representation.
package money

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// scale matches NUMERIC(12, 2) in SCHEMA.md.
const scale = 2

// Money is a decimal amount that always marshals as a fixed-scale JSON string
// (e.g. "175500.00"). The Postgres driver strips trailing zeros when decoding
// NUMERIC, so without this the same price could appear as "175500" in one response
// and "175500.50" in another.
type Money struct {
	amount decimal.Decimal
}

// From wraps a decimal read from the database or computed by a service.
func From(d decimal.Decimal) Money { return Money{amount: d} }

// Decimal unwraps the underlying value for arithmetic and persistence.
func (m Money) Decimal() decimal.Decimal { return m.amount }

// String renders the fixed-scale representation.
func (m Money) String() string { return m.amount.StringFixed(scale) }

// MarshalJSON emits the amount as a quoted fixed-scale decimal string.
func (m Money) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.String())
}

// UnmarshalJSON accepts either a JSON string or a JSON number, so clients that
// serialize amounts either way are both handled.
func (m *Money) UnmarshalJSON(data []byte) error {
	text := strings.TrimSpace(string(data))
	if text == "null" {
		m.amount = decimal.Zero
		return nil
	}

	if strings.HasPrefix(text, `"`) {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("money: %w", err)
		}
		text = strings.TrimSpace(s)
	}

	parsed, err := decimal.NewFromString(text)
	if err != nil {
		return fmt.Errorf("money: %q is not a valid decimal amount", text)
	}
	m.amount = parsed
	return nil
}

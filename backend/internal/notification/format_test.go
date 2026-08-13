package notification

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// Spec 016 FR-037: the documents print "IDR 550.000" — the IDR prefix with
// Indonesian separators, dot for thousands and comma for the decimal. They
// differ from the site (which writes "Rp") in the PREFIX ONLY; the separators
// match, because comma-as-thousands misreads as a decimal for these buyers and
// a receipt is where a misread total does the most damage.
func TestFormatIDRGroupsThousandsWithDots(t *testing.T) {
	assert.Equal(t, "IDR 0", formatIDR(decimal.Zero))
	assert.Equal(t, "IDR 750", formatIDR(decimal.NewFromInt(750)))
	assert.Equal(t, "IDR 550.000", formatIDR(decimal.NewFromInt(550000)))
	assert.Equal(t, "IDR 1.234.567", formatIDR(decimal.NewFromInt(1234567)))
}

// FR-037a: the fractional part appears when it is non-zero and is omitted when
// it is zero. "Rupiah has no cents" is true of prices and false of computed
// percentage fees, which is where the cents actually come from.
func TestFormatIDRPrintsAFractionOnlyWhenThereIsOne(t *testing.T) {
	assert.Equal(t, "IDR 15.000,92", formatIDR(decimal.RequireFromString("15000.92")))
	assert.Equal(t, "IDR 840.000", formatIDR(decimal.RequireFromString("840000.00")),
		"a zero fraction is noise on every row")
	// T070: a trailing zero keeps both digits. "IDR 15.000,9" reads as
	// malformed on a financial document.
	assert.Equal(t, "IDR 15.000,90", formatIDR(decimal.RequireFromString("15000.90")))
	assert.Equal(t, "IDR 0,05", formatIDR(decimal.RequireFromString("0.05")))
}

// FR-037b: never truncate. The previous implementation used IntPart(), which
// discarded the fraction — so on an order whose fee computes to cents the
// printed rows did not sum to the printed total (FR-014, SC-003).
func TestFormatIDRDoesNotTruncate(t *testing.T) {
	subtotal := decimal.RequireFromString("105000.00")
	fee := decimal.RequireFromString("11550.92")
	total := subtotal.Add(fee)

	assert.Equal(t, "IDR 105.000", formatIDR(subtotal))
	assert.Equal(t, "IDR 11.550,92", formatIDR(fee))
	assert.Equal(t, "IDR 116.550,92", formatIDR(total),
		"the printed rows must sum to the printed total")
}

// The grouping loop's `digits[i-1] != '-'` guard is what stops "IDR -,550.000".
// Nothing covered it before this test, so a rewrite that dropped it would have
// shipped green.
func TestFormatIDRHandlesNegativeAmounts(t *testing.T) {
	assert.Equal(t, "IDR -550.000", formatIDR(decimal.NewFromInt(-550000)))
	assert.Equal(t, "IDR -15.000,92", formatIDR(decimal.RequireFromString("-15000.92")))
	assert.Equal(t, "IDR -750", formatIDR(decimal.NewFromInt(-750)))
}

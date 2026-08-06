package notification

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// The receipt prints amounts the way the site does: dot-grouped rupiah with no
// cent display.
func TestFormatIDRGroupsThousandsWithDots(t *testing.T) {
	assert.Equal(t, "Rp 0", formatIDR(decimal.Zero))
	assert.Equal(t, "Rp 750", formatIDR(decimal.NewFromInt(750)))
	assert.Equal(t, "Rp 550.000", formatIDR(decimal.NewFromInt(550000)))
	assert.Equal(t, "Rp 1.234.567", formatIDR(decimal.NewFromInt(1234567)))
	assert.Equal(t, "Rp 840.000", formatIDR(decimal.RequireFromString("840000.00")))
}

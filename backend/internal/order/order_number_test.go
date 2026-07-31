package order_test

import (
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/manjo/ticketing/backend/internal/order"
)

var orderNumberPattern = regexp.MustCompile(`^ORD-\d{8}-[0-9A-Z]{6}$`)

func TestOrderNumberHasTheExpectedShape(t *testing.T) {
	number, err := order.GenerateOrderNumber(time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC))

	require.NoError(t, err)
	assert.Regexp(t, orderNumberPattern, number)
}

func TestOrderNumberEmbedsTheOrderDate(t *testing.T) {
	number, err := order.GenerateOrderNumber(time.Date(2026, 12, 5, 23, 59, 0, 0, time.UTC))

	require.NoError(t, err)
	assert.Contains(t, number, "ORD-20261205-")
}

func TestOrderNumberFitsTheColumn(t *testing.T) {
	number, err := order.GenerateOrderNumber(time.Now())

	require.NoError(t, err)
	assert.LessOrEqual(t, len(number), 100, "orders.order_number is VARCHAR(100)")
}

func TestOrderNumbersDoNotCollideInPractice(t *testing.T) {
	const draws = 5000
	at := time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)

	seen := make(map[string]struct{}, draws)
	for range draws {
		number, err := order.GenerateOrderNumber(at)
		require.NoError(t, err)
		seen[number] = struct{}{}
	}

	// The suffix is drawn from a crypto source; the DB unique constraint plus the
	// service's retry loop is the real guarantee, but a birthday collision this
	// dense would mean the suffix is far too small.
	assert.Greater(t, len(seen), draws-5)
}

// The suffix must avoid characters that are easy to misread when a support agent
// reads an order number back to a buyer.
func TestOrderNumberSuffixExcludesAmbiguousCharacters(t *testing.T) {
	for range 500 {
		number, err := order.GenerateOrderNumber(time.Now())
		require.NoError(t, err)

		suffix := number[len(number)-6:]
		assert.NotContains(t, suffix, "I")
		assert.NotContains(t, suffix, "O")
		assert.NotContains(t, suffix, "0")
		assert.NotContains(t, suffix, "1")
	}
}

package order

import (
	"fmt"
	"time"

	"github.com/manjo/ticketing/backend/pkg/randcode"
)

// orderNumberSuffixLength gives ~1.07e9 combinations per day from the 32-character
// unambiguous alphabet. Collisions are still possible, which is why the service
// retries against the orders.order_number unique constraint rather than trusting
// the draw.
const orderNumberSuffixLength = 6

// GenerateOrderNumber builds a human-quotable order number of the form
// ORD-YYYYMMDD-XXXXXX. The date prefix makes support lookups easy; the random
// suffix keeps order numbers non-enumerable.
func GenerateOrderNumber(at time.Time) (string, error) {
	suffix, err := randcode.New(orderNumberSuffixLength)
	if err != nil {
		return "", fmt.Errorf("generate order number: %w", err)
	}
	return fmt.Sprintf("ORD-%s-%s", at.UTC().Format("20060102"), suffix), nil
}

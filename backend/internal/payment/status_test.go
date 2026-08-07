package payment_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/manjo/ticketing/backend/internal/payment"
)

// This table mirrors the canonical "Payment Status Mapping" in
// specs/001-guest-purchase-flow/spec.md and contracts/api.md. Every notified
// provider status maps to exactly one outcome — none may be silently ignored.
func TestProviderStatusMapping(t *testing.T) {
	tests := []struct {
		transactionStatus string
		fraudStatus       string
		wantOrderStatus   string
		wantRestoreQuota  bool
		wantHandled       bool
	}{
		{"settlement", "", payment.OrderStatusPaid, false, true},
		{"settlement", "accept", payment.OrderStatusPaid, false, true},
		{"capture", "accept", payment.OrderStatusPaid, false, true},
		// Challenge is not a decision yet — leave the order PENDING and wait.
		{"capture", "challenge", payment.OrderStatusUnchanged, false, true},
		{"pending", "", payment.OrderStatusUnchanged, false, true},
		{"deny", "", payment.OrderStatusCancelled, true, true},
		{"cancel", "", payment.OrderStatusCancelled, true, true},
		{"expire", "", payment.OrderStatusExpired, true, true},
		{"failure", "", payment.OrderStatusCancelled, true, true},
	}

	for _, tc := range tests {
		name := tc.transactionStatus + "/" + tc.fraudStatus
		t.Run(name, func(t *testing.T) {
			got := payment.MapProviderStatus(tc.transactionStatus, tc.fraudStatus)

			assert.Equal(t, tc.wantOrderStatus, got.OrderStatus)
			assert.Equal(t, tc.wantRestoreQuota, got.RestoreQuota)
			assert.Equal(t, tc.wantHandled, got.Handled)
		})
	}
}

// An order's status must be one of the four rows in the order-status master list
// (a CHECK until migration 0009, a foreign key since, and a reference by id since
// 0013), so deny and failure both fold into CANCELLED. The raw provider status is
// what keeps them apart, and that is preserved in the payments table, not here.
func TestDenyAndFailureBothFoldIntoCancelled(t *testing.T) {
	deny := payment.MapProviderStatus("deny", "")
	failure := payment.MapProviderStatus("failure", "")

	assert.Equal(t, payment.OrderStatusCancelled, deny.OrderStatus)
	assert.Equal(t, payment.OrderStatusCancelled, failure.OrderStatus)
	assert.True(t, deny.RestoreQuota)
	assert.True(t, failure.RestoreQuota)
}

func TestUnknownStatusIsReportedAsUnhandledRatherThanIgnored(t *testing.T) {
	got := payment.MapProviderStatus("refund", "")

	assert.False(t, got.Handled, "an unrecognized status must be surfaced, not swallowed")
	assert.Equal(t, payment.OrderStatusUnchanged, got.OrderStatus)
	assert.False(t, got.RestoreQuota)
}

func TestCaptureWithAnUnrecognizedFraudStatusIsUnhandled(t *testing.T) {
	got := payment.MapProviderStatus("capture", "something-new")

	assert.False(t, got.Handled)
	assert.Equal(t, payment.OrderStatusUnchanged, got.OrderStatus)
}

func TestMappingIsCaseAndWhitespaceInsensitive(t *testing.T) {
	got := payment.MapProviderStatus("  SETTLEMENT ", "")

	assert.True(t, got.Handled)
	assert.Equal(t, payment.OrderStatusPaid, got.OrderStatus)
}

// Only a status that changes the order ever restores quota, and only the three
// terminal-failure outcomes do.
func TestQuotaIsRestoredOnlyForTerminalFailures(t *testing.T) {
	for _, status := range []string{"settlement", "pending"} {
		assert.False(t, payment.MapProviderStatus(status, "").RestoreQuota, status)
	}
	for _, status := range []string{"deny", "cancel", "expire", "failure"} {
		assert.True(t, payment.MapProviderStatus(status, "").RestoreQuota, status)
	}
}

func TestChangesOrderReportsWhetherAWriteIsNeeded(t *testing.T) {
	assert.True(t, payment.MapProviderStatus("settlement", "").ChangesOrder())
	assert.True(t, payment.MapProviderStatus("expire", "").ChangesOrder())
	assert.False(t, payment.MapProviderStatus("pending", "").ChangesOrder())
	assert.False(t, payment.MapProviderStatus("capture", "challenge").ChangesOrder())
}

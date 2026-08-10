package payment_test

import (
	"testing"

	"github.com/pgauto/cdtc/status"
	"github.com/stretchr/testify/assert"

	"github.com/manjo/ticketing/backend/internal/payment"
)

// This table mirrors the canonical status mapping in
// specs/012-manjo-payment-gateway/contracts/gateway.md. Every value the gateway
// can send maps to exactly one outcome — none may be silently ignored.
func TestProviderStatusMapping(t *testing.T) {
	tests := []struct {
		name             string
		status           status.Status
		wantOrderStatus  string
		wantRestoreQuota bool
		wantHandled      bool
	}{
		{"Pending", status.Pending, payment.OrderStatusUnchanged, false, true},
		{"Reject", status.Reject, payment.OrderStatusCancelled, true, true},
		{"Cancel", status.Cancel, payment.OrderStatusCancelled, true, true},
		{"Expired", status.Expired, payment.OrderStatusExpired, true, true},
		// "Undefined status from payment network or bank" — precisely the case
		// that must not be guessed at, so it holds the order AND signals.
		{"Obscure", status.Obscure, payment.OrderStatusUnchanged, false, false},
		{"Completed", status.Completed, payment.OrderStatusPaid, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := payment.MapProviderStatus(tc.status)

			assert.Equal(t, tc.wantOrderStatus, got.OrderStatus)
			assert.Equal(t, tc.wantRestoreQuota, got.RestoreQuota)
			assert.Equal(t, tc.wantHandled, got.Handled)
		})
	}
}

// The mapping must stay total as the enum grows. If the contract module adds a
// value and this fails, the mapping needs a decision — not a default.
func TestEveryEnumValueIsMapped(t *testing.T) {
	for _, s := range status.Enum() {
		got := payment.MapProviderStatus(s)
		if s == status.Obscure {
			assert.False(t, got.Handled, "Obscure must stay unhandled so it signals")
			continue
		}
		assert.True(t, got.Handled, "unmapped enum value %s (%d)", s, s)
	}
}

// An order's status must be one of the four rows in the order-status master list
// (a CHECK until migration 0009, a foreign key since, and a reference by id since
// 0013), so Reject and Cancel both fold into CANCELLED. The raw gateway status is
// what keeps them apart, and that is preserved in the payments table, not here.
func TestRejectAndCancelBothFoldIntoCancelled(t *testing.T) {
	reject := payment.MapProviderStatus(status.Reject)
	cancel := payment.MapProviderStatus(status.Cancel)

	assert.Equal(t, payment.OrderStatusCancelled, reject.OrderStatus)
	assert.Equal(t, payment.OrderStatusCancelled, cancel.OrderStatus)
	assert.True(t, reject.RestoreQuota)
	assert.True(t, cancel.RestoreQuota)
}

// A value outside the enum is a status this build has never seen. It may well be
// one that should have released quota, so it must be surfaced rather than
// swallowed as a no-op.
func TestOutOfRangeStatusIsReportedAsUnhandledRatherThanIgnored(t *testing.T) {
	got := payment.MapProviderStatus(status.Status(99))

	assert.False(t, got.Handled, "an unrecognised status must be surfaced, not swallowed")
	assert.Equal(t, payment.OrderStatusUnchanged, got.OrderStatus)
	assert.False(t, got.RestoreQuota)
}

// Pending is the enum's zero value, so a notification that omits the status
// decodes as "pending". The outcome must still be a safe no-op — the audit
// record is where absence is distinguished, not here.
func TestZeroValueIsASafeNoOp(t *testing.T) {
	var zero status.Status

	got := payment.MapProviderStatus(zero)

	assert.True(t, got.Handled)
	assert.Equal(t, payment.OrderStatusUnchanged, got.OrderStatus)
	assert.False(t, got.RestoreQuota)
}

// Only a status that changes the order ever restores quota, and only the
// terminal-failure outcomes do.
func TestQuotaIsRestoredOnlyForTerminalFailures(t *testing.T) {
	for _, s := range []status.Status{status.Completed, status.Pending, status.Obscure} {
		assert.False(t, payment.MapProviderStatus(s).RestoreQuota, s.String())
	}
	for _, s := range []status.Status{status.Reject, status.Cancel, status.Expired} {
		assert.True(t, payment.MapProviderStatus(s).RestoreQuota, s.String())
	}
}

func TestChangesOrderReportsWhetherAWriteIsNeeded(t *testing.T) {
	assert.True(t, payment.MapProviderStatus(status.Completed).ChangesOrder())
	assert.True(t, payment.MapProviderStatus(status.Expired).ChangesOrder())
	assert.False(t, payment.MapProviderStatus(status.Pending).ChangesOrder())
	assert.False(t, payment.MapProviderStatus(status.Obscure).ChangesOrder())
}

// IsTerminalFailure is what separates a notification that merely repeats a
// settled outcome from one that contradicts it, so a paid order is only marked
// DISPUTED for the second kind.
func TestIsTerminalFailureIdentifiesContradictions(t *testing.T) {
	for _, s := range []status.Status{status.Reject, status.Cancel, status.Expired} {
		assert.True(t, payment.MapProviderStatus(s).IsTerminalFailure(), s.String())
	}
	for _, s := range []status.Status{status.Completed, status.Pending, status.Obscure} {
		assert.False(t, payment.MapProviderStatus(s).IsTerminalFailure(), s.String())
	}
}

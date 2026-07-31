// Package payment owns the payments table, the gateway abstraction, and the
// webhook that drives an order through its lifecycle.
package payment

import "strings"

// Order statuses this mapping can produce. They match the CHECK constraint on
// orders.status in SCHEMA.md, which is LOCKED.
const (
	OrderStatusPaid      = "PAID"
	OrderStatusCancelled = "CANCELLED"
	OrderStatusExpired   = "EXPIRED"
	// OrderStatusUnchanged means the notification is a legitimate no-op: the order
	// keeps whatever status it already has.
	OrderStatusUnchanged = ""
)

// Outcome is the decision a single provider notification implies.
type Outcome struct {
	// OrderStatus is the status to move the order to, or OrderStatusUnchanged.
	OrderStatus string
	// RestoreQuota reports whether the reserved quota must be returned to the pool.
	RestoreQuota bool
	// Handled is false for a status this mapping does not recognize. Callers must
	// log that loudly rather than treat it as a no-op, because an unrecognized
	// status may well be one that should have released quota.
	Handled bool
}

// ChangesOrder reports whether this outcome requires writing to the order at all.
func (o Outcome) ChangesOrder() bool { return o.OrderStatus != OrderStatusUnchanged }

// MapProviderStatus is the total mapping from a provider's transaction_status
// (plus fraud_status where it matters) onto an order outcome. It is a pure
// function so the full table can be verified without a database or a gateway.
//
// The canonical table lives in specs/001-guest-purchase-flow/spec.md
// ("Payment Status Mapping", FR-011/FR-019) and contracts/api.md.
func MapProviderStatus(transactionStatus, fraudStatus string) Outcome {
	switch normalize(transactionStatus) {
	case "settlement":
		return Outcome{OrderStatus: OrderStatusPaid, Handled: true}

	case "capture":
		// A capture is only money in the bank once fraud review accepts it.
		switch normalize(fraudStatus) {
		case "accept":
			return Outcome{OrderStatus: OrderStatusPaid, Handled: true}
		case "challenge":
			// Not a decision yet: hold the order and its quota until a definitive
			// notification arrives.
			return Outcome{OrderStatus: OrderStatusUnchanged, Handled: true}
		default:
			return Outcome{OrderStatus: OrderStatusUnchanged, Handled: false}
		}

	case "pending":
		return Outcome{OrderStatus: OrderStatusUnchanged, Handled: true}

	// deny and failure both fold into CANCELLED: orders.status is CHECK-constrained
	// and SCHEMA.md is locked. The raw provider status is preserved in
	// payments.status so the two stay distinguishable for support and audit.
	case "deny", "cancel", "failure":
		return Outcome{OrderStatus: OrderStatusCancelled, RestoreQuota: true, Handled: true}

	case "expire":
		return Outcome{OrderStatus: OrderStatusExpired, RestoreQuota: true, Handled: true}

	default:
		return Outcome{OrderStatus: OrderStatusUnchanged, Handled: false}
	}
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

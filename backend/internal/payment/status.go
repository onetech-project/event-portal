// Package payment owns the payments table, the gateway abstraction, and the
// callback that drives an order through its lifecycle.
package payment

import (
	"github.com/pgauto/cdtc/status"
)

// Order statuses this mapping can produce. They match the CHECK constraint on
// orders.status in SCHEMA.md, which is LOCKED.
const (
	// OrderStatusPending is the only status an order can transition out of. Every
	// path that moves an order guards on it.
	OrderStatusPending   = "PENDING"
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

// IsTerminalFailure reports whether this outcome ends the order unpaid. It is
// what distinguishes a notification that merely repeats a recorded outcome from
// one that contradicts it (FR-016b).
func (o Outcome) IsTerminalFailure() bool {
	return o.OrderStatus == OrderStatusCancelled || o.OrderStatus == OrderStatusExpired
}

// MapProviderStatus is the total mapping from the gateway's status enum onto an
// order outcome. It is a pure function so the full table can be verified without
// a database or a gateway.
//
// The mapping preserves the constitution's requirement that deny/cancel/failure
// and expire atomically set CANCELLED/EXPIRED and restore quota: Reject and
// Cancel are this gateway's names for deny and failure, Expired for expire.
//
// Obscure is documented upstream as "undefined status from payment network or
// bank" — precisely the case that must not be guessed at, so it holds the order
// and signals rather than choosing a side.
//
// The canonical table lives in specs/012-manjo-payment-gateway/contracts/gateway.md.
func MapProviderStatus(s status.Status) Outcome {
	switch s {
	case status.Pending:
		return Outcome{OrderStatus: OrderStatusUnchanged, Handled: true}

	// Reject and Cancel both fold into CANCELLED: orders.status is
	// CHECK-constrained and SCHEMA.md is locked. The raw value is preserved in
	// payments.status so the two stay distinguishable for support and audit.
	case status.Reject, status.Cancel:
		return Outcome{OrderStatus: OrderStatusCancelled, RestoreQuota: true, Handled: true}

	case status.Expired:
		return Outcome{OrderStatus: OrderStatusExpired, RestoreQuota: true, Handled: true}

	case status.Obscure:
		return Outcome{OrderStatus: OrderStatusUnchanged, Handled: false}

	case status.Completed:
		return Outcome{OrderStatus: OrderStatusPaid, Handled: true}

	default:
		return Outcome{OrderStatus: OrderStatusUnchanged, Handled: false}
	}
}

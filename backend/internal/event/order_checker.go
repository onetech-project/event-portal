package event

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OrderChecker is the contract this domain needs from the order domain, declared
// here by its consumer (ARCHITECTURE.md §3.2). It is the ONLY dependency the event
// domain has on order data, and nothing here imports internal/order.
//
// Every method takes the caller's transaction where it guards a delete, so the
// check and the DELETE share one transaction and no order can slip in between
// them.
//
// The delete guards must consider order_items AND attendees: SCHEMA.md declares
// ON DELETE RESTRICT on both foreign keys, so guarding only one would surface a
// raw constraint violation instead of the 400 the constitution requires
// (Principle VI).
type OrderChecker interface {
	// HasOrdersForTicketType reports whether one ticket type is referenced.
	HasOrdersForTicketType(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID) (bool, error)
	// HasOrdersForTicketTypes reports whether any of a set is referenced, used
	// when deleting an event, which must consider all of its ticket types.
	HasOrdersForTicketTypes(ctx context.Context, tx pgx.Tx, ticketTypeIDs []uuid.UUID) (bool, error)
	// SoldCountByTicketType returns the derived sold count per ticket type,
	// batched for a whole page. Never stored, never JOINed across domains.
	SoldCountByTicketType(ctx context.Context, ticketTypeIDs []uuid.UUID) (map[uuid.UUID]int, error)
	// SoldCountByPackage returns the derived sold count per package, batched
	// for the admin dashboard. Uses the package_id column on order_items.
	SoldCountByPackage(ctx context.Context, packageIDs []uuid.UUID) (map[uuid.UUID]int, error)
	// HasOrdersForPackage reports whether a package is referenced by an
	// order_items or attendees row, guarding its delete.
	HasOrdersForPackage(ctx context.Context, tx pgx.Tx, packageID uuid.UUID) (bool, error)
	// HasPendingOrdersForPackage reports whether an open PENDING order holds the
	// package, locking its composition while the hold exists (research.md R-004).
	HasPendingOrdersForPackage(ctx context.Context, tx pgx.Tx, packageID uuid.UUID) (bool, error)
}

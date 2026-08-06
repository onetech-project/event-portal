package order

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/manjo/ticketing/backend/pkg/apperr"
)

// ExpandedItem is what one checkout item resolves to inside the transaction.
// Tickets pass through unchanged; packages are expanded into per-component
// demand for quota aggregation, but kept as a single order_item line at the
// package's own price.
type ExpandedItem struct {
	// Ref is the XOR: TicketLine for tickets, PackageLine for bundles.
	Ref       LineRef
	Quantity  int32
	UnitPrice decimal.Decimal
	// Demand is the per-ticket-type demand this item contributes to the
	// aggregate. For a ticket line: one entry. For a package: N entries
	// (one per component).
	Demand map[uuid.UUID]int32
	// PerUnitDemand is one bundle unit's composition (component ticket type →
	// quantity per single unit), so booking can stamp attendees.package_unit
	// (spec 010). Nil for ticket lines. Demand == PerUnitDemand × Quantity.
	PerUnitDemand map[uuid.UUID]int32
	// PackageName is non-empty only for package lines, for payment items.
	PackageName string
	// TicketName is non-empty only for ticket lines, for payment items.
	TicketName string
	// EventID is the event this line sells for, so booking can refuse a request
	// mixing another event's items into the one being booked.
	EventID uuid.UUID
}

// expandItem resolves one checkout item into server-validated demand. Server
// prices are always used (FR-006); client-supplied totals are ignored.
func expandItem(ctx context.Context, events EventProvider, tx pgx.Tx, item CheckoutItem, now time.Time) (ExpandedItem, error) {
	if item.PackageID != nil {
		return expandPackage(ctx, events, tx, *item.PackageID, item.Quantity, now)
	}
	return expandTicket(ctx, events, tx, *item.TicketTypeID, item.Quantity, now)
}

func expandTicket(ctx context.Context, events EventProvider, tx pgx.Tx, id uuid.UUID, qty int32, now time.Time) (ExpandedItem, error) {
	info, err := events.TicketTypeForCheckout(ctx, tx, id)
	if err != nil {
		return ExpandedItem{}, err
	}

	if now.Before(info.SalesStart) || now.After(info.SalesEnd) {
		return ExpandedItem{}, apperr.BadRequest(apperr.CodeTicketTypeNotOnSale,
			fmt.Sprintf("Ticket type %q is not currently on sale.", info.Name))
	}

	return ExpandedItem{
		Ref:        TicketLine(id),
		Quantity:   qty,
		UnitPrice:  info.Price,
		Demand:     map[uuid.UUID]int32{id: qty},
		TicketName: info.Name,
		EventID:    info.EventID,
	}, nil
}

func expandPackage(ctx context.Context, events EventProvider, tx pgx.Tx, id uuid.UUID, qty int32, now time.Time) (ExpandedItem, error) {
	pkg, err := events.PackageForCheckout(ctx, tx, id)
	if err != nil {
		return ExpandedItem{}, err
	}

	if now.Before(pkg.SalesStart) || now.After(pkg.SalesEnd) {
		return ExpandedItem{}, apperr.BadRequest(apperr.CodePackageNotOnSale,
			fmt.Sprintf("Package %q is not currently on sale.", pkg.Name))
	}

	if len(pkg.Components) == 0 {
		return ExpandedItem{}, apperr.BadRequest(apperr.CodeValidation,
			fmt.Sprintf("Package %q has no components and cannot be purchased.", pkg.Name))
	}

	// Validate every constituent's sales window is open (FR-012): the binding
	// constraint for package purchasability. A package whose Day 2 ticket is
	// outside its window cannot be sold even though Day 1 is open.
	for _, c := range pkg.Components {
		if now.Before(c.SalesStart) || now.After(c.SalesEnd) {
			return ExpandedItem{}, apperr.BadRequest(apperr.CodePackageNotOnSale,
				fmt.Sprintf("A constituent of package %q is not currently on sale.", pkg.Name))
		}
	}

	perUnit := make(map[uuid.UUID]int32, len(pkg.Components))
	for _, c := range pkg.Components {
		perUnit[c.TicketTypeID] += c.Quantity
	}
	demand := make(map[uuid.UUID]int32, len(perUnit))
	for ttID, unitQty := range perUnit {
		demand[ttID] = qty * unitQty
	}

	return ExpandedItem{
		Ref:           PackageLine(id),
		Quantity:      qty,
		UnitPrice:     pkg.Price,
		Demand:        demand,
		PerUnitDemand: perUnit,
		PackageName:   pkg.Name,
		EventID:       pkg.EventID,
	}, nil
}

// aggregateDemand merges every expanded item's per-ticket-type demand into one
// map, summing across standalone lines and package expansions. This is the step
// that prevents the oversell described in contracts/checkout-transaction.md
// §2.3: two lines touching the same ticket type must never be validated in
// isolation from each other.
func aggregateDemand(items []ExpandedItem) map[uuid.UUID]int32 {
	total := map[uuid.UUID]int32{}
	for _, item := range items {
		for ttID, qty := range item.Demand {
			total[ttID] += qty
		}
	}
	return total
}

// sortedTicketTypeIDs returns the keys of a demand map in ascending string
// order, which is the globally consistent lock order that prevents the
// deadlock described in contracts/checkout-transaction.md §2.4.
func sortedTicketTypeIDs(demand map[uuid.UUID]int32) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(demand))
	for id := range demand {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	return ids
}

// validateAttendees checks the request's attendee slots against the server's
// expansion. The cheap Validate pass in dto.go cannot size package attendee
// slots because the composition is only known once the database has been read:
// a bundle of quantity q with components c1..cN demands q × Σ(ci.quantity)
// slots, grouped by (package_id, ticket_type_id). Exactly matching those groups
// is what guarantees every bundle attendee maps to a real seat (FR-004, T046) —
// one too few, one too many, or a slot bound to the wrong constituent are all
// rejected here, before any quota is touched.
func validateAttendees(req CheckoutRequest, expanded []ExpandedItem) error {
	expected := make(map[string]int32, len(req.Items))
	for _, item := range expanded {
		if item.Ref.IsPackage() {
			pkgID := item.Ref.PackageID.UUID
			for ttID, qty := range item.Demand {
				expected[packageSlotKey(pkgID, ttID)] = qty
			}
			continue
		}
		expected["tt:"+item.Ref.TicketTypeID.UUID.String()] = item.Quantity
	}

	actual := make(map[string]int32, len(req.Attendees))
	for _, attendee := range req.Attendees {
		if attendee.PackageID != nil {
			actual[packageSlotKey(*attendee.PackageID, attendee.TicketTypeID)]++
			continue
		}
		actual["tt:"+attendee.TicketTypeID.String()]++
	}

	for ref, qty := range expected {
		if actual[ref] != qty {
			return apperr.BadRequest(apperr.CodeAttendeeCountMismatch,
				fmt.Sprintf("Reference %s requires %d attendee(s) after bundle expansion, got %d.",
					ref, qty, actual[ref]))
		}
	}
	for ref := range actual {
		if _, ok := expected[ref]; !ok {
			return apperr.BadRequest(apperr.CodeAttendeeCountMismatch,
				fmt.Sprintf("An attendee references %s, which is not in the expanded order.", ref))
		}
	}
	return nil
}

func packageSlotKey(pkgID, ttID uuid.UUID) string {
	return "pkg:" + pkgID.String() + ":" + ttID.String()
}

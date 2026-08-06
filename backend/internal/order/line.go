package order

import "github.com/google/uuid"

// LineRef identifies what one order line sells: an individual ticket type or a
// package, never both. It mirrors the order_items_line_kind_chk constraint, so a
// value that fails Valid cannot be persisted either.
//
// Packages are stored as ONE line at the package's own price rather than expanded
// into per-constituent rows, which is what keeps total_amount exactly equal to
// SUM(quantity * price) without inventing a price split across constituents.
type LineRef struct {
	TicketTypeID uuid.NullUUID
	PackageID    uuid.NullUUID
}

// TicketLine refers to an individual ticket type.
func TicketLine(id uuid.UUID) LineRef {
	return LineRef{TicketTypeID: uuid.NullUUID{UUID: id, Valid: true}}
}

// PackageLine refers to a package.
func PackageLine(id uuid.UUID) LineRef {
	return LineRef{PackageID: uuid.NullUUID{UUID: id, Valid: true}}
}

// Valid reports whether exactly one reference is set, matching the CHECK.
func (r LineRef) Valid() bool {
	return r.TicketTypeID.Valid != r.PackageID.Valid
}

// IsPackage reports whether this line sells a package.
func (r LineRef) IsPackage() bool { return r.PackageID.Valid }

// AttendeeRef binds one registrant slot to the ticket type whose gate their pass
// opens, and — when the slot came from a bundle — to the package it originated in.
//
// TicketTypeID is never null, including for bundle-derived registrants. That is
// what keeps one pass per attendee true for packages: a bundle produces more
// attendees rather than a pass that spans several ticket types.
type AttendeeRef struct {
	TicketTypeID uuid.UUID
	PackageID    uuid.NullUUID
	// PackageUnit is the ordinal of the purchased bundle unit this slot belongs
	// to (1..quantity of its package line), so one visitor form can fill the
	// whole unit (spec 010). Nil for standalone slots.
	PackageUnit *int16
}

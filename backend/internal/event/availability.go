// Domain types for ticket package bundles. A package holds no inventory: every
// figure here is derived at read time from the remaining quota of the ticket
// types reached through package_tickets (contracts/checkout-transaction.md §0).
package event

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// PackageAvailability is the derived, never-stored inventory view of a package.
//
// AvailableUnits is min over constituents of floor(quota / quantity): the number
// of COMPLETE package sets currently coverable. Whole sets only — a constituent
// with 5 remaining consumed 2 at a time yields 2, not 2.5.
//
// Purchasable folds in every gate the guest actually faces: units > 0, at least
// one component, the package's own sales window, and every constituent's window.
// It is computed server-side so the client renders one boolean and cannot
// disagree with the server about what is buyable.
//
// LimitingTicketTypeID names the scarcest constituent — the one that will be
// reported when a checkout is rejected. Zero (uuid.Nil) means the package has no
// components.
type PackageAvailability struct {
	PackageID            uuid.UUID
	AvailableUnits       int32
	Purchasable          bool
	LimitingTicketTypeID uuid.UUID
}

// PackageComponent is one constituent of a package, resolved for checkout:
// how many units of a given ticket type one unit of the package consumes, plus
// that ticket's own sales window (the binding constraint for purchasability).
type PackageComponent struct {
	TicketTypeID uuid.UUID
	Name         string
	PerUnit      int32
	SalesStart   time.Time
	SalesEnd     time.Time
}

// PackageForCheckout is the server-side truth about a package at checkout time:
// authoritative price, sales window, status and full composition. Client-supplied
// prices are ignored in favour of this (FR-025/FR-026).
type PackageForCheckout struct {
	ID         uuid.UUID
	EventID    uuid.UUID
	Name       string
	Price      decimal.Decimal
	SalesStart time.Time
	SalesEnd   time.Time
	IsActive   bool
	Components []PackageComponent
}

// HasComponents reports whether the package has at least one constituent, which
// checkout requires before it can expand a line into demand (FR-016).
func (p PackageForCheckout) HasComponents() bool { return len(p.Components) > 0 }

package cache

import (
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Family names one cacheable list. The set below is CLOSED: Constitution
// Principle VII enumerates exactly these surfaces, and adding a tenth requires
// amending the constitution, not just adding a constant here.
//
// Family is also the Prometheus label, which is why it is a small fixed set —
// an event id or a filter value must never reach a metric label.
type Family string

const (
	// Public, guest-facing. These carry the traffic that motivated the feature.
	FamilyEventsPublic      Family = "events_public"
	FamilyTicketTypesPublic Family = "ticket_types_public"
	FamilyPackagesPublic    Family = "packages_public"

	// Admin projections of the same rows, invalidated by the same writes.
	FamilyEventsAdmin      Family = "events_admin"
	FamilyTicketTypesAdmin Family = "ticket_types_admin"
	FamilyPackagesAdmin    Family = "packages_admin"
	FamilyOrdersAdmin      Family = "orders_admin"
	FamilyAttendeesAdmin   Family = "attendees_admin"

	// Packages resolved by event id rather than slug (internal read path).
	FamilyPackagesByEvent Family = "packages_by_event"
)

// Families is the registry. Anything not listed here cannot be cached: the key
// constructors below are the only way to build a Key, and each one names its
// family explicitly.
var Families = []Family{
	FamilyEventsPublic,
	FamilyTicketTypesPublic,
	FamilyPackagesPublic,
	FamilyEventsAdmin,
	FamilyTicketTypesAdmin,
	FamilyPackagesAdmin,
	FamilyOrdersAdmin,
	FamilyAttendeesAdmin,
	FamilyPackagesByEvent,
}

// --- Paging -----------------------------------------------------------------

// Paging is the slice of a list an entry holds. It joins the key's FINGERPRINT
// and deliberately touches neither Family nor Scope:
//
//   - not Family, because Family is the Prometheus label and a page number there
//     would make the label set unbounded;
//   - not Scope, because a write must be able to orphan every page of every
//     filter variant with one INCR, without knowing which pages were ever warm.
//
// That placement is also what keeps paging inside Constitution Principle VII's
// closed surface list: a page is a filtered variant of a list already on it, not
// a new surface (spec 021 research R5).
type Paging struct {
	Page int
	Size int
}

// fingerprint renders the page in the same fixed-order, builder-based style as
// every other fingerprint component, so the same request always produces the
// same key and two different requests never collide.
func (p Paging) fingerprint() string {
	var b strings.Builder
	b.WriteString("p=")
	b.WriteString(strconv.Itoa(p.Page))
	b.WriteString(":n=")
	b.WriteString(strconv.Itoa(p.Size))
	return b.String()
}

// --- Key constructors -------------------------------------------------------
//
// One per surface. Fingerprints are rendered in a FIXED order — never from map
// iteration — so the same parameters always produce the same key, and different
// parameters never collide.

// EventsPublicKey is the guest catalogue. No parameters.
func EventsPublicKey() Key {
	return Key{Family: FamilyEventsPublic, Scope: Events()}
}

// EventsAdminKey is one page of the admin event table.
func EventsAdminKey(pg Paging) Key {
	return Key{Family: FamilyEventsAdmin, Scope: Events(), Fingerprint: pg.fingerprint()}
}

// TicketTypesPublicKey is one event's guest-facing ticket list.
func TicketTypesPublicKey(eventID uuid.UUID) Key {
	return Key{Family: FamilyTicketTypesPublic, Scope: Event(eventID)}
}

// TicketTypesAdminKey is one event's admin ticket table.
func TicketTypesAdminKey(eventID uuid.UUID) Key {
	return Key{Family: FamilyTicketTypesAdmin, Scope: Event(eventID)}
}

// PackagesPublicKey is one event's guest-facing package list.
func PackagesPublicKey(eventID uuid.UUID) Key {
	return Key{Family: FamilyPackagesPublic, Scope: Event(eventID)}
}

// PackagesAdminKey is one event's admin package table.
func PackagesAdminKey(eventID uuid.UUID) Key {
	return Key{Family: FamilyPackagesAdmin, Scope: Event(eventID)}
}

// PackagesByEventKey is the by-id package read.
func PackagesByEventKey(eventID uuid.UUID) Key {
	return Key{Family: FamilyPackagesByEvent, Scope: Event(eventID)}
}

// OrdersAdminKey is one page of one filter combination of the admin order list.
// Every variant shares the orders scope, so a single order change invalidates all
// of them with one INCR — which is the whole reason the generation counter
// exists, and what lets paging multiply the entry count without multiplying the
// invalidation work.
func OrdersAdminKey(status *string, eventID *uuid.UUID, pg Paging) Key {
	var b strings.Builder
	b.WriteString("st=")
	b.WriteString(optString(status))
	b.WriteString(":ev=")
	b.WriteString(optUUID(eventID))
	b.WriteString(":")
	b.WriteString(pg.fingerprint())
	return Key{Family: FamilyOrdersAdmin, Scope: Orders(), Fingerprint: b.String()}
}

// AttendeesAdminKey is one page of one filter combination of the admin attendee
// list.
func AttendeesAdminKey(orderID, eventID *uuid.UUID, pg Paging) Key {
	var b strings.Builder
	b.WriteString("or=")
	b.WriteString(optUUID(orderID))
	b.WriteString(":ev=")
	b.WriteString(optUUID(eventID))
	b.WriteString(":")
	b.WriteString(pg.fingerprint())
	return Key{Family: FamilyAttendeesAdmin, Scope: Orders(), Fingerprint: b.String()}
}

// optString renders an absent filter as "_", which is distinct from every
// present value: statuses are a closed uppercase enum and never equal "_".
func optString(v *string) string {
	if v == nil || *v == "" {
		return "_"
	}
	return *v
}

// optUUID renders an absent id as "_", distinct from any UUID rendering.
func optUUID(v *uuid.UUID) string {
	if v == nil || *v == uuid.Nil {
		return "_"
	}
	return v.String()
}

package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// Family names one cacheable list. The set below is CLOSED: Constitution
// Principle VII enumerates exactly these surfaces, and adding to it requires
// amending the constitution, not just adding a constant here. FamilyGendersMaster
// was added that way — constitution v6.1.0 admits the gender master list BY NAME,
// and deliberately does not admit master data as a category.
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

	// The gender master list (spec 023). ONE family for both projections, not
	// two: the active-only list is a filtered variant of the all-known list, and
	// the Paging comment below records why a variant belongs in the fingerprint
	// rather than in the family — Family is the Prometheus label, and a variant
	// there makes the label set grow with the variants.
	FamilyGendersMaster Family = "genders_master"
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
	FamilyGendersMaster,
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

// GendersActiveKey is the ACTIVE-only gender master list: the options a holder
// form offers, and what a registration validates and resolves against.
//
// Distinct from GendersAllKey and it must stay that way. Collapsing the two would
// either refuse a retired gender that checkout must accept on a slot which
// already held it (spec 011 FR-031), or offer one that a registration form must
// not (spec 022 FR-022a). The fingerprints are compile-time constants, so nothing
// caller-supplied reaches the key and the optSearch hashing concern below does
// not arise here.
func GendersActiveKey() Key {
	return Key{Family: FamilyGendersMaster, Scope: Master(), Fingerprint: "proj=active"}
}

// GendersAllKey is every gender, retired included: what checkout resolves a
// restored form's stored value through. See GendersActiveKey for why the two are
// separate keys.
func GendersAllKey() Key {
	return Key{Family: FamilyGendersMaster, Scope: Master(), Fingerprint: "proj=all"}
}

// OrdersAdminKey is one page of one filter combination of the admin order list.
// Every variant shares the orders scope, so a single order change invalidates all
// of them with one INCR — which is the whole reason the generation counter
// exists, and what lets paging multiply the entry count without multiplying the
// invalidation work.
func OrdersAdminKey(status *string, eventID *uuid.UUID, search *string, pg Paging) Key {
	var b strings.Builder
	b.WriteString("st=")
	b.WriteString(optString(status))
	b.WriteString(":ev=")
	b.WriteString(optUUID(eventID))
	b.WriteString(":q=")
	b.WriteString(optSearch(search))
	b.WriteString(":")
	b.WriteString(pg.fingerprint())
	return Key{Family: FamilyOrdersAdmin, Scope: Orders(), Fingerprint: b.String()}
}

// AttendeesAdminKey is one page of one filter combination of the admin attendee
// list.
func AttendeesAdminKey(orderID, eventID *uuid.UUID, search *string, pg Paging) Key {
	var b strings.Builder
	b.WriteString("or=")
	b.WriteString(optUUID(orderID))
	b.WriteString(":ev=")
	b.WriteString(optUUID(eventID))
	b.WriteString(":q=")
	b.WriteString(optSearch(search))
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

// optSearch renders an operator's free-text search term (spec 022 FR-053).
//
// HASHED rather than interpolated, unlike optString beside it, and the difference
// is not stylistic. optString's contract rests on statuses being a closed
// uppercase enum: no status can collide with the "_" absence marker or contain
// the ":" that separates key components. A search term is arbitrary text an
// operator typed. Interpolating it would let "a:ev=..." forge a different
// filter's key and serve one search's results to another, and would let a pasted
// paragraph produce an unbounded Redis key.
//
// Truncated to 128 bits, which is far beyond what an accidental collision needs
// and keeps the key short. An absent or empty term stays "_", so the unfiltered
// list keeps a stable, readable key.
func optSearch(v *string) string {
	if v == nil || *v == "" {
		return "_"
	}
	sum := sha256.Sum256([]byte(*v))
	return hex.EncodeToString(sum[:16])
}

// optUUID renders an absent id as "_", distinct from any UUID rendering.
func optUUID(v *uuid.UUID) string {
	if v == nil || *v == uuid.Nil {
		return "_"
	}
	return v.String()
}

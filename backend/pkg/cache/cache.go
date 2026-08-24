// Package cache is the read cache in front of the product's highest-volume list
// reads (Constitution Principle VII). It is shared, non-domain infrastructure and
// therefore lives under pkg/ alongside db and observability (Principle I).
//
// Three rules shape everything here, and none of them is negotiable:
//
//   - PostgreSQL is the source of truth. Nothing exists only in this cache.
//     Flushing it entirely costs latency and nothing else.
//   - Freshness comes from invalidating on commit, never from expiry. The TTL is
//     a backstop that bounds the damage of an invalidation that was missed, not
//     the mechanism by which the system becomes correct.
//   - No cache command may be issued inside an order-writing transaction. The
//     quota-deducting UPDATE holds a row lock until commit, so a round-trip there
//     would serialise every concurrent buyer of the same ticket type behind
//     network latency. This is enforced at runtime, not by convention: see
//     ErrInTransaction and db.InTransaction.
package cache

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrInTransaction is returned when a cache operation is attempted from inside a
// database transaction. It is a programming error, never a runtime condition:
// callers must invalidate after their transaction commits.
var ErrInTransaction = errors.New("cache: operation attempted inside a database transaction")

// Lists is the contract domains depend on. They receive this interface and never
// a *redis.Client, so the store can be replaced or removed without touching a
// single domain (Principle VII).
type Lists interface {
	// Get returns the cached bytes for k. A miss is (nil, false, nil) — never an
	// error, because a cache miss is not a failure. A store failure is also
	// reported as a miss with a non-nil error, so callers can count it while
	// still falling through to the database.
	Get(ctx context.Context, k Key) ([]byte, bool, error)

	// Set stores v under k with the configured TTL. Failures are advisory: the
	// caller already has the value it needs.
	Set(ctx context.Context, k Key, v []byte) error

	// Invalidate bumps the generation of every scope, orphaning every entry
	// derived from them. It MUST be called only after the writing transaction has
	// committed.
	Invalidate(ctx context.Context, scopes ...Scope) error

	// FlushAll discards every entry and every generation counter, returning how
	// many keys were present beforehand.
	FlushAll(ctx context.Context) (int64, error)

	// Ping reports whether the store is reachable.
	Ping(ctx context.Context) error

	// Enabled reports whether this is a real cache. False for NoOp, which lets
	// Through skip its miss-collapsing so a disabled cache reproduces the
	// pre-cache execution path exactly (FR-021).
	Enabled() bool

	// Trusted reports whether cached values may currently be served. It goes
	// false when an invalidation failed after its write committed: at that point
	// entries are provably wrong, and reading the database is the only correct
	// answer (FR-014).
	Trusted() bool
}

// --- Scopes ----------------------------------------------------------------

// ScopeKind enumerates the invalidation scopes. There are deliberately no
// others: per-event granularity is what keeps one event's writes from disturbing
// another's entries, and a single shared orders scope is what lets one order
// change reach every admin filter variant with one command.
type ScopeKind string

const (
	// ScopeKindEvents covers the public catalogue and the admin event list.
	ScopeKindEvents ScopeKind = "events"
	// ScopeKindEvent covers one event's ticket-type and package lists, public
	// and admin.
	ScopeKindEvent ScopeKind = "event"
	// ScopeKindOrders covers every admin order and attendee list variant.
	ScopeKindOrders ScopeKind = "orders"
	// ScopeKindMaster covers master data — today, the gender list in both its
	// projections (constitution v6.1.0, spec 023).
	//
	// DISJOINT from the three above, and that is the point rather than an
	// accident: no write path touches master data and any other scope, because
	// master data has no write path at all. That disjointness is what lets the
	// startup invalidation this scope exists for stay narrow — bumping it must
	// not discard the event catalogue or the admin order lists, which are
	// expensive to rebuild and already correct by their own write-triggered
	// invalidation.
	ScopeKindMaster ScopeKind = "master"
)

// Scope names what a write invalidates.
type Scope struct {
	Kind ScopeKind
	ID   uuid.UUID // set only for ScopeKindEvent
}

// Events is the catalogue-wide scope.
func Events() Scope { return Scope{Kind: ScopeKindEvents} }

// Event is one event's scope.
func Event(id uuid.UUID) Scope { return Scope{Kind: ScopeKindEvent, ID: id} }

// Orders is the order/attendee-wide scope.
func Orders() Scope { return Scope{Kind: ScopeKindOrders} }

// Master is the master-data scope. Like Events and Orders, and unlike Event, it
// carries no id: there is one master-data scope for the process, and both gender
// projections derive from it — so one INCR orphans both, which is correct,
// because the migration that changes the table changes both.
func Master() Scope { return Scope{Kind: ScopeKindMaster} }

// GenerationKey is the Redis key holding this scope's counter. It carries no TTL:
// losing it while derived entries survived would reset the generation to a value
// whose entries already exist and are stale.
func (s Scope) GenerationKey() string {
	if s.Kind == ScopeKindEvent {
		return "gen:event:" + s.ID.String()
	}
	return "gen:" + string(s.Kind)
}

// String renders the scope for logs and metric labels. Event scopes collapse to
// the bare kind so the event id never becomes a Prometheus label value.
func (s Scope) String() string { return string(s.Kind) }

// ScopeSet is a deduplicated set of scopes, for accumulating what a write touched.
type ScopeSet struct {
	m map[Scope]struct{}
}

// NewScopeSet returns an empty set.
func NewScopeSet() *ScopeSet { return &ScopeSet{m: make(map[Scope]struct{})} }

// Add records scopes, ignoring repeats.
func (s *ScopeSet) Add(scopes ...Scope) {
	if s.m == nil {
		s.m = make(map[Scope]struct{})
	}
	for _, sc := range scopes {
		s.m[sc] = struct{}{}
	}
}

// Slice returns the accumulated scopes in unspecified order.
func (s *ScopeSet) Slice() []Scope {
	out := make([]Scope, 0, len(s.m))
	for sc := range s.m {
		out = append(out, sc)
	}
	return out
}

// Len reports how many distinct scopes were added.
func (s *ScopeSet) Len() int { return len(s.m) }

// --- Keys ------------------------------------------------------------------

// Key identifies one cached list response: which list it is, which scope's
// generation it derives from, and which parameters produced it.
type Key struct {
	Family      Family
	Scope       Scope
	Fingerprint string
}

// EntryPrefix is everything up to the generation number. The generation is
// appended last so the read script can build the full key with one concatenation
// after fetching the counter.
//
//	list:events_public:-:g
//	list:ticket_types_public:8f0e…:-:g
//	list:orders_admin:st=PAID:ev=_:g
func (k Key) EntryPrefix() string {
	var b strings.Builder
	b.WriteString("list:")
	b.WriteString(string(k.Family))
	b.WriteString(":")
	if k.Scope.Kind == ScopeKindEvent {
		b.WriteString(k.Scope.ID.String())
		b.WriteString(":")
	}
	if k.Fingerprint == "" {
		b.WriteString("-")
	} else {
		b.WriteString(k.Fingerprint)
	}
	b.WriteString(":g")
	return b.String()
}

// GenerationKey is the counter this key's generation is read from.
func (k Key) GenerationKey() string { return k.Scope.GenerationKey() }

// --- NoOp ------------------------------------------------------------------

// NoOp satisfies Lists while caching nothing. It is what gets wired when
// CACHE_ENABLED is false or REDIS_URL is empty, so the kill switch is a
// substitution rather than a branch scattered through the domains (FR-021).
type NoOp struct{}

func (NoOp) Get(context.Context, Key) ([]byte, bool, error) { return nil, false, nil }
func (NoOp) Set(context.Context, Key, []byte) error         { return nil }
func (NoOp) Invalidate(context.Context, ...Scope) error     { return nil }
func (NoOp) FlushAll(context.Context) (int64, error)        { return 0, nil }
func (NoOp) Ping(context.Context) error                     { return nil }
func (NoOp) Enabled() bool                                  { return false }
func (NoOp) Trusted() bool                                  { return true }

// compile-time check
var _ Lists = NoOp{}

# Phase 1 Data Model: Refresh-on-Write List Caching

**Feature**: 014-redis-list-cache | **Date**: 2026-08-10

**No PostgreSQL schema change.** No table, column, index, or constraint is added, altered,
or dropped, so `SCHEMA.md` is untouched. Every entity below lives in Redis or in process
memory and is disposable by definition (Constitution Principle VII).

---

## 1. Scope

The unit of invalidation. A scope is what a write names when it says "these lists are now
wrong".

| Field | Type | Notes |
|---|---|---|
| `Kind` | enum | `events` (global catalogue), `event` (one event), `orders` (global) |
| `ID` | `uuid.UUID` | Set only for `Kind == event`; zero otherwise |

Exactly three scope shapes exist:

| Scope | Redis generation key | Covers |
|---|---|---|
| `events` | `gen:events` | The public event catalogue and the admin event list |
| `event:{uuid}` | `gen:event:{uuid}` | That event's public and admin ticket-type lists, and its public and admin package lists |
| `orders` | `gen:orders` | Every admin order list variant and every admin attendee list variant |

**Why only three**: FR-009 requires that one event's writes not disturb another event's
entries, which forces per-event granularity for ticket and package lists. FR-010 requires
that a single order change reach every filter variant that could contain it, which forces
a *single* shared scope for the admin order and attendee lists — finer granularity there
would mean enumerating variants, which is the problem the generation counter exists to
avoid.

**Relationship**: `event:{uuid}` and `events` are independent counters, not nested. A
ticket-type change bumps `event:{uuid}` only; an event's own publish/unpublish bumps both,
because the catalogue row and the event's own lists both change.

---

## 2. Generation Counter

| Field | Type | Notes |
|---|---|---|
| key | string | `gen:{scope}` |
| value | int64 | Monotonic, incremented by `INCR` |
| TTL | none | Never expires — losing it must not resurrect stale entries |

**Invariant**: the counter has no TTL and is never deleted except by an explicit
`FlushAll`. If it were evicted while derived entries survived, the generation would reset
to a value whose entries already exist and are stale. `allkeys-lru` can evict it in
principle, so generation keys are written with `maxmemory-policy` in mind: they are tiny,
constantly touched by every read, and therefore the last things LRU would choose. The
`FlushAll` path deletes counters and entries together, so the reset is consistent.

**Missing counter** is read as generation `0`, not as an error — a cold Redis is a cold
cache.

---

## 3. Cache Entry

One stored list response.

| Field | Type | Notes |
|---|---|---|
| key | string | `list:{family}:g{generation}:{fingerprint}` — see [contracts/cache-keys.md](./contracts/cache-keys.md) |
| value | JSON bytes | The serialized DTO slice, identical to the HTTP response body's `data` |
| TTL | duration | `CACHE_TTL`, default 10 minutes — a backstop only (FR-015) |

**Validation rules**:

- The value MUST deserialize into the same Go type it was serialized from, with
  `decimal.Decimal` and `*time.Time` fields byte-identical after the round-trip (R6).
- An **empty list is a valid cached value** and MUST be stored and served as such
  (spec edge case: an event with no packages must not re-query on every request).
  Implementation note: distinguish "key absent" from "key holds `[]`" — the Lua script
  returns `nil` only for absence.
- An entry is never mutated. A change produces a new key under a new generation.

**Lifecycle**: `written on miss → served on hit → orphaned by an INCR → reclaimed by TTL
or LRU`. There is no update and no explicit delete outside `FlushAll`.

---

## 4. Cacheable Surface (compile-time registry)

Not stored — a table in `pkg/cache` that closes the set of cacheable reads, as Principle
VII requires. Adding a row requires a constitution amendment.

| Field | Type | Notes |
|---|---|---|
| `Family` | string | Metric label and key segment; 9 values |
| `Scope` | Scope | Which generation counter the key reads |
| `Fingerprint` | func | Renders the read's parameters into a stable key segment |

The nine rows are specified in [contracts/cache-keys.md](./contracts/cache-keys.md).

---

## 5. Scope Set

Accumulated during a transaction, flushed after commit. In-memory only.

| Field | Type | Notes |
|---|---|---|
| `scopes` | `map[Scope]struct{}` | Deduplicated; order irrelevant |

**State transitions**:

```text
empty ──add(scope)──▶ pending ──InTx commits──▶ flushed (INCR per scope) ──▶ discarded
                          │
                          └──InTx rolls back──▶ discarded WITHOUT flushing   (FR-007)
```

**Invariant**: a scope set is never flushed from inside a transaction. The context marker
described in §6 makes an attempt fail loudly rather than silently succeed.

---

## 6. Transaction Marker

In-memory, context-scoped. The mechanism behind FR-023 and Principle VII's
transaction-boundary rule.

| Field | Type | Notes |
|---|---|---|
| marker | context value | Set by `db.InTx` before calling `fn`, cleared before the post-commit flush |

**Invariant**: `cache.Lists` methods return `ErrInTransaction` when the marker is present,
without issuing any Redis command. This is asserted by test, not by convention.

---

## 7. Distrust Window

In-memory, per-process. The FR-014 recovery path.

| Field | Type | Notes |
|---|---|---|
| `until` | `time.Time` | Zero when trusted |
| `cooldown` | duration | Default 30 s |

**State transitions**:

```text
trusted ──post-commit flush fails twice──▶ distrusted (reads bypass cache entirely)
distrusted ──cooldown elapses──▶ trusted (next read repopulates)
```

**Rationale**: while distrusted the process behaves exactly as it does with
`CACHE_ENABLED=false` — a known-good state — so recovery needs no operator action.

---

## 8. Cache Health (operator-facing, derived)

Not stored; computed on demand for `/healthz` and exported to Prometheus.

| Field | Source | Exposed as |
|---|---|---|
| reachable | `PING` with a short timeout | `/healthz` → `status: ok \| degraded` (FR-018) |
| hits / misses | counters, labelled by family | `cache_requests_total{family,result}` (FR-019) |
| refreshes | counter, labelled by scope kind | `cache_invalidations_total{scope}` |
| refresh failures | counter | `cache_invalidation_failures_total` |
| errors | counter | `cache_errors_total{op}` |
| distrusted | gauge | `cache_distrusted` (0/1) |

Labels are bounded: `family` has 9 values, `scope` has 3, `op` has 4. No event id, order
id, or filter value ever becomes a label (R8 deferred note).

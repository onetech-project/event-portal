# Phase 0 Research: Refresh-on-Write List Caching

**Feature**: 012-redis-list-cache | **Date**: 2026-08-10

Eight decisions were open after the spec. All are resolved; no `NEEDS CLARIFICATION`
remains in the plan's Technical Context.

---

## R1 — Invalidation mechanism: generation counters, not key deletion

**Decision**: Each cache scope owns an integer counter key in Redis. Cache keys embed the
counter's current value (`list:admin_orders:g47:status=PAID`). Invalidation is
`INCR gen:{scope}` — one command, regardless of how many entries derive from the scope.
Orphaned entries are never deleted explicitly; they become unreachable and are reclaimed
by TTL and `allkeys-lru`.

**Rationale**: The admin order and attendee lists are filtered by status, event, and
order id. The number of live filter combinations grows with the event count, and FR-010
requires that *every* variant containing a changed record be invalidated. Explicit
deletion would need either a `SCAN`-and-delete (O(keyspace) per write, and Redis
documents `KEYS` as unsuitable for production) or a per-scope Redis SET tracking every
derived key (a second structure to keep consistent, with its own failure modes). A
counter costs one `INCR`, is atomic, cannot drift, and satisfies FR-009 scoping and
FR-010 fan-out with the same primitive. It also keeps FR-023 cheap: a single command
after commit rather than a variable-length delete batch.

**Alternatives considered**:
- *`SCAN` + `DEL` by prefix* — rejected: O(keyspace) per write, and every booking is a write.
- *Per-scope SET of derived keys* — rejected: the SET itself can drift from reality, and
  cleaning it is the same problem one level down.
- *Explicit `DEL` of an enumerated key list* — viable for the three public surfaces whose
  keys are enumerable, but not for the admin filter variants. Mixing two mechanisms was
  rejected as more complexity than the counter alone.

**Cost**: the read path needs the generation before it can build the data key — see R2.

---

## R2 — Read path round-trips: one, via a Lua script

**Decision**: Reads use a small `EVALSHA` script that fetches the generation and the
derived value in a single round-trip:

```lua
local g = redis.call('GET', KEYS[1])
if not g then return nil end
return redis.call('GET', KEYS[2] .. g)
```

A missing generation key is treated as generation `0` and initialized lazily on the write
path, so a cold Redis behaves as a cold cache, not an error.

**Rationale**: The naive form is two dependent round-trips (`GET gen`, then `GET data`).
Both would still clear SC-001's 30 ms budget on a local network, but the script makes the
cached path a single RTT, which keeps the win over Postgres unambiguous and leaves
headroom if Redis moves off-host later. `EVALSHA` with an `EVAL` fallback on `NOSCRIPT` is
the standard, well-supported pattern in `go-redis`.

**Alternatives considered**:
- *Two plain round-trips* — acceptable fallback, kept as the reference implementation the
  script must match behaviorally; retained in tests.
- *Caching the generation in-process for a short window* — rejected outright: it breaks
  read-your-write across instances (FR-011) and re-introduces exactly the staleness this
  feature exists to eliminate.

---

## R3 — Cross-instance propagation: none needed

**Decision**: No pub/sub, no invalidation broadcast. The generation counter lives in the
shared Redis, so an `INCR` from any instance is immediately visible to all of them.

**Rationale**: This falls out of R1 for free, and it matters twice over — Constitution
Principle VII explicitly forbids Redis pub/sub, so a broadcast-based design would have
been non-compliant as well as unnecessary. FR-011 is satisfied by the shared counter
alone.

---

## R4 — Redis client library: `github.com/redis/go-redis/v9`

**Decision**: `go-redis/v9`.

**Rationale**: The maintained successor to `go-redis/v8`, the de facto standard Go client,
with first-class context support (needed for the request-scoped timeouts the fail-open
path depends on), built-in connection pooling, and `EVALSHA` handling with automatic
`NOSCRIPT` fallback. It also has an OpenTelemetry instrumentation hook that matches the
`otelpgx`/`otelecho` instrumentation already in this codebase.

**Alternatives considered**:
- *`rueidis`* — faster under heavy pipelining, but the performance headroom is irrelevant
  at this scale and it is a less familiar dependency for a codebase whose stated goal is
  validation within a short window.
- *`redigo`* — no generics, weaker context support, effectively legacy.

---

## R5 — Thundering-herd control: `singleflight`, in-process

**Decision**: `golang.org/x/sync/singleflight`, keyed by the fully-resolved cache key,
wrapped around the miss path inside `cache.Through`.

**Rationale**: SC-006 caps a 500-request cold burst on one list at 5 Postgres queries.
In-process collapsing reduces that to one query per instance per key, so the ceiling is
the instance count — well inside 5 at the deployment size in `docker-compose.yml`. The
dependency is already in `go.mod` as an indirect at v0.22.0; this promotes it to direct.

**Alternatives considered**:
- *A distributed lock in Redis* — rejected twice over: Principle VII forbids Redis locks,
  and it would add a network round-trip to the miss path to save at most a handful of
  queries.
- *Probabilistic early expiry (XFetch)* — solves a problem this design does not have,
  since entries are invalidated by writes rather than expiring under load.

---

## R6 — Serialization: `encoding/json` over the existing DTOs

**Decision**: `encoding/json` on the exact DTO slices the services already return
(`[]EventSummary`, `[]TicketTypeSummary`, `[]PackageSummaryDTO`, `[]OrderSummary`,
`[]AttendeeSummary`, and the admin views).

**Rationale**: Principle VII requires cached values to be domain DTOs and forbids
serializing `sqlc` structs. These types already carry correct JSON tags because they are
the wire contract, which makes FR-005 (byte-identical responses) close to automatic — the
cached path and the uncached path serialize the same value with the same tags. Payload
sizes here are kilobytes; a faster codec would optimize a non-bottleneck.

One trap to handle explicitly: `decimal.Decimal` (prices) and `*time.Time` (nullable
timestamps) must survive the round-trip exactly. A round-trip equality test over every
cached DTO type is a required task, not an optional one — a silently lossy price is worse
than no cache.

**Alternatives considered**:
- *msgpack / protobuf* — rejected: a second schema to maintain for a payload measured in
  kilobytes.
- *Caching pre-rendered HTTP response bytes* — tempting for FR-005, but it would bypass
  the DTO layer and cache a wire artifact rather than a domain value; rejected as a
  Principle III/VII smell.

---

## R7 — Enforcing "no cache call inside a transaction"

**Decision**: `db.InTx` stamps a marker on the context it passes to `fn`. Every
`cache.Lists` method checks `db.InTransaction(ctx)` first and returns `ErrInTransaction`
without issuing a command. A unit test asserts that a cache call from inside `InTx` fails.

**Rationale**: Principle VII's transaction-boundary rule and FR-023 exist because the
quota-deducting `UPDATE` holds a row lock until commit — a Redis round-trip inside that
window would serialize every concurrent buyer of the same ticket type behind network
latency, which is the exact failure Principle IV already bans gateway calls to prevent.
A rule enforced only by code review erodes on the first hurried change. Making it a
runtime invariant costs about fifteen lines and one test.

**Alternatives considered**:
- *Documentation and review only* — rejected: this is the single most damaging mistake
  available in this feature, and it is invisible in tests that do not measure lock hold time.
- *A static analyzer / custom vet check* — more robust in principle, but disproportionate
  tooling for a single rule in a codebase with one transaction helper.

---

## R8 — Recovery when a post-commit flush fails (FR-014)

**Decision**: Retry the flush once after a short backoff. If it still fails, the process
enters a **distrust window** (default 30 s) during which `cache.Through` bypasses the
cache entirely and reads Postgres. Distrust is per-process, in-memory, and self-clearing.

**Rationale**: This is the one failure mode that breaks the feature's core promise — the
write committed, so the cached entry is now provably wrong, and the reader has no way to
know. TTL alone would leave it wrong for up to `CACHE_TTL`. Bypassing reads is the only
response that is correct without operator action (FR-014's requirement) and it degrades to
exactly the pre-cache behavior, which is known-good.

Note the common case resolves itself: if Redis is unreachable, the *read* path fails open
to Postgres for the same reason the flush failed, so reads are already correct. Distrust
covers the narrower case where Redis is reachable but the flush errored.

**Alternatives considered**:
- *A durable retry queue* — a queue in Redis is forbidden by Principle VII, and a
  Postgres-backed outbox is a substantially larger design than the problem warrants.
- *Shortening TTL to bound the damage* — rejected: it converts a correctness mechanism
  into a time-based one, which Principle VII explicitly forbids ("TTL expiry MUST NOT be
  the mechanism by which the system becomes correct").
- *Failing the write* — rejected: FR-013 requires writes to commit regardless of cache state.

---

## Deferred, deliberately

- **Fee lists** (`GET /admin/fees`) are not cached. Principle VII's enumeration does not
  include fees, and they are read rarely.
- **Cache warming on startup** — not implemented. Cold start is one Postgres query per
  list, collapsed by singleflight (R5); warming would add a startup dependency on Redis
  that Principle VII's "must start with Redis absent" rule argues against.
- **Per-key metrics cardinality** — metrics are labelled by *family* (9 values), never by
  the full key, which would put event ids and filter values into Prometheus label
  cardinality.

# Implementation Plan: Refresh-on-Write List Caching

**Branch**: `012-redis-list-cache` | **Date**: 2026-08-10 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/012-redis-list-cache/spec.md`

## Summary

Put a Redis read cache in front of the nine list reads that dominate database load —
the guest event catalogue, per-event ticket-type and package lists, and the admin
event / ticket-type / package / order / attendee lists — and keep them correct by
invalidating on commit rather than by expiry.

The technical core is a **generation-counter** scheme. Each cache scope (one event, or
the global orders scope) owns a monotonically increasing counter in Redis. Cache keys
embed the counter's current value, so a write invalidates every derived entry —
including every admin filter variant — with a single `INCR`. Old entries become
unreachable immediately and are reclaimed by TTL and `allkeys-lru`. This is what makes
FR-010 (invalidate every filter variant) affordable and FR-023 (one cheap command,
after commit, outside the transaction) structurally easy.

Invalidation hangs off the existing `db.InTx` chokepoint in `pkg/db`, which every
multi-statement write in the codebase already funnels through. A scope set accumulated
during the transaction is flushed only after `Commit` returns, so a rollback can never
invalidate (FR-007) and no Redis command is ever issued while the quota row lock is held
(FR-023, Constitution Principle VII). To stop that from being a convention that erodes,
`db.InTx` marks its context and the cache client refuses — loudly and testably — to run
any command on a context carrying that mark.

## Technical Context

**Language/Version**: Go 1.26.5 (backend); no frontend change

**Primary Dependencies**: Echo v4, pgx/v5, sqlc (existing). New: `github.com/redis/go-redis/v9`;
`golang.org/x/sync/singleflight` (already an indirect dependency at v0.22.0, promoted to direct)

**Storage**: PostgreSQL (unchanged, sole source of truth) + Redis 7 single node as a
disposable read cache

**Testing**: `go test` with `testify`; existing `internal/testsupport` harness against a
live Postgres; a `miniredis`-backed fake plus one Redis-integration test tier

**Target Platform**: Linux container, Docker Compose

**Project Type**: Web service (Go modular monolith) + Next.js frontend — backend only here

**Performance Goals**: cached list p99 < 30 ms at the service boundary (SC-001); ≥ 90%
of list requests served without a Postgres query (SC-003); ≤ 5 Postgres queries for a
500-request cold burst on one list (SC-006)

**Constraints**: zero Redis commands between `BEGIN` and `COMMIT` of any order-writing
transaction; fail open on Redis unavailability; byte-identical responses to today's;
full test suite green with caching both on and off

**Scale/Scope**: 9 cached read paths, ~14 write paths that must invalidate, 3 new
`pkg/` files, 1 new admin endpoint, 1 new compose service, 3 new env vars

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

Checked against Constitution v3.1.0.

| Principle | Gate | Verdict |
|---|---|---|
| I. Modular Monolith | Cache is shared non-domain infrastructure → belongs in `pkg/`, not a new domain | **PASS** — `pkg/cache/`, no new `internal/` domain |
| II. Domain Isolation | No domain may import another's repository, and no cross-domain join for writes | **PASS** — `order` already has `BookRequest.EventID`, so it names the event scope directly; `payment` resolves ticket types → event through a new consumer-declared `EventScopeLookup` seam satisfied by `event.Service`, mirroring the existing `QuotaRestorer`. No repository import, no join added |
| III. DTO Isolation | Cached values must be domain DTOs, never `sqlc` structs | **PASS** — the cache serializes exactly the `[]EventSummary`, `[]TicketTypeSummary`, `[]OrderSummary`, … values the services already return |
| IV. Transactional Integrity | No external network call inside an order-writing transaction | **PASS** — enforced by the context marker, not by convention; see Phase 1 §3 |
| V. Payment Gateway Abstraction | Untouched | **PASS** — the `Gateway` interface and every gateway call site are unchanged |
| VI. Guest-First MVP Scope | Redis permitted only as the Principle VII read cache | **PASS** — no queues, sessions, locks, pub/sub, or Cluster |
| VII. Cache as Disposable Read Accelerator | All eight sub-rules | **PASS** — see the rule-by-rule table below |

### Principle VII, rule by rule

| Rule | How this plan satisfies it | Where |
|---|---|---|
| Postgres sole source of truth | Cache holds only serialized copies of read results; `FLUSHALL` costs latency only | Phase 1 §1 |
| Closed list of cacheable surfaces | Exactly 9 read paths registered in one table; nothing else can be cached without editing it | [contracts/cache-keys.md](./contracts/cache-keys.md) |
| Invalidation triggered by commit | Scope set flushed in `InTx`'s success path only | Phase 1 §3 |
| Never authoritative for inventory | `CheckAndDeductQuota` / `RestoreQuota` are untouched and still run the row-locked `UPDATE` | Phase 1 §3 |
| No cache call inside a transaction | Context marker + client-side refusal + a test that asserts it | Phase 1 §3 |
| Fail open | Every cache error path returns "miss"; `/healthz` reports `degraded` | Phase 1 §4 |
| Swappable behind an interface | Domains depend on the `cache.Lists` interface, never on `go-redis` | Phase 1 §2 |
| Killable by configuration | `CACHE_ENABLED=false` installs a no-op implementation of the same interface | Phase 1 §5 |

**Post-Phase-1 re-check**: PASS, unchanged. No new violation surfaced during design, and
the Complexity Tracking table stays empty.

## Project Structure

### Documentation (this feature)

```text
specs/012-redis-list-cache/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output — 8 resolved decisions
├── data-model.md        # Phase 1 output — cache entities, key grammar, scope map
├── quickstart.md        # Phase 1 output — how to run and validate
├── contracts/
│   ├── cache-keys.md            # Key grammar, the 9 cacheable surfaces, TTLs
│   ├── invalidation-map.md      # Every write path → the scopes it bumps
│   └── admin-cache-refresh.md   # POST /api/v1/admin/cache/refresh
├── checklists/
│   └── requirements.md
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
backend/
├── cmd/api/
│   └── main.go                     # MODIFIED: build cache client, inject into services,
│                                   #   /healthz degraded branch, admin refresh route
├── pkg/
│   ├── cache/                      # NEW — shared, non-domain infrastructure (Principle I)
│   │   ├── cache.go                # Lists interface, Scope/ScopeSet, NoOp implementation
│   │   ├── redis.go                # go-redis implementation: generation keys, Lua GET,
│   │   │                           #   singleflight, fail-open error handling
│   │   ├── metrics.go              # hit/miss/refresh/refresh_failure counters per family
│   │   └── *_test.go               # miniredis unit tests + fail-open + in-tx refusal
│   ├── db/
│   │   └── db.go                   # MODIFIED: mark the tx context; run the after-commit
│   │                               #   flush; expose InTransaction(ctx)
│   └── config/
│       └── config.go               # MODIFIED: RedisURL, CacheEnabled, CacheTTL
└── internal/
    ├── event/
    │   ├── service.go              # MODIFIED: cache the 3 public list reads
    │   ├── admin_service.go        # MODIFIED: cache 2 admin list reads; invalidate on
    │   │                           #   event and ticket-type writes
    │   └── package_service.go      # MODIFIED: cache admin package list; invalidate on writes
    ├── order/
    │   ├── admin_service.go        # MODIFIED: cache order + attendee list reads
    │   ├── service.go              # MODIFIED: invalidate event + orders scopes after Book
    │   │                           #   and CheckoutOrder commit
    │   └── admin_handler.go        # MODIFIED: register the cache-refresh route
    └── payment/
        └── service.go              # MODIFIED: new EventScopeLookup seam; invalidate
                                    #   after applyOutcome / ExpireDueOrders commit
docker-compose.yml                  # MODIFIED: redis service, maxmemory + allkeys-lru
.env.example                        # MODIFIED: REDIS_URL, CACHE_ENABLED, CACHE_TTL
ARCHITECTURE.md                     # MODIFIED: cache component + refresh-on-write flow
```

**Structure Decision**: Existing modular-monolith layout, unchanged. The cache is shared
non-domain infrastructure and therefore lives in `pkg/cache/` per Principle I, exactly as
`pkg/db` and `pkg/observability` do. No new domain is created; no domain gains an import
of another domain. `cmd/api/main.go` remains the only place where components are wired to
one another.

## Phase 1 Design Summary

### 1. What is cached, and under what key

Nine read paths, each with a family name, a scope, and a parameter fingerprint:

```text
list:{family}:g{generation}:{fingerprint}
```

`{generation}` is read from the scope's counter key at request time. Because the counter
value is part of the key, bumping the counter orphans every entry derived from it at
once. Full grammar, the nine families, and their scopes: [contracts/cache-keys.md](./contracts/cache-keys.md).

The two scopes are `event:{uuid}` (that event's catalogue entry, ticket types, packages)
and `orders` (all admin order and attendee list variants). The public event catalogue
reads a third, `events` global scope. Nothing finer is needed and nothing coarser is
acceptable: a change to one event must not orphan another's entries (FR-009).

### 2. How a domain reaches the cache

```go
// pkg/cache
type Lists interface {
    Get(ctx context.Context, k Key, dst any) (bool, error)
    Set(ctx context.Context, k Key, v any) error
    Invalidate(ctx context.Context, scopes ScopeSet) error
    FlushAll(ctx context.Context) error
    Ping(ctx context.Context) error
}
```

Services receive a `cache.Lists`, never a `*redis.Client` (Principle VII). Read paths take
a uniform shape, so the diff on each of the nine methods is mechanical:

```go
func (s *Service) ListPublishedEvents(ctx context.Context) ([]EventSummary, error) {
    return cache.Through(ctx, s.cache, cache.EventsCatalogue(), func() ([]EventSummary, error) {
        return s.repo.ListPublishedEvents(ctx)
    })
}
```

`cache.Through` is a generic helper that owns miss handling, JSON round-tripping,
singleflight collapsing (FR-017, SC-006), metrics, and the fail-open rule — so no domain
repeats that logic and no domain can get it subtly wrong.

### 3. How invalidation is bound to commit

`db.InTx` is already the single chokepoint for every multi-statement write. It gains two
responsibilities:

1. **Mark the context** for the duration of the transaction. `cache.Lists`
   implementations check `db.InTransaction(ctx)` and return `ErrInTransaction` without
   issuing a command. This turns Principle VII's transaction-boundary rule and FR-023
   from a review checklist item into a runtime invariant with a test behind it.
2. **Flush after commit**. Scopes are accumulated into a `cache.ScopeSet` carried on the
   context; `InTx` invalidates them only on the success path, after `Commit` returns and
   after the mark is cleared. A rollback discards the set (FR-007).

Single-statement writes that do not go through `InTx` invalidate explicitly at the call
site, immediately after the write returns.

Every write path and the scopes it bumps: [contracts/invalidation-map.md](./contracts/invalidation-map.md).

One supporting change is needed. `orders` has no `event_id` column — an order reaches its
event only through `order_items → ticket_types` — so the payment domain, which holds only
`[]QuotaHold{TicketTypeID, Quantity}`, cannot name the event scope to bump after a quota
restore. It gets it through a new consumer-declared interface satisfied by `event.Service`
(`EventIDsForTicketTypes`, the mirror of the existing `TicketTypeIDsForEvent`), called
after commit. No schema change, no cross-domain join, no repository import — the same
shape as the existing `QuotaRestorer` and `EventProvider` seams.

### 4. Failure behavior

| Failure | Behavior |
|---|---|
| Redis unreachable on read | `Through` treats the error as a miss, reads Postgres, returns normally; counted as `cache_errors_total` |
| Redis unreachable on write-flush | The DB transaction has already committed and is never rolled back; the flush error is logged and retried once with a short backoff |
| Flush still failing after retry | The process enters **distrust** for a cooldown (default 30 s): reads bypass the cache entirely and go to Postgres. This is what makes FR-014 true without operator action — the system cannot keep serving pre-write content, because it stops serving from cache at all |
| Redis absent at startup | `main.go` logs a warning and continues; the API starts and serves (Principle VII) |
| Entry missed by every mechanism | TTL backstop (default 10 min) bounds it (FR-015) |

`/healthz` becomes: Postgres down → `503` as today; Postgres up and Redis down → `200`
with `{"status":"degraded","cache":"unavailable"}` (FR-018).

### 5. Configuration

| Variable | Default | Effect |
|---|---|---|
| `REDIS_URL` | `redis://localhost:6379/0` | Connection string; empty disables the cache like `CACHE_ENABLED=false` |
| `CACHE_ENABLED` | `true` | `false` wires `cache.NoOp{}` — every read goes to Postgres, every invalidate is a no-op (FR-021) |
| `CACHE_TTL` | `10m` | Backstop expiry on every entry (FR-015) |

`CACHE_ENABLED=false` must reproduce today's behavior exactly. The CI matrix runs the
full suite in both modes (SC-008).

### 6. Redis deployment

A single `redis:7-alpine` service in `docker-compose.yml` with
`--maxmemory 256mb --maxmemory-policy allkeys-lru` (FR-016) and **no persistence** — the
cache is disposable by definition, so AOF/RDB would buy nothing and cost restart time.
The `api` service does **not** gain a `depends_on` condition that could block startup.

## Complexity Tracking

> Fill ONLY if Constitution Check has violations that must be justified.

No violations. Table intentionally empty.

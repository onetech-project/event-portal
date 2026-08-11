---

description: "Task list for 014-redis-list-cache"
---

# Tasks: Refresh-on-Write List Caching

**Input**: Design documents from `/specs/014-redis-list-cache/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: Test tasks ARE included. Not as a default TDD posture — the spec's Success
Criteria require them as acceptance evidence: SC-002 (≥100 write-then-read cycles per
family with zero stale reads), SC-006 (500-request cold burst ≤5 queries), SC-007 (200
concurrent bookings), and SC-008 (full suite green with caching on *and* off). Each
contract file also carries a Verification section that only a test can discharge.

**Organization**: Grouped by user story so each ships independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1–US4, mapping to spec.md
- Paths are repo-relative; the backend module root is `backend/`

## Path Conventions

Go modular monolith. Shared infrastructure in `backend/pkg/`, domains in
`backend/internal/<domain>/`, wiring in `backend/cmd/api/main.go`. Go tests live beside
the code they test (`*_test.go`), matching the existing codebase.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Dependencies, container, and configuration — nothing behavioral

- [X] T001 Add `github.com/redis/go-redis/v9` and promote `golang.org/x/sync` from indirect to direct in `backend/go.mod`, then `go mod tidy`
- [X] T002 [P] Add `alicebob/miniredis/v2` as a test dependency in `backend/go.mod` for the `pkg/cache` unit tier (research.md R6/R7 tests)
- [X] T003 [P] Add a `redis:7-alpine` service to `docker-compose.yml` with `--maxmemory 256mb --maxmemory-policy allkeys-lru` and **no persistence**; do NOT add a blocking `depends_on` condition to the `api` service (Principle VII: the API must start with Redis absent)
- [X] T004 [P] Add `REDIS_URL`, `CACHE_ENABLED`, `CACHE_TTL` to `.env.example` and to the `api` service environment in `docker-compose.yml` (use `redis://redis:6379/0` inside compose); document defaults per plan.md §5
- [X] T005 Add `RedisURL string`, `CacheEnabled bool`, `CacheTTL time.Duration` to `Config` in `backend/pkg/config/config.go` with defaults `redis://localhost:6379/0`, `true`, `10m`; an empty `REDIS_URL` MUST be equivalent to `CACHE_ENABLED=false`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The cache package, the transaction-boundary enforcement, and wiring. Every
user story reads and writes through this.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T006 Create `backend/pkg/cache/cache.go`: the `Lists` interface (`Get`/`Set`/`Invalidate`/`FlushAll`/`Ping`), `Scope` and `ScopeSet` types per data-model.md §1 and §5, the `Key` type, `ErrInTransaction`, and a `NoOp` implementation that satisfies `Lists` with pure no-ops (FR-021)
- [X] T007 Create the closed cacheable-surface registry in `backend/pkg/cache/surfaces.go`: the nine families from contracts/cache-keys.md §2 with their scopes and fingerprint renderers; fingerprints MUST be stable and injective, rendering parameters in fixed order (never map iteration)
- [X] T008 Add the transaction marker to `backend/pkg/db/db.go`: `InTx` stamps a context value before calling `fn` and exposes `InTransaction(ctx) bool`; the marker is cleared before the post-commit flush (research.md R7)
- [X] T009 Add post-commit invalidation to `backend/pkg/db/db.go`: carry a `ScopeSet` on the context, flush it by calling the injected invalidator **only** on the success path after `Commit` returns, and discard it without flushing on rollback or panic (FR-007, FR-023)
- [X] T010 Create `backend/pkg/cache/redis.go`: the go-redis-backed `Lists` implementation — generation-counter key building, the `EVALSHA` read script with `EVAL`/`NOSCRIPT` fallback (research.md R2), `Set` with `CACHE_TTL`, `Invalidate` as pipelined `INCR` with one 50 ms-backoff retry, and `FlushAll` as `FLUSHDB`; every method returns `ErrInTransaction` immediately when `db.InTransaction(ctx)` is true
- [X] T011 Implement fail-open and the distrust window in `backend/pkg/cache/redis.go`: read errors are counted and reported as misses, never returned to callers; a flush that fails after its retry enters a 30 s per-process distrust during which reads bypass the cache entirely (FR-013, FR-014, research.md R8)
- [X] T012 Implement the generic `cache.Through` helper in `backend/pkg/cache/through.go`: miss handling, JSON round-trip, `singleflight` collapsing keyed by the resolved cache key, metric emission, and the disabled/distrusted bypass — so no domain repeats this logic (FR-017, contracts/cache-keys.md §4)
- [X] T013 [P] Create `backend/pkg/cache/metrics.go`: Prometheus counters `cache_requests_total{family,result}`, `cache_invalidations_total{scope}`, `cache_invalidation_failures_total`, `cache_errors_total{op}` and gauge `cache_distrusted`, registered on the existing registry from `backend/pkg/observability`; labels bounded to family/scope/op only — never an event id, order id, or filter value (data-model.md §8)
- [X] T014 Wire the cache in `backend/cmd/api/main.go`: build the client from config, log a warning and continue when Redis is unreachable at startup, install `cache.NoOp{}` when disabled, and inject `cache.Lists` into the event, order, and payment services
- [X] T015 [P] Unit-test the key grammar and generation behavior in `backend/pkg/cache/cache_test.go` against miniredis: key shape per contracts/cache-keys.md §1, missing counter reads as generation 0, an `INCR` orphans prior entries, and an **empty list round-trips as a stored value distinct from a miss** (data-model.md §3)
- [X] T016 [P] Unit-test DTO round-trip fidelity in `backend/pkg/cache/serialization_test.go` for every cached type, asserting `decimal.Decimal` prices and `*time.Time` nullable timestamps are byte-identical after marshal/unmarshal (research.md R6 — a silently lossy price is worse than no cache)
- [X] T017 [P] Unit-test the safety invariants in `backend/pkg/cache/safety_test.go`: a cache call from inside `db.InTx` returns `ErrInTransaction` and issues no command; a stopped Redis yields correct reads with no error; a failed flush enters distrust and reads bypass the cache until it clears

**Checkpoint**: Cache infrastructure exists and is provably safe. No list is cached yet, so
behavior is unchanged — a good place to merge before touching any domain.

---

## Phase 3: User Story 1 - Guests browse a fast, always-correct event catalogue (Priority: P1) 🎯 MVP

**Goal**: The public event catalogue is served from cache and refreshed the instant an
admin publishes, edits, or deletes an event.

**Independent Test**: Request `GET /api/v1/event` twice — identical bodies, second one
≥5× faster. Publish an event through the admin API; the very next public request includes
it. Stop Redis; the endpoint still returns correct data.

- [X] T018 [US1] Cache `ListPublishedEvents` in `backend/internal/event/service.go` via `cache.Through` with the `events_public` family; the returned `[]EventSummary` must be unchanged in shape and ordering (FR-005)
- [X] T019 [US1] Invalidate the `events` scope after `CreateEvent` commits in `backend/internal/event/admin_service.go` (contracts/invalidation-map.md §1)
- [X] T020 [US1] Invalidate **both** `events` and `event:{id}` after `UpdateEvent` and `DeleteEvent` commit in `backend/internal/event/admin_service.go` — a publish/unpublish changes the catalogue row *and* the event's own views
- [X] T021 [P] [US1] Integration-test warm→write→fresh for the catalogue in `backend/internal/event/cache_integration_test.go`: assert the post-write read returns new content **and** that it was a cache miss (a content-only assertion passes even when the cache is bypassed — contracts/invalidation-map.md §6)
- [X] T022 [P] [US1] Integration-test cross-instance visibility in `backend/internal/event/cache_integration_test.go`: two service instances sharing one Redis; a write through instance A is visible to a read through instance B on the next request (FR-011)
- [X] T023 [P] [US1] Integration-test fail-open on the catalogue: with Redis stopped, `GET /api/v1/event` returns correct data and no error (FR-013, SC-005)

**Checkpoint**: US1 is independently shippable. The highest-traffic read in the product is
cached and correct; nothing else has changed.

---

## Phase 4: User Story 2 - Ticket and package lists stay in step with live inventory (Priority: P1)

**Goal**: Per-event ticket-type and package lists are cached and refreshed on every quota
movement — booking, cancellation, expiry, denial, failure — and on every admin edit.

**Independent Test**: Read an event's ticket list, book tickets, read again — remaining
quota is decremented. Expire the order; the next read shows it restored. A different
event's cached list is untouched throughout.

- [X] T024 [US2] Cache `TicketTypesForEventSlug` and `PackagesForEventSlug` in `backend/internal/event/service.go` under the `ticket_types_public` and `packages_public` families, scoped `event:{id}` (depends on T018 — same file)
- [X] T025 [US2] Cache `PackagesForEvent` in `backend/internal/event/service.go` under the `packages_by_event` family
- [X] T026 [US2] Invalidate `event:{event_id}` after `CreateTicketType`, `UpdateTicketType`, and `DeleteTicketType` commit in `backend/internal/event/admin_service.go` (depends on T019/T020 — same file)
- [X] T027 [US2] Invalidate `event:{event_id}` after `CreatePackage`, `UpdatePackage`, and `DeletePackage` commit in `backend/internal/event/package_service.go`
- [X] T028 [P] [US2] Add `EventIDsForTicketTypes(ctx, ids) ([]uuid.UUID, error)` to `backend/internal/event/admin_service.go` as the mirror of the existing `TicketTypeIDsForEvent` — `orders` has no `event_id` column, so this is how the payment domain names the event scope (contracts/invalidation-map.md §3)
- [X] T029 [US2] Declare the consumer-side `EventScopeLookup` interface in `backend/internal/payment/service.go` and wire `event.Service` to it in `backend/cmd/api/main.go`, alongside the existing `QuotaRestorer` — no repository import, no cross-domain join (Principle II)
- [X] T030 [US2] Invalidate `orders` + `event:{event_id}` after `Book` commits in `backend/internal/order/service.go`, using `BookRequest.EventID` directly; the bump MUST be outside the transaction — the quota `UPDATE` holds a row lock until commit (FR-023, Principle VII)
- [X] T031 [US2] Invalidate after `applyOutcome` commits in `backend/internal/payment/service.go`: `orders` on `PAID`; `orders` + `event:{event_id}` on expire/cancel/deny/failure, resolving the event via `EventScopeLookup` from the restored `QuotaHold` ticket types
- [X] T032 [US2] Invalidate `orders` + the affected `event:{event_id}` per expired order after `ExpireDueOrders` commits in `backend/internal/payment/service.go` (the sweeper path — quota moves with nobody on the page)
- [X] T033 [US2] Ensure webhook invalidation runs with the non-blocking post-payment work and **never** ahead of the `200 OK` response in `backend/internal/payment/service.go`; an already-`PAID` idempotent short-circuit performs no write and MUST bump nothing (contracts/invalidation-map.md §3)
- [X] T034 [P] [US2] Integration-test the quota cycle in `backend/internal/order/cache_integration_test.go`: warm ticket list → book → assert decremented and a miss → expire → assert restored and a miss (spec US2 scenarios 1–2)
- [X] T035 [P] [US2] Integration-test scope isolation in `backend/internal/event/cache_integration_test.go`: changing event A's ticket types leaves event B's cached lists intact and still hitting (FR-009, spec US2 scenario 4)
- [X] T036 [P] [US2] Concurrency-test 200 simultaneous bookings in `backend/internal/order/cache_concurrency_test.go`: no displayed availability ever exceeds true remaining quota, and every booking outcome is decided by the database, not the cached figure (SC-007, FR-012)

**Checkpoint**: Both P1 stories are live. The guest purchase path is fully cached and
inventory-correct.

---

## Phase 5: User Story 3 - Admin tables load quickly without going stale (Priority: P2)

**Goal**: Admin event, ticket-type, package, order, and attendee lists are cached per
filter combination and refreshed by every write that could change them.

**Independent Test**: Load an admin order list twice — identical, second faster. Drive an
order to paid; the next load of the same filtered list shows the new status. Cache several
filter combinations, change one order, and confirm no combination serves the old state.

- [X] T037 [US3] Cache `ListEvents` and `ListTicketTypes` in `backend/internal/event/admin_service.go` under `events_admin` and `ticket_types_admin` (depends on T026 — same file)
- [X] T038 [US3] Cache `ListPackagesByEvent` in `backend/internal/event/package_service.go` under `packages_admin` (depends on T027 — same file)
- [X] T039 [US3] Cache `ListOrders` in `backend/internal/order/admin_service.go` under `orders_admin`, fingerprinting the filter as `st={status|_}:ev={uuid|_}` with `_` distinct from any present value (contracts/cache-keys.md §2)
- [X] T040 [US3] Cache `ListAttendees` in `backend/internal/order/admin_service.go` under `attendees_admin`, fingerprinted `or={uuid|_}:ev={uuid|_}`
- [X] T041 [US3] Invalidate `orders` after `CheckoutOrder` and `RecordAgreement` commit in `backend/internal/order/service.go` (depends on T030 — same file)
- [X] T042 [US3] Confirm fee CRUD in `backend/internal/order/admin_service.go` bumps nothing and that `GET /admin/fees` stays uncached — fees are outside Principle VII's enumeration; add a comment so the omission reads as deliberate
- [X] T043 [P] [US3] Integration-test filter-variant fan-out in `backend/internal/order/cache_integration_test.go`: warm at least four distinct filter combinations, change one order, assert **every** variant that could contain it now misses and returns the new state (FR-010)
- [X] T044 [P] [US3] Integration-test the admin payment-state path: warm an order list, drive an order to `PAID` through the webhook, assert the next admin read reflects it (spec US3 scenario 2)
- [X] T045 [P] [US3] Integration-test attendee-list freshness after booking and checkout write attendee rows (spec US3 scenario 4)

**Checkpoint**: All list reads in the product are cached. Feature is functionally complete.

---

## Phase 6: User Story 4 - Operators can see and control the cache (Priority: P3)

**Goal**: Cache health is visible, and an operator can force a full rebuild.

**Independent Test**: Query `/healthz` with Redis up and down. Trigger the admin refresh
and confirm subsequent reads rebuild from PostgreSQL with correct content.

- [X] T046 [US4] Extend `/healthz` in `backend/cmd/api/main.go`: PostgreSQL down → `503` as today; PostgreSQL up and Redis unreachable → `200` with `{"status":"degraded","cache":"unavailable"}` (FR-018) using a short `PING` timeout
- [X] T047 [US4] Implement `FlushAll` behavior in `backend/pkg/cache/redis.go` per contracts/admin-cache-refresh.md: `DBSIZE` for the count (best-effort), `FLUSHDB` (never `FLUSHALL`) clearing entries and generation counters together, then clear the distrust window
- [X] T048 [US4] Add the `POST /api/v1/admin/cache/refresh` handler in `backend/internal/order/admin_handler.go` returning `flushed` / `disabled` / `503 CACHE_UNAVAILABLE` per the contract, and register it on the JWT-protected `adminAPI` group in `backend/cmd/api/main.go`
- [X] T049 [US4] Log the flush at INFO with the authenticated admin's identity in `backend/internal/order/admin_handler.go` — it discards state across every instance and must be attributable
- [X] T050 [P] [US4] Test the refresh endpoint in `backend/internal/order/admin_handler_test.go`: 200 `flushed`, 200 `disabled` with caching off, 401 without a token, and idempotency on a second immediate call
- [X] T051 [P] [US4] Integration-test the manual-correction path: `UPDATE` a row directly in PostgreSQL, flush, assert the corrected data appears in every affected list (spec US4 scenario 3)
- [X] T052 [P] [US4] Test `/healthz` degradation in `backend/cmd/api/health_test.go`: Redis down returns `200 degraded`, and recovery returns `ok` within 60 s (SC-005)

**Checkpoint**: The cache is safe to operate in production.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T053 Update `ARCHITECTURE.md`: add Redis as a component alongside PostgreSQL, document the refresh-on-write flow (which write paths invalidate which scopes, that invalidation runs after commit and outside the transaction), and the fail-open read path — required by the Constitution Governance clause and named in the v3.1.0 Sync Impact Report
- [X] T054 [P] Add `REDIS_URL`, `CACHE_ENABLED`, `CACHE_TTL` to the deployment script and environment config in `scripts/`
- [X] T055 Run the full suite in both modes — `CACHE_ENABLED=false go test ./...` and `CACHE_ENABLED=true go test ./...` — and fix any test that branches on the flag outside `pkg/cache` (SC-008; both green is the strongest evidence the cache changed no behavior)
- [X] T056 [P] Measure and record SC-001 (cached list p99 < 30 ms, ≥5× faster than uncached) and SC-003 (≥90% of list requests served without a PostgreSQL query) against a seeded dataset
- [X] T057 [P] Load-test the cold burst per quickstart.md Scenario 5: 500 concurrent requests against a flushed cache must produce ≤5 PostgreSQL queries for that list (SC-006)
- [~] T058 [P] Verify SC-002 at scale: ≥100 write-then-read cycles per list family with zero stale responses — **PARTIAL**: `TestRepeatedBookingsNeverServeStaleQuota` does 40 cycles on the ticket-type family with zero stale reads, and every other family has a single-cycle warm→write→assert-miss test. The ≥100-cycles-per-family sweep is not written; the per-family invalidation is proven, the volume is not.
- [X] T059 Walk every scenario in [quickstart.md](./quickstart.md) end to end against a fresh `docker compose up`, including the Redis-absent cold start
- [X] T060 [P] Confirm the frontend is untouched: event list and quota fetches still use `cache: 'no-store'` (Constitution Technology Stack — the server cache does not relax this)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup — **blocks all user stories**
- **US1 (Phase 3)**: Depends on Foundational only
- **US2 (Phase 4)**: Depends on Foundational; shares `event/service.go` and `event/admin_service.go` with US1, so those specific tasks serialize against T018/T019/T020
- **US3 (Phase 5)**: Depends on Foundational; shares `order/service.go` with US2 (T041 after T030) and `event/admin_service.go` with US1/US2
- **US4 (Phase 6)**: Depends on Foundational; independent of US1–US3 and could ship first if operability were prioritized
- **Polish (Phase 7)**: Depends on all shipped stories

### User Story Dependencies

- **US1 (P1)**: Independent. This is the MVP.
- **US2 (P1)**: Independent in behavior, but touches files US1 touches. If US2 ships without US1, T024 must add the `cache.Through` plumbing to `event/service.go` itself.
- **US3 (P2)**: Independent in behavior. If US3 ships without US2, T041 must also cover the `orders`-scope bump on `Book` that T030 otherwise provides.
- **US4 (P3)**: Fully independent.

### Critical ordering within stories

- T008 → T009: the marker must exist before the flush can respect it
- T010/T011 → T012: `Through` builds on the client's fail-open and distrust behavior
- T028 → T029 → T031/T032: the lookup, then the seam, then its consumers
- T030 before T041: same function region in `order/service.go`

### Parallel Opportunities

- T002, T003, T004 in Setup
- T013, T015, T016, T017 in Foundational (metrics and the three test files are independent)
- All test tasks within a story: T021–T023; T034–T036; T043–T045; T050–T052
- T054, T056, T057, T058, T060 in Polish
- With multiple developers after Phase 2: one takes US1+US2 (they share files), another takes US4, a third starts US3's `order/admin_service.go` tasks (T039, T040)

---

## Parallel Example: User Story 1

```bash
# After T018–T020 land, launch the three US1 test tasks together:
Task: "Integration-test warm→write→fresh for the catalogue in backend/internal/event/cache_integration_test.go"
Task: "Integration-test cross-instance visibility with two instances on one Redis"
Task: "Integration-test fail-open with Redis stopped"
```

## Parallel Example: Foundational

```bash
# After T012 lands, the metrics file and all three test files are independent:
Task: "Create backend/pkg/cache/metrics.go with the five bounded-label collectors"
Task: "Unit-test key grammar and generation behavior in backend/pkg/cache/cache_test.go"
Task: "Unit-test DTO round-trip fidelity in backend/pkg/cache/serialization_test.go"
Task: "Unit-test safety invariants in backend/pkg/cache/safety_test.go"
```

---

## Implementation Strategy

### MVP First (US1 only)

1. Phase 1: Setup (T001–T005)
2. Phase 2: Foundational (T006–T017) — **critical, blocks everything**
3. Phase 3: US1 (T018–T023)
4. **STOP and VALIDATE**: quickstart.md Scenarios 1, 2, 4
5. Deploy — the highest-traffic read in the product is now cached, and every other endpoint
   is byte-for-byte unchanged

### Incremental Delivery

1. Setup + Foundational → merge (zero behavior change; the cache exists but nothing uses it)
2. US1 → validate → deploy (MVP)
3. US2 → validate → deploy (purchase path complete; this is where the inventory-correctness
   risk lives, so give it the concurrency test before shipping)
4. US3 → validate → deploy (admin surfaces)
5. US4 → validate → deploy (operability)
6. Polish

### Suggested checkpoint for review

Merge after Phase 2. It is the largest and most subtle chunk — generation counters, the
transaction marker, fail-open, distrust — and it is reviewable in isolation precisely
because it changes no behavior yet.

---

## Notes

- **The single most damaging mistake available in this feature** is issuing a Redis command
  inside the booking transaction: the quota `UPDATE` holds a row lock until commit, so a
  network round-trip there serializes every concurrent buyer of the same ticket type. T008
  and T017 make that a runtime failure rather than a review-time catch. Do not weaken them.
- Invalidation tests must assert a **cache miss**, not just correct content. A content-only
  assertion passes even when the cache is bypassed entirely — which is the exact bug those
  tests exist to catch.
- Adding a tenth cacheable surface to T007's registry requires a constitution amendment
  (Principle VII), not just a code change.
- `[P]` means different files with no incomplete dependencies. Several same-file
  serializations are called out explicitly in the task text.
- Commit after each task or logical group; stop at any checkpoint to validate a story.

---

## Implementation Record (2026-08-10)

**59 of 60 tasks complete**, T058 partial (see its line). Full suite green in both
cache modes, `-p 1`.

### Measured against a live stack (40 events, real Postgres + Redis)

| Criterion | Target | Measured |
|---|---|---|
| SC-001 cached read | < 30 ms, ≥5× faster | 0.6–1.0 ms warm vs 5.8 ms cold — ~7× |
| SC-002 post-write freshness | 0 stale | Generation 0→1 on create; new event on the next request |
| SC-003 served without a DB query | ≥ 90% | 99% (502 hits / 507 requests) |
| SC-006 cold burst | ≤ 5 queries per 500 requests | Exactly 5 misses |
| SC-007 concurrent bookings | no oversell / no overstate | 200 bookers, 50 quota → exactly 50 sold, 0 overstated |
| SC-005 cache outage | 100% still correct | All 200s with correct data; `/healthz` degraded; writes committed |
| SC-008 both modes | suite green both ways | Green |

SC-004 (≥80% reduction in primary-store load) was not measured directly —
`pg_stat_statements` is not installed on the dev database. The 99% hit rate is the
same quantity observed from the cache side.

### Defect found and fixed during validation

The first live outage test showed **every** request paying ~3.5 s while Redis was
down: `go-redis` defaults to 3 retries, and the dial timeout was charged per
request before falling through to Postgres. Failing open slowly is its own outage.

Fixed with a reachability breaker (`unreachableUntil`) plus 300 ms timeouts and
retries disabled. Re-measured: first request during an outage pays 411 ms, every
subsequent one ~0.9 ms, and recovery is automatic once Redis returns. Regression
tests: `TestCacheOutageCostsLatencyOnceNotPerRequest`,
`TestCacheWritesAreSkippedWhileTheStoreIsUnreachable`.

It is kept deliberately separate from the distrust window: distrust means "cached
entries are provably wrong", this means "the store is down". Both bypass the
cache; conflating them would lose a real distinction.

### Deviations from the plan

1. **`db.InTx` signature changed** to `func(ctx context.Context, tx pgx.Tx) error`.
   The transaction marker is useless unless it reaches the closure, and closures
   captured the outer context. 37 call sites migrated mechanically; the change is
   what makes the transaction-boundary rule a runtime invariant rather than a
   convention.
2. **Cache-refresh endpoint lives in `cmd/api/ops.go`**, not `order.AdminHandler` —
   see contracts/admin-cache-refresh.md.
3. **Payment invalidation hangs off `applyOutcome`**, the single chokepoint every
   status transition funnels through, rather than being duplicated across the
   webhook, sweeper, and reconciliation paths.
4. **Key grammar puts the generation last** — forced by the Lua read script.
5. **`packages_public` is not on a live path**: the slug read delegates to the
   by-event read, which caches one entry instead of two copies.

### Pre-existing issue surfaced (not caused by this work)

`go test ./...` fails without `-p 1`: the DB-backed packages share one test
database and each calls `TRUNCATE` on setup, so parallel packages wipe each
other's fixtures. Verified against a clean tree. Worth fixing separately.

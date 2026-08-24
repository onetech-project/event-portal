---

description: "Task list for 023-master-data-cache"
---

# Tasks: Master Data Read Cache

**Input**: Design documents from `/specs/023-master-data-cache/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/cache-surface.md](./contracts/cache-surface.md), [quickstart.md](./quickstart.md)

**Tests**: Test tasks are **included and not optional here**. The spec requires them by name — FR-023 (acceptance coverage travels with this change), SC-007 (both cache modes), SC-010 (the populate must be *provable*) — and Principle VIII makes `e2e/` an acceptance gate rather than a tier to skip.

**Organization**: grouped by the three user stories in [spec.md](./spec.md), each independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable — different files, no dependency on an incomplete task
- **[Story]**: US1, US2, US3 (Setup, Foundational and Polish carry no story label)

## Path Conventions

Web app layout per plan.md: `backend/` (Go modular monolith), `e2e/` (Playwright acceptance). No `frontend/` work in this feature.

---

## ⚠️ Read this before starting

**This feature's happy path is indistinguishable from a completely broken implementation.** A cache that never populates and a cache that works perfectly both return the correct gender list. Every assertion below is written so that passing requires the acceleration to be *real* — counting loader calls, inspecting generation counters — not merely the output to be right. If you find yourself writing a test that only checks the returned list, it is not testing this feature.

**Two invariants must survive untouched.** A retired gender is refused at registration (spec 022 FR-022a) and accepted at checkout on a slot that already held it (spec 011 FR-031). Those rules live *above* the cache and must not move into it. Existing tests prove them; if you have to edit one, stop — the design has drifted.

---

## Phase 1: Setup

**Purpose**: confirm the ground is where the plan says it is. No code.

- [X] T001 Verify the constitution gate is open: confirm `.specify/memory/constitution.md` reads `**Version**: 6.1.0` and that Principle VII enumerates `genders` and carries the service-start bullet. FR-020 forbids implementation before this; if it is missing, stop and run `/speckit-constitution`.
- [X] T002 Bring up the stack the tests need: `REDIS_PORT=6380 docker compose up -d postgres redis mailpit && docker compose run --rm migrate up` from the repository root.
- [X] T003 Confirm the baseline is green before changing anything: `cd backend && ./scripts/test.sh ./...`. A pre-existing failure must not be discovered halfway through this feature and mistaken for it.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: the cache surface itself. Every user story reads through it, so nothing else can start.

**⚠️ CRITICAL**: no user story work begins until T008 passes.

- [X] T004 Add `ScopeKindMaster ScopeKind = "master"` to the `ScopeKind` const block in `backend/pkg/cache/cache.go`, and update that block's doc comment — it currently says "enumerates the three invalidation scopes" and "There are deliberately no others", which becomes false with this change. State why master is disjoint from the other three: no write path touches both.
- [X] T005 Add the `Master() Scope` constructor in `backend/pkg/cache/cache.go` beside `Events()`, `Event()` and `Orders()`. It takes no id, like `Events()` and `Orders()`. `GenerationKey()` needs no change — the non-event branch already renders `gen:master`.
- [X] T006 Add `FamilyGendersMaster Family = "genders_master"` to the const block in `backend/pkg/cache/surfaces.go` **and** append it to the `Families` registry slice. Update the `Family` doc comment, which says the set is "CLOSED" and that "adding a tenth requires amending the constitution" — the amendment exists now, so cite v6.1.0 rather than leaving the comment reading as a prohibition already violated.
- [X] T007 Add `GendersActiveKey()` and `GendersAllKey()` to `backend/pkg/cache/surfaces.go`, with fingerprints `proj=active` and `proj=all`, both scoped to `Master()`. Per [contracts/cache-surface.md](./contracts/cache-surface.md) §2. Note in a comment why one family with two fingerprints rather than two families: the active list is a filtered variant of the all-known list, and `Paging`'s existing comment in this file already sets that precedent for keeping variants out of the Prometheus label.
- [X] T008 Add key-shape tests in `backend/pkg/cache/surfaces_test.go`: the two entry prefixes render as `list:genders_master:proj=active:g` and `list:genders_master:proj=all:g`; the two keys are **distinct**; both resolve to generation key `gen:master`; `Master().String()` is `"master"`. The distinctness assertion is the one that matters — a collision would serve the active list to checkout and silently refuse every retired gender a restored booking form carries.

**Checkpoint**: `cd backend && go test ./pkg/cache/...` green. The surface exists and nothing reads through it yet.

---

## Phase 3: User Story 1 — A guest fills in a form without waiting on master data (P1)

**Goal**: the four request paths stop reading the master list from the database on every visit.

**Independent test**: exercise the guest form paths with the cache enabled and confirm the options are identical to the cache-disabled run, while the database is read at most once per accelerator lifetime rather than once per visit.

- [X] T009 [US1] Route `Service.Genders` in `backend/internal/order/service.go` through `cache.Through[[]GenderRecord]` with `cache.GendersActiveKey()`, loading via `s.repo.ListActiveGenders`. Shape the `[]GenderOption` result **after** the cache call, exactly as today — the cached value is the record slice, not the DTO.
- [X] T010 [US1] Route `Service.genderMaps` in `backend/internal/order/service.go` through `cache.Through[[]GenderRecord]` with `cache.GendersAllKey()`, loading via `s.repo.ListGenders`. The `known`/`active` map construction stays below the cache call, unchanged. Do not merge this with T009's projection: `GenderRecord.IsActive` is documented as the zero value on the active-only query, so the two are not interchangeable.
- [X] T011 [US1] Route `Service.activeGenders` in `backend/internal/order/registration_service.go` through the same `cache.GendersActiveKey()` as T009, so registration validation and the form's options provably read one entry. Keep the active-only semantics and the FR-022b comment intact — the refusal must stay an explicit validation failure, never a zero-valued lookup.
- [X] T012 [US1] Add a database-backed test in `backend/internal/order/` asserting the cached and uncached paths return **identical** results for both projections — the same names in the same `ORDER BY name` order (FR-019, SC-002). Build one `Service` with `WithCache` and one without.
- [X] T013 [US1] Add a test in `backend/internal/order/gender_cache_test.go` proving a miss **populates**: with a counting fake `cache.Lists`, read a projection twice and assert the loader ran exactly once (FR-015a, SC-010). This is the test that separates a working cache from a cache-shaped no-op.
- [X] T014 [US1] Add a test in `backend/internal/order/gender_cache_test.go` proving concurrent first reads collapse: fire N goroutines at a cold projection, assert one loader call (FR-015).
- [X] T015 [US1] Add a test in `backend/internal/order/gender_cache_test.go` proving a failed store-back is harmless: with a fake whose `Set` always errors, assert the caller still receives the correct value and the next read simply repeats the load (FR-015b, SC-011).
- [X] T016 [P] [US1] Add a test in `backend/pkg/cache/master_test.go` proving a miss does **not** invalidate: read a cold projection and assert `gen:master` is unchanged across it (FR-015c). Without this, an implementation that invalidates on a miss would leave the two projections evicting each other forever and still pass every other test here.
- [X] T017 [US1] Confirm the existing gender tests in `backend/internal/order/` pass **without modification**. `Service` is constructed with `cache.NoOp{}`, so they exercise the database path exactly as before. Needing to edit one means behaviour moved that should not have.

**Checkpoint**: `cd backend && ./scripts/test.sh ./internal/order/... ./pkg/cache/...` green. Guests are served from the cache; freshness is not yet handled.

---

## Phase 4: User Story 2 — An operator changes the master list and the change takes effect (P1)

**Goal**: a migration that changes the list is picked up by the deployment that carries it, with no operator action.

**Independent test**: change the master list, restart, confirm the new list is served without any manual cache operation.

**⚠️ Without this phase, User Story 1 is a correctness bug rather than an accelerator.** Ship them together.

- [X] T018 [US2] Invalidate `cache.Master()` at service start in `backend/cmd/api/main.go`, on the branch immediately after the existing startup `Ping` succeeds (near the "connected to the cache" log). Invalidate **only** `Master()` — passing any other scope, or calling `FlushAll`, violates FR-009 by discarding surfaces that are expensive to rebuild and already correct.
- [X] T019 [US2] Handle the unreachable-store case per FR-008a: the service MUST still start and serve. Log the missed refresh as a degraded start rather than passing over it silently. Note in a comment that `Redis.distrust` already opens a time-bounded, self-clearing bypass window on invalidation failure, so reads stay correct during it and the TTL backstop covers what follows — this task adds a log line and a decision, not new error handling.
- [X] T020 [US2] Add a test in `backend/pkg/cache/master_test.go` asserting the startup invalidation is **scoped**: invalidating `Master()` leaves the `events`, `event:{id}` and `orders` generation counters untouched (FR-009). This is the narrowest rule in the feature and the one a shortcut would break.
- [X] T021 [US2] Add a test in `backend/pkg/cache/master_test.go` asserting an invalidation of `Master()` orphans **both** projections, since both derive from `gen:master` — correct, because a migration changing the table changes both.
- [X] T022 [US2] Confirm the operator flush already clears this surface (FR-010): `cacheRefreshHandler` in `backend/cmd/api/ops.go` calls `FlushAll`, which discards every entry and every generation counter. Verify rather than assume, and add an assertion if nothing covers it.

**Checkpoint**: the manual walkthrough in [quickstart.md](./quickstart.md) §2 behaves as described — retire a gender, restart, watch it disappear from the options.

---

## Phase 5: User Story 3 — The accelerator fails and nobody notices (P2)

**Goal**: an unreachable store degrades latency and nothing else.

**Independent test**: make the store unreachable under load; every affected path still answers correctly from the database.

- [X] T023 [US3] Add a test in `backend/internal/order/gender_cache_test.go` asserting every gender read falls back to the database and succeeds when the store is unreachable, with no error reaching the caller (FR-013, SC-004).
- [X] T024 [US3] Add a test in `backend/internal/order/gender_cache_test.go` asserting `cache.Through` refuses a gender read issued inside a transaction with `ErrInTransaction` (Principle VII). None of the three consumers does this today; the test is what stops a later refactor from quietly inlining one into checkout's transaction and serialising every concurrent buyer behind a Redis round trip.
- [X] T025 [US3] Verify `/healthz` still reports degraded (200), not failing, with Redis down and Postgres up (FR-014). Covered by `e2e/specs/cache-refresh.spec.ts` "health reports the cache" — confirm it still passes with the new surface present.

**Checkpoint**: `cd backend && ./scripts/test.sh ./...` fully green.

---

## Phase 6: Acceptance (Principle VIII — not optional)

**Purpose**: prove the assembled system. Per Principle VIII, a PR touching a covered flow with no `e2e/` diff and no justification is incomplete.

**Scope decision, made here rather than left open** ([research.md](./research.md) §R8): the retired-gender rules are **not** taken to the e2e tier. Retiring a gender is unreachable through any API — there is no admin CRUD for master lists (migration 000013) — and `e2e/support/db.ts` deliberately excludes `genders` from its reset as migration-seeded data. Proving those rules in a browser would mean introducing the first master-data write into a suite built to avoid one, to re-prove behaviour the Go tier already covers (spec 011 for checkout, spec 022 for registration) and that this feature must not change. E2E therefore asserts **cache-mode sameness and operator-visible behaviour**; the retired-gender rules stay proven at the Go tier by T012 and the untouched spec 022 tests.

- [X] T026 Extend `e2e/specs/free-registration.spec.ts`: the registration form's gender options and its validation behave identically with the cache warm — same options offered, same refusal for an invalid gender. Model it on the existing "repeat reads are served from cache" pattern in `cache-refresh.spec.ts`.
- [X] T027 [P] Extend `e2e/specs/guest-purchase.spec.ts`: the holder forms' gender options and a successful checkout are unchanged with the cache warm. This is the path that reads the all-known projection, so it is where a collapsed-projection bug would surface.
- [X] T028 [P] Extend `e2e/specs/cache-refresh.spec.ts`: an operator flush leaves gender reads correct afterwards (FR-010), alongside the existing flush scenario.
- [X] T029 Run the full suite in **both** cache modes — `cd e2e && npm test` and `cd e2e && E2E_CACHE_ENABLED=false npm test` (SC-007). The cache-off run is what proves Principle VII's kill switch still substitutes cleanly with the new surface present; it is not a formality.

**Note on what e2e cannot prove here**: the Playwright `webServer` starts the API once, so the startup invalidation of T018 is not exercisable from a browser spec. It is covered by T020/T021 at the Go tier and by the manual walkthrough in [quickstart.md](./quickstart.md) §2. Stated rather than left as a silent gap.

---

## Phase 7: Polish & Cross-Cutting

- [X] T030 [P] Confirm `backend/cmd/api/architecture_test.go` is green — `internal/order` reaching `pkg/cache` is legal shared infrastructure; reaching another domain is not (Principle II).
- [X] T031 [P] Verify via `backend/pkg/cache/metrics.go` and the `/metrics` endpoint that the new family appears under the `genders_master` label, and that no unbounded value (an id, a name) reaches a Prometheus label.
- [X] T032 [P] Re-read `ARCHITECTURE.md` §3.6a and remove the "admitted but not yet implemented" note plus "The registry still holds nine families until it lands" — both become false the moment T006 lands. The `master` scope row's "(spec 023, pending)" qualifier goes with them.
- [X] T033 Walk [quickstart.md](./quickstart.md) §4's definition-of-done checklist end to end and confirm every box.

---

## Dependencies

```text
Phase 1 (Setup: T001–T003)
        │
        ▼
Phase 2 (Foundational: T004–T008)   ← BLOCKS everything below
        │
        ├──────────────┬───────────────┐
        ▼              ▼               ▼
   US1 (T009–T017)  US3 (T023–T025)   (US2 needs US1's reads to exist)
        │              │
        ▼              │
   US2 (T018–T022) ◄───┘
        │
        ▼
Phase 6 Acceptance (T026–T029)
        │
        ▼
Phase 7 Polish (T030–T033)
```

**Story dependencies**:

- **US1** depends only on Foundational. It is the MVP.
- **US2** depends on US1 — there is nothing to keep fresh until something is cached. **But US1 must not ship without it**: a cached list with no refresh trigger serves a pre-migration list until its TTL lapses.
- **US3** depends only on Foundational and can be built in parallel with US1. Most of its behaviour is inherited from `cache.Through`; its tasks are assertions that it was inherited rather than accidentally overridden.

**Within-story ordering**: T009 → T011 (both use `GendersActiveKey`, so T009 establishes the key's use first). T010 is independent of both and can go in parallel. T018 → T019.

---

## Parallel execution examples

**Phase 2** — T004/T005 (`cache.go`) and T006/T007 (`surfaces.go`) touch different files and can proceed in parallel; T008 needs all four.

**Phase 3** — T013, T014 and T015 all land in `backend/internal/order/gender_cache_test.go`, so they are written in sequence, not in parallel. Only T016 is `[P]`: it lives in `backend/pkg/cache/master_test.go` and touches nothing the others do.

```text
T013 → T014 → T015   (one file, sequential)
T016                 [P] (backend/pkg/cache/master_test.go)
```

**Phase 5** — T023 and T024 share `gender_cache_test.go` and are sequential. T025 verifies an existing `e2e/` scenario rather than writing one, so it has nothing to parallelise against and carries no marker.

**Phase 6** — T027 and T028 touch different spec files. T026 and T027 both exercise gender options but in different specs, so they parallelise too; T029 needs all three.

**Phase 7** — T030, T031 and T032 are fully independent.

---

## Implementation strategy

**MVP = Phase 2 + US1 + US2.** Not US1 alone. US1 is the only story that delivers value, but shipping it without US2 converts a performance improvement into a correctness bug: the list would be refreshed by nothing, and the next migration that touches genders would be invisible until the TTL lapsed. The spec makes both P1 for exactly this reason.

**Incremental delivery**:

1. **Phase 2** — the surface exists, nothing reads it. Safe to merge; changes no behaviour.
2. **+ US1** — reads are accelerated. Do not merge alone.
3. **+ US2** — freshness is correct. This is the first shippable point.
4. **+ US3** — failure behaviour asserted. Mostly proving inherited behaviour.
5. **+ Phase 6** — the acceptance gate. Required before merge, not after.

**Suggested commit split** — this branch already carries unrelated work (the spec 022 backend and frontend gender fixes, and the constitution amendment). Keep this feature's commits separate from those, and separate the constitution amendment from the implementation so the governance change can be reviewed as governance.

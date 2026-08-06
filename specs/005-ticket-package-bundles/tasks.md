---

description: "Task list for ticket package bundles"
---

# Tasks: Ticket Package Bundles in the Selection Step

**Input**: Design documents from `/specs/005-ticket-package-bundles/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: Test tasks ARE included. Justification: SC-003/SC-004 state concurrency outcomes that can only be demonstrated by a test; `contracts/checkout-transaction.md` §5 enumerates eleven binding test obligations; and the repo already gates correctness on `./scripts/test.sh` plus `backend/cmd/api/architecture_test.go`. Tests here are not optional extras — the oversell and deadlock guarantees are the feature.

**Organization**: Grouped by user story so each is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US4)
- Exact file paths included in every task

## Path Conventions

Web app, per [plan.md](./plan.md) Structure Decision: `backend/` (Go modular monolith) and `frontend/` (Next.js App Router). No new top-level directories.

## Story Sequencing Note

US1 and US2 are **both P1**. Within that tier they are ordered by dependency, not by spec listing order: US2 delivers the `packages[]` payload with derived availability, and US1 is the screen that renders it. Building US2 first means US1 is developed against a real endpoint rather than a mock that must later be unwound. MVP is therefore **Phases 1–4** (US2 + US1) — that pair is the smallest increment that puts a working bundle in front of a buyer.

---

## Phase 1: Setup (Schema & Code Generation)

**Purpose**: Land the schema and absorb the generated-code fallout before any behaviour is written, so the existing suite is green before package logic starts.

- [ ] T001 Create `backend/migrations/000003_packages.up.sql` with the `ticket_types`/`packages` composite unique keys, the `packages` and `package_tickets` tables, the `order_items` and `attendees` alterations, and all five indexes exactly as specified in [contracts/schema.md](./contracts/schema.md) §4
- [ ] T002 Create `backend/migrations/000003_packages.down.sql` reversing T001 in FK-safe order per [contracts/schema.md](./contracts/schema.md) §4
- [ ] T003 Update `SCHEMA.md` to include `packages`, `package_tickets`, the altered `order_items`/`attendees` columns and the new indexes — the constitution makes this file the absolute source of truth and it must move with the migration
- [ ] T004 Apply the migration (`./scripts/setup-test-db.sh` and `docker compose up -d migrate`) and run `sqlc generate` in `backend/`, confirming `docker compose run --rm migrate version` reports `3`
- [ ] T005 Resolve the `uuid.NullUUID` compile fallout across `backend/internal/order/repository.go`, `backend/internal/order/dto.go` and `backend/internal/order/admin_service.go` — `order_items.ticket_type_id` is now nullable so every read must branch on line kind (see [research.md](./research.md) R-005); do NOT suppress this with casts or `sqlc.narrow`
- [ ] T006 [P] Add `SeedPackage` and `SeedPackageTicket` helpers to `backend/internal/testsupport/testsupport.go`, and add `packages` + `package_tickets` to its truncate list ahead of `ticket_types` to respect FK order
- [ ] T007 [P] Add error codes `CodePackageNotFound`, `CodePackageNotOnSale`, `CodePackageUnavailable`, `CodePackageHasOrders`, `CodePackageCompositionLocked` to `backend/pkg/apperr`

**Checkpoint**: Migration applied, `sqlc generate` clean, `./scripts/test.sh ./...` green with no package behaviour yet.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Queries, generated types, domain types and DTOs that every user story below depends on.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [ ] T008 Add package CRUD queries (`ListPackagesByEventID`, `GetPackageByID`, `CreatePackage`, `UpdatePackage`, `DeletePackage`) to `backend/internal/event/queries/event.sql`
- [ ] T009 Add composition queries (`ListPackageComponentsByPackageIDs`, `ReplacePackageComponents` as delete+insert, `ListPackagesByTicketTypeID`) to `backend/internal/event/queries/event.sql` per [contracts/checkout-transaction.md](./contracts/checkout-transaction.md) §1.3
- [ ] T010 Add the availability queries `GetPackageAvailability` and `ListPackagesWithAvailabilityByEventID` to `backend/internal/event/queries/event.sql` exactly as specified in [contracts/checkout-transaction.md](./contracts/checkout-transaction.md) §1.1–1.2, keeping the `LEFT JOIN` so a componentless package still lists
- [ ] T011 Add `ListQuotaHoldsByOrderID` to `backend/internal/order/queries/order.sql` per [contracts/checkout-transaction.md](./contracts/checkout-transaction.md) §3, with the `ORDER BY ticket_type_id` that matches deduction order
- [ ] T012 Extend the ticket-type `sold` query in `backend/internal/event/queries/event.sql` to UNION package lines through `package_tickets` per [contracts/api.md](./contracts/api.md) — a `sold` that ignores package lines under-reports every bundled sale
- [ ] T013 Run `sqlc generate` in `backend/` and verify `Package` and `PackageTicket` structs appear in `backend/internal/event/eventsql/models.go` with **no** quota field
- [ ] T014 [P] Add `PackageAvailability`, `PackageComponent` and `PackageForCheckout` domain types to a new `backend/internal/event/availability.go` per [contracts/models.md](./contracts/models.md) §2.1–2.2
- [ ] T015 [P] Add `PackageComponentDTO`, `PackageSummaryDTO` and `PackageAdminDTO` to `backend/internal/event/dto.go` per [contracts/models.md](./contracts/models.md) §2.5 — hand-written, no `eventsql` struct may leak
- [ ] T016 [P] Add the discriminated order-line DTO (`kind`, `package_name`, nullable `ticket_type_name`) to `backend/internal/order/dto.go` per [contracts/api.md](./contracts/api.md)
- [ ] T017 Add package read methods to `backend/internal/event/repository.go` wrapping the T008–T010 queries
- [ ] T018 Update `CreateOrderItem` and `CreateAttendee` in `backend/internal/order/repository.go` to accept nullable `ticket_type_id` / `package_id`, enforcing the XOR at the call boundary
- [ ] T019 [P] Add package wire types (`PackageComponent`, `PackageSummary`, `PackageAdminView`, `CheckoutItemInput`, `CheckoutAttendeeInput`, discriminated `PublicOrderItem`) to `frontend/lib/types.ts` per [contracts/models.md](./contracts/models.md) §3

**Checkpoint**: Foundation ready — user stories can begin.

---

## Phase 3: User Story 2 — The list reflects real, live availability (Priority: P1)

**Goal**: A package's availability is derived at read time from its constituents' remaining quota and served on the event detail payload, with no stored inventory anywhere.

**Independent Test**: Seed a package over two ticket types with quotas 5 and 2; `GET /api/v1/events/:slug` reports `available_units: 2`. Set one constituent to 0; the package reports `purchasable: false`. No checkout involved.

### Tests for User Story 2

- [ ] T020 [P] [US2] Repository test in `backend/internal/event/repository_test.go`: availability equals `MIN(quota / quantity_per_unit)` across constituents, asserting the 5-and-2 → 2 case
- [ ] T021 [P] [US2] Repository test in `backend/internal/event/repository_test.go`: whole-sets-only — quota 5 with `quantity_per_unit` 2 yields 2 units, not 3
- [ ] T022 [P] [US2] Repository test in `backend/internal/event/repository_test.go`: a componentless package returns 0 units and `purchasable: false` rather than vanishing from the list
- [ ] T023 [P] [US2] Service test in `backend/internal/event/public_service_test.go`: `purchasable` is false when the package's own window is open but a constituent's window has closed
- [ ] T024 [P] [US2] Service test in `backend/internal/event/public_service_test.go`: `INACTIVE` packages are absent from the public payload

### Implementation for User Story 2

- [ ] T025 [US2] Implement availability mapping (generated rows → `PackageAvailability`, including `limiting_ticket_type_id`) in `backend/internal/event/availability.go`
- [ ] T026 [US2] Extend `backend/internal/event/public_service.go` to load packages with availability plus batched components and assemble `PackageSummaryDTO` — two queries total, no N+1 across packages
- [ ] T027 [US2] Add `packages[]` to the `GET /events/:slug` response in `backend/internal/event/handler.go`, and confirm the `Cache-Control: no-store` header covers it
- [ ] T028 [P] [US2] Add `GET /events/:slug/packages` to `backend/internal/event/handler.go` for availability-only polling
- [ ] T029 [P] [US2] Register the new public route in `backend/cmd/api/main.go`
- [ ] T030 [P] [US2] Extend `useEventBySlug` and add a `usePackages` hook in `frontend/lib/queries.ts`, keeping the existing no-cache posture for quota-bearing reads
- [ ] T031 [P] [US2] Add `EventDetailWithPackages` and the `maxQuantityFor` helper to `frontend/lib/types.ts` per [contracts/models.md](./contracts/models.md) §4

**Checkpoint**: Bundle availability is queryable and correct. Quickstart Scenarios 1–3 pass.

---

## Phase 4: User Story 1 — Guest sees and selects a bundle in the ticket list (Priority: P1) 🎯 MVP

**Goal**: The booking step renders tickets and bundles in one list with identical Add → stepper mechanics, a bundle badge, and a live Selected Ticket summary panel — matching the supplied designs.

**Independent Test**: Load an event with two tickets and one bundle; all three rows render, only the bundle is badged, Add becomes a stepper, and the summary total matches the selection.

### Tests for User Story 1

- [ ] T032 [P] [US1] Component test in `frontend/components/booking/selectable-row.test.tsx`: Add becomes a stepper at 1; decrementing from 1 returns the row to Add
- [ ] T033 [P] [US1] Component test in `frontend/components/booking/selectable-row.test.tsx`: package rows render the bundle badge and ticket rows do not; the stepper ceiling is `available_units` for a package and `quota_remaining` for a ticket
- [ ] T034 [P] [US1] Component test in `frontend/components/booking/selection-summary.test.tsx`: empty state disables Buy Ticket; a selected bundle renders as **one** line at its own price, never decomposed into constituent lines
- [ ] T035 [P] [US1] Page test in `frontend/app/events/[slug]/page.test.tsx`: selecting Day 1 and Day 2 at quantity 1 each shows two lines and a Rp70.000 total; selecting the bundle alone shows Rp50.000

### Implementation for User Story 1

- [ ] T036 [P] [US1] Create `frontend/components/booking/selectable-row.tsx` — the shared Add → `− n +` stepper row with an optional bundle badge, disabled when unavailable, capped at the row's max quantity
- [ ] T037 [P] [US1] Create `frontend/components/booking/selection-summary.tsx` — the Selected Ticket panel with placeholder empty state, per-line quantity and subtotal, combined item count and total, and the Buy Ticket button
- [ ] T038 [US1] Rewrite `frontend/app/events/[slug]/page.tsx` to render tickets and packages as one list using `SelectableRow`, replacing the current quantity `<Select>` (see [research.md](./research.md) R-009)
- [ ] T039 [US1] Wire the Selected Ticket panel into `frontend/app/events/[slug]/page.tsx` with selection state keyed by kind and id, so a ticket and a package can never collide on the same key
- [ ] T040 [US1] Extend the checkout link in `frontend/app/events/[slug]/page.tsx` to emit `p=<packageId>:<qty>` pairs alongside the existing `t=<ticketTypeId>:<qty>`
- [ ] T041 [US1] Add unavailable-state rendering (sold out, not yet open, sales closed) for package rows in `frontend/components/booking/selectable-row.tsx`, reusing the existing ticket-row messaging

**Checkpoint**: 🎯 **MVP complete.** A guest can see and select a bundle against live availability. Quickstart Scenario 14 passes.

---

## Phase 5: User Story 3 — A selection containing bundles checks out without overselling (Priority: P2)

**Goal**: A mixed cart of tickets and bundles becomes a paid order, with per-ticket-type demand aggregated across the whole cart, deducted in deterministic order, and restored exactly on release.

**Independent Test**: Check out 1 bundle + 1 ticket, complete all attendee slots, verify quotas and issued passes; then run concurrent checkouts against a single remaining set and verify exactly one wins.

### Tests for User Story 3

- [ ] T042 [P] [US3] Unit test in `backend/internal/order/demand_test.go`: expanding `1 × Bundle(Day1, Day2)` plus `2 × Day1` aggregates to Day 1 → 3, Day 2 → 1 — the test that fails if lines are checked in isolation
- [ ] T043 [P] [US3] Unit test in `backend/internal/order/demand_test.go`: the deduction list is sorted ascending by `ticket_type_id` and is stable across repeated calls with shuffled input (guards against Go map iteration order)
- [ ] T044 [P] [US3] Unit test in `backend/internal/order/demand_test.go`: `quantity_per_unit > 1` multiplies correctly into aggregated demand
- [ ] T045 [P] [US3] Service test in `backend/internal/order/service_test.go`: a bundle checkout deducts each constituent and records **one** `order_items` row at the package's own price, with `total_amount` equal to the package price
- [ ] T046 [P] [US3] Service test in `backend/internal/order/service_test.go`: attendee counts grouped by `(ticket_type_id, package_id)` must equal the server's expansion — one too few and one too many both rejected
- [ ] T047 [P] [US3] Service test in `backend/internal/order/service_test.go`: a cart whose bundle is available but whose standalone ticket is not leaves **no** quota consumed anywhere
- [ ] T048 [US3] Concurrency test in `backend/internal/order/service_test.go`: N goroutines contend for one remaining complete set; exactly one succeeds, quotas land at 0, never negative (SC-003, SC-004)
- [ ] T049 [US3] Deadlock test in `backend/internal/order/service_test.go`: two overlapping bundles (`Day1+Day2`, `Day2+Day3`) bought concurrently over many iterations produce zero `SQLSTATE 40P01` errors
- [ ] T050 [P] [US3] Repository test in `backend/internal/order/repository_test.go`: `ListQuotaHoldsByOrderID` expands package lines through the junction and returns the exact inverse of what checkout deducted
- [ ] T051 [P] [US3] Service test in `backend/internal/order/service_test.go`: expiry restores every constituent exactly, and a replayed cancellation restores nothing further

### Implementation for User Story 3

- [ ] T052 [US3] Create `backend/internal/order/demand.go` with pure `ExpandItems` and `AggregateDemand` functions returning a `ticket_type_id`-sorted slice — no database access, so it is unit-testable in isolation
- [ ] T053 [US3] Add `PackageForCheckout` to the `EventProvider` interface in `backend/internal/order/event_provider.go`, taking the caller's `pgx.Tx` for the same pool-exhaustion reason the ticket-type variant does; deliberately add **no** `CheckAndDeductPackageQuota`
- [ ] T054 [US3] Implement the `PackageForCheckout` adapter in `backend/cmd/api/adapters.go`, keeping `internal/order` free of any `internal/event` import
- [ ] T055 [US3] Refactor `reserveOnce` in `backend/internal/order/service.go` from the per-item deduct loop into resolve → expand → aggregate → sort → deduct → persist, reusing the existing `CheckAndDeductQuota` unchanged
- [ ] T056 [US3] Add package line validation to `backend/internal/order/service.go`: XOR on ids, `ACTIVE` status, package window, every constituent's window, at least one component
- [ ] T057 [US3] Implement attendee-slot expansion and exact-count validation in `backend/internal/order/service.go`, persisting `package_id` alongside the non-null `ticket_type_id` on each bundle-derived attendee
- [ ] T058 [US3] Update the release path in `backend/internal/order/service.go` (expiry sweeper, webhook `expire`/`cancel`/`deny`/`failure`, and gateway-failure `compensate`) to restore from `ListQuotaHoldsByOrderID`, after the guarded `PENDING` transition so a duplicate signal cannot double-credit
- [ ] T059 [P] [US3] Update `frontend/app/checkout/page.tsx` to parse `p=` pairs, render one attendee form per constituent unit with independent name/email fields, and post `CheckoutRequestV2`
- [ ] T060 [P] [US3] Add Zod schemas for package checkout items and bundle attendee slots to `frontend/lib/schemas.ts`

**Checkpoint**: Bundles are purchasable end to end. Quickstart Scenarios 4–9 pass.

---

## Phase 6: User Story 4 — Administrator composes a bundle (Priority: P3)

**Goal**: An administrator can create, edit and delete bundles and their composition, and is never asked for an inventory figure.

**Independent Test**: Create a bundle over two of an event's tickets through the admin API, then load the public booking page and see it listed with the configured name and price.

### Tests for User Story 4

- [ ] T061 [P] [US4] Service test in `backend/internal/event/package_service_test.go`: a request body containing any quota-like field is **rejected**, not silently ignored (FR-036)
- [ ] T062 [P] [US4] Service test in `backend/internal/event/package_service_test.go`: empty components, duplicate ticket, `quantity_per_unit` of 0, negative price and inverted sales window are each rejected
- [ ] T063 [P] [US4] Repository test in `backend/internal/event/repository_test.go`: inserting a `package_tickets` row referencing another event's ticket type fails at the **database**, with application validation bypassed (FR-015)
- [ ] T064 [P] [US4] Service test in `backend/internal/event/package_service_test.go`: composition edits are rejected with `409` while a `PENDING` order exists, while name/price/window/status edits still succeed ([research.md](./research.md) R-004)
- [ ] T065 [P] [US4] Service test in `backend/internal/event/package_service_test.go`: deleting an ordered package, a ticket type used by a package, and an event with packages each return a clean `400` rather than a leaked constraint violation

### Implementation for User Story 4

- [ ] T066 [US4] Create `backend/internal/event/package_service.go` with admin CRUD, composition replacement and all validation rules from [data-model.md](./data-model.md) §4
- [ ] T067 [US4] Add the open-`PENDING`-order guard for composition changes to `backend/internal/event/package_service.go`
- [ ] T068 [US4] Add the delete guards for packages, package-referenced ticket types and events with packages to `backend/internal/event/package_service.go` and `backend/internal/event/admin_service.go`, returning `400` with the blocking package names
- [ ] T069 [US4] Create `backend/internal/event/package_handler.go` exposing `GET|POST /admin/packages`, `GET|PUT|DELETE /admin/packages/:id` and `GET /admin/packages/:id/availability` per [contracts/api.md](./contracts/api.md)
- [ ] T070 [US4] Register the admin package routes behind the JWT middleware in `backend/cmd/api/main.go`
- [ ] T071 [P] [US4] Add admin package queries and mutations to `frontend/lib/queries.ts`
- [ ] T072 [P] [US4] Add a package section to `frontend/app/admin/events/[id]/page.tsx` — composition picker limited to that event's ticket types, per-component quantity, and **no quota input**

**Checkpoint**: All four user stories independently functional. Quickstart Scenarios 10–13 pass.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T073 [P] Update `PRD.md` §1.5 to add `CRUD /api/v1/admin/packages` and the `packages[]` field on `GET /api/v1/events/:slug` — the section is LOCKED and the constitution requires it move with the change
- [ ] T074 [P] Amend `.specify/memory/constitution.md` (PATCH bump) to clarify that Principle VI's "associated Order" check on `attendees` is load-bearing, not redundant, because package-only orders leave `order_items.ticket_type_id` null; update the Sync Impact Report
- [ ] T075 [P] Extend `backend/internal/order/admin_service.go` and the attendee export so bundle-derived registrants expose `package_name`
- [ ] T076 [P] Verify `backend/cmd/api/architecture_test.go` still passes — no `internal/order` → `internal/event` import appeared and no `eventsql` struct reached a response
- [ ] T077 Confirm the booking list path in `backend/internal/event/public_service.go` stays within the SC-005 budget for 20 ticket types and 10 packages, and issues two queries rather than N+1
- [ ] T078 Verify pre-feature orders still render with every line as `kind: "ticket"` in `backend/internal/order/dto.go` and `frontend/app/orders/[orderNumber]/page.tsx`
- [ ] T079 Run the full suites: `cd backend && ./scripts/test.sh ./...` and `cd frontend && npx vitest run`
- [ ] T080 Walk every scenario in [quickstart.md](./quickstart.md), including the Scenario 8 deadlock loop at several hundred iterations

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately. T005 is the gate: nothing else compiles until the nullability fallout is resolved.
- **Foundational (Phase 2)**: Depends on Phase 1. **Blocks all user stories.**
- **US2 (Phase 3)**: Depends on Phase 2.
- **US1 (Phase 4)**: Depends on Phase 3 — it renders the payload US2 produces.
- **US3 (Phase 5)**: Depends on Phase 2 only. Can run in parallel with Phases 3–4 (backend checkout vs. frontend list touch disjoint files).
- **US4 (Phase 6)**: Depends on Phase 2 only. Can run in parallel with Phases 3–5.
- **Polish (Phase 7)**: Depends on all desired stories.

### User Story Dependencies

- **US2 (P1)**: Independent after Foundational.
- **US1 (P1)**: Needs US2's endpoint to be genuinely testable. This is the one real cross-story dependency and it is why US2 is sequenced first.
- **US3 (P2)**: Independent after Foundational — testable via API with no UI.
- **US4 (P3)**: Independent after Foundational — but seeding for US1–US3 is easier once it exists.

### Within Each User Story

- Tests before implementation; they must fail first.
- Queries → generated types → domain types → services → handlers → routes.
- `demand.go` (T052) before the `reserveOnce` refactor (T055) — pure logic proven in isolation before it is wired into a transaction.

### Parallel Opportunities

- T006, T007 in Setup.
- T014, T015, T016, T019 in Foundational, after T013.
- All of T020–T024 (US2 tests) together.
- All of T032–T035 (US1 tests) together; T036 and T037 are separate files.
- All of T042–T047, T050, T051 (US3 tests) together. **T048 and T049 must run alone** — they are concurrency tests and `./scripts/test.sh` already forces `-p 1` because database-backed packages truncate a shared schema.
- All of T061–T065 (US4 tests) together.
- Most of Phase 7.

---

## Parallel Example: User Story 3

```bash
# Launch the pure-logic tests together — no database, no shared fixture:
Task: "Aggregation test for mixed cart in backend/internal/order/demand_test.go"
Task: "Deterministic sort order test in backend/internal/order/demand_test.go"
Task: "quantity_per_unit multiplication test in backend/internal/order/demand_test.go"

# Then the database-backed service tests together:
Task: "Bundle checkout deducts constituents in backend/internal/order/service_test.go"
Task: "Attendee count validation in backend/internal/order/service_test.go"
Task: "All-or-nothing rejection in backend/internal/order/service_test.go"

# T048 and T049 run alone afterwards — concurrency fixtures must not be shared.
```

---

## Implementation Strategy

### MVP First (US2 + US1)

1. Phase 1: Setup — schema lands, suite green.
2. Phase 2: Foundational — **blocks everything**.
3. Phase 3: US2 — availability derives correctly and is served.
4. Phase 4: US1 — the selection screen renders it.
5. **STOP and VALIDATE**: Quickstart Scenarios 1–3 and 14.
6. Demo: a guest can see and select a bundle against live inventory.

### Incremental Delivery

1. Setup + Foundational → foundation ready.
2. US2 → availability API → validate → demo.
3. US1 → selection screen → validate → **MVP demo**.
4. US3 → purchasable end to end → validate → demo.
5. US4 → admin authoring → validate → demo.
6. Polish → governance docs, full quickstart walk.

### Parallel Team Strategy

After Foundational completes:

- **Developer A**: US2 → US1 (the vertical slice through the booking screen).
- **Developer B**: US3 (checkout refactor — the highest-risk work; front-load review here).
- **Developer C**: US4 (admin CRUD).

A and B touch disjoint files: A works in `internal/event` read paths and `frontend/app/events`, B in `internal/order` and `frontend/app/checkout`. The shared surface is the `EventProvider` port, fixed in T053, so agree that signature before splitting.

---

## Notes

- `[P]` tasks are different files with no incomplete dependencies.
- The two tasks worth the most review attention are **T055** (the `reserveOnce` refactor) and **T010** (the availability CTE). Everything else is conventional.
- T005 is deliberately unpleasant: the compiler is enumerating every site that must decide what a package line means. Do not shortcut it.
- Never add a quota column, DTO field or form input to a package — it is the one invariant the whole design rests on.
- Commit after each task or logical group; stop at any checkpoint to validate a story independently.

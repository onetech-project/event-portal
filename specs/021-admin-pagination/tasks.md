---

description: "Task list for Admin Console Pagination"
---

# Tasks: Admin Console Pagination

**Input**: Design documents from `/specs/021-admin-pagination/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/api.md](./contracts/api.md), [quickstart.md](./quickstart.md)

**Tests**: Test tasks below are **not** optional for this feature. FR-020 requires the
acceptance suite to be extended in the same change; Constitution Principle VIII makes
`e2e/` a gate; and `AGENTS.md` keeps the Go and Vitest tiers required where they already
apply. The paging correctness properties (no repeats, no gaps under a tie) are exactly the
kind that pass by luck when untested.

**Organization**: Grouped by user story so each can be implemented, tested and shipped on
its own.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1–US4, mapping to the user stories in [spec.md](./spec.md)

## Path Conventions

Web application, per plan.md: Go modular monolith in `backend/` (`internal/<domain>/`,
shared `pkg/`), Next.js App Router in `frontend/`, shared contract in `api/openapi.yml`,
acceptance suite in `e2e/`. All paths below are repository-relative.

---

## Phase 1: Setup

**Purpose**: Establish a trustworthy baseline, so that a red test later means this feature
broke something rather than something already being broken.

- [X] T001 Run all three tiers and record them green before touching anything: `cd backend && ./scripts/test.sh ./...`, `cd frontend && npx vitest run`, and `cd e2e && npm test` (infrastructure first: `REDIS_PORT=6380 docker compose up -d postgres redis mailpit && docker compose run --rm migrate up`)
- [X] T002 Confirm `cd backend && sqlc generate` leaves `git status` clean, so that later regeneration diffs are attributable to this feature's query edits and nothing else

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The shared paging primitives every story depends on — the transport type, the
cache-key change, the URL state hook, and the UI control. Nothing story-specific here.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

**Note on scope**: `useListParams` (T011–T012) is URL-backed from the start rather than
being state-backed and migrated later. That means FR-011's addressability arrives with the
foundation and User Story 4's phase carries the rest of its behavior (the size control, the
row-preserving size change, the clamp self-correction). This is called out rather than
papered over — it is the reason US4's phase is small.

### Backend shared primitives

- [X] T003 [P] Write failing unit tests for `PageRequest` clamping and `Page[T]` invariants in `backend/pkg/httpx/page_test.go` — cover page `<1`/absent/unparseable → 1, size absent/`<1`/unparseable → 20, size `>100` → 100, `TotalPages` of 0 when `Total` is 0, and `Items` marshalling as `[]` never `null` (rules in [data-model.md §1](./data-model.md))
- [X] T004 Implement `PageRequest`, `Page[T]`, `Offset()`, `Limit()` and `NewPage()` in `backend/pkg/httpx/page.go`, alongside the existing `Envelope`, so clamping is total and no downstream code re-checks
- [X] T005 Add `BindPage(c echo.Context) PageRequest` to `backend/pkg/httpx/page.go` — reads `page` and `page_size`, never returns an error, and add its cases to `backend/pkg/httpx/page_test.go` (FR-013, FR-014, SC-007)

### Cache keys

- [X] T006 [P] Write failing tests in `backend/pkg/cache/cache_test.go` asserting that two different pages of the same filter produce different `EntryPrefix()` values, that page and size appear in a fixed order in the fingerprint, and that `Key.Scope` is unchanged so one `Invalidate` still orphans every page
- [X] T007 Extend `OrdersAdminKey`, `AttendeesAdminKey` and `EventsAdminKey` with `page`/`size` fingerprint components in `backend/pkg/cache/surfaces.go`, using the existing `strings.Builder` fixed-order style — add no `Family`, no `ScopeKind`, no constructor ([research.md R5](./research.md))

### Frontend shared primitives

- [X] T008 [P] Add `Page<T>`, `ListParams` and `EventOption` types to `frontend/lib/types.ts`, mirroring [contracts/api.md](./contracts/api.md) field-for-field
- [X] T009 [P] Write failing tests for the `Pagination` component in `frontend/components/ui/pagination.test.tsx` — previous disabled on page 1, next disabled on the last page, nothing rendered for a single page or an empty result, keyboard operability, and current/total page announced to assistive technology (FR-006, FR-016, FR-017)
- [X] T010 Implement the shared `Pagination` component in `frontend/components/ui/pagination.tsx` — numbered pages with first/last/prev/next, a page-size `Select`, and a "Showing X–Y of Z" count, styled to match the existing `table.tsx` and `select.tsx` primitives
- [X] T011 [P] Write failing tests for `useListParams` in `frontend/lib/use-list-params.test.ts` — garbage clamped, defaults omitted from the URL, a filter change resetting `page` to 1, and the size-change page recalculation `floor(((oldPage-1)*oldSize)/newSize)+1` ([data-model.md §7](./data-model.md))
- [X] T012 Implement `useListParams` in `frontend/lib/use-list-params.ts` over `useSearchParams` + `router.replace`, owning page, page size and the existing filters as URL search params

### Acceptance-suite support

- [X] T013 [P] Add a paged admin list helper to `e2e/support/api.ts` that reads `data.items` and `data.total`, and extend the existing `adminOrders` helper to accept paging parameters
- [X] T014 [P] Add paging navigation to the `AdminConsole` page object in `e2e/support/journey.ts` — go to next/previous/numbered page, read the visible row identifiers, and read the reported total

**Checkpoint**: Paging primitives exist and are unit-tested. No endpoint has changed yet, so all three tiers must still be green here.

---

## Phase 3: User Story 1 - Orders list (Priority: P1) 🎯 MVP

**Goal**: `/admin/orders` returns and renders one bounded page at a time with an accurate
total, correct movement between pages, and filters that reset to page 1.

**Independent Test**: Seed more orders than one page holds (use a small `page_size` so the
orders can be created through the real booking API), open Orders, and confirm the first
page is bounded, the total is right, and page 2 is disjoint from page 1. Nothing else in
the console needs to change for this to be valuable.

### Tests for User Story 1

- [X] T015 [P] [US1] Write failing database-backed tests in `backend/internal/order/repository_test.go` for `CountOrdersAdmin` — agreement with an unpaginated `ListOrdersAdmin` under each filter combination, and `total: 0` for an event with no ticket types
- [X] T016 [P] [US1] Write failing tests in `backend/internal/order/admin_service_test.go` for `ListOrders` paging — page 2 disjoint from page 1, the two together equalling the full list, an out-of-range page clamping to the last page, and **rows that genuinely tie on `created_at`** ordering identically across two offset reads (FR-008, FR-013, SC-004)
- [X] T017 [P] [US1] Write failing tests in `backend/internal/order/admin_cache_test.go` proving two different pages take different cache keys and that a committed order write invalidates both
- [X] T018 [P] [US1] Create `backend/internal/order/admin_handler_test.go` with failing tests: `page=0`, `page=-3`, `page=abc`, `page_size=0`, `page_size=abc`, `page_size=5000` all return 200 with clamped values, while a malformed `status` still returns 400 ([research.md R8](./research.md))

### Implementation for User Story 1

- [X] T019 [US1] Add `CountOrdersAdmin` immediately above `ListOrdersAdmin` in `backend/internal/order/queries/order.sql` sharing its `WHERE` clause verbatim, and add `LIMIT`/`OFFSET` plus the `o.id DESC` tiebreaker to `ListOrdersAdmin`'s `ORDER BY`
- [X] T020 [US1] Run `cd backend && sqlc generate` and review the diff in `backend/internal/order/ordersql/order.sql.go` for the new params and count row
- [X] T021 [US1] Add paging to `OrderFilter` in `backend/internal/order/dto.go`
- [X] T022 [US1] Rework `AdminService.ListOrders`/`listOrders` in `backend/internal/order/admin_service.go` to count, clamp, then page — returning `httpx.Page[OrderSummary]`, keeping the whole page (count included) as the cached value, and preserving the `ticketTypeScope` short-circuit as an empty page with `total: 0`
- [X] T023 [US1] Parse paging via `httpx.BindPage` in `listOrders` in `backend/internal/order/admin_handler.go`, leaving the `status` and `event_id` 400s exactly as they are
- [X] T024 [P] [US1] Update `GET /api/v1/admin/orders` in `api/openapi.yml` — add the shared `PageParam`/`PageSizeParam` and `PageMeta` schemas plus the `EventOption` schema to `components`, and change `data` to the paged object ([contracts/api.md §1](./contracts/api.md))
- [X] T025 [US1] Change `useAdminOrders` in `frontend/lib/queries.ts` to take `ListParams` and return `Page<OrderSummary>`, and include page and page size in `queryKeys.adminOrders` — omitting them would serve page 1's rows for page 2
- [X] T026 [US1] Wire `useListParams` and `<Pagination>` into `frontend/app/(admin)/admin/orders/page.tsx`, moving the `status` and `event_id` filters out of `useState` and into the URL, and rewriting the URL when the server's clamped `page` differs from the requested one

**Checkpoint**: Orders paginates end to end. Backend and frontend tiers green. Other menus untouched and still working.

---

## Phase 4: User Story 2 - Attendees list (Priority: P1)

**Goal**: `/admin/attendees` behaves exactly as Orders now does, with the event filter still
applied and ticket-type names still resolved in one batched lookup.

**Independent Test**: Seed an event with more attendees than one page holds, filter to it,
and confirm bounded rows, an accurate per-event total, and correct movement including the
last page offering no further forward step.

### Tests for User Story 2

- [X] T027 [P] [US2] Write failing database-backed tests in `backend/internal/order/repository_test.go` for `CountAttendeesAdmin` agreeing with an unpaginated read under the `order_id` and `event_id` filters
- [X] T028 [P] [US2] Write failing tests in `backend/internal/order/admin_service_test.go` for `ListAttendees` paging, including **one order's attendees sharing a name** — the tie that makes `created_at, name` ordering non-total today ([research.md R4](./research.md))
- [X] T029 [P] [US2] Extend `backend/internal/order/admin_cache_test.go` with the attendee-page key and invalidation cases

### Implementation for User Story 2

- [X] T030 [US2] Add `CountAttendeesAdmin` beside `ListAttendeesAdmin` in `backend/internal/order/queries/order.sql`, and add `LIMIT`/`OFFSET` plus the `a.id ASC` tiebreaker to `ListAttendeesAdmin`
- [X] T031 [US2] Run `cd backend && sqlc generate` and review `backend/internal/order/ordersql/order.sql.go`
- [X] T032 [US2] Add paging to `AttendeeFilter` in `backend/internal/order/dto.go`
- [X] T033 [US2] Rework `AdminService.ListAttendees`/`listAttendees` in `backend/internal/order/admin_service.go` to count, clamp, then page, returning `httpx.Page[AttendeeSummary]` — `ticketTypeNames` now resolves only the page's rows, which must stay a single batched call
- [X] T034 [US2] Parse paging via `httpx.BindPage` in `listAttendees` in `backend/internal/order/admin_handler.go`
- [X] T035 [P] [US2] Update `GET /api/v1/admin/attendees` in `api/openapi.yml` ([contracts/api.md §2](./contracts/api.md))
- [X] T036 [US2] Change `useAdminAttendees` in `frontend/lib/queries.ts` to `ListParams` → `Page<AttendeeSummary>`, with paging in `queryKeys.adminAttendees`
- [X] T037 [US2] Wire `useListParams` and `<Pagination>` into `frontend/app/(admin)/admin/attendees/page.tsx`, moving the event filter into the URL

**Checkpoint**: Both P1 lists paginate. This is a shippable increment on its own.

---

## Phase 5: User Story 3 - Events and Fees lists (Priority: P2)

**Goal**: Events and Fees paginate identically to the P1 lists, and the event filter
dropdown on Orders and Attendees keeps naming every event.

**Independent Test**: Seed more events (and separately, more fees) than one page holds, open
each menu, and confirm behavior identical to Orders — then confirm an event from the second
page is still selectable as a filter on the Orders page.

**⚠️ Ordering within this phase is not cosmetic**: the selector endpoint (T038–T044) must
land **before** the events list is paginated (T045 onward). Reversing them leaves the event
filter silently truncated to one page in between ([research.md R6](./research.md)).

### Selector endpoint — prevents the filter regression

- [X] T038 [P] [US3] Write failing tests in `backend/internal/event/admin_handler_test.go` for `GET /admin/events/options` — returns every event as id/name ordered by name, 401 unauthenticated, and is **not** shadowed by the `/admin/events/:id` route
- [X] T039 [US3] Add `ListEventOptions` (`SELECT id, name FROM events ORDER BY name, id`) to `backend/internal/event/queries/event.sql` and run `cd backend && sqlc generate`
- [X] T040 [US3] Add the `EventOption` DTO to `backend/internal/event/admin_dto.go`
- [X] T041 [US3] Add the repository read to `backend/internal/event/repository.go` and an uncached `ListEventOptions` to `backend/internal/event/admin_service.go` — deliberately not routed through `cache.Through` ([research.md R6](./research.md))
- [X] T042 [US3] Register `GET /admin/events/options` in `backend/internal/event/admin_handler.go` **above** the `/admin/events/:id` route, or `options` is parsed as an event id
- [X] T043 [P] [US3] Add the `/api/v1/admin/events/options` path to `api/openapi.yml` ([contracts/api.md §5](./contracts/api.md))
- [X] T044 [US3] Add `useAdminEventOptions` to `frontend/lib/queries.ts` and switch the event filter `<Select>` on `frontend/app/(admin)/admin/orders/page.tsx` and `frontend/app/(admin)/admin/attendees/page.tsx` over to it

### Events list pagination

- [X] T045 [P] [US3] Write failing tests in `backend/internal/event/admin_service_test.go` and `backend/internal/event/cache_integration_test.go` for paged `ListEvents` — including **two events sharing a `start_date`**, the tie that makes the current ordering non-total
- [X] T046 [US3] Add `CountEvents` beside `ListEvents` in `backend/internal/event/queries/event.sql`, add `LIMIT`/`OFFSET` and the `id DESC` tiebreaker, and run `cd backend && sqlc generate`
- [X] T047 [US3] Rework `Service.ListEvents` in `backend/internal/event/admin_service.go` to count, clamp, then page, returning `httpx.Page[EventAdminView]` through the fingerprinted `EventsAdminKey`
- [X] T048 [US3] Parse paging via `httpx.BindPage` in `adminListEvents` in `backend/internal/event/admin_handler.go`
- [X] T049 [P] [US3] Update `GET /api/v1/admin/events` in `api/openapi.yml` ([contracts/api.md §3](./contracts/api.md))
- [X] T050 [US3] Change `useAdminEvents` in `frontend/lib/queries.ts` to `ListParams` → `Page<EventAdminView>` with paging in the query key, and grep for every remaining caller before merging — a caller left expecting the whole catalogue now silently sees one page
- [X] T051 [US3] Wire `useListParams` and `<Pagination>` into `frontend/app/(admin)/admin/events/page.tsx`, ensuring that creating an event leaves the operator on a valid page of the refreshed list (FR-015)

### Fees list pagination

- [X] T052 [P] [US3] Write failing tests in `backend/internal/order/admin_service_test.go` for paged `Fees` — and assert it stays **uncached**, since `fees` is not in the `Families` registry and making it one would need a constitutional amendment
- [X] T053 [US3] Add `CountFees` beside `ListFees` in `backend/internal/order/queries/order.sql`, add `LIMIT`/`OFFSET` and the `id` tiebreaker to `ListFees`, and run `cd backend && sqlc generate`
- [X] T054 [US3] Rework `AdminService.Fees` in `backend/internal/order/admin_service.go` and `ListFees` in `backend/internal/order/repository.go` to count, clamp, then page, returning `httpx.Page[FeeAdminView]`
- [X] T055 [US3] Parse paging via `httpx.BindPage` in `listFees` in `backend/internal/order/admin_handler.go`, leaving the fee `POST`/`PUT`/`DELETE` contracts untouched
- [X] T056 [P] [US3] Update `GET /api/v1/admin/fees` in `api/openapi.yml` ([contracts/api.md §4](./contracts/api.md))
- [X] T057 [US3] Change `useAdminFees` in `frontend/lib/queries.ts` to `ListParams` → `Page<FeeAdminView>`, and wire `useListParams` and `<Pagination>` into `frontend/app/(admin)/admin/fees/page.tsx` — deleting the last fee on the last page must leave the operator on a page that has rows (FR-015, edge case)

**Checkpoint**: All four menus paginate consistently. SC-005 is now a judgement you can actually make by looking at all four.

---

## Phase 6: User Story 4 - Page size control and address self-correction (Priority: P3)

**Goal**: An operator can resize a page and land where they were, and any address a person
could type resolves to a sensible view.

**Independent Test**: Go to page 3, copy the address, open it in a fresh session and land on
page 3 with the same filter; change the page size and confirm the row you were looking at is
still on screen; navigate to `?page=999` and land on the last page with the address
corrected.

**Note**: FR-011's addressability was delivered by T012 in the foundation. This phase carries
the remainder — the size control (FR-012), the row-preserving resize, and the clamp
self-correction.

- [X] T058 [P] [US4] Extend `frontend/lib/use-list-params.test.ts` with the resize case at several page/size combinations and the "server clamped, rewrite the URL" case
- [X] T059 [US4] Implement the row-preserving page-size change in `frontend/lib/use-list-params.ts` — `floor(((oldPage-1)*oldSize)/newSize)+1` ([data-model.md §7](./data-model.md))
- [X] T060 [US4] Make the server's returned `page` authoritative on all four admin pages: when it differs from the URL, rewrite the URL with `router.replace` so a stale bookmark self-corrects rather than showing a page number that is not what was served (FR-013)
- [X] T061 [P] [US4] Extend `frontend/components/ui/pagination.test.tsx` for the page-size `Select` — the offered sizes, the default, and that changing it reports the new size to the caller

**Checkpoint**: All four user stories complete.

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T062 [P] Add `GET /api/v1/admin/events/options` to the admin endpoint list in `PRD.md` (§ around line 69) — PRD.md owns product scope and enumerates these endpoints
- [X] T063 [P] Re-read `SCHEMA.md` and confirm it is untouched: this feature adds no migration, and if that changes the migration and the `SCHEMA.md` update must land in the same commit
- [X] T064 Grep the frontend for any remaining consumer of the four changed hooks that still expects a bare array, and for any e2e helper still reading `data` as a list
- [X] T065 Run the SC-001 benchmark from [quickstart.md §5](./quickstart.md): ~10k orders and ~25k attendees, cache disabled, first page under 2s and flat as the row count doubles. If it misses, add the index **and** the `SCHEMA.md` update in one commit ([research.md R9](./research.md))
- [X] T066 Walk [quickstart.md §4](./quickstart.md) across all four menus, including the two specific regressions it names — covered by automated scenarios rather than by hand: the event dropdown by the e2e scenario "the event filter still lists every event once the event list is paginated", the four-menu consistency by "every list menu paginates the same way", and the delete-the-last-row case by `TestFeesClampAPageBeyondTheEnd` plus the `syncServedPage` wiring. A human eye over the visual result is still worth having before merge

**End-to-end acceptance (Constitution Principle VIII) — NOT optional.**

- [X] T067 Add a multi-page walk scenario to `e2e/specs/admin-console.spec.ts`: arrange orders through the real booking API, open Orders at a small `page_size`, walk every page and assert each order number appears exactly once — no repeats, no gaps (SC-004). Confirm it fails against the unpaginated endpoint before implementing
- [X] T068 [P] Add a "filter resets to page 1" scenario to `e2e/specs/admin-console.spec.ts` — from page 2, change the status filter, assert page 1 and a total consistent with the new filter (FR-010)
- [X] T069 [P] Add an out-of-range clamp scenario to `e2e/specs/admin-console.spec.ts` — navigate to `?page=999`, assert the last page renders and the address corrects itself (FR-013, SC-007)
- [X] T070 [P] Add a shared-address scenario to `e2e/specs/admin-console.spec.ts` — copy the address from page 2 with a filter applied, open it fresh, assert the same page and the same filter (SC-006)
- [X] T071 Add a paged cache-coherence scenario to `e2e/specs/cache-refresh.spec.ts` — a committed write is visible on the next read of the page it belongs to. **Arrange through the real API only**: a direct database write would not invalidate the cache, so a scenario seeded that way can pass against a stale cache and prove nothing
- [X] T072 Run the full suite green: `cd e2e && npm test`
- [X] T073 Run it once more with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test` (SC-008, Principle VII's kill switch)
- [X] T074 Run it once more with throttling off: `cd e2e && E2E_RATE_LIMIT_ENABLED=false npm test` (Principle VIII requires both throttling modes; this feature adds no throttle, so this is a no-regression check)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: depends on Setup — **blocks every user story**
- **US1 (Phase 3)**: depends on Phase 2 only
- **US2 (Phase 4)**: depends on Phase 2 only — independent of US1
- **US3 (Phase 5)**: depends on Phase 2. Its selector-endpoint block (T038–T044) touches the Orders and Attendees pages, so if US1/US2 are being built concurrently, coordinate on those two files
- **US4 (Phase 6)**: depends on Phase 2, and is only meaningfully testable once at least one list phase has shipped
- **Polish (Phase 7)**: depends on every story you intend to ship

### Within Each User Story

- Failing tests first, confirmed red, then implementation
- SQL → `sqlc generate` → repository/service → handler → OpenAPI → frontend hook → page
- The service change is the pivot: nothing above it compiles until the generated code exists

### Hard ordering constraints (violating these produces a silent defect, not a build error)

1. **T007 before any service change** — a service returning `Page[T]` under an unfingerprinted key would serve page 1 for every page from cache, and only when the cache is on
2. **T038–T044 before T045–T051** — paginating events before the selector exists truncates the event filter with no error
3. **T042's route order** — `/admin/events/options` above `/admin/events/:id`
4. **T019/T030/T046/T053's tiebreakers before any multi-page test is trusted** — without a total order the walk tests pass or fail on luck

### Parallel Opportunities

- T003 and T006 (different packages); T008, T009 and T011 (different frontend files); T013 and T014 (different e2e files)
- Within each story, the test tasks marked [P] are all different files and can be written together
- OpenAPI edits (T024, T035, T043, T049, T056) are [P] against their story's code but all touch `api/openapi.yml` — do not run them concurrently *with each other*
- US1 and US2 are genuinely independent and can be built by different people once Phase 2 lands
- T068–T070 are [P] with each other but all touch `admin-console.spec.ts`; T067 must land first since it establishes the seeding helper the others reuse

---

## Parallel Example: Foundational Phase

```bash
# Three independent test-first tracks, different packages and files:
Task: "Failing PageRequest/Page[T] tests in backend/pkg/httpx/page_test.go"          # T003
Task: "Failing cache fingerprint tests in backend/pkg/cache/cache_test.go"           # T006
Task: "Failing Pagination component tests in frontend/components/ui/pagination.test.tsx"  # T009
```

## Parallel Example: User Story 1 Tests

```bash
Task: "CountOrdersAdmin agreement tests in backend/internal/order/repository_test.go"     # T015
Task: "ListOrders paging + tie tests in backend/internal/order/admin_service_test.go"     # T016
Task: "Page-key and invalidation tests in backend/internal/order/admin_cache_test.go"     # T017
Task: "Param clamping tests in backend/internal/order/admin_handler_test.go"              # T018
```

---

## Implementation Strategy

### MVP (User Story 1 only)

1. Phase 1: Setup
2. Phase 2: Foundational — blocks everything
3. Phase 3: User Story 1
4. **STOP and validate**: Orders paginates; the other three menus are untouched and still
   return whole lists, which is exactly what they do today. Nothing is half-migrated.
5. Ship if you want to.

### Incremental Delivery

1. Setup + Foundational → primitives exist, nothing user-visible has changed
2. + US1 → Orders paginates → ship
3. + US2 → both P1 lists paginate → ship
4. + US3 → all four menus consistent, event filter preserved → ship
5. + US4 → resize and address self-correction → ship

Each increment leaves the console coherent: a list is either paginated and complete, or
unchanged from today. There is no state in which a list is paginated but its filter is
broken — that is what the T038–T044-before-T045 ordering buys.

### Parallel Team Strategy

After Phase 2: developer A takes US1, developer B takes US2. US3 waits or coordinates,
because its selector block edits the Orders and Attendees pages that A and B are holding.

---

## Notes

- `[P]` means different files and no dependency on an incomplete task
- Every task names its file; the ones that say "run `sqlc generate`" also say which
  generated file to review, because an unreviewed regeneration is where schema drift hides
- Confirm each failing test is actually red before implementing — a paging test that was
  never seen red proves very little, since the unpaginated endpoint returns a superset that
  naive assertions accept
- Commit after each task or logical group; stop at any checkpoint

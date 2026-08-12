---

description: "Task list for Per-Ticket Event Dates"
---

# Tasks: Per-Ticket Event Dates

**Input**: Design documents from `/specs/015-ticket-event-dates/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/api.md](./contracts/api.md)

**Tests**: Test tasks are included and are **not** optional here. Constitution Principle
VIII makes `e2e/` an acceptance gate, and this feature both changes covered flows and
fixes a defect in one. The Go and Vitest tiers remain required where they already apply
(AGENTS.md).

**Organization**: Grouped by user story so each is independently implementable and
testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Exact file paths in every description

## Path Conventions

Web application, per [plan.md](./plan.md): Go modular monolith under `backend/internal/<domain>/`,
Next.js under `frontend/`, Playwright acceptance suite in `e2e/`.

## Phase ordering note — US2 ships before US1

The spec lists display (US1) first, but authoring (US2) is sequenced first here. Both are
P1, so priority order is not violated; this orders *within* P1 by dependency. The spec
itself gives the reason: "Nothing in User Story 1 or 3 can be demonstrated until an admin
can set these dates." It also keeps every later e2e scenario arrangeable through the real
API, which Principle VIII requires — without US2 the only way to seed differing windows
would be a direct database write.

**Two sequencing traps, both load-bearing:**

1. **T014 breaks all e2e seeding the moment it lands.** Making `event_start`/`event_end`
   required means every `createTicketType` call in `e2e/support/api.ts` starts getting a
   400. T024 repairs the helpers and must land in the same change, not in Polish.
2. **The red-first e2e scenarios sit inside their story's phase, before the
   implementation that makes them pass** (T027/T028 for US1, T049/T050 for US3). Deferring
   them to Polish would make it impossible to see them fail, and Principle VIII is explicit
   that a regression test never seen red proves nothing.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Get the columns into the database with a total backfill

- [X] T001 Create `backend/migrations/000014_ticket_type_event_window.up.sql`: add `event_start`/`event_end` as nullable `timestamptz`, backfill with `UPDATE ticket_types tt SET event_start = e.start_date, event_end = e.end_date FROM events e WHERE e.id = tt.event_id`, then `SET NOT NULL` on both and add `CONSTRAINT ticket_types_event_window_chk CHECK (event_end >= event_start)`. Follow the three-step shape and comment style of `backend/migrations/000013_master_list_identity_and_flags.up.sql:61-75`; no `BEGIN;`, no `IF NOT EXISTS`, a `--` header naming spec 015, and a `COMMENT ON COLUMN` on each explaining that this is the admission window, not the sales window
- [X] T002 [P] Create `backend/migrations/000014_ticket_type_event_window.down.sql` dropping both columns (no `IF EXISTS` guard — matches the style of `000011`/`000012`/`000013`)
- [X] T003 Update the `ticket_types` block in `SCHEMA.md:35-48` with both columns and the constraint, committed **together with T001 and T002** (AGENTS.md, constitution Governance)
- [X] T004 Apply and verify: `docker compose run --rm migrate up`, confirm `docker compose run --rm migrate version` reports 14, and run both backfill sanity queries from [quickstart.md](./quickstart.md) — each must return 0

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Carry the columns from the database up to the repository layer on the shared paths

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [X] T005 Add `event_start`, `event_end` to five queries in `backend/internal/event/queries/event.sql`: `ListTicketTypesByEventID` (:14), `GetTicketTypeByID` (:20), `ListTicketTypesAdmin` (:110), `CreateTicketType` (:121), `UpdateTicketType` (:126)
- [X] T006 Run `cd backend && sqlc generate && git diff --exit-code internal/` — a non-empty diff means generated code was hand-edited or the queries drifted
- [X] T007 Extend `AdminTicketTypeParams` and the ticket-type row mapping in `backend/internal/event/admin_repository.go` with both fields
- [X] T008 Extend the public ticket-type row mapping in `backend/internal/event/repository.go` (around :76) with both fields

**Checkpoint**: The columns are readable and writable end to end at the repository layer

---

## Phase 3: User Story 2 - The admin states when each ticket applies (Priority: P1) 🎯 MVP enabler

**Goal**: An admin can set and see a ticket type's event window, distinct from its sales window, with containment against the parent event enforced and reschedules left unblocked.

**Independent Test**: Open the admin ticket-type form, confirm Event start/end fields are present and separately labelled, save a valid pair, reopen and confirm persistence. Confirm an end-before-start save and an outside-the-event save are both refused. Widen an event and confirm no warning; move an event and confirm the save succeeds with a warning naming the stranded ticket types.

### Tests for User Story 2

> **Write these first and confirm they fail**

- [X] T009 [P] [US2] Go test in `backend/internal/event/admin_dto_test.go`: `TicketTypeRequest.Validate` refuses a missing `event_start`/`event_end` with `VALIDATION_ERROR` and an end-before-start with `INVALID_DATE_RANGE`; equal endpoints are accepted (FR-002)
- [X] T010 [P] [US2] Go test in `backend/internal/event/admin_service_test.go`: `CreateTicketType` and `UpdateTicketType` each refuse a window outside the parent event's dates with `INVALID_DATE_RANGE` (FR-005)
- [X] T011 [P] [US2] Go test in `backend/internal/event/admin_service_test.go`: `UpdateEvent` **succeeds** when the new dates strand an existing ticket-type window, and leaves those windows untouched (FR-005a, FR-005c)
- [X] T012 [P] [US2] Vitest in `frontend/lib/schemas.test.ts`: `ticketTypeFormSchema` requires both event fields and refuses an end before a start

### Implementation for User Story 2

- [X] T013 [US2] Add `EventStart`/`EventEnd` to `TicketTypeAdminView` in `backend/internal/event/admin_dto.go:58-69`
- [X] T014 [US2] Add `EventStart`/`EventEnd` to `TicketTypeRequest` and its presence + ordering checks to `Validate` in `backend/internal/event/admin_dto.go:126-165`. **This is the task that starts 400-ing every e2e seed — T024 must land with it**
- [X] T015 [US2] Add the containment check to `CreateTicketType` in `backend/internal/event/admin_service.go:236-272`, reusing the parent event already loaded at :244 and currently discarded into `_`. Refuse with `apperr.CodeInvalidDateRange` and attach the event's bounds via `(*Error).WithData`
- [X] T016 [US2] Add the containment check to `UpdateTicketType` in `backend/internal/event/admin_service.go:274-307`. Unlike create, this method loads no event today and learns `EventID` only from the returned row — add the lookup **before** the write
- [X] T017 [US2] Thread both fields through `toTicketTypeView` and `AdminTicketTypeParams` construction in `backend/internal/event/admin_service.go`
- [X] T018 [P] [US2] Add `event_start`/`event_end` to `TicketTypeAdminView` in `frontend/lib/types.ts:341-354`
- [X] T019 [US2] Add `eventStart`/`eventEnd` to `ticketTypeFormSchema` in `frontend/lib/schemas.ts:60-83`, with a `superRefine` ordering check mirroring the existing sales-window one
- [X] T020 [US2] Add two `datetime-local` fields to `frontend/components/admin/ticket-type-form.tsx` — defaults at :54-55, submit mapping at :69-70, inputs after :138. Label them so it is clear the event start is when **admission opens**, not showtime (FR-006), since validation admits no tolerance
- [X] T021 [P] [US2] Show each ticket type's event window alongside the existing `On sale` line in `frontend/app/(admin)/admin/events/[id]/page.tsx:243-244` (FR-007)
- [X] T022 [US2] Add the stranded-window warning to the admin event page (FR-005b), derived in the frontend from `EventAdminDetail`, which already carries both the event's dates and its ticket types' windows — no new endpoint and no server round trip
- [X] T023 [P] [US2] Vitest for `frontend/components/admin/ticket-type-form.tsx`: both fields render, are labelled distinctly from the sales window, and round-trip through submit
- [X] T024 [US2] Add `eventStart`/`eventEnd` options to `createTicketType`, `createSellableEvent`, and date overrides to `createEvent` in `e2e/support/api.ts:98-181`, defaulting the ticket window to the seeded event's range so containment holds. **Required for the suite to run at all once T014 lands**

**Checkpoint**: An admin can author event windows; the e2e suite still seeds successfully

---

## Phase 4: User Story 1 - The buyer sees the date of the ticket they picked (Priority: P1) 🎯 MVP

**Goal**: Every surface naming the date of a ticket names that ticket's window; every surface naming the date of the event still names the event's.

**Independent Test**: Create an event spanning several days with two ticket types on different days. Buy one and confirm the Order Summary panel and issued ticket show that ticket's window. Buy both and confirm each line differs. Confirm the events list, landing-page "Dates" cell, and countdown are unchanged.

### Tests for User Story 1

> **T027 and T028 must be written and confirmed RED before T029 onward**

- [X] T025 [P] [US1] Go test in `backend/internal/event/service_test.go`: the public ticket list carries each type's own window
- [X] T026 [P] [US1] Go test in `backend/internal/order/public_service_test.go`: order detail carries the window per line, two lines with different windows differ, a package line spans its constituents, and `event.start_date` still returns the **event's** date
- [X] T027 [US1] Add a multi-day scenario to `e2e/specs/guest-purchase.spec.ts`: an event with two ticket types on different days, buy both in one order, assert each Order Summary line shows its own date. **Run it and confirm it fails** — against unfixed code both lines render the event's date
- [X] T028 [US1] Add an assertion to the same scenario that the events list, the landing page "Dates" cell, and the countdown still show the event's own dates (FR-012)

### Implementation for User Story 1

- [X] T029 [US1] Add `EventStart`/`EventEnd` to `TicketTypeSummary` in `backend/internal/event/dto.go:34-43` and populate them in the cached loader in `backend/internal/event/service.go:178-190`
- [X] T030 [US1] Add `tt.event_start`, `tt.event_end` to `ListTicketTypeDisplaysByIDs` in `backend/internal/event/queries/event.sql:50-58` — alias them distinctly, the query already selects `e.start_date AS event_start_date`
- [X] T031 [US1] Extend `ListPackageDisplaysByIDs` in `backend/internal/event/queries/event.sql:291-299` with a join through `package_tickets` → `ticket_types` and `MIN(tt.event_start)` / `MAX(tt.event_end)` plus a `GROUP BY` (FR-021). Both tables are event-domain, so the join stays inside the boundary
- [X] T032 [US1] Run `cd backend && sqlc generate && git diff --exit-code internal/`
- [X] T033 [P] [US1] Add both fields to `TicketTypeDisplayRecord` in `backend/internal/event/admin_repository.go:208-216`
- [X] T034 [P] [US1] Add the derived span to `PackageDisplayRecord` in `backend/internal/event/package_repository.go:236-244`
- [X] T035 [US1] Add `EventStart`/`EventEnd` to `order.TicketTypeDisplay` (`backend/internal/order/admin_service.go:22-30`) and `order.PackageDisplay` (:55-63), **alongside** the existing `EventStartDate`/`EventEndDate`, which keep meaning the parent event's dates. Do not merge the pairs
- [X] T036 [US1] Extend the two field-by-field copy loops in `backend/cmd/api/adapters.go:425-444` and `:446-465`. `EventLookup`'s four-method signature is unchanged
- [X] T037 [US1] Add `EventStart`/`EventEnd` to `PublicOrderItem` in `backend/internal/order/dto.go:150-157`. Leave `PublicOrderEvent` (:127-134) alone
- [X] T038 [US1] Populate the pair per line in `publicItems` in `backend/internal/order/public_service.go:286-312`, on both the ticket and package branches. Do not touch `eventOf` (:256-284) — it collapses to one event by design
- [X] T039 [P] [US1] Add `event_start`/`event_end` to `PublicOrderItem` (`frontend/lib/types.ts:167-176`) and `TicketTypeSummary` (:19-32)
- [X] T040 [US1] Update `frontend/components/order/order-summary-panel.tsx`: the per-line date at :110 reads `item.event_start`; the range at :83 becomes `[min(event_start), max(event_end)]` across items; the gate line at :89 uses `min(event_start)` (FR-009, FR-010). `formatDateRange` already collapses a same-day range to one date
- [X] T041 [P] [US1] Vitest for `frontend/components/order/order-summary-panel.tsx`: differing per-line dates, a single-day window rendering as one date, and the derived range and gate time
- [X] T042 [US1] Swap `e.start_date` for `tt.event_start, tt.event_end` in `ListTicketDetailsByOrderID` in `backend/internal/ticket/queries/ticket.sql:36-48`; `e.venue` stays — the venue is still the event's
- [X] T043 [US1] Run `cd backend && sqlc generate && git diff --exit-code internal/`
- [X] T044 [US1] Replace `StartDate` with `EventStart`/`EventEnd` on `notification.TicketDetail` (`backend/internal/notification/pdf.go:24-32`), update the copy in `backend/cmd/api/adapters.go:388-397`, and render the window at `pdf.go:139` and `backend/internal/notification/service.go:230` (FR-011)
- [X] T045 [P] [US1] Go test in `backend/internal/notification/pdf_test.go`: the printed ticket carries its own ticket type's window, and a same-day window renders as one date with a time range

**Checkpoint**: T027 now passes. Guest-facing display is correct; validation is untouched

---

## Phase 5: User Story 3 - A ticket presented on the wrong day is refused (Priority: P2)

**Goal**: The gate refuses a ticket outside its ticket type's window, distinctly from unknown and already-used, with no grace period and both endpoints inclusive.

**Independent Test**: Issue a ticket from a type whose window is past; present its code and confirm the out-of-window outcome with no "Mark used". Repeat with a window covering now and confirm it validates and admits exactly as before.

### Tests for User Story 3

> **T047 must be written and confirmed RED before T050 onward**

- [X] T046 [US3] Extend `e2e/support/api.ts` so a scenario can seed an event **and** ticket type whose window spans now — the default `+30d/+31d` event (`e2e/support/api.ts:110-111`) cannot host a ticket validatable today, and FR-005 containment means the event must move too, not just the ticket
- [X] T047 [US3] Add out-of-window scenarios to `e2e/specs/admin-console.spec.ts`: a ticket whose window has not begun and one whose window has passed each report the distinct outcome, name the window, and hide "Mark used". **Run and confirm RED against unfixed code** (Principle VIII)
- [X] T048 [P] [US3] Go tests in `backend/internal/ticket/service_test.go`: all five outcomes; precedence `INVALID` → `ALREADY_USED` → window → `VALID`; both endpoints inclusive; one moment before the start refused; `INVALID` discloses no window (FR-014, FR-018, FR-019)
- [X] T049 [P] [US3] Go test in `backend/internal/ticket/service_test.go`: `MarkUsed` refuses an out-of-window ticket with 409 and leaves it `ACTIVE` (FR-017)
- [X] T050 [P] [US3] Vitest in `frontend/components/admin/validation-result-card.test.tsx`: both new verdicts render their label and window, and neither shows "Mark used"

### Implementation for User Story 3

- [X] T051 [US3] Add `tt.event_start`, `tt.event_end` to `GetTicketDetailByCode` in `backend/internal/ticket/queries/ticket.sql:8-17` — the join onto `ticket_types` and `events` already exists, so this is a projection change only
- [X] T052 [US3] Run `cd backend && sqlc generate && git diff --exit-code internal/`
- [X] T053 [US3] Add `ResultNotYetValid = "NOT_YET_VALID"` and `ResultExpired = "EXPIRED"` to `backend/internal/ticket/dto.go:10-21`; add `EventStart`/`EventEnd` as `*time.Time` to `ValidationResult` (:71-89) and as values to `Detail` (:25-31)
- [X] T054 [US3] Insert the window check on the `ACTIVE` branch of the switch in `backend/internal/ticket/service.go:149-163`, which yields FR-018's precedence for free. Keep the `INVALID` field-nilling at :161-163 intact and nil the window there too (FR-019). Both endpoints inclusive, no tolerance
- [X] T055 [US3] Add the out-of-window guard to `MarkUsed` in `backend/internal/ticket/service.go:179-201`, **before** the guarded `UPDATE` — that single statement's concurrency guarantee must not grow a date predicate (FR-020)
- [X] T056 [P] [US3] Add both fields and the two new result values to `ValidationResult` in `frontend/lib/types.ts:407-418`
- [X] T057 [US3] Add the two verdicts to the map in `frontend/components/admin/validation-result-card.tsx:8-22`, render the window in the detail list, and confirm the existing `result === "VALID"` gate at :76-85 already hides "Mark used" for them
- [X] T058 [US3] Repair the two existing validation scenarios in `e2e/specs/admin-console.spec.ts:89` and `:144` to seed a window covering now. These go red as a **side effect** of T054 — that is expected breakage, not evidence T047 works

**Checkpoint**: T047 now passes. All three stories independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T059 Add a Go test for ticket-type create/update/delete cache invalidation in `backend/internal/event/cache_integration_test.go` — no such test exists today ([research.md](./research.md) D-008), and this feature adds two guest-visible fields to exactly that cached DTO
- [X] T060 [P] Add a scenario to `e2e/specs/cache-refresh.spec.ts`: editing a ticket type's event window is visible on the next guest read with no manual flush (FR-008)
- [X] T061 [P] Update `PRD.md:32` — ticket types now carry Event Start/End as well as Sales Start/End
- [X] T062 Walk the 11-item contract test checklist at the foot of [contracts/api.md](./contracts/api.md) and confirm each holds
- [X] T063 Run `cd backend && ./scripts/test.sh ./...` and `cd frontend && npx vitest run` green
- [X] T064 Run `cd e2e && npm test` green
- [X] T065 Run `cd e2e && E2E_CACHE_ENABLED=false npm test` green — this is how Principle VII's kill switch is verified, and the FR-008 freshness check is the one most worth running twice
- [X] T066 Walk [quickstart.md](./quickstart.md)'s three scenarios, including the reschedule ordering of US2 and the boundary table for strict-instant validation. Covered by automation rather than by eye: the reschedule ordering became an e2e scenario ("rescheduling an event warns about stranded ticket types instead of refusing"), because FR-005b's warning is a new user-visible flow and Principle VIII requires coverage for one; the boundary table is `TestValidateTreatsBothWindowEndpointsAsInclusive` + `TestValidateAllowsNoGracePeriodBeforeTheWindow`
- [X] T067 Add the mandatory post-deploy `POST /admin/cache/refresh` to this feature's release notes. No test will catch its absence: stale entries decode cleanly with `0001-01-01` rather than failing over to the database ([research.md](./research.md) D-002)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: depends on Setup — BLOCKS all user stories
- **US2 (Phase 3)**: depends on Foundational. Sequenced first within P1 because US1 and US3 cannot be arranged through the real API without it
- **US1 (Phase 4)**: depends on Foundational; needs US2 for a *visible* demo, though the backend half is independently testable
- **US3 (Phase 5)**: depends on Foundational; needs US2 to seed windows and T046 to seed one covering now
- **Polish (Phase 6)**: depends on all three stories

### Critical path

```
T001-T004 (migration)
  → T005-T008 (columns to repository)
    → T014 + T024 (required fields + e2e helpers, same change)
      → T027 red → T029-T044 → T027 green
        → T047 red → T051-T057 → T047 green → T058
          → T059-T067
```

### Within each user story

- The e2e scenario is written and seen RED before the implementation that satisfies it
- Queries → `sqlc generate` → repository records → domain DTOs → wire DTOs → frontend types → components
- Backend DTOs before frontend types: the wire shape is the contract

### Parallel Opportunities

- T002 alongside T001
- All four US2 tests (T009-T012) together
- T018, T021, T023 together — different frontend files
- T025, T026 together; T033, T034 together; T039, T041, T045 together
- T048, T049, T050 together; T056 alongside the backend work
- T060, T061 together

---

## Parallel Example: User Story 2

```bash
# All four tests first, together:
Task: "Go test TicketTypeRequest.Validate refusals in backend/internal/event/admin_dto_test.go"
Task: "Go test containment refusals in backend/internal/event/admin_service_test.go"
Task: "Go test UpdateEvent succeeds when stranding windows in backend/internal/event/admin_service_test.go"
Task: "Vitest ticketTypeFormSchema in frontend/lib/schemas.test.ts"

# Then the independent frontend surfaces, together:
Task: "TicketTypeAdminView fields in frontend/lib/types.ts"
Task: "Event window column in frontend/app/(admin)/admin/events/[id]/page.tsx"
Task: "Vitest for ticket-type-form in frontend/components/admin/ticket-type-form.test.tsx"
```

---

## Implementation Strategy

### MVP: Phases 1-4

US2 plus US1 together are the smallest shippable increment that delivers the value asked
for — an admin can state a ticket's day and a buyer sees it. US2 alone changes nothing a
guest can see; US1 alone has no way to produce differing windows through a supported path.

1. Phase 1: Setup — migration lands with `SCHEMA.md`
2. Phase 2: Foundational — CRITICAL, blocks everything
3. Phase 3: US2 — authoring
4. Phase 4: US1 — display
5. **STOP and VALIDATE**: run [quickstart.md](./quickstart.md) scenarios 1 and 2
6. Deploy — **flush the cache** (T067)

### Incremental Delivery

US3 is a clean second increment. It has the highest operational blast radius — a wrong
refusal turns away a paying attendee — so shipping it behind a proven display fix is
deliberate, and it is the reason the spec priced it P2 while both others are P1.

### Parallel Team Strategy

After Phase 2, US1's backend half (T029-T038, T042-T045) and US3's backend half
(T051-T055) are genuinely independent — different domains, different files. Both need US2's
T014/T024 first, so a single developer should carry Phase 3 to completion before the split.

---

## Notes

- `[P]` = different files, no dependencies on incomplete tasks
- Run `sqlc generate` after **every** `.sql` edit and verify with `git diff --exit-code internal/`
- Frontend work: read the relevant guide in `node_modules/next/dist/docs/` first — this Next.js (16.2.12) differs from training data (`frontend/AGENTS.md`)
- Never write order status, tickets, or payment state straight into the database from a test — arrange through the real API, because those writes are also what invalidate the cache
- Commit after each task or logical group; `SCHEMA.md` and the migration are one commit

---

# Tasks — Revision 2 (2026-08-12)

**Input**: [plan.md](./plan.md) revision 2, [spec.md](./spec.md) Session 2026-08-12
clarifications.

Everything above (T001–T067) shipped and is green in all three tiers. The 2026-08-12
clarification session produced four answers, three of which contradict that shipped code.
These tasks fix only that delta.

**Tests**: not optional. Two of the three corrections are user-visible changes to a flow
`e2e/` already covers, and the package-line defect is exactly the "bugfix in a covered
flow" case where Principle VIII demands a scenario seen red first.

**No migration, no `SCHEMA.md` change, no cache flush.** This revision touches the
order-detail read and frontend rendering only, and order detail is not a cacheable
surface under Principle VII.

## What is being undone

Three tasks from revision 1 are partially reversed. They were not wrong when written —
they encode decisions since changed — so the work is replacement, not repair:

| Shipped | Now | Why |
|---|---|---|
| T037/T038 put `event_start`/`event_end` on each line | `admission_starts: []` replaces them | a single value cannot express a Day 1 + Day 2 bundle |
| T040 derived the Event box range and gate time from the order's lines | both revert to `order.event.*` | the box is labelled "Event" (FR-009, reversed) |
| T031 derived a package span with `MIN`/`MAX` | the span is dropped; constituents are listed | FR-021a–c |

---

## Phase 7: Foundational (the wire shape)

**Purpose**: carry a list of admission starts per line instead of one collapsed pair

**⚠️ CRITICAL**: the frontend cannot render what the wire does not carry. This phase
blocks Phase 8.

- [X] T068 Add `ListPackageAdmissionStartsByIDs` to `backend/internal/event/queries/event.sql`: `SELECT DISTINCT pt.package_id, tt.event_start FROM package_tickets pt JOIN ticket_types tt ON tt.id = pt.ticket_type_id WHERE pt.package_id = ANY(sqlc.arg(ids)::uuid[]) ORDER BY pt.package_id, tt.event_start`. Both tables are event-domain, so the join stays inside the boundary
- [X] T069 Simplify `ListPackageDisplaysByIDs` in the same file: drop the `MIN`/`MAX` aggregates, the `COALESCE`, the two `LEFT JOIN`s and the `GROUP BY`, returning it to a plain `packages ⋈ events` row. The derived span it computed is no longer displayed anywhere
- [X] T070 Run `cd backend && sqlc generate && git diff --exit-code internal/`
- [X] T071 Replace `TicketEventStart`/`TicketEventEnd` with `AdmissionStarts []time.Time` on `PackageDisplayRecord` in `backend/internal/event/package_repository.go`, assembling it by grouping T068's rows by `package_id`
- [X] T072 Replace the same pair with `AdmissionStarts []time.Time` on `TicketTypeDisplayRecord` in `backend/internal/event/admin_repository.go` — a one-element slice holding the type's own `event_start`, so both line kinds present one shape to the order domain
- [X] T073 Replace the pair with `AdmissionStarts []time.Time` on `order.TicketTypeDisplay` and `order.PackageDisplay` in `backend/internal/order/admin_service.go`. Leave `EventStartDate`/`EventEndDate` alone — those are the event's dates and FR-009 now depends on them
- [X] T074 Update the two field-by-field copy loops in `backend/cmd/api/adapters.go` (~:425 and ~:446). `EventLookup`'s four-method signature is unchanged
- [X] T075 Replace `EventStart`/`EventEnd` with `AdmissionStarts []time.Time` (`json:"admission_starts"`) on `PublicOrderItem` in `backend/internal/order/dto.go`. Drop `event_end` rather than keeping it: nothing reads it once the Event box reverts, and an unread field invites a future consumer to read the wrong thing
- [X] T076 Fill the slice per line in `publicItems` in `backend/internal/order/public_service.go`, on both the ticket and package branches. Leave `eventOf` untouched
- [X] T077 Update the faithful test double in `backend/internal/order/admin_service_test.go` to copy `AdmissionStarts`, matching `adapters.go`

**Checkpoint**: the wire carries every day each line admits on

---

## Phase 8: User Story 1 - The buyer sees the date of the ticket they picked (Priority: P1)

**Goal**: a bundle line names every day it admits on, and the Event box goes back to naming the event.

**Independent Test**: buy a Day 1 + Day 2 bundle and confirm its single line reads both dates, while the Event box above still shows the parent event's full range. Add a third day and confirm the line collapses to a range.

### Tests for User Story 1

> **T079 must be written and confirmed RED before T081 onward**

- [X] T078 [US1] Add a bundle-seeding helper to `e2e/support/api.ts` that creates a package over two ticket types admitting on different days, so the scenario can arrange through the real API
- [X] T079 [US1] Add a scenario to `e2e/specs/guest-purchase.spec.ts`: buy a Day 1 + Day 2 bundle and assert its one line shows both dates. **Run it and confirm it fails** — against current code the line renders only the earliest date, which is the defect in the reported screenshot
- [X] T080 [P] [US1] Go test in `backend/internal/order/public_service_test.go`: replace `TestPackageLineSpansItsConstituentWindows` with one asserting the line lists every distinct constituent start, ascending. Update `TestOrderLinesCarryTheirOwnAdmissionWindows` and `TestOrderEventBlockStillCarriesTheEventsOwnDates` for the new field
- [X] T081 [P] [US1] Vitest in `frontend/lib/admission-dates.test.ts` covering the rule's truth table from [data-model.md](./data-model.md): one date, two dates, two after dedupe, three collapsing to a range, three identical collapsing to one, and empty input

### Implementation for User Story 1

- [X] T082 [P] [US1] Replace `event_start`/`event_end` with `admission_starts: string[]` on `PublicOrderItem` in `frontend/lib/types.ts`
- [X] T083 [US1] Create `frontend/lib/admission-dates.ts` exporting `formatAdmissionDates(starts: string[]): string`: format each instant, dedupe the **rendered strings** preserving order, join one or two with a comma, and pass three or more to `formatDateRange(first, last)`. Deduping the rendered value rather than the instant is what makes "distinct calendar date" true in the viewer's own timezone ([research.md](./research.md) D-010)
- [X] T084 [US1] In `frontend/components/order/order-summary-panel.tsx`: delete `admissionSpanOf`, revert the Event box range and the "Gate opens at" line to `order.event.start_date`/`end_date`, and render the per-line date through `formatAdmissionDates(item.admission_starts)`
- [X] T085 [US1] Replace the two cases in `frontend/components/order/order-summary-panel.test.tsx` that assert the reversed span behaviour ("spans the order's own lines rather than the parent event" and "opens the gate at the earliest line's start") with cases asserting the Event box now reads the event's dates. These tests are not broken — they encode a decision that has been reversed
- [X] T086 [US1] Add a panel case asserting a package line renders both of its days on one line

**Checkpoint**: T079 passes. The screenshot's "Test - Day 1 & 2" names both days

---

## Phase 9: Cross-Cutting — English dates

**Purpose**: FR-012a/b. Not tied to one story: it changes every date in both interfaces.

- [X] T087 Switch the five date/time formatters in `frontend/lib/format.ts` from `id-ID` to `en-GB`: `dateTimeFormatter` (:9) and the `day`, `dayMonth`, `full` formatters inside `formatDateRange` (:41-43), plus `formatDate` (:104). `en-GB` keeps the day-first order `id-ID` already produced, so nothing shifts in the layout
- [X] T088 Leave `currencyFormatter` (:1) and both compact-count formatters (:81, :85) on `id-ID` (FR-012b). `Rp 170.400` uses a dot thousands separator; under `en-GB` it becomes `Rp 170,400` and misstates the amount to an Indonesian buyer
- [X] T089 [P] Update the Indonesian-month assertion in `frontend/lib/format.test.ts:66` from `/Okt/` to `/Oct/`
- [X] T090 [P] Update the Indonesian-month assertion in `frontend/components/order/order-summary-panel.test.tsx:76` (`/1 - 3 Agu/i`) to its English form
- [X] T091 [P] Add a Vitest case to `frontend/lib/format.test.ts` pinning `formatCurrency` to `Rp 170.400` with a dot separator, so a future locale sweep cannot quietly relocalise money

**Checkpoint**: every date reads English; every price still reads Indonesian

---

## Phase 10: Verification

- [X] T092 Run `cd backend && ./scripts/test.sh ./...` green
- [X] T093 Run `cd frontend && npx vitest run` green, noting that `selection-summary.test.tsx > multiplies a line by its quantity` fails on a clean tree and is not this feature's
- [X] T094 Run `cd e2e && npm test` green
- [X] T095 Run `cd e2e && E2E_CACHE_ENABLED=false npm test` green
- [X] T096 Walk the 9-item revision 2 contract checklist in [contracts/api.md](./contracts/api.md)
- [X] T097 Walk [quickstart.md](./quickstart.md) scenarios 4, 5 and 6. Covered by automation rather than by eye: scenario 4 by the e2e "a bundle line names every day it admits on" plus the two panel bundle cases, scenario 5 by the e2e "Event box names the event, not the tickets", and scenario 6 by `format.test.ts`'s `/Oct/` assertion together with `formatCurrency stays Indonesian`

---

## Dependencies & Execution Order (revision 2)

### Critical path

```
T068-T070 (queries + sqlc)
  → T071-T077 (records → displays → wire)
    → T078 (bundle seeding) → T079 red
      → T082-T086 (frontend) → T079 green
        → T087-T091 (locale)
          → T092-T097
```

### Phase dependencies

- **Phase 7** blocks Phase 8: the frontend cannot render a list the wire does not carry
- **Phase 8** and **Phase 9** are independent in principle, but T090 touches the same
  panel test file as T085/T086, so run Phase 9 after Phase 8 to avoid a conflict
- **Phase 10** depends on both

### Parallel opportunities

- T080, T081 together — different languages, different files
- T082 alongside T083 — types and the new module are separate files
- T089, T090, T091 together once Phase 8 has settled the panel test file

---

## Parallel Example: User Story 1

```bash
# Tests for the new behaviour, together:
Task: "Go test for per-line constituent starts in backend/internal/order/public_service_test.go"
Task: "Vitest for the 1/2/range rule in frontend/lib/admission-dates.test.ts"

# Then the two independent frontend files:
Task: "admission_starts on PublicOrderItem in frontend/lib/types.ts"
Task: "formatAdmissionDates in frontend/lib/admission-dates.ts"
```

---

## Implementation Strategy (revision 2)

There is no MVP split here — the three corrections are one coherent change to one panel,
and shipping the wire change without the frontend would leave the panel reading a field
that no longer exists.

The one place to slow down is T079. The package line is a defect a buyer can see in a
covered flow, so Principle VIII wants its scenario observed failing before the fix. That
is also the cheapest possible proof that the fix addresses the reported screenshot rather
than something adjacent.

Phase 9 is safe to defer to a follow-up commit if the locale sweep turns out to touch more
snapshots than expected — it is independent of the package fix and of FR-009.

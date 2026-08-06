# Tasks: Single Visitor Form per Bundle

**Input**: Design documents from `/specs/010-bundle-single-form/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/order-forms.md](contracts/order-forms.md), [quickstart.md](quickstart.md)

**Tests**: Included — the design docs enumerate them explicitly (quickstart "Automated checks") and the change breaks existing assertions (`page.test.tsx`, `checkout_forms_test.go`), so test work is unavoidable. Write the listed tests before their implementation task where the ordering shows them first.

**Organization**: Phases 1–2 build the shared plumbing (migration, unit stamping, wire exposure); each user story phase then delivers an independently testable increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on an incomplete task)
- **[Story]**: US1 / US2 / US3 from spec.md — user story phases only

## Phase 1: Setup (Schema)

**Purpose**: The additive column everything else reads and writes.

- [X] T001 Create migration `backend/migrations/000011_attendee_package_unit.up.sql` + `.down.sql` per [data-model.md §1](data-model.md) (`ALTER TABLE attendees ADD COLUMN package_unit SMALLINT CHECK (package_unit IS NULL OR package_unit >= 1)` + column comment; down drops it)
- [X] T002 [P] Document `attendees.package_unit` in `SCHEMA.md` (attendees table definition + column semantics; constitution requires SCHEMA.md in the same change)
- [X] T003 Apply migrations and recreate the test DB: `docker compose up -d postgres migrate` then `cd backend && ./scripts/setup-test-db.sh` (depends on T001)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Booking stamps bundle-unit ordinals; the order read exposes them. No user story works without this.

**⚠️ CRITICAL**: Complete before any user story phase.

- [X] T004 [P] Add `PerUnitDemand map[uuid.UUID]int32` to `ExpandedItem` and populate it in `expandPackage` in `backend/internal/order/demand.go` (aggregate `Demand` stays `PerUnitDemand × Quantity` and keeps feeding quota; ticket lines leave it nil)
- [X] T005 Update `backend/internal/order/queries/order.sql`: `CreateAttendeeSlot` inserts `package_unit`; `ListAttendeeSlotsByOrderID` selects `a.package_unit` and orders by `a.package_id NULLS FIRST, a.package_unit, a.id` (research R6); regenerate with `sqlc generate` (depends on T003)
- [X] T006 Add `PackageUnit *int16` to `AttendeeRef` and `AttendeeSlotRecord` in `backend/internal/order/repository.go`; pass/scan it in `CreateAttendeeSlot` and `ListAttendeeSlotsByOrderID` (depends on T005)
- [X] T007 Rework the package-line slot-creation loop in `bookOnce` in `backend/internal/order/service.go`: for each unit `1..Quantity`, create that unit's per-unit composition of slots with `PackageUnit` set; ticket lines unchanged with nil (depends on T004, T006)
- [X] T008 Add `PackageID *uuid.UUID` (`package_id`) and `PackageUnit *int` (`package_unit`) to `TicketOrderSlot` in `backend/internal/order/dto.go` and fill them where `AttendeeSlotRecord` is mapped to the order-detail DTO (depends on T006)
- [X] T009 [P] Mirror the two new nullable fields on `TicketOrderSlot` in `frontend/lib/types.ts` (`package_id: string | null`, `package_unit: number | null`)
- [X] T010 Extend `backend/internal/order/booking_test.go`: a 1× bundle booking stamps `package_unit = 1` on each constituent slot with the package's per-unit composition; standalone slots stay NULL (extends `TestBookBundleCreatesSlotsPerConstituentWithPackageOrigin`; depends on T007)

**Checkpoint**: `GET /ticket/order/:order_id` serves `package_id`/`package_unit` per [contracts/order-forms.md §1](contracts/order-forms.md); backend tests green.

---

## Phase 3: User Story 1 - One Form per Bundle (Priority: P1) 🎯 MVP

**Goal**: A bundle unit of N tickets shows one form titled with the bundle name; its data lands identically on all N slots; N QR tickets still issue.

**Independent Test**: Book 1× 2-ticket bundle → order page shows buyer block + exactly one bundle-titled visitor form → checkout succeeds → both attendee rows identical → after sandbox payment, 2 distinct tickets.

### Tests for User Story 1

- [X] T011 [P] [US1] Write `frontend/components/order/slot-groups.test.ts` first (failing): standalone slot → own group titled `ticket_type_name`; slots sharing `(package_id, package_unit)` → one group titled `package_name` with `ticketCount` = slot count; bundle slot with `package_unit: null` → own group (legacy fallback, data-model §5)

### Implementation for User Story 1

- [X] T012 [US1] Implement `groupOrderSlots` in new `frontend/components/order/slot-groups.ts` per [data-model.md §5](data-model.md) (make T011 pass)
- [X] T013 [US1] Rework `frontend/components/order/visitor-form.tsx`: render one card per group (bundle groups titled with the bundle name + "N tickets" badge, replacing the ticket-type title); Zod `attendees` entries carry hidden `slot_ids` per group; on submit fan each group out to one `attendees[]` element per slot id while recording a payloadIndex→groupIndex map; route server `attendees[i].field` errors through that map (depends on T012, T009)
- [X] T014 [US1] Update `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: the 2-slot bundle fixture gains `package_id`/`package_unit`; assert ONE visitor card titled with the bundle name (buyer + 1 name inputs, not 3); assert the submitted body has 2 `attendees[]` entries with identical fields and both slot ids; assert a server error on `attendees[1].dob` lands on the single bundle form (depends on T013)
- [X] T015 [US1] Add same-unit consistency validation in `CheckoutOrder` in `backend/internal/order/service.go` after `matchVisitorsToSlots`: visitors mapped to slots sharing non-null `(package_id, package_unit)` must be field-identical; divergence → existing 400001 shape keyed `attendees[<payload idx>].<field>` with "All tickets in the same bundle must use the same visitor information." ([contracts/order-forms.md §2](contracts/order-forms.md); depends on T008)
- [X] T016 [P] [US1] Extend `backend/internal/order/checkout_forms_test.go`: divergent field within one unit → 400001 with the right field key; identical data accepted end-to-end; different visitors on DIFFERENT units of the same package accepted; slots with NULL `package_unit` exempt (depends on T015)
- [X] T017 [US1] Verify the ticket-count invariant: `cd backend && go test ./internal/order/... ./internal/ticket/...` — `TestGenerateForOrderIssuesExactlyOneTicketPerAttendee` must be untouched and green (spec FR-004; depends on T015, T016)

**Checkpoint**: US1 fully functional — a bundle order is one form, stored identically, 2 QRs issued.

---

## Phase 4: User Story 2 - Mixed Order of Bundle and Standalone Tickets (Priority: P2)

**Goal**: Bundle collapse applies only to bundle slots; standalone tickets keep one form each.

**Independent Test**: Book 1× bundle (2 tickets) + 1 standalone → exactly 2 forms (bundle-titled + ticket-titled), each feeding the right tickets.

### Implementation for User Story 2

- [X] T018 [P] [US2] Add a mixed-order case to `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: fixture with one 2-slot bundle unit + one standalone slot renders exactly 2 visitor cards with correct titles; submitted payload has 3 `attendees[]` entries — bundle pair identical, standalone independent (depends on T013)
- [X] T019 [P] [US2] Add a mixed-order case to `backend/internal/order/checkout_forms_test.go`: bundle slots identical + standalone slot different → accepted; all five stored values correct per slot (depends on T015)

**Checkpoint**: US1 and US2 both pass; standalone behavior provably unchanged.

---

## Phase 5: User Story 3 - Multiple Units of the Same Bundle (Priority: P3)

**Goal**: 2× the same bundle → one form per unit, distinguishable, different visitors allowed, 4 tickets issued.

**Independent Test**: Book 2× a 2-ticket bundle → 2 labeled forms → different visitors → each unit's tickets carry its own visitor; 4 QRs.

### Implementation for User Story 3

- [X] T020 [US3] Add unit ordinal labels in `frontend/components/order/slot-groups.ts` (+ render in `visitor-form.tsx`): when one `package_id` has >1 unit in the order, groups get `unitLabel` "Visitor 1", "Visitor 2", … (research R4; depends on T012, T013)
- [X] T021 [P] [US3] Add a multi-unit case to `backend/internal/order/booking_test.go`: 2× bundle of {Day-1 ×1, Day-2 ×1} → 4 slots stamped units 1,2, each unit exactly one Day-1 + one Day-2 (never two Day-1s in one unit; depends on T007)
- [X] T022 [P] [US3] Add a multi-unit case to `frontend/components/order/slot-groups.test.ts` and `page.test.tsx`: 4 slots across 2 units → 2 labeled forms; filling them differently fans out to 4 payload entries grouped correctly per unit (depends on T020)

**Checkpoint**: All three user stories independently green.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T023 [P] Remove the legacy spec-001 `checkoutSchema`/`slotCounts` block from `frontend/lib/schemas.ts` and its bundle tests from `frontend/lib/schemas.test.ts` — they assert the reversed N-forms rule (research R7); keep any schema still imported by components
- [X] T024 Run the full suites: `cd backend && go test ./...` and `cd frontend && npx vitest run` (depends on all story phases)
- [X] T025 Execute the [quickstart.md](quickstart.md) manual validation: US1–US3 flows in the running app, SQL spot-checks for unit stamping and ticket counts, legacy NULL-unit fallback, admin double-scan check (depends on T024)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)** → starts immediately; T003 needs T001
- **Phase 2 (Foundational)** → needs Phase 1 (sqlc regen needs the applied migration); blocks all stories
- **Phase 3 (US1)** → needs Phase 2
- **Phase 4 (US2)** → needs US1's T013/T015 (it tests the same code paths on mixed fixtures) — start after Phase 3's implementation tasks
- **Phase 5 (US3)** → needs US1's T012/T013; T021 only needs Phase 2
- **Phase 6 (Polish)** → after all desired stories

### Key task-level dependencies

- T005 → T006 → {T007, T008}; T004 → T007; T007 → T010
- T009 ∥ backend Phase 2 (frontend file)
- T011 → T012 → T013 → {T014, T018, T020}
- T008 → T015 → {T016, T019}
- T020 → T022

### Parallel Opportunities

- T002 ∥ T001 (docs vs SQL); T004 ∥ T005 (different files); T009 ∥ all backend Phase 2
- After Phase 2: frontend track (T011–T014) ∥ backend track (T015–T016) — different files, meet at T017
- T018 ∥ T019 (frontend vs backend test files); T021 ∥ T020/T022
- T023 ∥ any story phase (touches only legacy files)

## Parallel Example: User Story 1

```bash
# Track A (frontend): T011 → T012 → T013 → T014
Task: "Write slot-groups.test.ts grouping cases"
Task: "Implement groupOrderSlots in frontend/components/order/slot-groups.ts"
# Track B (backend), simultaneously: T015 → T016
Task: "Add same-unit consistency validation in backend/internal/order/service.go"
Task: "Extend backend/internal/order/checkout_forms_test.go with consistency cases"
# Converge: T017 full order+ticket test run
```

## Implementation Strategy

**MVP first (US1 only)**: Phases 1 → 2 → 3, then STOP and validate with quickstart US1 (one bundle → one form → 2 QRs). That alone delivers the user-visible value.

**Incremental delivery**: US2 and US3 are mostly test coverage plus the unit-label polish — each lands independently and can ship with US1 in the same release. Ship migration + backend + frontend together (single release train, contracts §3); legacy in-flight orders are covered by the NULL-`package_unit` fallback on both sides, so no backfill and no deploy ordering constraints beyond "migration before backend".

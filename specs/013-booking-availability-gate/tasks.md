---
description: "Task list for 013-booking-availability-gate"
---

# Tasks: Booking Availability Gate

**Input**: Design documents from `/specs/013-booking-availability-gate/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md),
[data-model.md](data-model.md), [contracts/availability.md](contracts/availability.md),
[quickstart.md](quickstart.md)

**Tests**: Not optional here. Constitution **Principle VIII** makes `e2e/` an acceptance
gate for the guest purchase journey, which this feature alters; it also requires a bugfix
scenario to be **seen red** before the fix. [contracts/availability.md](contracts/availability.md)
§6 makes the oversell guard mandatory, and SC-005/SC-007 are stated in terms of suite
reachability. Test tasks below are therefore requirements, not extras.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: `[US1]` / `[US2]` per [spec.md](spec.md); Setup, Foundational and Polish carry none

## Path Conventions

Web application per [plan.md](plan.md): Go modular monolith in `backend/`, Next.js App
Router in `frontend/`, Playwright in `e2e/`. **No `src/` directory in either tier** — paths
below are the real ones.

---

## Phase 1: Setup

**Purpose**: A trustworthy baseline. Without it, the red-first step in T008 cannot be
attributed to the defect rather than to a broken environment.

- [X] T001 Bring up the stack the runner does not own: `REDIS_PORT=6380 docker compose up -d postgres redis && docker compose run --rm migrate up` from the repository root. This feature adds **no** migration — the schema must match `main`.
- [X] T002 Record a green baseline on unmodified code across all three tiers: `cd backend && ./scripts/test.sh ./...`, `cd frontend && npx vitest run`, `cd e2e && npm test`. Note any pre-existing failure now; Principle VIII does not accept "unrelated failure" as an exemption later.

**Checkpoint**: Baseline known-green. Any red from here is this feature's doing.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The wire shape and the quota plumbing both user stories read. Every task here
is **behavior-neutral** — no endpoint exists yet, no guest-visible change — which is what
lets T008's red-first reproduction still be genuinely red after this phase completes.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T003 Add `QuotaRemaining int32` to `TicketTypeInfo` in `backend/internal/order/event_provider.go`. Document on the field that it is a lock-free snapshot for the advisory check only, and that using it inside `bookOnce` to gate a sale reintroduces the oversell `CheckAndDeductQuota` exists to prevent (Constitution Principle IV; Principle VII "never authoritative for inventory").
- [X] T004 Populate `QuotaRemaining` from `row.Quota` in `eventProviderAdapter.TicketTypeForCheckout` in `backend/cmd/api/adapters.go`. `GetTicketTypeByID` already selects `quota` — the adapter is currently discarding it, so this adds no query, no round trip, and no lock.
- [X] T005 Add the oversell guard regression test in `backend/internal/order/booking_test.go` per [contracts/availability.md](contracts/availability.md) §6: N goroutines booking the last seat of one ticket type concurrently yield exactly one success and N−1 `INSUFFICIENT_QUOTA`, with `ticket_types.quota` ending at 0 and never negative. It must pass **now**, against unchanged booking code — its job is to stay green forever, catching any future use of T003's field as a pre-check.
- [X] T006 [P] Add `AvailabilityRequest`, `AvailabilityDecision`, `AvailabilityReason` to `backend/internal/order/dto.go` with the field set in [data-model.md](data-model.md). `AvailabilityRequest.Validate()` reuses the existing `validateItemLines`. `AvailabilityReason.Code` is the **string** `apperr` code, never the numeric envelope code — `apperr.Numeric()` collides `TICKET_TYPE_NOT_ON_SALE`, `PACKAGE_NOT_ON_SALE` and `VALIDATION_ERROR` all onto `400001`.
- [X] T007 [P] Add matching snake_case `AvailabilityDecision` and `AvailabilityReason` types to `frontend/lib/types.ts`.

**Checkpoint**: Contract shape and quota plumbing exist; the app behaves exactly as before.

---

## Phase 3: User Story 1 — Availability is confirmed before the Terms are shown (Priority: P1) 🎯 MVP

**Goal**: Buy Ticket asks the server whether the selection can still be bought. The Terms &
Conditions dialog opens only on a clean answer.

**Independent Test**: Exhaust a ticket type's quota through the real purchase path in
another session, then press Buy Ticket. The dialog does not open. Repeat with an available
selection: the dialog opens and the existing journey runs to issued tickets unchanged.

### Tests for User Story 1 ⚠️

> **T008 is the Principle VIII red-first step. Do not implement anything in this phase
> until it has been run and observed failing.**

- [X] T008 [US1] Write the reproduction scenario in `e2e/specs/guest-purchase.spec.ts`: seed a sellable event, exhaust its quota **through the real API** (`support/api.ts` + `support/payment.ts` — never write order or payment state straight to the database), select the sold-out ticket, press Buy Ticket, assert `await expect(page.getByRole("dialog")).toBeHidden()`. Run `cd e2e && npx playwright test guest-purchase --grep "sold out"` and **confirm it fails because the dialog is visible**. A failure from a bad selector or bad seeding proves nothing — fix that and re-confirm.
- [X] T009 [P] [US1] Cover the evaluator in `backend/internal/order/availability_test.go`: a purchasable selection returns `available: true` with empty `reasons`; each reason code in [contracts/availability.md](contracts/availability.md) §1 is reachable; two independently-broken lines return **two** reasons, not the first; a bundle plus a standalone ticket on the same ticket type, affordable alone but not together, is refused (FR-005); an event with no authored terms yields `TERMS_MISSING` with `item_index: null`; a malformed request (nil `event_id`, `quantity: 0`, both ids set) is a **400**, not a decision.
- [X] T010 [P] [US1] Cover the gate in `frontend/components/booking/selection-summary.test.tsx`: a clean decision opens the terms dialog; a refused decision does not open it and renders the reason's message; the inert empty-selection rendering is unchanged.

### Implementation for User Story 1

- [X] T011 [US1] Implement `EvaluateAvailability` in `backend/internal/order/availability.go` following [contracts/availability.md](contracts/availability.md) §2: call the **existing, unmodified** `expandItem` per line, catching each `*apperr.Error` into a reason and continuing instead of returning; aggregate the surviving lines with `aggregateDemand`; compare each ticket type's demand against `QuotaRemaining`, recording one reason per contributing line; call `CurrentTerms` for the order-level `TERMS_MISSING` reason. Steps 3 and 4 run even when step 1 produced reasons. Wrap in a single `db.InTx` for snapshot consistency, and issue **no** `UPDATE`, **no** `FOR UPDATE`, and **no** `CheckAndDeductQuota` (§3).
- [X] T012 [US1] Add the handler and `RegisterAvailabilityRoute(g *echo.Group)` for `POST /ticket/availability` in `backend/internal/order/handler.go`. A refusal is `httpx.Respond(c, http.StatusOK, decision)` — HTTP 200, envelope `200000`, `data.available: false`. Only malformed input returns 4xx through `apperr`.
- [X] T013 [US1] Mount the route on its own limiter group in `backend/cmd/api/main.go`: `availabilityRate = 1.0`, `availabilityBurst = 10`, reusing `rateLimitWindow`. Do **not** share `bookGroup` — a guest who hits two refusals would be rate limited out of the recovery path User Story 2 specifies.
- [X] T014 [P] [US1] Add `useCheckAvailability` to `frontend/lib/queries.ts` posting to `/ticket/availability`, with `retry: false` for the same reason `useBookOrder` has it.
- [X] T015 [US1] Make `frontend/components/booking/terms-dialog.tsx` controlled: accept `open` / `onOpenChange`, drop `triggerClassName`, and remove the `DialogTrigger` that currently *is* the Buy Ticket button. Leave the terms fetch, agree, book, navigation latch, and `TERMS_CHANGED` refetch untouched.
- [X] T016 [US1] Migrate `frontend/components/booking/terms-dialog.test.tsx` to mount with `open={true}` instead of clicking the removed trigger. The assertions about terms loading, agreeing, booking, the retry latch and `TERMS_CHANGED` should survive verbatim — if they need rewriting, T015 changed more than intended.
- [X] T017 [US1] Rewrite the Buy Ticket control in `frontend/components/booking/selection-summary.tsx`: a real `<button>` reusing `BUY_TICKET_CLASS` that fires the check and opens `TermsDialog` only on `available: true`. Replace the now-false comment "this opens the Terms & Conditions rather than navigating".
- [X] T018 [US1] Split the helpers in `e2e/support/journey.ts`: a `buyTicket()` that presses the button and awaits the check, with `agreeToTermsAndBook()` layered on it. Rewrite the doc comment "'Buy Ticket' opens the terms gate rather than navigating" — it describes a flow that no longer exists.
- [X] T019 [US1] Add e2e scenarios 2 and 3 to `e2e/specs/guest-purchase.spec.ts`: a selection whose sale window has closed is refused at Buy Ticket; an event with no authored Terms & Conditions is refused before an empty dialog can render.
- [X] T020 [US1] Re-run T008 and confirm it now passes, and that the existing happy-path scenario still passes with no edit beyond T018's helper split.

**Checkpoint**: A guest can no longer read terms for a purchase that cannot happen. FR-001
through FR-005, FR-011 and SC-001/SC-002 are satisfied.

---

## Phase 4: User Story 2 — A refused purchase is recoverable (Priority: P2)

**Goal**: Every refusal names its own specific problem, the selection survives, and the
guest can adjust and retry without reloading.

**Independent Test**: Trigger each refusal reason in turn and confirm each produces its own
message with quantities intact; then lower the offending quantity and complete the purchase.

### Tests for User Story 2 ⚠️

- [X] T021 [P] [US2] Unit-test the message mapping in `frontend/lib/availability.test.ts`: every string code from [contracts/availability.md](contracts/availability.md) §1 maps to its own sentence, and an unrecognized code degrades to a safe fallback rather than rendering a raw code at the guest.
- [X] T022 [P] [US2] Extend `frontend/components/booking/selection-summary.test.tsx`: chosen quantities are unchanged after a refusal (FR-007); a second press while a check is in flight issues no second request (FR-008); closing the dialog and pressing again issues a **fresh** check rather than reusing the previous decision (FR-009); a transport failure shows "could not check" and leaves the dialog shut (FR-002, FR-012).

### Implementation for User Story 2

- [X] T023 [P] [US2] Create `frontend/lib/availability.ts` with a pure `reasonMessage()` mapping each string code to its guest-facing sentence, plus the transport-failure case ("we could not check", which is *not* a refusal). Kept out of React like `frontend/lib/selection.ts`, so it is testable directly.
- [X] T024 [US2] Wire it into `frontend/components/booking/selection-summary.tsx`: render every reason rather than the first, guard against a concurrent check while one is in flight, and discard any previous decision on each press.
- [X] T025 [US2] Point `agreementErrorMessage` in `frontend/components/booking/terms-dialog.tsx` at the same helper so a guest who meets `INSUFFICIENT_QUOTA` at the check and again at Agree reads the same sentence (FR-013). This is the surviving-race path from User Story 1 scenario 6, so it must stay reachable.
- [X] T026 [US2] Add the recovery e2e scenario to `e2e/specs/guest-purchase.spec.ts`: a refused guest lowers the offending quantity, presses Buy Ticket again, and completes the purchase through to issued tickets — proving FR-007 and FR-009 together in a real browser.

**Checkpoint**: FR-006 through FR-009, FR-012, FR-013 and SC-004/SC-006/SC-007 satisfied.

---

## Phase 5: Polish & Cross-Cutting Concerns

- [X] T027 [P] Update `ARCHITECTURE.md`: the flow chart at ~line 166 sends the guest straight from selection to "Agree to the T&C", and the sequence diagram at ~lines 200–230 shows `POST /api/v1/ticket/book` as the booking step's first server call. Both must show the availability step ahead of the terms.
- [X] T028 [P] Add `POST /api/v1/ticket/availability` to the "Public APIs" list in `PRD.md` §1.5, immediately before `POST /api/v1/ticket/book` — the order the guest now meets them in.
- [X] T029 Confirm `SCHEMA.md` is untouched and no file was added under `backend/migrations/`. A migration appearing here means the design was departed from.
- [X] T030 Verify `backend/cmd/api/architecture_test.go` is still green: the order domain still reaches event data only through `EventProvider`, and never imports `internal/event` (Constitution Principle II).
- [X] T031 Confirm the diff leaves `bookOnce` in `backend/internal/order/service.go` unchanged (FR-011). Every refusal path it had — `expandItem`'s windows, event scoping, `CheckAndDeductQuota`, the terms guard — must still be there. If the diff touches it, that needs explaining.
- [X] T032 Manual contract check from [quickstart.md](quickstart.md): `curl -X POST localhost:8080/api/v1/ticket/availability` with a sold-out ticket returns **HTTP 200** with `code: 200000` and `data.available: false`. A 4xx means the decision was modelled as an error and [research.md](research.md) D1 was not followed.
- [X] T033 [P] Run the backend tier green: `cd backend && ./scripts/test.sh ./...`.
- [X] T034 [P] Run the frontend tier green: `cd frontend && npx vitest run`.

**End-to-end acceptance (Constitution Principle VIII) — NOT optional:**

- [X] T035 Run the full suite green headless: `cd e2e && npm test`.
- [X] T036 Run it once more with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test`. This feature never reads the cache, so any difference between the two runs is a real finding, not noise.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: needs Phase 1. Blocks both stories. Deliberately behavior-neutral so T008's red-first reproduction stays valid after it.
- **User Story 1 (Phase 3)**: needs Phase 2.
- **User Story 2 (Phase 4)**: needs Phase 2. In practice it also needs T017 from US1 — there is no gate to attach messages to until the button exists (see below).
- **Polish (Phase 5)**: needs every story you intend to ship.

### User Story Dependencies

- **US1 (P1)**: independent. Ship it alone and the defect is fixed — a guest never reads terms for a purchase that cannot happen. Refusals would carry one plain message.
- **US2 (P2)**: **not fully independent of US1**, and the spec says as much: "The early refusal in User Story 1 is worth little if the guest is left staring at a dead end." US2 refines the refusal US1 introduces, so T024 edits the component T017 creates. This is a deliberate dependency, not an oversight — do not staff US2 before T017 lands.

### Within Each Story

- Tests first, and for T008 specifically: **seen red** before implementation.
- Backend evaluator → handler → route mount.
- `TermsDialog` controlled (T015) before `SelectionSummary` drives it (T017).
- `e2e/support/journey.ts` helpers (T018) before the scenarios that use them (T019, T026).

### Parallel Opportunities

| Set | Tasks | Why safe |
|-----|-------|----------|
| Foundational DTOs | T006, T007 | Different files (`backend/internal/order/dto.go`, `frontend/lib/types.ts`), no shared symbol |
| US1 tests | T009, T010 | Different tiers, different files |
| US1 client hook | T014 alongside T011–T013 | `frontend/lib/queries.ts` is untouched by the backend tasks |
| US2 tests | T021, T022 | Different files |
| Governance docs | T027, T028 | `ARCHITECTURE.md` vs `PRD.md` |
| Final tiers | T033, T034 | Independent test runners |

**Not parallel, despite looking it**: T015 and T017 both change how the terms dialog is
mounted, and T017 depends on T015's props existing. T023 and T024 touch a new file and a
component that imports it — sequential. T035 and T036 are the same suite twice and must be
run one after the other.

---

## Parallel Example: User Story 1

```bash
# After T008 has been confirmed RED, launch the remaining US1 tests together:
Task: "Cover the evaluator in backend/internal/order/availability_test.go"
Task: "Cover the gate in frontend/components/booking/selection-summary.test.tsx"

# During backend implementation, the client hook is independent:
Task: "Add useCheckAvailability to frontend/lib/queries.ts"
```

---

## Implementation Strategy

### MVP (User Story 1 only)

1. Phase 1 Setup → baseline green.
2. Phase 2 Foundational → contract shape and quota plumbing, no behavior change.
3. **T008 red.** Do not skip this, and do not write it after the fix. Principle VIII:
   "A regression test never seen red proves nothing."
4. Phase 3 → the gate.
5. **STOP and VALIDATE**: sold-out selections never reach the terms; the happy path is
   untouched; run `e2e` in both cache modes.

That is a shippable fix on its own.

### Incremental Delivery

1. Setup + Foundational → foundation ready.
2. US1 → validate → ship (MVP: the defect is fixed).
3. US2 → validate → ship (the refusal becomes recoverable rather than a dead end).
4. Polish → governance docs synced, both cache modes green.

### Parallel Team Strategy

Limited value here — this is a small feature with a real US1 → US2 dependency. The one
genuine split is backend (T011–T013) against frontend (T014–T017) once Phase 2 lands, with
[contracts/availability.md](contracts/availability.md) as the agreed shape between them.

---

## Notes

- **`ticket_types.quota` is REMAINING quota**, never an original allocation (AGENTS.md).
- **No cache call and no gateway call inside a transaction** — the availability check has
  neither, and must keep it that way.
- **Never write order status, tickets, or payment state straight into the database from a
  test.** Arrange through the real API; those writes are also what invalidate the cache, so
  going around them makes the setup lie.
- The check is advisory. It reserves nothing, and `available: true` is already a statement
  about the past by the time the client reads it. Booking stays the only authority.
- Commit after each task or logical group.

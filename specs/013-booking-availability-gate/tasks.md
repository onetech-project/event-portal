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

---
---

# Tasks: the 2026-08-19 amendment — one general refusal message

**Input**: [plan.md](plan.md) *Amendment* section, [spec.md](spec.md) Clarifications
Session 2026-08-19, [research.md](research.md) D8–D14,
[contracts/availability.md](contracts/availability.md) *Message buckets* and
*TermsDialog after the 2026-08-19 amendment*, [quickstart.md](quickstart.md) *Amendment*.

**Everything above this line is the delivered feature and is complete.** Task IDs continue
from T036 so the two lists never collide.

**Tests**: still not optional. This is a behaviour change to a covered flow *and* a bugfix
in one, so Principle VIII requires both the suite update in this same commit and a
scenario **seen red** first. That scenario is T038, and it runs before any implementation.

**The one-paragraph summary**: the server is left alone. It keeps returning every offending
line with its own code and sentence, and FR-006 reclassifies that payload as diagnostic.
The client stops rendering those sentences and shows one fixed message for every
availability refusal, at the check and at Agree alike.

---

## Phase A1: Setup

**Purpose**: a baseline the red-first step in T038 can be attributed to.

- [X] T037 Bring the stack up and record a green baseline across all three tiers before touching anything: `REDIS_PORT=6380 docker compose up -d postgres redis mailpit && docker compose run --rm migrate up`, then `cd backend && ./scripts/test.sh ./...`, `cd frontend && npx vitest run`, `cd e2e && npm test`. A pre-existing failure must be identified now, not discovered mid-amendment.

**Checkpoint**: three tiers green on unmodified code.

---

## Phase A2: Foundational (Blocking Prerequisites)

**Purpose**: the single classifier both stories depend on. Nothing renders correctly until
this exists, and it is the only place the wording lives.

⚠️ **Blocks every task below.**

- [X] T038 Write **only** the FR-013a reproduction in `e2e/specs/guest-purchase.spec.ts` and run it against unmodified code: the check passes, another guest takes the last seat via `e2e/support/api.ts:353-369 bookAsAnotherGuest`, the guest presses Agree. **Assert the message text before asserting the dialog is hidden** — a bare `toBeHidden()` can pass on the first poll and hand back a false green on the bug under test, the trap the guard comment at `guest-purchase.spec.ts:852-854` already warns about. Confirm it fails for **both** real reasons: the dialog stays open, and the text is `"Only fewer than N ticket(s) remain."` Do not implement until it is red. **This is the gate**: it runs before T039-T041, because T040 rewrites `frontend/lib/availability.ts` and both refusal paths already consume it (`selection-summary.tsx:96` calls `reasonMessages`, `terms-dialog.tsx:126` reaches `failureMessage`), so landing T040 first would change the guest-visible text at both points and this scenario would never be observed failing for the reason it claims. Principle VIII: "Confirm it fails against the unfixed code."
- [X] T039 Apply the two spec deltas [plan.md](plan.md) records before implementing against them. **FR-012a**: add the third carve-out in `specs/013-booking-availability-gate/spec.md` — a throttled check is neither an availability race nor either listed exception ([research.md](research.md) D10) — and edit the lead sentence, which currently reads "The **two** refusals that are not an availability race"; appending a third bullet without fixing that word leaves the requirement contradicting its own list. Quote the 429 sentence verbatim, as FR-012 quotes its own, so SC-005's "character for character" bar has something to check against. **FR-006**: reconcile its "explained after the fact from server-side records" clause with **T051**, which is the task that actually creates those records.
- [X] T040 Rewrite `frontend/lib/availability.ts`: export the fixed general message as a single constant (heading line + body line, exactly as FR-012 quotes it); replace the `reasonMessage` pass-through with a `code`-aware bucket classifier over the seven contract codes per [contracts/availability.md](contracts/availability.md) *Message buckets*; make `reasonMessages` collapse to at most one general message plus any carve-out sentences; add a booking-side classifier branching on the **numeric** `ApiError.code` per [research.md](research.md) D8 (`400002`/`404001`/`400001` → availability, everything else → not); word HTTP 429 with the sentence `terms-dialog.tsx` already uses (D10); route an unrecognised code to the availability bucket. Rewrite the module docstring — its stated rule "the SERVER's sentence wins" is the rationale for the behaviour this amendment removes.
- [X] T041 Rewrite `frontend/lib/availability.test.ts` against the new contract. The existing cases at `:23-31`, `:33-40`, `:42-45` assert pass-through wording and `:60-63` asserts that booking echoes the server sentence — the exact opposite of FR-013. Cover: each FR-012 code → the general message character for character; `TERMS_MISSING` and `VALIDATION_ERROR` → their own sentences; transport (`status: 0`) → its own sentence; 429 → its own sentence; an unknown code → the general message.

**Checkpoint**: `npx vitest run frontend/lib/availability.test.ts` green; no component touched yet.

---

## Phase A3: User Story 1 — the general message, at both points (Priority: P1)

**Goal**: an availability refusal reads the same fixed sentence whether it arrives at Buy
Ticket or at Agree, and at Agree the Terms dialog gets out of the way first.

**Independent Test**: exhaust a ticket type through the real API and press Buy Ticket — the
general message appears and the dialog never opens. Then pass the check, have another guest
take the last seat, and press Agree — the dialog closes and the *same* message appears in
the *same* place.

### Tests for User Story 1 ⚠️ write first — the red gate is T038, already run

- [X] T042 [P] [US1] Add book-leg failure coverage to `frontend/components/booking/terms-dialog.test.tsx`, which has none today — the only failure it injects is the `agreement` leg at `:181-208`. Cover: an availability-coded book failure closes the dialog and reports upward; a non-availability failure (409001, 429001, 500000) keeps today's in-dialog alert and Retry; a dialog reopened after an availability refusal shows no ticked checkbox, no Retry and no stale sentence (the [research.md](research.md) D9 regression).
- [X] T043 [P] [US1] Update the refusal-rendering tests in `frontend/components/booking/selection-summary.test.tsx`: a decision carrying several reasons renders **one** message; the alert keeps its accessible name `"Why this selection cannot be bought"`; a booking-time availability refusal renders in that same region.

### Implementation for User Story 1

- [X] T044 [US1] In `frontend/components/booking/terms-dialog.tsx`, classify the `book` leg's failure with T040's numeric classifier; on an availability refusal call the component's **own** `onOpenChange(false)` and report the refusal upward through a new callback prop. Do **not** let the parent close it by flipping `open` — base-ui fires `onOpenChange` only from `setOpen`, so a controlled-prop flip skips `handleOpenChange` (`:81-92`) and leaves `agreed`, `bookedOrderId`, `navigating`, `book.error` and `agreement.error` stale ([research.md](research.md) D9). Non-availability failures keep the existing in-dialog alert at `:168-172` untouched.
- [X] T045 [US1] In `frontend/components/booking/selection-summary.tsx`, collapse the refusal state to a single message, replace the `<ul>`/`<li>` render at `:196-215` with `AlertTitle` + `AlertDescription` (D11), keep `aria-label="Why this selection cannot be bought"` — four e2e callers reach the region through it — and wire T044's callback so an Agree-time refusal lands in this same region. Fixing the render also removes the duplicate-React-key bug at `:209`, where `key={message}` collides because `quotaShortfalls` emits one identical sentence per contributing line.
- [X] T046 [US1] Make T038 green, then rewrite the three existing e2e assertions that assert the retired wording in `e2e/specs/guest-purchase.spec.ts` — `:855`, `:899` and `:954` — to expect the general message. Leave `:928` (terms-missing) asserting its own sentence and add the negative half: it must **not** be the general message.
- [X] T047 [P] [US1] In `e2e/support/journey.ts`, add a sibling of `agreeToTermsAndBook` for the refusal path — do not modify it, it blocks on `waitForURL` and has 21 happy-path callers. Either fix or stop using `visibleQuotaText` (`:123-127`): it is uncalled and its locator matches the whole ticket list rather than one row, so it cannot scope the "no quota number" assertion T052 needs.

**Checkpoint**: US1 is independently shippable. The general message appears at both points, the dialog closes at Agree, and the happy path is untouched.

---

## Phase A4: User Story 2 — a refused guest is told what to do next (Priority: P2)

**Goal**: every refusal the guest can actually reach produces the right one of the four
sentences, and the page they are left on is the one the message tells them to act on.

**Independent Test**: trigger each refusal in turn — sold out, sale window closed, item
deleted, terms unauthored, server unreachable, throttled — and confirm the first three read
identically while the last three each read differently.

- [X] T048 [P] [US2] Extend `e2e/specs/guest-purchase.spec.ts` for SC-005: several offending lines in one selection render **one** message in a browser, and an item deleted between page load and Buy Ticket is refused with the general message — `e2e/support/api.ts:200-205 deleteTicketType` already exists for the arrangement. Neither is covered at any tier today.
- [X] T049 [P] [US2] Cover the two FR-012a carve-outs and the third from T039 as negatives — terms-unauthored, transport failure, and a throttled check each produce their own sentence and never the general one. Note the throttled check has **zero** coverage at every tier today, and `e2e/specs/rate-limit.spec.ts` never drives `/ticket/availability` despite its header comment implying it. Because the throttled case is a scenario whose entire subject is a throttle refusal, Principle VIII requires two things of it that the others do not need: it MUST **skip itself** when `E2E_RATE_LIMIT_ENABLED=false`, and it MUST be **isolated** so that draining the availability allowance cannot refuse an unrelated scenario. Throttle state is per-client and process-local, so an unisolated scenario poisons whatever runs next.
- [X] T050 [US2] Assert SC-004 as restated: a refusal navigates nowhere and clears nothing by itself, and the guest is left on the selection page with the quantities they entered. Do **not** assert the selection survives a reload — the amended FR-007 explicitly gives that up.

**Checkpoint**: every guest-reachable refusal reads correctly, and the carve-outs are pinned against future collapse.

---

## Phase A5: Backend — make FR-006 true (D13)

**Purpose**: the only functional backend change in the amendment. Everything else in Go is
comment corrections.

- [X] T051 [P] Log a refused decision at WARN with its stable string codes in `backend/internal/order/availability.go`, mirroring `backend/pkg/httpx/error_handler.go:39-44`. Amended FR-006 justifies keeping the per-line detail "so a refusal can be explained after the fact from server-side records", and there are none today: the endpoint writes no log and a `200` never reaches the error handler. Additive, outside every transaction, no response change.
- [X] T052 [P] Correct the retired rationales in Go comments — `backend/internal/order/availability.go:29-32`, `:96-98`, `:142-145`, `:178-179` all justify behaviour by FR-013 wording parity or by telling the guest every fault at once, both of which the amendment retires; and `backend/internal/order/dto.go:87-89` and `:111-113`, which state FR-006's retired guest-facing motive and call `Message` "the guest-facing sentence". **Leave `dto.go:105-109` exactly as it is** — the string-code rationale it states is what T040's classifier depends on and it survives the amendment intact.

**Checkpoint**: `cd backend && ./scripts/test.sh ./...` green, with every existing assertion unweakened.

---

## Phase A6: Polish & Cross-Cutting

- [X] T053 [P] Annotate the superseded rationales in `specs/013-booking-availability-gate/research.md` — D2's "Guarantees FR-013 wording parity by construction" (`:224` and the `:72-76` paragraph) and D6 (`:175`, "owns the refusal message") — pointing at D8–D14. The reuse of `expandItem` is no less load-bearing; what it now buys is verdict parity, not wording parity.
- [X] T054 Verify no remaining-quota number reaches the guest on either path: `grep -rn "Only fewer than" frontend/ e2e/` must find no assertion that a guest reads it, and no rendered surface must contain it. This is the negative half of FR-013 and SC-005.
- [X] T055 Confirm the Go tests that assert stable codes and diagnostic messages are still green and still **strict** — `backend/internal/order/availability_test.go`, `booking_test.go`, `handler_test.go`. FR-006 preserves that payload; weakening these to accommodate the UI change is the wrong reading of the amendment and must be rejected in review.
- [X] T056 Run the full suite in all four required modes: `cd e2e && npm test`, then with `E2E_CACHE_ENABLED=false` (Principle VII) and with `E2E_RATE_LIMIT_ENABLED` both ways (Principle IX). This amendment reads no cache and adds no throttle, so any difference between runs is a real finding.
- [X] T057 Confirm no governance sync is outstanding: `PRD.md` §1.5 and `ARCHITECTURE.md:237-244` describe only the mechanism — advisory endpoint, `200 { available, reasons[] }`, refusals collected not failed-fast — all of which survives. `SCHEMA.md` is untouched; there is no migration.

---

## Dependencies (amendment)

```text
T037 (baseline)
  └── T038 RED FIRST — must be observed failing before ANY implementation
        └── T039 (spec deltas) ── T040 (classifier) ── T041 (its tests)
                                   │
                                   ├── Phase A3 / US1 ── T042,T043 ── T044,T045 ── T046,T047
                                   │                                                          │
                                   └── Phase A4 / US2 ─────────────────────────────────────── (needs US1's render)
                                   
T051, T052 (backend) — independent of both stories, may run any time after T037
Phase A6 — last
```

**Story order**: US1 before US2. US2 asserts the carve-outs *against* the render US1 builds,
so it has nothing to attach to until US1 lands. US1 is shippable alone.

## Parallel opportunities (amendment)

- T042 and T043 — different test files, both after T041.
- T047 alongside T046 — `journey.ts` and the spec file are separate.
- T048, T049, T050 — three independent e2e additions once US1's render exists.
- T051 and T052 — Go, independent of the entire frontend track. A second person can take
  the whole backend phase in parallel with Phase A3.
- T053 and T054 — docs and a grep sweep.

## Implementation Strategy (amendment)

### MVP — User Story 1 only

1. T037 baseline.
2. **T038 red.** Two independent real failures, both expected. It comes *before* the
   classifier, not after — T040 would otherwise fix half the bug this scenario exists to pin.
3. T039 spec deltas → T040/T041 classifier.
4. T042/T043 component tests → T044–T047 → the message appears at both points and the
   dialog gets out of the way.
5. **STOP and VALIDATE**: both cache modes, happy path untouched, no quota number anywhere.

Shippable on its own — it is the whole of what was asked for.

### Then

US2 pins the carve-outs; A5 makes FR-006 true; A6 closes the docs and the modes.

### The two review lines that matter

- **Anything weakening a Go test is wrong.** FR-006 keeps the server's per-line payload;
  the tests asserting it are the reason a future collapse gets caught.
- **`dto.go:105-109` and `types.ts:118-134` must survive.** They explain why the stable
  string code is needed, and the new classifier depends on exactly that.

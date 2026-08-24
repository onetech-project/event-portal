---
description: "Task list for 022 Free Ticket Registration"
---

# Tasks: Free Ticket Registration Form

**Input**: Design documents from `/specs/022-free-ticket-registration/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: **Not optional here.** `AGENTS.md` and Constitution Principle VIII make the Go tier, the
Vitest tier and the `e2e/` suite required where they already apply. Test tasks below are therefore
ordinary tasks, not a bracketed extra.

**Organization**: Grouped by user story. Story phases map to spec.md priorities.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1–US5 from [spec.md](./spec.md)
- Exact file paths in every description

## Path Conventions

Web app: Go modular monolith in `backend/`, Next.js App Router in `frontend/`, Playwright in `e2e/`.

---

## Phase R: Clarification reversals (added 2026-08-20 — BLOCKING)

Two `/speckit-clarify` sessions reversed decisions that were already implemented. Until this
phase completes, the code contradicts the spec. Nothing else in this file may be trusted as
"done" for the areas these touch.

**R-A — the per-address rule is removed** (FR-023/023a/023b rewritten). Nothing in the
registration write path may be keyed on the email address.

**R-B — `ticket_types.is_registration_only` becomes `is_visible`** (FR-001/FR-001a). This is a
POLARITY INVERSION, not a rename: default flips `FALSE` → `TRUE`, filters flip sense, writes flip
value. Find-and-replace produces code that compiles and is backwards. Every site is re-read.

- [X] R001 Rewrite `backend/migrations/000016_*` as `000016_ticket_type_visibility.{up,down}.sql`:
      `is_visible BOOLEAN NOT NULL DEFAULT TRUE`, drop `idx_attendees_email_lower` entirely (it
      existed only for the deleted duplicate check), rewrite both column comments. Nothing is
      committed yet, so this is an edit in place, not a corrective 000017.
- [X] R002 Update `SCHEMA.md` in the same change (constitution requirement).
- [X] R003 `backend/internal/event/queries/event.sql`: rename the column in all six queries and
      INVERT the guest filter `AND NOT is_registration_only` → `AND is_visible`. Regenerate sqlc.
- [X] R004 `backend/internal/order/queries/order.sql`: delete `EmailHasIssuedTicketForEvent` and
      `LockRegistrationIdentity`. Regenerate sqlc.
- [X] R005 Event domain Go: `admin_dto.go`, `admin_repository.go`, `admin_service.go`,
      `package_service.go`, `repository.go`, `registration.go`, `dto.go`. Invert
      `admin_service.go:352` by hand — it is a compound already carrying a negation.
- [X] R006 Order domain Go: `event_provider.go`, `demand.go`, `registration_service.go`,
      `registration_repository.go`, `cmd/api/adapters.go`. Delete the duplicate-check block.
- [X] R007 Remove `CodeEmailAlreadyRegistered` / `409007` from `backend/pkg/apperr/apperr.go`.
- [X] R008 `backend/pkg/config/throttle.go`: `RATE_LIMIT_REGISTER_BURST` 3 → 10, rate unchanged.
      Confirm the refill-vs-idle-TTL startup check still passes (FR-034a).
- [X] R009 Tests: delete the three obsolete duplicate-rule tests; INVERT
      `TestConcurrentRegistrationsOfOneAddressYieldExactlyOne` rather than deleting it — it still
      earns its place proving the two submissions do not contend and quota stays exact. Fix the
      `admin_handler_test.go` wire default `false` → `true`.
- [X] R010 Frontend: `lib/api-client.ts`, `lib/types.ts`, `lib/queries.ts`, the register page's
      duplicate-email message branch.
- [X] R011 Downstream artifacts: `data-model.md`, `plan.md`, `research.md`, `contracts/api.md`,
      `quickstart.md`, this file, and the one `is_registration_only` line in
      `.specify/memory/constitution.md`.

---

## Phase R2: `orders.is_registration` removed (added 2026-08-20 — BLOCKING)

Fourth clarification. The stored origin marker was questioned and removed by explicit
direction; the distinction is now DERIVED from the ticket type the order's line
references. This required a **constitution amendment** — Principle IV had made the marker
part of the permission that admitted a second origin for `PAID`.

**Accepted cost, on the record:** a derivation reports what is currently true, not what
happened. Making an invitation ticket type purchasable again reclassifies every
historical order that used it, and a resend of an already-delivered registration would
then render a receipt for an order that never had a payment. Raised before the decision;
chosen anyway; pinned by a test rather than described.

- [X] R101 Amend the constitution to **v6.0.0**: withdraw "MUST be marked at creation",
      replace with the derivation, restate the obligation on revenue surfaces, and record
      the accepted cost in the Sync Impact Report.
- [X] R102 Propagate to `SCHEMA.md` and `ARCHITECTURE.md` in the same change (Governance).
- [X] R103 Remove `orders.is_registration` from migration `000016` (edit in place —
      nothing is committed) and verify the up/down round-trip.
- [X] R104 Replace the column with a derived `EXISTS` in `GetOrderByID`,
      `GetOrderByNumber`, `ListOrdersAdmin` and `ListAttendeesAdmin`; drop it from both
      CTE write queries. Regenerate sqlc.
- [X] R105 Update the derivation comments in `cmd/api/adapters.go` and
      `internal/order/repository.go` — including that the value can now change for an
      order that has already been delivered.
- [X] R106 Rewrite the seven test queries that keyed on the raw column.
- [X] R107 Add `TestDerivedRegistrationFlagFlipsWhenTheTicketTypeBecomesPurchasable`
      (FR-033b): assert the reclassification HAPPENS, so a later "fix" collides with the
      decision instead of quietly reversing it.
- [X] R108 Update `spec.md` (FR-033/033a/033b + clarification), `research.md` D4,
      `data-model.md`, `contracts/api.md`, this file, and the checklist notes.

---

## Phase R3: The read-through gate removed (added 2026-08-21 — BLOCKING)

Seventh clarification, and the **fourth reversal of an already-implemented decision** — this one
reversing a decision made in this same spec, five days after it was made. Phase 6 built a gate: the
booking dialog's checkbox was made unclickable and `Agree` withheld until the guest reached the
bottom of the document. That is now forbidden by the same requirement numbers.

**What replaces it** (spec Clarifications, Session 2026-08-21):

- The checkbox is a **control** again. The guest may tick it at any moment, having read nothing, and
  `Agree` follows the checkbox rather than the scroll position (FR-014c, FR-045).
- Reaching the end still ticks the box — **at most once per opening**, and never over the top of a
  deliberate untick (FR-045a, new).
- FR-014a is **unchanged**: the registration form's own checkbox still opens the modal. The document
  is still put in front of every registering guest; what they do once it is open is their business.
- FR-051 gains a client obligation: on `TERMS_CHANGED` the form clears its checkbox and **reopens the
  document itself**, without waiting for a click.

**Two things that make this cheaper than it looks, both worth knowing before starting:**

1. **`components/terms/terms-viewer.tsx` needs no change at all.** It reports one fact — the reader
   reached the end — and never owned what that fact authorised. Its `firedRef` latch already
   satisfies FR-045a. R207 exists to stop someone "fixing" it.
2. **`e2e/support/journey.ts` needs no code change.** Its two helpers already scroll to the end and
   then press `Agree`; scrolling still ticks the box. T059's claim that neither may click the
   checkbox is now false, but the code it produced still works.

> **Principle VIII, and which scenario actually proves anything.** The **manual-tick** scenario
> (R201) is the one that fails against the current dialog, because the shipped checkbox carries
> `onCheckedChange={() => {}}`. The auto-tick scenario from T053 passes both before and after — it is
> a regression guard, not evidence. Shipping only the latter would satisfy the letter of FR-048 and
> none of its purpose.

- [X] R201 Add the **manual-tick** scenario to `e2e/specs/guest-purchase.spec.ts`, against the
      **long** document fixture T054 added: open the booking terms dialog, do **not** scroll, click
      the checkbox, and assert `Agree` becomes available and books. **Run it and confirm it FAILS**
      against the current dialog — it must, because the checkbox cannot currently be ticked. Red
      before green (Principle VIII).
- [X] R202 Change `frontend/components/terms/terms-dialog-shell.tsx`: replace the `reachedEnd: boolean`
      prop with `agreed: boolean` + `onAgreedChange: (next: boolean) => void`; render
      `<Checkbox checked={agreed} onCheckedChange={onAgreedChange} />`; compute
      `actionable = agreed && action.ready !== false`. **Replace the no-op `onCheckedChange={() => {}}`,
      do not supplement it** — leaving it while wiring an `onClick` elsewhere yields a checkbox that
      answers a mouse and not a keyboard `Space`. Do **not** add `disabled`: base-ui renders
      `<button role="checkbox">` carrying `disabled:opacity-50`, which FR-044 forbids as a visible
      change ([terms-gate.md §7](./contracts/terms-gate.md)).
- [X] R203 In `frontend/components/terms/terms-dialog-shell.tsx`, delete the *"(please read the
      document)"* hint from the checkbox label.
      US5 scenario 3 requires that nothing tells the guest they must read first, and that
      parenthetical is the only copy on either surface that does.
- [X] R204 Rename `useReadThroughGate` → `useTermsAgreement` in
      `frontend/components/terms/terms-viewer.tsx`, returning `{ agreed, setAgreed, onReachedEnd }`
      where `onReachedEnd` sets `agreed = true`. **Keep the render-phase reset on `open` verbatim** —
      it is what makes reopening start unchecked, its comment explains why it is not an effect, and it
      is already correct. The rename is not cosmetic: a hook named for a gate is where someone re-adds
      one.
- [X] R205 [P] Rewire `frontend/components/booking/terms-dialog.tsx` (lines 91, 174) onto the new hook
      and props. Its own `handleOpenChange` reset is unaffected.
- [X] R206 [P] Rewire `frontend/app/(public)/events/[slug]/register/[ticketId]/page.tsx` (lines 91,
      291) onto the new hook and props. Note this page already keeps its **own** `agreed` state for the
      form's checkbox (line 85) — that one is the form's, FR-014a keeps it, and it stays. The modal's
      agreement is separate.
- [X] R207 **Confirm `frontend/components/terms/terms-viewer.tsx` needs no functional change**, and
      leave it alone. Update only the doc comment on line 51, which names `useReadThroughGate`. Its
      `firedRef` latch is FR-045a and must not be replaced by anything that re-syncs the checkbox with
      the scroll position — that would re-tick a box the guest deliberately cleared.
- [X] R208 In `frontend/app/(public)/events/[slug]/register/[ticketId]/page.tsx`, extend the
      `termsChanged` branch (line 175) to `setTermsOpen(true)` alongside the existing `setAgreed(false)`
      (FR-051). This is the only place in the feature where the app opens the terms modal rather than
      the guest, and the one a reader will implement backwards.
- [X] R209 [P] Update `frontend/components/terms/terms-viewer.test.tsx` for the renamed hook and its
      new shape, and add the FR-045a assertion: fire `onReachedEnd`, call `setAgreed(false)`, fire
      `onReachedEnd` again, assert `agreed` stays false.
- [X] R210 [P] Add Vitest coverage for the manual route in a new
      `frontend/components/terms/terms-dialog-shell.test.tsx`. It needs no layout and so belongs at
      this tier (research D10): ticking the checkbox makes `Agree` available, unticking removes it,
      and no rendered copy tells the guest to read first. Remember `Agree` is an `aria-disabled <span>`
      while unavailable — assert with `queryByRole` expecting null, never `toBeDisabled()`.
- [X] R211 [P] Update `frontend/components/booking/terms-dialog.test.tsx` for the new props.
- [X] R212 Correct the now-false comment inside `agreeToTermsAndBook()`
      ([e2e/support/journey.ts:149](../../e2e/support/journey.ts#L149)), which states the checkbox is
      no longer a control and that clicking it does nothing. **No code change** — both helpers scroll
      and press `Agree`, which still works.
- [X] R213 Add the FR-045a latch scenario to `e2e/specs/guest-purchase.spec.ts`: scroll to the end,
      assert the box ticked itself, untick it, scroll away from the end and back, assert it is **still
      unticked** and `Agree` still unavailable.
- [X] R214 [P] Update the registration-side terms scenarios in `e2e/specs/free-registration.spec.ts`
      (T062, T075): add the manual tick, and change the stale-`event_terms_id` assertion to require the
      form to **reopen the document itself** (FR-051) rather than merely clearing the checkbox.
- [ ] R215 Run all three tiers and confirm R201 now passes and nothing regressed:
      `cd frontend && ./node_modules/.bin/vitest run`, then `cd e2e && npm test`, then the cache-off
      and throttle-off matrix runs (T079, T080).
      **Partially done 2026-08-21, BLOCKED on R217.** Go tier green. Vitest green (484 tests, 45
      files). Every terms scenario green in a real browser — guest-purchase 27/27 in-file,
      registration agreement 4/4, and R201 was confirmed RED first. The full suite is **not** green
      in the default mode: one unrelated scenario is refused by the shipped booking throttle. It IS
      green with `E2E_RATE_LIMIT_ENABLED=false` (40/40 on the affected pair). See R217.
- [ ] R217 **Pre-existing, surfaced by R215 — not caused by Phase R3.** `e2e/` exceeds the shipped
      per-IP booking allowance within a single run, so one arbitrary scenario is refused with
      `429 RATE_LIMITED` on `POST /api/v1/ticket/book` instead of the refusal it asserts.
      **Evidence**: `[api]` logs exactly one 429 on that path during the failing run;
      `guest-purchase.spec.ts` alone passes 27/27, but fails after `free-registration.spec.ts` has
      run; excluding all four Phase R3 scenarios does not fix it — it just moves the failure to a
      different test (line 423 instead of line 1015); and `E2E_RATE_LIMIT_ENABLED=false` passes.
      **Mechanism**: `Book` is rate 0.33/s, burst 5
      ([throttle.go:121](../../backend/pkg/config/throttle.go#L121)), every request in a local run
      comes from one address, and `playwright.config.ts` deliberately gives the main API the
      *shipped* thresholds "so the guest journey is exercised against what actually ships". The
      suite has simply outgrown that budget. `rate-limit.spec.ts` already solved this for itself by
      driving a second API instance; nothing protects the other files.
      **This needs a decision, not a patch** — raising the e2e rig's booking burst reverses a
      recorded design choice, and pacing or retrying in the helpers would mask real refusals.
- [X] R216 Downstream artifacts, done 2026-08-21 by `/speckit-clarify` and `/speckit-plan`:
      `spec.md` (Session 2026-08-21 + FR-014a/c/d/f, FR-043c, FR-044, FR-045, **FR-045a new**, FR-046,
      FR-047, FR-048, FR-051, US5, SC-011, SC-013), `plan.md`, `research.md` D9/D10/D11,
      `contracts/terms-gate.md`, `contracts/api.md`, `quickstart.md`, this file, and the checklist
      notes. `data-model.md` needed nothing.

**Checkpoint**: both routes to agreement work and are separately asserted in a real browser; the
auto-tick cannot override a deliberate untick. **Reached 2026-08-21** for everything R3 owns —
R201–R214 done and verified. R215 is held open by R217, a pre-existing throttle-budget defect in the
suite that Phase R3 surfaced but did not cause.

---

## Phase 1: Governance Gate (BLOCKING — nothing else may start)

**Purpose**: This feature contradicts two pieces of binding text. Governance requires the amendment
to land *with* its sibling documents, and requires deviations to be justified before merge — not
retrofitted after the code exists.

> **⚠️ Do not skip to Phase 2 "to get started".** Implementing first makes shipped code contradict a
> NON-NEGOTIABLE principle, and the amendment then gets written to describe whatever was built.

- [X] T001 Amend `.specify/memory/constitution.md`: (a) Principle IV — carve out that a free
      registration reaches `PAID` without a gateway webhook, while keeping webhook-only absolute for
      any order that *has* a payment session; (b) Critical Data Flow Rules — scope the
      exactly-two-attachments rule to **paid** orders and state what a registration sends instead.
      Add a Sync Impact Report entry and bump the version (MAJOR — Principle IV's invariant is
      redefined, not merely expanded).
- [X] T002 [P] Update `PRD.md` §1.4 for the registration delivery shape (one attachment, no receipt)
      and the free-registration acquisition path.
- [X] T003 [P] Update `ARCHITECTURE.md`: the registration write path, its post-commit fulfilment, and
      the fact that `PAID` now has two origins.
- [X] T004 Verify the governance gate: constitution, `PRD.md`, `ARCHITECTURE.md` and `SCHEMA.md` are
      mutually consistent, and the Sync Impact Report records `SCHEMA.md` as "no impact from the
      amendment itself" (its change rides with the migration in T005–T007).

**Checkpoint**: Deviations are legal. Implementation may begin.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Schema, config, the cross-domain seam, and the shared Terms & Conditions component —
all of which more than one story depends on.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

### Schema

- [X] T005 Create `backend/migrations/000016_ticket_type_registration_only.up.sql`: add
      `ticket_types.is_visible BOOLEAN NOT NULL DEFAULT TALSE`, add
      ~~`orders.is_registration`~~ (REMOVED 2026-08-20 — derived instead, Principle IV v6.0.0), add
      `CREATE INDEX idx_attendees_email_lower ON attendees (lower(email))`, and `COMMENT ON COLUMN`
      for both booleans. Follow 000014's style — no explicit `BEGIN`/`COMMIT` (000013–000015 omit it;
      golang-migrate wraps postgres migrations itself). **No CHECK tying the flag to `price`** (FR-003).
- [X] T006 Create `backend/migrations/000016_ticket_type_registration_only.down.sql` dropping both
      columns and the index.
- [X] T007 Update `SCHEMA.md` for all three objects — **same commit as T005/T006**, per
      `AGENTS.md` "Things that bite" and the constitution's Governance section.
- [X] T008 Verify the migration replays from empty: `docker compose run --rm migrate up`, then
      `backend/scripts/setup-test-db.sh` (which replays from zero — a migration that only works
      against a live database breaks the Go tier).

### Generated queries

- [X] T009 Add `is_visible` to exactly six queries in
      `backend/internal/event/queries/event.sql`: `ListTicketTypesByEventID`, `GetTicketTypeByID`,
      `ListTicketTypesAdmin`, `GetTicketTypeAdmin`, `CreateTicketType`, `UpdateTicketType`.
      **Do NOT touch `ListTicketTypeIDsByEventID`** — it feeds the `DeleteEvent` guard, and filtering
      it turns a required `400` into a raw FK violation (research D17).
- [X] T010 ~~Add `is_registration` to the `orders` INSERT~~ — SUPERSEDED: the column was removed and the flag is now DERIVED in the delivery read in
      `backend/internal/order/queries/order.sql`, and expose it on the notification domain's
      `OrderForDelivery` projection.
- [X] T011 Run `sqlc generate` in `backend/`. **Expect a zero diff until T009/T010 land** —
      `sqlc.yaml` sets `omit_unused_structs: true` and row structs are per-query, so the migration
      alone changes nothing generated (research D21).

### Config, errors, seam

- [X] T012 [P] ~~Add `CodeEmailAlreadyRegistered`~~ — RETIRED by R007 (FR-023 removed the rule); 409007 left unassigned in
      `backend/pkg/apperr/apperr.go` **and register its numeric value** — the default arm is
      `status * 1000`, so an unregistered code renders `409000` that no client or test knows.
- [X] T013 [P] Add `Register ThrottlePolicy` to `backend/pkg/config/throttle.go` with
      `RATE_LIMIT_REGISTER_{ENABLED,RATE,BURST}` defaulting to `true / 0.2 / 3`, and add it to
      `ratePolicies()` so it joins startup validation and the startup report.
- [X] T014 [P] Add a config test in `backend/pkg/config/throttle_test.go` asserting the defaults and
      that `RATE_LIMIT_IDLE_TTL` still strictly exceeds the longest full-burst refill window with the
      new surface enabled (startup *refuses* otherwise, which would take the whole e2e suite down).
- [X] T015 Thread `is_visible` across the domain boundary: `event.TicketTypeRow` →
      `eventProviderAdapter` in `backend/cmd/api/adapters.go` → `order.TicketTypeInfo` in
      `backend/internal/order/event_provider.go`. **No direct import either way** —
      `cmd/api/architecture_test.go` enforces it.

### Shared Terms & Conditions component

- [X] T016 Create `frontend/components/terms/terms-viewer.tsx` per
      [contracts/terms-gate.md](./contracts/terms-gate.md): header, sanitized body, scroll container,
      loading/error states, and the read-through gate. Primary mechanism is an
      `IntersectionObserver` sentinel; secondary is a **non-zero-tolerance** scroll computation;
      `ResizeObserver` re-evaluates; non-scrollable content satisfies immediately (FR-014d).
      Give the scroll region `tabIndex={0}` and an accessible name — without it `End`/`PageDown`
      never reach it and FR-046 fails outright.
- [X] T017 Create `frontend/components/terms/terms-viewer.test.tsx` covering everything *around* the
      gate by driving `onReachedEnd` directly. **Do not assert on scroll metrics or observer
      callbacks**: the test DOM is happy-dom, where `scrollHeight` is a getter-only `0` and both
      observers are registered but inert stubs, so such a test passes while verifying nothing
      (research D10).
- [X] T018 [P] Fix `frontend/components/layout/site-nav.tsx`: its hide rule is
      `pathname.startsWith("/events")`, which the new singular `/event` segment does not match, so the
      registration page would render the site nav every purchase page hides.

**Checkpoint**: Schema, config, seam and shared component ready — user stories can begin.

---

## Phase 3: User Story 1 — Invited guest registers and receives a free e-ticket (P1) 🎯 MVP

**Goal**: The whole point of the feature — form → validation → review → submit → free e-ticket by email.

**Independent Test**: Publish an event with a registration-only ticket type, open its form link,
submit a valid form, and confirm a one-attachment e-ticket email arrives whose code validates as
`Valid` in admin ticket validation.

### Backend — write path

- [X] T019 [US1] Add `RegisterRequest` / `RegisterResponse` and `RegisterRequest.Validate` to
      `backend/internal/order/dto.go`. **Reuse `visitorPhonePattern` and `visitorPhoneMessage`
      verbatim** (dto.go:251-253) and `emailShaped`, so the identical input fails identically on both
      forms. Report every offending field in one `400` field map (FR-024).
- [X] T020 [P] [US1] Add cases to `backend/internal/order/dto_test.go` for every rule in
      [data-model.md §3](./data-model.md): empty name, malformed email, `+62…` phone rejected, future
      DOB, unknown gender, `agreed:false`, and a multi-field failure returning all fields at once.
- [X] T021 [US1] Declare `TicketIssuer` and `TicketDeliverer` interfaces in
      `backend/internal/order/event_provider.go`, mirroring `payment.TicketIssuer` /
      `payment.TicketDeliverer`. `order` must **not** import `payment`.
- [X] T022 [US1] Add repository methods in `backend/internal/order/repository.go`:
      `CreateRegistrationOrder` (total 0, subtotal 0, status `PAID` resolved by name in-statement,
      `buyer_*` **on the INSERT** — `UpdateOrderBuyer` is `PENDING`-guarded
      and cannot patch afterwards), `CreateRegistrationAttendee` (fully filled, no empty-slot phase),
      and the FR-023 duplicate-email read.
- [X] T023 [US1] Implement `RegisterFree` in new
      `backend/internal/order/registration_service.go`, modelled on `bookOnce`
      ([service.go:154](../../backend/internal/order/service.go#L154)), following the sequence in
      [contracts/api.md §2.1](./contracts/api.md). Pre-`BEGIN`: resolve type/event, compare
      `event_terms_id` against `EventProvider.CurrentTerms`, resolve genders. Inside `db.InTx`:
      advisory lock, duplicate check, `CheckAndDeductQuota`, order, item, attendee,
      `cache.InvalidateAfterCommit(cache.Orders(), cache.Event(eventID))`.
- [X] T024 [US1] In `backend/internal/order/registration_service.go`, take `pg_advisory_xact_lock`
      on a hash of `(event_id, lower(email))` as the **first** statement in the transaction, before the
      quota deduction. A read-then-write duplicate check does
      not serialise — two concurrent submissions of one address both pass it and both insert
      (research D7, SC-002).
- [X] T025 [US1] Implement `fulfillRegistrationAsync` in `registration_service.go`, mirroring
      `payment.fulfillAsync` ([service.go:831-861](../../backend/internal/payment/service.go#L831-L861)):
      `context.WithTimeout(context.WithoutCancel(requestCtx), …)`, a `WaitGroup` the service owns and
      exposes for graceful drain, issuance **then** delivery, and stop on issuance failure.
- [X] T026 [US1] Add `RegisterRegistrationRoutes` to `backend/internal/order/handler.go`:
      `GET /ticket/register/:ticket_type_id` (unthrottled read group) and
      `POST /ticket/register/:ticket_type_id`. Collapse all six §1 refusal conditions onto one
      `404 TICKET_TYPE_NOT_FOUND` with one identical message (FR-012), logging the real reason
      server-side.
- [X] T027 [US1] Wire it in `backend/cmd/api/main.go`: a `registerGroup` behind
      `rateLimit(cfg.Throttle.Register.Active(...), …)`. **Construct the group in both modes** — a
      disabled throttle is substituted, never skipped, or unmatched `/api/v1` paths answer differently
      between modes (Principle IX). Add the order→ticket and order→notification adapters to
      `backend/cmd/api/adapters.go`.
- [X] T028 [US1] Add `backend/internal/order/registration_service_test.go` (database-backed):
      the happy path writes exactly the rows in [data-model.md §2](./data-model.md); quota decrements
      by one; a refusal writes nothing and moves no quota; **two concurrent submissions of the same
      email yield exactly one ticket**; a stale `event_terms_id` returns `TERMS_CHANGED`.

### Backend — delivery

- [X] T029 [US1] Branch `SendTicketEmail` in `backend/internal/notification/service.go` on the order's
      registration marker: render **only** the tickets PDF, attach one document, and use a subject
      without "E-receipt". The existing `order.Status != "PAID"` guard (service.go:173) already passes.
      **The paid path must still fail closed** — if either document fails to render, a purchase sends
      nothing.
- [X] T030 [US1] Branch the email **body** too, in `backend/internal/notification/service.go`.
      `emailTicketDetails` renders a "Total Payment" line
      that would read `IDR 0` for a registration, violating FR-036 — the attachments are not the only
      place money appears (research D5).
- [X] T031 [P] [US1] Add `backend/internal/notification/service_test.go` cases: a registration order
      produces **exactly one** attachment and no monetary string anywhere in body or subject; a paid
      order still produces exactly two, in receipt-then-tickets order.

### Frontend

- [X] T032 [US1] Add `useRegistrationForm` (GET) and `useSubmitRegistration` (POST) to
      `frontend/lib/queries.ts`, plus types in `frontend/lib/types.ts`. `retry: false` on the
      mutation — a retry could double-register.
- [X] T033 [US1] Create `frontend/app/(public)/events/[slug]/register/[ticketId]/page.tsx`: the form
      (full name, email, phone, gender, DOB), the single agreement checkbox that opens
      `TermsViewer` in a modal (FR-014a–FR-014f), the "Registration Details" review dialog, and the
      submit. No price, fee or monetary figure anywhere (FR-015).
- [X] T034 [US1] Create `frontend/app/(public)/events/[slug]/register/[ticketId]/success/page.tsx`
      rendering the approved copy **verbatim**: "Registration Complete!" and "Thank you for
      registering. Your complimentary invitation e-ticket for *[event name]* has been successfully
      generated and sent to your email." No email address, no reference (FR-039). Reload-safe, and it
      renders the same panel to anyone who types the address (FR-039d).
- [X] T035 [P] [US1] Add `frontend/app/(public)/events/[slug]/register/[ticketId]/page.test.tsx`: submit disabled until valid **and** agreed;
      server field errors surface per field; the checkbox cannot be ticked without the modal; the
      review dialog shows back exactly what was typed.
- [X] T036 [US1] Surface the `TERMS_CHANGED` refusal in
      `frontend/app/(public)/events/[slug]/register/[ticketId]/page.tsx`: clear the agreement control,
      re-present the current document, and require the read-through again before resubmission
      (FR-051).

**Checkpoint**: A guest can register and receive a free e-ticket. **MVP complete.**

---

## Phase 4: User Story 2 — Registration-only types are invisible to the purchase path (P2)

**Goal**: A complimentary ticket type cannot leak into the paid catalogue.

**Independent Test**: Flip the flag on a ticket type; confirm it vanishes from the guest ticket list
and the package picker while still appearing, labelled, in the admin ticket-type list.

- [X] T037 [US2] Filter registration-only types out of the guest list in
      `backend/internal/event/service.go` / `ListTicketTypesByEventID` (FR-006).
- [X] T038 [US2] Add `IsRegistrationOnly` to `TicketTypeAdminView` and `TicketTypeRequest` in
      `backend/internal/event/admin_dto.go`, and thread it through `admin_service.go` and
      `admin_repository.go` (FR-004).
- [X] T039 [US2] **Guard the full-replace hazard.** `UpdateTicketType` replaces every column
      unconditionally ([event.sql:150-157](../../backend/internal/event/queries/event.sql#L150-L157)),
      so a PUT omitting the flag **clears it** and republishes the type onto the guest list at price 0
      — the exact leak this story exists to prevent. Thread the field end to end and add a round-trip
      test in `backend/internal/event/admin_service_test.go` proving that editing only a price
      preserves the flag.
- [X] T040 [US2] Refuse clearing `is_visible` on a ticket type that is a package member,
      with `409 PACKAGE_COMPOSITION_LOCKED` naming the blocking packages, in
      `backend/internal/event/admin_service.go` (FR-005).
- [X] T041 [US2] Refuse a registration-only component in package composition writes in
      `backend/internal/event/package_service.go`, naming the offending type (FR-007).
- [X] T042 [P] [US2] Add `is_visible` to the admin ticket-type form in
      `frontend/components/admin/ticket-type-form.tsx` and update
      `frontend/components/admin/ticket-type-form.test.tsx`. Ensure the field is always submitted —
      an omitted field clears it (T039).
- [X] T043 [P] [US2] Distinguish registration-only types visually in the admin ticket-type table in
      `frontend/app/(admin)/admin/events/[id]/page.tsx`.
- [X] T044 [US2] Exclude registration-only types from the package composition picker in
      `frontend/components/admin/package-form.tsx`. This must be **client-side**: the picker is fed by
      the admin ticket-type list, which FR-009 forbids filtering server-side. Update
      `package-form.test.tsx`.
- [X] T045 [P] [US2] Add `backend/internal/event/service_test.go` cases: the guest list excludes the
      flagged type, the admin list includes it, and `ListTicketTypeIDsByEventID` still returns it so
      the delete guard keeps working.
- [X] T046 [US2] Add a cache-coherence test in `backend/internal/event/cache_integration_test.go`:
      flipping the flag invalidates the cached guest ticket-type list for that event.

**Checkpoint**: Containment holds through the admin console and the guest catalogue.

---

## Phase 5: User Story 3 — The form refuses anything that is not its own registration-only ticket (P2)

**Goal**: The boundary that stops the free-issuance path from minting paid tickets.

**Independent Test**: Substitute a paid ticket type's id into a valid form URL; confirm both the page
and a direct `POST` refuse, with nothing written.

- [X] T047 [US3] **Enforce FR-008 at the seam, not in `Book`.** Refuse registration-only types in
      `EventProvider.TicketTypeForCheckout` / `expandTicket`
      ([order/demand.go:52](../../backend/internal/order/demand.go#L52)). This covers booking,
      checkout, availability **and** package expansion at once. Placing it only in `Book` leaves
      `POST /ticket/availability` answering "available", putting the refusal *after* the T&C gate —
      the regression spec 013 exists to prevent (research D15).
- [X] T048 [US3] In `backend/internal/order/handler.go`, re-verify every §1 refusal condition on the
      `POST` handler independently of the `GET`, so a caller bypassing the UI is refused identically (US3 scenario 2).
- [X] T049 [P] [US3] Add `backend/internal/order/demand_test.go` cases: a registration-only type is
      refused by book, by checkout **and** by availability, with no quota movement.
- [X] T050 [P] [US3] Add `backend/internal/order/handler_test.go` cases asserting all six refusal
      conditions return the identical status, code and message — no enumeration oracle (FR-012).
- [X] T051 [US3] Render the refusal in
      `frontend/app/(public)/events/[slug]/register/[ticketId]/page.tsx` as one panel, one sentence, for
      every condition —
      no per-condition branch that would leak which one fired.
- [X] T052 [US3] Add the `409 TERMS_MISSING` pre-check to the `GET`, mirroring `Book`'s pre-transaction
      terms guard ([service.go:117-123](../../backend/internal/order/service.go#L117-L123)), so an
      event with no authored terms says so instead of presenting an unsubmittable form (FR-014g).

**Checkpoint**: The boundary is enforced server-side, not by hiding a form.

---

## Phase 6: User Story 5 — One agreement, identical on both surfaces (P2)

**Goal**: One T&C component, one meaning of consent — including in the paid checkout funnel.

**Independent Test**: On a long document, confirm the booking dialog's checkbox is unchecked and
`Agree` unavailable; tick the checkbox without scrolling and confirm `Agree` turns up; reopen, scroll
to the end, and confirm the checkbox ticks itself.

> **This phase changes a Principle VIII covered flow.** T053 must be seen **red** before T055 lands.

> **⚠️ PARTLY SUPERSEDED by [Phase R3](#phase-r3-the-read-through-gate-removed-added-2026-08-21--blocking).**
> The tasks below are kept checked because they were genuinely done — this is a record of what
> happened, not a claim about what the code should look like now. Four of them built the
> read-through gate the 2026-08-21 clarification removed: **T053**'s scenario is now a regression
> guard rather than proof, **T056** built the no-op `onCheckedChange` that R202 replaces, **T057**'s
> `reachedEnd` reset survives under a new name (R204), and **T059**'s instruction that neither helper
> may click the checkbox is now false — though the code it produced still works unchanged (R212).
> The Independent Test above has been rewritten to the current behaviour; the task text has not, on
> purpose.

> **⚠️ Scope addition discovered during implementation, not requested by the user.** Booking's
> "Terms & Conditions changed" refusal is **dead code**: `UpsertEventTerms` is
> `ON CONFLICT (event_id) DO UPDATE` on a UNIQUE `event_id`, so an edit preserves the row id, and
> `if current.ID != req.EventTermsID` ([order/service.go:320](../../backend/internal/order/service.go#L320))
> can never fire for the case its own doc comment describes. FR-050 needs a working check, and
> FR-043 requires consent to mean one thing on both surfaces — so the fix lands on both.
> **T053a–T053c run FIRST within this phase**, and T053a is a bugfix in a covered flow, so
> Principle VIII requires it be seen red before T053c.

- [X] T053a [US5] Add a scenario to `e2e/specs/guest-purchase.spec.ts`: open the booking terms
      dialog, have an admin republish that event's terms via `putTerms` while it is open, press
      `Agree`, and expect the `TERMS_CHANGED` refusal and a re-read. **Run it and confirm it FAILS**
      against the current code — it will, because the id never changes.
- [X] T053b [US5] Add `UpdatedAt time.Time` to `EventTermsInfo` in
      `backend/internal/order/event_provider.go`, populate it in the `eventProviderAdapter` in
      `backend/cmd/api/adapters.go` from the event domain's terms row, and add `EventTermsUpdatedAt`
      to `AgreementRequest` in `backend/internal/order/dto.go`.
- [X] T053c [US5] Change the staleness check in `RecordAgreement`
      ([backend/internal/order/service.go:320](../../backend/internal/order/service.go#L320)) to
      compare `updated_at` instead of the id, keeping `event_terms_id` as what gets stamped. Confirm
      T053a now passes. Update `frontend/lib/queries.ts` `useRecordAgreement` to send the timestamp.
- [X] T053d [US5] Correct the stale doc comment on `EventTermsDTO`
      ([backend/internal/event/dto.go:81-84](../../backend/internal/event/dto.go#L81-L84)), which
      claims the echoed **ID** is what detects a mid-flow change. It is now `updated_at`.

- [X] T053 [US5] Write the new gate scenario in `e2e/specs/guest-purchase.spec.ts` against a
      **deliberately long** terms document, asserting unavailable → reach the end → available.
      **Run it and confirm it FAILS** against the unfixed dialog. A regression test never seen red
      proves nothing (Principle VIII).
- [X] T054 [US5] Add a long-document fixture helper to `e2e/support/api.ts` or `journey.ts`. Existing
      fixtures use `putTerms(token, event.id, "<p>terms</p>")`, which under FR-014d auto-satisfies the
      gate — so the whole suite would stay green without ever exercising it (research D11).
- [X] T055 [US5] Rewire `frontend/components/booking/terms-dialog.tsx` onto `TermsViewer`. Keep its
      **existing layout** — same checkbox, Cancel and Agree, same positions and labels (FR-044).
      Reaching the end auto-checks the checkbox and turns up `Agree` (FR-045).
- [X] T056 [US5] In `frontend/components/booking/terms-dialog.tsx`, make the checkbox
      non-manually-checkable with a **no-op `onCheckedChange`**, not `disabled`: base-ui renders `<button role="checkbox">` and the component carries
      `disabled:opacity-50`, so `disabled` would dim it and drop it from the tab order — a visible
      change FR-044 forbids ([terms-gate.md §6](./contracts/terms-gate.md)).
- [X] T057 [US5] Add `reachedEnd` to the reset inside `handleOpenChange` in
      `frontend/components/booking/terms-dialog.tsx`. State
      added outside it survives a reopen and silently breaks "reopening starts ungated".
- [X] T058 [US5] Update `frontend/components/booking/terms-dialog.test.tsx`. Note `Agree` is an
      `aria-disabled <span>` while unavailable, so `getByRole("button", {name:/^agree$/i})` **throws**
      — assert with `queryByRole` expecting null, not `toBeDisabled()`.
- [X] T059 [US5] Update `agreeToTermsAndBook()` (journey.ts:149) and `agreeExpectingRefusal()`
      (journey.ts:182) in `e2e/support/journey.ts`: reach the end, assert the checkbox is checked, press
      `Agree`. **Neither may click the checkbox.** This is the whole fix for ~13 call sites.
- [X] T060 [US5] Run `cd e2e && npm test` and confirm T053 now passes and the ~13 existing
      purchase-journey scenarios still pass.
- [X] T061 [P] [US5] Add the keyboard-only gate scenario to `e2e/specs/guest-purchase.spec.ts`:
      focus the reading region, press `End`,
      confirm the gate opens (FR-046). This is the assertion that catches a scroll-listener-only
      implementation.
- [X] T062 [P] [US5] Add the registration-side gate scenario in `e2e/specs/free-registration.spec.ts`,
      including the short-document case (gate pre-satisfied, guest not trapped — FR-014d).

**Checkpoint**: Consent means the same thing on both surfaces, verified in a real browser.

---

## Phase 7: User Story 4 — Registrations are ordinary records in the admin console (P3)

**Goal**: One place to see who is coming — and the recovery path FR-039 makes load-bearing.

**Independent Test**: Complete one registration and one purchase for the same event; both appear in
the admin order and attendee lists, distinguishably.

- [X] T063 [US4] Surface the registration origin and zero total in the admin order list —
      `backend/internal/order/admin_service.go` DTO plus
      `frontend/app/(admin)/admin/orders/` rendering (FR-033, US4 scenario 1).
- [X] T064 [US4] Confirm `frontend/app/(admin)/admin/attendees/` and its backing projection in
      `backend/internal/order/admin_service.go` show a registrant with the same detail as a purchased
      attendee; add a test in `backend/internal/order/admin_service_test.go` if the projection changed.
- [X] T065 [US4] **Make admin resend work for a registration** — the sole recovery route (FR-038a).
      Verify `POST /admin/orders/:id/resend-email`
      ([notification/handler.go:28-29](../../backend/internal/notification/handler.go#L28-L29))
      does not assume a payment, a receipt or a non-zero total, and add a test that it re-sends the
      one-attachment registration email.
- [X] T066 [US4] **Enable operator lookup by name or partial email** scoped to the event (FR-053),
      in `backend/internal/order/admin_service.go` + `backend/internal/order/queries/order.sql`, surfaced
      in `frontend/app/(admin)/admin/attendees/`.
      The guest is never shown the address their ticket went to, so a recovery path requiring the
      exact address is not a recovery path. Extend admin attendee/order search.
- [X] T067 [P] [US4] Add `backend/internal/order/admin_service_test.go` cases: a registration appears
      in the order list with total 0 and its origin legible; the delete guard refuses an event and a
      ticket type carrying a registration, exactly as for a purchase (FR-042).
- [X] T068 [P] [US4] Correct the spec's Out-of-Scope wording: guest self-service resend is reachable
      by anyone holding the order number — `POST /ticket/resend-email` resolves any order number
      (research D18). It is obscurity, not enforcement, and the spec must not claim otherwise.

**Checkpoint**: Every story independently functional.

---

## Phase 8: Polish & Cross-Cutting Concerns

- [X] T069 [P] Document the new throttle's blast radius in `backend/.env.example`, beside the other
      `RATE_LIMIT_*` blocks and following their comment style: disabling
      `RATE_LIMIT_REGISTER_ENABLED` leaves an unauthenticated endpoint that deducts quota and sends
      real email, bounded only by the duplicate-email rule (Principle IX, final rule).
- [X] T070 [P] Update `api/openapi.yml` with both registration endpoints and
      the retired `EMAIL_ALREADY_REGISTERED` (FR-023 removed it).
- [X] T071 [P] Add a Redis flush (or admin cache-refresh call) to the release runbook. Cache keys
      carry no schema or deploy version, so a warm `ticket_types_public` entry built before deploy
      keeps serving registration-only types until an unrelated write or the TTL — SC-004 is not
      verifiable post-deploy without it (research D19).
- [X] T072 Run the Go tier: `cd backend && ./scripts/test.sh ./...` — including
      `cmd/api/architecture_test.go`, which enforces the closed domain set, the no-cross-domain-import
      rule, that `main.go` keeps a literal `e.IPExtractor =`, and that `ops.go` contains no
      `Throttle` / `RATE_LIMIT` strings.
- [X] T073 Run the frontend tier: `cd frontend && npx vitest run`.
- [ ] T074 **(needs a human — cannot be self-certified)** Run `quickstart.md` end to end manually,
      including the keyboard-only auto-tick check, the manual-tick and latch checks added
      2026-08-21 (quickstart §4), and the operator-lookup recovery check. **Re-run after Phase R3** —
      quickstart §4 was rewritten, so a pass recorded before that covered behaviour the spec no
      longer describes.

**End-to-end acceptance (Constitution Principle VIII) — NOT optional.**

- [X] T075 Complete `e2e/specs/free-registration.spec.ts`: full journey; **read the message off
      Mailpit and assert exactly ONE attachment and no monetary string**; the issued code validates as
      `Valid`; a REPEAT email accepted and issued its own second ticket (FR-023); quota exhaustion
      refused; a paid ticket-type id refused by the endpoint with nothing written; stale
      `event_terms_id` refused; success page reload-safe.
- [X] T076 [P] Update `e2e/specs/admin-console.spec.ts` for the new admin field, the package-picker
      exclusion, registration resend, and operator lookup.
- [X] T077 [P] Update `e2e/specs/cache-refresh.spec.ts` for the guest ticket-type list exclusion.
- [X] T078 Run the full suite green: `cd e2e && npm test`.
- [X] T079 Run it with the cache off: `E2E_CACHE_ENABLED=false npm test`.
- [X] T080 Run it with throttling off: `E2E_RATE_LIMIT_ENABLED=false npm test`. Ensure registration
      throttle scenarios skip themselves in this mode, and that throttle scenarios are isolated so
      draining one allowance cannot refuse an unrelated scenario.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phases R / R2 / R3 (clarification reversals)**: each blocks everything in the areas it touches.
  **R3 is the only one still open**, and it is the last work outstanding on this feature apart from
  the human walkthrough (T074).
- **Phase 1 (Governance)**: no dependencies — **blocks everything**.
- **Phase 2 (Foundational)**: depends on Phase 1 — **blocks all user stories**.
- **Phase 3 (US1, P1)**: depends on Phase 2. The MVP.
- **Phase 4 (US2, P2)**, **Phase 5 (US3, P2)**, **Phase 7 (US4, P3)**: depend on Phase 2 only —
  independent of US1 and of each other.
- **Phase 6 (US5, P2)**: depends on Phase 2 (T016 shared viewer). Independent of US1's backend.
- **Phase 8 (Polish)**: depends on all desired stories.
- **Phase R3**: depends on Phase 6 having landed (it edits what Phase 6 built) and on T054's
  long-document fixture, which R201 and R213 both need. Nothing depends on R3 in turn — it is the
  tail.

### Cross-story notes

- US1's form consumes `TermsViewer` (T016), which is why it sits in Foundational rather than in US5 —
  two stories need it, so neither can own it.
- US3's T047 hardens the same seam US2's filters cover from the other side. They are independent:
  US2 hides, US3 refuses. Shipping only one leaves a real hole.

### Within Each User Story

DTO/validation → repository → service → handler → wiring → tests, then frontend.
Exception: **US5 is test-first by mandate** — T053 must be seen red before T055, and in Phase R3
**R201 must be seen red before R202**. R201 is the only scenario in either phase that can fail
against the code it targets; the auto-tick scenarios pass on both sides of the change.

### Parallel Opportunities

- T002, T003 together (different governance docs).
- T012, T013, T014 together (apperr, config, config test).
- T020, T031, T035 together (test files in three tiers).
- T042, T043 together; T045 with either.
- T061, T062 together (different e2e specs).
- R205, R206 together (two callers, different files) — but only after R202/R203/R204.
- R209, R210, R211 together (three test files); R214 with any of them.
- T069, T070, T071 together; T076, T077 together.
- With capacity: after Phase 2, one developer per story across Phases 3–7.

---

## Parallel Example: Phase 2

```bash
# After the migration lands (T005-T008), these three are independent:
Task: "Add CodeEmailAlreadyRegistered + numeric registration in backend/pkg/apperr/apperr.go"
Task: "Add Register ThrottlePolicy in backend/pkg/config/throttle.go"
Task: "Fix the /event segment hide rule in frontend/components/layout/site-nav.tsx"
```

---

## Implementation Strategy

### MVP First

1. Phase 1 — Governance. **Non-negotiable and first.**
2. Phase 2 — Foundational.
3. Phase 3 — US1.
4. **STOP and VALIDATE**: register end to end, read the email off Mailpit, validate the code at the door.

At this point a guest can be issued a free ticket — but the registration-only type is still visible
on the purchase list. **US1 alone is demoable, not shippable.** US2 and US3 are what make it safe to
put in front of the public.

### Incremental Delivery

Foundational → US1 (demo) → US2 + US3 (shippable) → US5 (consent parity) → US4 (operability) → Polish.

**Current position (2026-08-21):** everything above has landed — 102 of 103 original tasks are done.
What remains is Phase R3 and the human walkthrough T074, in that order. R3 must land before T074 is
worth running, because quickstart §4 was rewritten to describe the post-R3 behaviour.

### Suggested shipping cut

**US1 + US2 + US3 + US5.** US4 improves operability and can follow — except **T065 and T066**, which
FR-039e makes load-bearing: without them, a registrant whose delivery bounces has been told it
succeeded and no one can find their record. Pull those two forward into the shipping cut.

---

## Notes

- `[P]` = different files, no dependencies on incomplete tasks.
- Commit after each task or logical group; T005–T007 **must be one commit** (`SCHEMA.md` with the
  migration).
- The fourteen hazards in [plan.md](./plan.md#implementation-hazards-worth-naming-in-the-plan) each
  map to a task above — hazards 12–14 were added 2026-08-21 and map to R207, R202 and R208. If a task seems to be doing something oddly specific, that table is why.
- Verify tests fail before implementing, wherever a task says so.

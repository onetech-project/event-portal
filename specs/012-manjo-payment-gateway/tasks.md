---

description: "Task list for Manjo payment gateway migration"
---

# Tasks: Manjo Payment Gateway Migration

**Input**: Design documents from `/specs/012-manjo-payment-gateway/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

**Tests**: Test tasks are included, and for the same reason as the first pass — not as speculative TDD
scaffolding but because this list **deletes** existing tests along with the code they cover. Shipping
the replacement without replacing that coverage would be a net regression in a payments path.

**Organization**: Grouped by user story so each can be implemented and verified independently.

## Status: this is a rework list, not a fresh build

T001–T089 shipped on 2026-08-10 and the feature works end to end. A clarification session then
**reversed two decisions** the implementation had already made, so the specification now describes
behaviour the code does not implement:

| Was built | Spec now says |
| --- | --- |
| A completion for an expired order is refused, marked `ORPHAN_PAYMENT`, and left for a human | It **settles** the order — seats re-taken, tickets issued, email sent (FR-019) |
| A staff member can confirm a payment by hand, writing `MANUAL_RECONCILED` | Nothing may record a payment on a person's word. Ticket issuance has exactly one trigger (FR-022d) |
| A standing reconciliation worklist lists orders awaiting a decision | No worklist. Orders are reached by number; anomalies arrive as operational signals (FR-022h) |

The recovery flow is now: the customer gives ops proof outside this system → ops tops up quota if the
seats were resold → ops asks the gateway to **resend** that transaction's notification → the
redelivered notification settles the order through the ordinary path.

T090–T125 below are the delta. The completed baseline is recorded at the bottom.

A second increment, **T126–T144**, was added on 2026-08-11 after a defect in the confirmation screen's
ticket-email resend. It is self-contained at the end of this file, with its own dependencies, parallel
opportunities, and strategy.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete work)
- **[Story]**: US1–US6, mapping to the user stories in [spec.md](./spec.md). Increment 2 uses `[RESEND]` instead — that work has no user story of its own, and borrowing a number that does not describe it would be worse than saying so.

## Path Conventions

Web application: `backend/` (Go modular monolith) and `frontend/` (Next.js App Router). All paths
below are repository-relative.

---

## Phase 10: Setup — align the documents the code will otherwise contradict

**Purpose**: The constitution currently forbids part of what the next phase builds, and two design
artifacts still describe the withdrawn surfaces as though they were the plan. None of this blocks a
compiler, but all of it blocks an honest review.

- [X] T090 Amend `.specify/memory/constitution.md` for the new quota-deduction site: Principle IV enumerates only the webhook outcomes that **restore** quota (`expire`, `cancel`, `deny`, `failure`), and this feature adds a webhook outcome that **deducts** it — a `Completed` notification settling an expired order re-takes its seats inside the status-change transaction, refusing outright when they are short. Add that case with its all-or-nothing rule, bump 3.0.1 → 3.1.0 (MINOR — expanded guidance, no principle removed or weakened), and prepend a Sync Impact Report per the governance clause
- [X] T091 [P] Rewrite the passages in `specs/012-manjo-payment-gateway/plan.md` that describe withdrawn surfaces: the Summary's "one capability is added to compensate: a staff reconciliation worklist" (line ~11) and its "the reconciliation worklist is derived from marker rows" (line ~13), the Constitution Check's Principle II evidence and its post-Phase-1 re-check (lines ~42, ~53), and the source-tree listing's `reconcile.go # NEW: worklist + manual-confirm (admin)` and `app/(admin)/…/reconciliation/ # NEW` (lines ~89, ~103). Restate each around the settle path and the two read views — the Principle II argument survives intact, only its subject changes
- [X] T092 [P] Rewrite `specs/012-manjo-payment-gateway/quickstart.md` Scenario 6 (lines ~140–150) as the redelivery walkthrough — expire an order with its callback suppressed, replay `s:5` while the quota is short (expect **200 with an error body**, no status change, a `SETTLE_REFUSED_NO_QUOTA` marker naming the shortfall), top the quota up, replay again (expect `PAID`, seats re-taken, one ticket set, one email, a `SETTLED_AFTER_EXPIRY` marker), replay a third time (expect nothing further) — and fix the Scenario 4 mirror at line ~125 that still expects `ORPHAN_PAYMENT` and a worklist entry. `FR-022f` and `FR-022g` no longer exist and must not be cited

---

## Phase 11: Foundational (Blocking Prerequisites)

**Purpose**: Reshape the marker vocabulary and the transition capability the settle path needs. These
land together because the deletions and the replacements do not compile apart.

**⚠️ CRITICAL**: No story rework can begin until this phase is complete.

- [X] T093 Replace the marker constants in `backend/internal/payment/service.go` (lines ~28–39) per [data-model.md](./data-model.md): delete `MarkerOrphanPayment` and `MarkerManualReconciled`, add `MarkerSettledAfterExpiry = "SETTLED_AFTER_EXPIRY"` and `MarkerSettleRefusedNoQuota = "SETTLE_REFUSED_NO_QUOTA"`. Keep `MarkerDisputed` and `MarkerSessionDuplicate` unchanged
- [X] T094 Narrow the `OrderReleaser` interface in `backend/internal/payment/reconcile.go` from the general `RestoreFromReleased(ctx, tx, orderID, from, to string) (bool, error)` to a single-purpose `SettleExpired(ctx, tx, orderID uuid.UUID) (bool, error)`. FR-019d forbids a completion reviving a `CANCELLED` order, and a method that **cannot express** the forbidden transition enforces that better than a caller who has to remember not to ask for it — the same reasoning that put this capability on a separate interface in the first place
- [X] T095 Bind the guard at the composition root in `backend/cmd/api/adapters.go` (line ~198): `paymentOrderAdapter.SettleExpired` calls `UpdateOrderStatusFrom` with both statuses fixed at the call site (`EXPIRED` → `PAID`). Retitle the comment on `backend/internal/order/queries/order.sql:389` — it reads "Manual reconciliation only (FR-022d)", which now names the exact thing FR-022d forbids. **No SQL change**: the guarded `UPDATE` is already precisely what the settle needs, and its two-admins-at-once rationale transfers verbatim to two redeliveries at once
- [X] T096 Move the settle capability from the optional `WithReconciliation` builder into `payment.NewService`'s required arguments, across `backend/internal/payment/service.go`, `backend/internal/payment/reconcile.go`, and `backend/cmd/api/main.go` (line ~194). Under the old design an unwired deployment refusing to confirm was the *safe* failure, because confirming meant a human asserting a payment. It is now the *wrong* failure: it silently refuses a payment the gateway confirmed, on the path that matters most and is least likely to be exercised before it matters
- [X] T097 Add a per-ticket-type remaining-quota read to the payment domain's view of the event domain, in `backend/internal/payment/reconcile.go` and `backend/cmd/api/adapters.go` alongside `quotaReserverAdapter`. FR-019c's refusal must name the shortfall **per ticket type** so an operator can size the top-up, and `CheckAndDeductQuota` reports only that it failed, never by how much. Read through the event-domain interface, never a cross-domain JOIN (Principle II)
- [X] T098 Delete the withdrawn queries from `backend/internal/payment/queries/payment.sql` — `ListReconciliationMarkers` (the worklist, FR-022h) and `CountPaymentsByOrderAndStatus` (manual-confirm idempotency, FR-022d) — then run `sqlc generate` and drop `ListMarkers` from `backend/internal/payment/repository.go`
- [X] T099 Update `isMarker` in `backend/internal/payment/reconcile.go` (line ~311) for the new marker set, so a marker row stays labelled as one in the per-order history rather than being read as a gateway status

**Checkpoint**: `go build ./...` now fails **only** where the withdrawn surfaces are still called — that failure list is Phase 14's work list.

---

## Phase 12: User Story 2 — a contradiction reaches a person, not a queue (Priority: P1)

**Goal**: Unchanged behaviour, corrected route. A terminal-failure notification against a paid order
still leaves it paid, still writes `DISPUTED`, still signals at error level. What changes is where a
person meets it: the order's own record instead of a worklist.

**Independent Test**: Settle an order, then deliver a `Reject` for it. Confirm the order stays `PAID`,
an error-level signal is raised, and the contradicting notification is readable on that order's
payment history alongside the one that settled it.

- [X] T100 [US2] Correct the contradiction comment in `backend/internal/payment/service.go` (line ~286): "it lands on the staff worklist with a signal" describes a surface FR-022h withdraws. The behaviour the comment sits above — order left `PAID`, `DISPUTED` marker, error-level signal, nothing reversed — is unchanged and correct under FR-016b/c/d; only the destination sentence is wrong
- [X] T101 [P] [US2] Add a test in `backend/internal/payment/service_test.go` asserting a `DISPUTED` marker is readable through `OrderNotifications` alongside the notification that settled the payment (US2 scenario 10, FR-016c). This coverage currently exists only inside the worklist test that Phase 14 deletes, so without it the contradiction path loses its only end-to-end assertion

**Checkpoint**: A contradiction is still impossible to miss, without a queue nobody patrols.

---

## Phase 13: User Story 3 — a cancelled order is never revived (Priority: P2)

**Goal**: Separate the two cases the old code folded together. Expiry is *this system's* verdict,
reached because a notification never came, so a completion reverses it. Cancellation is the
*gateway's* verdict, and reversing it would contradict the source of truth this whole design rests on.

**Independent Test**: Deliver a `Completed` notification to a `CANCELLED` order. Confirm the order is
untouched, no tickets are issued, the payload is recorded on the order, an error-level signal is
raised, and the answer is 200.

- [X] T102 [US3] Replace the single `outcome.OrderStatus == OrderStatusPaid && ord.Status != OrderStatusPending` guard in `backend/internal/payment/service.go` (line ~318) with a branch on the order's **actual** status: `PENDING` proceeds as today, `EXPIRED` routes to the settle path built in Phase 14, `CANCELLED` refuses — order unchanged, nothing issued, error-level signal, 200 (FR-019d). Per [data-model.md](./data-model.md) the cancelled case writes **no marker**: `applyProviderResult` has already written the audit row carrying the completion payload, and that row sits on the order's own record, which is where anyone looking the order up will find it. **Do not simply delete the guard** — `applyOutcome`'s only primitive is `UpdateStatusIfPending`, so an `EXPIRED` order falling through to it matches zero rows and logs "transition skipped" at *info* level, turning a settle into a silent drop
- [X] T103 [P] [US3] Split the non-pending-completion tests in `backend/internal/payment/service_test.go` (lines ~417 and ~613, both asserting `MarkerOrphanPayment`): the cancelled case asserts unchanged-plus-signalled with no marker, and the expired case moves to the US6 settle tests where it now belongs

**Checkpoint**: The gateway's verdicts stand; only our own is reversible.

---

## Phase 14: User Story 6 — a lost notification is recovered by asking the gateway to resend it (Priority: P2)

**Goal**: A redelivered `Completed` settles an expired order through the same path any other confirmed
payment takes — or refuses cleanly, naming what is short, when the seats have been resold.

**Independent Test**: Take an order, suppress its notification entirely, let it expire and its seats
return to the pool. Replay the notification: it must be refused with a named shortfall. Top the quota
back up and replay again: the order moves to paid, the tickets are issued once, the email is sent
once, and the seats are deducted again so the count stays truthful. Replay a third time: nothing more
happens.

### Withdraw the surfaces that assert a payment

- [X] T104 [US6] Delete `Service.ConfirmPayment`, `ConfirmationResult`, `Service.Worklist`, `WorklistEntry`, `reconciliationMarkers`, and `worklistLimit` from `backend/internal/payment/reconcile.go`, along with the file's opening rationale block (lines ~18–30) which argues for a human path back that FR-022d now forbids. Keep `OrderNotifications`, `isMarker`, `envelopeString`, `ErrQuotaUnavailable`, and `QuotaReserver`
- [X] T105 [P] [US6] Delete `WorklistItemResponse`, `ConfirmPaymentRequest`, `ConfirmPaymentResponse`, and `toWorklistResponse` from `backend/internal/payment/reconcile_dto.go`, keeping the notification-history response shape
- [X] T106 [US6] Delete the `worklist` and `confirmPayment` handlers from `backend/internal/payment/handler.go`, plus `Handler.adminIdentity`, `Handler.WithAdminIdentity`, and the `identify` field — with manual confirmation withdrawn, nothing in this domain records who acted. Remove the matching `WithAdminIdentity` wiring from `backend/cmd/api/main.go` (line ~249)

### Build the settle path

- [X] T107 [US6] Rewrite `confirmTransition` in `backend/internal/payment/reconcile.go` (lines ~262–309) as `settleExpiredOrder`, reached from the callback path rather than from a staff action. The transaction body is **already exactly right** and must be preserved: `SettleExpired` guarded on `EXPIRED` first, then `QuotaHolds`, then `ReserveQuota` per hold in the same deterministic ticket-type order checkout deducts in, all rolling back together (FR-019b). What changes is the caller, the pending-order branch (now unreachable — `applyOutcome` handles that case), and the failure behaviour
- [X] T108 [US6] Handle the quota shortfall in `backend/internal/payment/reconcile.go` (FR-019c): on `ErrQuotaUnavailable` nothing moves — no status change, no tickets, no quota — and the refusal reads the shortfall per ticket type through T097's reader, writes a `SETTLE_REFUSED_NO_QUOTA` marker carrying it, and raises an error-level signal. A partial settle is never an outcome: half an order is not something this system can issue tickets for
- [X] T109 [US6] Complete the success path in `backend/internal/payment/reconcile.go` (FR-019, FR-019a): write a `SETTLED_AFTER_EXPIRY` marker carrying the prior status and the quota lines re-taken, raise an operational signal because settling an order the deadline already released is not routine traffic, publish the paid status to the order's stream, and hand off to `fulfillAsync` so tickets and email leave by the same route a first-time notification uses
- [X] T110 [US6] Confirm the settle is idempotent under redelivery in `backend/internal/payment/service.go` (US6 scenario 5, FR-012d): a second delivery meets the existing already-`PAID` short-circuit *before* reaching the settle path, so no second tickets, no second email, no second deduction. Two concurrent redeliveries are separated by `SettleExpired`'s guarded `UPDATE` — one applies, the other reports not-applied and returns quietly. Verify both rather than assume; this is the guarantee the whole recovery flow rests on
- [X] T111 [US6] Carry the refusal out to the wire: `Service.HandleNotification` must report the quota refusal distinguishably, and `Handler.callback` in `backend/internal/payment/handler.go` must answer **200 with a body naming the unavailability** instead of the current bare `c.NoContent(http.StatusOK)` (FR-019c). Every other outcome keeps its empty 200. A non-200 here would spend the gateway's whole retry budget on an attempt guaranteed to fail identically
  > **Partly superseded by Increment 3** (FR-012e/f, FR-019e). The refusal-carries-a-body decision stands and is unchanged; the two details that do not are "a body" — it is now the standard envelope with the shortfall in `data` (T148, T149) — and "every other outcome keeps its empty 200", which FR-012f reverses: no answer is empty. Left checked because it was completed as written; see T147–T151.

### The two read views an operator needs before requesting a resend

- [X] T112 [US6] Move the notification-history route in `backend/internal/payment/handler.go` from `GET /admin/payment/reconciliation/:order_id` to `GET /admin/payment/order/:order_id/notifications` per [contracts/api.md](./contracts/api.md), and delete `GET /admin/payment/reconciliation` entirely
- [X] T113 [US6] Add `GET /api/v1/admin/payment/order/:order_id/holds` in `backend/internal/payment/handler.go` and `backend/internal/payment/reconcile.go`, returning per ticket type how many seats the order holds and how many currently remain (FR-022e), from `QuotaHolds` plus T097's reader. This figure is available nowhere else: the sold count on the ticket-type editor counts released orders too, so an operator sizing a top-up from it would add too few seats and the resend would be refused a second time
- [X] T114 [P] [US6] Define the holds response shape in `backend/internal/payment/reconcile_dto.go`, keeping sqlc structs off the wire (Constitution III)

### Tests

- [X] T115 [US6] Rewrite `backend/internal/payment/reconcile_test.go`: delete the worklist and manual-confirmation tests, and cover settle-with-sufficient-quota, settle-refused-with-a-named-shortfall, refuse-then-top-up-then-settle, replay-after-settle, a `CANCELLED` order refusing to revive, and two concurrent redeliveries settling exactly once
- [X] T116 [P] [US6] Extend `backend/internal/payment/handler_test.go` with the quota-refused callback answering **200 with an error body** rather than a non-200 (FR-019c), and assert the withdrawn admin routes 404

### Frontend

- [X] T117 [P] [US6] Delete `frontend/app/(admin)/admin/payments/reconciliation/page.tsx` and the `{ href: "/admin/payments/reconciliation", label: "Reconciliation" }` entry from `frontend/components/admin/admin-nav.tsx` (line ~15)
- [X] T118 [US6] Delete the worklist and confirm queries and types from `frontend/lib/queries.ts` and `frontend/lib/types.ts`, keeping the per-order notification query and repointing it at T112's path
- [X] T119 [US6] Add the per-order payment view to the admin orders surface in `frontend/app/(admin)/admin/orders/`, reached by order number from the existing list: the order's status, every notification recorded against it (gateway transaction id, raw status, arrival time, refused ones included), and what it holds against what remains (FR-022c, FR-022e). This is the whole of this system's part in the rescue — what ops reads before asking the gateway to resend
- [X] T120 [P] [US6] Test the new view in `frontend/app/(admin)/admin/orders/page.test.tsx` (or the detail route's own test file), covering the notification list including refused entries and the holds-versus-remaining figures
- [X] T121 [US6] Verify the operator's quota top-up path actually serves this flow and record what it demands in `specs/012-manjo-payment-gateway/quickstart.md`. **No code change** — two known hazards need stating rather than fixing: the ticket-type editor sets quota **absolutely** rather than as a delta, so a top-up during active sales can overwrite a concurrent purchase (spec 002 already records this race as accepted); and a package order holds seats across several ticket types at once, **every one** of which must cover its hold before the settle succeeds. T113's holds view is what makes both tractable for an operator

**Checkpoint**: A stranded payment has a route back that involves no developer and no person asserting a payment.

---

## Phase 15: Polish & Cross-Cutting Concerns

- [X] T122 [P] Sweep `ARCHITECTURE.md`, `PRD.md`, and `README.md` for the withdrawn worklist and manual confirmation, and for the added `EXPIRED → PAID` transition — the first lifecycle change this feature makes, which the architecture document's order-state description must now carry
- [X] T123 Confirm nothing stale survives: `rg -i "worklist|MANUAL_RECONCILED|ORPHAN_PAYMENT|ConfirmPayment|reconciliation" backend/ frontend/ --glob '!vendor' --glob '!node_modules'` must return only deliberate historical mentions in specs and comments explaining a withdrawal
- [X] T124 Run `go test ./...` in `backend/` and the Vitest suite plus `next build` in `frontend/`, confirming no test still references the removed markers, endpoints, or the admin screen
- [X] T125 Walk the amended [quickstart.md](./quickstart.md) Scenario 6 end to end against a stub gateway — expire, refuse on shortfall, top up, settle, replay — since this is the flow that only ever runs on the worst day of the month and is therefore the one most worth having exercised once deliberately

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 10)**: no code dependencies. T090 should land before the code that needs it, but nothing waits on it to compile. T091 and T092 are independent files.
- **Foundational (Phase 11)**: **blocks every story phase.** The marker rename (T093) and the interface narrowing (T094–T096) break every call site at once, and the deletions in Phase 14 are what repair them.
- **US2 (Phase 12)**: depends on Foundational. Almost free — one comment and one test.
- **US3 (Phase 13)**: depends on Foundational, and T102's `EXPIRED` branch has nowhere to route until T107 exists. Sequence T102 immediately before Phase 14 or implement the two together.
- **US6 (Phase 14)**: depends on Foundational and on T102's branch. Internally: deletions (T104–T106) → settle path (T107–T111) → read views (T112–T114) → tests (T115–T116) → frontend (T117–T120).
- **Polish (Phase 15)**: after the stories.
- **US1, US4, US5**: **untouched by this rework.** Nothing in the session-open path, the configuration gate, or the payment page changed.

### Critical Path

```
T093/T094/T095/T096 → T102 → T107 → T108/T109 → T110/T111 → spec is true again
                                        ↓
                             T112/T113 → T119 (an operator can act on it)
```

### Parallel Opportunities

- **Phase 10**: T091 and T092 are two different documents; T090 is a third.
- **Phase 11**: mostly sequential — T093 through T099 touch overlapping files in `internal/payment`. T097's adapter half in `cmd/api/adapters.go` can run alongside T098's SQL work.
- **Phase 14**: T105 (DTOs) is parallel to T104 (service). T114 (DTO) is parallel to T113 (handler+service). T116 and T120 are separate test files. T117 (delete page + nav) is parallel to all backend work.
- **Cross-phase**: Phase 12's two tasks touch only the contradiction path and can run alongside Phase 14 once Foundational lands.

---

## Parallel Example: Phase 14 frontend and tests

```bash
# Independent files, once the backend routes exist:
Task: "Delete the admin reconciliation page and its nav entry"
Task: "Extend handler_test.go with the 200-with-error-body callback case"
Task: "Define the holds response shape in reconcile_dto.go"
```

---

## Implementation Strategy

### MVP scope for this rework

**Phase 11 + T102 + T104–T111 + T115.** That is the settle path and the deletions that make room for
it — the smallest change that makes the implementation match what the specification says. At that
point a redelivered notification genuinely rescues a stranded order, and nothing can issue a ticket
on a person's word.

The read views (T112–T114) and the admin screen (T117–T120) are the operator's ergonomics. Without
them the rescue still works, but ops has to check the order against the gateway's dashboard without
being able to see what the order holds — which is exactly how a top-up gets sized wrong and the
resend gets refused twice.

### Incremental delivery

1. Phase 10 → the constitution no longer forbids what comes next
2. Phase 11 → the vocabulary and the capability are right; the build breaks loudly and specifically
3. **+ Phase 13 + Phase 14 settle path → the spec is true again**
4. + Phase 14 read views and screen → an operator can run the rescue unaided
5. + Phase 12 + Phase 15 → the contradiction path is covered and the documents agree

### What is deliberately not in this list

- **Refusing a settle past the event date.** Recorded in the spec as an accepted risk: nothing reads
  the order's age, so a resend requested long after the event would still settle it and email tickets
  to a show that already happened. The operator is a person who has just examined that order and
  chosen to resend it, so the guard would be catching their mistake rather than the system's.
- **Making the ticket-type editor set quota as a delta.** It sets absolutely, and a top-up during
  active sales can overwrite a concurrent purchase. Spec 002 already records that race as accepted;
  changing it is that feature's decision, not this one's. T121 states the hazard instead.
- **Proactive detection of paid-but-expired orders.** Still deferred. The first signal that a callback
  was lost remains a customer complaint.

---

## Rework record (2026-08-10)

All 36 tasks complete. Backend: 634 tests pass against a live database, up from 627 — the settle
path added more coverage than the withdrawn surfaces took away. Frontend: 264 pass, lint and
`next build` clean, `/admin/payments/reconciliation` gone from the route table. Four things are
worth recording.

### 1. The spec still contradicted itself in five places

The clarification reversed FR-019 and FR-022d/h but left passages describing the old behaviour.
Most seriously, **US3 scenario 6 still said a completed payment for an expired order issues no
tickets** — flatly contradicting FR-019 and US6 scenario 2, and it is the scenario a task list is
generated from. Also swept: US2 scenario 10, US6 scenarios 6–7, four edge cases, and two
Assumptions. Fixed before `/speckit-tasks` ran; validation after showed 79 FRs, 21 SCs, zero
dangling references.

### 2. Two design calls were made rather than left open

- **`OrderReleaser` was narrowed, not just re-pointed.** The old `RestoreFromReleased(…, from, to
  string)` could express any transition, so FR-019d — never revive a gateway-cancelled order — would
  have rested on a caller remembering not to ask. `SettleExpired(ctx, tx, orderID)` cannot say it;
  the adapter binds `EXPIRED → PAID` at the composition root. The SQL is untouched.
- **The settle capability moved into `NewService`'s required arguments.** Under the old design an
  unwired deployment refusing to confirm was the *safe* failure, because confirming meant a human
  asserting a payment. It is now the *wrong* failure: it silently refuses a payment the gateway
  confirmed, on the path least likely to be exercised before it matters.

### 3. The callback's wire contract widened

FR-019c wants 200 *with a body* on a quota shortfall, but `HandleNotification` returned only
`error` and the handler ended in `c.NoContent(http.StatusOK)`. The refusal is now a typed
`*SettleRefusedError` the handler catches with `errors.As` and renders as 200 plus the per-ticket-type
shortfall. Returning an error for a non-error outcome is deliberate: the settle genuinely did not
happen, and Go's idiom for "the caller must render this specially" is a typed error — it also
avoided changing every call site.

### 4. A pre-existing mermaid parse error surfaced

`ARCHITECTURE.md`'s checkout sequence diagram carried a `;` inside an unquoted message, which
mermaid reads as a statement separator. It predates this work and was fixed here alongside the
diagram edit this rework added. A syntax sweep over all mermaid blocks in `ARCHITECTURE.md`,
`README.md`, and `PRD.md` now reports clean.

### Carried forward, deliberately

- **The ticket-type editor sets quota absolutely, not as a delta**, so a top-up during active sales
  can overwrite a concurrent purchase. Spec 002 records this race as accepted; T121 states the
  hazard in `quickstart.md` rather than changing another feature's decision.
- **A redelivered notification may settle an order of any age.** Nothing reads the order's age or
  the event's date. Recorded as an accepted risk in the spec.
- **Two frontend tests still fail and still predate this work** —
  `components/booking/selection-summary.test.tsx`, about booking totals, untouched by this rework.

---

## Completed baseline — T001–T089 (shipped 2026-08-10)

All 89 tasks of the original list are complete: the Midtrans adapter is gone, the Manjo adapter opens
sessions and authenticates callbacks, configuration is environment-only with a startup gate, the
payment page renders the QRIS frame and updates over a live stream with a liveness watchdog, and the
backend suite passes 627 tests against a live database.

Four things diverged from that plan and are worth keeping:

### 1. The keep-alive had to change from a comment to a named event

FR-021e's liveness watchdog is built on the server's 25-second keep-alive. Research R6 assumed the
existing comment frame (`: keep-alive`) carried that signal. **It does not**: the browser's
`EventSource` discards SSE comments without dispatching anything.

That is not cosmetic. The server writes a data frame only when the status actually changes, so a
pending order is legitimately silent for its entire payment window — a watchdog with nothing to
observe would have declared *every single payment* degraded within 50 seconds, and the fallback reads
it exists to avoid would have run on every page anyway.

`backend/internal/payment/stream.go` now emits `event: keep-alive\ndata: {}`. A **named** event is
observable via `addEventListener` while — unlike an unnamed data frame — never reaching `onmessage`,
so the status path needs no filtering. R6's original objection to heartbeats applied only to the
unnamed form.

### 2. The duplicate-reference error is `MERCHANT_NOT_AVAILABLE`

The detection heuristic written before observation looked for `duplicate` / `already exist` /
`already registered`. A live gateway returns none of those. Left unfixed, every real duplicate would
have been read as a generic failure and FR-007e's prompt quota release would never have fired. See
research.md R11.

### 3. Two capabilities were deleted beyond the letter of the task list

`UpdatePaymentQR` (interface method, adapter, repository method, and SQL query) and the now-unread
`OrderRef` fields existed solely to serve `ReissueQR`. A live path that can still swap an order's QR
mid-window contradicts FR-010's one-order-one-code.

### 4. The contract module pin is newer than planned

`backend/go.mod` replaces to `v0.0.0-20260810054158-27225c7fa75d`, later than T001's
`…9d11826f4864` and carrying the same `ExpiredAt time.Time`.

### Still open, deliberately

- **The credential half mapping cannot be verified.** The gateway accepts the two halves swapped and
  refuses only an empty one. If `PG_SERVER_KEY`→`client_secret` is backwards, nothing in either system
  will say so (research.md R12).
- **Two frontend tests fail, and predate this work.** `components/booking/selection-summary.test.tsx`
  fails identically on a stashed clean tree; both are about booking totals and touch nothing this
  feature changed.

---

# Increment 2 (2026-08-11): the resend cooldown a guest can see

**Trigger**: an observed defect, not a planned gap. The confirmation screen's *Resend email* sent
nothing on the first press and left no log; the second press was refused as rate-limited; the button
then stayed dead until reload. Clarification session 2026-08-11 turned that into FR-021j–q, SC-022–024,
and six edge cases.

**Story label**: these tasks carry `[RESEND]` rather than `[US*]`. The increment has no user story of
its own — flagged during clarification, left open by the plan — because the endpoint belongs to spec
008 (FR-022) while the screen that carries it is built here. The label keeps the grouping honest rather
than borrowing a story number that does not describe this work.

**Baseline**: T090–T125 complete. This increment is T126–T144.

**Read first**: [research.md](./research.md) R13–R15, [contracts/api.md](./contracts/api.md)
"`POST /api/v1/ticket/resend-email`", [data-model.md](./data-model.md) "Domain types (Go) — resend
cooldown", [quickstart.md](./quickstart.md) Scenario 8.

---

## Phase 16: Foundational — make the request reach the handler at all

**Purpose**: One line has been silently defeating this endpoint. Everything else in this increment is
about the *next* such bug being a five-minute diagnosis, so it must not be built on top of a call that
never arrives.

**⚠️ CRITICAL**: T126 must land before any of the cooldown work, and it is shippable on its own.

- [X] T126 [RESEND] Fix the double-encoded request body in `frontend/lib/queries.ts` (line ~234): `useGuestResendTicketEmail` passes `body: JSON.stringify({ order_id: orderNumber })` while `apiFetch` stringifies whatever it receives (`frontend/lib/api-client.ts` line ~82), so the wire carries a JSON *string* and `c.Bind` fails server-side. Pass the object — `body: { order_id: orderNumber }`. This one change restores resend by itself; verify by pressing the button once and finding a `ticket email delivered` line in the server log
- [X] T127 [P] [RESEND] Pin the wire shape in `frontend/lib/api-client.test.ts`: assert that a `body` given as an object reaches `fetch` as JSON that parses back to that object, and that no caller pre-stringifies. `apiFetch`'s `body?: unknown` signature makes this mistake invisible at the call site, which is exactly why it survived — the guard belongs where the contract is, not at each caller
- [X] T128 [RESEND] Add `backend/pkg/httpx/cooldown.go`: a keyed token bucket over `golang.org/x/time/rate` exposing `NewCooldown(requestsPerSecond float64, burst int, expiresIn time.Duration) *Cooldown` and `Take(key string) (allowed bool, retryAfter time.Duration)`. Derive `retryAfter` from `rate.Limiter.TokensAt(now)` as `(1 - tokens) / limit` — a pure read, no `Reserve`/`Cancel` dance (research R13). Called after a successful take it yields the full window just started, which is what the accepted response must carry (FR-021j). Evict idle keys after `expiresIn`, and guard the map for concurrent callers the way the existing limiter store does
- [X] T129 [P] [RESEND] Test `backend/pkg/httpx/cooldown_test.go`: seconds present and correct on **both** answers; the refused value strictly decreasing across successive attempts (a constant equal to the window means the remainder is being reported as the window); distinct keys never interfering; and the eviction invariant — `expiresIn` strictly greater than the window, so no key is forgiven mid-cooldown

**Checkpoint**: Resend works again for a guest, and a cooldown that can report itself exists but is not yet wired.

---

## Phase 17: The endpoint's contract

**Goal**: Every answer states the wait, a body the server cannot key costs nobody anything, and every
attempt leaves a trace.

**Independent Test**: Press resend twice in quick succession — the first `202` carries
`retry_after_seconds: 60`, the second `429` carries a smaller, falling number. Send a malformed body
for order A and confirm a resend for order B is still accepted in the same window. Confirm all four
accepted outcomes return byte-identical bodies while producing four distinct log lines.

- [X] T130 [P] [RESEND] Add a `RetryAfterSeconds int` field, tagged `json:"retry_after_seconds"`, to `PublicResendResponse` in `backend/internal/notification/dto.go`. Leave `PublicResendMessage` a constant and say why in the doc comment: the non-disclosure property depends on every accepted outcome returning the same sentence, and the new field is safe beside it precisely because it is a property of the cooldown, which every attempt spends alike (FR-021m), not of what the attempt found
- [X] T131 [RESEND] Rewrite `resendPublic` in `backend/internal/notification/handler.go` (lines ~50–79) as five ordered steps, and keep the order — it *is* the requirement: (1) bind; on error or empty `order_id` return `apperr.BadRequest(apperr.CodeValidation, …)` **without touching the cooldown** (FR-021k); (2) `cooldown.Take(orderNumber)`; when refused return `apperr.New(http.StatusTooManyRequests, apperr.CodeRateLimited, …).WithData(retryAfter)` (FR-021j); (3) look the order up; (4) send; (5) return the one accepted body carrying the seconds. Steps 3–5 all answer identically (FR-021l). Delete the comment claiming "a parse error is as silent as an unknown order" — that is the behaviour being removed
- [X] T132 [RESEND] Log every branch in `backend/internal/notification/handler.go` with its real outcome and the order number — `delivered`, `order unknown`, `order not payable`, `send failed`, `refused for cooldown`, `body unreadable` (FR-021n). Info level for the ordinary ones, warn for a send that failed. The wire stays silent; the logs do not. The existing single `WarnContext` covers only one of the six, which is why the observed failure looked like a press that never happened
- [X] T133 [RESEND] Change `RegisterPublicRoutes` in `backend/internal/notification/handler.go` from `(g *echo.Group, mw ...echo.MiddlewareFunc)` to take the `*httpx.Cooldown` directly. The variadic middleware parameter existed to inject exactly this dependency; it is now the wrong shape, because the handler needs the number and the middleware cannot hand it one
- [X] T134 [RESEND] Rewire the composition root in `backend/cmd/api/main.go` (lines ~273–279): build the cooldown from the unchanged `guestResendRate` / `guestResendBurst` / `rateLimitWindow` constants, pass it to `RegisterPublicRoutes`, and mount the route on the ordinary `api` group — the separate `guestResend` group existed only to scope the middleware and now has nothing to scope. Update the constants' comment: it explains a per-order bucket, which is still true, but no longer explains a middleware
- [X] T135 [RESEND] Delete `RateLimitPerBodyField` and `bodyField` from `backend/pkg/httpx/rate_limit.go` and their cases from `backend/pkg/httpx/rate_limit_test.go` (research R14). Its documented fallback — "a request whose body is missing, unparseable, or lacks the field falls into a shared bucket" — is the defect, not an implementation slip, so it is removed rather than repaired. `RateLimitPerIP` and `RateLimitBy` stay untouched; they have other callers and no equivalent flaw
- [X] T136 [RESEND] Rewrite the guest cases in `backend/internal/notification/public_handler_test.go` around the six-outcome table in [data-model.md](./data-model.md). Two assertions carry the whole increment and must both be present: **byte-identical bodies** across all four accepted outcomes (the disclosure rule), and **a malformed request for order A leaving order B acceptable in the same window** (the shared bucket, which no existing test would have caught). Also assert the `400` spends nothing, that a `429` carries a falling `retry_after_seconds` in `data`, and that an attempt sending no mail still spends the window (FR-021m)

**Checkpoint**: The server can be asked "how long?" and answers truthfully, and one caller's malformed request is one caller's problem.

---

## Phase 18: The countdown on the confirmation screen

**Goal**: The guest sees when they can ask again, and the button comes back by itself.

**Independent Test**: Press resend, watch a countdown run down and the button re-arm at zero with no
reload. Reload mid-cooldown and confirm the button is armed, the press returns `429`, and the countdown
resumes from the server's remaining seconds rather than restarting at 60.

- [X] T137 [P] [RESEND] Add `retry_after_seconds: number` to `PublicResendResponse` in `frontend/lib/types.ts`
- [X] T138 [RESEND] Rewrite `ResendRow` in `frontend/components/order/order-confirmation.tsx` (lines ~106–152) around a countdown seeded from the server: take the seconds from `resend.data.retry_after_seconds` on success and from `(resend.error as ApiError).data` on a 429 (`ApiError.data` already carries the envelope's detail payload — see `frontend/lib/api-client.ts` line ~152), tick it down locally, and **re-enable the button at zero** (FR-021o). Today `disabled={resend.isPending || rateLimited}` has nothing that ever clears `rateLimited`, which is why the control died permanently. Do **not** reuse `ExpiryCountdown`: it corrects device skew against an absolute instant, and a duration needs no such correction (research R13)
- [X] T139 [RESEND] Keep the cooldown out of storage and out of cross-tab state in `frontend/components/order/order-confirmation.tsx` (FR-021p) — component state only, so a fresh load arms the button and the first press resolves the truth. A remembered deadline can outlive a server restart and hold a guest back from a resend the server would have accepted
- [X] T140 [RESEND] Separate the two messages in `frontend/components/order/order-confirmation.tsx` (FR-021q): a cooldown refusal reads as a wait and names the remaining time; a genuine send failure reads as a failure. They have opposite remedies, and the current copy — "Just sent. Please wait a minute before asking again." — states a fixed minute the server no longer requires anyone to guess at
- [X] T141 [P] [RESEND] Extend `frontend/app/(public)/events/[slug]/orders/[orderNumber]/success/page.test.tsx` (the resend cases begin around line ~200): a countdown appearing from the accepted response's seconds; the button re-arming at zero with fake timers; a 429's `data.retry_after_seconds` driving the countdown rather than a hardcoded 60; and nothing written to `localStorage` or `sessionStorage`

**Checkpoint**: A guest whose email did not arrive can always ask again, and always knows when.

---

## Phase 19: Polish & Cross-Cutting Concerns

- [X] T142 [P] [RESEND] Walk [quickstart.md](./quickstart.md) Scenario 8 end to end against a running stack — all nine steps, including the two `curl` calls that prove a malformed body no longer throttles a different order
- [X] T143 [RESEND] Run the regression greps from [quickstart.md](./quickstart.md): `rg "RateLimitPerBodyField|bodyField" backend/ --glob '!vendor'` and `rg "body: JSON.stringify" frontend/ --glob '!node_modules'` must both return nothing
- [X] T144 [RESEND] Run `go test ./...` in `backend/` and the Vitest suite plus `next build` in `frontend/`. Two frontend failures in `components/booking/selection-summary.test.tsx` predate this work and are unrelated — confirm the count has not grown rather than that it is zero

---

## Increment 2 — Dependencies & Execution Order

### Phase Dependencies

- **Phase 16**: T126 blocks everything, and blocks nothing in return — ship it first and alone. T128 is independent of T126 and can be written alongside it. T127 and T129 are separate test files.
- **Phase 17**: needs T128. Internally sequential where files overlap: T131 → T132 → T133 (all `handler.go`) → T134 (`main.go`) → T135 (the deletion, which only compiles once T134 stops calling it). T130 is a separate file and parallel to all of it; T136 follows the handler.
- **Phase 18**: needs Phase 17 on the wire. T137 is parallel to everything; T138 → T139 → T140 all touch `ResendRow`; T141 follows.
- **Phase 19**: after both.

### Critical Path

```
T126 → resend works again
T128 → T131 → T132 → T133 → T134 → T135 → the server states the wait
                                       ↓
                              T138 → T140 → the guest can see it
```

### Parallel Opportunities

- **Phase 16**: T126 (frontend) and T128 (backend) are different languages, let alone different files. T127 and T129 are their tests.
- **Phase 17**: T130 (DTO) runs alongside the whole handler sequence. T136 can be written against the contract before the handler is finished.
- **Phase 18**: T137 (types) is parallel to everything; T141 is a separate test file.
- **Cross-phase**: Phase 18's T137 can land as soon as T130 fixes the shape.

---

## Increment 2 — Implementation Strategy

### MVP scope

**T126 alone.** It is one line, it restores a broken user-facing capability, and it needs none of the
rest. Landing it separately also keeps the diagnosis honest in the history: the outage was an encoding
bug, and the cooldown work is what makes the next one visible rather than what fixes this one.

**Then T128 + Phase 17.** At that point the limit is truthful and no longer system-wide, which is the
part that affects other guests. The countdown is still absent, but the button no longer dies —
a refusal is just a refusal.

**Then Phase 18** for the countdown the request actually started from.

### What is deliberately not in this list

- **Changing the window.** One send per order per minute is unchanged. The defect was never about its
  length, and nothing observed suggests 60 seconds is wrong.
- **Alerting on repeated accepted-but-undelivered resends.** T132 puts the outcome in the logs, so the
  signal now exists in the data; turning it into an alert is a monitoring decision this list does not
  make. Recorded here so it is a choice rather than an oversight.
- **A user story for the resend flow.** Raised at clarification, left open by the plan, still open.
  The `[RESEND]` label is the workaround, not the resolution.
- **Touching the admin resend** (`POST /admin/orders/:id/resend-email`). It is authenticated and
  unlimited, and nothing in this increment changes that.

---

## Increment 2 record (2026-08-11)

All 19 tasks complete. Backend: full suite green, `go vet` clean. Frontend: 273 pass, lint clean,
`next build` clean. Four frontend tests fail and were verified to fail identically at `HEAD` with this
increment's five files stashed — `lib/booking-stage.test.ts` (1),
`components/booking/selection-summary.test.tsx` (2), and the order page's dialog test (1). None are in
files this increment touches. Net +7 passing tests.

### Walked against a live server, not only against fakes

The stack was already up, so Scenario 8's server-side steps ran for real against `ticketing-postgres`
and mailpit on a spare port. Every one behaved as specified:

| Step | Result |
| --- | --- |
| First press, real paid order | `202`, `retry_after_seconds: 60`, an email delivered, and **a log line** |
| Immediate second press | `429`, `retry_after_seconds: 60` |
| Third press 3 s later | `429`, `retry_after_seconds: 57` — the remainder, not the window |
| The double-encoded body verbatim | `400 VALIDATION_ERROR`, plus four other malformed shapes |
| Unrelated order in the same window | `202` — one caller's malformed request throttles nobody |
| Fictional order, twice | `202` then `429` — an attempt that sends no mail still spends the window |
| Accepted bodies compared | byte-identical between a real paid order and an unknown one |

### Three things worth recording

**1. The bug was one line, and the diagnosis was the expensive part.** `queries.ts` pre-stringified a
body `apiFetch` stringifies again. The endpoint had been answering "accepted" and sending nothing
since that call was written. What made it cost an afternoon was not the bug but that it left no
evidence: no log line, and a success message on screen. T132 is the task that actually pays for
itself.

**2. The shared bucket was worse than it looked.** `RateLimitPerBodyField` dropped every request it
could not key into one bucket for the whole system, so the malformed press throttled *every* order for
a minute, not just the one being resent. That is why the symptom read as "it always says rate limited"
rather than as a client bug. Verified gone: a malformed request now leaves an unrelated order
acceptable in the same window, asserted in both the handler test and the live walkthrough.

**3. The countdown moved out of an effect during review.** The first implementation seeded
`secondsLeft` from a `useEffect` watching the mutation result; `react-hooks/set-state-in-effect`
rejected it, correctly. It is now seeded from `mutate`'s own `onSuccess`/`onError`, which is what the
countdown is — a reaction to a response, not state kept in sync with a render.

### Still open, deliberately

- **No alert on repeated accepted-but-undelivered resends.** Every attempt now carries its outcome in
  the logs, so the signal exists in the data; nothing turns it into an alert.
- **The window is unchanged** at one send per order per minute. Nothing observed suggests it is wrong.
- **The resend flow still has no user story.** Raised at clarification, left open by the plan, carried
  by `[RESEND]` here.

### Follow-up (2026-08-11, found in manual testing)

The countdown did not appear and the button fired twelve 429s in two seconds. Two causes, one of them
mine.

**The API under test was the old binary.** Its `202` body carried no `retry_after_seconds`, and a
`{"nope":1}` body still answered `202` where the new handler answers `400`. The frontend had the new
code, the server did not — which is why the button no longer locked up but also never counted down.
Restarting the API resolves that half.

**A missing number left the button live — a real gap, not just deploy skew.** `retryAfterSeconds`
returned `0` when the payload had no field, `startCountdown` ignored zero, and nothing held the
control. That is the original machine-gun loop reached by a different route, and a proxy or an error
path could produce it against a current server too. `ResendRow` now falls back to a short floor
(`FALLBACK_COOLDOWN_SECONDS`, 5s) whenever the server names no wait — on the acceptance as well as the
refusal. Recorded as **FR-021j-i**, with two tests. The floor is deliberately far shorter than any
real window: it stops a burst without inventing a deadline a guest could be stranded behind.

---

# Increment 3 — the callback endpoint answers in the envelope (2026-08-11)

**Trigger**: an observed gap, not a planned one. `POST /v1.0/callback/exec` returns no body at all on
the ordinary path, while the one path that does answer with a body — the FR-019c settle refusal —
writes a bare object outside the envelope every other endpoint speaks. Three shapes across three
paths. Clarification session 2026-08-11 turned that into FR-012e, FR-012f, FR-019e, SC-025, SC-026,
one edge case, and two acceptance scenarios on User Story 2.

**Story labels**: `[US2]` for the acknowledgement (FR-012e/f, US2 scenarios 12–13) and `[US6]` for the
refusal (FR-019e, US6 scenario 3). Unlike Increment 2 this work does map onto real user stories, so it
borrows no label it has not earned.

**Baseline**: T126–T144 complete. This increment is T145–T155.

**Read first**: [research.md](./research.md) R16, [contracts/api.md](./contracts/api.md)
"`POST /v1.0/callback/exec`", [contracts/gateway.md](./contracts/gateway.md) §2 Response,
[data-model.md](./data-model.md) "Callback response shapes", [quickstart.md](./quickstart.md)
Scenarios 2 and 6.

**Smaller than it looks.** The error paths are already correct: `apperr.Error.Response()` renders the
same `{code, message, data}` envelope and `httpx.ErrorHandler` writes it for every returned error, so
authentication failure, unreadable body, and internal fault already satisfy FR-012e untouched. Two
writes in `payment/handler.go` deviate, and one registry arm is missing.

---

## Phase 20: Foundational — register the code before anything can emit it

**Purpose**: `apperr.Numeric`'s fallback is `status * 1000`. Until `TICKETS_UNAVAILABLE` is registered,
an `apperr` carrying HTTP 200 renders `200000` — byte-identical to `httpx.SuccessCode`. Wiring the
handler first would ship a refusal that announces itself as a success, and no test asserting on the
status line would catch it, because 200 is correct in both cases by design.

**⚠️ CRITICAL**: T145 must land before T148.

- [X] T145 [US6] Register the refusal code in `backend/pkg/apperr/apperr.go`: add `CodeTicketsUnavailable = "TICKETS_UNAVAILABLE"` to the const block (beside the other payment codes, ~line 29), and add `case CodeTicketsUnavailable: return 200001` to `Numeric` (~line 78). Note in a comment why the 200 band exists at all — FR-019c requires a refusal that must not be retried to travel on a 200, so this is the first envelope code whose status is a success; the sub-code is what carries the disagreement
- [X] T146 [P] [US6] Test the registry in `backend/pkg/apperr/apperr_test.go`: `Numeric(200, CodeTicketsUnavailable)` is `200001`, and — the assertion that matters — `Numeric(200, "ANYTHING_UNREGISTERED")` is `200000`, equal to `httpx.SuccessCode`. The second is not a curiosity: it pins the collision this task exists to avoid, so a future 200-band code added without a registry arm fails here rather than silently reporting success (research R16)

**Checkpoint**: A 200-status error can name itself without colliding with success.

---

## Phase 21: The callback's two writes

**Goal**: One shape across every path, and a refusal that is tellable from an acknowledgement by the
envelope code alone.

**Independent Test**: Post an unknown reference, a withdrawal (`tt:1`), an unrecognised `s`, and a
repeat against a paid order. All four return byte-identical
`{"code":200000,"message":"Success","data":null}`. Then post a completed payment against an expired
order whose quota is short: still `200`, but `code` is `200001` and `data.shortfall` names the gap.

- [X] T147 [US2] Replace `c.NoContent(http.StatusOK)` at the end of `Handler.callback` in `backend/internal/payment/handler.go` (line ~111) with `httpx.Respond(c, http.StatusOK, nil)`, which writes `{"code":200000,"message":"Success","data":null}` (FR-012e, FR-012f). `pkg/httpx/envelope.go` needs no change — `Respond` with a nil payload already produces exactly this. Update the function's doc comment: it currently explains only *which status* each outcome gets, and the response rule now also governs the body
- [X] T148 [US6] Render the refusal through `apperr` in `backend/internal/payment/handler.go` (lines ~97–100). Replace `c.JSON(http.StatusOK, toSettleRefusedResponse(refused))` with `return apperr.New(http.StatusOK, apperr.CodeTicketsUnavailable, <the existing message>).WithData(toSettleRefusedResponse(refused))`. The shared error handler writes the envelope and emits a `request rejected` warn line carrying the code — wanted here, because a refused settle is exactly what an operator should be able to find in the logs. Keep the comment explaining why this is a 200; extend it to say the code is now what distinguishes it (FR-019e)
- [X] T149 [US6] Reduce `SettleRefusedResponse` in `backend/internal/payment/reconcile_dto.go` (lines ~49–71) to the `data` payload it now is: drop `OrderNumber`, `Error`, and `Message` — the envelope's `code` and `message` carry the latter two, and the order number is the notification's own `ri`, echoed back to a caller that already sent it. Keep `Shortfall []QuotaShortfallResponse` under `json:"shortfall"`, so the body reads `"data": {"shortfall": [...]}` per [contracts/api.md](./contracts/api.md). `toSettleRefusedResponse` keeps its name and returns the reduced struct; the message string it built moves to T148's call site
- [X] T150 [US6] Update the quota-shortfall case in `backend/internal/payment/handler_test.go` (lines ~150–170) to decode `apperr.Body` rather than a bare `payment.SettleRefusedResponse`: assert `body.Code == 200001` alongside the existing `http.StatusOK`, and read the shortfall from `body.Data`. State in a comment that the status assertion alone is insufficient — it passes on a plain acknowledgement too, which is the whole reason FR-019e puts the discriminator in the code
- [X] T151 [P] [US2] Add an acknowledgement-uniformity case to `backend/internal/payment/handler_test.go`: drive the four outcomes FR-012c answers 200 without acting on — unknown `ri`, `tt:1` withdrawal, unrecognised `s`, and a repeat against an already-paid order — and assert all four produce **byte-identical** response bodies equal to `{"code":200000,"message":"Success","data":null}`. This is the FR-012f guarantee and nothing today would catch its erosion; it is the same shape of assertion as Increment 2's identical-answer test, for the same reason

**Checkpoint**: Every callback answer parses as the envelope, and the refusal is distinguishable without reading `data`.

---

## Phase 22: Polish & Cross-Cutting Concerns

- [X] T152 [P] [US6] Sync `ARCHITECTURE.md` (line ~52, Quota Re-deduction): "answers 200 with a body naming the shortfall" is still true but no longer complete — name the envelope code that separates it from an acknowledgement. `README.md` needs no change; its `PG_CALLBACK_TOKEN` row already says a mismatch is the only non-200 and stays correct
- [X] T153 [US2] Walk [quickstart.md](./quickstart.md) Scenario 2 against a running stack, including the `jq -e '.code'` check on every call and the four-outcome uniformity comparison, then Scenario 6 steps 4–5 confirming `200001` then `200000` on the same endpoint with the same status line
  > **Walked against a purpose-built binary on port 8090**, not the stack already on 8080 — that process could not be identified and predates these changes, which is the stale-binary trap the Increment 2 follow-up recorded. Verified live: the `jq -e '.code'` gate (which an empty body fails outright), byte-identical `{"code":200000,"message":"Success","data":null}` across unknown-reference, withdrawal, unrecognised-status and pending, and `{"code":401001,…}` on a missing token. **Not walked live: Scenario 6 steps 4–5**, which need a seeded expired order with short quota; the `200001` refusal is instead covered by `TestCallbackEndpointAnswers200WithAReasonWhenTheSeatsAreGone` against the same Postgres through the same handler and error handler.
- [X] T154 [P] [US2] Run the regression grep: `rg "NoContent" backend/internal/payment/ --glob '!vendor'` must return nothing. A reintroduced empty answer is invisible to any test asserting only on status
- [X] T155 [US2] Run `go test ./...` in `backend/`. The frontend is untouched by this increment — no `queries.ts`, `types.ts`, or component change — so the Vitest suite needs re-running only to confirm the two pre-existing `selection-summary.test.tsx` failures have not grown

---

## Increment 3 — Dependencies & Execution Order

### Phase Dependencies

- **Phase 20 (T145–T146)** — blocks Phase 21. T145 specifically blocks T148; emitting an unregistered
  200-band code renders `200000` and the refusal becomes indistinguishable from success.
- **Phase 21 (T147–T151)** — T147 and T148 touch the same function in the same file and are strictly
  sequential. T149 must precede T148's call site compiling. T150 follows T149 (it decodes the reduced
  shape); T151 is independent of both.
- **Phase 22 (T152–T155)** — all follow Phase 21. T153 needs a running stack.

### Critical Path

T145 → T149 → T148 → T150 → T153 → T155

T147 can land any time after T145 and is independently shippable: it fixes the empty acknowledgement
without touching the refusal at all.

### Parallel Opportunities

- T146 runs alongside T147 — different files, no shared symbol.
- T151 runs alongside T150 — same file, different test function; sequence them only if the repo's
  convention is one editor per file.
- T152 and T154 are documentation and a grep; both run any time after Phase 21.

---

## Increment 3 — Implementation Strategy

### MVP scope

**T145 + T147 alone close the reported gap.** The endpoint stops returning nothing, and every
acknowledgement becomes the envelope. That is the whole of what was observed. T148–T151 fix the
second, unreported inconsistency found while reading the handler — the refusal writing a bare object
outside the envelope — and they are worth doing in the same change precisely because the endpoint
having *one* shape is the requirement, not having a better one on the common path.

### What is deliberately not in this list

- **No change to what the acknowledgement reports.** FR-012f settles this: `data` is null, and what a
  notification did is read from the `payments` record through the per-order history endpoint. A
  response naming the outcome would be a second, transient account of something already recorded
  durably, and the two could disagree.
- **No frontend work.** Nothing in the browser reads this endpoint; the gateway is its only caller.
- **No change to the status codes.** FR-012c is untouched — the 200/non-200 rule is exactly as before,
  and this increment only gives each answer a body.

### Watch item, carried forward

Returning a non-nil error with a 200 status is unusual, and anything that counts handler errors as
failures — span status, an error-rate metric — will count the refusal. No such middleware exists in
this build. If one is added, this path needs an explicit exemption rather than a silent
reclassification into the failure count.

---

# Increment 4 — signalled anomalies name themselves (2026-08-11)

**Trigger**: observed on a live server. A notification whose reference matched no order logged twice
at ERROR and answered `{"code":200000,"message":"Success","data":null}`. Reported as "if the order is
not found it didn't send the response message, it should — keep the status but the error should
always be sent."

**Scope decision**: not just the unknown reference. Every outcome that raises an operational signal
names itself in the envelope's code (FR-012g); the uneventful ones stay uniform (FR-012f). See
[research.md](./research.md) R17 for why the signal is the line.

**Baseline**: T145–T155 complete. This increment is T156–T161.

- [X] T156 [US2] Add five sentinel errors to `backend/internal/payment/service.go` — `ErrNotificationUnknownOrder`, `ErrNotificationNotDeposit`, `ErrNotificationUnknownStatus`, `ErrNotificationContradiction`, `ErrNotificationOrderCancelled` — and return them from the five branches that previously returned nil: the non-deposit guard and the unknown-order guard in `HandleNotification`, and the contradiction, unrecognised-status and cancelled-order branches in `applyProviderResult`. Both functions have exactly one caller, so the change is contained
- [X] T157 [US2] Extend the 200 band in `backend/pkg/apperr/apperr.go` with `CodeNotificationOrderUnknown` → `200002`, `CodeNotificationNotDeposit` → `200003`, `CodeNotificationStatusUnknown` → `200004`, `CodeNotificationContradiction` → `200005`, `CodeNotificationOrderCancelled` → `200006`, each registered in `Numeric` for the reason T145 records — the fallback renders `200000` and would report an anomaly as success
- [X] T158 [US2] Map the sentinels to codes in `backend/internal/payment/handler.go` via a `notificationNotice` helper, applied before the generic `if err != nil` so none of them becomes a 500. Keep the mapping in the handler rather than the service: the domain reports what happened, the transport decides how to say it
- [X] T159 [US2] Narrow `TestCallbackEndpointAnswersEveryAcknowledgementIdentically` in `backend/internal/payment/handler_test.go` to the genuinely uneventful outcomes — payment applied, pending, repeat on a paid order — and add `TestCallbackEndpointNamesEverySignalledAnomaly` covering all five with their codes, asserting each keeps a 200, carries a non-empty message, carries null data, and does not act on the order
- [X] T160 [US2] Update the twelve `require.NoError` assertions in `backend/internal/payment/service_test.go` and `backend/internal/payment/reconcile_test.go` that covered these branches to `require.ErrorIs` against the matching sentinel. They asserted "this is not an error" where the code always meant "this changed nothing and someone should know"
- [X] T161 [US2] Sync the artifacts: FR-012f narrowed and FR-012g added in [spec.md](./spec.md) with SC-027, the response tables in [contracts/gateway.md](./contracts/gateway.md) and [contracts/api.md](./contracts/api.md), the code table in [data-model.md](./data-model.md), [quickstart.md](./quickstart.md) Scenario 2, and R17 in [research.md](./research.md)

**Verified**: full backend suite, 16 packages, zero skips against the test database. Then live against
a rebuilt binary on a spare port — the reported payload now answers `200002` with a readable message,
a withdrawal answers `200003`, and the status line stays `200` throughout so the gateway still never
retries. Auth failure remains the only non-200.

**Precedence worth remembering**: deposit check → order lookup → status mapping. An unknown reference
carrying `s:99` answers `200002`, not `200004` — a status cannot be mapped onto an order never found.

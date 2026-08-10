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

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependency on incomplete work)
- **[Story]**: US1–US6, mapping to the user stories in [spec.md](./spec.md)

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

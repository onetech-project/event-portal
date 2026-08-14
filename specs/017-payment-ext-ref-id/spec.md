# Feature Specification: Payment External Reference ID

**Feature Branch**: `017-payment-ext-ref-id`

**Created**: 2026-08-14

**Status**: Draft

**Input**: User description: "currently we dont store the external ref id (eri) from payment, i want to store it on the payment table, show it on the admin page (FE) and also show on the response of checkout API (ext_ref_id) but keep the ui just like today."

## Overview

When checkout opens a payment session, the gateway answers with its own identifier for
that transaction — the **external reference**. The system already reads it: it arrives on
the session answer, is carried on the provider-neutral session shape the payment
abstraction hands back, and reaches checkout intact. And then nothing happens to it. It is
never written down, never returned, and never shown. The instant the checkout response is
sent, the identifier is gone.

That identifier is the one thing that lets a human line an order in this system up against
a transaction in the gateway's own records. Its absence is felt exactly where it costs the
most — the stranded payment. A guest says they paid; no notification ever arrived; the
operator opens the order's payment view and finds a history of notifications that never
came, holds against remaining quota, and **nothing to search the gateway with**. The
operator's only handle is the order number the gateway was given at session open, which
answers "did this order reach the gateway at all" but not "which transaction is it, and
what does the gateway believe about it".

Note that the identifiers the gateway sends *back* are a different thing and not a
substitute. A settlement notification carries the gateway's network transaction id, which
the payment history already shows. That id only exists for transactions that actually
produced a notification — precisely the ones that are not stranded. The external
reference, by contrast, exists from the moment the session opens, which is the moment the
support case begins.

This feature writes the reference down and puts it in front of the two audiences that need
it: the **operator**, on the admin order's payment view, and the **caller of checkout**, as
`ext_ref_id` on the response.

Nothing else changes. Specifically, **the guest sees nothing new**: the payment screen, its
QR, its countdown, and every other guest-facing surface look and behave exactly as they do
today. The new field on the checkout response is carried but not rendered.

## Clarifications

### Session 2026-08-14

- Q: Which checkout outcomes must carry `ext_ref_id` — only a freshly opened session, or
  also the "payment already started" refusal and the concurrent-race answer, both of which
  are rebuilt from what was stored rather than from a live gateway answer? → A: **All
  three.** A caller gets the same field with the same meaning whatever happened. The
  reference must therefore be readable back at response-build time, not written and
  forgotten.
- Q: Where on the admin console does the external reference appear? → A: **An order-level
  line inside the existing per-order payment view.** No new column on the orders list, no
  new page, no reflow — the payment view is already where an operator goes to investigate a
  payment, and the reference belongs beside the notification history it explains.
- Q: When exactly does the reference arrive, and does any later gateway message carry it? →
  A: It arrives **on the gateway's answer to the session-open call — the same call that
  yields the QR payload** — as the answer's `eri`. Inbound notifications do not carry it.
  Session open is the only moment it is ever available, so that is the moment it must be
  written down.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Operator traces a stranded order to the gateway (Priority: P1)

An operator is handed an order that a guest insists was paid, but which this system still
shows as unpaid or expired. They open that order's payment view in the admin console. Along
with the notification history and the seat holds they can already see, they now find the
external reference the gateway issued when the payment session was opened. They copy it,
find the matching transaction in the gateway's own records, confirm what actually happened,
and — if it settled — top up any quota that was resold and ask the gateway to resend the
notification, which settles the order through the same path every ordinary purchase takes.

**Why this priority**: This is the entire reason the reference is worth keeping. It is the
only story that turns an unanswerable support case into an answerable one, and it is
independently valuable even if no API response ever carries the field. It also subsumes the
storage work: nothing can be displayed that was not first recorded.

**Independent Test**: Complete a checkout against the gateway so a payment session opens,
then open that order in the admin console and confirm the displayed reference is character
for character the one the gateway returned. Verifiable with no change to any API response
shape.

**Acceptance Scenarios**:

1. **Given** an order whose payment session was opened successfully, **When** an operator
   opens that order's payment view, **Then** the external reference issued by the gateway
   is displayed, in full and copyable.
2. **Given** an order that never reached a payment session (still awaiting checkout, or
   whose session open failed), **When** an operator opens its payment view, **Then** the
   view renders normally and shows an explicit "no reference" placeholder rather than a
   blank, an error, or a missing element.
3. **Given** an order created before this feature existed, **When** an operator opens its
   payment view, **Then** it behaves exactly as in scenario 2 — no error, no broken layout.
4. **Given** an order whose payment session opened and which has since received
   notifications, **When** an operator opens its payment view, **Then** the reference is
   still shown unchanged alongside the notification history, and the notification history
   itself is unchanged from today.

---

### User Story 2 - Checkout response carries the reference (Priority: P2)

A caller of the checkout endpoint — the app's own payment screen today, an integration or
an operator using the API directly tomorrow — receives `ext_ref_id` on the response
alongside the payment payload it already gets. It can log it, correlate it, or hand it to
support without a second round trip and without database access.

**Why this priority**: Genuinely useful and explicitly requested, but strictly second to
P1: an operator with admin access can already answer the support question once P1 ships,
whereas a caller holding the reference still needs someone with admin access to act on it.

**Independent Test**: Call checkout for a bookable order and assert `ext_ref_id` is present
on the response and equals the value the gateway returned.

**Acceptance Scenarios**:

1. **Given** a checkout that successfully opens a payment session, **When** the response is
   returned, **Then** it carries `ext_ref_id` holding the gateway's external reference,
   alongside every field it carries today, all unchanged.
2. **Given** an order whose payment already started, **When** checkout is called again and
   is refused as already-started, **Then** the payload accompanying that refusal carries the
   same `ext_ref_id` the original checkout returned.
3. **Given** two checkouts racing on one order, **When** the loser is served the payment
   payload the winner stored, **Then** it carries the winner's `ext_ref_id` — both callers
   name the same session.
4. **Given** a gateway that opened a session but supplied no external reference, **When**
   the response is returned, **Then** `ext_ref_id` is present and empty rather than absent,
   and the checkout still succeeds — a missing reference is a traceability gap, never a
   reason to refuse a guest a payable order.
5. **Given** a checkout that fails to open a session, **When** the error response is
   returned, **Then** it is what it is today — this feature adds nothing to failure paths.

---

### User Story 3 - The guest's experience is untouched (Priority: P3)

A guest books, fills in the holder forms, reaches the payment screen, scans the QR, and
pays. Every screen they see is identical to today's — same layout, same copy, same
countdown, same QR. The new field rides on the response they never look at.

**Why this priority**: This is a guard, not a capability. It carries no new value on its
own, but it is what makes the feature safe to ship: the requested change is explicitly
"keep the ui just like today", and an unnoticed leak of an internal gateway identifier onto
a guest-facing screen would violate the request outright.

**Independent Test**: Walk the guest purchase journey before and after the change and
confirm no guest-facing surface renders the reference and no guest-facing layout differs.

**Acceptance Scenarios**:

1. **Given** the guest payment screen for an order with a live payment session, **When** it
   renders, **Then** the external reference appears nowhere on it and the screen is
   unchanged from today in layout and content.
2. **Given** any other guest-facing surface — order lookup, ticket lookup, the receipt and
   ticket email — **When** it renders, **Then** the external reference appears nowhere on
   it.

---

### Edge Cases

- **The gateway opens a session but returns no reference.** Checkout succeeds, the order is
  payable, `ext_ref_id` is empty, and the admin view shows the "no reference" placeholder.
  The gap is recorded in the system's own logs so an operator can see the gateway stopped
  supplying it, rather than discovering it one stranded order at a time.
- **The session open fails.** Nothing is recorded, no reference exists, and the failure
  response is unchanged. This includes the duplicate-reference refusal, where the order is
  unpayable from birth and its seats have already been released.
- **Checkout is retried on an order whose payment already started.** The system refuses to
  open a second session and answers from what it stored. That answer carries the same
  `ext_ref_id` the original checkout returned (FR-011) — the caller cannot tell from the
  reference which of the two calls it came from, which is the point.
- **Two checkouts race and one loses.** The loser is served the payment payload the winner
  stored, carrying the winner's reference. Both callers end up naming the one session that
  actually exists.
- **Orders predating this feature.** No reference exists and none can be recovered. Every
  surface degrades to the "no reference" placeholder; nothing is back-filled and nothing
  errors.
- **A notification arrives for an order whose reference was recorded.** The reference is
  not altered, replaced, or removed by any later notification, marker, settlement,
  expiry, or cancellation. It states what the gateway called that session at the moment it
  opened, and that fact does not change afterwards.
- **The reference is unusually long, or contains characters that need escaping.** It is
  stored and displayed verbatim, never truncated silently in storage. A display that must
  shorten it keeps the full value copyable.

## Requirements *(mandatory)*

### Functional Requirements

**Capture and storage**

- **FR-001**: When a payment session is opened successfully, the system MUST durably record
  the external reference the gateway issued for that session, associated with the order the
  session was opened for, in the payment records.
- **FR-002**: The recorded reference MUST be written as part of completing that checkout —
  an order that can be paid MUST NOT exist with its reference unrecorded once the session
  has opened and the gateway supplied one.
- **FR-003**: The recording MUST NOT alter, replace, or remove anything already recorded
  against the order. Existing payment history — every notification and every marker, their
  order, their content — MUST remain exactly as it is today.
- **FR-004**: A session that opened without the gateway supplying a reference MUST still
  produce a payable order. The absent reference MUST be distinguishable from an unopened
  session at read time, and MUST be visible in the system's operational logs.
- **FR-005**: The reference MUST NOT be altered by any later event in the order's life —
  notification, settlement, expiry, cancellation, refusal, or operator-requested redelivery.
- **FR-006**: Recording the reference MUST NOT introduce any gateway call or external round
  trip inside a database transaction, and MUST NOT extend the transaction that reserves or
  restores quota.
- **FR-007**: Recording the reference MUST NOT change any order's status, quota, holds,
  deadlines, or any other stored value.

**Checkout response**

- **FR-008**: The checkout response MUST include the external reference under the name
  `ext_ref_id`.
- **FR-009**: Every other field of the checkout response MUST be unchanged in name, type,
  and value.
- **FR-010**: `ext_ref_id` MUST always be present on a successful checkout response, holding
  an empty value when no reference exists, so a caller never has to distinguish "absent
  field" from "no reference".
- **FR-011**: `ext_ref_id` MUST carry the order's real recorded reference on **every**
  checkout outcome that returns a payment payload, not only on the one that opened a live
  session. That means the freshly opened session, the "payment already started" refusal
  that answers a retried checkout, and the answer served to a checkout that lost a
  concurrent race. All three describe the same payment session, so all three MUST name it
  identically.
- **FR-012**: It follows from FR-011 that the recorded reference MUST be readable back
  after it is written, by whatever builds the checkout response — a reference that is
  written and never read again cannot satisfy the two rebuilt outcomes.
- **FR-013**: Failure responses from checkout MUST be unchanged.

**Admin console**

- **FR-014**: The admin console MUST display an order's external reference to an
  authenticated operator.
- **FR-015**: It MUST appear as an **order-level** value inside the existing per-order
  payment view, presented alongside that order's seat holds and notification history —
  stated once for the order, not repeated per notification row.
- **FR-016**: The orders list MUST NOT gain a column for it, and no new admin page or route
  may be introduced.
- **FR-017**: The reference MUST be displayed in full and be selectable for copying. A
  presentation that abbreviates it for layout MUST still make the complete value obtainable
  without leaving the view.
- **FR-018**: An order with no reference MUST render an explicit placeholder consistent with
  how the console already renders absent values, never a blank space, an error, or a missing
  element.
- **FR-019**: No other admin surface may change: the notification history, the seat-hold
  table, the orders list, and every existing control MUST behave exactly as they do today.
  In particular, no surface for recording a payment by hand is introduced.

**Guest surfaces**

- **FR-020**: No guest-facing surface may display the external reference — not the payment
  screen, not order or ticket lookup, not the receipt or ticket documents, not the emails.
- **FR-021**: No guest-facing layout, copy, or behavior may change as a result of this
  feature.

**Access**

- **FR-022**: The reference MUST be readable only by an authenticated operator through the
  admin surface, and by the caller of the checkout for that order. It MUST NOT be readable
  through any unauthenticated read of another order.

### Key Entities *(include if feature involves data)*

- **Payment record**: the per-order record of what happened with payment — today a log of
  gateway notifications and this system's own conclusions about them. It gains the
  **external reference**: the gateway's identifier for the payment session opened against
  that order.
- **External reference**: an opaque, gateway-issued string identifying one payment session.
  Issued once, at session open. Never derived, never edited, and meaningful only to the
  gateway — this system stores and displays it but attaches no behavior to its content. It
  is distinct from the order number (this system's identifier, given *to* the gateway) and
  from the network transaction id (the gateway's identifier for a *settled* transaction,
  which arrives on notifications and is already shown).
- **Order**: unchanged. It is what the reference is associated with; no order-level rule,
  status, or figure is affected.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For 100% of orders whose payment session opens with a gateway-supplied
  reference, that reference is retrievable afterwards and matches the gateway's value
  exactly.
- **SC-002**: An operator investigating a stranded payment can obtain the gateway reference
  for an order in under 30 seconds, from the order's payment view, without leaving the
  admin console and without asking anyone for database access.
- **SC-003**: 100% of checkout responses that carry a payment payload — freshly opened
  session, already-started refusal, and lost-race answer alike — carry `ext_ref_id`, and all
  three name the same session for the same order. 0% of callers must make an additional
  request to obtain it.
- **SC-004**: 0 guest-facing screens differ from their pre-change appearance, verified by
  walking the full guest purchase journey.
- **SC-005**: 0 regressions in the existing acceptance suite, including the run with the
  read cache disabled.
- **SC-006**: 100% of orders that predate this feature, or whose session never opened,
  render without error on every surface that shows the reference.
- **SC-007**: Checkout completion time is unchanged within normal run-to-run variation — the
  additional recording adds no perceptible delay to the guest reaching a scannable code.

## Assumptions

- **The reference is not a secret, but it is not public either.** It is an opaque gateway
  identifier with no monetary or authenticating power, so showing it to an operator and
  returning it to the caller that opened the session is safe. It is nevertheless withheld
  from every unauthenticated read of someone else's order, on the same footing as the other
  payment details.
- **One reference per order, in practice.** Checkout refuses to open a second session for an
  order that already has one, so an order carries at most one reference. If a second session
  could ever exist, the most recent reference is the one displayed and the earlier one is
  not destroyed.
- **The reference is stored as an opaque string.** No format, length, or character-set rule
  is assumed or enforced beyond what storage requires; the system never parses it, derives
  from it, or validates it.
- **Nothing is back-filled.** References for orders whose sessions opened before this change
  are not recoverable and no attempt is made to reconstruct them.
- **The reference is captured at session open, not from notifications.** It arrives on the
  gateway's answer to the session-open call — the same answer that yields the QR payload and
  its expiry — and inbound notifications do not carry it. A notification can therefore never
  supply a missing reference, which is what makes the write-once-at-session-open rule
  (FR-005) the only workable one.
- **The reference must be readable back, not merely appended.** FR-011 makes the two rebuilt
  checkout answers carry it, and the admin view reads it long after the session closed.
  Where it is stored is settled — the payment records — but planning must give it a read
  path that respects the existing domain boundaries, since nothing writes to or reads from
  the payment records on the checkout path today.
- **"The admin page" means the existing admin orders surface**, whose per-order payment view
  is already the place an operator goes to investigate a payment. The reference is added
  there as one order-level value; no new page, route, or list column is created (FR-015,
  FR-016).
- **"Keep the UI just like today" is read as: change no guest-facing surface at all**, and
  change the admin console only by surfacing this one value within the existing layout. No
  redesign, no new page, no reflow.
- **Existing architectural boundaries hold.** The payment gateway abstraction, the isolation
  between the order and payment domains, and the transaction rules that keep gateway calls
  and cache calls out of order-writing transactions all apply unchanged. How the reference
  travels from the gateway answer to the payment records within those boundaries is a design
  question for planning, not a scope question.
- **Coverage arrives with the change.** The end-to-end acceptance suite is the gate for the
  covered flows this feature touches — checkout and the admin order views — per the
  project's acceptance-coverage principle.

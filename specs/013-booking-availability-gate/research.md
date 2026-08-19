# Phase 0 Research: Booking Availability Gate

**Feature**: 013-booking-availability-gate · **Date**: 2026-08-11

The Technical Context carried no `NEEDS CLARIFICATION` markers — the stack, storage, and
test tiers are all established by existing specs. What follows is therefore not
technology selection but the seven design decisions this feature actually turns on, each
resolved against the code as it stands today.

## Starting position: what the code does now

Read before deciding anything:

| Location | What it establishes |
|----------|---------------------|
| [selection-summary.tsx:119-129](../../frontend/components/booking/selection-summary.tsx#L119-L129) | "Buy Ticket" is not a button. `SelectionSummary` renders `TermsDialog` directly, and `TermsDialog` renders `DialogTrigger` with the label "Buy Ticket". The press is pure client state. |
| [terms-dialog.tsx:86-114](../../frontend/components/booking/terms-dialog.tsx#L86-L114) | `handleAgree` is the first server contact that can refuse: `book` then `agreement`, back to back. |
| [demand.go:42-68](../../backend/internal/order/demand.go#L42-L68) | `expandItem` → `expandTicket` / `expandPackage` produce the existence, sales-window and price verdicts, keyed to `apperr` codes `TICKET_TYPE_NOT_ON_SALE` / `PACKAGE_NOT_ON_SALE`. |
| [demand.go:118-131](../../backend/internal/order/demand.go#L118-L131) | `aggregateDemand` sums per-ticket-type demand across lines — the rule that stops a bundle and a standalone ticket being judged in isolation. |
| [service.go:161-170](../../backend/internal/order/service.go#L161-L170) | Inside the transaction, `CheckAndDeductQuota` is called per ticket type in sorted-UUID lock order. This is the only quota authority. |
| [service.go:98-106](../../backend/internal/order/service.go#L98-L106) | `Book` already performs a **read-only pre-transaction guard** for missing terms via `s.events.CurrentTerms`. Precedent for exactly the kind of check this feature generalizes. |
| [event.sql:20-23](../../backend/internal/event/queries/event.sql#L20-L23) | `GetTicketTypeByID` selects `quota` and has no `FOR UPDATE`. The remaining-quota figure is already being read on the booking path and thrown away. |
| [apperr.go:70-76](../../backend/pkg/apperr/apperr.go#L70-L76) | `Numeric()` maps `CodeValidation`, `CodeTicketTypeNotOnSale` **and** `CodePackageNotOnSale` all onto `400001`. The numeric envelope code cannot distinguish them. |

That last row is the finding that shapes the whole contract.

---

## D1 — The check answers with HTTP 200 and a decision body, not a 4xx error

**Decision**: `POST /ticket/availability` returns `200` with
`data: { available: boolean, reasons: [...] }`. Only a malformed or unresolvable
*request* (bad UUID, quantity ≤ 0, both/neither of `ticket_type_id`/`package_id`, unknown
event) returns 4xx, through the existing `apperr` envelope.

**Rationale**: Two independent reasons, either sufficient.

1. **The envelope carries one code; FR-006 needs several.** `apperr.Error` has a single
   `Code`, rendered by `Numeric()` into a single integer. A selection can be
   simultaneously sold out on line 1 and off-sale on line 2, and the spec requires both
   be reported. Stuffing a list into `Data` while the top-level code names only one of
   them produces a body that contradicts itself.
2. **`Numeric()` already collides.** `TICKET_TYPE_NOT_ON_SALE` and
   `VALIDATION_ERROR` both render as `400001`, so a client branching on the numeric code —
   which is exactly what `API_CODES` in `frontend/lib/api-client.ts` does — literally
   cannot tell "your ticket stopped selling" from "your request was malformed". FR-012
   requires distinct messages for those two. A decision body sidesteps this by carrying
   the stable **string** codes, which do not collide.

"Unavailable" is also simply not an error: the question was well formed and the server
answered it correctly. Reserving 4xx for malformed input keeps that honest.

**Alternatives considered**:

- *409 Conflict with a reason list in `Data`* — rejected on (1) above; also invites the
  frontend's generic `ApiError` path to swallow the structured detail.
- *Adding new numeric sub-codes (400005, 400006, …) to `Numeric()`* — rejected. It edits a
  registry the 008 contract publishes to clients, to solve a problem the decision body
  does not have. Worth doing one day on its own merits; not as a side effect of this.
- *`GET` with the selection in the query string* — rejected. The selection is a list of
  `{id, quantity}` objects; it does not belong in a URL, and the user asked for a POST.

---

## D2 — Reuse `expandItem` per line, collecting verdicts instead of failing fast

**Decision**: `EvaluateAvailability` loops the request's items calling the **existing,
unmodified** `expandItem`. Where `bookOnce` returns on the first error, the evaluator
catches each `*apperr.Error`, records it against that line's index, and carries on to the
next item.

**Rationale**: FR-013 requires the check's wording for a reason to match booking's wording
for the same reason. The cheapest way to guarantee that is not to write the wording twice
— it is to call the same function. `expandTicket` already emits
`"Ticket type %q is not currently on sale."` with the type's real name; reimplementing
that string in a second place is how the two drift apart on the first rename.

Fail-fast stays where it belongs. `bookOnce` is a transaction holding row locks; the
first refusal should abort it immediately. The evaluator holds no locks and its whole
purpose is completeness, so it keeps going.

**Alternatives considered**:

- *Refactor `expandItem` to accumulate errors and have booking take the first* — rejected.
  It changes the booking path to serve the check's convenience, which is precisely the
  FR-011 hazard: booking's validation should be untouched by this feature.
- *A separate, simpler set of availability rules* — rejected. It would pass a selection
  the booking transaction then refuses, which is the failure this feature exists to
  remove.

---

## D3 — Quota is read without locking, from a field the adapter already discards

**Decision**: Add `QuotaRemaining int32` to `order.TicketTypeInfo`.
`eventProviderAdapter.TicketTypeForCheckout` populates it from `row.Quota`, which
`GetTicketTypeByID` already selects. The evaluator compares aggregated demand against it.
No new query, no new interface method, no `FOR UPDATE`, no migration.

**Rationale**: The figure is already crossing the boundary and being dropped on the floor
at [adapters.go:36-43](../../backend/cmd/api/adapters.go#L36-L43). Adding a `RemainingQuota(ctx, tx, ids)`
method to `EventProvider` would mean new SQL, a new adapter method, and a second round
trip, all to fetch a column the first round trip already returned.

For **package** lines, `expandPackage` yields per-ticket-type demand but not the
constituents' quota. The evaluator therefore fetches `TicketTypeForCheckout` for any
ticket type in the aggregated demand map that no ticket line already fetched. This is the
right primitive rather than the packages table's derived `available_units`, because
FR-005 scenario 4 — a bundle and a standalone ticket drawing on the same type, each
affordable alone but not together — is only answerable per ticket type.

**The hazard, stated plainly**: this puts a remaining-quota integer within reach of
`bookOnce`, where reading it to decide anything would reintroduce the oversell that the
row-locked `UPDATE` exists to prevent (Principle IV, Principle VII "never authoritative
for inventory"). The mitigation is a test, not a comment — see
[contracts/availability.md](contracts/availability.md) §6.

**Alternatives considered**:

- *New `EventProvider.RemainingQuota` method* — rejected as above: more surface, more SQL,
  a second read, no benefit.
- *Reuse the packages `available_units` aggregate* — rejected: cannot express cross-line
  aggregation, so it fails FR-005.

---

## D4 — One read-only transaction, no locks

**Decision**: The evaluator runs inside `db.InTx`, purely for a consistent snapshot across
the several primary-key reads. It issues no `UPDATE`, no `SELECT … FOR UPDATE`, and no
`CheckAndDeductQuota`.

**Rationale**: `EventProvider.TicketTypeForCheckout` and `PackageForCheckout` take a
`pgx.Tx` by contract, so a transaction is required to call them at all. A read-only one
costs a BEGIN/COMMIT pair and buys snapshot consistency: without it, a selection spanning
five ticket types could be judged against five different instants and report a
self-inconsistent picture.

What matters far more is what it must *not* do. Principle IV's rationale is that the
quota-deducting `UPDATE` holds a row lock until commit, so anything slow inside that
transaction serializes every concurrent buyer of the same ticket type. A check that took
the same locks would inherit that property while delivering an answer that is advisory
anyway — the worst of both. Lock-free is not an optimization here; it is the constraint
that keeps this endpoint from becoming a throughput ceiling on the product's hot path.

**Alternatives considered**:

- *Pool reads with no transaction* — would require changing the `EventProvider` signatures
  to accept a `Querier`, touching the booking path for no gain.
- *`FOR UPDATE` for a "more accurate" answer* — actively wrong. It would serialize buyers
  behind an advisory read, and the answer would still be stale by the time Agree lands.

---

## D5 — Terms availability is part of the decision, reusing booking's own guard

**Decision**: The evaluator calls `s.events.CurrentTerms(ctx, req.EventID)` and, on
`ErrNoTerms`, reports an order-level reason with code `TERMS_MISSING`.

**Rationale**: `Book` already does exactly this as a pre-transaction guard
([service.go:98-106](../../backend/internal/order/service.go#L98-L106)) — the precedent
and the code are both there. Without it, an event with no authored terms opens a dialog
whose body renders "not available yet" and whose Agree button can never usefully be
pressed. FR-004 folds it into the decision so the empty dialog is never shown at all.

Note this is the one reason that is **not** attributable to a line: it is a property of
the order. The contract therefore allows a reason with a null line reference.

---

## D6 — The dialog becomes controlled; the button becomes a button

**Decision**: `TermsDialog` takes `open` / `onOpenChange` props and no longer renders
`DialogTrigger`. `SelectionSummary` renders the real `<button>`, owns the check mutation,
owns the refusal message, and sets `open` only on `available: true`.

**Rationale**: The open state must be decided by a server answer, and the component that
awaits the answer is the one that should hold it. Keeping the trigger inside `TermsDialog`
would mean intercepting `onOpenChange`, firing the check from inside the component whose
job is the terms, and blocking its own opening — the dialog would be lying about its own
state for the duration of the round trip.

`SelectionSummary` already owns the empty/non-empty distinction for this exact control and
already shares `BUY_TICKET_CLASS` between a live and an inert rendering, so a third state
(checking / refused) belongs to the same place.

**Consequence for `terms-dialog.test.tsx`**: it currently mounts the component and clicks
the trigger. Those tests must mount it with `open={true}`. The dialog's own behavior —
terms fetch, agree, book, the retry latch, `TERMS_CHANGED` refetch — is otherwise
untouched and those assertions should survive as-is.

---

## D7 — Its own rate-limit group, more generous than booking's

**Decision**: Mount on a dedicated `e.Group("/api/v1", httpx.RateLimitPerIP(availabilityRate,
availabilityBurst, rateLimitWindow))` with `availabilityRate = 1.0`,
`availabilityBurst = 10`.

**Rationale**: Sharing `bookGroup`'s limiter would be wrong in both directions. Booking is
throttled at `0.33/s, burst 5` because it creates rows and holds quota for an hour; a
read-only check deserves no such caution. More importantly, sharing would make the check
*consume the guest's booking budget* — and the intended flow is check-then-book, plus
another check on every adjust-and-retry (User Story 2), so a guest who hits two refusals
would be rate limited out of the purchase this feature was built to smooth.

The endpoint is nonetheless unauthenticated and hits the database, so FR-010 requires a
limit. `1/s` with a burst of 10 absorbs an impatient guest's repeated presses while
keeping enumeration impractical.

**Alternatives considered**:

- *Mount on the unthrottled `api` group* — rejected: FR-010, and it is a public endpoint
  that reads the database on every call.
- *Share `bookGroup`* — rejected as above; it would starve the legitimate retry path.

---

## Consolidated decisions

| # | Decision | Chief rationale |
|---|----------|-----------------|
| D1 | 200 + decision body; 4xx only for malformed requests | One envelope code cannot carry several reasons; `Numeric()` already collides `NOT_ON_SALE` with `VALIDATION_ERROR` |
| D2 | Reuse `expandItem` per line, collecting verdicts | Guarantees FR-013 wording parity by construction, not by discipline — **see D8–D14: the guest now reads neither sentence, so what the reuse buys is verdict parity** |
| D3 | `TicketTypeInfo.QuotaRemaining`, populated from a column already read | No new SQL, no new interface method, no second round trip |
| D4 | One read-only, lock-free transaction | Snapshot consistency without inheriting Principle IV's serialization hazard |
| D5 | Terms presence folded into the decision, via booking's existing guard | Stops the empty dialog; precedent already in `Book` |
| D6 | Controlled `TermsDialog`; `SelectionSummary` owns the gate | The component that awaits the answer should own the state the answer decides — **superseded in part by D9: the dialog must close ITSELF, because a parent-driven close skips base-ui's `onOpenChange`** |
| D7 | Dedicated per-IP limiter, `1.0/s` burst 10 | Public DB-reading endpoint, but must not eat the booking budget |

## Deliberately unresolved

**The check cannot eliminate the race, and no decision here pretends to.** Between a
passing decision and the Agree press, another buyer can take the last seat. FR-003 and
FR-011 accept this: the check narrows the window in which a guest wastes effort, and
booking still refuses. This is why every rejection path in `bookOnce` must survive the
change untouched, and why scenario 6 in User Story 1 exists.

---

# Phase 0 Research: the 2026-08-19 amendment

**Date**: 2026-08-19 · **Trigger**: the clarification session recorded in
[spec.md](spec.md) — every availability refusal collapses to one fixed general message,
at the check and at Agree alike.

The decisions above (D1–D7) are the delivered feature and stand unchanged. D8–D14 cover
only what the amendment forces.

## Starting position: what the code does now

`frontend/lib/availability.ts` is built on one stated rule — *"the SERVER's sentence
wins"* (`:5-13`) — and `reasonMessage` (`:40-43`) is a deliberate pass-through that
ignores `reason.code` entirely. `reasonMessages` (`:46-48`) is 1:1 with
`decision.reasons`, so N offending lines yield N guest sentences, rendered as N `<li>`
peers at `selection-summary.tsx:196-215`. `failureMessage` (`:59-69`) words only the
transport case and echoes the server's message for everything else — which is how
`"Only fewer than N ticket(s) remain."` reaches the guest at Agree today.

The amendment inverts that rule for five of the seven codes. The module's docstring is
therefore not a comment to touch up but the written rationale for the retired behaviour.

## D8 — Booking classifies on the numeric code, and `400001` counts as availability

**Decision.** The client sorts a booking refusal into "availability" (general message,
dialog closes) or "everything else" (present in-dialog behaviour) using only
`ApiError.code`:

| Numeric | Classification |
|---------|----------------|
| `400002` | availability — `INSUFFICIENT_QUOTA`, unambiguous |
| `404001` | availability — only `TICKET_TYPE_NOT_FOUND` / `PACKAGE_NOT_FOUND` reach it from `Book` |
| `400001` | **availability** — see below |
| `409001`, `429001`, `500000`, anything else | not availability |

**Rationale.** `400001` is genuinely ambiguous on the wire: `apperr.Numeric`
(`apperr.go:99-101`) collapses `TICKET_TYPE_NOT_ON_SALE` and `PACKAGE_NOT_ON_SALE` — two
FR-012 availability races — into the same number as the whole `VALIDATION_ERROR` family,
which the spec's Assumptions deliberately exclude from the general message. There is no
third signal: `apperr.Body` carries `{code, message, data}` and never the stable string
(`apperr.go:180-201`), pinned by `handler_test.go:83-108`.

Of the two readings, only one is wrong on a path a real guest can reach. The sale-window
race — an admin closes sales, or a window lapses, between the check and Agree — is
reachable, and User Story 1 scenario 3 and the Edge Cases name it. Every `VALIDATION_ERROR`
booking can raise needs a request body the selection page cannot construct: empty items,
quantity below 1, duplicate lines, a null id, or an item from another event (spec
Assumptions already call that unreachable). The single exception — *"Package %q has no
components and cannot be purchased."* (`demand.go:83-86`) — is in substance an
availability condition anyway: the bundle can no longer be bought.

**Residual risk, stated rather than hidden.** A hand-crafted or tampered request that
trips a genuine validation error at Agree will read *"Someone was a bit faster!"*. That is
a wrong sentence shown to someone who is not using the selection page. Accepted.

**Alternatives considered.**

- *Serialize the stable string code on error responses.* Clean, and it would make the two
  paths symmetric. Rejected: it changes the error contract every endpoint shares,
  contradicts the `{code, message, data}` shape PRD §1.5 states blanket, and breaks the
  test written to pin it. The amendment asks for a wording change.
- *Treat only `400002` as availability.* Rejected: it leaves the sale-window race showing
  `"Ticket type "Early Bird" is not currently on sale."` inside the dialog at Agree, which
  is precisely the two-stories outcome FR-013 forbids.
- *Match on the message prose.* Rejected without hesitation — it is text-matching on the
  sentences this amendment exists to stop rendering.

## D9 — The dialog closes itself; the parent must not flip `open`

**Decision.** `TermsDialog` classifies its own `book` failure, calls its own
`onOpenChange(false)`, and reports the refusal upward through a new callback. The parent
does **not** close it by setting `termsOpen` to `false`.

**Rationale.** This looks like the obvious seam and is a trap. In the installed
`@base-ui/react`, `onOpenChange` fires only from `store.setOpen(...)`
(`dialog/root/useDialogRoot.js:29-31`), while a controlled `open` prop is synced through
`store.useControlledProp('openProp', openProp)`
(`dialog/root/useRenderDialogRoot.js:66`) **without firing it**. A parent-driven close
therefore skips `TermsDialog.handleOpenChange` (`terms-dialog.tsx:81-92`) entirely, and
with it `setAgreed(false)`, `setBookedOrderId(null)`, `setNavigating(false)`,
`book.reset()` and `agreement.reset()`. Since `errorMessage` derives from `book.error`
(`:126`) and `failed` from `book.isError` (`:79`), and the dialog is never unmounted
(`selection-summary.tsx:229` renders it unconditionally), reopening would show a ticked
checkbox, a "Retry" button and the stale server sentence — the exact state
`terms-dialog.tsx:39-41` says must never occur.

Nothing guards against closing mid-flight in either direction, so a programmatic close
collides with no existing logic. And an availability refusal always throws from the `book`
leg (`:99-102`) *before* `setBookedOrderId` (`:103`), so `bookedOrderId` is null and no
held order is abandoned.

**Alternative considered.** Widening `onOpenChange` to carry the close reason — base-ui
already types a second `eventDetails` argument (`DialogRoot.d.ts:40`) that
`terms-dialog.tsx:59` drops. Rejected: it muddles "the dialog's open state changed" with
"the purchase was refused". A separate callback is the cleaner seam.

## D10 — A throttled check is a third carve-out, not a race

**Decision.** HTTP 429 on the check gets its own sentence, matching the one the dialog
already shows.

**Rationale.** The check sits behind its own per-IP limiter (`main.go:396-399`) using
echo's default deny handler, so a throttled check answers 429 with the literal middleware
string `"rate limit exceeded"`. `failureMessage` returns `CHECK_FAILED` only for
`status === 0`, so that raw prose is echoed straight into the alert named *"Why this
selection cannot be bought"*. It is not an availability race and not either FR-012a case.

`terms-dialog.tsx:234-236` already words 429 for the booking path. So today the two points
tell different stories for one condition — the shape of defect FR-013 exists to prevent.
Giving the check the same sentence closes it.

**This is a spec gap.** FR-012a lists two exceptions; there are three. Recorded in
[plan.md](plan.md) under *Spec deltas this plan requires*.

## D11 — Two lines are a title and a body, not two list items

**Decision.** Render FR-012's message as `AlertTitle` + `AlertDescription` inside the
existing `Alert`, keeping `aria-label="Why this selection cannot be bought"`.

**Rationale.** The current `<ul>`/`<li>` structure exists so a screen reader announces
"3 items" — correct for N peer refusals, wrong for one message whose two lines are a
heading and its explanation. The accessible name is load-bearing and must survive: four
e2e callers reach the region through it (`journey.ts:119-121`), and it is what
distinguishes this alert from the page's other unnamed `role="alert"` surfaces — the
event-load `StatusAlert` (`tickets/page.tsx:47`) and the App Router's route announcer.

**Bonus fix.** `key={message}` (`:209`) collides today: `quotaShortfalls` builds the
shortfall sentence once (`availability.go:180`) and emits it for *every* line drawing on
that ticket type (`:181-190`), so a bundle plus a standalone on one type yields duplicate
React keys. Collapsing to one message removes the list and the bug with it.

## D12 — Do not rely on an announcement raised at the moment the dialog closes

**Decision.** The refusal is a persistent, named alert the guest lands on — not a
transient announcement timed to the close.

**Rationale.** base-ui hides the page from assistive tech while the dialog is open via
`markOthers(..., { ariaHidden: true })`, and the cleanup that un-hides it is a **passive**
`useEffect` keyed on `open` (`FloatingFocusManager.js:335`), so it runs after paint. A
message inserted in the same commit that flips `open` to `false` lands inside a still-
`aria-hidden` subtree for at least one frame, and an assertive announcement raised then can
be swallowed. The repo already documents this behaviour empirically at
`e2e/support/journey.ts:138-143`.

## D13 — Refused checks get a WARN log

**Decision.** `EvaluateAvailability` logs a refused decision at WARN with the stable
string codes, mirroring `pkg/httpx/error_handler.go:39-44`.

**Rationale.** Amended FR-006 keeps the per-line detail and justifies it *"so a refusal
can be explained after the fact from server-side records"*. There are no such records: the
endpoint writes none, and a `200` never reaches the error handler where booking refusals
are logged. Without this the requirement is unsatisfiable as written. It is additive,
outside every transaction, and changes no response.

**This is the amendment's only functional backend change.** Everything else in Go is
comment corrections.

## D14 — The feature's own artifacts re-seed the retired rule

**Decision.** Reconcile them in this same change; they are not stale prose but active
instructions to future implementers.

The sharpest is `contracts/availability.md:226-228`, which declares `TermsDialog`'s
internals *"unchanged"* — FR-013a requires exactly the opposite. Next is
`quickstart.md:91-94`, which tells a reader that if `terms-dialog.test.tsx` needs
rewriting then "the dialog's internals were changed further than this feature intends";
under FR-013a they must be. Then `contracts/availability.md:114-120` (the
`message` parity section, FR-013's retired meaning as a contract guarantee) and `:221`
("branches on the **string** `code`, not on `API_CODES`' numeric values"), which prohibits
precisely what D8 requires on the booking path.

Also carrying the retired premise: `contracts/availability.md:87-90`, `:128`;
`research.md:46-47`, `:72-76`, `:175`, `:224`; `data-model.md:60`, `:64`, `:107-109`;
`quickstart.md:53`, `:85`, `:89`; `tasks.md:108-118`.

**Do not sweep in `quickstart.md:54`** — *"two independently-broken lines returns two
reasons | FR-006"* is still true and still required.

## Consolidated decisions (amendment)

| # | Decision | Chief rationale |
|---|----------|-----------------|
| D8 | Booking classifies on the numeric code; `400001` counts as availability | The sale-window race is guest-reachable; every competing `VALIDATION_ERROR` is not |
| D9 | The dialog closes itself through `handleOpenChange` | A parent-driven close skips base-ui's `onOpenChange` and leaves `book.error` and `agreed` stale |
| D10 | A throttled check gets its own sentence | 429 is neither a race nor either FR-012a case, and the dialog already words it |
| D11 | `AlertTitle` + `AlertDescription`, name preserved | Two lines are a heading and a body; the accessible name has four e2e callers |
| D12 | A persistent named alert, not a close-timed announcement | The page stays `aria-hidden` for at least one frame after the dialog closes |
| D13 | WARN log on refused checks | FR-006's justification is otherwise unsatisfiable — no record exists today |
| D14 | Reconcile this feature's own artifacts | They instruct future implementers to rebuild the retired behaviour |

## Deliberately unresolved (amendment)

**The `400001` ambiguity is narrowed, not eliminated.** D8 picks the reading that is right
for every path the selection page can produce and accepts a wrong sentence on requests it
cannot. Closing it properly means putting a stable discriminator on error responses, which
is an API change across every endpoint — worth doing one day, out of scope for a wording
amendment.

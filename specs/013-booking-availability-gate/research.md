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
| D2 | Reuse `expandItem` per line, collecting verdicts | Guarantees FR-013 wording parity by construction, not by discipline |
| D3 | `TicketTypeInfo.QuotaRemaining`, populated from a column already read | No new SQL, no new interface method, no second round trip |
| D4 | One read-only, lock-free transaction | Snapshot consistency without inheriting Principle IV's serialization hazard |
| D5 | Terms presence folded into the decision, via booking's existing guard | Stops the empty dialog; precedent already in `Book` |
| D6 | Controlled `TermsDialog`; `SelectionSummary` owns the gate | The component that awaits the answer should own the state the answer decides |
| D7 | Dedicated per-IP limiter, `1.0/s` burst 10 | Public DB-reading endpoint, but must not eat the booking budget |

## Deliberately unresolved

**The check cannot eliminate the race, and no decision here pretends to.** Between a
passing decision and the Agree press, another buyer can take the last seat. FR-003 and
FR-011 accept this: the check narrows the window in which a guest wastes effort, and
booking still refuses. This is why every rejection path in `bookOnce` must survive the
change untouched, and why scenario 6 in User Story 1 exists.

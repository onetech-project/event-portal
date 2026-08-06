# Phase 0 Research: Ticket Package Bundles

**Feature**: `005-ticket-package-bundles` | **Date**: 2026-08-04

Nine decisions. Two were deferred by the specification (R-004, R-005); the rest arose from
reading the current implementation. No `NEEDS CLARIFICATION` remains.

---

## R-001 — Where packages live in the modular monolith

**Decision**: `packages` and `package_tickets` are owned by **`internal/event`**. The `order`
domain reaches them through one new method on the existing `EventProvider` port.

**Rationale**: Constitution Principle II forbids cross-domain JOINs for write paths and
forbids one domain importing another's repository. Availability *requires* joining
`packages ⋈ package_tickets ⋈ ticket_types`. Put packages in `event` and that join is
entirely intra-domain; put them anywhere else and it is a boundary violation on the hottest
read in the feature. `internal/event` already owns `events` and `ticket_types`, and already
exposes exactly this kind of port for quota — packages extend an established pattern rather
than inventing one.

**Alternatives considered**:

- *A new `package` domain* — rejected: the constitution's domain list is closed
  (`admin`, `event`, `order`, `payment`, `ticket`, `notification`), and adding one would
  require a MAJOR constitution amendment to buy nothing.
- *Owned by `order`* — rejected: `order` would need `ticket_types`, breaking Principle II and
  the existing `EventProvider` abstraction in the same stroke.

---

## R-002 — Deduction ordering (the latent deadlock this feature would otherwise ship)

**Decision**: Aggregate demand per `ticket_type_id` across the whole cart, then deduct in
**ascending `ticket_type_id` order**. Implemented as a pure function in
`internal/order/demand.go`, unit-tested without a database.

**Rationale**: Two distinct bugs, both invisible in the current per-item loop:

1. **Non-aggregation.** `reserveOnce` today iterates `req.Items` and deducts each
   independently (`backend/internal/order/service.go:158-187`). With individual tickets only,
   a repeated ticket type is harmless — two guarded `UPDATE`s against the same row serialise
   correctly. Packages break that: a cart of `1 × Bundle(Day1, Day2)` plus `2 × Day1` places
   demand of **3** on Day 1, and the "is it available?" question is only answerable against
   the aggregate. Aggregating first also makes the error message honest — it can name the
   ticket that actually ran out.

2. **Deadlock.** The guarded `UPDATE` holds a row lock until commit. Two transactions taking
   the same two rows in opposite order deadlock. This is **already possible today** — two
   buyers posting the same two ticket types in different `items[]` order — but is rare
   because clients happen to emit a stable order. Packages make it likely by construction: a
   bundle touches several ticket types at once, and two overlapping bundles (`Day1+Day2`,
   `Day2+Day3`) produce the crossing pattern naturally. A total order that every transaction
   obeys removes the cycle. Sorting by UUID is arbitrary; *global consistency* is the only
   property that matters.

Iterating a Go map to build the deduction list would reintroduce the deadlock, since Go
randomises map iteration order — the sort is not decoration.

**Alternatives considered**:

- *`SELECT ... FOR UPDATE` on all rows up front, ordered* — rejected: a second statement and a
  second round trip for the same guarantee the guarded `UPDATE` already gives, and it would
  still need the ordering.
- *Serializable isolation* — rejected: converts deadlocks into serialization failures needing
  a retry loop, and slows every checkout to protect a rare case.
- *Advisory lock per event* — rejected: serialises all buyers of an event, collapsing exactly
  the throughput Constitution IV's no-network-call rule exists to protect.

---

## R-003 — Order line representation for a package

**Decision**: One `order_items` row per **selected line**, with `ticket_type_id` XOR
`package_id` enforced by a CHECK constraint. A package line stores `quantity` and the
package's **own** price.

**Rationale**: `total_amount` must stay exactly `SUM(quantity × price)`. A bundle has one
price (Rp50.000), not a per-constituent price, so decomposing it into constituent rows would
force an invented allocation (Rp25.000 each? proportional to list price? rounding to whose
favour?). Every such split is arbitrary and immediately wrong in reporting. Keeping the line
whole also matches the designs, which show one summary line per selected row, and keeps the
order re-readable at its original price after an admin edits the package.

**Alternatives considered**:

- *Expand into per-constituent `order_items` rows* — rejected: the price-split problem above,
  plus it destroys the fact that the buyer bought a bundle.
- *A separate `order_packages` table* — rejected: duplicates ordering, pricing and quantity
  logic across two tables, and every read of "what is on this order" becomes a UNION.
- *Keep `ticket_type_id` NOT NULL and add a nullable `package_id` alongside* — rejected: it
  makes the two columns' relationship unexpressible, and a package line would need a
  meaningless ticket type.

---

## R-004 — Composition edits while orders are open *(deferred by the spec)*

**Decision**: **Reject** composition changes (`PUT /admin/packages/:id` altering
`components`) while the package has any `PENDING` order. Return `409`. Name, description,
price, window and status remain editable at all times.

**Rationale**: Quota restoration reconstructs an order's hold by expanding its package lines
through `package_tickets` *as it stands now* (`contracts/checkout-transaction.md` §3). If
composition changed between deduction and restoration, the amounts differ and quota drifts —
silently, and in either direction. Rejecting the edit is a two-line guard against a bug class
that is nearly impossible to detect in production. The blocking window is naturally short: it
lasts only as long as the payment deadline, after which the sweeper clears it.

**Alternatives considered**:

- *Snapshot the expansion into an `order_quota_holds` table at checkout* — rejected **for
  now**: it is the more general answer and makes composition freely editable, but it adds a
  table, a write per constituent per order, and a second reconciliation path, for a scenario
  (editing a live bundle mid-sale) that is rare and administratively avoidable. Recorded as
  the upgrade path if that need becomes real.
- *Allow the edit and accept drift* — rejected: violates the constitution's quota rules
  outright.
- *Version the composition and pin orders to a version* — rejected: same cost as the snapshot
  with more machinery.

---

## R-005 — `sqlc` fallout from making `order_items.ticket_type_id` nullable

**Decision**: Accept the regenerated nullable type and update call sites in the same commit as
the migration. `backend/sqlc.yaml` already maps nullable `uuid` to `uuid.NullUUID`, so
`OrderItem.TicketTypeID` becomes `uuid.NullUUID` and every read of it must branch.

**Rationale**: This is the one change that ripples beyond new code, so it is called out rather
than discovered during implementation. `sqlc` derives nullability from the schema: once the
column is nullable, **every** query selecting it regenerates with the nullable Go type,
breaking compilation in `internal/order/repository.go`, `dto.go` and `admin_service.go`. That
is a feature, not a nuisance — the compiler is enumerating exactly the places that must now
decide what a package line means. Doing the migration and the call-site updates as step 1 of
implementation gets the whole suite green before any package behaviour is written.

**Alternatives considered**:

- *`sqlc.narrow` / a `NOT NULL` cast in each existing query* — rejected: it would let existing
  code compile unchanged while silently panicking or zero-valuing on the first package line.
  Suppressing the compiler here suppresses the only automatic audit available.
- *Leave the column NOT NULL and store a sentinel ticket type on package lines* — rejected:
  a fake foreign key that every consumer must know to ignore.

---

## R-006 — Availability query shape

**Decision**: One CTE query per event returning every package with `available_units`,
`limiting_ticket_type_id` and a composite `purchasable` boolean
(`contracts/checkout-transaction.md` §1.2), plus one batched query for components across all
packages. Two queries total for the booking list, regardless of package count.

**Rationale**: SC-005 requires the list interactive within 2s, and the naive shape — fetch
packages, then per package fetch components and compute availability — is N+1 on the feature's
hottest read. Computing `purchasable` server-side (units > 0, package window, **every**
constituent's window, `ACTIVE`) means the client renders one boolean and cannot disagree with
the server about what is buyable; splitting that logic across the wire is how a UI ends up
offering a row that checkout then refuses.

`MIN(quota / quantity)` relies on PostgreSQL integer division flooring, which is the desired
whole-sets-only semantics. `LEFT JOIN` rather than `JOIN` so a componentless package still
appears to an administrator as broken instead of vanishing.

**Alternatives considered**:

- *Materialised view / cached availability column* — rejected: the constitution forbids
  caching live quota, and any stored availability is a second source of truth, which is the
  precise thing this feature is designed not to have.
- *Compute availability in Go from separately fetched rows* — rejected: same data, more round
  trips, and the min/floor logic would then exist in two places (Go and any admin query).

---

## R-007 — Attendee slots for bundle registrants

**Decision**: One attendee row per constituent unit, carrying `ticket_type_id` (NOT NULL) and
`package_id` (nullable). Checkout validates that attendee counts grouped by
`(ticket_type_id, package_id)` exactly equal the expansion — no more, no fewer.

**Rationale**: This is the clarification answered during `/speckit-specify` (**N forms → N
passes**, registrants may differ). Keeping `attendees.ticket_type_id` NOT NULL is what
preserves the constitution's one-pass-per-attendee rule with **no amendment** — a bundle
simply produces more attendees, and pass generation is untouched. The `package_id` column
exists only for provenance: grouping in the PDF/email and identifying bundle registrants in
admin exports.

The exact-count validation is the guard that stops a client paying for two bundle units and
registering four people. It must compare against the server's own expansion, never against a
client-declared count.

**Alternatives considered**:

- *One attendee, many passes* — rejected by the user's answer, and it would require amending
  the constitution plus making `tickets` many-to-many with ticket types and `USED` per-day.
- *One attendee shared across constituents via a join table* — rejected: more machinery, and
  it forbids the different-people case the user explicitly chose.

---

## R-008 — Same-event composition enforcement

**Decision**: Enforce in the **database** via composite foreign keys. Add
`UNIQUE (id, event_id)` to `ticket_types` and `packages`, denormalise `event_id` onto
`package_tickets`, and point both FKs at the composite keys.

**Rationale**: A bundle referencing another event's ticket would silently corrupt
availability, quota accounting and gate validation at once. A service-layer check is
correct until the first bulk import, admin script or future endpoint forgets it. The
composite-FK technique makes the state unrepresentable at the cost of one denormalised
column, and the column cannot drift because both FKs resolve against the same value in the
same row. Application-level validation still runs, to return a clean `400` rather than a raw
constraint violation — matching how the existing event/ticket-type guards behave.

**Alternatives considered**:

- *Service-layer validation only* — rejected: correctness that depends on every future caller
  remembering.
- *A CHECK with a subquery* — not supported by PostgreSQL.
- *A trigger* — rejected: same guarantee, worse discoverability, harder to test than a
  declarative constraint.

---

## R-009 — Booking-screen selection UI

**Decision**: Rewrite `frontend/app/events/[slug]/page.tsx` to the supplied designs: a single
list of ticket and package rows, an **Add** button that becomes a `− n +` stepper once
selected, a bundle badge on package rows, and a persistent **Selected Ticket** summary panel.
Extract `components/booking/selectable-row.tsx` and `selection-summary.tsx`. The stepper
ceiling is `quota_remaining` for a ticket and `available_units` for a package. The checkout
link gains `p=<packageId>:<qty>` pairs alongside the existing `t=<ticketTypeId>:<qty>`.

**Rationale**: The current page uses a quantity `<Select>` dropdown and only reveals a total
card after a selection exists (`page.tsx:170-185`, `108-123`) — neither matches the designs,
which show an always-present summary panel with an empty state and a disabled **Buy Ticket**
button. Since bundles must sit in the same list with identical mechanics (FR-004), sharing
one row component is what keeps them from drifting apart visually or behaviourally. The
`t=`/`p=` URL encoding extends the existing convention the checkout page already parses
rather than inventing a new transport.

The displayed total stays presentational: the server recomputes from stored prices, and the
page already says so.

**Alternatives considered**:

- *Keep the `<Select>` and just add package rows* — rejected: contradicts the approved
  designs, and a dropdown scales badly to the stepper-with-max behaviour FR-013 needs.
- *A separate "Bundles" section below tickets* — rejected: the designs interleave them by
  price in one list, and separating them buries the higher-value offer.
- *Client-side cart state in `localStorage`* — rejected: out of scope, and stale quota in
  persisted state is a new bug class for no gain.

---

## Summary of resolved unknowns

| ID | Question | Resolution |
| --- | --- | --- |
| R-001 | Which domain owns packages? | `internal/event` |
| R-002 | How to avoid oversell + deadlock? | Aggregate cart demand, deduct in sorted `ticket_type_id` order |
| R-003 | How is a package stored on an order? | One `order_items` row, `ticket_type_id` XOR `package_id`, package's own price |
| R-004 | Composition edits under open orders? | Reject with `409`; snapshot table recorded as upgrade path |
| R-005 | `sqlc` nullability fallout? | Accept `uuid.NullUUID`, fix call sites in the migration commit |
| R-006 | Availability query shape? | Single CTE + one batched components query; `purchasable` computed server-side |
| R-007 | Attendee model for bundles? | One attendee per constituent unit; `ticket_type_id` stays NOT NULL |
| R-008 | Same-event composition? | Composite FKs via `UNIQUE (id, event_id)` |
| R-009 | Selection UI? | Rewrite to Add → stepper rows + Selected Ticket panel; `p=` URL pairs |

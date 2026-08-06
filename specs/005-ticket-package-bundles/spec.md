# Feature Specification: Ticket Package Bundles in the Selection Step

**Feature Branch**: `005-ticket-package-bundles`

**Created**: 2026-08-04

**Status**: Draft

**Input**: User description: "Generate a comprehensive system specification, database schema, and core business logic for ticket packages (bundles). Hierarchy is Event > Ticket > Packages. Tickets carry `quota` as the single source of truth for inventory. Packages belong to an event, MUST NOT have their own quota column, and their availability derives entirely from the remaining quota of their underlying tickets. A `package_tickets` junction defines the composition. Deliver the ERD and SQL schema, TypeScript interfaces and Go structs, the race-condition-safe checkout transaction flow for a package, and the API endpoints for Events, Tickets and Packages. Keep the current schema; adding columns is fine." — with the JIVE booking-step designs supplied (Figma nodes 12-1523, 20-796, 244-6525) and the follow-up direction: **focus on the ticket list selection**.

## Clarifications

### Session 2026-08-04

- Q: A bundle spans two ticket types. When a guest buys one, how should registration and passes work? → A: **N forms → N passes.** The guest fills a separate registrant form per constituent; those registrants may be different people; one pass is issued per attendee.

### Session 2026-08-04 (scope addition)

- Q: Where should the new ticket-type description/remark appear? → A: **It replaces the booking card's non-refundable notice.** That line is currently a hardcoded English string; it becomes admin-authored per ticket type, falling back to the existing wording when left blank.
- Q: Where should the admin package form live? → A: **A Packages section on the event detail page** (`/admin/events/[id]`), below Ticket Types, matching how ticket types already work there — the composition picker needs that event's ticket types in scope anyway.

## Scope Note

This feature is centred on **step 1 of the booking flow — the ticket list selection screen** shown in the supplied designs, where bundles appear inline alongside individual tickets and are added to a running selection summary. Everything downstream (registration, payment, pass delivery) is specified only to the depth needed to keep a bundle-containing selection correct end to end, since a selection the guest cannot actually pay for delivers no value. Admin package authoring is included as a supporting story because a bundle cannot appear in the list until someone creates it.

---

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Guest sees and selects a bundle in the ticket list (Priority: P1)

A guest opens an event's booking page. The list shows every purchasable option for that event in one place: individual tickets ("Day 1", "Day 2") and bundles ("Day 1 and 2"). A bundle is visually marked as a bundle and shows a single all-in price, which may be lower than the sum of the tickets it contains. The guest presses **Add** on any row; that row switches to a quantity stepper, and the running **Selected Ticket** summary on the right lists each chosen row with its quantity and line total, a combined total, and an enabled **Buy Ticket** button. Adding a bundle works exactly like adding an individual ticket — same row layout, same stepper, same summary treatment.

**Why this priority**: This is the screen the feature exists for and the one the designs specify. Without it a bundle is invisible to buyers and generates zero revenue, no matter how correct the data model is.

**Independent Test**: Seed an event with two individual tickets and one bundle composed of both. Load the booking page and confirm all three rows render, the bundle carries its badge and its own price, Add turns into a stepper, and the summary total updates to match the selected rows. Fully testable without touching payment.

**Acceptance Scenarios**:

1. **Given** a published event with two tickets ("Day 1" Rp35.000, "Day 2" Rp35.000) and one bundle ("Day 1 and 2" Rp50.000) containing both, **When** the guest opens the booking step, **Then** three selectable rows are listed and only the bundle row displays a bundle marker.
2. **Given** the empty selection state, **When** nothing has been added, **Then** the summary shows its empty placeholder and **Buy Ticket** is disabled.
3. **Given** the guest presses **Add** on "Day 1", **When** the row updates, **Then** it shows a quantity stepper at 1, the summary lists that one line with its price, and **Buy Ticket** becomes enabled.
4. **Given** "Day 1" is already selected at quantity 1, **When** the guest also adds "Day 2" at quantity 1, **Then** the summary lists both lines, the combined ticket count reads 2, and the total reads Rp70.000.
5. **Given** the guest adds the bundle at quantity 1 instead, **When** the summary updates, **Then** it shows one line for the bundle priced at Rp50.000 — not two separate day lines — and the total is Rp50.000.
6. **Given** any selected row at quantity 1, **When** the guest decrements it, **Then** the row returns to its **Add** state and disappears from the summary.
7. **Given** a bundle and an individual ticket are both selected, **When** the guest reviews the summary, **Then** both appear as independent lines and the total is the sum of their line totals.

---

### User Story 2 - The list reflects real, live availability (Priority: P1)

Because a bundle owns no inventory of its own, what the list offers must be derived at read time from the remaining quota of the tickets inside it. A bundle whose scarcest constituent has run out must not be offered, and a bundle can never be stepped up past the number of complete sets its constituents can still cover.

**Why this priority**: Selection and availability are the same screen. A bundle row that lets a guest select more than exists converts a pleasant browse into a failed checkout, and — if the deduction were ever permissive — into overselling, which the constitution forbids outright.

**Independent Test**: Seed a bundle over two tickets with remaining quotas of 5 and 2. Confirm the page offers at most 2 bundle units. Set one constituent to 0 and confirm the bundle row is no longer purchasable.

**Acceptance Scenarios**:

1. **Given** a bundle containing "Day 1" (remaining 5) and "Day 2" (remaining 2), **When** the guest views the list, **Then** the bundle's available quantity is 2 — the lowest complete-set count across its constituents.
2. **Given** that same bundle, **When** the guest steps the quantity up, **Then** the stepper stops at 2 and cannot be raised further.
3. **Given** a bundle whose "Day 2" constituent has 0 remaining, **When** the guest views the list, **Then** the bundle is shown as unavailable and cannot be added, even though "Day 1" still has stock.
4. **Given** a bundle that specifies 2 units of "Day 1" per bundle and "Day 1" has 5 remaining, **When** availability is computed, **Then** 2 bundle units are offered (whole sets only; the leftover single unit is not offered as a partial bundle).
5. **Given** another guest completes a purchase that exhausts a constituent, **When** the first guest reloads the booking step, **Then** the bundle now reads as unavailable without any administrator action.
6. **Given** a bundle whose own sales window is open but one constituent's sales window has closed, **When** the guest views the list, **Then** the bundle is not purchasable.
7. **Given** an event's ticket list, **When** it is served, **Then** availability figures are computed fresh per request and never served from a cached copy.

---

### User Story 3 - A selection containing bundles checks out without overselling (Priority: P2)

The guest presses **Buy Ticket** with a selection that may mix individual tickets and bundles. Each bundle expands into its constituent tickets, and the registration step asks for one attendee per constituent unit — for a Day 1 + Day 2 bundle the guest fills a Day 1 registrant and a Day 2 registrant, which may be two different people. On successful payment, one pass is issued per attendee. Concurrent buyers competing for the last units never drive any ticket's remaining quota below zero.

**Why this priority**: The selection screen only pays off if the selection survives to a paid order. It sits below the list itself because the list is independently demonstrable, but this story is what makes the feature real.

**Independent Test**: Check out a selection of 1 bundle + 1 individual ticket, complete registration for all resulting attendee slots, mark the order paid, and verify the issued passes and the resulting remaining quotas. Then run concurrent checkouts against a single remaining set and verify exactly one succeeds.

**Acceptance Scenarios**:

1. **Given** a selection of 1 bundle (Day 1 + Day 2) and 1 individual "Day 1", **When** the guest reaches registration, **Then** three attendee forms are requested: one for the bundle's Day 1 slot, one for the bundle's Day 2 slot, and one for the standalone Day 1 ticket.
2. **Given** the guest fills different names into the bundle's two slots, **When** the order completes, **Then** the two passes are issued to those two different people.
3. **Given** a checkout of 1 bundle unit, **When** the order is created, **Then** each constituent ticket's remaining quota is reduced by that constituent's per-bundle count, and the order records the bundle as a single priced line at the bundle's own price.
4. **Given** the order total, **When** it is computed, **Then** the server uses its own stored prices for every line and ignores any amounts sent by the client.
5. **Given** exactly one complete set remains and two guests check out that bundle simultaneously, **When** both requests are processed, **Then** exactly one succeeds and the other is rejected as unavailable, with no ticket's remaining quota going negative.
6. **Given** a checkout where the bundle is available but a separately selected individual ticket is not, **When** the request is processed, **Then** the whole order is rejected and no quota is consumed by any line.
7. **Given** a paid order containing a bundle, **When** passes are issued, **Then** one pass is issued per attendee — a Day 1 + Day 2 bundle yields two separately scannable passes, each valid only at its own day's gate.
8. **Given** an unpaid bundle order that expires or is cancelled, **When** the system releases it, **Then** every constituent ticket's remaining quota is restored by exactly what that order consumed, once and only once even if the release signal arrives repeatedly.

---

### User Story 4 - Administrator composes a bundle (Priority: P3)

An administrator creates a bundle on an event, gives it a name, description, all-in price and sales window, and picks which of that event's tickets it contains and how many of each. The bundle is never asked for a quota of its own — the form does not offer one. Once saved and active, it appears in that event's booking list.

**Why this priority**: Necessary to populate the list, but the selection experience is specifiable and demonstrable against seeded data, so this trails the buyer-facing stories.

**Independent Test**: Create a bundle through the admin surface over two of an event's tickets, then load that event's public booking page and confirm the bundle row appears with the configured name and price.

**Acceptance Scenarios**:

1. **Given** an administrator creating a bundle, **When** the form is presented, **Then** it collects name, description, price, sales window and composition, and offers no inventory or quota field.
2. **Given** an administrator picking constituents, **When** the ticket picker is shown, **Then** only tickets belonging to the same event are selectable.
3. **Given** an attempt to save a bundle with no constituent tickets, **When** submitted, **Then** it is rejected — a bundle with nothing inside it has no meaningful availability.
4. **Given** an attempt to add the same ticket twice to one bundle, **When** submitted, **Then** it is rejected in favour of expressing the repeat as a per-constituent count.
5. **Given** a bundle that has already been sold, **When** an administrator attempts to delete it, **Then** the deletion is rejected so historical orders remain readable.
6. **Given** a ticket that is a constituent of any bundle, **When** an administrator attempts to delete that ticket, **Then** the deletion is rejected until the ticket is removed from every bundle.
7. **Given** an administrator edits a bundle's price, **When** the change is saved, **Then** it applies to new selections only and never alters the amount on an existing order.

---

### Edge Cases

- **Scarcest-constituent flip**: a bundle's availability is governed by whichever constituent is scarcest right now, and that constituent can change between two page loads as other buyers move stock. Availability must always be recomputed, never remembered.
- **Whole sets only**: a constituent with 5 remaining that a bundle consumes 2 at a time yields 2 bundle units, not 2.5. The remainder is never offered as a partial bundle.
- **Selection goes stale mid-session**: a guest who left the page open and steps a bundle to a quantity that was available minutes ago must be rejected cleanly at checkout with a message naming the sold-out ticket, not with a generic failure.
- **Cross-consumption within one selection**: selecting 1 bundle (which consumes Day 1) plus 2 standalone Day 1 tickets draws on the same pool. The combined demand — not each line in isolation — must be what is checked against remaining quota.
- **Overlapping bundles**: two bundles sharing a constituent compete for the same stock; concurrent purchases of both must not deadlock or oversell.
- **Empty or single-ticket bundle**: a bundle with zero constituents is invalid; a bundle with exactly one constituent is legal and behaves as a repriced ticket.
- **Constituent quota lowered below outstanding sales**: an administrator may set a remaining quota lower than what open unpaid orders hold. Those orders remain valid; the bundle simply reads as unavailable until stock recovers.
- **Bundle priced above its parts**: permitted — pricing is the administrator's decision and is not validated against the sum of constituents.
- **Free bundle**: a zero-priced bundle is permitted and still consumes constituent quota.
- **Sales-window disagreement**: a bundle open for sale whose constituent has closed is not purchasable; the constituent's window is the binding constraint.
- **Release arriving twice**: an expiry followed by a cancellation for the same order must restore quota exactly once.
- **Event unpublished while selected**: a selection on an event that leaves published state is rejected at checkout.

## Requirements *(mandatory)*

### Functional Requirements

**Selection list (the focus screen)**

- **FR-001**: The booking step MUST present, in a single list for a given event, every individual ticket and every active bundle available for that event.
- **FR-002**: Each bundle row MUST be visually distinguishable from an individual ticket row by a bundle marker.
- **FR-003**: Each bundle row MUST display one all-in price for the bundle, independent of the prices of its constituents.
- **FR-004**: Bundle rows and ticket rows MUST use identical selection mechanics: an Add control that becomes a quantity stepper once selected, and a decrement from 1 that returns the row to its Add state.
- **FR-005**: The selection summary MUST list one line per selected row, showing that row's name, quantity and line total, plus a combined item count and a combined total.
- **FR-006**: A selected bundle MUST appear in the summary as a single line under the bundle's name and price, not decomposed into its constituent tickets.
- **FR-007**: The purchase control MUST be disabled while the selection is empty and enabled as soon as at least one row is selected.
- **FR-008**: Availability figures shown in the list MUST be computed at request time from live remaining quota, and MUST NOT be served from a cache.

**Availability derivation**

- **FR-009**: A bundle MUST NOT store any inventory or quota value of its own; its inventory is derived exclusively from its constituent tickets.
- **FR-010**: A ticket's remaining quota MUST remain the single source of truth for all inventory, for both individual and bundled sales.
- **FR-011**: A bundle's available quantity MUST be the minimum, across all its constituents, of the whole number of complete sets each constituent's remaining quota can still cover.
- **FR-012**: A bundle MUST be presented as unavailable when its available quantity is zero, when its own sales window is not open, when the event is not published, or when any constituent's sales window is not open.
- **FR-013**: A bundle's quantity stepper MUST NOT permit a value exceeding its current available quantity.

**Ticket and bundle descriptions**

- **FR-040**: A ticket MUST support an optional free-text description, authored by an administrator.
- **FR-041**: The booking list MUST show that description as the row's notice line, for both tickets and bundles.
- **FR-042**: When a row has no description, the list MUST fall back to the standard non-refundable wording rather than showing an empty line.
- **FR-043**: Administrators MUST be able to read and edit a ticket's description wherever ticket details are managed, and see it in the ticket listing.

**Composition rules**

- **FR-014**: A bundle MUST belong to exactly one event.
- **FR-015**: Every constituent ticket of a bundle MUST belong to that same event, enforced at the data layer and not by convention alone.
- **FR-016**: A bundle MUST contain at least one constituent ticket.
- **FR-017**: Each constituent MUST carry a per-bundle count of at least 1, and a given ticket MUST appear at most once in a given bundle's composition.

**Checkout and inventory integrity**

- **FR-018**: Checkout MUST accept a mixed selection of individual tickets and bundles in a single order.
- **FR-019**: Checkout MUST aggregate demand per ticket across the entire selection — combining standalone lines with the expansion of every bundle line — and validate the aggregate against remaining quota.
- **FR-020**: The deduction of every affected ticket's quota, together with creation of the order and its lines, MUST occur within a single atomic unit that either fully succeeds or leaves no trace.
- **FR-021**: Quota deduction MUST be safe under concurrency such that no ticket's remaining quota can ever become negative, relying on guarded atomic decrements rather than a read-then-write check.
- **FR-022**: Concurrent orders touching overlapping sets of tickets MUST NOT deadlock; the system MUST apply deductions in a deterministic global order.
- **FR-023**: When any line of an order cannot be satisfied, the entire order MUST be rejected and no quota consumed.
- **FR-024**: A rejection caused by insufficient stock MUST identify which ticket ran out.
- **FR-025**: The order total MUST be computed server-side from stored prices — the bundle's own price for a bundle line, the ticket's price for a ticket line — and client-supplied amounts MUST be ignored.
- **FR-026**: An order line MUST record whether it was bought as a bundle or as an individual ticket, and MUST remain readable at its original price after the bundle or ticket is later edited.
- **FR-027**: No external network call may occur inside the quota-deducting atomic unit.

**Registration and passes**

- **FR-028**: Registration MUST request one attendee per constituent unit of each selected bundle, so a bundle spanning two tickets at quantity 1 requests two attendees.
- **FR-029**: The attendees of one bundle unit MUST be allowed to be different people.
- **FR-030**: Each attendee record MUST be bound to exactly one ticket, including attendees originating from a bundle, and MUST record which bundle it came from when applicable.
- **FR-031**: On payment, exactly one pass MUST be issued per attendee, so a two-ticket bundle yields two independently scannable passes.
- **FR-032**: Each pass issued from a bundle MUST be valid only for the gate of its own constituent ticket.

**Release of held inventory**

- **FR-033**: When an unpaid order expires or is cancelled, every ticket it drew on MUST have its remaining quota restored by exactly the amount that order consumed.
- **FR-034**: Restoration MUST be idempotent — repeated or duplicated release signals for the same order MUST restore quota only once.

**Administration**

- **FR-035**: Administrators MUST be able to create, read, update and delete bundles scoped to an event, and to define and revise each bundle's composition.
- **FR-036**: No administrative surface may present, accept or store a quota for a bundle.
- **FR-037**: Deletion of a bundle referenced by any order MUST be rejected.
- **FR-038**: Deletion of a ticket that is a constituent of any bundle MUST be rejected.
- **FR-039**: Any administrative display of a ticket's quota MUST be labelled as remaining quota, and any sold count shown beside it MUST be derived from order lines rather than stored.

### Key Entities

- **Event**: A dated, venued occasion with a publication state. Owns everything below it. An event's booking page is the list this feature centres on.
- **Ticket**: A purchasable individual access pass type belonging to one event, carrying a price, a sales window, and a **remaining quota** that is the sole inventory record in the system. Bought directly or drawn on by a bundle. *(Stored as `ticket_types` in the existing schema — see Assumptions.)*
- **Package (Bundle)**: A named, priced offer belonging to one event, composed of one or more of that event's tickets. Holds **no inventory of its own**; its availability is derived from its constituents at read time. Carries its own price and sales window, and is shown in the booking list badged as a bundle.
- **Package Composition**: The mapping that states which tickets a bundle contains and how many of each per bundle unit. Constrained so a bundle can only draw on tickets of its own event.
- **Order**: A buyer's purchase, holding buyer contact details, a server-computed total, a payment state and a payment deadline.
- **Order Line**: One priced row of an order, referring either to an individual ticket or to a bundle — never both — recording quantity and the price charged at purchase time.
- **Attendee**: One named registrant bound to exactly one ticket, optionally noting the bundle it originated from. A bundle unit produces one attendee per constituent unit.
- **Pass**: The issued, scannable credential generated one-per-attendee after payment, valid at its own ticket's gate.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A guest can go from opening the booking page to a selection containing a bundle in under 15 seconds and no more than two interactions.
- **SC-002**: 100% of bundle rows displayed as available can be successfully added to a selection at the quantity shown.
- **SC-003**: A ticket's remaining quota never falls below zero — 0 occurrences across a sustained concurrency test of at least 200 simultaneous purchase attempts on a bundle with a single remaining set.
- **SC-004**: Under that same test, exactly one purchase succeeds and every other receives a clear sold-out response naming the exhausted ticket.
- **SC-005**: The booking list, including live availability for every ticket and bundle, is ready for interaction within 2 seconds for an event carrying up to 20 tickets and 10 bundles.
- **SC-006**: The sum of tickets sold plus remaining quota reconciles exactly against the original allocation for every ticket after a mixed workload of individual and bundle purchases, expiries and cancellations — 0 discrepancies.
- **SC-007**: Expired and cancelled bundle orders return 100% of the quota they held, with no double restoration when release signals are duplicated.
- **SC-008**: A bundle becomes unpurchasable within one page load of its scarcest constituent reaching zero, with no administrator intervention.
- **SC-009**: An administrator can publish a working bundle over existing tickets in under 2 minutes, and is never asked for a bundle inventory figure.
- **SC-010**: Every paid bundle purchase yields exactly one scannable pass per constituent unit — 0 missing and 0 duplicate passes.
- **SC-011**: Zero orders are ever charged a total that differs from the server's own computation from stored prices.

## Assumptions

- **Naming**: the description's `tickets` table — the one carrying `quota` — corresponds to the existing `ticket_types` table, and the description's `tickets` concept of "individual access pass" is what this codebase already calls a ticket type. The existing `tickets` table holds *issued passes* generated after payment. Because the instruction is to keep the current schema, no table is renamed; the new junction keeps the requested name `package_tickets` and points at `ticket_types`. This document says "ticket" for the quota-bearing type and "pass" for the issued credential.
- The three supplied designs are three states of the same booking step — empty selection, one row selected, two rows selected — not three separate screens. The "Total 2 Ticket" label in the single-selection frame is read as a mock artefact; the count reflects actual selected units.
- Bundle pricing is set independently by the administrator and is typically but not necessarily a discount; the designs' Rp50.000 bundle over two Rp35.000 tickets is treated as illustrative, not as a pricing rule.
- Per the answered clarification, a bundle unit requests a separate registrant per constituent, and those registrants may be different people; the bundle is a purchase convenience, not a single-person multi-day identity.
- Quota is decremented at order creation, before payment, and restored on expiry or cancellation — matching the behaviour already established for individual tickets. Bundles introduce no separate reservation or hold mechanism.
- A bundle's constituent set is expected to be small (single digits); no pagination or lazy loading of composition is required.
- Bundles do not span events, do not nest inside other bundles, and cannot contain other bundles.
- Existing MVP exclusions continue to apply unchanged: no refunds, coupons, promotions, seat selection, waiting rooms or multi-currency. A bundle is not a promotion mechanism.
- The existing guest-first rule holds: no account is required to select or buy a bundle.
- The existing payment, pass-generation and email delivery pipelines are reused as-is; this feature adds no new payment behaviour.
- Availability is read-consistent but not reserved: a figure shown in the list may be gone by the time the guest checks out, and checkout is the authoritative gate.

## Companion Documents

The technical artefacts requested alongside this specification live beside it, kept out of the specification proper so it stays implementation-agnostic:

- `contracts/schema.md` — ERD, exact SQL DDL, migration against the current schema
- `contracts/models.md` — Go structs and TypeScript interfaces
- `contracts/checkout-transaction.md` — availability query and the race-condition-safe deduction flow
- `contracts/api.md` — endpoints for events, tickets and packages

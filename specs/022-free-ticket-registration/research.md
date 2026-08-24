# Phase 0 Research: Free Ticket Registration

**Feature**: 022-free-ticket-registration | **Date**: 2026-08-20

Every decision below is grounded in the code as it exists, cited by `file:line`. Decisions already
settled with the user during `/speckit-specify` and `/speckit-clarify` are recorded in the spec's
Clarifications and are not re-opened here; what follows is how to *implement* them.

---

## D1. The registration write path belongs to the `order` domain

**Decision**: A new `internal/order/registration_service.go`, modelled on `bookOnce`.

**Rationale**: It writes `orders`, `order_items` and `attendees` and deducts quota — precisely what
`order` already does ([order/service.go:154-278](../../backend/internal/order/service.go#L154-L278)).
`cmd/api/architecture_test.go` enforces that no domain imports another, and `order` already declares
the two cross-domain interfaces this needs: `EventProvider.CheckAndDeductQuota` and
`EventProvider.CurrentTerms` ([order/event_provider.go:90](../../backend/internal/order/event_provider.go#L90)).

**Alternatives rejected**:
- A new `registration` domain — Principle I fixes the domain set at six; adding one requires a
  constitution amendment for no benefit, since the rows are order rows.
- Putting it in `event` — that domain owns the catalogue, not order writes, and it would need to
  import `order`'s repository.
- Putting it in `ticket` — issuance is downstream of the order, not the other way round.

## D2. Issuance and delivery reach out through new narrow interfaces

**Decision**: `order` declares `TicketIssuer` and `TicketDeliverer` (its own, not `payment`'s), and
`cmd/api/adapters.go` satisfies them.

**Rationale**: This is an exact copy of an existing, working pattern —
`payment.TicketIssuer` / `payment.TicketDeliverer`
([payment/service.go:185-193](../../backend/internal/payment/service.go#L185-L193)) — wired in the
composition root, the one package allowed to see every domain. `order` cannot import `payment`'s
interfaces; declaring identical ones locally is what Principle II requires, not duplication to avoid.

**Note**: `notification.SendTicketEmail` returns `(string, error)` while the interface wants
`error`; the existing payment adapter already discards the string, so the new adapter follows suit.

## D3. Post-commit work mirrors `fulfillAsync` exactly

**Decision**: Issuance and delivery run in a goroutine using
`context.WithTimeout(context.WithoutCancel(requestCtx), fulfillmentTimeout)`, tracked by a
`WaitGroup` for graceful drain.

**Rationale**: [payment/service.go:831-861](../../backend/internal/payment/service.go#L831-L861).
`WithoutCancel` keeps the trace context while dropping the cancellation that fires when the response
is written — deriving from `context.Background()` would orphan the work from the request trace,
"which is exactly where you look when a buyer says the email never arrived". The registration has the
same property and deserves the same treatment.

**Ordering matters**: if issuance fails, stop — do not attempt delivery. Same as the paid path.

## D4. `orders.is_registration` — REVERSED: a derivation, not a column

**Decision (revised 2026-08-20, by explicit direction)**: there is **no** marker column. An order is
registration-originated exactly when it carries an `order_items` line for a ticket type with
`is_visible = FALSE`. Containment guarantees such a type can never be bought or bundled, so one such
line can only have come from the registration path. Constitution Principle IV was amended to v6.0.0
to withdraw the "MUST be marked at creation" requirement it previously imposed.

**Superseded decision, kept in full because its reasoning is what the reversal traded away**: add a
boolean marker to `orders`. Its rationale was that FR-033 requires the distinction in stored data,
and that deriving it would make `notification` depend on an `event`-domain flag to decide what to
attach — a cross-domain read on every delivery, for a fact the order itself could simply carry.
Alternatives rejected at that time: inferring from `total_amount = 0` (a genuinely free *purchase*
would collide — and FR-003 explicitly permits a purchasable type priced zero); inferring from the
absence of a `payments` row (a transient read failure would then change which documents a buyer
receives); a new `order_statuses` row such as `REGISTERED` (rejected by the user in favour of
`PAID`). Those three remain rejected — the reversal chose derivation, not inference.

**What the reversal costs, stated rather than mitigated.** A stored marker records what happened.
A derivation reports what is currently true. The two diverge the moment an admin makes an invitation
ticket type purchasable again: every historical order that used it silently reclassifies as a
purchase, and because `OrderForDelivery` reads this, a RESEND of an already-delivered registration
would render a receipt for an order that never had a payment. Nothing in the schema can prevent
this — the concern was raised before the decision and the derivation was chosen anyway. It is pinned
by `TestDerivedRegistrationFlagFlipsWhenTheTicketTypeBecomesPurchasable`, which asserts the
reclassification happens, so that anyone who later finds it surprising can see it was chosen rather
than overlooked.

**Shape discussion, now moot.** The superseded column was a boolean rather than a
`VARCHAR … CHECK` enum, because migration 0013 had converted order statuses *from* a CHECK
constraint *to* a master table — so a new VARCHAR enum was the pattern being retired, not the one in
force — and because Principle VI forbids building for hypotheticals when there are exactly two
origins. Retained only so a future third origin does not re-litigate it from scratch.

## D5. The e-ticket-without-receipt delivery shape

**Decision**: `notification.SendTicketEmail` branches on the order's registration marker: render the
tickets PDF only, one attachment, a subject without "E-receipt", and a body with no receipt block and
no monetary figure.

**Rationale**: Today the function unconditionally renders both documents and attaches exactly two
([notification/service.go:199-238](../../backend/internal/notification/service.go#L199-L238)), and
its `order.Status != "PAID"` guard at line 173 is *satisfied* by a registration — the deviation D6
buys that for free.

**Hazard**: the two-attachment invariant is load-bearing for paid orders and is enforced by tests
and asserted in `e2e/`. The branch must not weaken the paid path: a purchase must still fail closed —
if either document fails to render, nothing is sent and `email_sent` stays false.

**Alternatives rejected**: a separate `SendRegistrationEmail` entry point duplicating the loading,
recipient resolution, inline-image assembly and `MarkEmailSent` bookkeeping — a second copy of the
delivery path that would drift.

## D6. `PAID` is reused; the constitution is amended rather than the code contorted

Settled by the user. Recorded in [plan.md](./plan.md) Complexity Tracking. The implementation
consequence worth naming: **`notification.SendTicketEmail`'s `PAID` guard, `ticket` lookup, admin
listing and validation all keep working untouched**, which is what makes this the cheap option — and
also what makes the amendment mandatory rather than optional.

## D7. Duplicate-email race safety — the most likely place to be quietly wrong

**Decision (SUPERSEDED 2026-08-20)**: there is no lock and no duplicate check.

The original decision took `pg_advisory_xact_lock` keyed on a hash of `(event_id, lower(email))` at
the top of the registration transaction, before the quota deduction, then performed a duplicate check
under it — because FR-023's rule could not be expressed as a unique constraint (`attendees` has no
`event_id`, and "already has a place" additionally depended on the order being `PAID`), and a bare
read-then-write check is a race that lets two concurrent submissions of one address both commit.

FR-023 then removed the rule outright: an address may register as many times as remaining quota
allows. The lock went with it rather than being kept for safety — with no invariant left to protect,
an address-scoped lock only serialises the hot path, and FR-023a explicitly requires that two
submissions of one address do not contend. The quota row lock, per ticket type and shared with the
purchase path, is the only serialisation left.

The concurrency test was inverted rather than deleted, and is now the thing that would catch a lock
left behind: it asserts every concurrent attempt SUCCEEDS, which a leftover lock would still pass on
row count alone but not on the "no attempt refused on account of the others" assertion.

**Alternatives rejected**:
- *Unique index* — not expressible, as above.
- *`SERIALIZABLE` isolation for this transaction* — would work, but pushes retry handling onto the
  caller for a conflict class the rest of the system never produces, and interacts badly with the
  row-locked quota update.
- *Denormalising `event_id` onto `attendees`* — a schema change to a table on the hot purchase path,
  to serve one validation rule.
- *Accepting the race* — contradicts SC-002 and produces two tickets for one person at a door.

## D8. Terms version: compare `updated_at`, not the id — and fix booking's dead check

> **⚠️ This supersedes the original D8, which compared `event_terms_id`. That design was
> unimplementable, and discovering why also uncovered a pre-existing bug.**

**The finding**, verified directly rather than taken on report:

```sql
-- name: UpsertEventTerms :one
-- One live document per event (event_id UNIQUE): an edit overwrites in place.
INSERT INTO event_terms (event_id, content) VALUES ($1, $2)
ON CONFLICT (event_id) DO UPDATE SET content = EXCLUDED.content, updated_at = now()
RETURNING id, event_id, content, created_at, updated_at;
```

`event_terms.event_id` is `UNIQUE` (SCHEMA.md:280), so an admin edit **overwrites in place and
preserves the row id**. Therefore the existing check in `RecordAgreement` —
`if current.ID != req.EventTermsID` ([order/service.go:320](../../backend/internal/order/service.go#L320))
— **can never fire when terms are edited.** It fires only if the document is deleted and recreated,
which the API offers no way to do.

**This is a pre-existing defect in the booking flow, not merely a gap in this feature.**
`EventTermsDTO`'s doc comment states the id is echoed back "so the server can detect the document
changing mid-flow (409002)" ([event/dto.go:81-84](../../backend/internal/event/dto.go#L81-L84)). It
cannot. `CodeTermsChanged` is reachable in `RecordAgreement` only through the `ErrNoTerms` branch.

**Decision**: the version token is `event_terms.updated_at`.

- It already travels on the wire — `EventTermsDTO.UpdatedAt` is populated by
  `GetEventTermsByEventSlug`, so **no schema change and no new endpoint**.
- `order.EventTermsInfo` ([event_provider.go:32-36](../../backend/internal/order/event_provider.go#L32-L36))
  gains an `UpdatedAt` field, plus one line in the adapter.
- `orders.event_terms_id` is still stamped: the id records **which document**, `updated_at` records
  **which version**. Both are wanted, and FR-049 asks for the former by name.

**Scope consequence, flagged rather than absorbed silently**: the fix belongs to both surfaces.
Leaving booking comparing an id that cannot change, while registration compares `updated_at`, would
give one document two different meanings of "changed" — contradicting FR-043's whole premise that
consent means one thing on both surfaces. Because booking is a Principle VIII covered flow, its fix
requires a scenario **seen failing first**. Tasks T053a–T053c carry this.

**Residual race, accepted and named**: an admin could republish between the pre-`BEGIN` check and the
commit. The window is milliseconds, the consequence is one registration bound to a version superseded
moments earlier, and closing it would mean locking the terms document inside the order transaction.
Booking has the identical window.

## D8a. The registration order is written by one new query; the attendee reuses two existing ones

**Decision**: add `CreateRegistrationOrder` (new SQL). Reuse `CreateAttendeeSlot` +
`UpdateAttendeeDetails` unchanged.

**Rationale**, all verified in `backend/internal/order/queries/order.sql`:

- `CreateBookedOrder` hardcodes `'PENDING'`, leaves `buyer_*`, `terms_agreed_at` and `event_terms_id`
  NULL, and takes a **non-null** `payment_expires_at`. Not reusable.
- The two obvious patch-afterwards queries are both **`PENDING`-guarded** and would silently affect
  zero rows on an order created at `PAID`: `UpdateOrderBuyer` and `RecordTermsAgreement`. Their repo
  wrappers map "0 rows" to `false`, which the service reports as `410 ORDER_EXPIRED` — so a
  registration that actually succeeded would be reported as gone. Everything must be **inline on the
  INSERT**.
- `UpdateAttendeeDetails` is `WHERE id = $1 AND order_id = $2` — **no status guard** — so it works on
  a `PAID` order. The attendee needs no new SQL, and gender name→id resolution stays byte-identical
  to checkout's.
- `payment_expires_at` must stay NULL, or every order read path renders a live countdown and the
  registration looks payable.

## D9. The end-of-document signal uses a sentinel, not a scroll listener

**Decision**: Primary mechanism is an `IntersectionObserver` on a sentinel after the last content
node; a tolerance-based scroll computation is the secondary trigger; `ResizeObserver` re-evaluates
while the tick is still pending; non-scrollable content satisfies immediately.

**Rationale**: FR-046 requires the automatic tick to fire for keyboard and assistive-technology
users. A `scroll` handler makes it a pointer-only affordance — a screen-reader user who has read the
entire document may never generate a scroll event. Full mechanics in
[contracts/terms-gate.md §2.1](./contracts/terms-gate.md).

**Tolerance must be non-zero**: sub-pixel layout, fractional DPR and browser zoom routinely leave
`scrollTop + clientHeight` a fraction below `scrollHeight` (FR-047).

**Revised 2026-08-21 — the decision stands, its stake does not.** When this was written, failing to
fire meant a guest could never consent at all, which made the sentinel an accessibility
*requirement*. Since the gate was removed, a guest the observer fails can simply tick the box, so
the cost of getting this wrong dropped from "cannot proceed" to "must click one more thing". The
mechanism is kept unchanged anyway, on the ground that "the accessible path is the one allowed to
degrade silently" is not a defensible place to land — but the honest severity is now lower, and a
plan that still calls this a blocker would be overstating it.

**What the revision does add here is FR-045a: the signal fires at most once per opening.** The
shipped `TermsViewer` already latches it in `firedRef` and re-arms on close, so this is satisfied by
the existing code. It matters more than it did: the latch is now what stops the auto-tick from
overriding a guest who deliberately unticked the box, rather than merely avoiding a redundant
state write.

## D10. The scroll path cannot be tested at the unit tier — but the manual path can

**Decision**: Vitest covers everything reachable without layout, driving `onReachedEnd` directly as
an input; Playwright covers the scrolling itself against a **long** document.

**Revised 2026-08-21**: this decision got *cheaper*. The manual route the clarification added —
tick the checkbox, Agree turns up, untick it, Agree goes away — involves no scroll metrics and no
observers, so it is fully unit-testable, as is the FR-045a latch (fire `onReachedEnd`, untick, fire
it again, assert the box stays unticked). The untestable surface is now only the part where a real
browser must lay out a real document. This is the single place where removing the gate made
verification easier rather than harder, and it is worth spending: the latch is the property most
likely to be broken by a later refactor and it can be pinned in Vitest, cheaply, forever.

**Rationale**: the test DOM is **happy-dom**, not jsdom
([vitest.setup.ts](../../frontend/vitest.setup.ts) registers `@happy-dom/global-registrator`), and
for this purpose it is worse than jsdom would be. `scrollHeight`/`scrollWidth` are getter-only,
`PropertySymbol`-backed and initialised to `0`, so they cannot be stubbed by assignment. And
`IntersectionObserver`/`ResizeObserver` **are registered on `window`** — a feature-detect finds
them — but every method is an empty `// TODO: Not implemented` stub, so the callback never fires and
never throws. A Vitest test therefore evaluates `0 <= 0`, concludes the content is non-scrollable,
and passes while asserting nothing, with no way to detect the observer is inert. Writing one and
believing it is the specific failure this decision exists to prevent.

## D11. The e2e helpers need no change — and the fixtures are still a trap

**Decision (revised 2026-08-21)**: `agreeToTermsAndBook()` ([journey.ts:149](../../e2e/support/journey.ts#L149))
and `agreeExpectingRefusal()` (line 182) both scroll to the end and then press Agree. Scrolling still
ticks the box, so **both keep working unchanged** and the ~14 call sites are untouched. Only the
comment inside `agreeToTermsAndBook` — which explains that the checkbox is no longer a control — is
now false and must be corrected. Add two scenarios: the auto-tick against a long document, and the
manual tick with no scrolling at all.

*(The original decision was that both helpers must stop clicking the checkbox and start scrolling.
They already do. The 2026-08-20 work is what makes the 2026-08-21 reversal nearly free on this side.)*

**Rationale for the second new scenario**: without it the suite cannot distinguish "Agree follows the
checkbox" from "Agree follows the scroll position, and the helper happens to scroll" — which is the
entire content of this change.

**The fixture trap is unchanged and still the main risk.** Existing fixtures author
`putTerms(token, event.id, "<p>terms</p>")` — far shorter than the reading area. Under FR-014d that
content is non-scrollable, so the box is ticked the instant the modal opens and **the entire existing
suite stays green without exercising either route**. Principle VIII names this exact failure: "a
change that leaves the suite green only because the suite never looked has not been verified."

## D12. Migration and `sqlc`

**Decision**: `000016_ticket_type_registration_only.{up,down}.sql`, adding both booleans with
`NOT NULL DEFAULT FALSE` and `COMMENT ON COLUMN`. `SCHEMA.md` in the same commit.

**Rationale**: 000014 is the template — heavy intent comments in the DDL, `COMMENT ON COLUMN`, and an
explicit warning about sqlc type spellings ([000014 up](../../backend/migrations/000014_ticket_type_event_window.up.sql)).
Booleans need no equivalent care, and `DEFAULT FALSE` makes the migration total with no backfill
statement.

**Key finding**: every `ticket_types` query uses an **explicit column list**, never `SELECT *`
([event/queries/event.sql:14-28, 134-158](../../backend/internal/event/queries/event.sql)). So adding
a column breaks nothing and regenerates cleanly — but each query that needs the new field must be
edited deliberately. The ones to change: `ListTicketTypesByEventID` (guest — add the exclusion),
`GetTicketTypeByID` (registration endpoint reads the flag), `ListTicketTypesAdmin` and
`GetTicketTypeAdmin` (admin must see it), `CreateTicketType` and `UpdateTicketType` (admin writes it).

## D13. Throttling follows the existing policy shape

**Decision**: `Register ThrottlePolicy` with `RATE_LIMIT_REGISTER_{ENABLED,RATE,BURST}`, defaults
`true / 0.2 / 3`, added to `ratePolicies()`.

**Rationale**: [pkg/config/throttle.go:76-150](../../backend/pkg/config/throttle.go#L76-L150). The
checkout defaults are the right analogue — this surface deducts quota *and* sends real mail. Joining
`ratePolicies()` is what makes it inherit startup validation and the startup report; a policy that
skips that list is silently unvalidated and unreported.

## D14. Frontend route placement — plural `events`, reversing an earlier singular choice

**Decision (revised 2026-08-20)**: `app/(public)/events/[slug]/register/[ticketId]/` and its
`success/` child, under the same plural segment every other guest route uses.

**Superseded decision, kept because its rationale was sound and is what makes the reversal a
trade rather than a correction**: the route was to sit under a **singular** `app/(public)/event/`
segment. The supporting fact — that the **API already uses singular** (`GET /event`,
`GET /event/:id`, `GET /ticket/:event_id`,
[event/handler.go:28-35](../../backend/internal/event/handler.go#L28-L35)) — remains true. The page
URL and the API would have agreed. What decided it the other way was not that argument failing but
its cost: two sibling top-level segments differing by one character, which nothing structurally
prevents a future route from being added under the wrong one.

**What the reversal bought and what it cost.** Bought: one convention, and no one-character trap.
Cost: the route now inherits `events/[slug]/layout.tsx`, the purchase journey's chrome — a sales
countdown and a `Booking → Registration → Payment → Done` rail. Inherited unchanged that puts a step
named **Payment** on a surface FR-015 forbids from showing one, silently: nothing errors, the page
renders, and only a reader notices a free invitation form advertising a checkout.

The App Router gives a nested route no way to opt out of a parent layout, so the exclusion has to
live inside `EventFrame`, keyed on the URL exactly as the stage already is (FR-010a). Under the
singular segment this problem did not exist, which is the honest way to state what was given up.

## D15. Enforce FR-008 at one seam, which also covers availability

**Decision**: Put the refusal in `EventProvider.TicketTypeForCheckout` /
`expandTicket` ([order/demand.go:52](../../backend/internal/order/demand.go#L52)), not in `Book`.

**Rationale**: booking, checkout, availability and package expansion all resolve a ticket type
through that single method. One check covers four callers. FR-008 names only booking and checkout,
so the naive reading leaves `POST /ticket/availability` answering "available" for a
registration-only type — and availability deliberately runs *in front of* the T&C dialog so a guest
never opens a document for a purchase that cannot happen. A refusal arriving one step later is the
regression spec 013 exists to prevent. Full reasoning in [contracts/api.md §3.1](./contracts/api.md).

## D16. `UpdateTicketType` is a full replace — thread the flag or it leaks

**Decision**: Thread `is_visible` (as an optional boolean defaulting to visible) through `TicketTypeRequest`, the repository call and the
admin form, and add a round-trip test proving an edit to an unrelated field preserves it.

**Rationale**: [event.sql:150-157](../../backend/internal/event/queries/event.sql#L150-L157) replaces
every column unconditionally. An admin PUT omitting the boolean **clears** it, republishing the type
onto the guest purchase list — usually at price `0`, since that is the convention. The leak US2
exists to prevent, arriving through the edit form rather than a missing filter. This is the single
easiest way to ship this feature broken while every list filter is correct.

## D17. `ListTicketTypeIDsByEventID` must **not** gain the filter

**Decision**: Filter `ListTicketTypesByEventID` (guest list). Leave `ListTicketTypeIDsByEventID`
unfiltered.

**Rationale**: it feeds the `DeleteEvent` guard and `DeleteTicketTypesByEventID`. Filtering it would
make the guard skip registration-only types, so deletion would proceed and hit the
`ON DELETE RESTRICT` foreign key — surfacing a raw constraint violation instead of the `400` that
Principle VI requires. "Add the filter everywhere ticket types are listed" is the wrong instinct;
each query is a separate decision.

## D18. Guest self-service resend — obscurity, not enforcement

**Finding**: `POST /api/v1/ticket/resend-email` resolves **any** order number, including a
registration's ([notification/handler.go:69-121](../../backend/internal/notification/handler.go#L69-L121)).

The spec's Out of Scope says guest self-service resend is "deliberately not reachable" for a
registration. That is true only because the confirmation page shows no order number (FR-039) — the
endpoint itself would serve one. **Decision**: leave the endpoint as is and correct the
characterisation. It requires knowing an order number, which functions as a bearer secret and is
cooldown-limited per order number, so a registrant who learns theirs through support can use it.
Blocking it would remove a working recovery path for no security gain. What must not happen is the
plan claiming enforcement where there is only obscurity.

## D19. A warm cache outlives the deploy

**Finding**: cache keys carry family, scope and fingerprint but **no schema or deploy version**; a
write-bumped generation counter and a TTL are the only invalidation
([pkg/cache/cache.go:151-180](../../backend/pkg/cache/cache.go#L151-L180)).

So after deploying the SQL filter, a `ticket_types_public` entry built moments *before* the deploy
keeps serving the registration-only type until an unrelated write to that event or the TTL expires.
SC-004 ("0 registration-only types appear on any guest purchase surface") is therefore not
verifiable immediately post-deploy without a cache flush. **Decision**: the release runbook flushes
Redis, or the admin cache-refresh endpoint is called, as a deploy step. This costs nothing but
latency — Principle VII guarantees exactly that — and is invisible in any test environment that
starts cold, which is why it needs recording here.

## D20. Index `attendees.email` for the duplicate check — WITHDRAWN

The original decision added `CREATE INDEX idx_attendees_email_lower ON attendees (lower(email))`,
because FR-023's check filtered on a lowered address and joined through `tickets` and `ticket_types`,
and that read sat on an unauthenticated throttled endpoint inside a transaction holding a quota row
lock — the worst place in the system for a sequential scan.

FR-023 removed the check, so nothing filters on the address any more. The index is **not** shipped:
an index no query uses is write amplification on `attendees` insert, which is exactly the path this
feature adds load to.
## D21. `sqlc generate` produces a zero diff for this migration

**Finding**: `sqlc.yaml` sets `omit_unused_structs: true`, and `eventsql/models.go` has no
`TicketType` struct at all — row types are generated per query. So adding the column and running
`sqlc generate` changes **nothing** until a query names the column explicitly.

**Consequence for `/speckit-tasks`**: a task worded like "run `sqlc generate` and verify the
generated package exposes the new column" is **unsatisfiable** and would read as a broken toolchain.
The correct task is "add the column to the six queries that need it, then regenerate".

---

## Open questions for `/speckit-tasks`

None blocking. Two sequencing constraints:

1. **The constitution amendments come first.** Tasks must not schedule implementation ahead of them.
2. **Both e2e terms scenarios must be seen failing** against the current dialog before the change
   lands, per Principle VIII's rule for behaviour changes in covered flows. Note *which* one proves
   what: the **manual-tick** scenario is the one that genuinely fails today, because the shipped
   checkbox carries `onCheckedChange={() => {}}` and cannot be ticked. The **auto-tick** scenario
   passes both before and after — it is a regression guard, not evidence — so a task list that only
   adds that one has not seen red and has proved nothing (revised 2026-08-21).

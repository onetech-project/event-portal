<!--
Sync Impact Report
Version change: 5.0.0 -> 6.0.0 (MAJOR - an existing rule is REDEFINED, and a mandatory
requirement is REMOVED. Principle IV required a registration-originated order to be
"marked at creation", making the marker part of the permission that admitted the second
origin of `PAID`. That requirement is withdrawn: the distinction is now derived from the
ticket type the order's line references. Removing a MUST from a NON-NEGOTIABLE principle
is backward-incompatible for any consumer that relied on it, which is a MAJOR bump by the
same reasoning the 3.4.0 -> 4.0.0 report recorded.)

Trigger: explicit direction, 2026-08-20, during spec 022 implementation. The stored
marker was questioned and the derivation chosen in its place.

Modified sections:
  - Principle IV (Transactional Integrity & Idempotency) - "MUST be marked at creation as
    registration-originated" is replaced by "the distinction is derived, not stored: an
    order is registration-originated exactly when it carries a line for a ticket type
    that is not guest-visible". The obligation on revenue surfaces is unchanged and
    restated: they MUST apply the derivation, and MUST NOT read the status as evidence
    money moved.

COST ACCEPTED, RECORDED HERE BECAUSE NOTHING ELSE CAN ENFORCE IT: a derived value is not
stable over time. An admin who makes an invitation ticket type purchasable again
retroactively reclassifies every historical order that used it, and a resend of an old
registration would then render a receipt for an order that never had a payment. A stored
marker could not do that. This was raised before the decision and chosen anyway; it is
documented rather than mitigated, because the schema has no way to prevent it.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - SCHEMA.md - UPDATED WITH THIS CHANGE: `orders.is_registration` is removed and the
    derivation documented in its place.
  - ARCHITECTURE.md, PRD.md - UPDATED WITH THIS CHANGE where they named the marker.

Added principles: none. Removed sections: none.

Templates requiring follow-up: none.

---

Version change: 4.2.0 -> 5.0.0 (MAJOR - two existing rules are REDEFINED, not merely
extended. Principle IV said "no path other than a gateway webhook may move an order to
`PAID`", an absolute that did not fail to contemplate free registration so much as
forbid it; that prohibition becomes a permission, scoped to orders without a payment
session. The Critical Data Flow Rules said delivery carries "exactly two document
attachments" with neither sendable without the other; that becomes two for a paid order
and exactly one for a registration. Turning a prohibition into a permission is a
backward-incompatible redefinition by the same reasoning the 3.4.0 -> 4.0.0 report
recorded for its reversal, and unlike the 3.2.0 and 3.3.0 MINOR bumps, which genuinely
governed cases the previous text had never addressed.)

Trigger: spec 022 (Free Ticket Registration). An organiser needs to issue complimentary
invitation tickets: a guest fills one form, accepts the event's Terms & Conditions, and
is emailed an e-ticket. No money is owed at any point, so there is no payment to confirm
and no receipt that could honestly be issued.

Modified sections:
  - Principle IV (Transactional Integrity & Idempotency) - the webhook-only route to
    `PAID` is narrowed to orders that HAVE a payment session, where it stays absolute. A
    registration-originated order is admitted: zero total, no payment session, no gateway
    call, no `payments` row, no `order_fees`, quota deducted through the same atomic
    row-locked UPDATE inside one transaction, and unreachable by any webhook or sweeper.
    It MUST be marked registration-originated at creation. Rationale extended to state
    what `PAID` now means across both origins - "this order is final and its tickets are
    issuable" - and to record the honest cost: `PAID` alone no longer implies money
    moved, which is why the marker is mandatory and why any revenue surface MUST read it.
  - Critical Data Flow Rules, ticket-generation bullet - the two-attachment rule is
    scoped to PAID-by-payment orders. A registration carries exactly ONE attachment, the
    E-Ticket alone, and its message states no monetary amount anywhere. The all-or-nothing
    render rule applies unchanged to the one document it does send.

Added principles: none. Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - ARCHITECTURE.md - UPDATED WITH THIS CHANGE: SS3.4 gains the Free Registration write
    path and a note that the terms-agreement version token is `event_terms.updated_at`,
    never the row id.
  - PRD.md - UPDATED WITH THIS CHANGE: SS1.4 gains a Free Registration block and the
    one-attachment delivery case.
  - SCHEMA.md - the amendment ITSELF has no schema impact; spec 022's migration 000016
    (`ticket_types.is_visible`) updates SCHEMA.md in its own
    commit, as Governance and
    AGENTS.md both require.

Templates requiring follow-up: none. The plan template's Constitution Check reads this
file at runtime.

Follow-up TODOs:
  - The 4.2.0 report's Follow-up TODO about echo's IPExtractor is STALE and is retired
    here. Verified rather than assumed: backend/cmd/api/main.go:333-345 sets
    `echo.ExtractIPDirect()` by default and honours X-Forwarded-For only from
    TRUSTED_PROXY_CIDRS. Spec 018 closed it; the repository is compliant with Principle
    IX's client-identity rule.
  - Spec 022 uncovered a PRE-EXISTING defect, recorded here rather than left silent:
    `UpsertEventTerms` is `ON CONFLICT (event_id) DO UPDATE` on a UNIQUE `event_id`, so
    an admin edit preserves the row id. The booking flow's `TERMS_CHANGED` refusal
    compares ids and therefore cannot fire for the case its own doc comment describes.
    Spec 022 fixes it on both surfaces by comparing `updated_at`, with the regression seen
    failing first (tasks T053a-T053d).
-->

<!--
Sync Impact Report
Version change: 4.1.0 -> 4.2.0 (MINOR - a new principle is added. No existing principle
is removed or redefined, matching the 3.1.0 -> 3.2.0 precedent for adding a principle.
Note honestly that current code does NOT yet satisfy the new principle's client-identity
rule - see Follow-up TODOs. The amendment states the standard; spec 018 brings the code
to it.)

Trigger: spec 018 (Configurable Rate Limits). Six throttles were compiled into the API as
constants, so retuning any of them required a rebuild and a redeploy. A 9-agent review of
that spec also established that no governance document bound throttling at all: the spec
had claimed this project "already demands" of throttling what Principle VII demands of the
cache, and that claim was not granted by any binding text. This amendment makes it true
rather than deleting the claim.

Added principles:
  - IX. Request Throttling as Configurable, Killable Protection (NON-NEGOTIABLE) -
    deliberately mirrors Principle VII's shape, because a throttle and a cache are the
    same kind of thing constitutionally: an operator-controlled subsystem that must be
    removable without changing what the system computes. Eight MUST rules covering
    configurability, the two-granularity kill switch, substitution-not-skipping, the
    unforgeable-client-identity floor, startup validation, disposable per-instance state,
    never being authoritative for correctness, and documenting the blast radius.

Modified sections:
  - Principle VIII, acceptance-gate bullets - a "Both throttling modes" bullet added
    alongside "Both cache modes", naming E2E_RATE_LIMIT_ENABLED. It also carries an
    isolation requirement the cache bullet needs no analogue of: throttle state is keyed
    per client and process-local, so one scenario draining an allowance would otherwise
    refuse an unrelated one.

Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - ARCHITECTURE.md - UPDATED WITH THIS CHANGE: a Controls entry for throttling mirroring
    the cache's, and SS3.7's flat assertion that the ticket lookup "is rate limited per IP"
    corrected - it is now configurable and disableable, and the per-IP keying itself is
    only true once Principle IX's client-identity rule is satisfied.
  - PRD.md - UPDATED WITH THIS CHANGE: SS1.6 records that throttle state stays in process
    memory and does not extend Redis's admitted scope.
  - SCHEMA.md - no impact, verified rather than assumed: spec 018 adds no column, table,
    or migration. Throttle state is process memory and is never persisted, which is itself
    one of Principle IX's rules.

Templates requiring follow-up: none. The plan template's Constitution Check reads the
constitution at runtime and already carries the Principle VIII rows this exercises.

Follow-up TODOs:
  - Principle IX's client-identity rule is NOT satisfied by current code, and this is
    stated rather than glossed: no first-party file sets echo's IPExtractor, so RealIP()
    returns a caller-supplied X-Forwarded-For and five throttled surfaces are bypassable
    with one header. ARCHITECTURE.md and api/openapi.yml have both described that lookup
    limit as enforced. Spec 018 (FR-002a, FR-002b, SC-009) closes it, and its task T003
    requires the regression be seen failing first. Until that lands, the repository is
    knowingly non-compliant with one bullet of a NON-NEGOTIABLE principle - which is the
    correct place to record it, rather than softening the principle to fit the code.
-->

<!--
Sync Impact Report
Version change: 4.0.0 -> 4.1.0 (MINOR - materially expanded guidance. No principle is
removed or redefined and nothing compliant under 4.0.0 becomes non-compliant: the email
is still exactly one, to exactly the same address, still carrying every ticket in the
order in a single PDF. What changes is that the receipt - which 4.0.0 already required
to travel "together with" that PDF - now also travels as its own detachable document.
A buyer who received a compliant email under 4.0.0 receives a superset under 4.1.0.)

Trigger: spec 016 (Receipt + E-Ticket Email Attachments). The itemized proof of payment
existed only as HTML inside the email body, so a buyer needing it for reimbursement or
an expense claim had nothing to file or forward on its own.

Modified sections:
  - Critical Data Flow Rules, ticket-generation bullet - delivery carries exactly TWO
    attachments: a Payment Receipt document and the single E-Ticket document holding
    every ticket. The receipt stays in the body as well. Two constraints are stated
    rather than left implicit: the E-Ticket document carries no monetary figure (which
    is also what dissolves the per-ticket-price problem for bundle-issued tickets), and
    neither document may be sent without the other - a render failure sends nothing, so
    email_sent stays FALSE and resend stays armed.

Added principles: none. Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - PRD.md - UPDATED WITH THIS CHANGE: SS1.4's delivery bullet names two attachments.
  - ARCHITECTURE.md - UPDATED WITH THIS CHANGE: the post-payment sequence note names
    both documents.
  - SCHEMA.md - no impact, and this was verified rather than assumed: spec 016 adds no
    column, table, or migration. Every value the three surfaces render already had a
    column; the only newly READ ones (orders.updated_at, payments.payment_type,
    ticket_types.description, packages.description) were already stored and already
    selected or trivially selectable.

Templates requiring follow-up: none. The plan template's Constitution Check already
carries the Principle VIII row this change exercises.

Follow-up TODOs: none. Principle VIII is satisfied in the same change: the guest journey
spec now reads the delivered message off Mailpit and asserts the two-attachment outcome
and the per-ticket page count, which orders.email_sent alone could never observe.
-->

<!--
Sync Impact Report
Version change: 3.4.0 → 4.0.0 (MAJOR — the fee-presentation rule is redefined in the
opposite direction: behavior compliant under 3.4.0 — the form-filling step showing the
fee-inclusive grand total — is non-compliant under 4.0.0. Same reasoning the 2.0.0 and
3.0.0 bumps recorded: a reversal is a backward-incompatible redefinition, not expanded
guidance. 2.1.0 took MINOR for this same bullet precisely because it added guidance
without redefining an existing rule; that justification no longer applies.)

Trigger: clarification of 2026-08-12 (recorded in specs/011-order-buyer-info spec.md,
Clarifications): the order page's summary "includes the fee, it's supposed to be raw,
only on the checkout page the fee will be added on the total_amount". 2.1.0 suppressed
the itemized rows on the form-filling step but left the fee-inclusive figure standing,
so the guest committing to Continue to Payment was shown a total whose breakdown that
same rule had just removed. The figure now matches what the step can account for — the
ticket subtotal — and the grand total first appears on the awaiting-payment step, where
its breakdown appears with it.

Modified sections:
  - Critical Data Flow Rules, fee-presentation bullet — the form-filling step shows the
    pre-fee ticket subtotal, not the grand total, and no fee-inclusive figure appears
    before the awaiting-payment step. The "includes all taxes and fees" note is replaced
    by one stating that taxes and fees are added at the next step, because the old
    wording above a fee-free figure understates what the guest will be charged. The
    heading stays "Total Payment" on both steps; the note carries the distinction. The
    null-subtotal fallback (orders predating fees) is stated rather than left implicit.
    The display-only clause is strengthened, not weakened: the frozen `order_fees` math,
    the stored total, the amount charged, and the gateway's gross amount are all
    explicitly unchanged.

Added principles: none. Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - No impact — verified rather than assumed: `grep -i fee` returns nothing in
    ARCHITECTURE.md or PRD.md, and SCHEMA.md describes only the `fees` / `order_fees`
    tables, which a display rule does not touch. This is the same finding the 2.1.0
    report recorded for this bullet.
  - spec 011 (specs/011-order-buyer-info) FR-016 plus new FR-016a/b/c, User Story 4, and
    SC-005 / SC-005a amended in the same change, as 2.1.0 did for this bullet.

Templates requiring follow-up: none.

Follow-up TODOs: none for governance. Principle VIII leaves the implementation change
owing an `e2e/` scenario asserting the form-step figure equals the subtotal while the
awaiting-payment figure equals subtotal + fees; the guest journey asserts no order-page
total today, so nothing there would currently catch a regression.
-->

<!--
Sync Impact Report
Version change: 3.3.0 → 3.4.0 (MINOR — materially expanded guidance. No principle is
removed or redefined: the admission-window outcomes govern a case the previous
enumeration simply did not contemplate, and every ticket that validated before this
amendment still validates the same way, because migration 0014 backfills each ticket
type's window from its parent event.)

Trigger: spec 015 (Per-Ticket Event Dates). `ticket_types` gains an `event_start` /
`event_end` pair — when a ticket ADMITS its holder, as distinct from the `sales_start` /
`sales_end` pair that says only when it can be bought. A three-day festival's Day 1 and
Day 2 passes previously both advertised, and both printed, the parent event's opening
date.

Modified sections:
  - Critical Data Flow Rules, admin ticket validation — the outcome set grows from
    three to five. `Not yet valid` and `Expired` join `Valid`, `Already Used` and
    `Invalid`, with an explicit precedence and an explicit statement that the window
    admits no tolerance. The irreversibility of `Used` is unchanged.
  - Critical Data Flow Rules — a new bullet on which date each surface names: a
    ticket's date comes from its ticket type, an event's from the event.

Templates requiring updates: none. The plan template's Constitution Check already
carries the Principle VIII row this change exercises.

Follow-up TODOs: none. `PRD.md` §1.4 and `SCHEMA.md` are updated in this same change,
as Governance requires.
-->

<!--
Sync Impact Report
Version change: 3.2.0 → 3.3.0 (MINOR — materially expanded guidance. No principle is
removed or redefined and nothing compliant under 3.2.0 becomes non-compliant: the new
paragraph governs a webhook outcome the previous text simply did not contemplate.)

Trigger: merge of feat/payment. That branch was cut from 3.0.0 and authored two
amendments of its own, numbered 3.0.1 (PATCH — Midtrans → Manjo in the Technology Stack
and Principle V) and 3.1.0 (MINOR — settle-on-expiry). Meanwhile main took 3.1.0 for the
Redis read cache and 3.2.0 for the e2e acceptance gate, so the branch's 3.1.0 collides
with a different amendment. This merge re-versions both branch amendments onto the head
of the 3.2.0 line as a single 3.3.0; their substance is unchanged and is restated below.
The superseded 3.0.1 and 3.1.0 branch reports are not retained separately — this entry
replaces them.

Modified sections:
  - Principle IV (Transactional Integrity & Idempotency) — added the settle-on-expiry
    case: a completed-payment webhook for a self-expired order settles it and
    re-deducts its quota in the same transaction, all-or-nothing across every ticket
    type held; a shortfall changes nothing and still answers 200; a gateway-rejected or
    gateway-cancelled order is never settled this way; and no non-webhook path may move
    an order to `PAID`. The existing enumeration of quota-*restoring* outcomes
    (`expire`, `cancel`, `deny`, `failure`) is unchanged. Rationale extended.
  - Principle V (Payment Gateway Abstraction) — the example provider is updated, and
    `VerifyWebhook`'s parameter is noted as the presented credential rather than a body
    signature. The method keeps its name and its role: authenticate an inbound
    notification and normalise it. The Manjo contract carries no signature at all — the
    gateway presents a bearer token — so the parameter reflects reality while the
    interface the principle mandates is unchanged. `FetchStatus` was never named by this
    principle and its removal is therefore not a deviation.
  - Technology Stack Requirements, Payment bullet — the concrete implementation is the
    Manjo gateway over plain HTTP REST, with wire shapes from the shared `cdtc` contract
    module. No vendor SDK is or was a dependency. The Infrastructure bullet keeps the
    PostgreSQL + Redis wording introduced at 3.1.0; the two amendments are orthogonal.

Added principles: none. Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - ARCHITECTURE.md — follow-up in the implementation change: the order lifecycle gains
    an `EXPIRED → PAID` edge, the first transition out of a terminal state in this
    system.
  - PRD.md — UPDATED WITH THIS MERGE: §1.2 Tech Stack Payment names the Manjo gateway.
    The Redis Infrastructure and Cache bullets from 3.1.0 are retained.
  - SCHEMA.md — no impact. No schema change and no product-surface change: the recovery
    is a business process run outside the app, and this system gains only two read-only
    admin views.
  - README.md is not named by the Governance clause but described the withdrawn
    Admin → Reconciliation screen and the old `MIDTRANS_BASE_URL` override; both are
    swept in this merge.

Deferred items: none.

Previous report (3.1.0 → 3.2.0) follows.

Version change: 3.1.0 → 3.2.0 (MINOR — a new principle is added; no existing principle
is removed, weakened, or redefined. Nothing compliant under 3.1.0 becomes non-compliant
in behavior: Principle VIII binds the delivery process, not the running system. It does
make an existing informal practice mandatory, which is exactly the "materially expanded
guidance added" case the versioning policy assigns to MINOR.)

Trigger: user direction of 2026-08-11. The `e2e/` Playwright UAT suite exists and covers
the full purchase journey, the admin console, and cache coherence, but no governance
document mentioned it. An agent or contributor reading this constitution, PRD.md,
ARCHITECTURE.md, or the root README would finish a feature or a bugfix without ever
learning the suite was there — and a suite nobody is told to run is a suite that rots
into a permanent red. This amendment makes awareness structural rather than incidental.

Added principles:
  - VIII. End-to-End Acceptance Coverage (NON-NEGOTIABLE) — `e2e/` is the acceptance
    gate for every change touching a covered flow. The suite MUST be green before merge;
    a change altering observable behavior MUST update it in the same change; a bugfix in
    a covered flow MUST add a scenario that fails before the fix and passes after; the
    payment provider is the only substitution permitted, and tests MUST NOT write order
    state directly; the suite MUST pass with the Principle VII cache both on and off.

Modified sections:
  - Technology Stack Requirements — added a Testing bullet naming the three tiers (Go
    unit + database-backed, frontend Vitest, `e2e/` Playwright) and pointing at
    Principle VIII for when each is mandatory. No existing bullet changed.
  - Governance — the pre-merge check now explicitly names the e2e suite alongside the
    spec/plan/PR review that was already required.

Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - ARCHITECTURE.md — UPDATED WITH THIS AMENDMENT: new §3.9 "End-to-End Acceptance Suite
    (UAT)" documenting the test topology (real browser → real Next.js → real Go API →
    real PostgreSQL and Redis, with the gateway replaced at the network boundary), the
    port offsets, and the rule that settlement arrives as a signed webhook rather than a
    direct database write.
  - PRD.md — UPDATED WITH THIS AMENDMENT: §1.2 Tech Stack gains a Testing bullet. No
    functional requirement or scope boundary changes; the suite verifies §1.4, it does
    not extend it.
  - SCHEMA.md — no impact. The suite adds no column, table, or constraint. It TRUNCATEs
    the application tables of a disposable local database and deliberately preserves the
    migration-seeded master data (`genders`, `order_statuses`).

Non-governance documents updated in the same change, because they are where an agent
actually looks first:
  - AGENTS.md (new, repository root) + CLAUDE.md pointer — mirrors the existing
    frontend/AGENTS.md + frontend/CLAUDE.md convention, so the e2e obligation loads into
    every agent's context automatically instead of depending on someone reading this file.
  - README.md — the Tests section now lists all three tiers instead of two.
  - .specify/templates/plan-template.md — Constitution Check gains an explicit e2e gate.
  - .specify/templates/tasks-template.md — the Polish phase now requires an e2e task for
    any user-visible flow, so generated task lists carry the obligation forward.

Deferred items: none. No placeholder tokens remain in this document.

Templates requiring follow-up: none — both affected templates are updated in this change.

Previous report (3.0.0 → 3.1.0) follows.

Version change: 3.0.0 → 3.1.0 (MINOR — a new principle is added and Principle VI's
prohibition list is narrowed. This is deliberately NOT a MAJOR bump: nothing that was
compliant under 3.0.0 becomes non-compliant under 3.1.0. Removing an item from a
prohibition list only widens what is permitted; the 2.0.0 and 3.0.0 bumps were MAJOR
because they reversed a mandate and made previously-correct behavior wrong, which is not
the case here. The new Principle VII adds binding constraints, but only on a capability
that did not previously exist in the system.)

Trigger: user direction of 2026-08-10 — event lists, ticket_type lists, and order lists
are the most frequently read data in the product and should be served from a Redis cache
kept correct by refresh-on-write. Constitution 3.0.0 named Redis in Principle VI's
out-of-scope list and restricted infrastructure to PostgreSQL only, which blocked
specs/014-redis-list-cache at the specification stage. This amendment unblocks it under
narrow, enumerated conditions rather than by lifting the restriction wholesale.

Added principles:
  - VII. Cache as a Disposable Read Accelerator (NON-NEGOTIABLE) — Redis is admitted as
    a read cache only. PostgreSQL remains the sole source of truth; no data may exist
    only in Redis; the enumerated cacheable surfaces are closed; invalidation is
    commit-triggered, not TTL-driven; the cache is never authoritative for quota; cache
    calls are forbidden inside order-writing transactions (same row-lock reasoning as
    Principle IV); the system fails open when Redis is down.

Modified sections:
  - Principle VI (Guest-First MVP Scope Discipline) — "Redis" removed from the
    out-of-scope enumeration and replaced with a pointer to Principle VII, which
    re-prohibits every Redis use other than the read cache. Redis Cluster, Redis as a
    primary/only store, and Redis-backed queues, sessions, locks, and pub/sub remain out
    of scope. Every other item in the list is unchanged.
  - Technology Stack Requirements, Infrastructure bullet — Docker Compose now provisions
    PostgreSQL and Redis; Redis is declared a soft dependency that the API MUST start
    without.

Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - PRD.md — UPDATED WITH THIS AMENDMENT: §1.2 Tech Stack Infrastructure is now
    "Docker, Docker Compose (PostgreSQL, Redis)" with a new Cache bullet naming the three
    cached list families; §1.6 Out of Scope drops the bare "Redis" entry and gains the
    same carve-out paragraph used in Principle VI. §1.1's "Redis Cluster" reference is
    left as written — multi-node topology remains out of scope and the sentence is still
    accurate.
  - ARCHITECTURE.md — MUST be updated in the implementing change: Redis added as a
    component alongside PostgreSQL, the refresh-on-write invalidation flow documented
    (which write paths invalidate which cached surfaces, and that invalidation runs after
    commit and outside the transaction), and the fail-open read path described. The three
    existing sequence diagrams (~lines 140, 200, 296) are unaffected — none of them
    depicts a list read.
  - SCHEMA.md — no impact. This amendment adds no column, table, or constraint, and the
    cache stores DTO-shaped list responses, not rows.

Deferred items: none. No placeholder tokens remain in this document.

Templates requiring follow-up: none.

Downstream: specs/014-redis-list-cache is unblocked and may proceed to /speckit-plan. Its
FR-001..FR-003 (cacheable surfaces), FR-006..FR-011 (commit-triggered invalidation),
FR-012..FR-013 (never authoritative for quota; fail open), and FR-021 (kill switch) were
checked line-by-line against Principle VII and are consistent with it. Its Status line
still reads "BLOCKED on constitution amendment" and its Dependencies section still calls
the amendment blocking; both are stale as of this amendment and should be cleared when
that spec is next touched.

Previous report (2.1.0 → 3.0.0) follows.

Version change: 2.1.0 → 3.0.0 (MAJOR — the delivery mandate is redefined a second
time, in the opposite direction: behavior compliant under 2.x — one email per
distinct holder address — is non-compliant under 3.0.0. Same reasoning as the
2.0.0 bump: a reversal is a backward-incompatible redefinition, not expanded
guidance.)

Trigger: clarification of 2026-08-06 (recorded in specs/011-order-buyer-info
spec.md, Clarifications): the buyer is ticket holder 1, i.e. the first holder
form IS the buyer information, and "Buyer is the only one that gets invoice and
tickets, not the others". Delivery therefore returns to exactly one email — the
rule 1.1.1 held — while everything else spec 011 introduced (holder forms with no
separate buyer form, primary contact derived from the first canonical slot,
gender_id FK, the three-column buyer snapshot) stands unchanged.

Note for future readers: 2.0.0 moved this rule to per-holder fan-out and 3.0.0
moves it back. The intermediate state was implemented and then reverted inside a
single working session; the holder email addresses are still collected and stored
as holder identity, so widening delivery again later is a delivery-layer change
only, not a data-capture one.

Modified sections:
  - Critical Data Flow Rules, ticket-generation bullet — delivery is one email to
    the order's buyer (the primary-contact snapshot `orders.buyer_email`, i.e. the
    first holder form's address) carrying every ticket in the order as a PDF plus
    the receipt. No email is sent to the other holders' addresses.
    `orders.email_sent` is set after that one email is delivered.

Added principles: none. Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - PRD.md — follow-up in the implementation change: §1.4 "Payment & Delivery"
    returns to one email to the buyer (it was rewritten for per-holder delivery
    under 2.0.0).
  - ARCHITECTURE.md — follow-up in the implementation change: the delivery
    sequence diagram returns from per-holder fan-out to a single send.
  - SCHEMA.md — no impact; no column changes.

Templates requiring follow-up: none.

Previous report (2.0.0 → 2.1.0) follows.

Version change: 2.0.0 → 2.1.0 (MINOR — new guidance added to Critical Data Flow
Rules; no existing rule removed or redefined)

Trigger: user direction during the spec 011 design iteration (2026-08-06): the
Order Summary panel on the order page's form-filling (registration) step shows
only the grand Total Payment — the itemized fee breakdown does not belong on
that step. The awaiting-payment step's summary and the receipt email keep the
full itemization; the frozen per-order fee math (order_fees) is untouched.

Modified sections:
  - Critical Data Flow Rules — added the fee-presentation bullet (form-step
    summary shows the grand total only, labeled as including taxes and fees;
    itemized Ticket Total / per-fee rows remain on the awaiting-payment summary
    and in the receipt email).

Added principles: none. Removed sections: none.

Governance-document sync (ARCHITECTURE.md, PRD.md, SCHEMA.md):
  - No impact — none of the three documents describes the summary panel's fee
    rows; this is a display rule with no flow, product-scope, or schema change.
  - spec 011 (specs/011-order-buyer-info) FR-016/US4 amended in the same change.

Templates requiring follow-up: none.

Previous report (1.1.1 → 2.0.0) follows.

Version change: 1.1.1 → 2.0.0 (MAJOR — backward-incompatible redefinition of a
binding rule: the Critical Data Flow Rules delivery mandate is reversed, so
behavior compliant under 1.1.1 — one email to the buyer with all tickets — is
non-compliant under 2.0.0. No Core Principle is removed, but the versioning
policy's MAJOR criterion, backward-incompatible redefinition, is the honest fit;
MINOR's "materially expanded guidance added" does not describe a reversal.)

Trigger: spec 011 (specs/011-order-buyer-info) removes the separate buyer contact
form from the order page: identity is collected as one holder form per standalone
ticket / per bundle unit, the topmost form's holder is the order's primary contact,
and post-payment delivery becomes per-holder (spec FR-012). The Critical Data Flow
Rules bullet mandating "exactly one email is sent to the buyer containing all
tickets as a PDF" directly conflicted with that and is redefined. The
one-Ticket-per-Attendee half of the bullet is unchanged, as is Principle IV's
non-blocking dispatch and email_sent-after-delivery rule (delivery now means every
recipient email, not one).

Modified sections:
  - Critical Data Flow Rules, ticket-generation bullet — delivery is now one email
    per distinct holder email address (tickets grouped by attendees.email so a
    bundle unit's identical rows collapse into one message), each carrying that
    holder's tickets as a PDF plus the receipt; orders.email_sent is set only
    after every recipient email is delivered. orders.buyer_* columns remain as
    the denormalized primary-contact snapshot (the topmost form's holder).

Added principles: none. Removed sections: none.

Governance-document sync (Governance clause: ARCHITECTURE.md, PRD.md, SCHEMA.md
MUST be kept in sync with any amendment):
  - PRD.md — UPDATED WITH THIS AMENDMENT: §1.4 "Checkout & Order" (buyer info
    entry → per-holder forms, topmost form = primary contact) and "Payment &
    Delivery" (1 Email to Buyer → one email per distinct holder address with
    that holder's PDF + receipt).
  - ARCHITECTURE.md — follow-up in the implementation change (documents the
    running system): BOTH sequence diagrams, not just delivery — the
    booking/checkout diagram (~line 166 "Fill buyer + visitor forms" and ~line
    169 "TX-D — save buyer + attendee details" → holder forms only, TX-D saves
    attendee details + primary-contact snapshot) AND the delivery diagram
    (~lines 189-237: single email → per-holder fan-out, email_sent after all
    recipients).
  - SCHEMA.md — follow-up in the implementation change: buyer-column comments
    (~lines 103, 111, 114) re-documented as "primary contact — snapshot of the
    topmost holder form (spec 011)"; buyer_dob/buyer_gender and the attendees
    gender storage change ship with spec 011's migration and must be reflected
    in the same change.

Templates requiring follow-up: none.

Previous report (1.1.0 → 1.1.1) follows.

Version change: 1.1.0 → 1.1.1 (PATCH — wording only, no semantic change)

Trigger: spec 008 (e2e purchase flow) split the single-call checkout into the two-phase
guest flow and renamed the guest endpoints (research R10, clarification 2026-08-05).
Principle IV named `POST /api/v1/checkout` literally; that path no longer exists. The
transactional rule itself transfers unchanged: the order-creating transaction is now
`POST /ticket/book` (TX-B: order + order_items + attendee slots + quota deduction), and
the gateway call sits behind `POST /ticket/checkout/:order_id`, still outside any
row-locking transaction.

Modified sections:
  - Principle IV — replaced the literal `POST /api/v1/checkout` reference with the
    booking/checkout endpoints; no rule added, removed, or weakened.

Templates requiring follow-up: none.

Previous report (1.0.0 → 1.1.0) follows.

Version change: 1.0.0 → 1.1.0 (MINOR — materially expanded guidance, no principle removed
or redefined; all 1.0.0 rules remain in force unchanged)

Trigger: a cross-artifact review of specs/001-003 against PRD.md, ARCHITECTURE.md, and
SCHEMA.md found five critical defects. Three of them traced back to ambiguities this
document left open rather than to spec-authoring mistakes, so they are closed here to
prevent recurrence.

Modified sections:
  - Principle IV (Transactional Integrity & Idempotency) — added the prohibition on
    external network calls inside the checkout transaction; added `failure` to the
    enumerated webhook statuses that restore quota.
  - Principle VI (Guest-First MVP Scope Discipline) — clarified that "associated Order"
    means referenced by `order_items` OR `attendees` (both are ON DELETE RESTRICT).
  - Technology Stack Requirements — recorded that no object storage exists in this MVP
    and QR images are therefore generated on demand.
  - Critical Data Flow Rules — added the quota-is-remaining rule.

Added principles: none. Removed sections: none.

Deferred items resolved: RATIFICATION_DATE set to 2026-07-31, the date this constitution
was first adopted. Correct it if the project's actual adoption predates that.

Templates requiring follow-up: none. specs/001-003 were already updated to match every
rule below before this amendment was written.
-->

# Event Ticketing MVP Constitution

## Core Principles

### I. Modular Monolith (NON-NEGOTIABLE)
The backend MUST be structured as a domain-driven Modular Monolith, not a layer-first
(MVC) monolith. Business logic MUST live under `internal/<domain>/` with domains limited
to: `admin`, `event`, `order`, `payment`, `ticket`, `notification`. Shared, non-domain
utilities (DB connection, config, logger) belong in `pkg/`. SQL schema sources live in
`migrations/` and are the basis for `sqlc` generation.
Rationale: The MVP intentionally avoids microservices, but the codebase must remain
cleanly decomposable into services later without a rewrite.

### II. Domain Isolation
Domains MUST NOT import another domain's repository, and MUST NOT execute SQL JOINs
across tables owned by different domains for write/update operations. Cross-domain
reads needed by a domain's business logic (e.g., `order` checking `event` quota) MUST
go through a Go interface injected into the consuming domain's service (e.g.,
`EventProvider.CheckAndDeductQuota`), never a direct repository import.
Rationale: Preserves domain boundaries so each domain can be extracted into an
independent service later; prevents hidden coupling through the database.

### III. DTO Isolation
`sqlc`-generated database structs MUST NOT be returned in HTTP responses. Every domain
MUST define its own `dto.go` describing the exact JSON shape returned to clients.
Rationale: Decouples the wire contract from the storage schema so either can evolve
independently.

### IV. Transactional Integrity & Idempotency
Booking (`POST /api/v1/ticket/book`) MUST wrap creation of `orders`, `order_items`,
`attendees`, and the atomic deduction of `ticket_types` quota in a single SQL
transaction (`BEGIN ... COMMIT`), passed explicitly or via context as `pgx.Tx`.
No order-writing transaction — booking's, or checkout's
(`POST /api/v1/ticket/checkout/:order_id`) form-saving one — may contain any external
network call: notably the payment gateway's `CreateTransaction`, which MUST be invoked
only after the transaction has committed, with its result persisted by a subsequent
short transaction and a failed call compensated without losing the guest's hold or
saved forms.
Payment webhook handlers MUST be idempotent: if an order's status is already `PAID`,
the handler MUST return `200 OK` immediately without reprocessing. Webhooks receiving
`expire`, `cancel`, `deny`, or `failure` MUST atomically update order status to
`EXPIRED`/`CANCELLED` and restore the deducted quota in `ticket_types`. Post-payment
work (PDF generation, QR generation, SMTP dispatch) MUST run in a non-blocking
goroutine so the webhook responds `200 OK` instantly; `email_sent` MUST be set only
after successful delivery.
A webhook reporting a completed payment for an order the system itself expired MUST
settle that order, and settling MUST **re-deduct** its quota in the same transaction as
the status change — the one webhook outcome that takes quota rather than restoring it.
The re-deduction is all-or-nothing across every ticket type the order holds: if any is
short, nothing moves, the order's status is unchanged, no tickets are issued, and the
handler still answers `200 OK` (a body may carry the refusal; the status code may not,
because a retry would fail identically). An order the *gateway* rejected or cancelled
MUST NOT be settled this way — only the system's own expiry verdict is reversible.
No order that has a payment session MAY reach `PAID` by any path other than a gateway
webhook. That rule is absolute and unchanged: wherever money is owed, exactly one
authority — the gateway's notification — decides that it arrived.
`PAID` additionally carries orders that were never payable. A **free registration**
(spec 022) writes a zero-total order directly at `PAID`, never opening a payment session,
never calling the gateway, and never writing a `payments` row. The two origins of `PAID`
MUST remain distinguishable, and that distinction is **derived, not stored**: an order is
registration-originated exactly when it carries a line for a ticket type that is not
guest-visible. Containment guarantees such a type can never be bought or bundled, so one
such line can only have come from the registration path. Every surface that reports
revenue MUST apply that derivation; the status alone MUST NOT be read as evidence money
moved. Such an order MUST carry a
total of zero and no `order_fees`; MUST deduct quota through the same atomic, row-locked
`UPDATE` this principle already mandates, inside one transaction with its `orders`,
`order_items` and `attendees` rows; and MUST obey the no-network-call and no-cache-call
rules above without exception. A registration-originated order MUST NOT be reachable by
any webhook, MUST NOT be swept by the expiry sweeper, and MUST NOT restore quota.
Rationale: Prevents overselling, double-processing of payments, and slow/blocked
webhook responses that payment providers may retry or flag as failing. The
no-network-call rule exists because the quota-deducting `UPDATE` holds a row lock
until commit: a gateway round-trip inside that transaction would serialize every
concurrent buyer of the same ticket type behind it, collapsing checkout throughput to
roughly one order per gateway round-trip.
The settle-on-expiry rule exists because a lost notification is not hypothetical: a
gateway's automatic retry budget is finite, so an outage that outlasts it strands a
guest who genuinely paid. The recovery is to ask the gateway to redeliver, which means
the *notification* path — not a second, staff-only path — has to be able to finish the
job. Requiring that recovery run through the same webhook code every ordinary purchase
exercises is the whole point: a rescue path used once a month is a path nobody knows is
broken. Making it all-or-nothing keeps the no-overselling guarantee intact when the
seats have since been resold, and confining every *payable* order to the webhook route
keeps exactly one source of payment truth.
The registration carve-out is granted because the invariant it appears to break is not
the one that was ever load-bearing. What `PAID` had to guarantee was: *no order is
treated as settled unless the gateway said so*. A free registration is never settled,
because nothing was ever owed — there is no payment to confirm, no gateway that could
confirm it, and no second source of truth to disagree with. What `PAID` actually means
across both origins is narrower and now stated plainly: **this order is final and its
tickets are issuable.** The alternative — a separate terminal status — was rejected
deliberately: issuance, ticket lookup, e-ticket rendering, resend, admin listing and
validation are all keyed on `PAID`, so a second issued-state would fan out into every one
of those read paths and leave a permanent trap for the next path that forgets it. The
honest cost is recorded rather than hidden: `PAID` alone no longer implies money moved,
which is precisely why the registration marker is mandatory rather than advisory. Any
surface reporting revenue MUST read that marker, not the status.

### V. Payment Gateway Abstraction
The `internal/payment` domain MUST expose a `Gateway` interface
(`CreateTransaction`, `VerifyWebhook`) so gateways can be swapped without modifying the
`order` domain. `VerifyWebhook` takes the credential the caller presented — a bearer
token, a signature, or whatever the provider's contract specifies — and its job is
unchanged either way: authenticate an inbound notification and normalise it onto a
provider-neutral shape.
Rationale: Keeps payment-provider-specific logic isolated and replaceable, consistent
with Domain Isolation (Principle II). This has been exercised: the initial Midtrans
adapter was replaced wholesale by the Manjo one (spec 012) with no change to the
`order` domain beyond removing a DTO field the new contract has no use for.

### VI. Guest-First MVP Scope Discipline
Ticket purchase MUST work end-to-end for unauthenticated guest users (browse → dynamic
attendee entry → checkout → pay → receive QR/PDF ticket by email) without requiring
account creation. Deletion of an `Event` or `Ticket Type` that has at least one
associated `Order` MUST be rejected with `400 Bad Request`; "associated" means
referenced by an `order_items` row OR an `attendees` row, since SCHEMA.md declares
`ON DELETE RESTRICT` on both foreign keys and guarding only one of them surfaces a raw
constraint violation instead of the required `400`. Features explicitly out of
scope for this MVP (microservices deployment, Kafka/RabbitMQ, Kubernetes, CQRS,
Event Sourcing, loyalty points, leaderboards, multi-organizer, refunds, coupons,
promotions, waiting rooms, queue systems, seat selection, multi-currency,
multi-language) MUST NOT be implemented unless this constitution is amended.
Redis is admitted for exactly one purpose — the read cache defined in Principle VII —
and every other Redis use (primary or sole store, job queue, session store, distributed
lock, pub/sub, Redis Cluster or any multi-node topology) remains out of scope under this
principle.
Rationale: The MVP's goal is validation within a 5-day delivery window; scope creep
into enterprise/scale concerns directly threatens that timeline. The cache carve-out is
granted because it is additive and removable — it changes no schema, no wire contract,
and no business rule, and deleting it returns the system to its pre-cache behavior — so
it does not carry the timeline risk the rest of this list does.

### VII. Cache as a Disposable Read Accelerator (NON-NEGOTIABLE)
Redis MAY be used as a read cache in front of the product's highest-volume list reads,
subject to all of the following rules. Every rule is a MUST.

* **PostgreSQL remains the single source of truth.** No datum may exist only in Redis.
  Flushing Redis entirely MUST leave the system fully correct and MUST cost nothing but
  latency on the next read. Redis MUST NOT be used for writes, write-behind, deferred
  persistence, sessions, locks, queues, or pub/sub.
* **The cacheable surfaces are closed and enumerated.** Only *list* reads over these
  entity families may be cached, in both their public and their admin projections, and
  including filtered variants: `events`, `ticket_types`, ticket packages, `orders`, and
  `attendees`. The guest-facing published event list and per-event `ticket_types` and
  package lists are the primary targets — they carry by far the highest read volume — and
  the admin lists are admitted because they are the same rows invalidated by the same
  writes, so excluding them would add an exception without removing any risk. Nothing
  else may be cached: not single-record detail reads, not ticket lookup by `ticket_code`,
  not payment or checkout status, not the QRIS image. Adding a surface to this list
  requires amending this constitution.
* **Invalidation is triggered by commit, not by time.** Every committed write that
  changes cached data MUST invalidate the affected entries so the next read returns
  post-write content; a transaction that rolls back MUST NOT invalidate. This covers
  every write path, including admin CRUD, booking, checkout, payment webhooks
  (`settlement`/`capture`, `expire`, `cancel`, `deny`, `failure`), and any expiry sweep.
  A TTL MAY exist only as a backstop bounding the damage of a missed invalidation; TTL
  expiry MUST NOT be the mechanism by which the system becomes correct.
* **The cache is never authoritative for inventory.** Quota and availability decisions
  MUST continue to be made by the atomic, row-locked `UPDATE` inside the booking
  transaction (Principle IV). A cached availability figure is a display value only; it
  MUST NOT gate, authorize, or short-circuit a sale.
* **No cache call inside an order-writing transaction.** Redis commands MUST NOT be
  issued between `BEGIN` and `COMMIT` of booking's or checkout's transaction. Rationale:
  identical to Principle IV's ban on gateway calls — the quota-deducting `UPDATE` holds a
  row lock until commit, so a Redis round-trip inside it would serialize every concurrent
  buyer of the same ticket type behind network latency. Invalidation runs after the
  commit returns.
* **Fail open.** Redis being unreachable MUST degrade performance only. Reads MUST fall
  back to PostgreSQL and succeed; writes MUST commit; no endpoint may return an error
  because of Redis. `/healthz` MUST report degraded — not failing — when Redis is down
  and PostgreSQL is up. The API MUST start and serve with Redis absent.
* **Swappable behind an interface, consistent with Principles II, III, and V.** Domain
  services MUST reach the cache through an injected interface (shared infrastructure
  under `pkg/`), never by importing a Redis client directly, so the store can be replaced
  or removed. Cached values MUST be the domain's own DTOs — `sqlc`-generated structs MUST
  NOT be serialized into the cache, since that would smuggle the storage schema into a
  second contract.
* **Killable by configuration.** A single configuration switch MUST disable caching and
  return the system to direct database reads with no other behavioral difference. The
  test suite MUST pass with caching both on and off.

Rationale: Event, ticket-type, and order lists are read far more often than they change
and dominate database load, so caching them is the highest-value performance change
available. The constraints above exist so this remains a removable accelerator rather
than a second system of record — the failure mode that would make the modular monolith
harder, not easier, to decompose later.

### VIII. End-to-End Acceptance Coverage (NON-NEGOTIABLE)
The Playwright suite in `e2e/` is this project's acceptance gate. It drives a real
browser against the real Next.js app, the real Go API, and real PostgreSQL and Redis;
the payment provider is the only substitution permitted, and it is replaced at the
network boundary so the API still speaks the gateway protocol and settlement still
arrives as a correctly signed webhook. Every rule below is a MUST.

* **Green before merge.** No change merges with the suite failing. "Unrelated failure"
  is not an exemption — it is either a real regression or a broken test, and both are
  fixed before the merge, not after.
* **Behavior change means suite change, in the same commit.** Any change to a covered
  flow that alters what a user can see or do MUST update the affected specs in that same
  change. Covered flows are: the guest purchase journey end to end (browse → select →
  terms → book → holder forms → QRIS → settlement → confirmation → issued tickets, plus
  ticket lookup, hold expiry, and webhook signature rejection and replay), the admin
  console (auth, order list, ticket validation including the irreversible `USED`
  transition, and the delete guard), and cache coherence per Principle VII. A change that
  leaves the suite green only because the suite never looked has not been verified.
* **A bugfix in a covered flow MUST add a scenario that fails before the fix.** Confirm
  it fails against the unfixed code. A regression test that was never seen red proves
  nothing about the bug it claims to pin.
* **A new user-visible flow MUST arrive with coverage.** Adding a surface outside the
  covered list without extending the suite requires the same explicit justification the
  Governance section demands of any other deviation.
* **No shortcuts through the back door.** Specs MUST NOT write order status, issue
  tickets, or mutate payment state directly in the database, and MUST NOT reach past the
  UI for anything the UI can do. Arrangement goes through the real API — those writes are
  also what invalidate the cache, so arranging through them keeps the setup honest.
* **Both cache modes.** The suite MUST pass with the Principle VII cache enabled and
  disabled (`E2E_CACHE_ENABLED`), which is how that principle's kill-switch requirement
  is actually verified rather than merely asserted.
* **Both throttling modes.** The suite MUST pass with Principle IX's request throttling
  enabled and disabled (`E2E_RATE_LIMIT_ENABLED`), for the same reason. Scenarios whose
  entire subject is a throttle refusal are expected to skip themselves in the disabled
  run; every other scenario MUST run in both. Because throttle state is per-client and
  process-local, the suite MUST also isolate throttle scenarios so that draining one
  scenario's allowance cannot refuse an unrelated one.
* **Not a substitute for the lower tiers.** Go unit and database-backed tests and the
  frontend Vitest suite remain required where they already apply; `e2e/` proves the
  assembled system, not each part. Correspondingly, a defect reachable only through the
  assembled system MUST be pinned here, not approximated in a unit test.

Rationale: every earlier principle in this document constrains a boundary — domain,
DTO, transaction, gateway, cache. None of them observes the system as a buyer does, and
the failures that reach users are overwhelmingly integration failures between correct
parts. This suite is the only artifact that watches money move through the whole chain,
so its authority has to be structural: a test suite that is optional in practice
degrades to red and then to deleted, and the day it does, nothing is checking the
purchase flow at all.

### IX. Request Throttling as Configurable, Killable Protection (NON-NEGOTIABLE)
Every request throttle the API enforces — request-rate limiters, per-key cooldowns, and
concurrency caps alike — is subject to all of the following. Every rule is a MUST.

* **Thresholds are deployment configuration, never a build artifact.** No throttle
  threshold may exist only as a compiled-in constant. Every threshold MUST be settable
  from the deployment's environment, and every setting MUST default to the value the
  system shipped with, so a deployment that configures nothing behaves exactly as it did
  before the setting existed. Configuration is read at startup; changing it requires a
  restart, never a rebuild.
* **Killable by configuration, at two granularities.** A single master switch MUST
  disable all throttling. Each throttled surface MUST additionally carry its own switch,
  so one can be relieved without stripping the rest. The precedence between them MUST be
  defined in exactly one place: master off disables everything regardless of a surface's
  own switch; master on defers to it; unset means enabled.
* **Disabled means substituted, not skipped.** A disabled throttle MUST be replaced by a
  pass-through that allocates no throttle state — the same discharge Principle VII
  requires of the cache, where `CACHE_ENABLED=false` substitutes `cache.NoOp{}`. Routing
  MUST be otherwise unchanged: an unmatched path MUST answer identically in both modes.
* **A per-client throttle MUST key on an identity the client cannot forge.** The client
  address MUST be derived from the connection by default. A caller-supplied forwarding
  header MAY be honoured only from proxy addresses named in configuration. A throttle
  keyed on a value any caller can set is not a throttle, and MUST NOT be described as one
  in any specification, contract, or architecture document.
* **Invalid configuration refuses startup.** A malformed, negative, or lockout-producing
  value MUST be rejected at startup, naming the offending setting, alongside every other
  configuration problem in one pass rather than one per restart. A rejection MUST name
  the value that would make the configuration valid where one exists, so an operator is
  told what to set rather than only that they are wrong.
* **Throttle state is disposable and per-instance.** It MUST live in process memory,
  MUST NOT be persisted, and MUST NOT be moved to a shared store — PRD.md §1.6 keeps
  Redis to the Principle VII read cache alone. Each instance limiting independently is
  the accepted trade-off, and a restart discarding every allowance is correct behaviour,
  not a defect.
* **Never authoritative for correctness.** A throttle decides only whether a request is
  *admitted*. It MUST NOT gate, authorize, or short-circuit a sale, and MUST NOT be
  relied on as the enforcement of any inventory, payment, or authorization rule —
  Principle IV owns those. Disabling every throttle MUST leave the system correct, and
  cost nothing but exposure to load.
* **The blast radius of disabling MUST be documented where the switch is.** Several
  throttled surfaces are unauthenticated and expensive: booking holds quota, checkout
  opens a paid gateway session, the guest resend sends real mail, and the live-status cap
  is the only bound on held-open connections and their periodic database reads. The
  configuration documentation MUST state what each switch leaves exposed.

Rationale: throttles are the only protection standing in front of unauthenticated
surfaces that cost money, quota, and mail, and they are the protection most likely to be
wrong in production — too tight refuses real buyers during exactly the launch spike the
product exists for, too loose invites abuse. A threshold that can only be changed by
rebuilding is a threshold nobody will change in time. The constraints above exist so that
tuning is fast and safe, so that "off" is a real state rather than a claim, and so that a
limit which cannot actually be enforced is never documented as though it can.

## Technology Stack Requirements

* **Frontend**: Next.js (App Router), TypeScript, TailwindCSS, TanStack Query, React
  Hook Form, Zod. Fetches for Event Lists and Quotas MUST disable aggressive caching
  (e.g., `cache: 'no-store'`) since quota data is live inventory. The server-side cache
  admitted by Principle VII does NOT relax this: the browser must still ask the API every
  time, and the API answers from a cache that the last committed write already refreshed.
* **Backend**: Golang 1.24+, Echo v4, PostgreSQL, `sqlc` for type-safe SQL queries.
* **Infrastructure**: Docker and Docker Compose (PostgreSQL and Redis) only — no
  orchestration platforms per Principle VI. Redis is a single node used solely as the
  Principle VII read cache, and is a soft dependency: `docker compose up` and the API's
  startup MUST both succeed when it is unavailable.
* **Payment**: Payment Gateway Abstraction per Principle V; the concrete
  implementation is the Manjo gateway, spoken over plain HTTP REST with the wire
  shapes taken from the shared `cdtc` contract module. No vendor SDK is a
  dependency. (Midtrans SNAP Sandbox was the initial implementation and was
  replaced by spec 012.)
* **Email & PDF**: SMTP via `go-mail/mail` or `net/smtp`; PDF via `maroto` or
  `gofpdf`; QR codes via `go-qrcode`.
* **No object storage**: this MVP has no file storage, no CDN, and no static-asset
  serving. QR images MUST therefore be generated on demand from `ticket_code` at
  render time (initial delivery, resend, and any future download), and
  `tickets.qr_code_url` MUST be left empty. `events.banner_url` is a plain URL string
  supplied by the admin — there is no upload endpoint.
* **Schema**: `SCHEMA.md` is the absolute source of truth for the PostgreSQL schema
  and MUST match what `sqlc` generates from `migrations/`. Any schema change MUST
  update `SCHEMA.md` in the same change.
* **Testing**: three tiers, none of which replaces another. Go unit and database-backed
  tests (`backend/scripts/test.sh`) cover domain logic and SQL; the frontend Vitest suite
  covers component and client logic; the Playwright suite in `e2e/` is the whole-system
  acceptance gate governed by Principle VIII. The e2e suite owns its own dependencies
  (`e2e/package.json`) and runs the API from source via `go run`, so it always tests the
  working tree rather than a built artifact. Its ports are offset from the dev ones so a
  run never collides with a stack already open.

## Critical Data Flow Rules

* Ticket generation: on `PAID`, exactly one `Ticket` (unique `ticket_code` + QR) is
  generated per `Attendee`. Delivery is to the buyer alone: **exactly one email**,
  addressed to the order's primary-contact snapshot `orders.buyer_email` — the
  first holder form's address, that form being both ticket holder 1 and the buyer
  — carrying, **for a paid order**, **exactly two document attachments**: a Payment
  Receipt document, and a single
  E-Ticket document holding **every** ticket in the order, one page per ticket.
  The receipt also remains itemized in the email body; the attachment exists so a
  buyer can file or forward the proof of payment without the email around it. The
  E-Ticket document carries no monetary figure — money is the receipt's job, and a
  ticket issued from a bundle has no per-ticket price that could honestly be
  printed. Neither document may be sent without the other: if either fails to
  render, no email goes out at all. Images the body references inline (the brand
  mark, the location pin) are message parts, not documents, and are excluded from
  that count — a buyer's attachment list still shows exactly two files.
  A **registration-originated** order (Principle IV) carries **exactly one document
  attachment**: the E-Ticket document alone. It has no receipt, in the body or as an
  attachment, and its message MUST state no monetary amount anywhere — a receipt for a
  zero-amount registration would be a proof of payment for a payment that never
  happened, which is worse than sending none. The all-or-nothing render rule applies
  unchanged to the one document it does send: a failed render sends nothing, leaves
  `email_sent` FALSE, and keeps resend armed. Everything else about delivery is
  identical across both origins — one email, to `orders.buyer_email` alone, the
  E-Ticket still carrying no monetary figure.
  The other holders' `attendees.email` values are holder identity, not
  delivery addresses, and MUST NOT be mailed. `email_sent` MUST be set only after
  that email has been delivered; a failure leaves it FALSE so resend stays armed.
  Resend (guest and admin) targets the same single address.
* Admin ticket validation: lookup by manual `Ticket Code` (primary) or camera QR scan
  (secondary) MUST resolve to one of `Valid`, `Already Used`, `Invalid`, `Not yet valid`,
  or `Expired`; marking a ticket `Used` MUST be irreversible through the validation flow.
  The last two are the admission-window outcomes (spec 015): a ticket admits only between
  its ticket type's `event_start` and `event_end`, both endpoints inclusive, with **no
  grace period on either side** — which makes `event_start` the moment admission opens
  rather than showtime, and the admin authoring surface MUST say so. Precedence is
  `Invalid` (unknown or revoked), then `Already Used`, then the window, then `Valid`: a
  ticket that was already admitted reports that even when it is also out of window.
  An out-of-window outcome MUST name the window the ticket does apply to, MUST NOT offer
  the used transition, and the mark-used endpoint MUST refuse it independently of the UI
  — hiding a button is not enforcement. `Invalid` MUST continue to disclose nothing about
  an unknown code, the window included.
* Ticket dates vs event dates: a surface naming the date of a **ticket** MUST name that
  ticket type's admission window — the order summary's per-line date and range, the
  issued PDF, and the ticket email. A surface naming the date of the **event** — the
  event list, the event detail header, the countdown — MUST continue to name the event's
  own `start_date`/`end_date`. A ticket type's window MUST fall inside its parent event's
  dates, enforced on the ticket-type write only: enforcing it on the event write as well
  deadlocks a reschedule, since neither the event nor its ticket types could move first.
  An event edit that strands a window MUST warn rather than refuse, and MUST NOT cascade
  onto any ticket type's stored window.
* Quota (`ticket_types.quota`) MUST never go negative; enforce via the `CHECK (quota
  >= 0)` constraint and atomic deduction inside the checkout transaction (Principle
  IV) — application code MUST NOT rely on optimistic checks alone.
* Fee presentation: on the order page's form-filling (registration) step, the Order
  Summary panel MUST show the pre-fee ticket subtotal and MUST NOT surface fees in any
  form — neither itemized rows nor a fee-inclusive figure. The panel keeps the heading
  "Total Payment", and the note beneath it MUST state that taxes and fees are added at
  the next step; it MUST NOT claim the figure already includes them, since above a
  fee-free number that wording understates what the guest is about to be charged. The
  grand total, and the itemized breakdown that justifies it (ticket total, each frozen
  per-order fee, grand total), MUST appear together on the awaiting-payment step's
  summary and are mandatory in the receipt email — the grand total MUST NOT appear
  before that step, unaccompanied by the breakdown. An order carrying no subtotal of its
  own (one predating fees) MUST fall back to its stored total, which for such orders
  already excludes fees. This is a display rule only: the frozen `order_fees` math, the
  stored total, the amount charged, and the gateway's gross amount are all unchanged.
* Cache coherence: every flow above that writes to `events`, `ticket_types`, packages,
  `orders`, `order_items`, or `attendees` — including quota deduction at booking and
  quota restoration on `expire`/`cancel`/`deny`/`failure` — MUST invalidate the cached
  lists holding that data, after its transaction commits and outside it, per Principle
  VII. Invalidation MUST be scoped to the affected event or order where scoping is
  possible, and MUST cover every filtered variant that could contain a changed record
  where it is not. A webhook MUST still return `200 OK` immediately: invalidation belongs
  with the non-blocking post-payment work, never ahead of the response.
* Quota semantics: `ticket_types.quota` is the **remaining** quota — the live counter
  that checkout decrements and that cancel/expire/deny/failure restore. It is NOT the
  original allocation, and SCHEMA.md defines no `quota_total` column. Any admin-facing
  surface exposing this field MUST label it as remaining (never "Total"), and any
  sold-count shown alongside it MUST be derived (`SUM(order_items.quantity)`), never
  stored. Rationale: treating this column as a static total lets an admin edit reset it
  above the true remainder, silently creating tickets that were never allocated.

## Governance

This constitution supersedes `ARCHITECTURE.md`, `PRD.md`, and `SCHEMA.md` in case of
conflict on process/governance matters, but those three documents remain the
authoritative source for the specific architectural rules, product requirements, and
database schema this constitution summarizes and enforces — they MUST be kept in sync
with any amendment here.

Amendments require: (1) a documented rationale, (2) a version bump per the policy
below, (3) updating this file's Sync Impact Report. All specs, plans, and PRs MUST be
checked against this constitution before merge; any deviation MUST be explicitly
justified in the PR/spec or rejected.

That pre-merge check includes Principle VIII: the `e2e/` suite MUST be green, and a
change that altered a covered flow MUST show the spec updates that prove it. A PR
touching a covered flow with no `e2e/` diff and no justification is incomplete, whatever
its unit tests say.

Versioning policy (semantic versioning for governance):
- MAJOR: Backward-incompatible principle removal or redefinition (e.g., abandoning
  Modular Monolith or Domain Isolation).
- MINOR: New principle or materially expanded guidance added.
- PATCH: Wording clarifications and non-semantic fixes.

**Version**: 6.0.0 | **Ratified**: 2026-07-31 | **Last Amended**: 2026-08-20

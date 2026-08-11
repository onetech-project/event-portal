<!--
Sync Impact Report
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
specs/012-redis-list-cache at the specification stage. This amendment unblocks it under
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

Downstream: specs/012-redis-list-cache is unblocked and may proceed to /speckit-plan. Its
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
Rationale: Prevents overselling, double-processing of payments, and slow/blocked
webhook responses that payment providers may retry or flag as failing. The
no-network-call rule exists because the quota-deducting `UPDATE` holds a row lock
until commit: a gateway round-trip inside that transaction would serialize every
concurrent buyer of the same ticket type behind it, collapsing checkout throughput to
roughly one order per gateway round-trip.

### V. Payment Gateway Abstraction
The `internal/payment` domain MUST expose a `Gateway` interface
(`CreateTransaction`, `VerifyWebhook`) so gateways can be swapped (e.g., from Midtrans
to another provider) without modifying the `order` domain.
Rationale: Keeps payment-provider-specific logic isolated and replaceable, consistent
with Domain Isolation (Principle II).

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
* **Payment**: Payment Gateway Abstraction per Principle V; initial concrete
  implementation is Midtrans SNAP Sandbox.
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

## Critical Data Flow Rules

* Ticket generation: on `PAID`, exactly one `Ticket` (unique `ticket_code` + QR) is
  generated per `Attendee`. Delivery is to the buyer alone: **exactly one email**,
  addressed to the order's primary-contact snapshot `orders.buyer_email` — the
  first holder form's address, that form being both ticket holder 1 and the buyer
  — carrying **every** ticket in the order as a single PDF together with the
  receipt. The other holders' `attendees.email` values are holder identity, not
  delivery addresses, and MUST NOT be mailed. `email_sent` MUST be set only after
  that email has been delivered; a failure leaves it FALSE so resend stays armed.
  Resend (guest and admin) targets the same single address.
* Admin ticket validation: lookup by manual `Ticket Code` (primary) or camera QR scan
  (secondary) MUST resolve to one of `Valid`, `Already Used`, `Invalid`; marking a
  ticket `Used` MUST be irreversible through the validation flow.
* Quota (`ticket_types.quota`) MUST never go negative; enforce via the `CHECK (quota
  >= 0)` constraint and atomic deduction inside the checkout transaction (Principle
  IV) — application code MUST NOT rely on optimistic checks alone.
* Fee presentation: on the order page's form-filling (registration) step, the Order
  Summary panel MUST NOT itemize fees — no Ticket Total or per-fee rows — it shows
  only the grand Total Payment, labeled as including all taxes and fees. The itemized
  breakdown (ticket total, each frozen per-order fee, grand total) remains on the
  awaiting-payment step's summary and is mandatory in the receipt email. This is a
  display rule only: the frozen `order_fees` math and the totals themselves are
  unchanged.
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

Versioning policy (semantic versioning for governance):
- MAJOR: Backward-incompatible principle removal or redefinition (e.g., abandoning
  Modular Monolith or Domain Isolation).
- MINOR: New principle or materially expanded guidance added.
- PATCH: Wording clarifications and non-semantic fixes.

**Version**: 3.1.0 | **Ratified**: 2026-07-31 | **Last Amended**: 2026-08-10

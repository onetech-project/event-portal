<!--
Sync Impact Report
Version change: 3.0.1 → 3.1.0 (MINOR — materially expanded guidance. No principle is
removed or redefined and nothing compliant under 3.0.1 becomes non-compliant: the new
paragraph governs a webhook outcome the previous text simply did not contemplate.)

Trigger: specs/012-manjo-payment-gateway, clarification of 2026-08-10. Recovery from a
lost payment notification is now by operator-invoked redelivery rather than by a staff
member asserting the payment. That makes the webhook path responsible for a transition
out of `EXPIRED`, which is the first webhook outcome in this system that *deducts*
quota rather than restoring it.

Modified sections:
  - Principle IV (Transactional Integrity & Idempotency) — added the settle-on-expiry
    case: a completed-payment webhook for a self-expired order settles it and
    re-deducts its quota in the same transaction, all-or-nothing across every ticket
    type held; a shortfall changes nothing and still answers 200; a gateway-rejected or
    gateway-cancelled order is never settled this way; and no non-webhook path may move
    an order to `PAID`. The existing enumeration of quota-*restoring* outcomes
    (`expire`, `cancel`, `deny`, `failure`) is unchanged. Rationale extended.

Added principles: none. Removed sections: none.

Governance-document sync (Governance clause: ARCHITECTURE.md, PRD.md, SCHEMA.md MUST be
kept in sync with any amendment):
  - ARCHITECTURE.md — follow-up in the implementation change: the order lifecycle gains
    an `EXPIRED → PAID` edge, the first transition out of a terminal state in this
    system.
  - PRD.md, SCHEMA.md — no change. No schema change and no product-surface change: the
    recovery is a business process run outside the app, and this system gains only two
    read-only admin views.
  - README.md is not named by the Governance clause but describes the withdrawn
    Admin → Reconciliation screen and must be swept alongside.

Previous report (3.0.0 → 3.0.1) follows.

Version change: 3.0.0 → 3.0.1 (PATCH — a factual correction, not a change of
principle. The Technology Stack section named Midtrans SNAP Sandbox as the
concrete payment implementation; spec 012 replaces it with the Manjo gateway, so
the line stopped being true on merge. No principle's meaning changes and nothing
compliant under 3.0.0 becomes non-compliant.)

Trigger: specs/012-manjo-payment-gateway — the payment adapter is replaced.

Modified sections:
  - Technology Stack Requirements, Payment bullet — the concrete implementation
    is the Manjo gateway over plain HTTP REST, with wire shapes from the shared
    `cdtc` contract module. No vendor SDK is or was a dependency.
  - Principle V, Payment Gateway Abstraction — the example provider is updated,
    and `VerifyWebhook`'s parameter is noted as the presented credential rather
    than a body signature. The method keeps its name and its role: authenticate
    an inbound notification and normalise it. The Manjo contract carries no
    signature at all — the gateway presents a bearer token — so the parameter
    reflects reality while the interface the principle mandates is unchanged.
    `FetchStatus` was never named by this principle and its removal is therefore
    not a deviation.

Added principles: none. Removed sections: none.

Previous report (3.0.0) follows.

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
MUST NOT be settled this way — only the system's own expiry verdict is reversible — and
no path other than a gateway webhook may move an order to `PAID`.
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
seats have since been resold, and forbidding any non-webhook route to `PAID` keeps
exactly one source of payment truth.

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
scope for this MVP (microservices deployment, Kafka/RabbitMQ, Redis, Kubernetes, CQRS,
Event Sourcing, loyalty points, leaderboards, multi-organizer, refunds, coupons,
promotions, waiting rooms, queue systems, seat selection, multi-currency,
multi-language) MUST NOT be implemented unless this constitution is amended.
Rationale: The MVP's goal is validation within a 5-day delivery window; scope creep
into enterprise/scale concerns directly threatens that timeline.

## Technology Stack Requirements

* **Frontend**: Next.js (App Router), TypeScript, TailwindCSS, TanStack Query, React
  Hook Form, Zod. Fetches for Event Lists and Quotas MUST disable aggressive caching
  (e.g., `cache: 'no-store'`) since quota data is live inventory.
* **Backend**: Golang 1.24+, Echo v4, PostgreSQL, `sqlc` for type-safe SQL queries.
* **Infrastructure**: Docker and Docker Compose (PostgreSQL) only — no orchestration
  platforms per Principle VI.
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

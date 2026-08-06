<!--
Sync Impact Report
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
  generated per `Attendee`, and exactly one email is sent to the buyer containing all
  tickets as a PDF.
* Admin ticket validation: lookup by manual `Ticket Code` (primary) or camera QR scan
  (secondary) MUST resolve to one of `Valid`, `Already Used`, `Invalid`; marking a
  ticket `Used` MUST be irreversible through the validation flow.
* Quota (`ticket_types.quota`) MUST never go negative; enforce via the `CHECK (quota
  >= 0)` constraint and atomic deduction inside the checkout transaction (Principle
  IV) — application code MUST NOT rely on optimistic checks alone.
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

**Version**: 1.1.1 | **Ratified**: 2026-07-31 | **Last Amended**: 2026-08-05

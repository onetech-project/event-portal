<!--
Sync Impact Report
Version change: [TEMPLATE] → 1.0.0 (initial ratification)
Modified principles: N/A (first fill from template placeholders)
Added sections:
  - Core Principles I-VI (Modular Monolith, Domain Isolation, DTO Isolation, Transactional
    Integrity & Idempotency, Payment Gateway Abstraction, Guest-First MVP Scope Discipline)
  - Technology Stack Requirements
  - Critical Data Flow Rules
  - Governance
Removed sections: none (template placeholders replaced)
Deferred items: TODO(RATIFICATION_DATE) — original adoption date not provided by user
Templates requiring follow-up: none — this command only updates the constitution file itself.
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
Checkout (`POST /api/v1/checkout`) MUST wrap creation of `orders`, `order_items`,
`attendees`, and the atomic deduction of `ticket_types` quota in a single SQL
transaction (`BEGIN ... COMMIT`), passed explicitly or via context as `pgx.Tx`.
Payment webhook handlers MUST be idempotent: if an order's status is already `PAID`,
the handler MUST return `200 OK` immediately without reprocessing. Webhooks receiving
`expire`, `cancel`, or `deny` MUST atomically update order status to `EXPIRED`/
`CANCELLED` and restore the deducted quota in `ticket_types`. Post-payment work (PDF
generation, QR generation, SMTP dispatch) MUST run in a non-blocking goroutine so the
webhook responds `200 OK` instantly; `email_sent` MUST be set only after successful
delivery.
Rationale: Prevents overselling, double-processing of payments, and slow/blocked
webhook responses that payment providers may retry or flag as failing.

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
associated `Order` MUST be rejected with `400 Bad Request`. Features explicitly out of
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

**Version**: 1.0.0 | **Ratified**: TODO(RATIFICATION_DATE): original adoption date not
provided | **Last Amended**: 2026-07-31

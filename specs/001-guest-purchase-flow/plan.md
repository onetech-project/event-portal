# Implementation Plan: Guest Purchase Flow

**Branch**: `001-guest-purchase-flow` | **Date**: 2026-07-31 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/001-guest-purchase-flow/spec.md`

## Summary

Guests browse published events and their ticket types, submit a checkout with dynamic
per-attendee info, and are redirected to a Midtrans payment URL. A single DB
transaction (TX1) creates the order/order_items/attendees and atomically deducts
ticket_types quota — where `ticket_types.quota` is the *remaining* quota, not a
static total; the gateway call then happens outside any transaction and a short TX2
stamps the payment URL, with a compensating cancel+quota-restore if the gateway
fails. An idempotent payment webhook maps every provider status (settlement,
capture±fraud_status, pending, deny, cancel, expire, failure) onto
Paid/Cancelled/Expired/no-op; on Paid it asynchronously generates one ticket per
attendee — persisting only the unique ticket code — and emails the buyer a single
PDF whose QR images are rendered on demand from those codes; on
Cancelled/Expired/denied/failed it restores quota. Guests can also look up a ticket
by code through a rate-limited, minimal-disclosure endpoint. Built as Go/Echo/sqlc
domains (`event`, `order`, `payment`, `ticket`, `notification`) behind a Next.js
frontend.

## Technical Context

**Language/Version**: Go 1.24+ (backend), TypeScript (Next.js App Router, frontend)

**Primary Dependencies**: Echo v4, sqlc, pgx (Postgres driver/tx), go-qrcode,
maroto or gofpdf, go-mail/mail or net/smtp, Midtrans SNAP Go client (or raw HTTP);
Next.js, TanStack Query, React Hook Form, Zod, TailwindCSS

**Storage**: PostgreSQL (schema per SCHEMA.md: events, ticket_types, orders,
order_items, attendees, tickets, payments)

**Testing**: Go `testing` + `testify` for handlers/services (with a test Postgres
DB or sqlmock for repository-level unit tests); Playwright or manual quickstart for
frontend checkout flow

**Target Platform**: Linux server (Docker Compose: API + Postgres), browser (guest
frontend)

**Project Type**: Web application (backend + frontend)

**Performance Goals**: Checkout round-trip (validate + reserve quota + create order +
get payment URL) completes in well under 3s server-side to meet SC-001's 3-minute
end-to-end guest budget; ticket lookup responds in under 1s server-side to meet
SC-005's 5s user-facing budget

**Constraints**: Checkout's `orders` + `order_items` + `attendees` + quota deduction
MUST all commit inside one `pgx.Tx` (TX1), and the gateway HTTP call MUST NOT run
inside any transaction — it happens after TX1 commits, with TX2 stamping
`payment_url`/`payment_provider` and a compensating transaction cancelling the order
and restoring quota if the gateway fails (see research.md "Checkout transaction
shape"); quota deduction MUST be atomic under concurrent requests
(`CHECK (quota >= 0)` plus a guarded UPDATE, not optimistic read-then-write), and
`ticket_types.quota` MUST be treated as remaining quota throughout; the webhook MUST
implement the complete provider-status mapping (including `deny` and `failure`) and
return `200 OK` immediately (idempotent no-op if already `PAID`) with PDF/QR/email
work offloaded to a goroutine; ticket QR images MUST be generated on demand from
`ticket_code` (no `qr_code_url` persisted, no object storage or upload endpoint
anywhere in the stack); the public ticket-lookup endpoint MUST be rate limited and
MUST NOT disclose attendee email; SCHEMA.md is LOCKED — no new columns, tables, or
indexes (so ticket-code lookups MUST be exact-match against the existing
`idx_tickets_ticket_code`, never `UPPER(ticket_code) = ...`); frontend event/quota
fetches MUST use `cache: 'no-store'`

**Scale/Scope**: MVP validation scale — single Postgres instance, no horizontal
scaling requirement; covers 4 user stories (browse, checkout, payment delivery,
ticket lookup) across 3 backend domains (event as a read dependency, order, payment,
ticket, notification) and their guest-facing frontend pages

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Check | Status |
|---|---|---|
| I. Modular Monolith | Code lives under `internal/{event,order,payment,ticket,notification}`; no MVC layering | PASS |
| II. Domain Isolation | `order` depends on `event` only via an `EventProvider` interface for quota check/deduct; no cross-domain repo imports or cross-domain JOINs for writes | PASS |
| III. DTO Isolation | Each domain exposes its own `dto.go`; sqlc structs stay internal to the domain's repository layer | PASS |
| IV. Transactional Integrity & Idempotency | Checkout wraps order+items+attendees+quota deduction in one `pgx.Tx` (TX1) — see the note below on the gateway call; webhook checks `status == PAID` and returns 200 before any reprocessing; post-payment ticket/PDF/email run in a goroutine; cancel/expire/**deny**/**failure** each restore quota atomically with the status change | PASS |
| V. Payment Gateway Abstraction | `internal/payment` defines `Gateway{CreateTransaction, VerifyWebhook}`; Midtrans is the first concrete implementation, injected behind the interface | PASS |
| VI. Guest-First MVP Scope Discipline | No login required anywhere in this flow; deletion-constraint and out-of-scope items belong to other features, not touched here; the post-MVP stale-PENDING sweep noted in research.md is a plain scheduled task, not a queue/broker, so it stays inside PRD.md §1.6 | PASS |

**Note on Principle IV and the checkout gateway call**: ARCHITECTURE.md §3.4 and
Principle IV require `orders`, `order_items`, `attendees`, and the atomic quota
deduction to be created in **one** SQL transaction. All four are in TX1 and commit or
roll back together, so that requirement is met in full. The `Gateway.CreateTransaction`
HTTP call is deliberately placed *after* TX1 commits (with TX2 stamping only
`payment_url`/`payment_provider`, and a compensating cancel+restore on gateway
failure) because holding TX1's `ticket_types` row locks across a ~200-500 ms external
call would serialize every concurrent buyer of the same ticket type. Neither
ARCHITECTURE.md nor the constitution requires the payment-provider call to be inside
that transaction — this is therefore a compliant refinement, not a deviation, and no
Complexity Tracking entry is required. Rationale and the accepted crash-window risk
are recorded in research.md "Checkout transaction shape".

No violations — Complexity Tracking table is not needed.

## Project Structure

### Documentation (this feature)

```text
specs/001-guest-purchase-flow/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md         # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/            # Phase 1 output
└── tasks.md              # Phase 2 output (/speckit-tasks - not created here)
```

### Source Code (repository root)

```text
backend/
├── cmd/
│   └── api/
│       └── main.go                     # Echo setup, router, DI wiring
├── internal/
│   ├── event/
│   │   ├── repository.go               # sqlc-backed queries (events, ticket_types reads)
│   │   ├── service.go                  # EventProvider impl: CheckAndDeductQuota
│   │   └── dto.go                      # Event/TicketType response shapes
│   ├── order/
│   │   ├── handler.go                  # GET /events*, POST /checkout
│   │   ├── service.go                  # checkout: TX1 -> gateway (no tx) -> TX2, + compensation
│   │   ├── repository.go               # orders/order_items/attendees queries, payment_url update
│   │   └── dto.go                      # CheckoutRequest/OrderResponse
│   ├── payment/
│   │   ├── gateway.go                  # Gateway interface
│   │   ├── midtrans.go                 # Midtrans SNAP implementation
│   │   ├── status.go                   # provider status -> order status mapping (incl. deny/failure)
│   │   ├── handler.go                  # POST /payment/webhook/:provider
│   │   └── repository.go               # payments table writes (raw provider status preserved)
│   ├── ticket/
│   │   ├── handler.go                  # GET /tickets/:code (rate limited, minimal disclosure)
│   │   ├── service.go                  # ticket_code generation on PAID (no QR persisted)
│   │   ├── repository.go               # tickets table (exact-match code lookup)
│   │   └── dto.go
│   └── notification/
│       ├── service.go                  # goroutine-driven email dispatch
│       ├── pdf.go                      # maroto/gofpdf PDF; renders QR from ticket_code on demand
│       └── smtp.go                     # SMTP client
├── pkg/                                 # db connection, config, logger (shared)
└── migrations/                          # SQL matching SCHEMA.md

frontend/
├── app/
│   ├── events/
│   │   ├── page.tsx                    # event list (no-store fetch)
│   │   └── [slug]/page.tsx             # event detail + ticket type selection
│   ├── checkout/page.tsx               # buyer + dynamic attendee form (RHF + Zod)
│   ├── orders/[orderNumber]/page.tsx   # post-checkout / payment redirect status
│   └── tickets/[code]/page.tsx         # ticket lookup by code
├── components/                          # shared UI (ticket-type-selector, attendee-form-row, ...)
└── lib/                                  # api client (TanStack Query hooks), zod schemas
```

**Structure Decision**: Web application split (Option 2) — Go modular-monolith
backend under `backend/internal/<domain>` per ARCHITECTURE.md, paired with a
Next.js App Router frontend under `frontend/app`. This feature touches the `event`
domain only as a read/quota dependency (full event/ticket-type CRUD lives in the
Admin Management feature) and owns `order`, `payment`, `ticket`, and `notification`.

## Complexity Tracking

*No constitution violations — table intentionally omitted.*

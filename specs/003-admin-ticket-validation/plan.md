# Implementation Plan: Admin Ticket Validation

**Branch**: `003-admin-ticket-validation` | **Date**: 2026-07-31 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/003-admin-ticket-validation/spec.md`

## Summary

JWT-authenticated admins validate tickets at the door by manual code entry or
camera QR scan, receiving a Valid/Already Used/Invalid result, and can mark a Valid
ticket as Used. Admins can also manually resend a Paid order's ticket delivery
email. Entirely owned by `internal/ticket` (validation, mark-used) and
`internal/notification` (resend), reusing the JWT middleware from
`internal/admin` established in the Admin Management feature.

## Technical Context

**Language/Version**: Go 1.24+ (backend), TypeScript (Next.js App Router, admin
frontend)

**Primary Dependencies**: Echo v4, sqlc, pgx; a browser QR-scanning library on the
frontend (e.g., a `getUserMedia`-based JS QR decoder) feeding the same manual-code
lookup endpoint; existing `go-mail`/`net/smtp` + `maroto`/`gofpdf` from the Guest
Purchase Flow feature, reused unchanged for resend

**Storage**: PostgreSQL (`tickets` table owned here for status reads/mutation;
`orders`/`attendees` read-only for resend and for showing attendee name during
validation). No object storage, static file serving, or upload endpoint exists or
is added: `tickets.qr_code_url` stays NULL/empty and QR images are generated on
demand from `ticket_code` at render time

**Testing**: Go `testing` + `testify` for lookup/mark-used/resend service logic,
including a race test for concurrent mark-used attempts on the same ticket; manual
or Playwright test for the camera-scan-to-lookup UI path

**Target Platform**: Linux server (Docker Compose: API + Postgres), browser (admin
device — laptop or mobile — for scanning)

**Project Type**: Web application (backend + frontend)

**Performance Goals**: Ticket lookup responds in well under 1s server-side to meet
SC-001's 3-second door-side budget; resend completes in well under 1s server-side
(excluding actual SMTP send latency) to meet SC-003's 15-second budget

**Constraints**: Marking a ticket Used MUST be a single atomic, guarded UPDATE
(`UPDATE tickets SET status = 'USED', updated_at = now() WHERE ticket_code = $1
AND status = 'ACTIVE'`) so two near-simultaneous mark-used attempts on the same
ticket cannot both succeed; that `updated_at` write is also how the transition is
recorded, since the LOCKED schema has no `used_at` column. Ticket code lookups
MUST normalize the *input* only (trim + uppercase) and then match `ticket_code`
exactly — the column MUST NOT be wrapped in `UPPER()`/`LOWER()`, which would
bypass the plain btree `idx_tickets_ticket_code` and force a sequential scan on
every door scan. Resend MUST reuse the same PDF-generation path as initial
delivery, reusing the tickets' existing `ticket_code` values verbatim (never
regenerated) while re-generating each QR image from those codes at render time,
and MUST set `orders.email_sent = true` on successful delivery. Resend is allowed
only for orders whose status is `PAID`; `PENDING`/`CANCELLED`/`EXPIRED` are
rejected with `400 ORDER_NOT_PAID`

**Scale/Scope**: MVP validation scale — single door-validation UI, no offline mode
or dedicated scanner hardware integration; covers 3 user stories (validate, mark
used, resend email)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Check | Status |
|---|---|---|
| I. Modular Monolith | Validation/mark-used logic lives in `internal/ticket`; resend lives in `internal/notification`, which exposes its own `POST /admin/orders/:id/resend-email` handler registered in `cmd/api/main.go` — no domain reaches into another's internals | PASS |
| II. Domain Isolation | No domain imports another's repository: resend is `notification`'s own `ResendTicketEmail(orderID)`, and it reads buyer email, order status, and the order's attendees with their ticket codes exclusively through the narrow `OrderChecker` interface — the same name Admin Management (specs/002) established, implemented by the `order` domain and injected in `cmd/api/main.go` | PASS |
| III. DTO Isolation | `ticket` domain defines its own `dto.go` for `ValidationResult`/`TicketSummary`; no sqlc struct leaks into HTTP responses | PASS |
| IV. Transactional Integrity & Idempotency | Mark-used is a single guarded UPDATE (no multi-table transaction needed); this is consistent with, though distinct from, the checkout/webhook transactional rules in the Guest Purchase Flow feature | PASS |
| V. Payment Gateway Abstraction | N/A — this feature does not touch payment | N/A |
| VI. Guest-First MVP Scope Discipline | Entirely admin-only (JWT-gated); does not introduce any new guest-facing capability or expand MVP scope beyond PRD.md's Admin QR Validation requirements. One endpoint beyond PRD §1.5's LOCKED list is added deliberately — see "PRD §1.5 API Deviation" below | PASS (with documented deviation) |

No constitution violations — Complexity Tracking table is not needed.

### PRD §1.5 API Deviation (deliberate and justified)

PRD.md §1.4 mandates "Admin can mark `Valid` tickets as `Used`", while PRD.md
§1.5's LOCKED API list names only `POST /api/v1/admin/tickets/validate` for this
feature area. This plan therefore adds exactly one endpoint,
`POST /api/v1/admin/tickets/:code/use`, because:

1. No listed endpoint can perform the mandated `ACTIVE -> USED` transition.
2. `validate` MUST remain side-effect-free — repeated door scans of the same code
   are routine and must never consume a ticket (spec.md Edge Cases, SC-002, and
   the constitution's "marking a ticket `Used` MUST be irreversible" rule, which
   makes an accidental consume unrecoverable).

The deviation is admin-only and JWT-gated, adds no guest-facing surface, and is
recorded in spec.md ("Deliberate Extension to PRD §1.5"), research.md, and
contracts/api.md. Resend and validate use their PRD-listed paths unchanged.

## Project Structure

### Documentation (this feature)

```text
specs/003-admin-ticket-validation/
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
└── tasks.md              # /speckit-tasks output (not created here)
```

### Source Code (repository root)

```text
backend/
├── internal/
│   ├── ticket/
│   │   ├── handler.go                  # POST /admin/tickets/validate,
│   │   │                                #   POST /admin/tickets/:code/use
│   │   ├── service.go                  # code normalization, status lookup,
│   │   │                                #   guarded mark-used update
│   │   ├── repository.go               # tickets table queries
│   │   └── dto.go                      # ValidationResult DTO
│   └── notification/
│       ├── handler.go                  # POST /admin/orders/:id/resend-email
│       ├── service.go                  # adds ResendTicketEmail(orderID) behind the
│       │                                #   OrderChecker interface (specs/002);
│       │                                #   reuses existing PDF/SMTP send path,
│       │                                #   re-renders QR from existing ticket_code,
│       │                                #   sets orders.email_sent = true on success
│       └── (pdf.go, smtp.go unchanged from Guest Purchase Flow feature)

frontend/
├── app/
│   └── admin/
│       └── validate/
│           └── page.tsx                # manual code input + camera QR scan UI,
│                                        #   shows Valid/Already Used/Invalid + mark-used button
├── components/admin/                    # qr-scanner, validation-result-card
└── lib/                                  # validate/mark-used/resend api hooks
```

**Structure Decision**: Web application split (Option 2), reusing the
`backend`/`frontend` layout established in the prior two features. Validation and
mark-used live in `internal/ticket` (new domain code); resend extends
`internal/notification`, which was introduced as async infrastructure by the Guest
Purchase Flow feature and is now given a synchronous, admin-triggered entry point.
`cmd/api/main.go` injects the `order` domain's `OrderChecker` implementation (the
interface name established by specs/002) into `notification`'s service, so no
cross-domain repository import is needed (ARCHITECTURE.md §3.2). The resend
trigger in the admin UI hangs off the orders list page from specs/002, whose
`GET /api/v1/admin/orders` endpoint is served by
`backend/internal/order/handler.go`.

## Complexity Tracking

*No constitution violations — table intentionally omitted.*

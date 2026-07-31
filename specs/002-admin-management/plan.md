# Implementation Plan: Admin Management

**Branch**: `002-admin-management` | **Date**: 2026-07-31 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/002-admin-management/spec.md`

## Summary

JWT-authenticated admins manage the event catalog: full CRUD on Events and flat CRUD
on Ticket Types (`/api/v1/admin/ticket-types`, per locked PRD §1.5). Deletion of
either is blocked with a 400 if any `order_items` **or** `attendees` row references
the ticket type(s) involved; a permitted event delete removes the event's ticket
types and the event itself inside one transaction, since
`ticket_types.event_id` is `ON DELETE RESTRICT`. The `quota` an admin sets is the
**remaining** quota (the same column the Guest Purchase Flow decrements), shown
alongside a read-only derived Sold count. Admins can also list Orders and Attendees
(read-only). Auth lives in `internal/admin`; event/ticket-type CRUD in
`internal/event`; the admin order/attendee list endpoints are served by
`internal/order`, which owns those tables. The only cross-domain coupling is a narrow
`OrderChecker` interface (delete guards + sold count) defined in `event` and
implemented by `order`, consistent with Domain Isolation.

## Technical Context

**Language/Version**: Go 1.24+ (backend), TypeScript (Next.js App Router, admin
frontend)

**Primary Dependencies**: Echo v4, sqlc, pgx, JWT library (e.g., `golang-jwt/jwt`),
bcrypt/argon2 for password hashing; Next.js, TanStack Query, React Hook Form, Zod,
TailwindCSS for the admin UI

**Storage**: PostgreSQL (`admins`, `events`, `ticket_types` tables owned by the
`admin`/`event` domains; `orders`, `order_items`, `attendees` remain owned by the
`order` domain, which serves the read-only admin list endpoints itself and answers
the `event` domain's delete-guard/sold-count questions through `OrderChecker`). No
schema changes — SCHEMA.md is locked.

**Testing**: Go `testing` + `testify` for handler/service/repository tests
(including regression tests for deletion blocked by an `order_items` row **and** by
an `attendees`-only row, and for an admin quota write being an absolute set of the
remaining value); component/integration tests for the admin CRUD forms

**Target Platform**: Linux server (Docker Compose: API + Postgres), browser (admin
frontend, behind login)

**Project Type**: Web application (backend + frontend)

**Performance Goals**: Admin CRUD operations and list views respond in well under
1s server-side to comfortably meet SC-001 (event+ticket-type creation in under 5
minutes, dominated by human form-filling, not server latency) and SC-003 (locate
any order/attendee record in under 30s)

**Constraints**: All admin endpoints MUST require a valid JWT; every delete guard
MUST check **both** `order_items.ticket_type_id` and `attendees.ticket_type_id`
(both are `ON DELETE RESTRICT`; checking only one leaks a raw Postgres FK violation
instead of the required clean 400) and MUST run inside the same transaction that
performs the delete to avoid a check-then-delete TOCTOU race; deleting an event MUST
delete its `ticket_types` rows before the `events` row in one transaction; admin
quota writes set `ticket_types.quota` (the remaining counter) absolutely; slug
uniqueness and date-range validations (`end >= start`, `sales_end >= sales_start`)
enforced at the service layer backed by DB unique/check constraints

**Scale/Scope**: MVP validation scale — single admin role (no permission tiers per
Assumptions), covers 4 user stories (auth, event CRUD, ticket type CRUD, order/
attendee read views)

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Check | Status |
|---|---|---|
| I. Modular Monolith | Auth lives in `internal/admin`; event/ticket-type CRUD lives in `internal/event`; the admin order/attendee list endpoints live in `internal/order`, which owns those tables; no MVC layering | PASS |
| II. Domain Isolation | `internal/order` serves `GET /admin/orders` and `GET /admin/attendees` from its own repository (same domain, so no cross-domain access at all). The `event` domain's only cross-domain need — delete guards and the derived sold count — goes through the narrow `OrderChecker` interface defined in `event` and implemented by `order`, injected in `cmd/api/main.go`; no repository imports and no cross-domain JOIN in any write path | PASS |
| III. DTO Isolation | `admin`, `event`, and `order` each define their own `dto.go`; sqlc structs (admins, events, ticket_types, orders/attendees projections) never returned directly | PASS |
| IV. Transactional Integrity & Idempotency | Both deletes run guard-and-delete in one transaction so there is no TOCTOU window: ticket-type delete is `OrderChecker.HasOrdersForTicketType(ctx, tx, id)` → `DELETE FROM ticket_types WHERE id = $1` → COMMIT; event delete is `OrderChecker.HasOrdersForEvent(ctx, tx, id)` → `DELETE FROM ticket_types WHERE event_id = $1` → `DELETE FROM events WHERE id = $1` → COMMIT (the ticket-type delete first is mandatory — `ticket_types.event_id` is `ON DELETE RESTRICT`). Each guard checks `order_items` AND `attendees`, and `RESTRICT` remains the backstop | PASS |
| V. Payment Gateway Abstraction | N/A — this feature does not touch payment | N/A |
| VI. Guest-First MVP Scope Discipline | This feature is intentionally admin-only (JWT-gated); it does not add any guest-facing capability, staying within PRD.md's defined Admin role scope | PASS |

No violations — Complexity Tracking table is not needed.

## Project Structure

### Documentation (this feature)

```text
specs/002-admin-management/
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
│   ├── admin/
│   │   ├── handler.go                  # POST /admin/login
│   │   ├── service.go                  # credential verification, JWT issuance
│   │   ├── repository.go               # admins table queries
│   │   ├── middleware.go               # JWT auth middleware for /admin/* routes
│   │   └── dto.go                      # LoginRequest/LoginResponse
│   ├── event/
│   │   ├── handler.go                  # CRUD /admin/events,
│   │   │                                #   CRUD /admin/ticket-types (flat, PRD 1.5)
│   │   ├── service.go                  # slug uniqueness, date validation,
│   │   │                                #   delete guards, absolute remaining-quota set
│   │   ├── order_checker.go            # OrderChecker interface (consumer-defined):
│   │   │                                #   HasOrdersForTicketType/HasOrdersForEvent/
│   │   │                                #   SoldCountByTicketType
│   │   ├── repository.go               # events/ticket_types queries only
│   │   └── dto.go                      # Event/TicketType admin DTOs (incl. read-only `sold`)
│   └── order/                           # (owned by specs/001; extended here)
│       ├── handler.go                  # + GET /admin/orders, GET /admin/attendees
│       │                                #   (behind the admin JWT middleware)
│       ├── repository.go               # + read-only order/attendee list queries,
│       │                                #   + OrderChecker implementation
│       └── dto.go                      # + OrderSummary / AttendeeSummary DTOs
├── cmd/api/main.go                      # injects order's OrderChecker impl into
│                                        #   the event service; registers admin routes
└── migrations/                          # (no new tables/columns; SCHEMA.md is locked)

frontend/
├── app/
│   └── admin/
│       ├── login/page.tsx
│       ├── events/
│       │   ├── page.tsx                # list + create
│       │   └── [id]/page.tsx           # edit event + manage its ticket types
│       ├── orders/page.tsx             # read-only order list
│       └── attendees/page.tsx          # read-only attendee list
├── components/admin/                    # event-form, admin-nav,
│                                        #   ticket-type-form ("Sisa Kuota / Remaining
│                                        #   Quota" field + read-only Sold count)
└── lib/                                  # admin api client with JWT attach, zod schemas
```

**Structure Decision**: Web application split (Option 2), reusing the same
`backend`/`frontend` layout as the Guest Purchase Flow feature. Auth is isolated in
`internal/admin`; event and ticket-type management lives in `internal/event`, since
ticket types are owned by the event aggregate per ARCHITECTURE.md's directory
structure. The admin order/attendee read views live in `internal/order` rather than
`internal/event`, because the `order` domain already owns `orders`, `order_items`,
and `attendees` — placing those handlers there keeps each endpoint in the domain
that owns its tables and removes a pass-through interface that added nothing. The
`event` domain retains one narrow dependency on `order` — the `OrderChecker`
interface (`HasOrdersForTicketType`, `HasOrdersForEvent`, `SoldCountByTicketType`),
defined in `event`, implemented in `order`, injected in `cmd/api/main.go` — which is
what keeps the delete guards and the derived Sold count compliant with
ARCHITECTURE.md §3.2.

## Complexity Tracking

*No constitution violations — table intentionally omitted.*

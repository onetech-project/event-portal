# Implementation Plan: Ticket Package Bundles in the Selection Step

**Branch**: `005-ticket-package-bundles` | **Date**: 2026-08-04 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/005-ticket-package-bundles/spec.md`

## Summary

Add bundle offers ("Day 1 and 2") to the booking-step ticket list, alongside individual
tickets, badged and priced in their own right. A bundle holds **no inventory**: two new
tables (`packages`, `package_tickets`) describe *what a bundle contains*, and availability is
computed at read time as `MIN(quota / quantity_per_unit)` over its constituent
`ticket_types`. Checkout never deducts "a package" — it expands each package line into
per-ticket-type demand, **aggregates that demand across the whole cart**, sorts by
`ticket_type_id`, and reuses the existing guarded `CheckAndDeductQuota`. One write path
therefore guards all inventory, individual and bundled alike.

The technical approach is a refactor of `Service.reserveOnce` from *"loop over request items,
deduct each"* into *"resolve → expand → aggregate → sort → deduct → persist"*. That refactor
is load-bearing: the current per-item loop cannot express a cart where a bundle and a
standalone ticket draw on the same pool, and its request-order deduction is already
deadlock-prone today (see [research.md](./research.md) R-002). Frontend work replaces the
current quantity `<Select>` with the designs' Add → stepper row and a persistent **Selected
Ticket** summary panel.

## Technical Context

**Language/Version**: Go 1.26.5 (backend), TypeScript 5 / React 19 on Next.js App Router
(frontend)

**Primary Dependencies**: Echo v4, `pgx/v5`, `sqlc` v1.31.1, `shopspring/decimal`,
`google/uuid`, `golang-migrate`; TanStack Query, React Hook Form, Zod, TailwindCSS, shadcn/ui

**Storage**: PostgreSQL. Schema source of truth is `SCHEMA.md`, generated from
`backend/migrations/` — new migration `000003_packages.{up,down}.sql`

**Testing**: `go test` with `testify`; repository tests gated on `TEST_DATABASE_URL` via
`internal/testsupport`; architecture rules asserted in `backend/cmd/api/architecture_test.go`;
frontend uses colocated `*.test.tsx`

**Target Platform**: Linux server via Docker Compose; browser (desktop-first per the supplied
designs)

**Project Type**: Web application — Go modular-monolith API + Next.js frontend

**Performance Goals**: Booking list interactive within 2s for ≤20 ticket types + ≤10 packages
(SC-005); package availability for a whole event in **one** query, no N+1

**Constraints**: No ticket quota may ever go negative under concurrency (SC-003); no external
network call inside the checkout transaction (Constitution IV); quota reads are never cached
(`Cache-Control: no-store`); no object storage available

**Scale/Scope**: 2 new tables, 3 altered columns, 1 migration, ~12 new SQL queries, 1
extended domain port, 1 refactored checkout transaction, 5 new/changed API surfaces, 1
redesigned booking screen, 1 new admin CRUD screen

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Checked against `.specify/memory/constitution.md` v1.1.0.

| Principle | Verdict | How this design satisfies it |
| --- | --- | --- |
| **I. Modular Monolith** | PASS | `packages` and `package_tickets` are owned by `internal/event` — they hang off an event and join only to that event's `ticket_types`. No new domain is introduced; the closed set (`admin`, `event`, `order`, `payment`, `ticket`, `notification`) is unchanged. Schema lives in `migrations/`, `sqlc` generates from it. |
| **II. Domain Isolation** | PASS | `internal/order` gains **no** import of `internal/event`. Package resolution arrives through one new method on the existing consumer-declared `EventProvider` port (`PackageForCheckout`), adapted in the composition root. No cross-domain JOIN: the `packages ⋈ package_tickets ⋈ ticket_types` join is entirely inside the `event` domain's own tables. |
| **III. DTO Isolation** | PASS | `PackageSummaryDTO` / `PackageAdminDTO` are hand-written in `internal/event/dto.go`. No `eventsql` struct reaches a response. `architecture_test.go` already enforces this and will cover the new code automatically. |
| **IV. Transactional Integrity & Idempotency** | PASS | TX1 keeps its shape: order + items + attendees + all quota deduction in one `db.InTx`. Expansion, aggregation and sorting happen **inside** TX1 but involve no network I/O. The gateway call stays in TX2. Restore-on-expire/cancel/deny/failure extends to package lines via the junction, and stays idempotent behind the existing guarded `UPDATE ... WHERE status = 'PENDING'`. |
| **V. Payment Gateway Abstraction** | PASS | Untouched. A package line becomes an ordinary `PaymentItem`; the `Gateway` interface does not change. |
| **VI. Guest-First MVP Scope** | PASS WITH NOTE | Guest-first is preserved (no account needed to select or buy a bundle). The out-of-scope list names **"Promotions"** and **"Coupons"** — see Complexity Tracking for why a package is neither. Delete-guards are extended, not weakened. |
| **Quota semantics** | PASS | `ticket_types.quota` remains the sole, *remaining* counter. Packages add **no** quota column anywhere — schema, DTO, admin form or request body. `sold` for a ticket type is still derived, and is extended to count package lines through the junction (otherwise every bundled sale would be under-reported). |
| **Governance** | ACTION REQUIRED | PRD.md §1.5 is LOCKED and predates packages; `SCHEMA.md` must move with the migration. Both updates ship in this change. Principle VI's delete-guard wording needs a clarifying amendment (see Complexity Tracking). |

**Gate result: PASS.** Two items carry required follow-up actions rather than violations;
both are tracked below.

### Post-Design Re-check (after Phase 1)

Re-evaluated against [data-model.md](./data-model.md) and `contracts/`:

- **I / II hold.** Every new query lives in `internal/event/queries/event.sql`; `order`'s only
  new dependency is one port method. Verified by design, and asserted by
  `TestNoDomainImportsAnotherDomain`.
- **III holds.** The two new DTOs are hand-written; `AvailableUnits` and `Sold` are derived
  fields on the DTO, not generated columns.
- **IV holds, and is strengthened.** The aggregate-then-sort step closes a *pre-existing*
  latent deadlock in `reserveOnce` (research.md R-002), so this design leaves transactional
  integrity better than it found it.
- **VI holds.** No new out-of-scope capability is introduced; a package is a product, not a
  discount rule.
- **One new obligation surfaced by design**: composition edits under open `PENDING` orders
  would drift quota. Resolved in research.md R-004 by rejecting such edits — no schema
  change, no snapshot table, no scope growth.

**No violation requires justification.** Complexity Tracking records the two scope/governance
clarifications only.

## Project Structure

### Documentation (this feature)

```text
specs/005-ticket-package-bundles/
├── plan.md                          # This file
├── spec.md                          # Feature specification
├── research.md                      # Phase 0 output
├── data-model.md                    # Phase 1 output
├── quickstart.md                    # Phase 1 output
├── checklists/
│   └── requirements.md              # Spec quality checklist (16/16 pass)
├── contracts/
│   ├── schema.md                    # ERD, DDL, migration 000003
│   ├── models.md                    # Go structs + TypeScript interfaces
│   ├── checkout-transaction.md      # Availability query + race-safe deduction
│   └── api.md                       # Endpoints
└── tasks.md                         # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
backend/
├── migrations/
│   ├── 000003_packages.up.sql               # NEW
│   └── 000003_packages.down.sql             # NEW
├── internal/
│   ├── event/                               # OWNS packages + package_tickets
│   │   ├── queries/event.sql                # +package queries, availability CTE
│   │   ├── eventsql/                        # regenerated (sqlc)
│   │   ├── package_service.go               # NEW  admin CRUD + composition validation
│   │   ├── package_handler.go               # NEW  /admin/packages
│   │   ├── availability.go                  # NEW  derived availability, no stored state
│   │   ├── dto.go                           # +PackageSummaryDTO, PackageAdminDTO
│   │   ├── public_service.go                # +packages[] on GET /events/:slug
│   │   └── repository.go                    # +package reads/writes
│   └── order/
│       ├── event_provider.go                # +PackageForCheckout on the port
│       ├── service.go                       # REFACTOR reserveOnce; +expand/aggregate/sort
│       ├── demand.go                        # NEW  expansion + aggregation, pure & unit-tested
│       ├── repository.go                    # +package-aware order_items, attendee package_id
│       ├── queries/order.sql                # +ListQuotaHoldsByOrderID (junction expansion)
│       ├── dto.go                           # +discriminated order line
│       └── admin_service.go                 # +package-aware attendee/order views
├── cmd/api/
│   ├── adapters.go                          # adapt event→order port for packages
│   └── main.go                              # route registration
└── internal/testsupport/testsupport.go      # +SeedPackage, SeedPackageTicket, truncate

frontend/
├── app/events/[slug]/page.tsx               # REWRITE to the designs' selection screen
├── app/checkout/page.tsx                    # +package lines, +bundle attendee slots
├── components/booking/                      # NEW
│   ├── selectable-row.tsx                   #   Add → stepper row, bundle badge
│   └── selection-summary.tsx                #   "Selected Ticket" panel
├── app/admin/events/[id]/page.tsx           # +package section (no quota field)
├── lib/types.ts                             # +package wire types
├── lib/schemas.ts                           # +Zod schemas for package checkout
└── lib/queries.ts                           # +usePackages, admin package mutations
```

**Structure Decision**: The existing two-tier layout (`backend/` Go modular monolith +
`frontend/` Next.js App Router) is kept unchanged. No new top-level directory and no new
backend domain: packages belong to `internal/event` because they are composed exclusively of
that domain's tables, which is what keeps Principle II satisfiable without a second port. The
only new frontend directory is `components/booking/`, extracted because the selection row and
summary panel are shared between the event page and the checkout review step.

## Phase Plan

### Phase 0 — Research → [research.md](./research.md)

Nine decisions resolved, including the two the specification deferred (composition edits
under open orders; the `sqlc` nullability fallout of making `order_items.ticket_type_id`
nullable). No `NEEDS CLARIFICATION` remains.

### Phase 1 — Design & Contracts

- [data-model.md](./data-model.md) — entities, fields, relationships, validation rules,
  derived-value rules, state transitions
- `contracts/` — authored during `/speckit-specify`, unchanged by planning:
  [schema.md](./contracts/schema.md), [models.md](./contracts/models.md),
  [checkout-transaction.md](./contracts/checkout-transaction.md), [api.md](./contracts/api.md)
- [quickstart.md](./quickstart.md) — runnable validation, including the concurrency and
  deadlock proofs

### Phase 2 — Tasks

Not produced by this command. Run `/speckit-tasks`.

### Suggested implementation order

Sequenced so each step is independently verifiable and the riskiest work is proven early:

1. **Migration + `SCHEMA.md`** — schema lands, `sqlc` regenerates, existing suite goes green
   (this is where the nullability fallout of R-005 is absorbed).
2. **Availability reads** — the CTE query and `GET /events/:slug` `packages[]`. Demonstrable
   against seeded data with no checkout involved. Delivers spec Story 2.
3. **Checkout refactor** — `demand.go` expansion/aggregation as pure functions with unit
   tests, then wire into `reserveOnce`. Concurrency and deadlock tests here. Delivers Story 3.
4. **Restore path** — `ListQuotaHoldsByOrderID`, expiry/webhook restore, idempotency tests.
5. **Admin CRUD** — package authoring, composition validation, delete guards. Delivers Story 4.
6. **Booking screen** — Add → stepper rows, bundle badge, Selected Ticket panel. Delivers
   Story 1.
7. **Checkout screen** — bundle attendee slots, discriminated order lines.
8. **PRD.md + constitution clarification.**

Steps 2 and 3 are the ones worth front-loading review on; the rest is conventional.

## Complexity Tracking

> Two entries. Neither is a design violation — both are scope/governance clarifications the
> constitution requires be written down rather than assumed.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Principle VI lists **"Promotions"** and **"Coupons"** as out of scope, and a bundle priced below the sum of its parts resembles a discount | A package is a **product**, not a discount rule: it is a row a buyer selects, with its own name, price, sales window and order line. It carries no code to redeem, no conditions to evaluate, no eligibility rules, and no interaction with any other line in the cart. The excluded features are *rule engines applied to a cart*; this is a SKU. The supplied designs list it inline with tickets as a peer product, not as an applied discount. | Modelling bundles as a discount rule on individual tickets would import exactly the promotion engine the constitution excludes — conditional pricing, rule precedence, stacking — and would still need the same multi-ticket quota expansion. It is strictly more complexity for the same outcome. Omitting bundles entirely was rejected because they are the requested feature and are already drawn into the approved designs. |
| Principle VI defines "associated Order" as referenced by `order_items` **or** `attendees`, treating the second check as a redundant safety net | Package-only orders leave `order_items.ticket_type_id` NULL on every line, so a ticket-type delete-guard reading only `order_items` now sees nothing. The `attendees` check stops being redundant and becomes **load-bearing** (`attendees.ticket_type_id` stays NOT NULL precisely for this). The constitution's wording needs a PATCH-level clarification to say so. | Making `attendees` optional for bundle registrants, or expanding package lines into per-constituent `order_items` rows, were both rejected: the first breaks the one-pass-per-attendee rule, the second forces an invented per-constituent price split that would make `total_amount` stop reconciling against `SUM(quantity × price)`. |

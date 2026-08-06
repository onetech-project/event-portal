# Implementation Plan: Single Visitor Form per Bundle

**Branch**: `feat/ticket` (feature dir `010-bundle-single-form`) | **Date**: 2026-08-05 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/010-bundle-single-form/spec.md`

## Summary

Collapse the order page's per-ticket visitor forms into one form per **bundle unit**: a bundle containing N tickets shows a single form titled with the bundle's name, and its data is written identically onto all N attendee slots, while ticket/QR issuance (1 per slot) stays untouched.

Technical approach (research.md): booking stamps a new `attendees.package_unit` ordinal so slots are deterministically grouped per purchased bundle unit; the order read exposes `package_id` + `package_unit` on each slot; the frontend groups slots by `(package_id, package_unit)` into one form per group and **fans the form values out client-side** into the existing per-slot `attendees[]` checkout payload (wire contract unchanged); the backend adds a consistency validation so slots of the same unit can only be filled with identical visitor data.

## Technical Context

**Language/Version**: Go 1.24+ (backend), TypeScript / Next.js App Router (frontend)

**Primary Dependencies**: Echo v4, pgx/v5, sqlc (backend); React Hook Form + Zod, TanStack Query, TailwindCSS (frontend)

**Storage**: PostgreSQL — one additive migration (`attendees.package_unit SMALLINT NULL`), SCHEMA.md updated in the same change

**Testing**: `go test ./...` (backend), Vitest + Testing Library (frontend)

**Target Platform**: Linux server (Docker Compose) + modern browsers

**Project Type**: Web application (backend/ + frontend/)

**Performance Goals**: No new hot paths; booking gains one integer column per slot insert, checkout gains one in-memory grouping pass — negligible

**Constraints**: Checkout wire contract (`POST /ticket/checkout/:order_id`) must stay backward-compatible; orders booked before the migration (NULL `package_unit`) must keep working with today's one-form-per-slot behavior

**Scale/Scope**: MVP scale (spec 008); ~6 backend files, ~4 frontend files, 1 migration

## Constitution Check

*Constitution v1.1.1 — evaluated before Phase 0 and re-checked after Phase 1 design.*

| Principle | Verdict | Notes |
|---|---|---|
| I. Modular Monolith | PASS | All backend changes stay in `internal/order` (booking slot creation, checkout validation, DTOs); ticket issuance untouched. |
| II. Domain Isolation | PASS | No new cross-domain repository imports or JOINs; the slots query's existing read-only JOIN to `ticket_types` is unchanged in kind (only two columns added to its SELECT). |
| III. DTO Isolation | PASS | The wire change is made in `internal/order/dto.go` (`TicketOrderSlot` gains `package_id`, `package_unit`); no sqlc struct is exposed. |
| IV. Transactional Integrity & Idempotency | PASS | `package_unit` is stamped inside the existing TX-B booking transaction; the same-unit consistency check runs before TX-D like all other checkout validation; no network calls enter any transaction. |
| V. Payment Gateway Abstraction | PASS | Payment flow untouched. |
| VI. Guest-First MVP Scope | PASS | Refines the existing guest flow; nothing from the out-of-scope list is introduced. |
| Critical Data Flow Rules | PASS | One ticket per attendee row on PAID is preserved — slot count per order is unchanged, so FR-004 (N passes per bundle) holds with zero ticket-domain changes. Quota logic untouched. |
| SCHEMA.md sync | PASS (action required) | Migration `000011` must update `SCHEMA.md` (attendees table + index section) in the same change. |

**Post-Phase-1 re-check**: PASS — no violations introduced by the design artifacts; Complexity Tracking left empty.

## Project Structure

### Documentation (this feature)

```text
specs/010-bundle-single-form/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── order-forms.md   # Phase 1 output — slot DTO, grouping, checkout validation
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
backend/
├── migrations/
│   └── 000011_attendee_package_unit.{up,down}.sql   # NEW
├── internal/order/
│   ├── demand.go            # ExpandedItem gains per-unit demand
│   ├── service.go           # bookOnce per-unit slot loop; CheckoutOrder same-unit validation
│   ├── repository.go        # AttendeeRef/AttendeeSlotRecord gain PackageUnit
│   ├── dto.go               # TicketOrderSlot gains package_id, package_unit
│   ├── queries/order.sql    # CreateAttendeeSlot + ListAttendeeSlotsByOrderID columns
│   └── *_test.go            # booking_test.go, checkout_forms_test.go, checkout_handler_test.go
└── (sqlc regenerate)

frontend/
├── components/order/
│   ├── visitor-form.tsx     # group-based forms + payload fan-out + error remap
│   └── slot-groups.ts       # NEW — pure grouping helper (unit-testable)
├── lib/types.ts             # TicketOrderSlot gains package_id, package_unit
├── app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx  # updated assertions
└── lib/schemas.ts + lib/schemas.test.ts             # legacy checkoutSchema removal (cleanup)

SCHEMA.md                    # attendees.package_unit documented
```

**Structure Decision**: Existing web-application layout (`backend/` Go modular monolith + `frontend/` Next.js). No new packages or domains; one new frontend helper module colocated with the order components.

## Complexity Tracking

No constitution violations — table intentionally empty.

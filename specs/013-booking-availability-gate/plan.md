# Implementation Plan: Booking Availability Gate

**Branch**: `fix/ticket` | **Date**: 2026-08-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/013-booking-availability-gate/spec.md`

## Summary

Insert a server-confirmed availability step between the **Buy Ticket** press and the
**Terms & Conditions** dialog.

Today `TermsDialog` *is* the button: `DialogTrigger` renders "Buy Ticket", so the dialog
opens with no server involvement, and the first availability judgment the guest meets is
`expandItem` + `CheckAndDeductQuota` inside the booking transaction — fired by **Agree**,
after the whole document has been read.

The approach adds one unauthenticated, read-only, lock-free endpoint —
`POST /api/v1/ticket/availability` — that answers a well-formed question with **HTTP 200
and a decision body**, not an error envelope. The decision reuses booking's own
`expandItem` per line so existence, sales-window, and event-scoping verdicts are produced
by the identical code that will judge the eventual booking, then aggregates demand per
ticket type exactly as `aggregateDemand` does and compares it against remaining quota
read *without* `FOR UPDATE`.

On the client, `SelectionSummary` gains a real `<button>` that runs the check, and
`TermsDialog` becomes a controlled dialog opened only by a clean decision.

The check reserves nothing and locks nothing. Booking remains the sole authority.

## Technical Context

**Language/Version**: Go 1.24 (backend), TypeScript 5 / React 19 / Next.js 15 App Router
(frontend)

**Primary Dependencies**: Echo v4, pgx/v5, sqlc, shopspring/decimal (backend); TanStack
Query, Radix Dialog, Tailwind v4 (frontend); Playwright (e2e)

**Storage**: PostgreSQL — `ticket_types.quota` (REMAINING quota), `packages` +
`package_tickets`, `event_terms`. **No schema change; no migration; `SCHEMA.md`
untouched.**

**Testing**: `backend/scripts/test.sh ./...` (Go unit + database-backed), `vitest`
(frontend logic + components), `e2e/` Playwright against the real stack

**Target Platform**: Linux server (Docker Compose: PostgreSQL + Redis), evergreen browsers

**Project Type**: Web application — Go modular-monolith API + Next.js frontend

**Performance Goals**: The check adds one round trip on the path to the terms. SC-003
budgets 2 s at p95 for press → terms visible; the endpoint itself should land well under
200 ms p95, since it is a handful of indexed primary-key reads inside one snapshot.

**Constraints**: Takes **no row locks** — no `FOR UPDATE`, no `CheckAndDeductQuota`. It
must never be able to serialize concurrent buyers (Principle IV's reasoning). Reads live
PostgreSQL, never the Redis list cache (Principle VII: the cache is never authoritative
for inventory). Writes nothing, so invalidates nothing.

**Scale/Scope**: ~1 new endpoint, ~2 new backend files + 1 interface field, 3 frontend
component changes, 1 new frontend hook, 4 new e2e scenarios. Unauthenticated public
surface — must be rate limited.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Notes |
|-----------|---------|-------|
| **I. Modular Monolith** | PASS | All new backend logic lands in `internal/order/` (availability is an order-domain question). No new domain, no layer-first structure. |
| **II. Domain Isolation** | PASS | Event data is reached only through the existing `EventProvider` interface. The one interface change — adding `QuotaRemaining` to `TicketTypeInfo` — is a field on a DTO the order domain already declares for itself; `internal/event` is still never imported. `cmd/api/architecture_test.go` enforces this and must stay green. |
| **III. DTO Isolation** | PASS | New `AvailabilityRequest` / `AvailabilityDecision` DTOs in `internal/order/dto.go`. No `sqlc` struct reaches the wire. |
| **IV. Transactional Integrity & Idempotency** | PASS | The check creates nothing and deducts nothing, so idempotency is trivial. It runs in one read-only transaction purely for snapshot consistency and takes **no** row locks. Booking's transaction is not touched, not reordered, and not weakened — see FR-011 and the guard test in Phase 1. No network call inside any transaction. |
| **V. Payment Gateway Abstraction** | PASS | Payment is not involved. |
| **VI. Guest-First MVP Scope** | PASS | Unauthenticated guest surface, consistent with the rest of `/ticket/...`. No out-of-scope technology introduced. |
| **VII. Cache as a Disposable Read Accelerator** | PASS | The check reads live PostgreSQL and must **not** be served from the list cache — "a cached availability figure is a display value only; it MUST NOT gate, authorize, or short-circuit a sale." It performs no write, so it triggers no invalidation. Its behavior is identical with `E2E_CACHE_ENABLED=false`. |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes — the guest purchase
      journey, specifically the browse → select → **terms** → book segment.
      `e2e/specs/guest-purchase.spec.ts` must change in this same commit, and
      `e2e/support/journey.ts` alongside it: `agreeToTermsAndBook()` describes Buy Ticket
      as opening the terms gate directly, which stops being true.
- [x] **New user-visible flow → which new scenarios?** Four, in
      `e2e/specs/guest-purchase.spec.ts`: (1) a sold-out selection is refused at Buy
      Ticket with the terms dialog never opening; (2) a selection whose sale window
      closed is refused the same way; (3) an event with no authored terms is refused
      before an empty dialog can render; (4) a refused guest lowers the quantity and
      completes the purchase, proving the selection survived. The existing happy-path
      scenario is the fifth: it must pass unchanged.
- [x] **Bugfix in a covered flow → which scenario reproduces it, confirmed red first?**
      Scenario (1) is the reproduction. Against unfixed code the terms dialog opens on a
      sold-out selection and the failure only surfaces at Agree, so the assertion "the
      dialog is not visible after pressing Buy Ticket" fails. **It must be run and seen
      red before the implementation lands** — Principle VIII does not accept a regression
      test that was never observed failing.
- [x] **Behavior change under `E2E_CACHE_ENABLED=false`?** No. The endpoint never
      consults the cache in either mode. The suite must still be run both ways.

**Governance sync required in this change** (AGENTS.md: a constitution amendment must
update the other three — this is not an amendment, but `ARCHITECTURE.md` documents the
flow being altered):

- `ARCHITECTURE.md` line ~166 states the guest goes from selection straight to "Agree to
  the T&C in the booking dialog", and the sequence diagram at ~200–230 shows
  `POST /ticket/book` as the first server call of the booking step. Both are wrong after
  this change and MUST be updated in the same commit.
- `PRD.md` §1.5 "Public APIs" (line ~51) enumerates the guest endpoints and MUST gain
  `POST /api/v1/ticket/availability`, placed immediately before
  `POST /api/v1/ticket/book` — which is the order the guest now meets them in.
- `SCHEMA.md` — **no change**. No migration, no column.

**Post-Phase 1 re-check**: PASS, unchanged. The Phase 1 design introduced no new
dependency, no schema change, and no cache interaction. The one item that moved closer to
a principle is the `QuotaRemaining` field on `TicketTypeInfo` (Principle IV) — see
[research.md](research.md) D4 for why it is safe and
[contracts/availability.md](contracts/availability.md) for the guard test that keeps it so.

## Project Structure

### Documentation (this feature)

```text
specs/013-booking-availability-gate/
├── plan.md              # This file
├── research.md          # Phase 0 output — the seven decisions behind this design
├── data-model.md        # Phase 1 output — entities, no schema change
├── quickstart.md        # Phase 1 output — how to prove it works
├── contracts/
│   └── availability.md  # Phase 1 output — the endpoint contract
├── checklists/
│   └── requirements.md  # From /speckit-specify — 16/16
└── tasks.md             # Phase 2 — NOT created by /speckit-plan
```

### Source Code (repository root)

```text
backend/
├── cmd/api/
│   ├── main.go                       # MODIFY: mount the availability route on its
│   │                                 #   own per-IP limiter group
│   ├── adapters.go                   # MODIFY: pass ticket_types.quota through
│   │                                 #   eventProviderAdapter.TicketTypeForCheckout
│   └── architecture_test.go          # unchanged; must stay green
└── internal/order/
    ├── availability.go               # NEW: EvaluateAvailability — per-line verdicts,
    │                                 #   aggregated demand, lock-free quota comparison
    ├── availability_test.go          # NEW: unit + database-backed coverage
    ├── demand.go                     # unchanged — expandItem is reused verbatim
    ├── dto.go                        # MODIFY: AvailabilityRequest / AvailabilityDecision
    ├── event_provider.go             # MODIFY: TicketTypeInfo.QuotaRemaining
    ├── handler.go                    # MODIFY: RegisterAvailabilityRoute + handler
    └── service.go                    # unchanged — Book is deliberately not touched

frontend/
├── lib/
│   ├── queries.ts                    # MODIFY: useCheckAvailability mutation
│   └── types.ts                      # MODIFY: AvailabilityDecision, AvailabilityReason
└── components/booking/
    ├── selection-summary.tsx         # MODIFY: real button, runs the check, owns the
    │                                 #   refusal message and the dialog's open state
    ├── selection-summary.test.tsx    # MODIFY: the new gate's behavior
    ├── terms-dialog.tsx              # MODIFY: controlled (open/onOpenChange props);
    │                                 #   DialogTrigger removed
    └── terms-dialog.test.tsx         # MODIFY: mount controlled, not via the trigger

e2e/
├── support/journey.ts                # MODIFY: buyTicket() split from
│                                     #   agreeToTermsAndBook(); expectRefusedAtBuy()
└── specs/guest-purchase.spec.ts      # MODIFY: 4 new scenarios (see Principle VIII above)

ARCHITECTURE.md                       # MODIFY: flow chart ~166, sequence diagram ~200
PRD.md                                # MODIFY IF it narrates the terms-first flow
```

**Structure Decision**: The existing web-application split is kept exactly as it is. The
new backend logic belongs to `internal/order/` because availability is a question about a
prospective order, and because it must reuse `expandItem` — the function whose verdicts it
is required to match (FR-013). Putting it in `internal/event/` would either duplicate that
logic or force a cross-domain import, both of which Principle II forbids.

## Complexity Tracking

> No Constitution Check violations. This table is intentionally empty.

The one design choice that could look like a violation and is not: `TicketTypeInfo` gains
a `QuotaRemaining` field, which puts a remaining-quota figure within reach of the booking
transaction that must never read it (Principle IV / VII: quota decisions belong to the
atomic row-locked `UPDATE`). The field is safe because the underlying row is already read
by `GetTicketTypeByID`, which already selects `quota` — the adapter simply stops
discarding it, so no new read, query, or lock is introduced. The hazard is that a future
change *uses* it in `bookOnce` as a cheap pre-check and reintroduces the oversell that
`CheckAndDeductQuota` exists to prevent. That is addressed by test, not by comment: see
the guard in [contracts/availability.md](contracts/availability.md) §6.

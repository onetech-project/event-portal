# Implementation Plan: Booking Availability Gate

**Branch**: `fix/ticket` | **Date**: 2026-08-11, amended 2026-08-19 | **Spec**:
[spec.md](spec.md)

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

---

## Amendment — one general refusal message (2026-08-19)

The clarification session of 2026-08-19 inverts this feature's messaging rule. What was
delivered renders **one guest-facing sentence per offending line**, echoing the server's
own words; the amended spec renders **one fixed general message** for every availability
refusal, at both the check and at Agree.

### What changes

Everything below the wire. The server keeps producing exactly what it produces today —
every offending line, its stable string code, its sentence — and FR-006 reclassifies that
payload as **diagnostic**. The collapse happens in the client.

> **Someone was a bit faster!**
> One of your selected tickets is no longer available in this quantity. Please refresh
> the page and adjust your order.

Three refusals are **not** that message and keep their own wording: the event has no
authored terms (FR-012a), the check never reached the server (FR-012a), and a request the
selection page could not have produced (`VALIDATION_ERROR`, spec Assumptions). A fourth —
a throttled check — is a gap in the spec and is resolved in [research.md](research.md) D10.

### The one hard problem: the client cannot always tell which kind of refusal it has

This is the finding that shapes the whole amendment, and it is not visible from the spec.

The **check** answers `200` with `AvailabilityDecision.reasons[].code` — the *stable
string* code (`backend/internal/order/dto.go:110`). The client can separate all seven
codes exactly.

**Booking** answers with an error envelope, and `apperr.Body` is `{code int, message,
data}` (`backend/pkg/apperr/apperr.go:180-201`) — the stable string is **never
serialized**, and `backend/internal/order/handler_test.go:83-108` actively pins the body
to exactly those three fields. So the booking path sees only the numeric code, and
`apperr.Numeric` is lossy (`apperr.go:99-101`):

| Numeric | Booking-time meaning on this route | Classification |
|---------|-----------------------------------|----------------|
| `400002` | `INSUFFICIENT_QUOTA` — nothing else | availability (unambiguous) |
| `404001` | `TICKET_TYPE_NOT_FOUND` / `PACKAGE_NOT_FOUND` — verified nothing else reaches it from `Book` | availability |
| `409001` | `TERMS_MISSING` — nothing else | own wording, stays in-dialog |
| `429001` / `500000` | throttle / internal | own wording, stays in-dialog |
| **`400001`** | **both on-sale codes AND every `VALIDATION_ERROR`** | **ambiguous — decided in D8** |

`400001` is where FR-013a bites. It carries `TICKET_TYPE_NOT_ON_SALE` and
`PACKAGE_NOT_ON_SALE` — genuine FR-012 availability races the spec names explicitly — in
the same number as the `VALIDATION_ERROR` family the spec carves out. Nothing on the wire
separates them, and matching on the message prose is matching on the sentences this
amendment exists to stop rendering. [research.md](research.md) D8 decides it and states
the residual risk.

### Constitution re-check (amendment)

| Principle | Verdict | Notes |
|-----------|---------|-------|
| **I–III, V–VII** | PASS, untouched | No new domain, no DTO change, no gateway, no cache interaction. The wire contract is byte-identical. |
| **IV. Transactional Integrity** | PASS | Nothing in this amendment enters a transaction. `bookOnce` keeps every refusal path it has (FR-011); the only backend edit is a log line **after** the refusal is already an error, plus comment corrections. |
| **VIII. E2E Acceptance** | **ACTION REQUIRED** | See below — this is a behaviour change to a covered flow *and* a bugfix, so it needs both spec updates and a red-first scenario. |
| **IX. Throttling** | PASS, with a gap closed | D10 gives the throttled check its own wording instead of leaking echo's raw `"rate limit exceeded"` into the guest-facing alert. No throttle is added, removed, or retuned. |

**End-to-end acceptance (Principle VIII):**

- [x] **Touches a covered flow?** Yes — browse → select → terms → book. Four existing
      scenarios in `e2e/specs/guest-purchase.spec.ts` assert the *old* wording and go red
      on the correct implementation. They are rewritten, not extended:
      `:855`, `:899`, `:954` assert server sentences; `:928` (terms) is correct and gains
      the negative half of SC-005.
- [x] **Red-first bugfix scenario.** FR-013a has **zero coverage at any tier** — no e2e,
      no vitest, and `terms-dialog.test.tsx` never fails the `book` leg at all. The new
      scenario is US1 AS6: the check passes, another guest takes the last seat through the
      real API (`e2e/support/api.ts:353-369 bookAsAnotherGuest`), the guest presses Agree.
      Against current code it fails for **two** independent real reasons — the dialog stays
      open, and the text is `"Only fewer than N ticket(s) remain."`. **Assert the message
      before asserting the dialog is hidden**: a bare `toBeHidden()` can pass on the first
      poll and hand back a false green on exactly the bug under test.
- [x] **New coverage required.** FR-012's exact string (absent from the repo entirely
      today), FR-012a's two negatives, FR-013, FR-013a, and SC-005's "several offending
      lines collapse to one message". `deleteTicketType` already exists at
      `e2e/support/api.ts:200-205` for the item-deleted case SC-005 names.
- [x] **Both cache and throttle modes.** Unchanged — this amendment reads no cache and
      adds no throttle. Both runs must still pass.

**Governance sync: none required.** [PRD.md:58](../../PRD.md) and
[ARCHITECTURE.md:237-244](../../ARCHITECTURE.md) document only the mechanism — advisory
endpoint, `200 { available, reasons[] }`, refusals collected rather than failed-fast — and
every word of that survives. `SCHEMA.md` is untouched: no migration.

### Spec deltas this plan requires

Two gaps found in planning that the spec does not currently cover. Both are recorded here
rather than silently implemented:

1. **FR-012a enumerates two carve-outs; there are three.** A throttled check (HTTP 429) is
   neither an availability race nor either listed exception, and today it prints echo's
   middleware string `"rate limit exceeded"` into the alert named *"Why this selection
   cannot be bought"*. Meanwhile `terms-dialog.tsx:234-236` already words 429 properly —
   so the two points already tell different stories for one condition, which is what
   FR-013 forbids. D10 resolves it; FR-012a should gain the third bullet.
2. **FR-006 promises server-side records that do not exist.** Amended FR-006 justifies the
   per-line detail "so a refusal can be explained after the fact from server-side records",
   but the availability endpoint logs nothing — `availability.go` and `handler.go:86-98`
   contain no log call, and a `200` never reaches `pkg/httpx/error_handler.go:39-44` where
   booking refusals *are* logged at WARN with their stable code. D13 adds that log.

### Files this amendment touches

```text
frontend/
├── lib/
│   ├── availability.ts               # REWRITE: code-aware classifier replaces the
│   │                                 #   pass-through. Owns GENERAL_REFUSAL, the
│   │                                 #   FR-012 code set, and booking's numeric map.
│   │                                 #   Module docstring states the retired rule and
│   │                                 #   must be rewritten with it.
│   └── availability.test.ts          # REWRITE: :23-31/:33-40/:42-45 assert pass-through
│                                     #   wording; :60-63 asserts booking echoes the
│                                     #   server sentence — the opposite of FR-013.
├── components/booking/
│   ├── selection-summary.tsx         # MODIFY: refusals state collapses; ul/li becomes
│   │                                 #   AlertTitle+AlertDescription (:196-215); new
│   │                                 #   callback wired to TermsDialog (:229-236).
│   │                                 #   Keep aria-label "Why this selection cannot be
│   │                                 #   bought" — 4 e2e callers key on it.
│   ├── selection-summary.test.tsx    # MODIFY: refusal-rendering tests
│   ├── terms-dialog.tsx              # MODIFY: the book catch classifies its own
│   │                                 #   failure, closes THROUGH handleOpenChange, and
│   │                                 #   reports upward (see D9 — a parent-driven close
│   │                                 #   is not viable)
│   └── terms-dialog.test.tsx         # MODIFY: gains book-leg failure coverage, which
│                                     #   it has none of today
└── (no change) lib/types.ts, lib/api-client.ts, lib/queries.ts, lib/selection.ts

backend/
├── internal/order/availability.go    # MODIFY: WARN log of refused decisions (D13);
│                                     #   retired-rationale comments at :29-32, :96-98,
│                                     #   :142-145, :178-179
└── internal/order/dto.go             # MODIFY: comments only — :87-89 and :111-113 state
                                      #   the retired guest-facing motive. NOTE :105-109
                                      #   is CORRECT and must survive: the string-code
                                      #   rationale is what the new classifier depends on.

e2e/
├── support/journey.ts                # MODIFY: refusal helper; a sibling of
│                                     #   agreeToTermsAndBook (21 happy-path callers —
│                                     #   extend, never modify). Note visibleQuotaText
│                                     #   (:123-127) is unused AND broken — its locator
│                                     #   returns the whole list, not one row.
└── specs/guest-purchase.spec.ts      # MODIFY: 4 scenarios rewritten, 3+ added

specs/013-booking-availability-gate/  # contracts/availability.md, research.md,
                                      # data-model.md, quickstart.md, tasks.md all
                                      # encode the retired premise — see D14
```

**Not touched, deliberately**: `bookOnce`, `expandItem`, `aggregateDemand`,
`EvaluateAvailability`'s decision logic, `apperr`, every Go test asserting stable codes
(`availability_test.go`, `booking_test.go`, `handler_test.go`). Those assert the server's
diagnostic payload, which FR-006 preserves verbatim — weakening them would be the wrong
reading of this amendment.

---

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

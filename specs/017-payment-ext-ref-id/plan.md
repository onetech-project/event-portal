# Implementation Plan: Payment External Reference ID

**Branch**: `fix/payment` | **Date**: 2026-08-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/017-payment-ext-ref-id/spec.md`

## Summary

The gateway's external reference (`eri`) already reaches checkout on the session answer and
is thrown away. This feature writes it down, returns it, and shows it.

The design turns out to be mostly *filling in a hole the codebase already has*. Three
existing facts decide it:

1. **A payments row written during checkout is not new.** `MarkerSessionDuplicate` is
   already written to `payments` on the checkout path, from the composition root, via
   `payment.Service.ReleaseDuplicateSession`. This feature adds its mirror image:
   `SESSION_OPENED`, written when the session opens instead of when it is refused.
2. **The codebase already believes that row exists.** `payment.Service.SettlementForOrder`
   documents its behavior in terms of "the session-open row carries the instrument"
   ([service.go:798](../../backend/internal/payment/service.go#L798)) — a row nothing
   writes today. This feature makes that comment true.
3. **The e2e gateway stub already emits `eri` and already exposes `externalRefFor()`**
   ([gateway-stub.ts:166](../../e2e/support/gateway-stub.ts#L166)) — a helper with no
   callers, waiting for exactly this assertion.

So: `payments` gains an `ext_ref_id` column; one `SESSION_OPENED` row per order carries it;
the order domain reads it back through an interface it declares and the composition root
satisfies; `ext_ref_id` joins the checkout response on all three payload-bearing outcomes;
and the admin payment dialog gains one order-level line. No guest surface changes.

## Technical Context

**Language/Version**: Go 1.26.5 (backend), TypeScript 5 / React 19.2 / Next.js 16.2 (frontend)

**Primary Dependencies**: echo v4, pgx/v5, sqlc v1.31.1 (per-domain codegen), golang-migrate,
TanStack Query, Playwright 1.50

**Storage**: PostgreSQL — `payments` table gains one nullable column. Redis is untouched:
nothing in this feature is cached, and the orders-list invalidation that TX-P already
performs is unchanged.

**Testing**: `backend/scripts/test.sh` (Go unit + database-backed), `vitest` (frontend logic
+ components), Playwright acceptance suite in `e2e/`

**Target Platform**: Linux server (Go API + Next.js), modern browsers

**Project Type**: Web application — Go modular monolith + Next.js frontend + Playwright
acceptance suite

**Performance Goals**: Unchanged. One extra `INSERT` on the checkout path, outside every
transaction; one extra indexed-by-`order_id` `SELECT` on the two rebuilt checkout branches
only, both of which are already doing a full order read.

**Constraints**: The new write MUST sit outside every transaction (Principle IV and the
project's "no gateway call, no cache call inside an order-writing transaction" rule). The
order domain MUST NOT import the payment domain — `cmd/api/architecture_test.go` fails the
build if it does. Recording the reference MUST NOT be able to fail a checkout that the
gateway has already made payable.

**Scale/Scope**: 1 migration, 1 new column, 1 new sqlc query, 1 new marker constant, 2 new
service methods, 1 new consumer-declared interface + its composition-root adapter, 2 wire
fields (`ext_ref_id` on the checkout response and on the notification row), 1 frontend type
field, 1 frontend panel line. ~10 files touched, plus tests at all three tiers.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | How this feature complies | Verdict |
|-----------|---------------------------|---------|
| **I. Modular Monolith** | No new domain, no new service, no new deployable. The change lives inside `payment` and `order`, wired in `cmd/api`. | PASS |
| **II. Domain Isolation** | The order domain declares `PaymentRecords` (write + read) in its own package and never imports `internal/payment`; `cmd/api` adapts `payment.Service` onto it, exactly as it already does for `PaymentGateway`, `EventProvider`, and `payment.OrderProvider`. `TestNoDomainImportsAnotherDomain` is the enforcement and must stay green. | PASS |
| **III. DTO Isolation** | No sqlc struct crosses HTTP. `ext_ref_id` is added to the existing hand-written wire shapes (`order.CheckoutQRResponse`, `payment.NotificationResponse`); `paymentsql.Payment` stays inside the repository. | PASS |
| **IV. Transactional Integrity & Idempotency** | The `SESSION_OPENED` insert happens on the checkout path *between* TX-D and TX-P — inside no transaction, holding no quota row lock. A retried checkout short-circuits at the existing `payment_qr_string != ""` guard **before** any gateway call, so no second row is written. Nothing about quota, status, or the guarded `UPDATE` changes. | PASS |
| **V. Payment Gateway Abstraction** | `Gateway` keeps its two methods. The reference travels on the already-provider-neutral `PaymentSession.ProviderRef`; the order domain never learns it came from a field called `eri`. Recording it is the payment domain's own job, not the gateway interface's. | PASS |
| **VI. Guest-First MVP Scope** | Additive, guest-invisible, no out-of-scope capability introduced. No new Redis use. | PASS |
| **VII. Cache as Disposable Read Accelerator** | Nothing new is cached. `payments` has never been cached and is not now; the admin notification query already runs with `staleTime: 0`. The existing orders-scope invalidation in TX-P is untouched. Behavior is identical under `E2E_CACHE_ENABLED=false`. | PASS |
| **VIII. End-to-End Acceptance Coverage** | See the mandatory block below. | PASS |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes, two.
      **`e2e/specs/guest-purchase.spec.ts`** covers checkout, whose response gains a field —
      it must assert `ext_ref_id` arrives and matches the stub's `eri`.
      **`e2e/specs/admin-console.spec.ts`** covers the admin console, which gains the
      order-level reference line — it must assert an operator can read it off a real order's
      payment dialog. The dialog has no coverage at all today (no test in that file opens
      it), so this adds the first.
- [x] **New user-visible flow?** No new flow — one new value on two existing surfaces. Both
      get scenarios anyway, per the bullet above.
- [x] **Bugfix in a covered flow?** No. This is additive, so the "must be seen red first"
      rule does not apply; the new assertions nonetheless fail against unfixed code, because
      the field and the line do not exist yet.
- [x] **Behavior under `E2E_CACHE_ENABLED=false`?** No difference. Nothing here reads or
      writes the cache. The suite must pass in both modes, and the new scenarios are
      cache-independent by construction.

**Note on the suite's location.** On this branch the acceptance suite is at `e2e/`.
`AGENTS.md` describes it at `frontend/__test__/` — that relocation
([1e2ae2f](../../)) lives on `refractor/fe` and is not an ancestor of this branch. This plan
uses the paths that exist here. If this work later lands on top of that refactor, the spec
files move but nothing in this design does.

## Project Structure

### Documentation (this feature)

```text
specs/017-payment-ext-ref-id/
├── plan.md              # This file
├── research.md          # Phase 0 output — the three real decisions and what was rejected
├── data-model.md        # Phase 1 output — the column, the row, the read
├── quickstart.md        # Phase 1 output — how to prove it works
├── contracts/
│   └── api.md           # Phase 1 output — the two wire changes
├── checklists/
│   └── requirements.md  # From /speckit-specify
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
backend/
├── migrations/
│   └── 000015_payment_external_ref.{up,down}.sql   # NEW — payments.ext_ref_id
├── internal/
│   ├── payment/
│   │   ├── queries/payment.sql                     # ext_ref_id in insert/select; new ref lookup
│   │   ├── paymentsql/                             # regenerated by sqlc
│   │   ├── repository.go                           # PaymentLog/PaymentRecord gain ExtRefID; new lookup
│   │   ├── service.go                              # MarkerSessionOpened; RecordSessionOpened; ExternalRefForOrder
│   │   └── reconcile_dto.go                        # NotificationResponse gains ext_ref_id
│   └── order/
│       ├── event_provider.go                       # NEW consumer-declared PaymentRecords interface
│       ├── dto.go                                  # CheckoutQRResponse gains ExtRefID
│       └── service.go                              # record after stamp; qrResponseFor reads back
├── cmd/api/
│   ├── adapters.go                                 # orderPaymentRecordsAdapter over payment.Service
│   ├── main.go                                     # wire it into the order service
│   └── architecture_test.go                        # unchanged; must stay green
frontend/
├── lib/types.ts                                    # PaymentNotification + CheckoutQRResponse gain the field
└── components/admin/
    ├── order-payment-panel.tsx                     # one order-level line
    └── order-payment-panel.test.tsx                # its coverage
e2e/specs/
├── guest-purchase.spec.ts                          # checkout response carries the reference
└── admin-console.spec.ts                           # operator reads it off the payment dialog
api/openapi.yml                                     # CheckoutResponse + PaymentNotification schemas
SCHEMA.md                                           # payments table — same commit as the migration
```

**Structure Decision**: The existing web-application layout is used unchanged — Go modular
monolith under `backend/` (one package per domain, composition root in `cmd/api`), Next.js
App Router under `frontend/`, Playwright acceptance suite under `e2e/`. No new top-level
directory and no new package: every file above already exists except the migration pair.

## Post-Design Constitution Re-Check

Re-run after Phase 1. Every verdict above holds; three were sharpened by the design and one
regression surface was found and closed.

| Principle | What Phase 1 changed about the assessment | Verdict |
|-----------|-------------------------------------------|---------|
| **II. Domain Isolation** | The seam came out **read-only** — `PaymentRecords` has one method, `ExternalRefForOrder`. The write never crosses into the order domain at all: it happens in the composition root's payment adapter, which already calls `ReleaseDuplicateSession` on the sibling branch. Strictly less coupling than the pre-design sketch. | PASS |
| **IV. Transactional Integrity** | Confirmed against the real call sites: the `INSERT` lands in `CreateSession`, between TX-D and TX-P, inside no transaction. The retry guard at [service.go:400](../../backend/internal/order/service.go#L400) returns before any gateway call, so a retried checkout writes no second row. | PASS |
| **V. Gateway Abstraction** | Sharpened: the instrument written on the row (`"qris"`) is named by the **payment** domain, matching what `ReleaseDuplicateSession` already writes. The order domain never sees it and `Gateway` keeps its two methods. | PASS |
| **VII. Cache** | Confirmed by inspection: `payments` is not cached, the admin notification query runs `staleTime: 0`, and no invalidation is added or removed. Identical under `E2E_CACHE_ENABLED=false`. | PASS |
| **VIII. Acceptance Coverage** | Scenarios are named per file in [quickstart.md](quickstart.md) — including the negative assertion that the reference appears on **no** guest surface, which is a requirement here rather than a side effect. | PASS |

**Regression surface found in Phase 1 and closed.** Adding a row to an append-only log is read
by code that did not expect it. `SettlementForOrder` walks `payments` newest-first and takes
the first non-empty `payment_type` as the receipt's instrument. The `SESSION_OPENED` row is
the *oldest*, so a paid order still resolves its instrument from the notification rows —
unchanged — but this is asserted rather than assumed
([research.md](research.md) Decision 5, and a named test in [quickstart.md](quickstart.md)).

**Spec interaction worth the user's eye.** Spec FR-002 says an order that can be paid must not
exist with its reference unrecorded. A failed `RecordSessionOpened` is logged and swallowed,
so that state is reachable. The design reads FR-002 as binding *when* the reference is recorded
— at session open, never deferred — not as a demand that an audit-write failure abort a
checkout the gateway has already made payable. The fallback is FR-004's "no reference"
state, loudly logged. Recorded here because it is a judgment call, not a derivation.

## Complexity Tracking

No constitution violations. One design decision costs more than the obvious alternative and
is recorded here because the cheaper option was rejected on the user's explicit instruction
rather than on merit:

| Decision | Why it costs more | Cheaper alternative, and why it was not taken |
|----------|-------------------|-----------------------------------------------|
| Store the reference in `payments` rather than as an `orders` column | Needs a new row, a new column, a new sqlc query, a consumer-declared interface, and a composition-root adapter — because `orders` is the order domain's own table while `payments` belongs to the payment domain, and checkout must not reach across. An `orders.ext_ref_id` column would ride the `UPDATE` that already stamps `payment_qr_string`, need no interface, and be readable by `qrResponseFor` for free. | The user specified the payments table explicitly. The cost is real but modest, and the payments table is the better long-term home: it is where the payment story already lives, it timestamps the session-open event, and it makes `SettlementForOrder`'s existing "session-open row" comment true instead of aspirational. Not re-litigated. |

# Implementation Plan: Persistent Admin Session & In-App QRIS Payment Page

**Branch**: `004-session-qris-payment` | **Date**: 2026-08-01 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/004-session-qris-payment/spec.md`

## Summary

Two independent changes ship together.

**(1) Admin session.** The admin guard loses the session on every hard reload
because it reads `localStorage` through `useSyncExternalStore`'s *server* snapshot
during hydration and redirects in the same commit, before the real value is
available. The fix is a three-state guard (`loading` / `authenticated` /
`anonymous`) that resolves after mount and redirects only on `anonymous` —
client-side only, no backend or token-lifetime change.

**(2) Payment.** Replace the Snap redirect with an in-app order page. Checkout
opens a **Core API QRIS charge** instead of a hosted checkout session, storing the
QR payload and a server-owned deadline on the order. The guest lands on
`/orders/{order_number}`, which renders the order, a QR rendered on demand from the
stored payload, and a countdown. The page polls a cheap public status endpoint
every 3s and flips itself to a success state when the webhook lands; a rate-limited
"check payment status" button reconciles directly with the provider for the case
where the webhook has not arrived. Expiry is enforced by an in-process sweeper
funnelling into the same guarded transition the webhook already uses, so quota is
restored exactly once.

## Technical Context

**Language/Version**: Go 1.26 (backend, `go.mod` declares `go 1.26.5`), TypeScript
5 / React 19.2 / Next.js 16.2 App Router (frontend)

**Primary Dependencies**: Echo v4, sqlc, pgx v5, `shopspring/decimal`,
`skip2/go-qrcode` (already present — reused to render the QRIS PNG on demand);
TanStack Query v5 (already the frontend's data layer — supplies the polling, the
stop-on-final rule, and the offline/retry states). **No new dependency on either
side.**

**Storage**: PostgreSQL. One additive migration:
`orders.payment_qr_string TEXT` + `orders.payment_expires_at TIMESTAMPTZ` + a
partial index on `(payment_expires_at) WHERE status = 'PENDING'`. `SCHEMA.md` is
updated in the same change (constitution requirement). No object storage is
introduced — the QR image is generated per request from the stored payload.

**Testing**: Go `testing` + `testify` (gateway charge/status against
`httptest.Server` as `midtrans_test.go` already does; sweeper and reconciliation
idempotency, including concurrent expiry attempts); Vitest + Testing Library for
the session guard's three states, the clock-skew countdown, and the poll-to-paid
transition; manual end-to-end via the Midtrans sandbox QRIS simulator
([quickstart.md](./quickstart.md)).

**Target Platform**: Linux server (Docker Compose: API + Postgres), browsers —
mobile matters more than usual here, since the guest scans the QR with a second
device or pays on the same phone.

**Project Type**: Web application (backend + frontend)

**Performance Goals**: `GET /orders/:orderNumber` must stay a single indexed read
so a 3s poll per open payment page is negligible (~300 reads per 15-minute
window per guest); status flip visible ≤10s after the webhook (SC-004); quota
restored ≤1 minute after expiry via a 30s sweep (SC-007).

**Constraints**:
- The polled status endpoint MUST NOT make an outbound provider call or write —
  provider reconciliation lives only in the rate-limited POST.
- The provider charge MUST remain outside any database transaction, and the
  quota-restoring transition MUST remain a single guarded
  `UPDATE ... WHERE status = 'PENDING'` + `RestoreQuota` transaction, now shared by
  three callers (webhook, sweeper, reconciliation) — Constitution IV.
- `PAYMENT_EXPIRY` defaults to 15 minutes and MUST NOT be configured lower: the
  provider's expiry scheduler is unreliable below that (research.md §2).
- Core API uses `https://api.sandbox.midtrans.com`, a **different host** from the
  `app.sandbox.midtrans.com` the current Snap code targets.
- The public order view MUST NOT expose ticket codes, attendees, provider
  transaction ids, or any other order (FR-022).
- The countdown MUST be driven by `server_time` from the response, never the
  device clock (SC-005).
- `sqlc.yaml`'s `schema:` key MUST list both migration files, and Postgres only
  auto-runs `docker-entrypoint-initdb.d` on an empty volume — existing dev
  databases need the migration applied by hand.

**Scale/Scope**: MVP. ~4 user stories; 3 new public endpoints; 1 additive
migration; 2 changed internal interfaces (`payment.Gateway`,
`order.PaymentGateway`); 1 new frontend route (`/orders/[orderNumber]` is replaced,
not added) plus the admin guard rewrite.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design.*

| Principle | Check | Status |
|---|---|---|
| I. Modular Monolith | QRIS charge, status reconciliation, and the expiry sweeper live in `internal/payment`; the public order read lives in `internal/order`; the session fix is frontend-only. No layer-first grouping introduced. | PASS |
| II. Domain Isolation | The order read resolves ticket-type and event display names through the existing consumer-declared `EventLookup` interface (one new batched method), never a JOIN into `ticket_types`/`events`. The sweeper reaches orders through the existing `OrderProvider` interface (one new method, `DueForExpiry`). No domain imports another's repository. | PASS |
| III. DTO Isolation | `PublicOrderDetail`, `PaymentInstruction`, and the refresh response are hand-written DTOs in each domain's `dto.go`; no sqlc struct reaches a response. | PASS |
| IV. Transactional Integrity & Idempotency | Checkout keeps TX1 → charge outside any transaction → TX2 (now also stamping `payment_qr_string`/`payment_expires_at`), with the existing compensation on charge failure. Webhook, sweeper, and reconciliation share one guarded `UPDATE ... WHERE status='PENDING'` + quota-restore transaction, so quota moves exactly once no matter how many paths fire. Already-`PAID` orders short-circuit. Ticket/PDF/email work stays in the non-blocking goroutine. | PASS |
| V. Payment Gateway Abstraction | `Gateway` keeps its role and gains `FetchStatus`; `CreateTransaction` returns a `PaymentSession` instead of a bare URL. Both are provider-neutral shapes — the order domain still learns nothing Midtrans-specific, and a redirect-only gateway still fits (`RedirectURL`). | PASS |
| VI. Guest-First MVP Scope Discipline | Purchase stays account-free and gets *more* guest-first (no external hop). Nothing from the out-of-scope list is added: the sweeper is a plain in-process ticker, not a queue, scheduler, or Redis. Three endpoints beyond PRD §1.5's LOCKED list are added deliberately — see below. | PASS (with documented deviation) |

Constitution amendments needed: none. `SCHEMA.md` must be updated in the same
change as the migration, which the constitution requires and this plan schedules.

No violations — the Complexity Tracking table is not needed.

### PRD §1.5 API Deviation (deliberate and justified)

PRD.md §1.5's LOCKED public list assumes the guest leaves the site for the
provider's hosted page, so it has no guest-facing order endpoint. This feature
replaces that flow (FR-009), and the capability cannot be delivered without one.
Three endpoints are added — the minimum set:

| Endpoint | Why it cannot be folded into an existing endpoint |
|---|---|
| `GET /api/v1/orders/:orderNumber` | The order page's data source and the poll target for FR-016. No listed endpoint returns a guest-visible order. |
| `GET /api/v1/orders/:orderNumber/qris.png` | Keeps a 2-5 KB image out of a payload polled every 3s, and keeps the guest's browser off the provider's host. Independently cacheable. |
| `POST /api/v1/orders/:orderNumber/payment/refresh` | FR-017's control. It makes an outbound provider call and mutates order state — neither may happen on the endpoint polled every 3s, and a mutating GET is not safe or cacheable. |

All three are guest-facing by necessity, keyed only by order number, expose no
ticket codes or admin data, and add no admin surface. The same rationale is
recorded in [contracts/api.md](./contracts/api.md) and [research.md](./research.md).

### Post-design re-check (after Phase 1)

Re-evaluated against the generated data model and contracts: no principle moved
from PASS. Two design decisions were made *because* of the gates rather than in
spite of them — rendering the QR on demand from `qr_string` (no object storage) and
routing all three expiry paths through the single guarded transition (Principle IV)
rather than giving the sweeper its own update.

## Project Structure

### Documentation (this feature)

```text
specs/004-session-qris-payment/
├── plan.md              # this file
├── research.md          # Phase 0 — 10 decisions, provider facts verified against docs
├── data-model.md        # Phase 1 — schema delta, lifecycle, read-model rules
├── quickstart.md        # Phase 1 — 5 end-to-end validation scenarios
├── contracts/
│   └── api.md           # Phase 1 — 3 new endpoints + Gateway interface change
├── checklists/
│   └── requirements.md  # spec quality checklist
└── tasks.md             # /speckit-tasks output (not created here)
```

### Source Code (repository root)

```text
backend/
├── migrations/
│   └── 0002_payment_qris.sql            # + payment_qr_string, payment_expires_at,
│                                         #   partial index for the sweeper
├── sqlc.yaml                            # schema: now lists both migration files
├── internal/
│   ├── payment/
│   │   ├── gateway.go                   # Gateway: CreateTransaction -> PaymentSession,
│   │   │                                 #   + FetchStatus
│   │   ├── midtrans.go                  # Core API /v2/charge (payment_type: qris,
│   │   │                                 #   custom_expiry) + GET /v2/{order_id}/status
│   │   ├── dto.go                       # PaymentSession
│   │   ├── service.go                   # RefreshStatus(orderNumber) reusing applyOutcome;
│   │   │                                 #   ExpireDueOrders(now) sweep; shared apply path
│   │   ├── sweeper.go                   # in-process ticker (PAYMENT_SWEEP_INTERVAL)
│   │   ├── handler.go                   # POST /orders/:orderNumber/payment/refresh
│   │   └── queries/payment.sql          # unchanged
│   ├── order/
│   │   ├── event_provider.go            # PaymentGateway mirrors the new session shape
│   │   ├── admin_service.go             # EventLookup gains a batched display lookup
│   │   ├── service.go                   # TX2 stamps qr_string + expires_at
│   │   ├── public_service.go            # OrderByNumber read model (new)
│   │   ├── handler.go                   # GET /orders/:orderNumber,
│   │   │                                 #   GET /orders/:orderNumber/qris.png
│   │   ├── dto.go                       # PublicOrderDetail, PaymentInstruction
│   │   ├── repository.go                # + payment detail write, public read,
│   │   │                                 #   DueForExpiry
│   │   └── queries/order.sql            # matching sqlc queries
│   ├── event/                           # implements the new EventLookup method
│   └── ...
├── pkg/config/config.go                 # PAYMENT_EXPIRY, PAYMENT_SWEEP_INTERVAL,
│                                         #   Core API base URL
└── cmd/api/main.go                      # register new routes (refresh behind
                                          #   httpx.RateLimitPerIP), start/stop the sweeper

frontend/
├── app/
│   ├── admin/
│   │   ├── layout.tsx                   # three-state guard: no redirect while loading
│   │   └── login/page.tsx               # honours ?next= and ?reason=expired
│   ├── checkout/page.tsx                # route in-app to /orders/[orderNumber];
│   │                                     #   drop window.location = payment_url
│   └── orders/[orderNumber]/page.tsx    # REPLACED: order detail + QRIS + countdown
│                                         #   + check-status + auto-update
├── components/
│   ├── order/                           # qris-panel, expiry-countdown,
│   │                                     #   payment-status-card (new)
│   └── admin/admin-nav.tsx              # sign-out notifies this tab too
└── lib/
    ├── auth.ts                          # session state + in-page change notification
    ├── queries.ts                       # useOrderDetail (poll 3s, stop when final),
    │                                     #   useRefreshPaymentStatus
    └── types.ts                          # PublicOrderDetail types
```

**Structure Decision**: The established `backend` + `frontend` split from specs
001-003 is kept. Domain placement follows the ownership already in the codebase:
`internal/payment` owns everything that touches the gateway or moves an order out
of `PENDING` (charge, reconciliation, sweep) because that is where `Gateway`,
`MapProviderStatus`, and `applyOutcome` already live; `internal/order` owns the
public read because it owns `orders`/`order_items`. Cross-domain reads keep going
through the consumer-declared interfaces the codebase already established —
`EventLookup` (order → event, gains one batched method for ticket-type and event
display names) and `OrderProvider` (payment → order, gains `DueForExpiry`) — so no
new coupling direction is introduced. The expiry sweeper sits in `payment` rather
than `order` specifically so it can reuse `applyOutcome` and inherit its
idempotency, instead of a second code path that would need its own correctness
argument.

## Complexity Tracking

*No constitution violations — table intentionally omitted.*

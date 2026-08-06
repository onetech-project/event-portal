# Implementation Plan: Event-Scoped Guest Navigation

**Branch**: `007-event-scoped-routes` | **Date**: 2026-08-04 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/007-event-scoped-routes/spec.md`

> **Revision note**: this plan was regenerated after the 2026-08-04 `/speckit-clarify`
> session re-scoped the feature. Event-scoped ticket screens, the ticket-code resolver,
> the legacy order forwarder, and the `event_slug` API field are **gone**. A progress rail
> in the layout, a confirmation screen, and a public resend endpoint are **in**.

## Summary

The purchase journey — **events → choose ticket → checkout → order → done** — moves
entirely under `/events/:slug/…`, and a new `events/[slug]/layout.tsx` supplies shared
framing: one event fetch, the live start-date countdown, and the four-step progress rail.
The journey gains a real ending: a confirmation screen at
`/events/:slug/orders/:orderNumber/done` that any settled order forwards to.

Four things make this more than a file move:

1. **Framing must survive navigation.** App Router keeps a layout mounted while only the
   leaf changes, so hoisting the countdown into the layout gives FR-001/FR-002 structurally.
   The event is fetched by the layout via the existing `useEventBySlug`, sharing TanStack
   key `["events", slug]` with the detail page — dedup, not a second request (FR-006).
2. **The progress rail is currently broken.** `BookingSteps` renders in exactly one place,
   `events/[slug]/page.tsx:57`, hardcoded to `current="Booking"`. Stages 2–4 have never
   been shown to any guest. Moving it into the frame and deriving the stage from the
   pathname (FR-004, FR-005) fixes a live defect, not just a layout preference.
3. **The confirmation screen needs a public resend endpoint.** The approved design has a
   "Resend Email" button, but the only resend that exists is admin-authenticated and keyed
   by order **UUID**. A guest knows their order *number*. This is the one backend change.
4. **The in-flight move is half-done.** `events/[slug]/checkout/page.tsx` exists but still
   reads its slug from `?slug=`, and `encodeSelection()` still emits `/checkout?slug=…` —
   a path with no page. The selection link is broken right now.

## Technical Context

**Language/Version**: TypeScript 5 (frontend), Go 1.24+ (backend)

**Primary Dependencies**: Next.js 16.2.12 (App Router), React 19.2.4, TanStack Query
5.101, Tailwind 4, Base UI; Echo v4, `sqlc` (pgx/v5). **No new dependency** — the rate
limiter is Echo's own `middleware.RateLimiterWithConfig`, already vendored.

**Storage**: PostgreSQL. No schema change, no migration. `SCHEMA.md` untouched.

**Testing**: Vitest 4 + Testing Library + happy-dom (frontend, colocated `*.test.tsx`);
`go test` across the existing handler/service/repository layers plus
`backend/cmd/api/architecture_test.go`.

**Target Platform**: Web (`output: "standalone"` Next.js server + Go API).

**Project Type**: Web application — `frontend/` (Next.js) + `backend/` (Go modular monolith).

**Performance Goals**: Countdown ticks at 1 Hz and must not restart or blank when the
guest moves between nested routes (SC-001). Event fetches stay at one per slug per
revalidation across the whole journey.

**Constraints**: Guest event/quota reads stay uncached (`cache: "no-store"`,
`staleTime: 0`); no authentication introduced; the resend endpoint is unauthenticated and
must therefore be rate-limited and incapable of accepting a caller-supplied destination.

**Scale/Scope**: ~10 frontend files, ~5 backend files across `internal/notification/` and
`cmd/api/`. One new API endpoint. Zero migrations.

### Resolved policy decisions

| Decision | Value | Source |
|---|---|---|
| Resend rate limit | 1 per order per 60 seconds | FR-025; see [research.md](./research.md) R-006 |
| Resend response | `202 Accepted`, generic body, identical for unknown orders | FR-026 |
| Confirmation route | `/events/:slug/orders/:orderNumber/done` | FR-016 |
| Rail stage source | Derived from `usePathname()` | FR-005 |

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|---|---|---|
| **I. Modular Monolith** | PASS | Backend edits are confined to `internal/notification/` (a permitted domain) plus the composition root `cmd/api/`. No new domain, nothing added to `pkg/`. |
| **II. Domain Isolation** | PASS | The notification domain needs to resolve an order number to an ID. It declares the need on its own `OrderProvider` interface (`internal/notification/service.go:28`) and the composition root satisfies it with an adapter over `order.Repository.GetOrderByNumber` — the exact pattern `notificationOrderAdapter` already uses for `OrderForDelivery`. No cross-domain import, no cross-domain JOIN. |
| **III. DTO Isolation** | PASS | The resend response reuses/extends the notification domain's own `ResendResponse` in its `dto.go`. No `sqlc` struct reaches the wire. |
| **IV. Transactional Integrity & Idempotency** | PASS | Checkout, webhooks, and quota are untouched. Resend performs no quota or status mutation; it re-renders and re-sends an email for an already-`PAID` order, and `SendTicketEmail` already guards on status. Email dispatch stays off the request's critical path per existing behaviour. |
| **V. Payment Gateway Abstraction** | N/A | `internal/payment` untouched. |
| **VI. Guest-First MVP Scope** | PASS | Purely navigational plus a guest-recovery action. No account, auth, or session introduced — the resend is deliberately unauthenticated, matching the guest-first rule. Nothing on the out-of-scope list is touched. |
| **Tech stack — live inventory not cached** | PASS | The layout consumes the same `["events", slug]` key under the app-wide `staleTime: 0` and `cache: "no-store"`. Hoisting the fetch dedupes concurrent requests; it does not extend cache lifetime. |
| **Schema is source of truth** | PASS | No migration, no `SCHEMA.md` edit, no new query — `GetOrderByNumber` already exists at `internal/order/repository.go:293`. |

**Gate result: PASS.** No violations, so Complexity Tracking is omitted.

Two points recorded rather than flagged as violations:

- **FR-006 vs. live inventory.** "Resolve the event once for the whole journey" reads like
  caching. It is not — see [research.md](./research.md) R-002.
- **In-memory rate limiting is per-instance.** Principle VI forbids Redis and orchestration
  platforms, so a shared rate-limit store is not available and not wanted. The MVP runs a
  single API instance under Docker Compose, where an in-memory store is exactly correct.
  Recorded so a future multi-instance deployment knows this is a deliberate MVP bound.

## Project Structure

### Documentation (this feature)

```text
specs/007-event-scoped-routes/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── resend-email.md  #   NEW endpoint contract
│   └── routes.md        #   Guest URL contract
├── checklists/
│   └── requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
frontend/
├── app/
│   ├── page.tsx                                          # unchanged
│   └── (public)/
│       ├── events/
│       │   ├── page.tsx                                  # unchanged
│       │   └── [slug]/
│       │       ├── layout.tsx                            # NEW  — awaits params, renders EventFrame
│       │       ├── page.tsx                              # EDIT — drop SalesCountdown + BookingSteps
│       │       ├── checkout/page.tsx                     # EDIT — slug from segment; land on scoped order
│       │       └── orders/[orderNumber]/
│       │           ├── page.tsx                          # EDIT — ownership guard; forward when settled
│       │           └── done/page.tsx                     # NEW  — confirmation screen
│       └── tickets/                                      # UNCHANGED — root lookup stays as-is
│           ├── page.tsx
│           └── [code]/page.tsx
├── components/
│   ├── event/event-frame.tsx                             # NEW  — fetch, gate, countdown, rail
│   ├── booking/booking-steps.tsx                         # EDIT — checkmark for completed stages
│   ├── booking/sales-countdown.tsx                       # EDIT — label per FR-017
│   ├── order/expiry-countdown.tsx                         # EDIT — label per FR-017
│   └── order/order-confirmation.tsx                      # NEW  — receipt card + resend + back home
└── lib/
    ├── selection.ts                                      # EDIT — encodeSelection → /events/:slug/checkout
    ├── booking-stage.ts                                  # NEW  — pathname → stage (FR-005)
    ├── queries.ts                                        # EDIT — usePublicResendEmail mutation
    └── types.ts                                          # EDIT — PublicResendResponse

backend/
├── internal/notification/
│   ├── handler.go                                        # EDIT — RegisterPublicRoutes + resendPublic
│   ├── service.go                                        # EDIT — OrderProvider gains OrderIDByNumber
│   ├── dto.go                                            # EDIT — public resend response
│   └── handler_test.go                                   # EDIT — public endpoint cases
└── cmd/api/
    ├── adapters.go                                       # EDIT — adapter implements OrderIDByNumber
    └── main.go                                           # EDIT — mount public route + rate limiter
```

**Structure Decision**: The existing `frontend/` + `backend/` split is kept. Frontend work
stays inside the `(public)` route group so site chrome is unchanged. Backend work stays
inside `internal/notification/` plus the composition root. No new top-level directory, and
**no changes at all** under `internal/ticket/` — that domain is untouched by the re-scoped
feature.

### Where each requirement lands

| Requirement | Implementation site |
|---|---|
| FR-001, FR-006, FR-007, FR-008 | `events/[slug]/layout.tsx` + `components/event/event-frame.tsx` |
| FR-002, FR-003 | `components/booking/sales-countdown.tsx`, rendered by the frame |
| FR-004 | `components/booking/booking-steps.tsx`, rendered by the frame |
| FR-005 | `lib/booking-stage.ts` — pathname → stage |
| FR-009 | `events/[slug]/page.tsx` — remove its own countdown and rail |
| FR-010 | `events/[slug]/checkout/page.tsx` reads `params`; `lib/selection.ts` |
| FR-011, FR-012 | Checkout redirect target |
| FR-013 | Order page — compare `data.event.slug` to the segment, fail closed |
| FR-014 | Link audit: order page, `payment-status-card.tsx`, confirmation screen |
| FR-015 | Regression coverage — `useOrderDetail` polling and refresh untouched |
| FR-016, FR-019, FR-020, FR-022 | `orders/[orderNumber]/done/page.tsx` + `order-confirmation.tsx` |
| FR-017 | `sales-countdown.tsx` + `expiry-countdown.tsx` labels |
| FR-018 | Order page — `router.replace` to `./done` on settled status |
| FR-021 | `order-confirmation.tsx` + `usePublicResendEmail` |
| FR-023, FR-024, FR-026 | `internal/notification/handler.go`, `service.go`, `cmd/api/adapters.go` |
| FR-025 | `cmd/api/main.go` — Echo rate limiter keyed on the order number |
| FR-027, FR-028 | Nothing to build — `tickets/` untouched, no order forwarder created |

## Post-Design Constitution Re-Check

Re-evaluated after [data-model.md](./data-model.md) and [contracts/](./contracts/) were
written. Verdict unchanged: **PASS**.

The design added no domain, table, migration, or dependency. The one new endpoint lives in
the domain that already owns email delivery, reaches the order domain only through an
interface that domain's composition root satisfies, and returns the notification domain's
own DTO. `internal/ticket/` — modified by the previous revision of this plan — is now
untouched entirely.

The re-scope made the feature strictly smaller in domain surface (one domain instead of
two) while adding one endpoint. That trade is favourable under Principle II.

## Complexity Tracking

Not applicable — the Constitution Check passed with no violations.

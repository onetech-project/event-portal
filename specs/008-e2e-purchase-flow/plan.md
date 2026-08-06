# Implementation Plan: End-to-End Guest Purchase Flow

**Branch**: `feat/ticket` (spec dir `008-e2e-purchase-flow`) | **Date**: 2026-08-05 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/008-e2e-purchase-flow/spec.md`

## Summary

Restructure the guest journey from "select → forms → one-shot checkout (order +
gateway in one request)" into the JIVE design's five-stage journey:

1. **Homepage** = event grid + ticket-verification aside (replaces two-button chooser).
2. **Event detail page** (new) — CMS-authored content (WYSIWYG description,
   activities, guest stars, guidelines) served from new event-domain tables;
   ticket selection moves to its own route.
3. **Booking on T&C agreement** — agreeing to the (now CMS-authored, per-event)
   Terms creates the order immediately (`POST /ticket/book` + agreement
   recorded via `POST /ticket/terms-condition/:order_id`, same Agree click):
   `PENDING`, no payment fields, quota deducted atomically, attendee slots
   created empty, 1-hour hold via existing `payment_expires_at` + sweeper.
4. **Order page** — visitor forms (name/email/phone/DOB/gender per slot)
   submitted together with "Continue to payment"
   (`POST /ticket/checkout/:order_id`, Option B — no separate save), which
   calls the gateway (outside any
   transaction, per Constitution IV), sets a 14-minute server deadline, renders
   the QR in-app; a 7-minute QR refresh re-creates the gateway transaction; SSE
   pushes payment status with polling as fallback. Event countdown stays via
   the shared `events/[slug]` layout.
5. **Done page + email** — existing PAID fulfillment (PDF e-ticket + receipt
   email + resend) restyled to the JIVE email design.

The existing sweeper, webhook idempotency, quota compensation, package
expansion, and rate-limiting machinery are reused unchanged wherever possible.

## Technical Context

**Language/Version**: Go 1.24+ (backend), TypeScript / Next.js 16.2 App Router, React 19 (frontend)

**Primary Dependencies**: Echo v4, pgx v5, sqlc, shopspring/decimal, go-qrcode, gofpdf (existing); TanStack Query v5, React Hook Form + Zod, Tailwind v4, Base UI + shadcn (existing); **new**: TipTap (admin WYSIWYG), bluemonday (server-side HTML sanitization)

**Storage**: PostgreSQL (docker-compose), migrations via `migrations/` + sqlc generation; `SCHEMA.md` is the source of truth and is updated in the same change

**Testing**: `go test` (handler/service/repo + architecture tests), Vitest + Testing Library (frontend)

**Target Platform**: Linux server (Docker Compose), modern browsers

**Project Type**: Web application — `backend/` (modular monolith) + `frontend/` (Next.js)

**Performance Goals**: Booking TX holds quota row locks only for local SQL (no network calls in TX); payment status visible ≤5s after gateway confirmation (SSE push, 3s polling fallback); expiry sweep ≤1min lag (existing 30s interval)

**Constraints**: Provider QRIS floor is 15 minutes (`minPaymentExpiry`) — the 14-minute window is a **server-owned** deadline (`payment_expires_at`), gateway expiry stays ≥15m; no object storage (icons are named references, banner/images are URL strings); guest surface unauthenticated

**Scale/Scope**: MVP scale (single instance, one live event at a time); ~9 new/changed guest screens, ~4 admin CMS surfaces, 1 migration, ~10 new/changed endpoints

**Important frontend caveat**: this Next.js version has breaking changes vs
training data (`frontend/AGENTS.md`) — read `node_modules/next/dist/docs/`
guides before writing page/layout code (e.g. `params` is a Promise).

## Constitution Check

*GATE: evaluated against constitution v1.1.0 — pre-Phase-0 and re-checked post-Phase-1.*

| # | Principle | Status | Notes |
|---|-----------|--------|-------|
| I | Modular Monolith | ✅ PASS | New content/terms code lives in `internal/event`; booking split stays in `internal/order`; SSE in `internal/payment`; no new domains. |
| II | Domain Isolation | ✅ PASS | Order reads terms via extended `EventProvider` interface (injected), never imports event repo. Payment keeps `QuotaRestorer`/`TicketIssuer`/`TicketDeliverer` adapters. |
| III | DTO Isolation | ✅ PASS | New DTOs in each domain's `dto.go`; sqlc structs never serialized. |
| IV | Transactional Integrity & Idempotency | ✅ PASS | Booking TX = quota deduction + `orders` + `order_items` + `attendees` slots, no network call inside. Gateway `CreateTransaction` moves even further out — to the later "continue to payment" request, after TX committed. Webhook idempotency and quota-restore paths unchanged. |
| V | Payment Gateway Abstraction | ✅ PASS | QR refresh added to the `Gateway` interface (re-create transaction under a suffixed reference), Midtrans-specific logic stays in `internal/payment`. |
| VI | Guest-First MVP Scope | ✅ PASS | No accounts; `Visitor_Point`/loyalty excluded; no object storage (activity/guideline icons are named icon keys, not uploads); refunds not implemented (post-expiry confirmations flagged for manual reconciliation). |
| — | Quota semantics | ✅ PASS | `quota` remains the live remaining counter; booking decrements, expiry/cancel restores. |
| — | Tech stack | ⚠️ JUSTIFIED | Two new dependencies (TipTap frontend, bluemonday backend) — required by the WYSIWYG requirement (FR-004); see Complexity Tracking. |

**Post-Phase-1 re-check**: design artifacts introduce no new violations — the
two-step order flow strengthens Principle IV (gateway call moved out of the
checkout request entirely), and all new tables are owned by exactly one domain.

## Project Structure

### Documentation (this feature)

```text
specs/008-e2e-purchase-flow/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── api.md           # Endpoint contracts (guest + admin CMS)
│   └── booking-flow.md  # Two-phase order state machine & TX shapes
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
backend/
├── migrations/
│   └── 000005_event_content_and_booking.{up,down}.sql   # NEW
├── internal/
│   ├── event/            # + content blocks (activities/guest stars/guidelines),
│   │                     #   terms CRUD, sanitization, public detail DTO
│   ├── order/            # checkout → Book (no gateway) + RecordAgreement +
│   │                     #   Checkout (forms + payment, Option B); guest paths
│   │                     #   remounted on /event + /ticket namespaces
│   ├── payment/          # + SSE stream endpoint, QR re-issue (7-min refresh),
│   │                     #   sweeper unchanged
│   └── notification/     # email template restyle (receipt + e-ticket)
├── pkg/config/           # + PaymentWindow (14m), QRRefreshAfter (7m), BookingHold (1h)
├── pkg/httpx + pkg/apperr # envelope writer {code,message,data} + numeric-code
│                          # error handler; DTOs retagged snake_case (Option A)
└── cmd/api/main.go       # route wiring for new endpoints

frontend/
├── app/
│   ├── page.tsx                                  # homepage → event grid + verify aside
│   └── (public)/events/[slug]/
│       ├── page.tsx                              # NOW event detail (CMS content)
│       ├── tickets/page.tsx                      # NEW ticket selection (moved)
│       ├── layout.tsx                            # shared countdown/rail (kept on payment)
│       └── orders/[orderNumber]/                 # order forms → payment → done (existing routes)
├── app/(admin)/admin/events/[id]/                # + WYSIWYG description, terms,
│                                                 #   activities/guest stars/guidelines editors
├── components/booking/terms-dialog.tsx           # fetches per-event terms; Agree → POST book
├── components/order/                             # visitor form components, SSE hook, QR refresh
└── lib/                                          # api client additions, queries, terms removal
```

**Structure Decision**: Existing two-project web layout (`backend/` modular
monolith + `frontend/` Next.js App Router). No new top-level projects. Route
restructure: `events/[slug]` becomes the detail page; ticket selection moves to
`events/[slug]/tickets`; order/checkout/done routes stay where feature 007 put
them.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| New frontend dep: TipTap editor | FR-004 requires WYSIWYG authoring for event description + T&C | A plain `<textarea>` with raw HTML fails the "non-technical admin" requirement; building a custom editor is far more code than adopting the standard headless one |
| New backend dep: bluemonday | Admin-authored HTML must be sanitized before storage/serving (FR-004, SC-007) | Regex/hand-rolled sanitization is a known-vulnerable anti-pattern; client-only sanitization is bypassable |

# Tasks: End-to-End Guest Purchase Flow

**Input**: Design documents from `/specs/008-e2e-purchase-flow/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/api.md, contracts/booking-flow.md, quickstart.md

**Tests**: Included — the codebase convention is tests alongside every handler/service (`*_test.go`, `*.test.tsx`); new code follows it.

**Organization**: Grouped by user story. US1+US2 are the P1 transactional core; US3–US6 are P2 increments.

**Frontend caveat**: this Next.js version diverges from training data — read `frontend/node_modules/next/dist/docs/` before page/layout work (`frontend/AGENTS.md`). Figma frames listed in spec.md must be fetched via the `figma:figma-design-to-code` skill at implementation time.

## Format: `[ID] [P?] [Story] Description`

## Phase 1: Setup

**Purpose**: Dependencies and configuration every later phase needs

- [X] T001 Add config values `BOOKING_HOLD` (1h), `PAYMENT_WINDOW` (14m), `QR_REFRESH_AFTER` (7m) with validation invariants `PAYMENT_WINDOW < PAYMENT_EXPIRY` and `QR_REFRESH_AFTER < PAYMENT_WINDOW` in backend/pkg/config/config.go (+ backend/pkg/config/config_test.go, backend/.env.example)
- [X] T002 [P] Add `github.com/microcosm-cc/bluemonday` to backend/go.mod and create sanitizer helper (UGC policy + class attr for alignment) in backend/pkg/sanitize/sanitize.go with backend/pkg/sanitize/sanitize_test.go
- [X] T003 [P] Add TipTap packages (`@tiptap/react`, `@tiptap/starter-kit`, `@tiptap/extension-link`) to frontend/package.json — **MIT open-source core only, no Tiptap Cloud/Pro extensions** (clarification 2026-08-05)

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Schema, response envelope, and route namespaces that every story sits on

**⚠️ CRITICAL**: No user story work until this phase completes

### Schema

- [X] T004 Write migration backend/migrations/000005_event_content_and_booking.up.sql + .down.sql per data-model.md: create `event_terms`, `event_activities`, `event_guest_stars`, `event_guidelines` (+ event_id indexes); alter `orders` (add `terms_agreed_at`, `event_terms_id` FK; make `buyer_name`/`buyer_email`/`buyer_phone` nullable); alter `attendees` (make `name`/`email` nullable; add `phone`, `dob`, `gender` CHECK IN ('MALE','FEMALE')); backfill one `event_terms` row per existing event from the static copy in frontend/lib/terms.ts
- [X] T005 Sync SCHEMA.md with every 000005 change (constitution: same-change rule)
- [X] T006 Add new tables/columns to backend/sqlc.yaml query surface, write sqlc queries for terms + content blocks in backend/internal/eventsql/queries/ and order/attendee updates in backend/internal/ordersql/queries/, run `sqlc generate` (depends T004)

### Response envelope (clarification: Option A — whole API)

- [X] T007 Create envelope writer `Respond(c, status, data)` emitting `{code, message, data}` (success `200000`) in backend/pkg/httpx/envelope.go with backend/pkg/httpx/envelope_test.go
- [X] T008 Add numeric code registry (contracts/api.md table: 400001…502001) to backend/pkg/apperr and rewrite `httpx.ErrorHandler` to emit the envelope with numeric codes in backend/pkg/httpx/error_handler.go (update backend/pkg/httpx tests)
- [X] T009 Retag every response DTO json tag to snake_case and route all handler returns through the envelope writer across backend/internal/{admin,event,order,payment,ticket,notification}/dto.go + admin_dto.go + handlers; update their `*_test.go` expectations
- [X] T010 Migrate frontend API client to envelope unwrap (`data` extraction, numeric `code` error mapping) + snake_case types in frontend/lib/ (api/queries/types modules); update admin pages under frontend/app/(admin)/admin/ to the new shapes

### Route namespaces (clarification: literal endpoint list)

- [X] T011 Remount guest routes in backend/cmd/api/main.go: `GET /event`, `GET /event/:id` (content-only DTO with `has_terms`; tickets/packages removed), `GET /ticket/:event_id`, `GET /packages/:event_id` (new split handlers in backend/internal/event/handler.go), keep `GET /tickets/:code`; remove old `/events*`, `/checkout`, `/orders/*` guest paths; add routing-precedence test in backend/cmd/api/architecture_test.go
- [X] T012 Point guest frontend reads at the split endpoints: event queries in frontend/lib/queries.ts (`no-store` for `GET /ticket/:event_id` + `GET /packages/:event_id`), selection page frontend/app/(public)/events/[slug]/page.tsx builds items from the two queries

**Checkpoint**: DB migrated, envelope everywhere, new namespaces live — stories can start

---

## Phase 3: User Story 1 — Order Persists on T&C Agreement, 1-Hour Hold, Expiry (Priority: P1) 🎯 MVP

**Goal**: Agree on T&C → real `PENDING` order, quota locked, agreement recorded, 1-hour hold; sweeper expiry restores quota

**Independent Test**: quickstart.md Scenarios 3, 4, 8

### Backend

- [X] T013 [P] [US1] Terms read surface: repository + service methods (current terms by event, `has_terms`) in backend/internal/event/repository.go + service.go, public handler `GET /ticket/terms-condition/:event_id` in backend/internal/event/handler.go (+ tests)
- [X] T014 [P] [US1] Extend order-side `EventProvider` interface with terms lookup (id + existence) in backend/internal/order/event_provider.go and wire the adapter in backend/cmd/api/adapters.go
- [X] T015 [US1] `Book` service method in backend/internal/order/service.go: reuse reserve TX (contracts/booking-flow.md §1) minus gateway/buyer/details — empty attendee slots, `payment_expires_at = now()+BOOKING_HOLD`; refuse 409001 when event has no terms; keep collision retry + `INSUFFICIENT_QUOTA`; tests in backend/internal/order/service_test.go incl. concurrent last-ticket race
- [X] T016 [US1] `RecordAgreement` service method (idempotent guard per booking-flow.md §2, errors 400003/409002/410001) in backend/internal/order/service.go + repository update in backend/internal/order/repository.go (+ tests)
- [X] T017 [US1] Handlers + snake_case DTOs for `POST /ticket/book`, `POST /ticket/terms-condition/:order_id`, `GET /ticket/order/:order_id` (slots, `payment_started`, `expires_at`, `terms_agreed_at`) in backend/internal/order/handler.go + dto.go; per-IP limiter on book in backend/cmd/api/main.go (+ handler tests)
- [X] T018 [US1] Verify sweeper covers the 1-hour hold (same `payment_expires_at` query — no new job needed) and ensure every expired order is logged with order_number + restored quota lines (FR-009) in backend/internal/payment/service.go sweep path; add hold-expiry + quota-restore + log-assertion test in backend/internal/payment/service_test.go

### Frontend

- [X] T019 [US1] Rework frontend/components/booking/terms-dialog.tsx: fetch terms via `GET /ticket/terms-condition/:event_id` (render sanitized HTML), Agree fires book → record-agreement pair, then routes to `/events/[slug]/orders/[orderNumber]`; retry path for a failed second call; delete static frontend/lib/terms.ts; update terms-dialog.test.tsx
- [X] T020 [P] [US1] Expired-order state screen (Figma 288-2295) with restart link to `/events/[slug]/tickets` in frontend/components/order/expired-state.tsx, rendered by frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx when status is EXPIRED/CANCELLED (+ test)

**Checkpoint**: Booking + hold + expiry work end-to-end via API and dialog — MVP

---

## Phase 4: User Story 2 — QR Payment, 14-Minute Window, 7-Minute Refresh, Live Status (Priority: P1)

**Goal**: Checkout carries forms + starts payment; QR in-app; deadline 14m; auto-refresh at 7m; SSE status; expiry restores quota

**Independent Test**: quickstart.md Scenario 6 (+ Scenario 5's curl rejection)

### Backend

- [X] T021 [US2] `Checkout` service method per contracts/booking-flow.md §3 in backend/internal/order/service.go: guards (terms recorded, PENDING, unexpired, not started → idempotent QR return), TX-D saves buyer + slots, gateway call outside TX, TX-P stamps payment fields + `PAYMENT_WINDOW` deadline; visitor validation (name/email/phone/dob/gender) in backend/internal/order/dto.go; tests covering all failure branches in backend/internal/order/service_test.go
- [X] T022 [US2] Handler `POST /ticket/checkout/:order_id` (response incl. `qr_refresh_after_seconds`) in backend/internal/order/handler.go; move QR image route to `GET /ticket/order/:order_id/qris.png`; payment-refresh limiter group wiring in backend/cmd/api/main.go (+ handler tests)
- [X] T023 [P] [US2] QR re-issue: suffixed reference (`{orderNumber}-R{n}`) support in the `Gateway` usage + `ReissueQR` service in backend/internal/payment/service.go, handler `POST /ticket/checkout/:order_id/refresh-qr`; webhook matching strips `-R\d+` suffix in backend/internal/payment/handler.go (+ tests incl. paid-old-QR idempotency)
- [X] T024 [P] [US2] SSE hub (subscribe/publish per order, keep-alive 25s, terminal close, 15s DB drift re-read) in backend/internal/payment/stream.go; handler `GET /ticket/checkout/:order_id/status` with per-IP connection cap; publish transitions from webhook handler and sweeper; tests in backend/internal/payment/stream_test.go

### Frontend

- [X] T025 [US2] Payment screen on frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx (Figma 203-1157, 32-1366): QR panel (frontend/components/order/qris-panel.tsx) + 14-minute countdown from `expires_at`; **event countdown from events/[slug] layout stays mounted (FR-019)**; auto-refresh QR at `qr_refresh_after_seconds` with keep-old-QR-on-failure retry (+ page tests)
- [X] T026 [US2] SSE hook `useCheckoutStatus` (EventSource with 3s polling fallback of `GET /ticket/order/:order_id`) in frontend/lib/checkout-status.ts; wire paid → redirect `…/done`, expired → expired state (+ test)

**Checkpoint**: Full pay path: book → checkout → QR → paid/expired, live updates

---

## Phase 5: User Story 3 — Homepage Event Grid + Ticket Verification Aside (Priority: P2)

**Goal**: `/` = event grid + verify-ticket card; two-button chooser gone

**Independent Test**: quickstart.md Scenario 2 (first half)

- [X] T027 [P] [US3] Event grid component (name, date, venue, banner → links to `/events/[slug]`) in frontend/components/event/event-grid.tsx using `GET /event` (+ test)
- [X] T028 [P] [US3] Verify-ticket aside card wrapping the existing `GET /tickets/:code` lookup flow in frontend/components/ticket/verify-card.tsx (+ test)
- [X] T029 [US3] Replace frontend/app/page.tsx with grid + aside layout (Figma homepage frame); redirect `/events` → `/` in frontend/app/(public)/events/page.tsx (depends T027, T028)

**Checkpoint**: Homepage is the grid; lookup reachable without picking an event

---

## Phase 6: User Story 4 — CMS Event Detail Page + WYSIWYG Authoring (Priority: P2)

**Goal**: Admin authors description/T&C/content blocks; guest sees detail page before tickets; selection moves to `/events/[slug]/tickets`

**Independent Test**: quickstart.md Scenarios 1 + 2 (second half)

### Backend

- [X] T030 [P] [US4] Content-block CRUD (activities, guest stars, guidelines: create/list/update/delete, `position` ordering, fixed icon-key validation → 400004) in backend/internal/event/content_repository.go + admin_service.go + admin_handler.go routes `/admin/events/:id/{activities,guest-stars,guidelines}` (+ tests)
- [X] T031 [P] [US4] Terms admin upsert `GET/PUT /admin/events/:id/terms` with bluemonday sanitize-on-write (also sanitize event `description` in create/update) in backend/internal/event/admin_service.go + admin_handler.go (+ XSS strip test)
- [X] T032 [US4] Extend `GET /event/:id` DTO with `description` (HTML), `activities`, `guest_stars`, `guidelines` (position-ordered) in backend/internal/event/dto.go + service.go (+ tests) (depends T030)

### Frontend

- [X] T033 [P] [US4] TipTap WYSIWYG editor component (bold/italic/lists/headings/links, HTML value; MIT StarterKit only, toolbar built from existing shadcn/Base UI primitives) in frontend/components/admin/rich-text-editor.tsx (+ test)
- [X] T034 [US4] Admin event edit page: WYSIWYG description, T&C editor tab, content-block editors (activities/guest stars/guidelines with icon picker from the fixed set) in frontend/app/(admin)/admin/events/[id]/page.tsx + frontend/components/admin/content-block-form.tsx (depends T030, T031, T033)
- [X] T035 [US4] Guest event detail page (Figma 4-5): banner, schedule, venue, rendered HTML description, activities/guest stars/guidelines, "Buy Ticket" → `/events/[slug]/tickets`; `has_terms=false` disables buy with notice — rewrite frontend/app/(public)/events/[slug]/page.tsx (+ test)
- [X] T036 [US4] Move ticket selection to frontend/app/(public)/events/[slug]/tickets/page.tsx (Figma 12-1523, 20-796, 244-6525, 318-221), delete the old combined page content and frontend/app/(public)/events/[slug]/checkout/page.tsx (forms move to US5); update progress-rail step links in frontend/components/booking/booking-steps.tsx

**Checkpoint**: Detail page live with CMS content; selection on its own route

---

## Phase 7: User Story 5 — Order Page Visitor Forms (Priority: P2)

**Goal**: Order page collects buyer + per-slot visitor details; submits only with Continue to Payment (Option B)

**Independent Test**: quickstart.md Scenario 5

- [X] T037 [P] [US5] Visitor form components (per-slot name/email/phone/DOB/gender + buyer contact block, RHF + Zod snake_case payload) in frontend/components/order/visitor-form.tsx, reusing field pieces from the removed checkout page (+ tests)
- [X] T038 [US5] Order page forms phase (Figma 202-24, 244-7184, 203-379) in frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx: hold countdown from `expires_at`, empty forms on revisit (Option B), summary (subtotal/tax/service fee/total), Continue to Payment → `POST /ticket/checkout/:order_id` → flips to US2 payment phase; field-error mapping from 400001 (+ page tests)

**Checkpoint**: Forms → payment transition complete on one shared order page

---

## Phase 8: User Story 6 — Done Page, Email Receipt + E-Ticket, Resend (Priority: P2)

**Goal**: Paid → finished-order page; one email with receipt + PDF e-tickets (JIVE design); guest resend

**Independent Test**: quickstart.md Scenario 7

- [X] T039 [P] [US6] Restyle email HTML body + PDF e-ticket layout to Figma 251-2 in backend/internal/notification/smtp.go + pdf.go (+ golden/layout tests)
- [X] T040 [P] [US6] Remount guest resend as `POST /ticket/resend-email` with `{order_id}` body; move the per-order rate limit key from path param to body value in backend/internal/notification/handler.go + backend/cmd/api/main.go + backend/pkg/httpx rate limiter (+ tests: unpaid refused, limiter keyed per order)
- [X] T041 [US6] Done page styling to Figma 47-2396 + resend button on new endpoint in frontend/app/(public)/events/[slug]/orders/[orderNumber]/done/page.tsx + frontend/components/order/order-confirmation.tsx (+ tests)

**Checkpoint**: Full journey: grid → detail → tickets → T&C → order → pay → done → email

---

## Phase 9: Polish & Cross-Cutting

- [X] T042 [P] Update backend/cmd/api/architecture_test.go for new routes/domains boundaries; run `go test ./...` clean
- [X] T043 [P] Frontend sweep: `npm test`, `npm run lint`, `npx tsc --noEmit` clean; remove dead selection-encoding code in frontend/lib/selection.ts if unused
- [X] T044 [P] Patch-amend constitution Principle IV wording (`POST /api/v1/checkout` → book/checkout endpoints; research R10 doc-sync note) in .specify/memory/constitution.md + Sync Impact Report
- [X] T045 Update README.md API surface list + PRD.md/ARCHITECTURE.md endpoint references to the new namespaces
- [X] T046 Execute quickstart.md Scenarios 1–8 end-to-end against docker compose + sandbox and record results in specs/008-e2e-purchase-flow/quickstart.md notes

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 → Phase 2 → user stories**: Phase 2 blocks everything (schema + envelope + namespaces)
- **US1 (Phase 3)**: only Phase 2 — MVP
- **US2 (Phase 4)**: needs US1's order/slots (T015–T017)
- **US3 (Phase 5)**: only Phase 2 — fully parallel with US1/US2
- **US4 (Phase 6)**: only Phase 2 (terms table exists from T004 backfill) — parallel with US1/US2
- **US5 (Phase 7)**: needs US1 (order page exists) + US2 (checkout endpoint)
- **US6 (Phase 8)**: needs US2 (paid orders); T039/T040 independent of frontend stories
- **Phase 9**: after desired stories

### Story dependency graph

```
Setup → Foundational ─┬─ US1 ─┬─ US2 ─┬─ US5
                      │       │       └─ US6
                      ├─ US3  │
                      └─ US4 ─┘ (US4 route move T036 before US5 page work is cleanest)
```

### Parallel Opportunities

- Phase 1: T002 ∥ T003 (after T001 starts anytime)
- Phase 2: T007 ∥ T004; after T006+T008: T009, T010, T011 touch disjoint layers but share handler files with T009 — run T009 → T011 → T010, T012
- US1: T013 ∥ T014; T020 ∥ backend chain
- US2: T023 ∥ T024 after T021; frontend T025/T026 after T022
- US3 entirely ∥ US1/US2; T027 ∥ T028
- US4: T030 ∥ T031 ∥ T033
- US6: T039 ∥ T040

### Parallel example — after Foundational

```text
Dev A: T013→T018 (US1 backend) then T021→T024 (US2 backend)
Dev B: T019, T020, T025, T026 (guest frontend chain)
Dev C: T027–T029 (US3), then T030–T036 (US4)
```

---

## Implementation Strategy

**MVP = Phase 1 + Phase 2 + US1**: real orders with holds and expiry, testable entirely via API + dialog. Stop, validate quickstart Scenarios 3/4/8, then US2 for revenue, then US3–US6 in any order (US5 after US2).

Каждая story is a deployable increment; no task crosses story boundaries except the declared dependencies above.

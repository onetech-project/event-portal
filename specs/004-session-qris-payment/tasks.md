---

description: "Task list for 004-session-qris-payment"
---

# Tasks: Persistent Admin Session & In-App QRIS Payment Page

**Input**: Design documents from `/specs/004-session-qris-payment/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/api.md](./contracts/api.md), [quickstart.md](./quickstart.md)

**Tests**: Test tasks are included. plan.md's Technical Context specifies the test
approach explicitly (Go `testing`+`testify`, Vitest+Testing Library), and specs
001-003 established that convention across this codebase.

**Organization**: Tasks are grouped by user story so each can be implemented and
validated independently.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1-US4)
- Exact file paths are given in every task

## Path Conventions

Web app split, per plan.md: `backend/` (Go modular monolith) and `frontend/`
(Next.js App Router). Go tests live beside their source as `*_test.go`; frontend
tests live beside their source as `*.test.ts(x)`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Configuration and schema groundwork the payment stories build on

- [X] T001 [P] Add `PaymentExpiry` (default `15m`) and `PaymentSweepInterval` (default `30s`) to the config loader in `backend/pkg/config/config.go`, and reject `PAYMENT_EXPIRY` below 15 minutes with a clear error (research.md §2: the provider's expiry scheduler is unreliable below that); extend `backend/pkg/config/config_test.go` with default and override cases
- [X] T002 [P] Add `PAYMENT_EXPIRY=15m`, `PAYMENT_SWEEP_INTERVAL=30s`, and set `MIDTRANS_BASE_URL=https://api.sandbox.midtrans.com` (Core API host — **`api.`**, not `app.`) in `backend/.env.example` and `backend/.env`
- [X] T003 [P] Create `backend/migrations/0002_payment_qris.sql` adding `orders.payment_qr_string TEXT`, `orders.payment_expires_at TIMESTAMPTZ`, and `CREATE INDEX idx_orders_payment_expiry ON orders (payment_expires_at) WHERE status = 'PENDING'` (exact DDL in data-model.md §1)
- [X] T004 [P] Update `SCHEMA.md` with both new `orders` columns and the new index, and note that `payment_url` now holds the provider's QR-image action URL — the constitution requires `SCHEMA.md` to change in the same commit as the schema

**Checkpoint**: Configuration and schema source of truth are ready

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Interface, DTO, and persistence plumbing that US2, US3, and US4 all
depend on. Every task here keeps the tree compiling and the existing test suite
green — no behaviour changes yet.

**⚠️ CRITICAL**: US2/US3/US4 cannot begin until this phase is complete. **US1 is
frontend-only and does not depend on this phase at all** — it can be built in
parallel from the start.

- [X] T005 Change `schema:` in `backend/sqlc.yaml` from the single `migrations/0001_init.sql` to a list including `migrations/0002_payment_qris.sql`, for every domain block (depends on T003)
- [X] T006 [P] Add the `PaymentSession` struct (`ProviderRef`, `QRString`, `QRImageURL`, `ExpiresAt`, `RedirectURL`) to `backend/internal/payment/dto.go` per data-model.md §3
- [X] T007 Change the `Gateway` interface in `backend/internal/payment/gateway.go`: `CreateTransaction` returns `(PaymentSession, error)` instead of `(string, error)`, and add `FetchStatus(ctx, orderNumber) (*WebhookResult, error)` (contracts/api.md, "Gateway interface") (depends on T006)
- [X] T008 Mechanically adapt `backend/internal/payment/midtrans.go` to the new interface so the tree compiles — `CreateTransaction` returns `PaymentSession{RedirectURL: <snap url>}` for now, `FetchStatus` returns a not-implemented error — and update `backend/internal/payment/midtrans_test.go` accordingly. The real QRIS charge lands in T024 and the real status read in T035 (depends on T007)
- [X] T009 Mirror the new session shape on the consumer-declared `PaymentGateway` interface in `backend/internal/order/event_provider.go`, and add the `TicketTypeDisplay` struct (`TicketTypeName`, `EventName`, `EventSlug`) plus a batched `TicketTypeDisplays(ctx, ids)` method on the `EventLookup` interface in `backend/internal/order/admin_service.go` (data-model.md §3) (depends on T007)
- [X] T010 [P] Implement `TicketTypeDisplays` in the event domain (`backend/internal/event/` service + a query in `backend/internal/event/queries/event.sql`) joining only tables the event domain owns, and update its tests (depends on T009)
- [X] T011 Update the gateway and event-lookup adapters wired in `backend/cmd/api/main.go` so the composition root satisfies both changed interfaces (depends on T009, T010)
- [X] T012 Extend `UpdatePaymentDetails` in `backend/internal/order/queries/order.sql` and `backend/internal/order/repository.go` to persist `payment_qr_string` and `payment_expires_at` alongside `payment_url`/`payment_provider` (depends on T005)
- [X] T013 Run `sqlc generate` in `backend/` and commit the regenerated `*sql` packages; verify `ordersql` exposes the two new columns (depends on T005, T012)

**Checkpoint**: `go build ./... && ./scripts/test.sh` passes with no behaviour change; payment stories can begin

---

## Phase 3: User Story 1 - Admin session survives a page refresh (Priority: P1) 🎯 MVP

**Goal**: An admin stays signed in across reloads, direct URLs, and new tabs for
the full session lifetime; the login screen appears only when the session is
genuinely gone.

**Independent Test**: Sign in, reload every admin route, open an admin URL in a new
tab — no re-login, and no flash of the login screen. Quickstart Scenario 1.

**Independent of Phases 1-2**: this story touches only `frontend/`.

### Tests for User Story 1

- [X] T014 [P] [US1] Extend `frontend/lib/auth.test.ts` to cover the three-state session read (`loading` before mount, `authenticated` for a live token, `anonymous` for missing/corrupt/expired), that a corrupt entry is discarded without throwing, and that a same-tab sign-out notifies subscribers
- [X] T015 [P] [US1] Add `frontend/app/admin/layout.test.tsx` asserting the guard renders the loading placeholder (and does **not** redirect) on the first render, renders children once authenticated, and redirects to `/admin/login?next=<path>` only after resolving to anonymous — this is the regression test for the hydration race in research.md §1

### Implementation for User Story 1

- [X] T016 [US1] Rework `frontend/lib/auth.ts`: add a `readSession()` returning a discriminated state, an `authChanged` subscribe/notify pair that fires for **same-tab** writes (the native `storage` event does not), and have `clearToken` record whether the session ended by expiry or sign-out
- [X] T017 [US1] Rewrite the guard in `frontend/app/admin/layout.tsx` to resolve the session after mount (mounted flag / `useEffect`) with states `loading` | `authenticated` | `anonymous`; redirect only on `anonymous`, carrying `?next=<pathname>` and `?reason=expired`; keep the server and hydration renders identical so no hydration mismatch is introduced (depends on T016)
- [X] T018 [P] [US1] Update `frontend/app/admin/login/page.tsx` to honour `?next=` on successful sign-in (defaulting to the admin home) and to show an "your session expired" message when `?reason=expired` is present (FR-003, FR-005)
- [X] T019 [P] [US1] Update the sign-out handler in `frontend/components/admin/admin-nav.tsx` to notify the current tab via the new auth-change signal so it redirects immediately, while other tabs still react to the `storage` event (FR-006) (depends on T016)
- [X] T020 [US1] Update `frontend/lib/api-client.ts` so a `401` from `adminFetch` clears the token **and** marks the session as expired, so the next guard evaluation redirects with `reason=expired` (FR-004, FR-008) (depends on T016)

**Checkpoint**: Quickstart Scenario 1 passes end to end — the admin area is usable again

---

## Phase 4: User Story 2 - Guest pays with QRIS on the order page (Priority: P1)

**Goal**: Checkout lands the guest on `/orders/{order_number}` showing the order, a
scannable QRIS code, and a live countdown — never an external payment page.

**Independent Test**: Check out, confirm the browser stays on-site, and confirm the
page shows order details, a QR PNG that scans, and a decreasing countdown that
survives reloads unchanged. Quickstart Scenario 2.

**Depends on**: Phase 2.

### Tests for User Story 2

- [X] T021 [P] [US2] Add `httptest.Server` cases to `backend/internal/payment/midtrans_test.go` for the QRIS charge: request body carries `payment_type: "qris"`, `qris.acquirer`, whole-rupiah `gross_amount`, and `custom_expiry`; response parsing extracts `qr_string`, the `generate-qr-code` action URL, `expiry_time`, and `transaction_id`; non-2xx and missing-`qr_string` responses become errors
- [X] T022 [P] [US2] Add cases to `backend/internal/order/service_test.go` asserting checkout persists `payment_qr_string` and `payment_expires_at` in TX2, that the gateway call still happens outside any transaction, and that a charge failure still cancels the order and restores quota (FR-015)
- [X] T023 [P] [US2] Add `backend/internal/order/public_service_test.go` covering the public read model: `payment` present only while `PENDING` and before the deadline, `payment` null for `PAID`/`CANCELLED`/`EXPIRED`/past-deadline, and that ticket codes, attendees, and provider transaction ids never appear (FR-014, FR-022)

### Implementation for User Story 2

- [X] T024 [US2] Replace the Snap call in `backend/internal/payment/midtrans.go` with the Core API QRIS charge: `POST {base}/v2/charge` with `{"payment_type":"qris","transaction_details":{...},"qris":{"acquirer":"gopay"},"custom_expiry":{"expiry_duration":<PaymentExpiry>,"unit":"minute"}}`, Basic auth (server key, empty password), returning `PaymentSession{ProviderRef, QRString, QRImageURL, ExpiresAt}` (contracts/api.md, Midtrans notes) (depends on T008, T021)
- [X] T025 [US2] Update `Checkout` in `backend/internal/order/service.go` to consume `PaymentSession` and stamp `payment_qr_string` / `payment_expires_at` in the existing TX2, preserving the TX1 → charge-outside-transaction → TX2 sequence and the compensation path (depends on T012, T024)
- [X] T026 [P] [US2] Add `PublicOrderDetail`, `PublicOrderItem`, and `PaymentInstruction` DTOs to `backend/internal/order/dto.go` with the exact JSON shape in contracts/api.md, using `money.Money` for amounts and including `server_time`
- [X] T027 [US2] Add the public order read query to `backend/internal/order/queries/order.sql` (order + items by `order_number`, no cross-domain JOIN) and the matching repository method in `backend/internal/order/repository.go`, then re-run `sqlc generate` (depends on T013)
- [X] T028 [US2] Implement `backend/internal/order/public_service.go`: assemble `PublicOrderDetail`, resolve ticket-type and event display names through the batched `EventLookup.TicketTypeDisplays`, stamp `server_time`, and apply the payment-suppression rules from data-model.md §4 (depends on T009, T026, T027)
- [X] T029 [US2] Add `GET /orders/:orderNumber` to `backend/internal/order/handler.go` returning `PublicOrderDetail` with `Cache-Control: no-store`, and a `404 NOT_FOUND` that is byte-identical for unknown and unviewable orders; register it in `RegisterPublicRoutes` (depends on T028)
- [X] T030 [US2] Add `GET /orders/:orderNumber/qris.png` to `backend/internal/order/handler.go` rendering `payment_qr_string` on demand with `github.com/skip2/go-qrcode` at ≥300px, `Cache-Control: private, max-age=60`, and `404` whenever the order is not payable — no file is written anywhere (depends on T028)
- [X] T031 [P] [US2] Add `PublicOrderDetail` / `PaymentInstruction` types to `frontend/lib/types.ts` and a `useOrderDetail(orderNumber)` query to `frontend/lib/queries.ts` fetching `GET /orders/:orderNumber` with `cache: 'no-store'` (polling is added in US3)
- [X] T032 [US2] Replace `frontend/app/orders/[orderNumber]/page.tsx` with the real order detail page: order number, event, items with quantities and prices, buyer name/email, total, status, and — when `payment` is non-null — the QRIS panel (`<img>` pointing at `qris.png`), the amount to pay, and the countdown; not-found and loading states included (depends on T031)
- [X] T033 [P] [US2] Add `frontend/components/order/expiry-countdown.tsx` computing `offset = server_time − Date.now()` once per fetch and rendering `expires_at − (Date.now() + offset)`, ticking every second, plus `expiry-countdown.test.tsx` proving a device clock an hour off still shows the true remaining time (SC-005)
- [X] T034 [US2] Change `frontend/app/checkout/page.tsx` to route in-app to `/orders/${order_number}` on success and delete the `window.location.href = payment_url` redirect (FR-009) (depends on T032)

**Checkpoint**: Quickstart Scenario 2 passes — the guest pays without leaving the site

---

## Phase 5: User Story 3 - Page updates itself the moment payment succeeds (Priority: P1)

**Goal**: The open order page flips to a success state within 10s of the webhook,
and a rate-limited "check payment status" button reconciles on demand.

**Independent Test**: With the page open, complete a sandbox payment and watch it
switch by itself; separately press the button and confirm it reports the current
status even when nothing changed. Quickstart Scenario 3.

**Depends on**: Phase 2 and US2 (the page and read endpoint it updates).

### Tests for User Story 3

- [X] T035 [P] [US3] Add `httptest.Server` cases to `backend/internal/payment/midtrans_test.go` for `FetchStatus`: `GET {base}/v2/{order_id}/status` with Basic auth, mapping `transaction_status`/`fraud_status` onto the same `WebhookResult` shape a webhook produces, and surfacing transport failures as errors
- [X] T036 [P] [US3] Add cases to `backend/internal/payment/service_test.go` for `RefreshStatus`: it writes a `payments` audit row, applies the outcome through the existing guarded transition, reports `changed` correctly, is a no-op on an already-`PAID` order, and — run twice against a settlement — issues tickets and sends email exactly once (FR-025)
- [X] T037 [P] [US3] Add `frontend/app/orders/[orderNumber]/page.test.tsx` asserting the page polls while `PENDING`, stops polling once the status is final (FR-020), renders the success state with the buyer's email, and renders a cooldown rather than an error on a `429`

### Implementation for User Story 3

- [X] T038 [US3] Implement `FetchStatus` in `backend/internal/payment/midtrans.go` against `GET {base}/v2/{order_id}/status`, normalizing into `*WebhookResult` (depends on T008, T035)
- [X] T039 [US3] Extract the shared apply path in `backend/internal/payment/service.go` (audit-row write → already-`PAID` short-circuit → `MapProviderStatus` → `applyOutcome` → async fulfilment) so `HandleNotification` and the new `RefreshStatus(ctx, orderNumber) (RefreshResult, error)` both use it unchanged; `RefreshStatus` short-circuits final orders without calling the provider and force-expires orders already past `payment_expires_at` (contracts/api.md) (depends on T036, T038)
- [X] T040 [US3] Add `POST /orders/:orderNumber/payment/refresh` to `backend/internal/payment/handler.go` returning `{order_number, status, changed, checked_at}`, `404` for unknown orders, and `502 PAYMENT_STATUS_UNAVAILABLE` when the provider is unreachable (depends on T039)
- [X] T041 [US3] Register the refresh route in `backend/cmd/api/main.go` behind `httpx.RateLimitPerIP` (1 req / 5s with a small burst), returning `429 RATE_LIMITED` with `retry_after_seconds` — reuse the pattern already applied to the public ticket lookup (FR-018) (depends on T040)
- [X] T042 [US3] Add `refetchInterval` to `useOrderDetail` in `frontend/lib/queries.ts` — 3s while the status is `PENDING`, `false` once final — plus `refetchOnWindowFocus`/`refetchOnReconnect`, and add a `useRefreshPaymentStatus(orderNumber)` mutation that invalidates the detail query on success (depends on T031, T041)
- [X] T043 [US3] Add `frontend/components/order/payment-status-card.tsx` rendering the paid success state (replaces QR and countdown, names the buyer's email address, links to browse more events) and the still-waiting state (FR-019) (depends on T032)
- [X] T044 [US3] Wire the "check payment status" button into `frontend/app/orders/[orderNumber]/page.tsx`: always give visible feedback including "no change yet", disable with a visible cooldown on `429`, and surface a non-blocking "not live, retrying" indicator when the query is erroring or paused, recovering automatically (FR-017, FR-018, FR-021) (depends on T042, T043)

**Checkpoint**: Quickstart Scenario 3 passes — payment confirmation reaches the guest with no manual refresh

---

## Phase 6: User Story 4 - Expired or failed payment is handled cleanly (Priority: P2)

**Goal**: A deadline that passes flips the order to `EXPIRED`, returns its quota
exactly once, and stops showing a dead code.

**Independent Test**: Pull an order's deadline into the past and confirm the page
shows the expired state, the order becomes `EXPIRED` within 30s, and the ticket
quota returns to its pre-order value. Quickstart Scenario 4.

**Depends on**: Phase 2. Independent of US3, though both reuse the same apply path.

### Tests for User Story 4

- [X] T045 [P] [US4] Add cases to `backend/internal/payment/service_test.go` for `ExpireDueOrders`: only `PENDING` orders past their deadline are touched, quota is restored exactly once, two concurrent sweeps (and a sweep racing a webhook) restore quota once in total, and a sweep is a no-op when nothing is due (FR-024, SC-007)
- [X] T046 [P] [US4] Add cases to `backend/internal/order/repository_test.go` for `DueForExpiry`: it returns only `PENDING` orders whose `payment_expires_at` is in the past, honours the batch limit, and is ordered oldest-first

### Implementation for User Story 4

- [X] T047 [US4] Add the `DueForExpiry` query to `backend/internal/order/queries/order.sql` (`WHERE status = 'PENDING' AND payment_expires_at < $1 ORDER BY payment_expires_at LIMIT $2`, using the partial index from T003) and the repository method in `backend/internal/order/repository.go`, then re-run `sqlc generate` (depends on T013, T046)
- [X] T048 [US4] Add `DueForExpiry` to the consumer-declared `OrderProvider` interface in `backend/internal/payment/service.go` and implement the adapter in `backend/cmd/api/main.go` — the payment domain must not import the order repository (depends on T047)
- [X] T049 [US4] Implement `ExpireDueOrders(ctx, now)` in `backend/internal/payment/service.go`, applying the `EXPIRED` outcome with quota restoration through the **same** guarded `applyOutcome` transaction the webhook uses, and logging each expiry (depends on T045, T048)
- [X] T050 [US4] Add `backend/internal/payment/sweeper.go`: a ticker at `PaymentSweepInterval` calling `ExpireDueOrders`, started in `backend/cmd/api/main.go` and stopped during graceful shutdown alongside `WaitForFulfillment` (depends on T001, T049)
- [X] T051 [US4] Render the expired/cancelled state in `frontend/app/orders/[orderNumber]/page.tsx`: hide the QR and countdown the moment the countdown reaches zero, explain the order can no longer be paid, and link back to the event (`event.slug`) to start a new order (US4 acceptance scenarios 1 and 3) (depends on T032)
- [X] T052 [US4] Log a loud, admin-visible anomaly in `backend/internal/payment/service.go` when a successful notification arrives for an order that is already `EXPIRED`/`CANCELLED` — the order is not flipped back, and the discrepancy must be discoverable from the orders list (US4 acceptance scenario 5) (depends on T039)

**Checkpoint**: Quickstart Scenario 4 passes — quota accounting is correct under every expiry path

---

## Phase 7: Polish & Cross-Cutting Concerns

- [X] T053 [P] Remove the now-dead Snap constants and helpers from `backend/internal/payment/midtrans.go` (`SnapSandboxBaseURL`, `SnapProductionBaseURL`, `snapTransactionsPath`, unused request/response structs) and their tests
- [X] T054 [P] Update `README.md` with the new env vars (`PAYMENT_EXPIRY`, `PAYMENT_SWEEP_INTERVAL`, Core API `MIDTRANS_BASE_URL`), the manual `psql -f backend/migrations/0002_payment_qris.sql` step for existing dev databases, and the sandbox QRIS simulator URL
- [X] T055 [P] Add structured log fields for the new paths in `backend/internal/payment/` (`refresh`, `sweep`) — order number, outcome, whether it changed — consistent with the existing webhook logging so the observability stack picks them up
- [X] T056 Run the full suite: `cd backend && ./scripts/test.sh` and `cd frontend && npx vitest run` (depends on all prior phases)
- [X] T057 Walk every scenario in [quickstart.md](./quickstart.md) against a running stack, including the failure paths in Scenario 5 (depends on T056)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately
- **Foundational (Phase 2)**: depends on Phase 1; **blocks US2, US3, US4**
- **US1 (Phase 3)**: depends on nothing — frontend-only, can start on day one in parallel with Phase 1/2
- **US2 (Phase 4)**: depends on Phase 2
- **US3 (Phase 5)**: depends on Phase 2 and US2
- **US4 (Phase 6)**: depends on Phase 2; independent of US3
- **Polish (Phase 7)**: depends on the stories being delivered

### User Story Dependencies

- **US1 (P1)** — fully independent. Different codebase area (`frontend/app/admin/**`, `frontend/lib/auth.ts`) from every other story; it can ship alone.
- **US2 (P1)** — needs Phase 2 only.
- **US3 (P1)** — needs the page and read endpoint from US2 to update. Backend tasks (T035, T036, T038-T041) can start as soon as Phase 2 is done, in parallel with US2's frontend work.
- **US4 (P2)** — needs Phase 2 only. It shares `applyOutcome` with US3's T039; if US3 is not being built, extract that shared path as part of T049 instead.

### Within Each User Story

- Tests before the implementation they cover
- Migration and `sqlc generate` before any repository work
- Repository → service → handler → route registration
- Backend endpoint before the frontend that calls it

### Parallel Opportunities

- T001-T004 (all of Setup) run in parallel
- T006 and T010 run in parallel within Phase 2
- **All of US1 runs in parallel with all of Phases 1-2** — different developer, no shared files
- T021, T022, T023 (US2 tests) run in parallel
- T035, T036, T037 (US3 tests) run in parallel
- T045, T046 (US4 tests) run in parallel
- Once Phase 2 lands, US2-backend, US3-backend, and US4-backend are three independent tracks
- T053, T054, T055 run in parallel

---

## Parallel Example: after Phase 2

```bash
# Three independent tracks, one developer each:
Track A (US1, no dependency on Phase 2 at all):
  T014-T020  frontend/lib/auth.ts, frontend/app/admin/**

Track B (US2):
  T021,T022,T023 (tests in parallel) → T024 → T025 → T026-T034

Track C (US4, independent of US3):
  T045,T046 (tests in parallel) → T047 → T048 → T049 → T050
```

---

## Implementation Strategy

### MVP scope

**US1 alone is a shippable MVP** and the highest-value slice: it is a small,
frontend-only change that unblocks every admin feature already built in specs
002-003. Ship it first, independently of the payment work.

The payment change is only meaningful as **US2 + US3 together** — a QRIS page that
never confirms payment is a dead end (spec.md marks both P1). Treat them as one
release increment.

### Incremental delivery

1. Phase 1 + Phase 2 → foundation ready (tree compiles, tests green, no behaviour change)
2. **US1** → validate Quickstart Scenario 1 → ship (admin area usable again)
3. **US2 + US3** → validate Quickstart Scenarios 2 and 3 → ship (in-app QRIS payment with live confirmation)
4. **US4** → validate Quickstart Scenario 4 → ship (correct inventory under expiry)
5. Phase 7 → polish, full suite, quickstart walkthrough

### Risk notes

- **T005 + T013 are easy to forget**: `sqlc.yaml` must list both migration files or
  generation silently omits the new columns and every later repository task fails
  confusingly.
- **T003 on an existing database**: `docker-entrypoint-initdb.d` runs only on an
  empty volume. Apply `0002_payment_qris.sql` by hand to any dev database that
  already exists (quickstart.md, Prerequisites).
- **T024's host change**: Core API lives on `api.sandbox.midtrans.com`, not the
  `app.sandbox.midtrans.com` the Snap code used. A wrong host fails with a
  confusing 404.
- **T029/T030 must stay cheap**: no outbound provider call and no write on the
  polled endpoints — reconciliation belongs solely to T040.

## Notes

- [P] = different files, no dependencies on incomplete tasks
- Every transition out of `PENDING` must go through the single guarded
  `UPDATE ... WHERE status = 'PENDING'` + quota-restore transaction (Constitution
  Principle IV) — T039, T049, and the existing webhook all share it
- Commit after each task or logical group; stop at any checkpoint to validate a
  story independently

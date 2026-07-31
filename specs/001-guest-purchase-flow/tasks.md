---

description: "Task list for Guest Purchase Flow implementation"
---

# Tasks: Guest Purchase Flow

**Input**: Design documents from `/specs/001-guest-purchase-flow/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/api.md, quickstart.md

**Tests**: Not explicitly requested in spec.md; test tasks are omitted. Add
`testify`-based tests alongside services if the team wants TDD later.

**Organization**: Tasks are grouped by user story (US1-US4) to enable independent
implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3, US4)

## Path Conventions

Web app per plan.md: `backend/internal/<domain>/`, `backend/cmd/api/`,
`backend/pkg/`, `backend/migrations/`, `frontend/app/`, `frontend/components/`,
`frontend/lib/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Project initialization and basic structure

- [ ] T001 Create `backend/` (Go module, `cmd/api/main.go` entrypoint) and
      `frontend/` (Next.js App Router + TypeScript) project skeletons per plan.md
- [ ] T002 Add backend dependencies: Echo v4, sqlc, pgx, go-qrcode, maroto/gofpdf,
      go-mail (or net/smtp), Midtrans SNAP client, in `backend/go.mod`
- [ ] T003 Add frontend dependencies: TanStack Query, React Hook Form, Zod,
      TailwindCSS, in `frontend/package.json`
- [ ] T004 [P] Configure Go linting/formatting (`gofmt`, `golangci-lint`) in
      `backend/.golangci.yml`
- [ ] T005 [P] Configure frontend linting/formatting (ESLint, Prettier) in
      `frontend/.eslintrc.json`
- [ ] T006 Write `backend/migrations/0001_init.sql` from SCHEMA.md verbatim (all 8
      tables + the 4 indexes, no additions — SCHEMA.md is LOCKED) and set up
      `sqlc.yaml` pointing at it
- [ ] T007 Add `docker-compose.yml` at repo root with a PostgreSQL service and
      volume-mounted migrations

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can
be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [ ] T008 Implement DB connection pool setup (`pgxpool`) in
      `backend/pkg/db/db.go`
- [ ] T009 [P] Implement config loading (env vars: DB DSN, Midtrans keys, SMTP
      creds) in `backend/pkg/config/config.go`
- [ ] T010 [P] Implement structured logger in `backend/pkg/logger/logger.go`
- [ ] T011 Run `sqlc generate` to produce typed query structs from
      `backend/migrations/0001_init.sql` into `backend/internal/*/sqlc/` (or a
      shared `backend/pkg/sqlc/` package per domain table ownership)
- [ ] T012 Wire Echo router, global error-handling middleware, and health check
      in `backend/cmd/api/main.go`
- [ ] T013 Define `EventProvider` interface (`CheckAndDeductQuota`, `RestoreQuota`)
      contract in `backend/internal/event/service.go` (implementation completed in
      US2); document in the doc comment that `ticket_types.quota` is the REMAINING
      quota, decremented at checkout and restored on cancel/expire/deny (spec
      FR-018)
- [ ] T014 [P] Scaffold `frontend/lib/api-client.ts` (base fetch wrapper,
      `cache: 'no-store'` default for GET) and `frontend/lib/env.ts`

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Browse events and start checkout (Priority: P1) 🎯 MVP

**Goal**: Guests can list published events and view one event's detail with its
ticket types and remaining quota.

**Independent Test**: Request the published events list and a single event's
detail page; verify accurate name, schedule, venue, ticket types, prices, and
available quota, with no account required.

### Implementation for User Story 1

- [ ] T015 [P] [US1] Add `event` domain DTOs (`EventSummary`, `EventDetail`,
      `TicketTypeSummary`) in `backend/internal/event/dto.go`; `quota_remaining`
      maps 1:1 onto `ticket_types.quota` (no arithmetic — it already IS the
      remaining count) and `banner_url` is passed through verbatim as the
      admin-supplied URL string (nullable; no upload/storage exists)
- [ ] T016 [P] [US1] Implement `event` repository read queries (list published
      events, get event by slug with ticket types + remaining quota) in
      `backend/internal/event/repository.go`
- [ ] T017 [US1] Implement `event` service methods `ListPublishedEvents` and
      `GetPublishedEventBySlug` in `backend/internal/event/service.go` (depends on
      T015, T016)
- [ ] T018 [US1] Implement `GET /api/v1/events` and `GET /api/v1/events/:slug`
      handlers in `backend/internal/event/handler.go`, registered in
      `backend/cmd/api/main.go`
- [ ] T019 [P] [US1] Build `frontend/app/events/page.tsx` (published events list,
      `no-store` fetch)
- [ ] T020 [P] [US1] Build `frontend/app/events/[slug]/page.tsx` (event detail +
      ticket type list with remaining quota, `no-store` fetch)

**Checkpoint**: User Story 1 fully functional and independently testable via
quickstart.md Scenario 1

---

## Phase 4: User Story 2 - Complete checkout with dynamic attendee info (Priority: P1)

**Goal**: Guests can submit checkout (buyer info + per-attendee entries), get
quota atomically deducted, an order created, and a Payment URL returned.

**Independent Test**: Submit a checkout request with valid buyer/attendee info and
available quota; verify Order Number + Payment URL returned, order/attendee
records exist, and quota decreased by the purchased quantity (per quickstart.md
Scenario 2). Also force a gateway failure and verify the order ends `CANCELLED`
with its quota restored (spec FR-021).

### Implementation for User Story 2

- [ ] T021 [US2] Implement `CheckAndDeductQuota(ctx, tx, ticketTypeID, qty)` and its
      mirror `RestoreQuota(ctx, tx, ticketTypeID, qty)` on the `EventProvider`
      interface in `backend/internal/event/service.go` — guarded
      `UPDATE ticket_types SET quota = quota - $qty WHERE id = $id AND quota >= $qty
      RETURNING quota` for deduct (no rows affected ⇒ insufficient-quota error) and
      `SET quota = quota + $qty` for restore; both take the caller's `pgx.Tx`
      (depends on T013, T016)
- [ ] T022 [P] [US2] Add `order` domain DTOs (`CheckoutRequest`, `OrderResponse`)
      in `backend/internal/order/dto.go`
- [ ] T023 [P] [US2] Implement `order` repository writes in
      `backend/internal/order/repository.go`: insert order (with `status='PENDING'`,
      `payment_url` NULL), insert order items, insert attendees, plus
      `UpdatePaymentDetails(orderID, url, provider)` and
      `MarkCancelled(orderID)` used by TX2 and the compensation path
- [ ] T024 [US2] Define `payment.Gateway` interface (`CreateTransaction`,
      `VerifyWebhook`) in `backend/internal/payment/gateway.go`
- [ ] T025 [US2] Implement Midtrans SNAP Sandbox adapter for `CreateTransaction` in
      `backend/internal/payment/midtrans.go` (depends on T024)
- [ ] T026 [US2] Implement checkout **TX1** in `backend/internal/order/service.go`:
      pre-validate attendee count == total quantity, open `pgx.Tx`, validate each
      ticket type's sales window, call `EventProvider.CheckAndDeductQuota` per line
      item, recompute the total from current prices, insert order/order_items/
      attendees, `COMMIT`. No external network call may occur inside this
      transaction — this is the single transaction ARCHITECTURE.md §3.4 requires
      (depends on T021, T022, T023)
- [ ] T027 [US2] After TX1 commits, call `Gateway.CreateTransaction` **outside any
      transaction**, then open **TX2** to persist `payment_url`/`payment_provider`
      and commit, returning `payment_url` to the guest, in
      `backend/internal/order/service.go` (depends on T025, T026)
- [ ] T028 [US2] Implement the compensating transaction for gateway failure in
      `backend/internal/order/service.go`: set the order to `CANCELLED` and restore
      each line item's quota via `EventProvider.RestoreQuota` in one transaction,
      then return a `PAYMENT_INITIATION_FAILED` error (spec FR-021) (depends on
      T023, T026, T027)
- [ ] T029 [US2] Generate a unique `order_number` (e.g., date-based + random
      suffix, checked against the `orders.order_number` unique constraint) in
      `backend/internal/order/service.go`
- [ ] T030 [US2] Implement `POST /api/v1/checkout` handler with request validation
      and error mapping (400 for validation/insufficient quota, 502 for
      `PAYMENT_INITIATION_FAILED`) in `backend/internal/order/handler.go`,
      registered in `backend/cmd/api/main.go` (depends on T027, T028)
- [ ] T031 [P] [US2] Build `frontend/app/checkout/page.tsx`: ticket type quantity
      selectors, buyer info form, dynamic per-attendee name/email fields (React
      Hook Form + Zod), submit to checkout endpoint, surface the 502 retry message
- [ ] T032 [P] [US2] Build `frontend/app/orders/[orderNumber]/page.tsx` to redirect
      the guest to the returned `payment_url`

**Checkpoint**: User Stories 1 AND 2 both work independently

---

## Phase 5: User Story 3 - Receive tickets after payment (Priority: P1)

**Goal**: Payment webhook updates order status idempotently across the full
provider-status mapping; on Paid, generates one ticket per attendee and emails the
buyer a single PDF; on cancel/expire/deny/failure, restores quota.

**Independent Test**: Simulate a "payment successful" notification for a Pending
order; verify order becomes Paid, one ticket per attendee is created with a
unique code, and the buyer receives one email with a PDF attachment containing all
tickets (per quickstart.md Scenario 3); verify duplicate notifications are no-ops;
verify cancel/expire restores quota (Scenario 4); verify `deny` and `failure` also
land the order in `CANCELLED` with quota restored, and that `pending` /
`capture`+`challenge` leave the order untouched.

### Implementation for User Story 3

- [ ] T033 [P] [US3] Implement `payment` repository to append a `payments` row per
      notification received in `backend/internal/payment/repository.go`, storing the
      provider's **raw** `transaction_status` and full payload (so `deny` vs
      `failure` stays distinguishable even though both map to `CANCELLED`)
- [ ] T034 [US3] Implement `VerifyWebhook` (Midtrans signature-key verification) in
      `backend/internal/payment/midtrans.go` (depends on T024)
- [ ] T035 [US3] Implement the total provider-status → order-status mapping as a
      pure function in `backend/internal/payment/status.go`, per the Payment Status
      Mapping table in spec.md: `settlement` → PAID; `capture`+`fraud_status=accept`
      → PAID; `capture`+`fraud_status=challenge` → remain PENDING (no quota change);
      `pending` → remain PENDING (no-op); `deny` → CANCELLED + restore;
      `cancel` → CANCELLED + restore; `expire` → EXPIRED + restore;
      `failure` → CANCELLED + restore. Unknown statuses return an explicit
      "unhandled" result that the handler logs loudly rather than silently ignoring
      (depends on T024)
- [ ] T036 [US3] Implement `POST /api/v1/payment/webhook/:provider` handler in
      `backend/internal/payment/handler.go`: verify signature (401 on failure),
      append the `payments` row, load order, short-circuit `200 OK` if already
      `PAID`, else apply the T035 mapping — for restoring outcomes open one
      transaction that updates `orders.status` guarded on `WHERE status = 'PENDING'`
      and restores quota via `EventProvider.RestoreQuota` (so a replayed cancel
      cannot restore twice); always answer `200 OK` for authenticated notifications
      (depends on T021, T033, T034, T035)
- [ ] T037 [P] [US3] Add `ticket` domain DTOs and repository (insert one ticket per
      attendee persisting **only** the unique `ticket_code`, leaving `qr_code_url`
      NULL per spec FR-022) in `backend/internal/ticket/dto.go` and
      `backend/internal/ticket/repository.go`
- [ ] T038 [US3] Implement ticket code generation in
      `backend/internal/ticket/service.go`: canonical form at write time —
      uppercase, fixed length, unambiguous charset excluding `I`, `O`, `0`, `1`
      (e.g. `23456789ABCDEFGHJKLMNPQRSTUVWXYZ`), retried on the `ticket_code`
      unique-constraint violation. No QR image or URL is generated or stored here
      (depends on T037)
- [ ] T039 [P] [US3] Implement PDF rendering (maroto/gofpdf, one PDF with all of an
      order's tickets) in `backend/internal/notification/pdf.go`, generating each
      QR **on demand** from the ticket's `ticket_code` with `go-qrcode` into an
      in-memory image embedded in the PDF — never read from or written to storage
- [ ] T040 [P] [US3] Implement SMTP email dispatch in
      `backend/internal/notification/smtp.go`
- [ ] T041 [US3] Implement `notification` service `SendTicketEmail(order)`
      orchestrating PDF render + SMTP send + setting `orders.email_sent = true` in
      `backend/internal/notification/service.go`; it must be safely re-runnable for
      the admin resend path, re-rendering QR codes from the stored `ticket_code`
      (depends on T039, T040)
- [ ] T042 [US3] Wire the webhook handler's Paid path to launch a non-blocking
      goroutine calling ticket generation (T038) then `SendTicketEmail` (T041),
      returning `200 OK` before that work completes, in
      `backend/internal/payment/handler.go` (depends on T036, T038, T041)

**Checkpoint**: User Stories 1, 2, AND 3 all work independently; core purchase
flow is end-to-end functional

---

## Phase 6: User Story 4 - Look up a ticket by code (Priority: P3)

**Goal**: Guests can look up a ticket by code and see its event, attendee name,
and status — over a rate-limited endpoint that discloses nothing more.

**Independent Test**: Look up a known valid ticket code and an unknown/invalid
code; verify correct ticket details or a clear not-found result respectively (per
quickstart.md Scenario 5); verify the response body contains no email address; and
verify that hammering the endpoint trips the rate limit with `429`.

### Implementation for User Story 4

- [ ] T043 [P] [US4] Implement ticket-by-code read query in
      `backend/internal/ticket/repository.go` using an **exact** match
      (`WHERE ticket_code = $1`) so `idx_tickets_ticket_code` is used; never
      `UPPER(ticket_code) = UPPER($1)`, which would force a sequential scan since
      SCHEMA.md is locked and no functional index exists (depends on T037)
- [ ] T044 [US4] Implement `GET /api/v1/tickets/:code` handler in
      `backend/internal/ticket/handler.go`: normalize the input in Go (trim +
      uppercase) before the exact-match lookup, return only `ticket_code`,
      `status`, `event_name`, `attendee_name` (attendee email and all buyer data
      MUST be omitted — spec FR-016), flat 404 on not found; registered in
      `backend/cmd/api/main.go` (depends on T043)
- [ ] T045 [US4] Apply per-IP rate limiting to the `GET /api/v1/tickets/:code`
      route (Echo `middleware.RateLimiter` with the in-memory store — no Redis,
      which is out of scope per PRD.md §1.6), returning `429` over the limit, with
      the limit configurable via `backend/pkg/config/config.go`; registered in
      `backend/cmd/api/main.go` (spec FR-020) (depends on T044)
- [ ] T046 [P] [US4] Build `frontend/app/tickets/[code]/page.tsx` ticket lookup
      page

**Checkpoint**: All four user stories independently functional

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: Improvements that affect multiple user stories

- [ ] T047 [P] Add structured error codes (e.g., `INSUFFICIENT_QUOTA`,
      `ATTENDEE_COUNT_MISMATCH`, `TICKET_TYPE_NOT_ON_SALE`,
      `PAYMENT_INITIATION_FAILED`, `RATE_LIMITED`) consistently across
      `order`/`event`/`ticket` handlers
- [ ] T048 [P] Add request logging (order number, ticket code, webhook provider and
      raw provider status) across `order`/`payment`/`ticket` handlers using the
      logger from T010
- [ ] T049 Run `quickstart.md` end-to-end against a local Docker Compose stack and
      fix any discrepancies found
- [ ] T050 [P] Review all domain `dto.go` files to confirm no sqlc-generated
      struct is returned directly in any HTTP response (Constitution Principle III)
      and that no public response leaks attendee/buyer email or `qr_code_url`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - start immediately
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories
- **User Story 1 (Phase 3)**: Depends on Foundational only
- **User Story 2 (Phase 4)**: Depends on Foundational; reuses US1's `event`
  repository reads for pricing/sales-window checks but does not require US1's
  handlers/frontend
- **User Story 3 (Phase 5)**: Depends on Foundational and on US2's order/payment
  scaffolding (`Gateway` interface, order records existing in `PENDING`, and
  `EventProvider.RestoreQuota` from T021)
- **User Story 4 (Phase 6)**: Depends on Foundational and on US3's ticket
  generation existing (tickets must be created before they can be looked up)
- **Polish (Phase 7)**: Depends on all desired user stories being complete

### Parallel Opportunities

- T004/T005 (lint configs) in parallel
- T009/T010/T014 in Foundational in parallel
- Within US1: T015/T016 in parallel, then T019/T020 in parallel after T018
- Within US2: T022/T023 in parallel; T031/T032 in parallel after T030. T026 → T027
  → T028 are strictly sequential (TX1, then the gateway call, then compensation)
- Within US3: T033/T037 in parallel; T039/T040 in parallel
- Within US4: T043 and later T046 in parallel with T044/T045

---

## Parallel Example: User Story 2

```bash
Task: "Add order domain DTOs in backend/internal/order/dto.go"
Task: "Implement order repository writes in backend/internal/order/repository.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational
3. Complete Phase 3: User Story 1
4. **STOP and VALIDATE**: quickstart.md Scenario 1
5. Demo the browsable event catalog

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 (browse) → validate → demo
3. US2 (checkout) → validate → demo (guests can now order, quota deducts, payment
   URL issued)
4. US3 (payment → tickets) → validate → demo (full guest purchase loop complete —
   this is the true feature MVP, since US1+US2 alone deliver no tickets)
5. US4 (ticket lookup) → validate → demo

**Note**: Although US1 is technically the smallest deployable slice, the feature's
actual value (a guest receiving a ticket) is only realized once US1+US2+US3 are
all complete — treat those three as the effective MVP bundle.

---

## Notes

- [P] tasks touch different files with no unmet dependencies
- Commit after each task or logical group
- Re-run quickstart.md scenarios at each checkpoint
- Avoid cross-story file conflicts: `event` repository reads (T016) are shared but
  additive-only across US1/US2; no story should modify another story's files
- SCHEMA.md is LOCKED: no task may add a column, table, or index. In particular
  `tickets.qr_code_url` stays NULL (FR-022) and `ticket_types.quota` is the
  remaining quota (FR-018) — there is no total/sold counter to reconcile against
- Accepted MVP risk (research.md "Checkout transaction shape"): a crash between
  T026's TX1 and T027's TX2 leaves a `PENDING` order holding quota with no payment
  URL and no incoming webhook. The post-MVP mitigation is a periodic sweep expiring
  stale `PENDING` orders and restoring their quota — a plain scheduled task, not a
  queue/broker, so it stays within PRD.md §1.6. It is intentionally **not** a task
  in this feature

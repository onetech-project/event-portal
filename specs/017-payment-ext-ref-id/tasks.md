# Tasks: Payment External Reference ID

**Input**: Design documents from `/specs/017-payment-ext-ref-id/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/api.md](contracts/api.md), [quickstart.md](quickstart.md)

**Branch**: `fix/payment` — **not** `refractor/fe`. This matters for real paths:

| Thing | On this branch | On `refractor/fe` |
|-------|----------------|-------------------|
| Acceptance suite | `e2e/` | `frontend/__test__/` |
| Run it | `cd e2e && npm test` | `cd frontend && npm run test:e2e` |
| Slow/headed | `cd e2e && npm run test:slow` | `npm run test:e2e:slow` |

`AGENTS.md` documents the `refractor/fe` layout because that relocation (commit `1e2ae2f`) is
not an ancestor of this branch. **Every path below is the one that exists here.** Nothing in the design changes if this later rebases onto that
refactor — only the spec files move.

**Tests**: Included and NOT optional. Constitution Principle VIII makes `e2e/` an acceptance
gate, and this feature changes two covered flows (checkout, admin console).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

Web application, per [plan.md](plan.md): Go modular monolith in `backend/` (one package per
domain, composition root in `backend/cmd/api/`), Next.js App Router in `frontend/`, Playwright
acceptance suite in `e2e/`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Establish a green baseline, so anything red later is this feature's doing.

- [X] T001 Bring up the infrastructure the runner does not own: `REDIS_PORT=6380 docker compose up -d postgres redis mailpit && docker compose run --rm migrate up`
- [X] T002 Record a green baseline BEFORE touching anything — `cd backend && ./scripts/test.sh ./...`, `cd frontend && npx vitest run`, `cd e2e && npm test`. A tier already red is a pre-existing condition to report, not something to debug inside this feature.
- [X] T003 [P] Confirm the codegen toolchain is present: `sqlc version` reports v1.31.1 (matching the header in `backend/internal/payment/paymentsql/payment.sql.go`) and `docker compose run --rm migrate version` works

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The column, the row, and the write that fills it. Both user stories read what this
phase records, so neither can start until it lands.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

### Schema

- [X] T004 Create `backend/migrations/000015_payment_external_ref.up.sql` — `ALTER TABLE payments ADD COLUMN ext_ref_id VARCHAR(255);`. Nullable, no default, no constraint, no index; each omission is justified in [data-model.md](data-model.md) §1. Head the file with the same explanatory comment style as `000014_ticket_type_event_window.up.sql`.
- [X] T005 Create `backend/migrations/000015_payment_external_ref.down.sql` — `ALTER TABLE payments DROP COLUMN ext_ref_id;`
- [X] T006 Update the `payments` block in `SCHEMA.md` with the `ext_ref_id` column and the note that it is populated on exactly one row per order (the `SESSION_OPENED` marker) and NULL everywhere else, because the gateway supplies the reference only at session open and never on a callback. **Same commit as T004/T005** — standing repository rule.

### Queries and codegen

- [X] T007 Update `backend/internal/payment/queries/payment.sql`: add `ext_ref_id` to the `CreatePayment` insert column list and `$7`; add `ext_ref_id` to the `ListPaymentsByOrderID` select list. Preserve the existing explanatory comments verbatim.
- [X] T008 Add the `GetExternalRefByOrderID :one` query to `backend/internal/payment/queries/payment.sql` exactly as specified in [data-model.md](data-model.md) §4, including the comment explaining that no rows is a normal answer rather than an error
- [X] T009 Run `sqlc generate` from `backend/` and commit the regenerated `backend/internal/payment/paymentsql/` (depends on T004, T007, T008 — sqlc replays the migrations directory to build its schema, so the migration must exist first)

### Payment domain

- [X] T010 [P] Add `ExtRefID string` to `PaymentLog` in `backend/internal/payment/repository.go`, and map empty string → `NULL` in `CreatePayment` exactly as `PaymentType` already is
- [X] T011 [P] Add `ExtRefID string` to `PaymentRecord` in `backend/internal/payment/repository.go` and populate it in `toPaymentRecords`, dereferencing the nullable column to `""`
- [X] T012 Add `ExternalRefByOrderID` to `backend/internal/payment/repository.go` over the T008 query, translating `pgx.ErrNoRows` to `("", nil)` — never an error (depends on T009)
- [X] T013 [P] Add `MarkerSessionOpened = "SESSION_OPENED"` to the marker const block in `backend/internal/payment/service.go` (~line 80-93), documented as the counterpart of `MarkerSessionDuplicate` and as the row `SettlementForOrder` has always described itself as reading
- [X] T014 Implement `Service.RecordSessionOpened(ctx, orderNumber string, session PaymentSession) error` in `backend/internal/payment/service.go`, mirroring `ReleaseDuplicateSession`'s shape (order number in, order looked up internally). Writes one `PaymentLog`: `provider=gateway.Name()`, `transaction_id=orderNumber` (the network id does not exist yet and the column is NOT NULL), `payment_type="qris"`, `status=MarkerSessionOpened`, `ext_ref_id=session.ProviderRef`, and the marker envelope from [data-model.md](data-model.md) §2 as `raw_response` (depends on T010, T013)

### Composition root — the write

- [X] T015 In `backend/cmd/api/adapters.go`, call `a.payments.RecordSessionOpened(ctx, req.OrderNumber, session)` on the **success** branch of `(*gatewayAdapter).CreateTransaction`, immediately after `CreateTransaction` returns and before mapping onto `order.PaymentSession`. Guard on `a.payments != nil` and **log-and-swallow** any error, exactly as the sibling duplicate branch already does — an audit write must never destroy a payable session ([research.md](research.md) Decision 2) (depends on T014)

### Foundational tests

- [X] T016 [P] In `backend/internal/payment/repository_test.go`: a `PaymentLog` carrying `ExtRefID` round-trips through `CreatePayment` → `ListByOrder`; an empty `ExtRefID` is stored as SQL `NULL` (not `''`) and reads back as `""`
- [X] T017 [P] In `backend/internal/payment/repository_test.go`: `ExternalRefByOrderID` returns the reference for an order with a `SESSION_OPENED` row, and returns `("", nil)` — asserting explicitly that it is **not** an error — for an order without one
- [X] T018 [P] In `backend/internal/payment/service_test.go`: `RecordSessionOpened` writes exactly one row with every column from [data-model.md](data-model.md) §2, including the `transaction_id` order-number fallback and the envelope's `expiry_from_gateway` field
- [X] T019 [P] In `backend/internal/payment/service_test.go`: a repository failure inside `RecordSessionOpened` is surfaced to the caller but does not panic, and the `gatewayAdapter` path in T015 still returns a usable session
- [X] T020 [P] **Regression guard** in `backend/internal/payment/service_test.go`: with a `SESSION_OPENED` row preceding the notification rows, `SettlementForOrder` still resolves `Method` and `PaidAt` from the notification rows — the new row is oldest and must not win the newest-first walk ([research.md](research.md) Decision 5)
- [X] T021 Run `cd backend && ./scripts/test.sh ./...` and confirm `TestNoDomainImportsAnotherDomain` in `backend/cmd/api/architecture_test.go` is still green

**Checkpoint**: The reference is now recorded on every new checkout. Nothing reads it yet. US1 and US2 can proceed in parallel from here.

---

## Phase 3: User Story 1 - Operator traces a stranded order to the gateway (Priority: P1) 🎯 MVP

**Goal**: An operator investigating a payment can read the gateway's reference off the order's
payment view and go match it against the gateway's own records.

**Independent Test**: Complete a checkout so a session opens, then open that order in the admin
console and confirm the displayed reference is character-for-character the one the gateway
returned. Requires no change to any API response shape — verifiable entirely through Phase 2's
recorded row plus this phase's read.

### Backend

- [X] T022 [US1] Add `ExtRefID string \`json:"ext_ref_id"\`` to `NotificationResponse` in `backend/internal/payment/reconcile_dto.go` and populate it in `toNotificationResponse`, documented as empty on every row but the `SESSION_OPENED` marker
- [X] T023 [P] [US1] Test in `backend/internal/payment/` that `GET /admin/payment/order/:order_id/notifications` returns `ext_ref_id` populated on the `SESSION_OPENED` row and empty (never null) on notification rows

### Frontend

- [X] T024 [P] [US1] Add `ext_ref_id: string` to the `PaymentNotification` type in `frontend/lib/types.ts`, with a doc comment noting only the session-open marker row carries one
- [X] T025 [US1] In `frontend/components/admin/order-payment-panel.tsx`, derive the order-level value (`notifications.data?.find((n) => n.ext_ref_id)?.ext_ref_id ?? ""`) and render it as a **single labelled line** inside the existing dialog — full value, selectable for copy, absent-value placeholder consistent with the panel's existing `—` convention. No new request, no new query key, no layout reflow (FR-014 through FR-018) (depends on T024)
- [X] T026 [US1] Extend `frontend/components/admin/order-payment-panel.test.tsx`: the reference renders when a row carries it; the placeholder renders when none does; it appears exactly **once** rather than per notification row; the `SESSION_OPENED` row renders through the existing marker path with the "our note" badge; every existing assertion (holds table, shortfall alert, notification history) still passes unchanged

### Guard

- [X] T027 [US1] Confirm no other admin surface changed: the orders list gains no column, the seat-holds table and notification history behave as before, and no route was added (FR-016, FR-019)

**Checkpoint**: An operator can now trace any newly checked-out order to the gateway. This is the MVP — deliverable and demonstrable without US2.

---

## Phase 4: User Story 2 - Checkout response carries the reference (Priority: P2)

**Goal**: `ext_ref_id` rides every checkout response that carries a payment payload, naming the
same session on all three.

**Independent Test**: Call checkout for a bookable order and assert `ext_ref_id` equals the
gateway's value; retry it and assert the `409` payload names the same reference.

### The read seam

- [X] T028 [P] [US2] Declare the consumer-owned `PaymentRecords` interface in `backend/internal/order/event_provider.go` — one method, `ExternalRefForOrder(ctx, orderID uuid.UUID) (string, error)` — alongside the existing `EventProvider` and `PaymentGateway` declarations and documented the same way. **Read-only**: the write lives in the composition root ([research.md](research.md) Decision 3)
- [X] T029 [P] [US2] Add `ExtRefID string \`json:"ext_ref_id"\`` to `CheckoutQRResponse` in `backend/internal/order/dto.go`, documented as present-and-empty rather than absent when no reference exists (FR-010)
- [X] T030 [US2] Implement `Service.ExternalRefForOrder(ctx, orderID) (string, error)` in `backend/internal/payment/service.go` over the T012 repository method (depends on T012)
- [X] T031 [US2] Make `*gatewayAdapter` in `backend/cmd/api/adapters.go` satisfy `order.PaymentRecords` by adding `ExternalRefForOrder`, delegating to `a.payments` with the same `!= nil` guard. One adapter, two interfaces — the type already holds the payment service for T015 (depends on T030)
- [X] T032 [US2] Wire it in `backend/cmd/api/main.go`: add a `WithPaymentRecords(checkoutGateway)` builder call to the `order.NewService(...)` chain (~line 211), matching the existing `.WithCache(...)`/`.WithTimers(...)` style. Note `checkoutGateway.payments` is assigned later (~line 268) to close the mutual dependency — the pointer makes that safe, exactly as it already does for the duplicate-release path (depends on T028, T031)

### Populating the three outcomes

- [X] T033 [US2] In `backend/internal/order/service.go`, set `ExtRefID: session.ProviderRef` on the fresh-session success response (~line 549). No read needed — the gateway's answer is already in hand (depends on T029)
- [X] T034 [US2] Change `qrResponseFor` in `backend/internal/order/service.go` (~line 557) to take a context and populate `ExtRefID` via `PaymentRecords.ExternalRefForOrder`. A nil provider or a lookup error yields `""` and a log line — never a failed response. This covers **both** rebuilt outcomes: the `409 PAYMENT_ALREADY_STARTED` payload (~line 400) and the lost-race answer (~line 530) (depends on T028, T029)

### Tests

- [X] T035 [P] [US2] In `backend/internal/order/checkout_handler_test.go`: a successful checkout's `200` carries `ext_ref_id` equal to the fake gateway's `ProviderRef`
- [X] T036 [P] [US2] In `backend/internal/order/checkout_handler_test.go`: a retried checkout's `409 PAYMENT_ALREADY_STARTED` payload carries the **same** `ext_ref_id` the original `200` returned
- [X] T037 [P] [US2] In `backend/internal/order/service_test.go`: the lost-race branch returns the **stored** reference, not the caller's own `ProviderRef`. Add a fake `PaymentRecords` alongside the existing `PaymentGateway` stub (which already mints `ProviderRef: "txn-" + req.OrderNumber`)
- [X] T038 [P] [US2] In `backend/internal/order/service_test.go`: a gateway that supplies no reference still produces a payable order, with `ext_ref_id` present and empty (FR-004, FR-010)
- [X] T039 [P] [US2] In `backend/internal/order/checkout_handler_test.go`: every failure response (`400`, `404`, `409` terms, `409` duplicate, `410`, `502`) is unchanged — none gains the field (FR-013)

**Checkpoint**: Both US1 and US2 are complete and independently demonstrable.

---

## Phase 5: User Story 3 - The guest's experience is untouched (Priority: P3)

**Goal**: Prove the negative. "Keep the UI just like today" is a requirement here, not a side
effect, and it is the one most easily broken by accident.

**Independent Test**: Walk the guest purchase journey and confirm no guest-facing surface
renders the reference and no guest-facing layout differs.

- [X] T040 [US3] Add `ext_ref_id: string` to `CheckoutQRResponse` in `frontend/lib/types.ts` for type completeness, and render it **nowhere** (FR-020, FR-021)
- [X] T041 [P] [US3] Confirm the guest order page's existing tests in `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` pass unchanged — no snapshot, copy, or layout difference
- [X] T042 [US3] Audit that no guest-facing surface references the field: the payment screen, order lookup, ticket lookup, the SSE status frames in `frontend/lib/checkout-status.ts`, and the receipt/e-ticket documents and their email in `backend/internal/notification/`

**Checkpoint**: All three stories complete.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Contract documentation and the acceptance gate.

### Documentation

- [X] T043 [P] Add `ext_ref_id` to `components.schemas.CheckoutResponse` in `api/openapi.yml` (~line 2443) with the "always present, empty when unknown, opaque — do not parse" description
- [X] T044 [P] Add `ext_ref_id` to `components.schemas.PaymentNotification` in `api/openapi.yml` (~line 2909), noting it is populated only on the `SESSION_OPENED` marker row

**End-to-end acceptance (Constitution Principle VIII) — NOT optional.** Two covered flows
change. Arrange through the real API and the real signed webhook — never write order, ticket,
or payment state straight into the database, because those writes are also what invalidate the
cache.

- [X] T045 Extend `e2e/specs/guest-purchase.spec.ts`: drive the real journey and intercept the checkout response with `page.waitForResponse(...)`, asserting `data.ext_ref_id === \`stub-eri-${orderNumber}\`` — the stub mints it deterministically at [`gateway-stub.ts:119`](../../e2e/support/gateway-stub.ts#L119) and exposes `externalRefFor(refId)` at line 166, a helper with no callers today
- [X] T046 Add the negative assertion to the same spec: the reference appears nowhere in the payment page's rendered text (FR-020)
- [X] T047 Add a scenario to `e2e/specs/admin-console.spec.ts`: complete a real purchase, sign in, open that order's payment dialog, and assert the gateway reference is displayed, equals the stub's value, and appears exactly once. **This file opens the payment dialog nowhere today** — it is the first coverage that surface has ever had
- [X] T048 Add a scenario covering an order that never reached checkout: the payment dialog renders normally with the absent-value placeholder — no error, no blank, no broken layout (FR-018, SC-006)

### Validation

- [X] T049 Confirm the migration is reversible: `docker compose run --rm migrate down 1 && docker compose run --rm migrate up`
- [X] T050 Run the Go tier green: `cd backend && ./scripts/test.sh ./...`
- [X] T051 Run the frontend tier green: `cd frontend && npx vitest run`
- [X] T052 Run the acceptance suite green: `cd e2e && npm test`
- [X] T053 Run the acceptance suite green **with the cache off**: `cd e2e && E2E_CACHE_ENABLED=false npm test` — this is how Principle VII's kill switch is actually verified
- [X] T054 Walk the manual smoke check in [quickstart.md](quickstart.md), including the DevTools inspection of the `409` retry payload and the `SELECT status, transaction_id, ext_ref_id, created_at FROM payments` cross-check
- [X] T055 Confirm the definition-of-done list at the end of [quickstart.md](quickstart.md) — every box

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies — start immediately
- **Foundational (Phase 2)**: Depends on Setup — **BLOCKS both user stories**
- **US1 (Phase 3)** and **US2 (Phase 4)**: Both depend only on Foundational. Genuinely parallel — US1 touches the payment DTO and the frontend panel, US2 touches the order domain and the composition root. No shared file.
- **US3 (Phase 5)**: Depends on US2 (the type it declares is the one US2 added to the wire)
- **Polish (Phase 6)**: Depends on all three

### Why the write is Foundational rather than part of US1

Both stories read a reference that must already be recorded. Putting
`RecordSessionOpened` + the composition-root call in Phase 2 is what makes US1 and US2
independent of each other instead of US2 depending on US1.

### Critical path within Foundational

```
T004 (migration) ──┐
T007, T008 (SQL) ──┴──► T009 (sqlc generate) ──► T012 (repo lookup)
                                              └─► T010, T011 (domain fields) ──► T014 (RecordSessionOpened) ──► T015 (adapter call)
```

`T009` is the choke point: sqlc replays the migrations directory to build its schema, so the
migration must land before codegen, and nothing typed against the new column compiles before
codegen.

### Parallel Opportunities

- **Phase 2**: T010, T011, T013 in parallel after T009; then all five tests T016–T020 in parallel
- **Phase 3 vs Phase 4**: entire stories in parallel across two developers
- **Phase 3**: T023 and T024 in parallel; T025 waits on T024
- **Phase 4**: T028 and T029 in parallel; the five tests T035–T039 in parallel once T032/T034 land
- **Phase 6**: T043 and T044 in parallel

---

## Parallel Example: Phase 2 after codegen

```bash
# Domain field additions — different structs, no ordering between them:
Task: "Add ExtRefID to PaymentLog + empty→NULL mapping in backend/internal/payment/repository.go"
Task: "Add ExtRefID to PaymentRecord + toPaymentRecords in backend/internal/payment/repository.go"
Task: "Add MarkerSessionOpened const in backend/internal/payment/service.go"

# Then all foundational tests together:
Task: "Repository round-trip + NULL handling test"
Task: "ExternalRefByOrderID returns ("", nil) on no rows test"
Task: "RecordSessionOpened writes one row with the right columns test"
Task: "RecordSessionOpened failure is swallowed test"
Task: "SettlementForOrder regression guard test"
```

## Parallel Example: US1 and US2 across two developers

```bash
# Developer A — Phase 3 (admin, P1, the MVP):
#   backend/internal/payment/reconcile_dto.go
#   frontend/lib/types.ts, frontend/components/admin/order-payment-panel.tsx

# Developer B — Phase 4 (checkout, P2):
#   backend/internal/order/event_provider.go, dto.go, service.go
#   backend/cmd/api/adapters.go, main.go

# No file overlap. Both branch from the Phase 2 checkpoint.
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1: Setup — baseline green
2. Phase 2: Foundational — the column, the row, the write (**blocks everything**)
3. Phase 3: User Story 1 — the admin can read it
4. **STOP and VALIDATE**: check out an order, open it in the console, confirm the reference
   matches the gateway
5. This is shippable on its own. An operator with admin access can already answer the support
   question the feature exists for; US2 is convenience on top.

### Incremental Delivery

1. Setup + Foundational → the reference is being recorded, invisibly
2. + US1 → operators can trace orders (**MVP** — demo here)
3. + US2 → API callers get it without a second request
4. + US3 → the "nothing changed for guests" guarantee is proven, not assumed
5. + Polish → contract docs and the acceptance gate

### Risk Notes

- **T009 is the choke point.** Nothing typed against `ext_ref_id` compiles before `sqlc
  generate` runs, and sqlc needs the migration on disk. Do T004 → T007/T008 → T009 first and
  in that order.
- **T020 guards a real regression.** Adding a row to an append-only log is read by code that
  did not expect it — `SettlementForOrder` walks `payments` newest-first for the receipt's
  payment instrument. The new row is oldest so it should not win, but that is asserted here
  rather than assumed.
- **T015's swallow is deliberate, and contested.** A failed audit write must not destroy a
  payable session, so it logs and continues — which means spec FR-002's "no payable order with
  an unrecorded reference" is reachable on an error path. Flagged in [plan.md](plan.md)'s
  post-design section. If the user wants it fail-closed instead, T015 is the only task that
  changes.
- **T047 adds the first-ever coverage of the admin payment dialog.** Expect to build the page
  object interaction from scratch; `e2e/support/journey.ts`'s `AdminConsole` class is where it
  belongs.

---

## Notes

- `[P]` tasks touch different files and have no ordering between them
- `[Story]` labels map tasks to spec.md's user stories for traceability
- Commit after each task or logical group; `SCHEMA.md` (T006) must ride in the **same** commit
  as the migration (T004/T005)
- `node` is not on `PATH` in this environment and `npx` is intercepted — check the project's
  toolchain notes before running the frontend tiers
- Never write order status, tickets, or payment state straight into the database from a test.
  Arrange through the real API: those writes are also what invalidate the cache, so going
  around them makes the setup lie.

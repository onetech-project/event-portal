# Quickstart: Validating the Payment External Reference ID

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Contract**: [contracts/api.md](contracts/api.md)

How to prove this feature works, at each of the three tiers the constitution requires. This
is a validation guide — implementation belongs in `tasks.md`.

---

## Prerequisites

The runner does not own the infrastructure; bring it up first.

```bash
# Postgres, Redis and Mailpit. Mailpit is not optional — the delivery specs read
# real messages off it.
REDIS_PORT=6380 docker compose up -d postgres redis mailpit
docker compose run --rm migrate up      # includes 000015_payment_external_ref
```

Confirm the migration landed before anything else — every tier below depends on it:

```bash
docker compose exec postgres psql -U postgres -d ticketing \
  -c "\d payments" | grep ext_ref_id
# expect: ext_ref_id | character varying(255) |  |  |
```

Then confirm the down-migration is honest, because a migration that cannot be reversed is a
migration that cannot be tested twice:

```bash
docker compose run --rm migrate down 1 && docker compose run --rm migrate up
```

---

## Tier 1 — Go (unit + database-backed)

```bash
cd backend && ./scripts/test.sh ./...
```

### What must be true

| Check | Where |
|-------|-------|
| A `PaymentLog` carrying `ExtRefID` round-trips through `CreatePayment` → `ListByOrder` | `internal/payment/repository_test.go` |
| An empty `ExtRefID` is stored as `NULL`, and reads back as `""` — not as a literal empty string in the column | `internal/payment/repository_test.go` |
| `ExternalRefForOrder` returns the reference for an order that has a `SESSION_OPENED` row | `internal/payment/repository_test.go` |
| `ExternalRefForOrder` returns `("", nil)` — **not** an error — for an order with no such row | `internal/payment/repository_test.go` |
| `RecordSessionOpened` writes exactly one row, with `status=SESSION_OPENED`, `is_marker` semantics, `transaction_id` falling back to the order number, and the reference in `ext_ref_id` | `internal/payment/service_test.go` |
| A failing `RecordSessionOpened` is swallowed, not propagated — the checkout still succeeds | `internal/payment/service_test.go` |
| `SettlementForOrder` still resolves `Method` and `PaidAt` from the notification rows once a `SESSION_OPENED` row precedes them | `internal/payment/service_test.go` — **regression guard**, see [research.md](research.md) Decision 5 |
| Checkout's `200` carries `ext_ref_id` from the gateway session | `internal/order/checkout_handler_test.go` |
| Checkout's `409 PAYMENT_ALREADY_STARTED` payload carries the **same** `ext_ref_id` | `internal/order/checkout_handler_test.go` |
| The lost-race branch carries the stored reference, not the caller's own | `internal/order/service_test.go` |
| A checkout whose gateway supplies no reference still succeeds, with `ext_ref_id: ""` | `internal/order/service_test.go` |
| `TestNoDomainImportsAnotherDomain` still passes — `internal/order` imports nothing from `internal/payment` | `cmd/api/architecture_test.go` |

The order-side tests need a fake `PaymentRecords`; the existing `service_test.go` already
stubs `PaymentGateway` with `ProviderRef: "txn-" + req.OrderNumber`, so the fake follows that
shape.

---

## Tier 2 — Frontend (logic + components)

```bash
cd frontend && npx vitest run
```

| Check | Where |
|-------|-------|
| The panel renders the order-level reference when one row carries it | `components/admin/order-payment-panel.test.tsx` |
| It renders the absent-value placeholder when no row carries one | same |
| It shows the value **once**, not per notification row | same |
| The `SESSION_OPENED` row renders through the existing marker path — "our note" badge | same |
| Existing panel assertions (holds table, shortfall alert, notification history) still pass unchanged | same |

Note: `node` is not on `PATH` in this environment and `npx` is intercepted — see the
project's toolchain notes before running the frontend tiers.

---

## Tier 3 — Acceptance (the real browser, the real API, real Postgres)

This is the gate, not an optional tier.

```bash
cd frontend && npm run test:e2e        # headless
cd frontend && npm run test:e2e:slow   # headed, slowed for a human to follow
```

And again with the cache off — this is how Principle VII's kill switch is actually verified:

```bash
E2E_CACHE_ENABLED=false npm run test:e2e
```

### Scenario A — the checkout response carries the reference

**File**: `e2e/specs/guest-purchase.spec.ts`

Drive the real journey (browse → book → holder forms → checkout) and intercept the checkout
response in the browser rather than calling the API directly:

```ts
const checkout = page.waitForResponse((r) =>
  r.url().includes("/ticket/checkout/") && r.request().method() === "POST",
);
// …submit the holder forms…
const body = await (await checkout).json();
expect(body.data.ext_ref_id).toBe(`stub-eri-${orderNumber}`);
```

The expected value is deterministic: the stub mints `stub-eri-${refId}` where `refId` is the
order number ([gateway-stub.ts:119](../../e2e/support/gateway-stub.ts#L119)). The stub also
exposes `externalRefFor(refId)` — an accessor with no callers today, added for exactly this
assertion.

**Also assert the negative**, since "keep the UI just like today" is a requirement and not a
side effect: the reference must appear nowhere in the payment page's rendered text.

```ts
expect(await page.locator("body").textContent()).not.toContain(body.data.ext_ref_id);
```

### Scenario B — an operator reads the reference off the order

**File**: `e2e/specs/admin-console.spec.ts`

The payment dialog has **no coverage at all** today — no test in that file opens it — so this
scenario is the first. Arrange through the real API and the real settlement webhook, never by
writing order or payment state into the database directly.

1. Complete a real purchase so a session opens and a `SESSION_OPENED` row exists.
2. Sign in to the admin console and reach the orders list.
3. Open that order's payment dialog.
4. Assert the gateway reference is displayed and equals `stub-eri-${orderNumber}`.
5. Assert it appears **once** — an order-level line, not repeated down the notification rows.
6. Assert the notification history, the seat-holds table, and the orders list are otherwise
   as before.

### Scenario C — an order with no reference degrades cleanly

Open the payment dialog for an order that never reached checkout. The dialog must render
normally with the absent-value placeholder — no error, no blank, no broken layout (spec
FR-018, SC-006).

---

## Manual smoke check

For a human wanting to see it once, end to end:

1. `docker compose up` the stack and run a purchase through to the QR screen.
2. **Look at the payment screen.** It must be indistinguishable from before — this is the
   requirement most easily broken by accident.
3. Open DevTools → Network → the `POST /ticket/checkout/...` response. `data.ext_ref_id` is
   populated.
4. Reload and retry the checkout call. The `409` payload carries the *same* `ext_ref_id`.
5. Sign in to `/admin/orders`, open that order's payment dialog. The reference is shown once,
   in full, and selects cleanly for copy-paste.
6. Cross-check against the database:

```sql
SELECT status, transaction_id, ext_ref_id, created_at
FROM payments WHERE order_id = '<order-uuid>' ORDER BY created_at;
-- expect exactly one SESSION_OPENED row carrying ext_ref_id, oldest;
-- notification rows after it with ext_ref_id NULL
```

---

## Definition of done

- [ ] All three tiers green, and the acceptance suite green in **both** cache modes
- [ ] `SCHEMA.md` updated in the same commit as `000015_payment_external_ref` — standing rule
- [ ] `api/openapi.yml` updated for both response schemas
- [ ] `sqlc generate` re-run and the regenerated `paymentsql` committed
- [ ] `TestNoDomainImportsAnotherDomain` green — the order domain still imports no other domain
- [ ] No guest-facing file changed outside the type declaration in `frontend/lib/types.ts`

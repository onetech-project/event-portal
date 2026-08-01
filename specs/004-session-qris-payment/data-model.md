# Data Model: Persistent Admin Session & In-App QRIS Payment Page

**Feature**: `004-session-qris-payment` | **Date**: 2026-08-01

Source of truth for the database is [SCHEMA.md](../../SCHEMA.md), which this
feature amends. Everything else here is in-memory or client-side state.

---

## 1. Schema change (the only one)

Two nullable columns are added to `orders`. No table is created, renamed, or
dropped; no existing column changes type or constraint.

```sql
-- backend/migrations/0002_payment_qris.sql
ALTER TABLE orders ADD COLUMN payment_qr_string TEXT;
ALTER TABLE orders ADD COLUMN payment_expires_at TIMESTAMP WITH TIME ZONE;

-- The sweeper's only query: "PENDING orders whose deadline has passed".
-- Partial, so it stays roughly the size of the live payment window.
CREATE INDEX idx_orders_payment_expiry
    ON orders (payment_expires_at)
    WHERE status = 'PENDING';
```

`SCHEMA.md` MUST be updated in the same change (Constitution, Technology Stack
Requirements). `sqlc.yaml`'s `schema:` key MUST list both migration files or
generation will not see the new columns.

### `orders` after the change

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | unchanged |
| `order_number` | VARCHAR(100) UNIQUE | unchanged — the guest-facing handle and the provider's `order_id` |
| `buyer_name` / `buyer_email` / `buyer_phone` | VARCHAR | unchanged |
| `total_amount` | NUMERIC(12,2) | unchanged — the amount charged |
| `status` | VARCHAR(50) CHECK | unchanged — `PENDING` \| `PAID` \| `CANCELLED` \| `EXPIRED` |
| `payment_provider` | VARCHAR(50) | unchanged — e.g. `midtrans` |
| `payment_url` | TEXT | **repurposed**: now the provider's `generate-qr-code` action URL (fallback/audit), no longer a page the guest is sent to |
| `payment_qr_string` | TEXT **(new)** | the raw QRIS payload; the only input needed to render the QR image |
| `payment_expires_at` | TIMESTAMPTZ **(new)** | server-owned payment deadline; drives the countdown, the sweeper, and the expired state |
| `email_sent` | BOOLEAN | unchanged |
| `created_at` / `updated_at` | TIMESTAMPTZ | unchanged |

**Validation rules**

- `payment_qr_string` and `payment_expires_at` are set together, in the same
  post-charge transaction that already writes `payment_url` / `payment_provider`.
- Both are `NULL` for orders created before this feature; readers MUST treat
  `NULL` as "no payment instruction available" and never render a QR from it.
- `payment_expires_at` is always `created_at + PAYMENT_EXPIRY` as computed and
  echoed by the provider (`expiry_time`), never computed from a client clock.
- Nothing clears these columns on transition to a final state; the read layer
  suppresses them (see §4) so history stays intact for support.

**Unchanged tables**: `payments` continues to be the append-only audit log of
provider notifications — a reconciliation triggered by the check-status button
writes a row there exactly like a webhook does.

---

## 2. Order lifecycle (with the new deadline)

```
                       ┌──────────────────────────────────────────┐
                       │                                          │
   checkout            │        webhook: settlement /             │
   (TX1 reserves) ──►  PENDING ──── capture+accept ─────────────► PAID ──► tickets
                       │  ▲                                        (issued + emailed,
                       │  │                                         async, once)
                       │  └── webhook: pending  (no-op)
                       │
                       ├── webhook: expire ────────────────────► EXPIRED  (+quota back)
                       ├── sweeper: now > payment_expires_at ──► EXPIRED  (+quota back)
                       ├── check-status: provider says expire,
                       │   or now > payment_expires_at ────────► EXPIRED  (+quota back)
                       │
                       ├── webhook: deny / cancel / failure ───► CANCELLED (+quota back)
                       └── checkout compensation (charge failed) ► CANCELLED (+quota back)
```

**Invariants**

- Every transition out of `PENDING` runs as `UPDATE orders SET status = $new
  WHERE id = $id AND status = 'PENDING'` inside one transaction with the quota
  restoration, so concurrent paths (webhook, sweeper, check-status) cannot both
  apply — exactly one wins and quota moves exactly once (FR-024, FR-025).
- `PAID`, `CANCELLED`, and `EXPIRED` are terminal. A late `settlement` for an
  already-`EXPIRED` order does **not** flip it back; it is logged as an anomaly and
  surfaced to admins (US4 acceptance scenario 5).
- Ticket issuance and email dispatch remain triggered only by an actually-applied
  `PENDING → PAID` transition, so they still happen at most once per order.

---

## 3. In-memory / domain types

These are Go values, not tables. They exist so no provider-specific struct crosses
a domain boundary (Constitution III and V).

### `payment.PaymentSession` (new — returned by `Gateway.CreateTransaction`)

| Field | Type | Meaning |
|---|---|---|
| `ProviderRef` | `string` | provider's `transaction_id`, logged for support |
| `QRString` | `string` | raw QRIS payload → `orders.payment_qr_string` |
| `QRImageURL` | `string` | provider's `generate-qr-code` action URL → `orders.payment_url` |
| `ExpiresAt` | `time.Time` | provider's `expiry_time` → `orders.payment_expires_at` |
| `RedirectURL` | `string` | empty for QRIS; kept so a future gateway that only redirects still fits the interface |

This replaces the bare `string` return of the current `CreateTransaction` on both
`payment.Gateway` and the consumer-declared `order.PaymentGateway`.

### `payment.StatusResult` (new — returned by `Gateway.FetchStatus`)

Structurally identical to the existing `WebhookResult` (`OrderNumber`,
`TransactionID`, `TransactionStatus`, `FraudStatus`, `PaymentType`, `RawPayload`)
so a reconciliation and a notification feed the *same* `MapProviderStatus` →
`applyOutcome` path. Reusing `WebhookResult` directly is acceptable and preferred;
the alias exists only to name the intent at the call site.

### `order.PublicOrderDetail` (new DTO)

The shape behind `GET /api/v1/orders/:orderNumber`. See
[contracts/api.md](./contracts/api.md) for the exact JSON. Composed from:

- `orders` row (own domain),
- `order_items` rows (own domain),
- ticket type display names + event name/slug, resolved through the
  `EventLookup` interface — **never** a JOIN into `ticket_types` / `events`
  (Constitution II). `EventLookup` gains one batched method returning
  `{TicketTypeName, EventName, EventSlug}` per ticket type id, alongside the
  existing `TicketTypeNames` used by the admin views.

### `payment.expirySweep` (new, in-process)

No persisted state. A ticker (default 30s, configurable) calling
`OrderProvider.DueForExpiry(ctx, now, limit)` → `applyOutcome(EXPIRED, restore
quota)` per order. `DueForExpiry` is a new method on the consumer-declared
`OrderProvider` interface, implemented by the order domain against its own table.

---

## 4. Read-model rules for the public order view

The public endpoint is unauthenticated, so what it omits matters as much as what it
returns (FR-022).

**Always returned**: `order_number`, `status`, `total_amount`, `buyer_name`,
`buyer_email`, `created_at`, `server_time`, event `{name, slug}`, and items
`[{ticket_type_name, quantity, unit_price, subtotal}]`.

**Returned only while `status = 'PENDING'` and `payment_expires_at` is in the
future**: the `payment` object (`method: "QRIS"`, `provider`, `amount`,
`expires_at`, `qr_image_path`). Suppressed otherwise — a paid, cancelled, or
expired order returns `payment: null` (FR-014).

**Never returned**: `tickets.ticket_code` or any ticket row, attendee lists, the
provider's `transaction_id`, `payment_qr_string` of any other order, admin fields,
or anything from another order. The endpoint is keyed strictly by
`order_number`; an unknown number returns `404` with no distinction between "never
existed" and "not yours".

**Attendees**: deliberately excluded. The guest typed them in and does not need
them echoed on the payment page, and including them would widen what an
order-number leak exposes.

---

## 5. Client-side session state (frontend)

Not persisted server-side; this is the state machine the admin guard runs.

| State | When | Renders | Redirects |
|---|---|---|---|
| `loading` | server render + hydration render, before `localStorage` is read | neutral loading placeholder | no |
| `authenticated` | stored token present and `expires_at` in the future | admin nav + page | no |
| `anonymous` | no token, unreadable/tampered token, expired token, or a server `401` | nothing | `/admin/login?next=<path>` (+ `reason=expired` when a token was discarded) |

**Transitions**

- `loading → authenticated | anonymous`: once, after mount.
- `authenticated → anonymous`: sign-out (this tab, via an in-page event), a
  `storage` event from another tab, or `adminFetch` receiving a `401` and clearing
  the token.
- `anonymous → authenticated`: successful login writes the token, then navigates to
  `next` (defaulting to the admin home).

The stored entry keeps its existing shape — `{ token, expiresAt }` under
`ticketing.admin.token` — so no migration of stored sessions is required.

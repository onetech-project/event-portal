# Phase 1 Data Model: Payment External Reference ID

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Research**: [research.md](research.md)

One nullable column, one new row kind, one new read. Nothing is dropped, renamed, or
back-filled.

---

## 1. Schema change

### Migration `000015_payment_external_ref`

```sql
-- up
ALTER TABLE payments ADD COLUMN ext_ref_id VARCHAR(255);

-- down
ALTER TABLE payments DROP COLUMN ext_ref_id;
```

Nullable, no default, no constraint, no index. Each of those is a deliberate omission:

| Omission | Why |
|----------|-----|
| `NOT NULL` | Every existing row predates the column and no value can be reconstructed for them (spec: nothing is back-filled). Notification rows written after this change also carry no reference — the gateway does not send `eri` on callbacks — so the column is legitimately null on most rows forever. |
| `DEFAULT ''` | An empty string would be indistinguishable from "the gateway sent a blank reference", which spec FR-004 requires be distinguishable from "no session-open row here". |
| `UNIQUE` | The reference is unique *per gateway*, which this system cannot assert across a provider swap (Principle V has already been exercised once). A uniqueness violation would fail a checkout for an audit-record reason. |
| Index | `payments` is only ever read by `order_id`, which the existing queries already filter on, and per-order row counts are single digits. Speculative indexing is scope the constitution's Principle VI discourages. |

**`SCHEMA.md` MUST change in the same commit** — the repository's standing rule. The
`payments` block gains:

```sql
    ext_ref_id VARCHAR(255), -- gateway's external reference (eri) for the payment session;
                             -- set only on the SESSION_OPENED row, NULL on notification rows
```

### `SCHEMA.md` narrative to add

The payments section should state, alongside the existing note about `status` holding the
provider's raw value, that `ext_ref_id` is populated on exactly one row per order — the
`SESSION_OPENED` marker — and is null everywhere else, because the gateway supplies the
reference only when the session is opened and never on a callback.

---

## 2. The `SESSION_OPENED` row

A new marker in the same series as the existing four
([service.go:80-93](../../backend/internal/payment/service.go#L80)).

```go
// MarkerSessionOpened records the moment the gateway issued a code for this
// order, and carries the external reference it issued it under. It is the exact
// counterpart of MarkerSessionDuplicate: one is written when the gateway agrees
// to open a session, the other when it refuses to open a second.
//
// It is also the row SettlementForOrder has always described itself as reading.
MarkerSessionOpened = "SESSION_OPENED"
```

| Column | Value | Note |
|--------|-------|------|
| `order_id` | the order being checked out | |
| `provider` | `gateway.Name()` | same source as every other row |
| `transaction_id` | the **order number** | The gateway's network transaction id does not exist yet — it arrives on settlement. The column is `NOT NULL`, and `writeMarker` already falls back to the order number rather than writing a blank that would read as a real, empty gateway id ([service.go:498](../../backend/internal/payment/service.go#L498)). |
| `payment_type` | `"qris"` | The instrument, matching what `ReleaseDuplicateSession` writes on its sibling row. This is what makes `SettlementForOrder`'s "the session-open row carries the instrument" true. |
| `status` | `SESSION_OPENED` | A marker: this system's own statement, not a gateway status. Surfaces with the existing "our note" badge. |
| `ext_ref_id` | the gateway's `eri` | **The point of the feature.** Empty gateway value → stored as `NULL`, not `''`. |
| `raw_response` | marker envelope (below) | |
| `created_at` | default `CURRENT_TIMESTAMP` | The session-open timestamp — the start of the support timeline. |

### Marker envelope (`raw_response`)

Follows the existing envelope shape from `writeMarker`, carrying what is true at session open
and unrecoverable afterwards:

```json
{
  "marker": "SESSION_OPENED",
  "raised_at": "2026-08-14T02:43:09Z",
  "ext_ref_id": "A487336098162400838C",
  "expires_at": "2026-08-14T02:43:09Z",
  "expiry_from_gateway": true
}
```

`expiry_from_gateway` is included because it is already computed at this moment
(`PaymentSession.ExpiryFromGateway`) and answers, on the order's own record, whether the
deadline is one the gateway agreed to — today that fact reaches only the logs.

### Cardinality

**Exactly one per order** under the current gateway, which refuses a second session for a
reference it has already issued. See [research.md](research.md) Decision 2 for the race that
could in principle produce two, why it is unreachable here, and why it is accepted rather than
designed around.

---

## 3. Go types

### `payment` domain

```go
// PaymentLog — the write shape. Gains:
    // ExtRefID is the gateway's own reference for the payment session, known only
    // at session open. Empty on every notification row: callbacks do not carry it.
    ExtRefID string

// PaymentRecord — the read shape. Gains the same field.
```

Both are hand-written domain types in
[repository.go](../../backend/internal/payment/repository.go); the sqlc `paymentsql.Payment`
struct gains `ExtRefID *string` by regeneration and stays inside the repository (Principle III).

`CreatePayment` maps empty string → `NULL`, exactly as it already does for `PaymentType`.

### `order` domain

```go
// CheckoutQRResponse gains:
    // ExtRefID is the gateway's own reference for this payment session, carried so
    // a caller can match the order against the gateway's records without a second
    // request. Empty when no session has been opened or the gateway supplied none —
    // present-and-empty rather than absent, so callers never distinguish the two.
    ExtRefID string `json:"ext_ref_id"`

// PaymentRecords is the consumer-declared seam (research.md Decision 3). Read-only:
// the write happens in the composition root where the gateway answers, so checkout
// only ever asks what was recorded.
type PaymentRecords interface {
    ExternalRefForOrder(ctx context.Context, orderID uuid.UUID) (string, error)
}
```

`order` still imports nothing from `internal/payment`.

### New service methods (`payment` domain)

```go
// RecordSessionOpened writes the SESSION_OPENED row. Called from the composition
// root's payment adapter the moment the gateway issues a code — mirroring
// ReleaseDuplicateSession, which the same adapter calls when it refuses to.
//
// Errors are returned but the caller logs and swallows them: the gateway has
// already made the order payable, and an audit write must never destroy a live
// payment session (research.md Decision 2).
func (s *Service) RecordSessionOpened(ctx context.Context, orderNumber string, session PaymentSession) error

// ExternalRefForOrder answers what reference this order's session was opened under.
// No row is a normal answer: ("", nil), never an error.
func (s *Service) ExternalRefForOrder(ctx context.Context, orderID uuid.UUID) (string, error)
```

---

## 4. Queries

### Changed

```sql
-- name: CreatePayment :one
INSERT INTO payments (order_id, provider, transaction_id, payment_type, status, raw_response, ext_ref_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, order_id, provider, transaction_id, payment_type, status, created_at;

-- name: ListPaymentsByOrderID :many
SELECT id, order_id, provider, transaction_id, payment_type, status, raw_response, ext_ref_id, created_at
FROM payments
WHERE order_id = $1
ORDER BY created_at DESC;
```

### New

```sql
-- name: GetExternalRefByOrderID :one
-- The gateway's reference for this order's payment session. Only the SESSION_OPENED
-- row carries one, so this resolves to that row — newest first, defensively, for the
-- case a gateway that permits duplicate references ever produces two.
--
-- No rows is a normal answer, not an error: an order whose session never opened, or
-- one predating this column, simply has no reference, and the checkout response
-- carries the field empty rather than absent.
SELECT ext_ref_id FROM payments
WHERE order_id = $1 AND ext_ref_id IS NOT NULL
ORDER BY created_at DESC
LIMIT 1;
```

`pgx.ErrNoRows` is translated to `("", nil)` at the repository boundary.

---

## 5. Read/write paths

```
CHECKOUT (order domain)
  ├─ early guard: payment_qr_string != ""  ──► 409 + qrResponseFor(ctx, ord)
  │                                                └─ PaymentRecords.ExternalRefForOrder
  ├─ TX-D  primary contact + slots                    (no gateway call yet)
  ├─ gateway.CreateTransaction  ─────────────► cmd/api adapter
  │                                              ├─ success ─► payment.Service.RecordSessionOpened
  │                                              │               └─ INSERT payments (SESSION_OPENED, ext_ref_id)
  │                                              └─ duplicate ─► payment.Service.ReleaseDuplicateSession  (unchanged)
  ├─ TX-P  stamp qr_string / expires_at / provider   + orders-scope cache invalidation (unchanged)
  ├─ !stamped (lost race) ──► re-read order, qrResponseFor(ctx, current)
  │                             └─ PaymentRecords.ExternalRefForOrder
  └─ stamped ──► response with ExtRefID = session.ProviderRef   (no read needed)

ADMIN (payment domain)
  GET /admin/payment/order/:id/notifications
    └─ ListByOrder ──► NotificationResponse[] each with ext_ref_id
                        └─ panel derives the order-level value from the one non-empty row
```

Every write and read above sits **outside** all transactions. The `INSERT` happens between
TX-D and TX-P, holding no quota row lock, satisfying spec FR-006 and Principle IV.

---

## 6. State and invariants

The reference has no lifecycle — that is its defining property.

| Invariant | Enforced by |
|-----------|-------------|
| Written exactly once, at session open | Only `RecordSessionOpened` sets the column; the retry guard returns `409` before any second gateway call |
| Never updated | `payments` is append-only; no `UPDATE` statement in the domain touches it |
| Never cleared by settlement, expiry, cancellation, or redelivery | Those paths write *new* rows and never modify old ones |
| Null is a legitimate, readable state | Column nullable; lookup returns `("", nil)` on no rows; wire field present-and-empty (FR-010) |
| Opaque | Never parsed, validated, compared, or derived from — only stored, returned, and displayed |

## 7. Entities not changed

`orders`, `order_items`, `attendees`, `tickets`, `ticket_types`, and every quota, fee, status,
and deadline rule are untouched. This feature adds one column to one table and reads it back.

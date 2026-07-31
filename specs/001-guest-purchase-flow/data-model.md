# Phase 1 Data Model: Guest Purchase Flow

Entities are drawn from SCHEMA.md; only fields/rules relevant to this feature are
detailed. Full column definitions (types, defaults) live in SCHEMA.md — this
document adds the feature-specific validation and lifecycle rules from spec.md.

## Event (read-only in this feature)

Full CRUD owned by the Admin Management feature. Here it is read-only.

- `id`, `name`, `slug`, `description`, `venue`, `address`, `start_date`,
  `end_date`, `banner_url`, `status`
- **Feature rule (FR-001)**: only rows with `status = 'PUBLISHED'` are returned to
  guest-facing list/detail endpoints.
- `banner_url` is a plain URL string supplied by an admin and stored verbatim. There
  is no file upload, object storage, or static-file serving anywhere in this MVP;
  this feature only reads the column and passes it through to the client. It may be
  `NULL`, which clients MUST render as "no banner".

## Ticket Type (read + quota mutation in this feature)

Full CRUD owned by Admin Management. Here it is read plus quota deduction/restore.

- `id`, `event_id`, `name`, `price`, `quota`, `sales_start`, `sales_end`
- **`quota` is the REMAINING quota, not a total (FR-018)**. It is a live counter:
  decremented at checkout, incremented back on Cancelled/Expired (including
  deny/failure) and on checkout compensation. SCHEMA.md is locked, so no
  total/sold/reserved companion column exists — remaining availability is read
  directly from `quota`, and the original on-sale capacity is not stored anywhere.
  Any other feature that writes this column (notably Admin Management) is editing
  *remaining* stock, not capacity.
- **Feature rule (FR-005)**: a checkout selection is only valid when
  `sales_start <= now() <= sales_end`.
- **Feature rule (FR-006)**: `quota` MUST be deducted atomically at checkout
  (`UPDATE ticket_types SET quota = quota - $qty WHERE id = $id AND quota >= $qty
  RETURNING quota`) and restored atomically on Cancelled/Expired
  (`SET quota = quota + $qty`); never allowed below 0 (DB `CHECK (quota >= 0)`
  constraint is the backstop, the guarded `UPDATE` is the primary guard).

## Order

- `id`, `order_number` (unique, generated), `buyer_name`, `buyer_email`,
  `buyer_phone`, `total_amount`, `status`, `payment_provider`, `payment_url`,
  `email_sent`
- **Status lifecycle**: `PENDING` → `PAID` | `CANCELLED` | `EXPIRED`. No other
  transitions are valid; a webhook update to an order already in a terminal state
  other than the requested one is rejected/ignored per FR-012. The DB
  `CHECK (status IN ('PENDING','PAID','CANCELLED','EXPIRED'))` permits no other
  value, which is why denied and failed payments both fold into `CANCELLED`
  (FR-019) — see State Transition Summary below.
- **Write ordering (FR-021)**: the row is inserted in TX1 with `status = 'PENDING'`,
  `payment_url = NULL`, and `payment_provider = NULL`, together with its
  `order_items`, `attendees`, and the quota deduction. `payment_url` /
  `payment_provider` are stamped by a separate TX2 after the gateway call returns.
  If the gateway call fails, a compensating transaction sets `status = 'CANCELLED'`
  and restores the quota. Consumers MUST therefore tolerate a `PENDING` order whose
  `payment_url` is still `NULL`.
- **Feature rule (FR-008)**: `order_number` generated at creation, unique.
- **Feature rule (FR-009)**: `total_amount` is server-computed as the sum of
  `order_items.price * order_items.quantity` using current `ticket_types.price` at
  checkout time — never trusts a client-supplied total.

## Order Item

- `id`, `order_id`, `ticket_type_id`, `quantity`, `price` (snapshot of ticket
  type's price at purchase time)
- One row per distinct ticket type selected in a single checkout.

## Attendee

- `id`, `order_id`, `ticket_type_id`, `name`, `email`
- **Feature rule (FR-003/FR-004)**: exactly one Attendee row per unit of quantity
  across all Order Items in the order; count MUST match total quantity or checkout
  is rejected before any writes occur.

## Ticket

- `id`, `ticket_code` (unique, generated), `order_id`, `attendee_id`, `status`
  (`ACTIVE` | `USED` | `REVOKED`), `qr_code_url`
- **Feature rule (FR-013)**: created only when the parent order transitions to
  `PAID`; exactly one per Attendee row on that order; initial status `ACTIVE`.
- **`ticket_code` format (canonical at write time)**: uppercase, fixed length, drawn
  from an unambiguous charset that excludes `I`, `O`, `0`, `1`. Because every stored
  value is already canonical, lookups normalize the input (trim + uppercase) in
  application code and then match **exactly** (`WHERE ticket_code = $1`) so that
  `idx_tickets_ticket_code` is used. `WHERE UPPER(ticket_code) = UPPER($1)` MUST NOT
  be used — SCHEMA.md is locked, no functional index on `UPPER(ticket_code)` exists,
  and that form forces a sequential scan.
- **`qr_code_url` is left NULL/empty in this MVP (FR-022)**. Ticket generation
  persists only `ticket_code`; the QR image is rendered on demand from that code at
  PDF render time (initial delivery, resend, and any future download-QR feature).
  There is no object storage, static file serving, or upload endpoint in the stack,
  so no value could legitimately be stored here. Consumers MUST NOT read this column.
- Lookup by `ticket_code` (FR-016) is available to guests without authentication, is
  rate limited per client (FR-020), and returns only `ticket_code`, `status`,
  `event_name`, and `attendee_name` — never the attendee's email or any other buyer
  data. Mutation of `status` to `USED`/`REVOKED` is out of scope for this feature
  (owned by the Admin Ticket Validation feature).

## Payment

- `id`, `order_id`, `provider`, `transaction_id`, `payment_type`, `status`,
  `raw_response`
- One row appended per payment provider notification received for an order
  (append-only log used to drive the Order status transition and for auditability).
- `status` stores the provider's **raw** status verbatim (`settlement`, `capture`,
  `deny`, `failure`, ...). It is deliberately NOT constrained to the four `orders`
  statuses, so the distinction between `deny` and `failure` survives even though both
  map the order to `CANCELLED`.

## Relationships

```text
Event (1) ──< Ticket Type (many)
Order (1) ──< Order Item (many) >── Ticket Type (1)
Order (1) ──< Attendee (many) >── Ticket Type (1)
Order (1) ──< Ticket (many, post-PAID) >── Attendee (1, exactly one each)
Order (1) ──< Payment (many, one per webhook notification)
```

## State Transition Summary

Driven by the Payment Status Mapping in spec.md (mirrored in `contracts/api.md` and
`research.md`). Every provider status has an entry — none may be silently ignored.

| Provider status (Midtrans) | Condition | `orders.status` result | `ticket_types.quota` |
|---|---|---|---|
| *(any)* | order already `PAID` | unchanged | none — return `200 OK`, no reprocessing (FR-012) |
| `settlement` | — | `PENDING` → `PAID` | unchanged (deducted at checkout) |
| `capture` | `fraud_status = accept` | `PENDING` → `PAID` | unchanged (deducted at checkout) |
| `capture` | `fraud_status = challenge` | unchanged `PENDING` | unchanged — await resolution |
| `pending` | — | unchanged `PENDING` | unchanged — explicit no-op |
| `deny` | — | `PENDING` → `CANCELLED` (FR-019) | `+ qty` restored |
| `cancel` | — | `PENDING` → `CANCELLED` | `+ qty` restored |
| `expire` | — | `PENDING` → `EXPIRED` | `+ qty` restored |
| `failure` | — | `PENDING` → `CANCELLED` (FR-019) | `+ qty` restored |
| *(none — gateway call failed at checkout)* | compensation, FR-021 | `PENDING` → `CANCELLED` | `+ qty` restored |

Restoring transitions MUST be guarded (`... WHERE status = 'PENDING'`) so a repeated
cancel/expire notification cannot restore the same quota twice.

```text
Order.status:  PENDING --(settlement | capture+accept)--> PAID
               PENDING --(expire)--> EXPIRED                 [+ restore quota]
               PENDING --(cancel | deny | failure)--> CANCELLED [+ restore quota]
               PENDING --(gateway CreateTransaction failed)--> CANCELLED [+ restore quota]
               PENDING --(pending | capture+challenge)--> PENDING (no-op)
               PAID --(any further webhook)--> no-op, 200 OK (FR-012)

Ticket.status: (created ACTIVE on Order -> PAID transition; USED/REVOKED
                transitions belong to the Admin Ticket Validation feature)
```

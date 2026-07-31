# API Contracts: Guest Purchase Flow

Base path: `/api/v1`. All endpoints in this feature are public (no auth).

## GET /events

List published events.

**Response 200**:
```json
[
  {
    "id": "uuid",
    "name": "string",
    "slug": "string",
    "venue": "string",
    "address": "string",
    "start_date": "RFC3339",
    "end_date": "RFC3339",
    "banner_url": "string|null"
  }
]
```
`banner_url` is a plain, admin-supplied image URL echoed back verbatim (may be
`null`). The platform never uploads, stores, or serves banner images — there is no
upload endpoint in this MVP.

## GET /events/:slug

Event detail with its ticket types (only if event status is PUBLISHED; otherwise 404).

**Response 200**:
```json
{
  "id": "uuid",
  "name": "string",
  "slug": "string",
  "description": "string|null",
  "venue": "string",
  "address": "string",
  "start_date": "RFC3339",
  "end_date": "RFC3339",
  "banner_url": "string|null",
  "ticket_types": [
    {
      "id": "uuid",
      "name": "string",
      "price": "decimal string",
      "quota_remaining": "int",
      "sales_start": "RFC3339",
      "sales_end": "RFC3339"
    }
  ]
}
```
`quota_remaining` maps directly and 1:1 onto `ticket_types.quota`, which stores the
**remaining** quota (it is decremented at checkout and restored on
cancel/expire/deny — see spec FR-018). No arithmetic against a total is performed,
because no total is stored. `0` means sold out.

**Response 404**: event not found or not published.

## POST /checkout

**Request**:
```json
{
  "buyer_name": "string",
  "buyer_email": "string",
  "buyer_phone": "string",
  "items": [
    { "ticket_type_id": "uuid", "quantity": "int >= 1" }
  ],
  "attendees": [
    { "ticket_type_id": "uuid", "name": "string", "email": "string" }
  ]
}
```
Validation (see spec FR-003/004/005/006/009):
- `sum(items[].quantity) == len(attendees)`, and each attendee's `ticket_type_id`
  count matches that item's quantity.
- Every referenced `ticket_type_id` must belong to a PUBLISHED event and be within
  its sales window.
- Server recomputes `total_amount` from current ticket type prices; client does not
  send a total.

Server-side sequence (see research.md "Checkout transaction shape"): **TX1** commits
the quota deduction + `orders` (`PENDING`, `payment_url` NULL) + `order_items` +
`attendees` atomically; the gateway `CreateTransaction` call is then made **outside
any transaction**; **TX2** stamps `payment_url`/`payment_provider` and the response
is returned. A `201` is only ever emitted after TX2 commits, so `payment_url` in the
response body is always non-empty.

**Response 201**:
```json
{
  "order_number": "string",
  "status": "PENDING",
  "total_amount": "decimal string",
  "payment_url": "string"
}
```
**Response 400**: validation error (attendee/quantity mismatch, invalid sales
window, insufficient quota) — body includes a machine-readable `error_code` and
human-readable `message`.

**Response 502**: payment initiation with the provider failed after quota was
reserved. The server has already compensated — the order is `CANCELLED` and the
reserved quota is restored — so the guest may safely retry (spec FR-021). Body uses
the same `error_code`/`message` shape, e.g. `PAYMENT_INITIATION_FAILED`.

## POST /payment/webhook/:provider

Called by the payment gateway. Signature/payload shape is provider-specific
(Midtrans SNAP notification format); handler verifies via
`Gateway.VerifyWebhook`.

### Payment status mapping

Every notified provider status maps to exactly one outcome — none may be ignored.
Mirrors the canonical table in spec.md ("Payment Status Mapping", FR-011/FR-019):

| `transaction_status` | `fraud_status` | `orders.status` | Quota |
|---|---|---|---|
| *(any)* | *(any)* | order already `PAID` → unchanged | none — return `200 OK` immediately, no reprocessing |
| `settlement` | — | `PAID` | unchanged (deducted at checkout) |
| `capture` | `accept` | `PAID` | unchanged (deducted at checkout) |
| `capture` | `challenge` | unchanged `PENDING` | unchanged — await a definitive notification |
| `pending` | — | unchanged `PENDING` | unchanged — explicit no-op |
| `deny` | — | `CANCELLED` | restored |
| `cancel` | — | `CANCELLED` | restored |
| `expire` | — | `EXPIRED` | restored |
| `failure` | — | `CANCELLED` | restored |

`orders.status` is constrained by `CHECK (status IN ('PENDING','PAID','CANCELLED',
'EXPIRED'))` and SCHEMA.md is locked, so `deny` and `failure` both fold into
`CANCELLED`; the provider's raw status is preserved in `payments.status` /
`payments.raw_response`. Status changes and their quota restoration happen in one
transaction, guarded on the order still being `PENDING` so a replayed notification
cannot restore quota twice.

**Response 200**: always, once the notification is authenticated and either
processed or recognized as a no-op (order already PAID, a `pending`/`challenge`
status, or order not found — still acknowledged with 200 per gateway integration
convention to stop retries, logged internally as an anomaly).
**Response 401**: signature verification failed.

## GET /tickets/:code

Public and unauthenticated (spec FR-016/FR-017). `:code` is normalized by the server
(trim + uppercase) and then matched **exactly** against `tickets.ticket_code`, which
is always stored in canonical uppercase form so `idx_tickets_ticket_code` is used.

**Rate limited** per client IP (spec FR-020) to make ticket-code enumeration
impractical.

**Response 200**:
```json
{
  "ticket_code": "string",
  "status": "ACTIVE|USED|REVOKED",
  "event_name": "string",
  "attendee_name": "string"
}
```
This is the complete response body. It deliberately excludes the attendee's email,
the buyer's name/email/phone, the order number, and pricing — the endpoint is
unauthenticated, so it discloses only what the code holder already knows. No QR image
or `qr_code_url` is returned: `tickets.qr_code_url` is always NULL in this MVP and the
QR is rendered on demand from `ticket_code` at PDF render time (spec FR-022).

**Response 404**: no ticket with that code (flat response — it MUST NOT distinguish
"malformed code" from "unknown code").
**Response 429**: rate limit exceeded.

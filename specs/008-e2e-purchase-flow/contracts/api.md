# API Contracts: End-to-End Guest Purchase Flow

> **Superseded by [spec 012](../../012-manjo-payment-gateway/spec.md) (2026-08-10).**
> The payment gateway changed from Midtrans to Manjo, and three capabilities described
> below were withdrawn rather than reimplemented: the mid-window QR refresh
> (`POST /ticket/checkout/:order_id/refresh-qr` and `qr_refresh_after_seconds`), the
> guest's status-check button (`POST /ticket/order/:order_id/payment/refresh`), and
> the provider status query that backed it. The payment deadline is now the gateway's
> own (`qr_ea`), adopted verbatim, so one order gets one code for one window. Read the
> sections below as history.

**Feature**: 008-e2e-purchase-flow · Base path `/api/v1`

**Conventions** (clarifications 2026-08-05):
- Guest paths follow the user-mandated endpoint list verbatim.
- **Every endpoint — guest and admin — wraps its body in the envelope**
  `{ "code": number, "message": string, "data": T }` with **snake_case**
  property names throughout `data`. Success is `code: 200000`; error codes are
  `HTTP status × 1000 + sub-code` (registry below). The HTTP status line
  always mirrors the envelope's thousands digit.
- Exceptions (not JSON bodies): SSE frames carry raw snake_case objects per
  event (no envelope — the envelope is for request/response bodies);
  `qris.png` is binary.
- Request bodies are snake_case too, matching the response style.
- Implementation: one envelope writer + rewritten error handler in
  `pkg/httpx`/`pkg/apperr`; DTO json tags retagged snake_case; the frontend
  API client (guest **and** admin) migrates in the same change.

**Identifiers**: `:id` / `:event_id` is the event's public identifier (slug);
`:order_id` is the public order number — never database serials.

## Error code registry (this feature's surface)

| code | HTTP | Meaning | `data` |
|------|------|---------|--------|
| 200000 | 2xx | Success | payload |
| 400001 | 400 | Validation failed | `{ "fields": { "<field>": "<problem>" } }` |
| 400002 | 400 | Insufficient quota | null |
| 400003 | 400 | Terms not accepted (`agreed != true`) | null |
| 400004 | 400 | Unknown icon key (admin) | null |
| 401001 | 401 | Admin auth required/failed | null |
| 404001 | 404 | Resource not found (event/order/ticket) | null |
| 404002 | 404 | Terms not authored (GET terms) | null |
| 409001 | 409 | Terms missing — booking refused | null |
| 409002 | 409 | Terms changed — dialog must refetch | null |
| 409003 | 409 | Terms not recorded — checkout refused | null |
| 409004 | 409 | Payment already started | current QR payload (safe retry) |
| 409005 | 409 | Payment not started (refresh/status misuse) | null |
| 410001 | 410 | Order expired | null |
| 429001 | 429 | Rate limited | null |
| 500000 | 500 | Internal error | null |
| 502001 | 502 | Payment initiation failed | null |

## Guest surface (unauthenticated)

### 1. GET /event
Grid data for the homepage (existing list handler, remounted on this path).

```jsonc
{ "code": 200000, "message": "Success", "data": [ { "id": "…", "name": "…",
  "slug": "…", "venue": "…", "start_date": "…", "end_date": "…",
  "banner_url": "…", "status": "PUBLISHED" } ] }
```

### 2. GET /event/:id
Event detail — **content only** (clarification 2026-08-05): no ticket or
package data.

```jsonc
{ "code": 200000, "message": "Success", "data": {
  "id": "…", "name": "…", "slug": "…",
  "description": "<p>…</p>",              // sanitized HTML
  "venue": "…", "address": "…",
  "start_date": "…", "end_date": "…", "banner_url": "…", "status": "PUBLISHED",
  "activities":  [{ "title": "…", "description": "…", "icon": "music" }],   // position-ordered
  "guest_stars": [{ "name": "…" }],
  "guidelines":  [{ "description": "…", "icon": "no-smoking" }],
  "has_terms": true                        // false disables Buy Ticket with notice
} }
```

### 3. GET /ticket/:event_id
Position/price-ordered sellable ticket types with live remaining quota —
existing ticket-type DTO shape retagged snake_case, fetched with
`cache: 'no-store'` (live inventory). Used only by the ticket selection page.

### 4. GET /packages/:event_id
Active packages with derived availability — existing package DTO shape
retagged snake_case. Used only by the ticket selection page, queried alongside
`GET /ticket/:event_id`.

### 5. POST /ticket/book — **Book** (research R1)
Fired on T&C "Agree" (immediately followed by call 7). Creates the order and
locks the seats; **no gateway call**, no buyer/visitor details yet.

```jsonc
{ "event_id": "…", "items": [ { "ticket_type_id": "…", "quantity": 2 },
                              { "package_id": "…", "quantity": 1 } ] }
```

Response `201`:
```jsonc
{ "code": 200000, "message": "Success", "data": {
  "order_id": "…", "status": "PENDING", "total_amount": "…", "expires_at": "…"
} }
```
Runs the reservation transaction: order (`PENDING`, payment fields NULL,
`expires_at = now+1h`) + order items + empty attendee slots + atomic quota
deduction. Errors: 409001 (no authored terms — nothing to agree to) · 400002 ·
404001.

### 6. GET /ticket/terms-condition/:event_id
```jsonc
{ "code": 200000, "message": "Success", "data": {
  "id": "…", "content": "<ol>…</ol>", "updated_at": "…" } }
```
404002 when no terms authored. Feeds the T&C dialog.

### 7. POST /ticket/terms-condition/:order_id — **Record agreement**
Fired right after call 5 succeeds, same "Agree" click.

```jsonc
{ "agreed": true, "event_terms_id": "…" }    // id the dialog displayed
```
`200` — stamps `terms_agreed_at` + `event_terms_id` on the order. Idempotent.
Errors: 400003 · 409002 (id no longer current → dialog refetches call 6) ·
410001.

Failure between calls 5 and 7 leaves a held order without agreement: checkout
(call 8) refuses it with 409003, the frontend retries call 7, and the 1-hour
sweeper cleans up abandoned cases.

### 8. POST /ticket/checkout/:order_id — **Save forms + start payment** (Option B)
One call: carries the buyer + visitor forms, then starts payment. Details are
not persisted server-side before this call — a revisit before checkout shows
empty forms.

```jsonc
{
  "buyer_name": "…", "buyer_email": "…", "buyer_phone": "…",
  "buyer_dob": "1995-05-05", "buyer_gender": "FEMALE",   // same field set as a visitor (Figma 12-4456)
  "attendees": [ { "id": "…",              // slot id from GET /ticket/order/:order_id
                   "name": "…", "email": "…", "phone": "…",
                   "dob": "2000-01-31", "gender": "FEMALE" } ]
}
```

Gender values (buyer and visitors) must be active rows of the gender master
list (`GET /ticket/genders`); anything else lands in the 400001 field map.

Sequence: validate forms → save buyer + slots (local TX) → gateway
`CreateTransaction` (outside any TX) → stamp payment fields,
`expires_at = now+14m`.

`200`:
```jsonc
{ "code": 200000, "message": "Success", "data": {
  "order_id": "…", "qr_string": "…", "expires_at": "…",
  "qr_image_url": "/api/v1/ticket/order/…/qris.png",
  "qr_refresh_after_seconds": 420
} }
```
Errors: 400001 (field map) · 409003 · 409004 (data = current QR payload — safe
retry) · 410001 · 502001 (order stays PENDING, saved forms retained, retry
allowed).

### 8b. POST /ticket/checkout/:order_id/refresh-qr — **7-minute refresh** (research R3)
Not in the original list; required by FR-015. Re-issues the QR under a suffixed
provider reference; deadline unchanged. `200`: same `data` as call 8. Errors:
409005 · 410001 · 502001 (old QR remains shown client-side).
Rate limit: existing payment-refresh per-IP group.

### 9. GET /ticket/checkout/:order_id/status — **SSE** (research R4)
`Content-Type: text/event-stream`. Frames are raw snake_case objects — no
envelope.
```
event: status
data: {"status":"PENDING","paid_at":null,"expires_at":"…"}

event: status
data: {"status":"PAID","paid_at":"…"}
```
Initial snapshot on connect; keep-alive comment every 25s; server closes after
terminal status (PAID/EXPIRED/CANCELLED). Client falls back to 3s polling of
GET /ticket/order/:order_id on stream error.

### 10. POST /ticket/resend-email
```jsonc
{ "order_id": "…" }
```
`200` on dispatch. 409 (mapped sub-code) for unpaid/expired orders. Rate
limited per order (existing limiter; keyed on body order id).

### Supporting reads (kept, renamed into the namespace)

- `GET /ticket/order/:order_id` — order detail for screen routing and revisit
  (`data`): `status`, `expires_at`, `terms_agreed_at`, `payment_started`,
  totals, `event` (`name`, `slug`, `venue`, `address`, `start_date`,
  `end_date` — the registration page's Order Summary event box, Figma
  12-4456), slot list (`id`, `ticket_type_name`, `package_name?`, details or
  nulls), payment/QR fields when started.
- `GET /ticket/order/:order_id/qris.png` — QR image rendered from
  `payment_qr_string` (existing renderer, new path; binary, no envelope).
- `GET /tickets/:code` — public ticket verification (existing handler, response
  re-wrapped in the envelope; also serves the homepage aside card).
- `GET /ticket/genders` — the gender master list (`data`: `[{id, name}]`).
  The registration forms build their gender options from it (clarified
  2026-08-05); `name` is the canonical value submitted back on checkout.
- `GET /ticket/order/:order_id` also carries `subtotal` (pre-fee sum, null on
  pre-fee orders) and `fees: [{name, amount}]` — the breakdown frozen at
  booking (clarified 2026-08-05, Figma 32-1366). Booking's `total_amount`
  equals subtotal + fees.
- Admin fee master CRUD (JWT): `GET/POST /admin/fees`,
  `PUT/DELETE /admin/fees/:id` with body
  `{name, fee_type: "PERCENT"|"FIXED", value, position, is_active}`.
  Edits apply to future bookings only.
- `POST /payment/webhook/:provider` — existing; delta: match provider
  `order_id` with `-Rn` suffix stripped. (Provider-facing; response shape per
  provider expectations, not the envelope.)

## Admin surface (JWT — same envelope + snake_case, Option A)

All existing admin endpoints are re-wrapped in the envelope and retagged
snake_case in the same change; the admin frontend migrates with them.

### Events (exists — extended)
`POST/PUT /admin/events…` — `description` now carries WYSIWYG HTML; sanitized
server-side on write.

### Terms (new)
- `GET /admin/events/:id/terms` → `data: { id, content, updated_at }` or 404002
- `PUT /admin/events/:id/terms` `{ "content": "…" }` → upsert (sanitized)

### Content blocks (new — same pattern × 3)
For `activities`, `guest-stars`, `guidelines` under `/admin/events/:id/`:
- `GET    /admin/events/:id/activities` — position-ordered list
- `POST   /admin/events/:id/activities` — create `{ title, description, icon?, position }`
- `PUT    /admin/events/:id/activities/:activityId`
- `DELETE /admin/events/:id/activities/:activityId`

(guest-stars: `{ name, position }`; guidelines: `{ description, icon?, position }`.)
400004 for icon keys outside the fixed set.

## Rate limiting deltas

| Endpoint | Limiter |
|----------|---------|
| POST /ticket/book | new per-IP limiter (creates rows + holds quota) |
| POST /ticket/checkout/:order_id, …/refresh-qr | existing payment-refresh per-IP group |
| GET /ticket/checkout/:order_id/status | per-IP connection cap (e.g. 5 concurrent) |
| POST /ticket/resend-email | existing per-order limiter (now keyed on body order id) |

## Routing note

`/ticket/terms-condition/…`, `/ticket/book`, `/ticket/order/…`,
`/ticket/checkout/…` are static-prefix routes and take precedence over the
parameterized `GET /ticket/:event_id` in Echo's router; `GET /tickets/:code`
lives on a different prefix entirely. Old paths (`/events`, `/checkout`,
`/orders/:orderNumber…`) are **removed**, not aliased — the frontend API client
is updated in the same change.

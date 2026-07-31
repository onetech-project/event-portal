# API Contracts: Admin Management

Base path: `/api/v1`. All `/admin/*` endpoints except login require
`Authorization: Bearer <jwt>`. Paths match PRD.md §1.5, which is LOCKED — in
particular ticket types are **flat** (`/api/v1/admin/ticket-types`), never nested
under an event.

Handler ownership (see plan.md): `/admin/login` is served by `internal/admin`;
`/admin/events` and `/admin/ticket-types` by `internal/event`; `/admin/orders` and
`/admin/attendees` by `internal/order`, which owns those tables.

## POST /admin/login

**Request**: `{ "email": "string", "password": "string" }`
**Response 200**: `{ "token": "jwt string", "expires_at": "RFC3339" }`
**Response 401**: invalid credentials.

## Events

### GET /admin/events
**Response 200**: array of Event (all statuses, unlike the guest-facing endpoint).

### POST /admin/events
**Request**: `{ name, slug, description?, venue, address, start_date, end_date, banner_url?, status }`
`banner_url` is a plain URL string supplied by the admin and stored verbatim — there
is no file-upload endpoint in the MVP.
**Response 201**: created Event.
**Response 400**: slug not unique, or `end_date < start_date`.

### GET /admin/events/:id
**Response 200**: Event detail with its ticket types (each including its remaining
`quota` and derived read-only `sold` count).
**Response 404**: not found.

### PUT /admin/events/:id
Same body/validation as POST. **Response 200**: updated Event.

### DELETE /admin/events/:id
Deletes the event **and all of its ticket types** in one transaction (guard →
`DELETE FROM ticket_types WHERE event_id = $1` → `DELETE FROM events WHERE id = $1`
→ COMMIT). `ticket_types.event_id` is `ON DELETE RESTRICT`, so deleting the event
row alone is impossible.
**Response 204**: event and its ticket types deleted.
**Response 400**: `{ "error_code": "EVENT_HAS_ORDERS", "message": "..." }` when any
of its ticket types is referenced by an `order_items` row **or** an `attendees` row.
Nothing is deleted — the transaction is rolled back.
**Response 404**: not found.

## Ticket Types

`quota` in every request and response below is the **remaining** (still-available)
quota — the same live counter the guest checkout decrements and restores. It is not
an original total, and the admin UI must label it "Sisa Kuota / Remaining Quota".
`sold` is a derived, read-only field (`SUM(order_items.quantity)` for that ticket
type) supplied for context; sending it in a request body is ignored.

### GET /admin/ticket-types?event_id=<uuid>
`event_id` is required; it scopes the list to one event.
**Response 200**: array of
`{ id, event_id, name, price, quota, sold, sales_start, sales_end }`.
**Response 400**: `event_id` missing or not a UUID.

### POST /admin/ticket-types
**Request**: `{ event_id, name, price, quota, sales_start, sales_end }` — `event_id`
is supplied in the body, not the path.
**Response 201**: created Ticket Type (with `sold: 0`).
**Response 400**: `sales_end < sales_start`, `quota < 0`, or unknown `event_id`.

### GET /admin/ticket-types/:id
**Response 200**: Ticket Type with its derived `sold` count.
**Response 404**: not found.

### PUT /admin/ticket-types/:id
**Request**: `{ name, price, quota, sales_start, sales_end }` — `event_id` is
immutable and ignored if sent.
The submitted `quota` **replaces the remaining quota absolutely**; the server never
re-subtracts past sales from it. Concurrency note: a checkout that commits between
the admin's read and this write is absorbed by the submitted value (accepted MVP
risk — see research.md).
**Response 200**: updated Ticket Type.
**Response 400**: `sales_end < sales_start` or `quota < 0`.
**Response 404**: not found.

### DELETE /admin/ticket-types/:id
**Response 204**: deleted.
**Response 400**: `{ "error_code": "TICKET_TYPE_HAS_ORDERS", "message": "..." }` when
referenced by an `order_items` row **or** an `attendees` row.
**Response 404**: not found.

## Read-only views

Served by `internal/order`.

### GET /admin/orders
Optional query params: `status`, `event_id`.
**Response 200**: array of `{ order_number, buyer_name, buyer_email, status, total_amount, created_at }`.

### GET /admin/attendees
Optional query params: `order_id`, `event_id`.
**Response 200**: array of `{ name, email, ticket_type_name, order_number }`.

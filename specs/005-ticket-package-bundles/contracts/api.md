# Contract: API — Events, Tickets & Packages

Companion to `../spec.md`. Base path `/api/v1`. All `/admin/*` endpoints except login
require `Authorization: Bearer <jwt>`.

**Path convention is inherited, not invented.** PRD.md §1.5 is LOCKED and makes admin
resources **flat** — `/admin/ticket-types`, never nested under an event. Packages follow the
same shape: `/admin/packages`, scoped by an `event_id` query parameter or body field, not by
a nested path. Composition is the one exception: it is a sub-resource of a package because it
has no independent identity.

Money is a decimal **string** everywhere (`"50000.00"`); timestamps are RFC3339.

---

## Public

### GET /events
Unchanged. Published events only.

### GET /events/:slug — *changed*

Serves the ticket-list selection screen in one round trip.

**Response 200**

```json
{
  "id": "uuid",
  "name": "Jive Jakarta International Vape",
  "slug": "jive-2026",
  "description": "string|null",
  "venue": "string",
  "address": "string",
  "start_date": "RFC3339",
  "end_date": "RFC3339",
  "banner_url": "string|null",
  "ticket_types": [
    {
      "id": "uuid",
      "name": "Jive Jakarta International Vape (Day 1) - 26 Apr 2026",
      "price": "35000.00",
      "quota_remaining": 120,
      "sales_start": "RFC3339",
      "sales_end": "RFC3339"
    }
  ],
  "packages": [
    {
      "id": "uuid",
      "name": "Jive Jakarta International Vape (Day 1 and 2) - 26 - 27 Apr 2026",
      "description": "string|null",
      "price": "50000.00",
      "sales_start": "RFC3339",
      "sales_end": "RFC3339",
      "available_units": 2,
      "purchasable": true,
      "components": [
        { "ticket_type_id": "uuid", "ticket_type_name": "... (Day 1) ...", "quantity_per_unit": 1 },
        { "ticket_type_id": "uuid", "ticket_type_name": "... (Day 2) ...", "quantity_per_unit": 1 }
      ]
    }
  ]
}
```

Notes that are contractual, not commentary:

- `packages[]` carries **no quota field**. `available_units` is derived per request from the
  remaining quota of the tickets in `components` (see `checkout-transaction.md` §1.2) and is
  the stepper's ceiling.
- `purchasable` already folds in `available_units > 0`, the package's own sales window, every
  constituent's sales window, and `ACTIVE` status — the client renders one boolean and cannot
  disagree with the server about what is buyable.
- `Cache-Control: no-store`. Quota is live inventory; the constitution forbids caching it.
- Only `ACTIVE` packages appear. Inactive ones are admin-visible only.
- The client renders `ticket_types` and `packages` as one list, badging package rows as
  bundles (spec FR-001/FR-002).

### GET /events/:slug/packages *(optional)*

Same `packages[]` payload alone, for polling availability without refetching the whole event.
`Cache-Control: no-store`.

### POST /checkout — *changed*

**Request**

```json
{
  "buyer_name": "string",
  "buyer_email": "string",
  "buyer_phone": "string",
  "items": [
    { "ticket_type_id": "uuid", "quantity": 2 },
    { "package_id": "uuid", "quantity": 1 }
  ],
  "attendees": [
    { "ticket_type_id": "uuid-day1", "name": "A", "email": "a@x.com" },
    { "ticket_type_id": "uuid-day1", "name": "B", "email": "b@x.com" },
    { "ticket_type_id": "uuid-day1", "package_id": "uuid-pkg", "name": "C", "email": "c@x.com" },
    { "ticket_type_id": "uuid-day2", "package_id": "uuid-pkg", "name": "D", "email": "d@x.com" }
  ]
}
```

- Each `items[]` entry carries **exactly one** of `ticket_type_id` / `package_id`. Both or
  neither ⇒ `400`.
- Attendee slots for a package carry **both** ids: the ticket the pass grants access to, and
  the package the slot came from. Those slots may name different people.
- Attendee counts must match the expansion exactly: grouped by
  `(ticket_type_id, package_id)`, the count equals `item.quantity × component.quantity_per_unit`.
  Any mismatch ⇒ `400`.
- Prices are never accepted from the client; the total is computed from stored prices.

**Response 201** — unchanged shape.

```json
{ "order_number": "string", "status": "PENDING", "total_amount": "120000.00", "payment_url": "string" }
```

**Errors**

| Code | Condition | Body |
| --- | --- | --- |
| `400` | malformed item (both/neither id), attendee count mismatch, sales window closed, event not published, package has no components | `{ "error": "...", "field": "..." }` |
| `404` | unknown ticket type or package | `{ "error": "not found" }` |
| `409` | insufficient quota for any ticket type, after aggregating standalone and package demand | `{ "error": "sold out", "ticket_type_id": "uuid", "ticket_type_name": "..." }` |

`409` names the exhausted ticket type even when the failure came from a package line — the
guest needs to know which day sold out, not merely that "the bundle" failed (FR-024).

### GET /orders/:number — *changed*

`items[]` becomes discriminated so a bundle reads as one line at its own price:

```json
{
  "items": [
    { "kind": "package", "package_name": "Day 1 and 2", "ticket_type_name": null,
      "quantity": 1, "unit_price": "50000.00", "subtotal": "50000.00" },
    { "kind": "ticket", "package_name": null, "ticket_type_name": "Day 1",
      "quantity": 2, "unit_price": "35000.00", "subtotal": "70000.00" }
  ]
}
```

### GET /tickets/:code
Unchanged. A pass issued from a bundle is an ordinary pass bound to one ticket type and is
valid only at that ticket's gate.

### POST /payment/webhook/:provider
Unchanged in shape. `expire` / `cancel` / `deny` / `failure` restore quota by expanding the
order's package lines through the junction (`checkout-transaction.md` §3), and remain
idempotent via the guarded `PENDING` transition.

---

## Admin — Events

`CRUD /admin/events` unchanged, with one added guard: deleting an event with any package is
rejected with `400` (`packages.event_id` is `ON DELETE RESTRICT`), alongside the existing
ticket-type and order guards.

## Admin — Ticket Types

`CRUD /admin/ticket-types` unchanged, with two added behaviours:

- **DELETE** is rejected with `400` when the ticket type is a constituent of any package
  (FR-038). Message must name the packages so the administrator knows what to unpick.
- **GET** responses may include a read-only `in_packages: [{ id, name }]` so the admin UI can
  warn before an edit that changes a bundle's availability.

`quota` remains the **remaining** quota, labelled as such, with `sold` derived from order
lines. For a ticket type feeding packages, `sold` must count package lines too:

```sql
SELECT COALESCE(SUM(qty), 0) FROM (
  SELECT oi.quantity AS qty FROM order_items oi
    WHERE oi.ticket_type_id = $1
  UNION ALL
  SELECT oi.quantity * pt.quantity FROM order_items oi
    JOIN package_tickets pt ON pt.package_id = oi.package_id
    WHERE pt.ticket_type_id = $1
) s;
```

A `sold` that ignored package lines would under-report every bundled sale.

## Admin — Packages

### GET /admin/packages?event_id=:uuid

**Response 200**: array of package admin views (all statuses).

```json
[
  {
    "id": "uuid",
    "event_id": "uuid",
    "name": "Day 1 and 2",
    "description": "string|null",
    "price": "50000.00",
    "sales_start": "RFC3339",
    "sales_end": "RFC3339",
    "status": "ACTIVE",
    "components": [
      { "ticket_type_id": "uuid", "ticket_type_name": "Day 1", "quantity_per_unit": 1 }
    ],
    "available_units": 2,
    "sold": 14,
    "created_at": "RFC3339",
    "updated_at": "RFC3339"
  }
]
```

`available_units` and `sold` are derived and read-only. **There is no quota field, and none
is ever accepted** (FR-036).

### GET /admin/packages/:id
Single package admin view. `404` if unknown.

### POST /admin/packages

**Request**

```json
{
  "event_id": "uuid",
  "name": "string",
  "description": "string|null",
  "price": "50000.00",
  "sales_start": "RFC3339",
  "sales_end": "RFC3339",
  "status": "ACTIVE",
  "components": [
    { "ticket_type_id": "uuid", "quantity_per_unit": 1 },
    { "ticket_type_id": "uuid", "quantity_per_unit": 1 }
  ]
}
```

Validation:

| Rule | Code |
| --- | --- |
| `components` non-empty | `400` |
| every `ticket_type_id` belongs to `event_id` | `400` (also blocked by composite FK) |
| no duplicate `ticket_type_id` | `400` (also blocked by unique constraint) |
| `quantity_per_unit >= 1` | `400` |
| `price >= 0` | `400` |
| `sales_end > sales_start` | `400` |
| any `quota`-like field present in the body | `400` — reject, do not ignore |

`price` is **not** validated against the sum of its constituents: above, below or equal are
all the administrator's decision.

**Response 201**: the created package admin view.

### PUT /admin/packages/:id

Same body as POST minus `event_id` — a package never moves between events. Replaces
`components` wholesale.

Price and window changes apply to new selections only and never alter an existing order's
recorded amount (FR-026, FR-007 in the admin story).

**Composition changes**: rejected with `409` while the package has open `PENDING` orders. The
reason is quota-drift, not caution — restoration reconstructs an order's hold from the
package's *current* composition, so editing it under an open order would restore a different
amount than was deducted (`checkout-transaction.md` §3).

### DELETE /admin/packages/:id

- `204` when the package has never been ordered.
- `400` when referenced by any `order_items` or `attendees` row (FR-037). The FKs are
  `ON DELETE RESTRICT`; the service must return `400` rather than surfacing a raw constraint
  violation, matching how the existing event/ticket-type guards behave.
- Composition rows cascade with the package.

### GET /admin/packages/:id/availability

Diagnostic. Shows why a bundle is or is not offered.

```json
{
  "package_id": "uuid",
  "available_units": 2,
  "purchasable": true,
  "limiting_ticket_type": { "id": "uuid", "name": "Day 2", "quota_remaining": 2 },
  "components": [
    { "ticket_type_id": "uuid", "name": "Day 1", "quota_remaining": 5, "quantity_per_unit": 1, "units_supported": 5 },
    { "ticket_type_id": "uuid", "name": "Day 2", "quota_remaining": 2, "quantity_per_unit": 1, "units_supported": 2 }
  ]
}
```

## Admin — Orders & Attendees

`GET /admin/orders` and `GET /admin/attendees` gain package awareness:

- order line items use the discriminated shape above;
- attendee rows expose `package_name: string|null` so a bundle-derived registrant is
  identifiable at the door and in exports.

---

## Handler ownership

Per Constitution Principles I & II:

| Route group | Domain |
| --- | --- |
| `/events`, `/events/:slug`, `/events/:slug/packages` | `internal/event` |
| `/admin/events`, `/admin/ticket-types`, `/admin/packages` | `internal/event` |
| `/checkout`, `/orders/:number`, `/admin/orders`, `/admin/attendees` | `internal/order` |
| `/payment/webhook/:provider` | `internal/payment` |
| `/tickets/:code`, `/admin/tickets/validate` | `internal/ticket` |

`packages` and `package_tickets` are owned by `internal/event` — they belong to an event and
join only to that event's `ticket_types`, so no query crosses a domain boundary. The `order`
domain reaches them solely through the extended `EventProvider` port (`models.md` §2.3),
never by importing `internal/event`.

## PRD note

PRD.md §1.5 is LOCKED and predates packages. Adding `/admin/packages` and the `packages[]`
field on `GET /events/:slug` extends it; the PRD and `SCHEMA.md` must be updated in the same
change, per the constitution's Governance section.

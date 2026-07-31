# Phase 1 Data Model: Admin Management

## Admin

- `id`, `email` (unique), `password_hash`
- **Feature rule**: authentication compares submitted password against
  `password_hash` (bcrypt/argon2 verify); no other admin fields are mutated by this
  feature (no self-service profile edit in scope).

## Event (full CRUD owned here)

- `id`, `name`, `slug` (unique), `description`, `venue`, `address`, `start_date`,
  `end_date`, `banner_url`, `status` (`DRAFT` | `PUBLISHED` | `COMPLETED`)
- **Feature rule (FR-004)**: `slug` uniqueness enforced.
- **Feature rule (FR-005)**: `end_date >= start_date`.
- **Feature rule (FR-006)**: delete rejected (400, `EVENT_HAS_ORDERS`) if any
  `ticket_types` row for this event is referenced by any `order_items` row **or**
  by any `attendees` row. Both foreign keys are `ON DELETE RESTRICT`, so checking
  only `order_items` would let a raw Postgres FK violation escape instead of the
  clean 400 the PRD requires.
- **Feature rule (FR-013)**: `ticket_types.event_id` is `NOT NULL REFERENCES
  events(id) ON DELETE RESTRICT`, so Postgres will refuse to delete an event that
  still has ticket types. Deleting an event is therefore a transaction, not a
  single statement:
  1. `BEGIN` — verify no `ticket_types` row of this event is referenced by any
     `order_items` **or** `attendees` row; if any reference exists, abort with 400
     `EVENT_HAS_ORDERS` and leave everything intact.
  2. `DELETE FROM ticket_types WHERE event_id = $1`
  3. `DELETE FROM events WHERE id = $1`
  4. `COMMIT`
- **Feature rule (FR-012)**: `status` fully admin-controlled; only `PUBLISHED`
  affects guest visibility (enforced by the Guest Purchase Flow feature's reads).
- `banner_url` is a plain URL string supplied by the admin and stored verbatim.
  There is no file upload in the MVP — no storage service exists in the stack — so
  the backend neither accepts nor serves image binaries.

## Ticket Type (full CRUD owned here)

- `id`, `event_id`, `name`, `price`, `quota`, `sales_start`, `sales_end`
- **Feature rule (FR-008)**: `sales_end >= sales_start`.
- **Feature rule (FR-009)**: delete rejected (400, `TICKET_TYPE_HAS_ORDERS`) if
  referenced by any `order_items` row **or** any `attendees` row — both columns
  (`order_items.ticket_type_id`, `attendees.ticket_type_id`) are `ON DELETE
  RESTRICT`, so both must be checked to return a clean 400 instead of leaking a raw
  FK violation.
- **Feature rule (FR-014) — `quota` is REMAINING quota, not a total**:
  `ticket_types.quota` is the live remaining-inventory counter. The Guest Purchase
  Flow feature (specs/001) *decrements this exact column* inside the checkout
  transaction and restores it on cancel/expire. A ticket type opened at 50 that has
  sold 10 holds `quota = 40`.
  - The admin form field MUST be labelled **"Sisa Kuota / Remaining Quota"** — never
    "Total". Mislabelling it "Total" would invite an admin to "reset" 40 back to 50
    and silently mint 10 phantom tickets.
  - An admin write sets the remaining value **absolutely**: `UPDATE ticket_types SET
    quota = $new WHERE id = $1`. Past sales are never re-subtracted from it.
  - There is no `quota_total` column and none may be added (SCHEMA.md is locked).
- **Feature rule (FR-015) — derived `sold` count (read-only)**: for admin context,
  each ticket type is displayed with `sold = SUM(order_items.quantity)` for that
  ticket type. It is derived, never stored, and never writable. It MUST be obtained
  through `OrderChecker.SoldCountByTicketType` — an interface defined in the `event`
  domain and implemented by the `order` domain (see plan.md / research.md) —
  **never** via a cross-domain JOIN in a write path (ARCHITECTURE.md §3.2).
- **Accepted concurrency risk**: because the admin write is an absolute set, a guest
  checkout that commits between the admin's read and the admin's write is silently
  absorbed by the admin's value. Admins are advised not to edit quota during active
  sales. No optimistic-locking/version column is added — the schema is locked.

## Order (read-only in this feature)

- Exposed as an `OrderSummary` projection: `order_number`, `buyer_name`,
  `buyer_email`, `status`, `total_amount`, `created_at`. The projection and its
  `GET /api/v1/admin/orders` handler are owned by the `order` domain, which owns
  the underlying tables.
- Also the source of the `order_items` half of the delete guards and of the derived
  `sold` count, both surfaced to the `event` domain through the `OrderChecker`
  interface (defined in `event`, implemented in `order`, injected in
  `cmd/api/main.go`).
- Full lifecycle owned by the Guest Purchase Flow / Admin Ticket Validation
  features.

## Attendee (read-only in this feature)

- Exposed as an `AttendeeSummary` projection: `name`, `email`, `ticket_type_name`,
  `order_number`; served by the `order` domain's `GET /api/v1/admin/attendees`
  handler.
- `attendees.ticket_type_id` is a second `ON DELETE RESTRICT` reference to
  `ticket_types` and therefore participates in **both** delete guards (FR-006,
  FR-009) alongside `order_items`.

## Relationships (relevant to this feature)

```text
Event (1) ──< Ticket Type (many)          [FK ON DELETE RESTRICT — event delete
                                           must remove ticket types first]
Ticket Type (1) ──< Order Item (many, read-only; delete-guard + sold count + listing)
Ticket Type (1) ──< Attendee  (many, read-only; delete-guard + listing)
Order (1) ──< Attendee (many, read-only)
```

## Validation Summary

| Field/Action | Rule | Requirement |
|---|---|---|
| `events.slug` | unique across events | FR-004 |
| `events.end_date` | `>= start_date` | FR-005 |
| Delete Event | reject if any of its ticket types is referenced by `order_items` **or** `attendees` | FR-006 |
| Delete Event | on success, delete its `ticket_types` then the `events` row in one transaction | FR-013 |
| `ticket_types.sales_end` | `>= sales_start` | FR-008 |
| Delete Ticket Type | reject if referenced by `order_items` **or** `attendees` | FR-009 |
| `ticket_types.quota` | remaining quota; admin writes set it absolutely; labelled "Sisa Kuota / Remaining Quota" | FR-014 |
| `sold` | derived `SUM(order_items.quantity)` via `OrderChecker`; read-only | FR-015 |

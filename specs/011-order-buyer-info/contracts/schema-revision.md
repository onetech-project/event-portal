# Contract: Schema Revision & End-of-Journey Modal (rev. 3, 2026-08-07)

**Feature**: [spec 011](../spec.md) FR-022 – FR-029 | Sibling of [checkout-and-delivery.md](checkout-and-delivery.md). Only deltas are documented; everything not mentioned is unchanged (envelope, snake_case, error-code registry, rate limits, routing).

**Headline**: this revision is almost entirely invisible at the contract boundary. One admin field changes shape; everything else moves beneath the wire.

## 1. Order status — NO contract change (FR-026)

`orders.status` becomes a foreign key to the order-status master list in storage, but the **name** stays the exchanged value everywhere it is exchanged today:

| Surface | Field | Before | After |
|---|---|---|---|
| `GET /api/v1/ticket/orders/:order_number` | `status` | `"PENDING"` | `"PENDING"` |
| `GET /api/v1/ticket/checkout/:order_id/status` (SSE) | `status` | `"PENDING"` | `"PENDING"` |
| `GET /api/v1/admin/orders` (list + filter param) | `status` | `"PAID"` | `"PAID"` |
| Payment webhook processing | — | name-compared | name-compared |

Frontend `OrderStatus` (`frontend/lib/types.ts:183`, `:225`) is untouched. No client change, no version negotiation, no deprecation window. **This is a deliberate contract non-event** — it is documented precisely so a reviewer can confirm nothing leaked.

## 2. Package availability — `status` → `is_active` (FR-029) **[breaking, admin-only]**

The only contract change in this revision. Admin package endpoints exchange a boolean instead of the two-value string.

### `POST /api/v1/admin/packages` and `PUT /api/v1/admin/packages/:id` — request

```jsonc
{
  "event_id": "…", "name": "…", "description": "…",
  "price": 250000, "sales_start": "…", "sales_end": "…",
  // "status": "ACTIVE" | "INACTIVE"   — REMOVED
  "is_active": true                    // NEW: boolean, required, defaults to true on create
}
```

### Package responses (admin list, admin read, create/update echo)

```jsonc
{ "id": "…", "name": "…", /* … */,
  // "status": "ACTIVE"  — REMOVED
  "is_active": true }
```

**Public booking responses are unaffected in shape**: they never carried the package status — inactive packages are filtered out server-side (`event.sql:162`, `:206`), and that filter simply changes from `p.status = 'ACTIVE'` to `p.is_active`.

**Breaking-change handling**: admin-only surface, shipped on the same release train as the frontend that consumes it (the established pattern for specs 008/010/011). No dual-write or transition field — the clarification explicitly rejected carrying both.

## 3. Issued tickets — explicitly NOT changed (FR-029)

`tickets.status` keeps `ACTIVE | USED | REVOKED` in storage and on the wire (`frontend/lib/types.ts:232`). The gate validation contract is untouched: `POST /api/v1/admin/tickets/validate` still resolves to `VALID` / `ALREADY_USED` / `INVALID`, and `ALREADY_USED` remains distinguishable from a revoked pass. Recorded here because "Packages, Tickets" in the original request read as if it covered this endpoint; it does not.

## 4. Gender master list — NO contract change (FR-025)

`GET /api/v1/ticket/genders` keeps returning `{ id, name }`. The `id` narrows from a uuid string to a small integer in JSON, but **no client reads it**: the checkout request submits the gender **name** (contract §1 of [checkout-and-delivery.md](checkout-and-delivery.md)), and `frontend/components/order/visitor-form.tsx` binds its select to the name. Verify this before merge rather than assuming — if any client starts keying on `id`, this becomes a breaking change.

## 5. Master-list authorship — no endpoint (FR-028)

`created_by` / `updated_by` are storage-only in this revision. There is no admin CRUD for genders or order statuses in the codebase (only the read-only `GET /ticket/genders`), so no request or response carries an author. Seeded rows hold the literal `SYSTEM`. When master-list CRUD is eventually built, that is where the admin-identifier branch of FR-028 attaches (research R22).

## 6. End-of-journey modal — no API change (FR-022 – FR-024)

Entirely client-side. The order pages already receive `status` on their existing reads; the change is that an EXPIRED/CANCELLED value now renders a blocking dialog **over** the page instead of replacing the page. No new endpoint, no new field, no polling change.

One behavioural note with a contract flavour: on the payment screen the modal opens when the **client's** countdown reaches zero, without waiting for the server to report `EXPIRED`. The server's subsequent status change must therefore be idempotent from the UI's point of view — arriving after the modal is already up, it changes nothing on screen. This is a client rule, not a server one; the server's expiry sweep is unchanged.

# API Contracts: Admin Ticket Validation

Base path: `/api/v1`. All endpoints require `Authorization: Bearer <jwt>`.

## Relationship to PRD §1.5 (LOCKED API list)

| Endpoint | PRD §1.5 status |
|---|---|
| `POST /admin/tickets/validate` | Listed |
| `POST /admin/orders/:id/resend-email` | Listed |
| `POST /admin/tickets/:code/use` | **Deliberate addition — justified below** |

`POST /admin/tickets/:code/use` is not in PRD.md §1.5's LOCKED list, and this is a
conscious, documented extension rather than an accidental deviation:

- PRD.md §1.4 mandates "Admin can mark `Valid` tickets as `Used`", but §1.5 lists
  only `POST /admin/tickets/validate` for this feature area. No listed endpoint can
  perform the mandated `ACTIVE -> USED` transition, so the capability PRD §1.4
  requires cannot be delivered without one addition.
- The transition is kept **separate** from `validate` on purpose: `validate` MUST
  remain side-effect-free, because at a door the same code is routinely scanned
  more than once, and a side-effecting lookup would consume tickets the admin had
  not yet decided to admit. Marking a ticket `Used` is irreversible through this
  flow (constitution, Critical Data Flow Rules), so an accidental consume is
  unrecoverable.
- The addition is admin-only and JWT-gated; it introduces no guest-facing surface
  and no other endpoint beyond PRD §1.5's list.

The same rationale is recorded in spec.md ("Deliberate Extension to PRD §1.5"),
plan.md ("PRD §1.5 API Deviation"), and research.md.

## POST /admin/tickets/validate

Used for both manual code entry and camera QR scan (client resolves the QR payload
to a code string before calling this endpoint).

**Read-only**: this endpoint never mutates ticket state. Repeated calls with the
same code are safe and always return the same result until an explicit mark-used
call changes it.

**Code handling**: the raw `code` is normalized server-side — surrounding
whitespace trimmed, then uppercased — and matched **exactly** against
`tickets.ticket_code`. Stored codes are already canonical (uppercase, fixed
length, charset excluding `I`/`O`/`0`/`1`), so no case-folding function is applied
to the column; that would bypass `idx_tickets_ticket_code`.

**Request**: `{ "code": "string" }` (raw input, normalized server-side)
**Response 200**:
```json
{
  "result": "VALID|ALREADY_USED|INVALID",
  "ticket_code": "string",
  "attendee_name": "string|null",
  "ticket_type_name": "string|null",
  "event_name": "string|null"
}
```
`attendee_name`/`ticket_type_name`/`event_name` are `null` when `result` is
`INVALID`. `ticket_code` echoes the normalized code. No QR image is returned —
`tickets.qr_code_url` is empty in this MVP and QR images are generated on demand
from the ticket code where one is actually needed (PDF rendering).

## POST /admin/tickets/:code/use

Deliberate addition to PRD §1.5 — see "Relationship to PRD §1.5" above.

The `:code` path segment is normalized identically to the `validate` body field
(trim + uppercase, then exact match). The transition runs as one guarded UPDATE
(`... SET status = 'USED', updated_at = now() WHERE ticket_code = $1 AND status =
'ACTIVE'`), so two near-simultaneous calls cannot both succeed. `updated_at` is
what records *when* the ticket was consumed — SCHEMA.md is LOCKED and the `tickets`
table has no `used_at` column.

**Response 200**: `{ "ticket_code": "string", "status": "USED" }`
**Response 409**: `{ "error_code": "ALREADY_USED", "message": "..." }` when current
status is not `ACTIVE` (already `USED`, or `REVOKED`).
**Response 404**: no ticket with that code.

## POST /admin/orders/:id/resend-email

Allowed **only** when the order's status is `PAID` — exactly the orders that have
generated tickets. The tickets' existing `ticket_code` values are reused verbatim
and are never regenerated; each QR image is re-generated from its ticket code at
PDF render time, so a guest's original ticket stays valid. On successful delivery
the order's `email_sent` flag is set to `true`, exactly as the initial
post-payment send does.

**Response 200**: `{ "message": "Email resent", "sent_to": "buyer@example.com" }`
**Response 400**: `{ "error_code": "ORDER_NOT_PAID", "message": "..." }` for any
order whose status is not `PAID` — `PENDING`, `CANCELLED` (Midtrans
`deny`/`failure`/`cancel`), or `EXPIRED` (Midtrans `expire`) — since none of them
have generated tickets.
**Response 404**: order not found.

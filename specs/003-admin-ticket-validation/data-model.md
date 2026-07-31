# Phase 1 Data Model: Admin Ticket Validation

## Ticket (status mutation owned here)

- `id`, `ticket_code` (unique), `order_id`, `attendee_id`, `status` (`ACTIVE` |
  `USED` | `REVOKED`), `qr_code_url`, `updated_at`
- **Feature rule (FR-003)**: lookup maps status to exactly one of Valid (`ACTIVE`),
  Already Used (`USED`), or Invalid (not found or `REVOKED`).
- **Feature rule (FR-005/FR-006)**: mark-used is a guarded transition
  `ACTIVE -> USED` only; any other current status rejects the mark-used request.
  The transition is *recorded* by setting `updated_at` to the current timestamp in
  that same guarded UPDATE — SCHEMA.md is LOCKED and the `tickets` table has **no
  `used_at` column**; none may be proposed. For a row with `status = 'USED'`,
  `updated_at` is the consumption timestamp.
- **Feature rule (FR-010)**: the *input* is normalized (trim + uppercase) and then
  matched **exactly** against `ticket_code`. Stored codes are already canonical
  (uppercase, fixed length, charset excluding `I`/`O`/`0`/`1`) from specs/001, and
  `idx_tickets_ticket_code` is a plain btree, so the column must never be wrapped
  in `UPPER()`/`LOWER()` at lookup — that would make the index unusable.
- **`qr_code_url`**: left NULL/empty in the MVP and never read by this feature.
  There is no object storage, static file serving, or upload endpoint in the
  stack; each ticket's QR image is generated on demand from `ticket_code` at PDF
  render time (initial delivery and resend alike). Ticket codes are stable and are
  never regenerated; QR images always are.
- Ticket creation itself (on Order -> PAID) is owned by the Guest Purchase Flow
  feature; this feature only reads and transitions `status` (plus `updated_at`).

## Attendee (read-only)

- `name`, exposed alongside a Valid lookup result (FR-004) to let the admin
  visually confirm identity.

## Order (resend target — read-only except `email_sent`)

- `id`, `status`, `buyer_email`, `email_sent`
- **Feature rule (FR-007/FR-008)**: resend is allowed **only** when
  `status = 'PAID'` — exactly the orders that have generated tickets. Every other
  value of SCHEMA.md's `orders.status` CHECK is rejected with `400
  ORDER_NOT_PAID`: `PENDING`, `CANCELLED` (Midtrans `deny`/`failure`/`cancel` per
  specs/001's webhook mapping), and `EXPIRED` (Midtrans `expire`).
- **Feature rule (FR-011)**: on successful resend delivery, `email_sent` is set to
  `true`, exactly as the initial post-payment send does (ARCHITECTURE.md §3.4).
  The write is idempotent when the flag is already `true`. No other order column is
  written by this feature.
- Read access to the order (buyer email, status) and to its attendees and their
  ticket codes goes through the `OrderChecker` interface implemented by the `order`
  domain (naming per specs/002), never a direct repository import
  (ARCHITECTURE.md §3.2).

## State Transition Summary

```text
Ticket.status: ACTIVE --(admin marks used)--> USED, updated_at = now()   [FR-005]
               ACTIVE/USED/REVOKED --(lookup)--> read-only, no side effect
               USED --(admin attempts mark used again)--> rejected [FR-006]
               REVOKED --(any validate/mark-used action)--> treated as Invalid
```

## Validation Summary

| Field/Action | Rule | Requirement |
|---|---|---|
| Ticket lookup | *input* normalized (trim + uppercase), then exact match on indexed `ticket_code` | FR-010 |
| Ticket lookup | 3-way result: Valid / Already Used / Invalid | FR-003 |
| Mark ticket used | only from `ACTIVE`, atomically; `updated_at` records the transition (no `used_at` column) | FR-005/FR-006 |
| Resend email | only when `orders.status = 'PAID'`; `PENDING`/`CANCELLED`/`EXPIRED` rejected | FR-007/FR-008 |
| Resend email | existing `ticket_code` values reused verbatim; QR images re-generated from them at render time | FR-007 |
| Resend email | `orders.email_sent` set to `true` on successful delivery | FR-011 |

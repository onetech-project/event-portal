# Quickstart: Admin Ticket Validation

## Prerequisites

- Backend running with an admin JWT obtained per the Admin Management quickstart.
- At least one `PAID` order with a generated ticket (via the Guest Purchase Flow
  quickstart).

## Scenario 1: Validate a fresh ticket (User Story 1)

```bash
curl -s -X POST http://localhost:8080/api/v1/admin/tickets/validate \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"code":"<ticket_code>"}' | jq
```
**Expected**: `result: "VALID"` with attendee/ticket-type/event details.

Repeat the same call with the code lowercased and padded (e.g.
`{"code":"  <ticket_code lowercased>  "}`) — the input is normalized (trim +
uppercase) before an exact match on the indexed `ticket_code`, so the result must
be identical (FR-010). Lookups are read-only: repeating either call never changes
ticket status.

## Scenario 2: Mark it used, then re-validate (User Story 2)

```bash
curl -s -X POST http://localhost:8080/api/v1/admin/tickets/<ticket_code>/use \
  -H "Authorization: Bearer $TOKEN" | jq

curl -s -X POST http://localhost:8080/api/v1/admin/tickets/validate \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"code":"<ticket_code>"}' | jq
```
**Expected**: first call returns `status: "USED"`; second call now returns
`result: "ALREADY_USED"`. A repeated mark-used call on the same code returns 409.
The transition is recorded by that row's `updated_at` advancing to the time of the
guarded UPDATE — there is no `used_at` column in the locked schema:

```bash
psql "$DATABASE_URL" -c "SELECT status, updated_at FROM tickets WHERE ticket_code = '<ticket_code>'"
```

Note that `POST /admin/tickets/:code/use` is a deliberate, documented addition to
PRD §1.5's API list (see contracts/api.md) — `validate` stays side-effect-free so
repeated door scans are safe.

## Scenario 3: Invalid code (User Story 1)

```bash
curl -s -X POST http://localhost:8080/api/v1/admin/tickets/validate \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"code":"does-not-exist"}' | jq
```
**Expected**: `result: "INVALID"`.

## Scenario 4: Resend ticket email (User Story 3)

```bash
curl -s -X POST http://localhost:8080/api/v1/admin/orders/<order_id>/resend-email \
  -H "Authorization: Bearer $TOKEN" | jq
```
**Expected**: `200` and a second email with the same PDF ticket set arrives at the
buyer's inbox (or local SMTP catcher). The tickets' `ticket_code` values are
unchanged (they are reused verbatim, never regenerated); each QR image in the PDF
is re-generated from those codes at render time, since `tickets.qr_code_url` is
empty in this MVP and there is no object storage. On success the order's
`email_sent` flag is set to `true`, exactly as the initial post-payment send does:

```bash
psql "$DATABASE_URL" -c "SELECT status, email_sent FROM orders WHERE id = '<order_id>'"
```

Attempting this against any order whose status is not `PAID` — `PENDING`,
`CANCELLED`, or `EXPIRED` — returns `400` with `error_code: "ORDER_NOT_PAID"`,
since none of those have generated tickets.

## Reference

- Contract details: [contracts/api.md](./contracts/api.md)
- Entity/field details: [data-model.md](./data-model.md)
- Design rationale: [research.md](./research.md)

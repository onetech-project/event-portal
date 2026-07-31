# Quickstart: Guest Purchase Flow

## Prerequisites

- Docker Compose running PostgreSQL with migrations applied (SCHEMA.md schema).
- Backend running (`go run ./cmd/api`) with Midtrans Sandbox credentials and SMTP
  credentials configured via env/config.
- At least one `PUBLISHED` event with one `ticket_type` whose `sales_start <=
  now() <= sales_end` and `quota > 0`.

## Scenario 1: Browse and view an event (User Story 1)

```bash
curl -s http://localhost:8080/api/v1/events | jq
curl -s http://localhost:8080/api/v1/events/<slug> | jq
```
**Expected**: event appears in the list; detail response includes its ticket types
with `quota_remaining > 0`.

## Scenario 2: Checkout (User Story 2)

```bash
curl -s -X POST http://localhost:8080/api/v1/checkout \
  -H 'Content-Type: application/json' \
  -d '{
    "buyer_name": "Test Buyer",
    "buyer_email": "buyer@example.com",
    "buyer_phone": "081234567890",
    "items": [{"ticket_type_id": "<ticket_type_id>", "quantity": 2}],
    "attendees": [
      {"ticket_type_id": "<ticket_type_id>", "name": "A One", "email": "a1@example.com"},
      {"ticket_type_id": "<ticket_type_id>", "name": "A Two", "email": "a2@example.com"}
    ]
  }' | jq
```
**Expected**: `201` with `order_number`, `status: PENDING`, and a `payment_url`.
Re-check `GET /events/<slug>` — `quota_remaining` for that ticket type decreased
by 2.

## Scenario 3: Simulate payment success (User Story 3)

Trigger the Midtrans Sandbox test transaction status to `settlement`/`capture` for
the created order (via Midtrans Sandbox simulator or a signed test payload posted
to the webhook endpoint), then:

```bash
curl -s http://localhost:8080/api/v1/tickets/<ticket_code> | jq
```
**Expected**: order status becomes `PAID`; one ticket per attendee is created and
retrievable by code with `status: ACTIVE`; buyer's inbox (or local SMTP catcher,
e.g. Mailhog) receives exactly one email with a PDF attachment listing all tickets.
Re-posting the same webhook payload MUST return `200 OK` without creating
duplicate tickets or a second email.

## Scenario 4: Simulate expiration and quota restore

Trigger a Sandbox `expire` notification for a different Pending order, then re-check
`GET /events/<slug>` and confirm `quota_remaining` for the affected ticket type has
been restored by the originally deducted amount.

## Scenario 5: Ticket lookup (User Story 4)

```bash
curl -s http://localhost:8080/api/v1/tickets/does-not-exist | jq
```
**Expected**: `404` not-found response.

## Reference

- Contract details: [contracts/api.md](./contracts/api.md)
- Entity/field details: [data-model.md](./data-model.md)
- Design rationale: [research.md](./research.md)

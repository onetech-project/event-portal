# Quickstart: Admin Management

## Prerequisites

- Backend running with Postgres migrated per SCHEMA.md.
- One admin row seeded (e.g., via a bootstrap script/SQL insert with a bcrypt hash)
  since self-service signup is out of scope (see spec.md Assumptions).

## Scenario 1: Login (User Story 1)

```bash
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/admin/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"correct-password"}' | jq -r .token)
echo "$TOKEN"
```
**Expected**: a JWT is returned; using it as `Authorization: Bearer $TOKEN` on any
`/admin/*` endpoint succeeds. An unauthenticated call to any `/admin/*` endpoint
without the header returns 401.

## Scenario 2: Create an event and ticket type (User Story 2 + 3)

```bash
EVENT=$(curl -s -X POST http://localhost:8080/api/v1/admin/events \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Test Con","slug":"test-con","venue":"Hall A","address":"Somewhere",
       "start_date":"2026-09-01T09:00:00Z","end_date":"2026-09-01T18:00:00Z",
       "status":"PUBLISHED"}')
EVENT_ID=$(echo "$EVENT" | jq -r .id)

# Ticket-type routes are FLAT (PRD 1.5): event_id goes in the body, not the path.
# `quota` is the REMAINING quota — the same counter checkout decrements.
TT=$(curl -s -X POST http://localhost:8080/api/v1/admin/ticket-types \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d "{\"event_id\":\"$EVENT_ID\",\"name\":\"Regular\",\"price\":\"100000\",\"quota\":50,
       \"sales_start\":\"2026-08-01T00:00:00Z\",\"sales_end\":\"2026-08-31T23:59:59Z\"}")
TT_ID=$(echo "$TT" | jq -r .id)

curl -s -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/api/v1/admin/ticket-types?event_id=$EVENT_ID" | jq
```
**Expected**: event and ticket type created; the list call returns the ticket type
with `quota: 50` and `sold: 0`; `GET /api/v1/events/test-con` (guest endpoint) now
shows the event with this ticket type.

## Scenario 3: Remaining quota is absolute (User Story 3)

After one guest order for 10 tickets of the type above (e.g., via the Guest Purchase
Flow quickstart):

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/admin/ticket-types/$TT_ID | jq '{quota, sold}'
```
**Expected**: `{"quota": 40, "sold": 10}` — `quota` is what is **left**, and `sold`
is the derived read-only count shown for context.

```bash
curl -s -X PUT http://localhost:8080/api/v1/admin/ticket-types/$TT_ID \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"name":"Regular","price":"100000","quota":25,
       "sales_start":"2026-08-01T00:00:00Z","sales_end":"2026-08-31T23:59:59Z"}' \
  | jq '{quota, sold}'
```
**Expected**: `{"quota": 25, "sold": 10}` — the submitted value replaces the
remaining quota absolutely; sales are never re-subtracted from it. Do not run this
while sales are active: a checkout committing between your read and this write is
silently absorbed.

## Scenario 4: Deletion guard (User Story 2 + 3)

After at least one order or attendee exists against the ticket type created above:

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/admin/ticket-types/$TT_ID

curl -s -o /dev/null -w '%{http_code}\n' -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/v1/admin/events/$EVENT_ID
```
**Expected**: `400` for both (`TICKET_TYPE_HAS_ORDERS` / `EVENT_HAS_ORDERS`), and
nothing is deleted. Both guards check `order_items` **and** `attendees`, so an
attendee row alone is enough to block deletion.

For an event with ticket types but **no** orders or attendees, `DELETE
/api/v1/admin/events/:id` returns `204` and removes the event's ticket types and the
event together in one transaction — you do not (and cannot) delete the ticket types
in a separate request first.

## Scenario 5: View orders and attendees (User Story 4)

```bash
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/admin/orders | jq
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/admin/attendees | jq
```
**Expected**: orders/attendees created via checkout are visible with correct
status/buyer/attendee fields.

## Reference

- Contract details: [contracts/api.md](./contracts/api.md)
- Entity/field details: [data-model.md](./data-model.md)
- Design rationale: [research.md](./research.md)

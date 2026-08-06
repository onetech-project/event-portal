# Quickstart: Validating Single Visitor Form per Bundle

**Feature**: [spec.md](spec.md) | **Contract**: [contracts/order-forms.md](contracts/order-forms.md) | **Data model**: [data-model.md](data-model.md)

## Prerequisites

```bash
docker compose up -d postgres migrate      # applies migrations incl. new 000011; migrate exits when current
cd backend && ./scripts/setup-test-db.sh   # recreates the test DB with every migration
```

An event with at least one **package containing ≥ 2 tickets** (e.g. a "2-Day Bundle" of Day-1 + Day-2 constituents) and one standalone ticket type must exist — create via the admin panel or use the seeded data used by specs 005/008.

## Automated checks

```bash
cd backend && go test ./internal/order/... ./internal/ticket/...   # booking unit-stamping, checkout consistency, ticket-per-attendee invariant
cd backend && go test ./...                                        # full backend
cd frontend && npx vitest run                                      # grouping helper, form rendering/fan-out, page tests
```

Expected: all green, including (new/updated)

- `booking_test.go`: bundle slots stamped `package_unit` 1..Q with per-unit composition; standalone slots NULL.
- `checkout_forms_test.go`: divergent visitor data within one `(package_id, package_unit)` group → 400001 `attendees[i].<field>`; identical data accepted; legacy NULL-unit slots exempt.
- `ticket/service_test.go` `TestGenerateForOrderIssuesExactlyOneTicketPerAttendee`: unchanged and still green (FR-004).
- Frontend `slot-groups` tests: standalone/own-group, `(package_id, package_unit)` grouping, NULL-unit fallback, unit labels.
- `page.test.tsx`: a 2-slot bundle order renders **one** visitor card titled with the bundle name + "2 tickets"; submitted payload contains 2 `attendees[]` entries with identical fields and both slot ids.

## Manual end-to-end (maps to spec user stories)

Run the app: `docker compose up -d` then `cd frontend && npm run dev` (or the compose frontend), open the event page.

1. **US1 — one form per bundle**: add 1× "2-Day Bundle" (2 tickets) → agree T&C → order page shows Buyer contact + **one** visitor form titled with the bundle name and a "2 tickets" badge (not two forms titled Day-1/Day-2). Fill and continue → QRIS panel appears. Pay in sandbox → e-ticket email holds **2** tickets with distinct codes, same visitor name.
2. **US2 — mixed order**: book 1× bundle + 1× standalone ticket → exactly 2 visitor forms (bundle-titled + ticket-titled); fill differently; after payment the standalone ticket carries its own visitor.
3. **US3 — multiple units**: book 2× the same bundle → 2 forms with unit labels; fill different visitors → 4 tickets issued, unit 1's tickets carry visitor A, unit 2's visitor B.

Data spot-checks (`docker compose exec postgres psql -U <user> <db>`):

```sql
-- unit stamping (replace order number)
SELECT ticket_type_id, package_id, package_unit, name, email
FROM attendees a JOIN orders o ON o.id = a.order_id
WHERE o.order_number = '...' ORDER BY package_id NULLS FIRST, package_unit, a.id;
-- expect: units 1..Q per package line, identical name/email within a unit after checkout

-- ticket count preserved
SELECT count(*) FROM tickets t JOIN orders o ON o.id = t.order_id WHERE o.order_number = '...';
-- expect: equals the order's slot count
```

API-level check of the new rejection (after booking a bundle order and agreeing T&C):

```bash
# submit divergent data for two slots of the same unit → expect 400, code 400001,
# data key attendees[1].name with the same-bundle message (contracts §2)
curl -s -X POST localhost:8080/api/v1/ticket/checkout/<ORDER_NUMBER> -H 'Content-Type: application/json' -d @divergent-bundle.json
```

## Regression guardrails

- An order booked **before** migration 000011 (bundle slots with `package_unit` NULL) must still render one form per slot and check out successfully — verify with a pre-existing PENDING order if one survives locally, otherwise covered by the frontend fallback tests.
- `GET /ticket/order/:order_id` remains parseable by the done/status pages (additive fields only).
- Admin ticket validation: scan both QRs of one bundle unit — first `Valid` → `Used`, second still `Valid` independently.

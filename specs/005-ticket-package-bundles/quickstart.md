# Quickstart: Ticket Package Bundles

Runnable validation that proves the feature works end to end. Endpoint shapes are in
[contracts/api.md](./contracts/api.md); the transaction rules being validated are in
[contracts/checkout-transaction.md](./contracts/checkout-transaction.md); design rationale in
[research.md](./research.md).

## Prerequisites

```bash
docker compose up -d postgres migrate   # migrate exits once the schema is current
docker compose up -d api
cd frontend && npm run dev
```

Migration `000003_packages` must be applied. Verify:

```bash
docker compose run --rm migrate version   # expect: 3
```

On an existing dev database that predates the migration:

```bash
migrate -path backend/migrations -database "$TICKETING_DB" up
```

For the Go suite:

```bash
cd backend && ./scripts/setup-test-db.sh   # recreates ticketing_test, applies every migration
```

`SCHEMA.md` must match `migrations/` — the constitution makes it the source of truth for
`sqlc`. If `sqlc generate` produces a diff after the migration, the schema doc was not updated.

## Reference fixture

Matches the supplied designs and is assumed by every scenario below.

```
Event  "jive-2026"  (PUBLISHED)
├── Day 1        price 35000  quota 100
├── Day 2        price 35000  quota 100
└── Package "Day 1 and 2"  price 50000  ACTIVE
    ├── Day 1 × 1
    └── Day 2 × 1
```

Seed it through the admin API, or in Go tests via `testsupport.SeedPackage` /
`SeedPackageTicket`.

---

## Scenario 1 — The bundle appears with derived availability

```bash
curl -s localhost:8080/api/v1/events/jive-2026 | jq '.packages[0]'
```

**Expect**: `available_units: 100`, `purchasable: true`, two entries in `components`, and
**no quota field anywhere in the object**. Response carries `Cache-Control: no-store`.

Proves FR-001, FR-003, FR-008, FR-009, FR-011.

## Scenario 2 — Availability tracks the scarcest constituent

Set Day 2's remaining quota to 2 via `PUT /api/v1/admin/ticket-types/:id`, then re-read.

**Expect**: `available_units` drops to `2` while Day 1 still reads 100.
`limiting_ticket_type` names Day 2 on `GET /admin/packages/:id/availability`.

Set Day 2 to 0 and re-read.

**Expect**: `available_units: 0`, `purchasable: false` — with no administrator action taken
against the package itself.

Proves FR-011, FR-012, SC-008.

## Scenario 3 — Whole sets only

Change the Day 1 component to `quantity_per_unit: 2`, set Day 1 quota to 5, Day 2 to 100.

**Expect**: `available_units: 2`, not 2.5 and not 3. Integer division floors; the leftover
single Day 1 is never offered as a partial bundle.

Proves FR-011 and the whole-sets edge case.

## Scenario 4 — Buying a bundle deducts constituents

Restore quotas to 100/100, then:

```bash
curl -s -X POST localhost:8080/api/v1/checkout \
  -H 'Content-Type: application/json' -d '{
    "buyer_name":"Test","buyer_email":"t@x.com","buyer_phone":"0800",
    "items":[{"package_id":"<PKG>","quantity":1}],
    "attendees":[
      {"ticket_type_id":"<DAY1>","package_id":"<PKG>","name":"Ana","email":"ana@x.com"},
      {"ticket_type_id":"<DAY2>","package_id":"<PKG>","name":"Budi","email":"budi@x.com"}
    ]}' | jq
```

**Expect**: `201`, `total_amount: "50000.00"` — the package's own price, **not** 70000.
Both Day 1 and Day 2 drop to 99. `order_items` holds **one** row with `package_id` set and
`ticket_type_id` null. Two attendees exist, named differently.

Proves FR-018, FR-025, FR-026, FR-028, FR-029, FR-030.

## Scenario 5 — Attendee count must match the expansion

Repeat Scenario 4 with only one attendee, then with three.

**Expect**: `400` both times. The server compares against its own expansion, never a
client-declared count.

Proves the FR-028 guard.

## Scenario 6 — Mixed cart aggregates demand

Set Day 1 quota to **2**, Day 2 to 100. Submit one order containing
`1 × package` **and** `2 × Day 1` standalone.

**Expect**: `409` naming Day 1 — combined demand is 3 against 2 remaining. Day 1 must still
read 2 afterwards, and **no** order row exists.

This is the scenario that fails if aggregation is skipped and each line is checked in
isolation. Proves FR-019, FR-023, SC-006.

## Scenario 7 — Concurrency: exactly one winner

Set both quotas to 1 (one complete set), then fire 200 simultaneous bundle checkouts:

```bash
seq 200 | xargs -P 200 -I{} curl -s -o /dev/null -w '%{http_code}\n' \
  -X POST localhost:8080/api/v1/checkout -H 'Content-Type: application/json' \
  -d @/tmp/bundle-checkout.json | sort | uniq -c
```

**Expect**: exactly one `201`, 199 × `409`. Both quotas land at `0`, never negative, and each
`409` body names the exhausted ticket type.

Proves FR-021, FR-024, SC-003, SC-004. The Go equivalent belongs in
`internal/order/service_test.go` as a `t.Parallel` goroutine fan-out.

## Scenario 8 — Overlapping bundles do not deadlock

Add a second package `"Day 2 and 3"` over Day 2 + a new Day 3. Buy both packages
concurrently, in a loop, from two clients.

**Expect**: every request returns `201` or `409` — **never** a
`deadlock detected (SQLSTATE 40P01)`. Without the ascending `ticket_type_id` deduction order
this fails intermittently, so run at least a few hundred iterations before believing it.

Proves FR-022, research.md R-002.

## Scenario 9 — Expiry restores exactly

Note both quotas. Create a bundle order, let it pass `payment_expires_at` (or trigger the
sweeper), then re-read.

**Expect**: both quotas return to their exact pre-purchase values. Order status is `EXPIRED`.

Now replay a cancellation webhook for the same order.

**Expect**: `200`, and quotas **unchanged** — the guarded `PENDING` transition makes the
second release a no-op.

Proves FR-033, FR-034, SC-007.

## Scenario 10 — Admin authoring has no quota field

```bash
curl -s -X POST localhost:8080/api/v1/admin/packages -H "Authorization: Bearer $JWT" \
  -H 'Content-Type: application/json' -d '{ "event_id":"<EVT>", "name":"Day 1 and 2",
    "price":"50000.00","sales_start":"...","sales_end":"...","status":"ACTIVE",
    "components":[{"ticket_type_id":"<DAY1>","quantity_per_unit":1},
                  {"ticket_type_id":"<DAY2>","quantity_per_unit":1}]}'
```

**Expect**: `201`. Then confirm each rejection:

| Attempt | Expect |
| --- | --- |
| `"quota": 50` in the body | `400` — rejected, not silently ignored |
| empty `components` | `400` |
| a ticket type from another event | `400` |
| the same ticket twice | `400` |
| `quantity_per_unit: 0` | `400` |
| composition edit while a `PENDING` order exists | `409` |

Proves FR-014 – FR-017, FR-035, FR-036, R-004.

## Scenario 11 — Delete guards

| Attempt | Expect |
| --- | --- |
| delete a package that has been ordered | `400` |
| delete a ticket type used by a package | `400`, naming the packages |
| delete an event that has packages | `400` |
| delete an unsold package | `204`; its `package_tickets` rows are gone |

The `400`s must be clean API errors, not leaked constraint violations.

Proves FR-037, FR-038.

## Scenario 12 — Cross-event composition is impossible at the database level

With the service bypassed, insert a `package_tickets` row pointing at another event's ticket
type directly in `psql`.

**Expect**: foreign key violation. This must fail even when application validation is not in
the path.

Proves FR-015, research.md R-008.

## Scenario 13 — Sold counts include bundled sales

After Scenario 4, read `GET /api/v1/admin/ticket-types/:day1Id`.

**Expect**: `sold` includes the bundled unit, and `quota` is labelled **remaining**. A `sold`
that ignores package lines under-reports every bundled sale.

Proves FR-039.

## Scenario 14 — The selection screen

Open `http://localhost:3000/events/jive-2026` and walk the three design states:

1. **Empty** — three rows listed; only the bundle carries its badge; the Selected Ticket panel
   shows its placeholder; **Buy Ticket** disabled.
2. **One selected** — press **Add** on Day 1; the row becomes a `− 1 +` stepper; one summary
   line appears at Rp35.000; **Buy Ticket** enables.
3. **Two selected** — add Day 2; two summary lines, total Rp70.000.

Then check bundle-specific behaviour:

- adding the bundle produces **one** summary line at Rp50.000, not two day lines;
- decrementing from 1 returns the row to **Add** and drops it from the summary;
- with `available_units: 2`, the stepper refuses to pass 2;
- an unavailable bundle cannot be added.

Proves FR-001 – FR-007, FR-013, SC-001, SC-002.

---

## Regression checks

The migration changes an existing column's nullability, so confirm nothing that predates
packages broke:

```bash
cd backend && ./scripts/test.sh ./...
cd frontend && npx vitest run
```

Specifically:

- **Existing individual-ticket checkout still works** — deducts, expires, restores as before.
- **`sqlc generate` is clean** — no diff after the migration and `SCHEMA.md` update.
- **`architecture_test.go` passes** — no `internal/order` → `internal/event` import appeared,
  and no `eventsql` struct leaked into a response.
- **Orders predating packages still render** — every line reads as `kind: "ticket"`.

## Done when

- [ ] Scenarios 1–14 pass
- [ ] Scenario 7 shows exactly one winner and no negative quota
- [ ] Scenario 8 runs several hundred iterations with zero deadlocks
- [ ] Full Go and frontend suites green
- [ ] `SCHEMA.md` and PRD.md §1.5 updated in the same change as the migration

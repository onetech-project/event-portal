# Contract: Availability & the race-condition-safe package checkout

Companion to `../spec.md`. This is the load-bearing document of the feature: it is where
"a package has no quota" stops being a schema decision and becomes a runtime guarantee.

## 0. The one rule everything follows

> A package is never an inventory object. It is a **function over** the remaining quota of
> its constituent ticket types — read-time for availability, write-time for deduction.

Consequences, all of which the rest of this document implements:

- there is no `packages.quota` to read, write, lock or reconcile;
- checkout never "deducts a package". It expands the package into per-ticket-type demand and
  reuses the **existing** `CheckAndDeductQuota`, so individual and bundled sales contend on
  exactly one guarded write path;
- availability shown in the list is advisory. The guarded `UPDATE` at checkout is the only
  authority.

---

## 1. Availability query

### 1.1 One package

```sql
-- name: GetPackageAvailability :one
-- Whole complete sets only: integer division floors, so a constituent with 5 remaining
-- consumed 2-at-a-time yields 2 units, not 2.5. MIN across constituents because the
-- scarcest one governs.
SELECT
    MIN(tt.quota / pt.quantity)::int AS available_units,
    -- The scarcest constituent, reported to the guest when checkout is refused.
    (ARRAY_AGG(tt.id ORDER BY (tt.quota / pt.quantity) ASC, tt.id ASC))[1] AS limiting_ticket_type_id
FROM package_tickets pt
JOIN ticket_types tt ON tt.id = pt.ticket_type_id
WHERE pt.package_id = sqlc.arg(package_id);
```

A package with no constituents returns a single `NULL` row — treated as **unavailable**,
which is the correct reading of "a bundle containing nothing" (FR-016).

### 1.2 The booking-list query — every package of an event, in one round trip

This is the query behind the focus screen. It must not be N+1 across packages.

```sql
-- name: ListPackagesWithAvailabilityByEventID :many
-- Purchasability folds in every gate at once so the client renders a single boolean and
-- cannot disagree with the server about what is buyable:
--   * available_units > 0
--   * the package's own sales window is open
--   * EVERY constituent's sales window is open (the binding constraint, FR-012)
--   * the package is ACTIVE
-- Computed per request against live quota; never cached (FR-008).
WITH component_stats AS (
    SELECT
        pt.package_id,
        MIN(tt.quota / pt.quantity)::int AS available_units,
        (ARRAY_AGG(tt.id ORDER BY (tt.quota / pt.quantity) ASC, tt.id ASC))[1]
            AS limiting_ticket_type_id,
        BOOL_AND(tt.sales_start <= now() AND tt.sales_end >= now())
            AS all_components_on_sale,
        COUNT(*) AS component_count
    FROM package_tickets pt
    JOIN ticket_types tt ON tt.id = pt.ticket_type_id
    GROUP BY pt.package_id
)
SELECT
    p.id, p.event_id, p.name, p.description, p.price,
    p.sales_start, p.sales_end, p.status,
    COALESCE(cs.available_units, 0)::int AS available_units,
    cs.limiting_ticket_type_id,
    (
        COALESCE(cs.available_units, 0) > 0
        AND COALESCE(cs.component_count, 0) > 0
        AND COALESCE(cs.all_components_on_sale, FALSE)
        AND p.status = 'ACTIVE'
        AND p.sales_start <= now()
        AND p.sales_end   >= now()
    ) AS purchasable
FROM packages p
LEFT JOIN component_stats cs ON cs.package_id = p.id
WHERE p.event_id = sqlc.arg(event_id)
ORDER BY p.price ASC, p.name ASC;
```

`LEFT JOIN` rather than `JOIN`: a constituent-less package must still be *listed* to an
administrator as broken, not silently vanish.

### 1.3 Composition, for rendering and for checkout

```sql
-- name: ListPackageComponentsByPackageIDs :many
-- Batched over the whole list so rendering "what's inside this bundle" stays one query.
SELECT pt.package_id, pt.ticket_type_id, pt.quantity AS quantity_per_unit,
       tt.name AS ticket_type_name, tt.sales_start, tt.sales_end, tt.price
FROM package_tickets pt
JOIN ticket_types tt ON tt.id = pt.ticket_type_id
WHERE pt.package_id = ANY(sqlc.arg(package_ids)::uuid[])
ORDER BY pt.package_id, tt.name;
```

---

## 2. Checkout transaction

### 2.1 Shape, and why it is shaped this way

The constitution requires the order, its lines, its attendees and every quota deduction to
commit as one transaction, and forbids any external network call inside it. Packages do not
change that shape — they change what goes *into* the deduction set.

```
TX1  (single BEGIN..COMMIT, no network calls)
  1. resolve + validate every selected line, server-side
  2. EXPAND packages into per-ticket-type demand
  3. AGGREGATE demand per ticket type across the whole selection
  4. SORT the aggregate by ticket_type_id            <- deadlock prevention
  5. deduct each ticket type, guarded, in that order <- oversell prevention
  6. insert order, order_items, attendees
COMMIT

TX2  (after commit, outside any transaction)
  7. call the payment gateway
  8. short transaction: persist QR payload / deadline
     on gateway failure: cancel order + restore quota (compensation)
```

Steps 3 and 4 are the two additions packages force, and each prevents a distinct failure.

### 2.2 Step 2 — expansion

For each selected line:

| Line kind | Demand contributed |
| --- | --- |
| ticket, quantity `q` | `{ticket_type_id: q}` |
| package, quantity `q` | for each component `c`: `{c.ticket_type_id: q * c.quantity_per_unit}` |

Validation applied per line before expansion, all against server-stored values:

- the ticket type / package exists and belongs to the requested, `PUBLISHED` event;
- the package is `ACTIVE`;
- `now()` falls inside the package's sales window **and** inside every constituent's window;
- the package has at least one component;
- quantity ≥ 1.

### 2.3 Step 3 — aggregation, and the bug it prevents

**This is the step most implementations miss.** A selection of

```
1 × Bundle "Day 1 + Day 2"   (consumes 1 Day 1, 1 Day 2)
2 × Ticket "Day 1"           (consumes 2 Day 1)
```

places demand of **3** on Day 1, not "1, checked separately, then 2, checked separately".
With only 2 Day 1 remaining, per-line checking passes the bundle (1 ≤ 2), passes the ticket
line (2 ≤ remaining-after-bundle = 1? — only if the first deduction already landed), and in
any interleaving where the checks are independent it oversells. Aggregating first makes the
question unambiguous: *does Day 1 have 3?*

```go
// demand is keyed by ticket type, summed across the ENTIRE selection — standalone lines
// and every package expansion together. Two lines touching the same ticket type must
// never be validated in isolation from each other.
demand := map[uuid.UUID]int32{}
for _, item := range req.Items {
    switch {
    case item.TicketTypeID != nil:
        demand[*item.TicketTypeID] += item.Quantity
    case item.PackageID != nil:
        pkg, err := events.PackageForCheckout(ctx, tx, *item.PackageID)
        if err != nil {
            return err
        }
        for _, c := range pkg.Components {
            demand[c.TicketTypeID] += item.Quantity * c.PerUnit
        }
    }
}
```

### 2.4 Step 4 — deterministic ordering, and the deadlock it prevents

The guarded `UPDATE` takes a row lock held until commit. Two concurrent checkouts touching
the same two ticket types in opposite orders deadlock:

```
        TX A                             TX B
  UPDATE Day 1  (holds lock)       UPDATE Day 2  (holds lock)
  UPDATE Day 2  --- waits on B     UPDATE Day 1  --- waits on A
                        >>> deadlock <<<
```

Packages make this likely rather than exotic: a bundle touches several ticket types at once
by construction, and two overlapping bundles ("Day 1 + Day 2" and "Day 2 + Day 3") produce
exactly the crossing pattern above. The fix is a **total order every transaction obeys** —
sorting by `ticket_type_id` is arbitrary but globally consistent, which is all that is
required.

```go
ids := make([]uuid.UUID, 0, len(demand))
for id := range demand {
    ids = append(ids, id)
}
// Deterministic global lock order. Any total order works; what matters is that EVERY
// transaction uses the same one, so no two can hold each other's next lock.
// Iterating the map directly would be non-deterministic and reintroduce the deadlock.
sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
```

### 2.5 Step 5 — guarded deduction

Unchanged from what already ships, and reused verbatim. This is the point of expanding
packages instead of giving them their own path:

```sql
-- name: CheckAndDeductQuota :one
UPDATE ticket_types
SET quota = quota - sqlc.arg(qty), updated_at = now()
WHERE id = sqlc.arg(id) AND quota >= sqlc.arg(qty)
RETURNING quota;
```

`WHERE ... AND quota >= qty` makes the check and the write one atomic statement. Zero rows
affected means insufficient stock — never a negative value, and never a stale read acted on
later. The `CHECK (quota >= 0)` constraint is the belt to this braces.

```go
for _, ttID := range ids {
    if err := events.CheckAndDeductQuota(ctx, tx, ttID, demand[ttID]); err != nil {
        if errors.Is(err, ErrInsufficientQuota) {
            // Whole order fails; the transaction rolls back, so lines deducted earlier
            // in this loop are released automatically. No manual compensation here.
            return NewSoldOutError(ttID) // names the exhausted ticket type (FR-024)
        }
        return err
    }
}
```

Rolling back is what makes all-or-nothing free: a partially deducted order never becomes
visible, so FR-023 needs no cleanup code.

### 2.6 Step 6 — persisting lines and attendees

```go
// One order_items row per SELECTED line, not per expanded constituent. A package line is
// stored once at the package's own price, so total_amount = SUM(quantity * price) stays
// exact and no synthetic per-constituent price split is invented.
for _, item := range req.Items {
    switch {
    case item.TicketTypeID != nil:
        createOrderItem(orderID, ticketTypeID: item.TicketTypeID, packageID: nil,
                        qty: item.Quantity, price: ticketTypes[*item.TicketTypeID].Price)
    case item.PackageID != nil:
        createOrderItem(orderID, ticketTypeID: nil, packageID: item.PackageID,
                        qty: item.Quantity, price: packages[*item.PackageID].Price)
    }
}
```

Total, computed server-side only (FR-025):

```
total_amount = Σ(ticket line)  quantity × ticket_types.price
             + Σ(package line) quantity × packages.price
```

Attendees, one row per constituent unit (the answered clarification — separate registrant per
constituent, possibly different people):

```
for a package line of quantity q, for each component c:
    q × c.quantity_per_unit attendee rows
    each with ticket_type_id = c.ticket_type_id
              package_id     = the package
              name/email     = that slot's own submitted values
```

Server-side validation of the submitted attendee set: grouped by
`(ticket_type_id, package_id)`, the count must equal the expansion above exactly — no more,
no fewer. This is the guard that stops a client from paying for two bundle units and
registering four people.

### 2.7 Steps 7–8 — payment, outside the transaction

Unchanged. The gateway call stays outside TX1 because the deduction holds row locks until
commit: a network round-trip inside would serialise every concurrent buyer of a shared ticket
type behind it. Packages amplify this — a bundle holds locks on *several* hot ticket types at
once, so a gateway call inside the transaction would serialise buyers of every ticket the
bundle touches, not just one.

On gateway failure: cancel the order and restore quota, using §3.

## 3. Release — expiry, cancellation, failed payment

Restoration mirrors deduction and is driven off `order_items`, which is the only durable
record of what an order consumed.

```sql
-- name: ListQuotaHoldsByOrderID :many
-- Resolves an order's total per-ticket-type hold, expanding package lines through the
-- junction. This is the exact inverse of the checkout aggregation, so restore always
-- returns precisely what deduct took — including for a package whose composition has
-- since been edited, because... see the caveat below.
SELECT ticket_type_id, SUM(qty)::int AS qty
FROM (
    SELECT oi.ticket_type_id, oi.quantity AS qty
    FROM order_items oi
    WHERE oi.order_id = sqlc.arg(order_id) AND oi.ticket_type_id IS NOT NULL

    UNION ALL

    SELECT pt.ticket_type_id, oi.quantity * pt.quantity AS qty
    FROM order_items oi
    JOIN package_tickets pt ON pt.package_id = oi.package_id
    WHERE oi.order_id = sqlc.arg(order_id) AND oi.package_id IS NOT NULL
) holds
GROUP BY ticket_type_id
ORDER BY ticket_type_id;  -- same deterministic order as deduction
```

> **Caveat, and the reason composition edits need a rule.** This query reconstructs the hold
> from the package's *current* composition. If an administrator edits a bundle's composition
> while unpaid orders for it are open, restoration will not match what was deducted, and
> quota will drift. Two acceptable resolutions, to be settled during `/speckit-plan`:
> **(a)** reject composition edits on a package with open `PENDING` orders (simplest,
> recommended); or **(b)** snapshot the expansion at checkout into a dedicated hold table and
> restore from the snapshot. Do not ship without one of them.

Idempotency (FR-034) is already carried by the existing guarded status transition:

```sql
UPDATE orders SET status = $2, updated_at = now()
WHERE id = $1 AND status = 'PENDING';
```

Zero rows affected ⇒ the order already moved ⇒ **return early and restore nothing**. A
duplicate expiry-then-cancel pair therefore restores exactly once. The restore loop runs in
the same transaction as that guarded update, never before it.

```go
if rows, _ := q.UpdateOrderStatusIfPending(ctx, orderID, newStatus); rows == 0 {
    return nil // already transitioned; restoring here would double-credit quota
}
for _, h := range holds { // holds arrive pre-sorted by ticket_type_id
    if err := events.RestoreQuota(ctx, tx, h.TicketTypeID, h.Qty); err != nil {
        return err
    }
}
```

## 4. Failure modes and their responses

| Situation | Detected at | Response |
| --- | --- | --- |
| Package sold out before submit | step 5, zero rows affected | `409` naming the exhausted ticket type |
| Aggregate demand exceeds stock across mixed lines | step 3 + 5 | `409`, whole order rejected |
| Two buyers, one remaining set | step 5, guarded `UPDATE` | exactly one commits; other gets `409` |
| Overlapping bundles, concurrent | step 4 ordering | both serialise cleanly, no deadlock |
| Package's own window closed | step 2 | `400` |
| A constituent's window closed | step 2 | `400`, package not purchasable |
| Package has no components | step 2 / §1.1 `NULL` | `400` (guest) / listed as broken (admin) |
| Attendee count ≠ expansion | step 6 validation | `400` |
| Gateway call fails | TX2 | cancel order, restore quota via §3 |
| Duplicate release signal | guarded status `UPDATE` | no-op, `200` |

## 5. Test obligations

1. **Oversell, single bundle** — N goroutines buy the last set concurrently; exactly 1
   succeeds, quota lands at 0, never negative.
2. **Oversell, mixed line** — 1 bundle + 2 standalone of the same ticket against 2 remaining;
   rejected. This is the test that fails if aggregation (§2.3) is skipped.
3. **Deadlock** — two overlapping bundles bought concurrently in opposing natural order;
   completes with no deadlock error. Fails if the sort (§2.4) is skipped.
4. **Whole sets** — constituent quota 5, per-unit 2 ⇒ availability 2, and buying 3 is
   rejected.
5. **Scarcest constituent** — quotas 5 and 2 ⇒ availability 2; drop the second to 0 ⇒
   unavailable while the first still has stock.
6. **All-or-nothing** — available bundle plus sold-out standalone ticket in one order ⇒
   nothing deducted anywhere.
7. **Restore exactness** — buy, expire, and assert every constituent returns to its exact
   pre-purchase quota.
8. **Restore idempotency** — replay expiry and cancellation for one order; quota restored
   once.
9. **Attendee expansion** — a two-ticket bundle at quantity 1 requires exactly 2 registrant
   slots; 1 and 3 are both rejected.
10. **Server-side pricing** — a client sending its own prices is charged the stored ones.
11. **Cross-event composition** — attempting to add another event's ticket to a bundle is
    rejected by the database, not merely by the service.

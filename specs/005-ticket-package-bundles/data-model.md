# Phase 1 Data Model: Ticket Package Bundles

**Feature**: `005-ticket-package-bundles` | **Date**: 2026-08-04

Entity-level view. Exact DDL is in [contracts/schema.md](./contracts/schema.md); Go and
TypeScript shapes are in [contracts/models.md](./contracts/models.md).

**Naming**: the spec's *ticket* (quota-bearing) is the existing `ticket_types` table; the
existing `tickets` table holds *issued passes*. Nothing is renamed.

---

## 1. New entities

### 1.1 Package

A named, priced bundle offer belonging to one event. **Holds no inventory.**

| Field | Type | Rules |
| --- | --- | --- |
| `id` | UUID | PK |
| `event_id` | UUID | FK → `events(id)`, `ON DELETE RESTRICT`, required |
| `name` | VARCHAR(255) | required, non-empty |
| `description` | TEXT | optional |
| `price` | NUMERIC(12,2) | required, `>= 0`. **Not** validated against the sum of constituents |
| `sales_start` | TIMESTAMPTZ | required |
| `sales_end` | TIMESTAMPTZ | required, `> sales_start` |
| `status` | VARCHAR(50) | `ACTIVE` \| `INACTIVE`, default `ACTIVE` |
| `created_at`/`updated_at` | TIMESTAMPTZ | managed |

**Absent by design**: `quota`, `stock`, `inventory`, `remaining`, `capacity`. No column, no
DTO field, no request field. A package's availability is a *function over* its constituents,
never a stored number. Adding one would create a second source of truth and reintroduce the
overselling this design exists to prevent.

**Relationships**: belongs to one `Event`; has many `PackageComponent` (≥1); referenced by
`OrderItem` and `Attendee`.

### 1.2 PackageComponent (`package_tickets`)

One line of a package's composition.

| Field | Type | Rules |
| --- | --- | --- |
| `id` | UUID | PK |
| `package_id` | UUID | part of composite FK → `packages(id, event_id)`, `ON DELETE CASCADE` |
| `ticket_type_id` | UUID | part of composite FK → `ticket_types(id, event_id)`, `ON DELETE RESTRICT` |
| `event_id` | UUID | denormalised; shared by both composite FKs |
| `quantity` | INT | required, `> 0`, default 1 — units of this ticket per **one** package unit |
| `created_at` | TIMESTAMPTZ | managed |

**Constraints**: `UNIQUE (package_id, ticket_type_id)` — a ticket appears at most once per
package; repeats are expressed through `quantity`.

`event_id` exists solely so the two composite FKs make cross-event composition
*unrepresentable* rather than merely discouraged (research.md R-008). It cannot drift: both
FKs resolve against the same value in the same row.

**Relationships**: belongs to one `Package`; references one `TicketType`.

---

## 2. Changed entities

### 2.1 OrderItem — becomes a discriminated line

| Field | Change |
| --- | --- |
| `ticket_type_id` | **now nullable** (was NOT NULL) |
| `package_id` | **new**, nullable, FK → `packages(id)`, `ON DELETE RESTRICT` |
| `quantity` | `CHECK (quantity > 0)` added |

**Invariant** (`order_items_line_kind_chk`): exactly one of `ticket_type_id` / `package_id` is
non-null.

| Line kind | `ticket_type_id` | `package_id` | `price` holds |
| --- | --- | --- | --- |
| ticket | set | null | the ticket's price at purchase |
| package | null | set | the **package's own** price at purchase |

A package line is stored **once**, not decomposed. `total_amount` therefore stays exactly
`SUM(quantity × price)` with no invented per-constituent price allocation (research.md R-003).

> **Ripple**: this nullability change regenerates `OrderItem.TicketTypeID` as
> `uuid.NullUUID` for *every* existing query that selects it, breaking compilation across
> `internal/order`. That is intended — the compiler enumerates each place that must now decide
> what a package line means. See research.md R-005.

### 2.2 Attendee — gains provenance

| Field | Change |
| --- | --- |
| `ticket_type_id` | **unchanged, stays NOT NULL** |
| `package_id` | **new**, nullable, FK → `packages(id)`, `ON DELETE RESTRICT` |

Keeping `ticket_type_id` non-null for bundle registrants is what preserves the constitution's
one-pass-per-attendee rule without amendment: a bundle produces *more attendees*, and pass
generation is untouched. `package_id` records only which bundle the slot came from, for
grouping in the PDF/email and for admin exports.

### 2.3 TicketType — no schema change, one semantic addition

`quota` remains the sole, *remaining* counter. The derived `sold` figure must now count
package lines through the junction; a `sold` reading only `order_items.ticket_type_id` would
under-report every bundled sale. Query in [contracts/api.md](./contracts/api.md).

### 2.4 Event — no schema change

Gains a `packages[]` collection in its detail response and one more delete guard.

---

## 3. Derived values — computed, never stored

| Value | Definition | Where |
| --- | --- | --- |
| `available_units` | `MIN(tt.quota / pt.quantity)` over the package's components; integer division floors, so whole sets only | availability CTE |
| `limiting_ticket_type_id` | the component minimising the above; named in sold-out errors | availability CTE |
| `purchasable` | `available_units > 0` **AND** `component_count > 0` **AND** package `ACTIVE` **AND** package window open **AND** *every* component's window open | availability CTE |
| `sold` (package) | `SUM(order_items.quantity)` over that package's lines | admin query |
| `sold` (ticket type) | standalone lines **+** package lines expanded through the junction | admin query |
| order `total_amount` | `SUM(quantity × price)` from stored server-side prices only | checkout TX1 |

`purchasable` is composed server-side so the client renders one boolean and cannot disagree
with the server about what is buyable. A componentless package yields `NULL` from the
aggregate and is treated as unavailable — the correct reading of "a bundle containing
nothing".

---

## 4. Validation rules

### Package create / update

| Rule | Failure |
| --- | --- |
| `components` non-empty | `400` |
| every component's ticket belongs to the package's event | `400` (also blocked by composite FK) |
| no duplicate ticket within one package | `400` (also blocked by unique constraint) |
| `quantity >= 1` per component | `400` |
| `price >= 0` | `400` |
| `sales_end > sales_start` | `400` |
| any quota-like field present in the body | `400` — **reject, do not ignore** |
| composition change while a `PENDING` order exists | `409` (research.md R-004) |
| `event_id` on update | rejected — a package never moves between events |

### Checkout

| Rule | Failure |
| --- | --- |
| each item has exactly one of `ticket_type_id` / `package_id` | `400` |
| package is `ACTIVE`, its window is open, event is `PUBLISHED` | `400` |
| every component's window is open | `400` |
| package has ≥1 component | `400` |
| attendee counts grouped by `(ticket_type_id, package_id)` equal the server's own expansion, exactly | `400` |
| aggregated demand per ticket type ≤ remaining quota | `409`, naming the exhausted ticket |
| prices supplied by the client | ignored — total recomputed server-side |

### Deletion guards

| Attempt | Result |
| --- | --- |
| delete package referenced by `order_items` or `attendees` | `400` |
| delete ticket type that is a component of any package | `400`, naming the packages |
| delete event with any package | `400` |
| delete package with components, no orders | `204`; components cascade |

---

## 5. Lifecycle

### Package status

```
        create (default)
             │
             ▼
        ┌─ ACTIVE ─┐        ACTIVE   → listed publicly, purchasable if available
        │          │        INACTIVE → admin-visible only, never listed or sold
        └─ INACTIVE ┘
              deactivate / reactivate, freely
```

Status changes never touch inventory: there is none to touch. Deactivating a package removes
it from the list; the quota its open orders hold stays held until those orders resolve.

### Quota across an order's life

```
checkout      aggregate cart demand per ticket type
              → deduct each, sorted by ticket_type_id, guarded UPDATE
              → order PENDING

PAID          nothing further; the deduction stands
              → one pass issued per attendee

EXPIRED /     guarded transition from PENDING (idempotent)
CANCELLED     → expand order_items package lines through the junction
              → restore each ticket type by exactly what was taken
```

Restore is the exact inverse of deduct, driven off `order_items` — the only durable record of
what an order consumed — and is guarded by the existing
`UPDATE orders ... WHERE status = 'PENDING'`. Zero rows affected means the order already
moved, so nothing is restored and a duplicated release signal cannot double-credit.

---

## 6. Invariants

| Invariant | Enforced by |
| --- | --- |
| Packages hold no inventory | absence of any quota column; no code path writes stock to `packages` |
| `ticket_types.quota` never negative | `CHECK (quota >= 0)` + guarded atomic `UPDATE` |
| A bundle never spans events | composite FKs on `package_tickets` |
| A ticket appears once per bundle | `UNIQUE (package_id, ticket_type_id)` |
| Per-component count ≥ 1 | `CHECK (quantity > 0)` |
| An order line is ticket XOR package | `order_items_line_kind_chk` |
| Ticket in a bundle cannot be deleted | `ON DELETE RESTRICT` on the composite ticket FK |
| Sold bundle cannot be deleted | `ON DELETE RESTRICT` from `order_items`/`attendees` |
| Composition dies with its bundle | `ON DELETE CASCADE` on the composite package FK |
| Every attendee maps to one ticket | `attendees.ticket_type_id` stays NOT NULL |
| `total_amount = SUM(quantity × price)` | package lines stored whole, at the package's price |
| No deadlock between concurrent carts | deterministic ascending `ticket_type_id` deduction order |

---

## 7. Test data shape

The reference fixture, matching the supplied designs, used throughout
[quickstart.md](./quickstart.md):

```
Event  "jive-2026"  (PUBLISHED)
├── TicketType  "Day 1"        price 35000  quota 100
├── TicketType  "Day 2"        price 35000  quota 100
└── Package     "Day 1 and 2"  price 50000  ACTIVE     <- no quota column
    ├── component → Day 1, quantity_per_unit 1
    └── component → Day 2, quantity_per_unit 1

available_units = MIN(100/1, 100/1) = 100
```

`internal/testsupport` gains `SeedPackage` and `SeedPackageTicket`, and its truncate list
gains `packages` and `package_tickets` (before `ticket_types`, respecting FK order).

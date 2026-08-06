# Data Model: Single Visitor Form per Bundle

**Feature**: [spec.md](spec.md) | **Research**: [research.md](research.md) | **Date**: 2026-08-05

## 1. Schema change (migration `000011_attendee_package_unit`)

One additive, nullable column. `SCHEMA.md` (attendees table, §indexes) must be updated in the same change (constitution: SCHEMA.md is the source of truth).

```sql
-- up
ALTER TABLE attendees ADD COLUMN package_unit SMALLINT
    CHECK (package_unit IS NULL OR package_unit >= 1);
COMMENT ON COLUMN attendees.package_unit IS
    'Ordinal of the purchased bundle unit this slot belongs to (1..quantity of its package order line). NULL for standalone-ticket slots and for bundle slots booked before this column existed.';

-- down
ALTER TABLE attendees DROP COLUMN package_unit;
```

Invariants (enforced by booking code, not DDL):

- `package_unit IS NOT NULL` ⇒ `package_id IS NOT NULL` (only bundle slots get a unit).
- For one order and one package line of quantity Q, units are exactly `1..Q`, and each unit's slots match the package's per-unit composition (component ticket type × component quantity).
- Standalone slots: `package_id IS NULL AND package_unit IS NULL`.
- Pre-migration bundle slots: `package_id IS NOT NULL AND package_unit IS NULL` — legal, handled by the frontend fallback (one form per slot, today's behavior).

No index: slots are always read via the existing `idx_attendees_order_id`.

## 2. Entity changes

### Attendee slot (`attendees`) — gains unit provenance

| Field | Type | Change |
|---|---|---|
| `package_unit` | `SMALLINT NULL` | NEW — bundle-unit ordinal per §1 |

All other fields unchanged. Slot **count** per order is untouched — ticket issuance (1 ticket per attendee row) is therefore untouched (spec FR-004).

### Bundle Unit (conceptual — no table)

A bundle unit is the slot set `{slots | same order, same package_id, same package_unit}`. It is the unit of visitor identity: one form fills all its slots with identical data. It exists only as this grouping; nothing else references it.

## 3. Go type changes (`internal/order`)

- `AttendeeRef` ([repository.go](../../backend/internal/order/repository.go)): gains `PackageUnit *int16` (set only for package lines), passed through `CreateAttendeeSlot` → `queries/order.sql`.
- `AttendeeSlotRecord`: gains `PackageUnit *int16` (scanned from the new column; `PackageID uuid.NullUUID` already exists).
- `ExpandedItem` ([demand.go](../../backend/internal/order/demand.go)): gains `PerUnitDemand map[uuid.UUID]int32` for package lines (component ticket type → quantity **per single unit**); the existing aggregate `Demand` stays as `PerUnitDemand × Quantity` and keeps feeding quota deduction unchanged. Slot creation in `bookOnce` ([service.go:266-284](../../backend/internal/order/service.go#L266-L284)) switches for package lines to: `for unit in 1..Quantity → for each sorted ticket type in PerUnitDemand → for each of its per-unit count → CreateAttendeeSlot(ref{TicketTypeID, PackageID, PackageUnit: unit})`. Ticket lines keep the current loop with `PackageUnit = nil`.

## 4. Wire DTO changes (`internal/order/dto.go` ↔ `frontend/lib/types.ts`)

`TicketOrderSlot` (GET `/ticket/order/:order_id`) — additive, backward-compatible:

| Field | JSON | Type | Semantics |
|---|---|---|---|
| PackageID | `package_id` | `uuid \| null` | NEW — package origin id (name alone is not a safe grouping key) |
| PackageUnit | `package_unit` | `int \| null` | NEW — bundle-unit ordinal; null = standalone slot or pre-migration bundle slot |

`ListAttendeeSlotsByOrderID` ordering changes to `ORDER BY a.package_id NULLS FIRST, a.package_unit, a.id` so standalone slots come first and each unit's slots arrive contiguous (see research R6).

`CheckoutFormsRequest` / `CheckoutVisitor`: **unchanged** (client fan-out, research R2).

## 5. Frontend derived model (`components/order/slot-groups.ts` — NEW)

```ts
type SlotGroup = {
  key: string;          // slot id (standalone/fallback) or `${package_id}:${package_unit}`
  slotIds: string[];    // every slot this group's form fills (length ≥ 1)
  title: string;        // bundle name for bundle groups; ticket type name otherwise
  ticketCount: number;  // slotIds.length — drives the "N tickets" badge
  isBundle: boolean;
  unitLabel: string | null; // "Visitor <n>" only when the same package has >1 unit
};
groupOrderSlots(slots: TicketOrderSlot[]): SlotGroup[]
```

Grouping rules (research R4/R5): standalone slot → own group; bundle slot with `package_unit === null` → own group titled with the ticket type name plus the existing package badge (fallback = today's rendering); bundle slot with a unit → grouped by `(package_id, package_unit)`, titled `package_name`.

Form state: `attendees[i]` corresponds to `groups[i]` and carries `slot_ids` (hidden) instead of a single `id`; submission fans each group entry out to one `CheckoutVisitor` per slot id and keeps a `payloadIndex → groupIndex` map for server-error path remapping.

## 6. Validation rules

| Where | Rule | Failure shape |
|---|---|---|
| Frontend (Zod, per group) | name/email/phone/dob/gender — unchanged field rules, now validated once per group | inline field errors |
| Backend `matchVisitorsToSlots` | unchanged — exact 1:1 slot coverage of the fanned-out payload | existing 400001 field map |
| Backend `CheckoutOrder` (NEW, after slot matching, before TX-D) | visitors mapped to slots sharing `(package_id, package_unit)` (both non-null) must be field-identical (name, email, phone, dob, gender) | 400001, `attendees[<payload idx>].<field>` → "All tickets in the same bundle must use the same visitor information." |
| Backend booking | units stamped `1..Q` with per-unit composition (§3) | not reachable via API misuse; guarded by tests |

## 7. State transitions

None added. Order lifecycle (PENDING → PAID/EXPIRED/…), quota flow, and ticket issuance are untouched.

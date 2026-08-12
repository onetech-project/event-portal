# Phase 1 Data Model: Per-Ticket Event Dates

**Feature**: [spec.md](./spec.md) | **Research**: [research.md](./research.md)

## Storage change

One table changes. Nothing else in `SCHEMA.md` moves.

### `ticket_types` — two new columns

```sql
event_start TIMESTAMP WITH TIME ZONE NOT NULL,
event_end   TIMESTAMP WITH TIME ZONE NOT NULL,
CONSTRAINT ticket_types_event_window_chk CHECK (event_end >= event_start)
```

| Column | Type | Null | Meaning |
|---|---|---|---|
| `event_start` | `timestamptz` | NOT NULL | The instant admission opens for this ticket type. Not showtime — validation admits no tolerance (FR-014), so this is the earliest instant a holder can be admitted. |
| `event_end` | `timestamptz` | NOT NULL | The last instant a holder can be admitted. Inclusive. |

**Why `>=` and not `>`** — see [research.md](./research.md) D-001. An event with
`start_date == end_date` is legal today (`admin_dto.go:113-115` refuses only *before*), and
FR-004's backfill copies the event's dates verbatim, so a strict check would fail the
migration on real data. This deliberately diverges from `packages_sales_window_chk`, which
is strict.

**Relationship to the existing sales window.** None. `sales_start`/`sales_end` govern
purchase; `event_start`/`event_end` govern admission. FR-003 forbids any cross-constraint
in either direction. A ticket can legitimately remain on sale after its event window has
passed.

**Relationship to the parent event.** `event_start >= events.start_date` and
`event_end <= events.end_date`, enforced in the service at ticket-type write time only
(FR-005). Deliberately **not** a database constraint and **not** enforced on the event
side — see the deadlock argument in FR-005a.

### Migration `000014_ticket_type_event_window`

Three steps, following `000013_master_list_identity_and_flags.up.sql:61-75`:

1. `ALTER TABLE ticket_types ADD COLUMN event_start timestamptz, ADD COLUMN event_end timestamptz;`
2. `UPDATE ticket_types tt SET event_start = e.start_date, event_end = e.end_date FROM events e WHERE e.id = tt.event_id;`
3. `ALTER COLUMN … SET NOT NULL` on both, then add the CHECK.

The backfill is total: `event_id` is `NOT NULL REFERENCES events(id)`, so every row has a
parent and no row survives step 2 unset. Down migration drops both columns; the constraint
goes with them. Not lossy in the sense `000012`/`000013` label — the pre-feature system had
no such data.

## Entities as each layer sees them

The window crosses six representations. Adding a field means touching each in turn.

### 1. Event domain — public read

`event.TicketTypeSummary` (`backend/internal/event/dto.go:34-43`) gains:

```go
EventStart time.Time `json:"event_start"`
EventEnd   time.Time `json:"event_end"`
```

Populated in `Service.TicketTypesForEventSlug` (`service.go:178-190`), inside the
`cache.Through` loader. **This is the cached shape** — see the deploy hazard in
[research.md](./research.md) D-002.

Query: `ListTicketTypesByEventID` (`queries/event.sql:14-18`) adds the two columns.

### 2. Event domain — admin read

`event.TicketTypeAdminView` (`admin_dto.go:58-69`) gains the same two fields. Feeds the
admin ticket-type table and, via `EventAdminDetail` (`admin_dto.go:43-46`), the
frontend-derived stranded-window warning of FR-005b.

Query: `ListTicketTypesAdmin` (`queries/event.sql:110-114`) and `GetTicketTypeByID`
(`:20`) add the two columns.

### 3. Event domain — admin write

`event.TicketTypeRequest` (`admin_dto.go:126-141`) gains:

```go
EventStart time.Time `json:"event_start"`
EventEnd   time.Time `json:"event_end"`
```

Validation splits across two layers, because containment needs the database:

| Rule | Layer | Location |
|---|---|---|
| both present (FR-001) | `TicketTypeRequest.Validate` | `admin_dto.go:143-165` |
| `event_end >= event_start` (FR-002) | `TicketTypeRequest.Validate` | same |
| window within parent event (FR-005) | `Service.CreateTicketType` / `UpdateTicketType` | `admin_service.go:236` / `:274` |

`AdminTicketTypeParams` (`admin_repository.go`) and the `CreateTicketType` /
`UpdateTicketType` SQL (`queries/event.sql:121`, `:126`) carry the two columns through.

### 4. Event domain — cross-domain display records

`event.TicketTypeDisplayRecord` (`admin_repository.go:208-216`) gains `EventStart` /
`EventEnd`. Query `ListTicketTypeDisplaysByIDs` (`queries/event.sql:50-58`) adds
`tt.event_start`, `tt.event_end` — note it already selects `e.start_date AS
event_start_date`, so the new columns must be named distinctly to avoid collision.

`event.PackageDisplayRecord` (`package_repository.go:236-244`) gains a **derived** span.
Query `ListPackageDisplaysByIDs` (`queries/event.sql:291-299`) grows a join through
`package_tickets` → `ticket_types` with `MIN(tt.event_start)` / `MAX(tt.event_end)` and a
`GROUP BY`. Both tables belong to the event domain, so the join stays inside the boundary
(FR-021, [research.md](./research.md) D-004).

### 5. Order domain — line display and wire DTO

`order.TicketTypeDisplay` (`admin_service.go:22-30`) and `order.PackageDisplay` (`:55-63`)
each gain `EventStart` / `EventEnd`. These sit **alongside** the existing
`EventStartDate` / `EventEndDate`, which keep meaning the parent event's dates — the two
pairs are not interchangeable and must not be merged.

The copy in `orderEventLookupAdapter` (`backend/cmd/api/adapters.go:425-444`, `:446-465`)
extends field-for-field. No `EventLookup` signature change.

`order.PublicOrderItem` (`dto.go:150-157`) gains:

```go
EventStart time.Time `json:"event_start"`
EventEnd   time.Time `json:"event_end"`
```

Populated per line in `publicItems` (`public_service.go:286-312`). `PublicOrderEvent`
(`dto.go:127-134`) is **unchanged** — [research.md](./research.md) D-003.

### 6. Ticket domain — validation and the printed ticket

`ticket.ValidationResult` (`dto.go:71-89`) gains:

```go
EventStart *time.Time `json:"event_start"`
EventEnd   *time.Time `json:"event_end"`
```

Pointers, matching the existing nullable detail fields — they are nil on `INVALID`, which
FR-019 requires. Query `GetTicketDetailByCode` (`queries/ticket.sql:8-17`) adds
`tt.event_start`, `tt.event_end` to a join that already exists.

`ticket.Detail` (`dto.go:25-31`) gains the two as values.

`ListTicketDetailsByOrderID` (`queries/ticket.sql:36-48`) swaps `e.start_date` for
`tt.event_start, tt.event_end`; `e.venue` stays — the venue is still the event's.
`notification.TicketDetail` (`pdf.go:24-32`) replaces `StartDate time.Time` with
`EventStart` / `EventEnd`, consumed at `pdf.go:139` and `service.go:230` (FR-011).

## Validation result vocabulary

`backend/internal/ticket/dto.go:10-21` grows from three outcomes to five:

| Constant | Value | When | Mark-used offered |
|---|---|---|---|
| `ResultValid` | `VALID` | `ACTIVE` and now ∈ [`event_start`, `event_end`] | yes |
| `ResultAlreadyUsed` | `ALREADY_USED` | status `USED` | no |
| `ResultInvalid` | `INVALID` | no such code, or status `REVOKED` | no |
| `ResultNotYetValid` | **`NOT_YET_VALID`** | `ACTIVE` and now < `event_start` | no |
| `ResultExpired` | **`EXPIRED`** | `ACTIVE` and now > `event_end` | no |

Precedence (FR-018) falls out of inserting the window check on the `ACTIVE` branch of the
existing switch at `service.go:149-163`: `INVALID` → `ALREADY_USED` → window → `VALID`.
Both endpoints inclusive. Two codes rather than one — see
[research.md](./research.md) D-005.

## Frontend types

| Type | File | Change |
|---|---|---|
| `TicketTypeSummary` | `frontend/lib/types.ts:19-32` | `+ event_start, event_end` |
| `TicketTypeAdminView` | `types.ts:341-354` | `+ event_start, event_end` |
| `PublicOrderItem` | `types.ts:167-176` | `+ event_start, event_end` |
| `TicketOrderDetail.event` | `types.ts:242-249` | unchanged |
| `ValidationResult` | `types.ts:407-418` | `+ event_start, event_end` (nullable); result union `+ NOT_YET_VALID, EXPIRED` |
| `ticketTypeFormSchema` | `frontend/lib/schemas.ts:60-83` | `+ eventStart, eventEnd` required, `+ superRefine` ordering check |

## State transitions

None added. The only state machine here is `tickets.status`
(`ACTIVE → USED`, irreversible, `queries/ticket.sql:22-28`) and this feature does not
change it — it only gates whether the transition is *offered* and refuses it at the
service layer when out of window (FR-017, FR-020). The guarded `UPDATE` keeps its
concurrency guarantee untouched.

---

# Data Model — Revision 2 (2026-08-12)

**No storage change.** `ticket_types.event_start` / `event_end` already exist from
migration `000014`. Nothing below alters a table, a constraint, or `SCHEMA.md`. This
revision changes only how those stored values reach and render on the Order Summary panel.

## What changes, layer by layer

### 1. Event domain — package constituent dates

`ListPackageDisplaysByIDs` (`backend/internal/event/queries/event.sql`) **loses** its
`MIN`/`MAX` aggregate and its `LEFT JOIN` through `package_tickets`, returning to a plain
`packages ⋈ events` row. The span it computed is no longer displayed anywhere.

A new query supplies the list:

```sql
-- name: ListPackageAdmissionStartsByIDs :many
-- One row per (package, distinct constituent admission start), ascending.
SELECT DISTINCT pt.package_id, tt.event_start
FROM package_tickets pt
JOIN ticket_types tt ON tt.id = pt.ticket_type_id
WHERE pt.package_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY pt.package_id, tt.event_start;
```

`event.PackageDisplayRecord` drops `TicketEventStart`/`TicketEventEnd` and gains
`AdmissionStarts []time.Time`, assembled in `package_repository.go` by grouping the rows
above. `TicketTypeDisplayRecord` likewise carries `AdmissionStarts` — a one-element slice
holding its own `event_start` — so both line kinds present the same shape to the order
domain.

Both tables belong to the event domain, so the join stays inside the boundary
(Principle II).

### 2. Order domain — the wire

`order.TicketTypeDisplay` and `order.PackageDisplay` (`internal/order/admin_service.go`)
replace their `TicketEventStart`/`TicketEventEnd` pair with `AdmissionStarts []time.Time`.
Their `EventStartDate`/`EventEndDate` fields — the **event's** dates — are unchanged and
remain the source for the panel's Event box.

`order.PublicOrderItem` (`internal/order/dto.go`):

```go
// AdmissionStarts are the distinct instants this line admits on: one for a
// ticket line, one per distinct constituent for a package (spec 015 FR-021a).
// A single value cannot express a Day 1 + Day 2 bundle, which is the defect
// this field exists to fix.
AdmissionStarts []time.Time `json:"admission_starts"`
```

replacing `EventStart` / `EventEnd`. `PublicOrderEvent` stays exactly as it is —
`start_date` / `end_date` remain the event's own, and FR-009 now depends on that.

`publicItems` (`internal/order/public_service.go`) fills the slice from the display on
both the ticket and package branches. `eventOf` is untouched.

### 3. Frontend

| Type / file | Change |
|---|---|
| `PublicOrderItem` (`lib/types.ts`) | `event_start`/`event_end` → `admission_starts: string[]` |
| `lib/admission-dates.ts` | **NEW** — the 1 / 2 / range rule as a pure function |
| `lib/format.ts` | five date formatters `id-ID` → `en-GB`; currency and counts unchanged |
| `components/order/order-summary-panel.tsx` | `admissionSpanOf` **deleted**; Event box and gate line revert to `order.event.*`; the line renders the rule's output |

## The display rule

`admission-dates.ts` owns it, so it is testable without a panel:

```
formatAdmissionDates(starts: string[]): string
```

1. Format every instant with `formatDate` (which renders in the viewer's timezone).
2. Deduplicate the resulting **strings**, preserving ascending order. Deduplicating the
   rendered value rather than the instant is what makes "distinct calendar date" true in
   the timezone the buyer is actually reading — see [research.md](./research.md) D-010.
3. One or two distinct strings → join them (`"30 Sep 2026, 1 Oct 2026"`).
4. Three or more → `formatDateRange(first, last)` over the **first and last instants**,
   which are start instants, never ends (FR-021c).
5. Empty input → empty string; the caller renders nothing rather than a dash.

| Bundle | Distinct rendered dates | Line shows |
|---|---|---|
| Day 1 only | 1 | `30 Sep 2026` |
| Day 1 + Day 2 | 2 | `30 Sep 2026, 1 Oct 2026` |
| Day 1 ×2 + Day 2 | 2 (deduped) | `30 Sep 2026, 1 Oct 2026` |
| Day 1 + Day 2 + Day 3 | 3 | `30 Sep - 2 Oct 2026` |
| Day 1 + Day 2 + Day 5 | 3 | `30 Sep - 4 Oct 2026` — reads as contiguous; accepted tradeoff |
| Three tickets, same day | 1 | `30 Sep 2026` |

## What the Event box shows

Unchanged from before this whole feature: `order.event.start_date` /
`order.event.end_date`, with "Gate opens at" derived from `order.event.start_date`. FR-009
was reversed on 2026-08-12 — the box is labelled "Event" and names the event, not the
tickets on the order.

## State transitions

None. Validation, quota, and the irreversible `USED` transition are untouched by this
revision.

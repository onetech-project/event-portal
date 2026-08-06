# Research: Single Visitor Form per Bundle

**Feature**: [spec.md](spec.md) | **Date**: 2026-08-05

No external unknowns — every question is an internal design decision over code that was read directly ([visitor-form.tsx](../../frontend/components/order/visitor-form.tsx), [service.go](../../backend/internal/order/service.go), [demand.go](../../backend/internal/order/demand.go), [repository.go](../../backend/internal/order/repository.go), [dto.go](../../backend/internal/order/dto.go), [queries/order.sql](../../backend/internal/order/queries/order.sql), migrations 000001/000003). All decisions below are final; no NEEDS CLARIFICATION remains.

## R1 — How slots are grouped into bundle units

**Decision**: Add a nullable ordinal column `attendees.package_unit SMALLINT` (migration `000011`). Booking stamps it: for a package line of quantity Q, slots are created unit-by-unit (`package_unit` = 1…Q), each unit containing exactly the package's per-unit composition; standalone slots keep `NULL`. The grouping key for one form is `(package_id, package_unit)`.

**Rationale**: The unit identity must be decided somewhere, and booking is the only place that still knows it — `expandPackage` currently aggregates demand as `qty × component.quantity` ([demand.go:94-97](../../backend/internal/order/demand.go#L94-L97)), destroying unit boundaries before slots are created. A persisted ordinal makes grouping deterministic for the frontend, enforceable by the backend (R3), and self-documenting in the data. FR-006 (different visitor per unit) is unimplementable without some unit identity.

**Alternatives considered**:
- *Client-side partitioning without any schema change* (chunk same-package slots into units by dividing slot counts by order-item quantity): zero backend change, but the derivation is fragile (needs order-item ↔ slot reconciliation in UI code), a naive chunking can assemble a "unit" of 2× Day-1 tickets instead of Day-1+Day-2, and the server can never enforce FR-003. Rejected.
- *`order_item_id` FK on attendees plus row numbering*: heavier migration, still needs an intra-item ordinal, adds a relationship no requirement needs. Rejected.

## R2 — Where the data duplication happens

**Decision**: Client-side fan-out. The form state holds one entry per slot **group**; on submit the frontend expands each group into one `attendees[]` element per slot id (identical values, distinct `id`s). The `POST /ticket/checkout/:order_id` wire shape is unchanged, and `matchVisitorsToSlots` ([service.go:586-607](../../backend/internal/order/service.go#L586-L607)) still sees exact 1:1 slot coverage.

**Rationale**: Keeps the spec 008 checkout contract ([008 contracts/api.md](../008-e2e-purchase-flow/contracts/api.md) call 8) fully backward-compatible, leaves TX-D's per-slot `UpdateAttendeeDetails` loop untouched, and makes the feature deployable frontend-last with no API version dance. The user's requirement "the data on the backend will be the same per ticket" is a statement about stored state, which fan-out satisfies; R3 makes it a guarantee rather than a client courtesy.

**Alternatives considered**: Server-side fan-out with a new grouped payload (`bundles: [{package_id, unit, visitor}]`): breaks the wire contract, requires new error-field paths, reshapes `matchVisitorsToSlots` and the TX-D loop — more churn for the same stored result. Rejected.

## R3 — Server-side enforcement of "same data per ticket" (FR-003)

**Decision**: `CheckoutOrder` validates, after `matchVisitorsToSlots` and before TX-D: group the order's slots by `(package_id, package_unit)` (both non-NULL); within each group, every submitted visitor mapped to those slots must have identical `name`, `email`, `phone`, `dob`, `gender`. A divergent field is rejected as the existing 400001 validation shape, keyed `attendees[<payload index>].<field>` with message "All tickets in the same bundle must use the same visitor information."

**Rationale**: FR-003 is a data invariant; leaving it purely to the client means any direct API call can silently break the "one visitor per bundle unit" semantics that gate staff and e-tickets will assume. The check is a few maps over data already in memory.

**Alternatives considered**: Trust the client (no enforcement) — rejected as it makes FR-003 unverifiable at the boundary that owns the data. Normalizing server-side (first entry wins, overwrite the rest) — rejected: silently discarding submitted data hides client bugs.

## R4 — Frontend form state, error remapping, legacy-order fallback

**Decision**:
- A pure helper `groupOrderSlots(slots)` (new `frontend/components/order/slot-groups.ts`) returns ordered groups: `{ key, title, slotIds, isBundle, unitLabel }`. Standalone slot → own group titled `ticket_type_name`. Slots with non-null `package_id` **and** non-null `package_unit` → grouped per `(package_id, package_unit)`, titled with `package_name` (the bundle title — per stakeholder direction it replaces the ticket-type title) plus a ticket-count badge; when the same package has more than one unit, groups get an ordinal label ("Visitor 1", "Visitor 2").
- The RHF schema's `attendees` array becomes one entry per group, carrying `slot_ids: string[]` instead of a single `id`; submit fans out per R2 while recording a `payloadIndex → {groupIndex}` map, and the server-error path rewrite (`attendees[3].dob` → `attendees.<groupIndex>.dob`) goes through that map instead of the current 1:1 index rewrite ([visitor-form.tsx:111-115](../../frontend/components/order/visitor-form.tsx#L111-L115)).
- **Fallback**: a bundle slot with `package_unit === null` (order booked before migration 000011) is treated as its own group — exactly today's behavior, so in-flight orders keep working through a deploy.

**Rationale**: A pure grouping function is unit-testable without rendering; the fallback removes any need for data backfill; the index map keeps server field errors landing on the right visible form.

**Alternatives considered**: Backfilling `package_unit` for historical orders — rejected: reconstruction after aggregation is guesswork, and the fallback makes it unnecessary.

## R5 — Bundle form title (stakeholder direction, FR-005)

**Decision**: The bundle group's card title is the **bundle's name** (`package_name`), not any constituent `ticket_type_name`; a badge shows "N tickets" (the group's slot count). The current per-slot header (ticket-type title + package badge, [visitor-form.tsx:166-173](../../frontend/components/order/visitor-form.tsx#L166-L173)) inverts for bundle groups.

**Rationale**: Directed by the user mid-spec ("on the bundle form show the bundle title instead of ticket title"); the count badge carries the FR-005 requirement that one form visibly registers multiple passes.

## R6 — DTO and query changes

**Decision**: `TicketOrderSlot` gains `package_id: *uuid` and `package_unit: *int` (additive, nullable — old clients unaffected). `ListAttendeeSlotsByOrderID` selects the two extra columns and orders by `a.package_id NULLS FIRST, a.package_unit, a.id` so groups arrive contiguous and standalone slots first; `CreateAttendeeSlot` accepts the new column via `AttendeeRef.PackageUnit`. Frontend `TicketOrderSlot` type mirrors the two fields.

**Rationale**: Grouping by `package_name` alone would conflate two different bundles with the same display name; the id + unit pair is the real key. A stable, group-contiguous sort keeps form order deterministic across reloads (today's `ORDER BY a.id` is random-UUID order anyway, so no meaningful order is lost).

## R7 — Legacy artifact cleanup

**Decision**: Delete `frontend/lib/schemas.ts`'s `checkoutSchema`/`slotCounts` block and its tests in `frontend/lib/schemas.test.ts` (the spec-001 client-side N-forms validation — referenced by no component, and its bundle tests assert the exact behavior this feature reverses). Backend legacy `CheckoutRequest`/`validateAttendees` (unrouted spec-001 path) is left alone — removing it is unrelated refactoring.

**Rationale**: Keeping tests that encode the superseded rule invites false confidence and future confusion; the backend legacy path is dormant and its removal belongs to its own cleanup change.

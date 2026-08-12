# Phase 1 Contracts: Per-Ticket Event Dates

**Feature**: [spec.md](../spec.md) | **Data model**: [data-model.md](../data-model.md)

Every response stays inside the standard `{code, message, data}` envelope. Only the
deltas are shown; unlisted fields are unchanged. No route is added or removed.

---

## 1. `GET /api/v1/ticket/:event_slug` — sellable ticket types

Two fields added to each element.

```diff
  {
    "id": "…", "name": "Day 2 Pass", "description": null,
    "price": "150000.00", "quota_remaining": 42,
    "sales_start": "2026-07-01T00:00:00Z",
-   "sales_end":   "2026-08-02T23:59:59Z"
+   "sales_end":   "2026-08-02T23:59:59Z",
+   "event_start": "2026-08-02T09:00:00Z",
+   "event_end":   "2026-08-02T23:00:00Z"
  }
```

**Cached surface.** This response body is the cached value
(`list:ticket_types_public:{event_uuid}:-:g{gen}`). Entries written before deploy decode
into the new shape with `0001-01-01T00:00:00Z` for both fields — see the mandatory flush
step in [quickstart.md](../quickstart.md).

**Consumer note.** FR-013 keeps the ticket-selection page dateless, so these fields are
currently unread by the guest UI. They are on the contract because the same DTO backs
future reads and because omitting them would make the public and admin projections
disagree.

---

## 2. `GET /api/v1/ticket/order/:order_id` — order detail

Added **per line**, not on the event object.

```diff
  "event": {
    "name": "…", "slug": "…", "venue": "…", "address": "…",
    "start_date": "2026-08-01T09:00:00Z",
    "end_date":   "2026-08-03T23:00:00Z"
  },
  "items": [
    {
      "kind": "ticket",
      "ticket_type_name": "Day 2 Pass",
      "package_name": null,
      "quantity": 2,
      "unit_price": "150000.00",
-     "subtotal": "300000.00"
+     "subtotal": "300000.00",
+     "admission_starts": ["2026-08-02T09:00:00Z"]
    },
    {
      "kind": "package",
      "ticket_type_name": null,
      "package_name": "Day 1 & 2 Bundle",
      "quantity": 1,
      "unit_price": "250000.00",
      "subtotal": "250000.00",
+     "admission_starts": ["2026-08-01T09:00:00Z", "2026-08-02T09:00:00Z"]
    }
  ]
```

`admission_starts` **replaces** the `event_start` / `event_end` pair proposed in revision
1 and shipped briefly. A single pair cannot express a Day 1 + Day 2 bundle — the panel
printed only the earliest date and the second day vanished, which is the defect this
change exists to fix. `event_end` is dropped rather than retained because nothing reads
it once the Event box reverts to the event's own dates.

Ordering is ascending. Entries are distinct **instants**; the client deduplicates by
rendered date, because a calendar date depends on the timezone it is rendered in
([research.md](../research.md) D-010).

`event.start_date` / `event.end_date` keep meaning **the event's** dates and are
deliberately left alone (FR-012, [research.md](../research.md) D-003).

For a `"kind": "package"` line the payload must carry **every constituent's admission
start**, not a single collapsed pair, so the panel can name each distinct day
(FR-021a–c). A single `event_start` cannot express a Day 1 + Day 2 bundle, which is the
defect this contract change exists to fix.

The Order Summary panel's Event box and its "Gate opens at" line are **unchanged**: both
read `event.start_date` / `event.end_date`, because the box names the event, not the
tickets on the order (FR-009).

---

## 3. `POST /admin/ticket-types` and `PUT /admin/ticket-types/:id`

Request gains two **required** fields:

```diff
  {
    "event_id": "…", "name": "Day 2 Pass", "description": null,
    "price": "150000.00", "quota": 100,
    "sales_start": "2026-07-01T00:00:00Z",
-   "sales_end":   "2026-08-02T23:59:59Z"
+   "sales_end":   "2026-08-02T23:59:59Z",
+   "event_start": "2026-08-02T09:00:00Z",
+   "event_end":   "2026-08-02T23:00:00Z"
  }
```

Both verbs take the same shape. `event_id` remains immutable on update and ignored there.

### New refusals

| Condition | Status | Code | Message |
|---|---|---|---|
| either field absent/zero | 400 | `VALIDATION_ERROR` | `event_start and event_end are required.` |
| `event_end` before `event_start` | 400 | `INVALID_DATE_RANGE` | `event_end must not be before event_start.` |
| window outside the parent event's dates | 400 | `INVALID_DATE_RANGE` | `event_start and event_end must fall within the event's own dates (<start> – <end>).` |

Codes are reused, not invented — `apperr.go:11-12` treats them as a public contract.
The containment refusal attaches the event's bounds via `(*Error).WithData`
(`apperr.go:205-209`) so the form can point at the offending field.

Containment is checked in the service, not the DTO validator, because it needs the parent
event row ([research.md](../research.md) D-006).

### Response

`TicketTypeAdminView` gains `event_start` / `event_end`.

---

## 4. `PUT /admin/events/:id` — unchanged

**Deliberately no change.** FR-005a keeps this endpoint unguarded against stranded ticket
windows; enforcing containment on both sides deadlocks a reschedule. The FR-005b warning
is derived in the admin frontend from `EventAdminDetail`, which already carries both the
event's dates and its ticket types' windows — no new field, no new endpoint,
no round trip.

---

## 5. `POST /admin/tickets/validate` — the gate

Still always HTTP 200; the client branches on `result`.

```diff
  {
-   "result": "VALID",
+   "result": "VALID" | "ALREADY_USED" | "INVALID" | "NOT_YET_VALID" | "EXPIRED",
    "ticket_code": "TIX-…",
    "attendee_name": "…",
    "ticket_type_name": "Day 2 Pass",
-   "event_name": "…"
+   "event_name": "…",
+   "event_start": "2026-08-02T09:00:00Z",
+   "event_end":   "2026-08-02T23:00:00Z"
  }
```

### Outcome table

| `result` | Condition | Detail fields | Mark-used offered |
|---|---|---|---|
| `INVALID` | no such code, or `REVOKED` | all `null`, **including the window** | no |
| `ALREADY_USED` | status `USED` | populated | no |
| `NOT_YET_VALID` | `ACTIVE`, now < `event_start` | populated | no |
| `EXPIRED` | `ACTIVE`, now > `event_end` | populated | no |
| `VALID` | `ACTIVE`, `event_start` ≤ now ≤ `event_end` | populated | yes |

Precedence is exactly that order (FR-018): an already-used ticket presented out of window
reports `ALREADY_USED`. Both endpoints are inclusive and there is no grace period
(FR-014). `INVALID` continues to disclose nothing (FR-019).

The not-yet/passed distinction is decided **server-side**. A gate device's clock is the
least trustworthy one in the system, so the client must never derive the wording by
comparing the window to its own `Date.now()`.

---

## 6. `POST /admin/tickets/:code/use` — the admission

Route and success shape unchanged. One new refusal:

| Condition | Status | Code |
|---|---|---|
| ticket `ACTIVE` but outside its window | 409 | `INVALID_DATE_RANGE` |

This is the direct-invocation guard of FR-017 — the UI already hides the button, but the
endpoint must not rely on that. The check sits in `Service.MarkUsed`
(`service.go:179-201`) **before** the guarded `UPDATE`, leaving that statement's
concurrency guarantee untouched (FR-020).

---

## Revision 2 additions (2026-08-12)

Three corrections to the contract above, all on the order-detail read. Every other
endpoint in this document is unchanged and already shipped.

| # | Change | Requirement |
|---|---|---|
| 1 | `admission_starts: string[]` replaces `event_start`/`event_end` on each item | FR-021a–c |
| 2 | The panel's Event box and "Gate opens at" read `event.start_date`/`event.end_date` — **not** the order's tickets. Revision 1 had this backwards. | FR-009 |
| 3 | All dates render in English, day-first. Currency and counts stay Indonesian. | FR-012a/b |

Nothing changes on `GET /api/v1/ticket/:event_slug`, the admin ticket-type endpoints, the
validate endpoint, or mark-used. The email receipt and e-ticket are explicitly out of
scope.

### Revision 2 contract test checklist

Walked 2026-08-12. Each item names the test that holds it.

- [x] A package line's `admission_starts` lists every distinct constituent start,
      ascending — `TestPackageLineListsEveryConstituentAdmissionDay`
- [x] A ticket line's `admission_starts` holds exactly one entry —
      `TestOrderLinesCarryTheirOwnAdmissionWindows`
- [x] A bundle whose constituents share a day lists it once —
      `TestPackageLineDeduplicatesConstituentsSharingADay` (server side) and
      `admission-dates.test.ts` "counts distinct days, not tickets" (render side)
- [x] `event_start` / `event_end` no longer appear on any item — enforced by the
      compiler: the fields are gone from `PublicOrderItem` in Go and TypeScript
- [x] `event.start_date` / `event.end_date` unchanged and still the event's own —
      `TestOrderEventBlockStillCarriesTheEventsOwnDates`
- [x] The panel's Event box renders the event's dates, not any line's —
      `order-summary-panel.test.tsx` "shows the event's own range in the Event box" and
      "opens the gate at the event's start", plus the e2e "Event box names the event"
- [x] Two distinct dates render exactly; three or more render as a range —
      `admission-dates.test.ts` + the two panel bundle cases
- [x] The range's closing value is a start date, never an event end — `admission_starts`
      carries no ends, so this is structural rather than asserted
- [x] Dates render with English month names — `format.test.ts` `/Oct/`, and
      `formatCurrency stays Indonesian` guards the other direction

## Contract test checklist (revision 1)

Walked 2026-08-12. Each item names the test that holds it.

- [x] Public ticket list carries both fields and they differ across ticket types of one
      event — `e2e/specs/guest-purchase.spec.ts` "each order line shows its own ticket's
      date"; `backend/internal/event/handler_test.go` contract-shape assertion
- [x] Order detail carries the pair per line; two lines with different windows differ —
      `backend/internal/order/public_service_test.go` `TestOrderLinesCarryTheirOwnAdmissionWindows`
- [x] A package line's pair spans its constituents — `TestPackageLineSpansItsConstituentWindows`
- [x] Order detail's `event.start_date` still returns the **event's** date —
      `TestOrderEventBlockStillCarriesTheEventsOwnDates`
- [x] Ticket-type create/update refuses each of the three new conditions with the stated
      code — `admin_dto_test.go` (missing, inverted) and `admin_service_test.go`
      (`TestCreateTicketTypeRejectsAWindowOutsideItsEvent`, `TestUpdateTicketTypeRejectsAWindowOutsideItsEvent`)
- [x] Event update still succeeds when it strands a ticket window —
      `TestUpdateEventSucceedsEvenWhenItStrandsATicketWindow`
- [x] Validation returns each of the five outcomes — `internal/ticket/service_test.go`
      (valid / not-yet / expired / already-used / invalid) plus the three e2e gate scenarios
- [x] `INVALID` returns `null` for `event_start` / `event_end` —
      `TestValidateDisclosesNoWindowForAnUnknownCode`
- [x] `ALREADY_USED` wins over out-of-window — `TestValidateReportsAlreadyUsedAheadOfTheWindow`
- [x] Validation at exactly `event_start` and exactly `event_end` returns `VALID` —
      `TestValidateTreatsBothWindowEndpointsAsInclusive`, with
      `TestValidateAllowsNoGracePeriodBeforeTheWindow` pinning the other side
- [x] Mark-used refuses an out-of-window ticket with 409 —
      `TestMarkUsedRefusesATicketOutsideItsWindow` and the e2e direct-invocation scenario

# Feature Specification: Per-Ticket Event Dates

**Feature Branch**: `fix/ticket`

**Created**: 2026-08-11

**Status**: Draft

**Input**: User description: "currently the ticket_types only store sales_end and sales_start, now i want to have event_start and event_end, this what will be showed on the booking information that currently uses event_start (not the countdown) from the event not from the each ticket" — followed by: "this will be used for validation as well that this ticket will apply on that day"

## Overview

A ticket type today knows only when it can be **sold**. It does not know when it can be
**used**.

Its sales start and sales end bound the selling window: they decide whether a row on the
ticket-selection page is choosable, and when they exclude the moment the guest is
looking, the row says "Sales have not opened yet" or "Sales have closed". No date is ever
printed from them. Meanwhile the date under each ticket line on the Order Summary panel,
and the date printed on the issued ticket, are resolved from the parent **event**. Every
ticket type under one event therefore advertises the same date, because there is only one
date to advertise. (The panel's Event box and its "Gate opens at" line also read the
event — correctly so, and they stay that way: see FR-009.)

That is wrong for any event that is not a single indivisible session. A three-day
festival with a Day 1 pass, a Day 2 pass, and a weekend pass sells three products used on
three different schedules, and all three currently tell the buyer the festival's opening
date. The buyer holding a Day 2 pass is shown Day 1, and the ticket they carry to the
gate is printed with Day 1.

**Two consequences follow, and this feature addresses both.**

*Display.* Where a surface names the date of a **ticket**, it must name that ticket's
date. Where a surface names the date of the **event** — the events list, the event
landing page's "Dates" cell, the countdown in the event frame — it must keep naming the
event's. This feature draws that line; it does not move the event's own dates.

*Validation.* The same pair of dates gives the admin validator something it has never
had: a statement of which day a ticket applies to. Validation today resolves a code to
exactly `VALID`, `ALREADY_USED`, or `INVALID`, and consults no date whatsoever — a Day 1
pass presented at the Day 2 gate is waved through because the code exists and has not
been used yet. The ticket-type window makes that refusable.

The sales window is untouched, and so is the countdown. Nothing about when a ticket can
be *bought* changes. This feature only adds — and then uses — the statement of when a
ticket can be *attended*.

## Clarifications

### Session 2026-08-11

- Q: What is the ticket-level date pair for? → A: Both the booking-information display
  (replacing the event's date) **and** admin validation — the dates state which day the
  ticket applies to, and validation enforces it.
- Q: What does "applies on that day" mean at the gate — exact instants, whole calendar
  days, or instants plus a grace period? → A: **Strictly the stated instants.** No
  tolerance either side. The event start is therefore the moment admission opens for that
  ticket type, not showtime, and admins set it accordingly.
- Q: Should the ticket-selection page, which prints no date today, start showing each
  ticket type's event window? → A: **No — keep the current design.** The feature is
  confined to surfaces that already print a date.
- Q: Must a ticket type's event window be contained within its parent event's dates, and
  should an event date edit be refused when it would strand one? → A: **Contained at
  ticket-type save; the event edit stays unguarded and warns instead.** Enforcing both
  sides deadlocks a reschedule — neither the event nor its tickets can move first.
  Widening an event never strands anything, so only a shift or a shortening can warn.

### Session 2026-08-12

- Q: When a package's tickets share dates, does the "1 or 2 dates" rule count distinct
  calendar dates or ticket count? → A: **Distinct calendar dates**, deduplicated. Three
  tickets falling on two dates show both dates exactly; three tickets all on one day show
  one date.
- Q: Should every date across the app switch to English, or only the Order Summary's? →
  A: **Every date/time formatter, app-wide, in `en-GB`** (day-first, "1 Oct 2026").
  Currency and visitor counts stay Indonesian — `Rp 170.400` with dot separators is
  correct for IDR.
- Q: For three or more dates, does the range end at the last ticket's start or its end? →
  A: **Its start.** The range names the days admission begins, so a window crossing
  midnight does not advertise a following day on which nothing admits.
- Q: Does the multi-date rule extend to the email receipt and the e-ticket? → A: **Not
  now.** Scope is the Order Summary panel alone. The receipt's order lines stay dateless
  and each e-ticket keeps printing its own single start–end window, both unchanged.
- Q: Should the panel's top Event box follow the per-line rule? → A: **No — it shows the
  EVENT's own `start_date`/`end_date`, not any ticket's window.** The box is labelled
  "Event" and describes the event; only the lines beneath it describe tickets. This
  reverses the earlier reading of FR-009.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The buyer sees the date of the ticket they picked (Priority: P1)

A guest buying from an event with several ticket types sees, on the booking information
they read while filling in holder details and while paying, the date and time that
ticket admits them to — not the parent event's opening date. When they buy a Day 2 pass,
the ticket document that arrives after payment is printed with Day 2.

**Why this priority**: This is the defect the buyer actually experiences. A ticket
advertised and printed with the wrong date is a support incident at best and a missed
event at worst.

**Independent Test**: Create an event spanning several days with two ticket types whose
event windows differ. Buy one, and confirm the Order Summary panel's ticket line and the
issued ticket show that ticket type's window rather than the event's opening date, while
the panel's Event box still shows the event's own dates.

**Acceptance Scenarios**:

1. **Given** an order whose ticket type carries an event window differing from its parent
   event's dates, **When** the guest reaches the holder-details step, **Then** the Order
   Summary panel's **Event box** still shows the event's own dates, while the **ticket
   line** beneath it shows that ticket type's window.
2. **Given** the same order, **When** the guest reaches the payment step, **Then** the
   same panel shows the same split — event dates in the box, ticket dates on the lines.
3. **Given** an order containing two ticket types with different event windows, **When**
   the guest views the Order Summary panel, **Then** the date shown under each ticket
   line is that line's own ticket-type window, and the two lines differ.
4. **Given** an order that has settled, **When** the buyer opens the ticket document and
   the ticket email, **Then** the date and time printed for each ticket is the window of
   the ticket type that ticket was issued from.
5. **Given** any event, **When** a guest views the events list, the event landing page's
   "Dates" cell, or the countdown in the event frame, **Then** those continue to show the
   event's own dates and are unchanged by this feature.

---

### User Story 2 - The admin states when each ticket applies (Priority: P1)

An admin creating or editing a ticket type supplies its event start and event end
alongside the price, remaining quota, and sales window. The two windows are separately
labelled so that "when this is on sale" cannot be mistaken for "when this is used". A
ticket type cannot be saved without both, nor with an end at or before its start.

**Why this priority**: Nothing in User Story 1 or 3 can be demonstrated until an admin
can set these dates. It is the same slice of work, at the source of the data.

**Independent Test**: Open the admin ticket-type form, confirm the event start/end fields
are present and labelled distinctly from the sales window, save a valid pair, reopen the
record and confirm it persisted. Attempt a save with the end before the start and confirm
it is refused.

**Acceptance Scenarios**:

1. **Given** an admin is creating a ticket type, **When** they submit without an event
   start or event end, **Then** the save is refused with a message naming the missing
   field.
2. **Given** an admin is editing a ticket type, **When** they submit an event end earlier
   than the event start, **Then** the save is refused with a message explaining the
   ordering requirement.
3. **Given** a ticket type that existed before this feature, **When** an admin opens it,
   **Then** it shows an event window rather than empty fields, and those values are the
   parent event's start and end dates.
4. **Given** an admin submits a ticket-type event window outside its parent event's
   dates, **When** they save, **Then** the save is refused and the message names the
   event's own window.
5. **Given** an event running 1–2 August with ticket types on 1 and 2 August, **When** an
   admin extends the event to 1–3 August, **Then** the save succeeds with no warning and
   no ticket-type window changes.
6. **Given** the same event, **When** an admin moves it to 5–6 August, **Then** the save
   succeeds, a non-blocking warning names both stranded ticket types, their windows are
   unchanged, and each can then be moved into 5–6 August successfully.
7. **Given** an admin views the ticket types listed under an event, **When** the list
   renders, **Then** each row shows its event window distinctly from its "On sale" line.
8. **Given** an admin changes a ticket type's event window, **When** a guest next loads a
   surface showing it, **Then** the new dates appear — no stale cached read serves the
   old ones.

---

### User Story 3 - A ticket presented on the wrong day is refused (Priority: P2)

An admin at the gate enters a ticket code or scans its QR. The validator resolves the
ticket as it does today, and additionally checks it against the event window of the
ticket type it was issued from. A ticket whose day has not arrived, or has passed, is
reported distinctly from a code that does not exist and from a code already used, and
"Mark used" is not offered for it.

**Why this priority**: It depends on User Story 2 for its data and delivers value only
once real ticket types carry real windows. It also has the highest operational blast
radius — a wrong refusal turns away a paying attendee at the door — so it ships behind
the display fix rather than alongside it.

**Independent Test**: Issue a ticket from a ticket type whose event window is in the
past, present its code to the validator, and confirm the outcome is the out-of-window one
and that "Mark used" is absent. Repeat with a ticket whose window covers now and confirm
it validates and can be marked used exactly as today.

**Acceptance Scenarios**:

1. **Given** an active ticket whose ticket-type event window covers the moment of
   validation, **When** an admin validates it, **Then** the outcome is `VALID` and it can
   be marked used, exactly as before this feature.
2. **Given** an active ticket whose ticket-type event window has not begun, **When** an
   admin validates it, **Then** the outcome states the ticket does not apply yet, names
   the window it does apply to, and "Mark used" is not rendered.
3. **Given** an active ticket whose ticket-type event window has passed, **When** an admin
   validates it, **Then** the outcome states the ticket's window has passed, names that
   window, and "Mark used" is not rendered.
4. **Given** a ticket already marked used, **When** an admin validates it outside its
   window, **Then** `ALREADY_USED` still takes precedence — the admin is told it was used,
   not that it is out of window.
5. **Given** a code matching no ticket, **When** an admin validates it, **Then** the
   outcome is `INVALID`, no window is disclosed, and no window check is attempted.
6. **Given** an out-of-window ticket, **When** an admin attempts the mark-used operation
   directly rather than through the button, **Then** it is refused and the ticket remains
   active.
7. **Given** an active ticket validated at the exact instant of its event start, and
   another at the exact instant of its event end, **When** each is validated, **Then**
   both are `VALID` — the window is inclusive of both endpoints.
8. **Given** an active ticket validated one moment before its event start, **When** it is
   validated, **Then** it is refused as out of window — there is no grace period.

---

### Edge Cases

- **A ticket type's event window falls outside its parent event's dates.** Refused at
  ticket-type save (FR-005), with the event's own window named so the admin can see what
  they are bounded by.
- **The event's dates are widened.** An event running 1–2 August with ticket types on
  1 August and 2 August is extended to 1–3 August. Every existing window is still inside
  the new range, so nothing is refused, nothing warns, and nothing changes. Widening can
  never break containment.
- **The event is rescheduled or shortened.** An event running 1–2 August moves to
  5–6 August. The event edit succeeds (FR-005a) and warns that both ticket types now sit
  outside it (FR-005b); the admin then moves each ticket window into the new range, which
  now passes FR-005. The reverse order is impossible, which is why the event edit is
  deliberately unguarded.
- **The sales window outlives the event window.** A ticket type can remain on sale after
  the day it admits to has passed. The system does not refuse this — sales and attendance
  windows are independent by design — and the ticket simply validates as out of window
  when presented.
- **A ticket is presented shortly before the stated start.** It is refused — the window
  has no tolerance (FR-014). This makes the event start the moment admission opens rather
  than showtime, and an admin who sets it to showtime will turn away every early arrival.
  The admin-facing labelling in FR-006 must therefore say so.
- **An order mixes ticket types with different windows.** Each line keeps its own date.
  The panel's Event box is unaffected: it names the event, which is one thing however
  many windows the order spans.
- **A package bundles tickets across non-contiguous days.** A bundle admitting on Day 1,
  Day 2 and Day 5 has three distinct dates, so it renders as a range from the first to
  the last — which reads as though every day between is included. Accepted deliberately:
  past two dates a list stops fitting the line, and the ranged form is what was asked
  for. The exact days remain visible on each issued ticket.
- **An existing order references a ticket type whose window was edited after purchase.**
  Order pages and ticket documents resolve the window live through the ticket type, so
  the edit is reflected. Tickets already marked used are unaffected.
- **A package bundles ticket types whose windows differ.** Its line names every distinct
  admission date its parts carry — exactly, up to two; as a range beyond that (FR-021a
  to FR-021c) — because a bundle admits on every day its parts admit. Each ticket issued
  from a bundle still validates against its own ticket type's window, never any span
  derived for display.
- **Validation crosses a day boundary, or the admin's device is in another timezone.**
  The window is an absolute instant range evaluated server-side, so the outcome does not
  depend on the validating device.
- **A ticket type is deleted or its event unpublished between issue and validation.** The
  existing outcomes govern; this feature adds no new failure there.

## Requirements *(mandatory)*

### Functional Requirements

**The ticket type's event window**

- **FR-001**: A ticket type MUST carry an event start and an event end, both mandatory,
  in addition to its existing sales start and sales end.
- **FR-002**: A ticket type's event end MUST NOT be earlier than its event start; a save
  violating this MUST be refused with a message naming the ordering requirement. Equal
  endpoints are permitted, matching the rule an event's own dates already follow — a
  stricter rule here would make FR-004's backfill fail on any zero-length event.
- **FR-003**: The event window MUST be independent of the sales window. No rule MUST
  constrain either against the other, in either direction.
- **FR-004**: Ticket types that existed before this feature MUST carry an event window
  equal to their parent event's start and end dates, so that no ticket type is left
  without one and no pre-existing display changes.
- **FR-005**: A ticket type's event window MUST be contained within its parent event's
  start and end dates. A ticket-type save violating this MUST be refused with a message
  naming the event's own window.
- **FR-005a**: Editing an event's start or end date MUST NOT be refused on account of
  existing ticket-type windows, even when the new range would leave some of them outside
  it. Enforcing containment on both sides deadlocks a genuine reschedule: the event
  cannot move until its tickets do, and the tickets cannot move until the event does.
  Leaving the event edit unguarded forces the workable order — move the event, then move
  its ticket types into the new range.
- **FR-005b**: When an event's dates are edited such that one or more of its ticket types'
  event windows fall outside the new range, the admin MUST be shown a non-blocking
  warning naming those ticket types, both on save and on the event's admin detail surface,
  until every window is back inside the range.
- **FR-005c**: An event date edit MUST NOT alter any ticket type's event window. Windows
  are stored values, never derived, and never cascade.

**Admin authoring**

- **FR-006**: The admin ticket-type create and edit surfaces MUST accept an event start
  and event end, labelled distinctly from the sales window so the two cannot be confused.
  Because validation admits no tolerance (FR-014), the labelling MUST make clear that the
  event start is the moment admission opens for that ticket, not showtime.
- **FR-007**: The admin ticket-type list and detail surfaces MUST show a ticket type's
  event window alongside its existing "On sale" window.
- **FR-008**: A change to a ticket type's event window MUST be visible on the next guest
  read of any surface showing it, with no stale cached value served.

**Guest-facing display**

- **FR-009**: The Order Summary panel's **Event box** — its date range and its "Gate
  opens at" line — MUST resolve from the parent event's own `start_date` and `end_date`.
  The box is labelled "Event" and describes the event; deriving it from the tickets on
  the order would make a box named for one thing carry another. Only the ticket lines
  beneath it describe tickets.
- **FR-010**: The date shown under each ticket line in the Order Summary panel MUST be
  that line's own ticket-type event window, not the event's start date.
- **FR-011**: The issued ticket document and the ticket email MUST print, for each
  ticket, the event window of the ticket type that ticket was issued from.
- **FR-012**: Surfaces that describe the **event** rather than a ticket — the events
  list, the event landing page's "Dates" cell, the countdown in the event frame, and the
  Order Summary panel's Event box (FR-009) — MUST continue to resolve from the event's
  own start and end dates. Which date they name
  does not change; only its rendered language does, per FR-012a.
- **FR-012a**: Every date and time rendered anywhere in the guest and admin interfaces
  MUST be formatted in English, day-first (for example "1 Oct 2026"), replacing the
  Indonesian month names currently shown. Day-first is retained because it is the order
  the interface already uses, so no layout shifts.
- **FR-012b**: Currency and visitor counts MUST remain in Indonesian formatting.
  `Rp 170.400` uses dot thousand separators, which is correct for IDR; rendering it as
  `Rp 170,400` would misstate the amount to an Indonesian buyer.
- **FR-013**: The guest ticket-selection page MUST NOT change. Neither its ticket rows nor
  its Booking Detail summary print a date today, and neither gains one. This feature is
  confined to surfaces that already print a date.

**Validation**

- **FR-014**: Admin ticket validation MUST check the presented ticket against the event
  window of the ticket type it was issued from, using the stated instants exactly. The
  ticket applies from its event start up to and including its event end, with no grace
  period before or after and no widening to the surrounding calendar day.
- **FR-015**: When the moment of validation falls outside that window, validation MUST
  report an outcome distinct from `VALID`, `ALREADY_USED`, and `INVALID`.
- **FR-016**: The out-of-window outcome MUST name the window the ticket does apply to, so
  the admin can direct the attendee rather than merely turn them away.
- **FR-017**: A ticket resolving to the out-of-window outcome MUST NOT offer the mark-used
  action, and the mark-used operation MUST refuse it if invoked directly.
- **FR-018**: Outcome precedence MUST be `INVALID` (no such ticket, or revoked) first,
  then `ALREADY_USED`, then out-of-window, then `VALID`.
- **FR-019**: `INVALID` MUST continue to disclose nothing about the ticket — no window,
  no attendee, no ticket type, no event.
- **FR-020**: The irreversible active-to-used transition MUST be otherwise unchanged,
  including its concurrency guarantee that two simultaneous admits cannot both succeed.

**Packages**

- **FR-021**: Packages MUST NOT store their own event window. What a package displays
  MUST be derived from the ticket types it bundles, in the same spirit as its
  availability, which is already derived rather than stored.
- **FR-021a**: A package line on the Order Summary panel MUST name the admission dates of
  every ticket type it bundles, not a single collapsed date. Collapsing a Day 1 + Day 2
  bundle to its earliest date tells the buyer they are attending on one day when they
  hold admission for two.
- **FR-021b**: Those dates MUST be counted as **distinct calendar dates**, deduplicated:
  a bundle of three tickets falling on two dates has two dates, and a bundle of three
  tickets all admitting on the same day has one. Duplicates carry no information for a
  buyer, so the rule keys off dates rather than off how many tickets produced them.
- **FR-021c**: One or two distinct dates MUST be shown exactly. Three or more MUST be
  shown as a range running from the earliest to the latest **admission start date**.
  The range's closing date is a start date, never an event end: a ticket admitting
  22:00–02:00 ends on the following calendar day, and closing the range there would
  advertise a day on which no ticket in the bundle admits anyone.
- **FR-022**: A ticket issued as part of a package MUST validate against its own ticket
  type's event window, not the package's derived span.

### Key Entities *(include if feature involves data)*

- **Ticket Type**: gains an **event start** and **event end** — the window during which a
  ticket of this type admits its holder. Distinct from, and unconstrained by, the existing
  sales start and sales end, which govern only when the type can be purchased. Remaining
  quota, price, and every other attribute are unchanged.
- **Event**: unchanged. Retains its own start and end dates, which continue to describe
  the event as a whole and remain what the events list, landing page, and countdown show.
- **Package**: unchanged in storage. Its displayed window is derived at read time from
  its constituent ticket types.
- **Ticket**: unchanged in storage. Both the date it displays and the window it validates
  against resolve through its attendee's ticket type.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For an event whose ticket types sit on different days, 100% of the
  guest-facing surfaces that name a date for a purchased **ticket** — the Order Summary
  panel's per-line date, the ticket document, and the ticket email — name that ticket's
  own window, and none name the parent event's opening date. The panel's Event box is
  excluded by design: it names the event.
- **SC-001a**: A package line names every distinct day its bundle admits on. A two-day
  bundle never renders as one date.
- **SC-002**: The events list, event landing page "Dates" cell, countdown, and the Order
  Summary panel's Event box resolve from the same event dates before and after this
  feature. Their rendered month names change from Indonesian to English (FR-012a); the
  dates they name do not.
- **SC-003**: An admin can set a ticket type's event window while creating it, without
  leaving the ticket-type form, and the value is visible to a guest on the next page load.
- **SC-004**: No ticket type exists without an event window at any point during or after
  rollout, and no pre-existing ticket type's displayed date changes as a result of this
  feature alone.
- **SC-005**: A ticket presented outside its window is refused and cannot be marked used
  in 100% of attempts, and the message names the window the ticket does apply to.
- **SC-006**: A ticket presented within its window validates and can be marked used with
  no additional steps compared to before this feature, including at both exact endpoints
  of the window.
- **SC-007**: Zero support contacts arising from a buyer being shown or handed the wrong
  attendance date for a multi-day event.
- **SC-008**: An admin can reschedule an event and all of its ticket types to new dates
  without being blocked at any step, and is warned at every point in between that ticket
  windows sit outside their event.

## Assumptions

- **The event's own dates keep their present meaning.** This feature adds a narrower
  window per ticket type; it does not reinterpret the event's, and the event remains the
  thing the countdown counts down to.
- **Existing ticket types are backfilled from their parent event.** This is the only
  value that preserves current behaviour exactly, so the feature is invisible to every
  event that does not adopt it.
- **Single-session events are unaffected in practice.** Their ticket types will normally
  carry a window equal to the event's, and nothing they display or validate changes.
- **Validation is the gate's decision only.** The event window affects what the validator
  reports and whether mark-used is offered. Nothing about booking, quota, payment, or
  ticket issuance consults it — an out-of-window ticket type can still be sold if its
  sales window is open.
- **The event start doubles as admission-open time.** This follows from validation
  admitting no tolerance. It is consistent with the Order Summary panel's existing "Gate
  opens at" line, which is derived from the same value, so the buyer is told the same
  instant the gate enforces.
- **Both window endpoints are inclusive.** A ticket presented at exactly its event end
  is admitted, not refused.
- **The ticket document prints a window, not a bare start.** Where it currently prints a
  single "DATE & TIME" from the event start, it prints the ticket type's window; a window
  whose start and end fall on the same day renders as one date with a time range.
- **Admin authoring reuses the existing date-time control** already used for the sales
  window. No new input mechanism is introduced.
- **The window is stored and compared as an absolute instant range**, consistent with
  every other timestamp in the system.

## Out of Scope

- Any change to the sales window, to row availability gating, or to when a ticket may be
  bought.
- Any change to the countdown, which continues to count down to the event's start.
- Any change to the guest ticket-selection page — its rows and Booking Detail summary
  stay dateless (FR-013).
- Any grace period, early-admission tolerance, or per-ticket-type configuration of one.
- Per-session or per-time-slot ticketing beyond one contiguous window per ticket type.
- Seat or session selection at purchase time (out of scope per constitution Principle VI).
- Storing an event window on packages, or any package schema change.
- Re-issuing or re-mailing tickets already delivered, to correct the date printed on them.
- Any change to the irreversible nature of the used transition, or to revocation.

## Dependencies

- The admin ticket-type create/edit surfaces and their validation rules, which today
  enforce only the sales-window pair.
- The admin event edit surface, which gains the stranded-ticket-type warning of FR-005b
  but none of its existing date validation changes.
- The public ticket-type read, which today exposes no event date at all and must carry
  the window to the guest surfaces.
- The Order Summary panel and the ticket document/email renderers, each of which
  currently resolves its date from the event.
- The admin validation flow, which today resolves a code to `VALID` / `ALREADY_USED` /
  `INVALID` and gains a fourth outcome, plus the result card that renders it and gates
  "Mark used".
- The read cache over the ticket-type list, which must reflect an edited window on the
  next read (constitution Principle VII).
- `SCHEMA.md` MUST change in the same commit as the migration adding the columns
  (constitution Governance / AGENTS.md).
- The Playwright acceptance suite MUST gain scenarios for the multi-day display and the
  out-of-window refusal, in the same change (constitution Principle VIII). The refusal
  scenario MUST be seen failing against unfixed code.

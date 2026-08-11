# Feature Specification: Booking Availability Gate

**Feature Branch**: `fix/ticket`

**Created**: 2026-08-11

**Status**: Draft

**Input**: User description: "currently the buy ticket button only opens a modal, what i want now is that the buy ticket button send a POST to check the availability of the ticket, so not on the TnC, add layer before tnc and booking"

## Overview

Nothing between the guest's selection and the Terms & Conditions gate re-confirms that
what they picked can still be bought.

**Buy Ticket opens the Terms gate blind.** Today the button opens the Terms & Conditions
dialog immediately. The selection is only validated against live availability when the
guest ticks the box and presses Agree — after they have read the whole document. A guest
whose ticket sold out while they were choosing spends their attention on terms for a
purchase that was never going to happen, and the failure arrives at the worst possible
moment: one press from completion, with the document already read and agreed to.

This feature inserts an availability confirmation step between **Buy Ticket** and the
**Terms & Conditions** gate. The selection is confirmed against live server state first;
the terms are shown only to a guest whose purchase can actually proceed.

The check is advisory. It reserves nothing and creates nothing — booking on Agree remains
the only operation that takes quota and the only authority on whether a purchase
succeeds. This narrows the window in which a guest can waste their effort; it does not
eliminate the race, and the existing booking-time refusals stay exactly where they are.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Availability is confirmed before the Terms are shown (Priority: P1)

A guest has chosen their tickets and presses Buy Ticket. Before any terms appear, the
system asks the server whether that exact selection can still be bought right now. If it
can, the Terms & Conditions dialog opens exactly as it does today and the rest of the
journey is unchanged. If it cannot — a ticket sold out, a sale window closed while they
were choosing, or the event has no terms to agree to — the guest is told immediately, in
a message that names what is wrong, and the Terms dialog never opens.

**Why this priority**: This is the whole feature. It converts a late, confusing failure
into an early, actionable one, and it is the only slice that changes what a guest
experiences.

**Independent Test**: Select a ticket, exhaust its remaining quota through the normal
purchase path in another session, then press Buy Ticket. The Terms dialog does not open
and a sold-out message appears. Repeat with a fully available selection and confirm the
Terms dialog opens as before.

**Acceptance Scenarios**:

1. **Given** a selection every line of which is still purchasable, **When** the guest
   presses Buy Ticket, **Then** the Terms & Conditions dialog opens and the existing
   agree-and-book behaviour is unchanged.
2. **Given** a selection containing a ticket type whose remaining quota is now lower than
   the quantity chosen, **When** the guest presses Buy Ticket, **Then** the Terms dialog
   does not open, the guest is told which line is short and how many remain, and their
   selection is left intact.
3. **Given** a selection containing an item whose sale window closed while the guest was
   choosing, **When** the guest presses Buy Ticket, **Then** the Terms dialog does not
   open and the guest is told that item is no longer on sale.
4. **Given** a selection combining a bundle and a standalone ticket that both draw on the
   same ticket type, and remaining quota sufficient for either alone but not both, **When**
   the guest presses Buy Ticket, **Then** the check refuses the selection — the lines are
   evaluated together, not one at a time.
5. **Given** an event that has no Terms & Conditions authored yet, **When** the guest
   presses Buy Ticket, **Then** the guest is told terms are not available yet and the
   empty dialog is never shown.
6. **Given** the availability check passes, **When** the last matching ticket is taken by
   another buyer before the guest presses Agree, **Then** booking still refuses the order
   — the check reserves nothing — and the guest sees the same clear sold-out message.
7. **Given** the availability check is in flight, **When** the guest presses Buy Ticket
   again, **Then** no second check is issued and the control shows that work is under way.

---

### User Story 2 - A refused purchase is recoverable (Priority: P2)

A guest whose purchase is refused — at the availability check or at booking — keeps their
selection, sees a message that names the specific problem rather than a generic failure,
and can adjust quantities and try again without reloading or rebuilding their choice.

**Why this priority**: The early refusal in User Story 1 is worth little if the guest is
left staring at a dead end. This story is the difference between a failure the guest can
act on and a failure that just ends the journey sooner.

**Independent Test**: Trigger each refusal reason in turn (sold out, sale window closed,
terms unavailable, server unreachable) and confirm each produces its own message, the
selection survives, and a retry after adjusting the quantity succeeds.

**Acceptance Scenarios**:

1. **Given** a refused availability check, **When** the guest lowers the quantity of the
   offending line to one that is available, **Then** pressing Buy Ticket again succeeds
   and the Terms dialog opens.
2. **Given** the availability check cannot reach the server at all, **When** the guest
   presses Buy Ticket, **Then** they are told the check could not be completed and invited
   to try again — the Terms dialog does not open, and no order is created.
3. **Given** any refusal, **When** the message is shown, **Then** the guest's chosen
   quantities are still on screen and unchanged.

---

### Edge Cases

- **The selection is empty.** Buy Ticket is already inert; no availability check is
  issued.
- **Every item in the selection is unavailable.** The guest is told about every offending
  line, not just the first one found.
- **A ticket type or bundle is deleted, or the event unpublished, between the page load
  and the press.** The check refuses rather than reporting a stale success.
- **The check passes and the guest then edits their selection before opening the terms.**
  The passing decision described the selection as it was; a changed selection is a
  different question, and the guest is checked again on the next press.
- **The guest closes the Terms dialog and presses Buy Ticket again.** A fresh check runs.
  A decision is never reused across presses.
- **The check succeeds but booking fails anyway.** Expected, not a defect: the check is
  advisory and quota moves between the two. The booking-time message must read as the
  same problem, not a new one.
- **Repeated availability checks from one visitor.** The check is an unauthenticated call
  any visitor can issue; it must be throttled per client so it cannot be used to hammer
  the database.

## Requirements *(mandatory)*

### Functional Requirements

#### Availability confirmation before the Terms gate

- **FR-001**: Pressing **Buy Ticket** MUST submit the guest's current selection to the
  server for an availability decision before any Terms & Conditions content is shown.
- **FR-002**: The Terms & Conditions dialog MUST open only when the availability decision
  is that the whole selection is purchasable.
- **FR-003**: The availability check MUST NOT reserve, hold, or deduct quota, and MUST NOT
  create an order. It reports what is true at the moment it runs and nothing more.
- **FR-004**: The availability check MUST evaluate, for the selection as a whole: that each
  item still exists and belongs to the event being bought; that each item's sale window is
  currently open; that remaining quota covers the requested quantities; and that the event
  has Terms & Conditions authored.
- **FR-005**: Quantities MUST be aggregated per ticket type across every line before quota
  is judged, so that a bundle and a standalone ticket drawing on the same ticket type are
  assessed against their combined demand — the same rule the booking transaction already
  applies.
- **FR-006**: A refused check MUST identify every offending line and the reason for each,
  so the guest can be told which part of their selection to change.
- **FR-007**: A refused check MUST leave the guest's selection untouched and MUST NOT
  navigate away from the selection page.
- **FR-008**: While a check is in flight, the Buy Ticket control MUST indicate that work is
  under way and MUST NOT issue a second concurrent check.
- **FR-009**: Each press of Buy Ticket MUST produce a fresh decision. A previous passing
  decision MUST NOT be reused to open the Terms dialog a second time.
- **FR-010**: The availability check MUST be rate limited per client, on the same footing
  as the existing booking call.
- **FR-011**: Booking MUST remain the sole authority on whether a purchase succeeds. Every
  validation the booking transaction performs today MUST continue to be performed there,
  unchanged — the check adds a layer, it does not move or weaken one.

#### Messaging

- **FR-012**: Each refusal reason — insufficient quota, item not on sale, item no longer
  available, terms not authored, check could not be completed — MUST produce its own
  guest-facing message. A single generic failure message for all of them is not
  acceptable.
- **FR-013**: The message shown when booking is refused for a reason the availability
  check also reports MUST match the check's wording for that reason, so a guest who hits
  the same problem at two points in the journey is not told two different stories.

### Key Entities *(include if data involved)*

- **Selection**: The guest's chosen lines — each a ticket type or a bundle with a
  quantity — on one event. Already exists; unchanged by this feature.
- **Availability Decision**: The server's answer about one selection at one moment:
  purchasable or not, and if not, the offending lines with a reason each. It is advisory
  and transient — it reserves nothing, is not stored, and does not bind the booking that
  may follow.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of selections that cannot be bought are stopped before any Terms &
  Conditions text is displayed.
- **SC-002**: Fewer than 1% of terms agreements end in a purchase refused for
  availability reasons — down from every stale selection reaching that point today.
- **SC-003**: 95% of guests reach the Terms & Conditions within 2 seconds of pressing Buy
  Ticket, so the added confirmation step is not perceived as a delay.
- **SC-004**: A refused selection is preserved 100% of the time, and a guest who adjusts
  the offending quantity can complete the purchase without rebuilding their selection.
- **SC-005**: Every refusal reason enumerated in FR-012 is reachable in the acceptance
  suite and produces its own distinct message.
- **SC-006**: The purchase journey's existing end-to-end coverage passes unchanged — the
  guest whose selection is available notices no difference beyond the moment the terms
  appear.

## Out of Scope

- **Closing sales when the event has started.** Raised alongside this feature: the "Event
  starts in" countdown reaching zero does not currently stop a purchase, because ticket
  sale windows are authored per ticket type and are independent of the event's start time.
  That defect is real and unaddressed, and is deliberately deferred to its own change —
  nothing in this specification closes it. A selection whose items are inside their own
  sale windows will still pass this check after the event has begun.
- Removing started or completed events from public listings.
- Any change to how ticket type or bundle sale windows are authored or validated.

## Assumptions

- **The check is a read, not a reservation.** Booking on Agree remains the only operation
  that takes quota. The availability check narrows the window in which a guest can waste
  effort; it does not eliminate the race.
- **Whole-selection decision.** A refused check refuses the whole selection rather than
  partially proceeding or silently adjusting quantities down to what is available. The
  guest decides what to change.
- **Terms availability is part of the decision.** An event with no authored terms is
  refused at the check rather than opening a dialog with nothing in it — the guest cannot
  agree to a document that does not exist, and booking already refuses for this reason.
- **The existing agree-then-book sequence is unchanged.** Once the Terms dialog opens, the
  agree, book, and record-agreement behaviour, including the retry-on-agreement-failure
  path, works exactly as it does today.
- **The row-level availability already shown on the selection page stays as it is.** Rows
  continue to display remaining quota and sale-window state from the event read. This
  feature adds a confirmation at the moment of commitment; it does not change how the list
  presents availability while the guest browses.
- **No schema change is anticipated.** Per-item sale windows, bundle availability, and
  remaining quota already exist and are already read by the booking path.

## Dependencies

- Ticket type sale windows, bundle sale windows and derived availability, and remaining
  quota — all already present and already read by the booking path.
- The existing Terms & Conditions gate and the booking and agreement operations behind it.
- Per-client rate limiting, already applied to the booking call, extended to the new
  availability check.
- Constitution **Principle VIII**: the guest purchase journey is covered by the
  end-to-end acceptance suite, so this change arrives with scenarios that fail against the
  current code before they pass against the fix.
- Constitution **Principle VII**: remaining quota is never served from cache as an
  authority. The availability check reads live state.

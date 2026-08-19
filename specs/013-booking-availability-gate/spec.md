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

## Clarifications

### Session 2026-08-19

- Q: Should the new general message replace every refusal the Buy Ticket check can
  produce, or only the ones about tickets running out? (FR-012) → A: Only the
  availability refusals — insufficient quota, item no longer on sale, item no longer
  exists — collapse into one general message. "No Terms & Conditions authored yet" and
  "the check could not be completed" keep their own distinct wording, because the general
  message would be false for both and refreshing would not help.

- Q: When the guest follows the message's instruction to "refresh the page", should
  their chosen quantities survive? (FR-007, SC-004) → A: No. "Refresh" means a plain
  browser reload and the selection is discarded by design. FR-007 narrows to "the refusal
  itself clears nothing and does not navigate"; the promise that a refused guest never
  rebuilds their choice is retired, and SC-004 is restated accordingly.

- Q: When the guest gets all the way to Agree and booking then refuses because a ticket
  ran out, should they see this same general message? (FR-013) → A: Yes. Booking's
  availability refusals show the identical general message of FR-012, so the guest is
  told one story at both points and never sees a remaining-quota number.

- Q: Should the server still report which lines failed and why, even though the guest
  will never see that detail? (FR-006) → A: Yes. The server keeps reporting every
  offending line and its reason and the UI collapses them into the one message. FR-006
  becomes a diagnostic guarantee rather than a guest-facing one; the response contract is
  unchanged, so this is a wording change and not an API change.

- Q: When booking refuses at Agree, where should the general message appear — inside the
  open Terms dialog, or on the selection page behind it? (FR-013) → A: The Terms dialog
  closes and the message appears on the selection page, in the same region the pre-check's
  refusal uses. One message, one location, and the "reload and adjust" instruction is
  actionable where the guest lands.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Availability is confirmed before the Terms are shown (Priority: P1)

A guest has chosen their tickets and presses Buy Ticket. Before any terms appear, the
system asks the server whether that exact selection can still be bought right now. If it
can, the Terms & Conditions dialog opens exactly as it does today and the rest of the
journey is unchanged. If it cannot, the guest is told
immediately and the Terms dialog never opens: one general message for anything that means
the selection is no longer available at these quantities, and its own message for the two
cases that are not that — no terms authored yet, and a check that could not be completed.

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
   does not open and the guest is shown the one general availability message of FR-012 —
   not the offending line, not the reason, not the remaining count.
3. **Given** a selection containing an item whose sale window closed while the guest was
   choosing, **When** the guest presses Buy Ticket, **Then** the Terms dialog does not
   open and the guest is shown the same general availability message of FR-012 — a closed
   sale window and an exhausted quota are one message to the guest.
4. **Given** a selection combining a bundle and a standalone ticket that both draw on the
   same ticket type, and remaining quota sufficient for either alone but not both, **When**
   the guest presses Buy Ticket, **Then** the check refuses the selection — the lines are
   evaluated together, not one at a time.
5. **Given** an event that has no Terms & Conditions authored yet, **When** the guest
   presses Buy Ticket, **Then** the guest is told terms are not available yet and the
   empty dialog is never shown.
6. **Given** the availability check passes, **When** the last matching ticket is taken by
   another buyer before the guest presses Agree, **Then** booking still refuses the order
   — the check reserves nothing — the Terms dialog closes, and the guest sees the
   identical general message of FR-012 on the selection page, in the same place the
   pre-check's refusal appears, not a second and more specific one inside the dialog.
7. **Given** the availability check is in flight, **When** the guest presses Buy Ticket
   again, **Then** no second check is issued and the control shows that work is under way.

---

### User Story 2 - A refused guest is told what to do next (Priority: P2)

A guest whose purchase is refused for availability at the check is told plainly that one
of their tickets is no longer available in the quantity they chose, and what to do about
it: reload the page and adjust the order against the counts they then see. The refusal
itself does not move them or empty their basket — they read the message on the page they
were already on.

**Why this priority**: The early refusal in User Story 1 is worth little if the guest is
left staring at a dead end. This story is the difference between a failure the guest can
act on and a failure that just ends the journey sooner.

**Independent Test**: Trigger each availability refusal in turn (sold out, sale window
closed, item deleted) and confirm every one produces the identical general message; then
trigger the two non-availability refusals (terms unauthored, server unreachable) and
confirm each produces its own distinct message.

**Acceptance Scenarios**:

1. **Given** a refused availability check, **When** the guest reloads the page and
   chooses quantities that the reloaded counts show as available, **Then** pressing Buy
   Ticket succeeds and the Terms dialog opens.
2. **Given** the availability check cannot reach the server at all, **When** the guest
   presses Buy Ticket, **Then** they are told the check could not be completed and invited
   to try again — the Terms dialog does not open, and no order is created.
3. **Given** any refusal, **When** the message is shown, **Then** the guest is still on
   the selection page with the quantities they entered — the refusal itself has cleared,
   trimmed and navigated nothing. Whether they then reload is their choice, and a reload
   starts the selection over.

---

### Edge Cases

- **The selection is empty.** Buy Ticket is already inert; no availability check is
  issued.
- **Every item in the selection is unavailable.** The answer still records every
  offending line rather than stopping at the first, but the guest reads exactly the same
  single message they would read for one bad line — the count of faults is never
  guest-visible.
- **A ticket type or bundle is deleted, or the event unpublished, between the page load
  and the press.** The check refuses rather than reporting a stale success.
- **The check passes and the guest then edits their selection before opening the terms.**
  The passing decision described the selection as it was; a changed selection is a
  different question, and the guest is checked again on the next press.
- **The guest closes the Terms dialog and presses Buy Ticket again.** A fresh check runs.
  A decision is never reused across presses.
- **The check succeeds but booking fails anyway.** Expected, not a defect: the check is
  advisory and quota moves between the two. The Terms dialog closes and the same general
  message appears on the selection page, character for character — not a new problem, not
  a more detailed one, and not stranded behind a modal telling the guest to reload the
  page it is covering.
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
- **FR-006**: A refused check MUST still identify every offending line and the reason for
  each in its answer — evaluating the whole selection rather than stopping at the first
  fault — so a refusal can be explained after the fact from server-side records. A refused
  check MUST therefore be recorded in the server's logs with its stable reason codes, on
  the same footing as a refused booking, since a payload nobody can read afterwards is not
  a record. This detail is **diagnostic only**: it is never rendered to the guest, who sees
  the single general message of FR-012 no matter how many lines are at fault.
- **FR-007**: A refused check MUST NOT itself alter the guest's selection and MUST NOT
  navigate away from the selection page — the guest stays where they are, with their
  quantities still on screen, and reads the message there. The message directs them to
  reload the page; a reload discards the selection, and that is accepted. Carrying the
  selection across a reload is explicitly **not** a goal of this feature.
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

- **FR-012**: Every **availability** refusal — insufficient quota, item no longer on
  sale, item no longer available — MUST produce one single general message, regardless of
  how many lines are at fault or which of those reasons applies:

  > **Someone was a bit faster!**
  > One of your selected tickets is no longer available in this quantity. Please refresh
  > the page and adjust your order.

  The guest is not told which line is at fault, which reason applied, or how many remain.
- **FR-012a**: The **three** refusals that are not an availability race MUST each keep
  their own distinct guest-facing message, because the general message would be untrue for
  them and refreshing would resolve none of them:
  - the event has no Terms & Conditions authored yet;
  - the check could not be completed at all (the server was never reached);
  - the request was throttled. Being asked to slow down is not someone else having been
    faster, and the guest's remedy is to wait rather than to reload. Its message MUST be:

    > Too many attempts. Please wait a moment and try again.

    the same sentence a throttled booking shows, so one condition reads one way at both
    points rather than leaking the rate limiter's own prose at one of them.
- **FR-013**: Booking MUST show the **same** general message of FR-012 when it refuses
  for any availability reason — the guest who loses the race between the check and Agree
  reads one story, not a second and more detailed one at the later moment. In particular,
  no remaining-quota count reaches the guest at either point.
- **FR-013a**: When booking refuses for an availability reason the Terms & Conditions
  dialog MUST close, and the general message MUST be shown on the selection page in the
  same region the pre-check's refusal uses — so the guest reads the same sentence in the
  same place whichever point refused them, on a page they can act on. Booking refusals
  that are **not** availability reasons keep their present in-dialog behaviour.

### Key Entities *(include if data involved)*

- **Selection**: The guest's chosen lines — each a ticket type or a bundle with a
  quantity — on one event. Already exists; unchanged by this feature.
- **Availability Decision**: The server's answer about one selection at one moment:
  purchasable or not, and if not, the offending lines with a reason each. The per-line
  reasons are diagnostic — they are what the answer is made of, not what the guest is
  shown. It is advisory and transient: it reserves nothing, is not stored, and does not
  bind the booking that may follow.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of selections that cannot be bought are stopped before any Terms &
  Conditions text is displayed.
- **SC-002**: Fewer than 1% of terms agreements end in a purchase refused for
  availability reasons — down from every stale selection reaching that point today.
- **SC-003**: 95% of guests reach the Terms & Conditions within 2 seconds of pressing Buy
  Ticket, so the added confirmation step is not perceived as a delay.
- **SC-004**: A refusal never clears, trims, or navigates on its own — in 100% of
  refusals the guest is left on the selection page with the quantities they entered.
  Recovery is the guest's own reload, after which they choose again against the live
  remaining counts.
- **SC-005**: Every availability refusal reachable in the acceptance suite — insufficient
  quota, item no longer on sale, item no longer available, one offending line or several —
  produces the one general message of FR-012, character for character, and so does an
  availability refusal raised by booking at Agree. The three non-availability refusals of
  FR-012a each produce their own distinct message and never the general one.
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
- **An item belonging to another event is not an availability refusal.** FR-012's general
  message covers the three race conditions it names; a selection line pointing at an item
  from a different event is a malformed request, not a ticket someone else got first, and
  is unreachable through the selection page, which only offers that event's items. It is
  therefore treated like FR-012a's cases and keeps its own wording. Flagged rather than
  asked: the clarification quota was spent on guest-visible decisions, and this path is
  defensive.
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

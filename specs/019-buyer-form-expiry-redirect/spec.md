# Feature Specification: Bundle Form Labels & End-of-Journey Destination

**Feature Branch**: `019-buyer-form-expiry-redirect`

**Created**: 2026-08-18

**Status**: Draft

**Input**: User description: "currently the buyer form on the bundles, it shows visitor 1 2 3 etc, remove it, and when the time expires either on order page or on checkout page it should redirect to the event/[slug] page"

**Clarified 2026-08-18**: the redirect is **not** automatic. The end-of-journey modal keeps
every behaviour it has today — it opens over the preserved screen, it cannot be dismissed,
and it offers exactly one action. Only that action's destination changes: the site home
gives way to the event's own detail page. No guest is ever moved without pressing it.

## Scope note

Two small, independent changes to the guest purchase journey, both confined to the two
order screens:

- the **holder forms** screen — `/events/{slug}/orders/{orderNumber}` — where a guest types
  each ticket holder's details;
- the **payment** screen — `/events/{slug}/orders/{orderNumber}/checkout` — where the QRIS
  code and the Complete Purchase countdown live.

They ship together because they are the two complaints raised about the same pair of
screens, not because either depends on the other. Either can be delivered alone.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - The dead end leads back to the event, not to the front door (Priority: P1)

A guest's order ends without a purchase — the hold on their seats ran out, the payment
window closed, or an admin cancelled it. The screen they were on stays where it was and an
unclosable "Time's Up" dialog opens over it, exactly as today. Its single button is their
one way on, and today it deposits them on the site home page: away from the event they had
chosen, with no sign that the seats they just lost are back on sale.

That button now leads to the event's own detail page instead. The guest presses it, lands
on the event they were buying for, sees the released seats listed live, and starting again
is one step away rather than a navigation exercise. Nothing else about the dialog changes,
and nothing moves them without their press.

**Why this priority**: this is the exit from every abandoned purchase. Every guest whose
order ends reaches it, and today it points away from the sale. It is also the smaller of
the two changes and independent of everything else here.

**Independent Test**: end a held order on each of the two screens in turn, press the
dialog's single button, and confirm the browser arrives on that order's event detail page
with the released seats listed as available.

**Acceptance Scenarios**:

1. **Given** a pending order open at the holder forms with holder details half typed,
   **When** its hold runs out while the guest sits on the page, **Then** the forms, rail,
   and summary stay rendered and the unclosable dialog opens over them — the guest is not
   moved anywhere on their own.
2. **Given** that dialog is open, **When** the guest presses its single action, **Then** the
   browser goes to `/events/{slug}` for that order's own event.
3. **Given** a pending order open at the payment screen, **When** the visible countdown
   reaches `0 : 00`, **Then** the dialog opens at that moment without waiting for the system
   to confirm the expiry, and its single action leads to the same event detail page.
4. **Given** an order that was cancelled rather than timed out, **When** the guest is on
   either screen, **Then** the dialog opens with its cancellation wording and its single
   action leads to the event detail page just the same.
5. **Given** an order that had already ended before the guest opened either screen — a saved
   link reopened the next morning, a second tab — **When** the screen loads, **Then** the
   dialog opens over it as today and its action leads to the event detail page.
6. **Given** the dialog is open on either screen, **When** the guest reads its action,
   **Then** the label names the event page it leads to; no control reading "home page"
   leads anywhere but home.
7. **Given** the dialog is open, **When** the guest tries to dismiss it — the X, Escape, a
   backdrop click — **Then** it does not close, and the page behind stays inert: unchanged
   from today.
8. **Given** the dialog is open, **When** the guest counts its actions, **Then** there is
   exactly one. The change adds no second button.
9. **Given** the guest arrives on the event detail page from the dialog, **When** they look
   at the ticket list, **Then** the seats the ended order had held are listed as available
   again, with no manual refresh.
10. **Given** a guest on the payment screen whose payment settles inside the window,
    **When** the settlement lands, **Then** they reach the confirmation as before — no
    dialog, nothing about this feature applies.

---

### User Story 2 - Bundle holder cards carry no visitor number (Priority: P2)

A guest buying more than one unit of the same bundle sees one holder card per unit, and
today each card's heading is suffixed with a number — "Visitor 1", "Visitor 2". The
numbering reads as an instruction the order does not impose: it suggests a fixed identity
per card when in fact any holder can go on any card. It is removed. Each card keeps the
bundle's name and its ticket-count badge, which is what the guest actually needs to know
about what they are filling in.

**Why this priority**: cosmetic, and it changes no data and no submission. Valuable, but a
guest can complete a purchase today without it.

**Independent Test**: open the holder forms for an order containing two units of one bundle
and confirm two cards appear with no visitor number anywhere on either, then complete the
order and confirm both units' tickets are issued to the details typed.

**Acceptance Scenarios**:

1. **Given** an order containing two units of the same bundle, **When** the guest opens the
   holder forms, **Then** two cards appear, each titled with the bundle's name and badged
   with its ticket count, and neither carries a "Visitor 1" / "Visitor 2" style number.
2. **Given** that same order, **When** the guest fills both cards and continues to payment,
   **Then** the tickets issued are exactly as before the numbering was removed — every
   ticket in a unit carries that card's holder.
3. **Given** an order containing a single unit of a bundle, **When** the guest opens the
   holder forms, **Then** the card is unchanged — it never carried a number.
4. **Given** an order of standalone tickets, or a bundle order placed before per-unit
   grouping existed, **When** the guest opens the holder forms, **Then** those cards are
   unchanged.
5. **Given** a bundle whose name is too long for one line, **When** the guest looks at the
   card, **Then** the whole name is on screen — it wraps onto a second line rather than
   being cut off with an ellipsis, and no tooltip is needed to read it back.
6. **Given** any bundle card, **When** the heading wraps, **Then** the ticket-count badge
   stays against the right edge of the heading, on its own line if it had to move down.
7. **Given** any of the above, **When** a screen reader reads a card, **Then** it announces
   no visitor number — the numbering is gone from what is announced as well as from what is
   drawn.

---

### Edge Cases

- **Two units of one bundle become visually identical.** With the number gone, nothing on
  the two cards tells them apart. This is accepted: card order is stable, any holder may go
  on any card, and a correct submission does not depend on the guest distinguishing them.
  See Assumptions.
- **Pressing back after leaving through the dialog** returns the guest to the ended order
  screen, where the dialog opens again — the order is still ended, so there is nothing else
  to show. This is unchanged from today's behaviour with the home page, and FR-005 keeps it
  that way rather than introducing new history handling.
- **The countdown reaches zero at the same moment the payment settles.** A settled order is
  not an ended one: the guest reaches the confirmation and sees no dialog.
- **The system reports the expiry moments after the countdown already showed zero.** One
  ending, one dialog — it must not re-open, change wording, or change destination on the
  second signal.
- **The event has since been removed or unpublished.** The action still leads to
  `/events/{slug}`, which shows its own not-found treatment. The dialog does not pre-verify
  its destination.
- **An order reached through the wrong event's address** is still refused outright before
  any dialog is raised, so the destination can never be built from a slug that does not own
  the order.
- **Both order screens open in two tabs.** Each raises its own dialog with the same
  destination; neither is left showing a live-looking dead order.
- **The confirmation screen's own ended-order treatment** is a separate surface and is not
  touched.

## Requirements *(mandatory)*

### Functional Requirements

#### End-of-journey destination (User Story 1)

- **FR-001**: The end-of-journey dialog MUST keep every behaviour spec 011 FR-022 mandates:
  it opens over the screen the guest was on — holder forms or payment — with that screen's
  progress rail, forms or payment panel, and order summary still rendered behind it; it is
  the same dialog on both screens; on the payment screen it opens the moment the visible
  countdown reaches zero without waiting for the system to report the expiry; and nothing
  scannable is left behind it.
- **FR-002**: The dialog MUST remain unclosable exactly as spec 011 FR-023 mandates — no
  close control, no dismissal on Escape or backdrop click, the page behind inert — and MUST
  keep offering exactly ONE action, as spec 011 FR-024 mandates.
- **FR-003**: That single action MUST lead to the event detail page of the order's own
  event, `/events/{slug}`. This replaces the site home as its destination.
- **FR-004**: The action's label MUST name where it leads. A control reading "Return to Home
  Page" MUST NOT survive on a button that no longer goes to the home page.
- **FR-005**: The guest MUST NOT be moved off either order screen automatically. Pressing the
  dialog's action is the only thing that navigates; an ending on its own changes what is on
  screen and nothing else. No history entry is rewritten and no new back-button behaviour is
  introduced.
- **FR-006**: FR-003 MUST apply identically to both endings the dialog serves — an order
  that ran out of time and an order that was cancelled. The two variants continue to differ
  only in heading and body copy.
- **FR-007**: FR-003 MUST apply whether the order ended while the guest watched or had
  already ended when they opened the screen.
- **FR-008**: FR-003 MUST apply identically from both order screens. The destination is
  derived from the order's own event, so the two screens cannot drift to different
  destinations.
- **FR-009**: The dialog MUST still offer exactly one action after this change. In
  particular this MUST NOT reinstate the second "Repeat Order" action spec 011 FR-024
  deleted: that action led to the event's ticket selection step, whereas this single action
  leads to the event's landing page.
- **FR-010**: Nothing about seat release, order status, payment handling, or the dialog's
  body copy changes. This feature governs where the dialog's one action leads and how it is
  labelled, and nothing else.

#### Bundle holder cards (User Story 2)

- **FR-011**: The holder forms MUST NOT present a per-unit visitor number on any card. This
  covers everywhere the heading appears — the drawn title, the full-heading reveal used when
  the title is clipped, and anything announced to assistive technology.
- **FR-012**: A bundle card MUST keep its existing identity: the bundle's name as its title
  and its ticket-count badge. Removing the number MUST NOT remove or alter either.
- **FR-013**: Cards for standalone tickets, and for bundle orders placed before per-unit
  grouping existed, MUST be unchanged — they never carried a number.
- **FR-014**: Removing the number MUST NOT change how many cards appear, which tickets each
  card covers, the order they appear in, or what is submitted. An order of two units of one
  bundle MUST still present two independently filled cards, and each card's details MUST
  still apply to every ticket in its own unit.
- **FR-015** *(added 2026-08-18)*: A card's heading MUST show the group's whole name. It
  MUST NOT be clipped to a single line with an ellipsis, and MUST wrap onto further lines
  when it does not fit. Because nothing is hidden any more, the tooltip that existed solely
  to read back the clipped tail MUST be removed rather than left offering text already on
  screen.
- **FR-016** *(added 2026-08-18)*: The ticket-count badge MUST stay against the right edge
  of the heading, whether it shares the first line with the name or wraps onto its own. The
  heading MUST fill the width its card leaves it, so that "the right edge" means the card's
  edge rather than the end of the name — otherwise the badge lands in a different place on
  every card, which is the opposite of what right-aligning it is for. Where a card's header
  already carries something beside the heading, the badge sits at the edge of the space that
  remains.

> **Amends**: FR-003 and FR-004 amend spec 011 FR-024, which fixed the action as a button
> labelled "Return to Home Page" leading to the site home. Every other clause of FR-024 —
> the Figma `293-3` card, the alert icon in its tinted circle, the heading, the body copy
> that must not tell the guest to repeat their order, the single full-width filled button —
> stands unchanged, as do FR-022 and FR-023 in full. This is a change of destination and
> label, not of design or behaviour.

> **Out of scope**: the confirmation screen's own handling of an ended order
> (`/events/{slug}/orders/{orderNumber}/success`) is untouched — a guest who lands there has
> already left the two screens this feature governs. The event detail page is a destination
> here, not a subject: it gains nothing and changes in no way.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A guest whose order ends on either order screen reaches that event's detail
  page in exactly one press, for both endings and from both screens — four combinations,
  one press each.
- **SC-002**: From the page they land on, reaching that event's ticket selection to start
  again takes one further action, with no back button, no retyped address, and no pass
  through the site home.
- **SC-003**: The seats the ended order had held are shown as available on the page the
  guest lands on, on the first read, with no manual refresh.
- **SC-004**: The dialog's action label matches its destination in 100% of cases — no
  control naming the home page leads to the event page.
- **SC-005**: The guest is never navigated off either order screen without pressing the
  dialog's action — 0 automatic navigations across repeated runs of every ending on both
  screens.
- **SC-006**: The dialog offers exactly one action and refuses every dismissal — close
  control, Escape, backdrop — in all four combinations, with 0 regressions against today's
  behaviour.
- **SC-007**: One ending produces one dialog in 100% of runs, including when the visible
  countdown's zero and the system's own report of the expiry arrive seconds apart.
- **SC-008**: No visitor number appears on the holder forms for any order shape — a
  multi-unit bundle, a single-unit bundle, standalone tickets, and a bundle order placed
  before per-unit grouping — verified across all four.
- **SC-009**: Every order shape still issues exactly the tickets it issued before this
  change, to exactly the holders typed: 100% parity on card count, card ordering, and
  issued-ticket holder data.
- **SC-010**: A guest who pays inside the window still reaches the confirmation from either
  screen — no regression in the completed purchase journey.

## Assumptions

- **The action's new label is "Return to Event Page"**, keeping the existing sentence shape
  and swapping only the destination it names. Any wording that plainly names the event page
  satisfies FR-004; this is the minimal-change default, not a constraint.
- **The destination is the event's landing page, not its ticket selection step.** The
  request named `/events/{slug}`, which is where an event is introduced — one step short of
  the selection screen the deleted "Repeat Order" action used to jump to. Chosen as written
  rather than "improved" into a deeper link, since spec 011 removed that deeper link
  deliberately.
- **The destination is the event that owns the order.** Because a wrong-event address is
  refused before any dialog is raised, the address's slug and the order's event slug are
  necessarily the same at that point, so the two readings cannot diverge.
- **The action stays an ordinary navigation.** It adds a history entry as it does today, so
  pressing back returns to the ended order screen and its dialog. Left alone deliberately:
  the instruction was to keep the current logic and change only the destination.
- **The body copy is unchanged.** It states what happened and that the seats went back on
  sale, which stays true and stays consistent with FR-024's rule against promising a repeat.
- **Removing the visitor number is accepted as making two units of one bundle
  indistinguishable.** The numbering was introduced (spec 010 US3) precisely to tell them
  apart, so this is a deliberate reversal, not an oversight. It is safe because card order
  is stable within a render, every card demands the same fields, and no rule binds a
  particular holder to a particular unit — the guest fills them in the order shown and the
  submission is correct either way. Flagged because it is the one thing this removal costs.
- **The removal is presentational.** Whatever internal grouping produces one card per
  purchased unit stays as it is; only the label drawn from it goes.
- **The heading change (FR-015, FR-016) was requested during implementation** and is
  recorded here rather than folded in silently. It removes more than it adds: pinning the
  heading to one line required detecting overflow, which leaves no trace in the DOM, so it
  had carried a resize observer, a font-loading callback, and a tooltip whose only purpose
  was to hand back what the ellipsis took. Wrapping shows the name outright and all of that
  goes. It is grouped under User Story 2 because it touches the same heading, but it is
  independent of the visitor-number removal and could ship alone.
- **Coverage lands with the change.** Both screens' ended-order paths and the bundle card
  headings are covered by the acceptance suite today, so per constitution Principle VIII the
  affected scenarios are updated in the same change rather than after it — and the
  destination and label scenarios are confirmed red against the unchanged code first.

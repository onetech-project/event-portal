# Feature Specification: Event-Scoped Guest Navigation

**Feature Branch**: `007-event-scoped-routes`

**Created**: 2026-08-04

**Status**: Draft

**Input**: User description: "i want to make event countdown to be in the layout , move the orders, and tickets link to be inside of the event/:slug, so it will be isolated inside each events, the countdown can be inside the events/:slug layout"

## Overview

The guest purchase journey is **events → choose ticket → checkout → order → done**. Today
that journey has no continuity: the countdown to the event appears only on the first
screen, the four-step progress rail is shown once and then vanishes, the order screen
lives at a top-level address that says nothing about which event it belongs to, and there
is no confirmation screen at all — the order screen quietly relabels itself.

This feature makes an event a self-contained space. Every screen in the purchase journey
lives under that event, framed by shared chrome that shows which event the guest is in,
how long until it starts, and how far through the journey they are. The journey ends on a
dedicated confirmation screen.

Ticket lookup is deliberately **not** part of this. A guest holding a ticket code should
not have to know or pick an event first, so ticket lookup stays at the top level exactly
as it is today.

## Clarifications

### Session 2026-08-04

- Q: With no ticket pages under `events/[slug]`, what should the root `/tickets` and `/tickets/[code]` pages actually do? → A: Leave both exactly as they are today — root-level code entry and ticket detail. No event scoping, no forwarding, no backend change.
- Q: Should the four-step progress rail (Booking → Registration → Payment → Done) follow the guest across the journey? → A: Yes — move it into the event layout beside the countdown, and add a separate "done" confirmation screen after payment succeeds so the order page and the success screen are distinct routes.
- Q: When a guest re-opens the order page for an order that is already paid, what should happen? → A: Redirect to the done screen. The order page is only ever the *paying* screen; any settled order (paid, expired, cancelled) forwards to done, which shows the outcome.
- Q: Should guests be able to resend their own ticket email from the done page? → A: Yes — add a public, rate-limited resend endpoint reusing the existing notification service. The button is live.
- Q: The done page design shows the progress rail but no event countdown bar. Should the countdown render there? → A: Show it everywhere — the countdown renders on all four nested screens including done.

**Scope removed by this session** (previously specified, now explicitly out): event-scoped
ticket lookup screens, a top-level ticket-code resolver, a legacy `/orders/:orderNumber`
forwarder, and the `event_slug` API field that existed only to serve those.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Shared framing follows the guest through the journey (Priority: P1)

A guest picks tickets for an event, registers, pays, and reaches confirmation. Across all
four screens the same live countdown to the event's start stays visible, and the same
four-step progress rail advances with them — so they always know which event they are
committing to, how soon it happens, and how much is left to do.

**Why this priority**: This is the visible heart of the request and delivers value on its
own. It also fixes a live defect: the progress rail currently renders only on the first
screen, so steps 2–4 are never shown to anyone.

**Independent Test**: Walk the journey from an event through checkout to the order screen
and confirm the countdown renders identically and keeps ticking, and that the rail marks
the correct current stage on each screen.

**Acceptance Scenarios**:

1. **Given** an event whose start date is in the future, **When** a guest views any screen in that event's journey, **Then** a live countdown to the event's start is displayed above the screen's content and updates once per second.
2. **Given** a guest moves from ticket selection to checkout, **When** the new screen renders, **Then** the countdown continues without resetting, flashing, or disappearing.
3. **Given** a guest is on any screen in the journey, **When** the screen renders, **Then** the four-step rail shows the stage they are on as current, every earlier stage as completed, and every later stage as not yet reached.
4. **Given** an event whose start date has passed or is absent, **When** a guest views any screen in that event's journey, **Then** no countdown is displayed and the rest of the screen — including the rail — renders normally.

---

### User Story 2 - The purchase journey is addressed within its event (Priority: P1)

Checkout and the order screen live under the event they belong to. The address a guest
bookmarks names the event; an order opened under the wrong event is refused; and every
back link inside the journey keeps the guest in that event.

**Why this priority**: An order is meaningless without its event, and the order address is
what the guest keeps. This story also finishes a half-completed migration that currently
leaves the selection-to-checkout link broken.

**Independent Test**: Complete a checkout, confirm the resulting address is event-scoped
and reloads correctly, then open the same order number under a different event's slug.

**Acceptance Scenarios**:

1. **Given** a guest selects tickets on an event page, **When** they continue, **Then** the checkout address is scoped to that event and carries their selection.
2. **Given** a guest completes checkout, **When** the order screen opens, **Then** its address is scoped to that event and payment instructions are shown.
3. **Given** a guest re-opens a saved order address for an unpaid order, **When** the screen loads, **Then** the payment instructions and the event's shared framing are both present.
4. **Given** a guest opens an order address whose order belongs to a different event, **When** the screen loads, **Then** a clear "order not found for this event" message is shown, with no order details visible and no redirect to the owning event.
5. **Given** a guest is anywhere inside the journey, **When** they use a back or return link, **Then** they stay within the same event unless they explicitly choose to browse all events.

---

### User Story 3 - The journey ends on a confirmation screen (Priority: P2)

Once payment succeeds the guest lands on a dedicated confirmation screen showing what they
bought, what they paid, and where their tickets were emailed — with a way to resend that
email if it never arrived, and a way back to the start.

**Why this priority**: The journey is usable without it (the order screen already reports
a paid status), but the guest is left without a receipt, without a recovery path if the
email is lost, and with a progress rail whose fourth step is never reached.

**Independent Test**: Pay for an order in the sandbox and confirm you land on a distinct
confirmation address showing the order number, total paid, and purchased items; then
press resend and confirm the email arrives again.

**Acceptance Scenarios**:

1. **Given** an order's payment succeeds, **When** the system observes the settled status, **Then** the guest is taken to the confirmation screen for that order.
2. **Given** a guest opens the order address for an already-settled order, **When** the screen loads, **Then** they are taken to the confirmation screen rather than shown payment instructions again.
3. **Given** a guest is on the confirmation screen for a paid order, **When** the screen renders, **Then** it shows a success message naming the event, the order number, the total paid, every purchased item with its quantity, and a notice that a confirmation email was sent.
4. **Given** a guest is on the confirmation screen, **When** they press resend, **Then** the ticket email is sent again to the buyer's registered address and the screen confirms it.
5. **Given** a guest presses resend repeatedly, **When** the allowed frequency is exceeded, **Then** they are told to wait and no further email is sent.
6. **Given** an order settled as expired or cancelled rather than paid, **When** the guest reaches the confirmation screen, **Then** it reports that outcome instead of a success message, and offers a way back to the event.
7. **Given** a guest is on the confirmation screen, **When** they choose to leave, **Then** they return to the home screen.

---

### Edge Cases

- **Unknown event**: A guest opens any journey screen under an event slug that does not exist — the whole area, including the shared framing, must show a clear "event not found" state rather than a broken or half-rendered screen.
- **Event data still loading**: The framing needs the event's start date, which is not instantly available — nested screens must not be blocked from rendering, and no misleading placeholder countdown (e.g. `00:00:00`) may be shown before the real value is known.
- **Competing countdowns**: The order screen shows both the event-start countdown and the payment-expiry countdown at once. Each must be labelled so unambiguously that a guest glancing at the screen cannot mistake the event countdown for their payment deadline (FR-017).
- **Countdown crosses zero while a guest is on-screen**: The event starts mid-journey — the countdown must disappear cleanly rather than counting into negative values or freezing at zero.
- **Start date in the far future / far past**: Multi-year gaps must render sensibly (day counts are not capped at two digits) and past dates must not render at all.
- **Deep-linked mismatch**: An order number that exists but under a different event must be refused with a message scoped to the current event, never silently redirected to the other event.
- **Payment settles while the guest is watching**: The order screen forwards to confirmation the moment the settled status is observed, without the guest reloading.
- **Resend abuse**: The resend action is reachable by anyone who knows an order number, so it must be rate-limited and must only ever send to the buyer's stored address — never to an address supplied by the caller.
- **Very long event names**: The shared framing must not break its layout for events with unusually long names.

## Requirements *(mandatory)*

### Functional Requirements

#### Shared event framing

- **FR-001**: The system MUST provide shared framing for an event's journey that renders on every screen in it and is not re-created when the guest moves between those screens.
- **FR-002**: The shared framing MUST display a live countdown to the event's start, updating at least once per second, showing days, hours, minutes, and seconds — on every screen in the journey, including confirmation.
- **FR-003**: The system MUST hide the countdown entirely when the event has no start date recorded or when the start date has already passed, rather than showing zeros or negative values.
- **FR-004**: The shared framing MUST display the four-stage progress rail (Booking, Registration, Payment, Done), marking the guest's current stage, showing every earlier stage as completed, and every later stage as not yet reached.
- **FR-005**: The current stage MUST be derived from the screen the guest is on, not stored or passed by hand, so no screen can display a stage that contradicts its address.
- **FR-006**: The shared framing MUST resolve the event once for the whole journey, so nested screens do not each re-request the same event and the countdown does not restart when the guest navigates between them.
- **FR-007**: The shared framing MUST surface a clear "event not found" state for an unrecognised event, and MUST NOT render nested screen content in that case.
- **FR-008**: While the event is still being resolved, the system MUST NOT display a countdown containing placeholder or zero values.
- **FR-009**: No individual screen may render its own duplicate copy of the countdown or the progress rail once the shared framing provides them.

#### Event-scoped purchase journey

- **FR-010**: The checkout screen MUST be addressed within its event, taking the event from its address rather than from a query parameter.
- **FR-011**: Order screens MUST be addressed within their event, such that the address identifies both the event and the order.
- **FR-012**: On successful checkout, the system MUST send the guest to the event-scoped order address for the new order.
- **FR-013**: The system MUST reject an order address whose order does not belong to the named event, showing a not-found message and a way back into that event, and MUST NOT redirect to the owning event.
- **FR-014**: Return and back links inside the journey MUST keep the guest within that event unless they explicitly choose to browse all events.
- **FR-015**: All existing guest capabilities on the moved screens (order status polling, on-demand payment refresh, payment expiry handling) MUST continue to work unchanged after the move.

#### Confirmation screen

- **FR-016**: The system MUST provide a confirmation screen addressed within its event and its order, distinct from the order screen.
- **FR-017**: On any screen showing both the event-start countdown and a payment-expiry countdown, each MUST carry an explicit label naming what it counts down to, and the two MUST be visually distinguishable from each other by more than position alone.
- **FR-018**: The order screen MUST send the guest to the confirmation screen as soon as the order's status is settled — whether it settles while they watch, or was already settled when they opened it.
- **FR-019**: The confirmation screen MUST show, for a paid order: a success message naming the event, the order number, the total paid, every purchased item with its quantity, and a notice that a confirmation email was sent to the buyer's registered address.
- **FR-020**: The confirmation screen MUST report an expired or cancelled outcome distinctly from a successful one, and MUST offer a way back to the event in that case.
- **FR-021**: The confirmation screen MUST offer a resend action that re-sends the ticket email, and MUST report the outcome of that action to the guest.
- **FR-022**: The confirmation screen MUST offer a way back to the home screen.

#### Resending the ticket email

- **FR-023**: The system MUST allow a guest to trigger a resend of their ticket email without authenticating, identified by their order number.
- **FR-024**: A resend MUST deliver only to the buyer's stored email address; the system MUST NOT accept a destination address from the caller.
- **FR-025**: The system MUST limit how often a resend may be triggered for the same order, and MUST tell the guest to wait rather than silently discarding an over-frequent request.
- **FR-026**: A resend request for an unknown order number MUST be refused without disclosing whether that order exists.

#### Out of scope

- **FR-027**: Ticket lookup MUST remain at the top level, unchanged — a guest holding a ticket code MUST NOT be required to identify an event first.
- **FR-028**: The retired top-level order address is removed rather than forwarded; the system does not preserve it.

### Key Entities

- **Event**: The organising unit for the journey. Identified publicly by its slug; supplies the name and start date the framing displays. Checkout, order, and confirmation all belong to exactly one event.
- **Order**: A guest's purchase for a single event. Addressed by its order number within its event; carries the payment state that decides whether the guest sees the payment screen or the confirmation screen, plus the buyer email a resend targets.
- **Order item**: A purchased ticket type or bundle with its quantity, listed on the confirmation screen.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A guest sees the time remaining until the event starts on 100% of the screens in the purchase journey, without the countdown resetting or blanking between screens.
- **SC-002**: A guest can tell which of the four stages they are on from every screen in the journey; zero screens show a stage that contradicts their actual position.
- **SC-003**: 100% of guest-facing screens in the journey show that event's identity; zero screens leave the guest unable to tell which event they are in.
- **SC-004**: The complete purchase journey (select → checkout → pay → confirmation) succeeds end-to-end, with no loss of any capability available before this change.
- **SC-005**: Opening an order under the wrong event produces a clear, event-scoped not-found message in 100% of cases, and never exposes another event's order.
- **SC-006**: A guest whose confirmation email did not arrive can trigger a resend from the confirmation screen in one action, and receives it without contacting support.
- **SC-007**: Repeated resend attempts for the same order beyond the allowed frequency result in zero additional emails sent.
- **SC-008**: On the payment screen, a guest asked which number is their payment deadline identifies it correctly without hesitation; neither countdown is unlabelled.
- **SC-009**: A guest holding only a ticket code still reaches their ticket detail in the same number of steps as before this change.

## Assumptions

- The events browse list and the event detail screen stay at their current addresses; only screens *below* an event are affected.
- Administrative screens are unaffected, including the existing admin resend action, which continues to work as it does today.
- The countdown targets the event's **start date**, matching current behaviour, not any individual ticket's sales window.
- "Isolated inside each event" means addressing and framing, not access control: an order is still reachable by anyone who knows its number, exactly as today. No authentication is introduced.
- Confirmation emails already sent to guests contain the ticket PDF, not a link to an order screen, so removing the top-level order address strands no email. It may break browser bookmarks made since the order screen shipped; that is accepted (FR-028).
- The confirmation screen reads its contents from the order the guest already has access to — no new data is required beyond what the order screen already displays.
- Rate limiting for resend is enforced per order rather than per caller, since a guest's network address is not a reliable identity and the protected resource is the buyer's inbox.
- The visual design for the confirmation screen is fixed by the approved Figma frame; this specification describes what it must convey, not how it looks.

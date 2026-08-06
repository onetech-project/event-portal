# Feature Specification: End-to-End Guest Purchase Flow

**Feature Branch**: `008-e2e-purchase-flow`

**Created**: 2026-08-05

**Status**: Draft

**Input**: User description: "Implement end to end of the project: homepage becomes an event grid with a ticket-verification aside; clicking an event shows a CMS-driven event detail page before tickets; buying a ticket persists the order immediately (pending, no payment link) after T&C agreement with a 1-hour hold and background expiry that restores quota; order page collects visitor forms; payment creates a QR via the payment gateway with a 14-minute window and a QR refresh at 7 minutes; success page plus email receipt with e-ticket attachment; expired states for order and payment; admin CMS gains WYSIWYG inputs for event content and per-event Terms & Conditions; T&C agreement is recorded in the database."

## Overview

Today the guest journey stops short of a real transaction: the homepage forces a
choice between two buttons, an event's tickets appear with no context about the
event itself, and "buying" a ticket never creates a durable order — nothing is
reserved, nothing expires, and nothing is emailed.

This feature completes the journey end to end:

1. **Discover** — the homepage is a grid of events with a ticket-verification
   card alongside it.
2. **Learn** — selecting an event opens a rich event detail page whose content
   (description, activities, guest stars, guidelines) is authored by admins
   through a rich-text (WYSIWYG) editor in the CMS.
3. **Choose** — "Buy ticket" leads to the event's ticket/package selection page.
4. **Commit** — agreeing to the event's Terms & Conditions creates a real order
   in `PENDING` state with no payment link yet, records the agreement, deducts
   quota, and holds the seats for **1 hour**. A background process expires
   overdue orders and restores their quota.
5. **Identify** — the order page collects visitor details for each ticket.
6. **Pay** — continuing to payment obtains a QR from the payment gateway; the
   payment window is **14 minutes**, with the QR refreshed once at the
   **7-minute** mark. The event countdown remains visible via the shared event
   layout throughout payment.
7. **Confirm** — successful payment leads to a finished-order page, and the
   buyer receives an email receipt with the e-ticket(s) attached; the email can
   be re-sent.
8. **Fail gracefully** — expired orders and expired payments each show a clear
   expired state and let the guest start over.

### Design References (Figma — Project JIVE)

| Screen | Node |
|--------|------|
| Event detail page | [4-5](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=4-5&m=dev) |
| Ticket selection page | [12-1523](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=12-1523&m=dev), [20-796](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=20-796&m=dev), [244-6525](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=244-6525&m=dev), [318-221](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=318-221&m=dev) |
| T&C dialog | [41-1287](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=41-1287&m=dev) |
| Order / visitor forms | [202-24](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=202-24&m=dev), [244-7184](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=244-7184&m=dev), [203-379](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=203-379&m=dev) |
| Payment (QR) | [203-1157](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=203-1157&m=dev), [32-1366](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=32-1366&m=dev) — **deviation: event countdown stays visible (shared layout), unlike the design** |
| Finished order | [47-2396](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=47-2396&m=dev) |
| Expired order/payment | [288-2295](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=288-2295&m=dev) |
| Email receipt + e-ticket | [251-2](https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=251-2&m=dev) |

## Clarifications

### Session 2026-08-05

- Q: Should the event detail data include the event's tickets and packages? →
  A: No — the event detail page loads event content only (description,
  activities, guest stars, guidelines, terms availability). Ticket and package
  data are fetched by their own dedicated lookups only when the guest proceeds
  to the ticket selection page.
- Q: Should the service interface follow the codebase's existing naming or the
  originally proposed endpoint list? → A: Adopt the proposed endpoint list
  verbatim (`/event`, `/event/:id`, `/ticket/:event_id`, `/packages/:event_id`,
  `/ticket/book`, `/ticket/terms-condition/:event_id`,
  `/ticket/terms-condition/:order_id`, `/ticket/checkout/:order_id`, SSE
  status stream, `/ticket/resend-email`); recording the agreement is its own
  step against the created order.
- Q: Are visitor details saved separately before payment or submitted together
  with "Continue to Payment"? → A: Together with Continue to Payment (one
  call saves the forms and starts payment); a revisit before payment starts
  presents empty forms — details are not persisted server-side until then.
- Q: Should responses use the `{code, message, data}` envelope with snake_case
  properties, and on which endpoints? → A: Yes — every endpoint, guest and
  admin, returns `{code: number, message: string, data: T}` with snake_case
  property names throughout `data`.
- Q: Keep the existing UUID schema with the proposed tables mapped onto it, or
  rebuild the database to match the proposed serial/uid schema verbatim? →
  A: Keep the existing UUID schema; the proposed schema's content (CMS
  content tables, per-event T&C, visitor detail fields, agreement record) is
  delivered as an extension migration mapped onto existing conventions.
- Q: Is there a background job for the hold/payment deadlines, and must it
  log? → A: Yes — the existing expiry sweeper handles both deadlines (they
  share one expiry timestamp); every order it expires MUST be logged with the
  order identity and the quota restored. The 7-minute QR refresh is a
  client-side timer, not a job.
- Q: Must the WYSIWYG editor be free/open source? → A: Yes — open-source
  (MIT) editor only, integrated with the existing shadcn-style component
  system; no paid editor tiers or cloud extensions.
- Q: Does the event detail page show the booking stepper rail? → A: No —
  per Figma 4-5 the detail page renders without the Booking→Done stepper
  (the rail appears from the ticket selection page onward), and its banner
  spans the full viewport width edge-to-edge rather than sitting inside the
  content container.
- Q: Where does the dark info bar sit relative to the banner, and what does
  it contain? → A: Per Figma 4-5 the banner stays a square full-bleed image;
  the white content sheet below rises over its foot with rounded TOP corners
  (the rounding belongs to the sheet, not the image), and the bar straddles
  the sheet's top edge. It carries four cells: Dates, Scale, Venue,
  Buy Tickets. Dates render as a collapsed range — "26 - 27 Sept 2026" —
  repeating month/year only when the range crosses them. Scale is a NUMBER
  (expected visitor count) stored on the event and editable in the admin
  form; the guest page renders "{count}+ Visitors" — dot-grouped below one
  million ("100.000"), compact above ("1M", "1B", "1T") — and omits the
  cell when unset.
- Q: What renders where an event has no banner image? → A: A gray
  placeholder block with a centered image icon, both on the homepage grid
  card and on the detail page hero — never a collapsed or missing image
  area.
- Q: How does the registration (visitor forms) page differ from the first
  build? → A: Per Figma 12-4456: (1) the buyer card carries a blue info
  badge — "The invoice and e-ticket will be sent via email" — beside its
  title instead of a plain helper paragraph; (2) the order summary shows a
  PAYMENT METHOD section where QRIS renders as a selected radio control
  (sole method, pre-selected, not clickable-off); (3) the aside shows NO
  order-hold countdown box on this step — the shared header's event
  countdown remains the only timer; (4) the summary's EVENT box shows
  venue, address, the collapsed date range, and a "Gate opens at HH:MM WIB"
  line derived from the event start time. Per Figma 206-3145 the Order
  Summary is the same ticket-stub panel as the selection summary: icon-chip
  header (no booking-id line), labeled TICKETS rows with a brand ×qty badge,
  a notched dashed tear before PAYMENT METHOD, and the QRIS row as a
  ring-and-dot selected radio with a logo chip.
- Q: Where does the registration page's event data come from? → A: The
  `event` object on GET /ticket/order/:order_id is extended with `venue`,
  `address`, `start_date`, `end_date` (resolved through the order domain's
  event-lookup boundary, no cross-domain JOIN) — no new columns needed.
- Q: What does the buyer form collect? → A: The same personal field set as a
  visitor card (Figma 12-4456): full name, email, phone, gender, and date of
  birth. `orders` gains nullable `buyer_dob` and `buyer_gender` columns,
  written in checkout TX-D with the rest of the buyer identity; the checkout
  body gains `buyer_dob` (YYYY-MM-DD) and `buyer_gender`, validated with the
  same rules as the visitor fields.
- Q: Where do the gender options come from? → A: A `genders` master table
  (name unique, is_active), seeded FEMALE/MALE, served by the public
  GET /ticket/genders read. Every gender select builds its options from it —
  the set is data, not code — and checkout validates every submitted gender
  against the active rows, reporting failures in the same 400001 field map.
  Selects use the design-system Select component (not the native tag), and
  the trigger shows the human label ("Female") while the stored value stays
  canonical ("FEMALE").
- Q: Are order statuses also master data? → A: Yes — an `order_statuses`
  table (name unique, is_active) seeded PENDING/PAID/CANCELLED/EXPIRED.
  `orders.status` keeps its varchar value so status-filtering queries are
  untouched, but its CHECK constraint is replaced by a foreign key onto the
  master table, making the table authoritative.
- Q: How are order fees handled? → A: A `fees` master table the admin manages
  at /admin/fees (name, PERCENT-of-subtotal or FIXED type, value, position,
  active). Booking applies the active rows to the order's subtotal and
  FREEZES the computed lines into `order_fees` (display name with the
  percentage baked in, e.g. "PPN (11%)", plus amount) — a later fee edit
  never changes an existing order. `orders.subtotal` records the pre-fee sum;
  `total_amount` stays the single charged amount (= subtotal + fees, percent
  fees rounded to 2 dp). GET /ticket/order/:order_id exposes `subtotal` and
  `fees[]`, and the Order Summary panel renders the breakdown box — Subtotal
  (N items) plus each fee line — under TICKETS (Figma 32-1366). Fixed
  per-fee columns on orders (tax, service_fee) were rejected: the fee set is
  admin-editable data, so a new fee must not require a schema change.
- Q: What does the payment screen look like? → A: Per Figma 32-1366 /
  203-1157: a full-width Complete Purchase banner (clock icon, caption, and
  the mm : ss payment countdown) above a two-column layout — left, the Scan
  to Pay card (heading, "use any e-Wallet or Mobile Banking app supporting
  QRIS" caption, the QR image, and a TOTAL AMOUNT DUE box); right, the same
  Order Summary ticket-stub panel as registration, with a COLLAPSIBLE "How
  to pay with QRIS" section (collapsed by default, six numbered steps when
  opened) between the total and a disabled "Waiting for payment…" button.
  No page heading. The on-demand status check and self-update notice remain
  below the Scan to Pay card (FR-018), and the payment countdown lives only
  in the banner.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Order Is Real: Persist on T&C Agreement with 1-Hour Hold (Priority: P1)

A guest selects tickets and clicks "Buy ticket". The Terms & Conditions dialog
appears with the event's admin-authored terms. When the guest agrees, the system
creates an order in `PENDING` status with no payment link, records the guest's
agreement (what was agreed and when), deducts the ticket quota, and holds the
reservation for 1 hour. If the guest never completes the order, a background
process marks it `EXPIRED` after the hold lapses and restores the quota so
others can buy.

**Why this priority**: This is the transactional core of the entire feature.
Without a persisted order, holds, and expiry, every downstream screen (order
form, payment, email) has nothing real to operate on, and overselling or
permanently locked seats are possible.

**Independent Test**: Select tickets, agree to T&C, verify an order exists in
`PENDING` with a recorded agreement and 1-hour expiry and reduced quota; let the
hold lapse and verify the order flips to `EXPIRED` and quota is restored.

**Acceptance Scenarios**:

1. **Given** a guest has selected available tickets, **When** they agree to the
   T&C in the dialog, **Then** an order is created in `PENDING` status with no
   payment link, the agreement is recorded with a timestamp, quota is deducted,
   and the order's expiry is set 1 hour ahead.
2. **Given** a guest declines or dismisses the T&C dialog, **When** the dialog
   closes, **Then** no order is created and no quota is deducted.
3. **Given** a `PENDING` order whose 1-hour hold has lapsed without payment,
   **When** the background expiry process runs, **Then** the order status
   becomes `EXPIRED` and the held quota is restored.
4. **Given** an order that has just expired, **When** the guest returns to it,
   **Then** they see the expired state and are directed to start a new order.
5. **Given** the last remaining ticket is held by a `PENDING` order, **When**
   another guest tries to buy it, **Then** they are told the ticket is
   unavailable until (and unless) the hold expires.

---

### User Story 2 - Pay by QR with 14-Minute Window and Mid-Point Refresh (Priority: P1)

From the order page, the guest clicks "Continue to payment". The system requests
a QR from the payment gateway, shows it, and tightens the order's expiry to 14
minutes from that moment. At the 7-minute mark the QR is refreshed (a new QR is
obtained from the gateway). Payment status updates appear on the page without
the guest refreshing. If 14 minutes pass unpaid, the payment and order become
expired, quota is restored, and the guest must start over. The event countdown
remains visible on the payment screen through the shared event layout.

**Why this priority**: Payment is the revenue moment; the window, refresh, and
live status are what make the QR flow usable and prevent stale holds.

**Independent Test**: On an order with completed visitor forms, continue to
payment; verify a QR appears, expiry is now 14 minutes out, the QR visibly
refreshes at 7 minutes, a sandbox payment flips the page to success in real
time, and an unpaid order expires at 14 minutes with quota restored.

**Acceptance Scenarios**:

1. **Given** an order with completed forms, **When** the guest continues to
   payment, **Then** a payment QR is displayed and the order expiry is updated
   to 14 minutes from now.
2. **Given** the payment screen has been open 7 minutes unpaid, **When** the
   7-minute mark is reached, **Then** a fresh QR replaces the old one without
   the guest acting, and the 14-minute deadline is unchanged.
3. **Given** the guest completes payment via the QR, **When** the gateway
   confirms payment, **Then** the page updates to the paid state without a
   manual refresh and redirects to the finished-order page.
4. **Given** the 14-minute window lapses unpaid, **When** the deadline passes,
   **Then** the guest sees the expired-payment state, the order becomes
   `EXPIRED`, and quota is restored.
5. **Given** the guest is on the payment screen, **When** viewing the page,
   **Then** the event countdown from the shared event layout is still visible.
6. **Given** a payment confirmation arrives more than once for the same order,
   **When** duplicates are processed, **Then** the order is paid exactly once
   and no duplicate emails or tickets result.

---

### User Story 3 - Homepage Event Grid + Ticket Verification Aside (Priority: P2)

A guest lands on the homepage and sees a grid of active events (replacing the
current two-button "choose event / choose ticket" chooser). Alongside the grid
is a card where a ticket holder can verify/look up a ticket by its code without
picking an event first.

**Why this priority**: Entry point for discovery; valuable and independently
shippable, but purchase can be reached via direct links without it.

**Independent Test**: Load the homepage; verify events render as a grid, each
navigates to its event detail page, and the aside card performs a ticket lookup.

**Acceptance Scenarios**:

1. **Given** active events exist, **When** a guest opens the homepage, **Then**
   events are shown as a grid with key info (name, date, venue, banner).
2. **Given** the homepage is open, **When** the guest uses the verification
   card with a valid ticket code, **Then** the ticket's status is shown.
3. **Given** the old two-button chooser, **When** this feature ships, **Then**
   the chooser is gone and the grid is the default homepage.

---

### User Story 4 - CMS-Driven Event Detail Page (Priority: P2)

Clicking an event opens its detail page — not the ticket list. The page presents
the event's banner, schedule, venue, and rich content (description, activities,
guest stars, guidelines) authored by admins. A "Buy ticket" action leads to the
ticket selection page. Admins author the description and Terms & Conditions per
event in the CMS using a rich-text (WYSIWYG) editor.

**Why this priority**: Gives buyers context before purchase and gives admins
control of content; purchase works without it via direct ticket-page links.

**Independent Test**: As admin, author rich-text description and T&C for an
event; as guest, open the event from the grid and verify the authored content
renders and "Buy ticket" leads to ticket selection showing the authored T&C at
purchase time.

**Acceptance Scenarios**:

1. **Given** an event with authored content, **When** a guest clicks it on the
   grid, **Then** the event detail page shows the authored rich content, not
   the ticket list.
2. **Given** the event detail page, **When** the guest clicks "Buy ticket",
   **Then** they arrive at that event's ticket selection page.
3. **Given** an admin edits an event's description or T&C in the rich-text
   editor, **When** they save, **Then** the guest-facing page and T&C dialog
   reflect the change, with formatting preserved and unsafe markup neutralized.

---

### User Story 5 - Order Page: Visitor Forms per Ticket (Priority: P2)

After agreeing to the T&C, the guest is taken to the order page addressed by the
persisted order. They fill in buyer contact details and per-ticket visitor
details (name, email, phone, date of birth, gender). The order summary shows
subtotal, tax, service fee, and total. Only when the forms are valid can the
guest continue to payment.

**Why this priority**: Required to attribute tickets to attendees and to email
e-tickets; depends on Story 1's persisted order.

**Independent Test**: From a fresh `PENDING` order, verify the order page loads
by order identity, forms validate required fields, and completed forms unlock
"Continue to payment".

**Acceptance Scenarios**:

1. **Given** a newly created `PENDING` order, **When** the T&C dialog is
   submitted, **Then** the guest lands on the order page for that specific
   order (no hold countdown is shown — clarified 2026-08-05, Figma 12-4456).
2. **Given** incomplete or invalid visitor details, **When** the guest tries to
   continue, **Then** field-level errors are shown and payment is not started.
3. **Given** all forms valid, **When** the guest continues, **Then** visitor
   details are saved against the order's tickets and the payment step begins.
4. **Given** a guest revisits an order link before continuing to payment,
   **When** the order is `PENDING` and unexpired, **Then** the order summary
   and held tickets are intact and the visitor forms are presented empty
   (details are submitted only together with "Continue to Payment" —
   clarified 2026-08-05).

---

### User Story 6 - Success Page, Email Receipt + E-Ticket, Resend (Priority: P2)

After payment succeeds, the guest lands on the finished-order page summarizing
the purchase. An email is sent to the buyer containing the receipt and the
e-ticket(s) as an attachment. The guest can request the email again.

**Why this priority**: The ticket delivery moment — completes the promise of
the purchase; depends on Stories 1–2.

**Independent Test**: Complete a sandbox payment; verify the success page shows
the order summary, an email with receipt + e-ticket attachment arrives, and the
resend action delivers it again.

**Acceptance Scenarios**:

1. **Given** a paid order, **When** payment is confirmed, **Then** the guest is
   redirected to the finished-order page for that order.
2. **Given** a paid order, **When** confirmation is processed, **Then** exactly
   one email with receipt and e-ticket attachment is sent to the buyer, without
   delaying the payment confirmation itself.
3. **Given** the buyer didn't receive the email, **When** they use resend,
   **Then** the email is delivered again for that paid order only.
4. **Given** an unpaid or expired order, **When** resend is attempted, **Then**
   it is refused.

---

### Edge Cases

- Payment confirmed by the gateway at the same moment the expiry process runs:
  a confirmed payment that arrives before the order is marked `EXPIRED` wins;
  once `EXPIRED`, later confirmations are flagged for manual reconciliation
  rather than silently double-handled. Gateway-side QR validity is aligned to
  the 14-minute window to make this race rare.
- Two guests race for the last ticket: quota can never go negative; exactly one
  order succeeds, the other guest is told the ticket is unavailable.
- QR refresh at 7 minutes fails (gateway error): the existing QR remains shown
  with a retry; the 14-minute deadline does not extend.
- Guest closes the browser mid-payment and returns within the window: the
  payment page restores with the current QR and remaining time.
- Live status connection drops on the payment page: the page reconnects and/or
  falls back to polling so a completed payment is still detected.
- Email dispatch fails after payment: the order remains paid; delivery is
  retried and the guest can use resend. Email failure never blocks payment
  confirmation.
- Admin deactivates an event or ticket while `PENDING` orders exist: existing
  orders complete or expire normally; new purchases are prevented.
- Guest opens the T&C dialog when the event has no authored T&C: purchase is
  blocked with a clear message (terms must exist to be agreed to).
- Order link opened for someone else's / nonexistent order id: not-found state,
  no order details leak.
- Expiry process is down for a period: on recovery it sweeps all overdue
  `PENDING` orders; expiry is based on stored timestamps, not process uptime.

## Requirements *(mandatory)*

### Functional Requirements

**Homepage & discovery**

- **FR-001**: The homepage MUST display active events as a grid (name, date,
  venue, banner), replacing the current two-button chooser as the default view.
- **FR-002**: The homepage MUST include an aside card that verifies/looks up a
  ticket by its code without selecting an event first.

**Event detail (CMS content)**

- **FR-003**: Selecting an event MUST open its detail page (schedule, venue,
  banner, rich description, activities, guest stars, guidelines) — not the
  ticket list. The detail view MUST NOT load ticket or package availability
  data; that data is fetched separately, only when the guest proceeds to the
  ticket selection page.
- **FR-004**: Admins MUST be able to author each event's description and Terms
  & Conditions with a rich-text (WYSIWYG) editor in the CMS; guest-facing pages
  MUST render the authored formatting with unsafe markup neutralized.
- **FR-005**: The event detail page MUST provide a "Buy ticket" action leading
  to that event's ticket/package selection page.

**Order creation & hold**

- **FR-006**: Clicking "Buy ticket" on a selection MUST show the event's
  current Terms & Conditions in a dialog before any order is created.
- **FR-007**: On T&C agreement, the system MUST create the order in `PENDING`
  status with no payment link, atomically deduct quota for the selected
  tickets, and set the order to expire 1 hour later.
- **FR-008**: The system MUST record the T&C agreement durably with the order
  (agreement flag and time of agreement).
- **FR-009**: A background process MUST mark overdue `PENDING` orders as
  `EXPIRED` and restore their quota; expiry MUST derive from stored expiry
  timestamps so missed runs are swept on recovery. Each expired order MUST be
  logged (order identity + quota restored) so sweeps are auditable.
- **FR-010**: An expired order MUST show an expired state to the guest and
  require starting a new order; expired orders MUST NOT be payable or
  editable.

**Order page & visitor forms**

- **FR-011**: After T&C agreement, the guest MUST be redirected to the order
  page addressed by the persisted order's identity.
- **FR-012**: The order page MUST collect buyer contact details and per-ticket
  visitor details (name, email, phone, date of birth, gender) with validation,
  and MUST display subtotal, tax, service fee, and total.
- **FR-013**: The registration step MUST NOT render an order-hold countdown
  (Figma 12-4456, clarified 2026-08-05) — the shared header's event countdown
  is the only timer; the hold still expires server-side. Visitor details are
  submitted together with the continue-to-payment action; a revisit before
  payment starts shows the held order (summary, ticket slots) with empty
  forms.

**Payment**

- **FR-014**: "Continue to payment" MUST validate and save visitor details,
  obtain a payment QR from the payment gateway, and update the order's expiry
  to 14 minutes from that moment.
- **FR-015**: At 7 minutes on the payment screen, the system MUST obtain and
  display a fresh QR automatically; the 14-minute deadline MUST NOT extend.
- **FR-016**: Payment status changes MUST reach the payment page without manual
  refresh (live updates with automatic reconnection/fallback).
- **FR-017**: An unpaid order at the 14-minute deadline MUST become `EXPIRED`
  with quota restored, and the guest MUST see the expired-payment state.
- **FR-018**: Payment confirmation handling MUST be idempotent: duplicate
  confirmations MUST NOT double-pay an order, double-issue tickets, or
  double-send email.
- **FR-019**: The payment screen MUST keep the event countdown visible via the
  shared event layout (intentional deviation from the payment design frames).

**Completion & delivery**

- **FR-020**: Confirmed payment MUST redirect the guest to the finished-order
  page and record when the order was paid.
- **FR-021**: On payment confirmation, the system MUST send exactly one email
  to the buyer containing the receipt and e-ticket attachment(s), without
  delaying the confirmation response; delivery success MUST be tracked.
- **FR-022**: Guests MUST be able to request a resend of the receipt/e-ticket
  email for a paid order; resend MUST be refused for unpaid or expired orders
  and MUST be rate-limited against abuse.

**Integrity**

- **FR-023**: Ticket quota MUST never go negative; concurrent purchases of the
  last tickets MUST resolve to exactly one successful hold.
- **FR-024**: Order state transitions MUST be limited to: `PENDING` → `PAID`,
  `PENDING` → `EXPIRED`; no transitions out of `PAID` or `EXPIRED` via this
  flow.

### Key Entities

- **Event**: A ticketed happening — name, schedule, venue, scale, banner, rich
  description; owns its activities, guest stars, guidelines, tickets, packages,
  and Terms & Conditions.
- **Activity / Guest Star / Guideline**: Admin-managed content blocks displayed
  on the event detail page; each belongs to one event.
- **Terms & Conditions**: Per-event rich-text terms authored in the CMS; the
  version shown at purchase is what the guest agrees to.
- **Ticket**: A sellable admission type for an event — name, validity dates and
  times, price, remaining quota, display order.
- **Package**: A bundle grouping multiple tickets at a package price for an
  event (existing feature 005).
- **Order**: A guest's purchase — buyer email, monetary breakdown (subtotal,
  tax, service fee, total), T&C agreement record, expiry time, paid time,
  status (`PENDING`/`PAID`/`EXPIRED`).
- **Order Ticket (Attendee)**: One held ticket within an order carrying its
  visitor's details (name, email, phone, date of birth, gender); the unit an
  e-ticket is issued for.
- **Order Status**: Reference list of order states.
- **Payment**: The gateway interaction for an order — QR reference, its
  validity window, and confirmation outcome.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A guest can go from homepage to a confirmed, emailed ticket in
  under 10 minutes without creating an account.
- **SC-002**: 100% of orders that agree to T&C exist durably with a recorded
  agreement before any payment step begins.
- **SC-003**: Zero oversells: under concurrent load on the last available
  ticket, sold + held tickets never exceed quota.
- **SC-004**: 100% of orders unpaid at their deadline (1-hour hold or
  14-minute payment window) reach `EXPIRED` within 1 minute of the deadline,
  with quota restored.
- **SC-005**: Payment confirmation is reflected on the guest's screen within 5
  seconds of the gateway confirming, without a manual refresh.
- **SC-006**: 95% of receipt/e-ticket emails are dispatched within 2 minutes of
  payment confirmation; every paid buyer can obtain the email via resend.
- **SC-007**: Content authored in the CMS (event description, T&C) appears on
  guest-facing pages with formatting intact and no unsafe content executed.
- **SC-008**: Duplicate payment confirmations produce exactly one paid order,
  one set of tickets, and one automatic email in 100% of cases.

## Assumptions

- **Quota is held at order creation** (T&C agreement), not at payment — this is
  what "lock the place" means; expiry restores it. Quota is treated as the
  live remaining count.
- **1-hour hold, then 14-minute payment window**: continuing to payment
  replaces the remaining hold time with a fresh 14-minute deadline, even if
  more than 14 minutes of the original hold remained.
- **QR refresh at 7 minutes** replaces the QR once; the overall deadline never
  extends. Gateway-side QR validity is configured so a QR cannot be paid after
  the order's deadline.
- **Payment-vs-expiry race**: a gateway confirmation processed before the order
  is expired wins; after expiry, confirmations are surfaced for manual
  reconciliation (no automatic refund flow in this MVP — refunds are out of
  scope per the constitution).
- **The homepage verification card** reuses the existing top-level ticket
  lookup (feature 007 keeps lookup event-agnostic); this feature changes its
  placement, not its behavior.
- **Ticket selection page** builds on the existing event ticket page including
  package bundles (feature 005) and event-scoped layout/countdown (feature
  007).
- **T&C dialog** builds on feature 006; this feature adds CMS authorship,
  persistence of the agreement, and order creation on agreement.
- **Buyer email** is collected on the order page; the T&C step requires no
  personal data beyond the agreement itself.
- **One live T&C per event**: the currently authored terms are what guests see
  and agree to; no versioned history beyond the recorded agreement time.
- **Visitor loyalty points are out of scope**: the proposed `Visitor_Point`
  table is excluded — the constitution explicitly bars loyalty points from
  this MVP.
- **Proposed API routes and table schemas in the input are advisory**: final
  contracts and schema changes are decided at planning, must keep `SCHEMA.md`
  in sync, and must respect the existing constitution rules (atomic quota
  transactions, no external calls inside the checkout transaction, idempotent
  webhooks, gateway abstraction, DTO isolation).
- **Background expiry cadence**: the expiry sweep runs at least once per
  minute, matching SC-004.
- **Email attachments**: e-tickets are rendered as a PDF attachment containing
  one ticket (with scannable code) per attendee, per the constitution's
  delivery rules.

# Feature Specification: Guest Purchase Flow

**Feature Branch**: `001-guest-purchase-flow`

**Created**: 2026-07-31

**Status**: Draft

**Input**: User description: "Guest Purchase Flow: Guest (unauthenticated) users can browse published events, select quantities across multiple ticket types, fill in Buyer Info plus dynamic per-attendee Name & Email, and submit checkout. Checkout creates a unique Order Number, atomically deducts ticket type quota, and returns a Payment URL. A payment webhook updates order status Pending -> Paid/Cancelled/Expired; on Paid it generates one Ticket (unique code + QR) per Attendee and emails the buyer a PDF with all tickets; on cancel/expire it restores the deducted quota. Guests can also look up a ticket by code. No login/account creation required."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Browse events and start checkout (Priority: P1)

A prospective attendee browses the list of published events, opens one event's detail
page to see ticket types, pricing, and remaining availability, and decides which
ticket types and quantities to buy.

**Why this priority**: Without discovery, there is no purchase. This is the entry
point of the entire revenue flow.

**Independent Test**: Can be fully tested by requesting the published events list and
a single event's detail page and verifying accurate name, schedule, venue, ticket
types, prices, and available quota are shown, without requiring any account.

**Acceptance Scenarios**:

1. **Given** an event with status Published exists, **When** a guest requests the
   event list, **Then** the event appears with name, venue, schedule, and banner.
2. **Given** an event with status Draft or Completed, **When** a guest requests the
   event list, **Then** that event does not appear.
3. **Given** a published event's detail page, **When** a guest views it, **Then** all
   its ticket types are shown with price and remaining quota (sold-out types are
   visibly marked unavailable).
4. **Given** an event whose banner was configured by an admin as a plain image URL,
   **When** a guest views the list or detail page, **Then** that URL is returned
   as-is for the browser to load (the platform never hosts or uploads the image).

---

### User Story 2 - Complete checkout with dynamic attendee info (Priority: P1)

A guest selects quantities for one or more ticket types, provides Buyer Info (name,
email, phone), fills in one Name and Email per ticket purchased (dynamic attendee
form), and submits the order. The system reserves the tickets by deducting quota,
creates the order, and redirects the guest to a Payment URL to pay.

**Why this priority**: This is the core conversion step — without it, no order or
revenue exists. It must be reliable and safe even if many guests buy the same
scarce ticket type simultaneously.

**Independent Test**: Can be fully tested by submitting a checkout request with valid
buyer info, attendee info matching total quantity, and available quota, then
verifying an Order Number and Payment URL are returned, order/attendee records exist,
and quota decreased by the purchased quantity.

**Acceptance Scenarios**:

1. **Given** a ticket type with 5 remaining, **When** a guest checks out for 2 of
   that type with 2 matching attendee entries, **Then** the order is created, 2
   tickets' worth of quota is deducted (3 remaining), and a unique Order Number and
   Payment URL are returned.
2. **Given** a ticket type with 1 remaining, **When** two guests simultaneously check
   out for 1 each, **Then** exactly one checkout succeeds and the other is rejected
   with a clear "insufficient quota" error — quota never goes negative.
3. **Given** a checkout request where attendee entry count does not match the total
   ticket quantity, **When** submitted, **Then** the system rejects it with a
   validation error and creates no order.
4. **Given** a checkout request for a ticket type outside its sales window, **When**
   submitted, **Then** the system rejects it with a clear error and creates no order.
5. **Given** a checkout that passed validation and already reserved quota, **When**
   the payment provider cannot be reached or refuses to create the payment, **Then**
   the order is marked Cancelled, the reserved quota is released back to
   availability, and the guest receives a clear error inviting them to retry.

---

### User Story 3 - Receive tickets after payment (Priority: P1)

After the guest completes payment on the gateway, the system is notified, marks the
order Paid, generates one unique ticket (with scannable code) per attendee, and
emails the buyer a single PDF containing all their tickets.

**Why this priority**: Ticket delivery is the actual product the guest paid for;
without it the purchase has no value to the buyer.

**Independent Test**: Can be fully tested by simulating a "payment successful"
notification for a Pending order and verifying the order becomes Paid, one ticket per
attendee is created with a unique code, and the buyer receives one email with a PDF
attachment containing all tickets.

**Acceptance Scenarios**:

1. **Given** a Pending order that is fully paid, **When** the payment confirmation is
   received, **Then** order status becomes Paid, one ticket is generated per
   attendee, and one email with a PDF of all tickets is sent to the buyer's email.
2. **Given** an order already marked Paid, **When** a duplicate payment confirmation
   is received for the same order, **Then** the system acknowledges it without
   generating duplicate tickets or sending a duplicate email.
3. **Given** a Pending order, **When** the guest abandons payment and the gateway
   reports expiration or cancellation, **Then** order status becomes Expired/
   Cancelled and the previously deducted quota is restored for others to purchase.
4. **Given** a Pending order, **When** the gateway reports the payment was **denied**
   or **failed**, **Then** order status becomes Cancelled and the previously
   deducted quota is restored for others to purchase.
5. **Given** a Pending order, **When** the gateway reports the payment is still
   pending or held for fraud review, **Then** the order remains Pending, no tickets
   are generated, no email is sent, and quota is neither deducted again nor
   restored.

---

### User Story 4 - Look up a ticket by code (Priority: P3)

A guest who has a ticket code (e.g., from their email) can look it up to confirm its
details and current status.

**Why this priority**: Convenience/support feature; useful but not required for the
core purchase-and-deliver value to be realized.

**Independent Test**: Can be fully tested by looking up a known valid ticket code and
an unknown/invalid code, and verifying the correct ticket details or a not-found
result is returned respectively.

**Acceptance Scenarios**:

1. **Given** a ticket with a valid code exists, **When** a guest looks it up by that
   code, **Then** the ticket's event, attendee name, and status are returned — and
   nothing else; in particular the attendee's email address is never returned.
2. **Given** no ticket exists with the given code, **When** a guest looks it up,
   **Then** a clear not-found result is returned.
3. **Given** a client issuing lookups far faster than a human could, **When** it
   exceeds the configured per-client rate limit, **Then** further lookups are
   refused with a rate-limit error until the window resets.

---

### Edge Cases

- What happens when a guest submits checkout for a ticket type that has just sold
  out (race condition with another concurrent buyer)? System MUST reject the
  checkout without deducting quota and without creating a partial order.
- How does the system handle a payment notification for an order that does not
  exist or was already Cancelled/Expired? It MUST be safely ignored/rejected
  without side effects.
- What happens if attendee email delivery fails after tickets are generated? The
  order and tickets remain valid; delivery MUST be retryable (e.g., via a manual
  resend) without regenerating or duplicating tickets.
- What happens when a guest provides duplicate attendee emails within one order?
  Allowed — one physical person may be listed to receive info for another, or the
  buyer may enter the same email for multiple tickets intentionally.
- What happens when checkout total amount does not match the sum of selected ticket
  prices/quantities due to a client-side manipulation attempt? Server MUST
  recompute the total from current ticket type prices and reject any client-supplied
  total that doesn't match.
- What happens when quota was already reserved but the payment provider then fails
  to issue a payment URL? The system MUST compensate: mark the order Cancelled and
  return the reserved quota to availability before responding to the guest with an
  error (FR-021).
- What happens when the gateway reports a payment held for fraud review rather than
  a definitive outcome? The order MUST stay Pending and keep holding its reserved
  quota until a definitive notification arrives (see Payment Status Mapping).
- What happens if the process crashes after quota is reserved but before the payment
  URL is stored? The order stays Pending holding quota and no gateway notification
  will ever arrive (the gateway transaction was never created). This is an accepted
  MVP risk; see research.md "Checkout transaction shape" for the recommended
  post-MVP sweep.
- What happens when someone tries to enumerate ticket codes against the public
  lookup endpoint? The endpoint MUST be rate limited per client (FR-020) and MUST
  never disclose the attendee's email address (FR-016).

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST list only Published events to guests, with name, venue,
  address, schedule, banner, and description. The banner is a plain image URL string
  supplied by an admin; the system MUST NOT accept file uploads or host banner images
  in this MVP.
- **FR-002**: System MUST show a single published event's detail, including all its
  ticket types with name, price, and remaining quota (the stored quota value IS the
  remaining count — see FR-018).
- **FR-003**: System MUST allow a guest to submit a checkout containing: buyer name,
  email, phone; one or more (ticket type, quantity) selections; and one attendee
  entry (name + email) per unit of quantity purchased.
- **FR-004**: System MUST reject checkout when the number of attendee entries does
  not equal the total quantity requested.
- **FR-005**: System MUST reject checkout for any ticket type whose current time is
  outside its configured sales start/end window.
- **FR-006**: System MUST atomically verify and deduct available quota for every
  selected ticket type as part of creating the order, such that concurrent checkouts
  can never deduct more than the available quota (quota MUST NOT go negative).
- **FR-007**: System MUST create the order, all its associated line items and
  attendee records, and the quota deduction together in one atomic unit, such that a
  failure partway through leaves no partial order and no orphaned quota deduction.
- **FR-008**: System MUST generate a unique, guest-facing Order Number for every
  successful checkout.
- **FR-009**: System MUST compute the order total from authoritative, current ticket
  type prices, ignoring or validating away any client-supplied total.
- **FR-010**: System MUST initiate payment with a payment provider and return a
  Payment URL to the guest upon successful checkout.
- **FR-011**: System MUST receive asynchronous payment status notifications and map
  every notified provider status to an order status (or to an explicit no-op)
  according to the **Payment Status Mapping** table below — no notified status may be
  left unhandled.
- **FR-012**: System MUST treat a payment status notification for an order already
  in status Paid as a no-op (no duplicate ticket generation, no duplicate email),
  while still acknowledging the notification successfully.
- **FR-013**: When an order transitions to Paid, System MUST generate exactly one
  ticket per attendee on that order, each with a unique scannable code, and status
  Active. Only the code is persisted; the QR image is derived from it on demand
  (FR-022).
- **FR-014**: When an order transitions to Paid, System MUST send exactly one email
  to the buyer containing a single PDF with all tickets for that order.
- **FR-015**: When an order transitions to Cancelled or Expired — including via a
  denied or failed payment (FR-019) — System MUST restore the quota that was
  deducted for that order's ticket types, in the same atomic unit as the status
  change.
- **FR-016**: System MUST allow lookup of a single ticket by its unique code,
  returning ticket status (Active/Used/Revoked), the event name, and the attendee
  name, without requiring authentication. The response MUST NOT include the
  attendee's or buyer's email address, phone number, order number, or any other
  personal data beyond the attendee name.
- **FR-017**: System MUST NOT require guest account creation or login at any point
  in browsing, checkout, payment, or ticket lookup.
- **FR-018**: The quota value stored per ticket type IS the **remaining** quota, not
  a static original total. System MUST decrement it at checkout and increment it back
  on Cancelled/Expired (including denied/failed payments). No separate "sold" or
  "total" counter exists, so remaining availability MUST always be read directly from
  that value, and any consumer of it (including admin-facing features) MUST NOT treat
  it as an immutable capacity figure.
- **FR-019**: System MUST explicitly handle **denied** and **failed** payment
  notifications, not only cancel/expire: both MUST transition the order to Cancelled
  and restore its quota, exactly as a cancellation does.
- **FR-020**: The public ticket-lookup endpoint MUST be protected by basic per-client
  rate limiting so that ticket codes cannot be enumerated at speed by an
  unauthenticated caller; requests over the limit MUST be refused with a rate-limit
  error rather than served.
- **FR-021**: If payment initiation with the provider fails after quota has already
  been reserved for an order, System MUST compensate by setting that order to
  Cancelled and restoring the reserved quota before returning an error to the guest;
  no order may be left holding quota because of a failed payment initiation.
- **FR-022**: System MUST NOT persist a QR image or any QR image URL for a ticket in
  this MVP. The QR is rendered on demand from the ticket code whenever a ticket
  document is produced (initial delivery, later resends, and any future
  "download QR" feature). There is no object storage, static-file serving, or upload
  endpoint anywhere in the stack.

### Payment Status Mapping

Canonical mapping from a payment-provider notification (Midtrans SNAP naming) to the
resulting order status. This table is the single source of truth referenced by FR-011,
FR-012, FR-015, and FR-019; `contracts/api.md` and `research.md` mirror it.

| Provider status | Additional condition | Resulting order status | Quota effect |
|---|---|---|---|
| *(any)* | order is already Paid | unchanged (Paid) | none — acknowledge and stop (FR-012) |
| `settlement` | — | Paid | none (already deducted at checkout) |
| `capture` | `fraud_status = accept` | Paid | none (already deducted at checkout) |
| `capture` | `fraud_status = challenge` | unchanged (Pending) | none — await a definitive notification |
| `pending` | — | unchanged (Pending) | none — explicit no-op |
| `deny` | — | Cancelled | restore |
| `cancel` | — | Cancelled | restore |
| `expire` | — | Expired | restore |
| `failure` | — | Cancelled | restore |

Order status is constrained to exactly Pending / Paid / Cancelled / Expired, so
`deny` and `failure` both fold into **Cancelled** — there is deliberately no separate
"denied" or "failed" order status.

### Key Entities

- **Event**: A published happening guests can buy tickets for; has name, venue,
  address, schedule, description, a banner (an admin-supplied image URL string — no
  upload), and a status controlling visibility.
- **Ticket Type**: A purchasable category within an event; has name, price, a sales
  window (start/end), and a **remaining** quota — a live counter that is decremented
  at checkout and restored on cancel/expire/deny, never a static original total
  (FR-018).
- **Order**: A guest's purchase attempt; has a unique order number, buyer contact
  info, computed total amount, and a status (Pending, Paid, Cancelled, Expired).
- **Order Item**: A line within an order recording which ticket type and how many
  units were purchased at what price.
- **Attendee**: One named recipient of a single ticket within an order; has a name
  and email, tied to one ticket type.
- **Ticket**: Issued only after payment; one per attendee, with a unique scannable
  code and a status (Active, Used, Revoked). The QR is a rendering of that code
  produced on demand, not stored data (FR-022).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A guest can go from viewing an event to receiving a Payment URL in
  under 3 minutes for a typical multi-ticket-type purchase.
- **SC-002**: Under concurrent demand for a scarce ticket type, zero oversells occur
  (issued tickets never exceed configured quota).
- **SC-003**: 100% of orders that reach Paid status result in exactly one delivery
  email containing all purchased tickets, with no duplicates.
- **SC-004**: 100% of Cancelled/Expired orders — whether cancelled, expired, denied,
  failed, or compensated after a failed payment initiation — have their reserved
  quota returned to availability within moments of the status change, with no manual
  intervention.
- **SC-005**: A guest can retrieve ticket status by code with a result returned
  in under 5 seconds.
- **SC-006**: Every provider status listed in the Payment Status Mapping table
  resolves to its stated order status with no unhandled/ignored cases, verified by
  replaying one notification of each type.

## Assumptions

- Guests have a valid, reachable email address; ticket delivery is email-only for
  this feature (no SMS/push).
- One payment attempt is in flight per order at a time; the payment provider is the
  source of truth for payment outcome, communicated via an asynchronous notification.
- "Published" is the only event status visible to guests; Draft and Completed events
  are excluded from guest-facing browsing and detail views.
- Ticket price at time of purchase is locked into the order item; later changes to a
  ticket type's price do not retroactively affect existing orders.
- Currency and payment provider selection are fixed platform configuration, not a
  per-order guest choice, for this feature's scope.
- Event banners are referenced by an admin-supplied URL only. There is no file
  upload, object storage, or static asset hosting in this MVP; the same holds for
  ticket QR images, which are generated on the fly rather than stored (FR-022).
- Ticket quota is tracked as a single live "remaining" number per ticket type
  (FR-018); the MVP intentionally does not record an original capacity or a sold
  count, so "how many were originally on sale" is not answerable from stored data.
- Payment initiation with the provider happens immediately after the order and its
  quota reservation are committed, not inside that same commit, so that concurrent
  buyers of the same ticket type are not serialized behind an external network call
  (see research.md "Checkout transaction shape").

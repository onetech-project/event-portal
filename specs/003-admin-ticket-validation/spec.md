# Feature Specification: Admin Ticket Validation

**Feature Branch**: `003-admin-ticket-validation`

**Created**: 2026-07-31

**Status**: Draft

**Input**: User description: "Admin ticket validation: Admin validator accepts manual Ticket Code input as primary, or camera QR scan as secondary. Status lookup returns Valid, Already Used, Invalid. Admin can mark Valid tickets as Used. Admin can manually trigger 'Resend Ticket Email' for an order."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Validate a ticket at the door (Priority: P1)

At the event entrance, an authenticated admin enters a ticket code manually (or scans
its QR code) to check whether it is a genuine, unused ticket, and sees a clear
status: Valid, Already Used, or Invalid.

**Why this priority**: This is the entire purpose of the feature — controlling entry
by verifying tickets — and delivers value the moment a single ticket can be checked.

**Independent Test**: Can be fully tested by looking up a fresh unused ticket code
(expect Valid), a previously-used ticket code (expect Already Used), and a
nonexistent/malformed code (expect Invalid).

**Acceptance Scenarios**:

1. **Given** a ticket with status Active exists, **When** the admin looks it up by
   its code, **Then** the result is Valid and shows the attendee name and ticket
   type.
2. **Given** a ticket with status Used exists, **When** the admin looks it up by its
   code, **Then** the result is Already Used.
3. **Given** no ticket exists with the entered code, **When** the admin looks it up,
   **Then** the result is Invalid.
4. **Given** a ticket code is entered via manual text input, **When** submitted,
   **Then** the same lookup and result behavior applies as for a scanned code.
5. **Given** a ticket's QR code is scanned via camera, **When** the scan resolves to
   a code, **Then** the same lookup and result behavior applies as for manual entry.

---

### User Story 2 - Mark a valid ticket as used (Priority: P1)

After confirming a ticket is Valid, the admin marks it as Used to admit the attendee
and prevent that same ticket from being used again.

**Why this priority**: Validation without the ability to consume the ticket does not
actually prevent re-entry or duplicate use — this closes the loop that makes
validation meaningful.

**Independent Test**: Can be fully tested by marking a Valid ticket as Used, then
re-validating the same code and confirming it now returns Already Used.

**Acceptance Scenarios**:

1. **Given** a ticket currently Valid, **When** the admin marks it Used, **Then**
   its status changes to Used and the transition is recorded by that ticket's
   `updated_at` being set to the current timestamp as part of the same guarded
   update (SCHEMA.md is LOCKED and its `tickets` table has no `used_at` column;
   none may be added).
2. **Given** a ticket already Used, **When** the admin attempts to mark it Used
   again, **Then** the action is rejected, informing the admin it was already used.
3. **Given** a ticket with status Invalid/nonexistent, **When** the admin attempts
   to mark it Used, **Then** the action is rejected.

---

### User Story 3 - Resend a ticket email (Priority: P2)

An admin manually triggers re-sending the ticket delivery email for a given order,
for guests who report not receiving their original email.

**Why this priority**: Important support capability for guest satisfaction at/around
event time, but independent of and secondary to the core door-validation flow.

**Independent Test**: Can be fully tested by triggering resend on a Paid order and
verifying an email with the same PDF ticket set is sent again to the buyer.

**Acceptance Scenarios**:

1. **Given** a Paid order with generated tickets, **When** the admin triggers
   resend, **Then** an email with a PDF containing all of that order's tickets is
   sent to the buyer's email again, reusing the tickets' existing ticket codes
   unchanged, and the order is flagged as having had its ticket email delivered.
2. **Given** an order whose status is anything other than Paid — Pending,
   Cancelled, or Expired — and which therefore has no generated tickets, **When**
   the admin attempts resend, **Then** the action is rejected with a clear
   "order not paid" error.

---

### Edge Cases

- What happens when the same ticket code is scanned twice in quick succession before
  the admin marks it Used? Repeated lookups MUST be safe and side-effect-free
  (status changes only occur via the explicit "mark Used" action).
- What happens when a QR scan or manual entry produces a code with extra
  whitespace or lowercase letters versus the stored code? Lookup MUST normalize
  the *input* (trim + uppercase) and then match the stored code exactly, so
  equivalent codes match consistently without degrading lookup performance.
- What happens when a ticket's status is Revoked? Lookup MUST return a distinct
  Invalid-type result (not Valid), and marking it Used MUST be rejected.
- What happens when resend is triggered many times in quick succession for the same
  order? Each trigger MUST send an email; no automatic rate limit is required for
  this feature, but each send MUST be logged for traceability.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST allow an authenticated admin to look up a ticket by
  manually entering its code.
- **FR-002**: System MUST allow an authenticated admin to look up a ticket by
  scanning its QR code via camera, resolving to the same code-based lookup as manual
  entry.
- **FR-003**: System MUST return exactly one of three lookup results: Valid (ticket
  exists and is Active), Already Used (ticket exists and is Used), or Invalid
  (ticket does not exist or is Revoked).
- **FR-004**: A Valid lookup result MUST include the attendee name and ticket type
  so the admin can visually confirm identity/entitlement.
- **FR-005**: System MUST allow an authenticated admin to mark a currently Valid
  ticket as Used, recording the transition by setting that ticket's `updated_at`
  to the current timestamp as part of the same guarded status update. No `used_at`
  column exists in the locked schema and none may be introduced.
- **FR-006**: System MUST reject attempts to mark a ticket Used when its current
  status is not Active (already Used or Revoked/nonexistent).
- **FR-007**: System MUST allow an authenticated admin to trigger resending the
  ticket delivery email for a Paid order, re-rendering a PDF containing all of
  that order's tickets and sending it to the buyer's email. The tickets' existing
  ticket codes MUST be reused verbatim and MUST NOT be regenerated; each ticket's
  QR image is re-generated from its existing ticket code at render time.
- **FR-008**: System MUST reject a resend request for any order whose status is
  not Paid — that is, Pending, Cancelled, or Expired orders, none of which have
  generated tickets.
- **FR-009**: System MUST require admin authentication for all lookup, mark-used,
  and resend actions.
- **FR-010**: System MUST normalize the ticket code *input* (trim surrounding
  whitespace, uppercase) and then match it exactly against the stored
  `ticket_code`, so that manual entry and QR scan of the same physical ticket
  produce identical lookup results. Stored codes are already written in canonical
  form by the guest purchase flow, so the stored column MUST NOT be wrapped in a
  case-folding function during lookup (that would bypass `idx_tickets_ticket_code`
  and force a sequential scan on every door scan).
- **FR-011**: On successful resend delivery, System MUST set the order's
  `email_sent` flag to true, exactly as the initial post-payment delivery does.

### Deliberate Extension to PRD §1.5 (Mark-Used Endpoint)

PRD.md §1.4 mandates "Admin can mark `Valid` tickets as `Used`", but PRD.md §1.5's
LOCKED API list names only `POST /api/v1/admin/tickets/validate` for this feature
area and contains no endpoint for performing that state change. Delivering FR-005
therefore requires one addition to that list:
`POST /api/v1/admin/tickets/:code/use`.

This addition is deliberate and justified, not an accidental deviation:

- The mandated capability (PRD §1.4) cannot be delivered by the listed endpoints
  alone — some request must perform the `ACTIVE -> USED` transition.
- Folding the transition into `POST /api/v1/admin/tickets/validate` was rejected:
  lookup MUST remain side-effect-free so that repeated scans of the same ticket
  (a routine occurrence at a door) are safe and never consume a ticket the admin
  has not yet decided to admit. That safety property is required by this spec's
  Edge Cases and by SC-002.
- The addition is admin-only, JWT-gated, and adds no guest-facing surface, so it
  does not expand MVP scope in any other direction.

No other endpoint beyond PRD §1.5's list is introduced by this feature; validate
and resend both use their PRD-listed paths.

### Key Entities

- **Ticket**: The entity being validated; has a unique code, a status (Active,
  Used, Revoked), and links to its attendee and event/ticket type. Its stored QR
  image reference is left empty in this MVP — the QR image is generated on demand
  from the ticket code whenever one is needed.
- **Attendee**: The named person a ticket belongs to; shown to the admin during
  validation.
- **Order**: The purchase that produced one or more tickets; targeted by the resend
  action. Its status must be Paid for tickets to exist; it also carries the
  `email_sent` delivery flag that a successful resend sets to true (FR-011).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A door admin can resolve a ticket's status (Valid/Already
  Used/Invalid) in under 3 seconds from code entry or scan.
- **SC-002**: 100% of attempts to mark an already-Used or invalid ticket as Used are
  rejected, with zero duplicate entries admitted on the same ticket.
- **SC-003**: An admin can successfully resend a ticket email for a Paid order in
  under 15 seconds.
- **SC-004**: 100% of validation and resend actions performed without a valid,
  current admin login are rejected.

## Assumptions

- Camera QR scanning happens on an admin-operated device (e.g., laptop webcam or
  mobile browser camera); no dedicated hardware scanner integration is required.
- Ticket codes are written in canonical form by the guest purchase flow —
  uppercase, fixed length, drawn from an unambiguous character set that excludes
  `I`, `O`, `0`, and `1` — so normalizing the input alone is sufficient for an
  exact match.
- No object storage, static file serving, or upload endpoint exists anywhere in
  this stack, so the ticket's stored QR image reference (`tickets.qr_code_url`)
  stays empty in the MVP. Every QR image — for initial delivery, for resend, and
  for any future "download QR" capability — is generated on demand from the
  ticket code. Ticket codes are stable and are never regenerated; QR images are
  always regenerated.
- "Revoked" tickets (e.g., refunded/voided outside this MVP's normal flow) are
  treated as Invalid for lookup purposes, consistent with the ticket status values
  defined in the data model.
- No automatic rate-limiting or cooldown is required for repeated resend requests in
  this feature; abuse prevention is out of scope for the MVP.
- This feature operates on tickets/orders already created by the guest purchase
  flow and events/ticket types managed in the admin management feature; it does not
  duplicate their creation logic.

# Feature Specification: Admin Management

**Feature Branch**: `002-admin-management`

**Created**: 2026-07-31

**Status**: Draft

**Input**: User description: "Admin management: Admin login (JWT auth), CRUD events (name, slug, description, venue, address, start/end date, banner, status), CRUD ticket types per event (name, price, quota, sales start/end), view orders and attendees. Admin is prohibited from deleting an Event or Ticket Type that has at least one associated Order (must return 400 Bad Request)."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Admin authentication (Priority: P1)

An administrator logs in with their credentials to obtain access to the admin
management area, and stays authenticated for subsequent admin actions.

**Why this priority**: Every other admin capability depends on being able to
authenticate; without it there is no gated admin area.

**Independent Test**: Can be fully tested by submitting valid and invalid credentials
and verifying a valid login yields access to admin actions while an invalid one does
not.

**Acceptance Scenarios**:

1. **Given** an existing admin account, **When** the admin submits correct
   email/password, **Then** they receive an access credential and can perform admin
   actions.
2. **Given** an existing admin account, **When** the admin submits an incorrect
   password, **Then** login is rejected and no access credential is issued.
3. **Given** no active login, **When** an admin action is attempted, **Then** it is
   rejected as unauthorized.

---

### User Story 2 - Manage events (Priority: P1)

An authenticated admin creates, views, updates, and deletes events, including
setting each event's status (Draft, Published, Completed) to control guest
visibility.

**Why this priority**: Events are the root entity of the whole ticketing catalog —
nothing else (ticket types, orders) can exist meaningfully without them.

**Independent Test**: Can be fully tested by creating an event, editing its fields,
listing it, and deleting it (when it has no orders), verifying each operation's
effect and that a unique slug is enforced.

**Acceptance Scenarios**:

1. **Given** an admin is authenticated, **When** they create an event with name,
   slug, description, venue, address, start/end date, and status, **Then** the event
   is created and retrievable.
2. **Given** an existing event, **When** the admin submits a slug that is already
   used by another event, **Then** creation/update is rejected with a clear error.
3. **Given** an existing event whose ticket types are not referenced by any order
   line or attendee record, **When** the admin deletes it, **Then** the event and
   all of its ticket types are removed together in a single all-or-nothing
   operation and the event is no longer retrievable.
4. **Given** an existing event with at least one of its ticket types referenced by
   an order line or an attendee record, **When** the admin attempts to delete it,
   **Then** the deletion is rejected with a 400-level error and the event and all
   of its ticket types remain intact.
5. **Given** an existing event, **When** the admin updates its status to Published,
   **Then** it becomes visible to guests; setting it to Draft or Completed removes
   it from guest visibility.

---

### User Story 3 - Manage ticket types (Priority: P1)

An authenticated admin creates, views, updates, and deletes ticket types within an
event, each with its own price, remaining quota, and sales window.

**Why this priority**: Ticket types define what guests can actually buy; without
them, an event has nothing purchasable.

**Independent Test**: Can be fully tested by creating multiple ticket types under one
event, editing remaining quota/price/sales window, and deleting one with no orders,
verifying correctness and that deletion is blocked once an order or attendee
references it.

**Acceptance Scenarios**:

1. **Given** an existing event, **When** the admin creates a ticket type with name,
   price, remaining quota, and sales start/end, **Then** it is created under that
   event and guests can see it once the event is Published.
2. **Given** an existing ticket type with no orders, **When** the admin updates its
   price, remaining quota, or sales window, **Then** the changes are saved and
   reflected immediately.
3. **Given** a ticket type that was opened with 50 remaining and has since sold 10
   (so it now shows 40 remaining and a Sold count of 10), **When** the admin sets
   the remaining quota field to 25, **Then** the stored remaining quota becomes
   exactly 25 — the value is an absolute set of what is still available, never an
   original total from which sales are subtracted again — and the Sold count is
   unchanged.
4. **Given** any ticket type, **When** the admin opens its create/edit form,
   **Then** the quota field is labelled "Sisa Kuota / Remaining Quota" (never
   "Total"), and a read-only "Sold" count (total quantity ordered for that ticket
   type) is displayed next to it for context.
5. **Given** an existing ticket type not referenced by any order line or attendee
   record, **When** the admin deletes it, **Then** it is removed.
6. **Given** an existing ticket type referenced by at least one order line or
   attendee record, **When** the admin attempts to delete it, **Then** the deletion
   is rejected with a 400-level error and the ticket type remains.

---

### User Story 4 - View orders and attendees (Priority: P2)

An authenticated admin views the list of orders (with status, buyer info, totals) and
the list of attendees, to monitor sales and support guests.

**Why this priority**: Visibility into sales/orders is important for running the
event but is a read-only support capability, not required for the catalog or
purchase flow itself to function.

**Independent Test**: Can be fully tested by placing sample orders and verifying the
admin can list/filter them and see associated attendee details.

**Acceptance Scenarios**:

1. **Given** orders exist across multiple events and statuses, **When** the admin
   requests the order list, **Then** each order's number, buyer, status, and total
   are shown.
2. **Given** an order with multiple attendees, **When** the admin views attendees,
   **Then** each attendee's name, email, and associated ticket type are shown.

---

### Edge Cases

- What happens when an admin tries to set an event's end date before its start
  date, or a ticket type's sales end before sales start? System MUST reject with a
  validation error.
- What happens when an admin tries to delete an event that has ticket types but no
  orders or attendees on any of them? Deletion MUST succeed, but it MUST remove
  that event's ticket types first and the event second, inside one all-or-nothing
  transaction. An event can never be removed while any of its ticket types still
  exists, so "delete the event only" is never a valid outcome.
- What happens when an admin tries to delete an event or ticket type that is
  referenced by an attendee record but no order line (or vice versa)? Deletion MUST
  be rejected with the same 400-level error — both kinds of reference block
  deletion, not just order lines.
- What happens when a guest checkout commits between the moment an admin reads a
  ticket type's remaining quota and the moment the admin saves a new value? The
  admin's absolute set wins and silently absorbs that sale. This is an accepted MVP
  risk: admins are advised not to edit quota while sales are active. No locking or
  versioning mechanism is added.
- What happens when an admin's session/token expires mid-action? The action MUST be
  rejected as unauthorized, requiring re-login.
- What happens when two admins edit the same event concurrently? Last write wins;
  no conflict-resolution UI is required for this feature.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: System MUST authenticate admins via email and password, issuing a
  time-limited access credential upon success.
- **FR-002**: System MUST reject any admin management action when no valid access
  credential is presented.
- **FR-003**: System MUST allow authenticated admins to create, read, update, list,
  and delete events with fields: name, slug, description, venue, address, start
  date, end date, banner, and status (Draft, Published, Completed).
- **FR-004**: System MUST enforce that every event's slug is unique.
- **FR-005**: System MUST reject event creation/update where end date precedes
  start date.
- **FR-006**: System MUST reject deleting an event when any of its ticket types is
  referenced by at least one order line **or** at least one attendee record,
  returning a 400-level error and leaving the event and all of its ticket types
  intact.
- **FR-007**: System MUST allow authenticated admins to create, read, update, list,
  and delete ticket types within an event, with fields: name, price, remaining
  quota, sales start, and sales end.
- **FR-008**: System MUST reject ticket type creation/update where sales end
  precedes sales start.
- **FR-009**: System MUST reject deleting a ticket type that is referenced by at
  least one order line **or** at least one attendee record, returning a 400-level
  error and leaving the ticket type intact.
- **FR-010**: System MUST allow authenticated admins to list orders with their
  status, buyer info, and total amount.
- **FR-011**: System MUST allow authenticated admins to list attendees with their
  name, email, and associated ticket type.
- **FR-012**: Only events with status Published MUST be visible to guests; Draft and
  Completed events MUST remain visible to admins regardless of status.
- **FR-013**: When an event is deletable (FR-006 satisfied), the system MUST remove
  that event's ticket types and the event itself in a single all-or-nothing
  operation — ticket types first, then the event — so that a successful delete
  leaves no orphaned ticket types and a rejected delete changes nothing.
- **FR-014**: The ticket type quota an admin reads and writes MUST be the
  **remaining** (still-available) quantity, not an original total. The admin form
  field MUST be labelled "Sisa Kuota / Remaining Quota" and MUST NOT be labelled
  "Total". A submitted value replaces the remaining quantity absolutely; the system
  MUST NOT subtract past sales from it again.
- **FR-015**: The system MUST display, alongside the remaining quota field, a
  read-only "Sold" count per ticket type equal to the total ordered quantity for
  that ticket type. This value is derived, MUST NOT be editable, and exists only to
  give the admin context before an absolute quota set.

### Key Entities

- **Admin**: A management-area user identified by email, with credentials used to
  authenticate.
- **Event**: Managed catalog entry with name, slug, description, venue, address,
  schedule, banner, and status; owns zero or more ticket types. An event cannot be
  removed while it still owns ticket types (see FR-013).
- **Ticket Type**: Managed purchasable category within an event, with price,
  **remaining** quota, and sales window; referenced by zero or more order lines and
  zero or more attendee records. Its quota is live inventory shared with the guest
  purchase flow, which decrements it on checkout and restores it on
  cancellation/expiry — this feature reads and sets that same remaining value.
- **Order**: A guest purchase record (read-only in this feature) used to determine
  deletion eligibility of events/ticket types, to derive the read-only Sold count,
  and for admin visibility.
- **Attendee**: A named ticket recipient within an order (read-only in this
  feature), viewable by admins; an attendee reference to a ticket type also blocks
  deletion of that ticket type and of its event.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An admin can create a new event with at least one ticket type and have
  it visible to guests in under 5 minutes.
- **SC-002**: 100% of attempts to delete an event or ticket type referenced by an
  existing order line or attendee record are blocked, with zero data loss
  incidents.
- **SC-003**: Admins can locate any order or attendee record from the admin list
  views in under 30 seconds.
- **SC-004**: 100% of admin actions performed without a valid, current login are
  rejected.

## Assumptions

- Admin accounts are provisioned out-of-band (e.g., directly in the database or by a
  separate bootstrap process); self-service admin signup is out of scope.
- There is a single class of admin with full access to all management actions; no
  role-based permission tiers are required for this feature.
- "Banner" is a single image reference per event; multi-image galleries are out of
  scope.
- The banner is a plain URL string typed or pasted in by the admin and stored as-is.
  There is **no file upload** in the MVP: the stack contains no object-storage or
  file-hosting service, so the system never accepts, stores, resizes, or serves
  image binaries. Admins host the image elsewhere and supply the link.
- Order and Attendee management in this feature is read-only; creating or mutating
  orders is covered by the guest purchase flow, and ticket validation actions are
  covered by a separate feature.
- The ticket type quota is a single live "remaining" counter shared with the guest
  purchase flow. Because an admin's edit sets that counter absolutely, a sale that
  commits between the admin's read and write is absorbed by the admin's value. This
  is accepted for the MVP (admins are advised not to edit quota during active
  sales); no optimistic-locking or version column is introduced, since the schema
  is locked.

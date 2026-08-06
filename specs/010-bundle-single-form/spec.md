# Feature Specification: Single Visitor Form per Bundle

**Feature Branch**: `010-bundle-single-form`

**Created**: 2026-08-05

**Status**: Draft

**Input**: User description: "currently the order page the bundle have 2 or more form based on the how many tickets are there, on the ui user only needs to fill a single bundle form, the data on the backend will be the same per ticket. so if in a bundle have 2 tickets, then the user form only single form, the 2 ticket data will have the same user information. and the ordered ticket will be 2 (the qr ticket for validation)"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One Form per Bundle (Priority: P1)

A guest books an order containing one bundle unit that includes 2 tickets. On the order page's registration step, instead of filling two identical visitor forms (one per ticket), the guest sees the buyer block plus a single visitor form for the bundle, titled with the bundle's name (not a constituent ticket's name) and showing how many tickets it covers. After submitting, every ticket in the bundle carries that one visitor's information, and after payment the guest still receives 2 distinct QR tickets for entry validation.

**Why this priority**: This is the core of the feature — it removes the redundant data entry that exists today for every bundle purchase and directly shortens checkout.

**Independent Test**: Book an order for one bundle unit containing 2 tickets, open the order page, fill the single bundle form, complete checkout and payment, and confirm 2 distinct validatable tickets exist with identical visitor information.

**Acceptance Scenarios**:

1. **Given** a held order for one bundle unit containing 2 tickets, **When** the guest opens the order page's registration step, **Then** exactly one visitor form is shown for the bundle (plus the buyer information block), titled with the bundle's name and showing its ticket count.
2. **Given** the guest fills the single bundle form with valid visitor details, **When** checkout succeeds, **Then** both of the bundle's tickets store identical visitor information (name, email, phone, date of birth, gender).
3. **Given** the order is paid, **When** tickets are issued, **Then** the guest receives 2 tickets with distinct codes/QR passes, each independently validatable at the gate.

---

### User Story 2 - Mixed Order of Bundle and Standalone Tickets (Priority: P2)

A guest books an order containing one bundle unit (2 tickets) and one standalone ticket. The registration step shows two visitor forms: one for the bundle and one for the standalone ticket. Standalone tickets keep today's one-form-per-ticket behavior.

**Why this priority**: Mixed orders are a common real purchase shape; the collapse must apply only to bundle tickets without disturbing standalone ticket registration.

**Independent Test**: Book an order combining a bundle and a standalone ticket, and confirm the registration step shows exactly one form for the bundle and one for the standalone ticket, each feeding the correct tickets.

**Acceptance Scenarios**:

1. **Given** a held order with one bundle unit (2 tickets) and 1 standalone ticket, **When** the guest opens the registration step, **Then** exactly 2 visitor forms are shown — one labeled for the bundle, one for the standalone ticket.
2. **Given** both forms are filled with different visitor details, **When** checkout succeeds, **Then** the bundle's 2 tickets share the bundle form's information and the standalone ticket carries its own form's information.

---

### User Story 3 - Multiple Units of the Same Bundle (Priority: P3)

A guest books 2 units of the same bundle (each containing 2 tickets, 4 tickets total). The registration step shows one form per bundle unit — 2 forms — so each unit can belong to a different visitor. Each unit's tickets share that unit's information, and 4 QR tickets are issued.

**Why this priority**: Buying the same bundle for several people is a natural extension; without per-unit forms, one person's details would be forced onto every ticket of that bundle line.

**Independent Test**: Book 2 units of a 2-ticket bundle, fill each unit's form with a different visitor, and confirm each unit's 2 tickets carry that unit's visitor information and 4 distinct tickets are issued.

**Acceptance Scenarios**:

1. **Given** a held order for 2 units of the same 2-ticket bundle, **When** the guest opens the registration step, **Then** exactly 2 visitor forms are shown, one per bundle unit.
2. **Given** each form is filled with a different visitor, **When** checkout succeeds, **Then** each unit's 2 tickets carry that unit's visitor information, and 4 distinct QR tickets are issued after payment.

---

### Edge Cases

- An order containing only standalone tickets behaves exactly as today: one visitor form per ticket.
- A bundle whose composition totals exactly 1 ticket shows one form — indistinguishable from today's behavior.
- When the server rejects a field (e.g., invalid date of birth) for a bundle's visitor data, the error is presented on that bundle's single form, on the correct field.
- Two different bundle units filled with identical visitor details are accepted — there is no uniqueness rule on visitor information.
- A bundle spanning different ticket types (e.g., a Day-1 pass plus a Day-2 pass) still gets one form; all constituent tickets share the same visitor information.
- A guest at the gate presenting two tickets with the same visitor name is normal for bundles; each ticket is validated by its own code and marked used independently.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The order page's registration step MUST display exactly one visitor form per bundle unit in the order, regardless of how many tickets that unit contains.
- **FR-002**: Non-bundle (standalone) tickets MUST continue to require one visitor form per ticket.
- **FR-003**: On successful submission, every ticket belonging to a bundle unit MUST store visitor information identical to that unit's single form: name, email, phone, date of birth, and gender.
- **FR-004**: The number of issued tickets/QR passes MUST remain equal to the order's total ticket count — a bundle unit containing N tickets still yields N individually validatable passes.
- **FR-005**: Each bundle form MUST show the bundle's title as its heading — not the title of any constituent ticket type — together with the number of tickets it covers, so the guest understands that one form registers multiple passes.
- **FR-006**: When a guest purchases multiple units of the same bundle, each unit MUST get its own visitor form, allowing a different visitor per unit.
- **FR-007**: Validation errors for a bundle's visitor data MUST be presented on that bundle's single form, and completing checkout MUST require every bundle form and every standalone form to be valid.
- **FR-008**: The buyer information block MUST remain unchanged and separate from visitor forms.
- **FR-009**: Ticket validation (marking a ticket as used) MUST continue to operate per individual ticket, even when several tickets share identical visitor data.

> **Supersedes**: This feature reverses the "one form per pass" decision recorded in spec 005 (`specs/005-ticket-package-bundles`, clarification and FR-028–FR-030) for bundle tickets. The pass-count invariant of that spec (FR-031: a bundle unit yields one pass per constituent ticket) is retained unchanged by FR-004.

### Key Entities

- **Bundle (Package)**: A sellable unit composed of one or more tickets, possibly across different ticket types.
- **Bundle Unit**: One purchased instance of a bundle. In this feature, a bundle unit owns exactly one visitor identity, entered through its single form.
- **Order Ticket (Attendee)**: The per-ticket record created when an order is booked. Its count per order is unchanged by this feature — only how its visitor data is collected changes (shared per bundle unit instead of entered per ticket).
- **Ticket (QR Pass)**: The validatable pass issued one-per-order-ticket after payment; unchanged by this feature.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A guest ordering one 2-ticket bundle completes visitor registration by filling exactly 1 visitor form instead of 2 — for any bundle, the form count per unit drops from N (its ticket count) to 1.
- **SC-002**: 100% of tickets within a bundle unit carry identical visitor information after checkout.
- **SC-003**: For every paid order, the number of issued tickets equals the number of ordered tickets — no regression from today's behavior.
- **SC-004**: Every issued ticket remains individually scannable and can be marked used exactly once.
- **SC-005**: The number of fields a guest must fill for a bundle order drops proportionally to its ticket count (e.g., a 2-ticket bundle requires half the visitor fields it does today), measurably shortening registration time.

## Assumptions

- One bundle unit belongs to one visitor: duplicating that visitor's information across all of the unit's tickets — even across different ticket types or event days — is the intended behavior.
- The set of tickets created at booking time is unchanged; this feature only changes how visitor data is collected and recorded onto those tickets.
- Duplicated visitor information is acceptable downstream: the ticket email/PDF shows the same visitor name on each of a unit's tickets, and gate validation relies on the per-ticket code, not on visitor identity.
- Whether the duplication of visitor data happens on the client or the server is an implementation decision deferred to planning.
- The buyer information block and its fields are out of scope and remain as they are today.

# Feature Specification: Order Page Buyer Information — Per-Ticket Holder Forms

**Feature Branch**: `feat/buyer` (spec directory `011-order-buyer-info`)

**Created**: 2026-08-06

**Status**: Draft

**Input**: User description: "Implement the buyer information page (order page): one ticket holder form per purchased ticket (one per bundle unit), with no separate buyer contact form — e.g., 1 bundle + 2 different standalone tickets shows exactly 3 forms; an information banner at the top of the page about email delivery; per-field validation (full name required and not whitespace-only, valid email format, phone digits-only 10–12, gender Male/Female, date of birth DD/MM/YYYY not in the future); an order summary card on the right (Booking ID, event name and date, per-ticket lines with quantity, unit price, subtotal; payment method fixed to QRIS; ticket total, tax & service fee, grand total); a Continue to Payment button that is disabled until every form is valid and, on click, saves all participant data, creates the Booking ID if not yet created, and redirects to the payment page; after payment, the receipt and each ticket's QR are sent to each form's email — not just one."

## Clarifications

### Session 2026-08-06

- Q: How should the attendee's gender reference the genders master table — a `gender_id` identifier reference, or a foreign key on the name? → A: A `gender_id` foreign key to the genders master list; the gender NAME remains the value exchanged with clients (mapped server-side); existing rows backfilled by name; the old free-text column and its fixed Male/Female constraint are removed. (The identifier was specified here as a UUID; the 2026-08-07 schema revision below replaces the master list's identifier with a small auto-incrementing integer and the reference follows it. The direction of the reference and the name-on-the-wire rule are unchanged.)
- Q: What happens to `orders.buyer_gender` and `orders.buyer_dob`, the write-only primary-contact snapshot fields? → A: Drop both; the primary-contact snapshot narrows to name, email, and phone — exactly what payment, admin, and delivery-fallback consume.
- Q: Where should the email-delivery notice appear on the order page? → A: Not as a separate page-top banner — the FIRST ticket holder form always carries the notice at the top right of its header (per the Figma order-page design, node 206-1804): "The invoice and e-ticket will be sent via email". No other form carries it.
- Q: Should the form fields keep the compact default size? → A: No — every form field (text inputs, the date field, and the gender select) renders at the taller comfortable size shown in the design, with a uniform height across all field types.
- Q: How is Date of Birth entered? → A: As a DD/MM/YYYY text field — separators auto-inserted while the guest types digits — with no calendar picker control; a value that is not a real, non-future calendar date is rejected with an inline error.
- Q: Does the form step's summary itemize fees? → A: No — on the form-filling step the summary shows only the grand Total Payment, labeled as including all taxes and fees; the itemized Ticket Total / per-fee rows appear on the awaiting-payment step's summary and in the receipt email (constitution v2.1.0).
- Q: The progress rail marks Registration as finished and Payment as current while the guest is still filling the forms — where should the QR payment screen live so the rail can tell the two apart? → A: On its own route, `/events/{slug}/orders/{orderNumber}/checkout` (a sibling of the existing `/done`); the holder forms keep `/events/{slug}/orders/{orderNumber}` and the rail derives its stage from the route, so forms = Registration and the QR screen = Payment.
- Q: What happens when a guest opens the wrong one of the two order pages for their order's state? → A: Forward silently and without adding a history entry — forms-with-payment-started forwards to the payment route, payment-route-before-checkout forwards back to the forms, PAID forwards to `/done` (as today), and Continue to Payment navigates the same way, so the back button never bounces between two mutually forwarding pages.
- Q: How should the first form show the guest that it is the buyer's information? → A: No new label is needed — the delivery notice already on the first card ("The invoice and e-ticket will be sent via email") is what marks it; form 1 is simultaneously ticket holder 1 and the buyer, and no separate buyer form exists.
- Q: After payment, should the buyer's email receive anything the other ticket holders don't? → A: **The buyer's address is the only recipient.** Exactly one email is sent, to form 1's address, carrying the invoice/receipt and every ticket in the order. The other holders' addresses receive nothing. This reverses the per-holder delivery recorded earlier in this same session (constitution amendment required).

### Session 2026-08-07

- Q: The phone field accepted letters until blur. Should it keep doing so? → A: No — the field MUST refuse non-digit characters as they are typed, keystroke by keystroke, instead of accepting them and complaining on blur. `inputMode="numeric"` and `type="tel"` are keyboard hints, not filters, and never blocked anything on a physical keyboard.
- Q: When the guest types a local number like `08123456789` into the free-text phone box, what value is stored in the database and sent to the payment gateway? → A: **Exactly what was typed.** No normalization: the field is free text with no hardcoded country code, the guest may enter the number in either `62…` or `0…` form, and that choice is preserved verbatim through storage and on to the gateway. This reverses the normalize-to-`+62` direction taken earlier in this same session; no `+` is ever stored, because the field accepts digits only.
- Q: Must the typed number start with `0` or `62`, or is any 10-15 digit string accepted? → A: Any 10-15 digits. The rule is length alone — no prefix requirement, so a bare subscriber number or a foreign number passes. This keeps the validator a single length check on both sides of the wire and keeps the field genuinely free text.
- Q: Could Date of Birth use a native date control with its calendar button hidden by styling instead of the typed DD/MM/YYYY field? → A: No — the 2026-08-06 decision stands unchanged. A native date control displays its segments in the browser's locale order, so DD/MM/YYYY cannot be guaranteed; its picker button can only be hidden on some browsers, and the picker still opens by click and keyboard even when hidden. The typed field remains the requirement (FR-003, FR-007). Recorded because the alternative was considered and rejected, not merely overlooked.
- Q: The requested schema revision says "Packages, Tickets: status ACTIVE|INACTIVE → `is_active` boolean" — but the issued-ticket table's status is ACTIVE|USED|REVOKED and ticket types carry no status at all. Which table was meant? → A: **Packages only.** The package's two-state status becomes an `is_active` boolean. The issued ticket keeps its three-state ACTIVE|USED|REVOKED lifecycle untouched — the gate's irreversible ACTIVE→USED transition and the "already used" scan response depend on telling those states apart, which a boolean cannot do — and ticket types gain no status field.
- Q: Should the unclosable expired modal appear on both order screens — the ticket holder forms and the QRIS payment screen — or only on the forms screen? → A: **Both.** The ended-journey message stops replacing the page body on either screen; each keeps its own layout (progress rail, forms or QR panel, summary) and raises the same blocking dialog over it. Expiry is a state of the order, not of the screen, so it must not look like two different things depending on where the guest was sitting.
- Q: What exactly makes the expired modal "unclosable" — which of the usual ways out of a dialog should be removed? → A: **All of them.** No close (X) control, Escape does nothing, a backdrop click does nothing, and the page behind is inert — neither focusable nor scrollable — under a dimmed overlay. The modal's own action button is the only in-page exit; ordinary browser navigation (back button, address bar) is never blocked. A dead-end order has no state to return to, so an incidental dismissal would only strand the guest on a page they can no longer submit. The modal is redesigned to the Figma dialog at node `293-3`: a centered card on a dimmed backdrop, alert icon in a tinted circle, the "Time's Up" heading, the body copy, and a single full-width filled button.
- Q: The Figma dialog shows only a "Return to Home Page" button, yet its body copy tells the guest "Please repeat your order" — should the modal keep a "Repeat Order" action too, or is the single button the whole exit? → A: **The single button is the whole exit.** The design is followed literally: one filled "Return to Home Page" button, and the Repeat Order link is dropped. Because no repeat action is offered, the body copy MUST NOT instruct the guest to repeat their order — it says what happened and that the seats went back on sale, and nothing more.
- Q: On the QR screen the payment countdown hits zero a few seconds before the server marks the order EXPIRED — should the modal appear the instant the countdown reaches zero, or only once the server confirms? → A: **The instant the countdown reaches zero.** The countdown reaching zero is exactly what the modal announces, so it opens then, the QR stops being shown, and the server's EXPIRED status arriving moments later changes nothing on screen. The inline "This payment code has expired" notice that used to fill that gap is removed — one expiry produces one message, not a notice followed by a dialog.
- Q: Do the two master lists keep `name` NOT NULL and unique and `is_active` NOT NULL, or adopt the looser supplied DDL? → A: **Keep the guarantees, take the wider columns.** Both master lists keep a mandatory, unique name (order statuses at 256 characters, genders at 100) and a mandatory active flag defaulting to true. The looser supplied form was rejected because both the gender the form submits and the status name on the wire are resolved to their row by name — a duplicate or absent name leaves that lookup with no single answer, and a null active flag makes "is this option selectable?" unanswerable.
- Q: Once an order references its status by numeric identifier instead of by name, does the status name or the number travel on the wire? → A: **The name, exactly as today.** `PENDING`/`PAID`/`CANCELLED`/`EXPIRED` remain what the guest order response, the admin order views, the checkout status stream, and the payment path exchange and display; the numeric identifier is storage-only and mapped name↔id server-side. No client-visible change, consistent with the constitution's rule that the wire contract stays decoupled from the storage schema.
- Q: The supplied DDL adds mandatory "created by" and optional "updated by" audit fields that exist on no table today — should they be added, and who is recorded in them? → A: **Yes, on the two master lists only, recording the acting admin's identifier.** Rows the migration seeds (the four order statuses, the two genders) record the reserved literal `SYSTEM`, since no admin creates them; later admin edits record that admin's identifier. The audit fields are not extended to any other table in this change.
- Q: When a package's status becomes an active/inactive flag, does the admin interface exchange a true/false flag or keep the `ACTIVE`/`INACTIVE` words? → A: **The true/false flag, on the wire as well as in storage.** The admin API sends and receives an `is_active` boolean and the admin form becomes a toggle; the `ACTIVE`/`INACTIVE` strings leave the contract entirely. This is deliberately unlike the order status, which keeps its name on the wire — the order status names come from a master list that must round-trip, whereas a package's flag has only two states and no master list behind it, so a boolean says everything the strings did.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - One Form per Ticket Holder, No Separate Buyer Form (Priority: P1)

A guest who has booked tickets lands on the order page and sees exactly one ticket holder form for every pass that needs an identity: one form per standalone ticket (counting quantity) and one form per bundle unit — and no separate "buyer contact" block anywhere on the page. The person filling the topmost form is understood to be the order's primary contact.

| Order contents | Forms shown |
| --- | --- |
| Day 1 × 1 | 1 |
| Day 1 × 10 | 10 |
| Day 1 × 2 + Day 2 × 3 | 5 |
| 1 bundle unit + 2 different standalone tickets | 3 |

**Why this priority**: This is the core correction the feature exists for — today the page shows a separate buyer contact form on top of the holder forms, forcing redundant data entry and creating a single "buyer" identity the business no longer wants. Every other story renders on top of this page structure.

**Independent Test**: Book an order combining a bundle and standalone tickets, open the order page, and count the forms: exactly one per standalone ticket plus one per bundle unit, with no buyer contact block present.

**Acceptance Scenarios**:

1. **Given** a held order of Day 1 × 2 and Day 2 × 3, **When** the guest opens the order page, **Then** exactly 5 ticket holder forms are shown and no separate buyer contact form appears.
2. **Given** a held order of 1 bundle unit and 2 different standalone tickets, **When** the guest opens the order page, **Then** exactly 3 forms are shown — one titled for the bundle, one per standalone ticket.
3. **Given** any held order, **When** the order page renders, **Then** each form collects Full Name, Email, Phone Number, Gender (Male/Female), and Date of Birth, and every field is marked mandatory.

---

### User Story 2 - Validation Gates Continue to Payment (Priority: P2)

While filling the forms, the guest gets an inline error message on any field that violates its rule (empty or whitespace-only name, malformed email, phone that is too short or too long, unselected gender, missing or future date of birth). The Continue to Payment button starts disabled and only becomes active once every field of every form passes validation. Clicking it saves all participant data, ensures a Booking ID exists (creating it if it has not been created yet), and takes the guest to the payment page.

**Why this priority**: Without gating, invalid or incomplete holder data reaches payment and ticket issuance, producing undeliverable e-tickets; this story protects everything downstream.

**Independent Test**: On an order page with 2 forms, enter invalid values field by field and observe per-field errors with the button disabled; correct all fields and observe the button enable; click it and land on the payment page with the data saved.

**Acceptance Scenarios**:

1. **Given** any form field is empty, **When** the guest reviews the page, **Then** Continue to Payment is disabled.
2. **Given** an email like `user@` or `gmail.com`, **When** the field is validated, **Then** an error message is shown on that field and the button stays disabled; `user@email.com` passes.
3. **Given** a phone number with fewer than 10 digits or more than 15 digits, **When** the field is validated, **Then** an error message is shown on that field. Letters and symbols cannot reach the field at all, so they never produce one.
4. **Given** a full name consisting only of spaces, **When** the field is validated, **Then** an error message is shown.
5. **Given** an unselected gender or an empty date of birth, **When** the guest attempts to proceed, **Then** an error message is shown on the respective field.
6. **Given** a date of birth later than today, **When** the field is validated, **Then** it is rejected as a future date.
7. **Given** every field of every form is valid, **When** the guest reviews the page, **Then** Continue to Payment is active.
8. **Given** the button is active, **When** the guest clicks it, **Then** all participant data is saved, a Booking ID exists (created now if it did not already), and the guest is redirected to the payment page.

---

### User Story 3 - Email Delivery Notice and Delivery to the Buyer (Priority: P2)

The first ticket holder form is the buyer's own information as well as ticket holder 1's; it carries a notice at the top right of its header telling the buyer that the invoice and e-ticket will be sent via email. After successful payment, exactly one email goes to that first form's address, carrying the invoice/receipt and every ticket in the order. The other holders' addresses receive nothing — they are collected as ticket-holder identity, not as delivery addresses.

**Why this priority**: The buyer is the person who paid and the one accountable for distributing the passes; concentrating delivery on their address is what the notice on their form promises, and it keeps other holders' inboxes out of the transaction entirely.

**Independent Test**: Complete a paid order with 3 forms holding 3 distinct email addresses and verify that only form 1's address receives mail, that its single email contains the receipt and all three tickets, and that the other two addresses receive nothing.

**Acceptance Scenarios**:

1. **Given** the order page loads, **When** the guest views it, **Then** the first ticket holder form shows a notice at the top right of its header stating that the invoice and e-ticket will be sent via email; no other form and no separate page-level banner carries the notice.
2. **Given** a paid order with 3 forms and 3 distinct emails, **When** tickets are issued, **Then** form 1's address receives exactly one email containing the order receipt and all three ticket QR passes, and the other two addresses receive nothing.
3. **Given** a paid order containing a bundle unit, **When** tickets are issued, **Then** every pass of that unit is in the buyer's single email alongside the rest of the order's passes.
4. **Given** a paid order whose second form repeats the buyer's own email address, **When** tickets are issued, **Then** still exactly one email is sent — the repeat is not a second delivery.

---

### User Story 4 - Order Summary Card (Priority: P3)

While filling the forms, the guest can review the transaction in a summary card on the right side of the page. It shows the Booking ID, event name, and event date; one line per purchased ticket type or bundle with its name, quantity, unit price, and line subtotal; a payment method section fixed to QRIS (visible but not changeable); and the grand Total Payment, labeled as including all taxes and fees. The itemized fee breakdown (ticket total, tax & service fee, grand total) appears on the awaiting-payment step, not while the forms are being filled.

**Why this priority**: The summary builds purchase confidence and transparency but the transaction can complete correctly without it; it depends on the page from Story 1 existing.

**Independent Test**: Open the order page for a known order and compare every summary value (lines, quantities, unit prices, subtotals, fees, grand total) against the order's actual contents and pricing.

**Acceptance Scenarios**:

1. **Given** the order page on a desktop-width screen, **When** it renders, **Then** the summary card appears on the right side showing Booking ID, event name, and event date.
2. **Given** an order with Day 1 × 2 at Rp2.600.000, **When** the summary renders, **Then** that line shows the ticket name, quantity 2, unit price Rp2.600.000, and subtotal Rp5.200.000.
3. **Given** the payment method section, **When** the guest interacts with it, **Then** QRIS is shown as the selected method and cannot be changed to anything else.
4. **Given** a ticket total of Rp5.200.000 and tax & service fee of Rp10.000, **When** the form step's summary renders, **Then** it shows only Total Payment Rp5.210.000 (labeled as including taxes and fees) with no itemized fee rows; **When** the awaiting-payment summary renders, **Then** it itemizes Ticket Total Rp5.200.000, the fee Rp10.000, and Total Payment Rp5.210.000, which always equals ticket total plus tax & service fee.

---

### User Story 5 - Progress Rail Matches the Step the Guest Is On (Priority: P2)

The progress rail above every purchase screen shows four stages: Booking, Registration, Payment, Done. While the guest fills the ticket holder forms, the rail marks Registration as the current stage — not as already finished with Payment underway. To make that possible, the QR payment screen moves to its own address instead of sharing one with the forms, so each stage has exactly one address of its own.

**Why this priority**: The rail currently tells the guest they have finished a step they are still working on and are paying when they have not yet been shown a QR code — it misreports progress on the single screen where the guest is doing the most work. It is corrected at the same priority as the validation gate because both concern the same screen's trustworthiness.

**Independent Test**: Walk the flow from ticket selection to confirmation and, at each screen, read the rail: exactly one stage is current, it names the screen actually on display, everything before it is ticked, and nothing after it is.

**Acceptance Scenarios**:

1. **Given** the guest is filling the ticket holder forms, **When** the rail renders, **Then** Registration is the current stage, Booking is complete, and Payment and Done are not reached.
2. **Given** the guest has passed Continue to Payment and sees the QR code, **When** the rail renders, **Then** Payment is the current stage and Registration is complete; the address is the payment screen's own, distinct from the forms' address.
3. **Given** the guest is on the confirmation screen, **When** the rail renders, **Then** Done is the current stage and the three before it are complete.
4. **Given** an order whose payment has already started, **When** the guest opens the forms address (e.g. from a bookmark or the back button), **Then** they are forwarded to the payment address, and pressing Back does not return them to a screen that forwards forward again.
5. **Given** an order whose payment has not started, **When** the guest opens the payment address directly, **Then** they are forwarded back to the forms address.

---

### User Story 6 - Time's Up Arrives as a Modal, Not a New Screen (Priority: P2)

When an order runs out of time or is cancelled, the guest keeps the screen they were on — the progress rail, their filled-in forms or the payment panel, and the order summary all stay where they were — and a "Time's Up" dialog opens over them, dimming the page behind it. The dialog cannot be dismissed: there is no close control, Escape and backdrop clicks do nothing, and the page behind cannot be scrolled or typed into. Its single button returns the guest to the home page.

**Why this priority**: Today the screen is swapped wholesale for a full-page card, so the guest loses every bit of context about what they were doing at the moment they most need it — which order, which step, what they had filled in. It is corrected at the priority of the other same-screen trust fixes.

**Independent Test**: Let an order expire with the forms half-filled, and again while the QR is on screen; on each, confirm the page behind is unchanged and still visible, the dialog is up, Escape / backdrop / scrolling do nothing, and the one button leads home.

**Acceptance Scenarios**:

1. **Given** a guest filling the holder forms, **When** the order becomes EXPIRED, **Then** the forms, rail, and summary remain rendered and dimmed behind a "Time's Up" dialog; the page is not replaced by a full-page card.
2. **Given** a guest on the QR screen, **When** the payment countdown reaches zero, **Then** the same dialog opens immediately, the QR is no longer shown, and the server's later EXPIRED status changes nothing on screen.
3. **Given** the dialog is open, **When** the guest presses Escape, clicks the backdrop, or scrolls, **Then** it stays open and the page behind neither scrolls nor takes focus; no close control is present anywhere on it.
4. **Given** the dialog is open, **When** the guest reads it, **Then** it shows exactly one button, "Return to Home Page", and its body copy does not tell them to repeat their order.
5. **Given** a CANCELLED order opened at either order address, **When** the screen renders, **Then** that address's own screen renders with the same dialog over it, carrying the cancellation wording.

---

### Edge Cases

- An order of a single bundle unit shows exactly one form; that form's holder is the primary contact.
- Ten or more forms (e.g., Day 1 × 10) all render and remain individually fillable; the summary card remains reachable while scrolling the forms.
- Two forms filled with the same email address are accepted; delivery is unaffected, since only the buyer's address is ever written to.
- A phone number of exactly 10 or exactly 15 digits is valid (boundaries inclusive); leading zeros are preserved, since the value is a digit string and not an arithmetic number.
- The same number entered as `08123456789` and as `628123456789` produces two different stored values, and both are accepted — the form the guest chose is the form that is kept.
- A number pasted with separators (`+62 812-3456-789`) keeps only its digits (`628123456789`); the `+` is dropped along with the spaces and dashes, because the value is digits only.
- Typing letters or punctuation into the phone field produces nothing at all — the characters never appear, so there is no error message to clear. Typing past 15 digits is ignored rather than rejected.
- A date of birth equal to today is accepted (only future dates are rejected).
- Pasting a value that includes surrounding whitespace into Full Name is not rejected if real characters remain; a value of only whitespace is rejected.
- The guest fixes one invalid field among many: the button stays disabled until the last invalid field across all forms is corrected, and each error clears individually.
- The order's hold expires while the guest is still typing: the forms, the rail, and the summary stay exactly where they were and the end-of-journey modal opens over them (FR-022). What the guest typed is neither cleared nor submitted — it is simply behind an inert overlay, and the only offered way on is the modal's button.
- A mistyped email discovered after payment: the existing resend mechanism applies; resend delivers to the buyer's saved address — the same single recipient as the original send.
- A mistyped BUYER email is therefore the one address that matters for delivery; the notice on form 1 exists to make the guest check exactly that field.
- An order reached through the wrong event's address is still refused outright at the payment address, exactly as at the forms address — the state-based forwarding of FR-021 never overrides that refusal.
- An expired or cancelled order opened at either address is not forwarded onward: the address's own screen renders and the end-of-journey modal opens over it (FR-022).
- The payment countdown reaches zero while the guest watches: the modal opens at that moment, the QR is no longer shown behind it, and the server's own EXPIRED status arriving seconds later changes nothing on screen — one event, one message (FR-022).
- The guest presses Escape, clicks the dimmed backdrop, or tries to scroll the page while the modal is up: nothing happens on all three counts. The browser's own back button still works — the modal blocks the page, not the browser.
- A cancelled order shows the same modal with the cancellation wording rather than the expiry wording; the single button and the unclosable behavior are identical.
- A gender is deactivated in the master list while ticket holders already reference it: existing holders keep their recorded gender and it still displays by name; the option simply stops appearing in new forms. Deactivating never rewrites or orphans a stored reference.
- An attempt to add a master list entry whose name matches an existing one (in either list) is refused rather than creating a second entry the name lookup could resolve to either way.
- An order status entry that orders still reference cannot be deleted; the active flag is how a status is retired, so no order is ever left pointing at nothing.
- The revision runs on a database that already holds orders, holders and packages: every existing order keeps its state, every holder keeps the same gender name, and every package keeps the same availability — the identifiers beneath them change, the values above them do not.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The order page MUST NOT display a separate buyer contact form; ticket holder forms are the only identity forms on the page.
- **FR-002**: The page MUST display exactly one ticket holder form per standalone ticket purchased (counting quantity — Day 1 × 10 yields 10 forms) and exactly one form per bundle unit, regardless of how many passes the bundle unit contains (1 bundle unit + 2 standalone tickets yields 3 forms).
- **FR-003**: Each ticket holder form MUST collect exactly these mandatory fields: Full Name (text), Email (email), Phone Number (digits), Gender (single choice from the gender master list — currently Male / Female), Date of Birth (typed as DD/MM/YYYY text with auto-inserted separators — no calendar picker control).
- **FR-004**: Full Name MUST be rejected when empty or consisting only of whitespace.
- **FR-005**: Email MUST be rejected unless it is a valid email format (e.g., `user@email.com` passes; `user@` and `gmail.com` fail).
- **FR-006**: Phone Number MUST be 10 to 15 digits inclusive, and nothing but digits. The field is free text with no country code supplied by the interface: the guest enters the whole number themselves, in either international (`628123456789`) or local (`08123456789`) form, and that choice MUST be preserved verbatim — the value is stored and exchanged exactly as typed, with no normalization between the two forms and no `+` added. Validation is length alone; no particular prefix is required. The field MUST refuse non-digit characters as they are typed rather than accepting them and rejecting the value later, so letters, spaces, dashes and `+` never enter the value in the first place.
- **FR-007**: Gender MUST be explicitly selected; Date of Birth MUST be provided and MUST NOT be a future date.
- **FR-008**: Every validation failure MUST surface an error message on the specific field that failed, on the specific form it belongs to.
- **FR-009**: The Continue to Payment button MUST be disabled by default and MUST become active only when every field of every form passes validation; it MUST return to disabled if any field becomes invalid again.
- **FR-010**: Clicking the active Continue to Payment button MUST save all participants' data, MUST ensure a Booking ID exists for the order (creating one only if it does not already exist), and MUST then direct the guest to the payment page at its own address (FR-020), without leaving the forms page as a back-button stop.
- **FR-011**: The first ticket holder form MUST always display an information notice at the top right of its header stating that the invoice and e-ticket will be sent via email; the notice MUST NOT appear on any other form, and there is no separate page-level banner.
- **FR-012**: After successful payment, exactly ONE email MUST be sent, to the buyer's address — the email on form 1 (clarified 2026-08-06, superseding this session's earlier per-holder rule). It MUST carry the order receipt and every ticket QR pass in the order. No email is sent to the other ticket holders' addresses; those are collected as holder identity, not as delivery addresses. The order is marked as delivered only once that email is sent successfully.
- **FR-013**: The order summary card MUST be positioned on the right side of the page (alongside the forms on wide screens) and MUST show the Booking ID, event name, and event date.
- **FR-014**: The summary MUST list one line per purchased ticket type or bundle showing its name, quantity, price per unit, and line subtotal (quantity × unit price).
- **FR-015**: The summary MUST show a payment method section with QRIS as the default and only option; the section is visible but MUST NOT be changeable.
- **FR-016**: On the form-filling step, the summary MUST show only the Grand Total Payment, labeled as including all taxes and fees — no itemized fee rows (clarified 2026-08-06; constitution v2.1.0). The itemized breakdown of Ticket Total, Tax & Service Fee, and Grand Total Payment — where Grand Total equals Ticket Total plus Tax & Service Fee — MUST appear on the awaiting-payment step's summary and in the receipt email.
- **FR-017**: The topmost form's holder IS the buyer, and MUST serve as the order's primary contact wherever a single contact for the order is needed — payment records, admin order views, order-level communication, and the sole delivery recipient of FR-012; the contact details retained on the order are the holder's name, email, and phone only.
- **FR-018**: Each stored ticket holder gender MUST reference an entry of the gender master list (referential integrity enforced by the data store), replacing the previous fixed Male/Female storage constraint; the selectable options remain governed solely by the master list, while the gender's display name stays the value exchanged with the ordering screens.
- **FR-019**: All form fields (text inputs, the date field, and the gender select) MUST render at the taller comfortable field size shown in the design, with a uniform height across every field type.
- **FR-020**: The QR payment screen MUST occupy its own address, `/events/{slug}/orders/{orderNumber}/checkout`, distinct from the ticket-holder forms at `/events/{slug}/orders/{orderNumber}`. Each of the four progress stages MUST therefore correspond to exactly one address: ticket selection → Booking, holder forms → Registration, QR payment → Payment, confirmation → Done. The progress rail MUST mark the stage matching the current address as current, every earlier stage as complete, and every later stage as not reached — in particular, Registration MUST NOT be shown as complete while the holder forms are still being filled.
- **FR-021**: A guest who opens the address that does not match their order's state MUST be forwarded to the one that does, without adding a back-button stop: the forms address forwards to the payment address once payment has started, the payment address forwards back to the forms address before checkout, and a paid order forwards to the confirmation address from either. Consequently the back button MUST NOT bounce the guest between two addresses that forward to each other.
- **FR-022**: An order that has ended without a purchase (EXPIRED or CANCELLED) MUST NOT replace the screen's contents with the end-of-journey card. Both order screens — the holder forms at the forms address and the QR screen at the payment address — MUST keep rendering their own layout (progress rail, forms or payment panel, order summary) and MUST present the end-of-journey message as a blocking modal dialog layered over that layout. The same modal MUST be used on both screens, so the guest sees one presentation of the ended order regardless of which screen they were on. On the QR screen the modal MUST open the moment the payment countdown reaches zero, without waiting for the server to report the order EXPIRED, and the QR code MUST NOT remain on screen behind it; the server's status change arriving moments later MUST leave the screen unchanged, so one expiry produces one message rather than an inline notice followed by a modal. The separate inline "This payment code has expired" notice is therefore removed.
- **FR-023**: The end-of-journey modal MUST be unclosable: it MUST NOT offer a close (X) control, MUST NOT dismiss on Escape, and MUST NOT dismiss on a backdrop click. While it is open the page behind MUST be inert — not reachable by pointer or keyboard focus and not scrollable — beneath a dimmed backdrop. The modal's own action button is the only exit offered inside the page; ordinary browser navigation (back button, address bar) is never blocked. Once raised, the modal MUST NOT disappear on its own while the order remains in its ended state.
- **FR-024**: The modal MUST follow the dialog design at Figma node `293-3`: a centered card on a dimmed backdrop, an alert icon inside a tinted circle at the top, the heading, the body copy beneath it, and exactly ONE full-width filled action button labelled "Return to Home Page" leading to the site home. The previous "Repeat Order" link to the event's ticket selection MUST be removed, and because no repeat action is offered the body copy MUST NOT tell the guest to repeat their order — it states that the payment window ran out (or that the order was cancelled) and that the seats went back on sale.

> **Schema revision (clarified 2026-08-07) — scope note**: FR-025 through FR-029 are a storage revision requested during this feature's clarification. Only FR-025 and FR-027 touch this feature's own surface (they govern the gender master list that FR-003 and FR-018 depend on); the order-status and package requirements are carried here because they were decided here, and they change no guest-visible behaviour. If the team prefers, they can be split into their own spec without altering any decision recorded above.

- **FR-025**: The order-status and gender master lists MUST identify their entries by a compact auto-assigned number rather than a randomly generated identifier. Existing entries MUST keep their names and active state across the change, and every record referencing them — including each ticket holder's stored gender (FR-018) — MUST be re-pointed at the new identifier so no reference is lost or silently re-bound to a different entry.
- **FR-026**: An order MUST record its status as a reference to an entry of the order-status master list rather than by storing the status word itself. The status NAME MUST remain the value every interface exchanges and displays — guest order responses, admin order views, the checkout status stream, and payment records — so the change is invisible outside storage.
- **FR-027**: Every master list entry MUST carry a name that is mandatory and unique within its list, and an active flag that is mandatory and defaults to active. A blank or duplicated name MUST be refused, because both the gender a form submits and the order status carried on the wire are resolved to their entry by name, and a duplicate leaves that resolution with no single answer.
- **FR-028**: Each order-status and gender entry MUST record who created it and, once edited, who last updated it, alongside its existing created and updated timestamps. Entries seeded by the system (the four order statuses and the two genders) MUST record the reserved author `SYSTEM`; entries an admin creates or edits MUST record that admin's identifier. No other table gains these fields in this change.
- **FR-029**: A package MUST express its availability as a two-state active flag rather than an `ACTIVE`/`INACTIVE` word, in storage and on the wire alike: the admin interface MUST exchange it as a true/false value and present it as a toggle, and the `ACTIVE`/`INACTIVE` strings MUST NOT survive anywhere in the contract. Issued tickets are explicitly excluded — their ACTIVE/USED/REVOKED lifecycle MUST remain a three-state status, because the gate must tell a pass already scanned apart from one revoked, which a two-state flag cannot express.

> **Completes**: FR-020 and FR-021 finish what spec 007 (`specs/007-event-scoped-routes`, FR-004/FR-005) required but could not deliver: that spec mandated a rail stage derived from the address and never contradicting it, while leaving the holder forms and the QR screen sharing one address — so one of the two stages had to be misreported. Giving the payment screen its own address makes the stage-per-address rule satisfiable, and spec 007's SC-002 becomes achievable on all four screens.

> **Supersedes**: This feature removes the separate buyer information block that spec 010 (`specs/010-bundle-single-form`, FR-008) explicitly preserved, and replaces the buyer-contact collection of spec 008 (`specs/008-e2e-purchase-flow`, FR-012) with collection through form 1, which is both ticket holder 1 and the buyer. Delivery (FR-012 here) ends up where the constitution had it before this feature — exactly one email to the buyer containing all tickets — after a same-session detour through per-holder delivery; constitution v3.0.0 restores it.

### Key Entities

- **Ticket Holder (Attendee)**: The person a single form describes — full name, email, phone number, gender, date of birth. Owns one standalone ticket or all passes of one bundle unit. Gender is stored as a reference to a Gender Master List entry, not free text. The email identifies the holder; it is not a delivery address unless the holder is also the buyer (FR-012).
- **Gender Master List**: The admin-governed list of selectable genders (currently Male/Female). Sole source of both the form's options and the stored holder gender references. Each entry has a compact auto-assigned number as its identifier, a mandatory unique name, a mandatory active flag, and a record of who created and last updated it (FR-025, FR-027, FR-028).
- **Order Status Master List**: The admin-governed list of order states (seeded PENDING, PAID, CANCELLED, EXPIRED), with the same identifier, name, active-flag, and authorship shape as the Gender Master List. An order points at one of its entries; the entry's NAME is what every screen and interface shows (FR-026).
- **Order (Booking)**: The held purchase being completed; identified by a Booking ID shown in the summary and carrying the event's name and date. Its state is a reference to an Order Status Master List entry, exchanged and displayed by name.
- **Package (Bundle Offer)**: A purchasable bundle of ticket types. Its availability is a two-state active flag, exchanged with the admin interface as a true/false value (FR-029) — distinct from an issued ticket, whose ACTIVE/USED/REVOKED lifecycle stays a three-state status.
- **Order Line**: One purchased ticket type or bundle within the order — name, quantity, unit price, and derived subtotal.
- **Bundle Unit**: One purchased instance of a bundle; collects exactly one holder form covering all of the unit's passes (per spec 010).
- **Fees**: The tax & service amount added to the ticket total to produce the grand total.
- **Buyer (Primary Contact)**: The holder described by the topmost form — ticket holder 1 and the buyer at once. Stands in wherever the order needs a single contact, and is the only address the order's tickets and receipt are emailed to. Retained on the order as name, email, and phone only.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For 100% of orders, the number of forms shown equals (standalone ticket quantity) + (number of bundle units), and zero buyer contact forms appear — verified for at least the shapes 1 ticket, 10 tickets, 2+3 tickets, and 1 bundle + 2 tickets.
- **SC-002**: Zero orders reach the payment page with a missing or invalid holder field; every invalid field shows an error on that field before the guest can proceed.
- **SC-003**: For 100% of paid orders, exactly one email is sent and it goes to the buyer's address (form 1), carrying the receipt and every pass in the order; zero emails reach any other holder's address.
- **SC-004**: A guest completes the order page for a 3-ticket order (typing all fields) in under 3 minutes.
- **SC-005**: In 100% of rendered summaries, each line subtotal equals quantity × unit price and the grand total equals ticket total + tax & service fee; the itemized ticket-total and fee rows appear only on the awaiting-payment summary and in the receipt, never on the form-filling step.
- **SC-006**: On each of the four purchase screens, the progress rail marks exactly one stage as current and it is the stage that screen represents — verified on all four, with zero screens showing a later stage as current or an unfinished stage as complete.
- **SC-007**: The storage revision changes nothing a guest can observe: after it, 100% of order responses, admin order views, and checkout status frames still carry the status by name (`PENDING`/`PAID`/`CANCELLED`/`EXPIRED`), and every ticket holder's gender resolves to the same gender name it had before — zero holders end up pointing at a different entry, and zero orders lose their state.
- **SC-008**: After the revision, zero occurrences of the `ACTIVE`/`INACTIVE` package strings remain in the admin contract, and every issued ticket still reports one of ACTIVE, USED, or REVOKED — a pass already scanned remains distinguishable from a revoked one in 100% of gate validations.
- **SC-009**: 100% of entries in both master lists have a non-blank name that is unique within its list and a recorded author — the six system-seeded entries attributed to `SYSTEM`, every admin-created or admin-edited entry attributed to that admin — and an attempt to save a duplicate or blank name is refused in 100% of cases.
- **SC-010**: On both order screens, an order that ends without a purchase leaves the screen's own layout rendered in 100% of cases and shows the end-of-journey dialog over it; across Escape, backdrop click, and scroll attempts the dialog is dismissed zero times, and exactly one action button is present on it.

## Assumptions

- The guest reaches the order page only after booking, so an order (and its Booking ID) normally already exists; FR-010's "create if not yet created" is a safety net, not a new entry path.
- The original requirement's page-top banner copy ("sent to the email on the topmost form") predates the correction note. Resolved 2026-08-06: the notice is the design's chip on the first form's header top right, reading "The invoice and e-ticket will be sent via email" (FR-011) — and the final delivery rule matches that copy literally: one email, to the first form's address (FR-012).
- The topmost form doubles as the order's primary contact, replacing the data the removed buyer form used to provide; no other buyer-specific data is collected. Only the contact's name, email, and phone are retained at the order level — the holder's date of birth and gender live solely on the ticket holder record (clarified 2026-08-06).
- **Delivery history, recorded so the reversal is not re-litigated**: the original input asked for per-holder delivery ("not just one"), and this session first specified and shipped it. The 2026-08-06 clarification reversed it to a single email to the buyer. The other holders' email addresses are still collected and stored per FR-003 — they identify the holder and remain available if delivery is ever widened again — but nothing is sent to them.
- Duplicate email addresses across forms are allowed; no uniqueness rule applies to holder data.
- Bundle units keep the one-form-per-unit behavior of spec 010; this feature does not change how many passes a bundle produces.
- Tax & service fee amounts come from the existing fee configuration of the purchase flow (spec 008); this feature only displays them.
- QRIS remains the only payment method in this MVP; the fixed payment-method display simply reflects that.
- On narrow (mobile) screens the summary card may stack with the forms instead of sitting to the right; "right side" applies to desktop-width layouts.
- Date of birth carries no minimum-age rule; any non-future date is acceptable.
- The date of birth field is a masked DD/MM/YYYY text input (clarified 2026-08-06, superseding the earlier native-date-control deviation): the guest types digits, separators appear automatically, no calendar picker is shown, and impossible dates (e.g. 31/02) are rejected as invalid.
- In the order summary's payment detail, the frozen per-fee rows (e.g. "PPN (11%)", "Admin Fee") collectively constitute FR-016's "Tax & Service Fee", and the "Total Payment" line is FR-016's "Grand Total Payment" (accepted label mapping, 2026-08-06).
- **Schema revision (FR-025 – FR-029), added 2026-08-07**: the revision is a data-preserving conversion, not a reset. Existing master list entries keep their names and active state and acquire new identifiers; every record referencing them is re-pointed in the same change, so nothing is dropped and re-seeded. It is assumed the gender key change lands together with (or replaces) this feature's own gender migration rather than after it, since shipping the UUID key first and converting it afterwards would mean migrating data that never needed to exist.
- The `SYSTEM` author recorded on seeded master list entries is a reserved literal, not an admin account; nothing authenticates as it and no screen offers it as a choice.
- Retiring a master list entry is done by clearing its active flag, not by deleting the row — deletion is assumed never to be offered for entries that existing orders or holders reference.
- The order status names (`PENDING`, `PAID`, `CANCELLED`, `EXPIRED`) are treated as stable identifiers by everything that reads them, so the master list's names are not renamed casually even though the list is admin-governed.

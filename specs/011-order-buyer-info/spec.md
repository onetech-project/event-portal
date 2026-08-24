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
- Q: Does the form step's summary itemize fees? → A: No — on the form-filling step the summary shows only the grand Total Payment, labeled as including all taxes and fees; the itemized Ticket Total / per-fee rows appear on the awaiting-payment step's summary and in the receipt email (constitution v2.1.0). (This answer suppressed the itemized rows but kept the fee-inclusive figure. The 2026-08-12 session below narrows it further: the form step's figure becomes the raw ticket subtotal, and the fee-inclusive total is confined to the awaiting-payment step.)
- Q: The progress rail marks Registration as finished and Payment as current while the guest is still filling the forms — where should the QR payment screen live so the rail can tell the two apart? → A: On its own route, `/events/{slug}/orders/{orderNumber}/checkout` (a sibling of the existing `/done`); the holder forms keep `/events/{slug}/orders/{orderNumber}` and the rail derives its stage from the route, so forms = Registration and the QR screen = Payment.
- Q: What happens when a guest opens the wrong one of the two order pages for their order's state? → A: Forward silently and without adding a history entry — forms-with-payment-started forwards to the payment route, payment-route-before-checkout forwards back to the forms, PAID forwards to `/done` (as today), and Continue to Payment navigates the same way, so the back button never bounces between two mutually forwarding pages.
- Q: How should the first form show the guest that it is the buyer's information? → A: No new label is needed — the delivery notice already on the first card ("The invoice and e-ticket will be sent via email") is what marks it; form 1 is simultaneously ticket holder 1 and the buyer, and no separate buyer form exists.
- Q: After payment, should the buyer's email receive anything the other ticket holders don't? → A: **The buyer's address is the only recipient.** Exactly one email is sent, to form 1's address, carrying the invoice/receipt and every ticket in the order. The other holders' addresses receive nothing. This reverses the per-holder delivery recorded earlier in this same session (constitution amendment required).

### Session 2026-08-07

- Q: The phone field accepted letters until blur. Should it keep doing so? → A: No — the field MUST refuse non-digit characters as they are typed, keystroke by keystroke, instead of accepting them and complaining on blur. `inputMode="numeric"` and `type="tel"` are keyboard hints, not filters, and never blocked anything on a physical keyboard.
- Q: When the guest types a local number like `081234567890` into the free-text phone box, what value is stored in the database and sent to the payment gateway? → A: **Exactly what was typed.** No normalization: the field is free text with no hardcoded country code, the guest may enter the number in either `62…` or `0…` form, and that choice is preserved verbatim through storage and on to the gateway. This reverses the normalize-to-`+62` direction taken earlier in this same session; no `+` is ever stored, because the field accepts digits only.
- Q: Must the typed number start with `0` or `62`, or is any 10-15 digit string accepted? → A: Any 10-15 digits. The rule is length alone — no prefix requirement, so a bare subscriber number or a foreign number passes. This keeps the validator a single length check on both sides of the wire and keeps the field genuinely free text. (The 2026-08-13 session below raises the floor from 10 to 12; the length-alone, no-prefix character of the rule is unchanged.)
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

### Session 2026-08-12

- Q: On the form-filling step, does the summary's single money figure include the order's fees? → A: **No — it shows the raw ticket subtotal**, the sum of the order's lines with every fee excluded. The fee-inclusive figure appears only on the awaiting-payment (checkout) step. This narrows the 2026-08-06 answer above, which suppressed the itemized rows but still rendered the fee-inclusive grand total.
- Q: Is this a display change or does the order's stored total genuinely exclude fees until payment starts? → A: **Display only.** The stored total and the frozen per-order fee lines are untouched, so the amount charged, the gateway's gross amount, and the receipt are all unaffected. Only which of the two figures the form-filling step renders changes. Moving the fee math to payment-start was rejected: the total is frozen at booking alongside the fee lines, and the hold, the expiry restore, and the gateway charge all read that one value.
- Q: What labels the figure, now that "Includes all taxes and fees" is false above a fee-free number? → A: **The heading stays "Total Payment"; the sub-line becomes a statement that taxes and fees are added at the next step.** The stub's closing line keeps its visual weight, and the sub-line warns the buyer the number will rise rather than letting the higher checkout total arrive as a surprise.

### Session 2026-08-13

- Q: Does the 12-digit minimum apply to the number exactly as the guest typed it, so an 11-digit local number like `08123456789` — which FR-006 and the edge-case list previously called valid — is now rejected? → A: **Yes — length alone, 12 to 15 digits counted on the value as typed, with no prefix logic anywhere.** `08123456789` (11 digits) is rejected with the ordinary too-short error; `081234567890` and `628123456789` (12 digits each) pass. A prefix-aware floor (12 for `62…`, 11 for `0…`) was rejected: it would have reintroduced the prefix inspection the 2026-08-07 session deliberately removed, and the validator stays a single length check on both sides of the wire. The consequence is accepted deliberately — a guest whose local-form number is 11 digits must enter it in `62…` form to proceed.
- Q: What happens to phone numbers already stored under the old rule that are shorter than 12 digits — are they still valid where they sit? → A: **Valid at rest, permanently.** The 12-digit floor is a write-time rule on the holder forms only. Stored shorter numbers keep displaying, keep reaching the payment gateway, and keep working for resend; no database constraint, no migration, and no corrective sweep is introduced. Re-validating history was rejected because digits cannot be invented for a number already taken — the only outcomes would be broken reads or blocked saves, neither of which makes an old number reachable.
- Q: The order summary panel was hand-edited to match Figma 206-3145, dropping the Booking ID and each line's per-unit price, which FR-013 and FR-014 still required. Should the spec bend to the shipped design, or the panel be restored to the spec? → A: **The spec bends to the design.** FR-013 no longer asks for the Booking ID on the summary card (it stays on the confirmation screen and in the receipt) and FR-014 no longer asks for a per-unit price on each line (quantity and line subtotal remain, and the unit price stays the figure the subtotal is derived from). This closes the open UI/spec disagreement recorded in the quality checklist; the two acceptance assertions suspended in `page.test.tsx` and `checkout/page.test.tsx` are to be rewritten to the amended expectation rather than left commented out.

### Session 2026-08-19

- Q: In which situations should the holder forms come back pre-filled with the details the server already holds? → A: **Whenever the order's slots carry saved details — the data's presence is the only condition.** Every return to the forms screen prefills: an ordinary reload, a back-navigation, a second tab, a return after a payment attempt that did not settle, and an EXPIRED or CANCELLED order rendering behind the end-of-journey modal. The screen never inspects *why* the guest came back. The narrower "only after a failed payment" trigger was rejected because `payment_started` is derived from whether a payment code exists, so an order whose forms saved but whose code was never issued reports `payment_started: false` — precisely the reported case — and that trigger would not have fired on it.
- Q: If a restored form carries a gender that has since been deactivated in the master list, and so is no longer among the options the select offers, what should the card show? → A: **The saved gender, shown as selected, with that one card's option list widened to include it.** The guest's own recorded answer stays visible and is never silently rewritten, matching the rule already set for holders elsewhere in this spec. Once the guest changes it the retired option leaves the list and cannot be chosen again. Leaving the field blank was rejected as discarding a stated answer; showing it without widening the list was rejected because the value would then be refused on submit.
- Q: When a background re-read of the order arrives while the guest is part-way through typing, what should happen to the fields on screen? → A: **Nothing — the fields are seeded once, when the card first appears, and no later arrival of the order data rewrites any of them.** The saved details are a starting point, not a live feed. Overwriting untouched fields, or overwriting everything, were both rejected: the order is re-read whenever the guest returns to the tab, so either rule would move fields under a guest who had merely stepped away to their banking app or their email.
- Q: Should the page tell the guest that their previously entered details have been restored, or simply show the cards already filled? → A: **Simply show them filled — no notice is added.** Filled fields on return are what a guest already expects of any form, and they are reading back their own details. A restore notice was rejected because the holder cards deliberately carry exactly one notice (FR-011's delivery chip on the first card), and a second would compete with it and need its own design decision; correcting a stale value costs the same one edit either way.

### Session 2026-08-24

- Q: Should the value a holder form submits for gender be the master entry's identifier rather than its display name? → A: **Yes — the form submits the IDENTIFIER, and the order readback carries BOTH the identifier and the name.** This reverses FR-027's stated rationale for genders (the order status is unaffected and still resolves by name). The readback carrying both is not redundancy: today the name is *self-sufficient*, serving as the submitted value AND the display label, which is exactly why FR-031's retired-gender case works — the name comes back, the card widens its list with it, and the same name goes back out. Identifiers break that self-sufficiency, and the break is asymmetric. If the readback carried only the name, a client would have to resolve name → identifier to submit, which it cannot do for a retired entry absent from the active master list. If it carried only the identifier, the client would have to resolve identifier → name to render a label, which fails for the same reason and in the same case. Carrying both leaves a restored form holding a retired gender displayable *and* submittable with no lookup surface added and no widening of the master list.
- Q: Does the order-status master list change with it? → A: **No.** The status NAME remains what every interface exchanges (FR-026), and FR-027's by-name resolution still holds for it. Only the gender half of that rationale is withdrawn.

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
3. **Given** a phone number with fewer than 12 digits or more than 15 digits, **When** the field is validated, **Then** an error message is shown on that field. Letters and symbols cannot reach the field at all, so they never produce one.
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

While filling the forms, the guest can review the transaction in a summary card on the right side of the page. It shows the event name and event date; one line per purchased ticket type or bundle with its name, quantity, and line subtotal; a payment method section fixed to QRIS (visible but not changeable); and the ticket subtotal under a Total Payment heading, noted as having taxes and fees added at the next step. Neither the fees nor the fee-inclusive grand total appear while the forms are being filled; both arrive together on the awaiting-payment step.

**Why this priority**: The summary builds purchase confidence and transparency but the transaction can complete correctly without it; it depends on the page from Story 1 existing.

**Independent Test**: Open the order page for a known order and compare every summary value (lines, quantities, subtotals, fees, grand total) against the order's actual contents and pricing.

**Acceptance Scenarios**:

1. **Given** the order page on a desktop-width screen, **When** it renders, **Then** the summary card appears on the right side showing the event name and event date; no Booking ID appears on the card.
2. **Given** an order with Day 1 × 2 at Rp2.600.000, **When** the summary renders, **Then** that line shows the ticket name, quantity 2, and subtotal Rp5.200.000; the Rp2.600.000 unit price is not shown on the line.
3. **Given** the payment method section, **When** the guest interacts with it, **Then** QRIS is shown as the selected method and cannot be changed to anything else.
4. **Given** a ticket total of Rp5.200.000 and tax & service fee of Rp10.000, **When** the form step's summary renders, **Then** it shows Rp5.200.000 under the Total Payment heading, with the sub-line stating that taxes and fees are added at the next step, no itemized fee rows, and Rp5.210.000 appearing nowhere on the step; **When** the awaiting-payment summary renders, **Then** it itemizes Ticket Total Rp5.200.000, the fee Rp10.000, and Total Payment Rp5.210.000, which always equals ticket total plus tax & service fee.
5. **Given** that same order, **When** the guest continues from the form step to the awaiting-payment step, **Then** the figure rises from Rp5.200.000 to Rp5.210.000 and the amount actually charged is Rp5.210.000 — the form step's figure is a display of the subtotal, never a repricing of the order.

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

### User Story 7 - Returning to the Forms Finds Them Still Filled (Priority: P2)

A guest fills in every ticket holder, presses Continue to Payment, and the payment does not
complete — the code never appears, they close the tab, they come back from their banking app,
or they simply reload. They land on the holder forms again and find every card exactly as
they left it: their names, emails, phone numbers, dates of birth and genders still there. They
press Continue to Payment again and try the payment once more, without retyping a single
field.

**Why this priority**: a guest can still finish a purchase without this — by typing everything
a second time — so it sits below the forms themselves and their validation. But it is the
difference between a failed payment costing one press and costing a full re-entry of every
holder's details, and the details are already stored, so the retyping buys nothing.

**Independent Test**: fill and submit the forms for an order, arrange for the payment not to
complete, return to the forms address, and confirm every field carries what was submitted and
that continuing again needs no retyping.

**Acceptance Scenarios**:

1. **Given** an order whose holder details were submitted but whose payment did not complete,
   **When** the guest opens the holder forms again, **Then** every field of every card shows
   the submitted value.
2. **Given** that restored screen, **When** the guest reads the Continue to Payment button,
   **Then** it is enabled, and no field shows a validation error merely for having been
   restored.
3. **Given** that restored screen, **When** the guest presses Continue to Payment without
   editing anything, **Then** the order is accepted with exactly the details already stored.
4. **Given** an order whose forms have never been submitted, **When** the guest opens the
   holder forms, **Then** every card is empty, exactly as before this change.
5. **Given** the guest is part-way through typing into a restored card, **When** they switch
   to another tab and come back, **Then** every field is exactly as they left it — the
   restored values they kept, the edits they made, and the field they were mid-way through.
6. **Given** a restored card whose gender has since been deactivated in the master list,
   **When** the guest looks at that card, **Then** the gender shows as selected and submitting
   the card unchanged is accepted.
7. **Given** that same card, **When** the guest opens the gender select on a *different* card,
   **Then** the deactivated option is not offered there.
8. **Given** an order that has expired or been cancelled, **When** the forms render behind the
   end-of-journey modal, **Then** they are filled with the stored details rather than blank,
   so the guest can still see what they had entered.
9. **Given** any restored screen, **When** the guest reads the page, **Then** no notice, badge,
   or banner announces that the details were restored, and the first card's delivery chip
   remains the only notice on the forms.

### Edge Cases

- An order of a single bundle unit shows exactly one form; that form's holder is the primary contact.
- Ten or more forms (e.g., Day 1 × 10) all render and remain individually fillable; the summary card remains reachable while scrolling the forms.
- Two forms filled with the same email address are accepted; delivery is unaffected, since only the buyer's address is ever written to.
- A phone number of exactly 12 or exactly 15 digits is valid (boundaries inclusive); 11 digits is not. Leading zeros are preserved, since the value is a digit string and not an arithmetic number.
- The same number entered as `081234567890` and as `6281234567890` produces two different stored values, and both are accepted — the form the guest chose is the form that is kept.
- A number that is 12 digits in international form but only 11 in local form (`628123456789` versus `08123456789`) is accepted in the first spelling and rejected in the second. The length is counted on what was typed, so the two spellings genuinely differ in validity; the guest's way through is to type the `62…` form.
- A number pasted with separators (`+62 812-3456-789`) keeps only its digits (`628123456789`); the `+` is dropped along with the spaces and dashes, because the value is digits only.
- A holder record written before the floor rose holds a 10- or 11-digit number: it stays exactly as it is and stays usable — it displays, it reaches the gateway, and resend still delivers on it. Nothing re-validates it, so raising the floor never invalidates an order that already exists.
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
- A payment attempt saves the holder details and then fails before a payment code is issued: the order stays PENDING with its details stored and its hold deadline untouched, and it reports that payment has not started. Returning to the forms screen shows every card filled from those stored details (FR-030), and continuing re-runs only the payment leg. This is the case that motivated FR-030 — a trigger keyed on payment state would not fire here, because payment never started.
- Some slots hold saved details and others do not — an order whose forms have never been submitted, or a shape that changed: each card is filled from its own slots, and a card with nothing saved renders empty exactly as before (FR-030).
- A restored gender has since been deactivated: the card still shows it as selected, that card's option list is widened to hold it, and submitting the form unchanged is accepted (FR-031). No other card is offered the retired option.
- The guest steps away to their banking app or their email and returns, causing the order to be re-read: every field stays exactly as they left it, filled or half-typed (FR-032).
- The guest deliberately clears a restored field: it stays cleared, Continue disables until it is valid again, and nothing puts the old value back.
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
- **FR-006**: Phone Number MUST be 12 to 15 digits inclusive, and nothing but digits (clarified 2026-08-13, raising the floor from the 10 of the 2026-08-07 answer). The digits are counted on the value exactly as typed, with no prefix inspection: a number that reaches 12 digits only in its international spelling is too short in its local spelling and MUST be rejected there. The field is free text with no country code supplied by the interface: the guest enters the whole number themselves, in either international (`6281234567890`) or local (`081234567890`) form, and that choice MUST be preserved verbatim — the value is stored and exchanged exactly as typed, with no normalization between the two forms and no `+` added. Validation is length alone; no particular prefix is required. The field MUST refuse non-digit characters as they are typed rather than accepting them and rejecting the value later, so letters, spaces, dashes and `+` never enter the value in the first place.
- **FR-007**: Gender MUST be explicitly selected; Date of Birth MUST be provided and MUST NOT be a future date.
- **FR-008**: Every validation failure MUST surface an error message on the specific field that failed, on the specific form it belongs to.
- **FR-009**: The Continue to Payment button MUST be disabled by default and MUST become active only when every field of every form passes validation; it MUST return to disabled if any field becomes invalid again.
- **FR-010**: Clicking the active Continue to Payment button MUST save all participants' data, MUST ensure a Booking ID exists for the order (creating one only if it does not already exist), and MUST then direct the guest to the payment page at its own address (FR-020), without leaving the forms page as a back-button stop.
- **FR-011**: The first ticket holder form MUST always display an information notice at the top right of its header stating that the invoice and e-ticket will be sent via email; the notice MUST NOT appear on any other form, and there is no separate page-level banner.
- **FR-012**: After successful payment, exactly ONE email MUST be sent, to the buyer's address — the email on form 1 (clarified 2026-08-06, superseding this session's earlier per-holder rule). It MUST carry the order receipt and every ticket QR pass in the order. No email is sent to the other ticket holders' addresses; those are collected as holder identity, not as delivery addresses. The order is marked as delivered only once that email is sent successfully.
- **FR-013**: The order summary card MUST be positioned on the right side of the page (alongside the forms on wide screens) and MUST show the event name and event date. It MUST NOT carry the Booking ID (amended 2026-08-13 to the shipped design, Figma 206-3145); the Booking ID remains disclosed on the confirmation screen and in the receipt, which is where the guest needs to keep it.
- **FR-014**: The summary MUST list one line per purchased ticket type or bundle showing its name, quantity, and line subtotal (quantity × unit price). The per-unit price MUST NOT be shown on the line (amended 2026-08-13 to the shipped design, Figma 206-3145); the subtotal is still computed as quantity × unit price, so the omitted figure remains derivable from what the line displays.
- **FR-015**: The summary MUST show a payment method section with QRIS as the default and only option; the section is visible but MUST NOT be changeable.
- **FR-016**: On the form-filling step, the summary MUST show the **ticket subtotal** — the sum of the order's lines with every fee excluded — as its single money figure, with no itemized fee rows and no fee-inclusive figure anywhere on the step (clarified 2026-08-12, narrowing the 2026-08-06 answer that suppressed only the rows). The itemized breakdown of Ticket Total, Tax & Service Fee, and Grand Total Payment — where Grand Total equals Ticket Total plus Tax & Service Fee — MUST appear on the awaiting-payment step's summary and in the receipt email, and the Grand Total MUST appear nowhere earlier.
- **FR-016a**: The form-filling step's figure MUST keep the heading "Total Payment", and its sub-line MUST state that taxes and fees are added at the next step. That sub-line MUST NOT claim the figure already includes taxes or fees, because it no longer does; leaving the old wording above a fee-free number would understate what the guest is about to be charged.
- **FR-016b**: FR-016 is a display rule only. The order's stored total and its frozen per-order fee lines MUST NOT change, and neither MUST when they are computed: the total is frozen at booking, and the amount charged, the payment gateway's gross amount, the admin order views, and the receipt all continue to read that fee-inclusive value. Only which figure the form-filling step renders changes.
- **FR-016c**: An order that predates fees carries no subtotal of its own. On the form-filling step such an order MUST fall back to its stored total, which for those orders already excludes fees and is therefore the same number; no such order MUST render a blank or zero figure.
- **FR-017**: The topmost form's holder IS the buyer, and MUST serve as the order's primary contact wherever a single contact for the order is needed — payment records, admin order views, order-level communication, and the sole delivery recipient of FR-012; the contact details retained on the order are the holder's name, email, and phone only.
- **FR-018**: Each stored ticket holder gender MUST reference an entry of the gender master list (referential integrity enforced by the data store), replacing the previous fixed Male/Female storage constraint; the selectable options remain governed solely by the master list. The gender's display NAME was the value exchanged with the ordering screens until 2026-08-24; a form now submits the identifier and the readback carries both (FR-034, FR-035).
- **FR-019**: All form fields (text inputs, the date field, and the gender select) MUST render at the taller comfortable field size shown in the design, with a uniform height across every field type.
- **FR-020**: The QR payment screen MUST occupy its own address, `/events/{slug}/orders/{orderNumber}/checkout`, distinct from the ticket-holder forms at `/events/{slug}/orders/{orderNumber}`. Each of the four progress stages MUST therefore correspond to exactly one address: ticket selection → Booking, holder forms → Registration, QR payment → Payment, confirmation → Done. The progress rail MUST mark the stage matching the current address as current, every earlier stage as complete, and every later stage as not reached — in particular, Registration MUST NOT be shown as complete while the holder forms are still being filled.
- **FR-021**: A guest who opens the address that does not match their order's state MUST be forwarded to the one that does, without adding a back-button stop: the forms address forwards to the payment address once payment has started, the payment address forwards back to the forms address before checkout, and a paid order forwards to the confirmation address from either. Consequently the back button MUST NOT bounce the guest between two addresses that forward to each other.
- **FR-022**: An order that has ended without a purchase (EXPIRED or CANCELLED) MUST NOT replace the screen's contents with the end-of-journey card. Both order screens — the holder forms at the forms address and the QR screen at the payment address — MUST keep rendering their own layout (progress rail, forms or payment panel, order summary) and MUST present the end-of-journey message as a blocking modal dialog layered over that layout. The same modal MUST be used on both screens, so the guest sees one presentation of the ended order regardless of which screen they were on. On the QR screen the modal MUST open the moment the payment countdown reaches zero, without waiting for the server to report the order EXPIRED, and the QR code MUST NOT remain on screen behind it; the server's status change arriving moments later MUST leave the screen unchanged, so one expiry produces one message rather than an inline notice followed by a modal. The separate inline "This payment code has expired" notice is therefore removed.
- **FR-023**: The end-of-journey modal MUST be unclosable: it MUST NOT offer a close (X) control, MUST NOT dismiss on Escape, and MUST NOT dismiss on a backdrop click. While it is open the page behind MUST be inert — not reachable by pointer or keyboard focus and not scrollable — beneath a dimmed backdrop. The modal's own action button is the only exit offered inside the page; ordinary browser navigation (back button, address bar) is never blocked. Once raised, the modal MUST NOT disappear on its own while the order remains in its ended state.
- **FR-024**: The modal MUST follow the dialog design at Figma node `293-3`: a centered card on a dimmed backdrop, an alert icon inside a tinted circle at the top, the heading, the body copy beneath it, and exactly ONE full-width filled action button labelled "Return to Home Page" leading to the site home. The previous "Repeat Order" link to the event's ticket selection MUST be removed, and because no repeat action is offered the body copy MUST NOT tell the guest to repeat their order — it states that the payment window ran out (or that the order was cancelled) and that the seats went back on sale.

> **Schema revision (clarified 2026-08-07) — scope note**: FR-025 through FR-029 are a storage revision requested during this feature's clarification. Only FR-025 and FR-027 touch this feature's own surface (they govern the gender master list that FR-003 and FR-018 depend on); the order-status and package requirements are carried here because they were decided here, and they change no guest-visible behaviour. If the team prefers, they can be split into their own spec without altering any decision recorded above.

- **FR-025**: The order-status and gender master lists MUST identify their entries by a compact auto-assigned number rather than a randomly generated identifier. Existing entries MUST keep their names and active state across the change, and every record referencing them — including each ticket holder's stored gender (FR-018) — MUST be re-pointed at the new identifier so no reference is lost or silently re-bound to a different entry.
- **FR-026**: An order MUST record its status as a reference to an entry of the order-status master list rather than by storing the status word itself. The status NAME MUST remain the value every interface exchanges and displays — guest order responses, admin order views, the checkout status stream, and payment records — so the change is invisible outside storage.
- **FR-027**: Every master list entry MUST carry a name that is mandatory and unique within its list, and an active flag that is mandatory and defaults to active. A blank or duplicated name MUST be refused. The original rationale — that both the gender a form submits and the order status carried on the wire resolve to their entry by name — now holds for the ORDER STATUS only: as of 2026-08-24 a holder form submits the gender's identifier (FR-034). The rule itself is unchanged and still required: the gender name remains the displayed label and the value the readback carries (FR-035), and a blank or duplicated label is no more acceptable than a blank or duplicated resolution key.
- **FR-028**: Each order-status and gender entry MUST record who created it and, once edited, who last updated it, alongside its existing created and updated timestamps. Entries seeded by the system (the four order statuses and the two genders) MUST record the reserved author `SYSTEM`; entries an admin creates or edits MUST record that admin's identifier. No other table gains these fields in this change.
- **FR-029**: A package MUST express its availability as a two-state active flag rather than an `ACTIVE`/`INACTIVE` word, in storage and on the wire alike: the admin interface MUST exchange it as a true/false value and present it as a toggle, and the `ACTIVE`/`INACTIVE` strings MUST NOT survive anywhere in the contract. Issued tickets are explicitly excluded — their ACTIVE/USED/REVOKED lifecycle MUST remain a three-state status, because the gate must tell a pass already scanned apart from one revoked, which a two-state flag cannot express.

> **Completes**: FR-020 and FR-021 finish what spec 007 (`specs/007-event-scoped-routes`, FR-004/FR-005) required but could not deliver: that spec mandated a rail stage derived from the address and never contradicting it, while leaving the holder forms and the QR screen sharing one address — so one of the two stages had to be misreported. Giving the payment screen its own address makes the stage-per-address rule satisfiable, and spec 007's SC-002 becomes achievable on all four screens.

> **Supersedes**: This feature removes the separate buyer information block that spec 010 (`specs/010-bundle-single-form`, FR-008) explicitly preserved, and replaces the buyer-contact collection of spec 008 (`specs/008-e2e-purchase-flow`, FR-012) with collection through form 1, which is both ticket holder 1 and the buyer. Delivery (FR-012 here) ends up where the constitution had it before this feature — exactly one email to the buyer containing all tickets — after a same-session detour through per-holder delivery; constitution v3.0.0 restores it.

> **Restoring saved holder details (clarified 2026-08-19) — scope note**: FR-030 onward
> govern what the holder forms show when the guest returns to a screen whose details the
> server has already stored. They **supersede** the "Option B" rule of spec
> `008-e2e-purchase-flow` — that nothing is persisted before Continue to Payment and a
> revisit therefore always shows empty forms. That rule rested on a premise this feature's
> own checkout call invalidated: the call saves every holder's details, so a subsequent read
> of the order does have details to render. Spec 008's contracts, quickstart and data-model
> notes MUST be corrected in the same change.

- **FR-030**: When an order's slots carry saved holder details, the holder forms MUST render
  pre-filled with them. The presence of saved details MUST be the only condition — the screen
  MUST NOT inspect why the guest returned, and MUST NOT key the behaviour on payment state.
  This applies to an ordinary reload, a back-navigation, a second tab, a return after a
  payment attempt that did not settle, and an EXPIRED or CANCELLED order rendering behind the
  end-of-journey modal (FR-022). A slot carrying no saved details MUST render an empty card,
  exactly as today.
- **FR-031**: A restored gender that is no longer active in the master list MUST still render
  as the card's selected value, with that card's option list widened to include it so the
  select can display it. The retired value MUST NOT be offered on any card that does not
  already hold it, and once the guest picks a different gender the retired option MUST leave
  that card's list. Accepting the forms MUST NOT refuse a gender solely for being retired
  when it is the value already recorded on that slot; a retired gender MUST still be refused
  on a slot that did not already carry it.
- **FR-034**: A holder form MUST submit the gender master entry's IDENTIFIER, not its display
  name (clarified 2026-08-24). The server MUST validate the submitted identifier against the
  master list and MUST refuse one that names no entry, reporting it as a field-level failure
  rather than resolving it to an absent or zero reference.
- **FR-035**: The order readback that pre-fills a restored form MUST carry, for each filled
  slot, BOTH the gender's identifier and its display name. Neither alone is sufficient, and the
  reason is FR-031: a retired gender is absent from the active master list, so a client given
  only the name cannot resolve an identifier to submit, and a client given only the identifier
  cannot resolve a name to display. Carrying both keeps FR-031 working without adding a lookup
  surface or widening what the master list offers.
- **FR-036**: The gender's display name MUST remain the only gender value shown to a guest. An
  identifier MUST NOT be rendered in any interface, and no guest-facing copy, error message or
  confirmation may name one — the identifier is a submission detail, not something a guest has
  any use for.
- **FR-032**: The saved details MUST seed each card once, when it first appears. A later
  re-read of the same order MUST NOT rewrite any field, whether or not the guest has edited
  it. Nothing the guest has typed MUST be lost by the order being re-read — including the
  re-read that happens when they return to the tab or the network reconnects.
- **FR-033**: Restoring the saved details MUST NOT add any notice, banner, or badge to the
  screen. The holder cards MUST continue to carry exactly one notice — the delivery chip on
  the first card (FR-011) — and the restored fields MUST be presented no differently from
  fields the guest has just typed.

### Key Entities

- **Ticket Holder (Attendee)**: The person a single form describes — full name, email, phone number, gender, date of birth. Owns one standalone ticket or all passes of one bundle unit. Gender is stored as a reference to a Gender Master List entry, not free text. The email identifies the holder; it is not a delivery address unless the holder is also the buyer (FR-012).
- **Gender Master List**: The admin-governed list of selectable genders (currently Male/Female). Sole source of both the form's options and the stored holder gender references. Each entry has a compact auto-assigned number as its identifier, a mandatory unique name, a mandatory active flag, and a record of who created and last updated it (FR-025, FR-027, FR-028).
- **Order Status Master List**: The admin-governed list of order states (seeded PENDING, PAID, CANCELLED, EXPIRED), with the same identifier, name, active-flag, and authorship shape as the Gender Master List. An order points at one of its entries; the entry's NAME is what every screen and interface shows (FR-026).
- **Order (Booking)**: The held purchase being completed; identified by a Booking ID and carrying the event's name and date. The Booking ID is not shown on the summary card (FR-013) — it surfaces on the confirmation screen and in the receipt. Its state is a reference to an Order Status Master List entry, exchanged and displayed by name.
- **Package (Bundle Offer)**: A purchasable bundle of ticket types. Its availability is a two-state active flag, exchanged with the admin interface as a true/false value (FR-029) — distinct from an issued ticket, whose ACTIVE/USED/REVOKED lifecycle stays a three-state status.
- **Order Line**: One purchased ticket type or bundle within the order — name, quantity, unit price, and derived subtotal. The unit price remains part of the line and is what the subtotal is derived from; it is simply not rendered on the summary card (FR-014).
- **Bundle Unit**: One purchased instance of a bundle; collects exactly one holder form covering all of the unit's passes (per spec 010).
- **Fees**: The tax & service amount added to the ticket total to produce the grand total.
- **Buyer (Primary Contact)**: The holder described by the topmost form — ticket holder 1 and the buyer at once. Stands in wherever the order needs a single contact, and is the only address the order's tickets and receipt are emailed to. Retained on the order as name, email, and phone only.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: For 100% of orders, the number of forms shown equals (standalone ticket quantity) + (number of bundle units), and zero buyer contact forms appear — verified for at least the shapes 1 ticket, 10 tickets, 2+3 tickets, and 1 bundle + 2 tickets.
- **SC-002**: Zero orders reach the payment page with a missing or invalid holder field; every invalid field shows an error on that field before the guest can proceed.
- **SC-003**: For 100% of paid orders, exactly one email is sent and it goes to the buyer's address (form 1), carrying the receipt and every pass in the order; zero emails reach any other holder's address.
- **SC-004**: A guest completes the order page for a 3-ticket order (typing all fields) in under 3 minutes.
- **SC-005**: In 100% of rendered summaries, each displayed line subtotal equals quantity × the order's recorded unit price and the awaiting-payment grand total equals ticket total + tax & service fee. Zero summary cards display a Booking ID or a per-unit price (FR-013, FR-014). On the form-filling step the single figure equals the ticket subtotal exactly, with zero fees included, in 100% of orders that carry a subtotal; the itemized ticket-total and fee rows, and the fee-inclusive total itself, appear only on the awaiting-payment summary and in the receipt, never on the form-filling step.
- **SC-005a**: Across the change, 100% of orders are charged the same amount they would have been charged before it — the stored total, the gateway gross amount, and the receipt total are byte-identical, and zero orders have their frozen fee lines recomputed.
- **SC-006**: On each of the four purchase screens, the progress rail marks exactly one stage as current and it is the stage that screen represents — verified on all four, with zero screens showing a later stage as current or an unfinished stage as complete.
- **SC-007**: The storage revision changes nothing a guest can observe: after it, 100% of order responses, admin order views, and checkout status frames still carry the status by name (`PENDING`/`PAID`/`CANCELLED`/`EXPIRED`), and every ticket holder's gender resolves to the same gender name it had before — zero holders end up pointing at a different entry, and zero orders lose their state.
- **SC-008**: After the revision, zero occurrences of the `ACTIVE`/`INACTIVE` package strings remain in the admin contract, and every issued ticket still reports one of ACTIVE, USED, or REVOKED — a pass already scanned remains distinguishable from a revoked one in 100% of gate validations.
- **SC-009**: 100% of entries in both master lists have a non-blank name that is unique within its list and a recorded author — the six system-seeded entries attributed to `SYSTEM`, every admin-created or admin-edited entry attributed to that admin — and an attempt to save a duplicate or blank name is refused in 100% of cases.
- **SC-010**: On both order screens, an order that ends without a purchase leaves the screen's own layout rendered in 100% of cases and shows the end-of-journey dialog over it; across Escape, backdrop click, and scroll attempts the dialog is dismissed zero times, and exactly one action button is present on it.
- **SC-011**: A guest returning to the holder forms for an order whose details are stored sees every field of every card carrying those details, in 100% of returns — verified across an ordinary reload, a back-navigation, a second tab, a return after a payment attempt that did not settle, and an EXPIRED or CANCELLED order behind the end-of-journey modal.
- **SC-012**: Zero characters of a guest's typing are lost to the order being re-read, across repeated tab-away-and-return and reconnect cycles on a part-filled screen.
- **SC-013**: A form restored from stored details submits unchanged in 100% of cases, including one carrying a gender since deactivated — zero submissions refused for a value the slot already held.

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
- In the order summary's payment detail, the frozen per-fee rows (e.g. "PPN (11%)", "Admin Fee") collectively constitute FR-016's "Tax & Service Fee", and the awaiting-payment step's "Total Payment" line is FR-016's "Grand Total Payment" (accepted label mapping, 2026-08-06). The heading is shared across the two steps but the figure beneath it is not: on the form-filling step the same "Total Payment" heading sits above the ticket subtotal (FR-016a, clarified 2026-08-12). The shared heading is deliberate — the alternative, renaming the form step's heading to "Subtotal", was rejected so the stub's closing line keeps its weight, and the sub-line carries the distinction instead.
- **Schema revision (FR-025 – FR-029), added 2026-08-07**: the revision is a data-preserving conversion, not a reset. Existing master list entries keep their names and active state and acquire new identifiers; every record referencing them is re-pointed in the same change, so nothing is dropped and re-seeded. It is assumed the gender key change lands together with (or replaces) this feature's own gender migration rather than after it, since shipping the UUID key first and converting it afterwards would mean migrating data that never needed to exist.
- The `SYSTEM` author recorded on seeded master list entries is a reserved literal, not an admin account; nothing authenticates as it and no screen offers it as a choice.
- Retiring a master list entry is done by clearing its active flag, not by deleting the row — deletion is assumed never to be offered for entries that existing orders or holders reference.
- The order status names (`PENDING`, `PAID`, `CANCELLED`, `EXPIRED`) are treated as stable identifiers by everything that reads them, so the master list's names are not renamed casually even though the list is admin-governed.
- **Restoring saved details (FR-030 – FR-033), added 2026-08-19 — decisions taken without a
  question, recorded so they are not re-opened silently**:
  - A card restored to a complete, valid set of details arrives with Continue to Payment
    already enabled. This is FR-009 applied unchanged — the gate is "every field of every
    form passes validation" and nothing else — not a new rule. No field shows an error
    merely for having been restored rather than typed.
  - Date of birth is stored as a calendar date and typed as DD/MM/YYYY (FR-003, FR-007), so
    restoring it is a presentation conversion. It is assumed to round-trip exactly: a
    restored date submitted unchanged stores the same date it came from.
  - A bundle unit's slots always agree, because a card's values fan out to every slot in its
    unit on submit (spec 010). A unit's card is therefore seeded from any one of its slots,
    and no tie-break between them is specified.
  - Nothing new is exposed. The order response already carries every holder's details to
    anyone holding the order's address; this feature only draws what that response already
    contains, so it widens no audience and adds no data to the wire.
  - Per constitution Principle VIII the acceptance suite gains a scenario for the reported
    defect — details saved, payment not completed, forms empty on return — and it is
    confirmed red against the unfixed code before the fix lands. Spec `008-e2e-purchase-flow`
    asserts the opposite rule in `contracts/booking-flow.md`, `contracts/api.md`,
    `quickstart.md` and `data-model.md`; those MUST be corrected in the same change, since a
    superseded rule left standing in a contract document is what let this behaviour survive.

# Feature Specification: Free Ticket Registration Form

**Feature Branch**: `022-free-ticket-registration`

**Created**: 2026-08-20

**Status**: Draft

**Input**: User description: "i want to implement free registration form inside an event, so user can have a free ticket... 1. User go to /event/[slug]/form/[ticketId] 2. User input form then submit 3. User get email of the E-Ticket (no payment and no receipt). on ticket_types table add new column to identify that ticket is special, this ticket wont be shown on normal ticket list page and cannot be added to the packages... only the ticket_types that has this identifier can only submitted the form, normal ticket id cannot be submitted. form validation: 1. Full name cannot be empty 2. Email format must valid 3. Phone number only number 4. Email cannot use the registered email on that event. this will not be recorded on the order table, and immediately added on attendees and tickets."

## Clarifications

### Session 2026-08-20

- **Q: What should the new `ticket_types` column be named, and what should it mean?**
  → **A (SUPERSEDED 2026-08-20 — see the final clarification of this session):
  `is_registration_only`.** The original reasoning: it names the acquisition channel rather
  than the price (`is_free`) or one of its consequences (`is_visible`). All three required
  behaviours — hidden from the purchase list, not bundleable, only submittable through the
  registration form — follow from "obtained by registering, not by buying", whereas a price
  flag implies none of them and a visibility flag implies only the first. Price stays
  independently settable (conventionally `0`) with no contradiction between the two columns.
  **The column is now `is_visible`** at the user's direction; the argument above is retained
  because it is exactly the exposure the new name carries, and FR-001a records it.

- **Q: `attendees.order_id` and `tickets.order_id` are `NOT NULL` FKs to `orders`. How is a
  free registration anchored?**
  → **A: a real zero-amount `orders` row, at status `PAID`.** The registration writes
  `orders` (total `0`), `order_items` (quantity 1), `attendees`, and `tickets` exactly as a
  purchase does, so every order-keyed read path — ticket lookup, e-ticket PDF, resend,
  admin order and attendee lists, ticket validation, the Principle VI delete guard — keeps
  working with no schema surgery and no audit of order-joined queries. The user's original
  "not recorded on the order table" is superseded by this decision. **This deviates from
  Constitution Principle IV** and is recorded under *Constitution Deviations* below.

- **Q: "Email cannot use the registered email on that event" — which existing records
  block a new registration?**
  → **A (WITHDRAWN 2026-08-20 — see the final clarification of this session): none.** The
  original answer was "any attendee on a `PAID` order at that event". The rule has since been
  removed outright at the user's direction; no record blocks a registration. Retained only to
  explain why FR-023 now states the absence of a rule rather than omitting the subject.

- **Q: Where does the two-line consent copy in the design come from?**
  → **A (superseded by the revised design, same day — see below): hard-coded for now.**
  Retained only to explain why the requirements below no longer mention two checkboxes.

- **Q (revised design): How does the guest give consent?**
  → **A: one "I agree to the Terms & Conditions" checkbox that opens the event's Terms &
  Conditions in a modal.** The two wristband acknowledgements are no longer form controls at
  all — the revised design shows them as items 8 and 9 *inside* the event's authored T&C
  document. So consent is a single act against one document, the copy is admin-authored
  through the existing per-event Terms & Conditions surface rather than compiled into the
  product, and this feature adds no consent-copy storage of its own.

- **Q: What is the registration form's address?**
  → **A (SUPERSEDED 2026-08-20 — see the final clarifications of this session):
  `/event/[slug]/register/[ticketId]`.** The original reasoning: singular `event`, and `register`
  rather than `form`, chosen deliberately over the plural `events` segment used by every other
  guest route so the invitation surface is separable from the purchase surfaces at the address
  level. **The address is now `/events/[slug]/register/[ticketId]`** — plural, matching every
  other guest route. The separability argument is withdrawn; the hazard it created (two sibling
  segments differing by one character) is withdrawn with it.

- **Q: If a registrant never receives their e-ticket, how do they get another copy?**
  → **A: operator-assisted only.** They use the Help contact already in the page header, and an
  admin resends from the console. No self-service resend surface is added. The two alternatives
  that needed no reference were rejected on privacy grounds: resending on a repeated form
  submission would let any stranger make the system mail any address on demand, and an
  email-based public lookup would let anyone test whether a given person is attending.

- **Q: The approved success copy asserts the e-ticket was sent, though delivery has not yet
  completed and the page names no address. Should it change?**
  → **A: no — ship the approved design and copy verbatim.** "Thank you for registering. Your
  complimentary invitation e-ticket for *[event name]* has been successfully generated and sent
  to your email." No address is displayed. The concern was raised and the design reaffirmed;
  the resulting obligations on the resend and operator-lookup paths are written into FR-039e
  rather than left implicit.

- **Q: After a successful registration, where does the guest land, and what happens on reload
  or a second submit?**
  → **A: a distinct confirmation address, `/events/[slug]/register/[ticketId]/success`.** The
  form is behind them, so reloading re-shows the confirmation rather than an empty form.
  (The second reason given at the time — that a refresh would otherwise raise a duplicate-email
  error — no longer applies now that FR-023 removed that rule; the first reason still stands on
  its own.) The
  approved design is a static confirmation panel — a success mark, "Registration Complete!",
  and a line naming the event — carrying no registration reference of its own.

- **Q: If an admin republishes an event's Terms & Conditions after a guest accepted them but
  before they submit the registration, should the submission be refused so they see the new
  document?**
  → **A: yes, refused, exactly as the purchase flow already does it.** (Originally worded "so
  they re-read"; since 2026-08-21 the guest is re-shown the current document and must accept it
  again, but is not required to read it — see FR-051.) The registration records
  which version was agreed to, using the same `orders.terms_agreed_at` and
  `orders.event_terms_id` fields booking uses — free, since the registration creates an order
  either way — and a submission carrying a superseded version is refused rather than silently
  binding the guest to text they never saw.

- **Q: Should the read-to-the-bottom gate apply to the existing booking flow's Terms &
  Conditions dialog as well, or only to the new registration form?**
  → **A (the GATE half SUPERSEDED 2026-08-21 — see that session below): both surfaces,
  through one shared component.** The booking dialog **keeps its current design and layout** —
  checkbox, Cancel, Agree. What is added to it is the gate: the guest must reach the bottom of
  the terms, at which point its checkbox becomes checked automatically and its Agree button
  turns up. **The gate is gone as of 2026-08-21**; only the automatic tick on reaching the
  bottom survives, and the guest may tick the box themselves at any time. "Both surfaces,
  through one shared component" is unaffected and still holds. Consent therefore means the same thing on
  both surfaces. Because the booking dialog is part of the guest purchase journey, this is a
  change to a Principle VIII **covered flow**, and the affected `e2e/` specs change in the
  same commit.

- **Q: Now that one email address may register more than once for the same event, is there any
  limit on how many times it can?**
  → **A: no limit — but the endpoint keeps its rate limit.** The earlier "one email per event"
  rule is removed in full: nothing in the registration write path is keyed on the address, and
  an address that already holds a ticket at the event — from a purchase or from an earlier
  registration — registers again freely, each submission producing its own order, attendee and
  e-ticket. Remaining quota, the sales window and the per-source throttle (FR-034) are the only
  limits left. This deletes the duplicate check, the advisory lock that made it race-free, the
  `lower(email)` index that made it fast, and its dedicated refusal code. Two consequences are
  accepted deliberately: a group can now be registered from one device by submitting the form
  repeatedly, which is the point; and the throttle is now the *sole* defence against bulk
  submission through the unlisted link rather than one of two, which is why FR-034 is
  strengthened rather than left as it was.

- **Q: Should the registration endpoint's default throttle be loosened, now that submitting the
  form several times in a row is a legitimate thing to do?**
  → **A: yes — raise the burst to 10, leave the sustained rate at 0.2 per second.** The existing
  default (burst 3) was chosen when a second submission from one address was impossible, so
  repeat traffic from one source could only be abuse. It no longer can only be abuse: registering
  a group of eight from one phone is the feature working as intended, and under the old default
  the fourth guest is met with "Too many attempts". Raising only the burst clears a realistic
  group in one sitting while leaving the sustained ceiling exactly where it was (12/min), so the
  bulk bound does not move — only the tolerance for a legitimate burst. Full refill becomes 50s,
  still well inside the idle-TTL relationship that startup validates and refuses to violate.

- **Q: What is the registration form's address, after the route was moved?**
  → **A: `/events/[slug]/register/[ticketId]`** — plural `events`, the same segment every other
  guest route uses. This reverses the earlier decision to hold the invitation surface at a
  singular `/event` segment. One convention rather than two sibling segments differing by one
  character, and one fewer address for a hand-written link to get wrong.

- **Q: The plural segment puts the form under the purchase journey's layout. Should it render
  inside that chrome?**
  → **A: no — the registration route is excluded from the countdown and the progress rail.**
  `events/[slug]/layout.tsx` wraps everything beneath it in the purchase chrome, which renders a
  sales countdown and a `Booking → Registration → Payment → Done` rail. Inherited unchanged, an
  invited guest would be shown a step named **Payment** on a surface FR-015 forbids from showing
  any payment step at all, plus a countdown for a sale they are not part of. The rail already
  derives its state from the URL, so it gains one more predicate; the App Router gives a nested
  route no way to opt out of a parent layout, so excluding it inside the frame is the only way to
  keep this address. The event's surrounding chrome is otherwise kept.

- **Q: Why do the two designs differ — shouldn't the T&C and the holder form be the same components
  the booking flow uses?**
  → **A: they should, and now they are — shared markup, not just shared rules.** Two extractions:
  `HolderFields` (name, email, phone, gender, date of birth) is rendered by both the booking flow's
  per-attendee cards and the registration form; `TermsDialogShell` (dialog frame, header spacing,
  document, automatic tick on reaching the end, the rule above the actions) is rendered by both
  terms surfaces.

  **What was actually wrong.** The two surfaces already shared their *validation* — one copy of the
  phone and date rules in `lib/holder-fields.ts` — but each drew its own markup. So a label,
  placeholder or field order corrected on one stayed wrong on the other, silently, which is exactly
  the divergence that prompted the question. The registration form's phone placeholder was
  `0812XXXXXXXX`: eleven digits, an example the twelve-digit rule would have refused.

  **The footer is shared too — corrected after a screenshot.** This first shipped with the footer
  exempted, on the reasoning that booking needs checkbox + Cancel + Agree while registration only
  needs to accept. Seen side by side that was plainly a different dialog: one small right-aligned
  "Accept" against booking's agreement checkbox and two full-width actions. The exemption is gone;
  only the Agree *handler*, and the label while a press is in flight, differ now.

  *(The Figma files could not be consulted: the Figma MCP server was disconnected. This was resolved
  from the code, so any remaining visual difference against the designs is unverified.)*

- **Q: On the admin form, should unticking "Sell this ticket type" disable the Price field and set
  it to 0 — and should that zero overwrite the stored price?**
  → **A: yes, zero and disable it — and require a real price before the type can go back on sale.**
  Unticking disables Price, sets it to `0`, and saves `0`. Ticking the box again re-enables the
  field **empty and required**, so the admin must type a price before saving.

  The second half is not decoration. FR-005 only blocks the flip when the type is inside a package,
  so an ordinary ticket priced 150,000 can be made invitation-only. Without the guard, ticking the
  box again republishes it onto the guest purchase list at **0** — free tickets for anyone, no
  error, and nothing on screen to say the price had been destroyed. That is the leak US2 exists to
  prevent, arriving through the price field rather than through a missing filter.

  **This stays a FORM affordance.** The server is unchanged: it stores whatever price it is sent and
  derives nothing, so FR-003 holds exactly as written. An API client may still set any price on an
  invitation-only type, and a registration still charges nothing whatever is stored.

- **Q: Why do we need `is_registration` on the orders table?**
  → **A: we do not — it is removed, and the distinction is derived.** An order is
  registration-originated exactly when it carries a line for a ticket type that is not
  guest-visible; containment guarantees such a type can never be bought or bundled, so one such
  line can only have come from the registration path. Constitution Principle IV was amended to
  **v6.0.0** to withdraw the "MUST be marked at creation" requirement it previously imposed.

  Two inference alternatives stay rejected, and the derivation is not one of them: a zero total
  collides with a purchasable type priced zero, which FR-003 explicitly permits; an absent
  `payments` row is an absence an abandoned purchase also shows.

  **The cost, accepted with the concern already on the table.** A stored marker records what
  happened; a derivation reports what is currently true. An admin making an invitation ticket type
  purchasable again reclassifies every historical order that used it — and since the delivery path
  reads this, a resend of an already-delivered registration would render a receipt for an order
  that never had a payment. FR-033b requires that behaviour be pinned by a test rather than
  described, so nobody later "fixes" it without discovering it was a governance decision.

- **Q: Does `is_visible = FALSE` mean exactly what `is_registration_only = TRUE` meant — hidden
  from purchase, unbundleable, and the only kind the registration form accepts — or is it a
  plain visibility switch that only controls listing?**
  → **A: exactly the same three behaviours, in one column, with the polarity inverted.** The
  column becomes `ticket_types.is_visible BOOLEAN NOT NULL DEFAULT TRUE`, and
  `is_visible = FALSE` is identical in meaning to the former `is_registration_only = TRUE`.
  Nothing about the behaviour changes; only the name and the sense of the boolean do.

  **This is a polarity inversion, not a rename.** The default flips from `FALSE` to `TRUE`,
  every `NOT is_registration_only` filter becomes `is_visible`, and every place that set the
  flag true now sets it false. A mechanical find-and-replace over the 119 references produces
  code that is precisely backwards, so each site is re-read rather than substituted.

  The domain term **"registration-only ticket type" is retained** throughout this spec and in
  the admin UI. It names the behaviour accurately, which the column name no longer does; the
  two deliberately differ and FR-001a states the relationship so no reader has to infer it.

  There is no `orders.is_registration` column. It existed in an earlier draft and was removed
  on 2026-08-20 by explicit decision (Principle IV v6.0.0): whether an *order* originated as a
  registration is now derived from whether its line references a non-visible ticket type. See
  FR-033.

### Session 2026-08-21

- **Q: When the guest ticks the agreement checkbox without having reached the end of the
  document, should the dialog's Agree action become available immediately?**
  → **A: yes — Agree tracks the checkbox, and nothing is gated on reaching the end.** The
  checkbox is now directly checkable at any moment; reaching the end of the document only
  ticks it on the guest's behalf, as a convenience. This reverses the read-through *gate*
  introduced earlier the same week (FR-014c, FR-045, FR-043c as originally written) while
  keeping the automatic tick. The reasoning: a gate that merely moves from the checkbox to
  the button is still a gate, and "the guest may agree without reading" is only true if
  agreeing actually lets them proceed. What survives is the auto-tick, which still spares a
  guest who *did* read to the end a second deliberate action.

- **Q: On the registration form itself, should its "I agree to the Terms & Conditions"
  checkbox still open the Terms modal when clicked, or tick directly like an ordinary
  checkbox?**
  → **A: still open the modal — FR-014a is unchanged.** The 2026-08-21 loosening applies to
  the checkbox *inside* the dialog, not to the form's control. The form's box remains an
  affordance that opens the document and then reflects the outcome; the document is still put
  in front of every registering guest exactly once, and what they do once it is open — read it
  or tick and accept immediately — is their decision. Unchecking on the form still withdraws
  consent directly (FR-014f).

- **Q: After the guest deliberately unticks the agreement checkbox inside the dialog, should
  reaching the end of the document again re-tick it?**
  → **A: no — the automatic tick fires at most once per opening of the dialog.** It fires the
  first time the end is reached and never again for that opening: further scrolling, resizing
  or re-reaching the end leaves the checkbox exactly as the guest left it, and it never unticks
  the box either. An automatic action must not overwrite a deliberate one, least of all one
  about consent, and latching it also removes the re-tick hazard that FR-047's
  re-evaluate-on-resize rule would otherwise create.

- **Q: When consent is cleared — withdrawn by the guest (FR-014f) or voided by a superseded
  document (FR-051) — what must happen before they can agree again?**
  → **A: the document must be opened again in both cases, and on the superseded-terms refusal
  the form MUST re-present it without waiting to be asked.** Reading it through is not
  required in either case — that phrase is struck from both requirements. The distinction is
  who caused the loss: a guest who cleared the box themselves knows why and reopens it when
  ready, whereas a guest whose consent was voided by an admin republish deserves to be shown
  the document that changed under them, which is what US5's superseded-terms scenario already
  described.

- **Q: The review dialog's "Registration Info" section — the event name and the ticket type —
  does not appear in the design. Keep it, or remove it?**
  → **A: removed** (Figma node 764:659, user's direction 2026-08-21). FR-017 is amended: the
  review step shows back **only the values the guest entered**. The concern was raised before the
  decision — FR-017 existed so nobody confirms a registration without being shown which ticket
  they are claiming — and the decision stands anyway. What carries that weight instead is the
  form BEHIND the dialog, which FR-015 already requires to identify the event, and the address
  itself, which is per-ticket-type: a guest reaches this form through an invitation to one
  specific ticket and cannot choose another on it. The dialog is a check on typing, not on
  selection, because there is no selection to get wrong.

### Session 2026-08-24

- **Q: Where should the "master data lives in Redis" requirement be written — into this spec,
  or its own?**
  → **A: its own feature spec.** The request that prompted this session carried two things: a
  defect in this feature's registration path, and a cross-cutting change to how master data
  (genders, and by extension the other master lists) is read. Only the first is spec 022's.
  The second touches checkout and the booking surfaces as much as registration, and it cannot
  be built without amending Constitution Principle VII, whose cacheable-surface list is closed
  and enumerates `events`, `ticket_types`, packages, `orders` and `attendees` and nothing else —
  an amendment that must update ARCHITECTURE.md, PRD.md and SCHEMA.md in the same change. Folding
  that into a guest-registration spec would bury a constitutional change inside a feature nobody
  reviews for one. Recorded here, and under *Out of Scope*, so the boundary is explicit rather
  than implied by omission.

- **Q: When a registration is submitted carrying a gender that has since been retired from the
  master list, should it be refused?**
  → **A: yes — registration validates against the ACTIVE master only, deliberately unlike
  checkout.** Checkout resolves a submitted gender against *every* known entry, retired included
  (spec 011 FR-031), because a restored booking form legitimately carries a value that was active
  when it was saved. Registration has no such case: the form is rendered fresh from the
  prerequisites call on every visit and is never restored from saved state, so a retired value
  can only arrive from a stale tab or a hand-made request. The divergence is recorded rather than
  left to be inferred, because the two paths sit in the same package and read the same table, and
  an unexplained difference invites being "harmonised" into silently accepting retired values.
  FR-022a states the rule; FR-022b names the resolution map so the refusal cannot be
  re-implemented as a zero-valued foreign key.

- **Q: What evidence closes the defect, given that it made every existing test red?**
  → **A: a new database-backed Go test pinning the retired-gender refusal, plus the existing
  suites returning green — no `e2e/specs/` change.** Principle VIII's "confirm it fails before
  you fix it" is satisfied only trivially here: the package did not compile, so every test in it
  failed, which proves nothing about genders in particular. The rule that needs a test it has
  never had is the one clarified above. It is pinned at the Go DB-backed tier and not in `e2e/`
  because retiring a gender is not reachable through any API — there is no admin CRUD for the
  master lists (migration 000013) — and `e2e/support/db.ts` deliberately excludes `genders` from
  its reset as migration-seeded master data. Reaching in to flip `is_active` from a browser spec
  would add the first master-data write to a suite built to avoid exactly that.

### Session 2026-08-24 (second)

- **Q: Should the registration form submit the gender master entry's identifier rather than its
  display name?**
  → **A: yes, the identifier** — matching the holder forms, which change the same way in the same
  release (spec 011 FR-034, clarified the same day). Registration is the simpler half of the
  change: its form is always rendered fresh from the prerequisites call and never restored, so it
  has no equivalent of spec 011 FR-031's retired-gender case and needs no readback carrying two
  values. FR-022a is unaffected in substance and only changes what it matches on — a retired
  entry's IDENTIFIER is refused where its name was refused before. FR-022b's warning survives
  the change intact and matters more under it, not less: an identifier absent from the active
  master must produce an explicit field-level refusal, never a silent resolution to a zero value,
  which would now write a foreign key to no row rather than merely failing to match a name.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Invited guest registers and receives a free e-ticket (Priority: P1)

An invited guest opens a registration link for one specific ticket type of one specific
event. They see a registration form, fill in their identity details, read the event's Terms &
Conditions through to the end and accept them, review a summary of exactly what they typed,
and confirm. They are told
the registration succeeded and that the ticket has gone to their email — the address itself is
not shown back (FR-039). Shortly after, the e-ticket
arrives in their inbox as a document they can present at the door. At no point are they asked
to pay, shown a price, or shown a payment receipt.

**Why this priority**: This is the entire point of the feature. Without it nothing else has
value, and with it alone an organiser can distribute complimentary tickets end to end.

**Independent Test**: Publish an event with one registration-only ticket type, open its form
link, submit a complete valid form, and confirm an e-ticket email arrives and the ticket code
in it resolves as `Valid` in admin ticket validation.

**Acceptance Scenarios**:

1. **Given** a published event with a registration-only ticket type that has remaining quota
   and an open sales window, **When** a guest submits the form with a valid name, email,
   phone, gender, date of birth and the Terms & Conditions accepted, **Then** the system
   records the registration, issues exactly one ticket, and sends the guest to a confirmation
   page telling them their complimentary e-ticket for that event has been generated and sent to
   their email.
2. **Given** a registration that has just succeeded, **When** the delivery email arrives,
   **Then** it is addressed to the registrant, carries the e-ticket document, carries no
   payment receipt, and states no monetary amount anywhere.
3. **Given** a successful registration, **When** an admin looks the ticket code up in ticket
   validation during the ticket type's admission window, **Then** the outcome is `Valid` and
   the irreversible `Used` transition is offered.
4. **Given** a form with any field left blank or malformed, **When** the guest attempts to
   submit, **Then** submission is refused, every offending field is marked with its own
   message in one pass, and nothing is recorded.
5. **Given** a valid form, **When** the guest presses the primary action, **Then** a review
   step shows back the exact values entered before anything is recorded, and the guest can
   dismiss it and keep editing.
6. **Given** a successful registration, **When** the guest returns to the same form link and
   submits the same email address again, **Then** the submission is accepted, a second ticket is
   issued to that address, and both tickets are independently valid at the door.

---

### User Story 2 - Registration-only ticket types are invisible to the purchase path (Priority: P2)

An admin marks a ticket type as registration-only. From that moment it disappears from every
guest-facing purchase surface, cannot be added to a ticket package, and cannot be booked or
checked out. It remains fully visible and editable in the admin console, clearly labelled so
an admin can tell at a glance which types are distributed by invitation.

**Why this priority**: Without this containment a complimentary ticket type leaks into the
paid catalogue, where a guest could buy for `0` what was meant to be issued by invitation.

**Independent Test**: Flip the flag on an existing ticket type and confirm it vanishes from
the event's guest ticket list and from the package composition picker, while still appearing
in the admin ticket type list with a registration-only label.

**Acceptance Scenarios**:

1. **Given** an event with both ordinary and registration-only ticket types, **When** a guest
   views the event's ticket selection page, **Then** only the ordinary types are listed and
   the registration-only types appear nowhere on it.
2. **Given** an admin composing a ticket package, **When** they open the ticket type picker,
   **Then** registration-only types are not offered, and a direct attempt to add one to a
   package is refused with an explanatory message.
3. **Given** a registration-only ticket type, **When** a booking or checkout request names it,
   **Then** the request is refused and no quota is deducted.
4. **Given** an admin viewing the ticket type list for an event, **When** the list renders,
   **Then** registration-only types are present and visually distinguished from purchasable
   types.
5. **Given** a ticket type that is already a member of at least one package, **When** an admin
   attempts to mark it registration-only, **Then** the change is refused and the message names
   the packages that must be edited first.

---

### User Story 3 - The form refuses anything that is not its own registration-only ticket (Priority: P2)

The form URL carries an event slug and a ticket type identifier, both of which are
attacker-supplied. The form page and the submission endpoint independently verify that the
identified ticket type exists, belongs to the named event, and is registration-only, and
refuse everything else without disclosing which condition failed.

**Why this priority**: This is the boundary that keeps the free-issuance path from becoming a
way to mint paid tickets. It is stated as its own story because hiding the form is not
enforcement — the endpoint has to refuse independently of the UI.

**Independent Test**: Take a valid registration form URL, substitute the id of an ordinary
paid ticket type, and confirm both the page and a direct submission refuse it and no ticket
is issued.

**Acceptance Scenarios**:

1. **Given** a form URL naming an ordinary purchasable ticket type, **When** the page loads,
   **Then** no form is shown and the guest is told the registration is not available.
2. **Given** a submission naming an ordinary purchasable ticket type, **When** it reaches the
   server, **Then** it is refused, no order, attendee or ticket is created, and no quota moves
   — independently of whether the UI would have allowed it.
3. **Given** a form URL whose ticket type is registration-only but belongs to a *different*
   event than the slug names, **When** the page loads or a submission arrives, **Then** both
   are refused.
4. **Given** a form URL naming an unknown ticket type, an unknown slug, or an unpublished
   event, **When** the page loads or a submission arrives, **Then** the refusal is
   indistinguishable from the cases above and discloses nothing about which ticket types or
   events exist.

---

### User Story 4 - Registrations are ordinary records in the admin console (Priority: P3)

A registration appears in the admin console alongside purchases — in the order list, in the
attendee list, in the ticket validation flow, and in the delete guards — with its zero amount
and its registration origin legible rather than looking like a purchase that somehow cost
nothing.

**Why this priority**: Operators need one place to see who is coming. Reusing the existing
surfaces is what makes the chosen order-anchored design worth its governance cost; if
registrations were invisible there, the anchoring choice would have bought nothing.

**Independent Test**: Complete one registration and one ordinary purchase for the same event,
then confirm both appear in the admin order and attendee lists with the registration
distinguishable from the purchase.

**Acceptance Scenarios**:

1. **Given** a completed registration, **When** an admin views the order list, **Then** the
   registration is listed with a zero total and an indication that it originated from a
   registration rather than a payment.
2. **Given** a completed registration, **When** an admin views the attendee list for the
   event, **Then** the registrant appears with the same detail as any purchased attendee.
3. **Given** a registration whose delivery email failed, **When** an admin uses resend,
   **Then** the same single e-ticket email is re-sent to the registrant's address.
4. **Given** an event or ticket type with at least one registration, **When** an admin
   attempts to delete it, **Then** the deletion is refused exactly as it is for an event or
   ticket type with purchases.

---

### User Story 5 - One agreement, presented and behaving identically on both surfaces (Priority: P2)

Wherever a guest agrees to an event's Terms & Conditions — registering for a free ticket or
buying a paid one — they meet the same document, the same control and the same behaviour. One
component presents the terms on both surfaces, so the two cannot drift apart in wording,
behaviour, or what agreement is taken to mean. Reaching the end of the document ticks the
agreement box for the guest; a guest who would rather tick it without reading is free to
(clarified 2026-08-21), and is never blocked from proceeding.

**Why this priority**: Two surfaces of one product that ask for the same consent in visibly
different ways is a defect a guest can see. This is P2 rather than P1 because the registration
journey (US1) is demonstrable without it, but it ships in the same change.

**Independent Test**: Open the booking Terms & Conditions dialog for an event with a long
document, confirm the checkbox is unchecked and Agree unavailable, scroll to the end, and
confirm the checkbox checks itself and Agree turns up. Reopen it and instead tick the checkbox
straight away without scrolling, and confirm Agree turns up just the same — then repeat both on
the registration form's modal.

**Acceptance Scenarios**:

1. **Given** the booking Terms & Conditions dialog freshly opened on a document longer than its
   reading area, **When** it renders, **Then** its agreement checkbox is unchecked and its
   Agree action is unavailable.
2. **Given** that same dialog, **When** the guest reaches the end of the document, **Then** the
   agreement checkbox becomes checked without the guest clicking it, and the Agree action turns
   up.
3. **Given** that same dialog still scrolled to the top, **When** the guest ticks the agreement
   checkbox themselves, **Then** it checks and the Agree action turns up, with no scrolling
   required and nothing telling the guest they must read first.
4. **Given** a dialog whose checkbox was ticked automatically at the end of the document,
   **When** the guest unticks it and then scrolls away from the end and back, **Then** the box
   stays unticked and Agree stays unavailable — the automatic tick does not fire a second time.
5. **Given** the booking dialog, **When** it is compared with its behaviour before this change,
   **Then** its layout is unchanged — the same checkbox, the same Cancel and Agree actions in
   the same places — and only the automatic tick is new.
6. **Given** the registration form, **When** the guest interacts with the agreement checkbox,
   **Then** the same Terms & Conditions presentation opens, behaving the same way, and accepting
   returns them to the form with the box checked.
7. **Given** either surface with a document short enough that its reading area cannot scroll,
   **When** it opens, **Then** the end of the document counts as already reached, so the
   automatic tick fires immediately and the guest is not left waiting for a scroll that cannot
   happen.
8. **Given** a guest using only a keyboard, or a screen reader, on either surface, **When** they
   reach the end of the document by the means available to them, **Then** the automatic tick
   fires for them exactly as it does for a pointer user.
9. **Given** a guest who accepted the terms and is still filling in the registration form,
   **When** an admin republishes that event's Terms & Conditions and the guest then submits,
   **Then** the submission is refused, nothing is recorded, no quota moves, and the form
   re-presents the current document by itself, which the guest must accept again before the
   submission can go through.
10. **Given** a completed registration, **When** its stored record is inspected, **Then** it names
   both the moment of agreement and the exact version of the document agreed to.

---

### Edge Cases

- **Quota exhausted**: the last remaining unit is taken between the page load and the submit.
  The submission is refused with an availability message, no ticket is issued, and quota never
  goes negative. Two guests submitting simultaneously for a single remaining unit produce
  exactly one ticket.
- **Sales window closed or not yet open**: the form is unavailable and a submission is refused,
  using the same refusal language the purchase path already uses for an unavailable ticket type.
- **Same email submitted twice concurrently**: both succeed. Two in-flight submissions of one
  address for one event are two independent registrations and produce two tickets, each with its
  own order and attendee. Nothing serialises them, because nothing is keyed on the address; the
  only things that can refuse either one are exhausted quota, a closed window, and the throttle.
- **Email delivery fails**: the registration and ticket still exist and the delivered flag stays
  false, so resend stays armed. The guest has already been told the ticket was sent (FR-039,
  decided deliberately), so nothing on their screen will ever correct that — recovery depends
  entirely on the resend path and on an operator being able to find the registration.
- **Ticket type made registration-only while already inside a package**: refused (US2
  scenario 5) rather than silently emptying the package's composition.
- **Admission window**: a registration ticket admits only inside its ticket type's
  `event_start`/`event_end` window with no tolerance, and reports `Not yet valid` or `Expired`
  outside it — identical to a purchased ticket. Free issuance changes nothing about validation.
- **Date of birth in the future**, or a phone number containing spaces, `+`, or hyphens:
  refused by the same rules the existing holder forms apply, with the same message text.
- **Abusive submission volume**: the endpoint is unauthenticated, consumes quota, and sends
  real mail, so it is throttled per Constitution Principle IX and the throttle is
  configurable and killable.
- **Registration-only type with a non-zero price**: the column does not imply a price. A
  registration always charges nothing regardless of the stored price, and the form never shows
  a price.
- **Terms modal opened, read to the end, then dismissed**: everything about that opening is
  discarded with the dismissal. Reopening starts with the checkbox unchecked and the automatic
  tick re-armed, matching the existing booking dialog's rule that agreement is never remembered
  across openings — so a guest is never carried onward on an acceptance they abandoned.
- **Terms document replaced while a guest is reading it, or after they accepted but before they
  submit**: the guest MUST NOT be silently registered against a version they never saw. The
  existing booking flow already refuses this case and makes the guest accept the current version
  again; registration MUST behave the same way.
- **Event has no authored Terms & Conditions**: registration is unavailable and says so
  (FR-014g), rather than rendering a form whose agreement control can never be satisfied.
- **Gender retired between the form rendering and the submit**: the submission is refused with a
  field-level validation error naming the gender, nothing is recorded, and no attendee is written
  carrying an unresolved gender reference. Registration checks the ACTIVE master only, unlike
  checkout, which accepts a retired value because a restored booking form can legitimately hold
  one (FR-022a, FR-022b).

## Requirements *(mandatory)*

### Functional Requirements

**Ticket type designation**

- **FR-001**: `ticket_types` MUST gain a boolean attribute `is_visible`, NOT NULL, defaulting
  to **true**, so every pre-existing ticket type keeps behaving exactly as it did. A type is
  registration-only precisely when `is_visible` is false.
- **FR-001a**: The column name is deliberately narrower than what the column governs, and this
  MUST be stated wherever the column is documented rather than left to be discovered.
  `is_visible = FALSE` carries all three behaviours — excluded from guest purchase surfaces,
  ineligible for package composition, and the sole kind the registration form accepts — even
  though the name suggests only the first. The consequence MUST be recorded: there is no way to
  merely hide a ticket type. Making a purchasable type invisible also makes it free to register
  for, and any admin surface offering the toggle MUST say so at the point of use rather than
  presenting it as a listing preference.
- **FR-002**: `SCHEMA.md` MUST be updated in the same commit as the migration that adds it, and
  the column comment MUST carry the FR-001a warning, not merely the word "visible".
- **FR-003**: `is_visible` MUST be independent of `price`. The system MUST NOT derive one from
  the other, and a registration MUST charge nothing regardless of the stored price.
- **FR-004**: Admins MUST be able to set and clear `is_visible` when creating or editing a
  ticket type, and the admin ticket type list MUST visually distinguish registration-only types
  from purchasable ones.
- **FR-004a**: On the admin ticket-type form, clearing `is_visible` MUST disable the Price field
  and set it to `0`, and that `0` MUST be what is saved. This is a form affordance only — the
  server MUST continue to accept and store any price for any type, deriving nothing (FR-003).
- **FR-004b**: Setting `is_visible` again MUST re-enable the Price field **empty**, refusing to save
  until a price is entered. Carrying the zero forward would republish the type onto the guest
  purchase list at no charge — the US2 leak arriving through the price field — and because FR-005
  guards only package members, an ordinary priced ticket can reach this state.
- **FR-004c**: What is required is that a price be **typed**, NOT that it be non-zero. A purchasable
  ticket priced zero is legal and stays legal (FR-003): a deliberate giveaway is a real thing an
  organiser may want. The rule forbids *inheriting* a price nobody chose, not choosing zero.
  *(Recorded because the first attempt at FR-004b banned zero outright and contradicted FR-003 —
  the existing schema test that permits a free ticket caught it.)*
- **FR-004d**: The emptied field MUST say why it is empty, at the field. An empty required box with
  no explanation is the state FR-004b exists to prevent — the admin cannot otherwise tell a price
  that was destroyed from one that was never set. The explanation MUST NOT appear on a ticket type
  being created, where nothing was cleared and the claim would simply be false.
- **FR-005**: The system MUST refuse to clear `is_visible` on a ticket type that is a member of
  at least one package, and the refusal MUST name the packages blocking it.

**Containment: registration-only types are outside the purchase path**

- **FR-006**: Registration-only ticket types MUST be excluded from every guest-facing ticket
  listing for their event, including any filtered variant of it.
- **FR-007**: Registration-only ticket types MUST be excluded from package composition — not
  offered in any admin picker, and refused if named directly in a package composition write.
- **FR-008**: Booking and checkout MUST refuse any request naming a registration-only ticket
  type, deducting no quota and creating no order.
- **FR-009**: Registration-only ticket types MUST remain fully readable and editable in the
  admin console; containment applies to the purchase path, not to administration.

**The registration form**

- **FR-010**: The system MUST serve a registration form at a per-event, per-ticket-type
  address carrying the event slug and the ticket type identifier, at
  `/events/[slug]/register/[ticketId]` — the same plural `events` segment every other guest
  route uses.
- **FR-010a**: That address sits beneath the event purchase journey's layout, so the form MUST be
  excluded from the purchase chrome that layout applies: no sales countdown, and no
  booking-progress rail. The rail names a **Payment** step, which FR-015 forbids this surface from
  presenting, and the countdown counts down a sale the registrant is not part of. Excluding it
  MUST NOT change the address, and MUST NOT remove the event chrome the page otherwise shares.
- **FR-011**: The form page MUST refuse to render a form unless the identified ticket type
  exists, belongs to the event the slug names, is registration-only, is on an event visible to
  guests, is inside its sales window, and has remaining quota.
- **FR-012**: Every refusal in FR-011 MUST be indistinguishable from the others to the guest
  and MUST disclose nothing about which ticket types or events exist.
- **FR-013**: The form MUST collect exactly one registrant: full name, email address, phone
  number, gender, and date of birth.
- **FR-014**: The form MUST present exactly one agreement control — a single checkbox reading
  that the guest agrees to the Terms & Conditions, with the document's name presented as the
  affordance that opens it. The form MUST NOT carry any other consent checkbox, and this
  feature MUST NOT introduce consent copy of its own: the entry conditions are content of the
  event's authored Terms & Conditions document.
- **FR-014a**: Interacting with the unchecked agreement control MUST open the event's Terms &
  Conditions in a modal rather than checking the box directly. The guest MUST NOT be able to
  check the box without the document having been opened. This requirement is unaffected by the
  2026-08-21 loosening (FR-014c, FR-045), which frees the checkbox *inside* the dialog: the
  document is still opened for every registering guest, and it is inside the dialog that they
  may agree without reading.
- **FR-014b**: The modal MUST render the event's currently authored Terms & Conditions,
  identifying the event the document belongs to, in a scrollable region.
- **FR-014c**: The modal's accept action MUST be available whenever the agreement control is
  checked, by whatever means it came to be checked, and unavailable while it is unchecked.
  Reaching the end of the document MUST NOT be a precondition for accepting (clarified
  2026-08-21): a guest who ticks the box without reading MUST be able to proceed, or the gate
  has merely moved from the checkbox to the button.
- **FR-014d**: When the document is short enough that its region cannot scroll, the end MUST be
  treated as already reached, so the automatic tick fires as the modal opens rather than waiting
  for a scroll that can never occur.
- **FR-014e**: Accepting MUST check the form's agreement control and close the modal. Dismissing
  or cancelling the modal MUST leave the control unchecked and MUST NOT record consent.
- **FR-014f**: The guest MUST be able to withdraw consent by clearing the agreement control,
  which MUST return the form to its unsubmittable state. Re-consenting MUST require the document
  to be opened again — clicking the cleared control reopens it, per FR-014a — but MUST NOT
  require it to be read through (clarified 2026-08-21).
- **FR-014g**: When the event has no authored Terms & Conditions, the registration MUST be
  unavailable and MUST say so, rather than presenting a form that can never be submitted. This
  matches how booking already behaves for an event with no terms.
- **FR-015**: The form MUST identify the event it registers for, and MUST NOT display a price,
  a fee, a subtotal, or any other monetary figure.
- **FR-016**: The primary action MUST be unavailable until every field is valid and the
  agreement control is checked.
- **FR-017**: Confirming MUST first present a review step showing back the exact values entered,
  and MUST record nothing until the guest confirms from that step. Dismissing the review MUST
  return the guest to their entries intact.
- **FR-017a**: The review step MUST NOT name the event or the ticket type (amended 2026-08-21;
  it previously required the ticket type to be shown). The form behind it identifies the event
  (FR-015), and the address is per-ticket-type, so a guest cannot have chosen the wrong ticket
  on this surface — the review checks their typing, not their selection. The requirement is
  stated as a prohibition rather than by omission so the section is not reinstated as an
  improvement.

**Validation**

- **FR-018**: Full name MUST be rejected when empty or whitespace-only.
- **FR-019**: Email MUST be rejected when it is not a well-formed address.
- **FR-020**: Phone number MUST be digits only, with no spaces, `+`, hyphens or parentheses,
  and MUST use the same length rule and the same verbatim message as the existing holder
  forms, so the identical input fails identically on both surfaces.
- **FR-021**: Date of birth MUST be a real date and MUST NOT be in the future.
- **FR-022**: Gender MUST be one of the active entries of the shared gender master list. The
  submitted value is the entry's IDENTIFIER, not its display name (clarified 2026-08-24, matching
  spec 011 FR-034).
- **FR-022a**: A gender identifier that exists in the master list but is no longer active MUST be refused,
  reported as a field-level validation failure alongside any other offending field per FR-024.
  This is a DELIBERATE divergence from checkout, which accepts a retired value because a restored
  booking form can legitimately carry one; a registration form is rendered fresh on every visit
  and is never restored, so it has no such case (clarified 2026-08-24). The divergence MUST NOT be
  removed in the name of consistency between the two paths.
- **FR-022b**: The submitted gender identifier MUST be checked against the ACTIVE-only master.
  The refusal of FR-022a MUST be an explicit validation failure and MUST NOT be left to emerge
  from a lookup that yields no entry. This matters MORE now that an identifier rather than a name
  is submitted: an unmatched name merely failed to resolve, whereas an unchecked identifier would
  be written straight through as a foreign key to no row.
- **FR-023**: The email address MUST NOT be checked against prior use. An address that already
  holds a place at this event — whether from a purchase or from an earlier registration — MUST be
  accepted, and each accepted submission MUST produce its own order, attendee and e-ticket, each
  independently valid at the door. No uniqueness rule of any kind applies to the address.
- **FR-023a**: Nothing in the registration write path MUST be keyed on the email address. In
  particular there MUST be no address-scoped lock and no address-existence query, so two
  submissions of one address never contend with each other and neither is refused on account of
  the other. *(This reverses an earlier decision. The prior rule and the concurrency control that
  made it race-free are removed, not merely relaxed.)*
- **FR-023b**: Because no per-address limit remains, the per-source throttle of FR-034 is the
  ONLY bound on submission volume through the unlisted link. Its refusal MUST be reported to the
  guest as a distinct, temporary condition — "wait and retry", not "fix your form" — so a
  legitimate group registrant is not sent back to edit fields that are already correct.
- **FR-024**: Every validation rule MUST be enforced on the server independently of the client,
  and a rejection MUST report every offending field in one response so a guest never fixes one
  field only to be told about the next.
- **FR-025**: Client-side messages MUST be identical to the server's for the same failure.

**Issuance**

- **FR-026**: A confirmed registration MUST atomically create the order record, its single
  line item, the attendee, and the atomic quota deduction in one transaction, all-or-nothing.
- **FR-027**: The order MUST carry a total of zero, MUST have no fees applied to it, and MUST
  be recorded at status `PAID` (see *Constitution Deviations*).
- **FR-028**: Exactly one ticket MUST be issued per registration, with a unique ticket code and
  QR, following the existing one-ticket-per-attendee rule.
- **FR-029**: No payment session MUST be opened, no gateway call made, and no QR payment code
  produced. The registration path MUST reach the payment gateway not at all.
- **FR-030**: The registration transaction MUST contain no external network call and no cache
  call, per Principles IV and VII.
- **FR-031**: `ticket_types.quota` MUST never go negative; a registration that would exhaust
  it past zero MUST be refused with nothing recorded.
- **FR-032**: After the transaction commits and outside it, the affected cached lists —
  ticket types for the event, orders, and attendees — MUST be invalidated.
- **FR-033**: The registration MUST be distinguishable from a purchase, so admin surfaces can
  label it and so no future report mistakes it for revenue. The distinction MUST be **derived**,
  not stored: an order is registration-originated exactly when it carries a line for a ticket type
  that is not guest-visible (revised 2026-08-20; Constitution Principle IV v6.0.0 was amended to
  withdraw the marker it previously mandated).
- **FR-033a**: The derivation MUST NOT be replaced by an inference from a zero total or from the
  absence of a payments row. A purchasable ticket type priced at zero is explicitly legal (FR-003),
  and an absence is something an abandoned purchase also exhibits — either rule would misclassify.
- **FR-033b**: The consequence MUST be recorded rather than mitigated, because nothing in the
  schema can prevent it: a derived classification reports what is *currently* true, not what
  happened. Making an invitation ticket type purchasable again reclassifies every historical order
  that used it, and because the delivery path reads this, a **resend** of an already-delivered
  registration would then render a receipt for an order that never had a payment. This was raised
  before the decision and the derivation was chosen anyway. A test MUST pin the behaviour so it
  reads as chosen rather than overlooked.
- **FR-034**: The submission surface MUST be throttled with a threshold that is deployment
  configuration, killable by both the master switch and its own switch, and the documentation
  for that switch MUST state that disabling it leaves an unauthenticated surface that consumes
  quota and sends mail — **and that, since FR-023 removed the per-address rule, nothing else
  bounds submission volume at all.** The throttle MUST be enabled by default. Its threshold MUST
  accommodate the now-legitimate case of one person registering a group from one device in
  succession, rather than being tuned as though repeat submission from one source were
  necessarily abuse. Concretely, the default MUST allow at least 10 submissions from one source
  back to back. The sustained rate MUST NOT be raised alongside it: the burst governs how large a
  legitimate group may be, the sustained rate governs bulk abuse, and only the former changed.
- **FR-034a**: Raising the burst MUST NOT break the invariant the configuration loader already
  enforces — that a full token refill completes well inside the idle eviction TTL, so a limiter
  is never evicted while still holding debt. Startup already refuses a configuration that
  inverts that relationship, and this change MUST keep passing that check rather than relaxing
  it.

**Delivery**

- **FR-035**: Exactly one email MUST be sent, to the registrant's address alone, carrying the
  e-ticket document.
- **FR-036**: That email MUST NOT carry a payment receipt, in the body or as an attachment, and
  MUST state no monetary amount (see *Constitution Deviations*).
- **FR-037**: The delivered flag MUST be set only after successful delivery; a failure MUST
  leave it false so resend stays armed.
- **FR-038**: Admin resend MUST work for a registration, targeting the registrant's single
  address, and is the **only** recovery path a registrant can be told to use for undelivered
  registration mail. Recovery runs through the Help contact already present in the page header
  and an operator acting on FR-053.
- **FR-038b**: The claim that guest self-service resend is *unreachable* for a registration is
  **withdrawn as inaccurate**, and this correction is recorded rather than quietly deleted.
  `POST /ticket/resend-email` resolves **any** order number, including a registration's — it
  does not check how the order originated (research D18). What is true is narrower and weaker:
  the confirmation page shows the registrant no order number (FR-039), so a registrant has
  nothing to quote and will not find that route. **That is obscurity, not enforcement**, and the
  specification MUST NOT describe it as a boundary. Anyone who obtains or guesses a registration's
  order number can trigger a resend to the registrant's address, bounded only by that endpoint's
  own cooldown. Two consequences follow and are accepted deliberately: nothing here needs
  building to *close* the path, and nothing anywhere may be justified by assuming it is closed.
- **FR-038a**: Because FR-038 makes admin resend the sole recovery route, it MUST be reachable
  for a registration wherever it is reachable for a purchase, and MUST NOT depend on the
  registration having a payment, a receipt, or a non-zero total.
- **FR-039**: The confirmation MUST read: "Thank you for registering. Your complimentary
  invitation e-ticket for *[event name]* has been successfully generated and sent to your
  email." — under a success mark and a "Registration Complete!" heading, with the event's name
  interpolated. It MUST NOT display the registrant's email address.
- **FR-039e**: The copy above asserts a delivery that has not completed when the page renders,
  because delivery runs after the response and can fail (FR-037). This is accepted deliberately.
  Two consequences follow and MUST be handled rather than discovered: a registrant whose
  delivery fails has been told it succeeded, so the resend path is the only thing standing
  between them and a lost ticket and MUST work (FR-038); and because the address is not shown
  back, a mistyped address is not detectable by the guest, so operators MUST be able to find a
  registration by name or partial email, not only by exact address (FR-053).
- **FR-039a**: A successful registration MUST send the guest to
  `/events/[slug]/register/[ticketId]/success`, a distinct address, so the form is behind them.
- **FR-039b**: That address MUST be safe to reload: reloading MUST re-show the confirmation and
  MUST NOT re-submit anything or present an empty form.
- **FR-039c**: The confirmation MUST identify the event the guest registered for and MUST state
  that the e-ticket goes to their email. It MUST NOT display a price, a fee, a payment status,
  or a receipt.
- **FR-039d**: Because the confirmation carries no reference to a registration (FR-039), the
  page cannot distinguish a guest who just registered from anyone who typed the address, and it
  MUST NOT attempt to — no guessing from history, referrer, or client-held state that a reload
  would lose. It therefore renders the same panel to both. Accepted consequence, stated so it is
  not later filed as a defect: the address is publicly reachable and always reads "Registration
  Complete!". Nothing is disclosed by this, because the panel contains no registrant data — only
  the event's name, which the event's own public pages already carry.

**Shared Terms & Conditions presentation (both surfaces)**

- **FR-043**: One shared presentation MUST serve the event's Terms & Conditions on both the
  registration form and the booking flow, so the two cannot diverge in wording, in gating
  behaviour, or in what agreeing is taken to mean.
- **FR-043a**: That sharing MUST extend to the ENTIRE dialog — frame, header spacing, the document
  region, the rule above the actions, the agreement checkbox, Cancel, and Agree — not only to the
  document inside it. The two surfaces MUST be indistinguishable to a reader comparing them.
  *(Corrected 2026-08-20 after a screenshot: an earlier version of this requirement exempted the
  footer, on the reasoning that booking needs checkbox + Cancel + Agree while registration only
  needs to accept. The result was a single small right-aligned "Accept" against booking's checkbox
  and two full-width actions — visibly a different dialog on the same product. An exemption is
  where drift lives, which is the whole lesson of FR-043b.)*
- **FR-043c**: The ONLY thing either surface may vary is what pressing Agree does, and the label
  while a press is in flight — booking reads "Booking…" and then "Retry" on failure, because it is
  placing an order; registration has nothing to wait for and always reads "Agree". The idle label,
  the disabled affordance and every other element MUST come from the shared component, and the
  shared component MUST derive the action's availability from the agreement control's checked
  state so the two surfaces cannot answer differently.
- **FR-043b**: The holder identity fields — full name, email, phone, gender, date of birth — MUST
  likewise be ONE shared component rendered by both surfaces, not two implementations sharing only
  their validation rules. Sharing the rules while duplicating the markup is what allowed a label or
  placeholder corrected on one surface to stay wrong on the other with nothing reporting it; the
  registration form carried a phone placeholder of eleven digits that its own twelve-digit rule
  would have refused.
- **FR-044**: The booking dialog MUST keep its existing layout — its agreement checkbox and its
  Cancel and Agree actions, in their existing positions and with their existing labels. This
  change adds the automatic tick to that dialog; it MUST NOT restyle it or move its controls.
- **FR-045**: In the booking dialog, reaching the end of the document MUST check the agreement
  checkbox automatically. The guest MUST also be able to check it themselves at any moment,
  including before reaching the end (clarified 2026-08-21) — the automatic tick is a
  convenience for the guest who did read to the end, not a condition on the ones who did not.
  The checkbox MUST start unchecked, and Agree MUST follow the checkbox per FR-014c.
- **FR-045a**: The automatic tick MUST fire at most once per opening of the dialog, the first
  time the end is reached, and MUST NOT fire again for that opening. Once the guest has changed
  the checkbox themselves, their choice MUST stand until the dialog closes: further scrolling,
  re-reaching the end, or resizing the reading area MUST NOT re-tick a box the guest cleared.
  The automatic behaviour MUST NEVER untick the box — scrolling back up from the end MUST leave
  it as it is.
- **FR-046**: The end-of-document condition that drives the automatic tick MUST be reachable by
  every means a guest reads with — pointer scrolling, keyboard paging and the End key, and
  assistive technology — and MUST NOT depend on a pointer-generated scroll event. A guest who
  cannot produce a scroll gesture MUST still get the automatic tick on reaching the end, and in
  any case MUST be able to check the box directly (FR-045).
- **FR-047**: The end-of-document condition MUST tolerate sub-pixel and zoom-related rounding, so
  a guest who has visibly reached the end is recognised as having done so and is not denied the
  automatic tick over a fractional pixel. It MUST re-evaluate when the reading area is resized —
  but only while the automatic tick is still pending, since per FR-045a it fires at most once and
  never after the guest has set the checkbox themselves.
- **FR-048**: Because the booking dialog belongs to a Principle VIII covered flow, the affected
  `e2e/` scenarios MUST be updated in the same change and MUST assert BOTH routes deliberately:
  reaching the end as an explicit step and observing the automatic tick, and separately ticking
  the checkbox without scrolling and observing that Agree is available all the same. Neither may
  pass merely because a short fixture document never needed scrolling.
- **FR-049**: A registration MUST durably record that the guest agreed and **which version** of
  the Terms & Conditions they agreed to, using the same fields the purchase flow already writes
  (`orders.terms_agreed_at`, `orders.event_terms_id`). Recording only that they agreed, without
  the version, MUST NOT be considered sufficient.
- **FR-050**: The submission MUST carry the document version the guest accepted, and the server
  MUST refuse it when that version is no longer the event's current document. The refusal MUST
  be distinguishable from other failures so the form can respond by re-presenting the document,
  rather than reporting a generic error.
- **FR-051**: On that refusal nothing MUST be recorded, no quota MUST move, and the form MUST
  clear its agreement control. It MUST then re-present the current document itself, without
  waiting for the guest to click the cleared control (clarified 2026-08-21): the guest did not
  choose to lose their consent, and the document changed under them. Agreeing again MUST be a
  fresh acceptance against the current version; reading it through MUST NOT be required. A guest
  MUST NOT be able to resubmit a superseded acceptance by retrying.

**Existing guarantees that must continue to hold**

- **FR-040**: Ticket validation MUST treat a registration ticket identically to a purchased
  one, including the admission window outcomes and the irreversible used transition.
- **FR-041**: Public ticket lookup by ticket code MUST resolve a registration ticket.
- **FR-042**: The delete guard on events and ticket types MUST refuse deletion when a
  registration exists, exactly as it does for a purchase.
- **FR-053**: Because the guest is never shown the address their ticket went to (FR-039), an
  operator MUST be able to locate a registration without knowing the exact address — by
  registrant name, or by partial email match, scoped to the event. A recovery path that requires
  the guest to correctly recite an address they may have mistyped is not a recovery path.

### Key Entities

- **Ticket Type**: gains `is_visible` (false ⇒ registration-only) — whether this type is obtained by registering
  rather than buying. Governs guest-list visibility, package eligibility, booking eligibility,
  and which registration forms will accept it. Independent of `price`.
- **Registration Order**: a zero-total order at `PAID` with no payment session, no fees and no
  receipt, created by the registration path. Anchors the attendee and the ticket so every
  order-keyed read path continues to work.
- **Attendee**: the registrant — name, email, phone, date of birth, gender — the single holder
  of the issued ticket. The address is stored and delivered to; it is never consulted to decide
  whether a registration is permitted.
- **Ticket**: the issued e-ticket, identical in every respect to a purchased one: unique code,
  QR, active/used/revoked status, and the same admission window rules.
- **Event**: unchanged; scopes the form address and which Terms & Conditions document applies.
- **Terms & Conditions Document**: the event's single authored, admin-maintained document —
  unchanged by this feature in how it is written, sanitized, versioned or stored. This feature
  changes only how it is presented and consented to, and it is now where the entry conditions that were
  previously drafted as form checkboxes live as document content.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: An invited guest completes registration from an unopened link to a submitted form
  in under 90 seconds, without encountering any price, payment step or receipt.
- **SC-002**: 100% of successful registrations result in exactly one issued ticket, and 0%
  result in more than one. Concurrent submissions of the same address are two registrations, not
  one, and correctly yield two tickets; what is forbidden is a single submission yielding two.
- **SC-003**: The e-ticket email reaches the registrant within 2 minutes of confirmation in
  95% of registrations, and the code it carries validates at the door on first scan.
- **SC-004**: 0 registration-only ticket types appear on any guest purchase surface or in any
  package, verified across both cache modes.
- **SC-005**: 100% of submissions naming a ticket type that is not registration-only, or not
  part of the named event, are refused with nothing recorded — including submissions that
  bypass the user interface entirely.
- **SC-006**: Remaining quota after N successful registrations equals the pre-registration
  remainder minus N, and never falls below zero under any concurrency.
- **SC-007**: 100% of validation failures report every offending field in a single response, so
  a guest correcting all of them succeeds on their next attempt.
- **SC-008**: 0 registrations produce a payment record, a fee line, a receipt document, or a
  monetary figure on any guest-facing surface or in any email.
- **SC-009**: Every existing ticket type behaves identically before and after the migration, and
  every existing purchase flow scenario passes unchanged. This is the check that catches an
  inverted default: `is_visible` defaults to **true**, and a migration that copies the previous
  column's `DEFAULT FALSE` would turn every ticket type in the system registration-only at once —
  every event's ticket list emptied, every purchase refused, with no error raised anywhere. The
  migration MUST therefore be verified against a database holding pre-existing ticket types, not
  only against an empty one.
- **SC-010**: An admin can locate any registrant and their ticket in the admin console within
  30 seconds of learning their email address.
- **SC-011**: 0% of registrations and 0% of purchases can record an agreement without the event's
  Terms & Conditions having been presented to the guest, on either surface, by any input method —
  and, conversely, 0% of guests who decide to agree are prevented from doing so by not having
  reached the end of the document (revised 2026-08-21; the earlier form of this criterion
  measured the read-through gate, which no longer exists).
- **SC-012**: A guest using only a keyboard, and a guest using a screen reader, can each complete
  the agreement step on both surfaces without assistance.
- **SC-013**: The Terms & Conditions read identically, and behave identically, on the registration
  form and in the booking flow — verified by both surfaces exercising the same presentation
  rather than by comparing two implementations.
- **SC-014**: 0% of guests who reload the confirmation page, or navigate back to it, are shown an
  error or an empty form.
- **SC-015**: An operator given only a registrant's name, or a partial email, can locate that
  registration and resend its e-ticket within 2 minutes — the sole recovery route for
  undelivered registration mail.
- **SC-016**: 0 unauthenticated surfaces allow a caller to discover whether an arbitrary email
  address holds a ticket at an event — true by construction once FR-023 lands, since no
  registration path consults an address's prior use. Sending mail to an address the caller does
  not control remains possible through the unlisted link, as it was before; 100% of submissions
  beyond the configured per-source threshold are refused, and that throttle is the only bound.
- **SC-017**: 100% of submissions naming a gender that is no longer offered are refused with a
  field-level validation error and record nothing, and 0 registrants are stored carrying an
  unresolved gender reference (FR-022a, FR-022b; clarified 2026-08-24).

## Constitution Deviations Requiring Amendment

Governance requires deviations to be justified in the spec or rejected. This feature carries
two, both decided deliberately. **Neither may be implemented before the constitution is
amended in the same change, together with `PRD.md`, `ARCHITECTURE.md` and `SCHEMA.md` as
Governance requires.**

1. **Principle IV — "no path other than a gateway webhook may move an order to `PAID`."**
   FR-027 has the registration path write `PAID` directly. *Justification*: ticket issuance,
   ticket lookup, e-ticket rendering, resend, admin listing and validation are all keyed on
   `PAID`; introducing a separate terminal status would require touching every one of them and
   would leave a second "issued" state that every future read path must remember. *Cost stated
   honestly*: `PAID` stops meaning "a gateway confirmed money moved". The amendment MUST
   redefine that invariant rather than leave the principle contradicted by shipped code, and
   FR-033 exists so the two origins remain distinguishable despite sharing a status — by
   derivation from the ticket type, the marker having been withdrawn in v6.0.0.
   The webhook-only rule MUST remain absolute for orders that *have* a payment session.

2. **Critical Data Flow Rules — "exactly two document attachments... neither document may be
   sent without the other."** FR-035/FR-036 send the e-ticket with no receipt. *Justification*:
   a receipt for a zero-amount registration would be a proof of payment for a payment that
   never happened. *Amendment shape*: the two-attachment rule is a rule about **paid orders**;
   the amendment MUST scope it that way and state what a registration sends instead. The
   supporting constraint that the e-ticket document carries no monetary figure is already
   satisfied and is what makes this separation clean.

Additionally, **Principle VIII** makes `e2e/` coverage a merge gate: this is a new
user-visible flow, so it arrives with scenarios covering the registration journey end to end
(form → validation → review → submit → delivered e-ticket → validation at the door) and the
containment boundary (registration-only type absent from the purchase list and package picker;
a purchasable ticket type refused by the form endpoint). Those scenarios MUST pass with the
cache both enabled and disabled, and with throttling both enabled and disabled.

## Assumptions

- **The registration address is `/events/[slug]/register/[ticketId]`**, decided explicitly
  (Clarifications, superseding an earlier singular-`event` decision). It uses the same plural
  segment every other guest route uses, so there is one convention rather than two sibling
  top-level segments differing by one character. The cost of the reversal is recorded rather than
  glossed: the address is no longer distinguishable from a purchase surface by inspection, and
  the route inherits the purchase journey's layout — which is why FR-010a exists rather than the
  page being left to render whatever the layout gives it.
- **Phone rule is inherited, not reinvented.** "Phone number only number" is implemented as the
  existing holder-form rule — digits only, 12–15 of them, counted exactly as typed — sharing
  the message text verbatim. The Figma mock shows a `+62…` value in a filled state; that value
  would be rejected under the inherited rule, and the inherited rule wins, because two
  different phone rules on two forms of the same product is a defect.
- **Gender and date of birth are required.** The description lists only three validations, but
  the design shows both fields, `attendees` stores both, and the ticket-holder identity the
  e-ticket carries is the same shape as a purchased one. Both are required.
- **One ticket per submission.** The form collects a single holder and issues a single ticket.
  Registering a group means submitting the form once per person.
- **Registration-only ticket types are priced at zero by the admin FORM (FR-004a), not by the
  system.** No constraint enforces it and
  nothing reads it; FR-003 makes the registration charge nothing either way.
- **Ticket types remain the unit of designation.** There is no event-level "this event is free"
  switch; an event may carry any mix of purchasable and registration-only types.
- **The registration link is unlisted, not secret.** Anyone holding the URL can register while
  quota and the sales window last. Per-invitee tokens, invite codes and allow-lists are out of
  scope; quota, the sales window and the per-source rate limit are the only limits. One person
  may register a whole group by submitting the form once per person from one device, and that is
  now a supported use rather than something the duplicate rule made impossible.
- **Existing infrastructure is reused unchanged**: the gender master list, the e-ticket document
  renderer, the mail transport, the ticket code and QR generator, the admission-window
  validation rules, and the resend paths.
- **The Terms & Conditions presentation is reused by extraction, not duplication.** The booking
  flow's existing dialog is the starting point; the shared part becomes one component serving
  both surfaces — extended 2026-08-20 from the document alone (`TermsViewer`) to the whole dialog
  frame (`TermsDialogShell`), so the two cannot differ in width, padding or header spacing either.
  The holder form is shared the same way (`HolderFields`). The booking dialog's own responsibilities beyond presentation — holding the
  order, recording the agreement, and navigating onward — stay where they are and are not
  inherited by the registration form, which has no order to hold at that moment.
- **Presenting the document is a presentation rule, not an enforcement boundary.** Neither the
  opening of the document nor the reaching of its end can be verified by a server, and the spec
  does not pretend otherwise: they exist so a guest is not asked to consent to something they
  were never shown, not as protection against a determined caller. Since 2026-08-21 the guest
  may in any case agree without reading, so nothing here was ever load-bearing. Server-side, what is recorded is the agreement and the document version it was made
  against — the same guarantee the purchase flow already has.

## Out of Scope

- Any consent surface of this feature's own. Consent reuses the existing per-event Terms &
  Conditions document and its admin authoring surface; the entry conditions are written there as
  document content. A separate per-ticket-type consent document, or consent copy distinct from
  the event's terms, is out of scope.
- Any change to how the Terms & Conditions document is authored, sanitized, versioned, or
  stored. This feature changes only how it is *presented and consented to*, on both surfaces.
- Per-invitee invitation tokens, invite codes, allow-lists, or any authentication of the
  registrant.
- Cancelling, transferring, or self-editing a registration after submission.
- Guest self-service resend for a registration, and any surface that would let an unauthenticated
  caller discover or re-trigger mail for an arbitrary email address. Recovery is operator-assisted
  (FR-038).
- Bulk or CSV registration, and admin-initiated registration on a guest's behalf.
- Registering for more than one ticket, or more than one ticket type, in a single submission.
- Any change to how paid orders, fees, payments, receipts or the gateway behave.
- **Caching the gender master list, or any other master data, in Redis.** Requested alongside this
  feature's gender defect and deliberately separated from it (clarified 2026-08-24): the cacheable
  surfaces are closed by Constitution Principle VII and admitting a new one requires amending the
  constitution together with ARCHITECTURE.md, PRD.md and SCHEMA.md. It also spans checkout and the
  booking surfaces, not just registration. Registration therefore continues to read the master list
  directly from PostgreSQL, and MUST keep behaving identically if that read is later served from a
  cache — no correctness of this feature may come to depend on one.

# Feature Specification: Receipt + E-Ticket Email Attachments

**Feature Branch**: `feat/receipt`

**Created**: 2026-08-12

**Status**: Draft

**Input**: User description: "currently the email only sent a single attachment which is only e ticket, now it must send 2 file of attachments, receipt and e ticket (multiple tickets will be merged into single file pdf). here's the body design: Figma 741-5105. The Attachments: 1. Receipt — Figma 688-671. 2. E Tickets — Figma 683-148, 683-261, 683-379."

## Overview

Today a paid order produces exactly one email to the buyer carrying exactly one
attachment: a PDF of the order's e-tickets, one page per ticket. The receipt — the
itemized proof of payment — exists only as HTML inside the email body, so a buyer who
needs it for reimbursement, for an expense claim, or as a durable record has nothing to
file or forward on its own.

This feature splits proof-of-payment from proof-of-admission. The same single email to
the same single recipient now carries **two** attachments — a Payment Receipt document
and an E-Ticket document — and the email body is rewritten to the new design: a short
confirmation with the order summary, the event details, the itemized totals, the buyer's
contact details, and the entry rules, with the per-ticket cards removed from the body
because they now live in the attachment.

The E-Ticket document keeps today's behaviour of merging every ticket in the order into
one file, and gains the "Ticket N of M" pagination the design calls for. It carries no
money at all — price belongs to the receipt.

**Not in scope**: the number of emails sent (still exactly one, to the order's primary
contact), who receives it, when it is sent, ticket issuance, ticket codes, quota, fee
computation, per-event branding, or any change to what is stored in the database.

## Clarifications

### Session 2026-08-12

- **Q**: The e-ticket design showed a per-ticket price with a "Price includes Tax and
  Platform Fee" note, but fees are frozen per order, not per ticket. Which figure should
  the page print? → **A**: Neither. The design was revised to remove the price row and its
  note entirely; the e-ticket ends at the admission window and shows no money (FR-021).
- **Q**: Should the buyer's email and phone be masked or printed in full on the email body
  and the receipt? → **A**: Both surfaces render the same `buyer_email` (and the same buyer
  phone) from one source; the two placeholder addresses in the mocks are mock noise, not two
  different fields. Both surfaces mask, under one identical rule (FR-032, FR-033).
- **Q**: Should the brand mark, website, support address and platform attribution be
  per-event or platform-wide? → **A**: Platform-wide static configuration. Only the event's
  own name, venue and address vary per order (FR-035, FR-036).

### Session 2026-08-12 (design review)

Raised after the first implementation was rendered and compared against Figma.

- **Q**: Should the email body's layout use CSS flexbox/grid, given that Outlook on
  Windows renders with the Word engine and supports neither? → **A**: No — Option A. Keep
  nested tables and fix the spacing properly (consistent cell padding, right-aligned amount
  column, no border under Total Payment). The reported spacing defects are real and are
  fixed within the table layout, which is also what the design's own nested
  "Table Row"/"Table Cell" frame structure describes (FR-031, FR-026).
- **Q**: How should the real JIVE logo be carried in the email body, given it conflicts
  with the "exactly two attachments" rule? → **A**: Option A — a CID inline image
  (embedded MIME part), which renders in Outlook, Gmail and Apple Mail where a remote
  `<img>` is blocked by default and a `data:` URI is stripped by Gmail. The
  attachment-count rule is restated as **exactly two DOCUMENT attachments**; inline images
  are not documents and are not counted (FR-001, FR-023a, SC-001).
- **Q**: Which currency format, given the email mock shows `IDR 70,000` and the receipt
  mock shows `Rp70.000`? → **A**: Option B — `IDR 70,000` on every surface: the `IDR`
  prefix with comma thousands separators.
  **~~SUPERSEDED~~ later the same day** — see the currency entry at the end of this
  session. The `IDR` prefix survives; the comma-thousands separator does not. Retained
  here because the reversal's reasoning only makes sense against what it replaced.

Recorded from the same review, without needing a question — each was an unambiguous
correction against the designs:

- Times on every surface render in Asia/Jakarta with a `WIB` suffix, following the
  frontend's existing convention (FR-020a, FR-024b).
- The e-ticket drops the venue row (FR-019a) — the design has no such field. Noted as a
  deliberate trade: a holder at a gate no longer has the address on the pass.

Three further defects reported after the implementation was rendered. All three were
unambiguous corrections, so none needed a question:

- **Email body**: the dashed rule between the ticket lines and the fee rows is drawn even
  when the order has no fees, leaving a separator with nothing on one side of it
  (FR-026c).
- **Receipt**: the envelope icon sits ~2 mm above the support address rather than on its
  line (FR-015b). Root cause found during the fix: gofpdf's `MoveTo` moves the current
  cursor, so reading `GetY()` after drawing the icon returned the icon path's own Y and
  pushed the text down — the arithmetic was right and the reference point was not.
- **E-ticket**: the accent rule beside the QR must be `#334155`, not red, and rounded on
  its RIGHT corners only, since it sits flush against the left page edge (FR-022b).
- **Receipt**: the product table's left and right padding differ — the product column is
  inset from the table edge while the amounts right-align flush against it (FR-011b).
- **Receipt**: the `Price` column header is right-aligned and sits visibly off to one side
  of the figures beneath it; it should be centred over its column (FR-011c).
- **Email body**: the message does not fit a phone screen — the card is pinned to the
  design's 600px frame, so a 375px viewport shows its left portion and clips the rest
  (FR-031b).

Reopened later the same day, after the earlier currency answer proved incomplete:

- **Q**: Which complete number format should the documents use, now that amounts can
  carry decimals (a stored `15000.92` must print its `92`)? → **A**: Option A — full
  Indonesian: the `IDR` prefix with **dot thousands and comma decimal**, and the fractional
  part shown only when it is non-zero. `IDR 550.000` and `IDR 15.000,92`.

  This **partially reverses** the earlier answer in this same session, which took the email
  mock's `IDR 70,000` comma-thousands styling. The reversal is deliberate: the site's own
  formatter already carries a comment explaining that comma-as-thousands *"would misstate
  the sum they are about to pay"* for these buyers, and a receipt is where a misread total
  does the most damage. The documents now differ from the site in the **prefix only**
  (`IDR` vs `Rp`), not in the separators (FR-037, FR-037a).

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Buyer receives a filable receipt alongside the tickets (Priority: P1)

A guest completes a QRIS payment. Moments later a single email arrives. It carries two
files: a Payment Receipt showing who bought what, at what price, with which fees, for
what total; and an E-Ticket file with one page per ticket in the order, each carrying its
own QR code and ticket code. The buyer forwards the receipt to their finance team and
prints the e-tickets for the group.

**Why this priority**: This is the entire ask. Without it, the buyer has no receipt they
can file or forward — the current email body cannot be detached from the email. Shipping
this alone, with the existing body untouched, already delivers the value.

**Independent Test**: Complete a purchase end to end, settle it, and inspect the
delivered email — it carries exactly two attachments, one receipt and one e-ticket file,
with the e-ticket file holding exactly one page per issued ticket.

**Acceptance Scenarios**:

1. **Given** a paid order with 3 tickets across 2 ticket types, **When** the ticket email
   is delivered, **Then** the email carries exactly 2 document attachments — a receipt
   document and an e-ticket document — and the e-ticket document has exactly 3 pages.
2. **Given** a paid order with 1 ticket, **When** the ticket email is delivered, **Then**
   it still carries exactly 2 document attachments, and the e-ticket document has 1 page
   labelled "Ticket 1 of 1".
3. **Given** a paid order, **When** the receipt document is opened, **Then** every
   purchased line, every frozen fee line, the pre-fee subtotal, and the grand total appear
   and the grand total equals the amount the buyer was charged.
4. **Given** a paid order, **When** the e-ticket document is opened, **Then** each page
   shows a QR code that resolves to that ticket's own code, and no two pages carry the
   same ticket code.
5. **Given** a paid order, **When** the e-ticket document is opened, **Then** no page shows
   a price, a fee, or a total.

---

### User Story 2 - Email body reads as the new confirmation design (Priority: P2)

The buyer opens the email itself. It greets them by name, tells them the tickets have been
issued, and shows — in one glance — the order's status, number, date and payment method;
the event with its venue and address; the itemized ticket lines with fees and total
payment; their own contact details as recorded; and the rules for getting in. The
per-ticket cards that used to fill the body are gone, because the tickets are attached.

**Why this priority**: The body is what the buyer reads first, and the current body
duplicates content that now lives in the attachments. It is separable from P1 — the
attachments are correct and useful whichever body ships — so it follows rather than
blocks.

**Independent Test**: Deliver a ticket email and read the rendered body against the
design: every section is present, in order, populated from the real order.

**Acceptance Scenarios**:

1. **Given** a paid order, **When** the email body is rendered, **Then** it contains, in
   order: the header band, the confirmation headline and greeting naming the buyer, the
   order status panel, the event block, the ticket details with totals, the buyer
   information block, the important-information notice, and the automated-email footer.
2. **Given** a paid order, **When** the email body is rendered, **Then** it contains no
   per-ticket card, no QR code, and no ticket code — those appear only in the attachment.
3. **Given** an order whose event has a venue and address, **When** the body is rendered,
   **Then** both appear under the event name.
4. **Given** an order carrying frozen fee lines, **When** the body is rendered, **Then**
   each fee line appears under the ticket lines with the name frozen onto the order, and
   the total payment appears last and in the brand accent.
5. **Given** a paid order, **When** the body and the receipt are compared, **Then** the
   buyer name, email and phone are identical on both, masked identically.

---

### User Story 3 - A resend delivers the identical pair (Priority: P3)

A buyer loses the email. They use the resend on the confirmation screen, or support uses
the admin resend. The email that arrives is the same email: same body, same two
attachments, same ticket codes, same receipt figures.

**Why this priority**: Resend already exists and already shares one delivery path with the
automatic send. This story is the guarantee that the split into two attachments does not
give the two paths a reason to drift — valuable, but it is a property of P1 rather than
new capability.

**Independent Test**: Settle an order, capture the delivered email, resend it, and compare
— both carry two attachments, and the ticket codes in the e-ticket document are byte-for-
byte the codes issued the first time.

**Acceptance Scenarios**:

1. **Given** a paid order whose tickets have already been delivered, **When** the buyer
   requests a resend, **Then** the email carries the same two attachments and the ticket
   codes are unchanged from the original delivery.
2. **Given** a paid order, **When** an admin resends, **Then** the receipt document shows
   the same figures as the original — a fee master-data edit made after the order does not
   change them.

---

### Edge Cases

- **Amount with a non-zero fractional part** (e.g. a percentage fee computing to
  `15000.92`): the fraction is printed with a comma decimal, and the row still reconciles
  with the total. Whole amounts in the same document print no fractional part, so a single
  receipt can legitimately show `IDR 550.000` and `IDR 15.000,92` side by side.
- **Order with no fee lines** (predates order fees, so no pre-fee subtotal is recorded):
  the receipt and the body collapse the breakdown to the total payment alone, and no
  subtotal or fee row is invented.
- **Order containing a package/bundle line**: the line is priced once at the package price
  on the receipt, with no invented per-constituent allocation. The e-ticket pages for that
  package's holders are unaffected, because e-tickets carry no price at all.
- **Two holders sharing one name, or one holder holding several tickets**: the e-ticket
  document still has exactly one page per issued ticket, and the "N of M" numbering runs
  1..M with no gaps or repeats.
- **Ticket type whose admission window spans more than one day**: the e-ticket's
  "valid for" line shows the full window, not just the opening instant.
- **Ticket types with different admission dates in one order** (a Day 1 and a Day 2 pass):
  each e-ticket page shows its own ticket type's date, and each receipt line shows its own
  line's date — never the parent event's opening date.
- **Buyer with no recorded phone**: the buyer-information block and the receipt omit the
  phone row rather than printing an empty, placeholder, or fully-masked value.
- **Contact value too short to mask under the standard rule** (a very short local part or a
  short phone): it is masked from its second character onward rather than printed in full.
- **Very long event name, venue, address, or attendee name**: the value wraps within the
  document rather than overflowing the page or being silently truncated.
- **Order that is not paid**: no email and no documents are produced — unchanged from
  today.
- **Attachment generation fails** (either document): no email is sent, the order is not
  marked as delivered, and the resend stays armed — a half-delivered email carrying only
  one of the two documents is never sent.

## Requirements *(mandatory)*

### Functional Requirements

#### Delivery

- **FR-001**: The ticket email MUST carry exactly two **document** attachments: one
  Payment Receipt document and one E-Ticket document. No other document is included.
  Inline images referenced by the body (the brand mark, per FR-023a) are message parts,
  not documents, and are excluded from this count — the rule exists so a buyer never has
  to work out which file to open, and a decorative image does not affect that.
- **FR-002**: The E-Ticket document MUST contain every ticket in the order merged into a
  single file, one page per issued ticket, in the order's canonical ticket order.
- **FR-003**: Both attachments MUST be produced in a portable, printable page format
  (A4 portrait) that opens without additional software on desktop and mobile mail clients.
- **FR-004**: Each attachment's file name MUST identify the order it belongs to and
  distinguish the receipt from the e-tickets, so two files saved to one folder never
  collide or become ambiguous.
- **FR-005**: Delivery remains exactly one email to the order's primary contact. This
  feature MUST NOT change the recipient, the number of emails, or the trigger.
- **FR-006**: If either document cannot be produced, the email MUST NOT be sent, the
  order MUST NOT be marked as delivered, and the resend MUST remain available.
- **FR-007**: A resend MUST produce the same two attachments with the same already-issued
  ticket codes; ticket codes MUST NOT be regenerated by any send.

#### Payment Receipt document

- **FR-008**: The receipt MUST be titled "Payment Receipt" and MUST show the order number
  and the time the order was last updated.
- **FR-009**: The receipt MUST show an Order Details block containing the customer name,
  phone number, and email address as recorded on the order.
- **FR-010**: The receipt MUST show a Transaction Details block containing the payment
  status, the payment method, and the payment date and time.
- **FR-011**: The receipt MUST itemize every purchased line in a table of Product, Price,
  Quantity and Total, where Product carries the line's name and, beneath it, the line's
  admission date and ticket-type descriptor.
- **FR-012**: The receipt MUST show the pre-fee subtotal, then each frozen fee line under
  the name frozen onto the order at booking, then the grand total. A fee line derived from
  a percentage MUST carry the applicable-rates note the design specifies.
- **FR-013**: On an order with no recorded pre-fee subtotal, the receipt MUST show the
  total alone and MUST NOT display a subtotal row or fabricate fee rows.
- **FR-014**: The receipt's grand total MUST equal the amount charged for the order.
- **FR-015**: The receipt MUST close with the processing statement, the customer-service
  contact, and the platform attribution the design specifies.
- **FR-015a**: The customer-service address MUST be preceded by an envelope icon, as the
  design shows. An icon that cannot be drawn MUST be omitted rather than rendered as a
  substitute glyph or a missing-character box.
- **FR-015b**: The envelope icon MUST sit on the same line as the address it precedes,
  vertically centred against the text rather than floating above it.
- **FR-011a**: Every rule in the product table MUST span the table's full width, flush to
  both outer edges. The current rendering starts each row rule inset from the left, which
  leaves a visible notch where the rule meets the table's left border.
- **FR-011b**: The product table's content MUST be inset from its outer edges by the SAME
  amount on the left and the right. The first cut inset the product column but right-aligned
  the amount column flush against the table's own edge, so the table read as lopsided. The
  rules and the surrounding panel still run edge to edge (FR-011a) — it is the CONTENT that
  is inset, not the frame.
- **FR-011c**: The `Price` column header MUST be centred over its column. `Qty` and `Total`
  stay right-aligned: their values are narrow and sit on the same edge their headers do,
  whereas a price fills most of its column, which left a right-aligned `Price` header
  visibly off to one side of the figures beneath it.
- **FR-010a**: The payment method MUST be printed in upper case on every surface, whatever
  case the payment record stores.

#### E-Ticket document

- **FR-016**: Each e-ticket page MUST be headed "Ticket N of M", where N is that page's
  position and M is the number of tickets in the order.
- **FR-017**: Each e-ticket page MUST show a scannable QR code encoding that ticket's own
  ticket code, alongside the ticket code in readable text.
- **FR-018**: Each e-ticket page MUST show the holder's name and email address, and the
  order number the ticket belongs to.
- **FR-019**: Each e-ticket page MUST show the event name and the ticket type's name.
- **FR-019a**: An e-ticket page MUST NOT show the venue. The design carries no such field.
  This is a deliberate trade, recorded so it is not re-added by reflex: a holder arriving
  at a gate no longer has the address on the pass itself, and the venue remains on the
  email body and the confirmation screen.
- **FR-020**: Each e-ticket page MUST show the admission window of **that ticket's own
  ticket type** — never the parent event's window.
- **FR-020a**: A same-day admission window MUST render as
  `26 Apr 2026 @ 10:00 - 21:00 WIB` — date, `@`, then the time range, then the zone
  abbreviation. A window spanning more than one day MUST render both endpoints in full
  rather than collapsing to the opening date.
- **FR-021**: An e-ticket page MUST NOT show any price, fee, or total. Money appears on the
  receipt alone. The admission window is the last field on the page.
- **FR-022**: Each e-ticket page MUST carry the header band with the brand mark and the
  footer band with the event website and the customer-service contact.
- **FR-022a**: The e-ticket footer's two blocks MUST sit at opposite edges of the band —
  the site on the left, the customer-service contact right-aligned against the right
  margin — rather than both starting from the left.
- **FR-022b**: The accent rule beside the QR panel MUST be `#334155` and MUST be rounded on
  its RIGHT corners only — top-right and bottom-right. It is flush against the left page
  edge, so rounding its left corners would round a corner that is not visible and leave the
  rule looking detached from the edge.
- **FR-022c**: The e-ticket header band MUST carry the real logo image, embedded in the
  document (FR-023a's asset, embedded directly rather than by content ID, since a PDF has
  no MIME parts). A missing or unreadable asset falls back to the text wordmark.

#### Email body

- **FR-023**: The body MUST open with the header band carrying the brand mark, followed by
  the confirmation headline and a greeting naming the buyer.
- **FR-023a**: The brand mark MUST be the real logo image, not a text wordmark. In the
  email it MUST be carried as an inline image part referenced by content ID. A remote
  image URL MUST NOT be used (Outlook and Gmail block remote images by default, and a
  remote fetch from a receipt is a tracking signal); a `data:` URI MUST NOT be used (Gmail
  strips them from `img` sources). If the logo asset is missing or unreadable the body
  MUST fall back to the text wordmark rather than failing the send.
- **FR-024**: The body MUST show an order panel containing the order status as a badge, the
  order number, the order date, and the payment method.
- **FR-024c**: The payment method shown in the order panel MUST be upper case, per
  FR-010a — the same rule the receipt applies, so the two surfaces cannot disagree.
- **FR-024a**: The status badge MUST match the design: a slightly rounded rectangle, not a
  fully rounded pill, in the design's own colours. It MUST size to its label rather than
  stretching to fill its cell.
- **FR-024b**: The order date MUST render in Asia/Jakarta with a `WIB` suffix, as
  `04 Jul 2026 06:56 WIB`, following the convention the site's own date displays already
  use.
- **FR-025**: The body MUST show the event name with its venue and full address, preceded
  by a location-pin icon as the design shows. The icon MUST be inline SVG or an inline
  image part — never a remote image, and never an emoji character, which renders
  inconsistently across clients and platforms.
- **FR-026**: The body MUST itemize the ticket lines — each with its name, its admission
  date, its unit price and quantity, and its line total — followed by each frozen fee line
  and then the total payment, which is visually emphasised.
- **FR-026a**: Fee rows MUST use the same vertical rhythm and the same right-aligned
  amount column as the ticket lines above them, so the amounts form one column down the
  whole block. The current rendering crowds the fee rows against the line above and lets
  their amounts drift out of that column.
- **FR-026b**: There MUST be no rule or border beneath the Total Payment row. The total is
  the last element of the block and the design closes it with whitespace.
- **FR-026c**: The dashed rule separating the ticket lines from the fee rows MUST be drawn
  only when the order HAS at least one fee row. On a fee-free order it separates nothing
  from nothing and reads as a stray line.
- **FR-027**: The body MUST show a Buyer Information block containing the buyer's name,
  email address, and phone number as recorded on the order, omitting the phone row when
  none was recorded.
- **FR-028**: The body MUST show the important-information notice with the entry, sharing,
  and refund rules the design specifies.
- **FR-029**: The body MUST close with exactly this wording, on two lines: "This email was
  generated automatically. Please do not reply to this email." then
  "© 2026 manjo". The attribution "Powered By Manjo" MUST NOT appear in the email body;
  it remains the receipt document's closing line, where the design does use it.
- **FR-030**: The body MUST NOT contain per-ticket cards, QR codes, or ticket codes — those
  now belong to the attachment alone.
- **FR-031**: The body MUST render legibly in mail clients that ignore external stylesheets
  and block remote images, degrading to readable text rather than to an empty message.
- **FR-031b**: The body MUST fit the viewport on a phone, down to 320px wide, with no
  horizontal scrolling and nothing clipped. The card sizes to its container and is merely
  CAPPED at the design's 600px frame; it MUST NOT be pinned to a fixed pixel width, which
  is what clipped it. Outlook is the exception and keeps a fixed 600px layout — it reads
  the width attribute and the conditional wrapper, and has no phone client.
- **FR-031c**: Any stylesheet rule improving the small-screen layout is progressive
  enhancement ONLY. Every element it touches MUST already carry an inline style that is
  workable on its own, so a client that strips stylesheets loses polish and nothing more
  (FR-031).
- **FR-031a**: The body's layout MUST be built from inline-styled nested tables. CSS
  flexbox and CSS grid MUST NOT be used for layout: Outlook on Windows renders with the
  Word engine and supports neither, so a flex or grid layout collapses to stacked
  full-width blocks for a large share of recipients. Spacing defects are corrected within
  the table layout — consistent cell padding and a right-aligned amount column — not by
  changing layout technology. This requirement names a mechanism deliberately, because the
  mechanism is the decision (Clarifications, 2026-08-12 design review); it is checkable by
  asserting the rendered body declares no flex or grid layout.

#### Presentation of personal data

- **FR-032**: The email body's Buyer Information block and the receipt's Order Details block
  MUST render the **same** buyer contact values — the order's primary-contact name, email
  and phone — from one source. The two surfaces MUST NOT disagree about who bought the
  order.
- **FR-033**: The buyer's email address and phone number MUST be partially masked on both
  those surfaces, under one identical rule on both: an email keeps the first 5 characters of
  its local part and its whole domain, with the remainder of the local part replaced by
  asterisks; a phone number keeps its leading 5 and trailing 4 characters, with the middle
  replaced by asterisks. A value too short for that rule MUST be masked from its second
  character onward rather than printed in full.
- **FR-034**: The holder email printed on each e-ticket page MUST be shown in full — it is
  holder identity on a document presented at the gate, not a contact detail on a receipt
  that may be forwarded.

#### Branding

- **FR-035**: The brand mark, event website address, customer-service address, and platform
  attribution shown across all three surfaces MUST come from a single platform-wide
  configuration. They are identical on every order regardless of which event was bought.
- **FR-036**: Only the event's own recorded details — name, venue, address — vary per order
  on those surfaces. This feature MUST NOT introduce per-event branding, and MUST NOT add
  stored data of any kind.

#### Continuity

- **FR-037**: Monetary amounts MUST be rendered with the `IDR` prefix and Indonesian
  separators — **dot for thousands, comma for the decimal** — identically across the email
  body and the receipt: `IDR 550.000`, `IDR 15.000,92`.
  The documents differ from the site in the **prefix only** (`IDR` where the site writes
  `Rp`); the separators are the same, deliberately, because the site's formatter already
  records that comma-as-thousands misstates the sum for these buyers.
  (This requirement has been amended twice on 2026-08-12: first from "the same convention
  the site uses" to the mock's `IDR 70,000`, then to the present form when the decimal case
  showed the mock's separators to be unsafe. The Clarifications section records both.)
- **FR-037a**: The fractional part MUST be printed when it is non-zero and MUST be omitted
  when it is zero — `IDR 550.000`, not `IDR 550.000,00`; `IDR 15.000,92`, not
  `IDR 15.000`. Money columns are `NUMERIC(12,2)`, so at most two fractional digits exist
  and none are invented.
- **FR-037b**: Amounts MUST NOT be truncated or rounded for display. Every figure printed
  is the stored figure, so the printed lines, fees and total continue to reconcile exactly
  (FR-014, SC-003). Dropping a fractional part would make a receipt whose rows no longer
  sum to its own total.
- **FR-038**: The receipt MUST reflect the figures frozen onto the order at booking; later
  edits to fee master data MUST NOT change an already-placed order's documents.

### Key Entities

- **Order**: the purchase being confirmed — its number, status, dates, primary-contact
  name/email/phone, itemized lines, frozen fee lines, pre-fee subtotal and charged total.
  Source of everything on the receipt and the email body.
- **Order line**: one purchased row — a ticket type or a package, with a quantity, a unit
  price frozen at booking, and a line total.
- **Fee line**: one fee frozen onto the order at booking, carrying the name in force at
  that moment (including any percentage baked into the name).
- **Ticket**: one admission, with its unique code, its holder's name and email, its ticket
  type, and that type's admission window. One ticket produces exactly one page in the
  E-Ticket document.
- **Event**: the name, venue and address printed on the body and the e-tickets.
- **Payment Receipt document**: the new attachment — a self-contained proof of payment for
  the whole order.
- **E-Ticket document**: the existing attachment, extended with per-page numbering and the
  new layout, and stripped of price.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of ticket emails for paid orders arrive carrying exactly two document
  attachments — one receipt, one e-ticket file — with no email carrying one or three.
  Inline images (FR-023a) are not counted as documents.
- **SC-002**: For every order, the number of pages in the E-Ticket document equals the
  number of tickets issued for that order, and every page carries a distinct ticket code.
- **SC-003**: The grand total on the receipt matches the amount the buyer was charged for
  100% of orders, including orders with no fee lines.
- **SC-004**: A buyer can locate their receipt and file or forward it without opening the
  email body — verified by the receipt being self-contained: order number, buyer, lines,
  fees, total, and issuer contact all present on the document itself.
- **SC-005**: A ticket presented from the E-Ticket document validates at the gate on the
  first scan, including after a resend, for 100% of issued tickets.
- **SC-006**: The email body renders with every section of the design present and populated
  from the order, with no per-ticket card, QR, or ticket code appearing in the body.
- **SC-007**: No monetary figure appears anywhere in the E-Ticket document, for 100% of
  orders, including orders containing package lines.
- **SC-008**: The buyer's name, masked email and masked phone are byte-identical between the
  email body and the receipt for 100% of orders.
- **SC-009**: Documents for an order placed before a fee master-data edit still show the
  original figures after the edit, verified by resending that order.
- **SC-011**: The email body presents the same section order, the same column alignment
  and the same spacing in every mainstream mail client, including clients built on legacy
  rendering engines — no client shows a stacked or full-width fallback of a multi-column
  block.
- **SC-012**: The brand mark appears as the logo image, not as text, for a recipient whose
  client blocks remote image loading by default.
- **SC-013**: Every monetary amount on both surfaces carries the `IDR` prefix with dot
  thousands separators, no `Rp` prefix anywhere, and a comma decimal shown only where the
  stored amount has a non-zero fractional part.
- **SC-013a**: For an order whose amounts carry cents, the printed line totals plus the
  printed fees equal the printed grand total exactly — no figure is truncated for display.
- **SC-015**: An order with no fee lines renders no dashed separator in the email body, and
  an order with fee lines renders exactly one.
- **SC-016**: On the receipt, the gap between the table's left edge and its leftmost text
  equals the gap between its rightmost text and its right edge, within a millimetre.
- **SC-017**: The receipt's `Price` header is centred over its column, and `Qty` and `Total`
  remain right-aligned.
- **SC-018**: The email body produces no horizontal overflow at 320px, 375px or 600px
  viewport widths — the rendered document is never wider than the viewport.
- **SC-014**: Every displayed time on all three surfaces carries a `WIB` suffix and shows
  the Asia/Jakarta wall clock, for an order settled from any server timezone.
- **SC-010**: No regression in delivery: the share of paid orders reaching delivered status
  is unchanged from before this feature, and a failure to build either document leaves the
  order undelivered with resend armed rather than sending a partial email.

## Assumptions

- **One email, one recipient, unchanged.** The order's primary contact — the topmost holder
  form's snapshot — remains the sole recipient. The other holders' addresses stay holder
  identity, not delivery addresses. This feature changes what the email carries, not who
  gets it.
- **Both attachments are page documents.** The receipt is A4 portrait like the e-tickets,
  matching the design's canvas, so a buyer can print either.
- **The exact masking shapes in the mocks are illustrative, not specified.** The two designs
  mask the same kinds of value differently (`dimas***@gmail.com` vs `Palex******@gma**.com`;
  `+628123456****` vs `+62812****1119`). FR-033 picks one rule and applies it to both
  surfaces, because the clarification established that both render the same underlying
  value and two shapes for one value would read as two different buyers. The specific
  keep-5/keep-4 rule is the one detail here chosen rather than given — it is a display rule
  and cheap to change if the intended shape differs.
- **Fee line names are printed exactly as frozen onto the order.** The designs label fees
  variously ("Tax", "Platform Fee", "PPN (11%)", "Application Fee (Admin Fee)"); the order's
  own frozen names are authoritative, because inventing display names would make the
  receipt disagree with the checkout total the buyer already approved.
- **Order numbers are printed verbatim.** The designs show two different placeholder
  formats; the real order number is used everywhere it appears.
- **The payment method shown is the method the order was actually paid by.** The designs
  show QRIS because that is the only method this MVP offers.
- **The e-ticket's "Ticket N of M" counts tickets in the order**, not pages of a section —
  M equals the order's ticket count.
- **Recipient-side rendering is best-effort, within limits.** Mail clients vary; the body
  targets the design faithfully in mainstream clients — Outlook on Windows included, which
  is why FR-031a forbids flex and grid — and degrades to readable text elsewhere.
- **The documents differ from the site in the currency PREFIX only.** FR-037 fixes
  `IDR 550.000` for the email and the receipt while the site writes `Rp 550.000`. The
  separators are identical on purpose. An earlier answer in the 2026-08-12 session briefly
  set comma-thousands for the documents; it was reversed once the decimal case made the
  misreading risk concrete.
- **No new stored data.** Every value on the three surfaces is already recorded against the
  order, its lines, its fees, its attendees, its tickets, its ticket types, or its event.
  Branding is platform-wide configuration, not stored per event.
- **No schema change and no wire-contract change.** This feature alters rendered output
  only; the resend endpoints, their responses, and the delivered-flag semantics are
  untouched.
- **The confirmation screen is unaffected.** This feature covers the email and its
  attachments; on-site surfaces keep their current content.

## Dependencies

- The order's frozen fee lines and pre-fee subtotal, as recorded at booking.
- Per-ticket-type admission windows, as introduced for per-ticket event dates.
- The existing single delivery path shared by the automatic post-payment send, the
  guest-facing resend, and the admin resend — so all three inherit this change at once.
- Platform-wide brand assets per FR-035, supplied as configuration. The **logo image is now
  a hard dependency**, not an optional ornament: FR-023a requires the real mark on all three
  surfaces. The asset does not exist in the repository today and must be obtained (it is
  present in the Figma file as a raster node in the email header) before this feature can
  meet its own acceptance criteria. The text-wordmark fallback remains, but only as a
  failure path — shipping with it is not meeting FR-023a.
- A location-pin icon and an envelope icon (FR-025, FR-015a), likewise not in the
  repository today.

## Governance notes

- The binding rule that delivery is "exactly one email … carrying every ticket in the order
  as a single PDF together with the receipt" is **refined, not reversed**: the receipt moves
  from body-only to body plus a detachable document, and the ticket PDF stays exactly one
  document per order. Amended as constitution 4.1.0.
- **Outstanding as of the 2026-08-12 design review**: FR-001 and SC-001 now read "exactly
  two **document** attachments", because FR-023a's inline logo adds a third MIME part that
  several clients — and Mailpit's own API — report as an attachment. Three artifacts still
  say "exactly two attachments" and MUST be corrected in the same change that adds the
  logo, or the first inline image will make them contradict this spec and fail the suite:
  the constitution's ticket-generation bullet, `PRD.md` §1.4, and the `toHaveLength(2)`
  assertion in `e2e/specs/guest-purchase.spec.ts`. The e2e assertion should count parts
  whose disposition is `attachment`, not every part.
- End-to-end acceptance coverage is an acceptance gate. This change alters a covered flow
  (the guest purchase journey's delivery step), so the suite MUST be extended in the same
  change to assert the two-attachment outcome and the per-ticket page count.

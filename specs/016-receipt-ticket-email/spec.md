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

### Session 2026-08-19 (brand refresh + reported attachment count)

Opened by a report that the email arrives carrying **four** attachments — the two
documents plus the logo and the location pin — and by a new brand asset supplied in two
variants.

- **Q**: The logo and the pin are extra MIME parts. Should they be removed, given a buyer
  is promised exactly two attachments? → **A**: No — both stay exactly as they are. The
  report was a reading of the wrong instrument. Mailpit classifies the parts correctly (its
  API returns `Attachments` = the two PDFs and `Inline` = the two images, and its list
  endpoint reports `Attachments: 2`), but its message view renders one combined file strip
  from `Attachments + OtherParts + Inline` because it is a MIME debugger and exists to
  expose every part. Verified against the running service and against the shipped UI
  bundle. Gmail and Yopmail were checked by hand and list two attachments. FR-001, FR-023a,
  FR-025 and SC-001 are unchanged, and the mechanism is confirmed rather than amended.
- **Q**: Could the email body use SVG instead of a raster inline part? → **A**: No.
  Gmail strips `<svg>` elements from HTML mail, and Outlook on Windows renders through the
  Word engine, which has no SVG support at all; wrapping it as
  `<img src="data:image/svg+xml;…">` fails both again, since FR-023a already records that
  Gmail strips `data:` URIs from `img` sources. SVG works only in Apple Mail, iOS Mail and
  Thunderbird. FR-025's allowance of "inline SVG **or** an inline image part" was therefore
  unsafe as written and is narrowed to the inline image part (FR-025).
- **Q**: Figma 683-148's footer prints `https://www.jive-promotion.com/` and
  `help@manjo.co.id`, where the shipped defaults are `https://www.jive.co.id` and
  `help@manjo.com`. Which values do the documents carry? → **A**: Both are adopted from the
  design as real platform configuration, not mock content. The support address is one
  platform-wide value (FR-035), so it changes on every surface that prints it, not on the
  e-ticket alone. Both remain environment-overridable (FR-035a).
- **Q**: The new lockup carries fine print the old mark did not — "SPONSORED BY" and
  "-2° MINUS TWO" — which at today's ~98–108 px footprint renders around 2–3 px tall and is
  unreadable. How large should the mark be drawn? → **A**: Enlarge it and grow the bands to
  suit: roughly 250–300 px wide in the email body, with the site header and the e-ticket
  header band scaled to match. A sponsor credit that cannot be read defeats the reason a
  sponsor lockup replaced a plain wordmark. This is a layout change on three surfaces, not
  an asset swap (FR-023b).
- **Q**: With the envelope icon added, how is the e-ticket footer's customer-service block
  aligned? → **A**: The block is right-POSITIONED — its right edge meets the right margin —
  but its contents are **left-aligned** with one another, so the label, the icon and the
  address share a starting edge. This amends FR-022a, which required both lines flush right;
  frame 683:247 places the block at `x=432, width=123` with both children at `x=0`, and the
  flush right edge there is a coincidence of string length rather than a rule (FR-022a).
- **Q**: Frame 683:148's header still draws the OLD mark — a 71×40 image at the old asset's
  1.78:1 ratio — while the instruction is to update every logo call. Which mark does the
  e-ticket carry? → **A**: The new lockup, everywhere. The frame predates the asset, so the
  instruction supersedes it. One mark on all three surfaces; the old `jive-logo.png` is
  retired from the frontend and from the backend's embed so there is no second asset to
  drift (FR-023c).

Recorded from the same review, without needing a question — each was an unambiguous
finding rather than a decision:

- The **receipt carries no brand mark at all**. `drawBrandMark` is called only from the
  e-ticket page renderer; the receipt renderer draws no header band and no logo. FR-023a
  and FR-035 both speak of the mark appearing across "all three surfaces", which the
  receipt has never satisfied. Left outstanding deliberately rather than widened into this
  change — see the Governance notes.
- The frontend's `favicon.ico` is still the unmodified Next.js scaffold icon and the
  document title is still "Event Ticketing". Neither is a logo call, so neither is in
  scope here; recorded so the gap is not mistaken for something this change introduced.

- **Q**: How large should the new sponsor lockup be drawn, given its secondary line
  (`SPONSORED BY` / `-2° MINUS TWO`) is unreadable at the retired mark's size? → **A**:
  Enlarge it and grow the header bands to suit — ~280px in the email, ~200px on the site
  header, 55mm on the e-ticket.
  **~~SUPERSEDED~~ later the same day** — see the entry below. Retained because the
  reversal's reasoning only makes sense against what it replaced.
- **Q**: (Reopened after the enlargement was implemented and seen.) Should the mark be sized
  for legibility, or capped so no container grows? → **A**: **Capped by height.** Keep every
  container at its previous height and fit the mark inside it — the site header bar stays
  97px, the email header band 113px, the e-ticket header band 21mm. The width follows from
  the asset's own ratio.

  This reverses the answer above. Seen rendered, the enlargement cost a third of the
  e-ticket's header, doubled the email's header band from 113px to 211px, and gave the site
  header a fifth of the viewport before any content showed — all to make legible a sponsor
  credit nobody reads off a ticket or a navigation bar. Capping by height has the further
  property that swapping the asset can no longer change any layout (FR-023b).

### Session 2026-08-19 (subject line + phone masking)

Raised after a delivered email was read in a real inbox.

- **Q**: In the subject `[ORDERID] E-receipt & E-Ticket for JIVE 2026`, should "JIVE 2026" be
  a fixed literal string in every email, or the event's own name pulled from the order? →
  **A**: Dynamic — the subject carries the order's own event name, so it reads "JIVE 2026"
  while that is the event's name and stays correct for every other event the platform sells
  (FR-005a).
- **Q**: Which identifier is `[ORDERID]`, and do the square brackets appear literally? →
  **A**: The order number, in literal square brackets — the same identifier the receipt
  prints as "Order No." and both attachment filenames carry, not the internal database key
  (FR-005a).
- **Q**: Does the change to the buyer-phone masking apply to the receipt document too, or
  only to the email body? → **A**: Both. The receipt's email and phone MUST render in exactly
  the same shape as the email body's; one masking rule serves both surfaces, as FR-032 and
  FR-033 already require, and any revision to the rule changes both together.
- **Q**: Under the revised phone rule, which characters are replaced by asterisks? →
  **A**: Exactly the last 4 characters; every character before them prints verbatim, and the
  value is rendered as stored rather than normalised to any country-code format
  (`+6281234567890` → `+628123456****`). This replaces the previous keep-leading-5 /
  keep-trailing-4 rule, which masked the middle instead (FR-033). It is accepted that the
  new shape discloses more of the number than the old one; the reported defect was that the
  asterisks appeared in the wrong place, and legibility of the buyer's own number on their
  own receipt was preferred over the extra concealment.

### Session 2026-08-19 (e-ticket colours)

Raised while reading a rendered e-ticket against Figma `683-148`.

- **Q**: The event name above the ticket-type heading renders in the brand crimson
  `#cb1c4f`. What colour does the design specify? → **A**: `#475569`, taken exactly, as its
  own constant. **Not** the existing `#334155` — FR-022b pins that to the QR accent rule, a
  different meaning, and the two are visibly distinct slates at this size. The crimson then
  has no consumer left on the e-ticket (FR-019b).
- **Q**: The footer's emphasis is inverted against the design — the code draws the labels
  brighter than the values they head, so the support address is the dimmest text in the band.
  Adopt the design's emphasis? → **A**: Yes. Labels dim, values and the envelope icon in pure
  white (FR-022f).
- **Q**: Can the documents use Inter, the family the designs are drawn in, instead of the
  built-in Helvetica? → **A**: Yes, on **both** PDF documents — a receipt in Helvetica beside
  an e-ticket in Inter, arriving in one email, reads worse than both in Helvetica. It ships
  as its own increment immediately after the colour, subject and mask work, because the font
  moves every coordinate in both documents while the colour changes move none, and bundling
  them makes a failing geometry assertion impossible to bisect (FR-003a).
- **Q**: During the font swap, should the type sizes be re-read from the design wholesale, or
  should only the measured mismatches be corrected? → **A**: Wholesale, for the e-ticket —
  every text node in Figma `683:148`. Four elements were spot-checked and three were wrong
  (event name 9.5 vs 14, footer label 7.5 vs 10, footer value 9 vs 12), which is too poor a
  hit rate to extrapolate from, and the coordinates are being re-derived anyway. The page
  margin is explicitly **excluded** — the design insets at 14.1mm and the document at 17mm,
  and closing that moves the QR panel and the accent rule FR-022b pins, which is a layout
  change rather than a typographic one (FR-003b).
- **Q**: Reading the design node by node showed the e-ticket is a different colour *family*
  from the design throughout — Tailwind slate in the design, zinc/neutral in the document —
  and FR-019b and FR-022f reach only the event name and the footer. Should the rest follow? →
  **A**: Yes, and inside the typeface revision, where the same elements are already being
  retouched for the type scale. The identity block, the `Ticket N of M` heading, the
  valid-for row and the divider rule all move to the design's values (FR-019c).

### Session 2026-08-19 (email mask direction)

Raised after the phone rule shipped, on reading a delivered receipt.

- **Q**: How much of the email address should be hidden, and where? → **A**: The **last 3
  characters of the local part**, keeping everything before them and the whole domain —
  `dimasprasetyo@gmail.com` renders `dimasprase***@gmail.com`. This replaces the keep-first-5
  rule, under which the same address showed 8 asterisks and a local part of exactly 5
  characters was printed whole. At least one character always stays visible, so a 2-character
  local part gets 1 asterisk; only a single-character local part is masked entirely. The rule
  is now positionally the same as the phone rule — mask the tail, keep the head — differing
  only in how much (FR-033).
- **Q**: Should a value in the email field with no `@` get the same treatment, or be masked
  more aggressively? → **A**: The same rule. The field is format-validated at checkout, so
  this branch is close to unreachable, and a second masking rule is a second thing to keep
  correct. Accepted knowingly: it discloses more of such a value than the previous rule did
  (`notanemail` was `notan*****`, becomes `notanem***`).

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
6. **Given** a paid order, **When** the delivered email is listed in the recipient's inbox,
   **Then** its subject reads `[<order number>] E-receipt & E-Ticket for <event name>`,
   carrying the same order number the receipt prints and the name of the event the order was
   placed against (FR-005a).
7. **Given** a paid order, **When** an e-ticket page is opened, **Then** the event name above
   the ticket-type heading is slate `#475569` and no crimson appears anywhere on the page
   except within the brand mark itself, **and** in the footer each label is dimmer than the
   value beneath it (FR-019b, FR-022f), **and** the field labels, field values,
   pagination heading and divider carry the design's slate values rather than the neutral ones
   (FR-019c).
8. **Given** a paid order whose holder name carries a diacritic, **When** either document is
   opened on a machine that does not have the design's typeface installed, **Then** both
   render in that typeface and the name prints as stored rather than approximated, **and**
   the e-ticket's text sizes match the design (FR-003a, FR-003b).

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
6. **Given** a delivered email, **When** the recipient's client lists what arrived, **Then**
   it shows the two documents and nothing else, and the header mark and the location pin
   render as images in the body rather than as files to open (FR-001, FR-023a, FR-025).
7. **Given** a delivered email, **When** the header band is read at 100% zoom, **Then** the
   brand mark's secondary line is legible without magnification (FR-023b).
8. **Given** an order whose stored buyer phone is `+6281234567890`, **When** the body and the
   receipt are read, **Then** both show `+628123456****` — the stored value unchanged except
   for its last 4 characters, with no country-code normalisation and no reformatting
   (FR-033).
9. **Given** an order whose stored buyer email is `dimasprasetyo@gmail.com`, **When** the body
   and the receipt are read, **Then** both show `dimasprase***@gmail.com` — the whole domain
   and every character of the local part except its last 3 (FR-033).

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
- **Contact value too short to mask under the standard rule**: a very short email local part
  is masked from its second character onward rather than printed in full, and a phone of 4
  characters or fewer is masked entirely.
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
- **FR-003a**: Both documents MUST be typeset in **Inter**, the family the designs use,
  embedded in the file so they render identically for a reader who does not have it
  installed. Only the glyphs actually drawn need be embedded. This replaces the built-in
  Helvetica the documents shipped with, and it retires the transliteration step that
  Helvetica's single-byte encoding required — a holder name carrying a diacritic MUST render
  as stored rather than being approximated.
- **FR-003b**: The e-ticket's type sizes MUST match the design's (Figma `683:148`), read
  from every text node rather than from the elements that happened to be spot-checked. The
  page margins are explicitly out of scope and stay as they are: the design's narrower inset
  would move the QR panel and the accent rule FR-022b pins. **The receipt's type scale has
  not been examined against its own design and is not covered by this requirement** —
  recorded so its absence is not read as a finding of agreement.
- **FR-004**: Each attachment's file name MUST identify the order it belongs to and
  distinguish the receipt from the e-tickets, so two files saved to one folder never
  collide or become ambiguous.
- **FR-005**: Delivery remains exactly one email to the order's primary contact. This
  feature MUST NOT change the recipient, the number of emails, or the trigger.
- **FR-006**: If either document cannot be produced, the email MUST NOT be sent, the
  order MUST NOT be marked as delivered, and the resend MUST remain available.
- **FR-005a**: The email's subject line MUST identify both the order and the event, in the
  form `[<order number>] E-receipt & E-Ticket for <event name>` — the order number enclosed
  in literal square brackets, being the same human-facing identifier printed on the receipt
  and carried in both attachment filenames (FR-004, FR-009), never the order's internal
  database key. The event name MUST be the one recorded on the order rather than a fixed
  string, so an email for any other event names that event.
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
- **FR-019c**: The e-ticket's remaining text colours MUST follow the design rather than the
  document's current neutral family: field labels and the `VALID FOR` label `#64748b`, field
  values and the ticket-type heading `#0f172a`, the `Ticket N of M` heading `#1e293b`, and the
  divider rule `#e2e8f0`. Each differs from today's value by only a few percent; together they
  are the difference between a warm-neutral document and the design's cool slate, which is why
  they are specified as a set rather than individually.
- **FR-019b**: The event name above the ticket-type heading MUST be rendered in `#475569`
  and MUST NOT use the brand crimson `#cb1c4f`. The design gives it as a slate at that
  position (Figma `683:185`); crimson there reads as an alert rather than as a label. This is
  distinct from `#334155`, which FR-022b reserves for the QR accent rule.
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
  the site block against the left margin, the customer-service block against the right —
  rather than both starting from the left. Within the customer-service block the lines MUST
  be **left-aligned with one another**: the "Customer Service" label, the envelope icon and
  the address share one starting edge, and it is the BLOCK, not each line, that is
  positioned against the right margin.
  (Amended 2026-08-19. This previously required both lines flush against the right margin.
  Frame 683:247 places the block at `x=432, width=123` with both children at `x=0`, so the
  flush right edge in the mock is a coincidence of that particular address's length rather
  than a rule. Right-aligning each line independently would slide the envelope icon
  horizontally with every change of address length and break its alignment under the
  label — which is why the difference only became visible once the icon was added.)
- **FR-022b**: The accent rule beside the QR panel MUST be `#334155` and MUST be rounded on
  its RIGHT corners only — top-right and bottom-right. It is flush against the left page
  edge, so rounding its left corners would round a corner that is not visible and leave the
  rule looking detached from the edge.
- **FR-022c**: The e-ticket header band MUST carry the real logo image, embedded in the
  document (FR-023a's asset, embedded directly rather than by content ID, since a PDF has
  no MIME parts). A missing or unreadable asset falls back to the text wordmark. The band
  MUST be sized to the mark rather than the mark to the band (FR-023b), and the page's
  absolutely positioned content below it moves down accordingly.
- **FR-022d**: The customer-service address in the e-ticket footer MUST be preceded by an
  envelope icon, matching the receipt's treatment (FR-015a) and sitting on the address's own
  line rather than above it (FR-015b). The icon MUST be drawn with vector primitives in the
  band's own foreground colour rather than shipped as a raster or substituted with a font
  glyph, and an icon that cannot be drawn MUST be omitted rather than rendered as a
  missing-character box.
- **FR-022e**: The e-ticket footer's acceptance is behavioural, not pixel-matching: for any
  configured support address, short or long, the label, the envelope icon and the address
  still share one left edge and the block still sits against the right margin. A footer that
  lines up only for the mock's own address does not meet FR-022a.

- **FR-022f**: In the e-ticket footer, each section label MUST be rendered **dimmer** than
  the value it heads: the labels at white composited over the band at 80% opacity, the values
  and the envelope icon in pure white. The support address is the only value on the document
  a buyer needs to act on and MUST NOT be the dimmest text in the band — which is what the
  inverted emphasis produced, and it is worst in greyscale print.

#### Email body

- **FR-023**: The body MUST open with the header band carrying the brand mark, followed by
  the confirmation headline and a greeting naming the buyer.
- **FR-023a**: The brand mark MUST be the real logo image, not a text wordmark. In the
  email it MUST be carried as an inline image part referenced by content ID. A remote
  image URL MUST NOT be used (Outlook and Gmail block remote images by default, and a
  remote fetch from a receipt is a tracking signal); a `data:` URI MUST NOT be used (Gmail
  strips them from `img` sources). If the logo asset is missing or unreadable the body
  MUST fall back to the text wordmark rather than failing the send.
  **Confirmed unchanged on 2026-08-19**: a report that the email arrives with four
  attachments was traced to Mailpit's message view, which renders one combined file strip
  from `Attachments + OtherParts + Inline` because it is a MIME debugger. Its API and its
  own badge counts classify the parts correctly, and real clients list the two documents
  alone. The content-ID mechanism stands as specified.
- **FR-023b**: The brand mark MUST be drawn large enough for the lockup's secondary line —
  "SPONSORED BY" and "-2° MINUS TWO" — to be legible: approximately 250–300 px wide in the
  email body, and proportionally scaled on the site header and the e-ticket header band. The
  header bands on those surfaces MUST grow to accommodate it. Reducing the mark to the
  previous ~98–108 px footprint renders that line at roughly 2–3 px tall and does NOT
  satisfy this requirement — a sponsor credit nobody can read is the one outcome a sponsor
  lockup exists to prevent.

  **REVERSED 2026-08-19**, after the enlargement was implemented and seen. The mark
  MUST instead be **capped by the height of the container it sits in**, with its width
  following from its own aspect ratio, so that no container — the site header bar, the
  email header band, or the e-ticket header band — ever grows to accommodate it. The
  legibility goal is abandoned deliberately: a sponsor credit is not read off a ticket
  or a navigation bar, and making it readable cost a third of the e-ticket's header, a
  doubled email header band, and a site header taking a fifth of the viewport before any
  content showed. Capping by height also makes a future asset swap incapable of changing
  any layout.

  The mark's own aspect ratio MUST be preserved.
  (Corrected 2026-08-19, during planning: the supplied files are 1.49:1, but that is the
  FILE, not the mark — both carry symmetric transparent padding, and the artwork inside is
  1.757:1, effectively the retired mark's 1.77:1. Nothing is therefore forced to change by
  ratio; the growth in this requirement is driven by legibility alone. Derivatives MUST be
  trimmed to the artwork so that padding is a layout decision made in the layout rather than
  baked into the asset.)
- **FR-023c**: One brand asset MUST serve every surface. The mark ships in a light-on-dark
  (WHITE) and a dark-on-light (BLACK) variant; every current placement — site header, email
  header band, e-ticket header band — sits on the dark brand band, so all three use the
  WHITE variant, and the BLACK variant is carried for light surfaces that do not yet exist.
  The retired mark MUST be removed from both the frontend's public assets and the backend's
  embedded assets, so no second logo can drift out of step. Where a linked design still
  draws the retired mark, the asset supersedes the design.
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
  by a location-pin icon as the design shows. The icon MUST be an inline image part
  referenced by content ID — never a remote image, never an emoji character (which renders
  inconsistently across clients and platforms), and never inline SVG or an SVG `data:` URI.
  (Amended 2026-08-19: the original wording also permitted inline SVG. Gmail strips `<svg>`
  from HTML mail and Outlook on Windows renders through the Word engine, which cannot draw
  SVG at all, so that allowance would have failed the two largest client families. SVG
  renders only in Apple Mail, iOS Mail and Thunderbird.)
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
  those surfaces, under one rule shaped the same way for both — mask the tail, keep the head,
  differing only in how much.

  An email MUST keep its whole domain and every character of its local part except the last
  3, which are replaced by asterisks: `dimasprasetyo@gmail.com` renders
  `dimasprase***@gmail.com`. At least one character of the local part MUST stay visible, so
  the asterisk count is the smaller of 3 and one less than the local part's length; a
  single-character local part, which cannot satisfy both, MUST be masked entirely. A value in
  the email field carrying no `@` MUST be treated as a bare local part under this same rule
  rather than printed whole or given a stricter one of its own.

  A phone number MUST keep every character except its last 4, which are replaced by
  asterisks — so a stored `+6281234567890` renders as `+628123456****`. The phone rule MUST
  count characters, not digits, and MUST render the value as stored: no country-code
  normalisation, no reformatting, and no separators added or removed. A phone of 4 characters
  or fewer MUST be masked entirely.
- **FR-034**: The holder email printed on each e-ticket page MUST be shown in full — it is
  holder identity on a document presented at the gate, not a contact detail on a receipt
  that may be forwarded.

#### Branding

- **FR-035**: The brand mark, event website address, customer-service address, and platform
  attribution shown across all three surfaces MUST come from a single platform-wide
  configuration. They are identical on every order regardless of which event was bought.
- **FR-035a**: The configured brand values MUST match the linked designs: the site address
  is `https://www.jive-promotion.com/` and the customer-service address is
  `help@manjo.co.id`. Both remain environment-overridable so an operator can correct either
  without a rebuild. The customer-service address is a single platform-wide value shared by
  every surface that prints it, so changing it changes them together — that is the intent of
  FR-035, not a side effect of it.
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
- **SC-019**: Every surface that shows the brand mark shows the same mark, and the retired
  mark appears on no surface and is shipped with none of them. Verified by comparing the
  three placements against one another and by the retired mark being referenced nowhere.
- **SC-020**: The lockup's secondary line reads at 100% zoom on all three surfaces without
  magnification — in the delivered email, on the site header, and on a printed e-ticket page
  at A4 (FR-023b).
- **SC-021**: In the e-ticket footer, the "Customer Service" label, the envelope icon and
  the support address share one left edge, for any configured support address, and the block
  as a whole stays against the right margin (FR-022a, FR-022d).
- **SC-022**: The delivered email's subject reads `[<order number>] E-receipt & E-Ticket for
  <event name>` for 100% of sent orders, with the order number matching the one printed on
  the receipt and the event name matching the event the order was placed against (FR-005a).
- **SC-023**: The buyer's phone renders with only its last 4 characters replaced by
  asterisks, character-for-character identical to the stored value elsewhere, and identically
  on the email body and the receipt (FR-033).
- **SC-024**: The e-ticket carries no brand crimson outside the brand mark, and in its
  footer the site address and support address are the brightest text in the band while their
  labels are dimmer — for any configured address, and legibly so in greyscale print
  (FR-019b, FR-022f).
- **SC-025**: Both documents render in the same typeface as the site and the designs, for a
  reader who does not have that typeface installed, and a holder name carrying a diacritic
  prints as stored rather than approximated to its nearest plain letter (FR-003a).
- **SC-026**: The buyer's email renders with only the last 3 characters of its local part
  replaced by asterisks and its domain intact, identically on the email body and the receipt,
  and never with the local part printed whole — for a local part of any length down to one
  character (FR-033).
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
  value and two shapes for one value would read as two different buyers. The email shape
  (keep 5) remains the one detail chosen rather than given; the phone shape is no longer
  discretionary — the 2026-08-19 clarification fixed it as `+628123456****`, the first of
  the two mock shapes.
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
- Platform-wide brand assets per FR-035, supplied as configuration. The **logo image is a
  hard dependency**, not an optional ornament: FR-023a requires the real mark on the surfaces
  that carry one. The text-wordmark fallback remains, but only as a failure path — shipping
  with it is not meeting FR-023a.
  **Satisfied as of 2026-08-19**: the mark was supplied in both variants
  (`LOGO JIVE MINUS TWO_WHITE.png`, `LOGO JIVE MINUS TWO_BLACK.png`, 6400×4290, ~276 KB
  each). Both are far larger than any placement needs, so per-surface derivatives are
  produced from them; the originals are the source of truth, not the shipped files. Their
  names carry spaces, which become `%20` in a public URL and are awkward in a `//go:embed`
  directive, so the shipped derivatives are named canonically.
- A location-pin icon and an envelope icon (FR-025, FR-015a, FR-022d). Both now exist: the
  pin is an authored raster in the backend's embedded assets, and the envelope is drawn with
  vector primitives, which is what lets it be recoloured for the e-ticket's dark band.

## Governance notes

- The binding rule that delivery is "exactly one email … carrying every ticket in the order
  as a single PDF together with the receipt" is **refined, not reversed**: the receipt moves
  from body-only to body plus a detachable document, and the ticket PDF stays exactly one
  document per order. Amended as constitution 4.1.0.
- **RESOLVED as of 2026-08-19** (raised at the 2026-08-12 design review): FR-001 and SC-001
  read "exactly two **document** attachments" because FR-023a's inline images add MIME parts
  that some tooling reports alongside the documents. All three dependent artifacts were
  re-checked and already agree: the constitution's ticket-generation bullet and `PRD.md` §1.4
  both say "two document attachments", and `e2e/specs/guest-purchase.spec.ts` asserts against
  Mailpit's `Attachments` array, which the API populates with documents only — the inline
  parts arrive in a separate `Inline` array. Verified against the running service. The note
  about Mailpit's *API* reporting inline parts as attachments was mistaken: it is Mailpit's
  message *view* that merges them into one file strip, and that is the display a buyer never
  sees.
- **Outstanding, deliberately not fixed here**: FR-023a and FR-035 both describe the brand
  mark as appearing across "all three surfaces", but the receipt renderer draws no header
  band and no mark — `drawBrandMark` is called only from the e-ticket page renderer. Either
  the receipt gains the band or those two requirements narrow to the two surfaces that
  actually carry a mark. Widening the 2026-08-19 brand refresh to settle it would have
  redesigned a document nobody asked to change, so it is recorded rather than resolved.
- **Outstanding, out of scope**: `frontend/app/favicon.ico` is still the unmodified Next.js
  scaffold icon (a black disc with a triangle) and the document title is still
  "Event Ticketing". Neither is a logo *call*, so the 2026-08-19 instruction to update every
  logo call does not reach them, and the sponsor lockup would in any case need a glyph-only
  crop to survive 32×32 — an asset the brand has not supplied. Recorded so the scaffold icon
  is a known debt rather than something the brand pass missed.
- End-to-end acceptance coverage is an acceptance gate. This change alters a covered flow
  (the guest purchase journey's delivery step), so the suite MUST be extended in the same
  change to assert the two-attachment outcome and the per-ticket page count.

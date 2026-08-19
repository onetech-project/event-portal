# Phase 0 Research: Receipt + E-Ticket Email Attachments

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-08-12

The spec carried no `[NEEDS CLARIFICATION]` markers into planning — all three were
resolved in the spec's Clarifications section. The research below therefore covers
**implementation unknowns** raised by the Technical Context rather than open product
questions, plus the two items the spec deferred to planning by name.

---

## R-001 — How does the notification domain obtain the new fields without breaking Principle II?

**Unknown**: FR-009, FR-010, FR-011, FR-025 and FR-027 require the event address, the
buyer's phone, the payment method and date, and each order line's admission date and
descriptor. `internal/notification` may not import `internal/order` or `internal/event`.

**Decision**: Widen the consumer-declared structs (`notification.OrderDelivery`,
`notification.ReceiptLine`, `notification.TicketDetail`) and populate the new fields in
`notificationOrderAdapter` / `notificationTicketAdapter` in
[backend/cmd/api/adapters.go](../../backend/cmd/api/adapters.go). Add **no** new method to
`OrderProvider` or `TicketProvider` except one narrow payment read.

**Rationale**: Almost all of it is already fetched and then thrown away.
`OrderForDelivery` calls `a.guestReads.TicketOrderByNumber(ctx, rec.OrderNumber)` at
[adapters.go:318](../../backend/cmd/api/adapters.go#L318) and maps only `Items`, `Fees`
and `Subtotal` out of the returned `order.TicketOrderDetail`. That struct already carries:

| Needed for | Already available on `TicketOrderDetail` |
|-----------|------------------------------------------|
| FR-025 event block | `Event.Name`, `Event.Venue`, `Event.Address` (`PublicOrderEvent`, [dto.go:127](../../backend/internal/order/dto.go#L127)) |
| FR-011 / FR-026 line admission date | `Items[].AdmissionStarts []time.Time` ([dto.go:169](../../backend/internal/order/dto.go#L169)) |
| FR-011 line kind | `Items[].Kind` (`ticket` \| `package`) |

`AdmissionStarts` is a particularly good fit and worth calling out: it was designed for
spec 015 to express *"one date for a ticket line, one per distinct constituent for a
bundle"*, which is exactly the receipt's per-line date sub-line, bundles included. Nothing
new has to be derived.

`rec.BuyerPhone` and `rec.CreatedAt` are already on the `order.OrderRecord` the adapter
holds ([repository.go:34-59](../../backend/internal/order/repository.go#L34)) and are
likewise discarded today.

**Alternatives considered**:
- *Add `OrderProvider.EventForOrder` / `PaymentForOrder` methods.* Rejected: more interface
  surface for data one existing call already returns, and each new method is another thing
  the fake in `service_test.go` must implement.
- *Let `notification` call the event domain through a new `EventProvider`.* Rejected for
  the same reason, and it would add a second cross-domain hop for a field
  `TicketOrderByNumber` already resolved.
- *Denormalise onto `orders` in a migration.* Rejected outright — FR-036 forbids new stored
  data, and this is display-time information that already has a source of truth.

---

## R-002 — Where does the payment method and payment date come from?

**Unknown**: FR-010's Transaction Details block needs the instrument (`QRIS`) and the
settlement timestamp. `orders` records `payment_provider` (who acquired it), not the
instrument, and has no paid-at column.

**Decision**: Read the order's payment row. `payments` carries
`payment_type`, `status` and `created_at` ([SCHEMA.md:221-230](../../SCHEMA.md)). Expose it
through **one** new method on the existing `OrderProvider` interface, returning a
notification-owned struct — not a `payment` domain type:

```go
// PaymentSummary is the receipt's Transaction Details block.
type PaymentSummary struct {
    Method string    // "QRIS"; falls back to the provider name when payment_type is null
    Status string    // display status, e.g. "Paid"
    PaidAt time.Time // the settling payment row's created_at
}
```

**Rationale**: The alternative — hardcoding `"QRIS"` — is true today and false the moment
Principle V is exercised again, which it already has been once (Midtrans → Manjo, spec
012). A constant in a receipt is a latent correctness bug in a financial document, and
the cost of avoiding it is one indexed read on a path that already performs several.

`payment_type` is nullable, so the mapping falls back to `orders.payment_provider`, and
then to the literal `QRIS`, rather than rendering an empty cell.

**Alternatives considered**:
- *Constant `"QRIS"`.* Rejected as above.
- *`orders.updated_at` as the payment date.* Rejected: it moves on any write, including a
  resend's `email_sent` update, so a resent receipt would claim a later payment time than
  the original. The design's separate "Last updated" line (FR-008) is where `updated_at`
  legitimately belongs — see R-008.

---

## R-003 — What exactly is the FR-033 masking rule, at the edges?

**Unknown**: FR-033 states keep-first-5-of-local-part and keep-5/keep-4 for phones, with a
short-value fallback. The exact behaviour on degenerate input needs pinning before it is
written into a template.

**Decision**: A pure function pair in a new `mask.go`, with table-driven tests:

| Input | Output | Why |
|-------|--------|-----|
| `dimasprasetyo@gmail.com` | `dimas********@gmail.com` | the ordinary case; domain intact |
| `dimas@gmail.com` | `dimas@gmail.com` | local part is exactly the kept prefix — nothing to mask |
| `bud@gmail.com` | `b**@gmail.com` | shorter than the prefix → mask from char 2 (FR-033 fallback) |
| `a@gmail.com` | `*@gmail.com` | single char: masked rather than disclosed |
| `notanemail` | `n*********` | no `@`: treat the whole string as the local part; never print it whole |
| `` (empty) | `` | renders as an omitted row, not as `***` |
| `+628123456789` | `+6281****6789` | keep leading 5, trailing 4 |
| `081234567` | `08123****` | 9 chars: leading 5 kept, remainder masked, no trailing window |
| `0812` | `0***` | shorter than 5: mask from char 2 |

**Rationale**: The rule counts **characters, not digits**, so a leading `+` and any spacing
are preserved verbatim — the alternative (normalise to digits first) would reformat the
buyer's own phone number on their receipt, which is a surprise nobody asked for.

Masking never *lengthens* a value and never emits a fixed-width blob, so `Rp`-style
alignment in the receipt table is unaffected.

**Alternatives considered**:
- *Mask the domain too*, as the receipt mock's `@gma**.com` shows. Rejected: FR-033 chose
  one rule and keeping the domain is the more useful of the two — a buyer forwarding a
  receipt to finance still needs the address to be recognisable. Recorded in the spec's
  Assumptions as the discretionary call it is.
- *Reuse `pkg/sanitize`.* Rejected: that package escapes untrusted HTML, an unrelated
  concern; conflating them would make either harder to reason about.

---

## R-004 — Where do the branding strings live?

**Unknown**: FR-035 says platform-wide. Constants, or configuration?

**Decision**: `pkg/config` entries with compile-time defaults, surfaced to the domain as a
`notification.Branding` value injected into `NewService`:

| Field | Config key | Default |
|-------|-----------|---------|
| `SiteName` | `BRAND_SITE_NAME` | `JIVE` |
| `SiteURL` | `BRAND_SITE_URL` | `https://www.jive.co.id` |
| `SupportEmail` | `BRAND_SUPPORT_EMAIL` | `help@manjo.com` |
| `LegalEntity` | `BRAND_LEGAL_ENTITY` | `PT Manjo Teknologi Indonesia` |
| `Attribution` | `BRAND_ATTRIBUTION` | `Powered By Manjo` |

**Rationale**: Configuration costs one struct and zero runtime complexity, and it keeps a
support-address change out of a code review. Injecting it rather than reading config inside
the renderer keeps the render functions pure and testable, matching how `SMTPConfig` is
already handled in [smtp.go](../../backend/internal/notification/smtp.go).

**Alternatives considered**:
- *Hardcoded constants.* Workable, and honestly close in cost; rejected because a support
  email is exactly the kind of string that changes without an engineer available.
- *A `branding` database table.* Rejected — FR-036 forbids new stored data.

---

## R-005 — How is the brand mark rendered across three very different surfaces?

**Unknown**: FR-022 and FR-023 put a logo in the PDF header band and the email header
band. The repository ships no logo asset, and email clients block remote images (FR-031).

**Decision**: Three different treatments, deliberately:

| Surface | Treatment |
|---------|-----------|
| E-ticket / receipt PDF | If a logo file is configured and readable at startup, embed it via `gofpdf`'s image registration. Otherwise draw the site name as styled text in the same position. |
| Email body | Styled **text** wordmark on the dark band — never a remote `<img>`. |
| Either, on failure | Text fallback; a missing logo never fails a send (FR-006 covers document *generation*, and a decorative asset must not trip it). |

**Rationale**: A remote image in the email is the wrong default twice over — Gmail and
Outlook block it by default, and the tracking-pixel-shaped request it makes is a privacy
smell on a transactional receipt. Base64-inlining it would inflate every message. A text
wordmark degrades to exactly itself.

For the PDFs the tradeoff reverses: the asset is embedded in the file, so it renders
offline and when printed.

**Open item for `/speckit-tasks`**: the actual logo asset is not in the repository. The
task list must either add it under `backend/assets/` with a config path, or ship the text
fallback and note the gap. This is an asset-availability question, not a design one.

---

## R-006 — `gofpdf` and non-Latin-1 text

**Unknown**: `gofpdf` core fonts (Helvetica, Courier) are single-byte Latin-1. The current
`pdf.go` writes `ticket.EventName`, `ticket.Venue` and `ticket.AttendeeName` straight into
`MultiCell` with no encoding step.

**Decision**: Add a `latin1` sanitising helper applied to every free-text string written
into either PDF, transliterating the common offenders and dropping anything else:

- Typographic quotes and dashes (`’ ‘ “ ” – —`) → ASCII equivalents.
- `·` (already used as the design's separator) → `-`.
- Any remaining rune above U+00FF → dropped rather than emitted as a replacement glyph.

**Rationale**: This is a **pre-existing latent defect**, not one this feature introduces —
but this feature widens the exposure materially, because the receipt adds the event
`address` (free text, admin-authored, TEXT column) and the line descriptors. Fixing it
here is cheaper than discovering it in a printed ticket at a venue gate.

Per Principle VIII this is a behaviour change in a covered flow only if it changes what a
user sees; it changes mojibake into readable text, so a Go-level test in
`pdf_test.go` is the right tier — the assembled system adds nothing to the observation.

**Alternatives considered**:
- *Embed a UTF-8 TrueType font.* The correct long-term answer and the only one that
  renders, say, Japanese. Rejected for this feature: it needs a font file, a licence
  review, and `gofpdf`'s `AddUTF8Font` path — all real work orthogonal to attaching a
  receipt. Recorded here so the next person finds the reasoning rather than the symptom.

---

## R-007 — How does `e2e/` observe a two-attachment email?

**Unknown**: Principle VIII requires the two-attachment outcome to be pinned in `e2e/`.
The suite asserts delivery only via `orders.email_sent`
([e2e/support/db.ts](../../e2e/support/db.ts)), and
[playwright.config.ts:123-124](../../e2e/playwright.config.ts#L123) explicitly records that
a refused SMTP connection is tolerated.

**Decision**: Assert through **Mailpit's HTTP API**. It is already in
[docker-compose.yml:143-149](../../docker-compose.yml#L143) with SMTP on `:1025` and its UI
and REST API on `:8025`, and the API exposes `GET /api/v1/messages` and
`GET /api/v1/message/{id}` — the latter listing attachments with filename, content type and
size. A new `e2e/support/mail.ts` polls for the message addressed to the buyer and returns
its attachment list.

Consequences the task list must carry:

1. Mailpit becomes **required** for the specs that assert attachments — the current
   tolerate-a-refused-connection stance must not apply to them. `AGENTS.md`'s quoted
   startup line (`docker compose up -d postgres redis`) needs `mailpit` added.
2. The poll must tolerate the asynchronous post-payment goroutine, exactly as the existing
   `email_sent` assertions already do (`playwright.config.ts:43` notes this).
3. Mailpit state must be cleared between the specs that read it, or a resend test will
   match the first delivery's message.

**Rationale**: This observes the real MIME the production `SMTPMailer.Compose` produced,
through a real SMTP hop — which is the point of the tier. Reading `Compose`'s output in Go
proves the struct was populated, not that the message was sent or that a client can open
the parts.

**Alternatives considered**:
- *Assert only in Go via `SMTPMailer.Compose`.* Kept **as well** — it is the right place
  for per-part detail — but rejected as sufficient, per Principle VIII's "a defect
  reachable only through the assembled system MUST be pinned here".
- *A fake mailer capturing messages in the API process.* Rejected: it substitutes a
  component the constitution permits substituting only for the payment provider.

---

## R-008 — Which timestamp is the receipt's "Last updated"?

**Unknown**: FR-008 asks for the order's last-updated time. `order.OrderRecord` exposes
`CreatedAt` but not `updated_at`, though the column exists.

**Decision**: Add `UpdatedAt *time.Time` to the existing `OrderRecord` read. This is a
read-only widening of a query that already selects from `orders`; no migration, no
`SCHEMA.md` change.

**Rationale**: The design shows *both* `Last updated` (header) and a Transaction `Date`
(details block), and they differ in the mock — `14:35` vs `14:30`. They are genuinely two
different facts: when the record last changed, and when the money settled. Mapping both to
one timestamp would collapse a distinction the design drew on purpose.

**Caveat worth stating**: `updated_at` moves when `email_sent` is set, so the *first*
delivered receipt and a *resent* one can legitimately show different "Last updated" values
while every monetary figure stays identical. That is correct behaviour, and FR-038 /
SC-009 constrain only the figures — but it will look like a discrepancy to anyone
comparing two copies, so it belongs in the task notes.

---

## R-009 — What is the receipt line's "descriptor"?

**Unknown**: FR-011's product sub-line reads `26 Apr 2026 • Expo Entrance Ticket` in the
design. The date is settled (R-001). The trailing text is not.

**Decision**: Render the line's own **description** — `ticket_types.description` for a
ticket line, `packages.description` for a package line — and omit the `•` separator and
the descriptor entirely when the description is blank.

**Rationale**: It is admin-authored per-line text that already exists and already renders
guest-facing on the booking card ([SCHEMA.md:40-42](../../SCHEMA.md)), which makes it the
only field in the schema that plausibly produces a phrase like "Expo Entrance Ticket". It
requires no invented vocabulary, and an event that leaves descriptions blank simply gets a
date-only sub-line rather than a hardcoded word that may be wrong.

**Alternatives considered**:
- *A static label per line kind* (`Entrance Ticket` / `Bundle`). Rejected: it invents
  product vocabulary the organiser never wrote, and "Entrance Ticket" is wrong for a
  parking pass or a merchandise add-on.
- *The ticket type's name.* Rejected: on a ticket line that is already the Product cell
  directly above it, so the sub-line would repeat it.

**Note for `/speckit-tasks`**: `order.PublicOrderItem` does **not** currently carry a
description field, so unlike the rest of R-001 this one field does require widening the
`order` DTO and the query behind `lineDisplays`. That is an additive, read-only change to a
domain this feature otherwise only reads from — flag it in review as the single place the
blast radius leaves `internal/notification`.

---

## Summary of decisions

| ID | Decision | Blast radius |
|----|----------|--------------|
| R-001 | Widen consumer-declared structs; adapters map already-fetched data | `notification`, `cmd/api` |
| R-002 | One narrow payment read for method + paid-at | `notification`, `cmd/api`, `order` repo read |
| R-003 | Masking as a tested pure function, characters not digits | `notification` |
| R-004 | Branding as injected config with defaults | `notification`, `pkg/config` |
| R-005 | Logo: embedded in PDFs, text wordmark in email, text fallback always | `notification` (+ asset TBD) |
| R-006 | Latin-1 transliteration guard on all PDF free text | `notification` |
| R-007 | e2e observes attachments via Mailpit's HTTP API | `e2e/`, `AGENTS.md` |
| R-008 | `updated_at` for "Last updated"; payment `created_at` for the transaction date | `order` repo read |
| R-009 | Line descriptor = the line's own description; omitted when blank | `order` DTO (+ query) |

**All Technical Context unknowns are resolved.** Two items are handed forward as task-list
obligations rather than design questions: the logo asset (R-005) and the `PublicOrderItem`
description widening (R-009).

---

# Phase 0 (Revision 2): Design-Review Research — 2026-08-12

The 2026-08-12 design review raised 15 corrections and three clarification answers
(nested tables, CID inline logo, and a currency format later re-answered). This section records what was researched
to plan them. Five research lanes ran in parallel; the highest-risk claims were then
checked directly against library source, a live Mailpit, and the real assets.

**Verification status is stated per finding.** "Verified here" means confirmed in this
session against source or a running service, not merely asserted.

---

## R-010 — Carrying the logo as an inline image (FR-023a)

**Decision**: `go-mail`'s `EmbedReader`. No new dependency.

**Verified here** against `go-mail/mail/v2@v2.3.0`:

- `Message.EmbedReader(name, r, settings...)` exists (`message.go:309`).
- The writer emits `Content-Disposition: inline; filename="…"` and
  `Content-ID: <name>` for embedded files, and `attachment` for attached ones
  (`writeto.go:132-146`).
- It automatically opens a `multipart/related` around the HTML part whenever any
  embedded file exists (`writeto.go:35`, `:48`).

**Three details that would break it silently**, all worth pinning with a test:

1. The part's `Content-Type` is derived from the **file extension** via
   `mime.TypeByExtension`. A CID name without `.png` becomes
   `application/octet-stream` and Outlook renders nothing.
2. `fileFromReader`'s default `CopyFunc` drains the reader once. The existing attachment
   code already works around this with `mail.SetCopyFunc` — the inline path must do the
   same or a re-written message yields an empty image.
3. `notification.Message` has no field for an inline part. It needs one; see
   [contracts/notification.md](./contracts/notification.md).

**The `cid:` reference must match the embedded filename exactly**, because go-mail derives
the Content-ID from the name.

---

## R-011 — The "exactly two attachments" rule survives the inline images

**Decision**: no change to the e2e attachment assertion.

**Verified here** against the running Mailpit (sent a probe carrying one inline CID image
plus two PDF attachments):

```
Attachments: [('receipt-X.pdf', 'application/pdf'), ('tickets-X.pdf', 'application/pdf')]
Inline     : [('logo.png', 'logo.png', 'image/png')]
```

Mailpit reports inline parts in a **separate `Inline` array**. `Attachments` stays at 2.
So FR-001/SC-001's "exactly two document attachments" is not merely a wording dodge — it
is how the tooling already models it, and `toHaveLength(2)` in
`e2e/specs/guest-purchase.spec.ts` needs no change.

This corrects the concern raised at clarification time, which assumed the e2e assertion
would break.

---

## R-012 — The logo asset, and a colour defect it exposes

**Decision**: ship `backend/assets/brand/jive-logo.png` (216×122, 2× of the design's
108×61). Already staged in this change.

**Verified here** by sampling pixels:

| Thing | Colour | Note |
|---|---|---|
| Logo's own background | `#151A26` | **opaque — 0 of 105,408 pixels are transparent** |
| Design's header band | `#151A26` | sampled from the band export |
| **Current implementation's band** | `#141B2D` | `pdf.go` `pdfBand`, `service.go` `emailBand` |

The logo has **no transparency**, so it must sit on a band of exactly its own background
colour or a rectangular seam shows. The current band colour is wrong in both renderers and
must change to `#151A26`. This defect was not on the review list and would have appeared
the moment the logo landed.

The band's top corners sample as `#F5F5F5` — the card has rounded top corners over the
page background, which the current `border-radius:12px` already produces.

---

## R-013 — The location pin cannot be exported (FR-025)

**This contradicts the design and needs a decision before implementation.**

**Verified here**: Figma node `750:2` is named `📍` and is an **emoji text layer**, not a
vector. Every export route returns nothing usable:

- `download_assets` on `750:2` at 2× returns a **177-byte blank white PNG**.
- Its `svgAssets` entry is a degenerate `<path d="M0 16H16V0H0V16Z"/>` — a plain square.
- Exporting the parent row (`741:5375`) at 2× renders the venue and address text with
  **no pin at all**.

So the design's "icon" is the platform's own emoji glyph, and there is no asset to ship.
FR-025 forbids exactly that (`never an emoji character, which renders inconsistently
across clients and platforms`), so the design and the requirement are in direct conflict.

**Options**, none free:

| Option | Consequence |
|---|---|
| **A. Author a pin PNG** and carry it as a second CID part | Renders identically everywhere; costs a small design decision we make, not the designer |
| B. Use the `📍` character | Exactly what the design does, but each platform draws its own; violates FR-025 as written |
| C. Omit the icon | Loses a design element; FR-025 would need amending |

**Recommendation: A**, carried through the same CID mechanism as the logo. It is the only
option consistent with FR-025's own text. It needs sign-off because the glyph would be
ours rather than the designer's.

---

## R-014 — Outlook-safe techniques for the seven email defects

**Constraint (decided, not relitigated)**: nested tables only. Outlook on Windows renders
with the Word engine and supports neither flexbox nor grid (FR-031a).

Findings that drive the most work:

- **Fix the container first.** The body currently opens a bare `<div>` with
  `max-width:640px;margin:0 auto`. The Word engine does not honour `margin:0 auto`
  centring of a table, and treats `max-width` as partial. The design frame is **600px**
  with 40px side padding (520px content). Adopt `width="600"` as **both** an attribute and
  a style, wrapped in the Outlook ghost-table conditional, with 40px cell padding.
- **Emit a full HTML document.** `buildEmailBody` returns a fragment with no doctype or
  `<head>`. That blocks the only working fix for Outlook's DPI scaling of images: the
  `<o:OfficeDocumentSettings><o:PixelsPerInch>96</o:PixelsPerInch>` normaliser must live in
  `<head>` with the `xmlns:o` namespace. Without it the CID logo renders 25–50% oversized
  on 120/144 DPI Windows. Gmail discards `<head>` harmlessly; existing substring
  assertions are unaffected.
- **Images need `width`/`height` HTML attributes**, not CSS. Outlook ignores CSS
  dimensions on `<img>` and draws the natural pixel size — a 2× asset would render at
  double size there and correct everywhere else.
- **Badge**: Outlook ignores `border-radius`. A shrink-to-fit badge is a nested one-cell
  table with padding (not `inline-flex`, which the Word engine does not implement); it will
  render square-cornered in Outlook. That is the honest degradation.
- **Amount column**: the fee rows and the line rows must live in the *same* table so their
  right-aligned amount cells share one column edge. Today they are separate blocks, which
  is why the amounts drift.

---

## R-015 — Timezone: a confirmed correctness defect, not a cosmetic one

**Decision**: convert to `Asia/Jakarta` at every render site inside
`internal/notification`. Hardcode the location; do not add configuration.

**Verified here**:

- `pgx/v5`'s `TimestamptzCodec` carries a `ScanLocation` (`pgtype/timestamptz.go:134-136`)
  and the binary path builds values with `time.Unix(...)` (`:270`), which returns a time in
  **`time.Local`**. Nothing in this repository sets a session timezone — `grep` for
  `TimeZone`, `time_zone`, `SET TIME ZONE`, `PGTZ` across the backend returns nothing.
- The runtime image is `gcr.io/distroless/static-debian12:nonroot` with no `TZ` set, so
  `time.Local` is **UTC in production** while a Jakarta developer's machine is **WIB**. The
  defect is invisible locally and only appears in the container.

**The defect, reproduced in this session.** A temporary test rendered a receipt for a
ticket admitting at `2026-04-27 06:00 WIB` (= `2026-04-26T23:00Z`):

```
TZ=UTC          -> receipt sub-line shows: 26 Apr 2026 (WRONG - off by one)
TZ=Asia/Jakarta -> receipt sub-line shows: 26 Apr 2026 (WRONG - off by one)
```

Wrong in **both**, because `receiptSubLine` formats the value in *its own* location, which
is UTC here. A guest holding a Day 2 pass is told it admits on Day 1. This is a
correctness bug in shipped behaviour, not a formatting preference, and it is the single
most important thing this revision fixes.

**Consequences for testing**: any test asserting `WIB` must pin `TZ` to something else, or
it proves only that the host happened to be in Jakarta. The e2e API server inherits the
developer's `TZ` and must be pinned to `UTC` in `playwright.config.ts`, or the Principle
VIII "seen red first" requirement cannot be met.

**Rejected**: adding a DSN `TimeZone` parameter. It changes the PostgreSQL session zone,
which the binary protocol path ignores entirely — it would look like a fix and change
nothing.

---

## R-016 — Currency: one formatter, one non-obvious guard

**Decision (superseded, then re-answered)**: `formatIDR` becomes `IDR 550.000` /
`IDR 15.000,92` — dot thousands, comma decimal, fraction only when non-zero, never
truncated. An intermediate answer on the same day set `IDR 70,000` (comma thousands); it
was reversed when the decimal case surfaced. **This is more than three lines**: dropping
`IntPart()` changes the function's shape, not just two literals.

**Verified here**: `formatIDR` (`service.go:479-491`) is the **only** currency formatter in
the Go backend. The change is the separator literal (`'.'` → `','`), the prefix
(`"Rp "` → `"IDR "`), and the doc comment, which currently claims it "renders an amount the
way the site does" — a statement that becomes false and would invite a later revert.

**The trap**: line 485's condition `digits[i-1] != '-'` is what stops a negative amount
rendering as `-,550,000`. **No test covers it**, so a rewrite that drops it ships green.
The revision must keep the guard and add the missing assertion.

The frontend keeps `Rp`. That divergence is FR-037, deliberate and recorded.

---

## R-017 — The receipt's envelope icon (FR-015a)

**Decision**: draw it with gofpdf vector primitives. Not a font glyph, not a PNG.

**Verified here** against `gofpdf@v1.16.2`: `RoundedRect` (`fpdf.go:1135`),
`SetLineCapStyle` (`:1010`), `SetLineJoinStyle` (`:1029`), `MoveTo` (`:4840`), `LineTo`
(`:4850`) and `DrawPath` (`:4909`) all exist with the signatures the design needs.
ZapfDingbats **is** compiled into the library (`embedded.go`), and its envelope is at
code point `0x29`.

ZapfDingbats is nonetheless the wrong choice: its envelope is a thin outline with a centre
seal, while the design's is a solid filled slate envelope with a light flap. At 9pt they
read as different icons.

**A trap worth writing down**: `SetLineCapStyle` and `SetLineJoinStyle` are **sticky Fpdf
state**. They must be reset after drawing, or every later rule on the page inherits round
ends.

Choosing vector primitives also makes FR-015a's "an icon that cannot be drawn MUST be
omitted" **unreachable by construction** — there is no asset to be missing. Document that
rather than writing a dead fallback branch.

**Deliberate asymmetry, worth recording so neither is later "harmonised" into the other**:
the email pin must be a raster PNG because email clients cannot be trusted with vectors;
the PDF envelope must not be, because a PDF draws vectors natively.

---

# Phase 0 (Revision 3): Brand Refresh Research — 2026-08-19

Opened by a report that the email arrives carrying **four** attachments, and by a new brand
asset supplied in two variants plus a footer change on the e-ticket
(Figma `683-148`). Clarification answers are in [spec.md](./spec.md) §Clarifications,
Session 2026-08-19.

Everything in Revisions 1 and 2 still holds unless contradicted below. As before, every
finding here was verified against a running service, the real assets, or library source —
none is asserted from memory.

## R-018 — The four attachments: the instrument was wrong, not the message

**Decision**: change nothing. FR-001, FR-023a, FR-025 and SC-001 are confirmed, not amended.

**Verified here** against the Mailpit instance holding a real delivered message:

```
GET /api/v1/messages          -> "Attachments": 2
GET /api/v1/message/{id}
  Attachments: receipt-ORD-….pdf (application/pdf, PartID 2)
               tickets-ORD-….pdf (application/pdf, PartID 3)
  Inline:      jive-logo.png    (image/png, ContentID jive-logo.png, PartID 1.2)
               location-pin.png (image/png, ContentID location-pin.png, PartID 1.3)
```

Mailpit's **API** classifies correctly and its own UI badges read `Attachments (2)` /
`Inline images (2)`. What produced the report is the file strip *below* those badges. From
the shipped UI bundle (`/dist/app.js`):

```js
for (let o in t.Attachments) t.Attachments[o].ContentDisposition="Attachment", e.push(…)
for (let o in t.OtherParts)  t.OtherParts[o].ContentDisposition="Other",      e.push(…)
for (let o in t.Inline)      t.Inline[o].ContentDisposition="Inline",         e.push(…)
```

`allAttachments()` deliberately concatenates all three, because Mailpit is a MIME debugger
and exists to expose every part for download. That strip is a developer surface; no mail
client has an equivalent. Gmail and Yopmail were checked by hand and list two.

**Consequence for the plan**: there is **no bugfix here**, so Principle VIII owes no
red-first regression scenario for the attachment count. The existing `toHaveLength(2)`
assertion already reads the document-only array and stays correct. Revision 2's governance
follow-up is closed: `PRD.md` §1.4 and the constitution bullet were re-read and already say
"document attachments".

**Alternatives considered**: dropping the inline parts, or serving the logo from
`FRONTEND_URL` as a remote `<img>`. Both were put to the user and both were declined once
the measurement showed nothing was broken. Recorded because each would have traded a real
requirement (FR-023a's "the real logo image") for a defect that did not exist.

## R-019 — SVG is not available in the email body

**Decision**: FR-025 is narrowed to "an inline image part", removing its inline-SVG
allowance. The pin stays a raster PNG and the logo stays a raster CID part.

The question was raised directly by the requester. The answer is a support matrix, not a
preference:

| Client family | `<svg>` inline | `<img src="data:image/svg+xml">` |
|---|---|---|
| Gmail (web, iOS, Android) | Stripped | Stripped — `data:` URIs are removed from `img` sources |
| Outlook 2007–2021 / Microsoft 365 (Windows) | Not rendered — Word engine | Not rendered |
| Apple Mail, iOS Mail, Thunderbird | Renders | Renders |

FR-023a already records Gmail's `data:` URI stripping, so the second column was settled
before this revision; the first is what makes the allowance unsafe. An implementer
following FR-025 as originally written could have shipped an icon invisible to the two
largest client families and seen it render perfectly on their own Mac.

**This does not change any code** — the shipped pin is already a PNG (R-013 option A). It
removes a trap from the spec.

## R-020 — The new asset: padding, alpha, and the band-colour hack it retires

**Decision**: ship one trimmed, colour-quantised derivative per variant, generated from the
supplied originals. The originals are the source of truth and are not what ships.

**Verified here** with ImageMagick against both supplied files:

| Property | `…_WHITE.png` / `…_BLACK.png` | Retired `jive-logo.png` |
|---|---|---|
| File dimensions | 6400×4290 (**1.4918:1**) | 216×122 (1.7705:1) |
| Content bbox | `5532x3149+434+570` | `211x80+1+13` |
| **Artwork ratio, trimmed** | **1.7568:1** | 1.7705:1 |
| Alpha | `Blend`, mean 0.184 — **real transparency** | mean 1.0 — **fully opaque** |
| Size | ~276 KB each | 18 KB |

Two findings, both consequential:

**The 1.49:1 figure describes the file, not the mark.** The padding is symmetric — 434 px
each side horizontally, ~570 px vertically — so the artwork inside is 1.757:1, within 0.8%
of the mark it replaces. **No layout is forced to change by aspect ratio.** The spec's
FR-023b originally claimed otherwise and has been corrected. Derivatives must be trimmed so
that breathing room is decided in the layout rather than baked into the asset, where each
surface would inherit a different amount of it.

**The new asset has an alpha channel, which retires a documented hack.** R-012 established
that the retired logo had zero transparent pixels, and two things were built on that:

- `pdf.go` pins `pdfBand = #151A26` with the comment *"the design's band AND the logo's own
  opaque background"*;
- `drawBrandMark` fits the mark by **height** because *"the asset's background is opaque, so
  anything taller than the band draws a dark rectangle hanging past it."*

Neither constraint survives a transparent asset. The band colour becomes a free design
choice again, and the mark can be fitted by whichever dimension the layout prefers. Both
comments must be rewritten rather than left to mislead the next reader — they are correct
statements about an asset that will no longer exist.

**Derivative weight**, measured at candidate widths (the mark is flat-colour art, so
64-colour quantisation is visually lossless — checked by eye against the truecolor render):

| Width | Truecolor PNG | Quantised (64) |
|---|---|---|
| 400 px | 24.0 KB | **6.8 KB** |
| 560 px | 34.7 KB | **9.8 KB** |
| 800 px | 50.9 KB | **14.6 KB** |
| 1000 px | 64.4 KB | 18.4 KB |

**One 800×455 quantised derivative per variant (~15 KB) serves all three surfaces.** For
comparison the retired frontend asset is 349 KB — a 23× reduction on every page load.

## R-021 — How large the mark must be, measured rather than guessed

**Decision**: ~280 px wide in the email body; ~200 px on the site header; ~55 mm on the
e-ticket page.

FR-023b requires the lockup's secondary line — `SPONSORED BY` / `-2° MINUS TWO` — to be
legible. That line is roughly 150 px tall in a 5532 px-wide trimmed artwork, so rendered
cap height is `150 × W / 5532`:

| Placement | Displayed width | Secondary-line cap height | Verdict |
|---|---|---|---|
| Email, today | 108 px | ~2.9 px | Illegible |
| Email, proposed | 280 px | ~7.6 px | Legible |
| Site header, today | 98 px | ~2.7 px | Illegible |
| Site header, proposed | 200 px | ~5.4 px CSS px (~10.8 device px at DPR 2) | Legible |
| E-ticket, today | 24.6 mm | ~0.67 mm (~1.9 pt) | Illegible in print |
| E-ticket, proposed | 55 mm | ~1.5 mm (~4.2 pt) | Legible in print |

Confirmed by rendering the trimmed mark at 108 px and 280 px on the real `#151A26` band and
looking at both: at 108 px the secondary line is present but mushy; at 280 px it is clean.

**The site header is deliberately NOT scaled by the email's factor.** A literal reading of
"proportionally scaled" would put a 280 px mark in a 97 px-tall bar, forcing it to ~190 px
and dominating the public chrome. The web surface has a different legibility budget —
DPR ≥ 2 on most devices, and the user can zoom — so 200 px reaches the same *perceived*
legibility at a bar height of ~130 px. **This deviates from the strictest reading of the
user's answer and is flagged in the plan for a nod rather than assumed.**

## R-022 — Growing the e-ticket band inside an absolutely positioned page

**Decision**: raise `bandHeight` from 21 mm to ~38 mm and shift every constant below it by
the same delta. There is room.

`renderTicketPage` positions everything absolutely — this is deliberate (`SetAutoPageBreak`
is off so a tall event name cannot silently spill a ticket onto two pages). The consequence
is that the band's height is not a layout parameter anywhere; it is baked into each
constant below it:

| Element | Current y (mm) |
|---|---|
| Band | `0 … bandHeight` (21) |
| "Ticket N of M" | 29 |
| Accent rule | 40 … 112 |
| QR panel | 46 … 108 |
| Footer band | 272 … 297 |

At 55 mm wide the mark is 31.3 mm tall; with `logoBandInset` 3.5 mm top and bottom the band
becomes 38.3 mm, a delta of **+17.3 mm**. Cross-checked against the design's own proportions
that there is room: frame `683:148` ends its content at y=558/842 (≈197 mm) with the footer
starting at 776/842 (≈274 mm), leaving ~77 mm of whitespace. A 17 mm shift consumes under a
quarter of it.

**The constants must move together or not at all.** Shifting the band without the accent
rule puts a 72 mm rule through the band; shifting the rule without the QR separates them.
Expressing them as offsets from `bandHeight` rather than as literals is the change that
makes this safe, and is worth doing even though it is not strictly required.

## R-023 — Reusing the envelope in a dark band

**Decision**: give `drawEnvelope` explicit body and flap colours; do not fork a second
function.

`drawEnvelope` (R-017) hardcodes `pdfSlate` for the body and white for the flap, which is
correct on the receipt's white page and wrong on the e-ticket's `#151A26` band, where a
slate body has too little contrast. Widening the signature to take both colours is a
two-call-site change inside one package, and keeps a single icon definition — a forked copy
would drift the moment either is adjusted.

**Two traps carried forward**, both verified in the source:

- Cap and join style are **sticky Fpdf state**. R-017 already records the reset. The e-ticket
  footer is drawn last on each page, but the tickets PDF loops pages, so an un-reset style
  would leak into the *next* ticket's rules rather than the current one — which is exactly
  the kind of defect that renders fine in a one-ticket test.
- `drawEnvelope` resets cap, join and line width but leaves `SetDrawColor` white and
  `SetFillColor` at the body colour. Harmless today because of draw order; recorded because
  the second call site changes that order.

## R-024 — The first frontend change in this feature

**Decision**: swap the asset in `site-header.tsx` and grow the bar; no new component, no
`next/image` migration.

Revision 1 stated "**no frontend change**". That is no longer true — FR-023c puts the mark
on the site header too, so this revision touches `frontend/` for the first time and the
frontend test tier becomes relevant to the feature.

**Verified**: exactly one frontend logo reference exists — `site-header.tsx:21`,
`src="/brand/jive-logo.png"`, sized `h-13.75 w-24.5` (55×98 px) inside an `h-24.25` (97 px)
bar. `site-footer.tsx` carries no mark, and no other component references `/brand/` for the
logo. The element is a plain `<img>` with an eslint-disable for `@next/next/no-img-element`;
this revision keeps it that way, because switching to `next/image` for a single static
above-the-fold asset trades a lint suppression for a layout-shift and loader change that
nothing here needs.

**Out of scope, recorded so neither is mistaken for an oversight**: `frontend/app/favicon.ico`
is still the unmodified Next.js scaffold icon, and the document title is still
"Event Ticketing". Neither is a logo call, so "update every logo call" does not reach them —
and a 1.757:1 lockup makes a poor 32×32 favicon regardless, so that would need a
glyph-only crop the brand has not supplied.

## R-021a — The enlargement was reversed once seen

**Decision**: supersedes R-021. The mark is **capped by the height of its container**;
no container grows to fit it.

R-021 measured what the lockup's secondary line needs to be legible and concluded ~280px
in the email, ~200px on the site header and 55mm on the e-ticket. The arithmetic was right
and the conclusion was wrong, which only became visible once it was rendered:

| Surface | Before | Enlarged | Cost |
|---|---|---|---|
| E-ticket band | 21 mm | 38.3 mm | +17.3 mm of every ticket, pushing the page down |
| Email header band | 113 px | 211 px | The message opens on a brand panel |
| Site header bar | 97 px | ~130 px | Chrome before content on every page |

The premise R-021 never questioned is whether the sponsor credit needs to be *read* off
these surfaces at all. It does not: a ticket is scanned at a gate and a navigation bar is
looked past. The credit's job is presence, which it keeps at any size.

Capping by height also buys an invariant the enlargement destroyed: with the mark fitted to
a fixed container, **swapping the asset can no longer change any layout**. The width follows
from the asset's own ratio and nothing else moves. That is now asserted rather than assumed
— see `TestBrandMarkIsCappedByTheBandAndNeverGrowsIt`.

The `bandHeight`-offset refactor from R-022 is what made the reversal a one-line change, so
it is kept: it earned itself in both directions.

## R-025 — The e-ticket footer cannot be covered in `e2e/`

**Decision**: the footer's acceptance lands at the **Go tier**, through the existing
uncompressed test seam. `e2e/` gains email-body assertions only.

This corrects a claim made earlier in this same planning pass, which assumed the tickets PDF
was text-searchable from Playwright because `eticket_test.go:150` asserts
`Contains(string(doc), "JIVE")`. It does — but on a *different document*:

```go
// pdf.go:102
func renderTicketsPDF(order OrderDelivery, tickets []TicketDetail, brand Branding, compress bool) {
    pdf.SetCompression(compress)   // production passes true
```

```go
// export_test.go — test-only seam
func RenderTicketsPDFPlain(…) ([]byte, error) { return renderTicketsPDF(…, false) }
```

The comment above `renderTicketsPDF` states it outright: *"gofpdf compresses content
streams, so what a page SAYS is unreadable in the shipped bytes. Production always
compresses."* Playwright downloads the production document.

| Tier | Can assert | Cannot |
|---|---|---|
| Go, via `RenderTicketsPDFPlain` | Footer strings, geometry, band offsets | — |
| e2e, via `downloadAttachment` | Attachment count, filename, PDF magic, page count | Anything the page *says* |
| e2e, via `mail.HTML` | Mark dimensions, `cid:` pairing — the body is plain HTML | — |

This is the same split `tasks.md` §Known limits already records for the day-boundary defect,
which was pinned at the Go tier for exactly this reason and asserted through the email body
in `e2e/`. **Following that precedent is the correct move; inventing a PDF text extractor
for Playwright is not**, and would produce a test asserting against a decompression routine
rather than against the product.

## Summary of decisions — Revision 3

| ID | Decision | Touches |
|---|---|---|
| R-018 | No change; the report measured Mailpit's debug view, not the message | none |
| R-019 | FR-025 narrowed to an inline image part; SVG ruled out | spec only |
| R-020 | Trimmed, 64-colour derivatives; alpha retires the band-colour hack | `assets/`, `pdf.go`, `frontend/public/brand/` |
| R-021 | 280 px email / 200 px header / 55 mm e-ticket, measured for legibility | `service.go`, `pdf.go`, `site-header.tsx` |
| R-022 | Band 21 → 38.3 mm; express page constants as offsets from it | `pdf.go` |
| R-023 | `drawEnvelope` gains explicit colours; single definition | `receipt_pdf.go`, `pdf.go` |
| R-024 | First frontend change; plain `<img>` retained | `site-header.tsx` |
| R-025 | E-ticket footer covered at the Go tier; `e2e/` gets body assertions only | `*_test.go`, `guest-purchase.spec.ts` |

## R-026 — The subject line was never specified, and the implementation picked one

**Decision**: the subject becomes `[<order number>] E-receipt & E-Ticket for <event name>`,
composed at the send site from `order.OrderNumber` and `tickets[0].EventName` (FR-005a).

Three findings, in the order they matter:

**1. No requirement governed the subject before now.** `service.go:215` sends
`fmt.Sprintf("Your tickets for %s", tickets[0].EventName)`, and `grep -i subject spec.md`
returned nothing across all three prior revisions. This was an implementation choice that
nobody had recorded, which is why changing it needs a new FR rather than an amendment to an
existing one.

**2. `contracts/notification.md:113` documented that choice as though it were a contract.**

```go
Subject: "Your tickets for " + tickets[0].EventName,
```

The contract is downstream of the spec, so it is the file that moves — but it is worth
naming, because a reader consulting the contract rather than the spec would have concluded
the old subject was specified.

**3. No new data plumbing is required.** `OrderDelivery` already carries `OrderNumber` (it
is what both attachment filenames are built from at `service.go:225`/`229`), and
`tickets[0].EventName` is already read on the line being replaced. Orders are event-scoped
(spec 007), so every ticket in an order carries the same event name and index 0 is safe —
the existing code already depends on that.

**Assertable at the e2e tier.** `MailMessage.Subject` is already declared in
`e2e/support/mail.ts` and is read back off Mailpit on every delivery; no spec asserts it
today. This is the cheapest new coverage in the feature — one `expect` on a field the helper
already parses.

**Alternatives considered**: hardcoding "JIVE 2026" (rejected in clarification — the
platform is multi-event and every other surface prints the order's own event); the order's
UUID in the brackets (rejected — it matches nothing the buyer can see on the receipt or in
the filenames).

## R-027 — The phone mask direction, and the two tests that cannot catch it

**Decision**: `MaskPhone` keeps every character except the last 4, which become asterisks.
`phoneKeepLeading`/`phoneKeepTrailing` collapse to a single `phoneMaskTrailing = 4`.

**What the reported output actually was.** `14239***6621` is not a bug in the code — it is
`phoneKeepLeading=5` / `phoneKeepTrailing=4` applied faithfully to a stored value with no
`+62` prefix. The defect is that FR-033 specified the wrong shape, so the fix is to the rule,
not to its implementation.

| Stored value | Old rule | New rule |
|---|---|---|
| `+6281234567890` | `+62812****7890` | `+628123456****` |
| `+628123456789` | `+6281****6789` | `+62812345****` |
| `081234567890` | `08123***7890` | `08123456****` |
| `081234567` | `08123****` | `08123****` (coincides) |
| `0812` | `0***` | `****` |
| `0` | `*` | `*` |

**The short-value fallback changes shape and must be restated, not inherited.** The old code
routed anything at or under 9 characters through `maskFrom(phone, phoneKeepLeading)`. Under
the new rule that branch is gone: any value longer than 4 keeps `len-4` characters, and a
value of 4 or fewer is masked entirely (FR-033). `0812 → 0***` becoming `0812 → ****` is a
deliberate consequence, not an oversight — with only 4 characters, "everything except the
last 4" is nothing.

**`TestMaskingNeverLengthensAValue` still holds** and should not be touched: one asterisk
replaces one character, so output length equals input length exactly, as the receipt's
column alignment requires.

**The finding worth acting on: two existing tests assert against the function under test.**

```go
// receipt_pdf_test.go:113
assert.Contains(t, text, notification.MaskPhone("+628123456789"))
// service_test.go:507
maskedPhone := notification.MaskPhone("081234567890")
```

Both pass whatever `MaskPhone` returns. They verify that the two *surfaces* agree with the
helper — which is genuinely what SC-008 asks of them — but neither can fail if the helper's
shape is wrong. The literal shapes exist in exactly one place, `mask_test.go`'s table. So
the new e2e assertion MUST use a literal string (`08123456****`), never a call through the
helper, or the acceptance gate inherits the same blind spot.

**Privacy cost, recorded because it was chosen and not overlooked.** The mask now discloses
`len-4` characters where it disclosed 9. For a 12-digit Indonesian mobile that is 8 visible
characters instead of 5, and the whole prefix and network block are legible on a receipt the
buyer may forward. The clarification accepted this in exchange for the buyer recognising
their own number. FR-033 and the spec's Assumptions both say so, so a later reader does not
"restore" the old rule as a privacy fix.

## Summary of decisions — Revision 4

| ID | Decision | Touches |
|---|---|---|
| R-026 | Subject `[<order number>] E-receipt & E-Ticket for <event name>`, event read from the order | `service.go`, `contracts/notification.md`, `guest-purchase.spec.ts` |
| R-027 | `MaskPhone` masks the trailing 4 only; ≤4 chars masked entirely; e2e asserts a literal | `mask.go`, `mask_test.go`, `guest-purchase.spec.ts` |

## R-028 — The event name's crimson, and the constant it retires

**Decision**: `#475569` as its own constant (`pdfEventName`), and `pdfBrand` is deleted.

`pdfBrand = [3]int{203, 28, 79}` has **exactly one consumer** in the whole package —
`pdf.go:252`, immediately before the uppercased event name. Recolouring that call site leaves
the constant with no reader, so the choice is not "recolour or not" but "recolour and delete,
or recolour and leave dead code that encodes the retired decision". The same reasoning R-027
applied to the short-phone branch applies here.

**Consequence worth stating plainly**: after this, the e-ticket carries **no brand crimson at
all** outside the logo image itself. That matches the design, and SC-024 is written to fail
if any survives.

**Why not reuse `pdfAccent` (`#334155`)**: FR-022b pins that value to the QR accent rule, and
the spec's own wording is "`#334155`, not red" — it was chosen for structure, not for text.
Two visibly distinct slates doing two different jobs is the design's intent, not an
accident. The clarification confirmed this rather than assumed it.

**`emailBrand = "#cb1c4f"` is untouched.** It is the email body's Total Payment accent, a
different surface with its own design, and no clarification reached it.

## R-029 — The footer's emphasis is inverted, not merely off-shade

**Decision**: labels to `#d0d1d4`, values and the envelope to pure white.

The report was "the colour is a bit different". It is not a shade — the two roles are
swapped:

| Role | Design (`683:247`) | Code | Direction |
|---|---|---|---|
| Label (`JIVE`, `CUSTOMER SERVICE`) | white @ 80% opacity | `pdfBandFg` `#eceef3` | code is **brighter** |
| Value (site URL, support address) | pure `#ffffff` | `pdfBandDim` `#969cac` | code is **much dimmer** |
| Envelope icon | white | `pdfBandDim`, flap in band colour | code is dimmer |

The design makes the label recede so the value carries the eye. The code does the opposite,
which leaves `help@manjo.co.id` — the only value on the document a buyer needs to *act* on —
as the dimmest text in the band. It is worst in greyscale print, which is how a large share
of e-tickets are consumed.

**The composite, computed rather than eyeballed.** Figma expresses the label as white at 80%
opacity over the band; a PDF has no alpha here, so the flattened value must be worked out:

```
band #151a26 = (21, 26, 38)
label = 0.8 x 255 + 0.2 x band
      = (208.2, 209.2, 211.6)  →  (208, 209, 212) = #d0d1d4
```

**`pdfBandFg` survives; `pdfBandDim` does not.** `pdfBandFg` has a third consumer at
`pdf.go:430` (the text-wordmark fallback when the logo asset is missing) and a fourth at
`receipt_pdf.go:183`, so it stays as-is. `pdfBandDim`'s only two consumers are the footer
value and the envelope, both of which become white — so it is deleted, and a new
`pdfBandLabel = #d0d1d4` takes the label role.

## R-030 — The e-ticket's type scale and palette, measured node by node

**Decision**: adopt the design's sizes and weights wholesale (FR-003b). The palette finding
below is **flagged, not adopted** — no clarification covers it.

The Figma frame is 595x842, which is A4 at 72 dpi, so its px values are points 1:1 and
compare directly with gofpdf's `SetFont` sizes.

| Element | Design size/weight | Code | Ratio | Design colour | Code colour |
|---|---|---|---|---|---|
| `Ticket N of M` | 18 Bold | 16 B | 0.89 | `#1e293b` | `pdfInk` `#18181b` |
| Identity label | 11 SemiBold | 7.5 regular | 0.68 | `#64748b` | `pdfGray` `#6e6e6e` |
| Identity value | 16 Bold | 12 B | 0.75 | `#0f172a` | `pdfInk` `#18181b` |
| Event name | 14 Bold | 9.5 B | 0.68 | `#475569` | `pdfBrand` `#cb1c4f` |
| Ticket-type heading | 22 Bold | 20 B | 0.91 | `#0f172a` | `pdfInk` `#18181b` |
| `VALID FOR` label | 11 SemiBold | 7.5 regular | 0.68 | `#64748b` | `pdfGray` `#6e6e6e` |
| Valid-for value | 14 Bold | 12 B | 0.86 | `#0f172a` | `pdfInk` `#18181b` |
| Footer label | 10 SemiBold | 7.5 B | 0.75 | white @80% | `pdfBandFg` `#eceef3` |
| Footer value | 12 Medium | 9 regular | 0.75 | `#ffffff` | `pdfBandDim` `#969cac` |
| Divider rule | — | — | — | `#e2e8f0` | `pdfLine` `#e0e0e4` |
| Accent bar | 8 wide (2.82mm), 260 tall (91.7mm) | 3.0mm, 72.0mm | — | `#334155` | `pdfAccent` `#334155` ✓ |
| QR | 200 (70.6mm) | 62.0mm | 0.88 | — | — |

**Two findings fall out of this table that the spot check could not have produced.**

**1. The ratios are not a scale factor.** They range 0.68 to 0.91 with no pattern. This is
accumulated drift from hand-tuning, not a deliberate "the print version is smaller"
decision — which is exactly why FR-003b asks for the design to be re-read rather than for the
three known mismatches to be nudged.

**2. The palette is a different colour family, and nothing in the spec covers it.** The
design is Tailwind **slate** throughout — `#0f172a`, `#1e293b`, `#334155`, `#475569`,
`#64748b`, `#e2e8f0`. The code is **zinc/neutral** — `#18181b`, `#6e6e6e`, `#e0e0e4` — with
the single exception of `pdfAccent`, which is already the design's slate because FR-022b
pinned it there.

FR-019b and FR-022f fix the event name and the footer, which is what was asked. They leave
the identity block, the pagination heading, the valid-for row and the divider on the zinc
family. **This is recorded as a question for the user, not resolved here** — see plan.md
Revision 6 "The one thing that needs a nod".

## R-031 — Inter in gofpdf: mechanism, and the one asset trap

**Decision**: `AddUTF8FontFromBytes` with **static** Inter TTFs shipped through the existing
`//go:embed` assets path.

```go
// fpdf.go:1812 — takes bytes, not a path
func (f *Fpdf) AddUTF8FontFromBytes(familyStr, styleStr string, utf8Bytes []byte)
```

This matters because the alternative, `AddUTF8Font(family, style, fileStr)`, needs a real
file at render time and would drag a `SetFontLocation` and a deploy-time asset directory into
a service that currently ships everything embedded.

**Subsetting is automatic.** gofpdf tracks `usedRunes` per font (`fpdf.go:2217`, `:2428`,
`:4109`) and writes only the glyphs actually drawn. A document therefore grows by a few KB
per weight, not by the ~300 KB of a full Inter face. No manual subsetting step is needed.

**The trap: use the static TTFs, not `InterVariable.ttf`.** gofpdf's `utf8fontfile` parses
the classic TrueType tables. A variable font carries `fvar`/`gvar` on top of those, and
gofpdf neither reads nor applies them — the best case is that every weight renders as the
font's default instance, so Bold and SemiBold come out identical and nothing errors. That
failure is silent and looks like a styling bug, which is why it is recorded here rather than
discovered during implementation. Ship `Inter-Regular.ttf`, `Inter-Medium.ttf`,
`Inter-SemiBold.ttf`, `Inter-Bold.ttf`, `Inter-Italic.ttf` from the release's `static`
directory.

**Licensing is not a blocker**: Inter is SIL Open Font License, which permits embedding.

## R-032 — Four design weights, four gofpdf style slots, and they do not line up

**Decision**: register Inter Regular/Bold/Italic under family `"Inter"`, and Medium and
SemiBold as **their own families**.

gofpdf's style string understands only `""`, `"B"`, `"I"` and `"BI"` — there is no slot for
Medium (500) or SemiBold (600). The design uses four weights:

| Design weight | Used by | Registration |
|---|---|---|
| Bold (700) | headings, identity values, event name, valid-for value | `AddUTF8FontFromBytes("Inter", "B", …)` |
| SemiBold (600) | every uppercase label, both surfaces | `AddUTF8FontFromBytes("InterSemiBold", "", …)` |
| Medium (500) | footer values | `AddUTF8FontFromBytes("InterMedium", "", …)` |
| Regular (400) | receipt body text | `AddUTF8FontFromBytes("Inter", "", …)` |

Italic is not in the e-ticket design but the code uses one (`"Helvetica", "I", 8.5`), so
`Inter-Italic` is registered too rather than silently falling back.

**The `SetFont("Helvetica", style, size)` call at `pdf.go` with a *variable* style** is the
one call site that cannot be mechanically rewritten — it takes `style` as a parameter, so the
family it needs depends on the caller. It must be read, not sed-replaced.

## R-033 — `latin1()` must be removed, and leaving it is not the safe option

**Decision**: delete `latin1()` and its 22 call sites in the same change as the font.

It exists because gofpdf's core fonts are single-byte cp1252. For non-ASCII it does
`out.WriteByte(byte(r))` — which writes bytes in the `0x80`–`0xFF` range **raw**, producing a
Go string that is not valid UTF-8. Under a core font that is correct. Under an embedded UTF-8
font, gofpdf decodes the string as UTF-8 and those bytes are malformed, so the output is
garbage rather than an approximation.

So "leave it in, remove it later" is not the low-risk option it looks like: it is the option
that ships broken glyphs for exactly the names the change was supposed to fix.

| File | `latin1()` call sites |
|---|---|
| `pdf.go` | 12 |
| `receipt_pdf.go` | 10 |

**The upside, which arrived as a side effect**: today an Indonesian or European name carrying
a diacritic is transliterated to its nearest plain letter on a document the holder presents
at a gate. With Inter embedded it renders as stored. FR-003a states this so it is not
mistaken for incidental.

## R-034 — Letter-spacing cannot be reproduced, and no workaround is worth it

**Decision**: accept no tracking. Record it rather than fake it.

Every uppercase label in the design carries tracking — 0.55px on the identity and valid-for
labels, 0.5px on the footer labels, 0.35px on the event name. gofpdf exposes
`SetWordSpacing` (the PDF `Tw` operator) but **not** character spacing (`Tc`); there is no
method for it in v1.16.2.

The workarounds are worse than the gap:

- **Draw character by character with manual advances** — breaks `MultiCell` wrapping, which
  the event name depends on for long names (the spec's own edge case), and puts a per-glyph
  loop in the hot path of every label on both documents.
- **Insert thin spaces between letters** — corrupts the text layer, so the address is no
  longer selectable or searchable in a PDF reader.

At 10–11 pt the absent tracking is a sub-pixel difference per glyph. FR-003b's acceptance is
therefore about **size and weight**, and this is named as a known limit so a later reader does
not treat it as an unfinished item.

## R-035 — What catches metric drift, and what does not

Inter is wider than Helvetica at equal point size, so every hand-tuned coordinate moves.

**Catches it:**

- `receipt_pdf_test.go` table geometry — SC-016 (left inset) and SC-017 (`Price` centred,
  `Qty`/`Total` right-aligned) are measured through the `TableInsets` / `HeaderColumnRights`
  seams and will fail loudly on drift.
- `customerServiceBlock` — computes its own width from `GetStringWidth`, so it **self-adjusts**
  and FR-022e's short/long-address test keeps passing by construction.
- `eticket_test.go` band-geometry seam (T132) — the band and mark are measured, not assumed.

**Does not catch it:**

- Anything absolutely positioned with no assertion below it. The e-ticket's page constants are
  offsets from `bandHeight` (R-022) but the offsets themselves are literals: a heading that
  grows 16 → 18 pt pushes into the accent rule with nothing to notice.
- `e2e/` — the shipped PDF is compressed (R-025). Playwright sees attachment count, filename,
  magic bytes and page count, and nothing about layout.

**Therefore the type-scale change needs a visual diff, not just a green suite.** Render the
e-ticket before and after and compare against Figma `683:148` by eye — quickstart Revision 6
Step 3. This is the same conclusion R-021 reached about the mark's legibility, and for the
same reason: no assertion in this repo vouches for "looks like the design".

## Summary of decisions — Revisions 5 and 6

| ID | Decision | Touches |
|---|---|---|
| R-028 | Event name `#475569` as `pdfEventName`; `pdfBrand` deleted | `pdf.go` |
| R-029 | Footer emphasis inverted back: labels `#d0d1d4`, values and envelope white; `pdfBandDim` deleted | `pdf.go` |
| R-030 | E-ticket type scale adopted wholesale; **zinc-vs-slate palette flagged, not adopted** | `pdf.go` |
| R-031 | `AddUTF8FontFromBytes` + static Inter TTFs via `//go:embed`; never the variable font | `assets/`, `pdf.go`, `receipt_pdf.go` |
| R-032 | Regular/Bold/Italic as `Inter`; Medium and SemiBold as their own families | same |
| R-033 | `latin1()` deleted, 22 call sites, same change as the font | `text.go`, `pdf.go`, `receipt_pdf.go` |
| R-034 | Letter-spacing not reproducible; accepted and recorded | — |
| R-035 | Geometry drift needs a visual diff; the suite alone does not vouch for it | `quickstart.md` |

## R-036 — The email rule inverts, and `maskFrom` dies with it

**Decision**: `MaskEmail` masks the last 3 characters of the local part, keeping at least one
character visible. `emailKeep` and `maskFrom` are deleted.

| Local part | Old (keep first 5) | New (mask last 3) |
|---|---|---|
| `dimasprasetyo` (13) | `dimas********` | `dimasprase***` |
| `alexpradita` (11) | `alexp******` | `alexprada***` |
| `dimas` (5) | `dimas` — **printed whole** | `di***` |
| `budi` (4) | `b***` | `b***` (coincides) |
| `abc` (3) | `a**` | `a**` (coincides) |
| `ab` (2) | `a*` | `a*` (coincides) |
| `a` (1) | `*` | `*` (coincides) |

**The rule closes a disclosure, not just an asterisk count.** Under keep-first-5 a local part of
exactly 5 characters returned unchanged — `maskFrom` has an explicit `len == keep` branch that
returns `s`. A masking function returning its input is the one outcome such a function exists
to prevent, and no requirement had ever noticed. The new floor — at least one character
hidden, always — removes the branch entirely.

**`maskFrom` becomes dead code and must go.** Revision 4 deleted its phone caller; this deletes
its two email callers (`mask.go:46`, `:50`). Nothing else in the package references it. Left
behind, it would be a helper encoding a superseded rule, sitting in the same file as the rule
that replaced it.

**The floors are arithmetic, and worth writing down**: asterisks = `min(3, len-1)`, except a
one-character local part, which cannot both keep a character and hide one, and is masked
entirely. Four of the seven cases above coincide with the old rule — which is exactly why the
test fixtures need checking rather than trusting (R-037).

## R-037 — The e2e fixture is blind to this change

**Decision**: change `defaultHolder.email` from `budi@example.com` to a local part longer than
four characters, or the e2e assertion cannot be red.

`e2e/support/journey.ts:26` carries `budi@example.com`. Its local part is 4 characters, and
the table above shows 4-character locals render `b***` under **both** rules. An e2e assertion
written against the current fixture would be green before and after the fix — the "never seen
red" failure Principle VIII names by hand, and the second time this feature has walked into
it (R-027 caught the first).

**The blast radius is one line.** `defaultHolder.email` is referenced three times and every one
of them is by identity, not by literal:

```
guest-purchase.spec.ts:124  expect(order.buyer_email).toBe(defaultHolder.email)
guest-purchase.spec.ts:157  waitForMail(defaultHolder.email)
guest-purchase.spec.ts:245  waitForMail(defaultHolder.email, { minCount: 2 })
```

No spec hardcodes the address. Changing the constant is therefore safe, and it makes every
spec that fills a holder form exercise a mask that actually hides something.

`siti@example.com` (the second holder) stays as it is: holder addresses are printed in **full**
on the e-ticket by FR-034, so its length has no bearing on any mask.

**Alternative considered and rejected**: leave the fixture and cover the email at the Go tier
only. It would work, but it declines free acceptance coverage on a covered flow for no reason
other than not editing a constant — and it would leave `mail.HTML` asserting the phone mask
while silently ignoring the email mask beside it.

## R-038 — `MaskEmail` and `MaskPhone` stay two functions

**Decision**: keep them separate. Do not extract a shared "mask the trailing N" helper.

After this change the two are the same shape — mask the tail, keep the head — which invites a
merge. They differ in the one place that matters, their floor:

| | Trailing masked | Floor when the value is too short |
|---|---|---|
| Phone | 4 | masked **entirely** at 4 characters or fewer |
| Email local part | 3 | keeps **one character visible**; masked entirely only at length 1 |

Those floors come from different requirements, not from an accident: the phone's is arithmetic
("everything except the last four" is nothing at four), while the email's was asked for
directly — a two-character local part renders one asterisk.

A shared helper would carry the difference as a boolean parameter, which reads at every call
site as `maskTrailing(local, 3, true)` — a literal true that no reader can decode without
opening the definition. Two short functions, each obvious on its own, are the better trade.
Recorded because the symmetry is genuinely tempting and the merge looks like a cleanup.

## R-039 — The same two tests still cannot catch it

`receipt_pdf_test.go:112` and `service_test.go:510` call `MaskEmail` as their own oracle,
exactly as their phone counterparts do:

```go
assert.Contains(t, text, notification.MaskEmail("alexpradita@gmail.com"))
maskedEmail := notification.MaskEmail("budi@example.com")
```

Both pass whatever the helper returns. They stay as they are — proving the two surfaces agree
with one helper is what SC-008 asks of them — and `TestMaskEmail` remains the only place the
literal shapes are pinned, so **four of its seven cases change** (`bud`, `a` and the empty value coincide between the two rules).

`service_test.go:510`'s fixture is `budi@example.com`, whose output coincides under both
rules, so even a literal rewrite there would not be red. `receipt_pdf_test.go:112`'s
`alexpradita@gmail.com` does change (`alexp******` → `alexprada***`) and is the better fixture
if a literal assertion is wanted at that tier.

`TestMaskingNeverLengthensAValue` still holds: asterisks replace exactly the characters they
hide, so a masked value is never longer than the stored one.

## Summary of decisions — Revision 7

| ID | Decision | Touches |
|---|---|---|
| R-036 | `MaskEmail` masks the trailing 3, floor of one visible character; `emailKeep` and `maskFrom` deleted | `mask.go` |
| R-037 | `defaultHolder.email` lengthened so the e2e assertion can be red | `e2e/support/journey.ts` |
| R-038 | Two mask functions, not one shared helper — the floors differ by requirement | `mask.go` |
| R-039 | `TestMaskEmail` rewritten (4 of 7 cases change); the two oracle-style tests left alone | `mask_test.go` |

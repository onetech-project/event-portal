# Contracts: Receipt + E-Ticket Email Attachments

**Feature**: [../spec.md](../spec.md) | **Plan**: [../plan.md](../plan.md)

## HTTP contract: unchanged

This feature adds no endpoint and changes no request or response body. For completeness,
the three entry points that reach the changed code:

| Endpoint | Change |
|---|---|
| `POST /api/v1/admin/orders/:id/resend-email` | **None.** Still `{ message, sent_to }`. |
| `POST /api/v1/ticket/resend-email` | **None.** Still the constant non-disclosing body. |
| `POST /api/v1/payment/notification` (webhook) | **None.** Delivery still runs in the post-`200 OK` goroutine. |

**One additive wire change, outside this domain** (R-009): `GET /ticket/order/:order_id`
gains a nullable `description` on each `items[]` entry, because the receipt's line
descriptor has no other source. Additive and nullable, so no existing consumer breaks; the
frontend ignores unknown fields.

```jsonc
// PublicOrderItem — added field only
{
  "kind": "ticket",
  "ticket_type_name": "Jive Jakarta International Vape (Day 1)",
  "package_name": null,
  "quantity": 2,
  "unit_price": { /* … */ },
  "subtotal": { /* … */ },
  "admission_starts": ["2026-04-26T10:00:00+07:00"],
  "description": "Expo Entrance Ticket"        // NEW, nullable
}
```

## Internal contract: `notification.OrderProvider`

The consumer-declared interface in
[service.go](../../../backend/internal/notification/service.go), satisfied by
`notificationOrderAdapter` in [adapters.go](../../../backend/cmd/api/adapters.go).

```go
type OrderProvider interface {
    // UNCHANGED signature; the returned struct widens (see data-model.md).
    OrderForDelivery(ctx context.Context, orderID uuid.UUID) (OrderDelivery, error)
    MarkEmailSent(ctx context.Context, orderID uuid.UUID) error
    OrderIDByNumber(ctx context.Context, orderNumber string) (uuid.UUID, error)
}
```

**No method is added.** The payment summary (R-002) rides on the widened `OrderDelivery`
rather than becoming a fourth method, so the fake in `service_test.go` needs no new stub
and the interface stays the size it was.

**Contract obligations on the adapter**:

| Obligation | Consequence if violated |
|---|---|
| `Event.Venue` / `Event.Address` populated for every PAID order | Body renders an event block with a missing location (FR-025) |
| `Items[].AdmissionStarts` carried through, not collapsed | Per-line dates regress to a single date, undoing spec 015 |
| `Payment.Method` never empty — fall back provider → `"QRIS"` | Receipt shows a blank Transaction Details cell (FR-010) |
| `Fees` and `Subtotal` copied verbatim from the frozen order | Receipt stops reconciling with the charged amount (FR-014, FR-038) |
| `BuyerPhone` `""` rather than a placeholder when unrecorded | Receipt/body print an empty or fake row (FR-027) |

## Internal contract: `notification.TicketProvider`

```go
type TicketProvider interface {
    // UNCHANGED, in signature and in returned fields.
    TicketDetailsForOrder(ctx context.Context, orderID uuid.UUID) ([]TicketDetail, error)
}
```

Ordering is part of the contract even though the type cannot express it: the slice order
**is** the e-ticket page order, and therefore the `Ticket N of M` numbering (FR-016). It
must remain stable across a resend, or a buyer's page 2 becomes page 3 on the copy they
print. `ListDetailsByOrderID`'s existing ordering is the guarantee; a task must pin it with
a test rather than leave it incidental.

## Internal contract: render functions

```go
// UNCHANGED signature. Now renders the Figma 683-148 layout, with no price (FR-021).
func RenderTicketsPDF(order OrderDelivery, tickets []TicketDetail) ([]byte, error)

// NEW. Renders Figma 688-671.
func RenderReceiptPDF(order OrderDelivery, brand Branding) ([]byte, error)

// NEW. FR-033; pure, no error return — an unmaskable value is masked, never passed through.
func MaskEmail(addr string) string
func MaskPhone(phone string) string
```

**Invariants both renderers must hold**:

1. **Deterministic.** Same input ⟹ same bytes, apart from any PDF-internal timestamp.
   This is what makes SC-009 (a resend shows the original figures) testable by comparison.
2. **Pure with respect to I/O.** No database, no network, no disk write. QR images are
   generated in memory and never persisted — `tickets.qr_code_url` stays NULL.
3. **Total.** Every error is returned, never panicked. `SendTicketEmail` must be able to
   abort cleanly before `Send` (FR-006).
4. **`RenderReceiptPDF` emits no ticket code and no QR.** Money on the receipt, admission
   on the ticket.
5. **`RenderTicketsPDF` emits no monetary figure at all** (FR-021, SC-007) — including for
   package lines, where the old layout would have had nothing sensible to print anyway.

## Message contract

What `SendTicketEmail` hands to `Mailer.Send`:

```go
Message{
    To:      order.BuyerEmail,                       // unchanged: the primary contact, alone
    Subject: "Your tickets for " + tickets[0].EventName,
    HTMLBody: buildEmailBody(order, tickets, brand), // rewritten to Figma 741-5105
    Attachments: []Attachment{
        {Filename: "receipt-" + order.OrderNumber + ".pdf",  ContentType: "application/pdf", Content: receiptPDF},
        {Filename: "tickets-" + order.OrderNumber + ".pdf",  ContentType: "application/pdf", Content: ticketsPDF},
    },
}
```

**Assertable properties** (these are what the tests in
[../quickstart.md](../quickstart.md) check):

- `len(Attachments) == 2`, always — never 1, never 3.
- Both filenames contain the order number and differ from each other (FR-004).
- Both are `application/pdf` and both begin with the `%PDF-` magic bytes.
- `HTMLBody` contains no ticket code and no `data:image` QR (FR-030).
- Ordering: receipt first. Not required by the spec, but fixed here so the assertions can
  be positional rather than order-tolerant, and so two mail clients don't show the buyer a
  different first attachment.

## Configuration contract

New `pkg/config` keys, all optional with defaults (R-004):

| Key | Default | Used by |
|---|---|---|
| `BRAND_SITE_NAME` | `JIVE` | e-ticket footer, email header |
| `BRAND_SITE_URL` | `https://www.jive.co.id` | e-ticket footer |
| `BRAND_SUPPORT_EMAIL` | `help@manjo.com` | receipt footer, e-ticket footer |
| `BRAND_LEGAL_ENTITY` | `PT Manjo Teknologi Indonesia` | receipt footer |
| `BRAND_ATTRIBUTION` | `Powered By Manjo` | receipt footer, email footer |
| `BRAND_LOGO_PATH` | *(empty)* | PDF header bands; empty ⟹ text wordmark |

Every key has a working default, so the API starts and sends correctly with none of them
set — matching how the rest of `pkg/config` behaves and keeping this out of the deploy
critical path.

---

# Revision 2 — Design-Review Contracts (2026-08-12)

## HTTP contract: still unchanged

No endpoint, request or response changes. The `PublicOrderItem.description` field added in
Revision 1 stands.

## `notification.Message` — gains inline parts

The mail layer needed no change in Revision 1. It does now: the body references a logo (and,
pending the pin decision, an icon) by content ID, and those are **not** attachments.

```go
type Message struct {
    To          string
    Subject     string
    HTMLBody    string
    Attachments []Attachment  // DOCUMENTS only — the receipt and the e-tickets
    Inline      []Attachment  // NEW: images the body references as cid:<Filename>
}
```

`Inline` reuses `Attachment` rather than introducing a parallel type — the fields are
identical and the distinction is which slice a part is in, which is exactly the distinction
the wire makes.

**Contract obligations on `SMTPMailer.Compose`:**

| Obligation | Consequence if violated |
|---|---|
| Call `EmbedReader` (not `AttachReader`) for every `Inline` entry | The image becomes a downloadable attachment; the buyer sees three files and the body shows a broken image |
| Pass `mail.SetCopyFunc`, exactly as the attachment loop already does | go-mail's default `CopyFunc` drains the reader once; a re-written message yields an empty image part |
| `Filename` MUST carry a real image extension (`.png`) | The part's `Content-Type` comes from `mime.TypeByExtension`; without it the part is `application/octet-stream` and Outlook renders nothing |
| The `cid:` reference in `HTMLBody` MUST equal the `Filename` byte-for-byte | go-mail derives `Content-ID` from the name; a mismatch is a broken image with no error |

**Verified message shape** (probe against the running Mailpit, R-011):

```
multipart/mixed
├── multipart/related
│   ├── text/html
│   └── image/png        Content-Disposition: inline; Content-ID: <jive-logo.png>
├── application/pdf      Content-Disposition: attachment; receipt-<order>.pdf
└── application/pdf      Content-Disposition: attachment; tickets-<order>.pdf
```

Mailpit reports the PDFs under `Attachments` and the image under `Inline`. **`Attachments`
stays at length 2**, so FR-001/SC-001 and the existing e2e assertion hold unchanged.

## `notification.Branding` — gains asset paths

```go
type Branding struct {
    SiteName     string
    SiteURL      string
    SupportEmail string
    LegalEntity  string
    Attribution  string
    LogoPath     string  // now a REQUIRED asset (FR-023a), not an optional ornament
    PinPath      string  // pending the FR-025 pin decision; empty omits the icon
}
```

`LogoPath` keeps its fallback-to-wordmark behaviour on a read failure — FR-023a says a
missing asset must not fail the send — but shipping on that fallback does **not** satisfy
FR-023a. It is a failure path, not a supported mode.

## Render function contracts

```go
// UNCHANGED signatures.
func RenderTicketsPDF(order OrderDelivery, tickets []TicketDetail, brand Branding) ([]byte, error)
func RenderReceiptPDF(order OrderDelivery, brand Branding) ([]byte, error)

// buildEmailBody now returns the body AND the inline parts it references, so the two
// cannot drift: a cid: added to the HTML without a matching part is unrepresentable.
func buildEmailBody(order OrderDelivery, tickets []TicketDetail, brand Branding) (html string, inline []Attachment)
```

Returning the parts alongside the HTML is the point: the failure mode this prevents — a
`cid:` reference with no corresponding part, which renders as a broken image and raises no
error anywhere — is otherwise only caught by looking at a real message in a real client.

**New invariant for both PDF renderers**: `SetLineCapStyle` and `SetLineJoinStyle` are
sticky `Fpdf` state. Any function that sets them MUST restore `"butt"` / `"miter"` before
returning, or every subsequent rule on the page inherits round ends.

## Time rendering contract

One unexported package-level location, resolved once:

```go
// jakarta is the display zone for every time on every surface (FR-020a, FR-024b).
// Resolved once with a FixedZone fallback so LoadLocation can never fail on the
// delivery path. Hardcoded, not configured: a wrong display zone would silently
// misstate a financial document.
var jakarta = sync.OnceValue(func() *time.Location { ... })
```

**Every formatter MUST call `.In(jakarta())` before `Format`.** The incoming `time.Time`
carries whatever location `pgx` scanned it into — `time.Local`, which is UTC in the
container and Asia/Jakarta on a developer's machine. Formatting without conversion is
host-dependent output, and for `receiptSubLine` it is an off-by-one-day defect.

| Formatter | Layout | Example |
|---|---|---|
| `FormatTicketWindow` (same-day) | `02 Jan 2006 @ 15:04` + `" - "` + `15:04 MST` | `26 Apr 2026 @ 10:00 - 21:00 WIB` |
| email order stamp | `02 Jan 2006 15:04 MST` | `04 Jul 2026 06:56 WIB` |
| receipt transaction stamp | `02 Jan 2006, 15:04 MST` | `20 Apr 2026, 14:30 WIB` |
| receipt "Last updated" | `15:04, 02 Jan 2006 MST` | `14:35, 20 Apr 2026 WIB` |
| receipt sub-line date | `02 Jan 2006` | `27 Apr 2026` |

The email and receipt stamps differ by one comma. They currently share a formatter; they
must not, or the difference lives in a caller's memory rather than in code.

## Currency contract

```go
// formatIDR renders "IDR 550.000" / "IDR 15.000,92" (spec 016 FR-037/a/b).
// Indonesian separators — dot thousands, comma decimal. The documents differ from
// the site in the PREFIX only. Do not "correct" the separators.
func formatIDR(amount decimal.Decimal) string
```

| Input | Output |
|---|---|
| `0` | `IDR 0` |
| `750` | `IDR 750` |
| `550000` | `IDR 550.000` |
| `550000.00` | `IDR 550.000` — a zero fraction is omitted (FR-037a) |
| `15000.92` | `IDR 15.000,92` |
| `15000.90` | `IDR 15.000,9` **or** `IDR 15.000,90` — pick one and pin it; see below |
| `-550000` | `IDR -550.000` |
| `-15000.92` | `IDR -15.000,92` |

**This supersedes the comma-thousands table above it from earlier in this revision.**

Two traps:

1. The grouping loop's `digits[i-1] != '-'` guard is what stops `IDR -,550.000`. It is
   **currently untested**, so a rewrite that drops it ships green.
2. `IntPart()` **truncates**, which is what the current implementation uses. It must be
   replaced by exact rendering, or a receipt's rows stop summing to its own total
   (FR-037b, SC-013a). This is the substantive change; the separators are cosmetic beside it.

**Decide and pin the trailing-zero case**: `15000.90` is stored as `15000.90` in a
`NUMERIC(12,2)` column. Rendering `,9` is arguably "non-zero fraction, minimal digits";
rendering `,90` matches how money is read. Recommend `,90` — two digits whenever any
fraction exists — because `IDR 15.000,9` reads as malformed on a financial document.

## e2e support contract

```ts
export interface MailAttachment {
  PartID: string;
  FileName: string;
  ContentType: string;
  ContentID: string;   // NEW — empty for attachments, set for inline parts
  Size: number;
}

export interface MailMessage {
  // …
  Attachments: MailAttachment[];  // documents only; still length 2
  Inline: MailAttachment[];       // NEW
}
```

This lets one scenario assert both halves: `Attachments.length === 2` (FR-001 unbroken)
**and** every `cid:` in `HTML` resolves to an `Inline` entry (FR-023a actually delivered
rather than merely referenced).

**`e2e/playwright.config.ts` MUST set `TZ: "UTC"` on the api `webServer`.** Without it the
API inherits the developer's zone, a WIB assertion passes on a Jakarta machine with the bug
unfixed, and Principle VIII's "confirmed red first" is unachievable.

---

# Revision 3 — Brand Refresh Contracts (2026-08-19)

Research: [research.md](../research.md) R-018 … R-024.

## HTTP contract: still unchanged

No endpoint, request, response or status code changes. This revision touches rendering,
static assets and two configuration defaults.

## `notification.Message` — unchanged, and deliberately so

```go
type Message struct {
    To          string
    Subject     string
    HTMLBody    string
    Attachments []Attachment  // documents only — the two PDFs
    Inline      []Attachment  // cid: parts — the brand mark and the location pin
}
```

The four-attachment report did not reach this type. R-018 verified against a running
Mailpit that the split already works exactly as Revision 2 specified. **The `Attachments` /
`Inline` separation is load-bearing and must not be collapsed**; the assertions that pin it
are `smtp_test.go:119`–`:131`.

## `notification.Branding` — unchanged shape, changed values

No field is added or removed. What changes is what two of them default to (FR-035a):

| Field | Was | Is |
|---|---|---|
| `SiteURL` | `https://www.jive.co.id` | `https://www.jive-promotion.com/` |
| `SupportEmail` | `help@manjo.com` | `help@manjo.co.id` |

Both remain environment-overridable via `BRAND_SITE_URL` and `BRAND_SUPPORT_EMAIL`.
`SupportEmail` is a **single platform-wide value shared by every surface that prints it**
(FR-035), so this changes the receipt footer as well as the e-ticket footer. That coupling
is the intent of FR-035, not a side effect.

`LogoPath` keeps its meaning and its fallback-to-embedded behaviour. Only the embedded
asset behind it changes.

## Static asset contract

| Consumer | Path | Notes |
|---|---|---|
| Backend (embedded) | `internal/notification/assets/brand/jive-logo-white.png` | `//go:embed`; trimmed, 800 px wide, 64-colour, ~15 KB |
| Frontend (public) | `/brand/jive-logo-white.png` | Same derivative; served statically |
| Frontend (public) | `/brand/jive-logo-black.png` | Carried for light surfaces; **no consumer today** (FR-023c) |

`/brand/jive-logo.png` is **removed**. It is referenced today only by `site-header.tsx:21`;
SC-019 fails if any reference or either copy survives.

The content ID the email body references changes with the filename, and the constant and the
part name must stay in agreement — `service.go`'s `cidLogo` is the single place they meet:

```go
const cidLogo = "jive-logo-white.png"   // was "jive-logo.png"
```

A mismatch here renders a broken image and **raises no error anywhere** — not in the send,
not in the SMTP exchange, not in any assertion that counts attachments. The e2e pairing of
`cidReferences(mail.HTML)` against `mail.Inline` is what catches it.

## Render function contracts

Public signatures are unchanged:

```go
func RenderTicketsPDF(order OrderDelivery, tickets []TicketDetail, brand Branding) ([]byte, error)
func RenderReceiptPDF(order OrderDelivery, brand Branding) ([]byte, error)
func buildEmailBody(order OrderDelivery, tickets []TicketDetail, brand Branding) (html string, inline []Attachment)
```

One package-internal helper widens, so the icon has one definition rather than two (R-023):

```go
// was: func drawEnvelope(pdf *gofpdf.Fpdf, x, y, size float64)
func drawEnvelope(pdf *gofpdf.Fpdf, x, y, size float64, body, flap [3]int)
```

Call sites: the receipt passes `pdfSlate` / white; the e-ticket footer passes the band's
foreground / the band colour. The sticky cap-and-join reset stays inside the function.

## Layout contract — the numbers this revision fixes

These are contract-like because tests and three surfaces depend on them agreeing.

| Surface | Quantity | Was | Is |
|---|---|---|---|
| Email header | `<img>` width × height (attribute **and** CSS) | 108 × 61 | 280 × 159 |
| E-ticket page | `bandHeight` | 21 mm | 38.3 mm |
| E-ticket page | mark width | ~24.6 mm | 55 mm |
| E-ticket page | constants below the band | literals | offsets from `bandHeight`, +17.3 mm |
| Site header | mark width | 98 px | ~200 px *(pending the nod in plan.md)* |
| Site header | bar height | 97 px (`h-24.25`) | ~130 px *(pending)* |

The email's width and height must be set as HTML **attributes** as well as CSS: Outlook's
Word engine ignores CSS dimensions on an `<img>` and draws the natural pixel size, which is
why the shipped part is 2× the display size.

## e2e support contract — unchanged, and the footer does NOT belong here

`MailMessage`, `MailAttachment`, `cidReferences` and `downloadAttachment` are unchanged, and
no helper is added.

**The e-ticket footer is not assertable from Playwright.** `renderTicketsPDF(…, compress bool)`
is called with `true` in production, and gofpdf compresses content streams, so the text a page
draws does not appear in the shipped bytes. `export_test.go` exists precisely for this:

```go
// Test seam. Same drawing calls, compression off, so a test can read the page.
func RenderTicketsPDFPlain(order OrderDelivery, tickets []TicketDetail, brand Branding) ([]byte, error)
```

So the split is:

| Tier | Can assert about the e-ticket | Cannot |
|---|---|---|
| Go (`RenderTicketsPDFPlain`) | Footer strings, alignment geometry, band offsets | — |
| e2e (Playwright) | Attachment count, filename, PDF magic, **page count** | Anything the page *says* |

The email body is the opposite case — it is readable HTML on the wire, so the mark's
dimensions and the `cid:` pairing stay e2e-observable:

```ts
expect(mail.HTML).toContain('width="280" height="159"');     // FR-023b
const referenced = cidReferences(mail.HTML);                  // FR-023a
expect((mail.Inline ?? []).map(p => p.ContentID || p.FileName))
  .toEqual(expect.arrayContaining(referenced));
```

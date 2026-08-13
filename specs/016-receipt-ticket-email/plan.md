# Implementation Plan: Receipt + E-Ticket Email Attachments

**Branch**: `feat/receipt` (spec dir `016-receipt-ticket-email`) | **Date**: 2026-08-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/016-receipt-ticket-email/spec.md`

## Summary

The ticket email gains a second attachment. Today `notification.Service.SendTicketEmail`
renders one PDF (`RenderTicketsPDF`) and posts one `Attachment`; after this change it
renders two — a **Payment Receipt** and an **E-Ticket** document — and posts both on the
same single message to the same single recipient. The email body is rewritten to the new
design and loses its per-ticket cards, because those now live in the attachment.

The whole change lands inside `backend/internal/notification/`, plus the two adapters in
`backend/cmd/api/adapters.go` that feed it. There is **no migration, no new domain, no
wire-contract change, and no frontend change**.

The one non-trivial design problem is Principle II. The three surfaces need data the
notification domain cannot currently see — the event's address, the buyer's phone, the
payment method and date, and each order line's admission date and descriptor. None of it
may be fetched by importing `internal/order` or `internal/event`. All of it is already
assembled by `order.PublicService.TicketOrderByNumber`, which
`notificationOrderAdapter.OrderForDelivery` **already calls**. So the fix is to widen
`notification.OrderDelivery` — the consumer-declared struct — and map the extra fields
across in the adapter. Nothing new is queried and no boundary moves.

## Technical Context

**Language/Version**: Go 1.26.5 (backend); TypeScript 5 / Node (Playwright e2e)

**Primary Dependencies**: `github.com/jung-kurt/gofpdf` v1.16.2 (both documents),
`github.com/skip2/go-qrcode` (unchanged), `github.com/go-mail/mail/v2` (already models
`Attachments` as a slice — no change needed), `github.com/shopspring/decimal`,
`github.com/labstack/echo/v4`

**Storage**: PostgreSQL — **read-only for this feature**. No migration, so `SCHEMA.md` is
untouched. Redis is not involved: `notification` performs no cache reads or writes.

**Testing**: `cd backend && ./scripts/test.sh ./...` (Go unit + database-backed);
`cd e2e && npm test` (Playwright, Principle VIII acceptance gate). Frontend Vitest is
unaffected — no frontend file changes.

**Target Platform**: Linux server (Docker Compose); email rendered in third-party mail
clients

**Project Type**: Web application — Go modular-monolith API + Next.js frontend. This
feature touches the backend and `e2e/` only.

**Performance Goals**: Both documents are rendered inside the existing post-payment
goroutine, off the webhook's response path (Principle IV), so the webhook's instant
`200 OK` is unaffected. Budget: rendering both documents for a 10-ticket order stays under
~200 ms, dominated by QR encoding, which is unchanged in volume — the receipt adds one
page with no QR.

**Constraints**:
- No cross-domain import (Principle II) — enforced by `cmd/api/architecture_test.go`.
- No `sqlc` struct may reach the notification domain (Principle III).
- Both attachments MUST be built **before** `mailer.Send`, so a render failure sends
  nothing and leaves `email_sent` FALSE (FR-006).
- Email HTML must be inline-styled tables; mail clients drop stylesheets (FR-031).
- `gofpdf` core fonts are Latin-1; any non-Latin-1 rune in an event name, venue, address
  or holder name must be handled rather than silently mangled. See research R-006.

**Scale/Scope**: MVP. ~6 files changed in `backend/internal/notification/`, 1 adapter
file, 1–2 e2e spec/support files. No new package, no new endpoint.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-checked after Phase 1 design — see
[Post-Design Re-check](#post-design-constitution-re-check).*

| Principle | Gate | Verdict |
|-----------|------|---------|
| I — Modular Monolith | Work confined to `internal/notification/` + composition root | **PASS**. No new domain; no logic added to `pkg/`. |
| II — Domain Isolation | New data must not come from a cross-domain import | **PASS with design constraint**. `OrderDelivery` / `TicketDetail` widen; `cmd/api/adapters.go` populates them. `notification` imports nothing new. Verified by `architecture_test.go`. |
| III — DTO Isolation | No `sqlc` structs cross the boundary | **PASS**. Adapters map `order.TicketOrderDetail` → `notification.OrderDelivery`, exactly as today. |
| IV — Transactional Integrity | No new network/DB work inside a transaction; `email_sent` semantics preserved | **PASS**. Delivery already runs post-commit in the fulfilment goroutine. Both PDFs are built before `Send`; failure of either aborts before `MarkEmailSent`. |
| V — Payment Gateway Abstraction | Untouched | **PASS**. The receipt reads the *recorded* payment method; it does not call the gateway. |
| VI — Guest-First MVP Scope | No out-of-scope feature introduced | **PASS**. Branding is static configuration (FR-035), explicitly not per-event storage (FR-036). |
| VII — Cache | No cacheable surface added or changed | **PASS**. `notification` issues no Redis command. `OrderForDelivery` reads an order *detail*, which Principle VII does not admit to the cache and this change does not add. |
| VIII — E2E Acceptance | See below — mandatory row | **ACTION REQUIRED** (satisfied by plan). |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes — the guest purchase
      journey's delivery step. Specs that must change:
      **`e2e/specs/guest-purchase.spec.ts`** (the settled-order assertions), plus new
      support in **`e2e/support/`** for reading the delivered message.
- [x] **New user-visible flow?** No new flow, but a **new observable surface**: the suite
      asserts delivery today only through `orders.email_sent` in
      [e2e/support/db.ts](../../e2e/support/db.ts) and has never inspected the message
      itself. FR-001's "exactly two attachments" is unverifiable that way. This plan adds
      a Mailpit-backed assertion — `docker-compose.yml` already runs Mailpit with its API
      on `:8025`, and `playwright.config.ts` already points the API at `:1025`. See
      research **R-007**; the current comment in `playwright.config.ts:123-124` explicitly
      records that a refused SMTP connection is tolerated, and that tolerance must end for
      the specs asserting attachments.
- [x] **Bugfix in a covered flow?** No — this is a behaviour change, not a fix, so the
      fails-before-the-fix rule does not apply. The new assertions must still be confirmed
      to fail against today's single-attachment code before the change lands, which is
      cheap here and is written into [quickstart.md](./quickstart.md).
- [x] **Behaviour under `E2E_CACHE_ENABLED=false`?** No difference. Nothing on this path
      reads or writes the cache, so both modes exercise identical code. The suite must
      still be run both ways.

**Governance obligation (Constitution §Governance).** The Critical Data Flow Rules bind
delivery to *"exactly one email … carrying **every** ticket in the order as a single PDF
together with the receipt."* This feature keeps every clause — one email, one recipient,
one PDF holding every ticket — and only relocates the receipt from body-only to body plus
a detachable document. That is **materially expanded guidance, not a reversal**, so the
amendment is **MINOR**. Three documents must change in the same commit:

| Document | Change |
|----------|--------|
| `.specify/memory/constitution.md` | Critical Data Flow Rules, ticket-generation bullet: the email carries **two** attachments — the merged e-ticket PDF and the receipt PDF — and the receipt also remains in the body. Sync Impact Report prepended. |
| `PRD.md` §1.4 (line 43) | "containing every ticket in the order as a PDF plus the receipt" → two attachments. |
| `ARCHITECTURE.md` (lines 321–322, sequence diagram) | The `NS->>NS` note names both documents. |
| `SCHEMA.md` | **No change** — verified, not assumed: this feature adds no column, table, or migration. |

## Project Structure

### Documentation (this feature)

```text
specs/016-receipt-ticket-email/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── notification.md  # Phase 1 output — internal contracts, not HTTP
├── checklists/
│   └── requirements.md  # From /speckit-specify
└── tasks.md             # /speckit-tasks — NOT created here
```

### Source Code (repository root)

```text
backend/
├── internal/notification/          # ALL domain work lands here
│   ├── service.go                  # MODIFY: OrderDelivery/ReceiptLine widen;
│   │                               #   SendTicketEmail posts two Attachments;
│   │                               #   buildEmailBody rewritten to Figma 741-5105
│   ├── pdf.go                      # MODIFY: TicketDetail widens; e-ticket page
│   │                               #   relaid out (Figma 683-148); price removed
│   ├── receipt_pdf.go              # NEW: RenderReceiptPDF (Figma 688-671)
│   ├── mask.go                     # NEW: the FR-033 masking rule
│   ├── branding.go                 # NEW: platform-wide brand constants (FR-035)
│   ├── pdf_test.go                 # MODIFY: page count, no-price assertion
│   ├── receipt_pdf_test.go         # NEW
│   ├── mask_test.go                # NEW
│   └── service_test.go             # MODIFY: two attachments, body sections
└── cmd/api/
    └── adapters.go                 # MODIFY: notificationOrderAdapter maps the new
                                    #   fields it already fetches; ticket adapter
                                    #   passes through EventAddress

e2e/
├── support/
│   ├── mail.ts                     # NEW: Mailpit API client (list, fetch, attachments)
│   └── env.ts                      # MODIFY: Mailpit base URL config
└── specs/
    └── guest-purchase.spec.ts      # MODIFY: assert two attachments + page count
```

**Structure Decision**: The existing modular-monolith layout is kept unchanged. The
notification domain already owns rendering and delivery and already declares its own
consumer-side interfaces (`OrderProvider`, `TicketProvider`) in
[service.go](../../backend/internal/notification/service.go); this feature widens those
structs rather than introducing any new seam. `receipt_pdf.go` is a sibling of `pdf.go`
rather than a subpackage, because the two share the brand palette and the money and date
formatters, and splitting them would either duplicate those or force an internal package
for four helpers.

## Key design decisions

Full reasoning in [research.md](./research.md); the shape of every struct is in
[data-model.md](./data-model.md).

1. **Widen the consumer-declared structs; add no interface method.** `OrderForDelivery`
   already calls `TicketOrderByNumber`, whose `TicketOrderDetail` already carries
   `Event.Venue`, `Event.Address`, and per-item `AdmissionStarts`. The adapter currently
   discards them. Widening `OrderDelivery` costs one extra repository read (the payment
   row, for method and paid-at) and keeps the interface count flat. (R-001)
2. **Two independent render functions, one shared page toolkit.** `RenderTicketsPDF` stays
   as-is in name and signature shape; `RenderReceiptPDF` is new. Neither calls the other.
   A failure in either aborts the send before `MarkEmailSent` (FR-006). (R-002)
3. **Masking is a pure function in its own file, tested in isolation.** FR-033's rule has
   edge cases (short local part, short phone, missing `@`) that deserve table tests rather
   than being buried in a template. (R-003)
4. **Branding is `pkg/config`-backed with compile-time defaults.** FR-035 says
   platform-wide; making it configuration rather than hardcoded strings costs nothing and
   avoids a code change when the support address moves. The logo is the one asset needing
   a decision — see R-005. (R-004, R-005)
5. **Latin-1 transliteration guard for `gofpdf`.** Core-font PDFs cannot encode arbitrary
   UTF-8; an event name with a curly quote currently renders as mojibake. This feature
   adds pages carrying free-text venue and address, widening the exposure, so the guard
   lands here. (R-006)
6. **e2e asserts through Mailpit's HTTP API, not through SMTP internals.** (R-007)

## Complexity Tracking

> No constitutional violation requires justification. This table records the two places
> the plan deliberately spends complexity, for the reviewer's benefit.

| Decision | Why Needed | Simpler Alternative Rejected Because |
|----------|------------|--------------------------------------|
| One extra repository read for the payment method and paid-at | FR-010 requires the receipt's Transaction Details block; `orders` records the provider but not the instrument or the settlement time — those live on `payments` | Printing "QRIS" as a constant was rejected: it is a display lie the moment a second instrument exists, and Principle V exists precisely so that day is cheap |
| Mailpit-backed e2e assertions | FR-001's "exactly two attachments" is not observable through `orders.email_sent`, the only signal the suite reads today | Asserting attachment count in a Go unit test alone was rejected under Principle VIII: this is a defect reachable only through the assembled system (SMTP compose → MIME → client), so it must be pinned in `e2e/`, not approximated |

## Post-Design Constitution Re-check

Re-evaluated after Phase 1 produced [data-model.md](./data-model.md) and
[contracts/notification.md](./contracts/notification.md):

- **Principle II holds under the final shape.** The widened `OrderDelivery` names only Go
  primitives, `decimal.Decimal`, `time.Time`, and notification's own nested structs. No
  `order`, `event`, `ticket`, or `sqlc` type appears in any signature. `architecture_test.go`
  will pass unchanged.
- **Principle III holds.** `ReceiptLine` gains `AdmissionStarts` and `Descriptor` as plain
  fields, mapped in the adapter from `order.PublicOrderItem`. The `order` DTO does not
  cross.
- **Principle IV holds.** The added payment read happens inside `OrderForDelivery`, which
  runs in the post-payment goroutine, outside every transaction. No new call sits between
  a `BEGIN` and a `COMMIT`.
- **Principle VIII is satisfied by plan, not by promise.** [quickstart.md](./quickstart.md)
  names the exact scenarios, and requires confirming they fail against the current
  single-attachment code before the change lands.
- **No new gate is triggered.** The design adds no table, endpoint, cached surface, or
  gateway call.

**Result: PASS.** The only outstanding obligation is the MINOR constitution amendment
plus the `PRD.md` / `ARCHITECTURE.md` sync described above, which `/speckit-tasks` must
emit as tasks in this same change.

---

# Revision 2 — Design-Review Re-plan (2026-08-12)

The first implementation shipped and was compared against Figma. The review raised 15
corrections; three carried real tradeoffs and were resolved by clarification (nested
tables not flex/grid; CID inline logo; currency — later re-answered as `IDR 550.000` /
`IDR 15.000,92`). This revision plans the corrections.

Everything in Revision 1 still holds unless contradicted below. Research is in
[research.md](./research.md) **R-010 … R-017**, all verified against library source, a
running Mailpit, or the real assets rather than asserted.

## What changed in the problem

Revision 1 treated the logo as decorative with a text fallback, formatted money the way
the site does, and rendered times in whatever location the database happened to return.
All three are now wrong:

- the logo is a **hard dependency** (FR-023a),
- the documents' currency **deliberately diverges** from the site (FR-037),
- times must render **Asia/Jakarta / WIB** everywhere (FR-020a, FR-024b).

The third is not cosmetic. It is a **confirmed correctness defect** — see below.

## The one thing that is a bug, not a polish item

`receiptSubLine` formats each admission date in the value's own location with no
conversion. Reproduced in this session with a temporary test, for a ticket admitting at
`2026-04-27 06:00 WIB` (`2026-04-26T23:00Z`):

```
TZ=UTC          -> receipt sub-line shows: 26 Apr 2026 (WRONG - off by one)
TZ=Asia/Jakarta -> receipt sub-line shows: 26 Apr 2026 (WRONG - off by one)
```

A guest holding a Day 2 pass is told on their receipt that it admits on Day 1. It is wrong
in **both** host timezones, so no amount of local eyeballing would have caught it.

`pgx` returns `timestamptz` in `time.Local` (`pgtype/timestamptz.go:270`, `ScanLocation`
nil), nothing in the repo sets a session timezone, and the runtime image is distroless
with no `TZ` — so production is UTC while a Jakarta developer's machine is WIB. Per
Constitution Principle VIII this is a bugfix in a covered flow and **needs a scenario
confirmed red before the fix**, with `TZ` pinned so the test proves conversion rather than
proving the host's location.

## Constitution Check — Revision 2

| Principle | Verdict |
|---|---|
| I — Modular Monolith | **PASS**. Still confined to `internal/notification` + composition root + `pkg/config`. |
| II — Domain Isolation | **PASS**. No new cross-domain import. The timezone fix is entirely inside `internal/notification`. |
| III — DTO Isolation | **PASS**. `Message` gains an `Inline` field — a notification-owned type. |
| IV — Transactional Integrity | **PASS**. No new work inside any transaction; both documents still built before `Send`. |
| V — Gateway Abstraction | **PASS**. Untouched. |
| VI — Guest-First MVP Scope | **PASS**. Two static assets added; no feature from the out-of-scope list. |
| VII — Cache | **PASS**. No Redis interaction anywhere on this path. |
| VIII — E2E Acceptance | **ACTION REQUIRED**, satisfied below. |

**Principle VIII, restated for this revision:**

- [x] **Covered flow touched?** Yes — the guest journey's delivery step. `e2e/specs/guest-purchase.spec.ts` and `e2e/support/mail.ts` change.
- [x] **Bugfix in a covered flow?** **Yes** — the admission-date day-boundary defect. A scenario MUST be written and confirmed red first. It only goes red with `TZ` pinned to UTC on the API server, so `e2e/playwright.config.ts` must set `TZ: "UTC"` in the api `webServer` env **before** the scenario is written, or "seen red" is unachievable.
- [x] **New observable surface?** Yes — inline message parts. `mail.ts` gains `Inline` typing so a spec can assert the `cid:` references actually resolve.
- [x] **Cache modes?** No behavioural difference; still run both.

**Attachment-count wording** — resolved, and *less* work than clarification feared.
Verified against the running Mailpit: inline CID parts land in a separate `Inline` array
and `Attachments` stays at 2. The existing `toHaveLength(2)` assertion needs **no change**.
The constitution and `PRD.md` should still gain the word "document" for precision, but this
is a clarification, not a fix-or-fail. **No constitution version bump** — 4.1.0's substance
is unchanged.

## Assets — status

| Asset | Status |
|---|---|
| `backend/assets/brand/jive-logo.png` | **Staged in this change.** 216×122 (2× of the design's 108×61), 18 KB. |
| Location pin | **BLOCKED — needs a decision.** See below. |
| Receipt envelope | No asset needed; drawn with vector primitives (R-017). |

### The pin is a genuine design/requirement conflict

Figma node `750:2` is an **emoji text layer**, not a vector. It exports as a 177-byte blank
PNG; its SVG export is a plain square; and rendering the parent row shows no pin at all.
The design's "icon" is the recipient platform's own emoji glyph — which is exactly what
FR-025 forbids, because each platform draws its own.

Options are in [research.md](./research.md) R-013. **Recommendation: author a pin PNG and
carry it as a second CID part**, the only option consistent with FR-025 as written. It
needs sign-off because the glyph would be ours, not the designer's. Until then, the venue
block ships without an icon and FR-025 is not met.

## Work breakdown

### Cross-cutting

| # | Change | Where |
|---|---|---|
| 1 | One package-level `Asia/Jakarta` location, resolved once with a `time.FixedZone("WIB", 7*3600)` fallback so `LoadLocation` can never fail on the delivery path. Hardcoded, not configured — a wrong value would silently misstate a financial document. | `internal/notification` |
| 2 | Convert at **all four render sites** plus `receiptSubLine` — the callers are draw calls and would each have to remember. | `pdf.go`, `receipt_pdf.go`, `service.go` |
| 3 | Split the shared order/transaction stamp: the email wants `04 Jul 2026 06:56 WIB`, the receipt `20 Apr 2026, 14:30 WIB`. One comma apart, so express it in two named formatters rather than by accident. | `receipt_pdf.go` |
| 4 | `formatIDR` → `IDR 550.000` / `IDR 15.000,92`: dot thousands, comma decimal, fraction only when non-zero, and **stop truncating** — `IntPart()` currently discards cents, so a receipt's rows would not sum to its own total (FR-037b). **Keep the `digits[i-1] != '-'` guard**, which no test covers. | `service.go`, `format_test.go` |
| 5 | Band colour `#141B2D` → **`#151A26`** in both renderers, to match the logo's opaque background. | `pdf.go`, `service.go` |

### Email body

| # | Change |
|---|---|
| 6 | Emit a **full HTML document** — doctype, `xmlns:o`, `<head>` with the mso `PixelsPerInch` normaliser. Without it the CID logo renders 25–50% oversized on high-DPI Windows. |
| 7 | Fixed **600px** container: `width` attribute *and* style, Outlook ghost-table wrapper, 40px side padding → 520px content. Replaces `max-width:640px;margin:0 auto`, which the Word engine does not honour. |
| 8 | Logo as CID inline `<img>` with **`width`/`height` HTML attributes** (Outlook ignores CSS dimensions on images). |
| 9 | Badge: nested one-cell table, shrink-to-fit, design colours, slightly rounded. **Honest degradation: square corners in Outlook**, which ignores `border-radius`. |
| 10 | Payment method upper-cased. |
| 11 | Fee rows moved into the **same table** as the line rows so the amount cells share one right edge — that is why the amounts currently drift. |
| 12 | Remove the rule beneath Total Payment. |
| 13 | Footer wording → `"This email was generated automatically. Please do not reply to this email."` + `"© 2026 manjo"`. `Powered By Manjo` must not appear in the body. |
| 14 | Venue block gains the pin cell — **gated on the pin decision**. |

### Receipt PDF

| # | Change |
|---|---|
| 15 | Row rules span the table's **full width**, flush to both outer edges (currently inset from `colProductX`, leaving a notch). |
| 16 | Envelope drawn with `RoundedRect` + a stroked three-point path. **Reset `SetLineCapStyle`/`SetLineJoinStyle` afterwards — they are sticky Fpdf state** and every later rule would inherit round ends. |
| 17 | Payment method upper-cased; `Last updated` gains WIB. |

### E-Ticket PDF

| # | Change |
|---|---|
| 18 | Real logo in the header band (embedded directly — a PDF has no MIME parts). |
| 19 | `VALID FOR` → `26 Apr 2026 @ 10:00 - 21:00 WIB`. Drops the weekday, adds `@`. **The multi-day and single-instant branches are not covered by the requirement — pick and document a form rather than leaving them on the old layout.** |
| 20 | Accent rule: design colour, rounded ends. |
| 21 | **Remove the venue row.** (Revision 1 added it deliberately; the review overrides. Recorded in FR-019a as a trade: a holder at a gate no longer has the address on the pass.) |
| 22 | Footer justified — site left, support right-aligned to the right margin. |

## Tests that will break, and must be updated deliberately

| Test | Why |
|---|---|
| `format_test.go` (5 assertions) | Every one asserts `Rp …` |
| `service_test.go`, `receipt_pdf_test.go` (~15 assertions) | Assert `Rp 550.000` / `Rp 70.000` etc. |
| `pdf_test.go` `TestFormatTicketWindow*` (3) | Assert `Wed, 02 Sep 2026 09:00 UTC` — both the weekday and the zone change |
| `eticket_test.go` venue assertion | The venue row is being removed |
| `service_test.go` footer assertion | `Powered By Manjo` moves out of the body |

**Risk of a red suite is high and concentrated in string assertions.** They must be
rewritten to the new expectations with **UTC inputs and WIB expectations** (e.g. `09:00Z`
→ `16:00 WIB`), which proves the conversion happened rather than that a layout string was
copied.

## Complexity Tracking

| Decision | Why | Rejected alternative |
|---|---|---|
| Two CID inline parts rather than one | The pin cannot be inline SVG (Outlook) or emoji (FR-025) | A single sprite image — cannot be positioned separately in table cells |
| Vector-drawn envelope, raster pin | A PDF draws vectors natively; email clients cannot be trusted with them | Using one mechanism for both — would either ship a raster into an all-vector document or a vector into Outlook, where it does not render |
| Hardcoded `Asia/Jakarta` | An operator changing the display zone would silently misstate a financial document | An env var — `pkg/config`'s precedent is operator-tunable values, which this is not |

## Post-Design Constitution Re-check

**PASS.** No new gate is triggered: no table, endpoint, cached surface, gateway call, or
cross-domain import. Two static assets enter `backend/assets/`, which is new but is neither
stored data (FR-036 concerns per-event branding in the database) nor a domain.

The one open item is the **pin asset decision**, which blocks FR-025 only. Every other
correction can proceed without it.

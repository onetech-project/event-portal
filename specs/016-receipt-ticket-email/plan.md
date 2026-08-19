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

---

# Revision 3 — Brand Refresh Re-plan (2026-08-19)

**Branch**: `fix/receipt` (spec dir `016-receipt-ticket-email`) | **Spec**: [spec.md](./spec.md)
§Clarifications, Session 2026-08-19

Triggered by a report that the ticket email arrives with **four** attachments, and by a new
brand asset supplied in two variants plus a footer change on the e-ticket (Figma `683-148`).

Everything in Revisions 1 and 2 still holds unless contradicted below. Research is in
[research.md](./research.md) **R-018 … R-024**.

> **`setup-plan.sh` cannot resolve this feature.** It derives `FEATURE_DIR` from the branch
> name, and `fix/receipt` is not a numbered feature branch, so it falls back to
> `018-configurable-rate-limits`. It copied nothing (both plans already exist) and no file
> was touched, but `/speckit-tasks` and `/speckit-implement` will target the wrong feature
> unless they are pointed at `016-receipt-ticket-email` explicitly.

## What changed in the problem

**The reported defect is not a defect.** R-018 settles it against the running service:
Mailpit's API returns the two PDFs in `Attachments` and the two images in `Inline`, and its
own badges say so; the four-file strip is `allAttachments()`, which concatenates every part
because Mailpit is a MIME debugger. Gmail and Yopmail list two. **No code changes for this,
and Principle VIII owes no red-first scenario** — there is no bug to see red.

What is left is a brand refresh with real work in it:

- the mark is replaced everywhere by a **sponsor lockup** whose secondary line is unreadable
  at today's size, so three header bands grow (FR-023b, R-021);
- the new asset has **alpha**, retiring the opaque-background constraint two comments and one
  fitting strategy are built on (R-020);
- the e-ticket footer gains an **envelope icon** and changes alignment (FR-022a, FR-022d);
- `BRAND_SITE_URL` and `BRAND_SUPPORT_EMAIL` adopt the design's values (FR-035a);
- **the frontend is in scope for the first time** — Revision 1's "no frontend change" no
  longer holds (R-024).

> **Superseded 2026-08-19, after implementation.** The enlargement below was built, seen
> and reversed the same day: the mark is now capped by each container's existing height and
> no band grows. See spec.md §Clarifications and research.md R-021a. The section is kept
> because the reversal only makes sense against what it replaced.

## The one thing that needs a nod before implementation

**The site header bar grows, and how far is a judgment call I made rather than one the
clarification settled.**

The user chose "enlarge the mark, grow the bands". Applied literally — the same 2.6× factor
the email needs — the site header mark becomes 254×145 px inside a bar that must grow from
**97 px to ~190 px**, which would dominate the public chrome on every page. R-021 proposes
**200 px wide in a ~130 px bar** instead, on the grounds that the web has a different
legibility budget (DPR ≥ 2, and the reader can zoom) and reaches the same perceived result.

This is a deviation from the strictest reading of the answer, so it is flagged rather than
assumed. **If the intent was a uniformly scaled mark, the header numbers change and the
e-ticket ones do not.** Nothing else in this plan depends on which way it goes.

## Constitution Check — Revision 3

| Principle | Verdict |
|---|---|
| I — Modular Monolith | **PASS**. Backend work stays in `internal/notification` + `pkg/config`. The frontend change is one component and two static files in a separate app; no monolith boundary is involved. |
| II — Domain Isolation | **PASS**. No new cross-domain import. Nothing here reads data at all. |
| III — DTO Isolation | **PASS**. No `sqlc` struct goes near this. `Branding` is notification-owned and gains no field. |
| IV — Transactional Integrity | **PASS**. No transaction is touched; both documents are still built before `Send`. |
| V — Gateway Abstraction | **PASS**. Untouched. |
| VI — Guest-First MVP Scope | **PASS**. An asset swap, a layout change and two config defaults. No feature from the out-of-scope list. |
| VII — Cache | **PASS**. No Redis interaction on this path. |
| VIII — E2E Acceptance | **ACTION REQUIRED**, satisfied below. |

**Principle VIII, restated for this revision:**

- [x] **Covered flow touched?** Yes — the guest journey's delivery step and the e-ticket
      document it produces. `e2e/specs/guest-purchase.spec.ts` changes.
- [x] **Bugfix in a covered flow?** **No.** R-018 shows the reported defect was a
      misreading of Mailpit's debug view. No red-first scenario is owed, and claiming one
      would mean writing a test that cannot fail against the unfixed code — precisely the
      "never seen red" anti-pattern the principle exists to prevent.
- [x] **New user-visible surface?** Yes — the e-ticket footer's envelope and site address.
      **It cannot be covered in `e2e/`, and this was nearly planned wrong.** `renderTicketsPDF`
      takes a `compress` flag and *production always compresses*, so what a page says is not
      findable in the shipped bytes; every content assertion goes through the test-only
      `RenderTicketsPDFPlain` seam in `export_test.go`. Playwright downloads the production
      document, so it can assert the attachment count, filename, PDF magic and page count —
      never the footer's text. **The footer's coverage therefore lands at the Go tier**, which
      is the same split the day-boundary defect already took (see tasks.md §Known limits).
      The e2e tier gains only what it can genuinely observe: the email body's HTML.
- [x] **Frontend tier now relevant?** Yes, first time for this feature (R-024). Run
      `frontend` Vitest as well as the Go and Playwright tiers.
- [x] **Cache modes?** No behavioural difference; still run both.

## Assets — status

| Asset | Status |
|---|---|
| `…/brand/LOGO JIVE MINUS TWO_WHITE.png` | **Supplied.** 6400×4290, ~276 KB, alpha. Source of truth; not shipped as-is. |
| `…/brand/LOGO JIVE MINUS TWO_BLACK.png` | **Supplied.** Identical geometry. No current placement — every surface is a dark band (FR-023c). |
| Shipped derivatives | **To generate**: trim to artwork, resize to 800 px wide, quantise to 64 colours → ~15 KB each (R-020). |
| Retired `jive-logo.png` (frontend 349 KB + backend 18 KB) | **To delete**, both copies (FR-023c, SC-019). |
| Location pin | Unchanged. Still the authored PNG from R-013 option A. |
| Envelope | Unchanged as an asset — still vector primitives, now colour-parameterised (R-023). |

## Work breakdown

### Assets

| # | Change | Where |
|---|---|---|
| 1 | Generate trimmed, 64-colour, 800 px derivatives of both variants. Trimming is not optional — untrimmed, each surface inherits a different amount of baked-in padding (R-020). | `frontend/public/brand/`, `backend/internal/notification/assets/brand/` |
| 2 | Delete both copies of the retired mark. SC-019 is written to fail if either survives. | same |
| 3 | Name derivatives canonically (`jive-logo-white.png` / `-black.png`). The supplied names contain spaces, which become `%20` in a public URL and read badly in a `//go:embed` directive. | same |

### Backend — shared

| # | Change | Where |
|---|---|---|
| 4 | Point `//go:embed` at the new white derivative. `logoPNG`/`pinPNG` and the operator-override path are unchanged. | `assets.go` |
| 5 | `BRAND_SITE_URL` → `https://www.jive-promotion.com/`, `BRAND_SUPPORT_EMAIL` → `help@manjo.co.id` (FR-035a). Both stay env-overridable. Update `config_test.go`'s default assertions in the same commit. | `pkg/config/config.go` |

### Backend — e-ticket PDF

| # | Change | Where |
|---|---|---|
| 6 | `bandHeight` 21 → 38.3 mm; mark drawn at 55 mm wide (R-021, R-022). | `pdf.go` |
| 7 | Re-express the page's absolute constants as offsets from `bandHeight` instead of literals, then shift by +17.3 mm. They must move together — a band moved without the accent rule puts a 72 mm rule through it (R-022). | `pdf.go` |
| 8 | Rewrite the `pdfBand` and `drawBrandMark` comments. Both assert the logo has an opaque background; the new asset has alpha, so both are now false and actively misleading (R-020). | `pdf.go` |
| 9 | Footer: customer-service block right-**positioned**, contents left-aligned — label, icon and address on one left edge (FR-022a). Must hold for any address length, not just the mock's (FR-022e). | `pdf.go` |
| 10 | Draw the envelope before the address on its own line (FR-022d), using the band's foreground rather than slate. | `pdf.go` |
| 11 | `drawEnvelope` gains explicit body and flap colours; single definition, two call sites (R-023). | `receipt_pdf.go` |

### Backend — email body

| # | Change | Where |
|---|---|---|
| 12 | `emailHeader`: 108×61 → 280×159, in both the HTML **attributes** and the inline CSS. The attributes are what Outlook's Word engine obeys, so they cannot be left behind (existing comment at `service.go:362`). | `service.go` |
| 13 | Ship the CID part at 2× the display size, as today's asset already does for 108×61. | `assets.go`, `service.go` |

### Frontend

| # | Change | Where |
|---|---|---|
| 14 | Swap `src` to the new derivative; grow the mark to ~200 px and the bar from `h-24.25` to ~`h-32.5`, pending the nod above. Keep the plain `<img>` (R-024). | `components/layout/site-header.tsx` |

## Tests that will break, and must be updated deliberately

| Test | Why it breaks | Correct response |
|---|---|---|
| `notification/smtp_test.go:155` | Iterates `{"jive-logo.png", "location-pin.png"}` by filename. | Update the logo's name. The **inline-vs-attachment assertions at `:119`–`:131` must NOT be weakened** — they are what proves R-018's finding stays true. |
| `notification/service_test.go:386` | Asserts `cid:jive-logo.png` in the body. | Update the name. Keep the assertion. |
| `notification/eticket_test.go:150` | Asserts the text wordmark appears when the asset is missing. | Unchanged — it exercises the fallback path, which still exists. |
| `notification/pdf_test.go`, `receipt_pdf_test.go` | Any assertion keyed to y-coordinates below the band. | Re-derive from `bandHeight` rather than re-hardcoding, or the next band change breaks them again. |
| `pkg/config/config_test.go:275` | Asserts the old `BRAND_SITE_URL` / `BRAND_SUPPORT_EMAIL` defaults. | Update to the design's values. |
| `e2e/specs/guest-purchase.spec.ts:159` | `toHaveLength(2)` on `Attachments`. | **No change.** R-018 confirms it already reads the document-only array. Do **not** add footer assertions here — the production PDF is compressed and its text is unreachable from Playwright (R-025). |

## Complexity Tracking

| Item | Why it is not gold-plating |
|---|---|
| Re-expressing page constants as offsets from `bandHeight` (#7) | Not required by any FR. Justified because the alternative is shifting six literals by hand and having the next band change silently reintroduce the same class of defect. |
| Colour-parameterising `drawEnvelope` (#11) rather than copying it | A forked icon drifts the first time either is adjusted. One definition, two call sites, same package. |
| Rewriting comments that are now false (#8) | The comments encode a constraint (`opaque background`) that the new asset removes. Left alone, they would make a future reader preserve a fitting strategy for a reason that no longer exists. |

## Post-Design Constitution Re-check

Re-checked after the work breakdown above. **All eight principles still PASS**, with
Principle VIII's obligations discharged as listed: coverage extends to the e-ticket footer,
no red-first scenario is owed because no bug exists, and all three test tiers run.

No new domain, no new endpoint, no migration, and therefore **no `SCHEMA.md` change and no
constitution amendment**. The delivery rule the constitution states — one email, two
document attachments, inline images excluded from the count — is confirmed by R-018 rather
than altered by it.

---

# Revision 4 — Subject Line + Phone Mask Direction (2026-08-19)

**Branch**: `fix/receipt` (spec dir `016-receipt-ticket-email`) | **Spec**:
[spec.md](./spec.md) §Clarifications, Session 2026-08-19 (subject line + phone masking)

Triggered by two reports against the delivered email: the subject names neither the order
nor reads as the buyer expects, and the buyer's phone carries its asterisks in the middle
(`14239***6621`) where the design puts them at the end (`+628123456****`).

Everything in Revisions 1–3 still holds unless contradicted below. Research is in
[research.md](./research.md) **R-026 and R-027**.

> **`setup-plan.sh` still cannot resolve this feature**, for the reason Revision 3 recorded.
> It now falls back to `020-qris-frame-simplify` (the fallback target moved because a newer
> spec directory exists). It copied nothing — both plans exist — and touched no file, but
> `/speckit-tasks` and `/speckit-implement` MUST be pointed at `016-receipt-ticket-email`
> explicitly.

## What changed in the problem

This is the smallest revision in the feature: two string-shaped behaviours, no new asset, no
layout, no configuration key, no frontend change.

- **The subject was never specified.** R-026: `grep -i subject spec.md` returned nothing
  across three revisions, while `service.go:215` has been sending
  `"Your tickets for " + EventName` since Revision 1 and
  `contracts/notification.md:113` recorded that choice as though it were a contract. FR-005a
  is therefore a *new* requirement, not an amendment, and the contract file moves with it.
- **The phone mask is a spec defect, not a code defect.** R-027: `14239***6621` is
  `phoneKeepLeading=5`/`phoneKeepTrailing=4` applied correctly to a stored number that has no
  `+62` prefix. FR-033's rule was wrong; `MaskPhone` implemented it faithfully. The fix is
  one rule and one function, and it lands on both surfaces at once because both already call
  the same helper (`receipt_pdf.go:120` comment, `service.go:538` comment).
- **The mask discloses more than it used to, deliberately.** Recorded in FR-033, the spec's
  Assumptions, and R-027 so that a later reader does not reverse it as a privacy fix.

## The one thing worth a nod before implementation

**Two existing tests cannot catch a wrong mask shape, and the obvious e2e assertion would
inherit the same blind spot.**

```go
// receipt_pdf_test.go:113
assert.Contains(t, text, notification.MaskPhone("+628123456789"))
// service_test.go:507
maskedPhone := notification.MaskPhone("081234567890")
```

Both call the function under test as their own oracle. They are not wrong — proving the two
surfaces agree with one helper is exactly what SC-008 asks — but they pass for any shape the
helper returns. The literal shapes live in one place only, `mask_test.go`'s table.

So the new `e2e/` assertion MUST be the literal `"08123456****"`, never
`MaskPhone(holder.phone)`. Written the convenient way it would be green before and after the
fix, which is the "never seen red" failure Principle VIII exists to prevent. This is called
out here rather than left to the task list because it is the one way this small change can be
implemented wrongly while looking fully covered.

## Constitution Check — Revision 4

| Principle | Verdict |
|---|---|
| I — Modular Monolith | **PASS**. Both changes are inside `internal/notification`; nothing crosses a module boundary. |
| II — Domain Isolation | **PASS**. No new import. `OrderNumber` and `EventName` are already on the `OrderDelivery`/`TicketDetail` DTOs the domain hands over. |
| III — DTO Isolation | **PASS**. No `sqlc` struct is touched; no DTO gains a field. |
| IV — Transactional Integrity | **PASS**. Nothing near a transaction; both documents are still built before `Send`. |
| V — Gateway Abstraction | **PASS**. Untouched. |
| VI — Guest-First MVP Scope | **PASS**. Two display strings. No feature from the out-of-scope list. |
| VII — Cache | **PASS**. No Redis interaction on this path. |
| VIII — E2E Acceptance | **ACTION REQUIRED**, discharged below. |
| IX — Throttling | **PASS**. Untouched; delivery is not a throttled entry point. |

**Principle VIII, restated for this revision:**

- [x] **Covered flow touched?** Yes — the guest journey's delivery step.
      `e2e/specs/guest-purchase.spec.ts` changes in the same commit.
- [x] **Bugfix in a covered flow?** **Yes, for the phone mask** — unlike Revision 3, this
      report is a real defect. A scenario asserting `08123456****` MUST be confirmed red
      against the unfixed code first; today it renders `08123***7890`, so the failure is
      immediate and unambiguous. The subject change is a behaviour change rather than a
      bugfix (no requirement existed to violate), and needs coverage but owes no red-first
      confirmation — though it will in fact be red too, since nothing asserts `Subject`
      today.
- [x] **New user-visible surface?** The subject line, effectively. **It is fully observable
      at the e2e tier** — `MailMessage.Subject` is already parsed in `e2e/support/mail.ts`
      and read back off real Mailpit. This is the opposite of R-025's e-ticket footer: no
      compression, no PDF, nothing to push down to the Go tier.
- [x] **Both surfaces of the mask observable?** The email body yes, via `mail.HTML`. The
      receipt's copy is a compressed PDF, so its assertion stays at the Go tier
      (`receipt_pdf_test.go`) — the same split R-025 established.
- [x] **Frontend tier?** Not relevant. Revision 3's frontend change was the logo; nothing
      here reaches the frontend.
- [x] **Cache and throttle modes?** No behavioural difference; still run both, as the
      principle requires unconditionally.

## Work breakdown

### Backend — masking

| # | Change | Where |
|---|---|---|
| 1 | Replace `phoneKeepLeading`/`phoneKeepTrailing` with a single `phoneMaskTrailing = 4`. Rewrite `MaskPhone` to keep `len-4` characters and asterisk the rest; a value of 4 runes or fewer is masked entirely. Keep the character-counting behaviour and its comment — rendering the stored value verbatim is now an explicit FR-033 clause, not just a nicety. | `internal/notification/mask.go` |
| 2 | Delete the now-unreachable `maskFrom(phone, phoneKeepLeading)` branch. `maskFrom` itself stays — `MaskEmail` still needs it and its email behaviour is unchanged. | same |
| 3 | Rewrite `TestMaskPhone`'s table to the new shapes, including `0812 → ****` and the unchanged `081234567 → 08123****`. This table is the only place the literal shapes are pinned. | `mask.go`'s test |

### Backend — subject

| # | Change | Where |
|---|---|---|
| 4 | `Subject: fmt.Sprintf("[%s] E-receipt & E-Ticket for %s", order.OrderNumber, tickets[0].EventName)`. No new plumbing — both values are already in scope at that line. | `internal/notification/service.go:215` |
| 5 | Tighten `service_test.go:193` from `Contains(msg.Subject, "Jazz Night 2026")` to the full literal, so the bracketed order number and the fixed wording are both pinned at the Go tier. | `service_test.go` |

### e2e

| # | Change | Where |
|---|---|---|
| 6 | Assert the delivered `mail.Subject` equals `` `[${orderNumber}] E-receipt & E-Ticket for ${eventName}` ``, in the existing delivery block that already holds `orderNumber` (near `:159`). | `e2e/specs/guest-purchase.spec.ts` |
| 7 | Assert `mail.HTML` contains the **literal** `08123456****` — the default holder phone `081234567890` from `e2e/support/journey.ts:27` under the new rule — and does **not** contain `08123***7890`. The negative assertion is what makes the scenario unambiguously red before the fix. | same |

### Governance

| # | Change | Where |
|---|---|---|
| 8 | No `SCHEMA.md` change, no migration, no constitution amendment. `PRD.md` describes the delivery as one email with two documents and says nothing about the subject, so it needs no edit either — confirmed by reading it, not assumed. | — |

## Tests that will break, and must be updated deliberately

| Test | Why it breaks | Correct response |
|---|---|---|
| `notification/mask_test.go:50` `TestMaskPhone` | Every row encodes the old shape. | Rewrite the table (work item 3). This is the change, not a casualty of it. |
| `notification/service_test.go:193` | `Contains(msg.Subject, "Jazz Night 2026")` still passes under the new subject — it will **not** break, which is the problem. | Tighten it to the full literal anyway (work item 5). A test that survives a deliberate behaviour change unchanged was not asserting the behaviour. |
| `notification/receipt_pdf_test.go:113` | Calls `MaskPhone` as its own oracle, so it passes either way. | **Leave as is.** Its job is surface-vs-helper agreement (SC-008), and rewriting it to a literal would duplicate `mask_test.go` while losing that. R-027 explains why this is deliberate. |
| `notification/service_test.go:507` | Same pattern, same reasoning. | Leave as is. |
| `notification/mask_test.go:82` `TestMaskingNeverLengthensAValue` | Nothing breaks — output length still equals input length. | No change. Confirm it still runs; it is the guard on the receipt's column alignment. |
| `e2e/specs/guest-purchase.spec.ts` | Nothing breaks today; the new assertions are additions. | Confirm both are **red before the fix** (quickstart Revision 4, Step 0). |

## Complexity Tracking

| Item | Why it is not gold-plating |
|---|---|
| Tightening `service_test.go:193` (#5) when it would pass untouched | It is the only Go-tier assertion on the subject, and it currently proves only that the event name appears somewhere. Leaving it would mean the new FR-005a has coverage at exactly one tier. |
| The negative `not.toContain("08123***7890")` in e2e (#7) | Redundant once the positive assertion is right. Kept because it is what makes the red-first confirmation legible in the failure output rather than requiring the reader to compare two similar strings. |
| Deleting the short-phone branch (#2) rather than leaving it unreachable | Dead code that encodes the superseded rule is exactly what a future reader would restore the old behaviour from. |

## Post-Design Constitution Re-check

Re-checked after the work breakdown. **All nine principles still PASS**, with Principle
VIII's obligations discharged as listed: the phone mask is a genuine bugfix in a covered flow
and gets a scenario confirmed red first; the subject gains coverage at both the Go and e2e
tiers; both cache and throttle modes still run.

No new domain, no new endpoint, no migration, no configuration key — therefore **no
`SCHEMA.md` change, no `PRD.md` change, and no constitution amendment**.

---

# Revision 5 — E-Ticket Colours (2026-08-19)

**Branch**: `fix/receipt` (spec dir `016-receipt-ticket-email`) | **Spec**:
[spec.md](./spec.md) §Clarifications, Session 2026-08-19 (e-ticket colours)

Triggered by a rendered e-ticket read against Figma `683-148`: the event name is crimson where
the design has slate, and the footer's colours turned out to be inverted rather than merely
off-shade.

Everything in Revisions 1–4 still holds. Research is in [research.md](./research.md)
**R-028, R-029, R-030**.

> **`setup-plan.sh` still cannot resolve this feature** — it reports
> `020-qris-frame-simplify`. It copied nothing and touched no file. Point `/speckit-tasks`
> and `/speckit-implement` at `016-receipt-ticket-email` explicitly.

## What changed in the problem

**The event name (R-028).** `pdf.go:252` sets `pdfBrand` `#cb1c4f` before the uppercased
event name; the design gives `#475569`. `pdfBrand` has exactly one consumer, so the constant
dies with the change and the e-ticket ends up carrying no crimson outside the logo.

**The footer (R-029) — the report understated it.** "The colour is a bit different" is
actually a swap of the two roles: the code draws labels at `#eceef3` and values at `#969cac`;
the design does the reverse. `help@manjo.co.id` — the one value on the document a buyer acts
on — is currently the dimmest text in the band, worst in greyscale print. The label's design
value is white at 80% opacity, which a PDF cannot express, so it is flattened against the
band arithmetically: `0.8x255 + 0.2xband` → `#d0d1d4`.

**Nothing here can move a coordinate.** Every change is a colour constant or a `setColor`
argument. That property is the entire reason this ships before the typeface (Revision 6) and
not with it.

## Resolved after this plan was written: the palette question

> **Answered 2026-08-19: yes, and inside Revision 6** (FR-019c). The section is kept because
> the answer only reads correctly against the finding that prompted it.

**The e-ticket's whole palette is a different colour family from the design's.**

R-030 measured every text node. The design is Tailwind **slate** throughout — `#0f172a`,
`#1e293b`, `#334155`, `#475569`, `#64748b`, `#e2e8f0`. The code is **zinc/neutral** —
`#18181b`, `#6e6e6e`, `#e0e0e4` — except `pdfAccent`, which is already slate because FR-022b
pinned it there once before.

FR-019b and FR-022f fix the event name and the footer, which is what was asked. They leave
the **identity block labels and values, the `Ticket N of M` heading, the valid-for row and the
divider rule** on the zinc family.

| Element | Design | Code |
|---|---|---|
| Identity label, `VALID FOR` | `#64748b` | `pdfGray` `#6e6e6e` |
| Identity value, ticket-type heading, valid-for value | `#0f172a` | `pdfInk` `#18181b` |
| `Ticket N of M` | `#1e293b` | `pdfInk` `#18181b` |
| Divider rule | `#e2e8f0` | `pdfLine` `#e0e0e4` |

The differences are small individually and systematic together — a warm-neutral document
where the design is cool-blue. It was flagged rather than assumed, because "match the design"
was asked about two specific elements and widening it to the whole palette is a scope
decision, not an inference.

**The answer was yes, placed in Revision 6** — those exact elements are retouched for the type
scale there, so the marginal cost is close to zero, where a separate pass afterwards would
mean revisiting every one of them again. **Revision 5 is unchanged by this**: it still ships
only the event name and the footer.

## Constitution Check — Revision 5

| Principle | Verdict |
|---|---|
| I — Modular Monolith | **PASS**. Colour constants inside `internal/notification`. |
| II — Domain Isolation | **PASS**. No import changes; nothing reads data. |
| III — DTO Isolation | **PASS**. No struct touched. |
| IV — Transactional Integrity | **PASS**. No transaction on this path. |
| V — Gateway Abstraction | **PASS**. Untouched. |
| VI — Guest-First MVP Scope | **PASS**. Colour values on an existing document. |
| VII — Cache | **PASS**. No Redis interaction. |
| VIII — E2E Acceptance | **ACTION REQUIRED**, discharged below. |
| IX — Throttling | **PASS**. Untouched. |

**Principle VIII, restated:**

- [x] **Covered flow touched?** The e-ticket document inside the guest journey's delivery
      step — yes.
- [x] **Bugfix in a covered flow?** **Yes for the footer.** The inverted emphasis is a
      defect, and a red-first assertion is owed: the drawn colour of the support address is
      `#969cac` today and must be white. The event-name colour is closer to a spec gap being
      filled (no requirement governed it), but it is red-first for free.
- [x] **Where does it land?** **The Go tier, not `e2e/`.** R-025 already established that the
      shipped PDF is compressed and Playwright cannot read what a page says — and a *colour*
      is even further out of reach than text. Assertions go through
      `RenderTicketsPDFPlain`, and they must assert the **drawn colour operator**, not the
      presence of a string.
- [x] **New user-visible surface?** No. Existing elements, different values.
- [x] **Cache and throttle modes?** No behavioural difference; both still run.

## Work breakdown — Revision 5

| # | Change | Where |
|---|---|---|
| 1 | Add `pdfEventName = [3]int{71, 85, 105}` (`#475569`); use it at `pdf.go:252` in place of `pdfBrand` (FR-019b) | `pdf.go` |
| 2 | Delete `pdfBrand`. It has no other consumer (R-028); leaving it dead would preserve a retired decision in a palette block a future reader treats as authoritative | `pdf.go` |
| 3 | Add `pdfBandLabel = [3]int{208, 209, 212}` (`#d0d1d4`, white@80% flattened against the band — R-029 shows the arithmetic) and use it for both footer labels | `pdf.go` |
| 4 | Footer **values** to pure white at `pdf.go:322` and `:350`; the envelope body to white at `:348`, flap staying `pdfBand` so it still reads as a cut-out (FR-022f) | `pdf.go` |
| 5 | Delete `pdfBandDim` — the footer value and the envelope were its only consumers. **`pdfBandFg` stays**: it has a third consumer at `pdf.go:430` (the wordmark fallback) and a fourth at `receipt_pdf.go:183` | `pdf.go` |
| 6 | Add a colour seam to `export_test.go` if the existing seams cannot reach drawn colour, so the assertions in #7 test the document rather than the constants | `export_test.go` |
| 7 | Go-tier assertions: no crimson anywhere on an e-ticket page outside the logo image; footer values brighter than their labels (SC-024) | `eticket_test.go` |

## Tests that will break, and must be updated deliberately

| Test | Why | Correct response |
|---|---|---|
| Any `pdf_test.go` / `eticket_test.go` assertion naming `pdfBrand` or `pdfBandDim` | The constants are deleted. | Re-point at the new names. A test that compiles against a constant is not asserting a colour — see #7. |
| `eticket_test.go:150` (wordmark fallback) | Uses `pdfBandFg`, which survives. | No change. Confirm it still passes. |
| `receipt_pdf_test.go` | `pdfBandFg` survives and the receipt is otherwise untouched. | No change expected. If anything moves here, a colour leaked across documents — report it. |

## Complexity Tracking — Revision 5

| Item | Why it is not gold-plating |
|---|---|
| A colour seam in `export_test.go` (#6) | Without it the only assertable thing is the constant's value, which is circular — R-027 caught the same shape of blind spot in the mask tests. |
| Deleting two constants rather than leaving them (#2, #5) | Both encode superseded decisions and both sit in a palette block that reads as authoritative. |

## Post-Design Constitution Re-check — Revision 5

All nine principles **PASS**. Principle VIII discharged at the Go tier, which R-025 already
established as the correct home for e-ticket content assertions. No schema, no migration, no
config key, no constitution amendment.

---

# Revision 6 — Inter Typeface + E-Ticket Type Scale (2026-08-19)

**Branch**: `fix/receipt` (spec dir `016-receipt-ticket-email`) | **Spec**:
[spec.md](./spec.md) FR-003a, FR-003b

The documents move from gofpdf's built-in Helvetica to **Inter**, the family the designs are
drawn in, and the e-ticket's type sizes are re-read from Figma `683:148` wholesale.

Research: [research.md](./research.md) **R-031 … R-035**. This revision ships **after**
Revision 5, deliberately: colours cannot move a coordinate and the typeface moves all of them,
so bundling them would make a failing geometry assertion impossible to bisect.

## What changed in the problem

This is the largest revision since Revision 1, and the only one that touches both PDF
documents everywhere at once.

- **The mechanism is settled and cheap** (R-031): `AddUTF8FontFromBytes` takes TTF bytes, so
  Inter ships through the existing `//go:embed` path with no filesystem dependency, and
  gofpdf subsets automatically — a few KB per weight per document, not ~300 KB.
- **Four design weights do not fit gofpdf's four style slots** (R-032). Medium and SemiBold
  have no `styleStr` and must be registered as their own families.
- **`latin1()` must be deleted, not deferred** (R-033). It emits raw high bytes that are
  correct for a cp1252 core font and *malformed UTF-8* under an embedded one. Leaving it is
  the option that ships broken glyphs for the exact names the change fixes.
- **The type scale is drift, not a decision** (R-030). The design-to-code ratios run 0.68 to
  0.91 with no pattern — which is why FR-003b asks for a wholesale re-read.
- **Letter-spacing cannot be reproduced** (R-034). gofpdf has `SetWordSpacing`, not `Tc`.

## Two things to get right before implementation

**1. The palette question is answered and lands here** (FR-019c, resolved 2026-08-19). The
identity block, the `Ticket N of M` heading, the valid-for row and the divider move to the
design's slate values in the same pass that changes their sizes. Doing it here rather than
later is the whole point: T215/T216 already touch every one of those call sites.

**2. Inter's static TTFs must be added to the repo, and the variable font must not be.**
Nothing ships a font file today — the frontend gets Inter from `next/font/google` at build
time. Five static faces are needed (Regular, Medium, SemiBold, Bold, Italic). **Do not ship
`InterVariable.ttf`**: gofpdf parses the classic TrueType tables and ignores `fvar`/`gvar`, so
every weight silently renders as the default instance. Bold and SemiBold would come out
identical, nothing would error, and it would look like a styling bug (R-031).

## Constitution Check — Revision 6

| Principle | Verdict |
|---|---|
| I — Modular Monolith | **PASS**. Font assets and rendering stay in `internal/notification`. |
| II — Domain Isolation | **PASS**. No import changes. |
| III — DTO Isolation | **PASS**. No struct touched. |
| IV — Transactional Integrity | **PASS**. Rendering happens before `Send`, outside any transaction — unchanged. |
| V — Gateway Abstraction | **PASS**. Untouched. |
| VI — Guest-First MVP Scope | **PASS**. Typography on existing documents; no new feature. |
| VII — Cache | **PASS**. No Redis interaction. |
| VIII — E2E Acceptance | **ACTION REQUIRED**, discharged below. |
| IX — Throttling | **PASS**. Untouched. |

**Principle VIII, restated:**

- [x] **Covered flow touched?** Both documents in the delivery step — yes, and more broadly
      than any revision since the first.
- [x] **Bugfix?** **No.** No requirement was violated; FR-003a and FR-003b are new. No
      red-first scenario is owed, and manufacturing one would repeat Revision 3's mistake.
- [x] **New user-visible surface?** The diacritic behaviour is genuinely new and **is**
      assertable: render a holder name carrying a diacritic and confirm the glyph survives
      instead of being transliterated (SC-025). That belongs at the Go tier via
      `RenderTicketsPDFPlain`.
- [x] **What `e2e/` gains.** Nothing new. R-025 stands: the shipped PDF is compressed, so
      Playwright still sees only count, filename, magic and page count. **The suite must still
      be run and stay green** — it is what proves the font work did not break delivery, page
      count or attachment identity.
- [x] **The gap that no tier covers**: "looks like the design". R-035 — the receipt's table
      seams catch drift, `customerServiceBlock` self-adjusts, but the e-ticket's absolute
      offsets are literals with nothing below them to notice a collision. A **visual diff is
      required**, not optional (quickstart Revision 6 Step 3).
- [x] **Cache and throttle modes?** No behavioural difference; both still run.

## Work breakdown — Revision 6

### Assets

| # | Change | Where |
|---|---|---|
| 1 | Add the five **static** Inter faces (Regular, Medium, SemiBold, Bold, Italic) as embedded assets. Not the variable font (R-031) | `backend/internal/notification/assets/fonts/` |
| 2 | Record the Inter version and SIL OFL licence alongside the files, as the brand assets already are | same |

### Font registration

| # | Change | Where |
|---|---|---|
| 3 | Register the faces once per document: `Inter` (`""`, `B`, `I`) plus `InterMedium` and `InterSemiBold` as their own families (R-032) | `pdf.go`, `receipt_pdf.go` |
| 4 | Replace every `SetFont("Helvetica", …)` — ~24 call sites across both renderers. **The one call taking `style` as a variable cannot be sed-replaced**; it must be read and its family resolved per caller (R-032) | same |

### Encoding

| # | Change | Where |
|---|---|---|
| 5 | Delete `latin1()` and its 22 call sites — 12 in `pdf.go`, 10 in `receipt_pdf.go`. Not deferrable (R-033) | `text.go`, `pdf.go`, `receipt_pdf.go` |
| 6 | Keep `isASCII` only if something else uses it; otherwise it goes with `latin1()` | `text.go` |
| 7 | Add a Go-tier test rendering a holder name with a diacritic and asserting the glyph survives (SC-025) | `eticket_test.go` |

### Type scale (e-ticket)

| # | Change | Where |
|---|---|---|
| 8 | Apply the design's sizes and weights from R-030's table: `Ticket N of M` 16→18 Bold; identity label 7.5→11 SemiBold; identity value 12→16 Bold; event name 9.5→14 Bold; ticket-type heading 20→22 Bold; `VALID FOR` 7.5→11 SemiBold; valid-for value 12→14 Bold; footer label 7.5→10 SemiBold; footer value 9→12 Medium (FR-003b) | `pdf.go` |
| 9 | Re-derive every y-offset below a grown element. The page constants are offsets from `bandHeight` (R-022) but the offsets are literals — a heading growing 16→18 pt pushes toward the accent rule with **no assertion to notice** (R-035) | `pdf.go` |
| 10 | Leave the page margins at 17mm. FR-003b excludes them: the design's 14.1mm inset would move the QR panel and the accent rule FR-022b pins | — |
| 11 | Record letter-spacing as a known limit in tasks.md §Known limits, next to the day-boundary and footer-coverage entries (R-034) | `tasks.md` |

## Tests that will break, and must be updated deliberately

| Test | Why it breaks | Correct response |
|---|---|---|
| `receipt_pdf_test.go` table geometry (SC-016, SC-017) | Inter is wider than Helvetica; the `TableInsets` / `HeaderColumnRights` seams measure real positions. | **Re-derive from the seams, never re-hardcode.** These failing is the system working — they are the only automated guard on this change. |
| `pdf_test.go` y-coordinate assertions | Grown type pushes everything below it. | Re-derive from the T132 band seam, as Revision 3 required for the same reason. |
| `eticket_test.go` FR-022e short/long address | `customerServiceBlock` computes width from `GetStringWidth`, so it self-adjusts. | **No change expected.** If this breaks, the width math stopped being metric-driven — report it rather than adjusting the expectation. |
| Anything asserting `latin1()` output | The function is deleted. | Delete with it; replace with the diacritic test (#7), which asserts the real behaviour rather than the transliteration. |
| `e2e/specs/guest-purchase.spec.ts` | Nothing should break — count, filename, magic and page count are all font-independent. | **If it does break, stop.** A changed page count means type growth spilled a ticket onto a second page, which `SetAutoPageBreak(false)` is supposed to make impossible. |

## Complexity Tracking — Revision 6

| Item | Why it is not gold-plating |
|---|---|
| Registering five faces when three would render (#1, #3) | Medium and SemiBold are the design's label and footer-value weights. Approximating them with Regular and Bold reintroduces the emphasis error Revision 5 just fixed, one layer down. |
| Deleting `latin1()` in the same change (#5) | Not optional — see R-033. Keeping it produces malformed UTF-8 under an embedded font. |
| A mandated visual diff (quickstart Step 3) | No tier in this repo asserts "looks like the design", and R-035 shows the e-ticket's absolute layout has assertion gaps precisely where growth lands. |

## Post-Design Constitution Re-check — Revision 6

All nine principles **PASS**. Principle VIII is discharged with an explicit and unusual
finding: this revision owes **no** red-first scenario (nothing was violated), gains **no** new
`e2e/` assertions (the shipped PDF is compressed), and its real acceptance is a Go-tier
diacritic test plus a required human visual diff. That is stated plainly rather than dressed
up as coverage it does not have.

No schema, no migration, no configuration key, no endpoint — therefore **no `SCHEMA.md`
change, no `PRD.md` change and no constitution amendment**. The repository gains its first
committed font assets, which is a new asset class but not a new architectural boundary.

---

# Revision 7 — Email Mask Direction (2026-08-19)

**Branch**: `fix/receipt` (spec dir `016-receipt-ticket-email`) | **Spec**:
[spec.md](./spec.md) §Clarifications, Session 2026-08-19 (email mask direction)

Triggered by reading a delivered receipt after Revision 4 shipped the phone rule: the email
was still masked from the front, producing eight asterisks where the design shows three.

Research: [research.md](./research.md) **R-036 … R-039**. Revisions 4 and 5 are implemented;
Revision 6 is planned but not started. **This revision is independent of Revision 6** — it
touches `mask.go` and a test fixture, neither of which the typeface work goes near.

> **`setup-plan.sh` still cannot resolve this feature** — it reports
> `020-qris-frame-simplify`. It copied nothing and touched no file. Point `/speckit-tasks` and
> `/speckit-implement` at `016-receipt-ticket-email` explicitly.

## What changed in the problem

FR-033's email clause is replaced, not amended. It kept the first 5 characters of the local
part; it now keeps everything except the last 3. The two halves of FR-033 are finally the same
shape — mask the tail, keep the head — differing only in how much.

Two things fall out that the request did not mention:

- **A disclosure closes.** Under keep-first-5, a local part of exactly 5 characters was
  returned **unchanged** — `maskFrom` has an explicit `len == keep` branch that returns `s`.
  `dimas@gmail.com` rendered as itself. The new floor hides at least one character always
  (R-036).
- **`maskFrom` becomes dead.** Revision 4 removed its phone caller; this removes its two email
  callers. It must be deleted, not left as a helper encoding a superseded rule beside the rule
  that replaced it.

## The one thing that would go wrong quietly

**The e2e fixture cannot see this change.** `defaultHolder.email` is `budi@example.com`, whose
4-character local part renders `b***` under **both** rules. An assertion written against it
would be green before and after the fix.

That is the second time this feature has walked into a test that cannot fail — R-027 caught
the first, in the phone masking. The fix is one line (R-037): lengthen the fixture's local
part. All three references to `defaultHolder.email` are by identity, not by literal, so
nothing else moves.

**Four of the seven mask cases coincide between old and new rules.** Any fixture with a local
part of four characters or fewer is blind to this change. That is the fact to hold onto while
writing every assertion in this revision.

## Constitution Check — Revision 7

| Principle | Verdict |
|---|---|
| I — Modular Monolith | **PASS**. One function in `internal/notification`. |
| II — Domain Isolation | **PASS**. No import changes; nothing reads data. |
| III — DTO Isolation | **PASS**. No struct touched. |
| IV — Transactional Integrity | **PASS**. Not near a transaction. |
| V — Gateway Abstraction | **PASS**. Untouched. |
| VI — Guest-First MVP Scope | **PASS**. A display rule on an existing surface. |
| VII — Cache | **PASS**. No Redis interaction. |
| VIII — E2E Acceptance | **ACTION REQUIRED**, discharged below. |
| IX — Throttling | **PASS**. Untouched. |

**Principle VIII, restated:**

- [x] **Covered flow touched?** The guest journey's delivery step — the email body carries the
      masked address. `e2e/specs/guest-purchase.spec.ts` changes in the same commit.
- [x] **Bugfix in a covered flow?** **Yes.** The requested change is a defect in the shape,
      and the 5-character disclosure R-036 found is a second, unreported one. A red-first
      assertion is owed — **and it is only possible after the fixture changes** (R-037).
- [x] **Where does it land?** The email body is plain HTML in `mail.HTML`, so `e2e/` can
      assert the real shape. The receipt's copy stays at the Go tier, compressed PDF as ever
      (R-025).
- [x] **New user-visible surface?** No. Same rows, different value.
- [x] **Cache and throttle modes?** No behavioural difference; both still run.

## Work breakdown — Revision 7

### Backend

| # | Change | Where |
|---|---|---|
| 1 | Replace `emailKeep = 5` with `emailMaskTrailing = 3` | `internal/notification/mask.go` |
| 2 | Rewrite `MaskEmail` to keep the whole domain and every character of the local part except the last 3, with asterisks = `min(3, len-1)` and a one-character local part masked entirely (FR-033). A value with no `@` takes the same rule, which the clarification accepted knowingly | same |
| 3 | Delete `maskFrom`. Zero callers remain after #2 (R-036) | same |
| 4 | **Do not** merge `MaskEmail` and `MaskPhone` into one trailing-mask helper. Their floors come from different requirements and a shared helper would carry the difference as an undecodable boolean at each call site (R-038) | same |
| 5 | Rewrite `TestMaskEmail` — 4 of its 7 cases change; `bud`, `a` and the empty value coincide. Add a case for a 5-character local part, which is the disclosure the old rule had | `mask_test.go` |
| 6 | Leave `receipt_pdf_test.go:112` and `service_test.go:510` untouched. They call `MaskEmail` as their own oracle; surface-vs-helper agreement is their job (SC-008, R-039) | — |

### e2e

| # | Change | Where |
|---|---|---|
| 7 | Lengthen `defaultHolder.email`'s local part past four characters. Without this, #8 cannot be red (R-037) | `e2e/support/journey.ts` |
| 8 | Assert the delivered body carries the new masked shape as a **literal**, and not the old one — beside the phone assertions added in Revision 4 | `e2e/specs/guest-purchase.spec.ts` |

## Tests that will break, and must be updated deliberately

| Test | Why | Correct response |
|---|---|---|
| `mask_test.go` `TestMaskEmail` | 4 of 7 cases encode the old shape. | Rewrite (work item 5). This is the change, not a casualty. |
| `mask_test.go` `TestMaskingNeverLengthensAValue` | Nothing breaks — asterisks replace exactly what they hide. | No change. Confirm it still runs. |
| `receipt_pdf_test.go:112`, `service_test.go:510` | Nothing breaks — they call the helper as their own oracle. | **Leave them.** If either needs an edit, the two surfaces stopped sharing one helper: report it. |
| `e2e/specs/guest-purchase.spec.ts` | Nothing breaks; #8 is an addition. | Confirm it is **red before the fix**, which requires #7 to land first. |

## Complexity Tracking — Revision 7

| Item | Why it is not gold-plating |
|---|---|
| Changing an e2e fixture (#7) | Without it the acceptance assertion is decorative. The alternative is Go-tier-only coverage on a flow `e2e/` can see perfectly well. |
| Deleting `maskFrom` (#3) rather than leaving it unused | It encodes the superseded rule, in the same file as its replacement. |
| Declining the obvious refactor (#4) | Recorded precisely because the symmetry is tempting; the reasoning has to survive the next reader who notices it. |

## Post-Design Constitution Re-check — Revision 7

All nine principles **PASS**. Principle VIII is discharged with the ordering constraint made
explicit: the fixture change must precede the assertion, or the red-first step is not
reproducible.

No schema, no migration, no configuration key, no endpoint — **no `SCHEMA.md`, `PRD.md` or
constitution change**. `contracts/notification.md` is unaffected: masking is applied inside the
renderers and the Message contract does not describe it.

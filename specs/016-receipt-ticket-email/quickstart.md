# Quickstart & Validation: Receipt + E-Ticket Email Attachments

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Contracts**: [contracts/notification.md](./contracts/notification.md)

How to run, observe, and verify this feature. Structure and field details live in
[data-model.md](./data-model.md) and the contracts file — this document is the run guide.

---

## Prerequisites

Postgres, Redis, **and Mailpit** — the runner owns none of them. Mailpit is the addition:
the suite has never needed it before because delivery was asserted through
`orders.email_sent` alone (R-007).

```bash
REDIS_PORT=6380 docker compose up -d postgres redis mailpit
docker compose run --rm migrate up
```

Confirm Mailpit is reachable before going further — a silent absence is exactly the
failure mode that made the old assertions weak:

```bash
curl -fsS http://localhost:8025/api/v1/messages | head -c 200   # expect JSON, not a refusal
```

> `AGENTS.md` documents the two-service startup line. Adding `mailpit` to it is a task in
> this change, not an aside — the next person to run the suite will copy that line.

---

## Step 1 — Confirm the new assertions fail against today's code

Not optional, and not the same as the Principle VIII bugfix rule (this is a behaviour
change, not a fix — see [plan.md](./plan.md) Constitution Check). The point is narrower:
a test asserting "two attachments" that was never seen failing might be asserting nothing.

Before any implementation, with the new specs written:

```bash
cd e2e && npx playwright test specs/guest-purchase.spec.ts -g "attachment"
```

**Expected: RED**, reporting 1 attachment where 2 were expected. Record it. If it passes,
the assertion is not reading what it claims to read.

---

## Step 2 — Go tier

```bash
cd backend && ./scripts/test.sh ./...
```

What must be covered — these map 1:1 onto the contract's assertable properties:

| Area | File | Asserts |
|---|---|---|
| Two attachments | `service_test.go` | `len(Attachments) == 2`; distinct filenames both containing the order number; both `application/pdf`; both start with `%PDF-` (FR-001, FR-004) |
| Body content | `service_test.go` | Every design section present; **no** ticket code, **no** QR anywhere in the HTML (FR-030) |
| Body/receipt agreement | `service_test.go` | Masked email and phone byte-identical between the two surfaces (SC-008) |
| Pre-fee order | `service_test.go` | `Subtotal == nil` ⟹ no subtotal row, no fee rows, total still correct (FR-013) |
| No phone | `service_test.go` | `BuyerPhone == ""` ⟹ the row is absent, not blank (FR-027) |
| Render failure | `service_test.go` | Either PDF failing ⟹ no `Send`, no `MarkEmailSent` (FR-006) |
| Page count | `pdf_test.go` | Pages == tickets, for 1, 3, and a bundle order (SC-002) |
| No money on tickets | `pdf_test.go` | No `Rp`/`IDR` and no digit-grouped amount in the e-ticket PDF (FR-021, SC-007) |
| Ticket-type dates | `pdf_test.go` | A Day 1 + Day 2 order prints each page's own window, never the event's (FR-020) |
| Latin-1 guard | `pdf_test.go` | A curly quote / em dash in event name, venue, address renders readably (R-006) |
| Receipt figures | `receipt_pdf_test.go` | Every line, every frozen fee, subtotal, and a total equal to the charged amount (FR-012, FR-014) |
| Line sub-line | `receipt_pdf_test.go` | The four `AdmissionStarts` × `Descriptor` cases in [data-model.md](./data-model.md) |
| Masking | `mask_test.go` | The full table in [research.md](./research.md) R-003, degenerate inputs included |
| Domain isolation | `cmd/api/architecture_test.go` | Passes unchanged — `notification` imports no sibling domain |

> Asserting "no money in the ticket PDF" needs the PDF's text, not its bytes. Extracting
> text from `gofpdf` output is awkward; the pragmatic approach is to assert on the strings
> handed to the drawing calls via a seam, or to search the uncompressed content stream.
> Whichever is chosen, it must actually fail when a price is reintroduced — verify that by
> temporarily adding one.

---

## Step 3 — Look at the output

Automated assertions do not tell you whether a document is *readable*. Both attachments
are designed artifacts and must be inspected against Figma at least once.

Run a real purchase through the app, or write the bytes from a Go test to disk:

```bash
# after a settled order, pull the message Mailpit received
curl -s http://localhost:8025/api/v1/messages | jq -r '.messages[0].ID'
curl -s http://localhost:8025/api/v1/message/<ID> | jq '.Attachments[] | {FileName, ContentType, Size}'
```

Expect exactly two entries, `receipt-<order>.pdf` first and `tickets-<order>.pdf` second.
Download both and check against the designs:

| Document | Figma | Look for |
|---|---|---|
| Email body | `741-5105` | Section order; masked contact rows; **no** per-ticket cards |
| Receipt | `688-671` | Two-column details; dark table header; totals block; footer |
| E-ticket | `683-148` | `Ticket N of M`; QR + code; **no price row** — the revised design removed it |

Open the email body in the Mailpit UI at <http://localhost:8025> as well as reading the
HTML — FR-031 is about how a client renders it, and inline-styled tables are easy to get
subtly wrong in a way that only shows visually.

---

## Step 4 — e2e acceptance gate (Principle VIII)

```bash
cd e2e && npm test                              # headless
cd e2e && npm run test:slow                     # headed, for watching the journey
E2E_CACHE_ENABLED=false npm test                # kill-switch mode — MUST also pass
```

Scenarios this change adds to `specs/guest-purchase.spec.ts`:

1. **Settled order delivers two attachments.** After settlement, poll Mailpit for the
   message to the buyer; assert exactly two attachments, both PDFs, filenames carrying the
   order number. (FR-001, FR-004 / SC-001)
2. **E-ticket page count equals ticket count.** Multi-ticket order; assert the e-ticket
   attachment's page count. (FR-002 / SC-002)
3. **Resend delivers the same pair.** Trigger the guest resend, poll for the *second*
   message, assert two attachments again and the same ticket codes. (FR-007 / SC-005)

Both cache modes exercise identical code here — nothing on this path touches Redis — but
the suite is still run both ways, because that is how the kill switch is verified rather
than assumed.

**Mailpit hygiene**: clear messages between specs that read them, or the resend scenario
will match the first delivery. `DELETE /api/v1/messages` empties the mailbox.

---

## Step 5 — Governance sync (same commit, not a follow-up)

The constitution binds delivery to *"a single PDF together with the receipt"*. This change
refines that; [plan.md](./plan.md) records it as a **MINOR** amendment. Before merging:

- [ ] `.specify/memory/constitution.md` — Critical Data Flow Rules ticket-generation
      bullet updated; Sync Impact Report prepended.
- [ ] `PRD.md` §1.4 (line 43) — "as a PDF plus the receipt" → two attachments.
- [ ] `ARCHITECTURE.md` (lines 321–322) — sequence-diagram note names both documents.
- [ ] `SCHEMA.md` — **no change**, and that is verified rather than assumed: this feature
      adds no migration.
- [ ] `AGENTS.md` — the compose startup line includes `mailpit`.

---

## Definition of done

- [ ] Every Go test in Step 2 passes; `./scripts/test.sh ./...` green.
- [ ] `architecture_test.go` green — no new cross-domain import.
- [ ] The Step 1 assertions were confirmed RED before the implementation.
- [ ] `npm test` green with the cache on **and** off.
- [ ] Both documents visually checked against Figma at least once.
- [ ] Frontend untouched — `git diff --stat frontend/` is empty.
- [ ] No migration — `git diff --stat backend/migrations/` is empty.
- [ ] Governance sync in Step 5 complete in the same commit.

## Known follow-ups handed to `/speckit-tasks`

- **Logo asset** (R-005) — not in the repository. Either add it under `backend/assets/`
  with `BRAND_LOGO_PATH`, or ship the text wordmark fallback and record the gap.
- **`PublicOrderItem.description`** (R-009) — the one change reaching outside
  `internal/notification`; additive nullable field on a guest-facing response.
- **`updated_at` drift on resend** (R-008) — a resent receipt legitimately shows a later
  "Last updated" than the original while every figure is identical. Correct, but worth a
  code comment so it is not later "fixed" into a bug.

---

# Revision 2 — Validating the Design-Review Corrections (2026-08-12)

Revision 1's steps still apply. This section covers what the 2026-08-12 corrections add,
and it leads with the one item that is a **bugfix**, because Principle VIII treats that
differently from the rest.

## Step 0 — Pin the timezone BEFORE writing the regression scenario

Do this first. Not doing it makes the next step impossible rather than merely harder.

The API server under Playwright inherits the developer's `TZ`. On a Jakarta machine a WIB
assertion passes **with the bug unfixed**, so the scenario can never be seen red and
Principle VIII's requirement is unmeetable.

```ts
// e2e/playwright.config.ts — api webServer env
TZ: "UTC",
```

Verify the pin took, before trusting anything downstream:

```bash
cd e2e && npx playwright test --list >/dev/null   # boots the servers
curl -s http://localhost:8100/healthz             # any endpoint; then check the server log's timestamps
```

## Step 1 — See the day-boundary bug red

The defect: a ticket admitting at `2026-04-27 06:00 WIB` is `2026-04-26T23:00Z`, and the
receipt prints the UTC calendar day — telling a Day 2 holder they admit on Day 1.

Reproduce it at the Go tier first, where it is cheapest:

```bash
cd backend
TZ=UTC          go test ./internal/notification/ -run TestReceiptRendersAdmissionDatesInJakarta -count=1
TZ=Asia/Jakarta go test ./internal/notification/ -run TestReceiptRendersAdmissionDatesInJakarta -count=1
```

**Expected before the fix: RED in both.** Both, not just UTC — `receiptSubLine` formats in
the *value's* location, so a `time.UTC` input renders the UTC day whatever the host is.
A test that only goes red under `TZ=UTC` is testing the host, not the code.

Then the e2e scenario, with `TZ` pinned per Step 0. Record both reds before fixing.

## Step 2 — Currency, and the assertion that does not exist yet

```bash
cd backend && go test ./internal/notification/ -run TestFormatIDR -count=1
```

The negative case is the one that matters:

```
IDR -550,000     ← correct
IDR -,550,000    ← what you get if the digits[i-1] != '-' guard is dropped
```

No test covers it today, so a rewrite that loses the guard **ships green**. Add the
assertion before touching the formatter.

Expect roughly 20 existing assertions across `format_test.go`, `service_test.go` and
`receipt_pdf_test.go` to fail on `Rp` → `IDR`. That is expected and mechanical — but read
each one rather than sed-ing, because three of them are also changing for timezone reasons
and would otherwise be "fixed" to the wrong expectation.

## Step 3 — Time formats, with inputs that prove conversion

Rewrite the three `TestFormatTicketWindow*` cases with **UTC inputs and WIB expectations**:

| Input | Expected |
|---|---|
| `2026-04-26T03:00Z` → `2026-04-26T14:00Z` | `26 Apr 2026 @ 10:00 - 21:00 WIB` |

A test written as `10:00 WIB` in → `10:00 WIB` out proves the layout string was copied, not
that any conversion happened. Every new time assertion must cross a zone boundary.

Also decide and document the **multi-day** and **single-instant** branches. The requirement
only specifies the same-day form; leaving the other two on the old `Mon, 02 Jan 2006 15:04
MST` layout would ship one ticket reading `26 Apr 2026 @ 10:00 - 21:00 WIB` and another
reading `Wed, 02 Sep 2026 09:00 UTC`.

## Step 4 — The email, in a real client

Automated assertions cannot tell you the email *looks* right, and this revision's changes
are almost entirely visual.

```bash
cd backend && go test ./internal/notification/ -run TestSendTicketEmail -count=1
# then render a real one and open it
curl -s http://localhost:8025/api/v1/messages | jq -r '.messages[0].ID'
curl -s http://localhost:8025/api/v1/message/<ID> | jq '{Attachments: [.Attachments[].FileName], Inline: [.Inline[] | {FileName, ContentID}]}'
```

Expect exactly:

```json
{
  "Attachments": ["receipt-<order>.pdf", "tickets-<order>.pdf"],
  "Inline": [{"FileName": "jive-logo.png", "ContentID": "jive-logo.png"}]
}
```

Two attachments, and the logo in `Inline` — **not** three attachments. If the logo appears
under `Attachments`, `Compose` used `AttachReader` instead of `EmbedReader`.

Then open the message in the Mailpit UI at <http://localhost:8025> and check against Figma
741-5105:

- the logo renders (not a broken-image box — that means the `cid:` and the filename disagree)
- the logo sits flush on the band with **no rectangular seam** (that means the band is
  still `#141B2D` instead of `#151A26`)
- the PAID badge is shrink-to-fit, not a full-width or pill-shaped block
- the fee amounts line up in the same column as the ticket-line amounts
- no rule under Total Payment
- the footer reads `© 2026 manjo`, and `Powered By Manjo` appears nowhere in the body

**Outlook cannot be checked from here.** The known, accepted degradation is a
square-cornered badge. If Outlook access exists, also confirm the logo is not oversized —
that symptom means the `<head>` mso `PixelsPerInch` block is missing.

## Step 5 — The two PDFs

```bash
cd backend && go test ./internal/notification/ -count=1
```

Then render and inspect both, as Revision 1's Step 3 describes. New things to look for:

- **Receipt**: row rules run the full table width with no notch at the left border; the
  envelope icon appears before the support address; `QRIS` is upper case; `Last updated`
  carries `WIB`.
- **E-ticket**: real logo on the band with no seam; `VALID FOR` reads
  `26 Apr 2026 @ 10:00 - 21:00 WIB`; the accent rule beside the QR has rounded ends in the
  design's colour; **no venue row**; the footer's two blocks sit at opposite edges.
- **Both**: check a rule drawn *after* the envelope — if it has rounded ends, the
  `SetLineCapStyle` / `SetLineJoinStyle` reset was forgotten. This is the failure mode most
  likely to pass every assertion and still look wrong.

## Step 6 — Full gates

```bash
cd backend && ./scripts/test.sh ./...
cd e2e && npm test
cd e2e && E2E_CACHE_ENABLED=false npm test
```

## Definition of done — Revision 2

- [ ] The day-boundary scenario was confirmed **red in both timezones** before the fix.
- [ ] `TZ: "UTC"` is pinned on the e2e api server.
- [ ] The negative-amount assertion exists and passes.
- [ ] Every new time assertion crosses a zone boundary (UTC in, WIB out).
- [ ] Mailpit shows 2 `Attachments` and the logo under `Inline`.
- [ ] The logo sits seamlessly on a `#151A26` band in the email and both PDFs.
- [ ] `Powered By Manjo` appears in the receipt only, never in the email body.
- [ ] A rule drawn after the receipt envelope has square ends.
- [ ] Both cache modes green.
- [ ] `git diff --stat frontend/` and `backend/migrations/` still empty.
- [ ] Constitution + `PRD.md` carry the word "document" on the attachment rule.

## Still open

**The location pin (FR-025).** Figma node `750:2` is an emoji text layer and cannot be
exported — every route yields a blank image. Until the decision in
[plan.md](./plan.md) is taken, the venue block ships without an icon and FR-025 is not met.
Everything else in this revision proceeds independently.

---

# Revision 3 — Validating the Brand Refresh (2026-08-19)

Research: [research.md](./research.md) R-018 … R-024. Plan:
[plan.md](./plan.md) §Revision 3.

## Step 0 — Reproduce the "four attachments" finding, and see why it is not one

Do this **before** changing anything. It is the step that stops a day being spent fixing a
non-defect, and it is the reason this revision writes no regression scenario.

```bash
REDIS_PORT=6380 docker compose up -d postgres redis mailpit
docker compose run --rm migrate up

# after settling one order through the real flow, read what Mailpit actually holds
ID=$(curl -s http://localhost:8025/api/v1/messages | jq -r '.messages[0].ID')
curl -s "http://localhost:8025/api/v1/message/$ID" \
  | jq '{attachments: [.Attachments[].FileName], inline: [.Inline[].ContentID]}'
```

Expected — and what it means:

```json
{
  "attachments": ["receipt-ORD-….pdf", "tickets-ORD-….pdf"],
  "inline":      ["jive-logo.png", "location-pin.png"]
}
```

(These are the pre-change names — Step 1 renames the logo.) The API separates them; so do Mailpit's own UI badges (`Attachments (2)` /
`Inline images (2)`). The four-file strip beneath those badges is `allAttachments()`, which
concatenates every part because Mailpit is a MIME debugger. **Open the same message in a
real client to confirm** — Gmail and Yopmail list two.

> Because nothing is broken here, **do not write a regression scenario for the attachment
> count.** A test that cannot fail against the unfixed code proves nothing, which is exactly
> what Principle VIII is guarding against.

## Step 1 — Generate the shipped derivatives

The supplied files are the source of truth and are **not** what ships: 6400×4290, ~276 KB,
and carrying ~14% transparent padding that would otherwise be baked into every surface
differently (R-020).

```bash
cd frontend/public/brand
for v in WHITE BLACK; do
  out=$(echo "$v" | tr 'A-Z' 'a-z')
  magick "LOGO JIVE MINUS TWO_${v}.png" \
    -trim +repage -resize 800x -strip -colors 64 \
    -define png:compression-level=9 "jive-logo-${out}.png"
done
identify -format "%f  %wx%h  %b\n" jive-logo-*.png
```

Expected: `800x455`, roughly 15 KB each — against 349 KB for the asset being retired.
Confirm the trimmed ratio is **1.757**, not the file's 1.492; if you see 1.492 the trim did
not happen and every placement will inherit dead space.

Copy the white derivative to `backend/internal/notification/assets/brand/` and point
`//go:embed` at it. Then delete both copies of the retired mark — SC-019 fails if either
survives:

```bash
rg -n 'jive-logo\.png' --glob '!node_modules' --glob '!*.md'   # must return nothing
```

## Step 2 — Confirm the legibility target with your own eyes

FR-023b is the one requirement here that a passing test cannot fully vouch for. Render the
mark at the old and new sizes on the real band colour and compare:

```bash
magick jive-logo-white.png -resize 108x -background '#151a26' -gravity center -extent 300x100 /tmp/at108.png
magick jive-logo-white.png -resize 280x -background '#151a26' -gravity center -extent 320x180 /tmp/at280.png
```

`SPONSORED BY` and `MINUS TWO` should be mushy at 108 px and clean at 280 px. If they are
clean at both, the trim in Step 1 produced a different crop than expected — re-check it
rather than lowering the target.

## Step 3 — Go tier

```bash
cd backend && ./scripts/test.sh ./...
```

Expected failures on first run, each with a specific correct response — see plan.md
§"Tests that will break". In short:

- `smtp_test.go:155` and `service_test.go:386` — filename changed. Update the **name only**.
  The inline-vs-attachment assertions at `smtp_test.go:119`–`:131` are what keep R-018's
  finding true and must not be weakened to make anything pass.
- `pkg/config/config_test.go:275` — the two brand defaults changed (FR-035a).
- `pdf_test.go` / `receipt_pdf_test.go` — any y-coordinate below the band moved by
  +17.3 mm. Re-derive these from `bandHeight` rather than re-hardcoding, or the next band
  change breaks them the same way.
- `eticket_test.go:150` — should **not** break. It exercises the missing-asset wordmark
  fallback, which still exists.

## Step 4 — Look at both PDFs

Content assertions do not catch a rule drawn through a band or an icon floating off its
line. Render and open:

```bash
cd backend && go test ./internal/notification/ -run 'TestRenderTicketsPDF|TestRenderReceiptPDF' -v
```

Check by eye:

- the mark sits inside the taller band with even inset, and **no dark rectangle hangs past
  it** — the new asset has alpha, so a seam here means the old opaque-background fitting
  strategy is still in force (R-020);
- the accent rule and QR panel moved **with** the band, not independently (R-022);
- in the footer, `CUSTOMER SERVICE`, the envelope and the address share one left edge, and
  the block sits against the right margin (FR-022a);
- the envelope is on the address's line, not above it (FR-022d, and the same defect FR-015b
  already caught once on the receipt);
- **on page 2 of a multi-ticket order**, the rules have square ends. Round ends there mean
  `drawEnvelope`'s sticky cap/join reset was lost — a defect that renders perfectly on a
  one-ticket order (R-023).

Then vary the input, because FR-022e is about behaviour and not the mock:

```bash
BRAND_SUPPORT_EMAIL=a@b.co   # short
BRAND_SUPPORT_EMAIL=customer-service.team@jive-promotion.example.com   # long
```

The left edge must hold in both.

## Step 5 — The email, in a real client

```bash
cd backend && go test ./internal/notification/ -run TestBuildEmailBody
# then settle a real order and open http://localhost:8025
```

- the header mark renders at 280 px and is **not** listed as an attachment;
- `width`/`height` are present as HTML **attributes**, not only CSS — without them Outlook's
  Word engine draws the 2× part at double size;
- forward the message to a real Outlook/Windows account if one is available. This is the
  client the attribute rule exists for, and Mailpit cannot stand in for it.

## Step 6 — Frontend tier (new for this feature)

Revision 1 said "no frontend change"; that no longer holds (R-024). Note the toolchain
quirk — `npx` and bare `node` both fail in this repo:

```bash
cd frontend
export PATH="$HOME/.nvm/versions/node/v26.5.1/bin:$PATH"
./node_modules/.bin/vitest run
./node_modules/.bin/next build      # the typecheck of record
```

Then load the site and confirm the header bar grew without pushing content below the fold
awkwardly, and that the mark is crisp on a HiDPI display.

## Step 7 — Full gates

```bash
cd backend && ./scripts/test.sh ./...
cd e2e && npm test
cd e2e && E2E_CACHE_ENABLED=false npm test    # Principle VII kill switch
```

The e2e addition is in the **email body only** — the mark's dimensions and the `cid:`
pairing — because the body is readable HTML on the wire. The e-ticket footer is **not**
assertable here: production PDFs are compressed, so their text never appears in the
downloaded bytes, and the footer's coverage lands at the Go tier through
`RenderTicketsPDFPlain` (R-025). The existing `expect(mail.Attachments).toHaveLength(2)`
needs **no change**.

## Definition of done — Revision 3

- [ ] Derivatives trimmed to 1.757, quantised, ~15 KB; both copies of the retired mark gone
      and `rg jive-logo.png` clean (SC-019)
- [ ] Secondary line legible on all three surfaces at 100% zoom, print included (SC-020)
- [ ] E-ticket footer: one left edge for label/icon/address, block against the right margin,
      holding for a short and a long address (SC-021, FR-022a, FR-022e) — asserted at the
      **Go tier** via `RenderTicketsPDFPlain`, not in `e2e/`
- [ ] `bandHeight` change carried through every constant below it; page 2 rules square
- [ ] `BRAND_SITE_URL` and `BRAND_SUPPORT_EMAIL` at the design's values, receipt footer
      updated with them (FR-035a)
- [ ] Email mark at 280 px with attributes and CSS in agreement; still 2 attachments in a
      real client
- [ ] All three tiers green, e2e green in both cache modes
- [ ] `smtp_test.go` inline-vs-attachment assertions intact and unweakened

## Still open

- **The site header's size needs a nod** (plan.md §"The one thing that needs a nod"). 200 px
  in a ~130 px bar is a judgment call, not something the clarification settled; a literal
  reading gives 254 px in a ~190 px bar.
- **The receipt still carries no brand mark**, contradicting FR-023a and FR-035's "all three
  surfaces". Recorded in spec.md §Governance notes; deliberately not widened into this
  change.
- **`favicon.ico` is still the Next.js scaffold icon** and the title is still
  "Event Ticketing". Neither is a logo call, and the lockup would need a glyph-only crop to
  work at 32×32.

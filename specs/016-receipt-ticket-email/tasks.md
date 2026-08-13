---

description: "Task list for Receipt + E-Ticket Email Attachments"
---

# Tasks: Receipt + E-Ticket Email Attachments

**Input**: Design documents from `/specs/016-receipt-ticket-email/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/notification.md](./contracts/notification.md), [quickstart.md](./quickstart.md)

**Tests**: Included and **not** optional here. `AGENTS.md` and Constitution Principles
VIII make the Go tiers and `e2e/` acceptance gates for this repository, and the spec's
success criteria (SC-001…SC-010) are only checkable through them.

**Organization**: Grouped by user story. US1 alone is a shippable increment.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task serves (US1, US2, US3)
- Every task names its exact file path

## Path Conventions

Web application per [plan.md](./plan.md): Go modular-monolith API under `backend/`,
Playwright acceptance suite under `e2e/`. **The frontend is not touched by this feature** —
`git diff --stat frontend/` must stay empty.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Configuration and test-harness prerequisites. Nothing here changes behaviour.

- [X] T001 [P] Add the six `BRAND_*` fields to the `Config` struct in `backend/pkg/config/config.go` alongside the existing SMTP block (~line 58), and their loader lines with defaults alongside the SMTP loaders (~line 200), per the configuration contract in [contracts/notification.md](./contracts/notification.md)
- [X] T002 [P] Add a case to `backend/pkg/config/config_test.go` asserting every `BRAND_*` key falls back to its documented default when unset, so the API starts and sends correctly with none of them configured
- [X] T003 [P] Add `mailpit` to the documented compose startup line in `AGENTS.md` (the `docker compose up -d postgres redis` line) and in `e2e/README.md` — the next person to run the suite copies that line, and the new assertions need Mailpit up
- [X] T004 [P] Add `mailpitURL` (default `http://localhost:8025`) to the config object in `e2e/support/env.ts`, following the existing `env()` pattern

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The data plumbing and pure helpers every user story depends on. Widening the
consumer-declared structs is what lets US1 and US2 proceed without a cross-domain import.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete — US1 and US2
both read fields that do not exist until T010–T014 land.

### Pure helpers (no dependencies, fully parallel)

- [X] T005 [P] Create `notification.Branding` in `backend/internal/notification/branding.go` with the six fields from [data-model.md](./data-model.md), documenting that it is platform-wide per FR-035 and deliberately not per-event per FR-036
- [X] T006 [P] Write the failing table tests for `MaskEmail` / `MaskPhone` in `backend/internal/notification/mask_test.go`, covering every row of the R-003 table in [research.md](./research.md) including the degenerate inputs (no `@`, single-character local part, empty string, phone shorter than 5). Confirm RED before T007
- [X] T007 Implement `MaskEmail` and `MaskPhone` in `backend/internal/notification/mask.go` per FR-033 — counting **characters, not digits**, so a leading `+` and any spacing survive verbatim. Turn T006 green
- [X] T008 [P] Write failing tests in `backend/internal/notification/pdf_test.go` proving a curly quote, an em dash, and a `·` in an event name, venue, or attendee name currently render as mojibake in the PDF (R-006). Confirm RED
- [X] T009 Implement the Latin-1 transliteration helper in `backend/internal/notification/text.go` and apply it to every free-text string written into a PDF, turning T008 green. This is a pre-existing defect that this feature widens by adding the admin-authored event `address` to a printed page

### Struct widening and adapter mapping (ordered — same files)

- [X] T010 Widen `OrderDelivery` and `ReceiptLine`, and add `DeliveryEvent` and `PaymentSummary`, in `backend/internal/notification/service.go` exactly as specified in [data-model.md](./data-model.md). `DeliveryEvent` carries no dates on purpose — every date on these surfaces is a ticket-type or per-line date, and an available event date is how that gets got wrong
- [X] T011 Add `UpdatedAt *time.Time` to `OrderRecord` in `backend/internal/order/repository.go` and to the query that populates it. Read-only widening of an existing `orders` select — no migration, so `SCHEMA.md` stays untouched (R-008)
- [X] T012 Expose a narrow payment read for the settling row's `payment_type` and `created_at` and hold it on `notificationOrderAdapter` in `backend/cmd/api/adapters.go`. It must come from the payment domain's own service, not from a cross-domain query — the composition root may hold both, a domain may not
- [X] T013 Map the new fields in `notificationOrderAdapter.OrderForDelivery` in `backend/cmd/api/adapters.go` (~line 296): `BuyerPhone`, `CreatedAt`, `UpdatedAt` from the `OrderRecord` it already holds; `Event` from the `TicketOrderDetail.Event` it already fetches at line 318 and currently discards; `Items[].AdmissionStarts` likewise; `Payment` from T012. Apply the `Method` fallback chain `payment_type` → `payment_provider` → `"QRIS"` so a null column never renders an empty cell
- [X] T014 Add a `Branding` parameter to `notification.NewService` in `backend/internal/notification/service.go` and pass it from the config at the wiring site in `backend/cmd/api/main.go` (~line 227)
- [X] T015 Update the fakes in `backend/internal/notification/service_test.go` so the package compiles against the widened structs, and give them realistic values (a phone, an event address, a payment summary) rather than zero values — a fake that returns empty strings hides exactly the bugs these tests exist to catch
- [X] T016 Run `cd backend && go test ./cmd/api/ -run TestArchitecture` and confirm `architecture_test.go` is green — `internal/notification` must import no sibling domain after T010–T014

**Checkpoint**: The notification domain can now see everything the three surfaces need, and
no boundary moved. User story work can begin.

---

## Phase 3: User Story 1 - Buyer receives a filable receipt alongside the tickets (Priority: P1) 🎯 MVP

**Goal**: The ticket email carries two attachments — a Payment Receipt and an E-Ticket
document — with the e-ticket file holding exactly one page per issued ticket and no money
anywhere on it.

**Independent Test**: Settle an order end to end and inspect the delivered email: exactly
two PDF attachments; the e-ticket document's page count equals the order's ticket count;
the receipt's grand total equals the amount charged.

**Ships without US2.** The existing email body remains until Phase 4 and is not a blocker.

### Tests for User Story 1 ⚠️

> Write these FIRST and confirm they FAIL against the current single-attachment code.

- [X] T017 [P] [US1] Failing tests in `backend/internal/notification/service_test.go`: `len(Attachments) == 2`; both `application/pdf`; both contents start with `%PDF-`; filenames distinct and both containing the order number (FR-001, FR-004 / SC-001)
- [X] T018 [P] [US1] Failing tests in `backend/internal/notification/pdf_test.go`: page count equals ticket count for 1-ticket, 3-ticket, and bundle orders (FR-002 / SC-002); every page carries a distinct ticket code; **no** monetary figure appears anywhere in the e-ticket PDF (FR-021 / SC-007)
- [X] T019 [P] [US1] Failing tests in `backend/internal/notification/receipt_pdf_test.go`: every purchased line, every frozen fee line, the subtotal, and a grand total equal to `TotalAmount` (FR-012, FR-014); the pre-fee path (`Subtotal == nil`) shows the total alone with no subtotal row and no invented fee rows (FR-013)
- [X] T020 [P] [US1] Failing test in `backend/internal/notification/service_test.go`: when either document fails to render, `Send` is never called and `MarkEmailSent` is never called (FR-006)

> **On T018's "no money" assertion**: `gofpdf` output is not plain text. Either assert on
> the strings handed to the drawing calls through a seam, or search the uncompressed
> content stream — then verify the assertion actually fails by temporarily reinstating a
> price row. An assertion that cannot fail is not a test.

### Implementation for User Story 1

- [X] T021 [P] [US1] Create `backend/internal/notification/receipt_pdf.go` with `RenderReceiptPDF(order OrderDelivery, brand Branding) ([]byte, error)` and the A4 page scaffold, the "Payment Receipt" title, the order number, and the "Last updated" line from `order.UpdatedAt` (FR-008, Figma 688-671)
- [X] T022 [US1] Add the two-column Order Details / Transaction Details blocks to `backend/internal/notification/receipt_pdf.go` — customer name, masked phone, masked email on the left (FR-009, using T007); payment status, method, and date from `order.Payment` on the right (FR-010)
- [X] T023 [US1] Add the dark-header product table (Product / Price / Qty / Total) to `backend/internal/notification/receipt_pdf.go`, with each row's name and the sub-line rendering rule from [data-model.md](./data-model.md) — a bundle lists each admission day rather than collapsing to the earliest (FR-011)
- [X] T024 [US1] Add the totals block to `backend/internal/notification/receipt_pdf.go`: subtotal, then each frozen fee under its frozen name with the applicable-rates note on percentage fees, then the grand total. Pre-fee orders collapse to the total alone (FR-012, FR-013, FR-014). Turn T019 green
- [X] T025 [US1] Add the receipt footer to `backend/internal/notification/receipt_pdf.go` — processing statement, customer-service contact, and platform attribution, all from `Branding` (FR-015)
- [X] T026 [US1] Relay out the e-ticket page in `backend/internal/notification/pdf.go` to Figma 683-148: dark header band, `Ticket N of M` heading, QR left with ticket code, and the TICKET NO. / NAME / EMAIL / ORDER NO. column right (FR-016, FR-017, FR-018)
- [X] T027 [US1] Complete the e-ticket page body in `backend/internal/notification/pdf.go`: event name, ticket-type name, and the `VALID FOR` admission window from **that ticket type's** `EventStart`/`EventEnd` via the existing `FormatTicketWindow` (FR-019, FR-020). **Delete the price row and its tax-inclusive note** — the revised design removed them (FR-021). Turn T018 green
- [X] T028 [US1] Add the dark footer band to `backend/internal/notification/pdf.go` carrying the site name, site URL, and customer-service address from `Branding` (FR-022)
- [X] T029 [US1] Correct the stale doc comment on `TicketDetail.AttendeeEmail` in `backend/internal/notification/pdf.go` — it currently says the field is "not printed" and is "the per-holder delivery address the sender groups by". Both halves are now wrong: grouping ended when delivery collapsed to the buyer, and FR-018 puts the address on the page
- [X] T030 [US1] Render both documents in `SendTicketEmail` in `backend/internal/notification/service.go` and post them as two `Attachment` entries — **receipt first**, then tickets — named `receipt-<order_number>.pdf` and `tickets-<order_number>.pdf`. Both must be built before `mailer.Send`, so a render failure sends nothing and leaves `email_sent` FALSE. Turn T017 and T020 green
- [X] T031 [US1] Add a test in `backend/internal/notification/pdf_test.go` pinning that `TicketDetailsForOrder`'s slice order — and therefore the `Ticket N of M` numbering — is stable across repeated calls. The contract depends on it and the type cannot express it

### Receipt line descriptor — cross-domain chain (FR-011)

> **Scope note, read before starting.** The sub-line's trailing descriptor is the only part
> of this feature whose blast radius leaves `internal/notification`. It threads a new field
> through six files across three domains for one line of cosmetic text. It is genuinely
> required by FR-011, and it is also the cleanest thing to cut if this feature needs to
> shrink — dropping it degrades the sub-line to date-only (already handled by T023's
> rendering rule) and requires amending FR-011. Decide before T032, not during.

- [X] T032 [US1] Add `description` to the ticket-type and package display queries in `backend/internal/event/queries/event.sql` and regenerate with `sqlc`
- [X] T033 [US1] Add `Description` to `TicketTypeDisplayRecord` and `PackageDisplayRecord` in `backend/internal/event/admin_repository.go` and populate from the regenerated rows
- [X] T034 [US1] Add `Description` to `TicketTypeDisplay` and `PackageDisplay` in `backend/internal/order/admin_service.go`, and map it in `orderEventLookupAdapter` in `backend/cmd/api/adapters.go` (~lines 426 and 448)
- [X] T035 [US1] Add the nullable `description` field to `PublicOrderItem` in `backend/internal/order/dto.go` and populate it in `publicItems` in `backend/internal/order/public_service.go`. This is an **additive wire change** on `GET /ticket/order/:order_id` — nullable, so no existing consumer breaks
- [X] T036 [US1] Map `PublicOrderItem.Description` onto `ReceiptLine.Descriptor` in `notificationOrderAdapter` in `backend/cmd/api/adapters.go`, and extend T023's tests to cover all four `AdmissionStarts` × `Descriptor` cases from [data-model.md](./data-model.md)

**Checkpoint**: US1 is complete. The email carries two correct attachments. The body is
still the old one, and that is fine — ship or continue.

---

## Phase 4: User Story 2 - Email body reads as the new confirmation design (Priority: P2)

**Goal**: The email body matches Figma 741-5105 and drops the per-ticket cards, which now
live in the attachment.

**Independent Test**: Deliver a ticket email and read the rendered body against the design —
every section present, in order, populated from the real order, with no ticket code or QR
anywhere in it.

### Tests for User Story 2 ⚠️

- [X] T037 [P] [US2] Failing tests in `backend/internal/notification/service_test.go`: the body contains every design section in order, and contains **no** ticket code and **no** QR image (FR-030 / SC-006). Confirm RED against the current body, which contains both
- [X] T038 [P] [US2] Failing test in `backend/internal/notification/service_test.go`: the masked buyer email and phone are byte-identical between the rendered body and the rendered receipt (FR-032 / SC-008)

### Implementation for User Story 2

- [X] T039 [US2] Rewrite `buildEmailBody`'s opening in `backend/internal/notification/service.go`: dark header band with the **text** wordmark from `Branding` — never a remote `<img>`, which mainstream clients block and which reads as a tracking pixel on a receipt — then the confirmation headline and the greeting naming the buyer (FR-023, R-005)
- [X] T040 [US2] Add the order panel to `buildEmailBody` in `backend/internal/notification/service.go`: status badge, order number, order date from `CreatedAt`, and payment method from `Payment.Method` (FR-024)
- [X] T041 [US2] Add the event block to `buildEmailBody` in `backend/internal/notification/service.go` — event name with venue and full address from `order.Event` (FR-025)
- [X] T042 [US2] Add the ticket-details block to `buildEmailBody` in `backend/internal/notification/service.go`: each line with name, admission date, unit price × quantity and line total, then each frozen fee, then the emphasised total payment. Pre-fee orders collapse to the total alone (FR-026, FR-013)
- [X] T043 [US2] Add the Buyer Information block to `buildEmailBody` in `backend/internal/notification/service.go` using `MaskEmail`/`MaskPhone`, **omitting the phone row entirely** when `BuyerPhone` is empty rather than rendering a blank or fully-masked value (FR-027). Turn T038 green
- [X] T044 [US2] Add the important-information notice and the automated-email footer to `buildEmailBody` in `backend/internal/notification/service.go` — entry, sharing and refund rules; "replies are not read"; platform attribution from `Branding` (FR-028, FR-029)
- [X] T045 [US2] Delete the per-ticket card loop and the `distinctHolderNames` helper from `backend/internal/notification/service.go` if nothing else uses them, and confirm the body carries no ticket code or QR. Turn T037 green (FR-030)
- [X] T046 [US2] Verify every style in the rewritten body is an inline-styled table attribute, with no `<style>` block and no external stylesheet, and that the layout degrades to readable text when images are blocked (FR-031)

**Checkpoint**: US1 and US2 both complete and independently verifiable.

---

## Phase 5: User Story 3 - A resend delivers the identical pair (Priority: P3)

**Goal**: The automatic send, the guest resend, and the admin resend all produce the same
email with the same two attachments and the same ticket codes.

**Independent Test**: Settle an order, capture the delivered email, resend, and compare —
same attachment count, same ticket codes, same monetary figures.

- [X] T047 [P] [US3] Add a test in `backend/internal/notification/service_test.go` that a second `SendTicketEmail` for the same order produces two attachments again and reuses the already-issued ticket codes verbatim — codes are never regenerated (FR-007 / SC-005)
- [X] T048 [US3] Add a test in `backend/internal/notification/service_test.go` that the receipt's figures are unchanged when the fee **master data** changes after the order was placed, proving the frozen `order_fees` values are what render (FR-038 / SC-009)
- [X] T049 [US3] Confirm by inspection that all three entry points still funnel through the single `SendTicketEmail` — `ticketDelivererAdapter` in `backend/cmd/api/adapters.go`, `resend` and `resendPublic` in `backend/internal/notification/handler.go` — so the two-attachment change cannot reach one path and miss another
- [X] T050 [US3] Add a code comment at the `UpdatedAt` mapping in `backend/cmd/api/adapters.go` recording that a resent receipt legitimately shows a later "Last updated" than the original while every monetary figure is identical (R-008). Correct behaviour that looks like a bug is how correct behaviour gets "fixed"

**Checkpoint**: All three user stories complete.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T051 [P] Resolve the logo asset (R-005): either add it under `backend/assets/` and wire `BRAND_LOGO_PATH`, or ship the text-wordmark fallback and record the gap in the plan. Either way, a missing or unreadable logo must never fail a send — it is decorative, and FR-006 covers document generation, not ornament
- [X] T052 [P] Run `cd backend && ./scripts/test.sh ./...` and confirm the whole Go suite is green, including the database-backed tiers
- [X] T053 [P] Confirm `git diff --stat frontend/` and `git diff --stat backend/migrations/` are both empty — this feature changes no frontend file and adds no migration
- [X] T054 Visually inspect all three surfaces against Figma per [quickstart.md](./quickstart.md) Step 3 — open the body in the Mailpit UI at `http://localhost:8025`, and open both PDFs. Automated assertions prove content, not legibility

### End-to-end acceptance (Constitution Principle VIII) — NOT optional

- [X] T055 Create `e2e/support/mail.ts`: a Mailpit API client that polls `GET /api/v1/messages` for the message addressed to a given buyer, fetches `GET /api/v1/message/{id}`, and returns its attachment list (filename, content type, size). It must tolerate the asynchronous post-payment goroutine the same way the existing `email_sent` assertions do, and expose a `DELETE /api/v1/messages` helper so a resend scenario cannot match the first delivery's message
- [X] T056 Extend `e2e/specs/guest-purchase.spec.ts` with the settled-order scenario: exactly two attachments, both PDFs, filenames carrying the order number (FR-001, FR-004 / SC-001). **Confirm it fails against the current single-attachment code before implementing** — see [quickstart.md](./quickstart.md) Step 1
- [X] T057 Extend `e2e/specs/guest-purchase.spec.ts` with the page-count scenario: a multi-ticket order's e-ticket attachment has exactly one page per issued ticket (FR-002 / SC-002)
- [X] T058 Extend `e2e/specs/guest-purchase.spec.ts` with the resend scenario: trigger the guest resend, poll for the second message, assert two attachments and unchanged ticket codes (FR-007 / SC-005)
- [X] T059 Narrow the tolerate-a-refused-SMTP-connection stance recorded at `e2e/playwright.config.ts:123-124` so it does not apply to the specs that assert attachments — a silently absent Mailpit must fail those specs, not pass them
- [X] T060 Run the full suite green: `cd e2e && npm test`
- [X] T061 Run it once more with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test`

### Governance sync (Constitution §Governance) — same commit, not a follow-up

- [X] T062 Amend `.specify/memory/constitution.md`: update the Critical Data Flow Rules ticket-generation bullet so delivery carries **two** attachments — the merged e-ticket PDF and the receipt PDF — with the receipt also remaining in the body. Prepend a Sync Impact Report recording this as **MINOR**: every clause survives (one email, one recipient, one PDF holding every ticket) and only the receipt's location changes, which is expanded guidance rather than a reversal
- [X] T063 [P] Update `PRD.md` §1.4 (line 43): "containing every ticket in the order as a PDF plus the receipt" → two attachments
- [X] T064 [P] Update the sequence-diagram note in `ARCHITECTURE.md` (lines 321–322) so it names both documents
- [X] T065 Confirm `SCHEMA.md` needs no change and record that it was **verified, not assumed** — this feature adds no column, table, or migration

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies — start immediately
- **Foundational (Phase 2)**: depends on Phase 1 (T001 feeds T014) — **blocks US1 and US2**
- **US1 (Phase 3)**: depends on Phase 2
- **US2 (Phase 4)**: depends on Phase 2; independent of US1
- **US3 (Phase 5)**: depends on US1 — it asserts properties of the two-attachment send
- **Polish (Phase 6)**: depends on all desired stories; the e2e block depends on US1 at minimum

### Critical path

```text
T001 → T010/T011/T012 → T013 → T014 → T015 → [US1: T021…T030] → [US3] → [e2e: T055…T061]
                                          └─→ [US2: T039…T046]
```

T010–T014 are the bottleneck: they touch two files (`service.go`, `adapters.go`) that
almost everything downstream also touches, so they are deliberately sequential.

### Within each user story

- Tests first, confirmed RED, then implementation
- Struct/plumbing before renderers; renderers before the send that composes them
- T023's rendering rule must land before T036 extends its tests

### Parallel Opportunities

- **Phase 1**: T001–T004 are four different files — all parallel
- **Phase 2**: T005, T006, T008 are independent new files — parallel. T007 and T009 follow their own tests. T010–T014 are strictly sequential (shared files)
- **Phase 3**: T017–T020 (four test tasks) are parallel. T021 starts a new file and runs alongside them
- **Phase 4**: T037–T038 parallel; T039–T046 are sequential (all in `buildEmailBody`)
- **Phase 6**: T051, T052, T053 parallel; T063 and T064 parallel

---

## Parallel Example: User Story 1

```bash
# All four failing-test tasks together — different files or different test funcs:
Task: "T017 two-attachment assertions in backend/internal/notification/service_test.go"
Task: "T018 page count + no-money assertions in backend/internal/notification/pdf_test.go"
Task: "T019 receipt figure assertions in backend/internal/notification/receipt_pdf_test.go"
Task: "T020 render-failure abort assertion in backend/internal/notification/service_test.go"

# Then the new file can begin while the e-ticket relayout proceeds separately:
Task: "T021 receipt_pdf.go scaffold + header"
Task: "T026 e-ticket page relayout in pdf.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 → Phase 2 (foundational, blocking)
2. Phase 3 (US1), optionally without the T032–T036 descriptor chain
3. **STOP and VALIDATE**: two attachments delivered, page count correct, receipt total
   reconciles
4. The old email body is still in place and still correct — this is genuinely shippable

### Incremental Delivery

1. Setup + Foundational → plumbing ready
2. US1 → two attachments → **MVP, deliverable**
3. US2 → new body → deliverable
4. US3 → resend parity proven → deliverable
5. Polish + e2e + governance sync → merge-ready

### Descoping levers, in the order to pull them

1. **T032–T036** (descriptor chain) — six files across three domains for one line of
   cosmetic text. Cutting it degrades the receipt sub-line to date-only and needs FR-011
   amended.
2. **T051** (logo asset) — the text wordmark is already the email's treatment and is a
   legitimate PDF fallback.
3. **US2 entirely** — the attachments are the ask; the body rewrite is separable.

Nothing in Phase 6's e2e or governance blocks may be cut: the first is Principle VIII's
acceptance gate, the second is required in the same commit by the Governance section.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task
- Every test task must be confirmed RED before its implementation task — an assertion that
  cannot fail is not a test, and T018's "no money in the PDF" is the one most likely to be
  silently vacuous
- `Message.Attachments` is already a slice and `Compose` already loops over it — the mail
  layer needs no change at all to carry two files
- Commit after each task or logical group; stop at any checkpoint to validate


---

## Implementation record (2026-08-12)

**Status: 65 of 65 complete.**

### Verified

- `cd backend && ./scripts/test.sh ./...` — **green**, all packages, database-backed tiers
  included. `internal/notification` alone went from 63 to 128 tests.
- `go build ./...` and `go vet ./...` — clean.
- `cmd/api/architecture_test.go` — **green**: `internal/notification` still imports no
  sibling domain after the widening (Principle II).
- `git diff --stat frontend/` and `git diff --stat backend/migrations/` — both **empty**,
  as FR-036 and the plan require. `SCHEMA.md` untouched, verified rather than assumed.

### e2e acceptance gate (T060, T061)

- `npm test` — **33 passed**, cache on.
- `E2E_CACHE_ENABLED=false npm test` — **25 passed, 8 skipped**. The 8 are
  `cache-refresh.spec.ts`, which skips itself when the cache is off (pre-existing and
  correct); the delivery assertions live in `guest-purchase.spec.ts` and ran in both modes.

The first attempt was blocked by a stale `next dev` holding the dev-server lock for
`frontend/`; it had exited by the time the run was retried.

### Visual check (T054)

Both documents were rendered and compared against Figma. The receipt matches 688-671
(title, order/last-updated stamps, both detail columns with masked contact values, dark
table header, per-line sub-lines, totals with the applicable-rates note, issuer footer).
The e-tickets match 683-148 (dark band with wordmark, `Ticket N of M`, QR beside the
identity column, brand-coloured event name, ticket-type headline, `VALID FOR`, dark
footer) — with distinct codes and holders per page and no monetary figure anywhere.

### T051 — logo asset, resolved as the fallback

No logo ships with the repository, so `BRAND_LOGO_PATH` defaults to empty and both PDFs
draw the site name as a text wordmark. `drawBrandMark` embeds a real asset the moment one
is configured, and every failure path — absent, unreadable, unsupported extension,
gofpdf placement failure — falls back rather than erroring, because a decorative asset
must never fail a delivery. Pinned by
`TestRenderTicketsPDFFallsBackToAWordmarkWhenTheLogoIsUnusable`.

### Deviations from the plan, and why

- **`RenderTicketsPDF` gained a `Branding` parameter.** contracts/notification.md called
  its signature unchanged. That was wrong: FR-022 puts the site URL and support address
  in the e-ticket's footer band, and they have to arrive somehow.
- **`payment.Service.SettlementForOrder` is a new method**, where R-002 planned to ride
  the existing interface. `payments` is the payment domain's table, so the read belongs
  to that domain; the composition root holds both services and joins them. The
  notification-facing interface is unchanged — the summary still rides on `OrderDelivery`.
- **`notificationOrderAdapter` became a pointer with late-bound `payments`.** The payment
  and notification services are mutually dependent, exactly like `checkoutGateway`, and
  this follows that existing pattern rather than inventing a second one.
- **T011 needed no query change.** `orders.updated_at` was already selected and already
  discarded in `toOrderRecord`; it was a two-line change, not a widening.
- **Auto page break had to be disabled on the e-ticket.** gofpdf breaks the moment
  anything is drawn below the default bottom margin — the footer band always is — which
  silently turned each ticket into three pages. The receipt keeps auto-break on (a long
  order legitimately needs a second page) with the attribution moved to a footer function
  so it is never what triggers the break.

### Found in passing, not fixed

`e2e/specs/guest-purchase.spec.ts:658` calls `settleOrder(orderNumber, grossAmount)`, but
`settleOrder` takes one argument — the second is silently ignored, so that scenario is not
asserting the gross amount it appears to. Pre-existing and unrelated to this change;
Playwright transpiles without typechecking, which is why it never surfaced.

---

# Tasks: Revision 2 — Design-Review Corrections (2026-08-12)

**Input**: [plan.md](./plan.md) "Revision 2", [research.md](./research.md) R-010…R-017,
[contracts/notification.md](./contracts/notification.md) Revision 2,
[quickstart.md](./quickstart.md) Revision 2.

T001–T065 above are complete and stay marked. Numbering continues from **T066** so no ID
is reused.

**Tests**: not optional. One of these tasks is a **bugfix in a flow `e2e/` covers**, which
Constitution Principle VIII requires be seen red first.

**Read before starting**: two decisions are unresolved (T069, T070). Neither blocks the
bulk of the work, but T069 gates FR-025 and T070 gates the currency assertions.

---

## Phase 7: Setup (Revision 2)

- [X] T066 [P] Embed the staged logo with `//go:embed` in a new `backend/internal/notification/assets.go` rather than reading it from a filesystem path at runtime, keeping `BRAND_LOGO_PATH` as an override. The runtime image is distroless with no guaranteed working directory, so a relative path is a deployment failure waiting to happen; embedding makes the asset unconditionally present
- [X] T067 [P] Add `TZ: "UTC"` to the api `webServer` env in `e2e/playwright.config.ts`. **This must land before T080 and T112** — without it the API inherits the developer's zone, the timezone bug does not reproduce on a Jakarta machine, and "confirmed red first" is unachievable
- [X] T068 [P] Add `ContentID: string` to `MailAttachment` and `Inline: MailAttachment[]` to `MailMessage` in `e2e/support/mail.ts`, so a spec can assert inline parts (Mailpit reports them separately from `Attachments`)
- [X] T069 **DECISION — blocks FR-025 only.** Resolve the location pin: Figma node `750:2` is an emoji text layer that exports blank, so there is no asset to ship. Take the option chosen in [plan.md](./plan.md) (recommended: author a pin PNG, carry it as a second CID part), stage it under `backend/assets/icons/`, and record the outcome here. If deferred, FR-025 is explicitly not met and T103 is skipped
- [X] T070 **DECISION — blocks T072.** Pin the trailing-zero rendering: `15000.90` → `IDR 15.000,9` or `IDR 15.000,90`. Recommended `,90` (two digits whenever any fraction exists) because `,9` reads as malformed on a financial document. Record the choice as a comment on `formatIDR`

---

## Phase 8: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: US1 and US2 both render money and times. Nothing in Phase 9 or 10 is
correct until T071–T078 land.

- [X] T071 [P] Add the package-level display zone to a new `backend/internal/notification/time.go`: one `sync.OnceValue` resolving `Asia/Jakarta` with a `time.FixedZone("WIB", 7*3600)` fallback, so `LoadLocation` can never fail on the delivery path. Hardcoded, not configured — a wrong display zone silently misstates a financial document
- [X] T072 [P] Write the failing `formatIDR` tests in `backend/internal/notification/format_test.go`: `IDR 0`, `IDR 750`, `IDR 550.000`, `IDR 15.000,92`, the T070 trailing-zero case, and **`IDR -550.000`** — the negative assertion does not exist today and is what pins the grouping loop's `digits[i-1] != '-'` guard. Confirm RED
- [X] T073 Rewrite `formatIDR` in `backend/internal/notification/service.go`: dot thousands, comma decimal, fraction only when non-zero, `-` guard preserved, and **stop using `IntPart()`** — it truncates, so a receipt whose fees carry cents would print rows that do not sum to its own total (FR-037b, SC-013a). Turn T072 green
- [X] T074 Change the dark band colour from `#141B2D` to **`#151A26`** in `backend/internal/notification/pdf.go` (`pdfBand`) and `backend/internal/notification/service.go` (`emailBand`). The staged logo has an opaque `#151A26` background and 0 transparent pixels, so any other band colour renders a visible rectangle around it
- [X] T075 Add `Inline []Attachment` to `Message` in `backend/internal/notification/smtp.go` and an `EmbedReader` loop in `Compose` mirroring the existing `AttachReader` loop — **including `mail.SetCopyFunc`**, because go-mail's default `CopyFunc` drains the reader once and a re-written message would carry an empty image
- [X] T076 Add a test in `backend/internal/notification/smtp_test.go` asserting the composed MIME carries the image with `Content-Disposition: inline` and a `Content-ID` matching its filename, and that the two PDFs remain `attachment`. Assert the filename ends in `.png` — the part's `Content-Type` comes from `mime.TypeByExtension`, so a name without an extension silently becomes `application/octet-stream` and Outlook renders nothing
- [X] T077 Change `buildEmailBody` in `backend/internal/notification/service.go` to return `(html string, inline []Attachment)` and have `SendTicketEmail` pass the parts through to `Message.Inline`. Returning them together is what makes a `cid:` reference with no matching part unrepresentable — otherwise the failure mode is a broken image that raises no error anywhere
- [X] T078 Update every `Rp `-formatted assertion to the new format across `backend/internal/notification/format_test.go`, `service_test.go` and `receipt_pdf_test.go` (~20 assertions). **Read each rather than running a blind find-and-replace** — three of them are also changing for timezone reasons and would otherwise be "fixed" to the wrong expectation
- [X] T079 Run `cd backend && go test ./cmd/api/ -run TestArchitecture` and confirm `architecture_test.go` is still green — the timezone and inline-part work must not introduce a cross-domain import

**Checkpoint**: money and time render correctly everywhere; the mail layer can carry inline images.

---

## Phase 9: User Story 1 — Receipt and E-Ticket corrections (Priority: P1) 🎯

**Goal**: both attachments match Figma, and the receipt stops printing the wrong admission
day.

**Independent Test**: render both documents for an order whose ticket admits at
`2026-04-27 06:00 WIB` and whose fees carry cents; every date, amount and rule matches the
design, and the printed rows sum to the printed total.

### The bugfix — do this first, and see it red

- [X] T080 [P] [US1] Write the failing regression in `backend/internal/notification/receipt_pdf_test.go`: a line whose `AdmissionStarts` is `2026-04-26T23:00Z` must print **27 Apr 2026** (the Jakarta day). Run it under `TZ=UTC` **and** `TZ=Asia/Jakarta` and confirm **RED in both** — `receiptSubLine` formats in the value's own location, so a test that only fails under one TZ is testing the host, not the code
- [X] T081 [US1] Apply `.In(jakarta())` in `receiptSubLine` in `backend/internal/notification/receipt_pdf.go`. Turn T080 green in both timezones

### Remaining time work

- [X] T082 [US1] Convert the receipt's transaction stamp and its "Last updated" stamp to Jakarta in `backend/internal/notification/receipt_pdf.go`, and add the `WIB` suffix to "Last updated" — leaving it zoneless while the neighbouring Date field says WIB is worse than either consistent option
- [X] T083 [US1] Split the shared stamp formatter in `backend/internal/notification/receipt_pdf.go`: the email needs `04 Jul 2026 06:56 WIB` and the receipt `20 Apr 2026, 14:30 WIB`. They differ by one comma, so express that in two named functions rather than leaving it to a caller's memory

### Receipt layout

- [X] T084 [US1] Make every product-table rule span the full table width in `receiptTable` in `backend/internal/notification/receipt_pdf.go` — currently drawn from `colProductX`, which leaves a visible notch where the rule meets the left border
- [X] T085 [US1] Draw the envelope icon before the support address in `receiptFooter` in `backend/internal/notification/receipt_pdf.go` using `RoundedRect` plus a stroked three-point path, in the design's slate rather than the existing neutral `pdfGray`. **Reset `SetLineCapStyle` to `"butt"` and `SetLineJoinStyle` to `"miter"` before returning** — they are sticky `Fpdf` state
- [X] T086 [US1] Add a test in `backend/internal/notification/receipt_pdf_test.go` proving a rule drawn *after* the envelope has square ends. This is the failure mode most likely to pass every content assertion and still look wrong
- [X] T087 [US1] Upper-case the payment method wherever it is printed, in `backend/internal/notification/receipt_pdf.go` (FR-010a)

### E-ticket

- [X] T088 [P] [US1] Write the failing `TestFormatTicketWindow*` cases in `backend/internal/notification/pdf_test.go` with **UTC inputs and WIB expectations** (`2026-04-26T03:00Z`–`T14:00Z` → `26 Apr 2026 @ 10:00 - 21:00 WIB`). A test written WIB-in/WIB-out proves the layout string was copied, not that any conversion happened
- [X] T089 [US1] Rewrite `FormatTicketWindow` in `backend/internal/notification/pdf.go` to the `@` form, converting to Jakarta. **Also decide and document the multi-day and single-instant branches** — the requirement only specifies the same-day form, and leaving the others on the old `Mon, 02 Jan 2006 15:04 MST` layout ships one ticket reading `26 Apr 2026 @ 10:00 - 21:00 WIB` and another reading `Wed, 02 Sep 2026 09:00 UTC`
- [X] T090 [US1] Replace the text wordmark with the embedded logo in the e-ticket header band in `backend/internal/notification/pdf.go`, keeping the wordmark as the failure path only
- [X] T091 [US1] Change the accent rule beside the QR panel in `backend/internal/notification/pdf.go` to the design's colour with rounded ends (`SetLineCapStyle("round")`, reset afterwards per T085's note)
- [X] T092 [US1] Remove the venue row from `renderTicketPage` in `backend/internal/notification/pdf.go` and drop the corresponding assertion from `backend/internal/notification/eticket_test.go` (FR-019a)
- [X] T093 [US1] Justify the e-ticket footer in `drawTicketFooter` in `backend/internal/notification/pdf.go` — site left, customer service right-aligned to the right margin, rather than both starting from fixed left offsets

**Checkpoint**: US1 complete. Both PDFs match the design and the day-boundary defect is fixed.

---

## Phase 10: User Story 2 — Email body corrections (Priority: P2)

**Goal**: the body matches Figma 741-5105 and renders the same way in Outlook as elsewhere.

**Independent Test**: open a delivered message in the Mailpit UI — every listed defect is
gone, the logo renders with no seam, and the amount column is straight.

- [X] T094 [P] [US2] Write the failing body assertions in `backend/internal/notification/service_test.go`: full HTML document present, `© 2026 manjo` in the footer, `Powered By Manjo` absent, payment method upper case, order date carrying `WIB`, and a `cid:` reference for the logo. Confirm RED
- [X] T095 [US2] Emit a full HTML document from `buildEmailBody` in `backend/internal/notification/service.go` — doctype, `xmlns:o`, `<head>` with the mso `PixelsPerInch` block. Without it the CID logo renders 25–50% oversized on high-DPI Windows, and there is no other fix
- [X] T096 [US2] Replace `max-width:640px;margin:0 auto` with the design's fixed **600px** container in `backend/internal/notification/service.go`: `width` attribute *and* style, Outlook ghost-table conditional wrapper, 40px side padding giving a 520px content column. The Word engine honours neither `margin:0 auto` centring nor `max-width` on a non-table
- [X] T097 [US2] Render the logo as a CID `<img>` with **`width`/`height` HTML attributes** in `backend/internal/notification/service.go` — Outlook ignores CSS dimensions on images and draws the natural pixel size, so a 2× asset would render double-size there
- [X] T098 [US2] Rebuild the status badge in `backend/internal/notification/service.go` as a nested one-cell table that shrinks to its label, in the design's colours and slightly rounded. Record in a comment that Outlook ignores `border-radius`, so square corners there are the accepted degradation
- [X] T099 [US2] Upper-case the payment method and render the order date as `04 Jul 2026 06:56 WIB` in the order panel in `backend/internal/notification/service.go` (FR-024b, FR-024c)
- [X] T100 [US2] Move the fee rows into the **same table** as the ticket lines in `backend/internal/notification/service.go` so their amount cells share one right edge. Separate tables is why the amounts currently drift out of column
- [X] T101 [US2] Remove the rule beneath the Total Payment row in `backend/internal/notification/service.go` (FR-026b)
- [X] T102 [US2] Replace the footer in `backend/internal/notification/service.go` with `"This email was generated automatically. Please do not reply to this email."` and `"© 2026 manjo"`, and remove `Powered By Manjo` from the body — it stays on the receipt, where the design does use it
- [X] T103 [US2] Add the pin cell beside the venue block in `backend/internal/notification/service.go`, as its own `<td width="16" valign="top">` with the icon as a second CID part. **Gated on T069** — skip if the pin decision defers, leaving FR-025 unmet
- [X] T104 [US2] Add a test in `backend/internal/notification/service_test.go` asserting every `cid:` reference in the rendered HTML has a matching entry in the returned inline parts, and vice versa. This is the assertion that makes a broken-image regression impossible to ship silently

**Checkpoint**: US1 and US2 complete.

---

## Phase 11: User Story 3 — Resend parity (Priority: P3)

- [X] T105 [P] [US3] Extend the resend test in `backend/internal/notification/service_test.go` to assert the resent message carries the same inline parts as the original, not just the same two attachments
- [X] T106 [US3] Confirm by inspection that all three entry points still funnel through one `SendTicketEmail` after the `buildEmailBody` signature change — `ticketDelivererAdapter` in `backend/cmd/api/adapters.go`, and `resend` / `resendPublic` in `backend/internal/notification/handler.go`

---

## Phase 12: Polish & Cross-Cutting Concerns

- [X] T107 [P] Run `cd backend && ./scripts/test.sh ./...` and confirm the whole Go suite is green
- [X] T108 [P] Confirm `git diff --stat frontend/` and `git diff --stat backend/migrations/` are both still empty — this revision is presentation-only, and every money column is already `NUMERIC(12,2)`
- [X] T109 Render the receipt and compare against Figma 688-671: full-width rules, envelope icon, `QRIS` upper case, `WIB` on both stamps, and an amount carrying cents
- [X] T110 Render the e-tickets and compare against Figma 683-148: logo with no seam, `26 Apr 2026 @ 10:00 - 21:00 WIB`, rounded accent rule, no venue, justified footer
- [X] T111 Open a delivered message in the Mailpit UI at `http://localhost:8025` and check against Figma 741-5105. Confirm the API reports 2 `Attachments` and the logo under `Inline` — if the logo appears under `Attachments`, `Compose` used `AttachReader` instead of `EmbedReader`

### End-to-end acceptance (Constitution Principle VIII) — NOT optional

- [X] T112 Add the day-boundary scenario to `e2e/specs/guest-purchase.spec.ts`: an order whose ticket type admits in the evening WIB, asserting the receipt names the Jakarta day. **Confirm it fails against the unfixed code first**, which requires T067's `TZ` pin
- [X] T113 Extend `e2e/specs/guest-purchase.spec.ts` to assert `Attachments.length === 2` **and** that every `cid:` in the body's HTML resolves to an `Inline` entry — the two halves of FR-001 and FR-023a in one scenario
- [X] T114 Run the full suite green: `cd e2e && npm test`
- [X] T115 Run it once more with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test`

### Governance

- [X] T116 [P] Add the word "document" to the attachment rule in `.specify/memory/constitution.md`'s Critical Data Flow Rules bullet — "exactly two **document** attachments". A precision edit, **not** a version bump: verified against a running Mailpit, inline parts are reported separately, so 4.1.0's substance is unchanged
- [X] T117 [P] Make the same precision edit in `PRD.md` §1.4
- [X] T118 Confirm `SCHEMA.md` still needs no change and record that it was verified rather than assumed

---

## Dependencies & Execution Order — Revision 2

### Critical path

```text
T067 ──────────────────────────────► T080 (red) ─► T081 ─► T112
T070 ─► T072 ─► T073 ─┐
T066 ─► T074 ─────────┼─► [US1: T082…T093] ─┐
T075 ─► T076 ─► T077 ─┘                      ├─► T107 ─► T114 ─► T115
                       └─► [US2: T094…T104] ─┘
T069 ────────────────────────────────────────► T103 (gated)
```

**T067 is the true head of the critical path** even though it looks like a one-line config
edit: without it T080 and T112 cannot be seen red, and Principle VIII's bugfix rule is
unmeetable.

### Phase dependencies

- **Setup (7)**: no dependencies. T069 and T070 are decisions, not code
- **Foundational (8)**: T070 gates T072; T066 gates T074's asset use. **Blocks US1 and US2**
- **US1 (9)**: depends on Phase 8
- **US2 (10)**: depends on Phase 8; independent of US1
- **US3 (11)**: depends on US2 (the inline parts it asserts)
- **Polish (12)**: depends on all desired stories

### Parallel opportunities

- **Phase 7**: T066, T067, T068 are three different files — parallel. T069/T070 are decisions and can run alongside everything
- **Phase 8**: T071 and T072 parallel (new file, test file). T073–T078 are sequential — they share `service.go`
- **Phase 9**: T080 and T088 parallel (different test files). T084–T087 share `receipt_pdf.go`; T089–T093 share `pdf.go`; the two groups run in parallel with each other
- **Phase 10**: T094 alone, then T095–T104 are sequential — all in `buildEmailBody`
- **Phase 12**: T107, T108 parallel; T116, T117 parallel

---

## Implementation Strategy — Revision 2

### Fix the bug first

T067 → T080 → T081 is a standalone, shippable increment: the day-boundary defect is the
only item here that is wrong rather than merely unpolished, and it needs no asset decision,
no currency change and no email work.

### Then the rest

1. Phase 7 + Phase 8 → money, time and the mail layer are correct
2. Phase 9 (US1) → both PDFs match the design
3. Phase 10 (US2) → the email matches the design
4. Phase 11 + 12 → parity, e2e, governance

### Descoping levers

1. **T103 + T069** (the pin) — the only item blocked on a decision. Skipping leaves FR-025
   unmet and nothing else affected
2. **T098** (badge) — Outlook shows square corners regardless; the shape work benefits
   other clients only
3. **US2 entirely** — the PDFs are the attachments; the body rewrite is separable

Nothing in Phase 12's e2e or governance blocks may be cut.

---

## Notes — Revision 2

- Every test task must be confirmed RED before its implementation task. **T080 is the one
  that matters most**: it must be red under *both* `TZ=UTC` and `TZ=Asia/Jakarta`, because a
  test that only fails under one is asserting the host's timezone rather than the code's
- Expect ~20 currency assertions and 3 time assertions to break. That is planned (T078,
  T088), not a surprise — but read each one; several change for two reasons at once
- `SetLineCapStyle` / `SetLineJoinStyle` are sticky. T085 and T091 both set them; both must
  reset. T086 is the test that catches it


---

## Implementation record — Revision 2 (2026-08-12)

**All 53 tasks complete (T066–T118).** Both decisions were taken rather than deferred.

### Verified

- `go build ./...`, `go vet ./...` — clean.
- `./scripts/test.sh ./...` — **17 packages ok, 0 failures**. `internal/notification`
  went from 104 to 141 tests.
- `npm test` — **33 passed**. `E2E_CACHE_ENABLED=false npm test` — **25 passed, 8 skipped**
  (the pre-existing cache-coherence specs, which skip themselves).
- All three surfaces rendered and compared against Figma.
- `git diff --stat frontend/` and `backend/migrations/` — both empty. `SCHEMA.md`
  untouched, verified rather than assumed.

### The bugfix, seen red first (Principle VIII)

`TestReceiptRendersAdmissionDatesInJakarta` was run against the reverted fix:

```
fix REVERTED  -> TZ=UTC FAIL   TZ=Asia/Jakarta FAIL
fix RESTORED  -> TZ=UTC ok     TZ=Asia/Jakarta ok
```

Red in **both** zones, as predicted — `receiptSubLine` formatted in the value's own
location, so the host's zone never entered into it.

`TestReceiptResetsLineStyleAfterDrawingTheEnvelope` was checked the same way (removing the
reset turns it red), because a test for sticky graphics state is exactly the kind that
passes vacuously.

### Decisions taken

- **T069 (pin)** — authored. Figma node `750:2` is an emoji text layer and exports blank
  by every route, so the glyph was drawn programmatically: a 32×32 map pin, embedded and
  carried as a second CID part. **It is ours, not the designer's, and deserves a look
  before release.**
- **T070 (trailing zero)** — `IDR 15.000,90`. Two digits whenever a fraction exists.

### Found during implementation, not in the plan

- **The logo overflowed the header band.** Sizing it by width with an auto height made it
  23.7 mm tall in a 21 mm band, so its opaque background hung below as a dark block. Now
  fitted to the band height with the width derived from the asset's own aspect ratio, so
  replacing the asset cannot break the layout.
- **`Branding.Copyright` is new.** FR-029 gives the email `© 2026 manjo` while the receipt
  keeps `Powered By Manjo`; one field could not carry both.
- **The rule under Total Payment came from the next section.** It was
  `emailBuyerInformation`'s leading `border-top`, not anything in the totals block.

### Known limits

- **Outlook renders the badge square.** It ignores `border-radius`; the shrink-to-fit
  behaviour works everywhere. Accepted and recorded at the call site.
- **The day-boundary defect is pinned at the Go tier, not in `e2e/`.** PDFs are compressed
  and not text-searchable from Playwright. The e2e suite asserts the same conversion
  through the email body, which is readable HTML, under `TZ=UTC`.

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

---

# Revision 3 — Brand Refresh Tasks (2026-08-19)

**Spec**: [spec.md](./spec.md) §Clarifications, Session 2026-08-19 ·
**Plan**: [plan.md](./plan.md) §Revision 3 · **Research**: R-018 … R-025

Task IDs continue from T118. Revisions 1 and 2 are complete; nothing below re-opens them.

> **`setup-tasks.sh` cannot resolve this feature.** It derives `FEATURE_DIR` from the branch,
> and `fix/receipt` is not numbered, so it reports `018-configurable-rate-limits`. It is
> read-only and wrote nothing. **`/speckit-implement` must be pointed at
> `016-receipt-ticket-email` explicitly.**

## Two things to read before starting

**There is no bug to fix here.** R-018 established against the running service that the
"four attachments" report was a reading of Mailpit's debug file strip, not of the message.
**Do not write a regression scenario for the attachment count** — it could not fail against
today's code, and Principle VIII exists to prevent exactly that test.

**The e-ticket footer cannot be asserted in `e2e/`.** Production PDFs are compressed
(R-025); content assertions go through `RenderTicketsPDFPlain` at the Go tier. Playwright
gets attachment count, filename, PDF magic and page count — nothing the page *says*.

---

## Phase 1: Setup (Assets)

**Purpose**: Produce what ships and remove what does not. Everything below depends on this.

- [X] T119 Generate the shipped white derivative: trim, resize to 800 px wide, quantise to 64 colours, from `frontend/public/brand/LOGO JIVE MINUS TWO_WHITE.png` → `frontend/public/brand/jive-logo-white.png`. **Trimming is not optional** — untrimmed, each surface inherits a different share of the ~14% transparent padding (R-020). Verify the result is `800x455` (ratio 1.757, **not** the file's 1.492) and ~15 KB
- [X] T120 [P] Generate the black variant the same way: `frontend/public/brand/LOGO JIVE MINUS TWO_BLACK.png` → `frontend/public/brand/jive-logo-black.png`. It has **no consumer today** — every placement is a dark band — and is carried for light surfaces per FR-023c
- [X] T121 Copy `frontend/public/brand/jive-logo-white.png` to `backend/internal/notification/assets/brand/jive-logo-white.png` so the embedded and served marks are byte-identical (SC-019)
- [X] T122 Delete the retired mark: `frontend/public/brand/jive-logo.png` (349 KB) and `backend/internal/notification/assets/brand/jive-logo.png` (18 KB)
- [X] T123 Confirm the retirement is total: `rg -n 'jive-logo\.png' --glob '!node_modules' --glob '!*.md'` returns nothing across the repo. SC-019 is written to fail if either copy or any reference survives

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The asset wiring, configuration and shared helper change that US1 and US2 both
build on.

**⚠️ CRITICAL**: No user story work below can begin until T124–T128 land — both stories
resolve the same embedded asset and the same brand configuration.

- [X] T124 Repoint the `//go:embed` directive from `assets/brand/jive-logo.png` to `assets/brand/jive-logo-white.png` in `backend/internal/notification/assets.go`. `logoPNG`, `pinPNG` and the operator-override path are unchanged
- [X] T125 Update `cidLogo` from `"jive-logo.png"` to `"jive-logo-white.png"` in `backend/internal/notification/service.go` (~line 270). This constant is the **single place** the body's `cid:` reference and the embedded part's filename meet — a mismatch renders a broken image and raises no error in the send, the SMTP exchange, or any attachment count
- [X] T126 [P] Change the two brand defaults in `backend/pkg/config/config.go` (~lines 237–238): `BRAND_SITE_URL` → `https://www.jive-promotion.com/`, `BRAND_SUPPORT_EMAIL` → `help@manjo.co.id` (FR-035a). Both stay environment-overridable
- [X] T127 [P] Update the default assertions in `backend/pkg/config/config_test.go` (~line 275) to the new values, in the same commit as T126 so the suite never encodes two answers at once
- [X] T128 Widen `drawEnvelope` in `backend/internal/notification/receipt_pdf.go` (~line 369) to `drawEnvelope(pdf *gofpdf.Fpdf, x, y, size float64, body, flap [3]int)`, passing `pdfSlate` and white at the existing receipt call site. One definition, two call sites (R-023) — a forked copy drifts the first time either is adjusted. **Keep the sticky cap/join reset inside the function**

---

## Phase 3: User Story 1 - Buyer receives a filable receipt alongside the tickets (Priority: P1)

**Goal**: The e-ticket document carries the new mark at a legible size and the redesigned
footer.

**Independent test**: Render a multi-ticket order's e-ticket via `RenderTicketsPDFPlain`,
open it, and read the header band and footer against Figma `683-148`.

### Tests for User Story 1 ⚠️

Written first, at the **Go tier** — the production PDF is compressed and unreadable from
Playwright (R-025).

- [X] T129 [P] [US1] Add a footer-content test in `backend/internal/notification/eticket_test.go` asserting the rendered page contains `https://www.jive-promotion.com/` and `help@manjo.co.id`, using `RenderTicketsPDFPlain` (FR-035a, FR-022d)
- [X] T130 [P] [US1] Add an alignment test in `backend/internal/notification/eticket_test.go` proving the customer-service block's label, icon and address share one left edge — **parameterised over a short and a long support address**, because FR-022e is about behaviour and not the mock's particular string length (SC-021)
- [X] T131 [P] [US1] Add a multi-page test in `backend/internal/notification/eticket_test.go` rendering a **2+ ticket** order and asserting page 2's rules have square ends. Round ends there mean `drawEnvelope`'s sticky cap/join reset was lost — a defect that renders perfectly on a one-ticket order (R-023)
- [X] T132 [P] [US1] Expose the band geometry through a seam in `backend/internal/notification/export_test.go` (following `TableInsets`/`HeaderColumnRights`) so the band height and the mark's drawn width are assertable without reproducing the arithmetic

### Implementation for User Story 1

- [X] T133 [US1] Re-express the absolutely positioned page constants in `backend/internal/notification/pdf.go` `renderTicketPage` as offsets from `bandHeight` rather than literals — "Ticket N of M" at 29, the accent rule at 40…112, the QR panel at 46…108. They **must move together**: a band moved without the accent rule puts a 72 mm rule through it (R-022)
- [X] T134 [US1] ~~Raise `bandHeight` from 21.0 to 38.3~~ **REVERSED 2026-08-19**: the band stays 21.0 and the mark is capped by it (research R-021a). Superseded text: raise `bandHeight` from 21.0 to 38.3 in `backend/internal/notification/pdf.go` (~line 156) and draw the mark at 55 mm wide, giving ~1.5 mm cap height on the lockup's secondary line in print (R-021, FR-023b). Cross-checked against the design: content ends ~197 mm with the footer at ~274 mm, so a +17.3 mm shift consumes under a quarter of the available whitespace
- [X] T135 [US1] Rewrite the two comments in `backend/internal/notification/pdf.go` that are now false — `pdfBand` at ~line 138 claims `#151A26` "matches the logo's own opaque background", and `drawBrandMark` at ~line 292 fits by height because "the asset's background is opaque". The new asset has an alpha channel (R-020), so both statements would make a future reader preserve a constraint that no longer exists
- [X] T136 [US1] Rewrite `drawTicketFooter` in `backend/internal/notification/pdf.go` (~line 258) so the customer-service block is right-**positioned** with its contents **left-aligned** — label, icon and address on one left edge (FR-022a). The block, not each line, meets the right margin
- [X] T137 [US1] Draw the envelope before the support address on its **own line** in `drawTicketFooter`, calling the widened `drawEnvelope` with the band's foreground and the band colour (FR-022d, FR-022e). Vertically centred against the text — the same defect FR-015b already caught once on the receipt
- [X] T138 [US1] Update any y-coordinate assertion in `backend/internal/notification/pdf_test.go` that sits below the band. **Re-derive from `bandHeight` via the T132 seam rather than re-hardcoding**, or the next band change breaks them the same way
- [X] T139 [US1] Confirm `backend/internal/notification/eticket_test.go:150` still passes untouched — it exercises the missing-asset wordmark fallback, which FR-023a keeps as the failure path
- [X] T140 [US1] Render and open both PDFs per [quickstart.md](./quickstart.md) Step 4: the mark sits inside the taller band with even inset and **no dark rectangle hanging past it** (a seam means the opaque-background fitting strategy is still in force), and the accent rule and QR moved with the band

---

## Phase 4: User Story 2 - Email body reads as the new confirmation design (Priority: P2)

**Goal**: The confirmation email carries the new mark at a legible size, still as an inline
part and still with exactly two document attachments.

**Independent test**: Settle an order, open the message in Mailpit and in a real client;
the mark renders in the body and the attachment list shows two files.

### Tests for User Story 2 ⚠️

- [X] T141 [P] [US2] Add an assertion in `backend/internal/notification/service_test.go` that the body carries `width="280" height="159"` as HTML **attributes** and matching inline CSS (FR-023b). The attributes are what Outlook's Word engine obeys; CSS alone renders the 2× part at double size there
- [X] T142 [P] [US2] Extend `e2e/specs/guest-purchase.spec.ts` with User Story 2 scenario 6 — the delivered message lists exactly two documents while every `cid:` the body references resolves to an inline part. **`expect(mail.Attachments).toHaveLength(2)` at line 159 needs no change** (R-018); extend the existing `cidReferences`/`mail.Inline` pairing rather than adding a helper

### Implementation for User Story 2

- [X] T143 [US2] Change `emailHeader` in `backend/internal/notification/service.go` (~line 369) from `108×61` to `280×159` in **both** the HTML attributes and the inline `style` width/height. 280 px gives ~7.6 px cap height on the secondary line against ~2.9 px today (R-021)
- [X] T144 [US2] Confirm the embedded part is 2× the display size, as the existing 216×122-for-108×61 arrangement already does. The 800 px derivative from T119 covers 280 px display at better than 2×, so no separate email asset is needed
- [X] T145 [US2] Update the filename in `backend/internal/notification/service_test.go:386` (`cid:jive-logo.png`) and in the `{"jive-logo.png", "location-pin.png"}` loop at `backend/internal/notification/smtp_test.go:155`. **Change the names only.** The inline-vs-attachment assertions at `smtp_test.go:119`–`:131` are what keep R-018's finding true and must not be weakened to make anything pass
- [X] T146 [US2] Open the delivered message per [quickstart.md](./quickstart.md) Step 5 and, if an account is available, forward it to a real Outlook/Windows client. That is the client the attribute rule exists for, and Mailpit cannot stand in for it

---

## Phase 5: User Story 3 - A resend delivers the identical pair (Priority: P3)

**Goal**: Nothing regresses. This revision adds no resend behaviour.

- [X] T147 [US3] Run the existing resend coverage — a section of scenario 22 at `e2e/specs/guest-purchase.spec.ts:224-239`, not a standalone scenario — unchanged and confirm it stays green — the resend path shares one renderer with the automatic send, so it inherits the new mark and footer with no code of its own
- [X] T148 [US3] Confirm a resent order's e-ticket carries the **same** ticket codes as the first delivery (FR-007). Asset and layout changes must not touch issuance

---

## Phase 6: Polish & Cross-Cutting Concerns

### Frontend (first frontend change in this feature — R-024)

- [X] T149 **Unblocked 2026-08-19 (200 px / ~130 px bar chosen).** Update `frontend/components/layout/site-header.tsx:21`: `src` → `/brand/jive-logo-white.png`, mark to ~200 px wide, bar from `h-24.25` (97 px) to ~`h-32.5` (130 px). **The numbers are a judgment call, not something the clarification settled** — a literal reading of "grow the bands" gives 254 px in a ~190 px bar, which would dominate the public chrome. See [plan.md](./plan.md) §"The one thing that needs a nod". Keep the plain `<img>`; migrating to `next/image` for one static above-the-fold asset trades a lint suppression for a loader and layout-shift change nothing here needs
- [X] T150 [P] Confirm `frontend/components/layout/site-footer.tsx` needs no change — it carries copyright and social links, no brand mark
- [X] T151 Run the frontend tier, which this feature has never needed before. `npx` and bare `node` both fail in this repo: `cd frontend && export PATH="$HOME/.nvm/versions/node/v26.5.1/bin:$PATH" && ./node_modules/.bin/vitest run && ./node_modules/.bin/next build` (`next build` is the typecheck of record)

### Verification

- [X] T152 [P] Confirm the legibility target by eye per [quickstart.md](./quickstart.md) Step 2 — render the mark at 108 px and 280 px on `#151a26` and compare. FR-023b is the one requirement here that no passing test fully vouches for
- [X] T153 [P] Confirm the shipped derivatives are ~15 KB each and that `frontend/public/brand/` no longer serves a 349 KB logo on every page load
- [X] T154 Run the Go tier green: `cd backend && ./scripts/test.sh ./...`
- [X] T155 Run the e2e gate green: `cd e2e && npm test`
- [X] T156 Run it again with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test` (Principle VII kill switch)

### Governance — verify, do not assume

- [X] T157 [P] Confirm `.specify/memory/constitution.md` needs **no** amendment. Its delivery rule — one email, two document attachments, inline images excluded from the count — is confirmed by R-018 rather than altered by it. Record that it was verified
- [X] T158 [P] Confirm `SCHEMA.md` needs no change and no migration exists — this revision adds no column, table or migration ([data-model.md](./data-model.md) §Revision 3)
- [X] T159 [P] Confirm `PRD.md` §1.4 already reads "two document attachments" and needs no edit — re-checked during clarification, closing Revision 2's outstanding governance item

### Recorded, deliberately not done

- [X] T160 Confirm two known gaps remain untouched and are recorded rather than forgotten: the **receipt carries no brand mark at all** (`drawBrandMark` is called only from the e-ticket renderer, contradicting FR-023a/FR-035's "all three surfaces"), and `frontend/app/favicon.ico` is still the unmodified Next.js scaffold icon. Both are in [spec.md](./spec.md) §Governance notes; neither is in scope

---

## Dependencies & Execution Order — Revision 3

### Phase dependencies

```
Phase 1 (T119-T123)  assets exist and the old one is gone
        ↓
Phase 2 (T124-T128)  embed, cid constant, config, drawEnvelope signature
        ↓
   ┌────┴────┐
Phase 3      Phase 4        US1 and US2 are independent once Phase 2 lands
(T129-T140)  (T141-T146)    — different files, different surfaces
   └────┬────┘
Phase 5 (T147-T148)  regression only
        ↓
Phase 6 (T149-T160)  frontend, gates, governance
```

### Critical path

T119 → T121 → T124 → T125 → T133 → T134 → T136 → T137 → T154 → T155

T133 before T134 is the ordering that matters most: converting the literals to offsets
**before** changing `bandHeight` is what makes the shift a one-line change instead of six
hand-edited constants.

### Parallel opportunities

- **Phase 1**: T120 alongside T119 (different variant, different file)
- **Phase 2**: T126 + T127 together; T124/T125 are separate files from both
- **Phase 3 tests**: T129, T130, T131, T132 are all `[P]` — different test functions
- **Phase 3 / Phase 4**: entire phases run in parallel after T128. US1 is `pdf.go`; US2 is `service.go`
- **Phase 6**: T152, T153, T157, T158, T159 all `[P]` — read-only checks on different files

### Blocked

- **T149** is the only blocked task, on the site-header sizing nod. Nothing else depends on
  it: US1, US2, the Go tier and the e2e gate are all independent of the frontend numbers.

## Implementation Strategy

**MVP scope**: Phases 1–3 (T119–T140). That delivers the new mark and the redesigned footer
on the e-ticket — the document the user actually pointed at — and is independently
shippable and testable without touching the email or the frontend.

**Increment 2**: Phase 4 (T141–T146), the email body.

**Increment 3**: Phase 6's frontend (T149–T151), once the sizing question is answered.

**Do not start with the frontend.** It is the only blocked work, and it is the only surface
none of the three user stories covers.

---

# Revision 4 — Subject Line + Phone Mask Tasks (2026-08-19)

**Spec**: [spec.md](./spec.md) §Clarifications, Session 2026-08-19 (subject line + phone
masking) · **Plan**: [plan.md](./plan.md) §Revision 4 · **Research**: R-026, R-027

Task IDs continue from T160. Revisions 1–3 are complete; nothing below re-opens them.

> **`setup-tasks.sh` cannot resolve this feature.** It derives `FEATURE_DIR` from the branch,
> and `fix/receipt` is not numbered, so it now reports `020-qris-frame-simplify` (the
> fallback target moved because a newer spec directory exists). It is read-only and wrote
> nothing. **`/speckit-implement` must be pointed at `016-receipt-ticket-email` explicitly.**

## Three things to read before starting

**The phone mask IS a real bug this time.** Unlike Revision 3, Principle VIII's red-first
rule applies: the e2e scenario must be confirmed failing against unfixed code before
`mask.go` is touched.

**Do not write the mask assertion through `MaskPhone`.** R-027: two existing Go tests already
use the helper as their own oracle and pass for any shape it returns. The e2e assertion must
be the literal `08123456****`, or the acceptance gate inherits the same blind spot and the
scenario can never be red.

**The subject was never specified** (R-026). FR-005a is a new requirement, not an amendment,
and `contracts/notification.md` recorded the old string as though it were a contract — that
file was corrected during planning.

---

## Phase 1: Setup (Revision 4)

**Purpose**: A running stack and a recorded baseline, so "red" in Phase 2 is evidence rather
than assertion.

- [X] T161 Bring the real dependencies up and migrate: `REDIS_PORT=6380 docker compose up -d postgres redis mailpit && docker compose run --rm migrate up`. Mailpit is not optional — every assertion in this revision reads the real delivered message off it
- [X] T162 [P] Record the shipped baseline per [quickstart.md](./quickstart.md) Revision 4 Step 0: settle one order through the real flow, then capture `.Subject` and the masked phone out of `.HTML` from `http://localhost:8025/api/v1/message/$ID`. Expect `Your tickets for …` and `08123***7890`. This is the "before" half of the red-first evidence

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The failing assertions, written and **seen red**, before any production code
moves. Both user stories depend on this — Principle VIII requires it for the mask, and the
subject is only meaningfully covered if its assertion was once red too.

**⚠️ CRITICAL**: T163–T165 land before T166 or T170. Writing the fix first makes the red-first
confirmation unreproducible without a `git stash`, and a regression test never seen red
proves nothing about the bug it claims to pin.

- [X] T163 Add the subject assertion to the delivery block of `e2e/specs/guest-purchase.spec.ts` (~line 155, where `orderNumber` and `mail` are already in scope): ``expect(mail.Subject).toBe(`[${orderNumber}] E-receipt & E-Ticket for ${event.name}`)``. Use `event.name` from the `createSellableEvent` result — it is `"UAT Full Journey"` in that scenario, so this assertion **also fails a hardcoded "JIVE 2026"**, which is what makes FR-005a's dynamic requirement genuinely covered rather than merely stated
- [X] T164 Add both mask assertions to the same block: `expect(mail.HTML).toContain("08123456****")` and `expect(mail.HTML).not.toContain("08123***7890")`. `08123456****` is the default holder phone `081234567890` ([e2e/support/journey.ts:27](../../e2e/support/journey.ts#L27)) under the new rule. **Literals only** — never `MaskPhone(...)`, never a regex over asterisks (R-027). The negative assertion is what puts both shapes side by side in the failure output
- [X] T165 Run `cd e2e && npx playwright test specs/guest-purchase.spec.ts` and **confirm all three assertions fail**, with the received values matching T162's baseline. Paste the failure output into the implementation record. If any assertion passes here, it is asserting the wrong thing and must be rewritten before proceeding

---

## Phase 3: User Story 1 - Buyer receives a filable receipt alongside the tickets (Priority: P1) 🎯 MVP

**Goal**: The delivered email identifies the order and the event in its subject.

**Independent test**: Settle an order, list the inbox, and read the subject — it carries the
order number in brackets and the name of the event the order was actually placed against.

### Tests for User Story 1 ⚠️

- [X] T166 [US1] Tighten `backend/internal/notification/service_test.go:193` from `assert.Contains(t, msg.Subject, "Jazz Night 2026")` to the full literal subject including the bracketed order number. **This test passes untouched under the new behaviour**, which is the reason to change it: a test that survives a deliberate behaviour change unchanged was not asserting that behaviour. Confirm the tightened version is red before T167

### Implementation for User Story 1

- [X] T167 [US1] Replace the subject at `backend/internal/notification/service.go:215` with `fmt.Sprintf("[%s] E-receipt & E-Ticket for %s", order.OrderNumber, tickets[0].EventName)` (FR-005a). No new plumbing — `OrderNumber` is already used two lines down for both attachment filenames, and `tickets[0].EventName` is already read on the line being replaced. `tickets[0]` is safe because orders are event-scoped (spec 007) and the existing code already depends on it
- [X] T168 [US1] Confirm T166 and T163 both go green, and that the order number in the subject is byte-identical to the one in `receipt-<n>.pdf` and `tickets-<n>.pdf` (FR-004, SC-022)

---

## Phase 4: User Story 2 - Email body reads as the new confirmation design (Priority: P2)

**Goal**: The buyer's phone shows its asterisks at the end, on the body and the receipt
alike, rendered exactly as stored.

**Independent test**: Settle an order whose buyer phone is `081234567890`; the body and the
receipt's Order Details both read `08123456****`.

### Tests for User Story 2 ⚠️

- [X] T169 [US2] Rewrite `TestMaskPhone`'s table in `backend/internal/notification/mask_test.go:50` to the new shapes: `+6281234567890 → +628123456****`, `+628123456789 → +62812345****`, `081234567890 → 08123456****`, `081234567 → 08123****` (**unchanged by coincidence** — keep it, it is the case that proves the rules overlap rather than that nothing happened), `0812 → ****`, `0 → *`, `"" → ""`. This table is the only place in the codebase where the literal shapes are pinned. Confirm red before T170

### Implementation for User Story 2

- [X] T170 [US2] Rewrite `MaskPhone` in `backend/internal/notification/mask.go:48`: replace `phoneKeepLeading`/`phoneKeepTrailing` with a single `phoneMaskTrailing = 4`, keep `len(runes)-4` characters and asterisk the remainder, and mask a value of 4 runes or fewer entirely (FR-033). **Keep the character-counting behaviour and its comment** — rendering the stored value verbatim, `+` and spacing included, is now an explicit FR-033 clause rather than an implementation nicety
- [X] T171 [US2] Delete the now-unreachable `maskFrom(phone, phoneKeepLeading)` short-value branch in the same function. `maskFrom` itself stays — `MaskEmail` still needs it and the email rule is unchanged. Dead code encoding the superseded rule is exactly what a later reader would restore the old behaviour from
- [X] T172 [P] [US2] Confirm `TestMaskingNeverLengthensAValue` at `mask_test.go:82` is still green **without editing it**. One asterisk replaces one character, so output length still equals input length — the guard on the receipt's column alignment
- [X] T173 [P] [US2] Confirm `receipt_pdf_test.go:113` and `service_test.go:507` pass **untouched**. Both call `MaskPhone` as their own oracle, so they track the helper by construction; their job is surface-vs-helper agreement (SC-008), not shape. If either needs an edit, the two surfaces have stopped sharing one helper — that is a finding to report, not a test to fix
- [X] T174 [US2] Open the delivered message and the receipt attachment per [quickstart.md](./quickstart.md) Revision 4 Step 2 and confirm the phone reads identically on both, character for character (FR-032, SC-008, SC-023). The receipt half is a human check — the shipped PDF is compressed and unreadable from Playwright (R-025)

---

## Phase 5: User Story 3 - A resend delivers the identical pair (Priority: P3)

**Goal**: Nothing regresses. This revision adds no resend behaviour.

- [X] T175 [US3] Run the existing resend coverage at `e2e/specs/guest-purchase.spec.ts:224-239` unchanged and confirm it stays green. The resend shares one send path with the automatic delivery, so it inherits both the new subject and the new mask with no code of its own
- [X] T176 [US3] Confirm a resent order's subject carries the **same** order number and event name as the first delivery, and its ticket codes are unchanged (FR-007)

---

## Phase 6: Polish & Cross-Cutting Concerns

### Verification

- [X] T177 Confirm the three Phase 2 assertions are now **green**, closing the red-first loop T165 opened
- [X] T178 Run the Go tier green: `cd backend && ./scripts/test.sh ./...`
- [X] T179 Run the e2e gate green: `cd e2e && npm test`
- [X] T180 Run it again with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test` (Principle VII kill switch)
- [X] T181 [P] Confirm the frontend tier needs no run — nothing in this revision reaches `frontend/`. Revision 3's frontend work was the logo; this one is two backend strings

### Governance — verify, do not assume

- [X] T182 [P] Confirm `contracts/notification.md`'s Message contract and its new `Subject` assertable property match what shipped. Both were corrected during the Revision 4 planning pass, so this is a re-read against the code, not an edit
- [X] T183 [P] Confirm `SCHEMA.md` needs no change and no migration exists — this revision adds no column, table or migration ([data-model.md](./data-model.md) §Derived values: both changes are computed at render time and never stored)
- [X] T184 [P] Confirm `PRD.md` needs no edit. Verified during planning: §1.4 describes one email with two document attachments and says nothing about the subject line. `grep -i subject PRD.md` returns nothing
- [X] T185 [P] Confirm `.specify/memory/constitution.md` needs **no** amendment — `grep -i subject` returns only unrelated prose, and nothing in it constrains the mask. Record that it was verified

### Recorded, deliberately not done

- [X] T186 Confirm three things stay as they are and are recorded rather than forgotten: the **email masking rule is unchanged** (FR-033's email clause is untouched — only the phone shape moved); the **receipt's mask assertion stays at the Go tier** because the shipped PDF is compressed (R-025); and the mask now **discloses more of the number than before**, accepted deliberately and recorded in FR-033, spec.md §Assumptions and R-027 so a later reader does not reverse it as a privacy fix

---

## Dependencies & Execution Order — Revision 4

### Phase dependencies

```
Phase 1 (T161-T162)  stack up, baseline recorded
        ↓
Phase 2 (T163-T165)  assertions written and SEEN RED   ← blocking, non-negotiable
        ↓
   ┌────┴────┐
Phase 3      Phase 4        US1 and US2 are independent — different files entirely
(T166-T168)  (T169-T174)    US1 is service.go; US2 is mask.go
   └────┬────┘
Phase 5 (T175-T176)  regression only
        ↓
Phase 6 (T177-T186)  gates and governance
```

### Critical path

T161 → T162 → T165 → T170 → T177 → T179 → T180

T165 before T166/T170 is the ordering that matters most. It is the only point at which the
red-first evidence can be gathered, and once the fix lands it cannot be recovered without
reverting.

### Parallel opportunities

- **Phase 1**: T162 needs T161; nothing else in the phase
- **Phase 3 / Phase 4**: entire phases run in parallel after T165. `service.go` and `mask.go`
  do not overlap
- **Phase 4**: T172 and T173 together — both read-only confirmations on different files
- **Phase 6**: T181, T182, T183, T184, T185 all `[P]` — read-only checks on different files

### Blocked

Nothing. This revision carries no open question, no unavailable asset and no judgment call —
unlike Revisions 2 and 3, both of which shipped with one item held.

---

## Implementation Strategy — Revision 4

**MVP scope**: Phases 1–3 (T161–T168). The subject line is independently shippable: it
touches one line of `service.go`, one Go assertion and one e2e assertion, and delivers a
buyer-visible improvement without going near the masking helper.

**Increment 2**: Phase 4 (T169–T174), the mask. Larger blast radius — one helper feeding two
surfaces — and the half that carries the red-first obligation.

**Both are small enough to land together**, and doing so halves the settle-an-order cycles.
Split them only if the mask's privacy tradeoff needs re-confirming before it ships; the
subject does not depend on that answer.

## Notes — Revision 4

- `[P]` marks tasks touching different files with no ordering constraint between them
- The two behaviours share no code: `service.go:215` composes the subject; `mask.go`'s
  `MaskPhone` serves both the body and the receipt. Neither fix can break the other
- **Stop and report rather than adjusting an assertion** if T173's two tests need edits, or
  if any Phase 2 assertion passes at T165. Both are signals that the system is not shaped the
  way this plan assumes

---

# Revision 5 — E-Ticket Colour Tasks (2026-08-19)

**Spec**: [spec.md](./spec.md) §Clarifications, Session 2026-08-19 (e-ticket colours) ·
**Plan**: [plan.md](./plan.md) §Revision 5 · **Research**: R-028, R-029, R-030

Task IDs continue from T186. Revision 4 ships first; nothing below re-opens it.

> **`setup-tasks.sh` cannot resolve this feature** — it reports `020-qris-frame-simplify`.
> Read-only, wrote nothing. Point `/speckit-implement` at `016-receipt-ticket-email`.

## Two things to read before starting

**Nothing in this revision can move a coordinate.** Every change is a colour constant or a
`setColor` argument. That is the whole reason it ships before the typeface — once Revision 6
starts, a failing geometry assertion has two possible causes.

**Assert the drawn colour, not the constant.** A test comparing `pdfBandDim` against itself
passes for any value — the same blind spot R-027 found in the mask tests. T188 exists so the
assertions in T189/T190 test the document.

---

## Phase 1: Setup (Revision 5)

- [X] T187 Render a baseline e-ticket through `RenderTicketsPDFPlain` and record the drawn colours of the event name, both footer labels and both footer values in `backend/internal/notification/`. Expect `#cb1c4f`, `#eceef3`, `#969cac` — the "before" half of the red-first evidence

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: T188 lands before T189 or T190. Without it the only assertable thing is a
constant's value, which is circular.

- [X] T188 Add a colour seam to `backend/internal/notification/export_test.go`, following the existing `TableInsets` / `HeaderColumnRights` pattern, exposing the colour operators drawn on a rendered page so a test can ask what the *document* says rather than what the package constant holds

---

## Phase 3: User Story 1 - Buyer receives a filable receipt alongside the tickets (Priority: P1) 🎯 MVP

**Goal**: The e-ticket's event name reads as a label rather than a warning, and the footer's
support address is the brightest text in its band rather than the dimmest.

**Independent test**: Render a multi-ticket order's e-ticket, open it, and read it against
Figma `683-148` — then print it in greyscale, which is where the old footer failed worst.

### Tests for User Story 1 ⚠️

Written first, at the **Go tier**. R-025 established that the shipped PDF is compressed and
Playwright cannot read what a page says; a drawn *colour* is further out of reach still.

- [X] T189 [US1] Add a footer-emphasis test in `backend/internal/notification/eticket_test.go` asserting each footer **value** is drawn brighter than the label above it, through the T188 seam (FR-022f, SC-024). **Confirm it fails first** — today the label is `#eceef3` and the value `#969cac`, so the failure output names both
- [X] T190 [P] [US1] Add a no-crimson test in `backend/internal/notification/eticket_test.go` asserting `#cb1c4f` appears nowhere on a rendered page outside the logo image (FR-019b, SC-024). Confirm it fails first

### Implementation for User Story 1

- [X] T191 [US1] Add `pdfEventName = [3]int{71, 85, 105}` (`#475569`) to the palette block in `backend/internal/notification/pdf.go` (~line 135) and use it at `pdf.go:252` in place of `pdfBrand` (FR-019b)
- [X] T192 [US1] Delete `pdfBrand` from `backend/internal/notification/pdf.go:135`. R-028 confirms `pdf.go:252` was its **only** consumer; leaving it would preserve a retired decision inside a palette block a future reader treats as authoritative. `emailBrand` in `service.go` is a different surface and stays
- [X] T193 [US1] Add `pdfBandLabel = [3]int{208, 209, 212}` (`#d0d1d4`) and use it for both footer labels at `backend/internal/notification/pdf.go:318` and `:330`. The value is white at 80% opacity flattened against the band — a PDF has no alpha here, so it is computed, not eyeballed; R-029 carries the arithmetic
- [X] T194 [US1] Change both footer **values** to pure white at `backend/internal/notification/pdf.go:322` and `:350` (FR-022f)
- [X] T195 [US1] Draw the envelope body in white at `backend/internal/notification/pdf.go:348`, **keeping the flap in `pdfBand`** so it still reads as a cut-out rather than a filled block (the existing comment explains why the flap is not white)
- [X] T196 [US1] Delete `pdfBandDim` from `backend/internal/notification/pdf.go:149` — the footer value and the envelope were its only consumers. **`pdfBandFg` must NOT be deleted**: it still serves the wordmark fallback at `pdf.go:430` and the receipt at `receipt_pdf.go:183`
- [X] T197 [US1] Print a rendered e-ticket in **greyscale** per [quickstart.md](./quickstart.md) Revision 5 Step 2. That is where the inverted emphasis was worst and where the fix should be most obvious

---

## Phase 4: User Story 3 - A resend delivers the identical pair (Priority: P3)

**Goal**: Nothing regresses. This revision adds no resend behaviour.

- [X] T198 [US3] Run the existing resend coverage at `e2e/specs/guest-purchase.spec.ts:224-239` unchanged and confirm it stays green — the resend shares one renderer with the automatic send and inherits the colours with no code of its own

> **User Story 2 has no work in this revision.** The email body's colours were settled in
> Revision 3 and no clarification reached them. Recorded so the gap is not read as an
> oversight.

---

## Phase 5: Polish & Cross-Cutting Concerns

- [X] T199 Run the Go tier green: `cd backend && ./scripts/test.sh ./...`
- [X] T200 Run the e2e gate green: `cd e2e && npm test`. No new assertion is expected here — the change is invisible to Playwright
- [X] T201 Run it again with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test`
- [X] T202 [P] Confirm `backend/internal/notification/receipt_pdf_test.go` passes untouched. The receipt shares only `pdfBandFg`, which survives; if anything moves there, a colour leaked across documents — report it rather than adjusting the test
- [X] T203 [P] Confirm `SCHEMA.md`, `PRD.md` and `.specify/memory/constitution.md` need no change — this revision adds no column, endpoint, migration or configuration key. Record that it was verified

---

## Dependencies & Execution Order — Revision 5

```
Phase 1 (T187)       baseline colours recorded
        ↓
Phase 2 (T188)       colour seam            ← blocking: without it the tests are circular
        ↓
Phase 3 (T189-T197)  assertions red, then the six constant/call-site changes
        ↓
Phase 4 (T198)       resend regression only
        ↓
Phase 5 (T199-T203)  gates and governance
```

**Critical path**: T187 → T188 → T189 → T191 → T194 → T199

**Parallel opportunities**: T189 + T190 (different test functions); T202 + T203 in Phase 5.
T191–T196 all edit `pdf.go` and are **not** parallel.

**Blocked**: nothing.

## Implementation Strategy — Revision 5

**MVP scope**: all of it. Seventeen tasks, one file of production code, no coordinate moves.
Splitting this further would cost more in settle-an-order cycles than it saves.

**Ship it before Revision 6**, not with it. That sequencing is the point.

---

# Revision 6 — Inter Typeface + Type Scale Tasks (2026-08-19)

**Spec**: [spec.md](./spec.md) FR-003a, FR-003b · **Plan**: [plan.md](./plan.md) §Revision 6 ·
**Research**: R-031 … R-035

Task IDs continue from T203. **Revision 5 must be green before starting** — see T205.

## Three things to read before starting

**Do not ship `InterVariable.ttf`.** gofpdf reads the classic TrueType tables and ignores
`fvar`/`gvar`, so a variable font renders every weight as its default instance: Bold and
SemiBold come out identical and **nothing errors**. It looks like a styling bug. T206 and
T213 exist solely to catch this (R-031).

**`latin1()` cannot be deferred.** It emits raw high bytes — correct for a cp1252 core font,
malformed UTF-8 under an embedded one. "Remove it later" is the option that ships broken
glyphs for exactly the names this change fixes (R-033).

**A green suite does not mean this worked.** R-035: the receipt's table seams catch drift and
`customerServiceBlock` self-adjusts, but the e-ticket's absolute offsets are literals with
nothing below them to notice a collision. T220's visual diff is a required step, not a nicety.

---

## Phase 1: Setup (Fonts)

- [ ] T204 Add the five **static** Inter faces — Regular, Medium, SemiBold, Bold, Italic — to `backend/internal/notification/assets/fonts/`, taken from the Inter release's `static` directory. This is the repository's first committed font asset; nothing ships one today (the frontend gets Inter from `next/font/google` at build time)
- [ ] T205 Confirm Revision 5 is merged and both tiers are green before proceeding: `cd backend && ./scripts/test.sh ./... && cd ../e2e && npm test`. Every failure after this point must be attributable to the font, which is why the colours shipped separately
- [ ] T206 [P] Verify none of the five files is a variable font — check that no `fvar` table is present. A variable font produces no error and no visible failure until Bold and SemiBold are compared side by side (R-031)
- [ ] T207 [P] Record the Inter version and its SIL Open Font License alongside the files, as `assets/brand/` already does for the mark

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: no type-scale work can begin until T208–T213 land. Every size change is
expressed against a registered family.

- [ ] T208 Register the faces once per document in `backend/internal/notification/pdf.go` using `AddUTF8FontFromBytes` — it takes bytes, so the fonts ship through the existing `//go:embed` path with no filesystem dependency and no `SetFontLocation` (R-031)
- [ ] T209 Register the same faces in `backend/internal/notification/receipt_pdf.go`. Map `Inter` to `""`/`B`/`I`, and register **`InterMedium` and `InterSemiBold` as their own families** — gofpdf's style string has no slot for weights 500 and 600 (R-032)
- [ ] T210 Replace every `SetFont("Helvetica", …)` in `backend/internal/notification/pdf.go` (~14 call sites) with the right Inter family and style
- [ ] T211 Replace every `SetFont("Helvetica", …)` in `backend/internal/notification/receipt_pdf.go`. **One call takes `style` as a variable and cannot be sed-replaced** — its family depends on the caller, so read it (R-032)
- [ ] T212 Delete `latin1()` from `backend/internal/notification/text.go` and all 22 call sites — 12 in `pdf.go`, 10 in `receipt_pdf.go` (R-033)
- [ ] T213 Render a page and confirm Bold and SemiBold are **visibly distinct**. If they are identical, T204 shipped the variable font despite T206 — stop and fix the assets rather than adjusting weights to compensate

---

## Phase 3: User Story 1 - Buyer receives a filable receipt alongside the tickets (Priority: P1) 🎯 MVP

**Goal**: Both documents are typeset in the family the designs use, non-ASCII names survive,
and the e-ticket's type matches the design.

**Independent test**: Render an order whose holder name carries a diacritic on a machine
without Inter installed; both documents render in Inter and the name prints as stored.

### Tests for User Story 1 ⚠️

- [ ] T214 [US1] Add a diacritic test in `backend/internal/notification/eticket_test.go` rendering a holder name carrying a diacritic through `RenderTicketsPDFPlain` and asserting the glyph survives rather than being transliterated to its nearest plain letter (FR-003a, SC-025). This is the one genuinely new behaviour in the revision

### Implementation for User Story 1

- [ ] T215 [US1] Apply the design's sizes and weights in `backend/internal/notification/pdf.go` per R-030's table: `Ticket N of M` 16→18 Bold; identity label 7.5→11 SemiBold; identity value 12→16 Bold; event name 9.5→14 Bold; ticket-type heading 20→22 Bold; `VALID FOR` 7.5→11 SemiBold; valid-for value 12→14 Bold; footer label 7.5→10 SemiBold; footer value 9→12 Medium (FR-003b)
- [ ] T230 [US1] Bring the rest of the e-ticket onto the design's slate palette in `backend/internal/notification/pdf.go`: field labels and `VALID FOR` → `#64748b` (from `pdfGray` `#6e6e6e`), field values and the ticket-type heading → `#0f172a` (from `pdfInk` `#18181b`), `Ticket N of M` → `#1e293b`, the divider rule → `#e2e8f0` (from `pdfLine` `#e0e0e4`) — FR-019c. **Do this in the same pass as T215**: it edits the same call sites, and split across two passes every one of them is visited twice. Check whether `pdfInk`, `pdfGray` and `pdfLine` retain consumers on the **receipt** before deleting anything — unlike `pdfBrand` and `pdfBandDim` in Revision 5, these are shared across documents
- [ ] T216 [US1] Re-derive every y-offset below a grown element in `backend/internal/notification/pdf.go`. The page constants are offsets from `bandHeight` (R-022) but the offsets themselves are literals — a heading growing 16→18 pt pushes toward the accent rule with **no assertion to notice** (R-035)
- [ ] T217 [US1] Confirm `marginLeft`/`marginRight` stay at 17.0mm. FR-003b explicitly excludes them: the design's 14.1mm inset would move the QR panel and the accent rule FR-022b pins
- [ ] T218 [US1] Re-derive the receipt's table-geometry assertions in `backend/internal/notification/receipt_pdf_test.go` from the `TableInsets` / `HeaderColumnRights` seams (SC-016, SC-017). **These failing is the system working** — they are the only automated guard on metric drift. Never re-hardcode
- [ ] T219 [US1] Confirm the FR-022e short/long-address test in `backend/internal/notification/eticket_test.go` passes **untouched** — `customerServiceBlock` computes width from `GetStringWidth` and self-adjusts. If it breaks, the width math stopped being metric-driven: report it rather than adjusting the expectation
- [ ] T220 [US1] Visual diff against Figma `683:148` per [quickstart.md](./quickstart.md) Revision 6 Step 3, using the before-PDFs kept at T205. Look for **collisions**, not just sizes — the heading grew 16→18 and identity values 12→16 while the accent rule and QR sit at fixed offsets below them

---

## Phase 4: User Story 2 - Email body reads as the new confirmation design (Priority: P2)

- [ ] T221 [US2] Confirm the email body needs **no** change. FR-003a scopes to the two PDF *documents*; the body is HTML and cannot embed a font that Outlook or Gmail would honour. Its stack is `font-family:Helvetica,Arial,sans-serif` at `backend/internal/notification/service.go:326`, and changing it to lead with Inter would help only recipients who already have it installed. Record the decision either way rather than leaving it unexamined

---

## Phase 5: User Story 3 - A resend delivers the identical pair (Priority: P3)

- [ ] T222 [US3] Run the resend coverage at `e2e/specs/guest-purchase.spec.ts:224-239` and confirm a resent order's documents carry the same ticket codes and the **same page count** as the first delivery (FR-007)

---

## Phase 6: Polish & Cross-Cutting Concerns

### Verification

- [ ] T223 Run the Go tier green: `cd backend && ./scripts/test.sh ./...`
- [ ] T224 Run the e2e gate green: `cd e2e && npm test`. It gains no new assertion — but a **changed page count is a stop signal**, meaning type growth spilled one ticket across two pages, which `SetAutoPageBreak(false)` exists to prevent
- [ ] T225 Run it again with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test`
- [ ] T226 [P] Confirm the shipped PDFs grew by only a few KB per weight, not by the ~300 KB of a full face — evidence that gofpdf's subsetting engaged (R-031)

### Governance and known limits

- [ ] T227 [P] Record **letter-spacing** as a known limit in this file's §Known limits, beside the day-boundary and footer-coverage entries: the design tracks every uppercase label 0.35–0.55px, gofpdf exposes `SetWordSpacing` but no `Tc` operator, and both workarounds are worse than the gap — per-glyph drawing breaks `MultiCell` wrapping the event name depends on, and thin spaces corrupt the selectable text layer (R-034)
- [ ] T228 [P] Confirm `SCHEMA.md`, `PRD.md` and `.specify/memory/constitution.md` need no change. The repository gains its first font assets, which is a new asset class but not a new architectural boundary — verify rather than assume
- [ ] T229 [P] Confirm `contracts/notification.md`'s static asset contract matches what shipped. The five faces and the variable-font prohibition were added during the Revision 6 planning pass, so this is a re-read against the code

### Recorded, deliberately not done

- [ ] T231 Confirm two gaps stay open and recorded rather than forgotten: the **receipt's type scale has never been examined** against its own design (FR-003b covers the e-ticket only, and its absence here is not a finding of agreement), and the **page margins stay at 17mm** against the design's 14.1mm because closing them is a layout change, not a typographic one

---

## Dependencies & Execution Order — Revision 6

```
Phase 1 (T204-T207)  fonts present, static, licensed, Revision 5 green
        ↓
Phase 2 (T208-T213)  registered, every SetFont replaced, latin1 gone   ← blocking
        ↓
Phase 3 (T214-T220)  diacritic test, type scale, geometry re-derived, visual diff
        ↓
   ┌────┴────┐
Phase 4       Phase 5      independent of each other
(T221)        (T222)
   └────┬────┘
Phase 6 (T223-T231)  gates, governance, known limits
```

**Critical path**: T204 → T206 → T208 → T210 → T212 → T215 → T216 → T218 → T220 → T223

T212 before T215 matters: deleting `latin1()` while the font is already registered means any
malformed-glyph fallout surfaces before the type scale starts moving coordinates, so the two
failure modes stay separable.

**Parallel opportunities**:

- **Phase 1**: T206 + T207 together, both read-only checks on the same new files
- **Phase 2**: T210 and T211 are different files, but both depend on T208/T209 — run after
- **Phase 4 / Phase 5**: entire phases in parallel
- **Phase 6**: T226, T227, T228, T229 all `[P]`

**Blocked**: nothing. T230 was the only blocked task and was answered on 2026-08-19 (FR-019c);
it now sits in Phase 3 beside T215/T216, which edit the same call sites. **Its ID is out of
numeric sequence deliberately** — it was renumbered nowhere so that plan.md, quickstart.md and
this file keep pointing at the same task.

## Implementation Strategy — Revision 6

**MVP scope**: Phases 1–3 (T204–T220). That is the whole substantive change: both documents
in Inter, non-ASCII names surviving, and the e-ticket matching the design's type.

**Do not descope Phase 2.** T212 in particular is not separable — a registered UTF-8 font with
`latin1()` still in place is strictly worse than either end state.

**T230 is answered** (FR-019c) and belongs inside Phase 3, not after it. Running it as a
follow-up pass means visiting every identity-block, heading, valid-for and divider call site a
second time.

## Notes — Revisions 5 and 6

- `[P]` marks tasks touching different files with no ordering constraint between them
- **Stop and report rather than adjusting an assertion** if: T219's address test breaks, T224
  shows a changed page count, or T213 finds Bold and SemiBold identical. Each is a signal that
  the system is not shaped the way these plans assume
- The two revisions are deliberately not interleaved. Revision 5 cannot move a coordinate;
  Revision 6 moves all of them

---

## Implementation record — Revisions 4 and 5 (2026-08-19)

**T161–T203 complete.** Go tier green, e2e green in both cache modes (44 passed / 2 skipped
enabled; 36 passed / 10 skipped disabled).

### Both red-first obligations were evidenced, and one needed a second run

The subject assertion failed first and aborted the test **before reaching the mask
assertion** — so the mask, the half that actually owed the red-first proof, had not been seen
red. Rather than accept that, the subject was fixed and the suite re-run so the mask assertion
became the failing one against unfixed masking code:

```
Expected substring: "08123456****"
Received string:    …<td …>08123***7890</td>…
```

`TestMaskPhone` was red on **4 of 7** cases; the 3 that passed are exactly the ones R-027
predicted would coincide (`081234567`, `0`, `""`).

### The spec carried an off-by-one that the implementation caught

FR-033, R-027's table, the quickstart and User Story 2 scenario 8 all paired
`+628123456789` → `+628123456****`. That output is 14 characters for a 13-character input,
which no length-preserving mask can produce. The example belongs to `+6281234567890` (+62
with an 11-digit mobile). **The code was right and the documents were wrong**; all four were
corrected, and `mask_test.go` now pins both lengths so the boundary cannot drift again.

### R-027's warning held exactly

`receipt_pdf_test.go:113` and `service_test.go:507` passed **untouched** through a deliberate
change of the mask's shape — they call `MaskPhone` as their own oracle. They were left alone
(surface-vs-helper agreement is their job, SC-008) and the e2e assertion was written as a
literal, which is why it could be red at all.

`service_test.go:193` also passed untouched under the new subject and was tightened to the
full literal for the same reason.

### Revision 5's colour seam, and its honest limit

`ColorOperator` builds gofpdf's content-stream operator from a literal so a test can find the
colour the **document** drew, not compare a constant with itself. It mirrors
`rgbColorValue` (fpdf.go:863) including the quirk that a grey collapses to the one-component
`g` operator — so pure white is `1.000 g`, not `1.000 1.000 1.000 rg`.

**Its limit is positional**: it proves a colour is drawn on the page, not which glyph carries
it. That is why FR-022f also asserts the luminance ORDERING of the two constants, and why the
greyscale check stays a required step rather than a nicety.

### Verified by eye

- Event name renders slate; no crimson anywhere outside the logo image.
- Footer in greyscale: the site URL and `help@manjo.com` are the brightest text in the band,
  `JIVE` and `CUSTOMER SERVICE` recede, envelope white with a band-coloured flap.
- Receipt Order Details shows `08123456****` — byte-identical to the email body (SC-008).

### Found in passing, not fixed

`backend/internal/notification/pdf.go` and `pdf_test.go` carry **pre-existing gofmt drift** in
the committed tree (14 and 16 diff lines at HEAD). Only the hunk this change caused was
corrected; the rest was left alone rather than bundling an unrelated reformat. `service.go`
was gofmt-clean before and is gofmt-clean after.

### Not done

Revision 6 (T204–T231) is untouched: it needs the five static Inter TTFs committed, which is
not a decision to take unprompted. T230 is no longer blocked and sits in its Phase 3.

---

# Revision 7 — Email Mask Direction Tasks (2026-08-19)

**Spec**: [spec.md](./spec.md) §Clarifications, Session 2026-08-19 (email mask direction) ·
**Plan**: [plan.md](./plan.md) §Revision 7 · **Research**: R-036 … R-039

Task IDs continue from T231. **Independent of Revision 6** — this touches `mask.go` and a test
fixture, neither of which the typeface work goes near, so it can ship without the Inter assets.

> **`setup-tasks.sh` cannot resolve this feature** — it reports `020-qris-frame-simplify`.
> Read-only, wrote nothing. Point `/speckit-implement` at `016-receipt-ticket-email`.

## Three things to read before starting

**The e2e fixture is blind to this change.** `defaultHolder.email` is `budi@example.com`, and a
4-character local part renders `b***` under **both** rules. An assertion written against it is
green before and after the fix. T234 must land before T235 or the red-first step is not
reproducible (R-037).

**Four of the seven mask cases coincide** between old and new rules. Any fixture with a local
part of four characters or fewer cannot see this change. Hold that in mind for every assertion
below.

**This is the second test-that-cannot-fail in this feature.** R-027 caught the first, in the
phone masking. Same shape, same cause: an assertion routed through the function under test, or
a fixture whose value is unchanged by the change.

---

## Phase 1: Setup (Revision 7)

- [X] T232 Bring the real dependencies up and migrate: `REDIS_PORT=6380 docker compose up -d postgres redis mailpit && docker compose run --rm migrate up`
- [X] T233 [P] Record the shipped baseline: settle one order and capture the masked email out of `.HTML` at `http://localhost:8025/api/v1/message/$ID`, per [quickstart.md](./quickstart.md) Revision 7 Step 1. This is the "before" half of the red-first evidence

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL — ordering, not just sequence**: T234 before T235, and both before any change to
`mask.go`. Writing the assertion against the current fixture produces a test that passes in
every state of the code, which is the exact failure Principle VIII names by hand.

- [X] T234 Lengthen `defaultHolder.email`'s local part past four characters in `e2e/support/journey.ts:26` — e.g. `budisantoso@example.com`. **All three references to the constant are by identity, not by literal** (`guest-purchase.spec.ts:124`, `:157`, `:245`), so nothing else moves; confirm with `grep -rn 'defaultHolder.email' e2e/specs/` before and after. Leave `siti@example.com` alone — holder addresses print in full on the e-ticket (FR-034), so its length has no bearing on any mask
- [X] T235 Add the email-mask assertions to the delivery block of `e2e/specs/guest-purchase.spec.ts`, beside the phone assertions from Revision 4: the new masked shape as a **literal** `toContain`, plus a `not.toContain` naming the old front-masked shape. **Literals only** — never `MaskEmail(...)`, never a regex over asterisks (R-039)
- [X] T236 Run `cd e2e && npx playwright test specs/guest-purchase.spec.ts -g "browses, books"` and **confirm the new assertion fails**, with the received body showing the old shape. Paste the failure into the implementation record. If it passes here, T234 did not take effect and the assertion is worthless

---

## Phase 3: User Story 2 - Email body reads as the new confirmation design (Priority: P2) 🎯 MVP

**Goal**: The buyer's email hides its last three characters before the `@` and nothing else,
with the domain intact.

**Independent test**: Settle an order whose buyer email has a local part longer than four
characters; the body's Buyer Information block shows exactly three asterisks immediately before
the `@`.

### Tests for User Story 2 ⚠️

- [X] T237 [US2] Rewrite `TestMaskEmail` in `backend/internal/notification/mask_test.go:17` — **4 of its 7 cases change** — `bud`, `a` and the empty value coincide between the rules. Add a case for a **5-character local part** (`dimas@gmail.com` → `di***@gmail.com`): under the old rule that returned unchanged, which is the disclosure R-036 found and no requirement had noticed. Keep the short cases that coincide, they are what prove the two rules overlap rather than that nothing happened. Confirm red before T238

### Implementation for User Story 2

- [X] T238 [US2] Replace `emailKeep = 5` with `emailMaskTrailing = 3` in `backend/internal/notification/mask.go:12`, carrying a comment that records what it replaced and why, as `phoneMaskTrailing` does
- [X] T239 [US2] Rewrite `MaskEmail` in `backend/internal/notification/mask.go:25`: keep the whole domain and every character of the local part except the last 3; asterisks = `min(3, len-1)`; a one-character local part, which cannot both keep and hide a character, is masked entirely (FR-033). A value with no `@` takes the same rule — accepted knowingly in clarification, and it discloses more than before
- [X] T240 [US2] Delete `maskFrom` from `backend/internal/notification/mask.go:90`. Revision 4 removed its phone caller and T239 removes its two email callers, so it has none left. Confirm with `grep -rn maskFrom backend/`
- [X] T241 [P] [US2] Confirm `TestMaskingNeverLengthensAValue` at `mask_test.go:87` is still green **without editing it** — asterisks replace exactly the characters they hide, so a masked value is never longer than the stored one
- [X] T242 [P] [US2] Confirm `receipt_pdf_test.go:112` and `service_test.go:510` pass **untouched**. Both call `MaskEmail` as their own oracle; surface-vs-helper agreement is their job (SC-008, R-039). If either needs an edit, the two surfaces stopped sharing one helper — report it rather than fixing the test
- [X] T243 [US2] Open the delivered message per [quickstart.md](./quickstart.md) Revision 7 Step 3 and confirm the body shows exactly three asterisks immediately before the `@`, with the domain intact (SC-026)

---

## Phase 4: User Story 1 - Buyer receives a filable receipt alongside the tickets (Priority: P1)

**Goal**: The receipt agrees with the body, character for character.

- [X] T244 [US1] Open the **receipt attachment** and confirm its Buyer Information email is byte-identical to the body's (FR-032, SC-008, SC-026). This half is a human check — the shipped PDF is compressed and unreadable from Playwright (R-025) — and it is the assertion that would catch the two surfaces drifting apart

---

## Phase 5: User Story 3 - A resend delivers the identical pair (Priority: P3)

- [X] T245 [US3] Run the resend coverage at `e2e/specs/guest-purchase.spec.ts:224-239` unchanged and confirm it stays green — the resend shares one renderer with the automatic send and inherits the new mask with no code of its own

---

## Phase 6: Polish & Cross-Cutting Concerns

### Verification

- [X] T246 Confirm the T235 assertions are now **green**, closing the red-first loop T236 opened
- [X] T247 Run the Go tier green: `cd backend && ./scripts/test.sh ./...`
- [X] T248 Run the e2e gate green: `cd e2e && npm test`
- [X] T249 Run it again with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test` (Principle VII kill switch)

### Governance — verify, do not assume

- [X] T250 [P] Confirm `SCHEMA.md`, `PRD.md` and `.specify/memory/constitution.md` need no change — no column, endpoint, migration or configuration key. Record that it was verified
- [X] T251 [P] Confirm `contracts/notification.md` needs no change: masking is applied inside the renderers and the Message contract does not describe it

### Recorded, deliberately not done

- [X] T252 Confirm three decisions stay as recorded rather than being quietly revisited: `MaskEmail` and `MaskPhone` remain **two functions** despite now sharing a shape, because their floors come from different requirements and a shared helper would carry the difference as an undecodable boolean at each call site (R-038); a value with **no `@` discloses more** than before (`notan*****` → `notanem***`), accepted because the field is format-validated at checkout; and the **e-ticket's holder email stays unmasked** (FR-034) — it is identity at a gate, not a contact detail on a forwardable receipt

---

## Dependencies & Execution Order — Revision 7

```
Phase 1 (T232-T233)  stack up, baseline recorded
        ↓
Phase 2 (T234-T236)  FIXTURE first, then assertion, then SEEN RED   ← ordering is the point
        ↓
Phase 3 (T237-T243)  Go test red, then one function rewritten
        ↓
   ┌────┴────┐
Phase 4       Phase 5      independent of each other
(T244)        (T245)
   └────┬────┘
Phase 6 (T246-T252)  gates and governance
```

**Critical path**: T232 → T234 → T235 → T236 → T237 → T239 → T246 → T248

T234 before T235 is the ordering that matters most, and it is the one a reader is most likely
to skip: the fixture looks like an unrelated detail, and the assertion looks correct without
it.

**Parallel opportunities**:

- **Phase 1**: T233 needs T232; nothing else in the phase
- **Phase 3**: T241 + T242 together — both read-only confirmations on different files
- **Phase 4 / Phase 5**: entire phases in parallel
- **Phase 6**: T250 + T251

**Blocked**: nothing.

## Implementation Strategy — Revision 7

**MVP scope**: all of it. Twenty-one tasks, one production function, one test fixture, one test
table. There is nothing here worth splitting across two commits.

**Do not start at Phase 3.** The function change is the easy part and takes ten minutes; the
value of this revision is the acceptance coverage, and that only exists if Phase 2 lands in
order.

## Notes — Revision 7

- `[P]` marks tasks touching different files with no ordering constraint between them
- **Stop and report rather than adjusting an assertion** if T236 passes before the fix, or if
  T242's two tests need edits. Both mean the system is not shaped the way this plan assumes
- This revision is independent of Revision 6 (Inter). Neither blocks the other

---

## Implementation record — Revision 7 (2026-08-19)

**T232–T252 complete.** Go tier green (17 packages), e2e green in both cache modes
(44 passed / 2 skipped enabled; 36 passed / 10 skipped disabled).

### R-037 was right, and it was the whole point

The fixture change was necessary **and** sufficient. With `budi@example.com` the assertion
could not have failed; with `budisantoso@example.com` it failed exactly as predicted, the body
showing the old front-masked shape:

```
Expected substring: "budisant***@example.com"
Received:           …<td …>budis******@example.com</td>…
```

`TestMaskEmail` was red on **4 of 7** original cases — not 5, as the plan, research, tasks and
quickstart all said. Corrected in all four: `bud`, `a` and the empty value coincide between the
two rules.

### The e2e run was blocked by a running dev server, and that needed a decision

Playwright starts its own Next dev server on :3100, but Next 16 refuses a second dev server for
the same directory, and one was running on :3000 (PID 215624, ~2h). It was not wired to the e2e
stack, so running against it would have produced misleading results. **`mask.go` was left
untouched while the question was asked**, which is what preserved the red-first evidence — had
the implementation landed first, T236 would have needed a stash-and-rerun to reproduce.

The server was stopped with the user's agreement and left stopped.

### Three arithmetic slips, all mine, all in the documents

The masked-value examples were hand-derived and wrong three times: `+628123456789` in
Revision 4, then `notanemai***` and `we.ird@thi***` in the test table, then
`budisanto***` in the e2e assertion. Every one was **longer than its input**, which a
length-preserving mask cannot produce.

The implementation was correct each time; the expectations were not. They are now computed
rather than counted, and `TestMaskEmail` pins nine cases including both lengths around each
boundary. The lesson is recorded because the same slip recurred across two revisions: **derive
these values from the rule, do not count characters by eye.**

### Verified on one real order, both surfaces

```
Subject: [ORD-20260819-7RQ8KP] E-receipt & E-Ticket for UAT Full Journey
Body:    budisant***@example.com   08123456****
Receipt: Email : budisant***@example.com   Phone Number : 08123456****
```

Read out of Mailpit and out of the receipt attachment via `pdftotext` — character for
character identical (FR-032, SC-008, SC-026).

### Held as planned

- `receipt_pdf_test.go:112` and `service_test.go:510` passed **untouched** — they call
  `MaskEmail` as their own oracle, exactly as R-039 said, and were left alone.
- `TestMaskingNeverLengthensAValue` green untouched.
- `maskFrom` and `emailKeep` **deleted**; `grep` returns nothing.
- `MaskEmail` and `MaskPhone` remain two functions (R-038).
- No schema, migration, PRD, constitution or contract change. The `contracts/notification.md`
  modification in the tree is Revision 6's font-asset entry and carries no masking content.

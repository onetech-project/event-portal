# Tasks: Order Page Buyer Information — Per-Ticket Holder Forms

**Input**: Design documents from `/specs/011-order-buyer-info/`

**Prerequisites**: plan.md (rev. 3), spec.md (Clarifications 2026-08-06 + 2026-08-07), research.md R1–R24, data-model.md (§1–§6 shipped `000012`; §7 target `000013`), contracts/{checkout-and-delivery,schema-revision}.md, quickstart.md

**Tests**: The spec's success criteria and the existing suites demand test updates; test tasks below are updates/rewrites of existing suites (no new TDD scaffolding was requested). Toolchain: node is not on PATH — run `tsc`/vitest per project memory (`frontend-toolchain-invocation.md`); vitest binary is at `frontend/node_modules/.bin/vitest`.

**Organization**:

- **Phases 1–7 (T001–T030) are DELIVERED** — FR-001 – FR-021, through commit `01ce8fe`. Phases 3–6 map to US1 (P1) forms without buyer card, US2 (P2) validation gating, US3 (P2) banner + delivery, US4 (P3) order summary. Left in place as the record; do not re-run.
- **Phases 8–12 (T031–T051) are rev. 3**, added by the 2026-08-07 clarifications: **Track A** = Phase 8, User Story 6 (P2), the end-of-journey modal; **Track B** = Phases 9–11, the schema revision (FR-025 – FR-029), which maps to **no user story** and therefore carries no story label, per the spec's own scope note below FR-021.

## Phase 1: Setup (migration + schema truth)

**Purpose**: The one schema change everything else compiles against.

- [X] T001 Create `backend/migrations/000012_gender_fk_and_contact_snapshot.up.sql` and `.down.sql`: add `attendees.gender_id uuid REFERENCES genders(id)` (nullable); backfill `UPDATE attendees a SET gender_id = g.id FROM genders g WHERE a.gender = g.name`; `ALTER TABLE attendees DROP COLUMN gender`; `ALTER TABLE orders DROP COLUMN buyer_dob, DROP COLUMN buyer_gender`. Down-migration restores the old shape via join-backfill + re-created CHECK, re-adds empty buyer columns (document the lossy step in a comment). (data-model.md §1)
- [X] T002 Apply the migration locally and sync `SCHEMA.md` in the same change: attendees `gender_id` FK (gender column removed), orders `buyer_dob`/`buyer_gender` removed, and buyer-column comments (~lines 103/111/114) re-documented as "primary contact — snapshot of the topmost holder form (spec 011)". (Constitution v2.0.0 governance sync)

---

## Phase 2: Foundational (keep the build green post-migration)

**Purpose**: The dropped columns are referenced by live queries — these updates BLOCK all user stories.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T003 Update `backend/internal/order/queries/order.sql`: `UpdateAttendeeDetails` takes `gender_id` instead of `gender`; `UpdateOrderBuyer` narrows to `buyer_name/buyer_email/buyer_phone`; `ListAttendeeSlotsByOrderID` and `ListAttendeesAdmin` JOIN `genders g ON g.id = a.gender_id` and return `g.name AS gender`; run `sqlc generate`. (research R15, R1)
- [X] T004 Update `backend/internal/order/repository.go` bindings to the regenerated signatures (`UpdateAttendeeDetails` with `gender_id`, 3-column `UpdateOrderBuyer`, joined slot/admin reads).
- [X] T005 Update `backend/internal/order/service.go`: active-genders load (`:398-408`) becomes a name→id map; the checkout fill loop (`:479-491`) resolves the validated gender name to `gender_id`; the `UpdateOrderBuyer` call passes only name/email/phone (still sourced from the request's buyer fields in this transitional state). `go test ./internal/order/...` green.

**Checkpoint**: Backend compiles and existing order tests pass against migration 000012.

---

## Phase 3: User Story 1 — One Form per Ticket Holder, No Buyer Form (Priority: P1) 🎯 MVP

**Goal**: The order page shows only holder forms (one per standalone ticket, one per bundle unit), every field marked mandatory; checkout works without `buyer_*`; the topmost holder becomes the primary-contact snapshot fed to the gateway.

**Independent Test**: Quickstart Scenarios A + C — form counts for all four SC-001 shapes with no buyer card; `orders.buyer_name/email/phone` = topmost form's holder; `GET /ticket/order/:id` has no buyer keys.

- [X] T006 [US1] `backend/internal/order/dto.go`: remove `BuyerName/BuyerEmail/BuyerPhone/BuyerDob/BuyerGender` from `CheckoutFormsRequest` (`:189-198`) and their validation blocks (`:219-235`); remove `BuyerName`/`BuyerEmail` from `TicketOrderDetail` (`:327-328`).
- [X] T007 [US1] `backend/internal/order/service.go`: after `matchVisitorsToSlots`, derive the primary contact as the visitor mapped to the first slot in canonical slot order (contract §1 normative — NOT `attendees[0]`); write it via the 3-column `UpdateOrderBuyer` in TX-D; feed `CustomerName/Email/Phone` at the gateway call (`:505-512`) from the same holder. (research R2, R10)
- [X] T008 [US1] `backend/internal/order/public_service.go`: stop mapping buyer fields (`:162-163`); update order-domain tests: checkout succeeds without `buyer_*` (stale fields ignored), snapshot equals first-canonical-slot holder for standalone-first and bundle-only orders.
- [X] T009 [P] [US1] `frontend/lib/types.ts`: drop `buyer_name`/`buyer_email` from `TicketOrderDetail` (`:189-190`).
- [X] T010 [P] [US1] `frontend/components/order/payment-status-card.tsx` (prop `:11`, PAID branch `:26`) and `frontend/app/(public)/events/[slug]/orders/[orderNumber]/done/page.tsx` (`:102`, + `done/page.test.tsx`): drop the `buyerEmail` prop and reword the PAID copy to per-holder delivery ("sent to each ticket holder's email"). (research R11)
- [X] T011 [US1] `frontend/components/order/visitor-form.tsx`: delete the "Buyer contact" card (`:173-202`) and the `buyer_*` members of `formsSchema` (`:62-67`); assemble the checkout body from `attendees` only; keep the `400001` field-path remap working.
- [X] T012 [P] [US1] `frontend/components/ui/field.tsx`: add a `required` prop rendering an asterisk beside the label + `aria-required` on the control; set it on all five fields in `visitor-form.tsx`. (research R16, US1-AS3)
- [X] T013 [US1] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: remove buyer-card assertions (e.g. `:494-496`); assert form counts for the SC-001 shapes, no buyer card, and required markers.

**Checkpoint**: Order page = holder forms only; checkout + payment work end-to-end without a buyer block.

---

## Phase 4: User Story 2 — Validation Gates Continue to Payment (Priority: P2)

**Goal**: Phone `^[0-9]{10,15}$` on both sides with one shared message (clarified 2026-08-07: widened from 10-12, stored verbatim, and the field filters non-digits as they are typed); button disabled until every form is valid; untouched-field errors reachable via click-capture reveal.

**Independent Test**: Quickstart Scenario B — per-field inline errors, disabled/enabled transitions, reveal on disabled-button click, API-level `400001` for a 9-digit phone.

- [X] T014 [US2] `backend/internal/order/dto.go`: replace the non-empty phone check (`:255-257`) with `^[0-9]{10,15}$`, message exactly "Enter a phone number of 10-15 digits."; extend dto validation tests (short/long/separators/letters/plus/empty, boundaries 10 & 15, both `08…` and `62…` stored verbatim). (clarified 2026-08-07) (research R3)
- [X] T015 [P] [US2] `frontend/components/order/visitor-form.tsx`: `visitorSchema.phone` (`:57`) → `z.string().regex(/^[0-9]{10,15}$/, "Enter a phone number of 10-15 digits.")`; the phone input becomes a `PhoneInput` that masks to digits only, `inputMode="numeric"`, placeholder `08123456789`. (clarified 2026-08-07)
- [X] T016 [US2] `frontend/components/order/visitor-form.tsx`: `useForm` gets `mode: "onTouched"`; submit button `disabled={!formState.isValid || submitting}`; wrap the button in a click-capture element that calls `trigger()` when invalid so untouched fields surface their errors (never submits while invalid). (research R5)
- [X] T017 [US2] `page.test.tsx`: assert initial-disabled, per-field error messages (email/phone/name/DOB/gender), reveal-on-click for untouched fields, enable-when-all-valid, re-disable on edit.

**Checkpoint**: No invalid submission can reach checkout from the UI; server mirrors every rule.

---

## Phase 5: User Story 3 — Email Banner and Per-Holder Delivery (Priority: P2)

**Goal**: Page-top banner sets the expectation; on PAID, one email per distinct holder address with that holder's PDF + the full order receipt; all-or-nothing `email_sent`; resends fan out; admin `sent_to` becomes a list.

**Independent Test**: Quickstart Scenarios E + F — 3 distinct addresses get exactly their own passes + full receipt; duplicate-address order gets one combined email with two holder blocks; webhook replay sends nothing; admin resend returns the address array.

- [X] T018 [US3] `frontend/components/order/visitor-form.tsx`: render the info banner as the first element of the registration phase (headline "E-tickets and receipt are emailed to each ticket holder below." + accuracy/primary-contact line per research R12); the old buyer-card badge died with the card in T011 — verify no other banner remains.
- [X] T019 [US3] `backend/internal/ticket/queries/ticket.sql`: `ListTicketDetailsByOrderID` (`:36-46`) also selects `a.id AS attendee_id, a.email AS attendee_email`; run `sqlc generate`; map through `backend/internal/ticket/repository.go` (`ListDetailsByOrderID`) and `backend/internal/ticket/dto.go` (`FullDetail` += `AttendeeID`, `AttendeeEmail`).
- [X] T020 [US3] `backend/internal/notification/pdf.go`: `TicketDetail` (`:22-29`) += attendee id + email (renderer itself unchanged — it is page-per-ticket and reusable on subsets).
- [X] T021 [US3] `backend/internal/notification/service.go`: rework `SendTicketEmail` (`:70`) — group tickets by normalized (trim+lowercase) attendee email with fallback to `OrderDelivery.BuyerEmail`; per recipient: `RenderTicketsPDF` on the group's tickets + body = full order receipt (Inv #, all line items + fee breakdown, Total Payment) + one holder-details block and e-ticket card section per distinct holder in first-occurrence order; attempt every recipient even after a failure (collect errors); `MarkEmailSent` only when all succeeded; signature becomes `(ctx, orderID) ([]string, error)`. (research R7, R8; contract §3)
- [X] T022 [US3] `backend/cmd/api/adapters.go`: thread attendee id/email through the notification ticket-details copy (`:249-267`); adapt the payment wiring so `payment.TicketDeliverer` keeps its `error`-only signature (narrowing adapter over the new return). (data-model §3)
- [X] T023 [US3] `backend/internal/notification/handler.go` + `dto.go`: admin resend responds with the recipient list returned by `SendTicketEmail`; `ResendResponse.SentTo` → `[]string` (order unspecified per contract §4); guest resend body unchanged.
- [X] T024 [P] [US3] `frontend/lib/types.ts` (`:336`) `sent_to: string` → `string[]`; `frontend/app/(admin)/admin/orders/page.tsx` (`:98`) toast joins the array.
- [X] T025 [US3] Rewrite the one-email notification tests with distinct-attendee-email fixtures (the buyer-email fallback must not keep them vacuously green): `TestSendTicketEmailDeliversOneEmailWithOnePDFToTheBuyer` + `TestSendTicketEmailBodyNamesTheBuyerAndTicketCount` (`backend/internal/notification/service_test.go:101,:207`), `TestResendEndpointReturns200AndTheRecipient` (`handler_test.go:43`), `TestPublicResendSendsOneEmailToTheStoredBuyerAddress` + `TestPublicResendIgnoresAnAddressSuppliedInTheBody` (`public_handler_test.go:61,:73`), and the stale one-email constitution citation in `smtp_test.go:68-69`; add cases: bundle unit → one email with N pages; duplicate address → one email, two holder blocks; per-recipient failure → others sent, `email_sent` FALSE, joined error; webhook replay → zero sends.

**Checkpoint**: Per-holder delivery proven by tests + quickstart E/F; guest and admin resends consistent.

---

## Phase 6: User Story 4 — Order Summary Card (Priority: P3)

**Goal**: Summary shows Booking ID, per-line unit price, and the Ticket Total / per-fee / Total Payment breakdown on both phases.

**Independent Test**: Quickstart Scenario D — values reconcile with the order's items and frozen fees; QRIS fixed.

- [X] T026 [US4] `frontend/components/order/order-summary-panel.tsx`: add the Booking ID (`order.order_id`) in the header area (supersedes the `:34` "no booking id" comment); render `item.unit_price` per line; add the breakdown — "Ticket Total" (`order.subtotal`), one row per `order.fees[]`, "Total Payment" (`order.total_amount`), collapsing to grand-total-only when `subtotal` is null; fix the fragment/key bug in the items loop (`:89-113`). (research R6)
- [X] T027 [US4] `page.test.tsx`: re-point the stale breakdown assertions (`:116-117`) at the new labels; assert Booking ID, unit price, per-fee rows, and total arithmetic on both phases.

**Checkpoint**: All four stories independently functional.

---

## Phase 7: Polish & Cross-Cutting

- [X] T028 [P] Delete the dead pre-008 checkout path in one sweep: `backend/internal/order/dto.go` (`CheckoutRequest`, `CheckoutAttendee`), `service.go` (`Checkout`, `reserve`, `reserveOnce`, `paymentRequestFor:888-914`, plus newly-dead `compensate:832`, `reservedLine:72`, `ticketNameFor:861`, `priceFor:877`), `demand.go` (`validateAttendees:145-166` — keep `expandItem`/`expandPackage`/`aggregateDemand`), `public_service.go` (`PublicOrderDetail`, `OrderByNumber`), `repository.go` (`CreateOrder`, `CreateAttendee`), `queries/order.sql:3-25` statements; run `sqlc generate`; delete their tests. (research R11)
- [X] T029 [P] `ARCHITECTURE.md`: update **both** sequence diagrams — booking/checkout (~lines 166/169: holder forms only; TX-D saves attendee details + primary-contact snapshot) and delivery (~189-237: per-holder fan-out, all-recipients `email_sent`). (Constitution v2.0.0 sync record)
- [X] T030 Full verification: `cd backend && go vet ./... && go test ./...`; frontend `tsc --noEmit` + vitest (per memory toolchain note); walk quickstart Scenarios A–F end-to-end against Midtrans sandbox + Mailpit.

---

# Rev. 3 (2026-08-07) — added after T001–T030 shipped

Two independent tracks from the 2026-08-07 clarifications. **They share no file**, so they can be worked in either order or in parallel by two people. Track A is guest-visible and small; Track B is a storage change with no guest-visible effect at all.

**Design sources**: plan.md rev. 3, research R17–R24, data-model.md §7, contracts/schema-revision.md, quickstart Scenarios G–I.

---

## Phase 8: User Story 6 — Time's Up Arrives as a Modal, Not a New Screen (Priority: P2) — Track A

**Goal**: An EXPIRED or CANCELLED order stops replacing the screen. Both order screens keep their own layout rendered and raise one unclosable dialog over it, rebuilt to Figma `293-3` with a single "Return to Home Page" action.

**Independent Test**: Quickstart Scenario G, run on both addresses — the page behind stays rendered with typed values intact; Escape, backdrop click, and scrolling all do nothing; exactly one button and no X control; on the QR screen the modal opens at countdown zero with no QR behind it and no second inline notice.

**Track A is frontend-only. No API, no migration, no backend file.**

- [X] T031 [US6] Create `frontend/components/order/end-of-journey-dialog.tsx` — an `EndOfJourneyDialog({ status }: { status: "EXPIRED" | "CANCELLED" })` built on the existing `@/components/ui/dialog` wrapper (Base UI, **not** Radix). Seal all three dismissal routes (research R17): `<Dialog open onOpenChange={() => {}} disablePointerDismissal>` — a controlled `open` that is never lowered is what defeats Escape, since ignoring `onOpenChange` ignores every close reason including ones a future Base UI version may add — plus `<DialogContent showCloseButton={false}>` for the X. Leave `modal` at its default `true`: that is what makes the page behind inert (focus trap + scroll lock + pointer-block), so do **not** hand-roll `inert` or `overflow:hidden`. Content per Figma `293-3` (research R24): `TriangleAlert` in a tinted circle, heading (`Time's Up` / `Order Cancelled`), body copy, and **exactly one** full-width filled `Link` to `/` reading "Return to Home Page". Expiry copy drops the old "Please repeat your order." sentence — use "Sorry, your payment time has expired. The tickets have been released back on sale."; cancellation keeps its current wording.
- [X] T032 [P] [US6] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx`: delete the early return at `:100-102` and its `ExpiredState` import (`:7`); compute `const ended = data.status === "EXPIRED" || data.status === "CANCELLED"` and render the normal forms tree with `{ended && <EndOfJourneyDialog status={data.status} />}` alongside it. The `WrongEvent` refusal at `:94-96` MUST still outrank it — an order reached through the wrong event's address is refused, not shown a modal (spec edge case).
- [X] T033 [P] [US6] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.tsx`: delete the early return at `:157-160` and the `ExpiredState` import (`:9`); define `ended = data.status === "EXPIRED" || data.status === "CANCELLED" || locallyExpired` (reusing the existing `locallyExpired` latch at `:103`) and render the dialog over the page. Gate the panels on `!ended` so nothing scannable sits behind it — `showPayment` (`:166`) becomes `data.payment !== null && !ended`. **Delete the inline `StatusAlert` at `:211-213`** ("This payment code has expired…") and its `showPayment` false-branch entirely: FR-022 exists to make one expiry produce one message, and that alert is the second. Keep the countdown banner and the order summary rendered — they are what makes the screen recognisably the one the guest was on.
- [X] T034 [US6] Delete `frontend/components/order/expired-state.tsx` once T032 and T033 no longer import it. Confirm with `grep -rn "ExpiredState\|expired-state" frontend/` that nothing references it, including tests.
- [X] T035 [P] [US6] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: rewrite the expired-order test (`:157`). The repeat-order assertion at `:169` must go — FR-024 deletes that link. Assert instead: the "Time's Up" dialog is present **and the holder forms are still in the document behind it** (that assertion is what distinguishes FR-022 from the old full-page swap — without it the test passes for either design); exactly one link, to `/`; no `repeat order` link; no close/X control (`queryByRole("button", { name: /close/i })` is null). Add a CANCELLED case.
- [X] T036 [P] [US6] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.test.tsx`: rewrite the expired test (`:236`) the same way, plus the two cases only this screen has — (a) the countdown reaching zero with the order still PENDING server-side opens the dialog immediately and removes the QR, and (b) the server's later EXPIRED status changes nothing on screen. Assert the inline "payment code has expired" alert is **gone**. Cover the sealed dismissal routes here at least once: fire Escape and a backdrop click, and assert the dialog is still open — a manual-only check will not survive a Base UI upgrade that adds a dismissal reason.

**Checkpoint**: Both order screens keep their context on a dead-end order; one message per expiry; Scenario G passes on both addresses.

---

## Phase 9: Schema Revision — Migration (Setup) — Track B

**Purpose**: The one schema change Phases 10–11 compile against. **No user story** — FR-025 – FR-029 are a storage revision with no guest-visible behaviour (spec scope note below FR-021).

- [X] T037 Create `backend/migrations/000013_master_list_identity_and_flags.up.sql` and `.down.sql`. **Do NOT edit `000012`** — it is committed (`e189e19`) and may already be applied; editing it would leave `schema_migrations` claiming a version the database does not hold (research R18). The up-migration's step order is forced, not stylistic (data-model §7.2): (1) drop `orders_status_fkey`, the name-based FK from migration `0009`, before touching `order_statuses.id`; (2) for each master list add the new integer identity column beside the uuid, populate by joining on the old key, then drop the uuid — there is no uuid→integer cast, so add/populate/drop is the only shape available, and deriving each row's new id from *its own* old id is what makes FR-025's no-silent-rebinding guarantee checkable; (3) assign order-status ids **explicitly** as `1 PENDING, 2 PAID, 3 CANCELLED, 4 EXPIRED`, then reset the identity sequence past them; (4) convert `attendees.gender_id` uuid→smallint the same way, keeping it nullable; (5) add `orders.status_id smallint`, populate from the existing `orders.status` name, set `NOT NULL`, add the FK, drop `orders.status`; (6) widen names to `varchar(256)`/`varchar(100)` keeping `NOT NULL UNIQUE`, and `is_active NOT NULL DEFAULT true` (FR-027); (7) add `created_by varchar(50)` + `updated_by varchar(50)`, backfill every existing row `created_by = 'SYSTEM'` **before** setting `NOT NULL` (FR-028); (8) add `packages.is_active boolean NOT NULL DEFAULT true` backfilled `(status = 'ACTIVE')`, then drop `packages.status` (its CHECK dies with it). **`tickets.status` is NOT touched** — FR-029 excludes it by name.
- [X] T038 In the same migration, drop and recreate `idx_orders_payment_expiry` (created in `000002:24-26` as `WHERE status = 'PENDING'`) as `... WHERE status_id = 1`. This is the one place the name-resolving subquery used everywhere else cannot go: PostgreSQL requires a partial index predicate to be immutable and rejects a subquery outright, which is why T037 step (3) fixes the seeded ids (research R20). The index is not optional — the expiry sweeper scans pending orders by `payment_expires_at` on every tick, and losing it silently degrades that to a sequential scan. Write the down-migration to restore the original predicate.
- [X] T039 Apply the migration locally and sync `SCHEMA.md` **in the same commit** — this is a hard obligation, not a follow-up ("Any schema change MUST update `SCHEMA.md` in the same change", Constitution → Technology Stack Requirements). Update: both master-list tables (`:54-64`, `:66-76`) with integer keys, widened names, and the two audit columns; `orders.status` → `status_id` (`:121`); `attendees.gender_id` type (`:179`); `packages.status` → `is_active` (`:217`); and the partial index (`:301`). Leave `tickets.status` (`:187`) and `events.status` (`:27`) exactly as they are — neither is in scope.

**Checkpoint**: `migrate up` then `migrate down 1` then `up 1` round-trips cleanly; quickstart Scenario H steps 1–5 pass.

---

## Phase 10: Schema Revision — Order Status Beneath an Unchanged Wire (Foundational) — Track B

**Purpose**: Make the status change invisible above storage. **⚠️ These SQL edits BLOCK Phase 11** only in the sense that the build must be green; they are otherwise independent of the package work.

**The governing constraint**: the order status is compared as a Go **string** in 29 files (`payment/status.go:12-15` defines `OrderStatusPending = "PENDING"` and friends; `payment/service.go`, `order/service.go`, `order/admin_handler.go:144`, `notification/service.go:115` all compare against them). Every task below exists so that **not one of those comparisons changes** (research R19). If you find yourself editing a Go status comparison, the SQL is wrong.

- [X] T040 `backend/internal/order/queries/order.sql` — do the name↔id mapping **inside the SQL**. Reads: add `JOIN order_statuses os ON os.id = o.status_id` and select `os.name AS status` on every order-returning statement (`:44`, `:52`, `:63`, `:181`, `:213`, `:274`) so the generated structs keep `Status string` carrying the name. Writes: `SET status = $2` → `SET status_id = (SELECT id FROM order_statuses WHERE name = $2)` (`:32`). Filters: `status = 'PENDING'` → `status_id = (SELECT id FROM order_statuses WHERE name='PENDING')` (`:33`, `:65`, `:125`, `:230`, `:239`, `:259`, `:270`) — the conditional-update guards keep their shape, so each remains one atomic statement (Constitution IV). Admin filter (`:184`) resolves `sqlc.narg(status)` the same way; `INSERT INTO orders (… status …)` (`:211`) inserts the resolved id. Then run `sqlc generate`.
- [X] T041 `backend/internal/order/repository.go` + `service.go`: adjust only what the regenerated signatures force — `attendees.gender_id` now binds as `int16`/`pgtype.Int2` instead of `uuid.UUID`, so the name→id map at `service.go:398-408` changes its **value type only**. Its keys, its callers, and the gender name on the wire are unchanged. Verify no order-status Go comparison needed editing; if one did, return to T040.
- [X] T042 [P] `backend/internal/payment/queries/payment.sql`: **comment-only change.** Verified that this file never selects from or updates `orders` — `payments.status` is the provider's raw transaction status, a different column with a different meaning, and FR-026 does not touch it. Its comment at `:4` cites `orders.status = 'CANCELLED'` and is now stale; reword it. Do not add a query change here. `backend/internal/ticket/queries/ticket.sql` needs nothing at all — it never references `orders`.
- [X] T043 Run `cd backend && go build ./... && go vet ./... && ./scripts/test.sh ./...`. The whole existing suite is the regression test for this phase: nothing should change behaviour, which is the point. Add one focused test asserting an order round-trips its status by name through create → read → status update → read, so a future refactor that leaks `status_id` onto a DTO fails loudly.

**Checkpoint**: Backend green with zero Go status-comparison edits; quickstart Scenario H steps 6–9 pass (uniqueness enforced, authorship backfilled, wire still carrying names).

---

## Phase 11: Schema Revision — Package Availability as a Boolean — Track B

**Purpose**: The one deliberate contract change in the whole revision (FR-029, contract §2). Admin-only surface, shipped on the same release train as the frontend that consumes it.

**⚠️ Disambiguation**: `backend/internal/event/admin_dto.go` holds **two** unrelated status enums. `StatusDraft/StatusPublished/StatusCompleted` (`:17-19`, `:37`, `:85`, `:117-121`) is the **event** status and is **NOT in scope**. Only `PackageStatusActive/PackageStatusInactive` (`:169-170`) and its uses are.

- [X] T044 `backend/internal/event/queries/event.sql`: `p.status = 'ACTIVE'` → `p.is_active` (`:162`, `:206`, `:268`); swap `status` for `is_active` in the package column lists (`:155`, `:209`, `:216`, `:218`, `:226`, `:228`). Run `sqlc generate`.
- [X] T045 `backend/internal/event/admin_dto.go`: delete the `PackageStatusActive`/`PackageStatusInactive` constants (`:169-170`); the package request field (`:194`) and response field (`:262`) become `IsActive bool \`json:"is_active"\``; delete the two-value validation switch (`:223-227`) — a bool needs none. Leave every event-status symbol untouched.
- [X] T046 `backend/internal/event/package_repository.go` (`:25`, `:62`, `:90`, `:187`, `:290`, `:314`, `:367`) and `package_service.go` (`:381` create, `:410` response map): carry `IsActive bool` through instead of `Status string`. On create, default to `true` when the field is absent.
- [X] T047 `backend/internal/event/` tests (`package_service_test.go`, `admin_dto_test.go`, `admin_handler_test.go`): update fixtures to `is_active`. Add the test the conversion actually needs — **an inactive package is absent from the public booking list**. That filter is the only thing standing between a retired bundle and a guest buying it, and it moved in T044.
- [X] T048 [P] `frontend/lib/types.ts` (`:292`) package `status: "ACTIVE" | "INACTIVE"` → `is_active: boolean`; `frontend/lib/schemas.ts` (`:106`) `z.enum(["ACTIVE","INACTIVE"])` → `z.boolean()`. **Do not touch** `OrderStatus` (`:183`, `:225`) or the ticket status union (`:232`) — both are deliberately unchanged (FR-026, FR-029).
- [X] T049 [P] `frontend/components/admin/package-form.tsx`: replace the two-`<option>` `<select>` (`:153-154`) with a checkbox/toggle bound to `is_active`, default `true` (`:68`); update `package-form.test.tsx` (`:52`) fixture.

**Checkpoint**: Quickstart Scenario I passes end to end — toggle in the admin UI, `is_active` on the wire with no `status` key, inactive packages absent from the public list, and issued tickets still reporting ACTIVE/USED/REVOKED.

---

## Phase 12: Polish & Verification (rev. 3)

- [X] T050 [P] `ARCHITECTURE.md`: two narrow edits where the status column is spelled literally — `~:187` and the webhook sequence diagram `~:224`, both of which say `WHERE status = 'PENDING'`. Leave the ticket-validation line `~:332` (`UPDATE tickets SET status = 'USED' … AND status = 'ACTIVE'`) exactly as it is: it is correct and in FR-029's excluded set. **`PRD.md` needs no edit** — `:39` describes the transitions by name, which is precisely what survives. **No constitution amendment is required** by either track.
- [X] T051 Full verification. Backend: `cd backend && go vet ./... && ./scripts/test.sh ./...`. Frontend: `tsc --noEmit` + `./node_modules/.bin/vitest run` (node is not on PATH and there is no `test` script — see project memory `frontend-toolchain-invocation.md`). Then walk quickstart **Scenario G** on both order addresses, **Scenario H against a database that already holds orders, holders and packages** (a freshly seeded one proves nothing about the conversion — capture the before/after status and gender names and diff them), and **Scenario I**. Confirm the partial index is actually chosen: `EXPLAIN SELECT id FROM orders WHERE status_id = 1 AND payment_expires_at < now();` shows an index scan.

---

## Dependencies & Execution Order

### Rev. 1–2 (delivered)

- **Setup (T001–T002)** → **Foundational (T003–T005)** — blocked everything (dropped columns break un-updated queries).
- **US1 (T006–T013)**: first story; contained the wire-contract break the others assume.
- **US2 (T014–T017)**: edits `visitor-form.tsx` after T011 — ran after US1 to avoid same-file conflicts.
- **US3 (T018–T025)**: T019→T020→T021→T022→T023 chain; T018 and T024 parallel-safe.
- **US4 (T026–T027)**: touched only the summary panel + tests.
- **Polish (T028–T030)**: last; T028/T029 parallel.

### Rev. 3 — the two tracks are independent

**Track A and Track B share no file.** Either order; both at once with two people.

- **Track A (T031–T036)**: T031 first (T032 and T033 import it) → T032 ‖ T033 → T034 (delete only once nothing imports it) → T035 ‖ T036.
- **Track B**: T037 → T038 (same migration file, ordered steps) → T039 → then **Phase 10 (T040→T041, T042 [P], T043)** and **Phase 11 (T044→T045→T046→T047, with T048 ‖ T049)** are independent of each other and can run in parallel once the migration is applied.
- **T050 [P]** any time after T039. **T051** last.

### Parallel opportunities

- Track A: T032 ‖ T033 (different pages); T035 ‖ T036 (different test files).
- Track B: T042 ‖ the T040→T041 chain; T048 ‖ T049 ‖ the T044→T047 backend chain; T050 ‖ everything after T039.
- **Across tracks**: all of Track A ‖ all of Track B.

### Ordering constraints that are NOT preferences

- T037's eight steps are forced by PostgreSQL, not chosen (no uuid→integer cast; FK must drop before the key changes; backfill before `NOT NULL`) — see data-model §7.2.
- T038 must fix the seeded ids because a partial index predicate cannot hold a subquery.
- T034 after T032 **and** T033 — deleting a still-imported component breaks the build.

## Implementation Strategy

### Rev. 3 — recommended order

**Ship Track A first.** It is six frontend files, guest-visible, fixes a dead end that currently destroys the guest's context, and carries zero deployment risk — no migration, no contract change. It is a complete increment on its own.

**Then Track B**, as one unit: migration + backend + frontend deploy together on the same release train (contract §2 — the `is_active` package field is a breaking admin-side change with no transition period, by decision). Within it, Phase 10 (order status) and Phase 11 (packages) are genuinely independent and can be split between two people after the migration lands.

**Track B is separable into its own spec** if the modal is time-sensitive — the spec's own scope note below FR-021 permits it, and the design artifacts (research R18–R23, data-model §7, contracts/schema-revision.md) move with it intact while Track A's (R17, R24) stay put.

### Still open, not absorbed into these tasks

- **FR-013 / FR-014 vs. the shipped summary panel** — the Booking ID and per-unit price were hand-removed to match Figma `206-3145`, and two assertions are suspended in `page.test.tsx` / `checkout/page.test.tsx`. This needs a decision (restore the UI, or amend FR-013/FR-014), not a task. Flagged in [checklists/requirements.md](checklists/requirements.md); no task above touches it.
- **FR-028's admin branch** — "entries an admin creates or edits MUST record that admin's identifier" has no code path, because no admin CRUD for genders or order statuses exists (only the read-only `GET /ticket/genders`). The columns ship correct and populated with `SYSTEM`; the admin branch activates when that CRUD is built, which is a new admin capability outside this feature (research R22).

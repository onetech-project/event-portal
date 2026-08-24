# Tasks: Order Page Buyer Information — Per-Ticket Holder Forms

**Input**: Design documents from `/specs/011-order-buyer-info/`

**Prerequisites**: plan.md (rev. 5), spec.md (Clarifications 2026-08-06 + 2026-08-07 + 2026-08-12 + 2026-08-13), research.md R1–R30, data-model.md (§1–§6 shipped `000012`; §7 shipped `000013`; §8 no schema impact; §2's phone rule narrowed with no schema impact), contracts/{checkout-and-delivery,schema-revision,fee-presentation}.md, quickstart.md

**Tests**: The spec's success criteria and the existing suites demand test updates; test tasks below are updates/rewrites of existing suites (no new TDD scaffolding was requested). Toolchain: node is not on PATH — run `tsc`/vitest per project memory (`frontend-toolchain-invocation.md`); vitest binary is at `frontend/node_modules/.bin/vitest`.

**Organization**:

- **Phases 1–7 (T001–T030) are DELIVERED** — FR-001 – FR-021, through commit `01ce8fe`. Phases 3–6 map to US1 (P1) forms without buyer card, US2 (P2) validation gating, US3 (P2) banner + delivery, US4 (P3) order summary. Left in place as the record; do not re-run.
- **Phases 8–12 (T031–T051) are rev. 3 and are DELIVERED**, added by the 2026-08-07 clarifications: **Track A** = Phase 8, User Story 6 (P2), the end-of-journey modal; **Track B** = Phases 9–11, the schema revision (FR-025 – FR-029), which maps to **no user story** and therefore carries no story label, per the spec's own scope note below FR-021.
- **Phases 13–14 (T052–T060) are rev. 4 and are DELIVERED**: **Track C**, the form-step fee presentation (FR-016, FR-016a – FR-016c), under constitution **v4.0.0**. It belongs to **User Story 4 (P3)**, whose Phase 6 shipped the summary card that revision corrected.
- **Phases 15–17 (T061–T072) are rev. 5 and are DELIVERED**, added by the 2026-08-13 clarifications under an unchanged constitution **v4.0.0**: **Track D** = Phase 15, the phone floor moving 10 → 12 (FR-006), belonging to **User Story 2 (P2)**; **Track E** = Phase 16, the FR-013/FR-014 amendment to the shipped summary panel, belonging to **User Story 4 (P3)**. Neither needs an amendment — v4.0.0 governs the panel's money figures and says nothing about phone format, the Booking ID, or a per-unit price. Track E finally closes the disagreement Phases 12 and 14 both left standing.

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

**Goal**: Phone `^[0-9]{10,15}$` on both sides with one shared message (clarified 2026-08-07: widened from 10-12, stored verbatim, and the field filters non-digits as they are typed); button disabled until every form is valid; untouched-field errors reachable via click-capture reveal. **The `{10,15}` bound here is the historical record of what this phase delivered — Phase 15 (Track D) raised the floor to `{12,15}` on 2026-08-13. Implement from Phase 15, not from this line.**

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

# Rev. 4 (2026-08-12) — added after T001–T051 shipped

One track. **Track C** corrects what the form-filling step's Order Summary shows: the
fee-inclusive `total_amount` becomes the pre-fee `subtotal`, and the "Includes all taxes
and fees" note becomes a statement that fees are added at the next step.

**Design sources**: plan.md rev. 4, research R25–R28, data-model.md §8,
contracts/fee-presentation.md, quickstart Scenario J. Constitution **v4.0.0** — the
fee-presentation bullet was amended for this track before planning, so Track C implements
the current rule rather than proposing one.

**Read this before writing a single test.** Nothing in the repository currently
distinguishes the two figures, so the obvious version of every assertion below passes
against unfixed code (research R27):

- `frontend/components/order/order-summary-panel.test.tsx:25-27` sets `total_amount` and
  `subtotal` to the same value with `fees: []`.
- `e2e/support/db.ts:44-45` **TRUNCATEs the `fees` table** between runs. Migration
  `000010` seeds `PPN 11%` and `Admin Fee 1200`, but the reset deletes them, so every e2e
  order books with an empty fee set and `total_amount == subtotal`.

Every task below therefore arranges data where `subtotal ≠ total_amount`, or it asserts
nothing. This is also why T053 comes before T054.

---

## Phase 13: User Story 4 (cont.) — The Form Step Shows the Raw Subtotal (Priority: P3) — Track C

**Goal**: On the holder-forms step the Order Summary's closing figure is the ticket
subtotal, its sub-line says fees are added at the next step, and no fee-inclusive number
appears anywhere on that route. The checkout step is unchanged. Nothing about the amount
charged moves.

**Independent Test**: Quickstart Scenario J — with a real fee configured, the form step
shows Rp 500.000 and the checkout step shows Rp 555.000, and the guest is charged
Rp 555.000.

**Track C is frontend + e2e only. No API, no migration, no backend file.** `subtotal` and
`fees` have shipped on `GET /ticket/order/:order_id` since `000010` (research R25); any
backend diff in a Track C commit is out of scope and should be challenged in review.

- [X] T052 [US4] Add a fee-arranging helper to `e2e/support/api.ts` alongside the existing admin helpers: `createFee(token, { name, feeType, value })` posting to `POST /api/v1/admin/fees` (handler at `backend/internal/order/admin_handler.go:34`; response shape at `backend/internal/order/dto.go:424`). It MUST go through the API, never a direct `fees` INSERT — AGENTS.md forbids arranging order/payment state in the database from a test, and the API route is also what keeps cached reads honest. Note in a comment that `e2e/support/db.ts:44-45` truncates `fees`, so this helper must be called per-test rather than relying on the migration seed.
- [X] T053 [US4] Add the acceptance scenario to `e2e/specs/guest-purchase.spec.ts`, near the existing Order Summary tests (~`:237`, `:337`), using `createFee` from T052 **before** booking. Assert three things, in order: (a) on the holder-forms route the panel's closing figure equals the order's `subtotal` and the fee-inclusive total appears **nowhere** on the page; (b) after Continue to Payment, `/checkout` shows the itemized rows and the fee-inclusive `total_amount`; (c) **the charged amount is the fee-inclusive total** — read it from the payment instruction, not from the screen (SC-005a). **Run this against unfixed code and confirm it fails** (Principle VIII, and constitution Governance: "a regression test never seen red proves nothing"). If it passes before T054, the fixture has no fee and the test is asserting nothing — go back to T052.
- [X] T054 [US4] `frontend/components/order/order-summary-panel.tsx`: replace the `showFeeBreakdown?: boolean` prop with `phase: "registration" | "payment"`, **no default** (research R26 — the flag now selects the headline figure as well as the rows, so a boolean named for the rows alone misdescribes it, and a default would reintroduce the ambiguity at a new name). Gate the breakdown block (`:135-148`) on `phase === "payment"`. At `:171` render `phase === "registration" ? (order.subtotal ?? order.total_amount) : order.total_amount` — **nullish coalescing, not `||`** (research R28: `"0.00"` is a real subtotal that must render as `Rp 0`, and only a `null` subtotal takes the fallback per FR-016c). At `:167` replace "Includes all taxes and fees" with the next-step wording on the registration phase, keeping the existing note on payment; the `Total payment` heading at `:163-165` stays on **both** phases (FR-016a). Update the component's doc comment, which currently cites constitution v2.1.0 and spec 011 FR-016 as "grand total alone".
- [X] T055 [US4] Update both call sites in the same change as T054 — the prop rename breaks the build otherwise: `frontend/components/order/visitor-form.tsx:314` `showFeeBreakdown={false}` → `phase="registration"` (and correct the adjacent comment, which cites constitution v2.1.0's superseded rule), and `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.tsx:222` gains an explicit `phase="payment"` where it previously relied on the default. Confirm with `grep -rn "showFeeBreakdown" frontend/` that nothing references the old prop, tests included.
- [X] T056 [P] [US4] `frontend/components/order/order-summary-panel.test.tsx`: the shared `order()` fixture at `:21-47` sets `total_amount` and `subtotal` equal with `fees: []` — give it parameters (or add a second builder) so a case can set `subtotal: "500000.00"`, `total_amount: "555000.00"`, `fees: [{ name: "PPN (11%)", amount: "55000.00" }]`. Add four cases: registration renders the subtotal and not the total; payment renders the total and the itemized rows; `subtotal: null` on registration falls back to the total (FR-016c); `subtotal: "0.00"` on registration renders `Rp 0` and **not** the total — that last one is the case a `||` implementation fails, so it is the reason it exists. Leave the six existing admission-window cases untouched; add `phase` to their renders as required by T054's signature.
- [X] T057 [P] [US4] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: the assertion at `:541` (`/includes all taxes and fees/i`) asserts the superseded rule and **fails the moment T054 lands** — that failure is expected. Rewrite the surrounding block: `HELD` must carry `subtotal ≠ total_amount` (the comment at `:537` already notes `HELD` has a non-null subtotal and a fee, so verify it and widen the gap if they are equal), then assert the rendered figure is the subtotal, that the fee-inclusive amount is absent from the whole document, that the note states fees are added at the next step, and that `Total payment` is still the heading. Keep the existing "no itemized rows on this step" assertions — they remain correct.
- [X] T058 [P] [US4] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.test.tsx`: behaviour here is unchanged, so this task is about pinning it. The breakdown assertions at `:130-141` and the pre-fee-order case at `:149-158` must keep passing against the explicit `phase="payment"` from T055. Add one assertion that this screen shows the **fee-inclusive** total while the form step does not, so a future edit cannot quietly make both screens agree again.

**Checkpoint**: T053's e2e scenario, red before T054, now passes; quickstart Scenario J walks end to end; the charged amount is unchanged.

---

## Phase 14: Polish & Verification (rev. 4)

- [X] T059 [P] Sweep for surfaces that still name the superseded rule: `grep -rn "taxes and fees\|showFeeBreakdown\|v2\.1\.0" frontend/ specs/011-order-buyer-info/ --exclude-dir=.next`. Expected survivors are the checkout phase's own note, the historical Clarifications bullet in `spec.md:20`, and the archived 2.1.0 report inside `.specify/memory/constitution.md` — those are records and stay. Anything else is a stale comment. **No governance document needs an edit**: the v4.0.0 amendment already verified `grep -i fee` returns nothing in `PRD.md` or `ARCHITECTURE.md`, and `SCHEMA.md`'s `fees`/`order_fees` tables are untouched by a display rule.
- [X] T060 Full verification. Frontend: `tsc --noEmit` and `./node_modules/.bin/vitest run` (node is not on PATH and there is no `test` script — project memory `frontend-toolchain-invocation.md`). Backend: `cd backend && go vet ./... && ./scripts/test.sh ./...` — expected to be **entirely untouched**; any backend failure means Track C exceeded its scope. e2e: `cd e2e && npm test`, and again with `E2E_CACHE_ENABLED=false` (Principle VII's kill switch). Then walk quickstart **Scenario J** against a database with a real fee configured, confirming step 6's invariant directly: the QRIS amount and the gateway gross amount are the fee-inclusive total, unchanged by this track.

---

## Phase 15: User Story 2 (cont.) — The Phone Floor Is 12, Not 10 (Priority: P2) — Track D

**Goal**: `^[0-9]{10,15}$` becomes `^[0-9]{12,15}$` on both sides of the wire, with the one
canonical message moving with it. Length alone — no prefix branch is introduced, so the
floor is counted on the value exactly as typed. Nothing already stored is re-judged.

**Independent Test**: Quickstart Scenario K — `08123456789` (11 digits) is refused in the
form and by the API with the same message; `628123456789` is accepted; an existing order
whose holder phone is 11 digits still opens, still renders, and still resends.

**Track D is backend + frontend + e2e. No migration, no SQL, no DTO shape change.**
`attendees.phone` stays `VARCHAR(50)` with **no CHECK** — see T066. A schema diff in a
Track D commit is out of scope and should be challenged in review.

> **Ordering is not a preference here.** T061 comes first and must be seen RED. The e2e
> holder fixture is `081298765432` — 12 digits — so it passes under both the old floor and
> the new one, and every existing test stays green against unfixed code (research R29).
> Writing the scenario after the regex would produce a test that never had the chance to
> fail, which constitution Governance rejects outright.

- [X] T061 [US2] `e2e/specs/guest-purchase.spec.ts`: add the floor scenario near the existing holder-form validation tests. Fill a holder form with an **11-digit** phone (`08123456789`), assert the inline error "Enter a phone number of 12-15 digits." and that Continue to Payment stays disabled; then correct it to `628123456789` (12 digits) and assert the error clears and the button enables. Do **not** change the existing `081298765432` fixture at `:98` — it is 12 digits, still valid, and its unchanged passing is what proves the change is a floor move rather than a rewrite. **Run this against unfixed code and confirm it fails** (Principle VIII): under `{10,15}` the 11-digit value is accepted, so the first assertion genuinely goes red. If it passes before T062/T064, the value being typed is not 11 digits — recount it.
- [X] T062 [P] [US2] `backend/internal/order/dto.go`: `visitorPhonePattern` (`:231`) → a `regexp.MustCompile` of `^[0-9]{12,15}$`; `visitorPhoneMessage` (`:233`) → exactly `"Enter a phone number of 12-15 digits."`; update the rule comment at `:226`, which states the 10-15 bound in prose. Change nothing else in the file — the field's type, JSON name, and position in `CheckoutFormsRequest` are unchanged, and this must remain a validation edit rather than a DTO shape edit (Principle III).
- [X] T063 [US2] `backend/internal/order/checkout_forms_test.go`: update the rule comment at `:138` and the verbatim message assertion at `:164`. Move the boundary cases from 10/15 to **12/15** and add the case the whole track exists for: an 11-digit value is rejected. Keep the existing separators/letters/plus/empty cases and both the `08…` and `62…` verbatim-storage cases — none of those change. Depends on T062 (same rule, adjacent files); running it before T062 lands means asserting the new message against the old constant.
- [X] T064 [P] [US2] `frontend/components/order/visitor-form.tsx`: `PHONE_MESSAGE` (`:86`) → `"Enter a phone number of 12-15 digits."`; `phoneSchema` (`:88`) → `z.string().regex(/^[0-9]{12,15}$/, PHONE_MESSAGE)`; update the comment block at `:81-85`, which cites the 2026-08-07 clarification and the 10-15 bound. **Also fix the placeholder at `:556`**: it currently reads `08123456789`, an 11-digit value the field now rejects — a field must not advertise an example it refuses. Use `081234567890`. **Do NOT touch `phoneDigits` at `:641-643`**: `slice(0, 15)` is the typing ceiling and stays; there is deliberately no typing floor, because a guest cannot be stopped mid-number at digit 11 (research R29).
- [X] T065 [US2] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: update the three verbatim message assertions at `:600`, `:621`, `:629` and the rule comment at `:410`. The case at `:608` ("rejects a phone number outside 10-15 digits with the exact message") needs its title and its input updated — rename to the 12-15 bound and drive an 11-digit value so the case exercises the new floor rather than re-testing a length both rules reject. Depends on T064.
- [X] T066 [US2] Verify the at-rest guarantee rather than assuming it (spec Clarifications 2026-08-13, research R29). Confirm `grep -rn "phone" backend/migrations/` shows no new constraint and that `\d attendees` reports `phone | character varying(50)` with no CHECK. Then walk quickstart **Scenario K step 5** against a database holding an order whose holder phone is 10 or 11 digits: it must open in the admin order view, render its phone, and accept a resend. **Adding a CHECK or a backfill sweep is out of scope and would fail on any environment holding real orders** — this task exists to make that explicit at the point someone would be tempted to "finish the job".

**Checkpoint**: T061 red → green. Both validators refuse 11 digits with one message; the ceiling, the verbatim-storage rule, and every stored row are untouched.

---

## Phase 16: User Story 4 (cont.) — The Summary Card Amendment Gets Its Tests Back (Priority: P3) — Track E

**Goal**: Close the FR-013/FR-014 disagreement that rev. 3 and rev. 4 both carried openly.
The spec now matches the shipped panel, so nothing is restored and no production behaviour
changes — the deliverable is that two suspended comment blocks become live assertions and
the last record of the disagreement is cleared.

**Independent Test**: Quickstart Scenario L — the summary card on both order steps shows
the event name, the line name, quantity and subtotal, and carries neither a Booking ID nor
a per-unit price; the Booking ID is still disclosed on the confirmation screen.

**Track E changes no production behaviour.** The only non-test edit is deleting a comment.
Nothing goes red on its own, which is precisely why the assertions must be written — a gap
that nothing reports is the failure mode here.

- [X] T067 [P] [US4] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: replace the suspended NOTE at `:529-531` with live negative assertions — the summary card renders **no Booking ID** and **no per-unit price** on the ticket line. Keep them inside the existing block so each sits beside a rendered positive (the event name at `:525` and the QRIS radio at `:534` are already asserted there): an absence assertion passes vacuously if the panel failed to render, and the positive is what rules that out (research R30). Verify the fixture's derived unit price does not collide with its line subtotal, grand total, or a fee amount before asserting on it.
- [X] T068 [P] [US4] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.test.tsx`: same treatment for the suspended NOTE at `:125-126`. The fixture here is `Regular` × 2 with a 550.000 line, so the absent unit price is 275.000 — a figure that appears nowhere else on that screen, which makes it a clean assertion. The comment at `:119-120` already records that Figma `206-3145` shows no per-unit price; fold it into the assertion rather than leaving it as prose.
- [X] T069 [US4] `frontend/components/order/order-summary-panel.tsx`: delete the NOTE at `:63-66` recording the FR-013/FR-014 disagreement. It documented a spec-vs-code conflict that no longer exists, and leaving it invites a future reader to "fix" the panel back. Replace it with a one-line reference to FR-013/FR-014 as amended, matching how the surrounding Figma citations read. **No behaviour change** — if this task produces a rendering diff, it exceeded its scope.
- [X] T070 [US4] `specs/011-order-buyer-info/checklists/requirements.md`: the Notes bullet beginning "**Open UI/spec disagreement (not a spec defect — flagged for a decision)**" is now stale and is the last place the disagreement is recorded as open. Rewrite it to record the resolution — the spec was amended to the shipped design on 2026-08-13, FR-013 drops the Booking ID, FR-014 drops the per-unit price, and the suspended assertions were reinstated as negative checks. Keep the note; do not delete it, because the history of a reversed requirement is worth as much as the requirement.

**Checkpoint**: FR-013 and FR-014 are asserted on both order screens, and no artifact still describes them as undecided.

---

## Phase 17: Polish & Verification (rev. 5)

> **Two pre-existing red tests found during T072, both outside Tracks D and E,
> both now resolved — recorded rather than absorbed silently.**
>
> 1. **The registration-phase fee note.** `order-summary-panel.tsx` renders
>    `"Excludes taxes and fees"`, while `order-summary-panel.test.tsx` and
>    `page.test.tsx` both asserted `"taxes and fees added at the next step"` —
>    contradicting copy and test landed together in commit `10fe9b8`. **Resolved
>    in favour of the shipped copy**: the two tests were changed to expect
>    `"Excludes taxes and fees"`. ⚠️ **Governance follow-up owed**: constitution
>    v4.0.0's fee-presentation bullet and FR-016a both say the note MUST *state
>    that taxes and fees are added at the next step*. "Excludes taxes and fees"
>    does not claim the figure includes them (so the prohibition holds), but it
>    does not make the forward-looking statement either. Either the bullet and
>    FR-016a are amended to the chosen wording, or the copy moves back — until
>    one of those happens, `/speckit-analyze` will keep flagging it.
> 2. **The booking panel's unit noun.** `selection-summary.test.tsx > multiplies
>    a line by its quantity` expected `"3 Tickets"` for a bundle line while the
>    component rendered `"3 Bundle"` (also unpluralised). `59b2293` had added a
>    `line.kind === "package" ? "Bundle"` branch and unpluralised the total;
>    `7134157`, later, reverted the total to `"Total N Tickets"` and wrote these
>    assertions but left the line-level branch behind — so the panel counted the
>    same bundle as "3 Bundle" on the line and "3 Tickets" on the total. The
>    branch was removed so both count in the same words; bundles remain visually
>    distinguished by `selectable-row.tsx`'s own "Bundle" badge, which is
>    untouched.

- [X] T071 [P] Sweep for surfaces still naming the superseded floor: `grep -rn "10-15\|{10,15}" backend/ frontend/ e2e/ specs/011-order-buyer-info/ --exclude-dir=node_modules --exclude-dir=.next`. Expected survivors are records and stay: the historical Clarifications bullet at `spec.md:30`, research R3's heading and its superseded-floor banner, the three-cuts history in `contracts/checkout-and-delivery.md:11`, the delivered T014/T015 lines in this file, and plan.md's deliberate old-vs-new comparisons. Anything else — a source constant, a test assertion, a placeholder, a comment describing the live rule — is stale and belongs in T062–T065.
- [X] T072 Full verification. Backend: `cd backend && go build ./... && go vet ./... && ./scripts/test.sh ./...`. Frontend: `tsc --noEmit` and `./node_modules/.bin/vitest run` (node is not on PATH and there is no `test` script — project memory `frontend-toolchain-invocation.md`). e2e: `cd e2e && npm test`, and again with `E2E_CACHE_ENABLED=false` (Principle VII's kill switch). Then walk quickstart **Scenario K** end to end, including step 5 against a database that already holds a short legacy number, and **Scenario L** on both order steps. Expected non-events, each worth confirming rather than assuming: no migration ran, `SCHEMA.md` needs no edit, and the amount charged on any order is unchanged.

---

## Phase 18: User Story 7 — Returning to the Forms Finds Them Still Filled (Priority: P2) — Track F

**Goal**: The holder forms render pre-filled whenever the order's slots carry saved details,
seeded once and never re-synced, with a retired gender still displayable and still acceptable.
The API already tells the guest their details are saved; this makes the next screen agree.

**Independent Test**: Quickstart Scenario M — fill the forms, force the gateway to fail, return
to the forms address, and find every field carrying what was submitted; an order whose forms
were never submitted still shows empty cards.

**Track F is frontend + a narrow backend acceptance change + e2e. No migration, no schema
change, no response-shape change.** A migration in a Track F commit is out of scope and should
be challenged in review.

> **Ordering is not a preference here.** T073–T074 come first and T074 must be seen RED.
> This is the third instance of the R27 trap and the most deceptive: every holder-form fixture
> in the suite has empty slots, so "the forms are empty" is simultaneously what the tests
> assert, what unfixed code does, and what fixed code must still do for an unsubmitted order
> (FR-030's last clause). A scenario that opens the forms and looks at them cannot fail. The
> scenario must first SAVE details through the real checkout call, then return.

- [X] T073 [P] [US7] `e2e/support/gateway-stub.ts`: export a helper — `failNextGatewaySession()` — that does `POST ${env.gatewayStubURL}/__stub/fail-next-session`. The control surface already exists at `:80` and **nothing in the suite calls it today** (research R35); only the helper is new. Do not change the stub's behaviour: it answers `400` rather than `500` on purpose, because the API retries a `5xx` three times under one reference and a test wanting one clean failure should not wait out the backoff. **Note for T074**: the forced-failure branch returns before `transactions.set(refId, …)`, so the reference is never recorded and the retry does **not** hit the duplicate branch — the retry succeeds cleanly and the seats are never released.
- [X] T074 [US7] `e2e/specs/guest-purchase.spec.ts`: add quickstart **Scenario M**. Book an order, fill every card with recognisable values, arm `failNextGatewaySession()`, press Continue to Payment, and assert the failure message *"We could not start the payment with the provider. Your details are saved — please try again."* and that the order is still `PENDING` with `payment_started: false`. Reload the forms address and **assert every field carries its step-1 value** — this is the assertion that separates fixed from unfixed code. Then continue again without editing and assert the checkout is accepted. Add the paired negative: an order whose forms were never submitted still shows empty cards. Pair each "filled" assertion with a rendered positive (the summary, the first card's delivery chip) so a screen that failed to render cannot satisfy it vacuously. **Run against unfixed code and confirm the reload assertion fails** (Principle VIII). **Isolation (constitution v4.2.0 Principle VIII)**: this is the first scenario to call `POST /ticket/checkout/:order_id` **twice for one order**, and `RATE_LIMIT_CHECKOUT` defaults to rate `0.2`/s, burst `3` — book its own order, keep its own client identity, and run it with `E2E_RATE_LIMIT_ENABLED` both on and off.
- [X] T075 [P] [US7] `frontend/components/order/slot-groups.ts`: `SlotGroup` gains the seed values (`name`, `email`, `phone`, `dob`, `gender`), read from the group's **first** slot. A bundle unit's slots are written identically by the submit-time fan-out, so no tie-break between them is specified (spec Assumptions, rev. 6). Keep `slotIds` and every existing field exactly as they are — this adds to the group, it does not restructure it.
- [X] T076 [P] [US7] `frontend/components/order/slot-groups.test.ts`: assert the seed travels with the group for all three shapes the grouper already covers — a standalone slot, a multi-slot bundle unit, and the pre-010 one-card-per-slot legacy shape. Assert a group built from empty slots carries empty seeds rather than `null` leaking into the form.
- [X] T077 [US7] `frontend/components/order/visitor-form.tsx`: add `isoToDob`, the inverse of `dobToIso` (`:559`), converting the wire's `YYYY-MM-DD` to the field's `DD/MM/YYYY`; it must round-trip so a restored date submitted unchanged stores the same date. Then replace the hardcoded `defaultValues` at `:125` with the group's seed values, mapping `null` to `""` per field. **Delete the stale comment** ("Option B: always empty — the server holds nothing to prefill from") — it records a premise this feature's own checkout call invalidated (research R31). **Do NOT add a `useEffect`/`reset` that re-syncs the form when `order` changes, and do NOT switch to RHF's `values` prop.** `useForm` captures `defaultValues` at mount and `OrderForms` mounts with data in hand, which is exactly FR-032; a re-sync would wipe a guest's typing on every window-focus refetch (research R33). Depends on T075.
- [X] T078 [US7] `frontend/components/order/visitor-form.test.tsx` (**new file**): (a) a filled-slots fixture seeds every field of every card, including the DOB conversion and the gender; (b) an empty-slots fixture renders empty cards (FR-030's last clause); (c) **the clobber test (FR-032)** — render, edit one field, clear another, then re-render with a *changed* order object and assert no field moved. (c) is the only guard against the failure mode with no visible symptom until a real guest is mid-type. (d) FR-033: no restore notice, banner or badge is rendered, and the first card's delivery chip is still the only notice. Depends on T077.
- [X] T079 [US7] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: the case at `:482` asserts every input is empty on a **two-slot** fixture and is the assertion holding the defect in place. Re-point it at an explicit empty-slots fixture so it keeps covering FR-030's last clause, and add a filled-slots case beside it. Add the ended-order case (US7 scenario 8): an EXPIRED or CANCELLED order renders its forms **filled** behind the end-of-journey modal — FR-022 already says the guest can still see what they had entered, and FR-030 is what finally makes that true after a reload.
- [X] T080 [P] [US7] `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx`: delete the stale Option B comment at `:119`. **No code change** — the `isPending` guard above it already ensures `OrderForms` mounts with data, which is what makes seed-once correct. If this task produces a behaviour diff, it exceeded its scope.
- [X] T081 [P] [US7] `backend/internal/order/queries/order.sql`: add `-- name: ListGenders :many` returning `id, name, is_active` for every row, ordered by name. **Leave `ListActiveGenders` at `:288` exactly as it is** — `GET /ticket/genders` must keep serving active genders only (FR-031 widens one card's list, never the master list). Run `sqlc generate`.
- [X] T082 [US7] `backend/internal/order/repository.go`: add a `ListGenders` wrapper beside `ListActiveGenders`, returning rows carrying `IsActive`. Depends on T081.
- [X] T083 [US7] `backend/internal/order/dto.go`: `Validate`'s gender membership check at `:292` reads the **full** gender map rather than the active-only one. **Do not move the call site** — `Validate` runs before `GetOrderByNumber`, so a malformed payload against an unknown order still answers `400001` and not `404`; reordering to get slot context in one pass would change error precedence this work has no business touching (research R34). The message text and the field key (`attendees[i].gender`) are unchanged.
- [X] T084 [US7] `backend/internal/order/service.go`: add `allGenders(ctx)` beside `activeGenders(ctx)` (`:366`) and pass the full map to `Validate`. After `matchVisitorsToSlots` / `validateBundleUnitConsistency` (`:421-426`), add the slot allowance: any submitted gender that is **not** active must equal the gender already recorded on that same slot, else `400001` on `attendees[i].gender`. Resolve `GenderID` at `:452` from the **full** map — the active-only map yields `0` for a retired name, writing an invalid foreign key rather than refusing. Depends on T082, T083.
- [X] T085 [US7] `backend/internal/order/checkout_forms_test.go`: a retired gender is accepted on the slot that already carries it; refused on a slot that does not; a name absent from the master list entirely is still refused with the unchanged message; and the stored `gender_id` after the accepted case is the retired gender's real id, not `0`. Add a case pinning error precedence: a malformed payload against an unknown order number still answers `400001`, not `404`. Depends on T084.
- [X] T086 [US7] `frontend/components/order/visitor-form.tsx`: at the `GenderSelect` call site (`:266`), pass a per-card option list — the active list from `useGenders()`, widened with that card's own restored gender when it is absent from it. `GenderSelect` already takes `options` as a prop, so this is a call-site change, not a component rewrite. A retired entry has no master id, so key its `SelectItem` by name. Once the guest picks a different gender the retired option leaves that card's list, and it is never offered on a card that does not hold it. Add the covering cases to `visitor-form.test.tsx`. Depends on T077.
- [ ] T087 [US7] Walk quickstart **Scenario O** (retired gender) end to end against a running stack. **Coverage justification, recorded rather than assumed (Principle VIII)**: the retired-gender path gets backend coverage (T085) and component coverage (T086) but no e2e scenario, because reaching it through the browser requires deactivating a master-list entry mid-run, which would leak throttle-free admin state into the guest suite and make the gender fixtures order-dependent for every other scenario. The two lower tiers cover the rule on both sides of the wire; this task is the manual confirmation that they meet in the middle. If the walk disagrees with either tier, the tier is wrong, not the walk.

**Checkpoint**: T074 red → green. A guest whose payment fails returns to filled forms, a guest who never submitted still sees empty ones, and nothing a guest has typed is lost to a refetch.

---

## Phase 19: Polish & Verification (rev. 6)

> **Three deviations from the task text, each taken for a reason and recorded
> rather than absorbed silently.**
>
> 1. **`failNextGatewaySession()` lives in `support/payment.ts`, not
>    `support/gateway-stub.ts`.** That module is the standalone server Playwright
>    boots as a `webServer`; it imports `node:http` and nothing else, deliberately.
>    Putting a `config`-reading helper in it would make the server process load
>    the suite's env. `payment.ts` already imports `config` and is the test-side
>    gateway driver, which is exactly what this is.
> 2. **`isoToDob` lives in `slot-groups.ts`, not beside `dobToIso` in
>    `visitor-form.tsx`.** `visitor-form` imports `slot-groups`, so the reverse
>    import would be a cycle. The conversion sits with `seedFrom`, its only caller.
> 3. **The gender widening is inside `GenderSelect`, not at its call site.**
>    FR-031 requires the retired option to leave the card's list once the guest
>    picks another, which needs the live field value; a list widened from the seed
>    would keep offering it forever. Only the Controller has that value.
>
> **Two pre-existing red tests found during T090, both outside Track F, and
> handled differently on purpose.**
>
> 1. **The bundle card heading's `flex-1` — FIXED.** `page.test.tsx` asserted
>    `toHaveClass("flex", "flex-1", "flex-wrap")` on the `CardTitle` while the
>    component rendered `flex flex-wrap … lg:w-full`, with no `flex-1`. Red at
>    HEAD, before this session. The component's own surviving comment explains
>    why the class is needed — "flex-1 so the heading fills the width its header
>    leaves it … otherwise `ml-auto` pins the badge to the end of the NAME rather
>    than the edge of the card" — which is spec 019 FR-016 word for word. The
>    class had been dropped and `lg:w-full` left in its place, which only holds
>    at `lg` and above. **Resolved by restoring `flex-1`**: the documented intent
>    and the test agreed with each other and only the class disagreed.
> 2. **The confirmation screen's "Back to home" button — FIXED.**
>    `success/page.test.tsx` asserted a `back to home` button with `href="/"`. Red
>    at HEAD. The control is not on `success/page.tsx` but on the
>    `order-confirmation.tsx` card it renders, which is why a first pass looking
>    only at the page reported it missing — it is not missing, it moved: the
>    button leads to `/events/{slug}` and its label still said "Back to home".
>    **Resolved by moving the label to the destination, not the destination to the
>    label.** The href change is consistent with spec 019's direction — the guest
>    lands where they can act — and reverting it to `/` would fight that. A control
>    reading "Back to home" that does not go home is precisely the mismatch spec
>    019 FR-004 forbids on the end-of-journey dialog, for the same reason. The new
>    wording, **"Back to the event"**, is what every other link to this destination
>    already reads (`checkout/page.tsx:115`, `success/page.tsx:77`, the order
>    page), so this removes an inconsistency rather than inventing a word. The test
>    now pins both halves: the label AND the href, plus the absence of the old
>    wording, so the pair cannot drift apart again silently.


- [X] T088 [P] Correct spec `008-e2e-purchase-flow`, which still asserts the superseded rule in four places: `contracts/booking-flow.md:78` ("revisit of the order page shows the held order with…" empty forms), `contracts/api.md:133` (the Option B heading), `quickstart.md:122` ("revisit shows empty slots (Option B)"), and `data-model.md:151`. Record the supersession — spec 011 FR-030 – FR-033, clarified 2026-08-19 — rather than deleting the history: Option B's persistence rule (nothing saved before Continue to Payment) still stands and is still correct; only its *revisit* consequence is superseded, because checkout now leaves details behind to render. **This is required in the same change** (spec 011 FR-030 scope note): a superseded rule left standing in a contract document is exactly what let this behaviour survive long enough to be reported.
- [X] T089 [P] Sweep for surfaces still asserting the superseded revisit rule: `grep -rn "Option B" backend/ frontend/ e2e/ specs/ --exclude-dir=node_modules --exclude-dir=.next`. Expected survivors are records and stay — spec 008's own history once T088 has framed it, plan.md's deliberate old-vs-new comparisons, and the delivered task lines in this file. Anything else describing the **live** behaviour of the forms — a source comment, a test name, a fixture comment — is stale and belongs in T077 or T080.
- [X] T090 Full verification. Backend: `cd backend && go build ./... && go vet ./... && ./scripts/test.sh ./...`. Frontend: `tsc --noEmit` and `./node_modules/.bin/vitest run` (node is not on PATH and there is no `test` script — project memory `frontend-toolchain-invocation.md`). e2e: `cd e2e && npm test`, again with `E2E_CACHE_ENABLED=false` (Principle VII's kill switch), and again with `E2E_RATE_LIMIT_ENABLED=false` (Principle IX / VIII, v4.2.0) — T074 spends two of the checkout allowance's burst of three, so both throttle modes must pass. Scenario M is discharged by the automated scenario T074 added, run red-first. The residual manual walks — Scenario N's tab-away behaviour and Scenario O — are owned by T087, which stays open. Expected non-events, each worth confirming rather than assuming: no migration ran, `SCHEMA.md` needs no edit, `GET /ticket/order/:order_id` and `GET /ticket/genders` are byte-identical, and the amount charged on any order is unchanged.

**T090 results (2026-08-20)** — run against the real stack (Postgres, Redis, Mailpit up,
migrations current):

| Tier | Result |
|---|---|
| `go build ./... && go vet ./...` | clean |
| `./scripts/test.sh ./...` (backend, database-backed) | **all green** |
| `./node_modules/.bin/next build` (typecheck of record) | exit 0 |
| `./node_modules/.bin/vitest run` | **412/412** |
| `npx playwright test` | 49 passed, 2 skipped, 1 failed (a different flake each run — see below) |
| `E2E_CACHE_ENABLED=false` | 42 passed, 10 skipped, **0 failed** |
| `E2E_RATE_LIMIT_ENABLED=false` | 47 passed, 5 skipped, **0 failed** |

Both new scenarios ran (not skipped) in all three e2e modes and passed in all three, so
Principle VII's cache kill switch and Principle IX's throttle kill switch are both genuinely
exercised against this change rather than assumed.

**Two distinct flakes surfaced across three valid full runs**, one per run and never the same
one twice: `a seat taken between the check and Agree refuses with the same general message`
(guest-purchase, a race scenario) and `rescheduling an event warns about stranded ticket types
instead of refusing` (admin-console). **Each passes in isolation, and both kill-switch runs
were clean at 0 failures.** Neither touches the holder forms or the gender path, and nothing in
Track F is upstream of either. Recorded rather than re-run until green: a suite that drops a
different test each full run under load is worth someone's attention on its own terms, and
papering over it here would hide that.

> ⚠️ **Environment trap, cost one confusing 7-failure run.** `next build` is this project's
> typecheck of record (project memory `frontend-toolchain-invocation`), but it writes a
> PRODUCTION build into the same `frontend/.next` that Playwright's `webServer` dev process
> uses. Running it between e2e runs left a stale manifest, and the next suite failed 7 tests
> with the frontend answering **404 for routes that exist** — which reads like a routing
> regression and is not one. `rm -rf frontend/.next` before the next e2e run restores it.
> Run the typecheck either before the e2e runs or after them, and clear `.next` either way.

Expected non-events, confirmed rather than assumed: **zero** migrations and no `SCHEMA.md`
edit; `dto.go`'s only change is a parameter rename, so no response shape, field or JSON tag
moved; the payment domain is untouched; and `GET /ticket/genders` still reads
`ListActiveGenders`, so the master list was not widened.

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

### Rev. 4 — one track, one forced ordering

```text
T052 (e2e helper)
  └─→ T053 (e2e scenario, MUST be seen RED here)
        └─→ T054 (component) ──┬─→ T056 [P] panel tests
              T055 (call sites)│   T057 [P] forms-page test
              — same change ───┘   T058 [P] checkout-page test
                                     └─→ T059 [P] sweep
                                           └─→ T060 full verification
```

- **Parallel**: T056 ‖ T057 ‖ T058 (three different test files, no shared fixture). T059 ‖ T060's frontend leg.
- **Not parallel**: T054 and T055 are one change — the prop rename breaks the build between them, so they land together rather than as two commits.

### Rev. 5 — two tracks, fully independent of each other

```text
Track D (Phase 15)                          Track E (Phase 16)
T061 (e2e scenario, MUST be seen RED)       T067 [P] forms-page assertions
  ├─→ T062 [P] backend dto.go               T068 [P] checkout-page assertions
  │     └─→ T063 backend dto tests            └─→ T069 delete the panel NOTE
  └─→ T064 [P] frontend schema+placeholder          └─→ T070 checklist note
        └─→ T065 forms-page message tests
              └─→ T066 at-rest verification

        └────────────┬────────────┘
                T071 [P] sweep
                     └─→ T072 full verification
```

- **Parallel across tracks**: Track D and Track E share no file and no requirement. Either can ship first, or both at once by two people.
- **Parallel within Track D**: T062 ‖ T064 (one Go file, one TSX file, no shared symbol). Their tests follow each: T063 after T062, T065 after T064, and once both constants have moved T063 ‖ T065.
- **Parallel within Track E**: T067 ‖ T068 (two different test files). T069 and T070 are sequential only in the sense that they are the same decision recorded in two places.
- **Not parallel**: T062 and T063 — the test asserts the constant the source task changes, so running the test task first asserts a new message against an old constant. Same for T064/T065.

### Ordering constraints that are NOT preferences

- T037's eight steps are forced by PostgreSQL, not chosen (no uuid→integer cast; FK must drop before the key changes; backfill before `NOT NULL`) — see data-model §7.2.
- T038 must fix the seeded ids because a partial index predicate cannot hold a subquery.
- T034 after T032 **and** T033 — deleting a still-imported component breaks the build.
- **T053 before T054**, and observed failing. This is the one ordering in rev. 4 that is not a build constraint but a correctness one: on the current fixture data (no fees anywhere) the scenario would pass against unfixed code, so writing it afterwards would produce a test that never could have caught the defect. Constitution Principle VIII and the Governance section both require it to be seen red.
- **T052 before T053** — the scenario has nothing to assert until a fee exists, because `e2e/support/db.ts:44-45` truncates the table the migration seeds.
- **T061 before T062 and T064**, and observed failing. The same correctness ordering as T053, for the same reason in a new place: the e2e holder fixture is 12 digits and valid under both floors, so the entire suite stays green against unfixed code (research R29). A scenario written after the regex could never have failed, and constitution Governance rejects a regression test never seen red.
- **T062 and T064 in the same change** — they are one rule enforced twice. Shipping the backend floor without the frontend one leaves the guest reading "12-15" only after a round trip; shipping the frontend one alone leaves the API accepting what the form refuses. Neither half is a releasable increment.

### Rev. 6 — one track, one forced ordering, one genuinely parallel half

Track F splits cleanly in two, and the halves touch no common file:

- **Frontend restore** (T075 → T077 → T078, with T076, T079, T080 alongside) — this is the whole
  of FR-030, FR-032 and FR-033 and is what the reported defect asks for. It ships without the
  backend half.
- **Retired-gender acceptance** (T081 → T082 → T083 → T084 → T085, then T086) — FR-031 only.
  It is a correctness fix for a case the restore *creates*: before the restore, a retired gender
  never reached the form, so it never came back on a submit.

**Forced ordering**: T073 → T074 before any implementation task, and T074 must be seen red.
T075 → T077 → T078 (each consumes the previous). T081 → T082 → T083 → T084 → T085 (the query
before its wrapper, the wrapper before the caller, the caller before its test). T086 needs
T077's seeding to have something to widen the options around.

**Parallel opportunities**: T073/T075/T081 open three independent fronts (e2e helper, frontend
grouping, backend SQL). T076, T079, T080 are independent of each other and of the backend half.
T088 and T089 are independent documents.

**If Track F must be split across two commits**, ship the frontend half first: it delivers the
reported fix. Ship it *with* T086 and the backend half in the same release, though — the restore
is what makes a retired gender reachable on a submit, so shipping the restore alone widens a
path the checkout still refuses.

**Not decomposed here**: nothing. Tracks D and E were decomposed and delivered in Phases 15–17
(T061–T072); Track F is the last outstanding track in this feature.

## Implementation Strategy

### Rev. 5 — the whole of the remaining work

Twelve tasks across two tracks that share no file. **Ship Track E first** if you want a
clean start: it is four tasks, changes no production behaviour, and its only real content
is turning two suspended comments into assertions — which also clears the last artifact
still describing FR-013/FR-014 as undecided. Nothing depends on it and nothing blocks it.

**Track D is the one with a gate.** Six tasks, one regex constant on each side of the
wire, and its entire risk is that nobody proves it. Write T061 first and watch it go red;
everything after is mechanical. Do the two source edits (T062, T064) as one change — one
rule enforced twice is not two increments — then their tests, then T066's at-rest check.

**What would make this go wrong**, in the order it is likely: (1) writing the e2e scenario
after the regex, producing a test that never could have failed — the fixture is 12 digits
and passes either way, so this failure is silent; (2) moving the regex without moving all
five verbatim assertions of the message string, which surfaces as a copy mismatch and is
actually the rule and the guest-facing text disagreeing; (3) leaving the `08123456789`
placeholder at `visitor-form.tsx:556`, so the field advertises an example it refuses;
(4) "finishing the job" with a CHECK constraint or a backfill sweep, which the at-rest
clarification forbids and which would fail on any environment holding real orders;
(5) for Track E, deleting the suspended comments instead of replacing them — that converts
a known gap into an invisible one.

### Rev. 4 — delivered

Track C is nine tasks and two lines of production code. It is a complete increment on its
own, depends on nothing outstanding, and can ship the moment T060 is green.

**The order is the strategy.** Write the failing e2e scenario first (T052 → T053) and
watch it fail. Everything after that is mechanical: two lines in one component, two call
sites, three test files. If T053 passes before T054, stop — the fixture has no fee and
nothing is being tested.

**What would make this go wrong**, in the order it is likely: (1) shipping without the
e2e scenario, leaving a display rule with no coverage on a covered flow; (2) writing the
tests on the existing equal-value fixtures, producing assertions that pass either way;
(3) using `||` instead of `??` and breaking free orders; (4) touching the backend, which
would mean the change stopped being a display rule.

### Rev. 3 — recommended order

**Ship Track A first.** It is six frontend files, guest-visible, fixes a dead end that currently destroys the guest's context, and carries zero deployment risk — no migration, no contract change. It is a complete increment on its own.

**Then Track B**, as one unit: migration + backend + frontend deploy together on the same release train (contract §2 — the `is_active` package field is a breaking admin-side change with no transition period, by decision). Within it, Phase 10 (order status) and Phase 11 (packages) are genuinely independent and can be split between two people after the migration lands.

**Track B is separable into its own spec** if the modal is time-sensitive — the spec's own scope note below FR-021 permits it, and the design artifacts (research R18–R23, data-model §7, contracts/schema-revision.md) move with it intact while Track A's (R17, R24) stay put.

### Still open, not absorbed into these tasks

- ~~**FR-013 / FR-014 vs. the shipped summary panel**~~ — **RESOLVED 2026-08-13 and now Phase 16 (Track E).** The clarification session amended the spec to the shipped design rather than restoring the UI, so this is no longer a decision held open: T067–T070 reinstate the two suspended assertions as negative checks and clear the checklist note.
- **FR-028's admin branch** — "entries an admin creates or edits MUST record that admin's identifier" has no code path, because no admin CRUD for genders or order statuses exists (only the read-only `GET /ticket/genders`). The columns ship correct and populated with `SYSTEM`; the admin branch activates when that CRUD is built, which is a new admin capability outside this feature (research R22).

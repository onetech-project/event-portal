# Research: Order Page Buyer Information — Per-Ticket Holder Forms

**Feature**: 011-order-buyer-info · **Date**: 2026-08-06 (rev. 2 — post-verification + gender-FK clarification)
**Sources**: spec 008 contracts (`specs/008-e2e-purchase-flow/contracts/api.md`), spec 010 contract (`specs/010-bundle-single-form/contracts/order-forms.md`), a code survey of the working tree (`feat/buyer` HEAD), and an adversarial verification pass (6 reviewers) whose confirmed findings are folded in below.

Starting point established by the survey — what already matches the spec and what doesn't:

| Spec 011 requirement | Current state |
|---|---|
| FR-001 no buyer form | ❌ "Buyer contact" card at `frontend/components/order/visitor-form.tsx:173-202`; backend `CheckoutFormsRequest` requires all five `buyer_*` fields (`backend/internal/order/dto.go:189-198`, `:219-235`) |
| FR-002 one form per ticket / per bundle unit | ✅ implemented by spec 010 (`frontend/components/order/slot-groups.ts:38`; server consistency check `backend/internal/order/service.go:636-666`) |
| FR-003 five holder fields | ⚠️ fields exist (`visitorSchema`, `visitor-form.tsx:52-60`) but nothing marks them mandatory on screen (US1-AS3) — see R16 |
| FR-004/FR-005 name/email rules | ✅ both sides (trim-nonempty; `net/mail.ParseAddress` / `z.email`) |
| FR-006 phone digits-only 10–12 | ❌ frontend `min(6)` only (`visitor-form.tsx:57`, the `visitorSchema.phone` rule; the buyer counterpart at `:65` dies with the buyer block); backend non-empty only (`dto.go:255-257`) |
| FR-007 DOB not future | ✅ both sides; wire format is `YYYY-MM-DD`, entry via masked DD/MM/YYYY text input (see R4) |
| FR-008 per-field inline errors | ✅ RHF + server `400001` field-map remap (`visitor-form.tsx:138-158`) |
| FR-009 button disabled until all valid | ❌ disabled only while submitting (`visitor-form.tsx:320-323`) |
| FR-010 save + booking id + go to payment | ⚠️ save + phase flip exist; Booking ID always pre-exists (created at `POST /ticket/book`) — see R13 |
| FR-011 banner at top of page | ❌ only a badge inside the buyer card (`visitor-form.tsx:176-179`) |
| FR-012 per-holder email delivery | ❌ single email to `orders.buyer_email` (`backend/internal/notification/service.go:96-104`) |
| FR-013 Booking ID in summary | ❌ deliberately omitted (`frontend/components/order/order-summary-panel.tsx:34`) |
| FR-014 unit price per line | ❌ `unit_price` on the wire but unrendered (`order-summary-panel.tsx:83-116`) |
| FR-015 QRIS fixed | ✅ radio injected via `beforeTotal` slot (`visitor-form.tsx:284-316`) |
| FR-016 fee breakdown | ❌ only grand total rendered; `subtotal` + `fees[]` already on the wire (`lib/types.ts:186-188`) |
| FR-017 primary contact | ❌ no concept today — the buyer block plays that role |
| FR-018 gender referential integrity | ❌ `attendees.gender` is `VARCHAR(20) CHECK IN ('MALE','FEMALE')` (migration 000005) while options come from the `genders` master table (migration 000008) — see R15 |

---

> **SUPERSEDED 2026-08-06 (delivery only)**: the per-holder fan-out described below was implemented and then reversed by clarification — delivery is exactly ONE email to the buyer (form 1's address) carrying every ticket plus the receipt (spec.md FR-012, constitution v3.0.0). Everything else on this page still holds; the holder emails are still collected and stored, so only the delivery layer changed.

## R1. Removing the buyer block: `orders.buyer_name/email/phone` become the primary-contact snapshot; `buyer_dob`/`buyer_gender` are dropped

**Decision**: Delete the five `buyer_*` fields from `CheckoutFormsRequest` and the "Buyer contact" card from the UI. Keep `orders.buyer_name`, `buyer_email`, `buyer_phone` and keep writing them in checkout TX-D via `UpdateOrderBuyer` — sourced from the **primary contact** (the topmost form's holder, R2). **Drop `orders.buyer_dob` and `orders.buyer_gender`** in this feature's migration (clarification 2026-08-06): they are write-only — added by spec 008 solely to mirror the now-deleted buyer form; no reader exists (survey traced every `buyer_*` read: payment customer details, admin order list, notification recipient — all use name/email/phone only). `UpdateOrderBuyer` (`queries/order.sql:248-253`) narrows from five columns to three.

**Rationale**: The three retained columns keep every downstream reader working unchanged: payment QR re-issue customer details (`backend/cmd/api/adapters.go:146-148` → `internal/payment/service.go:373-381`), the admin order list (`internal/order/admin_service.go:111-112`), and the notification fallback (R7). The snapshot's meaning changes from "separate buyer identity" to "denormalized copy of the primary contact (FR-017)".

**Alternatives considered**: dropping all five and re-sourcing readers from the first attendee row — rejected (churn in three domains for zero user value); keeping dob/gender as unread snapshot columns — rejected by clarification (dead data, and gender would duplicate the FK-governed value un-checked, undermining FR-018).

## R2. "Topmost form" is defined by canonical slot order, server-side

**Decision**: The primary contact is the holder of the **first slot group in canonical slot order** — the order returned by `ListAttendeeSlotsByOrderID` (`ORDER BY a.package_id NULLS FIRST, a.package_unit ASC, a.id ASC`, `backend/internal/order/queries/order.sql:294-304`). The server derives it by matching the request's visitors to slots (existing `matchVisitorsToSlots`, `service.go:607-628`) and taking the visitor mapped to the first slot of that ordering — **not** `attendees[0]` of the client-controlled request array.

**Rationale**: The slot ordering is already normative for form rendering (spec 010 contract §1) and `groupOrderSlots` (`frontend/components/order/slot-groups.ts:43-85`) pushes groups in first-encounter order of that array, so the topmost rendered form and the server's choice provably agree. Note the render consequence: **standalone forms render before bundle forms** (NULLS FIRST) — in a mixed order the primary contact is the first standalone form's holder.

**Alternatives considered**: trusting `attendees[0]` — rejected (unvalidated client ordering); explicit `is_primary` request flag — rejected (redundant with deterministic ordering).

## R3. Phone rule enforced on both sides, one message (`^[0-9]{10,12}$` → `^[0-9]{10,15}$`, clarified 2026-08-07)

**Decision** (revised 2026-08-07 — see below): Frontend Zod `z.string().regex(/^[0-9]{10,12}$/, "Enter a phone number of 10–12 digits.")` replacing `min(6)` at `visitor-form.tsx:57`; backend adds the identical rule and the **identical message** for every `attendees[i].phone` in `CheckoutFormsRequest.Validate` (`dto.go:255-257`), reported through the existing `400001` field map — one canonical string in both validators, since server errors are remapped onto the same inline slots the Zod messages use. Digits only — no `+`, spaces, or dashes. The input gets `inputMode="numeric"` (mobile numeric keyboard; it does **not** block non-digit typing — the regex does the rejecting) and a digits-only placeholder (e.g. `08123456789`).

**Rationale**: Spec FR-006 is explicit. Validation applies at checkout write time only; existing rows with out-of-range phones (old 7–20 rule) are untouched — `attendees.phone` is `VARCHAR(50)` with no CHECK.

**Revision 2026-08-07**: two changes, one kept and one reversed.

*Kept*: the field now filters non-digits keystroke by keystroke. This section correctly called `inputMode="numeric"` a hint rather than a filter, and that is exactly how it failed — on a physical keyboard letters typed straight through and sat in the box until blur. A `PhoneInput` mask strips everything but digits on the way in, so the schema is a backstop rather than the only guard.

*Reversed*: an intermediate cut hardcoded a `+62` prefix into the field and normalised every entry to E.164. That is undone. The field is free text with no country code of its own: the guest types the whole number in whichever form they think in, `628123456789` or `08123456789`, and **that form is stored verbatim** — no normalisation between them, no `+` added. Validation is therefore length alone, `^[0-9]{10,15}$`, with no prefix requirement, which also means a bare subscriber number or a foreign number passes. Rows written under the old 10-12 rule stay untouched — still `VARCHAR(50)` with no CHECK.

**Alternatives considered**: permitting `+62` and normalizing — rejected (spec says digits only; normalization is an unscoped product decision); JS keystroke filtering — rejected (unplanned complexity; validation covers it).

## R4. Date of birth: masked DD/MM/YYYY text input, ISO wire format (revised 2026-08-06)

**Decision (superseding the original native-control decision)**: Replace `<input type="date">` with a masked DD/MM/YYYY **text** input — `inputMode="numeric"`, `maxLength=10`, separators auto-inserted as digits are typed, no calendar picker (the native control's picker button is what the clarification rules out). The RHF field holds the masked display value; `dobToIso` converts to `YYYY-MM-DD` at submit, so the **wire format and the entire backend are unchanged** (`time.Parse("2006-01-02")`, `dto.go:208`). Zod validates shape (`DD/MM/YYYY`), real-calendar-date (rejects 31/02 via a UTC round-trip), and not-in-the-future — the last already existed on both sides and stays.

**Rationale**: FR-003 asks for DD/MM/YYYY literally, and the native control cannot be forced to that format — it follows the viewer's locale. The clarification (2026-08-06) chose the explicit format over the native control, so the earlier "accepted deviation" is void. Cost is one small mask + one converter on the client only; the numeric keypad has no "/" key, which is exactly why the mask inserts separators rather than requiring the guest to type them.

**Alternatives considered**: keeping `<input type="date">` — rejected by the clarification (locale-dependent display, unwanted picker button); a date-picker component library — rejected (new dependency for a birth-date field guests type faster than they navigate).

## R5. Continue-to-Payment gating via RHF `isValid`, with a reveal-errors affordance for untouched fields

**Decision**: Set the RHF form to `mode: "onTouched"` and gate the button with `disabled={!formState.isValid || submitting}`. Because a disabled button can never fire submit, spec US2-AS5 ("error shown when the guest attempts to proceed") is satisfied by a **click-capture wrapper around the disabled button**: clicking the button area while invalid calls `trigger()` (validate + mark all fields touched), surfacing every outstanding inline error including never-touched Gender/DOB fields. The wrapper never submits; actual submission remains possible only once `isValid` is true. Server-side `400001` remap stays as the second line of defense.

**Rationale**: FR-009 requires disabled-until-valid; FR-008/AS5 require errors to be reachable. `onTouched` maintains `formState.isValid` continuously without flashing errors mid-typing; the wrapper reconciles the two requirements without softening the gate.

**Alternatives considered**: `mode: "onChange"` — rejected (errors while typing the first character); leaving the button enabled and validating on submit — rejected (violates FR-009's literal disabled-by-default); no reveal affordance — rejected (AS5 unreachable, silently-disabled-button UX).

## R6. Order summary: add Booking ID, unit price, and the fee breakdown

**Decision**: Extend `OrderSummaryPanel` (`frontend/components/order/order-summary-panel.tsx`) to render:
1. **Booking ID** — `order.order_id` (`ORD-YYYYMMDD-XXXXXX`) in the header area, overriding the "no booking id line (Figma 206-3145)" comment at `:34` — spec FR-013 supersedes it.
2. **Unit price per line** — `item.unit_price` (already on `PublicOrderItem`) alongside quantity and subtotal (FR-014).
3. **Payment breakdown** — "Ticket Total" from `order.subtotal`, then one row per `order.fees[]` entry (names arrive frozen, e.g. `PPN (11%)`, `Admin Fee`), then "Total Payment" = `order.total_amount` (FR-016). The label mapping (per-fee rows collectively = "Tax & Service Fee"; "Total Payment" = "Grand Total Payment") is recorded in the spec's Assumptions (2026-08-06). When `subtotal` is `null` (pre-fee legacy orders) the breakdown collapses to the grand total only.

The panel serves both phases (registration via `visitor-form.tsx:274-324`, payment via `page.tsx:223-233`). The QRIS block stays in the `beforeTotal` slot — FR-015 already holds. In passing, fix the JSX `key`-on-inner-element-of-fragment bug in the items loop (`:89-113`) and re-point the stale `page.test.tsx:116-117` breakdown assertions at the new labels.

**Rationale**: Every datum is already on `TicketOrderDetail`; this is pure rendering. Per-fee rows are more truthful to the frozen `order_fees` snapshot than a synthetic summed line.

**Alternatives considered**: single summed "Tax & Service Fee" scalar — rejected (hides the PPN/Admin split the fee master models); new derived backend field — rejected (wire already carries the breakdown).

## R7. Per-holder delivery: group tickets by attendee email inside `SendTicketEmail`; every email carries the full order receipt

**Decision**: Keep the single fulfillment seam (`payment.fulfillAsync` → `notification.SendTicketEmail`) and make `SendTicketEmail` fan out:

1. Extend the ticket-details read with the holder: add `a.id AS attendee_id, a.email AS attendee_email` to `ListTicketDetailsByOrderID` (`backend/internal/ticket/queries/ticket.sql:36-46`), thread through `ticket.FullDetail` (`internal/ticket/dto.go:34-42`), the adapter copy (`cmd/api/adapters.go:249-267`), and `notification.TicketDetail` (`internal/notification/pdf.go:22-29`).
2. In `SendTicketEmail` (`internal/notification/service.go:70`), group tickets by **normalized (trimmed, lowercased) attendee email**. Each group gets one email: attachment = PDF containing only that group's ticket pages (reusing `RenderTicketsPDF` on the subset — the renderer is page-per-ticket with no order-level coupling); body = **the full order receipt** (order `Inv: #`, all order line items with the fee breakdown, Total Payment — so the receipt is complete and self-consistent in every copy, satisfying FR-012's "the order receipt" literally) plus **one holder-details block and e-ticket card section per distinct holder in the group**, in first-occurrence order of the order's ticket list. The multi-holder case is real: two different holders may enter the same address (spec edge case) — each still gets their own details block and cards inside the one combined email.
3. Empty/NULL attendee email (defensive; unreachable for post-011 checkouts) falls back to `orders.buyer_email` — the primary-contact snapshot (R1).
4. **`SendTicketEmail` returns the distinct recipient list** — signature becomes `(ctx, orderID) ([]string, error)` — because the admin resend handler needs the post-grouping addresses for its response (R9). `payment.TicketDeliverer` (`payment/service.go:104-106`) stays untouched: the adapter in `cmd/api/adapters.go` narrows the new signature back to `error` for payment's use.

**Rationale**: All grouping data already exists (`tickets.attendee_id` FK; `attendees.email` validated + populated in TX-D). Consumer-declared provider interfaces (`notification/service.go:32-48`) are wired only in `cmd/api/adapters.go`, so no domain-to-domain import appears (Constitution II). One composer keeps both resend endpoints correct automatically. Grouping by email means a spec-010 bundle unit (N identical rows) produces **one** email with N ticket pages.

**Alternatives considered**: fan-out at the `payment.fulfillAsync` layer — rejected (payment shouldn't own attendee knowledge); per-attendee send without email grouping — rejected (N copies to one inbox for a bundle); BCC one message to all holders — rejected (everyone would get everyone's tickets); holder-scoped receipt body — rejected after review (its line items wouldn't sum to the printed order total, and FR-012 promises "the order receipt").

## R8. Delivery bookkeeping: `orders.email_sent` stays a single all-or-nothing flag

**Decision**: Keep the per-order `email_sent` boolean. Send to every recipient group even if an earlier group fails (collect errors, don't abort); set `email_sent = TRUE` only when **every** recipient send succeeded; on any failure return the joined error so the flag stays FALSE and resend remains armed.

**Rationale**: A per-attendee ledger means a new column, SCHEMA.md churn, and admin surface changes — none needed by the spec (Constitution VI). Accepted trade-off: after a partial failure, resend re-emails holders who already got theirs — harmless (same codes; gate validation is one-scan-one-use).

**Alternatives considered**: `attendees.email_sent_at` — rejected (schema churn beyond need); abort on first failure — rejected (one bad mailbox starves the rest).

## R9. Resend endpoints inherit the fan-out; admin response becomes an unordered list

**Decision**: Both resends (`POST /api/v1/ticket/resend-email` guest, `POST /api/v1/admin/orders/:id/resend-email` admin) keep routing through `SendTicketEmail` and fan out identically. The admin `ResendResponse.sent_to` (`internal/notification/dto.go:7`) becomes `[]string` — the distinct recipient addresses **in first-occurrence order of the order's ticket list (order not otherwise specified)**. The handler gets the list from `SendTicketEmail`'s new return value (R7.4). The guest endpoint's uniform 202 anti-enumeration body is unchanged.

**Rationale**: One composer, one behavior. Canonical-slot ordering of the list was considered and dropped: `ListTicketDetailsByOrderID` orders by `(created_at, ticket_code)`, and an order's tickets share one transaction timestamp, so the effective order is ticket-code-lexicographic; promising canonical order would require threading `package_id`/`package_unit` into notification for zero admin value.

**Alternatives considered**: keeping `sent_to` a single string — rejected (actively misleading once N emails go out); guaranteeing canonical slot order — rejected (extra threading for a JWT-only convenience field).

## R10. Payment-gateway customer details come from the primary contact

**Decision**: Checkout's `gateway.CreateTransaction` customer fields (`internal/order/service.go:505-512`, today `req.Buyer*`) switch to the primary contact resolved in R2 (name/email/phone). QR re-issue needs no change: its `OrderRef` reads `orders.buyer_*` (`cmd/api/adapters.go:146-148`), which R1 keeps populated with the same person.

**Rationale**: Midtrans wants one customer per transaction; the primary contact is that person by definition (FR-017).

## R11. Read-side DTO cleanup and dead code removal

**Decision**:
- Drop `buyer_name`/`buyer_email` from the guest `TicketOrderDetail` (`internal/order/dto.go:327-328`, `public_service.go:162-163`) and from `frontend/lib/types.ts:189-190`. **Two guest files consume the field and must be edited in the same change** (they are type-breaks, not behavior: the consuming branch is unreachable at runtime): `frontend/app/(public)/events/[slug]/orders/[orderNumber]/done/page.tsx:102` passes `buyerEmail={data.buyer_email}` into `frontend/components/order/payment-status-card.tsx` (prop declared `:11`, rendered in its PAID branch `:26` — "Your tickets are on their way to {buyerEmail}"). Fix: drop the `buyerEmail` prop and reword the PAID branch to per-holder copy ("sent to each ticket holder's email"), updating `done/page.test.tsx` accordingly. The admin `OrderSummary` keeps its buyer columns (primary-contact data, R1).
- Delete the unrouted pre-008 checkout path (no non-test callers): `Service.Checkout`, `CheckoutRequest`, `CheckoutAttendee`, `reserve`/`reserveOnce`, `paymentRequestFor` (`service.go:888-914`), `validateAttendees` (**`internal/order/demand.go:145-166`** — its only caller is dead `reserveOnce`; `expandItem`/`expandPackage`/`aggregateDemand` stay, live for booking), `Repository.CreateOrder`/`CreateAttendee` **plus their SQL statements (`queries/order.sql:3-25`) and an `sqlc generate` run**, and the unmounted `PublicOrderDetail` + `PublicService.OrderByNumber`. Sweep the then-newly-dead helpers `compensate` (`service.go:832`), `reservedLine` (`:72`), `ticketNameFor` (`:861`), `priceFor` (`:877`).

**Rationale**: Constitution III; carrying dead buyer-shaped code through a buyer-removal feature invites edits to code that never runs, and half-deleting it leaves compile errors (`validateAttendees` references `CheckoutRequest`).

## R12. Banner placement and copy

**Decision**: Remove the badge inside the buyer card (dies with the card) and render a full-width info banner as the **first element of the registration phase**, above the forms grid, in `OrderForms` (`visitor-form.tsx`). Copy (English, matching the app's voice): headline "E-tickets and receipt are emailed to each ticket holder below." supporting line "Every holder gets their own ticket at their email address — double-check that each email is active and correct. The first form is our primary contact for this order." The payment phase keeps its countdown banner unchanged.

**Rationale**: FR-011 says "very top of the order page"; the registration phase is the only phase with editable holder emails. The copy reflects per-form delivery (spec Assumptions).

**Alternatives considered**: page-level banner outside the phase switch — rejected (would mislead on the QR phase).

## R13. "Payment page" = the existing same-route phase flip

**Decision**: FR-010's "directed to the payment page" is satisfied by the existing mechanism: checkout success invalidates the order query, the refetched order has `payment_started: true`, and the same route re-renders into the QR phase (`page.tsx:164-169`). No new route. The Booking ID always exists before this page (created by `POST /ticket/book`), so "create if not yet created" is vacuously satisfied.

**Rationale**: Spec 008 deliberately made one URL serve both phases (deep-linkable, resumable).

## R14. Constitution amendment v2.0.0 and governance-document sync

**Decision**: The Critical Data Flow Rules delivery bullet is redefined to per-holder delivery (one email per distinct holder address, holder's tickets as PDF + the receipt; `email_sent` only after all recipients). Version **1.1.1 → 2.0.0 (MAJOR)**: the amendment reverses a binding rule — behavior compliant under 1.1.1 is non-compliant under 2.0.0 — which is the versioning policy's backward-incompatible-redefinition pattern; MINOR's "materially expanded guidance added" does not describe a reversal. Executed in `.specify/memory/constitution.md` with a full Sync Impact Report. Governance sync (per the Governance clause, which binds ARCHITECTURE.md, PRD.md, and SCHEMA.md to amendments):
- **PRD.md — already updated with the amendment** (§1.4: per-holder forms, no Buyer Info; per-holder delivery).
- **ARCHITECTURE.md — implementation-change follow-up, BOTH sequence diagrams**: the booking/checkout diagram (~line 166 "Fill buyer + visitor forms", ~line 169 "TX-D — save buyer + attendee details") and the delivery diagram (single email → fan-out).
- **SCHEMA.md — implementation-change follow-up**: buyer-column comments (~lines 103/111/114 → "primary contact — snapshot of the topmost holder form"), plus the spec-011 migration's changes (R15, R1).

## R15. Gender referential integrity: `attendees.gender_id` UUID FK; name stays on the wire (clarified 2026-08-06)

**Decision**: One migration (`000012`) makes the holder gender a real reference:
- `ALTER TABLE attendees ADD COLUMN gender_id uuid REFERENCES genders(id)` (nullable — slots are created empty at booking);
- backfill `UPDATE attendees a SET gender_id = g.id FROM genders g WHERE a.gender = g.name` (total: the old CHECK admitted only MALE/FEMALE, both seeded);
- `ALTER TABLE attendees DROP COLUMN gender` (the CHECK dies with it);
- `ALTER TABLE orders DROP COLUMN buyer_dob, DROP COLUMN buyer_gender` (R1);
- down-migration restores the old shape (name column + CHECK re-derived via join; buyer columns restored empty — documented lossy step).

**The wire contract does not change**: checkout still submits the gender **name** (validated against active master rows as today, `dto.go:263-265`); the service resolves name → id when writing `UpdateAttendeeDetails` (the active-genders load at `service.go:398-408` becomes a name→id map instead of a set). Read paths (`ListAttendeeSlotsByOrderID`, admin `ListAttendeesAdmin`) JOIN `genders` to keep returning the name. `sqlc generate` refreshes the query bindings. SCHEMA.md is updated in the same change.

**Rationale**: FR-018 (clarification). Id-keyed FK is rename-safe and conventionally normalized; keeping the name on the wire confines the change to the order domain's SQL + service mapping — frontend, notification, ticket, and payment are untouched (notification reads `attendees.email` only; the PDF prints names, not gender).

**Alternatives considered**: FK on `genders(name)` with ON UPDATE CASCADE — rejected in clarification (keys on a mutable display value); submitting `gender_id` on the wire — rejected in clarification (breaking API + frontend churn for no user value).

## R16. Mandatory-field marking on every holder form

**Decision**: All five fields of every holder form are visibly marked required — a `required` prop on the shared `Field` wrapper (`frontend/components/ui/field.tsx`) rendering an asterisk beside the label plus `aria-required` on the control; `visitor-form.tsx` sets it on all five fields.

**Rationale**: Spec US1-AS3 requires "every field is marked mandatory"; today's labels are plain text with no indicator, so acceptance would fail. A shared-prop implementation keeps the marking consistent and testable.

**Alternatives considered**: "(required)" text suffix — either is acceptable; asterisk + aria attribute is the lighter, conventional choice. Recording a deviation instead — rejected (trivial to satisfy properly).

---

# Rev. 3 — 2026-08-07: end-of-journey modal (FR-022 – FR-024) and the schema revision (FR-025 – FR-029)

R1–R16 are delivered (tasks.md T001–T030 all complete). The decisions below cover the two requirement groups added after that work landed.

## R17. The end-of-journey screen becomes an unclosable Base UI dialog over the live page

**Decision**: Delete the full-page `ExpiredState` card (`frontend/components/order/expired-state.tsx`) and replace it with an `EndOfJourneyDialog` built on the existing `components/ui/dialog.tsx` wrapper over **Base UI** `@base-ui/react/dialog` v1.6.0 (not Radix — verified against the installed package and the v1.6.0 API docs). Both order pages stop early-returning the card (`page.tsx:101`, `checkout/page.tsx:159`) and instead render their normal tree with the dialog mounted alongside it.

Unclosability is three separate settings, because Base UI has three separate dismissal paths:

| Dismissal path | How it is closed off |
|---|---|
| Close (X) control | `showCloseButton={false}` on the existing `DialogContent` wrapper (`ui/dialog.tsx:45`) — the prop already exists, defaulting to `true` |
| Backdrop / outside press | `disablePointerDismissal` on `Dialog.Root` (default `false`) |
| Escape key | Controlled `open` that is never lowered: `open={ended}` with an `onOpenChange` that ignores every close request. Base UI's alternative — inspecting `eventDetails.reason === 'escape-key'` and calling `eventDetails.cancel()` — is strictly weaker here, since the dialog must also survive reasons the API may add later |

Page inertness comes free: `modal` defaults to `true`, which traps focus, locks document scroll, and disables pointer interaction outside the popup — exactly FR-023's three requirements, so no manual `inert`/`overflow:hidden` handling is written.

**Rationale**: FR-022/FR-023/FR-024. The card today replaces the whole screen, destroying the context the guest needs most (which order, which step, what they had typed). A controlled-open dialog with no lowering path is the only construction where *every* dismissal route is closed, including ones a library upgrade might introduce.

**Accessibility trade-off, recorded deliberately**: Base UI's docs advise rendering a `Dialog.Close` inside the popup when `modal` is true, so touch screen-reader users have an escape. FR-023 forbids a close control. The "Return to Home Page" button is that escape — it is a real, focusable, single-tap exit inside the trap, so the guidance's *intent* is met even though its literal shape is not. Noted rather than silently diverged from.

**Alternatives considered**: keeping the full-page card and merely restyling it — rejected, it cannot satisfy FR-022's "layout stays rendered behind". A custom fixed-position overlay instead of the dialog primitive — rejected, it would hand-roll the focus trap and scroll lock that `modal` already provides correctly.

## R18. The revision ships as a NEW migration `000013`, not as an edit to `000012`

**Decision**: Add `backend/migrations/000013_master_list_identity_and_flags.{up,down}.sql`. Migration `000012` is left exactly as committed.

**Rationale**: `000012` is committed (`e189e19`) and therefore may already be applied in any environment built from this branch. Editing an applied migration means `schema_migrations` records version 12 as done while the database holds the *old* version-12 shape — a silent divergence no tool detects. The clarification report noted that folding the change into `000012` would avoid creating a UUID column just to convert it; that saving is real but only applies to databases that have not yet run `000012`, and it is not worth a migration history that lies. The conversion cost is one `ALTER TYPE` over a table with, at MVP scale, a few thousand rows.

**Alternatives considered**: rewriting `000012` in place and instructing everyone to re-migrate from scratch — rejected (relies on out-of-band instructions being followed). Squashing `000012`+`000013` before merge — viable only while nothing has been deployed; not assumed here, and it changes nothing about the code below.

## R19. `orders.status_id` with name-preserving SQL — zero Go comparison changes

**Decision**: `orders.status varchar` → `orders.status_id smallint NOT NULL REFERENCES order_statuses(id)`. The **SQL layer** does the name↔id mapping so nothing above it moves:

- **Reads** join the master list and alias the name back to `status`: `SELECT ..., os.name AS status FROM orders o JOIN order_statuses os ON os.id = o.status_id`. Every `sqlc`-generated struct keeps `Status string` carrying `PENDING`/`PAID`/…
- **Writes** resolve by name in the statement: `SET status_id = (SELECT id FROM order_statuses WHERE name = $2)`. Call sites keep passing the name.
- **Filters** likewise: `WHERE o.status_id = (SELECT id FROM order_statuses WHERE name = 'PENDING')`.

**Rationale**: FR-026 requires the change be invisible outside storage. A code survey found the status compared as a Go string in **29 files** — `payment/status.go:12-15` defines `OrderStatusPending = "PENDING"` and friends, and `payment/service.go`, `order/service.go`, `order/admin_handler.go:144`, `notification/service.go:115` all compare against them. Mapping inside SQL means **not one of those comparisons changes**, the DTOs are untouched, and the frontend `OrderStatus` union is untouched. Mapping in Go instead would touch every one of those files for no user-visible gain, and would leave two representations of the same fact in memory.

**Alternatives considered**: exposing `status_id` on the wire — rejected in clarification (FR-026). A Go-side enum↔id map in the repository layer — rejected: it puts a second source of truth beside the master table and still requires every read path to call it.

## R20. The seeded status ids must be deterministic, because a partial index predicate cannot hold a subquery

**Decision**: Seed the order statuses at fixed ids — `1 PENDING, 2 PAID, 3 CANCELLED, 4 EXPIRED` — and rebuild `idx_orders_payment_expiry` (migration `000002:24-26`) as `... WHERE status_id = 1`.

**Rationale**: This is the one place R19's subquery trick cannot reach. PostgreSQL requires a partial index predicate to be immutable, so `WHERE status_id = (SELECT id FROM order_statuses WHERE name='PENDING')` is rejected outright — the predicate must be a literal. That makes the seeded id part of the schema contract rather than an implementation detail, so the migration assigns the ids explicitly (overriding the identity sequence, then resetting it) instead of trusting insertion order. The index is not optional: the expiry sweeper (`payment/service.go`) scans pending orders by `payment_expires_at` on every tick.

**Alternatives considered**: dropping the partial index and using a full index — rejected, it inflates a hot index with rows the sweeper never reads. An `IMMUTABLE` lookup function in the predicate — rejected, lying about immutability corrupts the index the moment a status is renamed.

## R21. `genders.id`/`order_statuses.id` uuid → integer identity; `attendees.gender_id` converted in place

**Decision**: Both master lists move from `uuid PRIMARY KEY DEFAULT gen_random_uuid()` to a generated identity column — `order_statuses.id integer GENERATED ALWAYS AS IDENTITY`, `genders.id smallint GENERATED ALWAYS AS IDENTITY` (the smaller width the supplied DDL asked for via `smallserial`; two rows will never need more). `GENERATED ALWAYS AS IDENTITY` is the modern spelling of `serial` and is what the supplied `Order_Statuses` DDL itself used.

`attendees.gender_id` converts by mapping through the old uuid before it is dropped — add the new integer column, populate it by joining the old uuid to the master row, then drop the uuid column and add the FK. The old and new keys coexist for the length of the migration, which is what makes FR-025's "no reference silently re-bound to a different entry" provable rather than hoped for: every row's new id is derived from its own old id, never from row order.

**Rationale**: FR-025. Doing it as `ALTER COLUMN ... TYPE` with a `USING` clause is impossible — there is no cast from uuid to integer — so the add/populate/drop shape is forced, not chosen.

**Alternatives considered**: truncating and re-seeding the master lists — rejected, it orphans every `attendees.gender_id` and makes the "existing entries keep their names and active state" half of FR-025 unverifiable.

## R22. Audit columns land, but only the `SYSTEM` branch is reachable in this feature

**Decision**: Add `created_by varchar(50) NOT NULL`, `updated_by varchar(50)` to both master lists. The migration seeds and backfills every existing row with the literal `SYSTEM`. No application write path sets these columns in this feature.

**Recorded gap, deliberately not closed here**: FR-028's other branch — "entries an admin creates or edits MUST record that admin's identifier" — has **no code path to attach to**. A survey found the only master-list access in the codebase is the read-only `GET /ticket/genders` (`order/handler.go:45`) plus `ListActiveGenders` (`queries/order.sql:244`); there is no admin CRUD for genders or order statuses at all. Admin identity is available (`admin.RequireAuth` puts claims in the echo context, `admin/middleware.go:34`), so wiring `created_by`/`updated_by` is a small addition *when* that CRUD is built — but building master-list CRUD is a new admin capability, outside both this feature and Principle VI's MVP scope. The columns are therefore correct and populated from day one, and the admin branch activates with the CRUD rather than being retro-fitted.

**Rationale**: FR-028. `varchar(50)` fits a UUID (36 chars) but not an email (up to 255), which is why the clarification chose the admin's UUID as the recorded identifier.

**Alternatives considered**: making `created_by` nullable to avoid the sentinel — rejected, the supplied DDL specifies NOT NULL and a nullable audit column records nothing. Building master-list CRUD as part of this change — rejected as scope creep (see the FR-025 – FR-029 scope note in the spec).

## R23. `packages.status` → `is_active boolean` on both sides of the wire

**Decision**: `packages.status varchar CHECK (status IN ('ACTIVE','INACTIVE'))` → `is_active boolean NOT NULL DEFAULT true`, backfilled `is_active = (status = 'ACTIVE')`. Unlike the order status (R19), the boolean surfaces **on the wire too**: the admin package DTO exchanges `is_active`, and `frontend/components/admin/package-form.tsx:153-154` swaps its ACTIVE/INACTIVE `<option>` pair for a checkbox/toggle, with `frontend/lib/schemas.ts:106` (`z.enum(["ACTIVE","INACTIVE"])` → `z.boolean()`) and `frontend/lib/types.ts:292` following.

Backend touchpoints are the `p.status = 'ACTIVE'` filters in `internal/event/queries/event.sql` (`:162`, `:206`, `:268`) and the package insert/update/select column lists (`:155`, `:209`, `:216`, `:218`, `:226`, `:228`).

**Rationale**: FR-029, clarified explicitly against the order-status treatment: a package flag has two states and no master list behind it, so a boolean carries everything the strings did and is self-describing. `tickets.status` is untouched — its ACTIVE/USED/REVOKED lifecycle is a state machine the gate depends on (`ticket/queries/ticket.sql:27`, `ticket/service.go:174`), and FR-029 excludes it by name.

**Alternatives considered**: keeping the strings on the wire for symmetry with R19 — rejected in clarification. Converting `tickets.status` too — rejected in clarification (it would make "already used" and "revoked" indistinguishable).

## R24. The modal's card follows Figma `293-3`: one action, and copy that no longer promises a repeat

**Decision**: The dialog's contents are rebuilt to the Figma node `293-3` card (384×304): alert icon in a tinted circle, `Time's Up` heading, two-line body, and **one** full-width filled button, `Return to Home Page` → `/`. The `Repeat Order` link to `/events/{slug}/tickets` that the full-page card carried is **deleted**, and because nothing offers a repeat any more the body copy loses its "Please repeat your order." sentence — expiry reads "Sorry, your payment time has expired. The tickets have been released back on sale."; cancellation keeps its existing wording. The CANCELLED variant differs only in heading and body; the single button and every unclosability setting are identical.

**What `ended` means** (the `open` value from R17): `status === "EXPIRED" || status === "CANCELLED" || locallyExpired`, where `locallyExpired` is the existing client countdown latch on the payment screen (`checkout/page.tsx:100-103`). That third term is what makes the modal open the instant the countdown reaches zero rather than one poll later, and the term is self-healing — when the server's `EXPIRED` lands, `data.payment` goes null, `locallyExpired` falls back to false, and the first term takes over with no flicker. The now-redundant inline `StatusAlert` ("This payment code has expired", `checkout/page.tsx:211-213`) is deleted, and with it the `showPayment` false-branch: when `ended`, the QR panel and the status-checker card are simply not rendered, so nothing scannable sits behind the dialog. The countdown banner and the order summary stay, which is what keeps the screen recognisably the one the guest was on.

**Rationale**: FR-024 and the two clarifications of 2026-08-07. The design file is the visual authority, and it shows a single action; keeping `Repeat Order` alongside it would have been a silent deviation, while keeping the "repeat your order" sentence with no repeat button would promise an action the dialog does not offer.

**Alternatives considered**: keeping both buttons inside the Figma card — rejected in clarification (the design shows one). Pointing the single button at the event's ticket selection and relabelling it — rejected in the same clarification. Leaving the inline expired notice in place beneath the modal — rejected, it is the second message for one event that FR-022 exists to remove.

---

## Known issues flagged, out of scope

- `lib/booking-stage.ts:28` maps the order route to the "Payment" rail step for both phases (the "Registration" step never lights up). Pre-existing; unchanged.
- Vitest **is installed** (`frontend/node_modules/.bin/vitest` → vitest 4.1.10, resolved in `package-lock.json`), but `package.json` has no `test` script — invoke the binary directly per the toolchain steps in project memory (`frontend-toolchain-invocation.md`; node is not on PATH).
- The gender CHECK-vs-master-list drift previously listed here moved **into scope** as FR-018/R15 (clarification 2026-08-06).

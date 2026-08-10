# Research: End-to-End Guest Purchase Flow

> **Superseded by [spec 012](../012-manjo-payment-gateway/spec.md) (2026-08-10).**
> The payment gateway changed from Midtrans to Manjo, and three capabilities described
> below were withdrawn rather than reimplemented: the mid-window QR refresh
> (`POST /ticket/checkout/:order_id/refresh-qr` and `qr_refresh_after_seconds`), the
> guest's status-check button (`POST /ticket/order/:order_id/payment/refresh`), and
> the provider status query that backed it. The payment deadline is now the gateway's
> own (`qr_ea`), adopted verbatim, so one order gets one code for one window. Read the
> sections below as history.

**Feature**: 008-e2e-purchase-flow · **Date**: 2026-08-05

Every decision below resolves an unknown from plan.md's Technical Context or a
tension between the user input and the existing codebase/constitution.

---

## R1. Two-phase order flow (book at T&C, pay later)

**Decision**: Split the current single checkout into (paths per clarification
2026-08-05):

1. **Book** — `POST /ticket/book`, fired on T&C "Agree": runs today's TX1
   exactly (validate → expand packages → aggregate demand → sorted quota
   deduction → `orders` + `order_items` + `attendees`), but **stops there** — no
   gateway call, no payment fields, no buyer/visitor details. Order is created
   `PENDING` with `payment_expires_at = now() + BookingHold (1h)`; attendee
   rows are created as **empty slots** (ticket-type-bound, details null).
2. **Record agreement** — `POST /ticket/terms-condition/:order_id`, fired
   immediately after Book on the same Agree click: stamps `terms_agreed_at` +
   `event_terms_id`. Checkout refuses unagreed orders, so a failure between
   the pair cannot reach payment; the sweeper reclaims abandoned holds.
3. **Checkout** — `POST /ticket/checkout/:order_id` carries the buyer +
   visitor forms **and** starts payment in one call (clarification Option B):
   validates and saves details (local TX), calls `Gateway.CreateTransaction`
   (outside any TX, per Constitution IV), then TX2 stamps payment fields and
   moves `payment_expires_at` to `now() + PaymentWindow (14m)`. Details are
   not persisted before this call — a pre-checkout revisit shows empty forms.

**Rationale**: Reuses the battle-tested reservation TX verbatim (oversell
protection, deterministic lock order, package expansion). The gateway call
moves even further from the quota TX than the constitution requires. The
existing sweeper needs zero changes: it already expires `PENDING` orders past
`payment_expires_at` and restores quota — the 1-hour hold and the 14-minute
payment window are just two different values of the same column.

**Alternatives considered**:
- *Create order only at payment start (status quo)* — rejected: violates the
  core requirement (order persisted + seats locked at T&C agreement).
- *Soft reservation table separate from orders* — rejected: second inventory
  ledger, more states to reconcile, sweeper duplication.
- *New `AWAITING_PAYMENT` status* — rejected: `PENDING` + the two deadline
  values already distinguish the phases; payment fields being null tells the
  client which screen to show. Fewer states = fewer webhook/sweeper branches.

## R2. 14-minute window vs the provider's 15-minute QRIS floor

**Decision**: Keep the gateway-facing expiry at the configured
`PAYMENT_EXPIRY` (≥ 15m floor, unchanged validation). Introduce a separate
server-owned `PAYMENT_WINDOW` (default **14m**) written to
`payment_expires_at` at payment start. The countdown, the sweeper, and the
expired state all key on the server deadline, so the guest experiences a
14-minute window while the QR at the provider stays technically valid a little
longer.

**Rationale**: `minPaymentExpiry` exists because the provider rejects shorter
QRIS expiries (`backend/pkg/config/config.go:13`). A server deadline shorter
than the gateway validity is safe: the sweeper expires the order and restores
quota; a payment that lands in the ~1-minute gap hits the existing
webhook-after-expiry path and is flagged for manual reconciliation (spec edge
case, no automatic refunds per Constitution VI).

**Alternatives considered**:
- *Lower the floor to 14m* — rejected: the floor mirrors provider docs;
  a rejected gateway call at runtime is worse than a 1-minute overlap.
- *Set both to 15m* — rejected: user explicitly specified 14 minutes.

## R3. 7-minute QR refresh mechanism

**Decision**: `POST /ticket/checkout/:order_id/refresh-qr` re-issues the QR:
the backend calls `Gateway.CreateTransaction` again under a **suffixed
reference** (`{orderNumber}-R1`, `-R2`, …) with `gross_amount` unchanged,
stores the new `payment_qr_string`/`payment_url` (TX2 pattern), and returns the
new QR payload. `payment_expires_at` is **not** touched — the 14-minute
deadline never extends (FR-015). The previous provider transaction is left to
expire on its own. The webhook handler matches orders by stripping the `-Rn`
suffix from the provider's `order_id`. The frontend triggers this once at the
7-minute mark; failure keeps the old QR visible with a retry affordance.

**Rationale**: Midtrans treats `order_id` as unique per transaction — the same
id cannot get a second QR — so a fresh reference is the standard re-issue
pattern. Not cancelling the old transaction keeps the refresh single-round-trip
and harmless: two live QRs both credit the same order, and the idempotent
webhook accepts whichever is paid first.

**Alternatives considered**:
- *Cancel old transaction then create new* — rejected: two provider calls, and
  a cancel failure would strand the guest with no QR at all.
- *Extend expiry of existing transaction* — rejected: QRIS transactions are
  immutable at the provider; no such API.

## R4. Live payment status: SSE with polling fallback

**Decision**: Add `GET /ticket/checkout/:order_id/status` —
Server-Sent Events from `internal/payment`. Implementation: an in-process
publish/subscribe hub (`map[orderNumber]chan status` guarded by a mutex);
the webhook handler and the sweeper publish transitions; the SSE handler
subscribes, sends an initial snapshot, then pushes events, with a periodic
keep-alive comment and a DB re-read every 15s as drift guard. The frontend
`EventSource` hook falls back to the existing 3-second TanStack Query polling
when the stream errors (spec edge case: dropped connection).

**Rationale**: User explicitly requested SSE. Single-instance MVP makes an
in-process hub sufficient (no Redis — barred by Constitution VI). Keeping the
polling path as fallback preserves today's behavior under proxies that buffer
streaming responses.

**Alternatives considered**:
- *Polling only (status quo)* — rejected: explicit user requirement.
- *WebSockets* — rejected: bidirectional transport for a unidirectional need;
  SSE is plain HTTP and works with Echo's streaming response directly.
- *LISTEN/NOTIFY via PostgreSQL* — deferred: adds pgx listener lifecycle for
  no benefit while the process is single-instance.

## R5. WYSIWYG editor and HTML safety

**Decision**: **TipTap** (headless, React 19 compatible; **MIT open-source
core + StarterKit only — no paid Tiptap Cloud/Pro extensions**, toolbar styled
from the existing shadcn/Base UI primitives) in the admin app for
event description and T&C authoring, storing **HTML strings**. The backend
sanitizes on **write** with **bluemonday** `UGCPolicy()` (plus class-attribute
allowance for text alignment) in the event domain service, so the database only
ever contains clean HTML. Guest pages render it via `dangerouslySetInnerHTML`
inside a `.prose`-styled container; no client-side sanitizer needed because the
API is the only writer.

**Rationale**: TipTap is the current default headless editor for React and
plays well with Tailwind/shadcn projects; storing sanitized HTML keeps the
guest render path dependency-free. Server-side sanitization is the only
non-bypassable point (SC-007).

**Alternatives considered**:
- *Lexical* — viable, heavier integration surface for a two-field CMS.
- *Markdown + textarea* — rejected: fails "WYSIWYG" requirement for
  non-technical admins.
- *Store editor JSON, render server-side* — rejected: adds a render pipeline
  for no MVP benefit; HTML is the natural interchange here.

## R6. Attendee slots with deferred details

**Decision**: Booking creates one `attendees` row per constituent unit (exact
same expansion as today) with `name`/`email` **made nullable** (migration) and
new nullable `phone`, `dob`, `gender` columns. `POST /ticket/checkout/:order_id`
carries and saves them (Option B), refusing with field errors while any slot
is incomplete. Ticket
issuance (on PAID) is unchanged — by then slots are guaranteed complete.
`gender` is a `VARCHAR CHECK (gender IN ('MALE','FEMALE'))` — no `genders`
lookup table.

**Rationale**: Slot rows must exist at booking time because the
package-expansion math that sizes them lives in the booking TX. Nullable
columns + a service-level completeness gate is the smallest change that keeps
one-pass-per-attendee semantics. A lookup table for two static values is
schema noise (user's `Genders` table treated as advisory).

**Alternatives considered**:
- *Create attendees only at payment start* — rejected: re-running expansion
  outside the quota TX invites drift between what was reserved and what is
  registered.
- *JSON details column* — rejected: breaks existing ticket/PDF queries that
  read `attendees.name`/`email`.

## R7. Event content blocks (activities, guest stars, guidelines)

**Decision**: Three new event-domain tables — `event_activities` (title,
description, icon, position), `event_guest_stars` (name, position),
`event_guidelines` (description, icon, position) — each `event_id FK ON DELETE
CASCADE`, admin CRUD nested under `/admin/events/:id/…`, and embedded in the
public `GET /event/:id` response. `icon` is a **named icon key** from a
fixed, documented set rendered by `lucide-react` on the client (no uploads —
Constitution: no object storage). Ordering via integer `position`.

**Rationale**: Mirrors the user's proposed `Activities`/`Guest_Star`/
`Guidelines` tables mapped onto the existing UUID/naming conventions of
SCHEMA.md. CASCADE (not RESTRICT) because content blocks are presentation
data — unlike orders they must not block event deletion.

**Alternatives considered**:
- *One polymorphic `event_content_blocks` table* — rejected: three flat tables
  are simpler for sqlc and admin forms; no shared behavior beyond `event_id`.
- *Icon as URL* — rejected: implies hosting/upload; icon keys need none.

## R8. Terms & Conditions storage and agreement recording

**Decision**: New `event_terms` table — `event_id UNIQUE` (one live terms per
event), `content` (sanitized HTML), timestamps. Public read joins into the
terms dialog via `GET /api/v1/ticket/terms-condition/:event_id`. Agreement is
recorded by `POST /ticket/terms-condition/:order_id` (fired with book on the
same Agree click) onto `orders`: `terms_agreed_at TIMESTAMPTZ` and
`event_terms_id UUID` (FK); checkout refuses orders without it. Booking is
refused (`409`) when the event has no authored terms (spec edge case). The static `frontend/lib/terms.ts` content
is retired; the seed/migration backfills each existing event's terms from that
copy so the dialog never goes blank.

**Rationale**: Matches the spec's "one live T&C per event" assumption; the
`(terms_agreed_at, event_terms_id)` pair is the durable agreement record
(FR-008) without a versioned-history table the MVP doesn't need.

**Alternatives considered**:
- *Snapshot full terms HTML onto the order* — rejected for MVP: heavy row
  bloat; the FK + timestamp satisfies FR-008. Revisit if legal requires
  verbatim snapshots.
- *Global (non-per-event) terms* — rejected: user requires per-event CMS terms.

## R9. Route restructure (frontend)

**Decision**:
- `/` — event grid + ticket-verification aside (new components; grid logic
  lifted from `app/(public)/events/page.tsx`; verify card wraps the existing
  ticket-lookup flow). `/events` redirects to `/`.
- `/events/[slug]` — **event detail page** (CMS content, Figma 4-5) with "Buy
  Ticket" → `/events/[slug]/tickets`.
- `/events/[slug]/tickets` — ticket selection (current `events/[slug]/page.tsx`
  content moves here; Figma 12-1523 et al.).
- Terms dialog: on Agree it now `POST`s the booking and routes to
  `/events/[slug]/orders/[orderNumber]` (existing route from 007) instead of a
  client-side selection-encoding checkout URL. The old
  `/events/[slug]/checkout` page is removed; its form components are reused on
  the order page.
- Payment and done screens stay at `orders/[orderNumber]` / `…/done` inside the
  `events/[slug]` layout, keeping the countdown visible during payment (FR-019).

**Rationale**: Minimal moves that satisfy the design; the 007 event-scoped
layout already provides the shared countdown/progress chrome the payment screen
must keep.

## R10. Response envelope and endpoint naming from the user input

**Decision** (revised — clarification 2026-08-05): Adopt the user's endpoint
list **verbatim** for the guest surface: `GET /event`, `GET /event/:id`,
`GET /ticket/:event_id`, `GET /packages/:event_id`, `POST /ticket/book`,
`GET /ticket/terms-condition/:event_id`,
`POST /ticket/terms-condition/:order_id`, `POST /ticket/checkout/:order_id`,
`GET /ticket/checkout/:order_id/status` (SSE), `POST /ticket/resend-email` —
plus supporting reads (`GET /ticket/order/:order_id`, `…/qris.png`) and
`POST /ticket/checkout/:order_id/refresh-qr` in the same namespace. Old guest
paths are removed, not aliased; the frontend API client migrates in the same
change. `:id`/`:event_id` = event slug; `:order_id` = public order number.
**Envelope + casing** (second clarification, Option A): every endpoint — guest
and admin — wraps its body in `{code: number, message: string, data: T}` with
snake_case properties; success is `200000`, errors are `HTTP×1000+sub-code`
(registry in contracts/api.md). Implemented centrally: an envelope writer in
`pkg/httpx`, the `apperr` error handler rewritten to emit the envelope with
numeric codes, DTO json tags retagged snake_case, and both guest and admin
frontend API clients migrated in the same change. SSE frames and binary
responses (qris.png) stay unenveloped; the provider webhook answers per
provider expectations. Admin paths unchanged.

**Rationale**: The user reaffirmed the endpoint list after the original
uniformity-first decision, then extended it to the envelope and snake_case
across the whole API — their call; applying it everywhere avoids a permanently
split response style. Doc-sync note: the constitution's
Principle IV names `POST /api/v1/checkout` literally; the transactional rule
transfers to `POST /ticket/book` + `POST /ticket/checkout/:order_id`
unchanged, and the constitution wording should be patch-amended when this
ships (PATCH bump, no semantic change).

## R11. Email receipt + e-ticket (Figma 251-2)

**Decision**: Keep the existing fulfillment pipeline (PAID → PDF via gofpdf +
QR via go-qrcode → SMTP, `email_sent` after delivery, public resend
rate-limited per order). Work is limited to restyling the HTML email body and
PDF layout to the JIVE design.

**Rationale**: Pipeline already satisfies FR-021/FR-022 and Constitution's
critical data-flow rules; only presentation changes.

## R12. Design references at implementation time

**Decision**: Figma node IDs are recorded in spec.md's Design References table.
Implementation tasks must fetch them through the Figma MCP
(`figma-design-to-code` skill first) rather than baking measurements into this
plan.

**Rationale**: Pixel data belongs next to the component work; plan stays
stable if design frames are updated.

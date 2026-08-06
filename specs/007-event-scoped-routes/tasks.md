---

description: "Task list for event-scoped guest navigation"
---

# Tasks: Event-Scoped Guest Navigation

**Input**: Design documents from `/specs/007-event-scoped-routes/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/](./contracts/)

> **Revision note**: regenerated after the 2026-08-04 `/speckit-clarify` session re-scoped
> the feature. The previous list's US3 (event-scoped ticket screens) and US4 (code
> resolver) are **gone**, along with every `internal/ticket/` task. A progress rail, a
> confirmation screen, and a public resend endpoint are **in**.

**Tests**: Test tasks ARE included. Justification: the ownership guard (FR-013) must fail
closed and the resend endpoint must not disclose order existence (FR-026) — both look
identical to a working implementation when they silently stop working. SC-001 (countdown
survives navigation) and FR-008 (no placeholder countdown) are invisible in a passing
manual click-through unless deliberately probed. The repo already colocates `*.test.tsx`
beside pages and gates the backend on `backend/cmd/api/architecture_test.go`.

**Organization**: Grouped by user story so each is independently implementable and testable.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to (US1–US3)
- Exact file paths included in every task

## Path Conventions

Web app, per [plan.md](./plan.md): `backend/` (Go modular monolith) and `frontend/`
(Next.js App Router). Frontend routes live under `frontend/app/(public)/`.

## Starting-State Note

The working tree is mid-migration and currently **broken**:
`frontend/app/(public)/checkout/page.tsx` and
`frontend/app/(public)/orders/[orderNumber]/page.tsx` are deleted, their replacements exist
under `events/[slug]/`, but `encodeSelection()` still emits `/checkout?slug=…` — a path
with no page. Phase 2 repairs that before any story work.

**Do not restore** `frontend/app/(public)/orders/[orderNumber]/` — its deletion is now the
intended end state (FR-028).

**Do not touch** `backend/internal/ticket/` or `frontend/app/(public)/tickets/` — the
re-scope removed all work there (FR-027).

---

## Phase 1: Setup

- [X] T001 Record the current test baseline: run `cd backend && go test ./...` and `cd frontend && npx vitest run`, noting any pre-existing failures in the PR description so new breakage is distinguishable
- [X] T002 [P] Confirm `sqlc` output is reproducible by running `cd backend && sqlc generate && git diff --exit-code internal/` — this feature adds no query, so this must stay clean for the whole branch

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Repair the half-finished checkout migration so the journey is walkable. Nothing user-visible ships here — every story's independent test walks this path.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete.

- [X] T003 Change `encodeSelection(slug, lines)` in `frontend/lib/selection.ts` to return `/events/${encodeURIComponent(slug)}/checkout?${params}` and stop writing `slug` into the query string; leave `parseSelection()` untouched since it reads only `t=` and `p=`
- [X] T004 [P] Update the `encodeSelection` cases in `frontend/lib/selection.test.ts` to assert the event-scoped path and the absence of a `slug` query parameter (create the file if selection has no test yet)
- [X] T005 Change `frontend/app/(public)/events/[slug]/checkout/page.tsx` to take `params: Promise<{ slug: string }>` and read the slug from the route segment via `use(params)`, removing the `useSearchParams().get("slug")` read at line 40; `useSearchParams` keeps carrying only the `t=`/`p=` pairs
- [X] T006 Verify by hand: from `/events/<slug>` select a ticket, press continue, and confirm you land on `/events/<slug>/checkout?t=…` with the summary populated and no `?slug=` present

**Checkpoint**: The selection → checkout hop works again. User stories can begin.

---

## Phase 3: User Story 1 - Shared framing follows the guest (Priority: P1) 🎯 MVP

**Goal**: An `events/[slug]` layout that fetches the event once, gates unknown events, and renders both the start-date countdown and the four-step progress rail — persisting across every nested screen.

**Independent Test**: Walk `/events/<slug>` → checkout → order and confirm the countdown keeps ticking without resetting while the rail advances Booking → Registration → Payment; then open `/events/does-not-exist` and confirm nothing nested renders.

### Tests for User Story 1

> Write these first and confirm they fail before implementing.

- [X] T007 [P] [US1] Create `frontend/lib/booking-stage.test.ts` asserting the pathname → stage mapping in [contracts/routes.md](./contracts/routes.md), including that `/events/x/orders/N/done` resolves to `Done` and not `Payment` (the order route is a prefix of the done route)
- [X] T008 [P] [US1] Create `frontend/components/event/event-frame.test.tsx` covering: no countdown while the event query is pending (FR-008), the rail rendered anyway during pending, children rendered during pending, children suppressed with an "event not found" message on a 404 (FR-007), and countdown plus rail plus children once resolved
- [X] T009 [P] [US1] Create `frontend/components/booking/sales-countdown.test.tsx` covering: nothing rendered for a null start date, nothing for a past start date (FR-003), day/hour/minute/second units for a future date, and a label naming what it counts down to (FR-017)

### Implementation for User Story 1

- [X] T010 [P] [US1] Create `frontend/lib/booking-stage.ts` exporting a pure `bookingStageFromPathname(pathname)` returning a `BookingStep` from the existing `BOOKING_STEPS` tuple, matching `/done` before the bare order route (FR-005)
- [X] T011 [US1] Create `frontend/components/event/event-frame.tsx` as a `"use client"` component taking `{ slug, children }`, calling the existing `useEventBySlug(slug)`, deriving the stage with `usePathname()` + T010, and rendering the three states from [data-model.md](./data-model.md): pending → rail + children, error/absent → not-found panel with a link to `/events` and **no** children, resolved → countdown + rail + children
- [X] T012 [US1] Create `frontend/app/(public)/events/[slug]/layout.tsx` as a **server** component (not `"use client"`) that awaits `params: Promise<{ slug: string }>` and returns `<EventFrame slug={slug}>{children}</EventFrame>` — see [research.md](./research.md) R-001 for why the client boundary sits in the frame
- [X] T013 [US1] Remove both the `<SalesCountdown>` and the `<BookingSteps current="Booking" />` lines and their now-unused imports from `frontend/app/(public)/events/[slug]/page.tsx` so neither renders twice (FR-009)
- [X] T014 [P] [US1] Replace the `"The event will take place on"` heading in `frontend/components/booking/sales-countdown.tsx` with a label naming the duration — "Event starts in" — keeping the existing section `aria-label` (FR-017)
- [X] T015 [P] [US1] Change completed stages in `frontend/components/booking/booking-steps.tsx` to render a checkmark instead of their number, keeping the current stage's number, per the approved design; the existing `isDone`/`isCurrent`/`isReached` logic already distinguishes them so only the rendered glyph changes
- [ ] T016 [US1] Run [quickstart.md](./quickstart.md) scenarios 1 and 2: countdown continuous across selection → checkout → payment, rail advancing correctly, past-dated event showing no countdown, throttled load showing no `00:00:00` placeholder, and unknown event suppressing all nested content

**Checkpoint**: Framing lives in the layout and persists across the journey. The rail's stages 2–4 are visible to a guest for the first time.

---

## Phase 4: User Story 2 - The journey is addressed within its event (Priority: P1)

**Goal**: Checkout lands on an event-scoped order address, that screen refuses an order belonging to another event, and every in-journey link keeps the guest in their event.

**Independent Test**: Complete a checkout, confirm the confirmation URL is event-scoped and reloads; then open the same order number under a different event's slug and confirm a not-found message with no order details and no redirect.

### Tests for User Story 2

- [X] T017 [P] [US2] Update `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` for the new `OrderView` signature (it currently renders `<OrderView orderNumber={…} />` with no slug) and add a case asserting that when `data.event.slug` differs from the passed slug the screen shows the wrong-event message, renders **none** of the order's details, and does **not** navigate (FR-013)

### Implementation for User Story 2

- [X] T018 [US2] Add an `eventSlug` prop to `OrderView` in `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx`, unwrap both segments in the default export via `use(params)` typed as `Promise<{ slug: string; orderNumber: string }>`, and render a "not found for this event" panel when `data.event.slug !== eventSlug` — failing closed, never redirecting to the owning event
- [X] T019 [US2] Point the error-branch link at line 62 of the same file at `/events/${eventSlug}` instead of `/events` so a guest stays inside their event (FR-014)
- [X] T020 [P] [US2] Change the "Browse events" link at line 36 of `frontend/components/order/payment-status-card.tsx` to use the `eventSlug` prop the component already receives, matching the correct behaviour already at line 61 (FR-014)
- [X] T021 [US2] Change the post-checkout redirect at line 106 of `frontend/app/(public)/events/[slug]/checkout/page.tsx` from `/orders/${order_number}` to `/events/${slug}/orders/${order_number}` (FR-012)
- [X] T022 [P] [US2] Relabel the payment timer in `frontend/components/order/expiry-countdown.tsx` so it names the payment deadline — "Pay within" rather than "This code expires in" — keeping the existing `aria-live` region and the `text-destructive` treatment under 60 seconds (FR-017)
- [X] T023 [US2] Run [quickstart.md](./quickstart.md) scenarios 3, 4, and 5: event-scoped checkout and order URLs, both countdowns simultaneously visible and unmistakably labelled, and the ownership guard refusing a wrong-event URL on the order route

**Checkpoint**: The journey is event-scoped and guarded, and the two countdowns coexist legibly. US1 + US2 both work.

---

## Phase 5: User Story 3 - The journey ends on a confirmation screen (Priority: P2)

**Goal**: A distinct `done` route showing the receipt, reached automatically by any settled order, with a working guest-facing resend.

**Independent Test**: Pay in the sandbox and confirm you arrive at a distinct confirmation address showing the order number, total paid, and purchased items; then press resend and confirm the email arrives again.

### Backend: the public resend endpoint

Per [contracts/resend-email.md](./contracts/resend-email.md) and [research.md](./research.md) R-006. Sequential — each layer feeds the next.

- [X] T024 [US3] Add `OrderIDByNumber(ctx context.Context, orderNumber string) (uuid.UUID, error)` to the `OrderProvider` interface in `backend/internal/notification/service.go` — the notification domain declaring its own need rather than importing the order domain (Constitution Principle II)
- [X] T025 [US3] Implement `OrderIDByNumber` on `notificationOrderAdapter` in `backend/cmd/api/adapters.go`, delegating to the existing `order.Repository.GetOrderByNumber` (`internal/order/repository.go:293`) and mapping a missing row to the same not-found error `OrderForDelivery` already returns at line 184
- [X] T026 [US3] Add the public resend response shape to `backend/internal/notification/dto.go` — a single generic `message` field that discloses neither the recipient nor whether the order exists, distinct from the admin `ResendResponse` which keeps `sent_to`
- [X] T027 [US3] Add `RegisterPublicRoutes` and a `resendPublic` handler to `backend/internal/notification/handler.go` that resolves the order number via `OrderIDByNumber`, calls `SendTicketEmail`, and returns `202 Accepted` with the generic body — swallowing a not-found into the identical `202` so existence is never disclosed (FR-026), and accepting no request body so no caller-supplied address can reach the mailer (FR-024)
- [X] T028 [US3] Mount the public route in `backend/cmd/api/main.go` with Echo's `middleware.RateLimiterWithConfig` at 1 request per 60 seconds, using an `IdentifierExtractor` that returns the `:orderNumber` path parameter so the bucket is per order, not per caller (FR-025). Attach it to **this route only** — never the API group, or the 3-second order poll would be throttled
- [X] T029 [P] [US3] Extend `backend/internal/notification/handler_test.go` with the seven obligations in [contracts/resend-email.md](./contracts/resend-email.md): paid order sends once; unknown order returns a byte-identical `202` and sends nothing; non-paid order sends nothing; a second call inside the window returns `429`; two order numbers do not share a bucket; a body naming another address changes no destination; the admin endpoint is unchanged
- [X] T030 [P] [US3] Add `PublicResendResponse` to `frontend/lib/types.ts` and a `usePublicResendEmail(orderNumber)` mutation to `frontend/lib/queries.ts` posting to `/orders/${orderNumber}/resend-email`, surfacing a `429` as the "please wait" message

### Frontend: the confirmation screen

- [X] T031 [P] [US3] Create `frontend/app/(public)/events/[slug]/orders/[orderNumber]/done/page.test.tsx` covering: a paid order renders the event name, order number, total paid, and every item with its quantity (FR-019); an expired order renders the expired outcome instead of a success message (FR-020); the ownership guard refuses an order from another event (FR-013); and the resend button reports success and the rate-limited case
- [X] T032 [P] [US3] Create `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` cases — or extend T017's file — asserting that a settled status calls `router.replace` with the `done` path and that a `PENDING` order does not (FR-018)
- [X] T033 [US3] Add the settled-status forward to `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx`: when `isFinalOrderStatus(data.status)` (the helper already exists in `frontend/lib/queries.ts` and already backs the polling stop condition), `router.replace` to `/events/${eventSlug}/orders/${orderNumber}/done` — covering both an order that settles while watched and one already settled on open (FR-018)
- [X] T034 [US3] Create `frontend/components/order/order-confirmation.tsx` implementing the approved Figma design: success icon and heading naming the event, a receipt panel with order number and total paid, the itemised list with per-item quantities, the email-sent notice with the resend button, and a "Back to Home" action to `/` (FR-019, FR-021, FR-022)
- [X] T035 [US3] Create `frontend/app/(public)/events/[slug]/orders/[orderNumber]/done/page.tsx` unwrapping both segments, reading the order through the same `useOrderDetail` query the payment screen already warmed, applying the ownership guard (FR-013), and rendering `OrderConfirmation` for `PAID` or the expired/cancelled outcome with a link back to `/events/${slug}` otherwise (FR-016, FR-020)
- [X] T036 [US3] Move the expired/cancelled treatment out of the payment screen: `PaymentStatusCard` now belongs to the confirmation screen, since a settled order never renders the payment screen any more — remove its use from `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx` and keep the payment screen to pending orders only
- [ ] T037 [US3] Run [quickstart.md](./quickstart.md) scenarios 6 and 7: automatic forward on payment, receipt contents, back button not bouncing, re-opening a settled order landing on done, resend working, expired order reporting its outcome, plus the full `curl` sequence for rate limiting and non-disclosure

**Checkpoint**: The journey has an ending. All three stories work independently.

---

## Phase 6: Polish & Cross-Cutting Concerns

- [X] T038 [P] Run `cd frontend && npx tsc --noEmit && npm run lint` and fix any fallout from the changed `OrderView` signature and the new mutation
- [X] T039 [P] Run `cd backend && go test ./...` including `backend/cmd/api/architecture_test.go`, confirming the domain-isolation, DTO-isolation, and closed-domain-set assertions still pass after the notification change
- [X] T040 [P] Run `cd frontend && npx vitest run` and confirm the suite is green against the T001 baseline
- [X] T041 Run the grep guards from [quickstart.md](./quickstart.md): no `app/(public)/orders` directory exists, nothing links to a top-level `/orders` address, and `git diff --stat -- backend/internal/ticket frontend/app/\(public\)/tickets` is empty
- [ ] T042 Run [quickstart.md](./quickstart.md) scenario 8 (ticket lookup untouched — including that `GET /api/v1/tickets/:code` still has **no** `event_slug`) and scenario 9 (polling, manual refresh, expiry, PDF delivery, admin screens all unregressed — FR-015)
- [X] T043 Run the constitution spot-check from [quickstart.md](./quickstart.md): the diff touches only `frontend/**`, `backend/internal/notification/**`, and `backend/cmd/api/**`; `migrations/`, `SCHEMA.md`, and every `queries/*.sql` are unchanged; `sqlc generate` produces no diff; no dependency was added
- [X] T044 Confirm `git status` shows no stale deleted-but-unreplaced routes, and that `frontend/app/(public)/orders/` remains absent by design

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies
- **Foundational (Phase 2)**: depends on Setup — **blocks all user stories**, because every story's independent test walks the purchase journey and that journey is currently broken
- **US1 (Phase 3)**: depends on Foundational only
- **US2 (Phase 4)**: depends on Foundational only
- **US3 (Phase 5)**: depends on Foundational, and its frontend half (T031–T037) depends on **US2's** order-screen work — the settled-status forward is added to the same file US2 restructures
- **Polish (Phase 6)**: depends on all stories

### Story Dependencies

US1 and US2 are mutually independent — US1 touches the layout, frame, and countdown; US2 touches the order screen and checkout redirect. US3's backend half (T024–T030) is independent of everything and can start immediately after Phase 2; its frontend half needs US2 landed first.

### Within Each Story

- Tests before implementation, confirmed failing first
- Pure helper before its consumer (T010 before T011)
- Backend interface → adapter → dto → handler → route mounting, in that order
- Manual quickstart validation last, as the story's checkpoint

### Parallel Opportunities

- T004 alongside T003 (different files)
- All of US1's tests (T007, T008, T009) together; then T010, T014, T015 are three different files
- T020 and T022 parallelise with T018/T019 in US2 — different files
- US3's backend chain (T024→T028) runs entirely in parallel with US1 and US2, since it shares no file with either
- T029 and T030 together once the backend chain lands — one Go, one TypeScript
- All of Phase 6's `[P]` checks together

---

## Parallel Example: User Story 1

```bash
# Tests first, together:
Task: "Create frontend/lib/booking-stage.test.ts for the pathname → stage mapping"
Task: "Create frontend/components/event/event-frame.test.tsx for the three framing states"
Task: "Create frontend/components/booking/sales-countdown.test.tsx for the hide/show rules"

# Then three independent files:
Task: "Create the pure stage helper in frontend/lib/booking-stage.ts"
Task: "Relabel the event countdown in frontend/components/booking/sales-countdown.tsx"
Task: "Render checkmarks for completed stages in frontend/components/booking/booking-steps.tsx"
```

---

## Implementation Strategy

### MVP (Phases 1–3)

Setup → Foundational → US1. The smallest increment worth showing: the purchase journey
works again, the countdown follows the guest, and the progress rail finally shows all four
stages. Stop and validate against quickstart scenarios 1 and 2.

### Incremental Delivery

1. Phases 1–2 → the journey is walkable again (no user-visible change, nothing broken)
2. + US1 → framing persists across the journey — **demo-able MVP**
3. + US2 → the journey is event-scoped and guarded; both countdowns legible
4. + US3 → the journey has an ending, with a receipt and a working resend
5. Phase 6 → full validation

Each step leaves the app shippable.

### Parallel Team Strategy

After Phase 2: A takes US1 (layout, frame, rail, countdown), B takes US2 (order screen,
checkout redirect, labels), C takes US3's backend chain immediately — it shares no file
with either. When B finishes, C or B picks up US3's frontend half.

---

## Notes

- `[P]` means different files with no incomplete dependency — not "safe to rush"
- The ownership guard must fail closed. Redirecting a wrong-event URL to its true owner would make the URL work and defeat the isolation this feature exists to create
- `router.replace`, never `push`, for the settled-order forward — otherwise the back button bounces the guest between payment and confirmation
- The resend limiter goes on the resend route only. On the API group it would throttle the 3-second order poll
- The rate limiter's key is the order number, not the caller's IP — the protected resource is the buyer's inbox
- `backend/internal/ticket/` and `frontend/app/(public)/tickets/` are out of scope. If a task tempts you to edit either, stop and re-read [research.md](./research.md)'s "Withdrawn decisions"
- Commit after each task or logical group; stop at any checkpoint to validate a story independently

> **Manual validation outstanding**: T016, T037, and T042 are interactive walkthroughs (browser navigation, Midtrans sandbox payment, SMTP delivery) that were not run in the implementing session. Everything they cover has automated equivalents that pass — see the completion report — but the browser and sandbox-payment paths still need a human pass before release.

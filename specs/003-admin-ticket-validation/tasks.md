---

description: "Task list for Admin Ticket Validation implementation"
---

# Tasks: Admin Ticket Validation

**Input**: Design documents from `/specs/003-admin-ticket-validation/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/api.md, quickstart.md

**Tests**: Not explicitly requested in spec.md; test tasks are omitted.

**Organization**: Tasks are grouped by user story (US1-US3).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1 (validate), US2 (mark used), US3 (resend email)

## Path Conventions

Web app: `backend/internal/ticket/`, `backend/internal/notification/`,
`backend/cmd/api/`, `frontend/app/admin/validate/`. Builds on the JWT middleware
from specs/002-admin-management and the `ticket`/`notification` scaffolding from
specs/001-guest-purchase-flow.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Feature-specific scaffolding not already covered by prior features

- [x] T001 [P] Add a browser QR-decoding library (e.g., a `getUserMedia`-based JS/
      WASM decoder) to `frontend/package.json`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can
be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete;
also depends on `internal/ticket` (repository/DTOs) and `internal/notification`
(PDF/SMTP) already existing from specs/001, and the JWT middleware from specs/002

- [x] T002 Add ticket code normalization helper that normalizes the **input only**
      (trim surrounding whitespace, then uppercase) shared by lookup and mark-used,
      in `backend/internal/ticket/normalize.go`. The normalized string is bound to a
      plain equality predicate (`WHERE ticket_code = $1`); never wrap the column in
      `UPPER()`/`LOWER()` — stored codes are already canonical (specs/001) and
      `idx_tickets_ticket_code` is a plain btree that a function on the column would
      make unusable
- [x] T003 [P] Add `ValidationResult` DTO in `backend/internal/ticket/dto.go`

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Validate a ticket at the door (Priority: P1) 🎯 MVP

**Goal**: Admins look up a ticket by manual code entry or camera QR scan and get
back Valid / Already Used / Invalid.

**Independent Test**: Look up a fresh unused ticket code (expect Valid), a
previously-used ticket code (expect Already Used), and a nonexistent/malformed
code (expect Invalid) — per quickstart.md Scenarios 1 and 3.

### Implementation for User Story 1

- [x] T004 [US1] Implement ticket lookup-by-normalized-code query — exact match
      `WHERE ticket_code = $1` so `idx_tickets_ticket_code` is used, joining
      attendee name, ticket type name, event name — in
      `backend/internal/ticket/repository.go` (depends on T002)
- [x] T005 [US1] Implement `ticket` service `Validate(code)` mapping ticket
      status to `VALID`/`ALREADY_USED`/`INVALID` (not-found and `REVOKED` both map
      to `INVALID`) in `backend/internal/ticket/service.go` (depends on T003, T004)
- [x] T006 [US1] Implement `POST /api/v1/admin/tickets/validate` handler in
      `backend/internal/ticket/handler.go`, registered behind JWT middleware in
      `backend/cmd/api/main.go` (depends on T005)
- [x] T007 [P] [US1] Build `frontend/app/admin/validate/page.tsx` with manual code
      input calling the validate endpoint
- [x] T008 [US1] Add camera QR scan capture in
      `frontend/components/admin/qr-scanner.tsx`, feeding decoded text into the
      same validate call as manual entry (depends on T001, T007)
- [x] T009 [P] [US1] Build `frontend/components/admin/validation-result-card.tsx`
      showing Valid/Already Used/Invalid with attendee/ticket-type/event details

**Checkpoint**: User Story 1 fully functional and independently testable

---

## Phase 4: User Story 2 - Mark a valid ticket as used (Priority: P1)

**Goal**: Admins mark a Valid ticket as Used, atomically preventing re-use.

**Independent Test**: Mark a Valid ticket as Used, then re-validate the same code
and confirm it now returns Already Used; confirm marking an already-Used or
invalid ticket is rejected (per quickstart.md Scenario 2).

### Implementation for User Story 2

- [x] T010 [US2] Implement guarded mark-used update (`UPDATE tickets SET status =
      'USED', updated_at = now() WHERE ticket_code = $1 AND status = 'ACTIVE'
      RETURNING id`, with `$1` the normalized input code) in
      `backend/internal/ticket/repository.go`. The `updated_at = now()` write is
      what records the transition — the LOCKED schema has no `used_at` column and
      none may be added (depends on T002)
- [x] T011 [US2] Implement `ticket` service `MarkUsed(code)` returning a
      not-found (404) or already-used/invalid (409) distinction based on rows
      affected in `backend/internal/ticket/service.go` (depends on T010)
- [x] T012 [US2] Implement `POST /api/v1/admin/tickets/:code/use` handler in
      `backend/internal/ticket/handler.go`, registered in
      `backend/cmd/api/main.go`, normalizing the `:code` path segment with T002's
      helper. This endpoint is a deliberate, documented addition to PRD §1.5's
      LOCKED API list (rationale in contracts/api.md and spec.md): `validate` must
      stay side-effect-free, so the `ACTIVE -> USED` transition needs its own
      endpoint (depends on T011)
- [x] T013 [US2] Add a "Mark Used" action button to
      `frontend/components/admin/validation-result-card.tsx`, calling the
      mark-used endpoint and refreshing the displayed result (depends on T009,
      T012)

**Checkpoint**: User Stories 1 AND 2 both work independently

---

## Phase 5: User Story 3 - Resend a ticket email (Priority: P2)

**Goal**: Admins manually trigger re-sending the ticket delivery email for a Paid
order.

**Independent Test**: Trigger resend on a Paid order and verify an email with the
same PDF ticket set is sent again to the buyer; verify resend is rejected for a
non-Paid order (per quickstart.md Scenario 4).

### Implementation for User Story 3

- [x] T014 [US3] Implement `notification` service `ResendTicketEmail(orderID)` in
      `backend/internal/notification/service.go`: fetch the order (buyer email,
      status) plus its attendees and their existing `ticket_code` values through the
      narrow `OrderChecker` interface established in specs/002 and implemented by
      the `order` domain — never importing `order`'s repository (ARCHITECTURE §3.2);
      reject any order whose status is not `PAID` (`PENDING`, `CANCELLED`,
      `EXPIRED`) with `400 ORDER_NOT_PAID`; reuse the existing ticket codes verbatim
      (never regenerate them) while re-generating each QR image from its ticket code
      at render time (`tickets.qr_code_url` is empty and unused — there is no object
      storage); reuse the PDF-render + SMTP-send path from specs/001 (depends on
      prior features' `pdf.go`/`smtp.go`)
- [x] T015 [US3] Implement `POST /api/v1/admin/orders/:id/resend-email` handler
      in `backend/internal/notification/handler.go`, registered behind JWT
      middleware in `backend/cmd/api/main.go` — where the `order` domain's
      `OrderChecker` implementation is also injected into the `notification`
      service, extending rather than duplicating the `OrderChecker` wiring from
      specs/002 — setting `orders.email_sent = true` on successful delivery
      (FR-011, same as the initial post-payment send) and returning
      `400 ORDER_NOT_PAID` for non-`PAID` orders (depends on T014)
- [x] T016 [P] [US3] Add a "Resend Ticket Email" action to the admin orders list at
      `frontend/app/admin/orders/page.tsx` from specs/002 (whose
      `GET /api/v1/admin/orders` data is served by
      `backend/internal/order/handler.go`), or to a small order-detail view, calling
      the resend endpoint

**Checkpoint**: All three user stories independently functional

---

## Phase 6: Polish & Cross-Cutting Concerns

- [x] T017 [P] Add structured error codes (`ALREADY_USED`, `ORDER_NOT_PAID`)
      consistently across `internal/ticket` and `internal/notification` handlers
- [x] T018 Run `quickstart.md` end-to-end (validate → mark used → re-validate →
      resend) and fix discrepancies; confirm a lowercase/whitespace-padded code
      resolves identically to the canonical one, that resend leaves every
      `ticket_code` byte-identical while producing fresh QR images, and that
      `orders.email_sent` is `true` afterwards
- [x] T019 [P] Review `internal/ticket` `dto.go` to confirm no sqlc-generated
      struct leaks into HTTP responses (Constitution Principle III)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories; also
  depends on `internal/ticket`/`internal/notification` scaffolding from specs/001
  and JWT middleware from specs/002 already existing
- **User Story 1 (Phase 3)**: Depends on Foundational only
- **User Story 2 (Phase 4)**: Depends on Foundational; builds on US1's ticket
  lookup/normalization but is independently testable via direct API calls
- **User Story 3 (Phase 5)**: Depends on Foundational and on specs/001's PDF/SMTP
  code and specs/002's `OrderChecker` interface; independent of US1/US2 at the code
  level
- **Polish (Phase 6)**: Depends on all desired user stories being complete

### Parallel Opportunities

- T001/T003 in early phases
- Within US1: T007/T009 in parallel; T008 after T007
- T016 (US3 frontend) independent of US1/US2 frontend work

---

## Parallel Example: User Story 1

```bash
Task: "Build frontend/app/admin/validate/page.tsx with manual code input"
Task: "Build frontend/components/admin/validation-result-card.tsx"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational
3. Complete Phase 3: User Story 1 (validate)
4. **STOP and VALIDATE**: quickstart.md Scenarios 1 and 3
5. Demo door-side ticket status lookup

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 (validate) → validate → demo
3. US2 (mark used) → validate → demo (validation loop is now closed — re-entry
   prevented)
4. US3 (resend email) → validate → demo (support capability added)

**Note**: US1+US2 together form the effective door-operations MVP; US3 is a
valuable but separable support feature.

---

## Notes

- [P] tasks touch different files with no unmet dependencies
- Commit after each task or logical group
- Re-run quickstart.md scenarios at each checkpoint
- This feature depends on tickets already existing (from specs/001) and on the
  JWT admin middleware (from specs/002) — implement those first, or use seeded
  test data if working on this feature in isolation

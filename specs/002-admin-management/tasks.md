---

description: "Task list for Admin Management implementation"
---

# Tasks: Admin Management

**Input**: Design documents from `/specs/002-admin-management/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/api.md, quickstart.md

**Tests**: Not explicitly requested in spec.md; test tasks are omitted.

**Organization**: Tasks are grouped by user story (US1-US4).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1 (auth), US2 (events), US3 (ticket types), US4 (orders/attendees
  read views)

## Path Conventions

Web app: `backend/internal/admin/`, `backend/internal/event/`,
`backend/internal/order/`, `backend/cmd/api/`, `frontend/app/admin/`. This feature
builds on the `backend`/`frontend` skeleton and the `event`/`order` domain
scaffolding created by the Guest Purchase Flow feature (specs/001). The admin
order/attendee list endpoints belong to `internal/order` (it owns those tables);
`internal/event` reaches the order domain only through the `OrderChecker` interface.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Admin-specific project scaffolding not already covered by specs/001

- [ ] T001 Add JWT library (`golang-jwt/jwt`) and password hashing library
      (bcrypt or argon2id) to `backend/go.mod`
- [ ] T002 [P] Add admin seed script/SQL (`backend/migrations/seed_admin.sql` or a
      small Go bootstrap command) to insert one admin row with a hashed password
      for local development

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: Core infrastructure that MUST be complete before ANY user story can
be implemented

**⚠️ CRITICAL**: No user story work can begin until this phase is complete

- [ ] T003 Define the `OrderChecker` interface in
      `backend/internal/event/order_checker.go` (consumer-defined, per
      ARCHITECTURE.md §3.2):
      `HasOrdersForTicketType(ctx, tx, ticketTypeID) (bool, error)`,
      `HasOrdersForEvent(ctx, tx, eventID) (bool, error)`,
      `SoldCountByTicketType(ctx, ticketTypeIDs) (map[uuid.UUID]int, error)`.
      The `Has*` methods take the caller's `pgx.Tx` so the guard runs inside the
      delete transaction. This is the ONLY dependency `event` has on `order` — the
      admin order/attendee list endpoints do NOT go through it.
- [ ] T004 Implement `OrderChecker` against the `order` domain's own tables in
      `backend/internal/order/repository.go` (depends on T003). Both `Has*` methods
      MUST check `NOT EXISTS` against **`order_items` AND `attendees`** (both FKs
      are `ON DELETE RESTRICT`); `SoldCountByTicketType` returns batched
      `SUM(order_items.quantity)` grouped by `ticket_type_id` (one query per page,
      no N+1)
- [ ] T005 Inject the `order` domain's `OrderChecker` implementation into the
      `event` service in `backend/cmd/api/main.go` (depends on T004)
- [ ] T006 [P] Add `admin` domain DTOs (`LoginRequest`, `LoginResponse`) in
      `backend/internal/admin/dto.go`
- [ ] T007 Implement JWT issuance/verification helpers in
      `backend/internal/admin/jwt.go`
- [ ] T008 Implement JWT auth Echo middleware in
      `backend/internal/admin/middleware.go`, applied to all `/admin/*` routes
      except `POST /admin/login` in `backend/cmd/api/main.go` (including the
      `/admin/orders` and `/admin/attendees` routes owned by `internal/order`)

**Checkpoint**: Foundation ready - user story implementation can now begin

---

## Phase 3: User Story 1 - Admin authentication (Priority: P1) 🎯 MVP

**Goal**: Admins log in with email/password and receive a JWT usable for
subsequent admin actions.

**Independent Test**: Submit valid and invalid credentials; verify a valid login
yields a usable JWT while an invalid one is rejected (per quickstart.md
Scenario 1).

### Implementation for User Story 1

- [ ] T009 [P] [US1] Implement `admin` repository lookup-by-email query in
      `backend/internal/admin/repository.go`
- [ ] T010 [US1] Implement `admin` service `Login(email, password)` (bcrypt/argon2
      verify + JWT issuance via T007) in `backend/internal/admin/service.go`
      (depends on T007, T009)
- [ ] T011 [US1] Implement `POST /api/v1/admin/login` handler in
      `backend/internal/admin/handler.go`, registered in `backend/cmd/api/main.go`
- [ ] T012 [P] [US1] Build `frontend/app/admin/login/page.tsx` (login form, stores
      JWT for subsequent requests)

**Checkpoint**: User Story 1 fully functional and independently testable

---

## Phase 4: User Story 2 - Manage events (Priority: P1)

**Goal**: Admins can create, read, update, list, and delete events, with slug
uniqueness, date validation, and delete-blocked-by-order/attendee enforcement.

**Independent Test**: Create an event, edit its fields, list it, and delete it
(when none of its ticket types is referenced by an order line or attendee record);
verify deletion is rejected with 400 when such a reference exists (per quickstart.md
Scenarios 2 and 4).

### Implementation for User Story 2

- [ ] T013 [P] [US2] Add admin-facing `event` DTOs (`EventAdminView`,
      `CreateEventRequest`, `UpdateEventRequest`) in
      `backend/internal/event/dto.go` — `banner_url` is a plain URL string field
      (no upload, no multipart)
- [ ] T014 [P] [US2] Implement admin `event` repository CRUD queries (all
      statuses, not just PUBLISHED) in `backend/internal/event/repository.go`
- [ ] T015 [US2] Implement the transactional event delete in
      `backend/internal/event/repository.go` (depends on T003, T014): open a
      `pgx.Tx`, call `OrderChecker.HasOrdersForEvent(ctx, tx, eventID)` and abort if
      true, then `DELETE FROM ticket_types WHERE event_id = $1`, then
      `DELETE FROM events WHERE id = $1`, then COMMIT. A single-statement event
      delete is impossible — `ticket_types.event_id` is `ON DELETE RESTRICT`, so the
      ticket types MUST be deleted first inside the same transaction
- [ ] T016 [US2] Implement `event` service create/update validation (slug
      uniqueness error mapping, `end_date >= start_date`) in
      `backend/internal/event/service.go` (depends on T013, T014)
- [ ] T017 [US2] Implement `event` service `DeleteEvent` mapping the guard outcome
      to a distinct 404 (event not found) vs 400 `EVENT_HAS_ORDERS` (rolled back,
      nothing deleted) result in `backend/internal/event/service.go` (depends on
      T015)
- [ ] T018 [US2] Implement `CRUD /api/v1/admin/events` handlers (list, create, get,
      update, delete) in `backend/internal/event/handler.go`, registered behind the
      JWT middleware in `backend/cmd/api/main.go` (depends on T016, T017)
- [ ] T019 [P] [US2] Build `frontend/app/admin/events/page.tsx` (list + create
      form; banner is a plain URL text input, not a file picker)
- [ ] T020 [P] [US2] Build `frontend/app/admin/events/[id]/page.tsx` (edit event
      fields, delete button surfacing the 400 `EVENT_HAS_ORDERS` error and warning
      that deleting the event also deletes its ticket types)

**Checkpoint**: User Stories 1 AND 2 both work independently

---

## Phase 5: User Story 3 - Manage ticket types (Priority: P1)

**Goal**: Admins can create, read, update, list, and delete ticket types via the
flat `/api/v1/admin/ticket-types` routes (locked PRD §1.5), with sales-window
validation, remaining-quota semantics, and delete-blocked-by-order/attendee
enforcement.

**Independent Test**: Create multiple ticket types under one event, edit remaining
quota/price/sales window, delete one with no orders; verify an absolute quota set
behaves as specified and that deletion is blocked once an order line or attendee
record references it (per quickstart.md Scenarios 2-4).

### Implementation for User Story 3

- [ ] T021 [P] [US3] Add admin-facing `TicketTypeAdminView`,
      `CreateTicketTypeRequest`, `UpdateTicketTypeRequest` DTOs in
      `backend/internal/event/dto.go`. `quota` is the REMAINING quota (the same
      column checkout decrements); `event_id` is a body field on create and
      immutable on update; `sold` is response-only and read-only
- [ ] T022 [P] [US3] Implement admin ticket-type repository CRUD queries in
      `backend/internal/event/repository.go` — list filtered by `event_id`, get by
      id, insert, and an absolute-set update
      (`UPDATE ticket_types SET quota = $new ...`, never `quota - sold`)
- [ ] T023 [US3] Implement the transactional guarded delete for ticket types in
      `backend/internal/event/repository.go` (depends on T003, T022): open a
      `pgx.Tx`, call `OrderChecker.HasOrdersForTicketType(ctx, tx, id)` — which
      checks `order_items` **and** `attendees` (T004) — abort if true, otherwise
      `DELETE FROM ticket_types WHERE id = $1`, then COMMIT. Guard and delete MUST
      share the transaction (no TOCTOU window); checking `order_items` alone leaks a
      raw FK violation instead of the required 400
- [ ] T024 [US3] Implement ticket-type service create/update validation
      (`sales_end >= sales_start`, `quota >= 0`, existing `event_id`) and the
      absolute remaining-quota set in `backend/internal/event/service.go` (depends
      on T021, T022)
- [ ] T025 [US3] Enrich ticket-type list/detail responses with the derived
      read-only `sold` count via `OrderChecker.SoldCountByTicketType` (batched for
      the whole page) in `backend/internal/event/service.go` (depends on T003,
      T021) — never via a cross-domain JOIN, and never in a write path
- [ ] T026 [US3] Implement ticket-type service `DeleteTicketType` mapping
      zero-rows-deleted to 404 vs 400 `TICKET_TYPE_HAS_ORDERS` in
      `backend/internal/event/service.go` (depends on T023)
- [ ] T027 [US3] Implement the flat ticket-type handlers in
      `backend/internal/event/handler.go`, registered behind the JWT middleware in
      `backend/cmd/api/main.go` (depends on T024, T025, T026):
      `GET /api/v1/admin/ticket-types?event_id=<uuid>`,
      `POST /api/v1/admin/ticket-types` (`event_id` in the body),
      `GET|PUT|DELETE /api/v1/admin/ticket-types/:id`. No nested
      `/admin/events/:eventId/ticket-types` route is registered
- [ ] T028 [P] [US3] Build ticket-type management UI within
      `frontend/app/admin/events/[id]/page.tsx` and
      `frontend/components/admin/ticket-type-form.tsx` (add/edit/delete ticket types
      for that event). The quota input MUST be labelled **"Sisa Kuota / Remaining
      Quota"** — never "Total" — with the read-only `sold` count displayed beside it
      and a note that the value is set absolutely and should not be edited during
      active sales

**Checkpoint**: User Stories 1, 2, AND 3 all work independently

---

## Phase 6: User Story 4 - View orders and attendees (Priority: P2)

**Goal**: Admins can list orders (status, buyer, total) and attendees (name,
email, ticket type), read-only, served by the `order` domain that owns those
tables.

**Independent Test**: Place sample orders and verify the admin can list/filter
them and see associated attendee details (per quickstart.md Scenario 5).

### Implementation for User Story 4

- [ ] T029 [P] [US4] Add `OrderSummary` and `AttendeeSummary` DTOs in
      `backend/internal/order/dto.go` (no sqlc struct leaks, Constitution
      Principle III)
- [ ] T030 [US4] Implement read-only order/attendee list queries (with optional
      `status`/`event_id`/`order_id` filters) in
      `backend/internal/order/repository.go` — these are same-domain reads, so no
      interface indirection is used
- [ ] T031 [US4] Implement `GET /api/v1/admin/orders` and
      `GET /api/v1/admin/attendees` handlers in
      `backend/internal/order/handler.go`, registered behind the admin JWT
      middleware in `backend/cmd/api/main.go` (depends on T029, T030)
- [ ] T032 [P] [US4] Build `frontend/app/admin/orders/page.tsx` (read-only order
      list)
- [ ] T033 [P] [US4] Build `frontend/app/admin/attendees/page.tsx` (read-only
      attendee list)

**Checkpoint**: All four user stories independently functional

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T034 [P] Add `frontend/components/admin/admin-nav.tsx` shared navigation
      across all `/admin/*` pages
- [ ] T035 [P] Add structured error codes (`SLUG_NOT_UNIQUE`, `INVALID_DATE_RANGE`,
      `EVENT_HAS_ORDERS`, `TICKET_TYPE_HAS_ORDERS`) consistently in
      `backend/internal/event/handler.go`
- [ ] T036 Run `quickstart.md` end-to-end (login → create event/ticket type via the
      flat routes → verify remaining-quota/sold semantics → attempt blocked deletes
      → view orders/attendees) and fix discrepancies
- [ ] T037 [P] Review `internal/admin`, `internal/event`, and `internal/order`
      `dto.go` files to confirm no sqlc-generated struct leaks into HTTP responses
      (Constitution Principle III)
- [ ] T038 [P] Verify Domain Isolation (Constitution Principle II /
      ARCHITECTURE.md §3.2): `internal/event` imports no `internal/order` package,
      its only coupling is the `OrderChecker` interface, and no admin write path
      JOINs across domain-owned tables

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies
- **Foundational (Phase 2)**: Depends on Setup - BLOCKS all user stories; also
  depends on specs/001's `backend`/`frontend` skeleton and `event`/`order` domains
  already existing
- **User Story 1 (Phase 3)**: Depends on Foundational only
- **User Story 2 (Phase 4)**: Depends on Foundational; independent of US1's
  handlers (only needs the JWT middleware from T008) but requires the
  `OrderChecker` wiring (T003-T005) for its delete guard
- **User Story 3 (Phase 5)**: Depends on Foundational (including T003-T005 for the
  delete guard and the `sold` count); independent of US2 at the code level but
  conceptually needs an event to attach ticket types to (use test data from US2 or
  seed one directly)
- **User Story 4 (Phase 6)**: Depends on Foundational and on the `order` domain
  existing (from specs/001); it now implements its handlers inside `internal/order`
  and does not depend on `internal/event` at all
- **Polish (Phase 7)**: Depends on all desired user stories being complete

### Parallel Opportunities

- T002 parallel with T001; T006 parallel with T003/T004 in Phase 2
- Within US2: T013/T014 in parallel; T019/T020 in parallel after T018
- Within US3: T021/T022 in parallel; T028 after T027
- Within US4: T029 parallel with the start of T030; T032/T033 in parallel after
  T031
- US4 (Phase 6) touches only `internal/order` and `frontend/app/admin/orders|
  attendees`, so it can run fully in parallel with US2/US3 once Foundational is done

---

## Parallel Example: User Story 2

```bash
Task: "Add admin-facing event DTOs in backend/internal/event/dto.go"
Task: "Implement admin event repository CRUD queries in backend/internal/event/repository.go"
```

---

## Implementation Strategy

### MVP First (User Story 1 Only)

1. Complete Phase 1: Setup
2. Complete Phase 2: Foundational
3. Complete Phase 3: User Story 1 (login)
4. **STOP and VALIDATE**: quickstart.md Scenario 1
5. Demo admin login

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 (auth) → validate → demo
3. US2 (events) → validate → demo (admins can now build the catalog)
4. US3 (ticket types) → validate → demo (catalog is now sellable)
5. US4 (orders/attendees) → validate → demo (admins get sales visibility)

**Note**: US1+US2+US3 together are the effective MVP for this feature — an admin
needs to both log in and create a full event+ticket-type before the Guest Purchase
Flow feature has anything to sell.

---

## Notes

- [P] tasks touch different files with no unmet dependencies
- Commit after each task or logical group
- Re-run quickstart.md scenarios at each checkpoint
- `ticket_types.quota` is REMAINING quota everywhere in this feature; never label it
  "Total" and never re-subtract sales from an admin-submitted value
- Every delete guard checks BOTH `order_items` and `attendees`; both FKs are
  `ON DELETE RESTRICT`
- This feature's US4 deliberately depends on the `order` domain owned by
  specs/001-guest-purchase-flow; implement specs/001 first (or seed test orders
  directly) if working on US4 in isolation

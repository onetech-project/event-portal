# Phase 0 Research: Admin Management

No `NEEDS CLARIFICATION` markers remain. This document records key decisions.

## JWT authentication

- **Decision**: Issue a short-to-medium-lived (e.g., 8-24h) signed JWT on
  successful login containing the admin's `id`/`email`; verify via Echo middleware
  on all `/admin/*` routes except `POST /admin/login`.
- **Rationale**: PRD.md specifies JWT Auth explicitly; no refresh-token flow is
  called for at MVP scope (Assumptions: single admin role, out-of-band
  provisioning).
- **Alternatives considered**: Session cookies with server-side session store
  (rejected — adds a stateful session table not in SCHEMA.md; JWT matches the PRD's
  explicit stack choice).

## Password storage

- **Decision**: Store `password_hash` using bcrypt (or argon2id) with a
  per-install cost/work factor; never store or log plaintext passwords.
- **Rationale**: `admins.password_hash` column name in SCHEMA.md implies a hashed
  credential; bcrypt/argon2id are the standard choices for Go.
- **Alternatives considered**: None — plaintext or reversible encryption is a
  security defect, not a real alternative.

## Delete-blocked-by-order check (ticket type)

- **Decision**: The guard MUST test **both** restricting foreign keys —
  `order_items.ticket_type_id` **and** `attendees.ticket_type_id` — and MUST run in
  the same transaction as the delete. Because those two tables belong to the `order`
  domain, the `event` domain does not query them itself: it opens a `pgx.Tx`, calls
  `OrderChecker.HasOrdersForTicketType(ctx, tx, id)`, and deletes only if it returns
  false. The `order` domain's implementation of that method runs, against its own
  tables and on the caller's `tx`:
  ```sql
  SELECT EXISTS (SELECT 1 FROM order_items WHERE ticket_type_id = $1)
      OR EXISTS (SELECT 1 FROM attendees   WHERE ticket_type_id = $1);
  ```
  and the `event` repository then issues `DELETE FROM ticket_types WHERE id = $1`
  before `COMMIT`. Distinguish "not found" (404) from "blocked by existing
  order/attendee" (400) from the existence check and the delete's rows-affected
  count, both inside the same transaction.
- **Rationale**: SCHEMA.md declares `ON DELETE RESTRICT` on **both**
  `order_items.ticket_type_id` and `attendees.ticket_type_id`. Guarding only
  `order_items` leaves the `attendees` reference to be caught by Postgres itself,
  surfacing a raw FK-violation (SQLSTATE 23503) instead of the clean
  `400 TICKET_TYPE_HAS_ORDERS` the PRD demands. Running guard and delete in one
  transaction closes the TOCTOU window a separate SELECT-then-DELETE would leave
  open under concurrent checkout, and `RESTRICT` remains the final backstop — it
  aborts the transaction rather than allowing a partial delete.
- **Alternatives considered**: One self-contained guarded statement in the `event`
  repository —
  `DELETE FROM ticket_types WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM order_items
  ...) AND NOT EXISTS (SELECT 1 FROM attendees ...)` — (rejected: atomicity is ideal,
  but it puts `order`-owned tables in an `event`-domain write statement, which
  ARCHITECTURE.md §3.2 forbids; the transaction + `OrderChecker` form gives the same
  atomicity while keeping the domain boundary). A preceding `SELECT EXISTS(...)`
  outside any transaction (rejected — reintroduces the TOCTOU race). Catching the FK
  violation and translating SQLSTATE 23503 to a 400 (rejected — it cannot
  distinguish which reference blocked, and error-code sniffing is brittle).

## Event deletion must be a transaction, not one statement

- **Decision**: Deleting an event is a multi-statement transaction, because
  `ticket_types.event_id` is `NOT NULL REFERENCES events(id) ON DELETE RESTRICT` —
  Postgres will refuse to delete an event that still owns ticket types, so
  "delete the event and leave its ticket types" is not merely undesirable, it is
  impossible. Sequence:
  1. `BEGIN`; verify no ticket type of this event is referenced, via
     `OrderChecker.HasOrdersForEvent(ctx, tx, eventID)`. The `order` domain
     implements it on the caller's `tx`, resolving the event's ticket types in a
     read-only subquery and checking **both** referencing tables it owns:
     ```sql
     SELECT EXISTS (
       SELECT 1 FROM order_items
       WHERE ticket_type_id IN (SELECT id FROM ticket_types WHERE event_id = $1)
     ) OR EXISTS (
       SELECT 1 FROM attendees
       WHERE ticket_type_id IN (SELECT id FROM ticket_types WHERE event_id = $1)
     );
     ```
     If true → `ROLLBACK` and return `400 EVENT_HAS_ORDERS`, leaving everything
     intact. (The `ticket_types` lookup here is a read-only subquery inside the
     interface implementation, not a cross-domain write; the `event` domain never
     touches `order_items`/`attendees` itself.)
  2. `DELETE FROM ticket_types WHERE event_id = $1`
  3. `DELETE FROM events WHERE id = $1`
  4. `COMMIT`
- **Rationale**: Keeps the check and both deletes in one atomic unit, so a
  concurrent checkout cannot slip an `order_items`/`attendees` row in between the
  guard and the deletes (the FK's `RESTRICT` is the final backstop, and it will
  abort the whole transaction rather than half-delete). Also satisfies FR-013's
  all-or-nothing requirement.
- **Alternatives considered**: Changing the FK to `ON DELETE CASCADE` (rejected —
  SCHEMA.md is locked, and cascade would also make an accidental event delete
  destroy paid inventory silently). Deleting ticket types in a separate request
  first (rejected — leaves the catalog in a half-deleted state if the second
  request never arrives).

## Endpoint ownership: admin order/attendee reads live in the `order` domain

- **Decision**: `GET /api/v1/admin/orders` and `GET /api/v1/admin/attendees` are
  handled by `backend/internal/order/handler.go`, registered behind the admin JWT
  middleware in `cmd/api/main.go`. **No interface indirection** is used for these
  reads — the `order` domain already owns `orders`, `order_items`, and `attendees`,
  so it queries its own repository directly and returns its own DTOs.
- **Rationale**: Routing these reads through the `event` domain put the handlers in
  the wrong domain and added a pass-through interface that bought nothing: the
  data, the tables, and the projections are all the `order` domain's. Placing the
  handler where the tables live is what ARCHITECTURE.md §3.1's domain-driven layout
  asks for, and it removes a layer that would otherwise have to be re-plumbed when
  the domain is extracted into a service.
- **Alternatives considered**: Keeping the handlers in `internal/event` behind an
  `OrderReader` interface (rejected — wrong domain, needless indirection); a
  dedicated read-only `internal/reporting` domain (rejected — a seventh domain
  outside the constitution's fixed domain list, and unnecessary complexity for two
  list endpoints).

## `OrderChecker`: the narrow interface the `event` domain still needs

- **Decision**: The `event` domain still has to ask the `order` domain two
  questions — "is this ticket type / event referenced by an order?" (delete guard)
  and "how many have been sold?" (the read-only Sold count). Define a narrow
  interface **in the `event` domain**, implemented by the `order` domain, injected
  in `cmd/api/main.go`:
  ```go
  // internal/event — defined by the consumer, implemented by internal/order
  type OrderChecker interface {
      HasOrdersForTicketType(ctx context.Context, tx pgx.Tx, ticketTypeID uuid.UUID) (bool, error)
      HasOrdersForEvent(ctx context.Context, tx pgx.Tx, eventID uuid.UUID) (bool, error)
      SoldCountByTicketType(ctx context.Context, ticketTypeIDs []uuid.UUID) (map[uuid.UUID]int, error)
  }
  ```
  `Has*` take the caller's `pgx.Tx` so the guard runs inside the same transaction as
  the delete (see the two delete sections above). `HasOrdersForTicketType` and
  `HasOrdersForEvent` both check `order_items` **and** `attendees`.
  `SoldCountByTicketType` is batched (one query for a page of ticket types) to avoid
  N+1 lookups on the list view.
- **Rationale**: Preserves ARCHITECTURE.md §3.2 / Constitution Principle II — the
  `event` domain never imports the `order` domain's repository and never JOINs
  across domain-owned tables in a write path — while keeping the delete guard
  atomic. It is deliberately the smallest surface that satisfies FR-006, FR-009,
  and FR-015.
- **Alternatives considered**: Letting `event` JOIN `order_items`/`attendees`
  directly in the delete statement (rejected — a cross-domain JOIN in a write path,
  explicitly forbidden by ARCHITECTURE.md §3.2); computing Sold in the frontend from
  the order list (rejected — unbounded fetch, and wrong for paginated views).

## Quota semantics: `ticket_types.quota` is REMAINING, and admin edits set it absolutely

- **Decision**: Treat `ticket_types.quota` as the **remaining** quota everywhere in
  this feature. The admin form field is labelled "Sisa Kuota / Remaining Quota"
  (never "Total"), an admin write performs an absolute set
  (`UPDATE ticket_types SET quota = $new WHERE id = $1`), and the UI additionally
  shows a read-only derived `sold` count obtained via
  `OrderChecker.SoldCountByTicketType`.
- **Rationale**: specs/001 (Guest Purchase Flow) decrements this exact column inside
  the checkout transaction and restores it on cancel/expire, so after 10 sales from
  an initial 50 the column holds 40. Labelling that field "Total" in the admin UI
  would invite an admin to "restore" it to 50 and silently create 10 phantom
  tickets — an oversell the `CHECK (quota >= 0)` constraint cannot catch. SCHEMA.md
  is locked, so there is no `quota_total` column to reconcile against; the derived
  Sold count supplies the missing context instead.
- **Accepted risk**: An absolute set can absorb a sale that commits between the
  admin's read and write (admin reads 40, a guest buys 1 leaving 39, admin saves 40
  → one unit of phantom inventory). Mitigation is procedural: advise admins not to
  edit quota during active sales. No optimistic-locking/version column is added —
  the schema is locked, and the MVP's validation goal does not justify a schema
  amendment.
- **Alternatives considered**: Adding a `quota_total` column and deriving remaining
  as `quota_total - sold` (rejected — SCHEMA.md is locked and specs/001 already
  writes `quota` directly); a relative "adjust by ±N" admin input (rejected — more
  confusing than an absolute remaining value, and still racy); optimistic locking
  via a version/`updated_at` compare-and-set (rejected — requires a schema change
  and/or a semantics change specs/001 does not honour).

## Slug and date-range validation

- **Decision**: Enforce slug uniqueness via the existing `events.slug` UNIQUE
  constraint, surfaced as a 400 with a clear message on constraint violation; date
  ordering (`end_date >= start_date`, `sales_end >= sales_start`) validated in the
  service layer before hitting the database.
- **Rationale**: Matches FR-004/FR-005/FR-008 exactly; DB constraint is the source
  of truth for uniqueness, service-layer check gives a clean error message rather
  than a raw constraint-violation error.
- **Alternatives considered**: DB-level `CHECK` constraint for date ordering
  (viable addition, but not present in SCHEMA.md as locked; service-layer
  validation is sufficient without a schema change).

## Ticket-type route shape (flat, not nested)

- **Decision**: Ticket-type endpoints are flat under `/api/v1/admin/ticket-types`:
  list is `GET /api/v1/admin/ticket-types?event_id=<uuid>`, create is
  `POST /api/v1/admin/ticket-types` with `event_id` in the request body, and
  `GET`/`PUT`/`DELETE /api/v1/admin/ticket-types/:id` address a single row.
- **Rationale**: PRD.md §1.5 is marked LOCKED and specifies exactly
  `CRUD /api/v1/admin/ticket-types`. A nested
  `POST /admin/events/:eventId/ticket-types` would contradict a locked contract, and
  would also split the resource across two shapes (nested create, flat update/
  delete) for no gain.
- **Alternatives considered**: Nested-under-event routes (rejected — contradicts
  locked PRD §1.5); both shapes registered as aliases (rejected — two ways to do the
  same thing, and the nested form is still off-contract).

## Banner handling

- **Decision**: `events.banner_url` holds a plain URL string typed/pasted by the
  admin and stored verbatim. No upload endpoint, no multipart handling, no image
  processing, no serving of binaries.
- **Rationale**: The stack (PRD.md §1.2) contains no object storage or file-hosting
  service, and adding one is outside the 5-day MVP scope. A URL field needs no
  infrastructure and is trivially replaceable later.
- **Alternatives considered**: Local disk uploads served by the API (rejected — no
  persistence story across container restarts, and outside MVP scope); an S3-style
  object store (rejected — new infrastructure, out of scope per PRD.md §1.6).

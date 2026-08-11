# Feature Specification: Refresh-on-Write List Caching

**Feature Branch**: `012-redis-list-cache`

**Created**: 2026-08-10

**Status**: Draft — ready for planning (constitution amended to v3.1.0 on 2026-08-10)

**Input**: User description: "enhancement untuk cache list event, list ticket dan list order di redis dengan menggunakan teknik refresh tiap kali ada perubahan data di db"

## Overview

Three families of list responses are re-computed from the primary database on every
single request today: the event lists (public catalogue and admin table), the ticket
lists (public ticket types and packages for an event, admin ticket-type and package
tables), and the order lists (admin orders and admin attendees). These lists change
rarely relative to how often they are read — a published event catalogue may be read
thousands of times between two admin edits.

This feature introduces a shared read cache in front of those list reads, kept correct
by **refresh-on-write**: whenever the underlying data changes in the database, the
affected cached lists are refreshed as part of that same change, rather than being
left to expire. Readers therefore never see a list that is older than the last
committed write.

**Explicitly not in scope**: caching of anything that is not a list read — single-record
detail endpoints, ticket lookup by code, payment status, checkout/booking writes, and
the order page's live payment state remain uncached and unchanged.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Guests browse a fast, always-correct event catalogue (Priority: P1)

A guest opens the ticketing site and sees the list of published events. Hundreds of
guests may do this in the same minute, especially in the first moments after an event
goes on sale. Each of them gets the same list, so it should be assembled once and
reused — but the instant an admin publishes, unpublishes, or edits an event, the very
next guest must see the updated catalogue, with no waiting period.

**Why this priority**: The public event list is the highest-traffic read in the product
and the entry point to every purchase. It delivers the largest share of the benefit and
is fully useful on its own, without any of the other lists being cached.

**Independent Test**: Request the public event list repeatedly and confirm identical,
correct content with reduced response time; then change an event through the admin
surface and confirm the very next public request reflects the change.

**Acceptance Scenarios**:

1. **Given** a set of published events and a warm cache, **When** a guest requests the
   event list, **Then** the response content is byte-identical to what an uncached read
   would return, and is served faster than the uncached read.
2. **Given** a warm cache, **When** an admin publishes a new event, **Then** the next
   public event-list request includes that event with no manual cache action and no
   waiting period.
3. **Given** a warm cache, **When** an admin unpublishes or deletes an event, **Then**
   the next public event-list request no longer contains it.
4. **Given** the cache store is unavailable, **When** a guest requests the event list,
   **Then** the request still succeeds with correct data read from the primary database,
   and the failure is recorded for operators without being shown to the guest.
5. **Given** multiple API instances are running, **When** one instance processes an
   admin edit, **Then** requests served by every other instance also reflect the change
   immediately.

---

### User Story 2 - Ticket and package lists stay in step with live inventory (Priority: P1)

A guest opens an event page and sees its ticket types and packages, including how many
are still available. Availability is live inventory: it drops when someone books and is
restored when an order is cancelled, expired, denied, or fails. A cached availability
number that lags reality would either hide tickets that exist or advertise tickets that
are gone, so every change to remaining quota must refresh these lists at the moment it
commits.

**Why this priority**: Same read volume as the event list and directly on the purchase
path, but it carries the correctness risk that makes refresh-on-write mandatory rather
than optional. It is co-equal with US1 and must not ship with weaker invalidation.

**Independent Test**: Read an event's ticket list, complete a booking that deducts quota,
and confirm the next read of that list shows the reduced remaining quota; then expire the
order and confirm the restored quota appears on the following read.

**Acceptance Scenarios**:

1. **Given** a warm ticket list for an event, **When** a guest books tickets and the
   booking commits, **Then** the next read of that event's ticket list shows the
   decremented remaining quota.
2. **Given** a warm ticket list for an event, **When** an order for that event is
   cancelled, expired, denied, or fails and its quota is restored, **Then** the next
   read of that event's ticket list shows the restored remaining quota.
3. **Given** a warm ticket list, **When** an admin creates, edits, or removes a ticket
   type or a package for that event, **Then** the next read reflects the change.
4. **Given** an event whose ticket list is cached, **When** a different event's tickets
   change, **Then** the first event's cached list is left intact (changes are scoped to
   the affected event).
5. **Given** any moment during concurrent booking traffic, **When** a guest reads a
   ticket list and then immediately attempts to book what it showed as available,
   **Then** the booking outcome is decided by the database's own quota check, never by
   the cached number — the cache never authorizes a sale.

---

### User Story 3 - Admin tables load quickly without going stale (Priority: P2)

An admin works through the orders table and the attendees table, filtering by status and
by event, and repeatedly returns to the same views while processing the day's sales.
Those repeated views should be fast, but an admin must never act on an order list that
predates a payment that has already been confirmed.

**Why this priority**: Real benefit and the heaviest queries in the product, but a small,
authenticated audience — so the traffic saved is far smaller than P1. Correctness
pressure is high (an admin acting on stale payment state is a real operational error),
which is why it is included rather than deferred, but it can ship after the public lists.

**Independent Test**: Load an admin order list twice and confirm the second load is
faster with identical content; then drive an order from pending to paid through the
payment flow and confirm the next load of the same filtered list shows the new status.

**Acceptance Scenarios**:

1. **Given** a warm admin order list for a given filter combination, **When** the admin
   reloads the same filters, **Then** the content is identical to an uncached read and
   is served faster.
2. **Given** a warm admin order list, **When** an order is created, has its status
   changed by payment confirmation, expiry, cancellation, or failure, or has its data
   edited, **Then** the next admin list read shows the new state.
3. **Given** admin lists cached under several different filter combinations, **When**
   any single order changes, **Then** no filter combination continues to serve the old
   state of that order.
4. **Given** a warm admin attendee list, **When** attendee data is written during
   booking or checkout, **Then** the next admin attendee list read shows it.

---

### User Story 4 - Operators can see and control the cache (Priority: P3)

An operator needs to know whether the cache is healthy and being used, and needs a way
to force every cached list to be rebuilt from the database — for a suspected
inconsistency, after a manual database correction, or during an incident.

**Why this priority**: Not required for the feature to deliver value, but it is what
makes the cache safe to operate in production; without it, an inconsistency has no
remedy short of a restart.

**Independent Test**: Query the operational health surface and confirm it reports cache
availability and usage; trigger a full refresh and confirm subsequent reads are rebuilt
from the database while still returning correct content.

**Acceptance Scenarios**:

1. **Given** the system is running, **When** an operator inspects the health surface,
   **Then** it reports whether the cache store is reachable and reports how often list
   reads are being served from cache versus from the database.
2. **Given** cached lists exist, **When** an authenticated admin triggers a full cache
   refresh, **Then** all cached lists are discarded and the next read of each returns
   correct, freshly-computed content.
3. **Given** a manual database correction was made outside the application, **When** the
   operator triggers a full refresh, **Then** the corrected data appears in every
   affected list.

---

### Edge Cases

- **Cache store unreachable on read**: the read falls back to the primary database and
  succeeds. Cache unavailability MUST NOT turn into a user-visible error on any endpoint.
- **Cache store unreachable on write-refresh**: the database write still commits — the
  write path MUST NOT be rolled back or blocked because the cache could not be updated.
  The affected entries must not be left holding stale content; a store that cannot be
  reached to be cleared must be treated as untrusted until it is confirmed refreshed.
- **Write commits, refresh fails afterwards**: the system MUST NOT be left silently
  serving pre-write content. This is the one failure mode that breaks the feature's
  correctness promise and needs an explicit recovery (retry, mark-untrusted, or bounded
  expiry backstop).
- **Write transaction rolls back**: the cache MUST NOT be refreshed from, or invalidated
  toward, data that never committed.
- **Concurrent write and refresh**: two admins editing the same event at nearly the same
  moment must not leave the cache holding the earlier of the two committed states.
- **Unbounded key growth from filters**: admin lists are filtered by status, event, and
  order. The number of distinct filter combinations is bounded but grows with the number
  of events; cached entries MUST NOT grow without limit or without eviction.
- **Cold start**: after a restart or a full flush, every first read is a database read.
  A traffic spike against a cold cache MUST NOT produce a thundering herd of identical
  database queries.
- **Multiple API instances**: an instance that did not process the write must still serve
  post-write content — no instance may hold a private copy that the write path cannot
  reach.
- **Payment webhook path**: quota restoration and status changes arriving by webhook are
  writes like any other and MUST refresh the same lists; the webhook must still respond
  immediately, so refresh work must not delay its response.
- **Empty results**: an empty list (e.g. an event with no packages) is a valid cached
  value and MUST NOT be re-queried on every request as though it were a cache miss.

## Requirements *(mandatory)*

### Functional Requirements

**Scope of what is cached**

- **FR-001**: The system MUST cache the public event list, the public per-event ticket
  type list, and the public per-event package list.
- **FR-002**: The system MUST cache the admin event list, the admin ticket-type list, and
  the admin package list.
- **FR-003**: The system MUST cache the admin order list and the admin attendee list,
  including their filtered variants, with each distinct filter combination cached
  separately.
- **FR-004**: The system MUST NOT cache single-record detail reads, ticket lookup by
  code, payment or checkout status, or any write endpoint.
- **FR-005**: Cached content MUST be identical to what the uncached read returns for the
  same request — same fields, same ordering, same shape. Introducing the cache MUST NOT
  change any response contract.

**Refresh on write**

- **FR-006**: Every committed database change to events, ticket types, packages, orders,
  order items, attendees, or quota MUST cause the cached lists containing that data to be
  refreshed, so that the next read returns post-write content.
- **FR-007**: Refresh MUST be triggered by the commit of the change, not by a timer, and
  MUST NOT happen for a transaction that rolls back.
- **FR-008**: Refresh MUST cover every write path that reaches the affected data,
  including admin edits, guest booking, checkout, payment webhooks (paid, expired,
  cancelled, denied, failed), and any background or scheduled expiry process.
- **FR-009**: Refresh MUST be scoped to the affected data where scoping is possible — a
  change to one event's tickets MUST NOT discard other events' cached lists.
- **FR-010**: Where a change cannot be scoped precisely to a filter combination (for
  example an order change affecting several cached admin filter variants), the system
  MUST refresh every variant that could contain the changed record rather than leave any
  variant stale.
- **FR-011**: Refresh MUST take effect for all API instances, not only the instance that
  processed the write.
- **FR-023**: No cache operation may be issued from inside an order-writing transaction.
  Refresh MUST run after the transaction commits and outside it, so that the row lock
  held by the quota-deducting update is never held across a cache round-trip.

**Correctness and safety**

- **FR-012**: A cached value MUST never be used as the authority for a quota or
  availability decision; booking and checkout MUST continue to decide against the
  database.
- **FR-013**: When the cache store is unavailable, all read endpoints MUST continue to
  serve correct data from the primary database, and all write endpoints MUST continue to
  commit.
- **FR-014**: When a refresh cannot be completed after its write has committed, the system
  MUST NOT continue serving the pre-write content indefinitely; it MUST recover to
  post-write content without operator intervention.
- **FR-015**: Cached entries MUST carry a bounded maximum lifetime as a backstop, so that
  any entry missed by a refresh cannot remain stale indefinitely.
- **FR-016**: Cached entries MUST be bounded in total footprint, with eviction of the
  least useful entries when the bound is reached.
- **FR-017**: A burst of simultaneous misses for the same list MUST result in the list
  being computed once rather than once per waiting request.

**Operability**

- **FR-018**: The system MUST report whether the cache store is reachable on its health
  surface, and MUST report health as degraded — not failed — when the cache is down but
  the database is up.
- **FR-019**: The system MUST expose counts of cache hits, misses, refreshes, and refresh
  failures per cached list family for operators.
- **FR-020**: An authenticated admin MUST be able to discard all cached lists and force
  them to be rebuilt from the database.
- **FR-021**: Caching MUST be switchable off by configuration, returning the system to
  direct database reads on every request with no other behavioral change.
- **FR-022**: Refresh failures MUST be logged with enough detail to identify which list
  and which record were affected.

### Key Entities

- **Cached List Entry**: one stored list response, identified by which list it is and
  which parameters produced it (event, filter values). Holds the response content, the
  time it was produced, and its expiry backstop.
- **Cache Scope**: the grouping used to decide what a given write must refresh — e.g.
  "all public event lists", "one event's ticket and package lists", "all admin order list
  variants". Every write path maps to one or more scopes.
- **Refresh Trigger**: the record of a committed write that must cause refresh —
  identifies the changed entity, its identity, and the scopes derived from it.
- **Cache Health**: the operator-facing view — reachability of the store, per-family hit
  and miss counts, refresh counts, and refresh failure counts.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A repeat request for an already-cached list returns in under 30 ms at the
  service boundary, at least 5× faster than the same request served from the primary
  database.
- **SC-002**: After any committed change to the underlying data, the very next read of an
  affected list returns the new content — 0 requests return pre-change content, measured
  across at least 100 write-then-read cycles per list family.
- **SC-003**: Under steady traffic with a normal edit rate, at least 90% of list requests
  are served without re-querying the primary data store.
- **SC-004**: Load on the primary data store from list reads drops by at least 80% at
  equal request volume.
- **SC-005**: With the cache store fully unavailable, 100% of list requests still return
  correct data and no request fails as a result; measured recovery to normal serving
  within 60 seconds of the store returning.
- **SC-006**: A 500-request burst against a cold cache for the same list produces no more
  than 5 primary-data-store queries for that list.
- **SC-007**: No availability number shown to a guest ever exceeds the true remaining
  quota, verified across a concurrent booking test of at least 200 simultaneous bookings.
- **SC-008**: Turning caching off by configuration reproduces current behavior exactly —
  identical responses on every covered endpoint, verified by the existing test suite
  passing unchanged in both modes.

## Assumptions

- **Both public and admin lists are in scope.** The description names "list event, list
  ticket, list order"; orders are only ever listed on admin surfaces, so restricting the
  feature to public endpoints would exclude order lists entirely. Admin attendee lists are
  included with orders because they are written by the same paths.
- **"Refresh" is implemented as invalidate-then-recompute-on-next-read**, not as eager
  pre-computation at write time. Both satisfy "the next reader sees post-write content";
  invalidation is chosen because eager recomputation would have the write path compute
  every filter variant of the admin lists, which is unbounded work inside a write.
- **A bounded expiry backstop coexists with refresh-on-write.** Refresh is the primary
  correctness mechanism; expiry exists only to bound the damage of a missed refresh, not
  as the freshness strategy.
- **The cache is a shared, out-of-process store** so that all API instances observe the
  same state — an in-process cache cannot satisfy FR-011.
- **Frontend caching behavior is unchanged.** The constitution requires the frontend to
  disable aggressive caching for event lists and quotas; this feature caches on the
  server side only and does not relax that rule.
- **No response contract changes.** This is a performance enhancement behind existing
  endpoints; no new fields, no changed ordering, no new error shapes on the cached
  endpoints. The admin refresh control (FR-020) is the only new endpoint.
- **Read-your-write consistency is required; cross-instance eventual consistency is not
  acceptable** for the lists in scope, because an admin who saves an edit and immediately
  reloads must see it.

## Dependencies

- **Governed by Constitution Principle VII** (Cache as a Disposable Read Accelerator),
  added in the v3.1.0 amendment of 2026-08-10 that unblocked this feature. Principle VII
  binds the implementation to: PostgreSQL as sole source of truth, a closed list of
  cacheable surfaces, commit-triggered invalidation with TTL only as a backstop, the
  cache never being authoritative for quota, **no cache call between `BEGIN` and `COMMIT`
  of an order-writing transaction**, fail-open behavior, access through an injected
  interface rather than a direct client import, and a configuration kill switch. The
  requirements below were checked against it and are consistent (the transaction-boundary
  rule is captured as FR-023).
- Depends on the existing event, ticket-type, package, order, and attendee list reads and
  on every write path that touches them (admin CRUD, booking, checkout, payment webhooks,
  expiry).
- Requires a shared cache store to be added to the deployment (`docker-compose.yml`) and
  to the deployment script, including its availability being a monitored, non-fatal
  dependency.
- Requires ARCHITECTURE.md to be updated to document the cache as a component and the
  refresh-on-write flow, per the constitution's Governance clause.

## Out of Scope

- Caching of detail endpoints, ticket lookup by code, payment status streams, or the QRIS
  image endpoint.
- Caching of write results or any form of write-behind / deferred database writes.
- Session storage, rate-limit counters, job queues, or pub/sub messaging in the cache
  store — this feature adds a read cache only.
- Frontend or CDN caching changes.
- Cross-region or multi-datacenter cache replication.

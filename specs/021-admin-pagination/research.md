# Phase 0 Research: Admin Console Pagination

Every decision below was taken against the code as it stands on 2026-08-19, not against a
remembered shape of it. File references are the evidence.

---

## R1 — Paging model: offset/limit, not cursor

**Decision**: Page number + page size, translated to SQL `LIMIT`/`OFFSET`.

**Rationale**: FR-005 requires jumping to an arbitrary numbered page including the last,
and FR-004 requires an exact total. Keyset (cursor) paging gives neither without extra
machinery. The volumes in SC-001 — 10k orders, 25k attendees — are two orders of
magnitude below where `OFFSET` scan cost becomes the dominant term, and the deepest page
at size 20 is a 25,000-row offset, which PostgreSQL walks in single-digit milliseconds
over an already-sorted result.

**Alternatives considered**:

- *Keyset/cursor paging*: strictly better at very large offsets, but cannot express "page
  47 of 312" or "last page", both of which the spec requires. Rejected on requirements,
  not on performance.
- *Client-side slicing of a full fetch*: explicitly forbidden by FR-007, and it would
  leave the actual cost — the query, the JSON, the transfer — exactly where it is today.

---

## R2 — Response shape: a paged object inside the existing envelope

**Decision**: Keep the universal `{code, message, data}` envelope
([`pkg/httpx/envelope.go`](../../backend/pkg/httpx/envelope.go)) and make `data` a paged
object: `{items, page, page_size, total, total_pages}`.

**Rationale**: The envelope is shared by every endpoint in the product and is asserted by
the frontend client, the OpenAPI contract, and the e2e helpers. Adding a `meta` sibling
field would put a mostly-null field on *every* response including errors — a far wider
blast radius than changing the `data` of four admin endpoints whose only consumer is this
project's own admin console.

**Alternatives considered**:

- *`meta` beside `data` in `Envelope`*: touches one shared struct but changes the contract
  of ~40 endpoints that will never use it.
- *`X-Total-Count` header*: invisible to the envelope contract, easy for a client or proxy
  to drop, and awkward to express in the existing `allOf: [Envelope, …]` OpenAPI style.

**Consequence, stated plainly**: this is a breaking change to four endpoint response
shapes. Acceptable because they are admin-only and have exactly one first-party consumer,
which changes in the same commit.

---

## R3 — Count and slice: two queries, count first

**Decision**: Each surface gets a `Count…` query and a paged `List…` query. The service
reads the count, clamps the requested page against it, then reads the page.

**Rationale**: FR-013 requires a page beyond the end to resolve to the last page rather
than error or show an empty table. Clamping needs the total *before* the slice is taken,
so the count has to come first whatever else is true. Two cheap round-trips on a read path
that is cached anyway is a good trade for that.

**Alternatives considered**:

- *`COUNT(*) OVER()` in the paged query*: one round trip, and normally the better trick —
  but it returns **no rows and therefore no count** when the offset is past the end, which
  is precisely the case FR-013 exists to handle. It would need a second query as a fallback
  anyway, at which point it is the more complex option.
- *Estimated counts from `pg_class.reltuples`*: cheap, but wrong under a filter, and the
  spec's assumption section commits to exact counts at this product's volume.

**Cost accepted**: the `WHERE` clause is written twice per surface. Mitigated by keeping
each count query immediately above its list query in the same `.sql` file with a comment
binding them, and by a database-backed test that asserts the two agree under the same
filter.

---

## R4 — Stable ordering is not currently guaranteed

**Decision**: Append the primary key as a final tiebreaker to all four list orderings.

**Evidence** — every one of these can tie today:

| Surface | Current `ORDER BY` | Tie risk |
|---|---|---|
| Orders ([`order.sql:223`](../../backend/internal/order/queries/order.sql)) | `o.created_at DESC` | Two orders created in the same instant |
| Attendees ([`order.sql:234`](../../backend/internal/order/queries/order.sql)) | `o.created_at DESC, a.name ASC` | High — one order's attendees share `created_at`, and duplicate names are ordinary |
| Events ([`event.sql:88`](../../backend/internal/event/queries/event.sql)) | `start_date DESC` | High — events on the same day are routine |
| Fees ([`order.sql:362`](../../backend/internal/order/queries/order.sql)) | `position, name` | Two fees at the same position with the same name |

Without a total order, PostgreSQL is free to return tied rows differently between the
`OFFSET 0` read and the `OFFSET 20` read, which shows an operator a duplicate on page 2
and hides a different record entirely. That is exactly the failure FR-008 and SC-004
forbid, and it is invisible in a single unpaginated read — which is why it has not bitten
yet.

**FR-009 compliance**: adding a tiebreaker changes the order only of rows that previously
had no defined order between them. The first record an operator sees is unchanged.

---

## R5 — Cache: fingerprints extend, families do not

**Decision**: Add page and page size to the fingerprint built by `OrdersAdminKey`,
`AttendeesAdminKey` and `EventsAdminKey` in
[`pkg/cache/surfaces.go`](../../backend/pkg/cache/surfaces.go). Add no `Family`, no
`ScopeKind`, and no constructor.

**Why this is Principle VII-clean**: the principle closes the list of *surfaces*
(`events`, `ticket_types`, packages, `orders`, `attendees`) and explicitly admits
"filtered variants" of each. A page is a filtered variant of a list already on that list —
same rows, same admin projection, invalidated by the same writes. `Family` is also the
Prometheus label, and it does not change, so no page number or filter value can leak into
a metric label.

**Invalidation still works with one command**: `Key.Scope` is untouched, so every page of
every filter variant of the order list still derives from the single `gen:orders` counter.
One committed write, one `INCR`, every page orphaned — which is the property that made the
existing design able to absorb filter variants in the first place.

**Entry cardinality**: entries multiply by (pages × offered sizes). Bounded by the
enforced maximum page size, by the fact that operators cluster on the first pages, and by
the existing TTL backstop. Orphaned generations are already handled by the generation
counter — stale entries are unreachable, not merely stale.

**Fees are out of the cache entirely**: `fees` is not in the `Families` registry and
`AdminService.Fees` reads the repository directly
([`admin_service.go:245`](../../backend/internal/order/admin_service.go)). Paginating it
touches no cache code, and it must stay that way — adding it *would* require a
constitutional amendment.

---

## R6 — The event filter dropdown breaks unless something is done

**Finding**: [`orders/page.tsx`](../../frontend/app/\(admin\)/admin/orders/page.tsx) and
[`attendees/page.tsx`](../../frontend/app/\(admin\)/admin/attendees/page.tsx) both call
`useAdminEvents()` to populate their event `<Select>`. That is the same hook, and the same
endpoint, that User Story 3 paginates.

**Decision**: Add `GET /api/v1/admin/events/options`, returning `{id, name}` for every
event ordered by name, unpaginated and uncached.

**Rationale**: it separates "the events table an operator browses" from "the list of
events a filter can name", which are genuinely different reads that only looked like one
read while neither was paginated. Uncached is deliberate: the payload is two columns, and
leaving it out of the cache avoids any argument about whether a selector projection is
inside Principle VII's closed surface list.

**Alternatives considered**:

- *Dropdown requests `page_size=100`*: a silent cap that produces a wrong filter at the
  101st event, with no error and no way for an operator to notice.
- *Make the dropdown itself paged or type-ahead*: more UI than the problem warrants, and
  it would make filtering harder than it is today.

---

## R7 — URL state must carry filters, not just the page

**Decision**: `page`, `page_size` **and** the existing filters (`status`, `event_id`) live
in the URL search params. Filters move out of `useState`.

**Rationale**: SC-006 says a shared address reopens on the same page for a second
operator. If filters stay in component state, a shared link lands on page 3 of a
*different* result set — the page number is preserved and the meaning is not. Putting both
in one place is also what makes FR-010's "changing a filter returns to page 1" a single
obvious operation rather than two pieces of state to keep in sync.

**Mechanism**: a `useListParams` hook over `useSearchParams` + `router.replace`, so paging
does not push a history entry per click while the back button still leaves the list. The
hook is the single place that clamps and defaults, so all four pages behave identically
(SC-005).

---

## R8 — Invalid paging input clamps; invalid filter input still 400s

**Decision**: Out-of-range or malformed `page`/`page_size` resolve to the nearest valid
value (FR-013). A malformed `status` or `event_id` keeps returning 400 as it does today
([`admin_handler.go:89`](../../backend/internal/order/admin_handler.go)).

**Rationale**: the asymmetry is deliberate and worth naming. A bad filter value means the
caller asked for something that does not exist and should be told. A bad page number means
the caller asked for a position that has drifted — after a deletion, from a stale bookmark
— and the useful answer is the nearest real page, which is what SC-007 requires for any
value a person could type into an address bar.

**Enforced maximum**: page size caps at 100 (FR-014), applied server-side. A client asking
for 5,000 gets 100, not an error and not 5,000.

---

## R9 — No schema migration

**Decision**: No migration, therefore no `SCHEMA.md` change.

**Rationale**: `orders.created_at` and `events.start_date` are unindexed today
([`SCHEMA.md:320-329`](../../SCHEMA.md)), so the sort is a full scan plus sort. At 10k
orders that is comfortably inside SC-001's 2-second budget, and adding an index for a
cost that has not been measured is speculation.

**Condition under which this changes**: if the SC-001 benchmark in `quickstart.md` misses
its budget, the fix is an index on `orders(created_at DESC, id DESC)` and the matching
`attendees`/`events` orderings — and that migration lands **in the same commit as the
`SCHEMA.md` update**, per the repository rule. This is recorded so the decision is a
measurement, not an omission.

---

## R10 — Test tiers

**Decision**: All three tiers change, as Principle VIII requires and as `AGENTS.md`
restates.

- **Go, database-backed**: count/list agreement under each filter; clamping; the
  tiebreaker actually producing a total order; page 2 disjoint from page 1.
- **Go, cache**: `admin_cache_test.go` gains a case proving two different pages take
  different keys, and one proving a write invalidates both.
- **Frontend Vitest**: the `Pagination` component's disabled states, keyboard operation,
  and aria announcement (FR-017); `useListParams` clamping and filter-resets-page.
- **e2e**: the four scenarios named in the plan's Principle VIII rows, run at a small
  `page_size` so that seeding stays honest — orders are created through the real booking
  API, never inserted into the database.

**Note on e2e arrangement**: the cheap way to get a multi-page list would be to insert
rows directly. Principle VIII forbids it, and for a reason that bites here specifically —
direct writes do not invalidate the cache, so a paging test seeded that way could pass
against a stale cache and prove nothing. Small page sizes are how the suite gets multiple
pages out of a handful of real orders.

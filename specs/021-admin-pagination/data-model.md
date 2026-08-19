# Phase 1 Data Model: Admin Console Pagination

**No table, column, index, or migration is added or changed by this feature.** `SCHEMA.md`
is therefore untouched. Everything below is a transport and query-shape model over rows
that already exist.

---

## 1. Transport types

### `PageRequest` (backend, `pkg/httpx/page.go`)

What a caller asked for, after validation.

| Field | Type | Rule |
|---|---|---|
| `Page` | `int` | 1-based. Values `< 1`, absent, or unparseable resolve to `1`. |
| `Size` | `int` | Absent or unparseable resolves to `20`. `< 1` resolves to `20`. `> 100` resolves to `100`. |

Derived: `Offset() = (Page - 1) * Size`, `Limit() = Size`.

Clamping is total — `PageRequest` cannot hold an invalid value, so no downstream code
re-checks (FR-013, FR-014, SC-007).

### `Page[T]` (backend, `pkg/httpx/page.go`)

What the endpoint returns inside the existing `{code, message, data}` envelope. `T` is
always the owning domain's own `dto.go` type, so Principle III's requirement that each
domain define its wire shape is untouched.

| Field | JSON | Type | Meaning |
|---|---|---|---|
| `Items` | `items` | `[]T` | The rows on this page. Never `null` — an empty page marshals as `[]`. |
| `Page` | `page` | `int` | The page actually served, after clamping. May differ from what was requested. |
| `PageSize` | `page_size` | `int` | The size actually applied, after clamping. |
| `Total` | `total` | `int64` | Rows matching the filters, ignoring paging (FR-004). |
| `TotalPages` | `total_pages` | `int` | `ceil(Total / PageSize)`, and `0` when `Total` is `0`. |

**Invariants** (assert these in tests, not just in prose):

- `len(Items) <= PageSize` always (SC-002).
- `Total == 0` ⟹ `Items` is empty, `TotalPages == 0`, `Page == 1`.
- `Total > 0` ⟹ `1 <= Page <= TotalPages`.
- Concatenating `Items` across `Page = 1..TotalPages` against a static dataset yields every
  matching row exactly once (SC-004) — which holds only because of the ordering rule in §3.

### `Page<T>` (frontend, `lib/types.ts`)

The mirror image, consumed by `adminFetch<Page<OrderSummary>>(…)`. Same five fields, same
names.

---

## 2. Query parameters

Applied identically on all four list endpoints.

| Param | Type | Default | Invalid input |
|---|---|---|---|
| `page` | integer ≥ 1 | `1` | Clamped, never rejected |
| `page_size` | integer 1–100 | `20` | Clamped, never rejected |

Existing filters are unchanged in name, meaning and validation: `status` and `event_id` on
orders, `order_id` and `event_id` on attendees. They keep returning **400** when malformed
— see [research.md R8](./research.md) for why the two kinds of input are treated
differently.

**Order of operations**: filter → count → clamp page against count → order → offset →
limit. Filtering before counting is what makes `total` mean "matching the current filters"
rather than "in the table" (FR-010).

---

## 3. Ordering (the correctness-critical part)

Each list gains its primary key as a final tiebreaker so the ordering is *total*, not
merely sorted. Without this, tied rows may be returned in different relative orders by two
different `OFFSET` reads, duplicating one record onto page 2 while hiding another
entirely.

| List | Ordering after this change |
|---|---|
| Orders | `o.created_at DESC, o.id DESC` |
| Attendees | `o.created_at DESC, a.name ASC, a.id ASC` |
| Events | `start_date DESC, id DESC` |
| Fees | `position, name, id` |

The leading keys are unchanged, so the record an operator sees first is the same as today
(FR-009). Evidence that every one of these can tie today is in
[research.md R4](./research.md).

---

## 4. Query pairs

Each surface gets a count query and a paged list query, sharing one `WHERE` clause written
twice. They live adjacent in the `.sql` file, and a database-backed test asserts they agree
under the same filter.

| Surface | Count | Page | File |
|---|---|---|---|
| Orders | `CountOrdersAdmin` (new) | `ListOrdersAdmin` (+ `LIMIT`/`OFFSET`) | `internal/order/queries/order.sql` |
| Attendees | `CountAttendeesAdmin` (new) | `ListAttendeesAdmin` (+ `LIMIT`/`OFFSET`) | `internal/order/queries/order.sql` |
| Fees | `CountFees` (new) | `ListFees` (+ `LIMIT`/`OFFSET`) | `internal/order/queries/order.sql` |
| Events | `CountEvents` (new) | `ListEvents` (+ `LIMIT`/`OFFSET`) | `internal/event/queries/event.sql` |
| Event options | — | `ListEventOptions` (new, unpaginated) | `internal/event/queries/event.sql` |

**Preserved subtlety**: the orders and attendees `event_id` filter is not a column
comparison. `AdminService.ticketTypeScope` resolves the event to its ticket-type ids
through the injected `EventLookup`, and the SQL matches `ticket_type_ids`
([`admin_service.go:204`](../../backend/internal/order/admin_service.go)). The count query
**must** take the same `ticket_type_ids` parameter, and the "event has no ticket types"
short-circuit must return an empty page with `total = 0` rather than an unfiltered count.

---

## 5. New DTO

### `EventOption` (`internal/event/admin_dto.go`)

For filter dropdowns only. Two fields, deliberately — see
[research.md R6](./research.md).

| Field | JSON | Type |
|---|---|---|
| `ID` | `id` | UUID |
| `Name` | `name` | string |

Returned as a plain array in `data`, not a `Page`. It is not paginated, and pretending
otherwise would invite someone to paginate it later and reintroduce the truncated-filter
bug it exists to prevent.

---

## 6. Cache keys

No new `Family`, no new `Scope`, no new constructor — only wider fingerprints. Detail and
the Principle VII argument are in [research.md R5](./research.md).

| Key | Fingerprint before | Fingerprint after |
|---|---|---|
| `EventsAdminKey` | *(none)* | `p=<page>:n=<size>` |
| `OrdersAdminKey` | `st=<status>:ev=<event>` | `st=<status>:ev=<event>:p=<page>:n=<size>` |
| `AttendeesAdminKey` | `or=<order>:ev=<event>` | `or=<order>:ev=<event>:p=<page>:n=<size>` |

Fingerprint components stay in fixed order and are built with a `strings.Builder`, never
from map iteration, matching the existing constructors.

**Cached value**: the whole `Page[T]`, count included. A cache hit therefore serves the
same total the database would have, and never a page whose count came from a different
read.

**Invalidation is unchanged**: `Scope` is untouched, so `gen:orders` and `gen:events` still
orphan every page of every filter variant with a single `INCR` after commit.

**Fees**: not a cacheable family and not made one. The fee page read stays direct
(`AdminService.Fees` → repository), so paginating it involves no cache code at all.

---

## 7. Frontend state

`useListParams` (`frontend/lib/use-list-params.ts`) owns page, page size and filters as URL
search params.

| Key | Meaning | Omitted from the URL when |
|---|---|---|
| `page` | 1-based page | `1` |
| `page_size` | rows per page | default (`20`) |
| `status`, `event_id`, `order_id` | existing filters | unset |

Defaults are omitted so a first-visit URL stays clean and a shared URL carries only what
was actually chosen.

**Transitions**:

- Changing any filter → `page` resets to `1` (FR-010).
- Changing page size → land on the page containing the first row previously shown
  (US4 scenario 2), i.e. `newPage = floor(((oldPage - 1) * oldSize) / newSize) + 1`.
- Navigating pages uses `router.replace`, so a ten-page walk does not bury the previous
  screen under ten history entries — while the back button still leaves the list.
- The server's clamped `page` is authoritative: if it differs from the URL, the URL is
  rewritten to match, so an out-of-range bookmark self-corrects (FR-013).

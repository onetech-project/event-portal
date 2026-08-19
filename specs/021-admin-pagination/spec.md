# Feature Specification: Admin Console Pagination

**Feature Branch**: `021-admin-pagination`

**Created**: 2026-08-19

**Status**: Draft

**Input**: User description: "tambahkan pagination di halaman admin, untuk semua menu"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Work through the Orders list without waiting for all of it (Priority: P1)

An operator opens Orders to find a specific order a caller is asking about. Today the
page tries to present every order that has ever been placed in one screen, so it gets
slower every week and the operator scrolls through hundreds of rows. With pagination the
operator sees a fixed-size first page immediately, sees how many orders match their
current status/event filter in total, and moves forward or back a page at a time — or
jumps straight to a page — until they find the order.

**Why this priority**: Orders is the highest-volume admin list and the one used under
time pressure, while a guest is on the phone. It grows without bound with sales, so it is
the surface where the current all-at-once behavior degrades first and worst.

**Independent Test**: Seed more orders than one page holds, open Orders, and confirm the
first page renders a bounded number of rows, reports the total number of matches, and
that walking to the next page shows a different, non-overlapping set of orders. Delivers
value on its own with no other list changed.

**Acceptance Scenarios**:

1. **Given** more orders exist than fit on one page, **When** the operator opens Orders,
   **Then** only the first page of orders is listed, the total number of matching orders
   is shown, and page controls are available.
2. **Given** the operator is on page 1, **When** they go to the next page, **Then** the
   list shows the following set of orders, no order appears on both pages, and no order
   between the two pages is skipped.
3. **Given** the operator is on page 3, **When** they change the status filter, **Then**
   the list returns to page 1 and the reported total reflects the new filter.
4. **Given** the operator is on any page, **When** they open an order's payment detail
   and close it again, **Then** they are still on the same page of the same list.

---

### User Story 2 - Work through the Attendees list the same way (Priority: P1)

An operator opens Attendees to check who a ticket was bought for. This list has one row
per issued ticket, so it is the largest list in the console — larger than Orders. The
operator needs the same bounded page, total count, and page-to-page movement, with the
event filter still applied.

**Why this priority**: Attendees grows faster than Orders (multiple attendees per order)
and is the list most likely to become unusable first. It is P1 alongside Orders because
the two are used together and share the same problem.

**Independent Test**: Seed an event with more attendees than one page holds, open
Attendees, and confirm bounded rows, an accurate total, and correct forward/back
movement while the event filter is set.

**Acceptance Scenarios**:

1. **Given** an event with more attendees than fit on one page, **When** the operator
   filters to that event, **Then** the first page of that event's attendees is shown with
   the total count for that event.
2. **Given** the operator is on the last page, **When** they look at the page controls,
   **Then** moving further forward is not offered.

---

### User Story 3 - Events and Fees lists paginate consistently (Priority: P2)

An operator opens Events or Fees. These lists are smaller than Orders and Attendees but
still grow over time, and an operator should not have to learn a different interaction
per menu. Both present the same bounded page, the same total count, and the same page
controls.

**Why this priority**: Consistency across the console, and protection against the same
unbounded growth later. Lower than P1 because the pain today is smaller — these lists are
measured in tens, not thousands.

**Independent Test**: Seed more events (and separately, more fees) than one page holds,
open each menu, and confirm identical paging behavior to Orders.

**Acceptance Scenarios**:

1. **Given** more events exist than fit on one page, **When** the operator opens Events,
   **Then** the first page is listed with the total count and page controls.
2. **Given** a list whose matches all fit on one page, **When** the operator views it,
   **Then** the page controls do not offer a next or previous page.
3. **Given** the operator creates a new event from the Events page, **When** the list
   refreshes, **Then** the operator is returned to the page where the new record appears
   under the list's ordering.

---

### User Story 4 - Return to, share, and size a specific page (Priority: P3)

An operator on page 4 of Orders sends the link to a colleague, or uses the browser back
button after opening an order. The page they were on is part of the address, so the link
opens on page 4 and back returns to page 4. The operator can also choose how many rows a
page holds when they want a denser view.

**Why this priority**: A quality-of-life layer on top of paging that already works.
Valuable, but the console is usable without it.

**Independent Test**: Navigate to page 3 of a list, copy the address, open it in a fresh
session, and confirm it lands on page 3; change the page size and confirm the row count
and page count change accordingly.

**Acceptance Scenarios**:

1. **Given** the operator is on page 3 of a list, **When** they reload the browser,
   **Then** they are still on page 3 with the same rows.
2. **Given** the operator changes the page size from the default to a larger size,
   **When** the list refreshes, **Then** the list shows that many rows per page, the
   total page count is recalculated, and the operator is placed on the page containing
   the first row they were previously viewing.
3. **Given** an address requests a page beyond the last page, **When** the list loads,
   **Then** the last available page is shown rather than an error or an empty table.

---

### Edge Cases

- **No matches at all**: the list shows its existing empty-state message and no page
  controls, not "page 1 of 0".
- **Exactly one page of matches**: no forward or back movement is offered.
- **Requesting a page past the end** (bookmark used after records were deleted, or a
  hand-edited address): the last available page is shown.
- **A malformed page number or page size in the address** (zero, negative, non-numeric,
  or larger than the permitted maximum): the value is corrected to the nearest valid one
  and the list still loads.
- **The last row on the last page is deleted** (a fee, an event): the operator is moved
  to the new last page rather than left looking at an empty table.
- **Records are created or changed between two page views**: no error; because ordering
  is stable and deterministic, a row may shift position but the operator is never shown a
  page that silently drops a record that was neither created nor deleted.
- **Filter narrows the result below the current page**: the operator returns to page 1
  rather than being stranded on an out-of-range page.
- **The list is slow to load a page**: the previously loaded page stays visible with a
  loading indication rather than the table blanking out.
- **A total count is momentarily unavailable**: paging still works; the count area shows
  a neutral placeholder rather than blocking the list.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: Every admin list surface in scope MUST return and display at most one
  page of records at a time, never the complete result set.
- **FR-002**: The lists in scope are exactly the four list-bearing top-level admin nav
  menus: Orders, Attendees, Events, and Fees.
- **FR-003**: The Validate menu MUST be unchanged; it presents a single scan result, not
  a list, and has nothing to paginate.
- **FR-003a**: The lists nested inside a single event's detail page — ticket types,
  packages, and the content blocks (activities, guest stars, guidelines) — MUST be left
  unchanged by this feature. They are reached from within an event, not from the nav, and
  are explicitly out of scope.
- **FR-004**: Each paginated list MUST report the total number of records matching the
  operator's current filters, independent of how many are on the current page.
- **FR-005**: Each paginated list MUST offer controls to move to the next page, to the
  previous page, and to jump directly to a numbered page, including the first and last.
- **FR-006**: Forward and backward movement MUST be offered only where a page exists in
  that direction.
- **FR-007**: The amount of data transferred and the time to render a list page MUST NOT
  grow with the total number of records — only with the page size. Fetching every record
  and slicing it in the browser does not satisfy this requirement.
- **FR-008**: Records MUST be ordered deterministically and stably within each list, so
  that consecutive pages neither repeat nor skip a record that was not created or deleted
  between the two reads.
- **FR-009**: Each list's existing ordering MUST be preserved; pagination MUST NOT change
  which record an operator sees first.
- **FR-010**: Existing filters (order status, event) MUST continue to work, MUST be
  applied before paging, and changing any filter MUST return the operator to the first
  page.
- **FR-011**: The current page and page size MUST be part of the page address, so that
  reloading, bookmarking, sharing, or using the browser back button returns the operator
  to the same page.
- **FR-012**: Operators MUST be able to choose the page size from a small set of
  offered sizes, with a default applied when none is chosen.
- **FR-013**: A requested page beyond the last available page MUST resolve to the last
  available page; an out-of-range or malformed page size MUST resolve to the nearest
  permitted value. Neither MUST produce an error.
- **FR-014**: A request MUST NOT be able to demand an unbounded or arbitrarily large page;
  the permitted page size MUST have an enforced maximum.
- **FR-015**: Creating, updating, or deleting a record from within a paginated list MUST
  leave the operator on a valid page of the refreshed list, and the refreshed list MUST
  reflect the write.
- **FR-016**: Empty results MUST continue to show each list's existing empty state, with
  no page controls.
- **FR-017**: Page controls MUST be operable by keyboard and expose their current page and
  total page count to assistive technology.
- **FR-018**: Pagination MUST NOT weaken the admin authorization already required for
  these lists; an unauthenticated request for any page MUST still be refused.
- **FR-019**: The system MUST remain correct with the read cache disabled, and MUST
  remain correct after the cache is flushed — a cached page MUST NOT be the only place a
  page of results exists, and a committed write MUST still be visible on the next read of
  an affected page.
- **FR-020**: End-to-end acceptance coverage MUST be extended in the same change to
  exercise paging on the covered admin lists, including a multi-page walk and the
  filter-resets-to-first-page behavior.

### Key Entities

- **Page of records**: a bounded slice of one list's filtered, ordered results, together
  with the position it was taken from, the size that was requested, and the total number
  of records the filter matched. Not stored — computed per read.
- **Paging selection**: the operator's current position and chosen page size for a list,
  carried in the page address rather than persisted server-side.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With 10,000 orders and 25,000 attendees in the system, an operator sees the
  first page of either list within 2 seconds, and that time does not measurably increase
  when the record count doubles.
- **SC-002**: The number of records delivered to the browser for a list view never
  exceeds the operator's chosen page size, regardless of how many records match.
- **SC-003**: An operator can reach any specific page of a list in at most two
  interactions from the list's first page.
- **SC-004**: Walking every page of a list from first to last returns each matching
  record exactly once, with none repeated and none missed, in 100% of runs against a
  static data set.
- **SC-005**: Every admin list menu in scope presents the same paging interaction — same
  controls, same placement, same count wording — so an operator who has used one needs no
  additional instruction for the others.
- **SC-006**: A shared or bookmarked list address reopens on the same page for a second
  operator in 100% of attempts.
- **SC-007**: No list view returns an error or a blank table for any page number or page
  size a person could type into the address, including zero, negative, non-numeric, and
  values far beyond the last page.
- **SC-008**: The acceptance suite passes with the read cache both enabled and disabled.

## Assumptions

- "Semua menu" was confirmed to mean the top-level admin nav menus, and only those. The
  Validate menu and the Login page have no list and are excluded; the admin landing page
  is excluded unless it gains a list; the lists inside an event's detail page are out of
  scope (FR-003a). Ticket-type and package lists will keep growing with an event's
  catalogue, so they are a likely follow-up rather than a closed question.
- Paging is applied at the data source, not by fetching everything and slicing in the
  browser — otherwise the feature would change the interface without removing the cost it
  exists to remove. This is stated as a requirement (FR-007) rather than left to
  implementation.
- The default page size is 20 rows, with 20 / 50 / 100 offered and 100 enforced as the
  maximum. These numbers are a reasonable default for an admin table and can be retuned
  without changing the specification's intent.
- Numbered page controls (rather than infinite scroll or a "load more" button) are the
  right pattern here, because operators need to return to a known position and share it.
- Existing list ordering is already appropriate for operators and is not being revisited
  by this feature; where an existing list's ordering is not already deterministic, making
  it so is in scope only to the extent FR-008 requires.
- Total counts are exact rather than estimated. At the record volumes this product
  handles, an exact count is affordable.
- No new permissions, roles, or authorization rules are introduced; the lists remain
  admin-only exactly as they are today.
- The existing read cache continues to be a disposable accelerator. Whether individual
  pages are cached is an implementation choice; correctness with the cache off is not.
- Sorting by column, full-text search over these lists, and exporting a whole list are
  separate concerns and are out of scope for this feature.

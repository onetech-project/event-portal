# Quickstart: Validating Admin Console Pagination

How to prove this feature works. Each section states what it checks and which requirement
it discharges. No implementation code here — see [contracts/api.md](./contracts/api.md) for
shapes and [data-model.md](./data-model.md) for rules.

## Prerequisites

The runner does not own the infrastructure. Start it first:

```bash
REDIS_PORT=6380 docker compose up -d postgres redis mailpit
docker compose run --rm migrate up
```

Mailpit is not needed by this feature but is required for the delivery specs that share
the suite.

---

## 1. Backend tiers

```bash
cd backend && ./scripts/test.sh ./...
```

**Must cover, or the tier has not verified this feature:**

| Check | Discharges |
|---|---|
| Count and page queries agree under each filter combination | FR-004 |
| Page 2 is disjoint from page 1, and the two together equal an unpaginated read | FR-008, SC-004 |
| A page past the last resolves to the last page, not an error or an empty table | FR-013, SC-007 |
| `page_size` above the maximum is reduced to it | FR-014 |
| `page=0`, `page=-3`, `page=abc`, `page_size=0`, `page_size=abc` all return 200 | SC-007 |
| A malformed `status` or `event_id` still returns 400 | FR-010, no regression |
| An `event_id` whose event has no ticket types returns `total: 0`, not an unfiltered count | FR-010 |
| Tied rows order identically across two different offset reads | FR-008 |
| Two different pages take two different cache keys | Principle VII |
| A committed write invalidates every page of the affected list | Principle VII |

The tiebreaker test is the one worth writing carefully: seed rows that genuinely tie on
the leading sort key (two orders created in the same instant, two events on the same day,
one order's attendees sharing a name) and assert a full walk returns each exactly once.
That failure mode is invisible without a tie.

## 2. Frontend tier

```bash
cd frontend && npx vitest run
```

**Must cover:**

| Check | Discharges |
|---|---|
| `Pagination` disables previous on page 1 and next on the last page | FR-006 |
| A single-page result renders no forward/back affordance | FR-006, US3 |
| An empty result renders the existing empty state and no controls | FR-016 |
| Controls are reachable and operable by keyboard, and announce current/total page | FR-017 |
| `useListParams` clamps garbage, omits defaults from the URL, and resets page on a filter change | FR-010, FR-013 |
| Changing page size lands on the page holding the previously first-visible row | US4 scenario 2 |

## 3. Acceptance gate

```bash
cd e2e && npm test                    # headless
cd e2e && npm run test:slow           # headed, for watching it happen
E2E_CACHE_ENABLED=false npm test      # Principle VII's kill switch, SC-008
```

Constitution Principle VIII makes this the gate, not a bonus tier. Scenarios to add in
`e2e/specs/admin-console.spec.ts`:

1. **Multi-page walk** — arrange enough orders through the real booking API, open Orders
   at a small `page_size`, walk every page, and assert each order number appears exactly
   once across the whole walk.
2. **Filter resets to page 1** — go to page 2, change the status filter, assert page 1 and
   a total consistent with the new filter.
3. **Out-of-range page clamps** — navigate directly to `?page=999`, assert the last page
   renders and the address corrects itself.
4. **Shared address reopens the same view** — copy the address from page 2 with a filter
   applied, open it fresh, assert the same page and the same filter.

Plus one in `e2e/specs/cache-refresh.spec.ts`: a write invalidates the page it belongs to,
and the change is visible on the next read of that page in both cache modes.

**Arrangement rule, and it matters here specifically**: never insert orders, tickets or
payment state directly into the database to get a multi-page list. Direct writes do not
invalidate the cache, so a paging test seeded that way can pass against a stale cache and
prove nothing. Use a small `page_size` to get several pages out of a handful of genuinely
booked orders.

---

## 4. Manual walkthrough

With the stack running, sign in at `/admin/login` and check each menu — **Orders**,
**Attendees**, **Events**, **Fees**:

- The first page renders with a bounded number of rows and a total ("Showing 1–20 of 137").
- Next/previous move a page; jumping to a numbered page works; first and last are reachable.
- The address bar shows `page` and `page_size`; reloading keeps the position; the back
  button behaves.
- Applying a filter returns to page 1 and the total changes with it.
- The four menus look and behave the same (SC-005 is a judgement call — make it by
  looking at all four).

Two specific things to confirm because they are the ones this feature could quietly break:

- **The event filter dropdown on Orders and Attendees still lists every event**, not just
  the first page of them ([research.md R6](./research.md)). With more events than one page
  holds, confirm an event from the second page is still selectable as a filter.
- **Deleting the last fee on the last page** leaves the operator on a valid page showing
  rows, not an empty table.

---

## 5. Performance check (SC-001)

Seed roughly 10,000 orders and 25,000 attendees and measure time to first page for Orders
and Attendees, with the cache disabled so the database path is what is being measured.
Budget is 2 seconds, and the figure should not move materially when the row count doubles.

If it misses, the remedy is an index on the ordering columns — and per the repository
rule, that migration lands **in the same commit as the `SCHEMA.md` update**. It is
deliberately not pre-emptive: see [research.md R9](./research.md).

**Measured on 2026-08-19**, cache off, against the real API:

| Dataset | Read | Time |
|---|---|---|
| 10k orders | `/admin/orders?page=1&page_size=20` | 7.4 ms |
| 10k orders | `/admin/orders?page=250` (deep offset) | 7.2 ms |
| 25k attendees | `/admin/attendees?page=1&page_size=20` | 12.9 ms |
| 25k attendees | `/admin/attendees?page=625` (deep offset) | 18.5 ms |
| 20k orders (doubled) | `/admin/orders?page=1&page_size=20` | 11.3 ms |
| 35k attendees | `/admin/attendees?page=1&page_size=20` | 33.8 ms |

Two honest readings of this. The 2-second budget is met with a margin of roughly 60×,
and deep offsets cost no more than shallow ones — `OFFSET 5000` is as cheap as `OFFSET 0`
at these volumes, which is what made cursor paging unnecessary (research R1).

But the second half of SC-001 — "does not measurably increase when the record count
doubles" — is **not literally true**: doubling the orders moved page 1 from 7.4 ms to
11.3 ms, and the count query plus the sort is why. It grows sub-linearly and from a very
low base, so no index was added; the criterion's practical intent is comfortably met and
its literal wording is not. Recorded here rather than rounded off, because the number to
watch is the growth rate, and the next person to run this needs the earlier figures to
compare against.

---

## 6. Contract check

`api/openapi.yml` must describe what the API actually does before this is done: four
modified paths, one new path, `PageMeta`, `EventOption`, and the two shared parameters.
A contract that still advertises a bare array is a contract that is wrong.

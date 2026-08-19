# Implementation Plan: Admin Console Pagination

**Branch**: `021-admin-pagination` (spec directory; the git working branch is still `main` — no branch hook is registered) | **Date**: 2026-08-19 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/021-admin-pagination/spec.md`

## Summary

Four admin list endpoints (`/admin/orders`, `/admin/attendees`, `/admin/events`,
`/admin/fees`) currently return every matching row. Each gains `page` and `page_size`
query parameters and returns a paged envelope carrying the rows plus the exact total, so
response size and render time track the page size rather than the table size (FR-001,
FR-007).

The approach is offset paging with an exact count: a `Count…` query and a
`LIMIT/OFFSET` query per surface, with the service reading the count first so it can
clamp an out-of-range page to the last page instead of erroring (FR-013). Every list's
`ORDER BY` gains its primary key as a final tiebreaker so consecutive pages cannot repeat
or skip a row (FR-008). Paging parameters join the existing cache-key fingerprints — same
families, same scopes, so one `INCR` still invalidates every page of every filter variant
and Principle VII needs no amendment.

On the frontend a single `Pagination` component and a `useListParams` hook put page, page
size **and the existing filters** in the URL, which is what makes a shared address
reproduce the view (FR-011, SC-006) and gives all four menus one identical interaction
(SC-005).

One dependency the spec did not name surfaced during research and is in scope because
without it FR-010 regresses: the Orders and Attendees pages fill their event filter
dropdown from `useAdminEvents()`. Paginating that endpoint would silently truncate the
dropdown to one page of events, so a small `GET /admin/events/options` read is added for
selectors.

## Technical Context

**Language/Version**: Go 1.26.5 (backend), TypeScript / React 19.2 on Next.js 16.2 (frontend)

**Primary Dependencies**: Echo v4, `sqlc`, `pgx/v5`, `go-redis`; TanStack Query v5, Base UI, Tailwind v4

**Storage**: PostgreSQL (source of truth), Redis (disposable read cache, Principle VII)

**Testing**: `backend/scripts/test.sh` (Go unit + database-backed), `npx vitest run` (frontend), `e2e/` Playwright against the real stack

**Target Platform**: Linux server API + browser admin console

**Project Type**: Web application — Go modular monolith backend, Next.js frontend, shared OpenAPI contract

**Performance Goals**: First page of any admin list within 2s at 10k orders / 25k attendees, and flat as the table doubles (SC-001); bytes delivered bounded by page size (SC-002)

**Constraints**: No new cache family (Principle VII's surface list is closed); no cache or gateway call inside a transaction (this feature opens none); correctness identical with `CACHE_ENABLED=false`; no schema migration planned, so `SCHEMA.md` is untouched

**Scale/Scope**: 4 list endpoints + 1 new selector endpoint; 4 admin pages; 1 shared UI component; 8 new/edited `sqlc` queries; 3 test tiers

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Basis |
|---|---|---|
| I. Modular Monolith | PASS | Paging stays inside the existing `order` and `event` domains; the shared `Page[T]` wrapper goes to `pkg/httpx` beside `Envelope`, which is where shared non-domain HTTP concerns already live. |
| II. Domain Isolation | PASS | No new cross-domain import. The `order` domain keeps reaching event data through the injected `EventLookup`; the new selector read lives in the `event` domain that owns the table. `cmd/api/architecture_test.go` continues to enforce this. |
| III. DTO Isolation | PASS, with the reasoning stated | No `sqlc` struct reaches a response. `Page[T]` is a generic wrapper whose `T` is always the domain's own `dto.go` type, so each domain still defines its exact row shape. Placing the wrapper in `pkg/httpx` follows the precedent already set by `httpx.Envelope`, which every domain likewise shares. |
| IV. Transactional Integrity | PASS — not exercised | Read-only feature. No transaction is opened, no quota is touched, no idempotency key is involved. |
| V. Payment Gateway Abstraction | PASS — not exercised | No payment surface changes. |
| VI. Guest-First MVP Scope | PASS | Admin-only. No guest surface is paginated; the public catalogue keeps returning whole lists. |
| VII. Cache as Disposable Read Accelerator | PASS — no amendment needed | Paging parameters extend the **fingerprint** of existing keys in existing families (`orders_admin`, `attendees_admin`, `events_admin`). No family is added, so the closed surface list is unchanged. Scopes are unchanged, so one `INCR` still orphans every page of every variant. Fees are not a cacheable family and stay uncached. `cache.Through` keeps fail-open and miss-collapsing intact. Detail: [research.md R5](./research.md). |
| VIII. End-to-End Acceptance Coverage | PASS — see rows below | |
| IX. Request Throttling | PASS — not exercised | No throttle is added, removed, or retuned. Admin lists are unthrottled today and stay so. |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes. `e2e/specs/admin-console.spec.ts`
      covers the admin order list, which is named explicitly in Principle VIII's covered
      flows, and `e2e/specs/cache-refresh.spec.ts` covers cache coherence over these same
      admin lists. Both must change in this commit. `e2e/support/journey.ts` (the
      `AdminConsole` page object) and `e2e/support/api.ts` (`adminOrders`) change with them.
- [x] **New user-visible flows and their scenarios.** Paging controls are new UI on four
      covered pages. New scenarios: a multi-page walk that asserts no repeats and no gaps;
      changing a filter returns to page 1; an out-of-range page in the address clamps to
      the last page; a copied address reopens on the same page with the same filter.
      Scenarios run with a deliberately small `page_size` so the arrangement stays honest —
      orders are still created through the real booking API, never written into the
      database (Principle VIII, "no shortcuts through the back door").
- [x] **Is this a bugfix in a covered flow?** No. It is new behavior, so the
      "must fail before the fix" rule does not apply. The equivalent discipline here is
      that each new scenario is written against the unpaginated endpoint first and
      confirmed red.
- [x] **Does anything change behavior under `E2E_CACHE_ENABLED=false`?** No, and that is
      the point: paging is computed in SQL, so a cache miss and a cache hit return the same
      page. The suite must pass in both modes (SC-008), and the cache-coherence spec gains
      a paged variant proving a write invalidates the page it belongs to.

**Post-Phase-1 re-check**: unchanged. The Phase 1 design introduced no new cache family,
no cross-domain import, no transaction, and no migration. The one addition beyond the
spec's literal scope — the `/admin/events/options` selector read — is justified in the
Complexity Tracking table below rather than absorbed silently.

## Project Structure

### Documentation (this feature)

```text
specs/021-admin-pagination/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── api.md           # Phase 1 output — endpoint contracts
├── checklists/
│   └── requirements.md  # From /speckit-specify
└── tasks.md             # /speckit-tasks output — NOT created here
```

### Source Code (repository root)

```text
backend/
├── pkg/httpx/
│   ├── envelope.go            # unchanged
│   └── page.go                # NEW — Page[T], PageRequest, clamping rules
├── pkg/cache/
│   └── surfaces.go            # EDIT — page/size join the key fingerprints
├── internal/order/
│   ├── admin_handler.go       # EDIT — parse+clamp page params on orders, attendees, fees
│   ├── admin_service.go       # EDIT — count-then-page, clamp, cache keys
│   ├── dto.go                 # EDIT — OrderFilter/AttendeeFilter gain paging
│   ├── repository.go          # EDIT — count + paged fee reads
│   └── queries/order.sql      # EDIT — Count*/List* pairs, stable ORDER BY
├── internal/event/
│   ├── admin_handler.go       # EDIT — paged event list + /admin/events/options
│   ├── admin_service.go       # EDIT — count-then-page, cache key
│   ├── admin_dto.go           # EDIT — EventOption
│   ├── repository.go          # EDIT
│   └── queries/event.sql      # EDIT — CountEvents, paged ListEvents, ListEventOptions
└── cmd/api/                   # route registration only if a new path needs mounting

api/openapi.yml                # EDIT — 4 paths gain params + paged data; 1 path added

frontend/
├── components/ui/
│   └── pagination.tsx         # NEW — shared controls, page-size select, a11y
├── lib/
│   ├── use-list-params.ts     # NEW — page/size/filters <-> URL search params
│   ├── queries.ts             # EDIT — admin list hooks take paging, new options hook
│   └── types.ts               # EDIT — Page<T>, EventOption
└── app/(admin)/admin/
    ├── orders/page.tsx        # EDIT
    ├── attendees/page.tsx     # EDIT
    ├── events/page.tsx        # EDIT
    └── fees/page.tsx          # EDIT

e2e/
├── specs/admin-console.spec.ts   # EDIT — paging scenarios
├── specs/cache-refresh.spec.ts   # EDIT — a paged coherence scenario
└── support/{api,journey}.ts      # EDIT — paged helpers, page-object methods
```

**Structure Decision**: The existing web-application split is kept exactly as it is —
`backend/` (Go modular monolith with `internal/<domain>/` and shared `pkg/`), `frontend/`
(Next.js App Router), `api/openapi.yml` as the shared contract, and `e2e/` as the
acceptance gate. This feature adds two files to `backend/pkg`, two to `frontend`, and
otherwise edits in place. No new domain, no new package boundary.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| A fifth admin endpoint, `GET /admin/events/options`, beyond the spec's four lists | The Orders and Attendees pages build their event filter from the full admin event list. Paginating that list would truncate the dropdown to 20 events and silently break the filter, regressing FR-010. | Asking the dropdown for `page_size=100` was rejected: it is a cap that fails silently at the 101st event, which is the same defect one size larger. Making the dropdown itself paginated was rejected as worse UX than the problem it solves. |
| `Page[T]` in `pkg/httpx` rather than a paged DTO per domain | Four surfaces across two domains must return an identical shape for SC-005 to hold, and duplicating it four times invites drift. | Per-domain wrappers were rejected as duplication with no isolation benefit: `T` is still the domain's own DTO, so Principle III's actual requirement — the domain owns its wire shape — is untouched. |
| Two SQL queries per surface (count, then page) instead of one `COUNT(*) OVER()` | FR-013 requires an out-of-range page to clamp to the last page, which cannot be computed from a page that came back empty. The count must precede the slice regardless. | The window-function form was rejected because it returns no count at all for an over-the-end page — exactly the case FR-013 is about. See [research.md R3](./research.md). |

# Implementation Plan: Master Data Read Cache

**Branch**: `023-master-data-cache` | **Date**: 2026-08-24 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/023-master-data-cache/spec.md`

## Summary

Serve the gender master list from Redis through the existing read-through path, so the four
request paths that need it stop spending a database connection per visit on two rows that only a
migration changes.

The technical approach is deliberately small, and the shape of it is set by one finding: there are
**three gender consumers in Go but only two distinct database queries**. Caching belongs at the
query boundary, not the consumer boundary — two cache keys serve all three consumers, and the
shaping each consumer does today (`Genders`, `activeGenders`, `genderMaps`) stays exactly as it
is. That keeps the retired-gender divergence between checkout and registration untouched, which
matters, because that divergence was a live defect in this repository three commits ago.

Freshness comes from invalidating the master scope at service start (Principle VII's third case,
added in constitution v6.1.0), with the standard TTL as the backstop behind it.

## Technical Context

**Language/Version**: Go 1.x (backend), TypeScript/Next.js (frontend, untouched by this feature)

**Primary Dependencies**: `github.com/redis/go-redis/v9`, `labstack/echo/v4`, `pgx/v5`, existing
first-party `backend/pkg/cache`

**Storage**: PostgreSQL (source of truth, unchanged); Redis (read accelerator, one new surface)

**Testing**: Go unit + database-backed (`backend/scripts/test.sh`), Vitest (frontend, not
exercised here), Playwright (`e2e/`, the acceptance gate)

**Target Platform**: Linux server, Docker Compose

**Project Type**: Web service — modular monolith backend with a separate Next.js frontend

**Performance Goals**: eliminate the per-request database read of the gender master list; a warm
accelerator serves both projections with zero database round trips

**Constraints**: no cache call inside an order-writing transaction; fail open; byte-identical
responses in both cache modes; no change to what either projection contains

**Scale/Scope**: one table, two rows, two cache keys, one new scope, one new family. Backend only —
no migration, no schema change, no API contract change, no frontend change.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

Evaluated against constitution **v6.1.0**, which was amended for this feature.

| Principle | Verdict | Note |
|---|---|---|
| I. Modular Monolith | PASS | No new deployable, no service split. |
| II. Domain Isolation | PASS | `internal/order` reaches `pkg/cache` (shared infrastructure), not another domain. `cmd/api/architecture_test.go` enforces this and must stay green. |
| III. DTO Isolation | PASS | The cached value is `order.GenderRecord`, a first-party repository type. No `ordersql` generated struct is serialized (FR-016). |
| IV. Transactional Integrity | PASS | Every cached read is outside a transaction. `cache.Through` refuses on a transaction context, so a future inlining mistake fails loudly rather than silently serializing buyers. |
| V. Payment Gateway Abstraction | N/A | No gateway involvement. |
| VI. Guest-First MVP Scope | PASS | No authentication added; the public options endpoint stays unauthenticated. |
| VII. Cache as Disposable Read Accelerator | PASS | Requires v6.1.0, which admits `genders` by name and adds service start as the third invalidation trigger. Every other rule already satisfied by the reused infrastructure — see the per-rule table in [research.md](./research.md) §R7. |
| VIII. E2E Acceptance Coverage | PASS | See the rows below. |
| IX. Request Throttling | N/A | No throttle change. |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes — three of them, because the
      gender master list is read on all three. `e2e/specs/guest-purchase.spec.ts` (checkout's
      all-known projection, including the retired-gender scenarios),
      `e2e/specs/free-registration.spec.ts` (the active-only projection on prerequisites and
      submission), and `e2e/specs/cache-refresh.spec.ts` (the operator flush, which must now also
      clear this surface).
- [x] **New user-visible flow?** None. This feature is invisible by construction: SC-002 requires
      identical option lists in both cache modes, and any observable difference is a defect. The
      new scenarios therefore assert *sameness* plus the operator-visible behaviour (flush clears
      it; a retired gender still behaves as before), not new user capability.
- [x] **Bugfix in a covered flow?** No. This is an accelerator, not a fix. (The gender defects
      repaired earlier on this branch — `RegisterFree` failing to compile, and the frontend losing
      its FR-031 option widening — are separate work under spec 022 and are already covered.)
- [x] **Does anything change behaviour under `E2E_CACHE_ENABLED=false`?** No, and that is the
      point. With the cache off, `cache.Through` calls the loader directly and the reads are
      exactly today's. The suite must pass in both modes (SC-007), and the cache-off run is what
      proves Principle VII's kill switch still works with the new surface present.

**Gate result: PASS.** No violations, so Complexity Tracking is omitted.

### Post-design re-check (after Phase 1)

Re-evaluated against the completed design. **Still PASS**, with three notes the design surfaced
that the pre-design check could not have:

- **Principle III held under pressure.** The cached value is `order.GenderRecord`, a first-party
  repository type. Caching one projection derived from the other was considered and rejected in
  research R1 partly because `GenderRecord.IsActive` carries a documented per-query caveat — the
  design that would have been most convenient was also the one that leaked query semantics into
  the cached value.
- **Principle II gained a concrete guard.** The design puts `cache.Through` calls inside
  `internal/order`, which reaches `pkg/cache` only. `cmd/api/architecture_test.go` already enforces
  that genders' three consumers cannot pull in another domain, and it stays green.
- **Principle VII's narrowest rule is the one design most nearly broke.** FR-009 requires the
  startup invalidation to be scoped. Reusing an existing scope — the obvious shortcut — would have
  made service start discard the admin order lists or the whole event catalogue. `ScopeKindMaster`
  (research R3) exists specifically to keep that invalidation narrow, and the test asserting the
  other generations are untouched is what proves it.

## Project Structure

### Documentation (this feature)

```text
specs/023-master-data-cache/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── cache-surface.md # Phase 1 output — the internal surface contract
├── checklists/
│   └── requirements.md  # From /speckit-specify, re-validated by /speckit-clarify
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
backend/
├── pkg/cache/
│   ├── cache.go              # ADD ScopeKindMaster + Master() constructor
│   ├── surfaces.go           # ADD FamilyGendersMaster, registry entry, two key constructors
│   └── surfaces_test.go      # ADD key-shape and collision tests
├── internal/order/
│   ├── service.go            # Genders() and genderMaps() read through the cache
│   └── registration_service.go # activeGenders() reads through the cache
└── cmd/api/
    └── main.go               # ADD master-scope invalidation at startup, after the Ping

e2e/
├── specs/
│   ├── cache-refresh.spec.ts     # EXTEND — the flush clears the gender surface too
│   ├── guest-purchase.spec.ts    # EXTEND — checkout unchanged with the cache warm
│   └── free-registration.spec.ts # EXTEND — options and validation unchanged with the cache warm
└── support/
    └── db.ts                     # possibly a master-data helper; see quickstart.md
```

**Structure Decision**: the existing backend layout is used unchanged. The feature adds no package,
no file of its own beyond tests, and no frontend change. Three files carry the whole
implementation: `pkg/cache` gains the surface, `internal/order` reads through it, and `cmd/api`
triggers the startup invalidation. That concentration is intentional — the surface registry is
closed by design (`surfaces.go:12`), so the only correct way to add a surface is to edit the
registry rather than to build a parallel path around it.

## Complexity Tracking

> Not applicable — the Constitution Check gate passed with no violations.

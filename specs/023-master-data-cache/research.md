# Phase 0 Research: Master Data Read Cache

All Technical Context unknowns are resolved. There are no `NEEDS CLARIFICATION` markers: the three
decisions that would have produced them — store, expiry, and freshness trigger — were settled in
the two clarification sessions recorded in [spec.md](./spec.md).

---

## R1. Where does the cache go: the consumer or the query?

**Decision**: at the **repository query boundary**, caching `[]order.GenderRecord` for each of the
two queries. The three consumers keep shaping their own results exactly as they do today.

> *Updated 2026-08-24:* the consumer shapes below became id-keyed when the wire moved to
> identifiers (spec 011 FR-034). The decision recorded here is unaffected — still two queries
> feeding three consumers, still shaped above the cache — and the change touched none of the
> caching code, which is the clearest evidence the boundary was drawn in the right place.

**Rationale**: there are three gender consumers in Go but only two distinct database reads.

| Consumer | Query | Shape produced | Serves |
|---|---|---|---|
| `Service.Genders` (`service.go:372`) | `ListActiveGenders` | `[]GenderOption` | `GET /ticket/genders`, registration prerequisites |
| `Service.activeGenders` (`registration_service.go:108`) | `ListActiveGenders` | a set of active ids | registration validation |
| `Service.genderMaps` (`service.go:396`) | `ListGenders` | `(known ids, active ids)` | checkout |

Caching per consumer would produce three entries from two reads, and the first two would hold the
same rows in two different shapes — an invitation for them to drift. Caching per query produces
two entries that all three consumers share, and the shaping stays where it already is.

This matters more than it looks. The difference between `activeGenders` and `genderMaps` is the
retired-gender rule: registration refuses a retired value, checkout accepts one on a slot that
already held it. That divergence is deliberate (spec 022 FR-022a) and was a live compile-time
defect in this repository as recently as this branch. Caching *underneath* the shaping leaves it
untouched by construction; caching *at* the shaping would put it inside the blast radius of this
feature for no gain.

**Alternatives considered**:
- *Cache the three shaped results.* Rejected: more entries than reads, duplicated rows across two
  of them, and it moves the retired-gender logic into the changed surface.
- *Cache one list and derive both projections from it.* Rejected: `ListActiveGenders` selects only
  active rows and `GenderRecord.IsActive` is documented as the zero value on that path
  (`repository.go:528`). Deriving the active list from the all-known one would work, but it
  silently changes which query each path issues and makes a cache-off run take a different code
  path from a cache-on run — the exact thing FR-019 and SC-002 forbid.

---

## R2. One family or two?

**Decision**: **one family**, `genders_master`, with two fingerprints — `proj=active` and
`proj=all`.

**Rationale**: `Family` is the Prometheus label (`surfaces.go:16-17`), and the codebase already
has a rule for this exact question. `Paging`'s comment (`surfaces.go:54-66`) records that a
filtered variant of a list is *not* a new surface, and that placing the variant in the fingerprint
rather than the family is what keeps the label set bounded and the surface list closed. The
active-only list is a filtered variant of the all-known list by definition.

It also matches the amended constitution, which admits "the `genders` master list" as **one**
surface whose "two projections are covered" — not two surfaces.

**Alternatives considered**:
- *Two families (`genders_active`, `genders_all`).* Rejected. Tempting for per-projection hit-rate
  metrics, but it contradicts both the `Paging` precedent and the amendment's wording, and it
  spends two entries on the closed registry where one suffices. Per-projection visibility, if it
  is ever wanted, is a fingerprint dimension on a dashboard query, not a second label value.

---

## R3. Which scope invalidates it?

**Decision**: a **new** `ScopeKindMaster` (`"master"`), generation key `gen:master`, constructor
`cache.Master()`.

**Rationale**: the three existing scopes (`events`, `event:{id}`, `orders`) each name rows a write
path touches. Gender rows are touched by none of them, so reusing any would couple the master
list's lifetime to an unrelated write — an order placement would evict the gender list, and worse,
the startup invalidation would then discard the admin order lists, violating FR-009's requirement
that the startup refresh be scoped to master data alone.

`Scope.String()` is a metric label; `"master"` is a bounded constant, so nothing unbounded reaches
Prometheus. `GenerationKey()` needs no change — the non-event branch already renders
`"gen:" + kind`.

**Alternatives considered**:
- *Reuse `Events()`.* Rejected: an event write would evict genders, and a startup master refresh
  would evict the whole catalogue.
- *No scope; rely on TTL and flush.* Rejected: forbidden by Principle VII, and it is the design the
  2026-08-24 clarification explicitly moved away from.

---

## R4. Where does the startup invalidation go?

**Decision**: `cmd/api/main.go`, immediately after the existing startup `Ping` succeeds
(`main.go:196-202`), invalidating **only** `cache.Master()`.

**Rationale**: the composition root already establishes a `startupCtx`, constructs the Redis
client, and probes it with a 2-second timeout — the invalidation has a natural home inside that
block, on the branch that already knows the store answered. Placing it in a domain would force
`internal/order` to own a process-lifecycle concern, which is the same reasoning that put the
operator flush handler in `cmd/api/ops.go` rather than in a domain handler (`ops.go:64-68`).

**Failure behaviour, and a useful finding**: if the invalidation fails, `Redis.distrust`
(`redis.go:154`) opens a **time-bounded, self-clearing** distrust window in which every read
bypasses the cache and goes to the database. That is a better interim behaviour than anything this
feature would have written by hand: during the window reads are guaranteed fresh, and once it
self-clears the TTL backstop bounds whatever stale entry survived. FR-008a is therefore satisfied
by existing machinery plus a log line, not by new error handling.

**Alternatives considered**:
- *Invalidate lazily on the first gender read after boot.* Rejected: needs process-local state to
  remember whether it has run, and that state is exactly what a "disposable accelerator" should
  not accumulate.
- *`FlushAll` at startup.* Rejected outright — it discards every surface, directly violating FR-009.

---

## R5. How does the domain reach the cache?

**Decision**: `cache.Through[[]GenderRecord]` inside `internal/order`, using the `cache.Lists`
already injected on `Service` (`service.go:49`, installed by `WithCache` at `service.go:103`).

**Rationale**: no new wiring. The order domain already holds the interface and already calls
`cache.InvalidateAfterCommit` on two paths (`service.go:282`, `service.go:569`). `Through` owns
miss handling, JSON round-tripping, miss collapsing via `singleflight`, metrics, the fail-open
rule, and the transaction refusal — which is precisely the list of things FR-013, FR-015, FR-015a,
FR-015b, FR-017 and FR-019 require, so reusing it satisfies six requirements at once and lets none
of them be re-implemented subtly differently.

**Note on the default**: `Service` is constructed with `cache.NoOp{}` (`service.go:75`), so a
`Service` built without `WithCache` reads the database. Tests that do not install a cache are
unaffected, which is what keeps the existing gender tests meaningful.

---

## R6. Does the backfill already behave as FR-015a and FR-015b require?

**Decision**: yes — `Through` (`through.go:29-82`) already implements it; no change needed.

**Rationale**, mapped requirement by requirement:

- **FR-015a (populate on miss)**: on a miss, `Through` calls the loader inside `loaders.Do`, then
  marshals and calls `c.Set`. The value is stored, so the next read hits.
- **FR-015b (best-effort store-back)**: the `Set` result is explicitly discarded
  (`_ = c.Set(...)`), and a marshal failure only increments `failed("encode")`. The caller's value
  is returned either way.
- **FR-015 (collapse concurrent misses)**: `singleflight` keyed on the fully resolved entry key,
  which includes the generation — so a request arriving after an invalidation joins a new flight
  rather than the orphaned one's result.
- **FR-015c (a miss must not invalidate)**: `Through` contains no `Invalidate` call on any path.
  The requirement is satisfied by absence, and the test that matters is one asserting the
  generation counter is unchanged across a miss.

The decode-failure path is also already correct for the edge case the spec records: an undecodable
stored value increments `failed("decode")` and falls through to the loader, which re-`Set`s in the
current shape — replacing the bad entry rather than re-reading past it forever.

---

## R7. Principle VII rule-by-rule compliance

Recorded explicitly, because Principle VII is NON-NEGOTIABLE and "we reused the infrastructure" is
an assertion, not evidence.

| Rule | How this feature satisfies it |
|---|---|
| PostgreSQL is the single source of truth | Genders are read from Postgres and stored as a copy. `FLUSHALL` costs one rebuild. |
| Surfaces closed and enumerated | v6.1.0 admits `genders` by name. The registry entry is added to `Families`, the only way to build a `Key`. |
| Invalidation by event, not time | Service start (v6.1.0's third case). TTL stays a backstop. |
| Never authoritative for inventory | Genders are not inventory. Quota logic is untouched. |
| No cache call inside an order-writing transaction | All three consumers read outside transactions; `Through` refuses on a transaction context regardless. |
| Fail open | `Through` returns `load(ctx)` on every failure path; `/healthz` already reports degraded. |
| Swappable behind an interface | `cache.Lists`, already injected. No Redis client in the domain. |
| Killable by configuration | `CACHE_ENABLED=false` substitutes `NoOp`; the reads become today's reads. |

---

## R8. What the acceptance tier has to prove

**Decision**: extend three existing specs rather than add a new one.

**Rationale**: the feature adds no user-visible flow, so a new spec file would have no new journey
to describe. What needs proving is *sameness under a new condition*, which belongs beside the
scenarios that already establish the behaviour:

- `free-registration.spec.ts` — options and validation identical with the accelerator warm,
  including that a retired gender is still refused.
- `guest-purchase.spec.ts` — checkout still accepts a retired gender on a slot that held it.
  This is the scenario most worth having: it is the one the two projections could plausibly be
  collapsed into breaking.
- `cache-refresh.spec.ts` — the operator flush clears the gender surface too (FR-010).

**Note on a limitation, stated rather than glossed**: retiring a gender is not reachable through
any API — there is no admin CRUD for the master lists (migration 000013) — and `e2e/support/db.ts`
deliberately excludes `genders` from its reset, treating it as migration-seeded master data
(`db.ts:50-51`). Any e2e scenario that needs a retired gender must therefore write master data
directly, which the suite has so far avoided. AGENTS.md's prohibition names order status, tickets
and payment state specifically, so this is not forbidden — but it is a first for this suite and
should be a deliberate, documented helper rather than an inline `UPDATE`. The alternative is to
prove the retired-gender rules at the Go tier only, where spec 022 already proves them, and have
e2e assert only cache-mode sameness. `/speckit-tasks` should make this an explicit task decision.

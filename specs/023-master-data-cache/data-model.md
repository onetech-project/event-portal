# Phase 1 Data Model: Master Data Read Cache

**No database change.** No table, column, index, constraint or migration is added or altered.
`SCHEMA.md` was verified unaffected during the constitution v6.1.0 amendment rather than assumed.

What follows is the *cache-side* model: the entities this feature introduces into `pkg/cache`, and
the existing entities it reads.

---

## 1. Existing entities (read, never modified)

### `genders` (PostgreSQL — source of truth)

Defined at `SCHEMA.md:106`, re-keyed by migration 000013. Reproduced here only for the fields this
feature reads.

| Column | Type | Note |
|---|---|---|
| `id` | `smallint`, identity PK | Narrowed from uuid by migration 000013 |
| `name` | `varchar(100)`, NOT NULL, UNIQUE | The canonical value; **the wire carries the name, never the id** |
| `is_active` | `boolean`, NOT NULL, default true | Retired entries stay resolvable but stop being offered |

Two rows are seeded by migration 000008: `FEMALE`, `MALE`. There is **no admin CRUD** for this
table (migration 000013 states this explicitly), which is the whole reason the freshness trigger
is service start rather than a commit.

### `order.GenderRecord` (Go — the cached value)

`backend/internal/order/repository.go:526`. A first-party repository type, **not** an `ordersql`
generated struct, which is what satisfies FR-016 and Principle III.

```go
type GenderRecord struct {
    ID       int16
    Name     string
    IsActive bool // zero value on rows read through ListActiveGenders
}
```

That `IsActive` caveat is load-bearing and is documented in the repository already: the field is
meaningful only on the all-known projection. It is one reason R1 rejected deriving one projection
from the other.

---

## 2. New cache entities

### 2.1 Scope — `ScopeKindMaster`

```go
const ScopeKindMaster ScopeKind = "master"

func Master() Scope { return Scope{Kind: ScopeKindMaster} }
```

| Property | Value |
|---|---|
| Generation key | `gen:master` (existing non-event branch of `Scope.GenerationKey()`, unchanged) |
| Metric label | `"master"` — a bounded constant, so nothing unbounded reaches Prometheus |
| Carries an ID | No. Like `Events()` and `Orders()`, not like `Event(id)` |
| Invalidated by | Service start (FR-008); the operator flush clears it with everything else (FR-010) |

**Relationship to other scopes**: none. It is deliberately disjoint — no write path touches both
`master` and any other scope, which is what lets the startup invalidation be narrow (FR-009).

### 2.2 Family — `FamilyGendersMaster`

```go
FamilyGendersMaster Family = "genders_master"
```

Added to the `Families` registry. One family, two fingerprints (R2).

### 2.3 Keys — two, one per projection

```go
func GendersActiveKey() Key {
    return Key{Family: FamilyGendersMaster, Scope: Master(), Fingerprint: "proj=active"}
}

func GendersAllKey() Key {
    return Key{Family: FamilyGendersMaster, Scope: Master(), Fingerprint: "proj=all"}
}
```

Rendered entry prefixes (via the existing `Key.EntryPrefix()`, no change required):

```text
list:genders_master:proj=active:g
list:genders_master:proj=all:g
```

Both derive from `gen:master`, so one `INCR` orphans both — which is correct, since both come from
the same table and a migration changing it changes both.

---

## 3. The two projections

The distinction below is the single most important invariant in this feature. Collapsing these two
lists would either refuse a gender checkout must accept or offer one a form must not — and the
constitution now pins it (v6.1.0, cacheable-surfaces bullet).

| | **Active** (`proj=active`) | **All-known** (`proj=all`) |
|---|---|---|
| Query | `ListActiveGenders` — `WHERE is_active` | `ListGenders` — every row |
| Cached value | `[]GenderRecord` (`IsActive` unset) | `[]GenderRecord` (`IsActive` meaningful) |
| Consumers | `Service.Genders`, `Service.activeGenders` | `Service.genderMaps` |
| Serves | Form options; registration validation and resolution | Checkout resolution |
| Retired gender | **Absent** — registration refuses it (spec 022 FR-022a) | **Present** — checkout accepts it on a slot that already held it (spec 011 FR-031) |

Ordering is `ORDER BY name` in both queries and is preserved verbatim through the cache: `Through`
does not transform the value, so the cached and uncached responses are the same bytes (FR-019).

---

## 4. Validation rules

Carried over unchanged — this feature introduces none of its own and must not alter any.

- A submitted gender is matched **by name**, never by id (`attendees.gender_id` comment,
  migration 000013).
- Registration validates against the active projection only; an unfound name is an explicit
  field-level refusal, never a zero-valued resolution (spec 022 FR-022b).
- Checkout resolves against the all-known projection, then applies its own separate active-check
  per slot (`service.go:460`).

---

## 5. State transitions

The cached entries have exactly three transitions, and no others are permitted:

```text
                 ┌──────────────────────────────────────────┐
                 │                                          │
   (empty) ──────▼──── read miss ──► load from DB ──► store ─┴──► (warm)
                                                                   │
      (warm) ──── service start / operator flush ──► orphaned ──────┘
                                                                   │
      (warm) ──── TTL lapses ──► expired ─────────────────────────┘
```

- **Populate** — on a miss. Never on a write, because there is no write path.
- **Orphan** — service start (FR-008) or operator flush (FR-010). Both are explicit events.
- **Expire** — the backstop only (FR-007). Never the mechanism of correctness.

A miss **must not** invalidate (FR-015c). The two are opposite operations, and conflating them here
would be unusually damaging: both projections share `gen:master`, so invalidating on a miss of one
would orphan the other, leaving the two projections evicting each other on every miss and a hit
rate near zero — a cache that looks correct and accelerates nothing.

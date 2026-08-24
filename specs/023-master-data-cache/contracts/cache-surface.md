# Contract: Gender Master Cache Surface

**No HTTP contract changes.** `GET /api/v1/ticket/genders` keeps its request shape, response
shape, status codes and error codes exactly. So does every other affected path — registration
prerequisites, registration submission, checkout. FR-019 requires the response to be byte-identical
whether it came from the accelerator or the database, so an OpenAPI diff for this feature would be
empty, and `api/openapi.yml` is not touched.

The contract this feature actually adds is **internal**: a new surface in the `pkg/cache` registry.
It is a contract in the sense that matters here — the registry is closed, other domains depend on
its invariants, and getting the key shape wrong silently serves one list's data for another's.

---

## 1. Registry additions

```go
// pkg/cache/cache.go
const ScopeKindMaster ScopeKind = "master"
func Master() Scope

// pkg/cache/surfaces.go
const FamilyGendersMaster Family = "genders_master"   // added to Families
func GendersActiveKey() Key
func GendersAllKey() Key
```

## 2. Key shapes

| Projection | Family | Scope | Fingerprint | Entry prefix |
|---|---|---|---|---|
| Active-only | `genders_master` | `master` | `proj=active` | `list:genders_master:proj=active:g` |
| All-known | `genders_master` | `master` | `proj=all` | `list:genders_master:proj=all:g` |

Generation counter for both: `gen:master`.

**Invariants** (each is a test):

1. The two keys are distinct. A collision would serve the active list to checkout, silently
   refusing every retired gender a restored booking form carries.
2. Both resolve to `gen:master`, so one `INCR` orphans both.
3. Neither fingerprint contains `:` beyond its own separator, and neither embeds a caller-supplied
   value — both are compile-time constants, so the `optSearch` hashing concern (`surfaces.go`) does
   not arise here.
4. `Scope.String()` returns `"master"` — a bounded label value.

## 3. Value contract

| | |
|---|---|
| Go type | `[]order.GenderRecord` |
| Encoding | JSON, via `cache.Through`'s own marshalling — the domain does not serialize |
| Ordering | `ORDER BY name`, preserved verbatim; `Through` does not transform the value |
| Forbidden | Any `ordersql` generated struct (Principle III, FR-016) |

## 4. Behavioural contract

Provided entirely by `cache.Through` — this feature adds no new behaviour, and the tests assert
that it did not accidentally acquire any.

| Condition | Required behaviour | Req |
|---|---|---|
| Hit | Serve the stored value; no database read | FR-001 |
| Miss | Read the database, return it, **and store it back** | FR-015a |
| Store-back fails | Caller still served; failure counted; next reader repeats the read | FR-015b |
| Concurrent misses, same key | One database read, not one per waiter | FR-015 |
| Any miss | **No invalidation** — `gen:master` must be unchanged across it | FR-015c |
| Stored value undecodable | Discard, read database, re-store in the current shape | Edge case |
| Store unreachable | Fall back to the database and succeed; never an error to the caller | FR-013 |
| Called inside a transaction | Refuse with `ErrInTransaction` | Principle VII |
| `CACHE_ENABLED=false` | `NoOp` substituted; reads are exactly today's | FR-018 |

## 5. Lifecycle contract

| Event | Required behaviour | Req |
|---|---|---|
| Service start, store reachable | Invalidate `Master()` **and nothing else** | FR-008, FR-009 |
| Service start, store unreachable | Start and serve anyway; record the degraded start | FR-008a |
| Operator flush (`ops.go`) | Clears this surface with all others | FR-010 |
| TTL lapses | Entry rebuilt on next read — backstop only | FR-006, FR-007 |
| A master write path is ever built | Must invalidate `Master()` on commit | FR-011 |

## 6. Consumer contract (unchanged, and that is the point)

| Consumer | Reads | Retired gender |
|---|---|---|
| `Service.Genders` | active | not offered |
| `Service.activeGenders` | active | **refused** (spec 022 FR-022a) |
| `Service.genderMaps` | all-known | **accepted** on a slot that held it (spec 011 FR-031) |

Any change to this table is a defect in this feature, not a feature of it.

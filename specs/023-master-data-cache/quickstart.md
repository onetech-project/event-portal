# Quickstart: Validating the Master Data Read Cache

How to prove this feature works, and — because it is invisible by design — how to prove it did
**not** change anything a guest sees.

The trap to avoid: this feature's happy path looks identical to a completely broken
implementation. A cache that never populates, and a cache that populates perfectly, both return
the right gender list. Every scenario below is written so that passing requires the acceleration to
be real, not just the output to be correct.

---

## Prerequisites

The runner owns none of these — start them first.

```bash
REDIS_PORT=6380 docker compose up -d postgres redis mailpit
docker compose run --rm migrate up
```

Mailpit is not optional: the free-registration specs read real messages off it (spec 016).

---

## 1. Backend tier

```bash
cd backend && ./scripts/test.sh ./...
```

Database-backed tests skip themselves when `TEST_DATABASE_URL` is unset, so this works with or
without the stack — but the scenarios that matter here need it, so bring it up.

**What must be true:**

- `cmd/api/architecture_test.go` stays green. `internal/order` reaching `pkg/cache` is legal
  (shared infrastructure); reaching another domain is not.
- The existing gender tests still pass **unchanged**. `Service` is constructed with `cache.NoOp{}`
  (`service.go:75`), so any test that does not call `WithCache` exercises the database path exactly
  as before. If one of those needed editing, the feature changed behaviour it should not have.

**New scenarios worth writing (details belong in `tasks.md`):**

| Scenario | Proves |
|---|---|
| Read twice with a fake cache installed; assert the loader ran **once** | FR-015a — the miss populated, rather than reading past the store |
| Read once with a store whose `Set` always fails; assert the caller is served and the loader runs again next time | FR-015b |
| Read a miss; assert `gen:master` is **unchanged** | FR-015c — a miss must not invalidate |
| Fire N concurrent first reads; assert one loader call | FR-015 |
| Invalidate `Master()`; assert the event/orders generations are untouched | FR-009 |
| Compare active and all-known results against the direct repository reads | FR-019, and the projection split |

That last one is the guard against the failure mode this feature is most likely to produce:
serving one projection where the other belongs.

---

## 2. Manual walkthrough

Useful for seeing the mechanism, and quicker than reasoning about it.

```bash
# Terminal 1
cd backend && go run ./cmd/api

# Terminal 2 — what the boot invalidation did
redis-cli -p 6380 GET gen:master

# Read both projections through the app
curl -s localhost:8080/api/v1/ticket/genders | jq

# The entries now exist, one per projection
redis-cli -p 6380 --scan --pattern 'list:genders_master:*'
```

Expected: `gen:master` is set by startup; after a read, `list:genders_master:proj=active:g<N>`
exists. Checkout on a booked order populates `proj=all`.

**The one that proves freshness (User Story 2):**

```bash
# Retire a gender the way only a migration can
docker compose exec postgres psql -U ticketing -d ticketing \
  -c "UPDATE genders SET is_active = false WHERE name = 'MALE';"

# Before restart: the warm entry still offers it — expected, not a bug
curl -s localhost:8080/api/v1/ticket/genders | jq

# Restart, which is what a deployment does
# → gen:master increments, both entries orphaned
redis-cli -p 6380 GET gen:master
curl -s localhost:8080/api/v1/ticket/genders | jq   # MALE is gone

# Put it back
docker compose exec postgres psql -U ticketing -d ticketing \
  -c "UPDATE genders SET is_active = true WHERE name = 'MALE';"
```

The pre-restart read still showing `MALE` is the design working, not failing: FR-006 makes expiry a
backstop, and FR-008 makes the deployment the event. A migration never lands without one.

**Store down at boot (FR-008a):**

```bash
docker compose stop redis
cd backend && go run ./cmd/api      # must start and serve
curl -s localhost:8080/api/v1/ticket/genders | jq   # correct, from the database
curl -s localhost:8080/healthz | jq                 # degraded, not failing, 200
docker compose start redis
```

The service must start, the log must record the degraded start, and no guest-facing request may
fail.

---

## 3. Acceptance tier — the gate

```bash
cd e2e && npm test              # headless
cd e2e && npm run test:slow     # headed, slowed for a human
```

If `e2e/node_modules` is absent, install first — the suite also needs its browsers.

**Both cache modes are mandatory** (Principle VII's kill switch, SC-007):

```bash
cd e2e && E2E_CACHE_ENABLED=false npm test
```

The cache-off run is what proves the substitution is real. If it fails, `NoOp` is not returning the
system to the pre-cache path.

Specs that must change with this feature:

| Spec | Must show |
|---|---|
| `free-registration.spec.ts` | Options and validation identical with the accelerator warm; a retired gender still refused |
| `guest-purchase.spec.ts` | Checkout still accepts a retired gender on a slot that already held it |
| `cache-refresh.spec.ts` | The operator flush clears this surface too (FR-010) |

**Known friction, decide it in `tasks.md`:** any e2e scenario needing a *retired* gender must write
master data directly. There is no admin API for it (migration 000013), and `e2e/support/db.ts`
deliberately excludes `genders` from its reset as migration-seeded data (`db.ts:50-51`). AGENTS.md
forbids writing order status, tickets and payment state from a test — genders are not on that list,
so this is permitted but unprecedented in this suite. Either add a documented helper, or leave the
retired-gender rules proven at the Go tier (where spec 022 already proves them) and have e2e assert
only cache-mode sameness. Do not let it become an undocumented inline `UPDATE`.

---

## 4. Definition of done

- [ ] `backend/scripts/test.sh ./...` green, including `architecture_test.go`
- [ ] Existing gender tests pass **without modification**
- [ ] `cd e2e && npm test` green
- [ ] `cd e2e && E2E_CACHE_ENABLED=false npm test` green
- [ ] A warm accelerator serves both projections with zero database reads (SC-001)
- [ ] A miss populates — provable, not assumed (SC-010)
- [ ] Retired gender: still refused at registration, still accepted at checkout (SC-005)
- [ ] Service starts and serves with Redis down (SC-004, SC-009)

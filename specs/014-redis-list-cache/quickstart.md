# Quickstart: Refresh-on-Write List Caching

**Feature**: 014-redis-list-cache | **Date**: 2026-08-10

How to run the feature locally and prove it works. Design details live in
[plan.md](./plan.md); key and invalidation rules live in [contracts/](./contracts/).

---

## Prerequisites

- Docker + Docker Compose
- Go 1.26.5 (for the test tiers)
- `redis-cli` (optional, but the inspection steps below use it)

## Setup

```bash
cp .env.example .env    # then confirm the three new vars below are present
docker compose up -d postgres redis migrate
docker compose up -d api
```

New environment variables:

| Variable | Default | Purpose |
|---|---|---|
| `REDIS_URL` | `redis://localhost:6379/0` | Inside compose the `api` service uses `redis://redis:6379/0`; empty disables the cache entirely |
| `CACHE_ENABLED` | `true` | `false` restores pre-cache behavior (FR-021) |
| `CACHE_TTL` | `10m` | Backstop expiry only |

Confirm both dependencies are up:

```bash
curl -s localhost:8080/healthz
# {"status":"ok"}
```

---

## Scenario 1 — The cache is actually being used (US1, SC-001, SC-003)

```bash
# Cold
time curl -s localhost:8080/api/v1/event > /tmp/cold.json
# Warm
time curl -s localhost:8080/api/v1/event > /tmp/warm.json

diff /tmp/cold.json /tmp/warm.json && echo "IDENTICAL (FR-005)"
```

**Expected**: identical bodies; the warm read at least 5× faster and under 30 ms (SC-001).

Confirm the entry exists and the counter is where you expect:

```bash
redis-cli -n 0 KEYS 'list:events_public:*'
redis-cli -n 0 GET gen:events        # nil or an integer — nil reads as generation 0
```

Confirm via metrics rather than timing alone:

```bash
curl -s localhost:8080/metrics | grep cache_requests_total
# cache_requests_total{family="events_public",result="miss"} 1
# cache_requests_total{family="events_public",result="hit"} 1
```

---

## Scenario 2 — A write refreshes it immediately (US1, SC-002)

```bash
TOKEN=$(curl -s -X POST localhost:8080/api/v1/admin/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"<from your seed>"}' | jq -r .data.token)

curl -s localhost:8080/api/v1/event | jq '.data | length'          # warm; note the count

curl -s -X POST localhost:8080/api/v1/admin/events \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Cache Probe","status":"PUBLISHED", ...}'

curl -s localhost:8080/api/v1/event | jq '.data | length'          # count+1, immediately
redis-cli -n 0 GET gen:events                                      # incremented by 1
```

**Expected**: the new event appears on the very next request — no wait, no manual action
(FR-006). The generation counter advanced by exactly one.

**Fail signal**: the count is unchanged, or changes only after `CACHE_TTL` elapses. That
means invalidation is not wired for this write path — check
[contracts/invalidation-map.md](./contracts/invalidation-map.md) §1.

---

## Scenario 3 — Quota moves refresh the ticket list (US2, SC-007)

The correctness-critical one.

```bash
SLUG=<a published event slug>
curl -s localhost:8080/api/v1/ticket/$SLUG | jq '.data[] | {name, quota}'   # warm

# Book through the real guest flow (event_id + one item line)
curl -s -X POST localhost:8080/api/v1/ticket/book \
  -H 'Content-Type: application/json' \
  -d '{"event_id":"<event uuid>","items":[{"ticket_type_id":"<uuid>","quantity":2}]}'

curl -s localhost:8080/api/v1/ticket/$SLUG | jq '.data[] | {name, quota}'   # decremented
```

Then let the order expire (or drive a `cancel` webhook) and read again — the quota must be
restored on the next read.

**Expected**: remaining quota tracks the database exactly at every step. Per FR-012 the
cached number is display-only: a booking attempt against a stale figure is still decided
by the row-locked `UPDATE`, so the outcome is correct even if the display were not.

---

## Scenario 4 — Redis down, nothing breaks (US1 scenario 4, FR-013, SC-005)

```bash
docker compose stop redis

curl -s localhost:8080/api/v1/event | jq '.data | length'   # still correct
curl -s localhost:8080/healthz
# {"status":"degraded","cache":"unavailable"}   ← 200, not 503 (FR-018)

# writes still commit
curl -s -X POST localhost:8080/api/v1/admin/events -H "Authorization: Bearer $TOKEN" ...

docker compose start redis
sleep 5
curl -s localhost:8080/healthz          # back to {"status":"ok"} within 60s (SC-005)
```

**Expected**: zero failed requests throughout. Any 5xx on a list endpoint while Redis is
down is a fail-open violation.

Also verify a cold start with no Redis at all — `docker compose up api` without the redis
service must start and serve (Principle VII).

---

## Scenario 5 — Cold-burst collapsing (FR-017, SC-006)

```bash
redis-cli -n 0 FLUSHDB
curl -s localhost:8080/metrics | grep pg_queries   # or watch pg_stat_statements
hey -n 500 -c 50 http://localhost:8080/api/v1/event
```

**Expected**: ≤ 5 Postgres queries for that list across all 500 requests. Anything near 500
means singleflight is not wrapping the miss path.

---

## Scenario 6 — Operator flush (US4)

```bash
curl -s -X POST localhost:8080/api/v1/admin/cache/refresh -H "Authorization: Bearer $TOKEN"
# {"data":{"status":"flushed","entries_cleared":12}}

redis-cli -n 0 DBSIZE     # 0
curl -s localhost:8080/api/v1/event   # correct content, rebuilt from Postgres
```

Then the manual-correction case: `UPDATE` an event title directly in psql, flush, and
confirm the new title appears. See [contracts/admin-cache-refresh.md](./contracts/admin-cache-refresh.md).

---

## Scenario 7 — The kill switch reproduces today exactly (FR-021, SC-008)

```bash
CACHE_ENABLED=false go test ./...
CACHE_ENABLED=true  go test ./...
```

**Expected**: both green, with no test skipped or conditionally branched on the flag other
than the cache package's own. This is the strongest evidence that the cache changed no
behavior — and it is what makes the feature safe to disable in production without thought.

---

## Test tiers

| Tier | Command | Covers |
|---|---|---|
| Unit (miniredis) | `go test ./pkg/cache/...` | Key grammar, generation bumps, fail-open, distrust window, DTO round-trip fidelity (R6), `ErrInTransaction` refusal (R7) |
| Integration (Postgres + Redis) | `go test -tags=integration ./internal/...` | Every row of the invalidation map: warm → write → assert fresh **and** assert miss |
| Both modes | `CACHE_ENABLED=false go test ./...` | SC-008 |

The invalidation-map tests must assert the second read was a **miss**, not merely that its
content was correct — a content-only assertion passes even when the cache is bypassed
entirely, which is precisely the bug those tests exist to catch.

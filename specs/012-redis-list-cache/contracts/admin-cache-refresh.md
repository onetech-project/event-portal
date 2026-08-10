# Contract: `POST /api/v1/admin/cache/refresh`

**Feature**: 012-redis-list-cache | **Requirement**: FR-020

The operator escape hatch: discard every cached list and force the next read of each to be
rebuilt from PostgreSQL. Used after a manual database correction, on suspected
inconsistency, or during an incident.

---

## Request

```http
POST /api/v1/admin/cache/refresh
Authorization: Bearer <admin JWT>
```

No body. No parameters — partial/scoped flushing is deliberately not offered: an operator
reaching for this endpoint does not know which scope is wrong, and a full flush costs only
one Postgres query per list afterwards.

**Auth**: mounts on the `adminAPI` group in `cmd/api/main.go` (behind `admin.RequireAuth`),
alongside the other `/admin/*` routes.

**Implementation note.** The handler lives in `cmd/api/ops.go`, not in
`order.AdminHandler` as this contract originally specified. Two reasons, both
found while building it: the cache is shared infrastructure rather than any
domain's business — the same argument that puts `/healthz` and `/metrics` there —
and attributing the flush requires the authenticated admin's identity, which would
have forced `internal/order` to import `internal/admin`. Principle II exists to
prevent exactly that edge, and the architecture test in `cmd/api` enforces it.

---

## Responses

### 200 OK — flushed

```json
{
  "data": {
    "status": "flushed",
    "entries_cleared": 143
  }
}
```

`entries_cleared` is the `DBSIZE` observed immediately before the flush — informational
only, and approximate under concurrent traffic.

### 200 OK — cache not enabled

```json
{
  "data": {
    "status": "disabled",
    "entries_cleared": 0
  }
}
```

Returned when `CACHE_ENABLED=false` or `REDIS_URL` is empty. **Not** an error: nothing is
cached, so the caller's intent — "no stale list is being served" — is already satisfied.

### 503 Service Unavailable — cache unreachable

The project's standard flat error envelope, not a nested error object. `code` is
numeric: `apperr.Numeric` maps unregistered string codes to `status × 1000`, so
`CACHE_UNAVAILABLE` renders as `503000`.

```json
{
  "code": 503000,
  "message": "The cache is not reachable, so it could not be refreshed.",
  "data": null
}
```

This is the one place a cache failure legitimately surfaces as an HTTP error, and it does
not contradict FR-013's fail-open rule: FR-013 governs endpoints that *serve product data*.
Here the cache is the subject of the request, and reporting success when nothing was
flushed would mislead an operator mid-incident.

Note the practical case is benign: if Redis is unreachable, every read is already failing
open to Postgres, so no stale content is being served anyway.

### 401 Unauthorized

Standard `admin.RequireAuth` response — missing, malformed, or expired token.

---

## Behavior

1. `DBSIZE` for the count (best-effort; a failure here yields `0`, not an error).
2. `FLUSHDB` on the configured Redis database — entries **and** generation counters
   together. Flushing entries alone would leave counters at their current values, which is
   harmless; flushing counters alone would be a correctness bug, resurrecting orphaned
   entries under a reset generation. Doing both atomically avoids the question.
3. Clear the in-process distrust window: a successful flush means the cache is reachable
   and empty, so there is nothing left to distrust.
4. Log at INFO with the authenticated admin's identity — this discards state across every
   instance and should be attributable.

**Scope**: `FLUSHDB`, never `FLUSHALL`. The cache owns its own Redis database number
(`REDIS_URL` carries `/0`), and `FLUSHALL` would exceed this feature's blast radius if that
instance is ever shared.

**Idempotent**: calling it twice in a row is safe; the second call clears nothing and
returns `entries_cleared: 0`.

**Not rate limited**: it sits behind admin JWT auth, and an operator retrying during an
incident should not be throttled.

---

## Verification

- Warm several lists, flush, then assert the next read of each is a cache miss returning
  correct content (spec US4 scenario 2).
- Write directly to Postgres bypassing the application, flush, and assert the corrected
  data appears in every affected list (US4 scenario 3).
- Call with no token → 401. Call with `CACHE_ENABLED=false` → 200 `disabled`.

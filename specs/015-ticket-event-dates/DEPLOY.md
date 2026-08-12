# Deploy notes: Per-Ticket Event Dates (spec 015)

Two steps that no test in any tier will catch if you skip them.

## 1. Migration runs before the API boots

`docker-compose.yml` already gates this — the `api` service depends on `migrate`
completing successfully — so an ordinary compose deploy is safe. Confirm afterwards:

```bash
docker compose run --rm migrate version    # expect: 14
```

The migration backfills every existing ticket type's admission window from its parent
event, which is what makes this feature invisible to events that do not adopt it. Verify
the backfill was total:

```sql
SELECT count(*) FROM ticket_types WHERE event_start IS NULL OR event_end IS NULL;  -- 0
```

## 2. Flush the read cache — MANDATORY

**Run this after the new binary is serving.**

```bash
curl -X POST -H "Authorization: Bearer $ADMIN_JWT" \
  https://<host>/api/v1/admin/cache/refresh
```

### Why this is not optional

The public ticket list is cached as the domain DTO, JSON-encoded, under
`list:ticket_types_public:{event_uuid}:-:g{generation}`. The generation counter is bumped
by **writes**, not by deploys, and the key carries no schema version. So entries written
by the previous binary stay live and addressable by the new one.

`encoding/json` ignores fields that are absent from the payload. An old-shape entry
therefore decodes **cleanly** into the new struct with `event_start` and `event_end` left
at `0001-01-01T00:00:00Z` — it does not fail, so the cache's decode-error fallback to
PostgreSQL never fires. Guests would be shown a January year-1 date for up to `CACHE_TTL`
(default 10 minutes), and for the whole of a rolling deploy, since old and new pods share
the same keys.

Constitution Principle VII forbids leaning on the TTL for correctness — "TTL expiry MUST
NOT be the mechanism by which the system becomes correct" — so the flush is the
mechanism.

The call is safe to run unconditionally: with caching switched off it returns
`200 {"status":"disabled","entries_cleared":0}` rather than an error.

## 3. Tell the admins what "Event start" means

Validation admits **no** tolerance either side of a ticket's window. An admin who reads
"Event start" as showtime rather than as the moment the gate opens will have every early
arrival refused at the door. The form's hint text says this, but it is worth saying once
out loud to whoever authors the events.

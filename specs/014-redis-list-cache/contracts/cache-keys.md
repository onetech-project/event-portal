# Contract: Cache Key Grammar and Cacheable Surfaces

**Feature**: 014-redis-list-cache | **Date**: 2026-08-10

This file is the closed list Constitution Principle VII requires. **Adding a row to the
surface table requires a constitution amendment**, not just a code change.

---

## 1. Key grammar

```text
generation key :  gen:events
                  gen:event:{event_uuid}
                  gen:orders

entry key      :  list:{family}:[{event_uuid}:]{fingerprint}:g{generation}
```

The generation is the **last** segment, not a middle one as an earlier draft of
this file had it. That is forced by the read script: it fetches the counter and
then builds the data key by concatenating the prefix with the value, which only
works if the value goes on the end. Worked examples:

```text
list:events_public:-:g0
list:ticket_types_public:8f0e…:-:g3
list:orders_admin:st=PAID:ev=_:g7
```

Rules:

- `{family}` is one of the nine values in §2. It is also the Prometheus label.
- `{generation}` is the current integer value of the family's scope counter, read at
  request time. Absent counter reads as `0`.
- `{fingerprint}` renders the read's parameters. It MUST be **stable** (same parameters →
  same string, always) and **injective** (different parameters → different strings).
  Parameters are rendered in a fixed order, never map-iteration order.
- `{fingerprint}` is `-` for reads that take no parameters.
- Keys are ASCII, colon-separated, and contain no user-supplied free text — only UUIDs and
  values from closed enums. There is no key-injection surface.

---

## 2. The nine cacheable surfaces

| # | Family | Read path | Scope | Fingerprint |
|---|---|---|---|---|
| 1 | `events_public` | `event.Service.ListPublishedEvents` | `events` | `-` |
| 2 | `ticket_types_public` | `event.Service.TicketTypesForEventSlug` | `event:{id}` | `-` (key is already per-event) |
| 3 | `packages_public` | `event.Service.PackagesForEventSlug` | `event:{id}` | `-` |¹
| 4 | `events_admin` | `event.Service.ListEvents` | `events` | `-` |
| 5 | `ticket_types_admin` | `event.Service.ListTicketTypes` | `event:{id}` | `-` |
| 6 | `packages_admin` | `event.Service.ListPackagesByEvent` | `event:{id}` | `-` |
| 7 | `orders_admin` | `order.AdminService.ListOrders` | `orders` | `st={status\|_}:ev={uuid\|_}` |
| 8 | `attendees_admin` | `order.AdminService.ListAttendees` | `orders` | `or={uuid\|_}:ev={uuid\|_}` |
| 9 | `packages_by_event` | `event.Service.PackagesForEvent` | `event:{id}` | `-` |

`_` denotes an absent optional filter, distinct from any present value.

¹ **Implementation note.** `PackagesForEventSlug` resolves the slug to an event id
and then delegates to `PackagesForEvent`, so in practice the slug path is served
by the `packages_by_event` entry (#9) and stores nothing of its own. Caching at
the delegate is strictly better — one entry per event instead of two copies of the
same list under two keys — so the `packages_public` constructor exists and is
tested but is not currently on a live read path.

**Per-event families** (2, 3, 5, 6, 9) embed the event id in their scope counter, so the
entry key is `list:{family}:{event_uuid}:g{generation}`. The event id appears in both the
generation key and the entry key; that redundancy is deliberate, so an entry can be read
back and attributed without consulting the counter.

### Explicitly NOT cacheable

Per Principle VII, and enforced by the absence of a registry row:

- `GET /event/:id` — single event detail
- `GET /ticket/order/:order_id` — order detail
- `GET /tickets/:code` — ticket lookup
- `GET /ticket/checkout/:order_id/status` — payment status (SSE)
- `GET /ticket/order/:order_id/qris.png` — QR image
- `GET /admin/fees` — fee list (not in Principle VII's enumeration; see research.md)
- `GET /ticket/genders` — small static lookup, not worth a network hop
- Any single-record admin `GET /admin/{entity}/:id`
- Every `POST` / `PUT` / `DELETE`

---

## 3. TTL

Every entry key is written with `CACHE_TTL` (default `10m`). Generation keys are written
**without** TTL.

The TTL is a backstop bounding the damage of a missed invalidation (FR-015). It is not the
freshness mechanism, and no requirement may be satisfied by "it expires eventually" —
Principle VII forbids that reading explicitly.

---

## 4. Read algorithm

```text
1. if cache disabled OR distrusted           → read Postgres, return
2. if db.InTransaction(ctx)                  → ErrInTransaction (programming error)
3. EVALSHA(get_script, gen_key, entry_prefix)
     ├── error                               → count cache_errors_total, read Postgres, return
     ├── nil (miss)                          → step 4
     └── bytes (hit)                         → count hit, unmarshal, return
4. singleflight(entry_key):
     a. read Postgres
     b. marshal; SET entry_key value EX CACHE_TTL   (best-effort; error is logged, not returned)
     c. return value
```

Step 3's error branch is the fail-open rule (FR-013): no cache error may ever surface as
an HTTP error.

## 5. Invalidate algorithm

```text
1. if cache disabled                         → no-op
2. if db.InTransaction(ctx)                  → ErrInTransaction (must never happen; FR-023)
3. for each scope in set: INCR gen:{scope}   (pipelined)
     ├── all succeed                         → count cache_invalidations_total
     └── any fails                           → retry once after 50ms backoff
           └── still failing                 → enter distrust for 30s, log with scope + record id
```

Distrust is the FR-014 recovery: while distrusted, step 1 of the read algorithm bypasses
the cache, so the system cannot serve pre-write content.

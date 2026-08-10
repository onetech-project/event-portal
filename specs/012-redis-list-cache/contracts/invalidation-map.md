# Contract: Invalidation Map

**Feature**: 012-redis-list-cache | **Date**: 2026-08-10

Every write path that touches cached data, and the scopes it MUST bump after its
transaction commits. This table is the checklist for FR-006 and FR-008: a write path
missing from it is a staleness bug.

**Timing rule for every row**: the bump happens *after* commit and *outside* the
transaction (FR-023, Principle VII). Paths that go through `db.InTx` add their scopes to
the context's scope set and `InTx` flushes them on the success path; paths that issue a
single statement invalidate at the call site immediately after the write returns.

---

## 1. Event domain — `internal/event`

| Write | File | Scopes bumped |
|---|---|---|
| `CreateEvent` | `admin_service.go:44` | `events` |
| `UpdateEvent` | `admin_service.go:60` | `events`, `event:{id}` |
| `DeleteEvent` | `admin_service.go:87` | `events`, `event:{id}` |
| `CreateTicketType` | `admin_service.go:185` | `event:{event_id}` |
| `UpdateTicketType` | `admin_service.go:222` | `event:{event_id}` |
| `DeleteTicketType` | `admin_service.go:256` | `event:{event_id}` |
| `CreatePackage` | `package_service.go:178` | `event:{event_id}` |
| `UpdatePackage` | `package_service.go:219` | `event:{event_id}` |
| `DeletePackage` | `package_service.go:273` | `event:{event_id}` |

**`UpdateEvent` bumps both scopes** because a publish/unpublish or a title change alters
the catalogue row *and* the event's own admin view. `CreateEvent` needs only `events`: a
brand-new event has no derived per-event entries yet.

**A ticket-type write does not bump `events`.** The catalogue (`events_public`) carries no
per-ticket data. If that ever changes — a "from Rp X" price badge, a sold-out flag — this
row must change with it, or the catalogue goes stale. Flagged here because it is the
likeliest future regression in this table.

---

## 2. Order domain — `internal/order`

| Write | File | Scopes bumped |
|---|---|---|
| `Book` (TX-B: order + items + attendees + quota deduction) | `service.go:81` | `orders`, `event:{event_id}` |
| `CheckoutOrder` (form saving) | `service.go:340` | `orders` |
| `RecordAgreement` | `service.go:239` | `orders` |

`Book` bumps the event scope because quota deduction changes what
`ticket_types_public` and `packages_public` report as available (US2). This is the highest-frequency
invalidation in the system and the one that most needs to be a single `INCR` outside the
lock window — a booking commits while other buyers are queued on the same ticket-type row.

Fee CRUD (`CreateFee`/`UpdateFee`/`DeleteFee`) bumps nothing: fee lists are not cached.

---

## 3. Payment domain — `internal/payment`

| Write | File | Scopes bumped |
|---|---|---|
| `applyOutcome` → `PAID` (settlement/capture) | `service.go:531` | `orders` |
| `applyOutcome` → `EXPIRED`/`CANCELLED` (expire, cancel, deny, failure) + quota restore | `service.go:531` | `orders`, `event:{event_id}` |
| `ExpireDueOrders` (sweeper) | `service.go:286` | `orders`, `event:{event_id}` per expired order |

**Required supporting change — resolving the event scope in the payment domain.**
`orders` has **no `event_id` column**: an order reaches its event only through
`order_items → ticket_types.event_id`. The payment domain holds `[]QuotaHold`, which
carries `TicketTypeID` and `Quantity` only, so it cannot name the event scope on its own.

Resolve it the way this codebase already resolves the same relationship — through an
injected interface, never a cross-domain join or repository import (Principle II).
`internal/order/admin_service.go` already does the forward direction via
`orderEventLookupAdapter`; this feature needs the inverse:

```go
// internal/payment — declared by the consumer, per ARCHITECTURE.md §3.2.
// Satisfied by event.Service, wired in main.go alongside the existing QuotaRestorer.
type EventScopeLookup interface {
    EventIDsForTicketTypes(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}
```

`event.Service` gains `EventIDsForTicketTypes` as the mirror of its existing
`TicketTypeIDsForEvent` (`admin_service.go:152`). It is called **after** the transaction
commits, on the same holds the restore already iterated — so it adds one indexed read to
the webhook's post-commit path and nothing at all to the locked window.

Rejected alternative: adding `event_id` to `orders`, or extending
`ListQuotaHoldsByOrderID` to select `ticket_types.event_id`. Both are schema/query changes
serving a cache concern, and the second puts a cross-domain join into the order domain's
SQL to save one indexed lookup.

**Webhook timing (FR-008 + Principle IV)**: the webhook must still return `200 OK`
immediately. Invalidation is part of the post-commit work, not the response path — but it
belongs *before* or alongside the non-blocking fulfilment goroutine, never behind PDF
generation and SMTP. A guest refreshing the event page must not wait on an email send to
see restored quota.

**Idempotency**: a webhook that short-circuits on an already-`PAID` order performs no
write and MUST NOT bump anything. A redundant bump is harmless for correctness but
discards a warm cache on every duplicate delivery, and payment providers retry.

---

## 4. Ticket domain — `internal/ticket`

Ticket issuance writes `tickets` rows, which no cached surface reads. **No invalidation.**

Listed here so the omission is visibly deliberate rather than an oversight.

---

## 5. Admin flush

| Action | Scopes |
|---|---|
| `POST /api/v1/admin/cache/refresh` | All — `FLUSHDB` on the cache's database, counters included |

See [admin-cache-refresh.md](./admin-cache-refresh.md).

---

## 6. Verification

Each row needs an integration test of the shape: read the list (warm), perform the write,
read again, assert the second read shows the change **and** that the second read was a
miss. Testing only the returned value would pass even if the cache were bypassed
entirely — the miss assertion is what proves invalidation actually happened.

SC-002 requires this across ≥ 100 write-then-read cycles per list family with zero stale
responses.

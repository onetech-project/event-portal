# API Contract: Availability Check

**Feature**: 013-booking-availability-gate · Base path `/api/v1`

Conventions are inherited from [specs/008 contracts/api.md](../../008-e2e-purchase-flow/contracts/api.md):
every body is wrapped in `{ "code": number, "message": string, "data": T }`, snake_case
throughout, success is `code: 200000`, errors are `HTTP status × 1000 + sub-code`.

---

## 1. `POST /api/v1/ticket/availability`

Answers whether one selection can be bought **right now**. Unauthenticated, like the rest
of the guest surface (Constitution Principle VI).

**This endpoint reserves nothing.** It creates no order, takes no row lock, deducts no
quota, and writes nothing. A `true` answer confers no right to book — see §5.

### Request

Identical in shape to `POST /ticket/book`, deliberately: the client sends the same object
to both, so the check cannot approve a selection that booking never saw.

```json
{
  "event_id": "9b1f...-uuid",
  "items": [
    { "ticket_type_id": "0c2a...-uuid", "package_id": null, "quantity": 2 },
    { "ticket_type_id": null, "package_id": "7e55...-uuid", "quantity": 1 }
  ]
}
```

| Field | Type | Rules |
|-------|------|-------|
| `event_id` | uuid | Required, non-nil |
| `items` | array | Required, non-empty |
| `items[].ticket_type_id` / `items[].package_id` | uuid \| null | Exactly one set (XOR) |
| `items[].quantity` | int | `> 0` |

Reuses `BookRequest`'s existing `validateItemLines`.

### Response — 200, selection is purchasable

```json
{
  "code": 200000,
  "message": "OK",
  "data": { "available": true, "reasons": [] }
}
```

### Response — 200, selection is refused

`available: false` is a **successful answer to a well-formed question**, not an error. The
HTTP status is 200 and the envelope code is 200000.

```json
{
  "code": 200000,
  "message": "OK",
  "data": {
    "available": false,
    "reasons": [
      {
        "item_index": 0,
        "ticket_type_id": "0c2a...-uuid",
        "package_id": null,
        "code": "INSUFFICIENT_QUOTA",
        "message": "Only fewer than 2 ticket(s) remain."
      },
      {
        "item_index": 1,
        "ticket_type_id": null,
        "package_id": "7e55...-uuid",
        "code": "PACKAGE_NOT_ON_SALE",
        "message": "Package \"Weekend Pass\" is not currently on sale."
      }
    ]
  }
}
```

**Every** refusal is reported, not the first (FR-006). `reasons` is empty if and only if
`available` is true.

`code` is the **stable string** code, not the numeric envelope code. This is load-bearing:
`apperr.Numeric()` renders `TICKET_TYPE_NOT_ON_SALE`, `PACKAGE_NOT_ON_SALE` and
`VALIDATION_ERROR` all as `400001`, so a client branching on numbers could not satisfy
FR-012's requirement that those produce distinct messages.

| `code` | Meaning | `item_index` |
|--------|---------|--------------|
| `INSUFFICIENT_QUOTA` | Aggregated demand for a ticket type exceeds its remaining quota | set — one reason per contributing line |
| `TICKET_TYPE_NOT_ON_SALE` | Outside that ticket's sale window | set |
| `PACKAGE_NOT_ON_SALE` | Outside the bundle's window, or any constituent's | set |
| `TICKET_TYPE_NOT_FOUND` | Ticket type no longer exists | set |
| `PACKAGE_NOT_FOUND` | Bundle no longer exists | set |
| `VALIDATION_ERROR` | Line belongs to a different event than `event_id` | set |
| `TERMS_MISSING` | Event has no authored Terms & Conditions | **null** — order-level |

No new `apperr` code is introduced by this feature.

### Response — 4xx, the request itself is wrong

Malformed input is not a decision; it goes through the normal error envelope.

| HTTP | envelope `code` | When |
|------|-----------------|------|
| 400 | 400001 | `event_id` nil, `items` empty, XOR violated, `quantity <= 0`, unparseable body |
| 429 | 429001 | Per-IP limit exceeded (§4) |
| 500 | 500000 | Database unreachable |

### `message` parity (FR-013)

The `message` on a reason is produced by the same code that produces booking's message for
that condition — `expandTicket`, `expandPackage`, and the `INSUFFICIENT_QUOTA` formatter in
`bookOnce`. A guest who meets the same problem at the check and again at Agree is told the
same sentence. This is guaranteed by construction (the evaluator calls `expandItem`), not
by two strings kept in step by hand.

---

## 2. Evaluation order

1. **Per line**, in request order: `expandItem` — existence, sale window, event scoping.
   A failure records a reason and **continues to the next line** (booking fails fast here;
   the check does not — FR-006).
2. **Aggregate** the successfully-expanded lines' demand per ticket type via
   `aggregateDemand`. This is what makes a bundle and a standalone ticket drawing on the
   same type judged **together** (FR-005).
3. **Per ticket type** in the aggregate: compare demand against `QuotaRemaining`. A
   shortfall records one reason per line contributing to that type.
4. **Order-level**: `CurrentTerms(event_id)`; `ErrNoTerms` records a `TERMS_MISSING` reason
   with `item_index: null`.

Steps 3 and 4 run even when step 1 produced reasons, so the guest sees everything wrong at
once rather than peeling problems off one press at a time.

Lines that failed step 1 contribute no demand to step 2 — their quantity is unknowable
against a type that may not exist.

---

## 3. Transaction and locking

Runs inside a single `db.InTx` transaction for snapshot consistency across the
primary-key reads, because `EventProvider.TicketTypeForCheckout` / `PackageForCheckout`
take a `pgx.Tx` by contract.

**It MUST NOT** issue `UPDATE`, `SELECT … FOR UPDATE`, or `CheckAndDeductQuota`. Principle
IV's rationale is that the quota-deducting `UPDATE` holds a row lock until commit, so
anything inside such a transaction serializes every concurrent buyer of that ticket type.
An advisory check that took those locks would inherit that cost while still returning an
answer that can be stale a millisecond later.

**It MUST NOT** read the Redis list cache. Principle VII: "A cached availability figure is
a display value only; it MUST NOT gate, authorize, or short-circuit a sale." Behavior is
therefore identical with `E2E_CACHE_ENABLED=false`, and the endpoint performs no write, so
it triggers no invalidation.

---

## 4. Rate limiting

Mounted on its own group in the composition root:

```go
availabilityGroup := e.Group("/api/v1",
    httpx.RateLimitPerIP(availabilityRate, availabilityBurst, rateLimitWindow))
orderHandler.RegisterAvailabilityRoute(availabilityGroup)
```

with `availabilityRate = 1.0`, `availabilityBurst = 10`.

**Not** shared with `bookGroup` (`0.33/s`, burst 5). The intended flow is check-then-book
plus another check on every adjust-and-retry, so sharing would make a guest who hits two
refusals rate-limited out of the very recovery path User Story 2 specifies.

Route registration: `POST /ticket/availability`. No conflict with the event domain's
`GET /ticket/:event_id` — different method, and Echo prefers static segments anyway.

---

## 5. What this endpoint does not promise

`available: true` is a statement about the past by the time the client reads it. Between
the decision and the Agree press, another buyer can take the last seat, an admin can close
a sale window, or a bundle's constituent can sell out.

Booking remains the sole authority (FR-011). **Every** refusal path in `bookOnce` —
`expandItem`'s window checks, the event-scoping check, `CheckAndDeductQuota`, the terms
guard — MUST survive this change untouched. The check narrows the window in which a guest
wastes effort; it does not close it, and it must never be treated as permission.

---

## 6. Guard test (required, not optional)

`TicketTypeInfo.QuotaRemaining` puts a lock-free quota figure within reach of the booking
transaction. If a later change uses it there as a "cheap pre-check", the oversell that
`CheckAndDeductQuota` exists to prevent comes straight back — and it would come back
silently, because the happy path would look identical.

The implementation MUST include a database-backed regression test asserting that
concurrent bookings cannot oversell: N goroutines booking the last seat of a ticket type
concurrently produce exactly one success and N−1 `INSUFFICIENT_QUOTA` refusals, with
`ticket_types.quota` landing at 0 and never negative.

`cmd/api/architecture_test.go` must also stay green: the order domain still reaches event
data only through `EventProvider`, and `internal/event` is still never imported.

---

## 7. Client contract

`frontend/lib/queries.ts` gains `useCheckAvailability`:

- `retry: false` — same reasoning as `useBookOrder`. A retry on an ambiguous transport
  failure would double the load on an endpoint whose answer is advisory anyway.
- Branches on the **string** `code` in each reason, not on `API_CODES`' numeric values.
- A transport failure (`ApiError` with `status: 0`) is **not** a refusal: it is "we could
  not check". FR-012 requires it to have its own message, and FR-002 requires the terms
  dialog to stay shut (a failed check is not a passing one).

`TermsDialog` becomes controlled — `open` / `onOpenChange` props, `DialogTrigger` removed.
Its internal behavior (terms fetch, agree, book, retry latch, `TERMS_CHANGED` refetch) is
unchanged.

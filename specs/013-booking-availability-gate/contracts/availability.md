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

`code` is the **stable string** code, not the numeric envelope code. This is load-bearing,
and remains so after the 2026-08-19 amendment: `apperr.Numeric()` renders
`TICKET_TYPE_NOT_ON_SALE`, `PACKAGE_NOT_ON_SALE` and `VALIDATION_ERROR` all as `400001`,
so a client branching on numbers cannot tell an FR-012 availability race from the
`VALIDATION_ERROR` the spec's Assumptions carve out. The reason the string is needed
changed — from "one distinct sentence per code" to "which of the three message buckets
this refusal falls in" — but the need did not.

| `code` | Meaning | `item_index` |
|--------|---------|--------------|
| `INSUFFICIENT_QUOTA` | Aggregated demand for a ticket type exceeds its remaining quota | set — one reason per contributing line |
| `TICKET_TYPE_NOT_ON_SALE` | Outside that ticket's sale window | set |
| `PACKAGE_NOT_ON_SALE` | Outside the bundle's window, or any constituent's | set |
| `TICKET_TYPE_NOT_FOUND` | Ticket type no longer exists | set |
| `PACKAGE_NOT_FOUND` | Bundle no longer exists | set |
| `VALIDATION_ERROR` | Line belongs to a different event than `event_id`, **or** a bundle has no components | set |
| `TERMS_MISSING` | Event has no authored Terms & Conditions | **null** — order-level |
| `INTERNAL_ERROR` | `expandItem` failed with a non-`apperr` fault — defensive, not expected | set |

No new `apperr` code is introduced by this feature.

`TICKET_TYPE_NOT_FOUND` has a second, order-level source with `item_index` **null**: a
bundle constituent that vanished between the package read and the aggregate read
(`availability.go:157-171`). `TERMS_MISSING` is therefore not the only null-index reason.

**Message buckets (2026-08-19 amendment).** The client renders by bucket, never by
sentence:

| Bucket | Codes | Guest sees |
|--------|-------|------------|
| Availability (FR-012) | `INSUFFICIENT_QUOTA`, `TICKET_TYPE_NOT_ON_SALE`, `PACKAGE_NOT_ON_SALE`, `TICKET_TYPE_NOT_FOUND`, `PACKAGE_NOT_FOUND` | the one fixed general message, once, however many reasons arrived |
| Terms (FR-012a) | `TERMS_MISSING` | its own sentence |
| Not a race | `VALIDATION_ERROR`, `INTERNAL_ERROR` | its own sentence |

An unrecognised code MUST fall into the availability bucket: a decision the client cannot
classify is still a refusal, and the general message is the safe thing to say about one.

### Response — 4xx, the request itself is wrong

Malformed input is not a decision; it goes through the normal error envelope.

| HTTP | envelope `code` | When |
|------|-----------------|------|
| 400 | 400001 | `event_id` nil, `items` empty, XOR violated, `quantity <= 0`, unparseable body |
| 429 | 429001 | Per-IP limit exceeded (§4) |
| 500 | 500000 | Database unreachable |

### `message` is diagnostic (FR-006, as amended 2026-08-19)

The `message` on a reason is produced by the same code that produces booking's message for
that condition — `expandTicket`, `expandPackage`, and the `INSUFFICIENT_QUOTA` formatter in
`bookOnce`. It is **never rendered to a guest**. FR-006 keeps it as the record of why a
selection was refused; FR-012 replaces it on screen with one fixed general message.

FR-013's "one story at both points" is now satisfied by both points showing that same
fixed message — not, as originally, by both echoing this sentence. In particular
`"Only fewer than %d ticket(s) remain."` MUST NOT reach the guest at either point.

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
- Branches on the **string** `code` in each reason, not on `API_CODES`' numeric values —
  on **this** endpoint, where the string is available and exact.
- A transport failure (`ApiError` with `status: 0`) is **not** a refusal: it is "we could
  not check". FR-012a requires it to have its own message, and FR-002 requires the terms
  dialog to stay shut (a failed check is not a passing one).
- A throttled check (HTTP 429 / `429001`) is likewise **not** an availability refusal and
  MUST be worded by the client. Echo's default deny handler answers with the raw
  middleware string `"rate limit exceeded"`, which must never reach the alert. Use the
  same sentence `TermsDialog` already shows for a throttled booking, so one condition
  reads one way at both points. See [research.md](../research.md) D10.

`TermsDialog` becomes controlled — `open` / `onOpenChange` props, `DialogTrigger` removed.

### `TermsDialog` after the 2026-08-19 amendment

Its internals are **no longer unchanged** — FR-013a requires the book leg to classify its
own failure and close.

- The `book` catch classifies on the **numeric** `ApiError.code`, because the booking
  error envelope is `{code int, message, data}` and never carries the stable string
  (`apperr.go:180-201`, pinned by `handler_test.go:83-108`). Availability =
  `400002`, `404001`, `400001`; everything else keeps today's in-dialog alert. The
  `400001` call and its residual risk are argued in [research.md](../research.md) D8.
- On an availability refusal the dialog MUST close by calling **its own**
  `onOpenChange(false)`, then report upward through a separate callback so
  `SelectionSummary` can render the general message in the region the pre-check uses.
  A parent that merely flips `open` does **not** run `handleOpenChange`, because base-ui
  fires `onOpenChange` only from `setOpen` — leaving `agreed`, `book.error` and the
  "Retry" button live for the next open. [research.md](../research.md) D9.
- Nothing is abandoned by that close: an availability refusal throws from the `book` leg
  before `setBookedOrderId`, so no held order exists yet.
- The terms fetch, the agree leg, the retry latch and the `TERMS_CHANGED` refetch are
  genuinely unchanged.

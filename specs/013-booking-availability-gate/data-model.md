# Phase 1 Data Model: Booking Availability Gate

**Feature**: 013-booking-availability-gate · **Date**: 2026-08-11

## Database schema: no change

**No migration. No new table, column, index, or constraint. `SCHEMA.md` is not touched by
this feature.**

That is worth stating rather than leaving implicit, because AGENTS.md makes
"`SCHEMA.md` changes in the same commit as any migration" a standing rule — and the
absence of a migration here is a design outcome, not an oversight. Everything the
availability decision needs is already stored and already read on the booking path:

| Existing storage | Read by | Used for |
|------------------|---------|----------|
| `ticket_types.quota` (**REMAINING** quota, never an allocation) | `GetTicketTypeByID` | Quota comparison — the figure the adapter currently discards |
| `ticket_types.sales_start` / `sales_end` | same row | Per-ticket sale window |
| `ticket_types.event_id` | same row | Event-scoping refusal |
| `packages.price`, `sales_start`, `sales_end`, `is_active` | `GetPackageForCheckout` | Bundle window and status |
| `package_tickets.quantity` + constituents' windows | same query | Per-unit composition and constituent windows |
| `event_terms` (current document) | `CurrentTermsForEvent` | Whether there is anything to agree to |

## Runtime entities

### AvailabilityRequest *(new — wire input)*

Deliberately identical in shape to `BookRequest`, so the client sends the *same* object to
the check and to booking. Anything else invites the two to diverge and the check to
approve a selection booking never saw.

| Field | Type | Rules |
|-------|------|-------|
| `event_id` | UUID | Required, non-nil. Same rule as `BookRequest.Validate`. |
| `items` | `[]CheckoutItem` | Required, non-empty. Reuses `validateItemLines` — the existing XOR of `ticket_type_id` / `package_id` and `quantity > 0`. |

Validation failures here are **400s through `apperr`**, not decisions. A malformed request
has no availability answer (see [research.md](research.md) D1).

### AvailabilityDecision *(new — wire output, transient)*

The server's answer about one selection at one instant. **Not persisted, not cached, not
binding.** It reserves nothing and confers no right to book.

| Field | Type | Meaning |
|-------|------|---------|
| `available` | bool | True only when `reasons` is empty. The client gates the terms dialog on this and nothing else. |
| `reasons` | `[]AvailabilityReason` | Every refusal found, not the first. Empty when `available` is true. |

### AvailabilityReason *(new)*

One refusal, addressed to the line that caused it where a line caused it.

| Field | Type | Meaning |
|-------|------|---------|
| `item_index` | `*int` | 0-based index into the request's `items`. **Null** for an order-level reason — today only `TERMS_MISSING`. |
| `ticket_type_id` | `*UUID` | Set when the reason is attributable to a ticket type. For a quota shortfall reached through a bundle this names the *constituent*, which is what the guest needs to understand why. |
| `package_id` | `*UUID` | Set when the offending line was a bundle. |
| `code` | string | A **stable string** code, not the numeric envelope code — see below. |
| `message` | string | The guest-facing sentence, produced by the same code that produces booking's. |

**Why the string code and not the numeric one.** `apperr.Numeric()` renders
`TICKET_TYPE_NOT_ON_SALE`, `PACKAGE_NOT_ON_SALE` and `VALIDATION_ERROR` all as `400001`.
FR-012 requires those to be distinguishable by the client. The string codes do not
collide, and they are already the vocabulary the backend uses internally.

Codes a reason may carry, all drawn from the existing `apperr` registry — this feature
introduces no new code:

| `code` | Raised when | Attributed to |
|--------|-------------|---------------|
| `INSUFFICIENT_QUOTA` | Aggregated demand for a ticket type exceeds its remaining quota | Every line contributing demand to that type |
| `TICKET_TYPE_NOT_ON_SALE` | `now` outside a ticket's `sales_start`/`sales_end` | That line |
| `PACKAGE_NOT_ON_SALE` | `now` outside the bundle's window, **or** outside any constituent's | That line |
| `TICKET_TYPE_NOT_FOUND` | The ticket type no longer exists | That line |
| `PACKAGE_NOT_FOUND` | The bundle no longer exists | That line |
| `VALIDATION_ERROR` | The line belongs to a different event than `event_id` | That line |
| `TERMS_MISSING` | The event has no authored Terms & Conditions | The order (`item_index` null) |

### TicketTypeInfo *(existing — one field added)*

```go
type TicketTypeInfo struct {
    ID         uuid.UUID
    EventID    uuid.UUID
    Name       string
    Price      decimal.Decimal
    SalesStart time.Time
    SalesEnd   time.Time
    QuotaRemaining int32   // NEW
}
```

`QuotaRemaining` is the remaining-seat figure from `ticket_types.quota`, populated by
`eventProviderAdapter.TicketTypeForCheckout` from a column `GetTicketTypeByID` already
selects.

> **This field is for the advisory check only.** It is a snapshot read under no lock, and
> using it inside `bookOnce` to decide whether a sale may proceed would reintroduce
> exactly the oversell that the atomic, row-locked `CheckAndDeductQuota` exists to
> prevent (Constitution Principle IV; Principle VII, "the cache is never authoritative for
> inventory" — a lock-free read is no better). The guard test in
> [contracts/availability.md](contracts/availability.md) §6 enforces this.

### ExpandedItem, demand map *(existing — unchanged)*

`expandItem` and `aggregateDemand` in [demand.go](../../backend/internal/order/demand.go)
are reused verbatim. The evaluator does not fork, copy, or modify them; that is the whole
mechanism by which FR-013's wording parity holds.

## Client-side types

### `frontend/lib/types.ts` *(new)*

`AvailabilityDecision` and `AvailabilityReason` mirror the wire shape in snake_case, as
every other DTO in this file does.

### Selection state *(existing — unchanged)*

`Quantities`, `SelectionLine`, and `checkoutItems()` in
[selection.ts](../../frontend/lib/selection.ts) are untouched. FR-007 requires a refused
check to leave the selection intact, which is satisfied by the check never writing to that
state — no code change is the correct implementation of that requirement.

## State transitions

The Buy Ticket control, owned by `SelectionSummary`:

```text
        selection empty
            │
            ▼
      ┌───────────┐   selection made    ┌───────┐
      │   INERT   │────────────────────▶│ IDLE  │
      └───────────┘◀────────────────────└───────┘
                     selection cleared      │ press
                                            ▼
                                      ┌──────────┐
                                      │ CHECKING │──── press ignored (FR-008)
                                      └──────────┘
                                       │        │
                     available: true   │        │  available: false, or transport error
                                       ▼        ▼
                              ┌──────────┐   ┌──────────┐
                              │ TERMS    │   │ REFUSED  │
                              │ OPEN     │   └──────────┘
                              └──────────┘        │ selection edited, or press again
                                   │              ▼
                                   │          ┌───────┐
                                   └─────────▶│ IDLE  │  (dialog closed without booking)
                                              └───────┘
```

Two rules this diagram encodes:

- **`REFUSED` → `CHECKING` on the next press, never `REFUSED` → `TERMS OPEN`.** FR-009: a
  decision is never reused. There is no cached "it was fine a moment ago".
- **`CHECKING` absorbs further presses.** FR-008: no second concurrent check.

The order lifecycle (`PENDING` → `PAID` / `EXPIRED` / `CANCELLED`) is **not** touched. The
check creates no order, so it participates in no order state transition — which is the
single most important property of this design.

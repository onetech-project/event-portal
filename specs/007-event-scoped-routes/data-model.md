# Phase 1 Data Model: Event-Scoped Guest Navigation

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Research**: [research.md](./research.md)

**Database impact: none.** No table, column, index, constraint, or migration changes.
`SCHEMA.md` stays untouched, and no new SQL query is written — the one lookup the new
endpoint needs, `GetOrderByNumber`, already exists at
`backend/internal/order/repository.go:293`.

What follows describes the entities as this feature sees them: which fields drive the
routing, framing, guarding, and confirmation decisions.

---

## Entities

### Event

The organising unit for the journey. Everything under `/events/:slug` belongs to exactly
one of these.

| Field | Type | Role in this feature |
|---|---|---|
| `slug` | string | The route segment. Sole identifier of an event in every journey URL. |
| `name` | string | Displayed by the framing and on the confirmation screen; must not break layout when unusually long. |
| `start_date` | ISO 8601 string | The countdown target. |

Source: `EventDetail` (`frontend/lib/types.ts`), fetched by `useEventBySlug` from
`GET /api/v1/events/:slug`. Already contains everything needed — no change.

**Rules**

- The countdown renders only when `start_date` parses to a future instant. A past or
  unparseable date renders nothing (FR-003). `EventSummary.start_date` is typed non-null,
  but `SalesCountdown` already accepts `string | null` and that guard is kept rather than
  tightened — an absent date must degrade, not throw.
- An unresolvable slug suppresses all nested content (FR-007).

---

### Order

A guest's purchase for a single event. Drives which screen the guest sees and what the
confirmation reports.

| Field | Type | Role in this feature |
|---|---|---|
| `order_number` | string | The route segment under the event; the resend endpoint's identifier. |
| `event.slug` | string | **Ownership key.** Compared against the URL segment on both order routes (FR-013). |
| `event.name` | string | Named in the confirmation's success message. |
| `status` | `PENDING` / `PAID` / `EXPIRED` / `CANCELLED` | **Routing key.** Anything final forwards the guest to confirmation (FR-018) and selects success vs. expired/cancelled treatment (FR-020). |
| `total_amount` | decimal string | "Total Paid" on the confirmation. |
| `items[]` | order items | The purchased-items list on the confirmation. |
| `buyer_email` | string | Where a resend goes. Displayed only as an assurance that mail was sent — never as an input. |
| `payment`, `server_time` | — | Payment screen only; untouched (FR-015). |

Source: `PublicOrderDetail` (`frontend/lib/types.ts`), fetched by `useOrderDetail` from
`GET /api/v1/orders/:orderNumber`. **Everything the confirmation screen needs is already
in this response** — `event.slug`, `event.name`, `status`, `total_amount`, `items`, and
`buyer_email`. No new read endpoint.

**Rules**

- `event.slug !== <url slug>` → not-found treatment scoped to the current event. Never a
  redirect to the true owner (FR-013).
- `isFinalOrderStatus(status)` → `router.replace` to the `done` route (FR-018). The helper
  already exists in `frontend/lib/queries.ts` and already backs the polling stop condition,
  so one predicate governs both "stop asking" and "move on".
- A 404 from the API keeps its existing message; the wrong-event case gets its own wording
  so the two failures stay distinguishable in support conversations.

---

### Order item

One purchased ticket type or bundle. Already rendered on the order summary card; the
confirmation screen re-presents the same list in the approved design's form.

| Field | Role |
|---|---|
| `kind` | Selects `ticket_type_name` or `package_name` |
| `ticket_type_name` / `package_name` | The item label |
| `quantity` | Shown as "Quantity: N Ticket(s)" |
| `subtotal` | Not shown on the confirmation — the design shows only the order total |

No change to the type.

---

### Booking stage (derived, not stored)

A pure function of the current pathname. Not persisted, not passed as a prop, not held in
context — see [research.md](./research.md) R-004 for why.

| Pathname shape | Stage |
|---|---|
| `/events/:slug` | `Booking` |
| `/events/:slug/checkout` | `Registration` |
| `/events/:slug/orders/:n` | `Payment` |
| `/events/:slug/orders/:n/done` | `Done` |

Implemented in `frontend/lib/booking-stage.ts` against the existing `BOOKING_STEPS` tuple
in `frontend/components/booking/booking-steps.tsx`. `/done` must be matched before the
bare order route, which is a prefix of it.

---

## Backend: the new interface method

The public resend endpoint needs an order **UUID** but receives an order **number**. Per
Constitution Principle II the notification domain declares that need on its own interface
rather than importing the order domain.

```text
internal/notification/service.go
  type OrderProvider interface {
      OrderForDelivery(ctx, orderID uuid.UUID) (OrderDelivery, error)   // exists
      MarkEmailSent(ctx, orderID uuid.UUID) error                       // exists
      OrderIDByNumber(ctx, orderNumber string) (uuid.UUID, error)       // NEW
  }
        ▲ satisfied by
cmd/api/adapters.go
  notificationOrderAdapter.OrderIDByNumber
        │ delegates to
internal/order/repository.go:293
  Repository.GetOrderByNumber(ctx, orderNumber) (OrderRecord, error)    // exists
```

The adapter is the same shape as `OrderForDelivery` at `cmd/api/adapters.go:179`. A
missing order returns the domain's existing not-found error, which the public handler
swallows into a uniform `202` (FR-026) rather than surfacing.

**No `sqlc` regeneration.** Nothing under `internal/*/queries/` changes.

---

## Client-side state

No new store, context, or reducer.

| State | Owner | Lifetime |
|---|---|---|
| Event query `["events", slug]` | TanStack Query cache | Shared by the frame and the detail page; `staleTime: 0` |
| Countdown remaining time | `SalesCountdown` `useState` + 1 Hz interval | Mounted with the layout, survives leaf navigation |
| Booking stage | derived from `usePathname()` | Recomputed per render; no state |
| Order query `["orders", n]` | TanStack Query cache | Shared by the payment and confirmation screens — the forward to `done` costs no extra fetch |
| Resend mutation | `usePublicResendEmail` | Confirmation screen only |
| Selection quantities | `events/[slug]/page.tsx` | Leaf-scoped; unchanged |

The countdown's survival across navigation is a property of where the component is
mounted, not of any state being lifted or persisted. Likewise, the confirmation screen
reuses the order query the payment screen already warmed, so forwarding is instant.

---

## Validation rules by requirement

| Rule | Where enforced | Requirement |
|---|---|---|
| Countdown hidden when start date is absent or past | `SalesCountdown.remainingFrom()` — already returns `null` | FR-003 |
| No placeholder countdown while the event resolves | `EventFrame` renders no countdown during `isPending` | FR-008 |
| Rail renders even while the event is pending | `EventFrame` — the rail depends on pathname, not on the event | FR-004 |
| Stage cannot contradict the address | `lib/booking-stage.ts`, derived; no page sets it | FR-005 |
| Nested content suppressed for an unknown event | `EventFrame` gates `{children}` on the error branch | FR-007 |
| Order belongs to the event in the URL | both order routes: `data.event.slug === slug` | FR-013 |
| Settled orders never show payment instructions | order page: `isFinalOrderStatus(status)` → `replace` | FR-018 |
| Wrong-event access never redirects to the owner | guards render, never navigate | Edge case: deep-linked mismatch |
| Resend targets only the stored address | endpoint accepts no body; address read from the order | FR-024 |
| Resend frequency capped | Echo rate limiter keyed on `:orderNumber`, 1 / 60s | FR-025 |
| Resend discloses nothing about existence | uniform `202` for known and unknown orders | FR-026 |

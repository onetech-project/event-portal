# API Contract: `POST /api/v1/orders/:orderNumber/resend-email`

**Feature**: [007-event-scoped-routes](../spec.md) | **Rationale**: [research.md R-006](../research.md)

**New endpoint.** The only API change in this feature. It lets a guest on the confirmation
screen re-send their own ticket email without authenticating (FR-021, FR-023).

---

## Request

```http
POST /api/v1/orders/ORD-20260801-A1B2C3D4/resend-email
```

No body. No authentication. The order number in the path is the **only** input — the
endpoint deliberately accepts no destination address (FR-024).

## Response

Always `202 Accepted` with a generic body, whether or not the order exists:

```json
{ "message": "If that order exists, its ticket email has been sent again." }
```

| Condition | Status | Behaviour |
|---|---|---|
| Order exists and is `PAID` | 202 | Email re-sent to the buyer's stored address |
| Order exists, not `PAID` | 202 | No email — `SendTicketEmail` already refuses non-paid orders |
| Order does not exist | 202 | No email, identical body (FR-026) |
| Rate limit exceeded | 429 | `{"message": "Please wait before requesting another email."}` |

### Why 202 and not 200/404

A `404` would let anyone probe whether a given order number exists, and order numbers are
short enough to guess at. A uniform `202` closes that. The cost is that a guest who
mistypes gets no correction — acceptable, because the button is only ever reached from the
guest's own confirmation screen, where the order number is filled in for them.

`429` is the one status that *does* differ, but it leaks nothing: the limiter is keyed on
the order number string and rejects unknown numbers at the same rate as real ones.

---

## Rate limiting (FR-025)

- **Key**: the `:orderNumber` path parameter — not the caller's IP. The protected resource
  is the buyer's inbox, and a guest's address is not a reliable identity (mobile networks,
  shared NAT). Keying on the order means one buyer cannot be mail-bombed regardless of how
  many callers participate.
- **Rate**: 1 request per 60 seconds per order number.
- **Store**: Echo's in-memory `middleware.RateLimiterMemoryStore`. Per-instance, which is
  correct for this MVP — Principle VI forbids Redis, and the deployment is single-instance
  under Docker Compose. Recorded in [plan.md](../plan.md)'s Constitution Check as a
  deliberate bound, not an oversight.
- **Scope**: the middleware is attached to this route only. It must not be applied to the
  API group, or it would throttle order polling, which runs every 3 seconds by design.

---

## Domain placement

`internal/notification` owns email delivery and already exposes the admin resend. The
public route is registered by the same handler:

```go
// internal/notification/handler.go
func (h *Handler) RegisterPublicRoutes(g *echo.Group) {
    g.POST("/orders/:orderNumber/resend-email", h.resendPublic, rateLimiter)
}
```

The handler needs an order **UUID**, but the guest supplies an order **number**. Per
Constitution Principle II the notification domain declares that need on its own interface
rather than importing the order domain:

```go
// internal/notification/service.go
type OrderProvider interface {
    OrderForDelivery(ctx context.Context, orderID uuid.UUID) (OrderDelivery, error)
    MarkEmailSent(ctx context.Context, orderID uuid.UUID) error
    OrderIDByNumber(ctx context.Context, orderNumber string) (uuid.UUID, error) // NEW
}
```

The composition root satisfies it, exactly as it already satisfies the other two:

```go
// cmd/api/adapters.go — notificationOrderAdapter
func (a notificationOrderAdapter) OrderIDByNumber(ctx context.Context, n string) (uuid.UUID, error) {
    record, err := a.orders.GetOrderByNumber(ctx, n)   // already exists, repository.go:293
    ...
}
```

No new SQL, no new query file, no migration.

---

## Relationship to the admin endpoint

`POST /api/v1/admin/orders/:id/resend-email` is **unchanged**: still JWT-guarded, still
keyed by UUID, still returns `ResendResponse` with the destination address in
`sent_to`. The two differ deliberately:

| | Admin | Public |
|---|---|---|
| Auth | JWT | none |
| Identifier | order UUID | order number |
| Discloses recipient | yes (`sent_to`) | no |
| Discloses existence | yes (404) | no (uniform 202) |
| Rate limited | no | 1 / 60s per order |

An admin operator is trusted and needs to see where the mail went; a guest is not and does
not.

---

## Consumer contract

```ts
// frontend/lib/types.ts
export type PublicResendResponse = { message: string };
```

Consumed by a `usePublicResendEmail(orderNumber)` mutation in `frontend/lib/queries.ts`,
called from the confirmation screen's resend button. A `429` surfaces as the "please wait"
message; every other outcome shows the returned `message`.

---

## Test obligations

1. A `PAID` order returns 202 and dispatches exactly one email to the buyer's stored address.
2. A non-existent order number returns 202 with a byte-identical body and dispatches nothing.
3. A non-`PAID` order returns 202 and dispatches nothing.
4. A second request for the same order inside 60 seconds returns 429 and dispatches nothing.
5. Two different order numbers do not share a limiter bucket.
6. The request body is ignored — supplying an email address in it changes no destination.
7. The admin endpoint's existing behaviour and response shape are unchanged.

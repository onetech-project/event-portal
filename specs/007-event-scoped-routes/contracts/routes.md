# URL Contract: Guest Routes

**Feature**: [007-event-scoped-routes](../spec.md) | **Plan**: [plan.md](../plan.md)

The guest-facing URL surface before and after this feature. Admin routes (`/admin/**`) are
untouched.

---

## After

| URL | Screen | Framing | Rail stage | Status |
|---|---|---|---|---|
| `/` | Home | site chrome only | — | unchanged |
| `/events` | Event list | site chrome only | — | unchanged |
| `/events/:slug` | Ticket & bundle selection | **event frame** | Booking | moved into the frame |
| `/events/:slug/checkout?t=…&p=…` | Registration + payment start | **event frame** | Registration | slug moves to the segment |
| `/events/:slug/orders/:orderNumber` | Payment (QRIS + live status) | **event frame** | Payment | new home; ownership-guarded |
| `/events/:slug/orders/:orderNumber/done` | Confirmation / receipt | **event frame** | Done | **new** |
| `/tickets` | Ticket code entry | site chrome only | — | **unchanged** |
| `/tickets/:code` | Ticket detail | site chrome only | — | **unchanged** |

"Event frame" = rendered inside `events/[slug]/layout.tsx`, which supplies the shared event
fetch, the not-found gate, the start-date countdown, and the progress rail.

The journey is exactly the flow the spec names: **events → choose ticket → checkout →
order → done**.

---

## Changes from before

| Before | After | Mechanism |
|---|---|---|
| `/checkout?slug=X&t=…&p=…` | `/events/X/checkout?t=…&p=…` | `encodeSelection()` emits the new path; the page reads the segment |
| `/orders/:n` | `/events/:slug/orders/:n` | Checkout redirect target changes |
| *(no confirmation screen)* | `/events/:slug/orders/:n/done` | New route; settled orders forward to it |
| `/orders/:n` as a route at all | **removed** | No forwarder — see below |

### Removed without replacement

The top-level `/orders/:orderNumber` route is deleted, not forwarded (FR-028). A guest
hitting it gets the standard 404.

This is safe because confirmation emails carry the ticket PDF, not a link — verified by
grepping `backend/internal/notification/`, which contains no guest-facing URL. The only
breakage is a browser bookmark made since the order screen shipped, and the guest can
still reach the order from the event.

### Explicitly unchanged

`/tickets` and `/tickets/:code` keep their current behaviour: root-level code entry and
root-level ticket detail. A guest holding a ticket code is **not** required to identify an
event first (FR-027). There is no event-scoped ticket screen, no code-to-event resolver,
and no `event_slug` field on the ticket API — all three were specified in an earlier
revision and removed by the 2026-08-04 clarification.

---

## Progress rail derivation (FR-005)

The frame derives the stage from `usePathname()`. No page passes its stage by hand, so no
screen can display a stage that contradicts its address.

| Pathname shape | Stage |
|---|---|
| `/events/:slug` | Booking |
| `/events/:slug/checkout` | Registration |
| `/events/:slug/orders/:n` | Payment |
| `/events/:slug/orders/:n/done` | Done |

Order matters: `/done` must be matched before the bare order route, since the latter is a
prefix of the former.

---

## Settled-order forwarding (FR-018)

The order screen is the *paying* screen only. Any settled status forwards to confirmation:

```text
GET /events/:slug/orders/:n
  status PENDING                       → render payment instructions
  status PAID | EXPIRED | CANCELLED    → router.replace(`/events/:slug/orders/:n/done`)
```

This covers both cases in one rule: an order that settles while the guest watches (the
existing 3-second poll observes it) and an order that was already settled when they opened
the link. `replace`, not `push`, so the back button does not bounce them into a payment
screen that immediately forwards again.

The confirmation screen reads the same order and reports the outcome — success for `PAID`,
a distinct expired/cancelled treatment otherwise (FR-020).

---

## Ownership guard (FR-013)

Both event-scoped order routes verify that the order they loaded belongs to the event in
the URL, and **fail closed**:

| Route | Check | On mismatch |
|---|---|---|
| `/events/:slug/orders/:n` | `data.event.slug === slug` | "Order not found for this event" + link back into `:slug` |
| `/events/:slug/orders/:n/done` | same | same |

Redirecting to the true owner would turn a wrong-event URL into a working one and defeat
the isolation this feature exists to create.

`PublicOrderDetail` already carries `event: { name, slug }`, so this needs no backend
change.

---

## Link inventory (FR-014)

| Location | Current target | Required target |
|---|---|---|
| `app/page.tsx:17` | `/tickets` | `/tickets` — unchanged |
| `lib/selection.ts` `encodeSelection()` | `/checkout?slug=…` | `/events/:slug/checkout?…` |
| `events/[slug]/checkout/page.tsx:106` | `/orders/:n` | `/events/:slug/orders/:n` |
| `events/[slug]/page.tsx` error branch | `/events` | `/events` — unchanged (browsing all events is explicit) |
| `events/[slug]/orders/[orderNumber]/page.tsx:62` | `/events` | `/events/:slug` |
| `components/order/payment-status-card.tsx:36` | `/events` | `/events/:slug` when the slug is known |
| `components/order/payment-status-card.tsx:61` | `/events/:slug` | unchanged — already correct |
| Confirmation screen "Back to Home" | — | `/` (per the approved design) |
| Confirmation screen, expired/cancelled | — | `/events/:slug` (FR-020) |
| `tickets/[code]/page.tsx:61` | `/tickets` | `/tickets` — unchanged |

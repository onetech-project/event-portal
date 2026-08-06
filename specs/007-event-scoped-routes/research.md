# Phase 0 Research: Event-Scoped Guest Navigation

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Date**: 2026-08-04

> **Revision note**: regenerated after the 2026-08-04 clarification re-scoped the feature.
> The previous revision's R-004 (`event_slug` on the ticket API), R-006 (ticket-code
> resolver), and the ticket half of R-005 are **withdrawn** — those screens no longer
> exist. See "Withdrawn decisions" at the end for what changed and why.

The Technical Context in plan.md carries no `NEEDS CLARIFICATION` markers — the stack is
fixed by the constitution and already in use. What follows resolves the design questions
the requirements raise against this codebase.

---

## R-001: How does the shared framing survive navigation?

**Decision**: A server `layout.tsx` at `frontend/app/(public)/events/[slug]/layout.tsx`
awaits `params` and hands the slug to a `"use client"` `EventFrame` that owns the fetch,
the not-found gate, the countdown, and the rail. `{children}` renders inside it.

**Rationale**: Next.js 16 App Router keeps a layout mounted when navigation changes only
the leaf segment — the framework's own bfcache tests state that "layout state persists
because the layout's key does not depend on child params". So the `setInterval` inside
`SalesCountdown` is never torn down as the guest moves through the journey. That is
FR-001 and SC-001 satisfied structurally rather than by re-synchronising a timer per screen.

Layouts receive `params` as a Promise in this version:

```tsx
export default async function Layout({ children, params }: {
  children: React.ReactNode
  params: Promise<{ slug: string }>
}) {
  const { slug } = await params
  return <EventFrame slug={slug}>{children}</EventFrame>
}
```

The split matters: the layout must not be `"use client"`, or every nested page is pulled
into the client boundary and the ability to `await params` on the server is lost.

**Alternatives considered**:

- *Render the countdown on each page separately.* Rejected — re-mounts on every navigation,
  which is exactly the reset SC-001 forbids, and duplicates the fetch on four screens.
- *`template.tsx` instead of `layout.tsx`.* Rejected — templates deliberately re-mount.
- *Lift to the `(public)` group layout with path sniffing.* Rejected — that layout also
  wraps `/events`, `/tickets`, and home, none of which have an event in scope.

---

## R-002: Does hoisting the event fetch violate the live-inventory rule?

**Decision**: No. `EventFrame` calls the existing `useEventBySlug(slug)`, the same hook the
detail page already calls, sharing TanStack key `["events", slug]`.

**Rationale**: Worth stating because FR-006 ("resolve the event once for the whole
journey") superficially reads like caching, and the constitution requires event/quota
fetches to disable aggressive caching. Two distinct mechanisms:

- **Deduplication** — two components mounting the same key in one render pass produce one
  in-flight request. This is what FR-006 asks for.
- **Staleness** — governed by `staleTime`, which `frontend/app/providers.tsx` sets to `0`
  app-wide with a comment naming quota freshness, on top of `cache: "no-store"` on every
  GET in `frontend/lib/api-client.ts`.

Adding a second consumer does not raise `staleTime`. Quota remains live inventory.

**Alternatives considered**: passing the event down via React context (more machinery, no
gain — the query cache already shares it); server-fetching in the layout (delivers a
snapshot that never refreshes without navigation, worse for quota).

---

## R-003: How does the framing behave while loading, and on an unknown event?

**Decision**: Three states in `EventFrame`:

| State | Countdown | Rail | `{children}` |
|---|---|---|---|
| `isPending` | not rendered | rendered | rendered |
| error / absent | not rendered | not rendered | **not** rendered; "event not found" shown |
| resolved | rendered if start date is future | rendered | rendered |

**Rationale**: FR-008 forbids a placeholder countdown, so rendering nothing while pending
is the only honest option — a `00:00:00` skeleton reads as an event starting right now.
The **rail is different**: it depends only on the pathname, not on the event, so it can
render immediately and should, since it is the guest's sense of progress.

FR-007 requires nested content suppressed for an unknown event, and the layout is the only
place that can enforce it for all four screens at once. Children still render during
`isPending` so the nested screen shows its own loading state — the spec's edge case is
explicit that nested screens must not be blocked.

**Alternatives considered**: `notFound()` from the server layout (rejected — the event is
fetched client-side, so the server layout has nothing to decide on); reserving vertical
space for the countdown (rejected as a layout-shift optimisation not worth the complexity).

---

## R-004: How does the rail know which stage the guest is on?

**Decision**: `EventFrame` derives it from `usePathname()` via a pure helper in
`frontend/lib/booking-stage.ts`. No page declares its own stage.

**Rationale**: FR-005 requires that no screen can display a stage contradicting its
address. A prop or context that each page sets is exactly the thing that drifts — the
current code proves it: `BookingSteps` is rendered once, at
`frontend/app/(public)/events/[slug]/page.tsx:57`, hardcoded to `current="Booking"`, and
the other three stages have never been shown to any guest. Deriving from the pathname makes
the wrong state unrepresentable.

The mapping is in [contracts/routes.md](./contracts/routes.md). One ordering trap: `/done`
must be tested before the bare order route, because the order route is a prefix of it.

The component also needs a visual change to match the approved design — completed stages
show a checkmark rather than their number, while the current stage keeps its number. The
existing `isDone`/`isCurrent`/`isReached` logic already distinguishes these; only the
rendered glyph changes.

**Alternatives considered**:

- *A `BookingStageProvider` context each page writes to.* Rejected — reintroduces the
  drift FR-005 exists to prevent, and needs a client boundary in every page.
- *Keep the rail per-page.* Rejected — it is the defect being fixed.

---

## R-005: Where does the journey end, and how do guests get there?

**Decision**: A new route `/events/:slug/orders/:orderNumber/done`. The order screen
`router.replace`s to it whenever the order's status is settled — `PAID`, `EXPIRED`, or
`CANCELLED`.

**Rationale**: FR-018 has to cover two situations that look different but are one rule:
an order that settles while the guest watches (the existing 3-second poll observes it) and
an order that was already settled when the link was opened. Forwarding on settled status
handles both with a single condition, and `isFinalOrderStatus()` already exists in
`frontend/lib/queries.ts` to express it.

`replace` rather than `push` so the back button does not return the guest to a payment
screen that immediately forwards again.

Placing `done` under the order (rather than at `/events/:slug/done`) keeps the receipt
addressable per order — a guest with two orders for the same event gets two distinct
confirmations, and the ownership guard applies unchanged.

`PaymentStatusCard` survives: it moves from the order screen to the confirmation screen,
where the expired/cancelled treatment it already renders becomes FR-020's outcome display.

**Alternatives considered**:

- *Keep the paid state in place on the order screen (clarification option B).* Rejected by
  the user's answer — it leaves a bookmarked order link showing a screen that duplicates
  the confirmation, and the rail's fourth stage unreachable.
- *`/events/:slug/done?order=…`.* Rejected — a query parameter for something that is part
  of the resource's identity.

---

## R-006: How does a guest resend their own ticket email?

**Decision**: A new public endpoint `POST /api/v1/orders/:orderNumber/resend-email`,
rate-limited to 1 per order per 60 seconds, returning a uniform `202` regardless of whether
the order exists. Full contract: [contracts/resend-email.md](./contracts/resend-email.md).

**Rationale**: This is the only genuine backend work in the feature. The approved design
puts a "Resend Email" button on the confirmation screen, but the only resend that exists is
`POST /api/v1/admin/orders/:id/resend-email` — JWT-guarded and keyed by order **UUID**,
neither of which a guest has.

Three constraints shape it:

- **No caller-supplied destination** (FR-024). The endpoint takes only the order number;
  the address comes from the stored order. Otherwise it is an open mail relay.
- **Rate limit keyed on the order, not the caller** (FR-025). The protected resource is the
  buyer's inbox. A guest's IP is not a reliable identity — mobile carriers and shared NAT
  make per-IP limits both leaky and unfair — while per-order limiting caps the damage
  regardless of how many callers participate.
- **Uniform response** (FR-026). A `404` would let anyone probe which order numbers exist.
  A flat `202` closes that; the cost is that a mistyped number gets no correction, which is
  acceptable because the button is only reached from the guest's own confirmation screen
  where the number is already filled in.

Domain placement is settled by Principle II: `internal/notification` owns email delivery
and already exposes the admin resend, so the public route joins it. It needs an order UUID
from an order number, so it extends its **own** `OrderProvider` interface with
`OrderIDByNumber`, and the composition root satisfies it over the existing
`order.Repository.GetOrderByNumber` (`internal/order/repository.go:293`) — the same shape
as the `OrderForDelivery` adapter already in `cmd/api/adapters.go:179`. No new SQL.

**Alternatives considered**:

- *Reuse the admin endpoint with a relaxed guard.* Rejected — it discloses the recipient
  address in `sent_to` and returns 404 for unknown orders, both wrong for an unauthenticated
  caller, and weakening it would degrade the admin contract.
- *Per-IP rate limiting.* Rejected as above.
- *A signed resend token embedded in the confirmation URL.* Rejected — it introduces a
  credential concept into a deliberately guest-first, credential-free flow (Principle VI)
  to protect an action whose worst case is re-sending a guest their own email.
- *No rate limit.* Rejected — an unauthenticated endpoint that sends mail is a mail-bomb
  vector against the buyer.

---

## R-007: Two countdowns on the order screen

**Decision**: Both render; each gets an explicit, non-positional label.

- `SalesCountdown`'s heading changes from *"The event will take place on"* to *"Event
  starts in"*. The current wording introduces a date, not a duration, and does not satisfy
  FR-017's requirement to name what the number counts down to.
- `ExpiryCountdown` currently reads *"This code expires in 4:59"*; it gains the payment
  framing — *"Pay within"*.
- Distinction is not left to labels alone: `SalesCountdown` is a full-width bar of
  brand-coloured unit blocks with DAYS/HOURS/MIN/SEC captions; `ExpiryCountdown` is inline
  body text inside the "Scan to pay" card that turns `text-destructive` under 60 seconds.
  Different placement, form, and colour under pressure.

**Rationale**: The user chose uniform framing on every screen over per-screen suppression,
which keeps `EventFrame` free of order-state awareness. The cost is both timers on the
highest-stakes screen, so FR-017 and SC-008 exist to make the misread implausible.

Note that this only affects the *payment* screen. On the confirmation screen the payment
timer is gone by definition, so the event countdown stands alone.

---

## R-008: Finishing the half-migrated checkout route

**Decision**: Move the slug from the query string to the route segment, and point
`encodeSelection()` at the scoped path.

**Rationale**: A live inconsistency, not a hypothetical.
`frontend/app/(public)/events/[slug]/checkout/page.tsx` sits under a `[slug]` segment but
reads `useSearchParams().get("slug")` at line 40, and `encodeSelection()` in
`frontend/lib/selection.ts:236` still returns `/checkout?${params}`. The old `/checkout`
page is deleted, so the selection link is already broken.

Three coordinated edits:

1. `encodeSelection(slug, lines)` → `/events/${slug}/checkout?${ticketAndPackageParams}`,
   dropping `slug` from the query string.
2. The checkout page takes `params: Promise<{ slug: string }>`; `useSearchParams` keeps
   carrying only the `t=`/`p=` pairs.
3. The post-checkout redirect at line 106 changes to
   `/events/${slug}/orders/${order_number}` (FR-012).

`parseSelection()` is unaffected — it reads only `t=` and `p=`.

**Alternatives considered**: keeping `?slug=` and ignoring the segment (rejected — two
sources of truth, and the layout's `params` would drive the countdown while the page used
the query string, so a mismatched URL would show one event's framing over another event's
checkout).

---

## R-009: What guards ownership on the order screens?

**Decision**: Compare `data.event.slug` against the route segment inside both the order
screen and the confirmation screen, rendering a not-found treatment on mismatch. Fail
closed — never redirect to the owning event.

**Rationale**: `PublicOrderDetail` already carries `event: { name, slug }`, so this is a
two-line comparison with no backend change. Silently forwarding would turn a wrong-event
URL into a working one and defeat the isolation the feature exists to create; the spec's
edge case says so directly.

**Alternatives considered**: a server-side `GET /events/:slug/orders/:number` (rejected — a
new endpoint for a check the client can already make, and it would fragment the order API
the polling hook depends on).

---

## Withdrawn decisions

The 2026-08-04 clarification removed scope. These decisions from the previous revision no
longer apply:

| Withdrawn | Was | Why gone |
|---|---|---|
| `event_slug` on `GET /api/v1/tickets/:code` | The one backend change; needed for a ticket ownership guard and a code resolver | Both consumers were deleted. `internal/ticket/` is now untouched entirely. |
| Event-scoped ticket screens | `/events/:slug/tickets` and `/events/:slug/tickets/:code` | A guest with a code should not have to identify an event first (FR-027). |
| Top-level ticket-code resolver | `/tickets` would resolve a code then forward into its event | Nothing to forward to. `/tickets` stays exactly as it is today. |
| `/orders/:n` legacy forwarder | Would look up the order and forward into its event | Dropped by decision (FR-028). Emails carry the PDF, not a link — verified by grepping `backend/internal/notification/` — so no sent mail is stranded. |

---

## Resolved decisions summary

| # | Question | Resolution |
|---|---|---|
| R-001 | Framing persistence | Server `layout.tsx` + client `EventFrame`; App Router keeps layouts mounted |
| R-002 | Fetch hoisting vs. live inventory | Query-key dedup ≠ caching; `staleTime: 0` unchanged |
| R-003 | Loading / unknown-event states | No countdown while pending; rail renders anyway; children suppressed only on not-found |
| R-004 | Rail stage source | Derived from `usePathname()`; fixes a live defect where stages 2–4 were never shown |
| R-005 | End of journey | `/events/:slug/orders/:n/done`; any settled status `replace`s to it |
| R-006 | Guest resend | **New endpoint**, per-order rate limit, uniform 202; lives in `internal/notification` |
| R-007 | Two countdowns | Both shown, relabelled, distinct in placement, form, and colour |
| R-008 | Half-migrated checkout | Slug moves to the route segment; `encodeSelection` and the redirect follow |
| R-009 | Ownership guard | Client-side slug comparison on both order routes, failing closed |

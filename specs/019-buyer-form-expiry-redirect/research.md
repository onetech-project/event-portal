# Phase 0 Research: Bundle Form Labels & End-of-Journey Destination

**Feature**: `019-buyer-form-expiry-redirect` | **Date**: 2026-08-18

The Technical Context carried no `NEEDS CLARIFICATION`: this is a frontend change inside a
subsystem the repository already documents in detail. What follows is the load-bearing
verification behind that claim — every question whose wrong answer would have changed the
plan, together with what was actually read to settle it.

---

## R1 — Where the visitor number lives, and whether anything else depends on it

**Decision**: delete `SlotGroup.unitLabel` and the second pass in `groupOrderSlots` that
computes it, then remove the two places `GroupTitle` renders it.

**Rationale**: the number is produced in exactly one place —
`frontend/components/order/slot-groups.ts:87-95`, a post-pass that sets
`unitLabel = "Visitor <n>"` only when one package contributed more than one purchased unit
— and consumed in exactly two, both inside `GroupTitle` in
`frontend/components/order/visitor-form.tsx`: the `fullTitle` join at line 399 (which feeds
the truncation tooltip) and the rendered `<span>` at lines 425-428. A repository-wide search
for `unitLabel` outside `slot-groups.*` returns only those two sites, and `e2e/` never
references the label at all — `GuestJourney.fillHolder` locates fields by `getByLabel(...)
.nth(index)` within the `Visitor registration` form, so card *headings* play no part in how
the acceptance suite fills them. Deleting the field therefore cannot reach any other
surface, and the `unitsPerPackage` bookkeeping that exists only to feed it goes with it.

**Alternatives considered**:

- *Keep `unitLabel` computed, stop rendering it.* Rejected: it leaves a field whose only
  purpose is to be ignored, and the next reader has to prove the omission is deliberate. The
  spec's own assumption — "only the label drawn from it goes" — is about the *grouping*
  surviving, and the grouping is `key`/`slotIds`/`packageUnit`, none of which is touched.
- *Replace the number with something else (an initial, the ticket names).* Rejected: out of
  scope, and it re-introduces the thing the request removed under a new name.

---

## R2 — Whether the end-of-journey dialog can reach the event slug

**Decision**: add an `eventSlug` prop to `EndOfJourneyDialog` and pass the route's
`eventSlug` from both call sites.

**Rationale**: the dialog is rendered at exactly two places —
`app/(public)/events/[slug]/orders/[orderNumber]/page.tsx:124` and
`.../checkout/page.tsx:243` — and both are inside components (`OrderView`, `CheckoutView`)
whose signature already carries `eventSlug`. No fetching, no context, and no prop drilling
through intermediate components is required; the value is one identifier away at both sites.

**Which slug**: the route's `eventSlug`, not `data.event.slug`. The two are provably equal
at the point the dialog renders, because both screens return `<WrongEvent>` earlier
(`page.tsx:94`, `checkout/page.tsx:126`) whenever `data.event.slug !== eventSlug`, so the
dialog is unreachable while they differ. Using the route param keeps the dialog consistent
with its siblings on the same screens (`orderDonePath(eventSlug, …)`,
`orderCheckoutPath(eventSlug, …)`), which all take the route param. Spec FR-008's "derived
from the order's own event" is satisfied either way, and this records *why* rather than
leaving the equivalence implicit.

**Alternatives considered**:

- *Pass a ready-made `href` (and label) from each screen.* Rejected: it lets the two screens
  drift to different destinations, which is exactly what FR-008 forbids. One prop with the
  path built inside the dialog makes divergence impossible rather than merely unlikely.
- *Read the slug from `usePathname()` inside the dialog.* Rejected: it re-derives, by regex,
  a value the caller already holds — the drift `lib/order-routes.ts` was written to prevent.

---

## R3 — How an ended order is arranged in `e2e/` without a back-door write

**Decision**: use two different, already-supported arrangements — the signed gateway
notification for the payment screen, and the real booking hold lapsing for the forms screen.

**Rationale**: Principle VIII forbids writing order status directly into the database, so
both arrangements had to be shown to exist before the coverage plan could be written.

- *Payment screen.* `expireOrder(orderNumber)` in `e2e/support/payment.ts:117` delivers a
  genuine `expire` notification to the production callback handler, authenticated with the
  callback token. It is the real provider path, already used by the existing scenario at
  `guest-purchase.spec.ts:257`. It requires the order to have started payment, which is
  precisely why it cannot cover the forms screen.
- *Forms screen.* The booking hold and the payment window share one column. `SCHEMA.md:130`
  records it plainly: booking sets `payment_expires_at = now() + BOOKING_HOLD`, and checkout
  later *overwrites* it with the gateway's deadline. The sweeper's query
  (`ListOrdersDueForExpiry`) selects any PENDING order with `payment_expires_at <= now()`,
  so it expires an untouched booking hold exactly as it expires an abandoned payment. In the
  suite, `e2e/playwright.config.ts:115-116` already sets `BOOKING_HOLD` to ~30s (scaled) and
  `PAYMENT_SWEEP_INTERVAL` to 3s, so a booked order that is left alone becomes EXPIRED
  within roughly 33 seconds, and the forms page's 3-second status fallback surfaces it
  within one more tick.

This was the single assumption most likely to be wrong — a sweeper keyed on a
payment-only column would have made the forms-screen expiry unreachable without a forbidden
write — so the SQL was read rather than inferred from the function name.

**Cost, stated rather than hidden**: the new forms-screen scenario waits out a real hold and
adds ~35s to a suite run. It fits inside the configured `timeout: scaled(90_000)`. The wait
MUST be derived from the same scaling the config applies (`E2E_SLOW_MO` grows `BOOKING_HOLD`
deliberately, so a watched run is not failed by a hold that expired mid-click); a hardcoded
30-second wait would pass headless and hang headed.

**Alternatives considered**:

- *Write `status_id` or `payment_expires_at` straight to Postgres.* Rejected outright:
  Principle VIII's "no shortcuts through the back door" names this exact write, and it would
  also skip the cache invalidation that makes the arrangement honest.
- *Cover the forms screen only in Vitest.* Rejected. It would leave the suite green because
  it never looked at a flow Principle VIII names explicitly ("hold expiry"), which that
  principle calls out as not being verification.
- *Lower `BOOKING_HOLD` for the run.* Rejected: it is one process-wide env for the whole
  suite, and shortening it would expire other scenarios' holds mid-journey.

---

## R4 — Whether the multi-unit bundle form is reachable in `e2e/` today

**Decision**: it is not, and a scenario is added.

**Rationale**: `guest-purchase.spec.ts` books a package exactly once, at line 412, with
`selectQuantity("Day 1 & 2 Bundle", 1)`. One unit never produced a visitor number even
before this change — the label appears only when a package contributes more than one unit
(R1). The two-unit holder form is therefore assembled nowhere but jsdom
(`page.test.tsx:920-960`). Since Track B changes what a guest sees on the holder forms, and
the holder forms are inside Principle VIII's covered guest journey, the assembled-system
proof has to exist. `createPackage` and `selectQuantity(name, n)` already support it, so the
scenario is arrangement the suite can already express.

**Alternatives considered**:

- *Rely on the Vitest coverage alone.* Rejected for the reason above, and because the
  fan-out this scenario also proves — one card's values reaching every ticket in its unit —
  has only ever been asserted against a stubbed `fetch`.

---

## R5 — Whether the change can introduce an automatic navigation by accident

**Decision**: no `router` call is added; the action stays a plain `Link`.

**Rationale**: the clarified requirement (FR-005) is that nothing moves the guest without a
press, and both screens already contain `router.replace(...)` effects for their *state*
forwards (paid → confirmation, started → checkout). The risk is not theoretical: a
well-meaning implementation could add `EXPIRED` to those effects and satisfy a naive reading
of the original request. Two existing guards keep the distinction visible and MUST be left
intact — `page.test.tsx:174` and `checkout/page.test.tsx:274` both assert
`expect(replace).not.toHaveBeenCalled()` for an expired order. They are the regression
barrier for FR-005 and are not modified by this feature.

**Alternatives considered**: none. An automatic redirect is the design the clarification
explicitly rejected.

---

## R6 — What the label becomes

**Decision**: `Return to Event Page`.

**Rationale**: FR-004 requires only that the label name its destination. This wording keeps
the existing sentence shape ("Return to …") and swaps the noun, so the diff is a substitution
rather than a rewrite, and the accessible-name assertions in both page tests change in one
predictable way. The spec records it as an assumption, not a constraint.

**Alternatives considered**: "Back to Event" (shorter, but breaks the established phrasing on
a Figma-specified card); "Return to Event" (ambiguous — the guest is going to a page, not to
the event itself); "Browse Tickets" (implies the selection step, which is the destination
FR-009 explicitly does *not* choose).

---

## R7 — Whether any governance document records the current destination

**Decision**: none does; no governance sync is required.

**Rationale**: `PRD.md` and `ARCHITECTURE.md` were searched for the dialog, its heading, and
its destination and contain no mention of any of them. `SCHEMA.md` is untouched because
there is no migration. The only binding text that fixes the destination is spec 011's
FR-024, which this feature's spec amends explicitly under **Amends**. Recorded because
"no documents need updating" is a claim worth having been checked rather than assumed —
the constitution requires governance documents to move with the behaviour they describe.

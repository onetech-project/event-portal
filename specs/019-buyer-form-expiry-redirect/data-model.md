# Phase 1 Data Model: Bundle Form Labels & End-of-Journey Destination

**Feature**: `019-buyer-form-expiry-redirect` | **Date**: 2026-08-18

## Persistent data: no change

There is no migration, no column, no table, and no index in this feature. `SCHEMA.md` is
not edited, and the rule that a schema change and its migration land in the same commit is
not engaged because neither exists.

This is stated rather than omitted so a reviewer can tell the difference between "no schema
change" and "the schema section was forgotten". Both tracks read data the screens already
have and write nothing:

- Track A changes where one link points. It reads `eventSlug`, already a route parameter.
- Track B stops rendering a string that was computed in the browser from data already on
  the order response.

Consequently: seat release, quota restoration, order status transitions, and payment
records all behave exactly as they do today (spec FR-010), and no cache key is read,
written, or invalidated.

## Wire contracts: no change

`api/openapi.yml` is untouched. `TicketOrderDetail` and its nested `TicketOrderSlot` are
consumed exactly as they are today — in particular `package_id` and `package_unit`, which
are what group a bundle's slots into one card per purchased unit, keep both their meaning
and their use. What is removed is a *label derived from* `package_unit` in the browser, not
`package_unit` itself.

## Client-side shapes that do change

Two internal TypeScript shapes move. Neither crosses a process boundary; both are covered
by the contracts note in [`contracts/README.md`](./contracts/README.md).

### `SlotGroup` — `frontend/components/order/slot-groups.ts`

One holder card's worth of slots. A bundle unit's slots collapse into one group; a
standalone ticket is its own group.

| Field | Change | Notes |
|---|---|---|
| `key` | unchanged | Stable card key: slot id for solo groups, `packageId:unit` for bundle units. |
| `slotIds` | unchanged | Every slot this card's single form fills. |
| `title` | unchanged | Bundle name for bundle groups, ticket-type name otherwise. |
| `ticketCount` | unchanged | Drives the "N tickets" badge, which FR-012 keeps. |
| `isBundle` | unchanged | |
| `packageId` | unchanged | |
| `packageUnit` | unchanged | **Retained.** It is what separates unit 1's slots from unit 2's; only the label built from it goes. |
| `packageBadge` | unchanged | Pre-010 bundle slots still badge their package name. |
| `unitLabel` | **removed** | Was `"Visitor <n>"` when a package contributed more than one unit; `null` otherwise. FR-011. |

**Derivation removed**: the second pass over `groups` that counted units per package and
assigned `unitLabel`, together with the `unitsPerPackage` map that existed only to feed it.
The grouping itself — the first pass, which builds one group per `(package_id,
package_unit)` — is untouched, so FR-014's guarantee that card count, card contents, and
card order are unchanged holds by construction rather than by test alone.

**Validation rules**: none. `SlotGroup` is a rendering shape with no validity constraints;
the Zod schemas that validate holder input (`visitorSchema`, `formsSchema` in
`visitor-form.tsx`) key off `slot_ids` and are not touched.

### `EndOfJourneyDialog` props — `frontend/components/order/end-of-journey-dialog.tsx`

| Prop | Change | Notes |
|---|---|---|
| `status` | unchanged | `"EXPIRED" \| "CANCELLED"` — still the narrowed union, still the only thing that varies heading and body copy. |
| `eventSlug` | **added** (`string`, required) | The event whose detail page the single action leads to. Required, not optional: an optional slug would silently fall back to some default destination at a call site that forgot it, which is the drift FR-008 forbids. |

The component's own output changes in exactly two ways — the action's `href`
(`/` → `/events/{slug}`, built via `eventDetailPath`) and its text
(`"Return to Home Page"` → `"Return to Event Page"`). Everything else the component
encodes — the unclosable dialog's three separate escape hatches being closed off, the
`modal` default that makes the page behind inert, the single-action shape — is unchanged
and is the subject of FR-001, FR-002, and FR-009.

## State transitions

None are introduced or altered. The order lifecycle (PENDING → PAID / EXPIRED / CANCELLED)
is server-owned and untouched. The one client-side latch involved — `expiredDeadline` on
the payment screen, which remembers *which* deadline ran out so the dialog opens at
countdown zero rather than one poll later — keeps its existing behaviour, because FR-001
preserves the promptness requirement it implements.

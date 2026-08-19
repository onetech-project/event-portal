# Contracts: Bundle Form Labels & End-of-Journey Destination

**Feature**: `019-buyer-form-expiry-redirect` | **Date**: 2026-08-18

## External contracts: none change

`api/openapi.yml` is not edited. No endpoint is added, removed, or altered; no request or
response shape moves; no status code changes. The feature is confined to how the browser
renders data it already receives, and to where one link points.

This file exists to record that as a verified outcome rather than an omission — the plan
template asks for contracts, and "there are none" is the answer for a frontend-only change
that adds no surface.

## Internal UI contracts that change

Two, both inside `frontend/`, both compile-time enforced by TypeScript. They are listed
here because they are the interfaces this feature's callers must satisfy, and because a
reviewer looking for "what did the shapes become" should find one answer, not three.

### 1. `EndOfJourneyDialog`

```ts
// frontend/components/order/end-of-journey-dialog.tsx
export function EndOfJourneyDialog(props: {
  status: "EXPIRED" | "CANCELLED";
  eventSlug: string;                    // NEW — required
}): JSX.Element
```

**Obligations on the component**

- Renders exactly ONE action (FR-002, FR-009). It is a link, never a button that navigates
  programmatically (FR-005).
- That action's destination is `eventDetailPath(eventSlug)` and nothing else (FR-003), for
  both values of `status` (FR-006).
- That action's text names the event page (FR-004).
- Every unclosability guarantee of spec 011 FR-023 is preserved: no close control, no
  Escape dismissal, no backdrop dismissal, page behind inert.

**Obligations on callers**

- Both call sites MUST pass the same kind of slug — the route's `eventSlug` — so the two
  screens cannot resolve different destinations (FR-008). `eventSlug` is required rather
  than optional precisely so a caller cannot omit it and inherit a fallback.

**Callers**: `app/(public)/events/[slug]/orders/[orderNumber]/page.tsx` and
`app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.tsx`. There are no others.

### 2. `eventDetailPath`

```ts
// frontend/lib/order-routes.ts
export function eventDetailPath(eventSlug: string): string  // "/events/{encoded slug}"
```

Joins `orderFormsPath`, `orderCheckoutPath`, and `orderDonePath` in the module that already
owns every address an order occupies. It encodes its argument, matching its three siblings.

It belongs here rather than inside the dialog for the reason that module's own header
states: a path built by hand in one place and matched by a regex in another is how an
address drifts from the thing that recognises it. `bookingStageFromPathname` matches
`/events/{slug}` shapes, and `EventFrame` treats the bare event path as the one screen
without a progress rail — so the detail path already has more than one reader.

### Unchanged, and deliberately so

`SlotGroup` loses a field (see [data-model.md](../data-model.md)), but it is an internal
rendering shape with a single producer and a single consumer inside one directory, not an
interface between parts. It is recorded there rather than here.

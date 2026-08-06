# Contract: Order Read & Checkout Forms with Bundle Units

**Feature**: [spec 010](../spec.md) | Extends [spec 008 contracts/api.md](../../008-e2e-purchase-flow/contracts/api.md) (calls 7–8) and [booking-flow.md §3](../../008-e2e-purchase-flow/contracts/booking-flow.md). Only deltas are documented here; everything not mentioned is unchanged.

## 1. `GET /api/v1/ticket/order/:order_id` — slot shape (additive)

Each element of `data.slots` gains two nullable fields:

```jsonc
{
  "id": "b7b1…",                    // unchanged — the slot id checkout fills
  "ticket_type_name": "Day 1 Pass", // unchanged
  "package_name": "2-Day Bundle",   // unchanged; null for standalone slots
  "package_id": "9f2c…",            // NEW: null for standalone slots
  "package_unit": 1,                // NEW: bundle-unit ordinal (1-based); null for
                                    //      standalone slots AND for bundle slots
                                    //      booked before migration 000011
  "name": null, "email": null, "phone": null, "dob": null, "gender": null
}
```

Guarantees:

- `package_unit != null` ⇒ `package_id != null`.
- For a package order line of quantity Q, the order's slots for that package carry units exactly `1..Q`, each unit repeated per the package's per-unit composition.
- Slot ordering is deterministic: standalone slots first, then bundle slots grouped contiguously by `(package_id, package_unit)`.

**Client grouping rule (normative for any UI):** render one visitor form per group, where a group is — a standalone slot by itself; a bundle slot with `package_unit == null` by itself (legacy fallback, pre-010 rendering); otherwise all slots sharing `(package_id, package_unit)`. Bundle groups are titled with `package_name` (never a constituent ticket-type name) and show the group's slot count.

## 2. `POST /api/v1/ticket/checkout/:order_id` — wire shape UNCHANGED, one new rejection

The request body stays exactly spec 008 call 8: `buyer_*` plus `attendees: [{id, name, email, phone, dob, gender}]` with **one element per slot**. Clients implementing the single-bundle-form UX duplicate each bundle form's values into one element per slot of that unit (distinct `id`s, identical other fields).

New validation (after slot matching, before anything is written):

- **Same-unit consistency**: all `attendees[]` elements whose slot ids share a `(package_id, package_unit)` group must be identical in `name`, `email`, `phone`, `dob`, `gender`. Slots with `package_unit = null` are exempt (legacy orders).
- Failure: HTTP 400, envelope code `400001`, `data` field-map keyed by the divergent payload element, e.g.:

```jsonc
{ "code": 400001, "message": "The visitor forms do not match the order's tickets.",
  "data": { "attendees[2].email": "All tickets in the same bundle must use the same visitor information." } }
```

All existing rejections (coverage mismatch `attendees`, foreign slot `attendees[i].id`, field validation, 410/409 order states) are unchanged.

## 3. Compatibility matrix

| Client | Server pre-010 | Server post-010 |
|---|---|---|
| Pre-010 UI (one form per slot) | today | works — per-slot forms naturally satisfy consistency only if the guest types identical data; **but** pre-010 UI never groups, so divergent data on a bundle is now rejected with the 400001 above. Deploy backend + frontend together (single release train) to avoid exposing this window. |
| Post-010 UI (grouped forms, fan-out) | works — server ignores the two unknown-to-it… (fields simply absent pre-migration); grouping falls back to per-slot because `package_unit` is absent/null | target state |

The practical rule: ship migration + backend + frontend in one release (this repo deploys as one unit), and legacy in-flight orders keep working through the `package_unit = null` fallback on both sides.

## 4. Unchanged surfaces (asserted, not modified)

- Ticket issuance: one ticket per attendee row on PAID — bundle unit of N slots still yields N tickets/QRs (spec FR-004).
- `POST /ticket/book`, quota deduction, fees, payment session, webhook, PDF/email flows.
- Admin validation: per-ticket-code lookup and mark-used, regardless of duplicated visitor data.

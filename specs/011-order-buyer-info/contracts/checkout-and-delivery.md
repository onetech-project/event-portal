# Contract: Checkout Without Buyer Block & Per-Holder Delivery

**Feature**: [spec 011](../spec.md) | Extends [spec 008 contracts/api.md](../../008-e2e-purchase-flow/contracts/api.md) (calls 8, 10, supporting reads) and [spec 010 contracts/order-forms.md](../../010-bundle-single-form/contracts/order-forms.md). Only deltas are documented; everything not mentioned is unchanged (envelope, snake_case, error-code registry, rate limits, routing).

## 1. `POST /api/v1/ticket/checkout/:order_id` — buyer fields removed, phone rule added

> Phone rule clarified 2026-08-07 and narrowed 2026-08-13: **12-15 digits**,
> nothing else, **verbatim**. The field carries no country code of its own — the
> guest types the whole number in either `62…` or `08…` form and that form is
> what is stored and sent on. No normalisation, no `+`. The shape has never
> changed across the three cuts (digits-only 10-12 → 10-15 → 12-15): it is one
> length check with no prefix branch, and the 2026-08-13 clarification kept it
> that way deliberately. Because the floor is counted on the value as typed, an
> 11-digit local number (`08123456789`) is refused while the same number in
> `62…` form (`628123456789`, 12 digits) is accepted.
>
> **At rest, nothing is re-validated.** Rows written under either earlier rule
> stay valid permanently; `attendees.phone` keeps `VARCHAR(50)` with no CHECK,
> and no migration or sweep accompanies this change (research R29).

### Request (breaking — single release train, same pattern as spec 010)

```jsonc
{
  // buyer_name / buyer_email / buyer_phone / buyer_dob / buyer_gender — REMOVED.
  // Unknown fields sent by stale clients are ignored by binding (not rejected).
  "attendees": [ { "id": "<slot uuid>",           // one element per slot, unchanged
                   "name": "…", "email": "…",
                   "phone": "081234567890",        // NEW RULE: ^[0-9]{12,15}$
                   "dob": "2000-01-31",
                   "gender": "FEMALE" } ]          // still the NAME from GET /ticket/genders
}
```

Validation deltas (all reported in the existing `400001` field map):

| Field | Rule | Message |
|---|---|---|
| `attendees[i].phone` | `^[0-9]{12,15}$` — length only, no prefix required, stored verbatim (was: non-empty, then digits-only 10-12) | "Enter a phone number of 12-15 digits." (same string as the client-side error) |
| `buyer_*` | no longer validated (fields gone) | — |

Unchanged: slot coverage (`matchVisitorsToSlots`), bundle-unit consistency (spec 010 `400001` map), name/email/dob rules, gender **name** membership in the active master list, order-state guards (`409003`/`409004`/`410001`), response body, `502001` semantics.

**Storage note (not wire-visible)**: the accepted gender name is persisted as `attendees.gender_id` (FK to `genders(id)`, migration 000012); order reads keep returning the name via JOIN. `orders.buyer_dob`/`buyer_gender` no longer exist; checkout's TX-D writes only `buyer_name`/`buyer_email`/`buyer_phone`.

### Primary-contact derivation (server-side, normative)

After slot matching, the **primary contact** is the visitor mapped to the **first slot in canonical slot order** — the same `ORDER BY package_id NULLS FIRST, package_unit ASC, id ASC` that drives form rendering (spec 010 contract §1; note this puts standalone slots before bundle slots, so in a mixed order the topmost rendered form is a standalone form) — **not** `attendees[0]` of the request array. The server writes the contact's name/email/phone to `orders.buyer_*` in the checkout transaction and feeds the same three values to the gateway's `CustomerName/Email/Phone`. Clients cannot influence it except by editing the topmost form.

## 2. `GET /api/v1/ticket/order/:order_id` — buyer fields dropped (breaking, read side)

`data.buyer_name` and `data.buyer_email` are **removed** from `TicketOrderDetail`. No other field changes; `slots[]` (still carrying `gender` as a name, now joined from the master list), `items[]` (`unit_price` already present), `subtotal`, `fees[]`, `total_amount`, `payment` are unchanged. The order summary's Booking ID / unit-price / breakdown rendering consumes only existing fields.

**Client migration note**: besides `lib/types.ts`, two guest files consume `buyer_email` from this endpoint on a runtime-unreachable branch and are updated in the same change — `done/page.tsx:102` and `payment-status-card.tsx` (prop dropped; PAID copy reworded to point at the first holder form's address).

## 3. Post-payment delivery — one email to the buyer (behavioral, no endpoint change)

On PAID (webhook `settlement`/`capture+accept`, or reconcile):

- Recipient = `orders.buyer_email`, the primary-contact snapshot written at checkout from the first canonical slot's form — form 1, which is ticket holder 1 and the buyer at once. The other `attendees.email` values are holder identity and are never mailed. An empty snapshot is an error (the order never completed checkout): nothing is sent and `email_sent` stays FALSE.
- Exactly **one email**: attachment = `tickets-{order_number}.pdf` containing **every ticket page in the order**; body = the full order receipt (order `Inv: #`, every line item with the fee breakdown, Total Payment) plus every holder's name and one e-ticket card per ticket.
- `orders.email_sent` becomes TRUE only after that send succeeds; a failure leaves it FALSE with resend re-armed.
- Idempotency unchanged: replayed webhooks short-circuit on `status == PAID` before any delivery work.

> Revision note: an earlier draft of this contract specified a per-holder fan-out (one email per distinct holder address, each with a PDF subset). It was implemented and then reversed by the 2026-08-06 clarification; constitution v3.0.0 is the governing rule.

## 4. Resend endpoints — same single delivery

- `POST /api/v1/ticket/resend-email` (guest): request/response unchanged (uniform 202 anti-enumeration body). Behavior: repeats the same single delivery to the buyer.
- `POST /api/v1/admin/orders/:id/resend-email` (admin, JWT): response `data.sent_to` stays a **`string`** — the buyer's address, sourced from `SendTicketEmail`'s return value. No shape change from pre-011.

## 5. Compatibility

| Surface | Break? | Mitigation |
|---|---|---|
| Checkout request | Yes (server ignores stale `buyer_*`, requires new phone rule) | Frontend + backend ship together (single release train; repo deploys as one unit) |
| Order read | Yes (two fields removed) | No reachable guest render path uses them; the two type-level consumers (`done/page.tsx`, `payment-status-card.tsx`) are updated in the same change |
| Admin resend response | No (`sent_to` stays a string) | — |
| Guest resend, webhook, SSE, QR endpoints, `GET /ticket/genders` | No | — |
| Legacy orders (checked out pre-011: buyer data present, phones 7-20 digits, gender backfilled to `gender_id`) | No | Stored data untouched except the total gender backfill; delivery reads `orders.buyer_email`; resend works |

## 6. Unchanged surfaces (asserted, not modified)

- `POST /ticket/book`, quota deduction, fee freezing, terms agreement, SSE status stream, QR image/refresh endpoints, sweeper.
- Ticket issuance: one ticket per attendee row; codes/QR unchanged; admin validation (one-scan-one-use) unchanged.
- Gender master list endpoint (`GET /ticket/genders`) and its admin CRUD; membership validation still by name.

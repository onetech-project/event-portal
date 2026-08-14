# Phase 1 API Contract: Payment External Reference ID

**Feature**: [../spec.md](../spec.md) | **Plan**: [../plan.md](../plan.md) | **Data model**: [../data-model.md](../data-model.md)

Two response shapes gain one field each. No new route, no removed field, no changed type, no
changed status code. Both changes are backward compatible: an existing client that ignores
unknown fields is unaffected.

---

## 1. `POST /api/v1/ticket/checkout/{order_id}`

Unchanged in every respect except one added response field.

### Success — `200 OK`

```jsonc
{
  "success": true,
  "data": {
    "order_id": "ORD-20260814-0007",
    "qr_string": "00020101021226620015ID.CO.MANJO.WWW…6304A7C0",
    "expires_at": "2026-08-14T02:43:09Z",
    "qr_image_url": "/api/v1/ticket/order/ORD-20260814-0007/qr.png",
    "ext_ref_id": "A487336098162400838C"    // NEW
  }
}
```

| Field | Type | Notes |
|-------|------|-------|
| `ext_ref_id` | `string` | The gateway's own reference for this payment session. **Always present**, empty string when no reference exists (spec FR-010) — a caller never has to distinguish an absent field from an absent reference. Opaque: no format is promised, and it must not be parsed. |

### `ext_ref_id` on every payload-bearing outcome (spec FR-011)

This is the part that is easy to get wrong, so it is stated per outcome:

| Outcome | Status | Where `ext_ref_id` comes from |
|---------|--------|-------------------------------|
| A session was opened for this call | `200` | The gateway's answer to this call |
| Payment already started for this order (retry) | `409` `PAYMENT_ALREADY_STARTED`, payload in `error.data` | Read back from the order's recorded session |
| This call lost the stamping race to a concurrent checkout | `200` | Read back — **not** this caller's own session, which is not the one the order holds |

The `409` body keeps its existing envelope; only the `data` payload it already carries gains
the field:

```jsonc
{
  "success": false,
  "error": {
    "code": 409004,
    "message": "Payment for this order has already started.",
    "data": {
      "order_id": "ORD-20260814-0007",
      "qr_string": "00020101021226620015ID.CO.MANJO.WWW…6304A7C0",
      "expires_at": "2026-08-14T02:43:09Z",
      "qr_image_url": "/api/v1/ticket/order/ORD-20260814-0007/qr.png",
      "ext_ref_id": "A487336098162400838C"    // NEW — same value the 200 returned
    }
  }
}
```

### Unchanged failure responses

`400` validation, `404` order not found, `409` terms-not-recorded, `409` session-duplicate,
`410` order expired, `502` payment-initiation-failed — all byte-identical to today (spec
FR-013). None of them carries a payment payload, so none gains the field.

### Consumer impact

The frontend's `CheckoutQRResponse` type gains the field for completeness, and **renders it
nowhere** (spec FR-020, FR-021). The guest payment screen is unchanged.

---

## 2. `GET /api/v1/admin/payment/order/{order_id}/notifications`

Admin-authenticated. Array shape, ordering, and every existing field unchanged.

```jsonc
{
  "success": true,
  "data": [
    {
      "id": "9f1c…",
      "provider": "manjo",
      "transaction_id": "A48593Completed",
      "status": "Completed",
      "is_marker": false,
      "payment_type": "qris",
      "raw_payload": { "ri": "ORD-20260814-0007", "nti": "A48593Completed", "s": 2 },
      "ext_ref_id": "",                          // NEW — empty on notification rows
      "received_at": "2026-08-14T02:44:11Z"
    },
    {
      "id": "3b7e…",
      "provider": "manjo",
      "transaction_id": "ORD-20260814-0007",
      "status": "SESSION_OPENED",                // NEW row kind
      "is_marker": true,
      "payment_type": "qris",
      "raw_payload": {
        "marker": "SESSION_OPENED",
        "raised_at": "2026-08-14T02:43:09Z",
        "ext_ref_id": "A487336098162400838C",
        "expires_at": "2026-08-14T02:43:09Z",
        "expiry_from_gateway": true
      },
      "ext_ref_id": "A487336098162400838C",      // NEW — the only row that carries one
      "received_at": "2026-08-14T02:43:09Z"
    }
  ]
}
```

| Field | Type | Notes |
|-------|------|-------|
| `ext_ref_id` | `string` | Empty on every row except the `SESSION_OPENED` marker. Always present, never null. |

### The new row

Orders checked out after this change carry one additional row, the `SESSION_OPENED` marker,
oldest in the list. It renders through the existing marker path — `is_marker: true`, the "our
note" badge — because it is this system's own statement, not something the gateway said.
Checkout-time markers are not new to this list: `SESSION_DUPLICATE` is already written from
the same path today.

Orders that predate this change carry no such row and no reference. The panel shows its
absent-value placeholder (spec FR-018).

### Consumer impact — the admin panel

`OrderPaymentPanel` derives **one order-level value** from the list it already fetches:

```ts
const extRefId = notifications.data?.find((n) => n.ext_ref_id)?.ext_ref_id ?? "";
```

Rendered as a single labelled line within the existing dialog — full value, selectable,
placeholder when empty (spec FR-014, FR-015, FR-017, FR-018). No new request, no new query
key, no new route, no layout reflow (spec FR-016, FR-019).

---

## 3. Surfaces explicitly NOT changed

Listed because "no change" is a requirement here, not an omission:

| Surface | Requirement |
|---------|-------------|
| `GET /ticket/order/{order_id}` (guest order read) | FR-020 — no reference, and it is unauthenticated |
| `GET /ticket/checkout/{order_id}/status` (SSE) | FR-021 — frame shape unchanged |
| `GET /admin/payment/order/{order_id}/holds` | FR-019 |
| Admin orders list read | FR-016, FR-019 — no new column |
| Receipt and e-ticket documents, and their email | FR-020 |
| Webhook callback endpoint | Untouched; the gateway never sends `eri` on a callback |

---

## 4. Documentation to update in the same change

[`api/openapi.yml`](../../../api/openapi.yml) is the complete HTTP surface and must stay so:

- `components.schemas.CheckoutResponse` — add `ext_ref_id` with the "always present, empty
  when unknown, opaque" description.
- `components.schemas.PaymentNotification` — add `ext_ref_id`, noting it is populated only on
  the `SESSION_OPENED` marker row.

No path entry, parameter, or status code changes.

# API Contracts: Persistent Admin Session & In-App QRIS Payment Page

**Feature**: `004-session-qris-payment` | Base path: `/api/v1`

---

## Relationship to PRD §1.5 (LOCKED API list)

| Endpoint | PRD §1.5 status |
|---|---|
| `POST /checkout` | Listed — request unchanged, response semantics clarified |
| `POST /payment/webhook/:provider` | Listed — unchanged |
| `POST /admin/login` | Listed — unchanged |
| `GET /orders/:orderNumber` | **Deliberate addition — justified below** |
| `GET /orders/:orderNumber/qris.png` | **Deliberate addition — justified below** |
| `POST /orders/:orderNumber/payment/refresh` | **Deliberate addition — justified below** |

PRD.md §1.5 was written for a flow in which the guest left the site for the
provider's hosted page, so it needs no guest-facing order endpoint. This feature
replaces that flow (spec FR-009), and the capability cannot be delivered without a
way for the guest's own page to read its order. The three additions are the
minimum set:

1. **`GET /orders/:orderNumber`** — the page's data source, and the target of the
   3-second poll that delivers FR-016's automatic update. No listed endpoint
   returns a guest-visible order.
2. **`GET /orders/:orderNumber/qris.png`** — keeps the QR image out of the polled
   JSON payload and keeps the guest's browser from calling the provider's host
   directly (research.md §3). Splitting it also makes it independently cacheable.
3. **`POST /orders/:orderNumber/payment/refresh`** — the "check payment status"
   control of FR-017. It is separate from the GET because it performs an outbound
   provider call and mutates order state, which must never happen on the endpoint
   polled every 3 seconds, and because a mutating GET is not cacheable or safe.

All three are guest-facing by necessity, are keyed only by order number, expose no
ticket codes or admin data (FR-022), and add no admin surface. The same rationale
is recorded in [plan.md](../plan.md) ("PRD §1.5 API Deviation") and
[research.md](../research.md).

---

## GET /orders/:orderNumber

Public, unauthenticated. The order page's data source, polled every 3 seconds
while the order is `PENDING` (FR-016) and not polled at all once it is final
(FR-020).

Must remain a cheap, read-only, indexed lookup: **no outbound provider call, no
writes.** `idx_orders_order_number` already exists.

**Response 200**

```json
{
  "order_number": "ORD-20260801-A1B2C3D4",
  "status": "PENDING",
  "total_amount": "550000.00",
  "buyer_name": "Siti Rahayu",
  "buyer_email": "siti@example.com",
  "created_at": "2026-08-01T10:00:00Z",
  "server_time": "2026-08-01T10:03:12Z",
  "event": { "name": "Jazz Night 2026", "slug": "jazz-night-2026" },
  "items": [
    {
      "ticket_type_name": "Regular",
      "quantity": 2,
      "unit_price": "275000.00",
      "subtotal": "550000.00"
    }
  ],
  "payment": {
    "method": "QRIS",
    "provider": "midtrans",
    "amount": "550000.00",
    "expires_at": "2026-08-01T10:15:00Z",
    "qr_image_path": "/api/v1/orders/ORD-20260801-A1B2C3D4/qris.png"
  }
}
```

**`payment` is `null`** when the order is not payable — `status` is `PAID`,
`CANCELLED`, or `EXPIRED`, or `payment_expires_at` has passed, or no payment
instruction was ever recorded (FR-014). Clients MUST render the QR and countdown
only when `payment` is non-null.

**`server_time`** is the server's clock at response time. The client computes
`offset = server_time − Date.now()` and renders the countdown as
`expires_at − (Date.now() + offset)`, so a wrong device clock cannot skew it
(FR-012, SC-005).

**Amounts** use the existing `money.Money` string encoding, as elsewhere in the
API — never floats.

**Response 404**: `{ "error_code": "NOT_FOUND", "message": "Order not found." }` —
identical for an order that never existed and one that exists but cannot be shown,
so the endpoint reveals nothing by comparison.

**Caching**: `Cache-Control: no-store` (live status, same rule the constitution
sets for quota reads).

---

## GET /orders/:orderNumber/qris.png

Public, unauthenticated. Renders `orders.payment_qr_string` to a PNG **on demand**
with `github.com/skip2/go-qrcode` — no file is stored, consistent with the
constitution's no-object-storage rule.

**Response 200**: `Content-Type: image/png`, a QR encoding of the stored QRIS
payload. Recommended size ≥ 300 px so it scans from a phone held at arm's length.

**Response 404**: order not found, **or** the order has no payment instruction, or
the order is no longer payable — the image disappears at exactly the moment the
`payment` object does, so a stale page cannot keep showing a live-looking code.

**Caching**: `Cache-Control: private, max-age=60`. The payload is immutable while
the order is payable, and a short TTL keeps a revoked code from lingering.

---

## POST /orders/:orderNumber/payment/refresh

Public, unauthenticated, **rate limited per IP** (research.md §5): 1 request per 5
seconds with a small burst, using the existing `httpx.RateLimitPerIP` middleware.
Backs the "check payment status" button (FR-017).

Behaviour:

1. Resolve the order; `404` if unknown.
2. If the order is already final, return its status immediately — no provider call.
3. If `payment_expires_at` has passed, apply the `EXPIRED` outcome (status +
   quota restoration) through the same guarded transition the webhook uses, and
   return the result.
4. Otherwise call the provider's status API
   (`GET {base}/v2/{order_number}/status`), map the answer through
   `MapProviderStatus`, and apply it through the same `applyOutcome` path — so a
   reconciliation is idempotent, writes its `payments` audit row, and can trigger
   ticket issuance + email exactly once, exactly like a webhook (FR-024, FR-025).

**Request body**: none.

**Response 200**

```json
{
  "order_number": "ORD-20260801-A1B2C3D4",
  "status": "PAID",
  "changed": true,
  "checked_at": "2026-08-01T10:06:41Z"
}
```

`changed` reports whether this call moved the order, so the UI can say "payment
confirmed" versus "still waiting" and always give visible feedback, including when
nothing changed (FR-017).

**Response 429**: `{ "error_code": "RATE_LIMITED", "message": "...", "retry_after_seconds": 5 }`.
The client MUST render this as a visible cooldown on the button, not as an error
(FR-018).

**Response 404**: order not found.

**Response 502**: `{ "error_code": "PAYMENT_STATUS_UNAVAILABLE", "message": "..." }`
when the provider cannot be reached. The order is left untouched; the page keeps
polling and the button stays available.

---

## POST /checkout (existing — behaviour clarified, contract compatible)

Request body unchanged. The response keeps its existing fields:

```json
{
  "order_number": "ORD-20260801-A1B2C3D4",
  "status": "PENDING",
  "total_amount": "550000.00",
  "payment_url": "https://api.sandbox.midtrans.com/v2/qris/<txn>/qr-code"
}
```

What changes:

- The client MUST route in-app to `/orders/{order_number}` and MUST NOT navigate
  the browser to `payment_url` (FR-009).
- `payment_url` now holds the provider's QR-image action URL rather than a hosted
  checkout page. It is retained for compatibility and audit; it is not a page a
  guest is sent to.
- The same three-step sequence is preserved: TX1 reserves quota and creates the
  order, the provider charge happens **outside** any transaction, TX2 stamps the
  payment details — now including `payment_qr_string` and `payment_expires_at`
  (Constitution IV).
- A failed charge still compensates: order `CANCELLED`, quota restored, `502
  PAYMENT_INITIATION_FAILED` (FR-015).

---

## POST /payment/webhook/:provider (existing — unchanged)

Signature verification, the `MapProviderStatus` table, idempotency on already-`PAID`
orders, quota restoration on `expire`/`cancel`/`deny`/`failure`, and async
ticket+email fulfilment are all unchanged. The only new fact is that a
reconciliation (`/payment/refresh`) and the sweeper enter the *same* apply path, so
concurrent arrivals are already covered by the existing `WHERE status = 'PENDING'`
guard.

---

## Gateway interface (internal contract, Constitution V)

Not an HTTP contract, but it is the boundary that keeps the provider swappable, and
it changes here.

```go
type Gateway interface {
    Name() string
    // CreateTransaction now returns a session, not a bare URL: a QRIS charge
    // yields a payload and a deadline, which a redirect-only shape cannot carry.
    CreateTransaction(ctx context.Context, req TransactionRequest) (PaymentSession, error)
    VerifyWebhook(payload []byte, signature string) (*WebhookResult, error)
    // FetchStatus reads the provider's authoritative status for an order,
    // normalized onto the same shape a webhook produces.
    FetchStatus(ctx context.Context, orderNumber string) (*WebhookResult, error)
}
```

`order.PaymentGateway` (the consumer-declared mirror in the order domain) changes
in step. Neither the order domain nor any handler learns a provider-specific field.

### Midtrans implementation notes (verified against the provider docs)

- Charge: `POST {base}/v2/charge`, Basic auth (server key as username, empty
  password), body
  `{"payment_type":"qris","transaction_details":{"order_id","gross_amount"},"qris":{"acquirer":"gopay"},"custom_expiry":{"expiry_duration":15,"unit":"minute"}}`.
- Charge response supplies `transaction_id`, `transaction_status: "pending"`,
  `actions[{name:"generate-qr-code", url}]`, `qr_string`, `expiry_time`.
- Status: `GET {base}/v2/{order_id}/status`, same auth, returns
  `transaction_status` / `fraud_status`.
- Base URL for Core API is **`https://api.sandbox.midtrans.com`** — a different
  host from the `app.sandbox.midtrans.com` the current Snap code uses. Both are
  configuration, injected at wiring time.
- Expiry below 15 minutes is unsupported by the provider's expiry scheduler; the
  configured `PAYMENT_EXPIRY` default is 15 minutes and MUST NOT be set lower.

---

## Admin session (no HTTP contract change)

`POST /admin/login` keeps its request and response (`token`, `expires_at`), and
every `/admin/*` endpoint keeps rejecting requests without a valid bearer token —
the server remains the only authority (FR-008). The entire session fix is
client-side (research.md §1): the guard resolves its state after mount instead of
redirecting on the hydration render.

Two client conventions are introduced, and neither is server-visible:

- `/admin/login?next=<pathname>` — where to land after a successful sign-in
  (FR-003).
- `/admin/login?reason=expired` — why the session ended (FR-005).

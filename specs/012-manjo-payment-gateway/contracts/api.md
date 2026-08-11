# Contract: This system's HTTP surface

**Feature**: [../spec.md](../spec.md)

Only what this feature adds, changes, or removes. Everything else in the purchase flow is untouched.

---

## Added

### `POST /v1.0/callback/exec` — gateway notification

Public, authenticated by bearer token. Full behaviour in [gateway.md §2](./gateway.md). Mounted at
the root, **not** under `/api/v1`, because the path is fixed by the caller.

| Property | Value |
| --- | --- |
| Auth | `Authorization: Bearer {PG_CALLBACK_TOKEN}`, constant-time comparison |
| Success | `200`, empty body |
| Auth failure | non-200; nothing processed, attempt recorded |
| Body cap | as today's webhook — bounded read, a malformed body must not consume unbounded memory |
| Budget | must answer within 5 s (SC-011) |

### `GET /api/v1/admin/payment/order/:order_id/notifications` — an order's payment history

JWT-protected, mounted on the admin group.

Every notification recorded against the order — accepted and refused, gateway-sent and
system-written — with the gateway's transaction id, its raw status, and arrival time, so ops can
match it against the gateway's own dashboard before asking for a resend (FR-022c).

Derived from `payments` alone; order details come through the order-provider interface, never a
cross-domain JOIN (Principle II).

### `GET /api/v1/admin/payment/order/:order_id/holds` — what the order holds

JWT-protected. Returns, per ticket type, how many seats the order holds and how many currently
remain (FR-022e).

This is what an operator needs before topping up quota for a resend, and it is not available
anywhere else: the sold-count shown on the ticket-type editor counts released orders too, so it
overstates what has actually been sold and would lead an operator to add too few seats.

### Not added — a reconciliation worklist

An earlier revision of this contract specified a worklist endpoint returning orders awaiting a human
decision. It is **withdrawn** (FR-022h). The case it existed for — a notification that never arrived
— writes no row and so never appeared on it; what remains is reached by order number, and anomalies
reach a person as operational signals.

### Not added — a manual payment confirmation

Also withdrawn (FR-022d). Ticket issuance has exactly one trigger: a notification from the gateway.
An order stranded by a lost notification is recovered by asking the gateway to resend it, not by a
person asserting the payment inside this system.

---

## Changed

### `POST /api/v1/ticket/checkout/:order_id`

Response drops `qr_refresh_after_seconds`. `expires_at` now carries the gateway's expiry rather than a
locally computed deadline — same field, same type, different provenance.

New failure mode: a duplicate-reference response releases the order's quota and returns an actionable
error telling the guest to start again (FR-007e). Distinct from a generic gateway failure, which
leaves the order and its hold intact for a retry.

### `GET /api/v1/ticket/order/:order_number`

Response drops `qr_refresh_after_seconds`.

**The endpoint stays.** "Remove polling" means removing the client's fixed interval, not this route —
it still serves the initial render, the refresh the stream triggers, and the degraded-mode fallback
(FR-021f).

### `GET /api/v1/ticket/checkout/:order_id/status` — live status

Behaviour unchanged server-side; the snapshot-on-connect that makes reconnection self-healing is
already correct. Two things become load-bearing rather than incidental:

- The 25 s keep-alive is now a **liveness signal**, not just a connection nurse. The client declares
  the stream dead after 50 s of silence (FR-021e).
- A 429 at the concurrent-stream cap is a permanent failure from the browser's perspective —
  `EventSource` does not retry a non-200 — so the client must treat refusal as a failure state, not
  wait for a reconnect (FR-021i). The cap now reads 10 per address.

### `POST /api/v1/ticket/resend-email` — guest ticket-email resend

The endpoint is spec 008's (FR-022). What changes here is how it reports its cooldown and what it does
with a request it cannot key.

**Request** — unchanged: `{"order_id": "ORD-…"}`, carrying the order **number**. No address field is
read, so no caller can redirect the mail.

**Responses**:

| Case | Status | Body |
| --- | --- | --- |
| Accepted — any outcome the disclosure rule covers | `202` | `{"message": "<the constant sentence>", "retry_after_seconds": 60}` |
| Cooldown still running | `429` `RATE_LIMITED` (429001) | `data: {"retry_after_seconds": n}` |
| Body unreadable, or `order_id` missing/empty | `400` `VALIDATION_ERROR` (400001) | error envelope, no detail |

Three properties this pins down:

- **The seconds are on both answers** (FR-021j). An acceptance reports the window it has just started;
  a refusal reports what is left of the one already running. The client never computes the wait.
- **`400` is new, and it is not a disclosure leak** (FR-021k, FR-021l). It reports that the *request*
  was unreadable, which says nothing about any order. A well-formed but unknown order number still
  answers `202` with the same bytes as a successful send — that is the case the silence exists for.
- **A `400` spends nothing.** The cooldown is consulted only after the body is understood, so no
  malformed request can cost any order its window. There is no longer a bucket shared between
  requests the server could not key.

Every attempt is logged server-side with its real outcome (FR-021n). The wire stays silent; the logs
do not.

---

## Removed

| Endpoint | Why |
| --- | --- |
| `POST /api/v1/ticket/order/:order_id/payment/refresh` | No provider status query exists to back it, and the design has no button for it (FR-022, FR-022a) |
| `POST /api/v1/ticket/checkout/:order_id/refresh-qr` | Code refresh withdrawn; the deadline is now the code's own lifetime (FR-010) |
| `POST /api/v1/payment/webhook/:provider` | Replaced by the fixed-path callback endpoint |

Removing the first two may empty the rate-limited route group that existed because each call cost an
outbound round-trip. If nothing remains in it, remove the group rather than leave it configured
around nothing.

### Error codes

| Code | Fate |
| --- | --- |
| `PAYMENT_STATUS_UNAVAILABLE` | remove — only the withdrawn refresh route raised it |
| `PAYMENT_INITIATION_FAILED` | keep — still raised by a failed session open |
| `INVALID_SIGNATURE` | keep, meaning narrows to a rejected bearer token |
| `ORDER_EXPIRED`, `PAYMENT_NOT_STARTED`, `PAYMENT_ALREADY_STARTED` | unchanged |

A new code is needed for the duplicate-reference case: it is neither a transient gateway failure nor
an expired order, and the guest's instruction differs from both.

---

## Frontend contract impact

`qr_refresh_after_seconds` disappears from two responses the frontend reads. That is the one place
this feature can break a running page, so backend and frontend removal must ship together.

`retry_after_seconds` is additive on the resend response, so it cannot break a running page. The
resend request body, however, is currently sent double-encoded — `apiFetch` stringifies a value the
caller had already stringified — which is why the endpoint has been answering `202` while sending
nothing. That fix stands alone and does not wait for the rest: **land it first**, or the cooldown work
ships on top of a call that never reaches the handler's send path at all.

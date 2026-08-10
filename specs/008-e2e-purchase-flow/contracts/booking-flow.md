# Contract: Two-Phase Booking Flow — Transactions & Timers

> **Superseded by [spec 012](../../012-manjo-payment-gateway/spec.md) (2026-08-10).**
> The payment gateway changed from Midtrans to Manjo, and three capabilities described
> below were withdrawn rather than reimplemented: the mid-window QR refresh
> (`POST /ticket/checkout/:order_id/refresh-qr` and `qr_refresh_after_seconds`), the
> guest's status-check button (`POST /ticket/order/:order_id/payment/refresh`), and
> the provider status query that backed it. The payment deadline is now the gateway's
> own (`qr_ea`), adopted verbatim, so one order gets one code for one window. Read the
> sections below as history.

**Feature**: 008-e2e-purchase-flow · Companion to [api.md](./api.md) and
[data-model.md](../data-model.md) §4. Constitution IV governs every shape here.
Endpoint paths per clarification 2026-08-05.

## 1. Book (POST /ticket/book) — fired on T&C "Agree"

```
TX-B (single transaction, NO network calls):
  1. resolve + validate items, expand packages          (existing code path)
  2. aggregate per-ticket-type demand                   (existing)
  3. sorted deterministic quota lock order              (existing)
  4. guarded deduction: UPDATE … WHERE quota >= qty     (existing)
  5. INSERT orders  (status PENDING,
                     buyer fields NULL,
                     payment_* NULL,
                     payment_expires_at = now()+BOOKING_HOLD)
  6. INSERT order_items                                  (existing)
  7. INSERT attendees — EMPTY SLOTS (details NULL)       (changed)
COMMIT
```

Pre-TX guard (read-only): event has authored terms (409 `TERMS_MISSING`
otherwise — nothing to agree to). No gateway call anywhere in this request.
Order-number collision retry loop unchanged (5 attempts).

## 2. Record agreement (POST /ticket/terms-condition/:order_id) — same "Agree" click

Single short TX: guard (`PENDING` ∧ unexpired ∧ `event_terms_id` is the event's
current terms) → `UPDATE orders SET terms_agreed_at = now(), event_terms_id`.
Idempotent (re-POST overwrites with same values).

The book+agreement pair fires back-to-back from the dialog. If the second call
fails, the order sits held but unagreed: checkout refuses it
(`TERMS_NOT_RECORDED`), the frontend retries this call, and the hold sweeper
reclaims abandoned cases at the 1-hour mark. Spec FR-007's "created on
agreement" is preserved from the guest's perspective — both calls are the same
Agree action, and an unagreed order can never reach payment.

## 3. Checkout (POST /ticket/checkout/:order_id) — forms + payment in one call (Option B)

```
read     order + slots; guards: PENDING, unexpired, terms recorded,
         payment not already started
         (if already started: return existing QR payload — idempotent retry)
validate buyer + per-slot visitor details from the request body
TX-D     UPDATE orders buyer fields; UPDATE each attendee slot by id
         (slot must belong to the order; all slots must be covered)
network  Gateway.CreateTransaction(orderNumber, total, …)      ← outside any TX
TX-P     UPDATE orders SET payment_url, payment_qr_string, payment_provider,
                            payment_expires_at = now()+PAYMENT_WINDOW
         guarded by: status='PENDING' AND payment_qr_string IS NULL
```

Failure handling:
- Validation/TX-D fails → nothing saved, 400 field map, guest fixes forms.
- Gateway fails after TX-D → 502; order still PENDING, hold deadline
  unchanged, **saved forms retained** — retry re-validates and re-attempts the
  gateway. No compensation needed: quota was committed at booking and stays
  held by the live order.
- TX-P fails after gateway success → 502; retry hits the idempotent-return
  branch or re-creates under a suffix; webhook resolves whichever session is
  paid.
- TX-P guard matched 0 rows (concurrent checkout) → re-read and return the
  stored QR payload.

Before this call, visitor details exist nowhere server-side (clarification
2026-08-05, Option B): a revisit of the order page shows the held order with
empty forms.

## 4. QR refresh (POST /ticket/checkout/:order_id/refresh-qr)

```
guards   PENDING ∧ payment started ∧ unexpired
network  Gateway.CreateTransaction("{orderNumber}-R{n}", same total)   ← outside TX
TX-R     UPDATE orders SET payment_url, payment_qr_string        (deadline NOT touched)
```

`n` = count of prior refreshes + 1 (derived from payments log rows for the
order). Webhook matches by stripping `-R\d+` suffix before order lookup, then
proceeds through the existing idempotent settlement path — first settlement
wins, later ones return 200 immediately.

## 5. Timers (server-owned, config `pkg/config`)

| Name | Env | Default | Written to |
|------|-----|---------|------------|
| Booking hold | `BOOKING_HOLD` | 1h | `payment_expires_at` at book |
| Payment window | `PAYMENT_WINDOW` | 14m | `payment_expires_at` at checkout (overwrite) |
| QR refresh point | `QR_REFRESH_AFTER` | 7m | frontend timer only (served in checkout response as `qr_refresh_after_seconds`) |
| Gateway QR validity | `PAYMENT_EXPIRY` | 15m (floor 15m, unchanged) | provider request |

Invariants:
- `PAYMENT_WINDOW < PAYMENT_EXPIRY` (config validation) — server deadline always
  strictly inside gateway validity (research R2).
- `QR_REFRESH_AFTER < PAYMENT_WINDOW`.
- Deadlines are absolute timestamps in the DB; countdowns render from
  `expires_at`, never from client clocks alone.

## 6. Expiry (existing sweeper — no changes)

One query drives both phases: `status='PENDING' AND payment_expires_at < now()`
→ EXPIRED + restore quota per order lines (packages expanded exactly as
reserved). The 1-hour hold and the 14-minute window expire through the same
path. Every expired order is logged with its order number and restored quota
lines (FR-009 — auditable sweeps). SSE hub is notified on each transition so
open payment screens flip to the expired state live.

## 7. Frontend screen contract

| Order state | Screen (route under events/[slug]) |
|-------------|-------------------------------------|
| PENDING, `payment_started=false` | orders/[orderNumber] — empty visitor forms + hold countdown (details load nothing — Option B) |
| PENDING, `payment_started=true` | orders/[orderNumber] — QR panel + 14m countdown; QR refresh at `qr_refresh_after_seconds`; SSE subscription; **event countdown from shared layout stays mounted (FR-019)** |
| PAID | redirect → orders/[orderNumber]/done |
| EXPIRED / CANCELLED | expired state (Figma 288-2295) + link to events/[slug]/tickets to restart |

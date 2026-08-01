# Quickstart: Persistent Admin Session & In-App QRIS Payment Page

Validation scenarios that prove the feature works end to end. Contract details are
in [contracts/api.md](./contracts/api.md); design rationale in
[research.md](./research.md).

## Prerequisites

- `docker compose up -d` (Postgres + API) and `npm run dev` in `frontend/`.
- Midtrans **sandbox** credentials in `backend/.env`, with the Core API host:

  ```bash
  MIDTRANS_SERVER_KEY=SB-Mid-server-xxxx
  MIDTRANS_BASE_URL=https://api.sandbox.midtrans.com   # api., not app.
  PAYMENT_EXPIRY=15m                                    # provider floor; do not go lower
  PAYMENT_SWEEP_INTERVAL=30s
  ```

- **Schema**: `backend/migrations/0002_payment_qris.sql` is picked up automatically
  only on a **fresh** Postgres volume (`docker-entrypoint-initdb.d` runs once). On
  an existing dev database, apply it manually:

  ```bash
  psql "$DATABASE_URL" -f backend/migrations/0002_payment_qris.sql
  psql "$DATABASE_URL" -c '\d orders'   # expect payment_qr_string, payment_expires_at
  ```

- At least one published event with an on-sale ticket type.

---

## Scenario 1: Admin session survives a refresh (User Story 1)

1. Sign in at `http://localhost:3000/admin/login`.
2. Navigate to `/admin/orders`, then press **F5**.

**Expected**: the orders page re-renders signed in. The login screen must not
appear at any point — not even as a flash. This is the whole defect: before the
fix, the hydration render redirects before the stored session is read
(research.md §1).

3. Open `http://localhost:3000/admin/events` directly in a new tab.

**Expected**: renders signed in, no credentials requested.

4. Open `http://localhost:3000/admin/attendees` in a private window (no session).

**Expected**: redirected to `/admin/login?next=%2Fadmin%2Fattendees`; after signing
in you land on `/admin/attendees`, not the admin home (FR-003).

5. Simulate an expired session, then reload any admin page:

   ```js
   // browser console
   const k = "ticketing.admin.token";
   const e = JSON.parse(localStorage.getItem(k));
   localStorage.setItem(k, JSON.stringify({ ...e, expiresAt: new Date(Date.now() - 1000).toISOString() }));
   ```

**Expected**: `/admin/login?reason=expired` with a message saying the session
expired (FR-005).

6. Corrupt the entry (`localStorage.setItem("ticketing.admin.token", "{{{")`) and
   reload.

**Expected**: clean redirect to login, no error page (FR-007).

7. With two tabs open on admin pages, sign out in one.

**Expected**: the signing-out tab returns to login immediately; the other tab stops
showing admin content on its next navigation or refresh (FR-006).

---

## Scenario 2: Checkout lands on the order page with a QRIS code (User Story 2)

Complete a checkout in the UI, or via the API:

```bash
curl -s -X POST http://localhost:8080/api/v1/checkout \
  -H 'Content-Type: application/json' \
  -d '{"buyer_name":"Siti","buyer_email":"siti@example.com","buyer_phone":"08123456789",
       "items":[{"ticket_type_id":"<uuid>","quantity":1}],
       "attendees":[{"ticket_type_id":"<uuid>","name":"Siti","email":"siti@example.com"}]}' | jq
```

**Expected**: `200` with `order_number` and `status: "PENDING"`. In the browser the
guest must land on `/orders/<order_number>` — **not** on a Midtrans page (FR-009).

```bash
ORDER=<order_number>
curl -s http://localhost:8080/api/v1/orders/$ORDER | jq
```

**Expected**: order number, event, items, buyer, total, `status: "PENDING"`,
`server_time`, and a non-null `payment` object with `method: "QRIS"`,
`expires_at` ≈ 15 minutes out, and `qr_image_path`.

```bash
curl -s -o /tmp/qris.png -w '%{content_type} %{size_download}\n' \
  http://localhost:8080/api/v1/orders/$ORDER/qris.png
```

**Expected**: `image/png` with a non-trivial size; opening it shows a scannable QR.

Confirm it is rendered on demand from the stored payload, with nothing on disk:

```bash
psql "$DATABASE_URL" -c \
  "SELECT left(payment_qr_string, 24) AS qr, payment_expires_at, payment_provider FROM orders WHERE order_number = '$ORDER'"
```

**Reload check (FR-013)**: refresh `/orders/<order_number>` several times and
re-run the curl above.

**Expected**: identical `qr_string` and identical `expires_at` every time — no new
payment attempt is created. The countdown continues from where it should, not from
15:00.

**Clock-skew check (SC-005)**: set the OS clock forward by an hour and reload.

**Expected**: the countdown still shows the true remaining time, because it is
computed from `server_time`, not the device clock.

---

## Scenario 3: Paying updates the page by itself (User Story 3)

With `/orders/<order_number>` open in the browser, pay the sandbox transaction at
the Midtrans QRIS simulator (`https://simulator.sandbox.midtrans.com/qris/index`),
pasting the `qr_string` or scanning the PNG.

**Expected**: within ~10 seconds and with no interaction, the page replaces the QR
and countdown with a payment-successful state naming the buyer's email (FR-016,
FR-019). Polling then stops — confirm in DevTools → Network that requests to
`/orders/<order_number>` cease once the status is final (FR-020).

If the webhook cannot reach your machine (the usual local case), the manual control
is the path that closes the loop:

```bash
curl -s -X POST http://localhost:8080/api/v1/orders/$ORDER/payment/refresh | jq
```

**Expected**: `{"status":"PAID","changed":true, ...}` on the first call after
paying, then `changed: false` on subsequent calls — the reconciliation runs the same
idempotent transition as the webhook.

**Rate limit (FR-018)**: fire it repeatedly —

```bash
for i in $(seq 1 5); do curl -s -o /dev/null -w '%{http_code} ' \
  -X POST http://localhost:8080/api/v1/orders/$ORDER/payment/refresh; done; echo
```

**Expected**: a `429` with `retry_after_seconds`; in the UI the button shows a
cooldown rather than an error.

**Fulfilment is still exactly once (FR-025)**: deliver the same webhook twice, or
mix a webhook with a refresh, then check:

```bash
psql "$DATABASE_URL" -c \
  "SELECT (SELECT count(*) FROM tickets t JOIN orders o ON o.id=t.order_id WHERE o.order_number='$ORDER') AS tickets,
          (SELECT email_sent FROM orders WHERE order_number='$ORDER') AS email_sent,
          (SELECT count(*) FROM payments p JOIN orders o ON o.id=p.order_id WHERE o.order_number='$ORDER') AS payment_rows"
```

**Expected**: one ticket per attendee (never doubled), `email_sent = true`, and one
`payments` audit row per notification received — the log grows, the effects do not.

**Offline behaviour (FR-021)**: with an unpaid order open, switch DevTools to
Offline.

**Expected**: the page shows that live updating is interrupted and keeps retrying;
back Online it recovers on its own without a manual refresh.

---

## Scenario 4: Expiry returns the quota (User Story 4)

Note the ticket type's remaining quota, then create an order and let it expire.
To avoid waiting 15 minutes, pull the deadline back directly:

```bash
psql "$DATABASE_URL" -c \
  "UPDATE orders SET payment_expires_at = now() - interval '1 minute' WHERE order_number = '$ORDER'"
```

**Expected in the browser**: the countdown hits zero, the QR disappears, and the
page shows an expired state offering a link back to the event (FR-014, US4).

**Expected server-side within 30 seconds** (the sweeper interval):

```bash
psql "$DATABASE_URL" -c "SELECT status FROM orders WHERE order_number = '$ORDER'"
psql "$DATABASE_URL" -c "SELECT name, quota FROM ticket_types WHERE id = '<uuid>'"
```

`status = 'EXPIRED'` and the quota is back to its pre-order value (FR-023, FR-024,
SC-007).

`GET /orders/$ORDER` must now return `payment: null`, and
`GET /orders/$ORDER/qris.png` must return `404` — an expired order never keeps
showing a live-looking code.

**No double restore**: run the refresh endpoint and deliver an `expire` webhook
after the sweeper has already expired the order.

**Expected**: quota unchanged from the restored value. The guarded
`WHERE status = 'PENDING'` transition means only the first path applies.

---

## Scenario 5: Failure paths

**Provider unreachable during checkout (FR-015)**: point `MIDTRANS_BASE_URL` at an
unroutable host and check out.

**Expected**: `502 PAYMENT_INITIATION_FAILED`, the order is `CANCELLED`, and the
quota is back — the guest can retry immediately.

**Provider unreachable during a status check**: same host change, with a
`PENDING` order.

**Expected**: `502 PAYMENT_STATUS_UNAVAILABLE`, order untouched, page keeps polling.

**Unknown order**: `curl -s -o /dev/null -w '%{http_code}\n'
http://localhost:8080/api/v1/orders/ORD-DOES-NOT-EXIST` → `404`, with a message
identical to any other not-found order (FR-022).

---

## Automated checks

```bash
cd backend && ./scripts/test.sh          # gateway charge/status mapping, sweeper idempotency,
                                          # reconciliation reusing the webhook outcome path
cd frontend && npx vitest run            # session guard states, countdown skew, poll-to-paid
```

## Reference

- Contract details: [contracts/api.md](./contracts/api.md)
- Entity/field details: [data-model.md](./data-model.md)
- Design rationale: [research.md](./research.md)

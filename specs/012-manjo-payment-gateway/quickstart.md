# Quickstart: Validating the Manjo Payment Gateway Migration

**Feature**: [spec.md](./spec.md) | **Contracts**: [gateway.md](./contracts/gateway.md), [api.md](./contracts/api.md)

How to prove this feature works end to end. Every scenario below is runnable without a reachable
gateway — pointing `PG_BASE_URL` at a stub is the ordinary way to run it (FR-008a), not a special
test mode.

## Prerequisites

```fish
cd backend
cp .env.example .env   # then edit per the table below
docker compose up -d db
go run ./cmd/api        # or the project's usual runner
```

### Environment

| Variable | Value for local validation |
| --- | --- |
| `PG_BASE_URL` | your stub's address, e.g. `http://localhost:10327` — **required, no default** |
| `PG_CLIENT_KEY` | any non-empty string |
| `PG_SERVER_KEY` | any non-empty string |
| `PG_CALLBACK_TOKEN` | any non-empty string; you will send it back as a bearer token |
| `PAYMENT_WINDOW` | `15m` — now only a fallback basis and an expectation |

Removed — startup must **fail** if these linger in a way that suggests they still work:
`MIDTRANS_*`, `PAYMENT_EXPIRY`, `QR_REFRESH_AFTER`.

### Startup gate (User Story 4)

Before anything else, confirm the system refuses to start badly:

```fish
# 1. missing address → must refuse, naming PG_BASE_URL
env -u PG_BASE_URL go run ./cmd/api
# 2. unusable address → must refuse, naming the problem
env PG_BASE_URL='not a url' go run ./cmd/api
# 3. correct → starts
```

A start that succeeds in cases 1 or 2 is a failure of FR-026, not a convenience.

---

## Scenario 1 — Guest pays with a Manjo-issued code (User Stories 1, 5)

**Stub returns**: `s: true`, an `eri`, a `qr_r` payload, and `qr_ea` set ~15 minutes out.

1. Book and check out as a guest through the normal flow.
2. The payment page shows a scannable code inside the QRIS frame, the amount due, the order summary
   with each fee on its own line, and a countdown.
3. **Verify the deadline came from the gateway**: set the stub's `qr_ea` to a distinctive value
   (say 9 minutes out, not 15). The countdown must start from *that*, not from `PAYMENT_WINDOW`.
   A 15-minute countdown here means `qr_ea` was dropped — the exact silent failure R1 warns about.
4. Confirm the request the stub received carries **six** fields and **no** `e`.
5. Confirm the reference is the order number unmodified, 19 characters, no `-R` suffix.

**Expiry handling** — three stub variations, each must behave differently:

| Stub `qr_ea` | Expected |
| --- | --- |
| absent | falls back to `PAYMENT_WINDOW` **and** raises a signal (FR-009b). Watch for a countdown starting in year 1 or a wildly negative remaining time — that is the zero value being treated as a real expiry instead of as missing. |
| already past | same as absent |
| malformed (e.g. `"tomorrow"`) | falls back **and still shows the code** (FR-009e). A failed checkout here means the decode error took the payload down with the timestamp. |
| much longer than expected (e.g. 4h) | **adopted as-is**, plus a signal (FR-009c/d) — not capped |
| offset other than `+07:00` (e.g. the same instant as `+00:00`) | identical countdown — the instant is what matters, not the wall-clock reading (FR-009a) |

## Scenario 2 — Payment confirms over the callback (User Story 2)

With the payment page open:

```fish
curl -X POST http://localhost:8080/v1.0/callback/exec \
  -H "Authorization: Bearer $PG_CALLBACK_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"ri":"ORD-20260810-XXXXXX","nti":"A48593…","s":5,"td":"2026-08-10T10:15:22+07:00","tt":0}'
```

Expect: `200` **within 5 seconds**, body `{"code":200000,"message":"Success","data":null}`, order →
`PAID`, tickets issued once, one email, and the open page flips to the paid state with no reload.

Time the response — exceeding 5 s guarantees a duplicate delivery in production even when everything
else is correct.

**The body is never empty** (FR-012e). Pipe every call in this scenario through `jq -e '.code'` — a
`204`-style empty answer or a bare object outside the envelope fails that outright, and neither is
visible if you check only the status line.

**The uneventful outcomes look identical** (FR-012f). Send a pending notification (`s:0`) and a
repeat against an already-paid order. Both must return exactly
`{"code":200000,"message":"Success","data":null}` — the same bytes as the successful call above. What
each did is read from the per-order notification history (Scenario 6, step 3), not from the response.

**Every signalled anomaly names itself** (FR-012g). Each keeps a `200` status — assert the code:

```fish
# unknown reference → 200002
curl -s -X POST … -d '{"ri":"ORD-NOT-REAL","nti":"X","s":5,"tt":0}' | jq -e '.code == 200002'
# withdrawal → 200003
curl -s -X POST … -d '{"ri":"ORD-NOT-REAL","nti":"X","s":5,"tt":1}' | jq -e '.code == 200003'
```

The remaining three need a real order: an unrecognised `s` against a pending order (`200004`), a
`Reject` against a paid one (`200005`), and a `Completed` against a cancelled one (`200006`).
**Precedence when several could apply**: the deposit check runs first, then the order lookup, then the
status mapping — so an unknown reference carrying `s:99` answers `200002`, not `200004`.

**Authentication** — each must change nothing, and each must still answer in the envelope:

```fish
# no token, wrong token
curl -X POST … -d '{…}'
curl -X POST … -H 'Authorization: Bearer wrong' -d '{…}'
```

Expect a non-200 whose body is the standard error envelope, code `401001`. This path needs no new
code — the shared error handler already renders it — so it is a regression check rather than a new
behaviour.

**Idempotency** — replay the successful call 10 times. Exactly one set of tickets, one email, `200`
with `code: 200000` every time (SC-004).

## Scenario 3 — Status mapping and quota (User Story 3)

For each `s` value, send a callback against a fresh pending order and check the ticket type's
remaining quota returns to its pre-order value exactly once:

| `s` | Order | Quota | Signal |
| --- | --- | --- | --- |
| 0 `Pending` | unchanged | held | no |
| 1 `Reject` | `CANCELLED` | restored | no |
| 2 `Cancel` | `CANCELLED` | restored | no |
| 3 `Expired` | `EXPIRED` | restored | no |
| 4 `Obscure` | unchanged | held | **yes** |
| 5 `Completed` | `PAID` | consumed | no |
| 99 (unknown) | unchanged | held | **yes** |

Also send `"tt":1` (withdraw): no order change, recorded, signalled, still `200`.

## Scenario 4 — Contradiction, and the two orders a completion may not revive

1. Settle an order with `s:5`.
2. Send `s:2` (Cancel) for the same `ri`.

Expect: order **stays** `PAID`, tickets stay valid, quota not returned, an error-level signal
distinguishable from routine traffic, and a `DISPUTED` marker readable on that order's own payment
history (FR-016b/c).

3. Send `s:5` again. Expect: quiet — recorded, no signal (FR-016 / US2 scenario 11).

Then the case that must **not** settle: take an order the gateway itself cancelled (`s:1` or `s:2`
against a pending order) and send `s:5`. Expect the order unchanged, no tickets, the payload recorded
on the order, an error-level signal, and `200`. Only *our own* expiry verdict is reversible — see
Scenario 6 (FR-019d).

## Scenario 5 — Live status without polling (FR-021d–i)

1. Open the payment page with devtools' network tab filtered to the order endpoint.
   **Expect zero repeating requests** while the stream is healthy (SC-015).
2. Kill the stream (devtools offline toggle). Within ~50 s the page must start fallback reads, then
   stop them when the stream returns — with no duplicate or flickering status (US5 scenario 12).
3. **Silent-buffering case**, the one that motivated the watchdog: have the stub proxy accept the
   connection and forward nothing. The page must still notice within ~50 s, because connection state
   alone reports health that is not there.
4. Open 11 streams from one address. The 11th is refused with `429`; that page must fall back rather
   than wait for a reconnect the browser will never attempt.

## Scenario 6 — Recovery by redelivery (User Story 6)

This is the flow that only ever runs on the worst day of the month, which is exactly why it is worth
walking deliberately once. Note what is **not** here: no step in which a person tells this system that
a payment happened. The gateway settles the order; ops only makes it possible.

1. Take an order, suppress its callback entirely, and let it expire. Its seats return to the pool.
2. Sell those seats to someone else, so the ticket type can no longer cover the stranded order.
3. As an admin, look the order up by its number. Expect its status, every notification recorded
   against it (including refused ones) with the gateway's transaction id, raw status and arrival
   time, and **what the order holds against what remains** — the figure needed to size the top-up
   (FR-022c, FR-022e).
4. Replay the notification (`s:5`). Expect: **`200` carrying envelope code `200001`**, with the
   per-ticket-type shortfall in `data`, status unchanged, no tickets, no quota movement, and a
   `SETTLE_REFUSED_NO_QUOTA` marker (FR-019c, FR-019e). A non-200 here would be the bug: the gateway
   would spend its whole retry budget on an attempt guaranteed to fail identically.

   **Assert the code, not the status.** This answer and the successful one in step 5 are both `200`;
   `jq -e '.code == 200001'` is what tells them apart, and a test checking `200` alone passes on
   either. Size the step-5 top-up straight from `.data.shortfall` rather than from the ticket-type
   editor's sold count, which counts released orders and understates what is needed.
5. Top the ticket type's quota up by the shortfall, then replay again. Expect: `200` with code
   `200000` and null `data`, order `PAID`, seats deducted again, tickets issued **once**, email sent
   **once**, and a `SETTLED_AFTER_EXPIRY` marker recording the prior status and the lines re-taken
   (FR-019, FR-019b).
6. Replay a third time. Expect nothing further — no second tickets, no second email, no second
   deduction (US6 scenario 5, FR-012d).

**What the top-up demands of an operator**, worth knowing before step 5 rather than during it:

- The ticket-type editor sets quota **absolutely**, not as a delta. A top-up during active sales can
  overwrite a purchase that landed in between. Spec 002 records this race as an accepted risk; it is
  not introduced here, but this is the flow most likely to meet it.
- A **package order** holds seats across several ticket types at once, and *every one* of them must
  cover its hold before the settle succeeds — the refusal in step 4 is all-or-nothing. Step 3's
  holds view is what makes that tractable; the sold count on the ticket-type editor is not, because
  it counts released orders and so overstates what has actually been sold.

## Scenario 7 — Retry and duplicate reference (FR-007a–f)

1. **Transient failure**: stub refuses the connection on the first attempt, succeeds on the second.
   Checkout must succeed, with the same `ri` on both attempts.
2. **Duplicate reference**: stub returns the duplicate-reference error. Expect: no further retries,
   quota released **promptly** rather than at the deadline, a `SESSION_DUPLICATE` marker, and a clear
   "start again" message — not a payment page with no code.
3. Confirm the retry budget is bounded well inside a guest's patience. If it approaches the gateway's
   own 10-second pacing, FR-007c is violated.

## Scenario 8 — Resend cooldown on the confirmation screen (FR-021j–q)

Start from a paid order, on its confirmation screen. Step 1 is the one that would have caught the
defect this scenario exists because of.

1. **The request reaches the handler at all.** Press *Resend email* once and watch the server log.
   Expect a line naming the order and the outcome `delivered`, and an email. A `202` with no log line
   is the failure mode that hid a double-encoded body for an afternoon — the wire body must parse to
   an object with `order_id`, not to a string.
2. **The seconds arrive on the acceptance.** Same press: the `202` body carries
   `retry_after_seconds` (60), and the button goes into a visible countdown rather than staying
   pressable (FR-021j).
3. **The refusal carries what is left.** Press again immediately via `curl`. Expect `429`,
   `RATE_LIMITED`, and `data.retry_after_seconds` **below** 60 and falling between attempts — a
   constant 60 means the remaining time is being reported as the window, not as the remainder.
4. **The button re-arms itself.** Let the countdown finish with the page untouched. The button must
   become usable again with no reload and no further press, and the next press must send (FR-021o).
   This is the exact behaviour that was missing: a 429 disabled the control permanently.
5. **Nothing is remembered across loads** (FR-021p). Press once, then reload immediately. The button
   is armed; pressing it returns `429` with the true remaining seconds, and the countdown resumes from
   *that* number. Confirm nothing was written to browser storage.
6. **A malformed body refuses only itself** (FR-021k, FR-021l). This is the regression that matters
   most, because its absence is invisible until two guests are on the site at once:

   ```fish
   # unreadable body → 400, and no allowance spent anywhere
   curl -i -X POST http://localhost:8080/api/v1/ticket/resend-email \
     -H 'Content-Type: application/json' -d '"{\"order_id\":\"ORD-A\"}"'

   # a DIFFERENT order must still be accepted in the same window
   curl -i -X POST http://localhost:8080/api/v1/ticket/resend-email \
     -H 'Content-Type: application/json' -d '{"order_id":"ORD-B"}'
   ```

   Expect `400` then `202`. A `429` on the second call means the shared bucket survived.
7. **An unknown order is still indistinguishable from a send** (FR-021l). Post a well-formed but
   fictional order number. Expect `202`, the same sentence, the same `retry_after_seconds`, byte for
   byte identical to step 1's body — and a log line reading `order unknown`.
8. **Every attempt spends the window** (FR-021m). Post the fictional number twice. The second is
   `429`: an outcome that sent no mail still counts, or order-number probing is unlimited.
9. **A cooldown refusal does not read as a failure** (FR-021q). Compare the screen at step 3 against
   the screen when the mailer is stubbed to error: one says when to come back, the other says it could
   not send. If they look alike, the guest cannot tell waiting from broken.

---

## Regression checks

These prove the withdrawals actually happened rather than being merely unwired:

```fish
# routes must 404
curl -i -X POST http://localhost:8080/api/v1/ticket/order/ORD-.../payment/refresh
curl -i -X POST http://localhost:8080/api/v1/ticket/checkout/ORD-.../refresh-qr
curl -i -X POST http://localhost:8080/api/v1/payment/webhook/midtrans

# field must be gone from both responses
curl -s http://localhost:8080/api/v1/ticket/order/ORD-... | grep qr_refresh_after_seconds  # no match

# no provider name left in the backend
rg -i midtrans backend/ --glob '!vendor'   # no match

# the withdrawn reconciliation surface must be gone, not merely unlinked
curl -i http://localhost:8080/api/v1/admin/payment/reconciliation                 # 404
curl -i -X POST http://localhost:8080/api/v1/admin/payment/reconciliation/<id>/confirm  # 404

# and nothing may remain that records a payment on a person's word (FR-022d)
rg -i "worklist|MANUAL_RECONCILED|ORPHAN_PAYMENT|ConfirmPayment" backend/ frontend/ \
  --glob '!vendor' --glob '!node_modules'   # no match

# the shared-bucket limiter must be gone, not merely unwired (FR-021k)
rg "func RateLimitPerBodyField|func bodyField|httpx\.RateLimitPerBodyField" \
  backend/ --glob '!vendor'                                      # no match

# and no source file may pre-stringify a body apiFetch will stringify again
rg "body: JSON.stringify" frontend/lib frontend/components frontend/app \
  --glob '!*.test.*' --glob '!node_modules'                      # no match
```

Both greps name the *definition or call site* rather than the string, because two
deliberate mentions survive and should: a comment in `backend/pkg/httpx/cooldown_test.go`
explaining what the withdrawn helper got wrong, and a test in
`frontend/lib/api-client.test.ts` that pins the double-encoding trap by demonstrating it.
A grep that flagged those would train the next reader to ignore it.

And confirm no migration was added: `git diff --stat backend/migrations/` must be empty, and
`SCHEMA.md` unchanged.

## ~~Release step — verify the QRIS frame identity~~ (retired by spec 020)

> **This step no longer exists.** [Spec 020](../020-qris-frame-simplify/spec.md) removed the merchant
> name, registration number and terminal label from the frame, which retires FR-021b: there is no
> configured label left that could disagree with the issued code. Nothing has to be re-verified when
> the merchant account changes.
>
> The decoding recipe below is kept because it is still the way to read what a code actually carries
> — useful when diagnosing a gateway problem — but it is no longer a release gate, and the frontend
> variables it names no longer exist.

**Formerly**: a release step, not a test, repeated whenever the merchant account changed. The
payment instructions told the guest to check the merchant name before entering a PIN, so a configured
label that disagreed with the issued code defeated the very check it invited.

Open one session and decode the payload:

```fish
curl -s -X POST "$PG_BASE_URL/v1/manjo/transaction/incoming" \
  -H 'Content-Type: application/json' \
  -d '{"ri":"FRAMECHECK-01","a":10000,"c":"IDR","m":0,"r":"Payment for goods",
       "ac":{"cr":{"client_id":"'$PG_CLIENT_KEY'","client_secret":"'$PG_SERVER_KEY'"}}}'
```

What the tags carry — unchanged by spec 020, since the payload was never touched, only what the
page printed beside it:

| Payload tag | Meaning | Former frontend variable | Value observed 2026-08-10 |
| --- | --- | --- | --- |
| `59` | merchant name | ~~`NEXT_PUBLIC_QRIS_MERCHANT_NAME`~~ retired | `Pupuk Kalteng` |
| `26`→`01` | NMID | ~~`NEXT_PUBLIC_QRIS_MERCHANT_ID`~~ retired | `936008580287697876` |
| `62`→`07` | terminal label | ~~`NEXT_PUBLIC_QRIS_TERMINAL_LABEL`~~ retired | `659` |
| `01` | must be `12` | — | `12` (dynamic; the amount is fixed by the code) |

The check earned its place while it existed: the values first configured from an older sample
(`Ayoborong` / `…176412711` / `A01`) were wrong for this environment and were corrected here. The
payer's own app still reads tag `59` out of the code, which is the identity that was always
authoritative.

## Settled by observation — no longer open

| Was open | Answer |
| --- | --- |
| **R10** fractional rupiah | The gateway preserves it verbatim. `a: 11550.11` came back as tag `54` = `11550.11` — not rejected, not truncated, not rounded. |
| **Duplicate reference wording** | It is `MERCHANT_NOT_AVAILABLE`, not anything containing "duplicate". A fresh reference immediately before and after both succeed, so the merchant is available and the *reference* is taken. |
| **R5** credential half mapping | **Unconfirmable.** Swapping the two halves is accepted; emptying `client_id` is refused. The gateway checks presence, not privilege, so nothing can tell you if the mapping is backwards. |
| Gateway's default validity | ~8 minutes, not 15. Adopted verbatim; `PAYMENT_WINDOW` is only the fallback and the expectation. |

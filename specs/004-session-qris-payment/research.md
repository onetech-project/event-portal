# Phase 0 Research: Persistent Admin Session & In-App QRIS Payment Page

**Feature**: `004-session-qris-payment` | **Date**: 2026-08-01

All Technical Context unknowns from [plan.md](./plan.md) are resolved below. Each
entry records the decision, why it was chosen, and what was rejected.

---

## 1. Root cause of the admin session loss

**Decision**: The defect is a client-side hydration race in
[frontend/app/admin/layout.tsx](../../frontend/app/admin/layout.tsx), not a token
lifetime or backend problem. Fix it by giving the guard a three-state session
value — `loading` / `authenticated` / `anonymous` — and redirecting only on
`anonymous`.

**Evidence**:

- The token is stored in `localStorage` with the server's `expires_at`
  ([frontend/lib/auth.ts:9](../../frontend/lib/auth.ts#L9)) and `JWT_TTL` is 12h
  (`backend/.env`), so the credential itself survives a reload. Nothing clears it
  on refresh.
- The guard reads it through
  `useSyncExternalStore(subscribe, () => isAuthenticated(), () => false)`. The
  third argument is the **server snapshot**, and React also uses it for the first
  client render during hydration so the markup matches. The guard's `useEffect`
  runs in that same commit, sees `authed === false`, and calls
  `router.replace("/admin/login")` before the post-hydration snapshot re-read can
  schedule a re-render. Every hard navigation into `/admin/*` therefore bounces to
  login even with a perfectly valid token.
- This also explains why in-app client-side navigation works fine (no hydration
  pass) while F5 does not — exactly the reported symptom.

**Fix shape**:

- Read the stored session **after mount** (`useEffect` / mounted flag), keeping the
  server render and the hydration render identical (`loading`), so no hydration
  mismatch is introduced.
- Redirect only once the state resolves to `anonymous`; render the existing
  `Loading` placeholder while `loading`.
- Preserve the requested path (`?next=<pathname>`) so post-login lands where the
  admin asked for (FR-003), and pass `?reason=expired` when a stored session was
  rejected or timed out (FR-005).
- Keep the `storage` event subscription for cross-tab sign-out, and dispatch an
  in-page event on sign-out so the current tab updates without a reload (FR-006):
  the native `storage` event does **not** fire in the tab that wrote the change.

**Alternatives considered**:

- *Move the session to an httpOnly cookie issued by the backend* — genuinely
  better against XSS, but it needs a new backend login response shape, CORS
  `credentials: "include"` across the split origins, CSRF protection on every
  admin mutation, and a change to `adminFetch`. The spec's assumption is that the
  session mechanism stays the same in kind; deferred, and recorded here as the
  natural follow-up if the admin surface ever leaves the MVP.
- *Middleware-based route guard (`middleware.ts`)* — cannot see `localStorage`; it
  would only work with the cookie approach above.
- *`getServerSnapshot` returning `true`* — swaps a false negative for a false
  positive: unauthenticated visitors would briefly see admin chrome.

---

## 2. Payment provider: QRIS via Midtrans Core API

**Decision**: Replace the Snap redirect with a **Core API QRIS charge**
(`POST /v2/charge`, `payment_type: "qris"`, `qris.acquirer: "gopay"`), issued at
checkout, behind the existing `payment.Gateway` abstraction.

**Verified provider facts** (Midtrans docs, checked 2026-08-01):

| Fact | Value |
|---|---|
| Charge endpoint | `POST {base}/v2/charge`, Basic auth with server key, empty password |
| Request | `{"payment_type":"qris","transaction_details":{"order_id","gross_amount"},"qris":{"acquirer":"gopay"},"custom_expiry":{...}}` |
| Response | `transaction_id`, `order_id`, `gross_amount`, `transaction_status: "pending"`, `actions[{name:"generate-qr-code",method:"GET",url}]`, `qr_string`, `expiry_time` |
| `custom_expiry` | `{order_time, expiry_duration, unit}` — e.g. `{"expiry_duration":15,"unit":"minute"}` |
| Default QRIS expiry | 15 minutes; **below 15 minutes is explicitly not recommended** — the provider's expiry scheduler only reliably expires transactions at ≥15 minutes |
| Status read | `GET {base}/v2/{order_id}/status`, same Basic auth, returns `transaction_status` / `fraud_status` |
| Sandbox base | `https://api.sandbox.midtrans.com` (note: **api.**sandbox, not the `app.sandbox` host the Snap code uses) |

**Consequences for the spec's assumptions**:

- The spec's assumed 15-minute deadline is exactly the provider default and its
  recommended floor — keep 15 minutes as the configured default
  (`PAYMENT_EXPIRY`), and treat values below 15 minutes as unsupported.
- `qr_string` is the raw QRIS payload; rendering it ourselves is what keeps the
  "no object storage, generate on demand" rule intact (see §3).

**Alternatives considered**:

- *Snap with `enabled_payments: ["other_qris"]`* — still hands back a hosted page
  or requires the Snap JS popup; it does not give us a QR payload to render on our
  own page, which is the whole point of the change.
- *Keep Snap and embed it in an iframe* — the provider does not support framing,
  and it would not satisfy "show the order detail page with QRIS".
- *A different acquirer (`airpay_shopee`)* — narrows which apps can scan;
  `gopay` acquiring issues a standard interoperable QRIS payload.

---

## 3. Rendering the QR image

**Decision**: Store the provider's `qr_string` and render the QR **server-side on
demand** as a PNG from a dedicated endpoint, using `github.com/skip2/go-qrcode`
(already a direct dependency, used for ticket QR codes).

**Rationale**:

- The constitution's Technology Stack section forbids object storage and requires
  QR images to be generated on demand at render time. Rendering from `qr_string`
  follows the identical pattern already used for ticket QR codes.
- Serving our own PNG avoids making the guest's browser call the provider's host
  (`actions[].url`) directly, which would leak the provider's transaction id into
  the page and make the QR unavailable if that host is slow or blocked.
- A separate image endpoint keeps the JSON status payload small — important
  because that payload is polled every few seconds (§4). The PNG is cacheable and
  fetched once.

**Alternatives considered**:

- *Inline base64 data URI in the status JSON* — re-sends 2-5 KB on every poll tick
  for no benefit; would force a second query key to avoid it.
- *Add a JS QR generator to the frontend* — a new dependency for something the
  backend already has a library for; `@zxing/*` in the project reads QR codes, it
  does not generate them.
- *Redirect the browser to `actions[].url`* — kept as a stored fallback only
  (`orders.payment_url`), not used for display.

---

## 4. Live update mechanism (webhook → open page)

**Decision**: **TanStack Query polling** of the public order status endpoint every
**3 seconds** while the order is `PENDING`, stopping at any final state.

**Rationale**:

- FR-016 requires ≤10s; a 3s interval gives a ~3s median and a 3s worst case,
  comfortably inside the 10s budget and inside SC-004.
- FR-020 (stop when settled) is one line with TanStack Query
  (`refetchInterval: (query) => isFinal(query.state.data) ? false : 3000`), and the
  client already uses TanStack Query throughout.
- FR-021 (show "not live", keep retrying, recover) maps directly onto the query's
  `isError` / `fetchStatus: "paused"` states plus `refetchOnWindowFocus` and
  `refetchOnReconnect`, with no custom transport code.
- Cost is bounded: a payment window is 15 minutes, so a single guest generates at
  most ~300 cheap indexed reads on `idx_orders_order_number`.

**Alternatives considered**:

- *Server-Sent Events* — one connection instead of N requests, but needs in-process
  pub/sub between the webhook handler and open connections, a heartbeat, proxy
  buffering config, and reconnect handling. It also pins each guest to a
  long-lived connection. The constitution rules out Redis/queues, so the fan-out
  would be process-local — acceptable for a single-instance MVP but strictly more
  moving parts for a benefit the 10s budget does not need. Recorded as the upgrade
  path if the interval ever needs to drop below ~1s.
- *WebSocket* — same objections, plus a protocol upgrade and a new dependency.
- *Long polling* — holds a server goroutine per waiting guest for the whole payment
  window; worse than short polling at this scale.

---

## 5. "Check payment status" must reconcile with the provider

**Decision**: The button calls a **separate rate-limited POST endpoint** that reads
the authoritative status from the provider (`GET /v2/{order_id}/status`) and funnels
the result through the **same `MapProviderStatus` → `applyOutcome` path the webhook
uses**. The 3-second poll keeps hitting the cheap read-only GET.

**Rationale**:

- A button that only re-reads our own database is useless in exactly the situation
  it exists for: the webhook has not arrived. In local development the provider
  cannot reach `localhost` at all, so without provider reconciliation the whole
  paid-state flow is untestable end to end.
- Reusing `MapProviderStatus` and the existing guarded
  `UpdateStatusIfPending` transition means reconciliation is idempotent by
  construction — it cannot double-issue tickets or double-restore quota (FR-024,
  FR-025), and it needs no new correctness argument.
- Keeping it out of the polled GET matters: the poll must stay a single indexed
  read, never an outbound provider call every 3 seconds from every open page.

**Rate limit**: reuse `httpx.RateLimitPerIP`, already applied to the public ticket
lookup in [cmd/api/main.go:201](../../backend/cmd/api/main.go#L201). Budget: 1
request / 5s with a small burst; a 429 is surfaced as a visible cooldown on the
button rather than an error (FR-018).

**Alternatives considered**:

- *`GET /orders/:n?refresh=true`* — a mutating GET; also caches badly and would be
  hit by the poll.
- *Reconcile inside the poll every Nth tick* — hides an outbound network call
  inside an endpoint that must stay trivially cheap, and makes the rate limit
  depend on how long a page stays open.

---

## 6. Expiring an unpaid order and restoring its quota

**Decision**: Three cooperating paths, all funnelling into one idempotent
transition:

1. **Provider webhook** (`expire`) — already implemented and unchanged.
2. **Background sweeper** — an in-process ticker (default 30s) in the payment
   service that finds `PENDING` orders past `payment_expires_at` and applies the
   `EXPIRED` outcome with quota restoration.
3. **On-demand reconciliation** — the check-status endpoint force-expires an order
   already past its deadline, so the guest gets an authoritative answer instantly.

**Rationale**:

- SC-007 requires quota back within 1 minute of expiry. The webhook alone cannot
  guarantee that (and never fires in local development), and lazy expiry-on-read
  only fires for orders someone happens to be watching — an abandoned cart would
  hold quota forever, which is precisely the case that matters for inventory.
- A 30s ticker gives a ≤30s worst case, inside the 1-minute budget.
- All three call the same guarded transaction (`UPDATE ... WHERE status =
  'PENDING'` + `RestoreQuota`), so overlapping paths cannot restore twice — the
  same argument that already protects the webhook.
- The ticker is plain in-process Go, consistent with the existing post-payment
  goroutines; it introduces no queue, scheduler, or Redis (Constitution VI).

**Display rule**: the read endpoint reports the stored `status` plus
`payment_expires_at`, and the client shows the expired state as soon as the
countdown hits zero. The authoritative flip follows within ≤30s. This avoids the
order domain having to call into the payment domain on a read path.

**Alternatives considered**:

- *Rely on the provider webhook only* — fails SC-007 whenever the webhook is
  delayed, and makes the flow undemonstrable locally.
- *`pg_cron` / an external cron container* — new infrastructure for a 20-line
  ticker.
- *Expire lazily on read only* — leaves abandoned orders holding quota
  indefinitely.

---

## 7. Where the new state lives (schema)

**Decision**: Two nullable columns on `orders` — `payment_qr_string TEXT` and
`payment_expires_at TIMESTAMPTZ` — added in a new
`backend/migrations/0002_payment_qris.sql`, with `SCHEMA.md` updated in the same
change. `orders.payment_url` is retained and now stores the provider's
`generate-qr-code` action URL (fallback / audit), and `payment_provider` is
unchanged.

**Rationale**:

- The QR payload and deadline are per-order, single-valued, and read on every
  status poll — exactly like `payment_url`/`payment_provider`, which already sit on
  `orders`.
- `payment_expires_at` must be indexable for the sweeper's "due for expiry" query;
  a partial index `WHERE status = 'PENDING'` keeps it small.
- The constitution requires `SCHEMA.md` to be updated in the same change as any
  schema change — this is a documented amendment, not a silent drift.

**Migration mechanics** (important, easy to get wrong here):

- `docker-compose.yml` mounts `backend/migrations` at
  `/docker-entrypoint-initdb.d`, which Postgres runs **only when the data volume is
  empty**. A new `0002_*.sql` is picked up automatically on a fresh volume, and
  must be applied manually (`psql -f`) to an existing dev database.
- `sqlc.yaml` currently points `schema:` at the single file
  `migrations/0001_init.sql`; it must become a list including `0002_*.sql` or
  generation will not see the new columns.

**Alternatives considered**:

- *Store the instruction as a `payments` row* — that table is the append-only
  provider notification log; "current instruction" would become "latest row with
  the right status", which is more code and a worse query.
- *A new `payment_instructions` table* — a 1:1 table for two nullable columns, and
  a new table for a locked schema, with no second use case in sight.
- *Editing `0001_init.sql` in place* — silently diverges from any database already
  initialised from it.

---

## 8. Clock skew on the countdown

**Decision**: The status response carries `server_time` alongside
`payment_expires_at`. The client computes `offset = server_time - Date.now()` once
per fetch and renders `expires_at - (Date.now() + offset)`.

**Rationale**: SC-005 demands the countdown be within 2 seconds of the true
deadline regardless of the device clock. A device clock hours off would otherwise
show a wildly wrong or immediately-expired countdown. The offset is refreshed on
every poll, so it self-corrects.

**Alternative considered**: sending a `seconds_remaining` integer instead. It
becomes stale between polls and forces the client to tick it down anyway, which is
the same arithmetic with less information.

---

## 9. Checkout no longer redirects

**Decision**: `POST /api/v1/checkout` keeps its path and request body, but the
response drops the guest-facing redirect as the primary output: the client now
routes to `/orders/{order_number}` in-app. `payment_url` remains in the response
body for compatibility and is simply not used for navigation.

**Rationale**: FR-009. Keeping the field avoids a breaking contract change for a
response shape three prior specs already document; the behavioural change is
entirely on the client plus what the gateway now returns internally.

**Gateway contract change**: `Gateway.CreateTransaction` must return more than a
URL. It returns a `PaymentSession{ProviderRef, QRString, QRImageURL, ExpiresAt,
RedirectURL}` value instead. Both consumer-declared interfaces
(`payment.Gateway`, `order.PaymentGateway`) change together; the order domain still
knows nothing provider-specific (Constitution V).

---

## 10. Testing approach

**Decision**:

- **Go**: table tests for the new gateway mapping (charge request shape, response
  parsing, status read) against an `httptest.Server`, exactly as
  `midtrans_test.go` already does; service tests for the sweeper's idempotency
  (two concurrent expiry attempts restore quota once) and for reconciliation
  reusing the webhook's outcome path.
- **Frontend**: Vitest + Testing Library for the session guard's three states
  (loading renders no login screen, anonymous redirects with `next`, authenticated
  renders children) and for the countdown's skew handling; the polling-to-paid
  transition is covered with a mocked query client.
- **Manual**: the Midtrans sandbox QRIS simulator completes a payment without a
  real banking app — see [quickstart.md](./quickstart.md).

**Rationale**: mirrors the testing conventions already established across specs
001-003; no new test infrastructure.

---

## Sources

- [Midtrans — QRIS (Core API reference)](https://docs.midtrans.com/reference/qris)
- [Midtrans — GoPay QRIS POS Integration](https://docs.midtrans.com/docs/gopay-qris-pos-integration)
- [Midtrans — Custom Expiry Object](https://docs.midtrans.com/reference/custom-expiry-object)
- [Midtrans — GET Status API Requests](https://docs.midtrans.com/docs/get-status-api-requests)
- [Midtrans — Charge Transactions](https://docs.midtrans.com/reference/charge-transactions-1)

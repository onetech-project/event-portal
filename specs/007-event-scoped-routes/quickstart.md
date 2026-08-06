# Quickstart: Validating Event-Scoped Guest Navigation

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Routes**: [contracts/routes.md](./contracts/routes.md)

How to prove this feature works end to end. Each scenario names the requirement it covers,
so a failure points straight at the gap.

---

## Prerequisites

- PostgreSQL running via Docker Compose, migrations applied
- Backend on `:8080`, frontend on `:3000`
- At least one `PUBLISHED` event with a **future** `start_date` and one published ticket
  type with quota remaining
- Midtrans SNAP Sandbox credentials configured (scenarios 3–6)
- A working SMTP target for scenario 6 — MailHog or equivalent is enough

```bash
docker compose up -d db
cd backend && go run ./cmd/api
cd frontend && npm run dev
```

---

## Automated checks

```bash
# Backend — the public resend endpoint and its rate limiter
cd backend && go test ./internal/notification/...

# Backend — architecture rules and everything else
cd backend && go test ./...

# Frontend — framing, stage derivation, guards, confirmation
cd frontend && npx vitest run

# Frontend — types and lint
cd frontend && npx tsc --noEmit && npm run lint
```

`npx vitest run` is the invocation because `package.json` defines no `test` script; config
lives in `vitest.config.mts`.

**Grep guard** — the top-level order route must not exist and nothing may link to it:

```bash
cd frontend && ls "app/(public)/orders" 2>/dev/null && echo "FAIL: top-level orders route still present"
cd frontend && grep -rn 'href="/orders\|href={`/orders\|push(`/orders\|replace(`/orders' app components lib
```

Both should produce nothing.

**Untouched-domain guard** — the re-scope means `internal/ticket/` must not appear in the diff:

```bash
git diff --stat -- backend/internal/ticket frontend/app/\(public\)/tickets
```

Should be empty.

---

## Scenario 1 — Framing persists across the journey (FR-001, FR-002, FR-004, SC-001, SC-002)

1. Open `http://localhost:3000/events/<slug>`.
2. Confirm a countdown bar labelled **"Event starts in"** with DAYS/HOURS/MIN/SEC, ticking
   once per second, and the four-step rail below it with **Booking** current.
3. Note the seconds value. Select a ticket and continue to checkout.
4. **Expected**: the countdown is still there, still ticking, did **not** restart; the rail
   now shows Booking completed (checkmark) and **Registration** current.
5. Complete checkout to reach the payment screen. **Expected**: countdown continuous; rail
   shows Booking and Registration completed, **Payment** current.
6. Pay in the sandbox. **Expected**: you arrive at confirmation; rail shows the first three
   completed and **Done** current; the countdown is still present (FR-002).

**Fails if**: the countdown re-mounts (seconds jump backwards to a rounded value), blanks
during navigation, or the rail shows a stage that disagrees with the address.

---

## Scenario 2 — Framing edge states (FR-003, FR-007, FR-008)

1. Set an event's `start_date` to the past via admin; open `/events/<slug>`.
   **Expected**: no countdown at all — not a zeroed one. The rail and ticket rows still render.
2. Throttle the network (DevTools → Slow 3G) and reload a future-dated event.
   **Expected**: no `00 : 00 : 00` placeholder bar during the load; the rail appears
   immediately (it does not depend on the event); the countdown appears only with real values.
3. Open `/events/does-not-exist` and `/events/does-not-exist/orders/ANYTHING`.
   **Expected on both**: a clear "event not found" state with **no** nested content behind
   or below it — no ticket list, no order panel, no countdown.

---

## Scenario 3 — The journey is event-scoped (FR-010, FR-011, FR-012, SC-003, SC-004)

1. From `/events/<slug>`, select tickets and press continue.
2. **Expected**: the address is `/events/<slug>/checkout?t=…` — slug in the **path**, no
   `?slug=` query parameter anywhere.
3. Complete the form and submit.
4. **Expected**: you land on `/events/<slug>/orders/<orderNumber>` with the QRIS panel.

**Fails if**: the URL still carries `?slug=`, or the redirect lands on a top-level `/orders/<n>`.

---

## Scenario 4 — Both countdowns, unmistakably labelled (FR-017, SC-008)

On the payment screen from scenario 3, with payment still pending:

1. **Expected**: two countdowns — the event bar at the top and the payment timer inside the
   "Scan to pay" card.
2. Each names what it counts down to: **"Event starts in"** vs. **"Pay within"**.
3. They differ by more than position — the event countdown uses brand-coloured unit blocks
   with unit captions; the payment timer is inline text that turns red under 60 seconds.
4. Ask someone who has not read this document which number is the payment deadline.
   **Expected**: correct answer without hesitation.

---

## Scenario 5 — Order ownership guard (FR-013, SC-005)

With a real order number `N` belonging to event `A`:

1. Open `/events/<slug-A>/orders/N`. **Expected**: renders normally.
2. Open `/events/<slug-B>/orders/N` for a different event `B`.
3. **Expected**: "Order not found for this event", a link back into event `B`, **no**
   redirect to `A`, and none of order `N`'s details visible.
4. Repeat both against `/events/<slug>/orders/N/done`. **Expected**: same behaviour.

---

## Scenario 6 — Confirmation screen (FR-016, FR-018, FR-019, FR-020, FR-021, FR-022, SC-006)

1. From a pending order screen, pay in the sandbox and leave the tab open.
   **Expected**: within a few seconds you are moved to `/events/<slug>/orders/<n>/done`
   without reloading — the existing poll observes the settled status (FR-018).
2. **Expected on the screen**: a success message naming the event, the order number, the
   total paid, every purchased item with its quantity, and a notice that a confirmation
   email was sent.
3. Press the browser back button. **Expected**: you do **not** bounce between the payment
   screen and confirmation — the forward used `replace`.
4. Re-open `/events/<slug>/orders/<n>` directly. **Expected**: you are sent to `done` again
   rather than shown payment instructions.
5. Press **Resend Email**. **Expected**: the screen confirms it and the ticket email
   arrives again at the buyer's address.
6. Press **Back to Home**. **Expected**: you land on `/`.
7. Let a different order expire without paying, then open its order address.
   **Expected**: you reach `done`, which reports the expired outcome — not a success
   message — and offers a way back to the event (FR-020).

---

## Scenario 7 — Resend limits and disclosure (FR-023, FR-024, FR-025, FR-026, SC-007)

```bash
# 1. First call for a paid order — 202, one email sent
curl -si -X POST localhost:8080/api/v1/orders/<ORDER_NUMBER>/resend-email | head -1

# 2. Immediately again — 429, no second email
curl -si -X POST localhost:8080/api/v1/orders/<ORDER_NUMBER>/resend-email | head -1

# 3. A number that does not exist — must be byte-identical to call 1's body
curl -s -X POST localhost:8080/api/v1/orders/ORD-00000000-DEADBEEF/resend-email

# 4. A body naming another address must change nothing
curl -s -X POST localhost:8080/api/v1/orders/<ORDER_NUMBER>/resend-email \
  -H 'Content-Type: application/json' -d '{"email":"attacker@example.com"}'

# 5. A different order number is not throttled by step 1
curl -si -X POST localhost:8080/api/v1/orders/<OTHER_ORDER>/resend-email | head -1
```

**Expected**: (1) `202`, (2) `429`, (3) `202` with the same body as (1), (4) no mail to the
supplied address — check the SMTP catcher, (5) `202`.

**Also confirm**: order polling is unaffected. Sit on a pending order screen for a minute
and watch the network tab — the 3-second poll must never be rate-limited, which proves the
limiter is attached to the resend route only, not to the API group.

**And**: the admin resend still works unchanged at
`POST /api/v1/admin/orders/:id/resend-email` with a valid JWT.

---

## Scenario 8 — Ticket lookup is untouched (FR-027, SC-009)

1. From the home screen press **"Look up a ticket"**. **Expected**: `/tickets`, the same
   code box as before.
2. Enter a valid code. **Expected**: `/tickets/<code>` with the ticket detail — no event
   slug anywhere, no event selection step, exactly as before this feature.
3. Confirm the ticket API response is unchanged:
   ```bash
   curl -s localhost:8080/api/v1/tickets/<CODE> | jq
   ```
   **Expected**: `ticket_code`, `status`, `event_name`, `attendee_name` — and **no**
   `event_slug`. That field belonged to a removed revision of this feature.

---

## Scenario 9 — Nothing regressed (FR-015, SC-004)

1. On a pending order, press "check payment status". **Expected**: the refresh mutation
   fires and the panel updates, as before.
2. Let a payment window expire on-screen. **Expected**: the QRIS code is hidden and the
   guest is moved to `done` showing the expired outcome.
3. Confirm the emailed PDF still arrives with a scannable QR.
4. Open an admin screen and confirm nothing there changed.

---

## Constitution spot-check

- `git diff --stat` touches only `frontend/**`, `backend/internal/notification/**`, and
  `backend/cmd/api/**` — **not** `backend/internal/ticket/**`.
- `migrations/`, `SCHEMA.md`, and every `*/queries/*.sql` are unchanged; `sqlc generate`
  produces no diff.
- `frontend/package.json` and `backend/go.mod` gained no dependency — the rate limiter is
  Echo's own middleware.
- `backend/cmd/api/architecture_test.go` passes: no domain imports another, no `sqlc`
  struct reaches the transport layer, the domain set is unchanged.
- The event query still carries `cache: "no-store"` and `staleTime: 0` — navigate
  `/events/:slug` → checkout → back and confirm the event is re-requested rather than
  served from a long-lived cache.

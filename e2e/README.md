# UAT automation (Playwright)

Full end-to-end acceptance tests: a real browser driving the real Next.js app,
against the real Go API, against real PostgreSQL and Redis.

The only thing substituted is the payment provider — and even that is replaced at
the network boundary, so the API still speaks the Midtrans Core API protocol and
settlement still arrives as a correctly signed webhook that the production
handler verifies. Nothing in this suite writes an order status directly.

## What it covers

| Spec | Scenarios |
|---|---|
| [`guest-purchase.spec.ts`](specs/guest-purchase.spec.ts) | Browse → select → agree to terms → book → holder forms → QRIS → settlement → confirmation → issued tickets. Plus ticket lookup, hold expiry returning quota, a tampered webhook signature, and a replayed settlement. |
| [`admin-console.spec.ts`](specs/admin-console.spec.ts) | Sign in, wrong password, unauthenticated redirect, a paid order reaching the order list, ticket validation including the irreversible "Mark used" transition, unknown code, and the delete guard on an event with orders. |
| [`cache-refresh.spec.ts`](specs/cache-refresh.spec.ts) | The read cache (spec 012) staying invisible: newly published events appearing immediately, unpublish removing them, quota moving on booking, per-event scoping, hit counting, operator flush, and health reporting. |

## Prerequisites

PostgreSQL and Redis are containers and are **not** managed by the test runner —
owning their lifecycle from a test process makes failures much harder to read.
Bring them up first:

```bash
# From the repository root.
# REDIS_PORT is offset because 6379 is often already taken locally.
REDIS_PORT=6380 docker compose up -d postgres redis
docker compose run --rm migrate up
```

Then install once:

```bash
cd e2e
npm install
npm run install-browsers
```

The frontend's dependencies must also be installed (`cd frontend && npm install`),
because the suite runs `next dev` from that directory.

## Running

```bash
cd e2e
npm test                 # headless, all specs
npm run test:headed      # watch it drive the browser
npm run test:slow        # headed, slowed to human speed (E2E_SLOW_MO=500)
npm run test:ui          # Playwright's interactive runner
npm run report           # open the HTML report from the last run

npx playwright test --grep "Guest purchase"        # one describe block
npx playwright test specs/cache-refresh.spec.ts    # one file
```

### Watching the flow

`E2E_SLOW_MO` pauses that many milliseconds before every browser operation, so
the run is followable by eye. Pick your own pace:

```bash
E2E_SLOW_MO=250 npx playwright test --headed --grep "Guest purchase"
```

The per-test, expect, action and navigation timeouts all scale with it, and so
does the API's `BOOKING_HOLD` — otherwise a checkout you are watching would run
past its 30-second seat hold and fail for reasons unrelated to the code. Leave
it unset (or 0) for normal and CI runs; nothing changes at full speed.

The runner starts three processes itself and shuts them down afterwards:

| Process | Port | Notes |
|---|---|---|
| Midtrans stub | 8101 | [`support/gateway-stub.ts`](support/gateway-stub.ts) |
| Go API | 8100 | `go run ./cmd/api`, so it always tests the working tree |
| Next.js | 3100 | `next dev`, because `NEXT_PUBLIC_*` is baked at build time |

Ports are offset from the normal dev ones (8080/3000) so a run never collides
with a stack you already have open.

## How payment is driven

`POST /v2/charge` and `GET /v2/:order/status` are served by the stub, which
returns the shapes `internal/payment/midtrans.go` parses — including a non-empty
`qr_string` (checkout deliberately fails without one) and a Jakarta-local
`expiry_time`.

Settlement is a signed notification, not a database write:

```
signature = sha512(order_id + status_code + gross_amount + server_key)
```

`support/payment.ts` reproduces that, so `settleOrder()` exercises the real
verification, the real status transition, ticket issuance, the PDF and email
dispatch, the SSE push that moves the browser to the confirmation screen, and the
cache invalidation — all of it. That is what makes this end to end rather than a
UI walkthrough that stops at the QR code.

`deliverNotification({ tamperSignature: true })` proves the rejection path.

## Safety

The suite TRUNCATEs every application table before each test.
[`support/env.ts`](support/env.ts) refuses to run if `E2E_DATABASE_URL` points at
a non-local host or looks production-shaped. That guard is a hard failure, not a
warning — a UAT suite that can be aimed at production by a stray environment
variable is a liability rather than a test.

`genders` and `order_statuses` are deliberately left intact: they are master data
seeded by migration, and the visitor form and order queries depend on them.

## Configuration

Everything is overridable by environment variable; the defaults match the
prerequisites above.

| Variable | Default |
|---|---|
| `E2E_FRONTEND_URL` | `http://localhost:3100` |
| `E2E_API_URL` | `http://localhost:8100/api/v1` |
| `E2E_GATEWAY_URL` | `http://localhost:8101` |
| `E2E_DATABASE_URL` | `postgres://ticketing:ticketing@localhost:5433/ticketing?sslmode=disable` |
| `E2E_REDIS_URL` | `redis://localhost:6380/0` |
| `E2E_CACHE_ENABLED` | `true` — set `false` to run the suite against a cache-free API |
| `E2E_MANAGE_SERVERS` | `true` — set `false` to point at servers you started yourself |
| `E2E_ADMIN_EMAIL` / `E2E_ADMIN_PASSWORD` | `uat@example.com` / `uat-password-2026` |

The admin password's bcrypt hash is precomputed in `support/env.ts` so seeding
needs no bcrypt dependency and no Go toolchain. Change the password and you must
regenerate the hash.

## Writing new scenarios

Selectors go through roles and accessible names
([`support/journey.ts`](support/journey.ts)), not CSS paths. The app has almost no
test ids, and that turns out to be a feature: a change that breaks these
selectors is usually a change that broke accessibility too.

Two rules worth keeping:

- **Arrange through the API, not through SQL.** `support/api.ts` creates events
  and ticket types the way an admin would. Since those writes are also what
  invalidate the cache, arranging through them keeps the setup honest.
- **Assert on exact labels.** An earlier version of the validation test asserted
  `/used/i` and passed by matching the "Mark used" *button* on a still-valid
  card — the transition never happened. Loose regexes produce false greens, which
  are worse than no test at all.

## Known constraints

- Specs run serially (`workers: 1`). Each one truncates the database, so they
  cannot share it. This is correctness, not a tuning oversight.
- The `/healthz` degraded branch is asserted in the Go unit tests rather than
  here: taking Redis down mid-run would disrupt every other browser scenario.
- Email delivery is asserted via `orders.email_sent` rather than by reading a
  mailbox. Point `E2E_SMTP_HOST` at Mailpit (`docker compose up -d mailpit`) if
  you want to inspect the messages by hand.

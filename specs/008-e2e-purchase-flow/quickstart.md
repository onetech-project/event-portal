# Quickstart Validation: End-to-End Guest Purchase Flow

**Feature**: 008-e2e-purchase-flow · Proves the journey in
[spec.md](./spec.md) against the contracts in [contracts/](./contracts/).

## Prerequisites

```bash
docker compose up -d postgres          # DB
cd backend && go run ./cmd/api         # API on :8080 (reads .env; Midtrans sandbox keys required)
cd frontend && npm run dev             # Next.js on :3000
```

Migration + seed: migration `000005` applies via the existing migration flow on
API start; verify with `psql … -c '\d event_terms'`. Admin login seeded via
`go run ./cmd/seedadmin`. SMTP: use the compose mailpit/mailhog service (or
sandbox SMTP creds in `.env`) to observe emails.

Config for fast validation (optional): `BOOKING_HOLD=3m PAYMENT_WINDOW=14m
QR_REFRESH_AFTER=1m PAYMENT_SWEEP_INTERVAL=10s`.

## Scenario 1 — Admin authors CMS content (spec US4)

1. `http://localhost:3000/admin` → login.
2. Edit an event: write formatted description in the WYSIWYG; add ≥1 activity
   (with icon), guest star, guideline; author Terms & Conditions.
3. Expected: saves succeed; `GET /api/v1/event/:id` returns
   `description` (sanitized HTML), blocks in position order, `has_terms: true`; pasting
   `<script>alert(1)</script>` into the editor is stripped on save (SC-007).

## Scenario 2 — Homepage & event detail (US3, US4)

1. Open `http://localhost:3000/`.
2. Expected: event grid (no two-button chooser); aside card looks up a ticket
   code; clicking an event opens `/events/:slug` showing the authored content —
   not the ticket list; "Buy Ticket" → `/events/:slug/tickets`.

## Scenario 3 — Booking with 1-hour hold (US1)

1. On `/events/:slug/tickets` select tickets (and a package), click Buy Ticket.
2. Expected: dialog shows the CMS terms (not static copy, via
   `GET /api/v1/ticket/terms-condition/:event_id`); Agree fires
   `POST /api/v1/ticket/book` then `POST /api/v1/ticket/terms-condition/:order_id`
   → lands on `/events/:slug/orders/:orderNumber`.
3. Verify in DB: order `PENDING`, `payment_qr_string IS NULL`, buyer fields
   NULL, `terms_agreed_at` set, `payment_expires_at ≈ now()+1h` (or
   `BOOKING_HOLD`); `ticket_types.quota` decremented; attendee slots exist
   with NULL details.
4. Negative: dismiss dialog → no order row; event without terms → Buy Ticket
   blocked (`409 TERMS_MISSING` path); quota exhausted → `INSUFFICIENT_QUOTA`.

## Scenario 4 — Hold expiry restores quota (US1)

1. Book, then let `BOOKING_HOLD` lapse without paying.
2. Expected: within one sweep interval order → `EXPIRED`, quota restored;
   revisiting the order URL shows the expired state (Figma 288-2295) with a
   restart link.

## Scenario 5 — Visitor forms (US5)

1. On the fresh order page: hold countdown visible; fill buyer + per-slot
   visitor details (name/email/phone/DOB/gender).
2. Expected: invalid fields error inline; forms submit only with "Continue to
   Payment" (`POST /api/v1/ticket/checkout/:order_id` carries them — Option B);
   a reload before that shows the held order with **empty** forms; incomplete
   forms are rejected with a 400 field map if forced via curl.

## Scenario 6 — Payment window, QR refresh, live status (US2)

1. Continue to payment.
2. Expected: QR renders in-app; countdown now ≈ `PAYMENT_WINDOW`; **event
   countdown from the shared layout still visible** (FR-019); at
   `QR_REFRESH_AFTER` the QR swaps automatically (network tab:
   `POST …/ticket/checkout/:order_id/refresh-qr`; DB: new `payment_qr_string`,
   `payment_expires_at` unchanged).
3. Pay via Midtrans sandbox QRIS simulator → page flips to paid via SSE
   (`GET …/ticket/checkout/:order_id/status` in network tab) and redirects to
   `…/done` within 5s (SC-005).
4. Negative: let the window lapse → expired payment state, quota restored;
   kill the SSE connection (devtools offline toggle) → polling fallback still
   detects payment.

## Scenario 7 — Done page + email + resend (US6)

1. After payment: `…/done` shows the finished-order summary (Figma 47-2396).
2. Expected: exactly one email (receipt + PDF e-ticket per attendee, JIVE
   design) in mailpit; `orders.email_sent=true`; resend button delivers again;
   resend on an unpaid order → refused; duplicate webhook replay (re-POST
   sandbox notification) → no second email/tickets (SC-008).

## Scenario 8 — Concurrency (SC-003)

```bash
# two parallel bookings for the last remaining ticket
seq 2 | xargs -P2 -I{} curl -s -X POST localhost:8080/api/v1/ticket/book -H 'Content-Type: application/json' -d @book-last-ticket.json
```
Expected: exactly one `201`, one `INSUFFICIENT_QUOTA`; quota never negative.

## Automated gates

```bash
cd backend  && go test ./...        # includes architecture tests (domain isolation)
cd frontend && npm test && npm run lint && npx tsc --noEmit
```

## Validation run — 2026-08-05 (T046)

Environment: docker `ticketing-postgres` + `ticketing-mailpit`; API built from
branch and run with `MIDTRANS_BASE_URL` pointed at a local Core-API stub
(`/v2/charge` + `/v2/:order/status`), `BOOKING_HOLD=25s`,
`PAYMENT_SWEEP_INTERVAL=3s`. Executed with curl against `:8089`; UI states
covered by the vitest suites. Seed data (events `qa-quickstart-008`,
`qa-no-terms-008`, admin `qa008@example.com`) left in the dev DB for manual
inspection.

| # | Scenario | Result |
|---|----------|--------|
| 1 | Admin CMS content | ✅ terms/activities/guest-stars/guidelines saved; `<script>` stripped on write (event description AND terms); unknown icon → `400004`; detail returns blocks + `has_terms:true` |
| 2 | Homepage & detail | ✅ `GET /event` grid + content-only detail (API); grid/verify-card/detail UI covered by vitest |
| 3 | Booking + hold | ✅ `POST /ticket/book` → PENDING, `payment_qr_string` NULL, buyer NULL, `terms_agreed_at` set, hold = BOOKING_HOLD, quota 10→8, 2 empty slots; no-terms event → `409001`; over-quota → `400002` |
| 4 | Hold expiry | ✅ order → EXPIRED within one sweep; quota 8→10; FR-009 log carries order identity + `restored_quota` `ttID:+2` |
| 5 | Visitor forms | ✅ revisit shows empty slots (Option B); bad forms → `400001` with per-field map (`attendees[0].dob` …); a lapsed hold during form entry → `410001` |
| 6 | Payment window + QR | ✅ checkout → QR + 14-min window + `qr_refresh_after_seconds:420`; `qris.png` renders (image/png); `refresh-qr` re-issues `-R1` ref with deadline unchanged; SSE streamed `PENDING` → `PAID` frames; webhook (signed) settled the order |
| 7 | Email + resend | ✅ exactly one email w/ 1 PDF attachment on settle; `email_sent=t`; valid-signature webhook replay → 200, no second email; tampered signature → `401001`; `POST /ticket/resend-email` `{order_id}` → 202 + second email; immediate repeat → `429001` (per-order bucket); unpaid order → same 202, zero mail |
| 8 | Concurrency | ✅ two parallel books for the last ticket: one `201`, one `400002`; quota never negative (ends 0) |

Automated gates: `go test ./...` all packages ok; frontend 202 vitest tests, 0
eslint issues, `tsc --noEmit` clean.

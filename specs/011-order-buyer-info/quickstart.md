# Quickstart: Validating Order Page Buyer Information — Per-Ticket Holder Forms

**Feature**: 011-order-buyer-info · Validation guide (no implementation code here — see [plan.md](plan.md), [contracts/checkout-and-delivery.md](contracts/checkout-and-delivery.md), [data-model.md](data-model.md)).

## Prerequisites

- Docker Compose PostgreSQL up with migrations through `000013` applied; backend `.env` with Midtrans sandbox keys + SMTP (use MailHog/Mailpit or a real inbox — the delivery scenario needs **multiple distinct addresses** you can read, to prove only the buyer's is written to).
- Seeded: one published event with authored T&C, ≥2 ticket types, and one active package/bundle whose composition spans ≥2 tickets; active fees (default seed: `PPN (11%)`, `Admin Fee`).
- Backend: `cd backend && go run ./cmd/api` (or the compose service). Frontend: see project memory `frontend-toolchain-invocation.md` for running `next dev` / `tsc` / vitest (node not on PATH; vitest is installed at `node_modules/.bin/vitest` but there is no `test` script — invoke the binary directly).

## Scenario A — Form count, ordering, and no buyer card (spec US1 / SC-001)

Verify each SC-001 shape; in every case **no "Buyer contact" card appears anywhere** and every field label carries the required marker (asterisk / `aria-required`):

1. **Day 1 × 1** → exactly **1 form**.
2. **Day 1 × 10** → exactly **10 forms**, all individually fillable; scroll the list and confirm the order summary stays reachable (edge case, spec).
3. **Day 1 × 2 + Day 2 × 3** → exactly **5 forms**.
4. **1 bundle unit + 2 different standalone tickets** → exactly **3 forms**, in canonical slot order: the **two standalone-ticket forms first, then the bundle form** titled with the bundle name + ticket-count badge (standalone groups precede bundle groups per `ORDER BY package_id NULLS FIRST`).
5. The FIRST holder card (and only the first) carries the delivery-notice chip at the top right of its header: "The invoice and e-ticket will be sent via email" (Figma 206-1804; clarified 2026-08-06 — no page-top banner).

## Scenario B — Validation gates the button (US2 / SC-002)

1. With any held order open: **Expect** Continue to Payment **disabled** initially.
2. Enter `user@` as an email, blur → inline error on that field; button stays disabled. `user@email.com` clears it.
3. Phone: free text, digits only. `081234` (too short) → inline "Enter a phone number of 10-15 digits."; `081234567890` passes, and so does `628123456789` — both forms are accepted and each is stored exactly as typed. Letters and punctuation never appear at all: type `abc0812-345 def6789` and watch it land on `08123456789`. Typing past 15 digits is ignored. Mobile keyboards show the numeric layout via `inputMode`.
4. Leave one form's Gender and DOB untouched, fill everything else, then **click the (disabled) Continue button area** → the click-capture reveal runs validation and the untouched fields show their inline errors (US2-AS5 mechanism, research R5). A whitespace-only name and a future DOB also show errors.
5. Fill every field of every form validly → **button enables**; blank one field → disables again.
6. API-level check (bypass UI): `POST /api/v1/ticket/checkout/{ORD-…}` with a 9-digit phone → `400` envelope code `400001`, field map keyed `attendees[i].phone`. A body **without** `buyer_*` fields succeeds; a body **with** stale `buyer_*` fields is accepted (ignored).

## Scenario C — Checkout, primary contact, payment (US2 / FR-010, FR-017)

1. Click the enabled button → the guest lands on the QR screen at its **own address**, `/events/{slug}/orders/{ORD-…}/checkout` (`payment_started: true`); Booking ID unchanged. Press Back: it must NOT return to the forms and bounce forward again (FR-021 — both hops replace).
2. DB check: `SELECT buyer_name, buyer_email, buyer_phone FROM orders WHERE order_number = 'ORD-…';` → equals the **topmost form's** values (first canonical slot group — with a mixed order that is the first standalone form on screen). Confirm the columns `buyer_dob`/`buyer_gender` no longer exist.
3. Gender integrity: `SELECT a.gender_id, g.name FROM attendees a JOIN genders g ON g.id = a.gender_id WHERE a.order_id = …;` → every filled slot has a non-NULL `gender_id` resolving to the submitted name; `GET /ticket/order/{ORD-…}` still returns `slots[].gender` as the name.
4. Single-bundle-only variant: book **1 bundle unit alone** → exactly 1 form; after checkout, `orders.buyer_*` equals that bundle form's holder (edge case: bundle holder is primary contact).
5. Midtrans sandbox dashboard (or gateway request log): customer name/email/phone = the topmost holder.
6. `GET /api/v1/ticket/order/{ORD-…}` → response contains **no** `buyer_name`/`buyer_email` keys.

## Scenario D — Order summary card (US4 / SC-005)

On both screens — the holder forms (`…/orders/{ORD-…}`) and the QR screen (`…/orders/{ORD-…}/checkout`):

- Event name + date shown. (FR-013's Booking ID and FR-014's per-unit price were removed from the panel by hand to match the design — restore them or amend the spec; the UI and the spec currently disagree.)
- Each line: name, `x{qty}`, and line subtotal.
- Registration phase: NO itemized fee rows (clarified 2026-08-06, constitution v2.1.0) — only "Total Payment" with its "includes all taxes and fees" note.
- QR (awaiting-payment) phase breakdown: "Ticket Total" = pre-fee subtotal; one row per fee (`PPN (11%)`, `Admin Fee` — collectively the spec's "Tax & Service Fee" per the recorded label mapping); "Total Payment" = subtotal + Σ fees (the spec's "Grand Total Payment").
- QRIS shown selected, not changeable (registration phase).

## Scenario E — Delivery to the buyer (US3 / SC-003)

1. Use an order with 3 forms and **3 distinct emails** (form 1 = buyer A, then B, C). Pay via Midtrans sandbox QRIS (or simulate the `settlement` webhook with a valid SHA-512 signature).
2. **Expect exactly 1 email**, to **A only**. Mailpit must show nothing for B or C. A's message carries **every** ticket in the order in one PDF (total pages = total tickets, all codes distinct) and a body with the full order receipt (Inv #, all line items + fee breakdown, Total Payment), every holder's name in the TICKET HOLDER block, and one e-ticket card per ticket.
3. Buyer-repeat variant: make form 2 reuse A's address → still exactly **one** email; the repeat is not a second delivery.
4. DB: `email_sent = TRUE` only after that send succeeded; a failing mailer leaves it FALSE with resend re-armed (covered by the notification unit tests).
5. Replay the webhook → no duplicate email (idempotency short-circuit).

## Scenario F — Resend (edge case + contract §4)

1. Guest: `POST /api/v1/ticket/resend-email {"order_id":"ORD-…"}` → 202 uniform body; the **buyer** receives the same single email again, and no other holder does.
2. Admin: `POST /api/v1/admin/orders/{uuid}/resend-email` (JWT) → `data.sent_to` is the buyer's address as a **string**; the admin orders page toast renders it.

## Scenario G — End-of-journey modal (US6 / FR-022 – FR-024)

Run this twice: once on the **forms** address, once on the **QR** address. The expected outcome is identical on both — that sameness is the requirement.

1. Open the screen and half-fill it (forms: type into two holder cards; QR: let the QR render). Force the order to end: `UPDATE orders SET payment_expires_at = now() - interval '1 minute' WHERE order_number = 'ORD-…';` and let the sweeper run, or set the status directly.
2. **Expect** the page *behind* to be exactly as it was — progress rail, the forms with what you typed still in them (or the payment panel), and the order summary — visible and dimmed. It must **not** be replaced by a full-page card.
3. The dialog shows: alert icon in a tinted circle, "Time's Up", body copy that does **not** tell the guest to repeat the order, and **exactly one** full-width button reading "Return to Home Page". Confirm there is no second button and no X control anywhere on it.
4. Try all three dismissal routes: press **Escape**, click the **backdrop**, and **scroll** the page. Nothing happens on all three — the dialog stays, the page behind neither scrolls nor takes focus. Tab repeatedly: focus must cycle inside the dialog and never reach the form fields behind it.
5. The browser **Back button still works** — the modal blocks the page, not the browser.
6. QR screen only: watch the countdown hit zero with the order still PENDING server-side. **Expect** the modal at that instant, the QR gone from behind it, and no inline "This payment code has expired" notice. When the server's EXPIRED status arrives seconds later, nothing on screen changes — one expiry, one message.
7. Cancelled variant: `UPDATE orders SET status_id = (SELECT id FROM order_statuses WHERE name='CANCELLED') WHERE …` → same dialog, same single button, cancellation wording.

## Scenario H — Schema revision is invisible above storage (FR-025 – FR-027 / SC-007)

Run against a database that **already holds** orders, holders, and packages — a freshly seeded one proves nothing about the conversion.

1. Before migrating, capture the truth to compare against:
   `SELECT order_number, status FROM orders ORDER BY order_number;` and
   `SELECT a.id, g.name FROM attendees a LEFT JOIN genders g ON g.id = a.gender_id ORDER BY a.id;`
2. Apply `000013`. Re-run both queries. **Expect byte-identical output** — every order on the same status name, every holder on the same gender name. Any drift here is FR-025's silent-rebinding failure.
3. Key types changed underneath: `\d order_statuses` and `\d genders` show integer identity primary keys; `\d orders` shows `status_id smallint NOT NULL` and no `status` column; `\d attendees` shows `gender_id smallint`.
4. Seeded ids are the fixed ones R20 depends on: `SELECT id, name FROM order_statuses ORDER BY id;` → exactly `1 PENDING, 2 PAID, 3 CANCELLED, 4 EXPIRED`.
5. The partial index survived with a literal predicate: `SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_orders_payment_expiry';` → `… WHERE (status_id = 1)`. Then confirm it is actually used: `EXPLAIN SELECT id FROM orders WHERE status_id = 1 AND payment_expires_at < now();` shows an index scan, not a seq scan.
6. Uniqueness is enforced, not merely intended (FR-027): `INSERT INTO genders (name, is_active, created_by) VALUES ('MALE', true, 'SYSTEM');` → **rejected** by the unique constraint. Same for a duplicate order-status name.
7. Authorship backfilled (FR-028): `SELECT name, created_by FROM order_statuses UNION ALL SELECT name, created_by FROM genders;` → every row reads `SYSTEM`, none NULL.
8. Wire unchanged (SC-007): `GET /api/v1/ticket/order/{ORD-…}` still returns `"status": "PENDING"`, the SSE status frame still carries the name, and the admin order list still filters by `?status=PAID`. **No numeric status appears in any response.**
9. Deactivation does not orphan (edge case): `UPDATE genders SET is_active = false WHERE name = 'MALE';` → existing holders still resolve to `MALE` on read, and `GET /ticket/genders` stops offering it. Re-activate afterwards.
10. Roll back and forward once: `migrate down 1` then `up 1`. The down step restores `orders.status` as a name and `packages.status`; note the master-list uuids are regenerated (documented lossy step, data-model §7.2) — order statuses and gender names must still be correct after the round trip.

## Scenario I — Package availability is a boolean end to end (FR-029 / SC-008)

1. Admin UI → edit a package. **Expect** a checkbox/toggle labelled active, not an ACTIVE/INACTIVE dropdown.
2. Untick it and save. `GET /api/v1/admin/packages/:id` → `"is_active": false`, and **no** `"status"` key anywhere in the response.
3. Public booking page for that event → the package is **gone** from the list (the server-side filter now reads `p.is_active`). Re-tick it → it reappears.
4. DB: `\d packages` shows `is_active boolean NOT NULL DEFAULT true` and no `status` column or CHECK.
5. Tickets are untouched (the exclusion FR-029 spells out): `\d tickets` still shows `status VARCHAR(50) … CHECK (status IN ('ACTIVE','USED','REVOKED'))`. Scan a valid pass at `POST /api/v1/admin/tickets/validate` → `VALID`; scan the same pass again → `ALREADY_USED`, **not** the same answer as a revoked pass. A boolean here would have collapsed those two into one.

## Automated checks

- Backend: `cd backend && ./scripts/test.sh ./...` plus `go build ./... && go vet ./...`. The notification suite encodes the single-recipient contract directly: its fixture's two holders both have addresses that are NOT the buyer's, so a regression back to fan-out fails immediately rather than passing vacuously.
- Frontend: `./node_modules/.bin/vitest run` and `./node_modules/.bin/next build` (see project memory `frontend-toolchain-invocation.md`; node is not on PATH and there is no `test` script). `checkout/page.test.tsx` covers the QR screen and the FR-021 forwards; `booking-stage.test.ts` pins one rail stage per address.
- Track A starting signal: the two existing "Time's Up" tests (`page.test.tsx:157`, `checkout/page.test.tsx:236`) **assert a `repeat order` link that FR-024 deletes**, so they fail the moment the modal lands — that failure is expected, not a regression.
- **Rev. 3 additions**: the modal's three sealed dismissal routes (Escape / backdrop / scroll) and its single-button content belong in a component test, not only in Scenario G — a manual-only check will not survive a Base UI upgrade that adds a dismissal reason. Assert on the *page behind* too: the forms must still be in the DOM with their typed values, which is what distinguishes FR-022 from the old full-page swap. On the backend, the `packages.is_active` conversion needs a test that an inactive package is absent from the public booking list, since that filter is the only thing standing between a retired bundle and a guest buying it.

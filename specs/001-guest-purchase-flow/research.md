# Phase 0 Research: Guest Purchase Flow

No `NEEDS CLARIFICATION` markers remain from the spec or Technical Context — all
technology choices are fixed by PRD.md/ARCHITECTURE.md. This document records the
key implementation decisions and alternatives considered.

## Quota semantics (`ticket_types.quota` is REMAINING quota)

- **Decision**: `ticket_types.quota` holds the **remaining** quota, not an original
  capacity. It is decremented at checkout and incremented back on
  cancel/expire/deny/failure and on checkout compensation. SCHEMA.md is locked, so
  there is no `quota_total`, `quota_sold`, or `quota_reserved` column and none may be
  proposed. The guest-facing `quota_remaining` field in `contracts/api.md` maps
  directly and 1:1 onto this column with no arithmetic.
- **Rationale**: One live counter with a `CHECK (quota >= 0)` backstop is the
  simplest overselling-proof design available under the locked schema. Making the
  semantics explicit here prevents downstream features (notably Admin Management,
  spec 002, which edits ticket types) from mistaking the column for a static total —
  writing a "total" into it would silently resurrect or destroy availability.
- **Consequences to respect elsewhere**: an admin editing a ticket type is editing
  *remaining* stock; the number of tickets originally on sale is not recoverable from
  stored data (no sold/total counters exist in this MVP).
- **Alternatives considered**: separate total/sold columns with remaining computed as
  a difference (rejected — SCHEMA.md is locked); a materialized "sold" view derived
  from `order_items` (rejected — would double-count PENDING vs PAID orders and adds a
  cross-domain read for every quota check).

## Quota deduction under concurrency

- **Decision**: Deduct quota with a single guarded SQL statement inside the checkout
  transaction: `UPDATE ticket_types SET quota = quota - $qty WHERE id = $id AND
  quota >= $qty RETURNING quota`. If no row is returned/updated, abort the
  transaction and return an insufficient-quota error. Restoration is the mirror
  statement: `UPDATE ticket_types SET quota = quota + $qty WHERE id = $id`.
- **Rationale**: Relies on Postgres row-level locking implicit in `UPDATE` plus the
  existing `CHECK (quota >= 0)` constraint as a hard backstop; avoids
  read-then-write races without needing `SELECT ... FOR UPDATE` plus a separate
  check, and needs no application-level locking or advisory locks.
- **Alternatives considered**: Optimistic locking with a version column (rejected —
  adds a column not in SCHEMA.md and requires retry logic); `SELECT ... FOR UPDATE`
  then application-side check-and-update (rejected — two round trips, no benefit
  over the guarded single-statement UPDATE).

## Checkout transaction shape

- **Decision**: Never hold a database transaction open across the payment gateway's
  HTTP round-trip. Checkout is split into **TX1 → gateway call (no transaction) →
  TX2**, with a compensating transaction on gateway failure:

  1. **TX1 (`pgx.Tx`)** — validate each ticket type's sales window; guarded quota
     deduct per line item (`UPDATE ticket_types SET quota = quota - $qty WHERE id =
     $id AND quota >= $qty RETURNING quota`); insert `orders` with `status =
     'PENDING'` and `payment_url = NULL`; insert `order_items`; insert `attendees`;
     `COMMIT`. Row locks on `ticket_types` are held only for the few milliseconds
     these statements take.
  2. **Gateway call, outside any transaction** — `payment.Gateway.CreateTransaction`
     using the committed order's number and total. No DB locks are held while this
     ~200-500 ms external call is in flight.
  3. **TX2** — `UPDATE orders SET payment_url = $1, payment_provider = $2 WHERE id =
     $3`; `COMMIT`. Return `payment_url` to the guest.
  4. **Compensation on gateway failure** — a separate transaction sets the order to
     `CANCELLED` and adds the deducted quantity back to each affected
     `ticket_types.quota`, then checkout returns an error to the guest (spec FR-021).

- **This still satisfies ARCHITECTURE.md §3.4 / Constitution Principle IV**, which
  requires `orders` + `order_items` + `attendees` + the atomic quota deduction to be
  created in **one** SQL transaction: all four of those live entirely inside TX1 and
  commit or roll back together. TX2 only stamps two payment-metadata columns onto an
  already-consistent order; it creates nothing and deducts nothing. The split is
  therefore not a deviation from §3.4 and needs no Complexity Tracking entry.
- **Rationale**: The rejected shape (gateway call inside the transaction) keeps the
  `ticket_types` row lock taken by the quota `UPDATE` held for the entire external
  HTTP round-trip. Every other buyer of that same ticket type blocks behind that
  lock, so concurrent checkout throughput for a popular ticket type collapses to
  roughly `1 / gateway_latency` (~2-5 checkouts/second) and a slow or hanging gateway
  stalls all of them — precisely the scarce-ticket contention case in spec US2
  scenario 2. Moving the network call outside the transaction removes that
  serialization while keeping the overselling guarantee, which lives in the guarded
  `UPDATE` plus `CHECK (quota >= 0)`, not in transaction duration.
- **Accepted MVP risk**: if the process crashes between TX1 and TX2, the order is
  left `PENDING` holding quota with no `payment_url` — and, because the gateway
  transaction was never created, Midtrans will never send an `expire` notification
  for it, so the webhook path cannot clean it up. The quota stays reserved
  indefinitely. This is accepted for the MVP: the window is milliseconds wide and the
  blast radius is one order's quota, recoverable by an admin.
- **Recommended post-MVP mitigation**: a periodic sweep that expires `PENDING` orders
  older than N minutes (`status = 'EXPIRED'` + restore their quota, reusing the same
  restoration path as the webhook). This is a plain scheduled task inside the
  existing API process (e.g. a ticker goroutine or a cron-invoked command) — **not** a
  queue, broker, or job runner — so it introduces none of the infrastructure PRD.md
  §1.6 puts out of scope (Kafka/RabbitMQ, Redis, queue systems).
- **Alternatives considered**: gateway call inside the single transaction (rejected —
  the serialization defect described above); pre-creating the gateway transaction
  *before* TX1 (rejected — produces a payment URL for an order that may then fail the
  quota check, and leaves an orphaned payable transaction at the provider);
  two-phase/outbox pattern for the payment URL (rejected — an outbox plus its worker
  is queue infrastructure by another name, out of scope per PRD.md §1.6).

## Payment status mapping (Midtrans → order status)

- **Decision**: Map every Midtrans `transaction_status` (plus `fraud_status` where it
  applies) through one explicit, total mapping function. Mirrors spec.md's
  **Payment Status Mapping** table and `contracts/api.md`:

  | Midtrans `transaction_status` | `fraud_status` | Order status | Quota effect |
  |---|---|---|---|
  | *(any)* — order already `PAID` | — | unchanged `PAID` | none; return `200 OK` immediately, no reprocessing |
  | `settlement` | — | `PAID` | none (deducted at checkout) |
  | `capture` | `accept` | `PAID` | none (deducted at checkout) |
  | `capture` | `challenge` | unchanged `PENDING` | none — await resolution |
  | `pending` | — | unchanged `PENDING` | none — explicit no-op |
  | `deny` | — | `CANCELLED` | restore |
  | `cancel` | — | `CANCELLED` | restore |
  | `expire` | — | `EXPIRED` | restore |
  | `failure` | — | `CANCELLED` | restore |

- **Why `deny` and `failure` both become `CANCELLED`**: `orders.status` carries
  `CHECK (status IN ('PENDING','PAID','CANCELLED','EXPIRED'))` and SCHEMA.md is
  locked, so no `DENIED`/`FAILED` status can be added. Both are terminal
  non-payment outcomes that must release quota, which is exactly `CANCELLED`'s
  meaning here. The provider's raw status is still preserved verbatim in
  `payments.status` / `payments.raw_response`, so the distinction is never lost for
  auditing.
- **Rationale**: ARCHITECTURE.md §3.4 explicitly requires the webhook to listen for
  `expire`, `cancel`, **and `deny`**; an incomplete mapping that silently ignores
  `deny`/`failure` would strand quota on orders that will never be paid. Making the
  mapping total (every status has a row, including the deliberate no-ops) also means
  an unknown/unlisted status can be logged and rejected loudly rather than dropped.
- **Alternatives considered**: treating any non-`settlement` status as a cancel
  (rejected — would wrongly cancel `pending` and fraud-`challenge` orders and release
  quota for payments still in flight); adding a `DENIED` order status (rejected —
  SCHEMA.md is locked and the CHECK constraint forbids it).

## Webhook idempotency & quota restoration

- **Decision**: Webhook handler verifies the signature via `Gateway.VerifyWebhook`
  (`401` on failure), loads the order by its provider transaction/order reference,
  short-circuits with `200 OK` if `orders.status == 'PAID'` (no reprocessing, no
  duplicate tickets or email), appends the notification to `payments`, then applies
  the mapping above — for the quota-restoring statuses, within **one** transaction
  that updates `orders.status` and adds back the deducted quantity to each affected
  `ticket_types.quota`. Statuses that map to "unchanged" perform no writes to
  `orders`/`ticket_types` at all beyond the `payments` audit row.
- **Rationale**: Directly implements ARCHITECTURE.md §3.4's idempotency and quota
  restoration rules; keeping the status update and quota restoration in one
  transaction prevents a partial state (status flipped but quota not restored).
  Restoration reads the deducted quantities from that order's `order_items`, so a
  duplicate cancel notification for an already-`CANCELLED`/`EXPIRED` order must be a
  no-op (guard the status transition with `WHERE status = 'PENDING'`) or quota would
  be restored twice.
- **Alternatives considered**: Restoring quota via a background reconciliation job
  (rejected — adds operational complexity not justified for MVP scope, and delays
  quota becoming available again).

## Ticket code format & lookup (index-preserving)

- **Decision**: Generate `ticket_code` in **canonical form at write time**:
  uppercase, fixed length, drawn from an unambiguous charset that excludes the
  visually confusable characters `I`, `O`, `0`, and `1` (e.g. the 32-character set
  `23456789ABCDEFGHJKLMNPQRSTUVWXYZ`), optionally with a short fixed prefix. Lookups
  normalize the caller's input (trim surrounding whitespace, uppercase, and — if a
  prefix/grouping separator is used in display — strip it) and then perform an
  **exact equality match**: `SELECT ... FROM tickets WHERE ticket_code = $1`.
- **Rationale**: Because every stored code is already canonical, exact equality is
  sufficient and uses `idx_tickets_ticket_code` (SCHEMA.md line 106) for an index
  scan. Excluding `I/O/0/1` makes codes safe to read aloud and retype from a printed
  PDF, which matters for the admin's manual-entry validation path.
- **Explicitly rejected**: `WHERE UPPER(ticket_code) = UPPER($1)`. SCHEMA.md is
  locked, so no functional/expression index on `UPPER(ticket_code)` exists and none
  may be added; wrapping the column in a function makes the plain B-tree index
  unusable and forces a sequential scan of `tickets` on every public lookup — an
  unauthenticated endpoint that degrades linearly with ticket volume. Normalize in
  the application, never in the predicate.
- **Alternatives considered**: raw UUID as the ticket code (rejected — 36 characters
  with hyphens, unreadable and error-prone for manual entry); `citext` column or a
  case-insensitive collation (rejected — both are schema changes and SCHEMA.md is
  locked).

## QR code generation and storage

- **Decision**: `tickets.qr_code_url` is left **NULL/empty** in this MVP. Ticket
  generation persists only the unique `ticket_code`. The QR image is generated **on
  demand** from `ticket_code` with `go-qrcode` at PDF render time — for the initial
  delivery email, for any later admin-triggered resend, and for any future
  "download QR" feature — and is embedded directly in the rendered document as
  in-memory bytes. Nothing is written to disk or to any bucket.
- **Rationale**: There is no object storage, no static file serving, and no upload
  endpoint anywhere in this stack (PRD.md §1.2 lists only Docker Compose +
  PostgreSQL), so there is no location a `qr_code_url` could legitimately point at.
  A QR image is a pure, deterministic function of the ticket code — storing it would
  be caching a value cheaper to recompute (single-digit milliseconds) than to store,
  and would create a second source of truth that could drift from `ticket_code`.
  Resend therefore regenerates an identical PDF rather than fetching a stored asset.
- **Consequences**: any consumer must treat `qr_code_url` as always empty and must
  not build features that read it; the ticket lookup contract does not expose it.
- **Alternatives considered**: storing a base64 data URI in `qr_code_url` (rejected —
  bloats every `tickets` row with derivable data and still needs a renderer);
  serving `GET /tickets/:code/qr.png` as a static-ish image endpoint (rejected —
  unnecessary for the MVP's email-delivery flow, and it would widen the
  unauthenticated attack surface addressed under "Public ticket lookup hardening").

## Public ticket lookup hardening

- **Decision**: `GET /api/v1/tickets/:code` stays unauthenticated (spec FR-016/FR-017)
  but is (a) **rate limited per client IP** with Echo's built-in
  `middleware.RateLimiter` (in-memory store — no Redis, which PRD.md §1.6 puts out of
  scope), returning `429 Too Many Requests` over the limit, and (b) restricted to a
  **minimal response body**: `ticket_code`, `status`, `event_name`, `attendee_name`.
  It MUST NOT return the attendee email, buyer email/phone, order number, or ticket
  type pricing. Unknown codes return a flat `404` with no distinguishing detail.
- **Rationale**: The endpoint takes a guessable-shaped identifier from an anonymous
  caller and returns personal data, so unlimited requests are a free enumeration
  oracle: an attacker who lands on a valid code would otherwise harvest attendee
  identities and emails. Rate limiting caps the guess rate, and dropping email from
  the payload caps the value of any successful guess. The high-entropy canonical
  code format above (32^N keyspace) is the third leg of the defence.
- **Alternatives considered**: requiring the buyer's email as a second lookup
  parameter (rejected — breaks the "type the code from your PDF" UX and still
  discloses on a match); a signed lookup token in the email link (rejected — a plain
  code must remain manually enterable per PRD.md §1.4); no limiting at all (rejected
  — the defect this decision fixes).

## Async post-payment processing

- **Decision**: After the webhook transaction commits `PAID`, launch a goroutine
  that generates one ticket per attendee (persisting only the unique `ticket_code`;
  `qr_code_url` is left NULL), renders the PDF (`maroto`/`gofpdf`) drawing each QR on
  demand from its `ticket_code` via `go-qrcode`, sends the email (SMTP), and finally
  sets `email_sent = true`. The webhook HTTP handler returns `200 OK` before the
  goroutine is scheduled to run its I/O.
- **Rationale**: Matches ARCHITECTURE.md §3.4 — payment providers expect a fast
  webhook response; PDF rendering/SMTP delivery are the slow, failure-prone steps
  and must not block that response.
- **Alternatives considered**: A durable job queue (e.g., a `jobs` table polled by
  a worker) for post-payment processing (rejected for MVP — PRD.md explicitly puts
  Kafka/queueing infra out of scope; a goroutine is sufficient at MVP scale, with
  the resend-email admin action as the manual recovery path for failures).

## Payment gateway integration (Midtrans SNAP Sandbox)

- **Decision**: Implement `internal/payment.Gateway` with a `midtrans.go` adapter
  calling Midtrans SNAP's Sandbox API for `CreateTransaction`, and verifying webhook
  signatures per Midtrans's documented signature-key algorithm in `VerifyWebhook`.
- **Rationale**: PRD.md names Midtrans SNAP Sandbox as the initial concrete gateway;
  the `Gateway` interface (Principle V) keeps this swappable later.
- **Alternatives considered**: None — provider is fixed by PRD.md for this MVP.

## Frontend data freshness

- **Decision**: Event list/detail and ticket-type quota reads use Next.js
  `fetch(..., { cache: 'no-store' })`; checkout submission uses TanStack Query
  mutations with no client-side caching of the result beyond the current page.
- **Rationale**: ARCHITECTURE.md §3.4 explicitly requires disabling aggressive
  caching for event/quota data since it is live inventory that changes with every
  checkout.
- **Alternatives considered**: Short-TTL ISR/revalidation (rejected — quota can
  change within seconds under concurrent buyers; only `no-store` guarantees the
  guest sees current availability before submitting checkout).

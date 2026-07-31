# Phase 0 Research: Admin Ticket Validation

No `NEEDS CLARIFICATION` markers remain. This document records key decisions.

## Atomic mark-used

- **Decision**: `UPDATE tickets SET status = 'USED', updated_at = now() WHERE
  ticket_code = $1 AND status = 'ACTIVE' RETURNING id` (with `$1` bound to the
  normalized input code). Zero rows returned means reject with "already used or
  invalid".
- **Rationale**: Guarantees exactly one of two near-simultaneous requests for the
  same ticket succeeds, satisfying FR-006 and SC-002 without needing explicit
  row locks or application-level mutexes. The `updated_at = now()` clause is also
  how the transition is *recorded* (FR-005): SCHEMA.md is LOCKED and its `tickets`
  table has no `used_at` column, so `updated_at` on a row whose `status = 'USED'`
  is the authoritative "when it was consumed" timestamp.
- **Alternatives considered**: `SELECT ... FOR UPDATE` then separate `UPDATE`
  (rejected — extra round trip with no additional safety over the single guarded
  statement); adding a dedicated `used_at` column (rejected — SCHEMA.md is LOCKED;
  no new columns or tables may be proposed).

## Separate mark-used endpoint (justified extension to PRD §1.5)

- **Decision**: Expose the `ACTIVE -> USED` transition as its own endpoint,
  `POST /api/v1/admin/tickets/:code/use`, and keep
  `POST /api/v1/admin/tickets/validate` strictly read-only. This is a deliberate,
  documented addition to PRD.md §1.5's LOCKED API list — recorded here and in
  contracts/api.md and spec.md so it is not read as an accidental deviation.
- **Rationale**: PRD.md §1.4 mandates "Admin can mark `Valid` tickets as `Used`",
  but §1.5 lists only the `validate` endpoint for this feature area, so the
  mandated capability cannot be delivered by the listed endpoints alone. Splitting
  the two is what makes repeated scanning safe: at a door the same code is often
  read two or three times in seconds, and a side-effecting lookup would silently
  consume tickets the admin had not yet decided to admit — the exact behaviour
  this spec's Edge Cases and SC-002 forbid.
- **Alternatives considered**: Fold the transition into `validate` behind a
  `mark_used: true` request flag (rejected — the endpoint would no longer be
  safely repeatable, and a client defaulting the flag the wrong way would consume
  every ticket it merely looked up); a `PATCH /admin/tickets/:code` general status
  mutation (rejected — a wider surface than PRD §1.4 asks for, and it would permit
  transitions such as `USED -> ACTIVE` that the constitution's "marking a ticket
  Used MUST be irreversible" rule prohibits).

## Ticket code normalization

- **Decision**: Normalize the **input only**, then match exactly.
  `ticket_code` is already generated in canonical form at write time by the guest
  purchase flow (specs/001): uppercase, fixed length, unambiguous character set
  excluding `I`, `O`, `0`, and `1`. At lookup, the admin-supplied string is
  trimmed of surrounding whitespace and uppercased in Go, and the result is bound
  to a plain equality predicate — `WHERE ticket_code = $1` — for both validate and
  mark-used.
- **Rationale**: FR-010 requires manual entry and QR scan of the same physical
  ticket to resolve identically; humans retyping a code are prone to
  case/whitespace slips that a scanner wouldn't introduce. Normalizing the input
  fixes that without touching the column, so `idx_tickets_ticket_code` — a plain
  btree on `ticket_code` in the LOCKED SCHEMA.md — is still used by the planner.
  Excluding `I`, `O`, `0`, `1` from the generated charset removes the remaining
  class of transcription errors that case-folding could never fix anyway.
- **Alternatives considered**: A case-insensitive predicate such as
  `UPPER(ticket_code) = UPPER($1)` (**rejected** — wrapping the column in a
  function makes the plain btree index unusable, forcing a sequential scan of
  `tickets` on every door scan, which directly threatens SC-001's 3-second budget
  as ticket volume grows; the fix would be a functional index or `citext`, and
  SCHEMA.md is LOCKED so neither may be added). Case-sensitive exact match on raw,
  un-normalized input (rejected — fails FR-010 for manual entry, the stated
  primary input method).

## QR scanning approach

- **Decision**: Use a browser-based QR decoder (e.g., a `getUserMedia` camera
  stream piped into a JS/WASM QR decoding library) on the admin frontend; decoded
  text is submitted to the same `POST /admin/tickets/validate` endpoint used for
  manual entry.
- **Rationale**: PRD.md specifies "Camera QR scan as secondary" input on the admin
  side — no dedicated hardware scanner is implied; reusing one backend endpoint for
  both input methods keeps validation logic single-sourced (FR-002).
- **Alternatives considered**: Native mobile app with a hardware scanner SDK
  (rejected — out of scope; PRD.md's stack is web-only for the admin UI).

## QR image generation (no stored image, no object storage)

- **Decision**: `tickets.qr_code_url` is left NULL/empty in the MVP. There is no
  object storage, no static file serving, and no upload endpoint anywhere in this
  stack, and none is introduced here. The QR image is generated **on demand from
  `ticket_code`** (via `go-qrcode`) at the moment it is needed: at PDF render time
  for initial delivery, at PDF render time again for resend, and for any future
  "download QR" capability.
- **Rationale**: The ticket code *is* the QR payload, so the image is a pure
  function of data already stored — persisting it would add infrastructure
  (bucket, credentials, public URLs, lifecycle) that PRD.md's MVP scope does not
  fund, with no information gain. It also makes the invariant crisp: **ticket
  codes are never regenerated; QR images always are.**
- **Alternatives considered**: Persist a rendered PNG to object storage and store
  its URL in `qr_code_url` (rejected — no object storage exists in the MVP stack
  and adding one is out of scope); store a data-URI of the PNG in `qr_code_url`
  (rejected — bloats every `tickets` row for a value that is trivially
  recomputable, and the column stays available for a post-MVP hosted-image
  implementation without a schema change).

## Resend without regenerating tickets

- **Decision**: `ResendTicketEmail(orderID)` in `internal/notification` re-fetches
  the order (buyer email, status) and its attendees' existing tickets — critically,
  their already-generated **`ticket_code` values, which are stable and MUST NOT be
  regenerated** — through the narrow `OrderChecker` interface defined by
  specs/002-admin-management and implemented by the `order` domain (injected in
  `cmd/api/main.go`). It then re-renders each ticket's **QR image from that
  existing `ticket_code`**, re-renders the PDF, and re-sends the email. It never
  calls the ticket generation path again and never writes to `tickets`.
- **Rationale**: FR-007/FR-008 require resending the *same* ticket set; regenerating
  codes would invalidate tickets guests already hold and would break door
  validation for anyone who kept the first email. QR images, by contrast, carry no
  independent state (see "QR image generation" above) and are simply re-derived at
  render time — `qr_code_url` is empty and is not read.
- **Eligibility & status vocabulary**: Resend is allowed **only** for orders whose
  status is `PAID`, which are exactly the orders that have generated tickets. Every
  other status in SCHEMA.md's `orders.status` CHECK — `PENDING`, `CANCELLED`
  (Midtrans `deny`, `failure`, and `cancel` per specs/001's webhook mapping), and
  `EXPIRED` (Midtrans `expire`) — is rejected with `400 ORDER_NOT_PAID`.
- **`email_sent` handling**: On successful delivery, resend sets
  `orders.email_sent = true`, exactly as the initial post-payment send does
  (FR-011, ARCHITECTURE.md §3.4). A resend for an order whose earlier delivery
  failed is therefore also the documented recovery path that finally flips the
  flag; for an order where it is already true, the write is idempotent.
- **Cross-domain rule**: Per ARCHITECTURE.md §3.2 and Constitution Principle II,
  `internal/notification` MUST NOT import the `order` domain's repository — the
  `OrderChecker` interface is the only channel, keeping the naming consistent with
  specs/002 rather than introducing an `OrderReader`/`OrderProvider` variant.
- **Alternatives considered**: Cache the originally-generated PDF bytes and resend
  the identical file (viable optimization, deferred — no blob storage exists, and
  re-rendering from stable ticket codes is simpler and produces an identical
  result at MVP scale). Importing `order`'s repository directly (rejected —
  violates Principle II).

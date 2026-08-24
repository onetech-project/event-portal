# Data Model: End-to-End Guest Purchase Flow

**Feature**: 008-e2e-purchase-flow · **Date**: 2026-08-05
**Migration**: `backend/migrations/000005_event_content_and_booking.{up,down}.sql`
**Rule**: `SCHEMA.md` must be updated in the same change (Constitution, Technology Stack).

## 1. New tables (event domain)

### event_activities

| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK, default uuid_generate_v4() |
| event_id | UUID | NOT NULL REFERENCES events(id) ON DELETE CASCADE |
| title | VARCHAR(255) | NOT NULL |
| description | TEXT | NOT NULL |
| icon | VARCHAR(100) | NULL — named icon key from the fixed set (research R7), never a URL |
| position | INT | NOT NULL DEFAULT 0 |
| created_at / updated_at | TIMESTAMPTZ | DEFAULT CURRENT_TIMESTAMP |

### event_guest_stars

| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK |
| event_id | UUID | NOT NULL REFERENCES events(id) ON DELETE CASCADE |
| name | VARCHAR(255) | NOT NULL |
| position | INT | NOT NULL DEFAULT 0 |
| created_at / updated_at | TIMESTAMPTZ | DEFAULT CURRENT_TIMESTAMP |

### event_guidelines

| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK |
| event_id | UUID | NOT NULL REFERENCES events(id) ON DELETE CASCADE |
| description | VARCHAR(500) | NOT NULL |
| icon | VARCHAR(100) | NULL — named icon key |
| position | INT | NOT NULL DEFAULT 0 |
| created_at / updated_at | TIMESTAMPTZ | DEFAULT CURRENT_TIMESTAMP |

### event_terms

| Column | Type | Constraints |
|--------|------|-------------|
| id | UUID | PK |
| event_id | UUID | NOT NULL UNIQUE REFERENCES events(id) ON DELETE CASCADE — one live terms per event |
| content | TEXT | NOT NULL — sanitized HTML (bluemonday on write) |
| created_at / updated_at | TIMESTAMPTZ | DEFAULT CURRENT_TIMESTAMP |

CASCADE rationale: content blocks and terms are presentation data owned by the
event; they must not block event deletion the way orders do. Order rows keep
their own durable agreement timestamp, so deleting an event (only possible when
it has no orders at all, per the existing delete guard) cannot orphan a live
agreement record that matters.

**Indexes**: `event_id` index on each of the four tables (`idx_<table>_event_id`).

**Seed/backfill**: the up-migration inserts an `event_terms` row per existing
event using the retired static copy from `frontend/lib/terms.ts`, so booking
never hits the "no terms" refusal for pre-existing events.

## 2. Changed tables

### orders — T&C agreement (FR-007/FR-008)

| Column | Type | Constraints |
|--------|------|-------------|
| terms_agreed_at | TIMESTAMPTZ | NULL — set by `POST /ticket/terms-condition/:order_id`; checkout refuses orders where it is NULL |
| event_terms_id | UUID | NULL REFERENCES event_terms(id) ON DELETE SET NULL — which terms were agreed |
| buyer_name / buyer_email / buyer_phone | — | NOT NULL → **NULL** — book creates the order before any buyer details exist (Option B); filled by checkout |

Existing columns reused, unchanged: `status`, `payment_provider`,
`payment_url`, `payment_qr_string`, `payment_expires_at`, `email_sent`.
`payment_expires_at` now carries **two successive deadlines**: booking sets
`now()+1h` (hold), payment start overwrites with `now()+14m` (window). The
partial index `idx_orders_payment_expiry` and the sweeper query are unchanged.

### attendees — deferred visitor details (research R6)

| Change | Detail |
|--------|--------|
| `name` | NOT NULL → **NULL** (slot created empty at booking) |
| `email` | NOT NULL → **NULL** |
| + `phone` | VARCHAR(50) NULL |
| + `dob` | DATE NULL |
| + `gender` | VARCHAR(20) NULL CHECK (gender IN ('MALE','FEMALE')) |

Completeness is enforced by the service at payment start (`422` if any slot
lacks name/email/phone/dob/gender), not by the schema — the schema must allow
the empty-slot phase. Ticket issuance on PAID reads `name`/`email` as before
and is only reachable after the completeness gate.

Down-migration restores NOT NULL by filling placeholders only if empty slots
exist (documented destructive step).

## 3. Explicitly not created

| Proposed (user input) | Disposition |
|-----------------------|-------------|
| `Genders` lookup table | CHECK constraint on `attendees.gender` instead |
| `Visitor_Point` | Excluded — loyalty barred by Constitution VI |
| `constant_parameters` | Env-var config (`pkg/config`) is the existing pattern |
| `Order_Statuses` lookup | Existing VARCHAR + CHECK on `orders.status` retained |
| serial-ID schema variant | Existing UUID schema retained; SCHEMA.md is truth |

## 4. Order state machine (unchanged states, new phase semantics)

```
                 T&C agree: POST /ticket/book
  [no order] ──────────────────────────► PENDING  (buyer + payment fields NULL,
                                            │       expires = +1h)
       + POST /ticket/terms-condition/:id   │  ── same Agree click:
         (terms_agreed_at set)              │     stays PENDING
                                            │
       POST /ticket/checkout/:id            │  ── stays PENDING
         (forms saved, gateway OK,          │
          payment fields set,               │
          expires = +14m)                   │
                                            ├── webhook settlement ──► PAID ──► tickets + email
                                            ├── sweeper past expires ─► EXPIRED (+ restore quota)
                                            ├── webhook expire/cancel/deny/failure
                                            │                        ─► EXPIRED/CANCELLED (+ restore quota)
                                            └── gateway call failed at book-compensation
                                                                     ─► CANCELLED (+ restore quota)
```

- Client distinguishes the two PENDING phases by `payment_qr_string == null`
  (forms screen) vs set (payment screen).
- No transitions out of PAID / EXPIRED / CANCELLED via this flow (FR-024).
- Webhook after EXPIRED: existing idempotent path — logged + flagged for manual
  reconciliation, no automatic refund (spec edge case, Constitution VI).

## 5. Entity → domain ownership (Constitution I/II)

| Entity | Owning domain | Cross-domain access |
|--------|---------------|---------------------|
| event_activities / event_guest_stars / event_guidelines / event_terms | `event` | `order` reads terms via extended `EventProvider` interface (never the repo) |
| orders (+ new columns) | `order` | `payment` via existing order adapter |
| attendees (+ new columns) | `order` | `ticket` issuance via existing adapter |
| SSE status hub | `payment` | webhook + sweeper publish internally |

## 6. Validation rules (service layer)

| Rule | Where | Error |
|------|-------|-------|
| Booking refused when event has no terms | order.Book (via EventProvider) | 409 `TERMS_MISSING` |
| Agreement requires `agreed: true` + current terms id | order.RecordAgreement | 400 `TERMS_NOT_ACCEPTED` / 409 `TERMS_CHANGED` |
| Agreement only while PENDING + unexpired | order.RecordAgreement | 410 `ORDER_EXPIRED` |
| Checkout refused until agreement recorded | order.Checkout | 409 `TERMS_NOT_RECORDED` |
| Visitor details: name ≤255, valid email, phone 7–20 digits, dob past date, gender enum; every slot covered | order.Checkout (body carries all forms — Option B) | 400 field errors |
| A gender no longer active in the master list, on the slot that already carries it (spec 011 FR-031, 2026-08-19) | order.Checkout, after the slots are matched | accepted; refused on any other slot |
| Checkout only while PENDING + unexpired; idempotent when payment already started | order.Checkout | 409 `PAYMENT_ALREADY_STARTED` / 410 `ORDER_EXPIRED` |
| QR refresh only while payment started + PENDING + unexpired | payment.ReissueQR | 409 / 410 |
| WYSIWYG HTML sanitized on write | event service (bluemonday UGC) | silently cleaned |
| Icon keys must be in the fixed set | event admin validation | 400 `UNKNOWN_ICON` |

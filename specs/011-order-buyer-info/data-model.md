# Data Model: Order Page Buyer Information — Per-Ticket Holder Forms

**Feature**: 011-order-buyer-info · **Date**: 2026-08-06 (rev. 2 — post-verification + gender-FK clarification)
**Migration**: `backend/migrations/000012_gender_fk_and_contact_snapshot.{up,down}.sql`
**Rule**: `SCHEMA.md` must be updated in the same change (Constitution, Technology Stack + Governance sync for amendment v2.0.0).

> **SUPERSEDED 2026-08-06 (delivery only)**: the per-holder fan-out described below was implemented and then reversed by clarification — delivery is exactly ONE email to the buyer (form 1's address) carrying every ticket plus the receipt (spec.md FR-012, constitution v3.0.0). Everything else on this page still holds; the holder emails are still collected and stored, so only the delivery layer changed.

> **SUPERSEDED 2026-08-07 (gender key type)**: §1–§6 below describe migration `000012` **as shipped** (`e189e19`) and remain the accurate record of it — including `gender_id uuid`. They are no longer the *target* shape: the 2026-08-07 schema revision (spec.md FR-025 – FR-029) converts both master-list keys to integers, adds authorship columns, moves `orders.status` to a reference, and converts `packages.status` to a boolean. **§7 below is the current target**; read §1–§6 as history, not as instructions. The FK direction, the name-on-the-wire rule, and every other decision on this page are unchanged by the revision.

## 1. Schema changes (migration 000012)

```sql
-- up
ALTER TABLE attendees ADD COLUMN gender_id uuid REFERENCES genders(id);      -- nullable: slots created empty at booking
UPDATE attendees a SET gender_id = g.id FROM genders g WHERE a.gender = g.name;  -- total backfill: old CHECK admitted only MALE/FEMALE, both seeded
ALTER TABLE attendees DROP COLUMN gender;                                    -- CHECK (gender IN ('MALE','FEMALE')) dies with the column
ALTER TABLE orders DROP COLUMN buyer_dob;                                    -- write-only since 000007; buyer form removed
ALTER TABLE orders DROP COLUMN buyer_gender;                                 -- write-only since 000007; FK-governed value lives on attendees
```

Down-migration restores `attendees.gender VARCHAR(20)` + CHECK (backfilled by joining `genders` on `gender_id`) and re-adds `orders.buyer_dob`/`buyer_gender` as empty columns — a documented lossy step (their values are reconstructible from the primary holder's attendee row).

Not changed, verified against `SCHEMA.md` + `backend/migrations/`:

- `orders.buyer_name / buyer_email / buyer_phone` — remain, nullable (since 000005), no CHECKs; retained as the primary-contact snapshot (§2).
- `attendees.name / email / phone / dob` — unchanged nullable slot-fill columns; `package_id` (000003) + `package_unit` (000011) carry bundle-unit grouping.
- `orders.email_sent BOOLEAN DEFAULT FALSE` (000001) — retained as the per-order, all-or-nothing delivery flag (research R8). No per-attendee ledger.
- `tickets.attendee_id` FK (000001) — already links each ticket to its holder.
- `genders` (000008): `id uuid PK, name varchar(50) NOT NULL UNIQUE, is_active` — now the FK target of `attendees.gender_id`.

## 2. Semantic changes (same columns, new meaning)

### orders.buyer_name/email/phone → primary-contact snapshot

| Aspect | Before | After |
|---|---|---|
| Source | Dedicated "Buyer contact" form (five fields) | The **topmost holder form** — the visitor mapped to the first slot in canonical slot order (`ORDER BY package_id NULLS FIRST, package_unit ASC, id ASC`, `queries/order.sql:294-304`), resolved server-side (research R2) |
| Columns written | five (`UpdateOrderBuyer`, `queries/order.sql:248-253`) | **three** — name, email, phone; same statement, same TX-D, narrowed |
| Read by | Payment QR re-issue customer details (`cmd/api/adapters.go:146-148`), admin order list (`admin_service.go:111-112`), notification recipient (`notification/service.go:97`), guest order read | Same, except: notification uses it only as **fallback** for an empty holder email; the guest order read stops exposing it (§3) |
| SCHEMA.md doc | "buyer …" (lines ~103/111/114) | "primary contact — snapshot of the topmost holder form (spec 011)" |

### attendees — the only identity source; gender now referential

Each attendee row's `name/email/phone/dob` plus the new `gender_id` is the canonical holder identity. Gender is submitted and returned as the **name** on the wire; the order service maps name↔id (active-genders load at `service.go:398-408` becomes a name→id map; read queries JOIN `genders` to return the name). New write-time phone validation on both sides: `^[0-9]{12,15}$` (research R3, widened 2026-08-07 from digits-only 10-12, then floor raised 10 → 12 on 2026-08-13 per research R29); the value is stored exactly as the guest typed it, in either `62…` or `08…` form. Existing rows written under any earlier rule remain valid at rest — `VARCHAR(50)` with **no CHECK**, and none is to be added: the 2026-08-13 clarification makes at-rest validity permanent, so a 10- or 11-digit legacy number keeps displaying, keeps reaching the gateway, and keeps working for resend. Validation is a write-time rule on the holder forms only.

### Delivery grouping (derived, not stored)

Recipient set for a PAID order = distinct normalized (trimmed, lowercased) `attendees.email` values across the order's slots; each recipient receives the tickets whose attendee rows carry that email (research R7). A bundle unit's N identical rows collapse into one message with N ticket pages; two *different* holders sharing one address also collapse into one message that renders **one holder-details block + e-ticket card section per distinct holder**, in first-occurrence order of the order's ticket list. Empty/NULL holder email (defensive) falls back to `orders.buyer_email`.

## 3. Domain-object (DTO/interface) changes

| Object | File | Change |
|---|---|---|
| `CheckoutFormsRequest` | `backend/internal/order/dto.go:189-198` | Remove `BuyerName/BuyerEmail/BuyerPhone/BuyerDob/BuyerGender` + their validation blocks (`:219-235`); add per-visitor phone rule (message: "Enter a phone number of 12-15 digits.") |
| `TicketOrderDetail` | `backend/internal/order/dto.go:327-328` | Remove `BuyerName`, `BuyerEmail` |
| Active-genders load | `backend/internal/order/service.go:398-408` | Set → **name→id map**; checkout writes `gender_id` via `UpdateAttendeeDetails` |
| Order SQL | `backend/internal/order/queries/order.sql` | `UpdateOrderBuyer` → 3 columns; `UpdateAttendeeDetails` → `gender_id`; `ListAttendeeSlotsByOrderID` + `ListAttendeesAdmin` JOIN `genders` for the name; delete `CreateOrder`/`CreateAttendee` statements (`:3-25`); **`sqlc generate`** |
| `ticket.FullDetail` | `backend/internal/ticket/dto.go:34-42` | Add `AttendeeID uuid.UUID`, `AttendeeEmail string` (from extended `ListTicketDetailsByOrderID`) |
| `notification.TicketDetail` | `backend/internal/notification/pdf.go:22-29` | Add attendee id + email (grouping key) |
| `notification.Service.SendTicketEmail` | `backend/internal/notification/service.go:70` | Signature `(ctx, orderID) error` → **`(ctx, orderID) ([]string, error)`** — returns distinct recipients for the admin resend response |
| `payment.TicketDeliverer` | `backend/internal/payment/service.go:104-106` | **Unchanged** — the adapter in `cmd/api/adapters.go` narrows the new notification signature back to `error` for payment |
| `notification.OrderDelivery` | `backend/internal/notification/service.go:19-27` | `BuyerName/BuyerEmail` remain, re-documented as primary-contact fallback |
| `notification.ResendResponse` | `backend/internal/notification/dto.go:7` | `SentTo string` → `SentTo []string` (distinct recipients, first-occurrence order — not canonical slot order) |
| `payment.OrderRef` | `backend/internal/payment/service.go:44-50` | Unchanged — still fed from `orders.buyer_*` (now primary contact) |
| Frontend `TicketOrderDetail` | `frontend/lib/types.ts:189-190` | Remove `buyer_name`, `buyer_email` |
| Frontend admin resend type | `frontend/lib/types.ts:336` | `sent_to: string` → `sent_to: string[]`; toast render at `frontend/app/(admin)/admin/orders/page.tsx:98` joins the list |
| Frontend guest consumers of `buyer_email` | `frontend/app/(public)/events/[slug]/orders/[orderNumber]/done/page.tsx:102`, `frontend/components/order/payment-status-card.tsx:11,:26` | Drop the `buyerEmail` prop; reword the PAID branch to per-holder copy; update `done/page.test.tsx` |
| Frontend `OrderFormsValues` | `frontend/components/order/visitor-form.tsx:62-71` | Remove `buyer_*` members from `formsSchema`; `attendees` array becomes the whole form state |
| Frontend `Field` wrapper | `frontend/components/ui/field.tsx` | New `required` prop (asterisk + `aria-required`) — set on all five holder fields (research R16) |

**Deleted (dead since spec 008 rerouting — no non-test callers)**: `order.CheckoutRequest`, `order.CheckoutAttendee`, `order.Service.Checkout`, `reserve`/`reserveOnce`, `paymentRequestFor` (`service.go:888-914`), `validateAttendees` (`demand.go:145-166` — `expandItem`/`expandPackage`/`aggregateDemand` stay, live for booking), `Repository.CreateOrder`/`CreateAttendee` + their SQL, `PublicOrderDetail`, `PublicService.OrderByNumber`, and the consequently-dead helpers `compensate` (`service.go:832`), `reservedLine` (`:72`), `ticketNameFor` (`:861`), `priceFor` (`:877`).

## 4. Validation rules (write path, service layer)

| Rule | Where | Error |
|---|---|---|
| `attendees[]` must cover the order's slots exactly (existing) | `matchVisitorsToSlots`, `order/service.go:607-628` | 400 `attendees` |
| Bundle-unit field consistency (existing, spec 010) | `validateBundleUnitConsistency`, `service.go:636-666` | 400 `400001` per-field |
| Name trimmed non-empty; email `net/mail.ParseAddress`; DOB `YYYY-MM-DD` + not future; gender name in active master list (all existing, per visitor) | `CheckoutFormsRequest.Validate` | 400 `400001` field map |
| **New**: phone `^[0-9]{12,15}$` (length only, stored verbatim), per visitor | `CheckoutFormsRequest.Validate` (replacing non-empty check, `dto.go:255-257`) | `attendees[i].phone`: "Enter a phone number of 12-15 digits." |
| **New**: gender name → `gender_id` resolution at write (name valid ⇒ id exists; FK is the backstop) | checkout fill loop (`service.go:479-491`) | n/a (cannot fail after membership check) |
| **Removed**: all five `buyer_*` validations | `dto.go:219-235` | — |
| Primary contact derivation: visitor mapped to first canonical slot → `orders.buyer_name/email/phone` in TX-D → gateway `CustomerName/Email/Phone` | `order/service.go` checkout path (R2, R10) | n/a (derived) |
| Frontend mirror: Zod `regex(/^[0-9]{12,15}$/, "Enter a phone number of 12-15 digits.")` behind a `PhoneInput` that filters non-digits on the way in but changes nothing else about the value; button gated on `formState.isValid` (`mode: "onTouched"`) with click-capture `trigger()` reveal (R5); all fields marked required (R16) | `visitor-form.tsx`, `ui/field.tsx` | inline field errors |

## 5. Delivery state machine (per-order, unchanged states — new fan-out semantics)

```
PAID (webhook/reconcile, idempotent)
  └─ fulfillAsync goroutine (unchanged seam)
       ├─ IssueTicketsForOrder — one ticket per attendee row (unchanged)
       └─ SendTicketEmail  → returns []recipients
            ├─ group tickets by normalized attendees.email  (fallback: orders.buyer_email)
            ├─ for each recipient: full order receipt + per-holder detail blocks
            │  + PDF of that group's tickets → SMTP send
            │    └─ per-recipient failure: log, continue remaining recipients
            └─ all succeeded → email_sent = TRUE
               any failed   → email_sent stays FALSE (resend re-sends to all recipients)
```

Resend (guest + admin) reuses `SendTicketEmail` verbatim → identical fan-out; the admin handler surfaces the returned recipient list as `sent_to`. Duplicate re-delivery to holders who already received mail is accepted (same ticket codes; gate validation is one-scan-one-use).

## 6. Entity → domain ownership (Constitution I/II — unchanged)

| Entity | Owning domain | Cross-domain access after this feature |
|---|---|---|
| orders (3-column primary-contact snapshot) | `order` | payment via existing `OrderRef` adapter; notification via `OrderDelivery` adapter (fallback only) |
| attendees (holder identity, `gender_id` FK) | `order` | ticket issuance via existing adapter; notification receives attendee email **through the extended ticket-details adapter copy** (`cmd/api/adapters.go`), never a repository import |
| genders (master list) | `order` (existing owner of the master + admin CRUD) | unchanged; FK from `attendees` is intra-database referential integrity, not a cross-domain write JOIN |
| tickets | `ticket` | notification via `TicketProvider` (extended with attendee id/email) |
| fees / order_fees | `order` | unchanged; summary breakdown is client-side rendering of existing wire data |

---

# 7. Rev. 3 (2026-08-07) — schema revision, migration `000013`

**Migration**: `backend/migrations/000013_master_list_identity_and_flags.{up,down}.sql`
**Requirements**: FR-025 – FR-029 · **Research**: R18–R23
**Rule unchanged**: `SCHEMA.md` must be updated in the same change (Constitution, Technology Stack).

## 7.1 Target shape

```sql
-- order_statuses: uuid key -> integer identity, + audit authorship
order_statuses(
  id          integer      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,  -- seeded 1..4, fixed
  name        varchar(256) NOT NULL UNIQUE,                           -- widened from 50 (FR-027)
  is_active   boolean      NOT NULL DEFAULT true,
  created_at  timestamptz  NOT NULL DEFAULT now(),
  created_by  varchar(50)  NOT NULL,                                  -- 'SYSTEM' for seeds (FR-028)
  updated_at  timestamptz,
  updated_by  varchar(50)
)

-- genders: same, narrower key and name (FR-025, FR-027, FR-028)
genders(
  id          smallint     GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name        varchar(100) NOT NULL UNIQUE,                           -- widened from 50
  is_active   boolean      NOT NULL DEFAULT true,
  created_at  timestamptz  NOT NULL DEFAULT now(),
  created_by  varchar(50)  NOT NULL,
  updated_at  timestamptz,
  updated_by  varchar(50)
)

orders.status varchar(50) REFERENCES order_statuses(name)
  -> orders.status_id smallint NOT NULL REFERENCES order_statuses(id)   -- FR-026

attendees.gender_id uuid REFERENCES genders(id)
  -> attendees.gender_id smallint REFERENCES genders(id)                -- FR-025 (still nullable)

packages.status varchar(50) CHECK (status IN ('ACTIVE','INACTIVE'))
  -> packages.is_active boolean NOT NULL DEFAULT true                   -- FR-029

tickets.status  -- UNCHANGED: ACTIVE | USED | REVOKED (FR-029 excludes it by name)
```

## 7.2 Ordering constraints the migration must respect

The steps are not freely reorderable — each of these is a hard dependency, not a preference:

1. **Drop `orders_status_fkey` (name FK, migration 0009) before touching `order_statuses.id`.** The old FK points at `name`; the new one points at `id`.
2. **Add the new key column alongside the old one, populate by joining on the old key, then drop the old one.** There is no cast from `uuid` to `integer`, so `ALTER COLUMN ... TYPE ... USING` is unavailable for either master list or for `attendees.gender_id` (R21). Deriving each row's new id from *its own* old id — never from row order — is what makes FR-025's no-silent-rebinding guarantee checkable.
3. **Assign the order-status ids explicitly (1 PENDING, 2 PAID, 3 CANCELLED, 4 EXPIRED), then reset the identity sequence past them.** Required by step 5 (R20).
4. **Populate `orders.status_id` from the existing `orders.status` name before dropping the name column**, then set `NOT NULL`.
5. **Drop and recreate `idx_orders_payment_expiry`** (created in `000002:24-26` as `WHERE status = 'PENDING'`) as `WHERE status_id = 1`. A partial index predicate must be immutable, so the literal is mandatory — a subquery is rejected by PostgreSQL (R20). Everything else can use a subquery; this one cannot.
6. **Backfill `created_by = 'SYSTEM'` on every existing master-list row before adding the NOT NULL constraint.**
7. **`packages.is_active` backfills as `(status = 'ACTIVE')`** before `packages.status` is dropped; the CHECK dies with the column.

**Down migration**: restores the uuid keys with fresh `gen_random_uuid()` values, the name-based order FK, the `packages.status` varchar + CHECK, and the original partial index. **Lossy in one respect, documented**: the regenerated master-list uuids are new values, not the originals — nothing outside the database stores them (the wire has only ever carried names), so no external reference breaks, but a uuid captured in an old log will not resolve. `created_by`/`updated_by` are dropped outright.

## 7.3 Query changes (all under `sqlc generate`)

| File | Change |
|---|---|
| `backend/internal/order/queries/order.sql` | Reads: `JOIN order_statuses os ON os.id = o.status_id` + `os.name AS status` on every order-returning statement (`:44`, `:52`, `:63`, `:181`, `:213`, `:274`). Writes/filters: `status = 'PENDING'` → `status_id = (SELECT id FROM order_statuses WHERE name='PENDING')` (`:33`, `:65`, `:125`, `:230`, `:239`, `:259`, `:270`); `SET status = $2` → `SET status_id = (SELECT id FROM order_statuses WHERE name = $2)` (`:32`); admin filter (`:184`) resolves `sqlc.narg(status)` the same way; `INSERT INTO orders (… status …)` (`:211`) inserts the resolved id. `ListActiveGenders` (`:244`) and the attendee-slot gender JOIN (`:293`) are unchanged in text — only the column type beneath them moves. |
| `backend/internal/event/queries/event.sql` | `p.status = 'ACTIVE'` → `p.is_active` (`:162`, `:206`, `:268`); package column lists swap `status` for `is_active` (`:155`, `:209`, `:216`, `:218`, `:226`, `:228`). |
| `backend/internal/payment/queries/payment.sql` | **No query change** — verified: it never selects from or updates `orders`. `payments.status` is the provider's raw transaction status, a different column with a different meaning; FR-026 does not touch it. Its comment at `:4` cites `orders.status = 'CANCELLED'` and goes stale — reword the comment, nothing else. |
| `backend/internal/ticket/queries/ticket.sql` | **Unchanged** — never references `orders`, and `tickets.status` is excluded by FR-029. |

## 7.4 Go changes — deliberately almost none

| Layer | Change |
|---|---|
| Order-status comparisons (29 files, incl. `payment/status.go:12-15`, `payment/service.go`, `order/service.go`, `order/admin_handler.go:144`, `notification/service.go:115`) | **None.** R19's SQL aliasing keeps every `sqlc` struct's `Status string` carrying the name, so `ord.Status != "PENDING"` still compiles and still means the same thing. |
| `internal/order` repository bindings | Regenerated by `sqlc`; `attendees.gender_id` binds as `int16`/`pgtype.Int2` instead of `uuid.UUID`. The name→id map in `service.go:398-408` changes its value type only. |
| `internal/event` package DTO + service | `Status string` → `IsActive bool` on the admin package DTO; the `ACTIVE`/`INACTIVE` literals leave the domain (FR-029). |
| `cmd/api/adapters.go` | **None** — no adapter carries a status or gender id. |

## 7.5 Frontend changes

| File | Change |
|---|---|
| `frontend/lib/types.ts:292` | Package `status: "ACTIVE" \| "INACTIVE"` → `is_active: boolean` |
| `frontend/lib/schemas.ts:106` | `z.enum(["ACTIVE","INACTIVE"])` → `z.boolean()` |
| `frontend/components/admin/package-form.tsx:68,:153-154` | `<select>` with two `<option>`s → a checkbox/toggle bound to `is_active`; default `true` |
| `frontend/components/admin/package-form.test.tsx:52` | Fixture follows |
| `frontend/lib/types.ts:183,:225` (`OrderStatus`) | **Unchanged** — FR-026 keeps names on the wire |
| `frontend/lib/types.ts:232` (ticket status union) | **Unchanged** — FR-029 excludes issued tickets |

## 7.6 End-of-journey modal (FR-022 – FR-024) — no schema impact

Frontend-only; listed here so the feature's data surface is complete. `frontend/components/order/expired-state.tsx` (full-page card, two buttons) is replaced by an `EndOfJourneyDialog` rendered *over* each order page's normal tree rather than in place of it. The order's EXPIRED/CANCELLED state is read from the same `status` field as today — which, per R19, does not change shape.

## 8. Form-step fee presentation (FR-016, FR-016a – FR-016c) — no schema impact, and no wire impact either

Listed for completeness, and to record a stronger claim than §7.6's: Track C changes neither storage nor the wire. It selects between two fields that both already exist on the guest order read.

| Field | Source | Track C effect |
|---|---|---|
| `orders.total_amount` | column, frozen at booking (TX-B) | **Unchanged** — same value stored, charged, sent to the gateway, and printed on the receipt |
| `orders.subtotal` | column, frozen at booking, `NULL` on pre-`000010` orders | **Unchanged** — newly *rendered* on the form step, having previously been rendered only on the payment step |
| `order_fees` rows | frozen per-order snapshot | **Unchanged** — same rows, same names, same amounts, same computation moment |
| `TicketOrderDetail.subtotal` / `.fees` / `.total_amount` | `backend/internal/order/dto.go:330-336` | **Unchanged** — no field added, removed, renamed, or retyped |

**Which figure each phase renders** (the whole of the change):

| Phase | Before | After |
|---|---|---|
| Registration (holder forms) | `total_amount`, note "Includes all taxes and fees" | `subtotal ?? total_amount`, note "taxes and fees added at the next step" |
| Awaiting payment (checkout) | `total_amount`, itemized rows above it | **Unchanged** |

The `??` is nullish, not falsy, by decision R28: `"0.00"` is a real subtotal and must render as `Rp 0`; only a `NULL` subtotal (an order predating fees) takes the fallback, and for such an order the stored total already excludes fees, so the fallback is exact rather than approximate.

**Invariant the tests must hold** (SC-005a): for every order, the amount charged after this change equals the amount that would have been charged before it. Track C has no legitimate path to violating this, which is precisely why it should be asserted — a display change that moves money would do so silently.

## 9. Restoring saved holder details (FR-030 – FR-033) — no schema impact; one new read, no new column

**No migration. No table, column, type, index or constraint changes.** Every value FR-030
restores is already stored and already read: `attendees.name`, `.email`, `.phone`, `.dob` and
`.gender_id` are written by checkout TX-D and returned — with the gender resolved to its master
row's NAME — by `ListAttendeeSlotsByOrderID` (§7.3). The restore renders data the order page
already fetches and currently discards, so nothing about storage moves.

### 9.1 The one storage-adjacent change: reading the whole gender list

FR-031 requires the checkout to accept a gender that is no longer active when it is the value
already recorded on that slot. Today the only gender read is `ListActiveGenders`
(`WHERE is_active`), which serves both `GET /ticket/genders` and the checkout's membership
check. Those two uses now need different sets, so they stop sharing one query:

| Read | Set | Used by | Change |
|---|---|---|---|
| `ListActiveGenders` | active only | `GET /ticket/genders` — the forms' options | **unchanged**, and must stay so: FR-031 widens one card's list, never the master list |
| `ListGenders` *(new)* | every row, with `is_active` | checkout validation + `gender_id` resolution | new `:many` returning `id, name, is_active` |

The new query adds no column and no table. `genders` keeps the shape §7.1 gave it.

### 9.2 Validation, restated as a rule over a slot rather than over the list

The gender rule stops being "is this name in the active list" and becomes a two-part rule, held
apart so the existing error precedence survives (research R34):

| Stage | Runs | Rule | On failure |
|---|---|---|---|
| Shape check | before the order is loaded, as today | the name exists in the **full** gender list | `400001`, `attendees[i].gender` — unchanged text, unchanged position |
| Slot allowance | after slots are matched | if the name is **inactive**, it MUST equal the gender already recorded on that same slot | `400001`, `attendees[i].gender` |

`gender_id` then resolves from the full map, so a retired name resolves to its real id rather
than to the zero value the active-only map would have produced.

**Invariants preserved**: a gender never appears on a slot that did not already carry it; no
guest submission reactivates a master row; `attendees.gender_id` remains a valid foreign key in
every path, including the retired one — which today it would not be.

### 9.3 What the restore reads, per field

| Wire field (`slots[i]`) | Stored as | Form field | Conversion |
|---|---|---|---|
| `name`, `email`, `phone` | `text` | same | none — verbatim |
| `dob` | `date`, crosses as `YYYY-MM-DD` | typed `DD/MM/YYYY` | inverse of `dobToIso`; must round-trip to the same date |
| `gender` | `gender_id` → master NAME via LEFT JOIN | select value | none — the wire already carries the name |
| `id` | `attendees.id` | the card's `slot_ids` | already used; a bundle unit's card seeds from its first slot (§7.5, spec 010 grouping) |

A `null` in any of these means the slot was never filled; that card renders empty (FR-030, last
clause). A partially-filled slot is not a state the API can produce — checkout validates every
field of every slot before TX-D opens and writes them in one transaction — so no per-field
fallback is specified.

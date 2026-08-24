# Data Model: Free Ticket Registration

**Feature**: 022-free-ticket-registration | **Date**: 2026-08-20

`SCHEMA.md` is the absolute source of truth for the database and **MUST be updated in the same
commit as the migration** (Constitution, Governance). This document describes the delta and the
reasoning; it does not replace `SCHEMA.md`.

## 1. Schema changes

### 1.1 `ticket_types.is_visible` (required)

```sql
ALTER TABLE ticket_types
    ADD COLUMN is_visible BOOLEAN NOT NULL DEFAULT TRUE;
```

| Property | Value | Why |
|---|---|---|
| Type | `BOOLEAN` | Two states, and the codebase already uses plain booleans for exactly this shape (`packages.is_active`, `genders.is_active`, `order_statuses.is_active`, migration 000013). |
| Nullability | `NOT NULL` | A third "unknown" state has no meaning and every read would have to defend against it. |
| Default | **`TRUE`** | Makes the migration total and backward-compatible with no backfill: every pre-existing ticket type stays visible and purchasable (SC-009). |
| Naming | `is_visible` | The user's decision, taken after `is_registration_only` had already been implemented. See §1.1b — the column is the NEGATION of what it replaced, not a rename. |

**The default is the dangerous part.** `is_visible` is the logical negation of the
`is_registration_only` this column replaced, whose default was `FALSE`. Copying that default across
would make every ticket type in the system invisible at once — every event's list emptied, every
purchase refused — while raising no error anywhere. SC-009 exists to catch exactly this, and can only
catch it against a database that already holds ticket types, which is why the migration is verified
against a populated database rather than an empty one.

**The name is narrower than the rule (FR-001a).** `FALSE` does not merely unlist the type. It carries
all three behaviours: excluded from every guest purchase surface, refused by booking, checkout and
availability, and ineligible for package composition. There is consequently no way to *just* hide a
ticket type — making a purchasable type invisible also makes it free to register for. Every place the
column is documented states this, and the admin control is labelled for the consequence rather than
for the column.

**No `CHECK` constraint tying it to `price`.** FR-003 makes the two independent: a registration
charges nothing regardless of the stored price, and pricing such a type at 0 is a convention, not an
invariant. A constraint would encode the wrong relationship.

**Index on this column**: none. The guest list query is already `WHERE event_id = $1`, served by the
existing `event_id` index; a partial index would filter a result set of single-digit rows per event.

### 1.1b Polarity: what inverts, and where

Because this is a negation rather than a rename, a find-and-replace over the ~119 references produces
code that compiles, typechecks, and is exactly backwards. The sites where the SENSE changes:

| Site | Was | Is |
|---|---|---|
| Migration default | `DEFAULT FALSE` | `DEFAULT TRUE` |
| Guest list filter | `AND NOT is_registration_only` | `AND is_visible` |
| Package-member guard | `if row.IsRegistrationOnly` | `if !row.IsVisible` |
| Purchase seam refusal | `if info.IsRegistrationOnly` | `if !info.IsVisible` |
| Registration seam refusal | `if !target.IsRegistrationOnly` | `if target.IsVisible` |
| FR-005 transition guard | `if req.X && !existing.X` | `if !req.IsVisible && existing.IsVisible` |
| Admin wire default | `false` | `true` |

The FR-005 guard is the one most likely to be got half-right: it is a compound already carrying one
negation, and inverting only one half guards the transition OUT of registration-only, which is always
safe and leaves FR-005 silently unenforced.

**The admin request DTO carries this as a POINTER**, not a plain bool. Under the old polarity an
omitted JSON key decoded to `false` = an ordinary purchasable ticket, which was harmless. Under the
new polarity it decodes to `false` = invisible, so a client that omits the key withdraws the ticket
type from sale — and since the update is an absolute full replace, that fires on an edit of any
unrelated field. Absent therefore means visible, matching the column default.

### 1.1a No supporting index on `attendees.email`

An earlier draft added `CREATE INDEX idx_attendees_email_lower ON attendees (lower(email))` for the
per-address duplicate check. FR-023 removed that rule, and with it the only read that filtered on the
address. The index is **not** carried forward: nothing queries it, and an unused index is write
amplification on attendee insert — the very path this feature adds load to.

**Column comment** (migration 000014 sets the precedent of documenting intent in the DDL):

```sql
COMMENT ON COLUMN ticket_types.is_visible IS
    'FALSE means REGISTRATION-ONLY, not merely unlisted: obtained at /events/:slug/register/:id rather than by buying, hidden from every guest purchase surface, refused by booking, checkout and availability, and ineligible for package composition. There is no way to only hide a type — clearing this also makes it free to register for (spec 022 FR-001a). Independent of price (FR-003). Defaults TRUE; an inverted default would silently hide every ticket type in the system.';
```

### 1.2 Registration origin marker on `orders` (required)

FR-033 requires a registration to be distinguishable from a purchase in stored data, and the
notification service must select its delivery shape from that distinction. The decision and the
alternatives are in [research.md](./research.md); the resulting column:

**No column is added.** The distinction is derived (revised 2026-08-20; see research D4):

```sql
EXISTS (
    SELECT 1 FROM order_items oi
    JOIN ticket_types tt ON tt.id = oi.ticket_type_id
    WHERE oi.order_id = o.id AND NOT tt.is_visible
) AS is_registration
```

Every existing order derives `FALSE`, so every existing delivery keeps its two attachments and every
existing admin view keeps its meaning. The cross-domain JOIN is permitted because this is a READ;
the registration WRITE still reaches quota only through `EventProvider` (Principle II).

**Not stable over time.** Making an invitation ticket type purchasable again reclassifies every
historical order that used it. Accepted deliberately — see research D4 for what that costs on the
resend path.

> **Why a column and not a derivation.** A registration *could* be inferred by joining
> `order_items → ticket_types.is_visible`. That was rejected: `orders` is the `order`
> domain's table and `ticket_types` is the `event` domain's, so the join would either violate
> Principle II or require a cross-domain interface call on every delivery — and `notification`
> would then need to know about ticket-type flags to decide what to attach. A marker on the row
> the domain already owns keeps the decision local and makes it a single indexed boolean read.

### 1.3 Nothing else changes

`attendees`, `tickets`, `order_items`, `order_fees`, `payments`, `event_terms` and `packages` are
**untouched**. That is the whole point of the order-anchored design: a registration produces rows
already shaped exactly as every downstream reader expects.

## 2. Rows a single registration produces

One submission, one transaction, five row-writes plus one quota update:

```text
orders                      1 row   total_amount = 0
                                    subtotal_amount = 0
                                    status → PAID              (Principle IV deviation)
                                    (registration: derived from its line)
                                    buyer_email/name/phone = the registrant's
                                    terms_agreed_at = now()
                                    event_terms_id = the accepted version
                                    payment_expires_at = NULL  (nothing to expire)
                                    payment_qr_string = NULL   (no gateway session)
  └─ order_items            1 row   ticket_type_id = the registration-only type
                                    quantity = 1
                                    unit_price = 0
  └─ attendees              1 row   name, email, phone, dob, gender_id — filled immediately,
                                    NOT the two-step empty-slot-then-fill of the purchase path
       └─ tickets           1 row   issued AFTER commit, by the existing
                                    ticket.IssueTicketsForOrder
ticket_types.quota          -1      atomic, row-locked, inside the same transaction
order_fees                  0 rows  a zero-total order has no fees (FR-027)
payments                    0 rows  no gateway session was ever opened (FR-029)
```

### 2.1 Field-by-field on `orders`

| Column | Registration value | Note |
|---|---|---|
| `order_number` | generated | Reuses `GenerateOrderNumber` + the existing collision-retry loop. A registration is an order; it gets a real number, which is also what admin resend and support conversations quote. |
| `total_amount` | `0` | |
| `subtotal_amount` | `0` | Not NULL — the fee-presentation fallback for null subtotals exists for pre-fee orders, and a registration is not one. |
| `status` | `PAID` | Resolved by name in-statement, as migration 0013 requires. |
| `is_registration` | *(derived)* | No column; computed from the line's ticket type. |
| `buyer_email` | registrant's email | The delivery address — `notification.SendTicketEmail` reads `orders.buyer_email`, **never the attendee's**, and refuses an empty snapshot ([service.go:191](../../backend/internal/notification/service.go#L191)). **Must be supplied on the `INSERT`**: `UpdateOrderBuyer` is guarded `AND status_id = (… 'PENDING')` ([order.sql:324-325](../../backend/internal/order/queries/order.sql#L324-L325)), so an order inserted at `PAID` is invisible to it and cannot be patched afterwards. |
| `buyer_name` / `buyer_phone` | registrant's | Same single holder. |
| `terms_agreed_at` | `now()` | Written in the same transaction, not by a second call — the registration has no held-order phase in which to record it separately. `RecordTermsAgreement` is `PENDING`-guarded and cannot be used here. |
| `event_terms_id` | the accepted document | FR-049. Records **which document**; the *version* is carried by `event_terms.updated_at` — see below. |
| `payment_expires_at` | `NULL` | Nothing is held; the row is already final. Keeps the expiry sweeper's partial index (`status_id = 1`) from ever seeing it. |
| `email_sent` | `FALSE` → `TRUE` | Set only after delivery succeeds (FR-037). |

## 3. Validation rules

Enforced **server-side authoritatively** (FR-024); the client mirrors them for immediacy with
identical message text (FR-025).

| Field | Rule | Source |
|---|---|---|
| `name` | non-empty after trim | FR-018; matches `CheckoutFormsRequest.Validate` |
| `email` | well-formed address | FR-019; reuse `emailShaped` |
| `phone` | `^[0-9]{12,15}$`, message *"Enter a phone number of 12-15 digits."* | FR-020; **reuse `visitorPhonePattern` and `visitorPhoneMessage` verbatim** from `order/dto.go:251-253` |
| `dob` | `YYYY-MM-DD`, not in the future | FR-021 |
| `gender` | a name in the active gender master | FR-022; reuse `activeGenders` |
| `agreed` | must be true | FR-016 |
| `event_terms_updated_at` | must equal the event's current terms `updated_at` | FR-050 — **not** the id; see below |

All field failures return in **one** `400` whose `data` is a `field → message` map, exactly as
`CheckoutFormsRequest.Validate` does.

### 3.0 The terms version token is `updated_at`, not the id

`UpsertEventTerms` is `ON CONFLICT (event_id) DO UPDATE SET content = …, updated_at = now()` and
`event_terms.event_id` is `UNIQUE`, so **an edit preserves the row id**. Comparing ids can therefore
never detect an edit — which is also why booking's existing `TERMS_CHANGED` refusal has never been
able to fire for the case it documents. See [research.md D8](./research.md).

The submission carries `event_terms_updated_at`; the server refuses when it differs from the current
document's. `orders.event_terms_id` is still stamped, so the stored record names both the document
and — through that document's current `updated_at` — the version. No schema change: `updated_at`
already exists on `event_terms` and already travels on `EventTermsDTO`.

### 3.1 No duplicate-email rule (FR-023)

> An address may register as many times as remaining quota allows.

This section previously specified a refusal keyed on the address. That rule was removed at the user's
direction after it had been implemented, and what replaces it is the **absence** of a rule — which has
to be stated rather than omitted, because an absence is only observable as a completed second write.

- **Nothing in the write path is keyed on the address.** No existence query, no address-scoped lock.
- **Two submissions of one address must not contend.** A leftover lock would serialise them invisibly
  and still pass a "both succeeded" row count, which is why the concurrency test asserts that every
  attempt succeeds rather than only counting rows afterwards.
- **Case is not a rule any more.** `Halo@Example.com` and `halo@example.com` are two registrations.
- **The only limits left** are remaining quota, the sales window, and the per-source throttle
  (FR-034) — now the sole bound on submission volume through the unlisted link rather than one of two.

The concurrency test was **inverted rather than deleted**: it used to prove six concurrent
submissions of one address produced exactly one ticket, and now proves they produce six with quota
exact. It earns its place more than it did before, being the only thing that would catch a lock left
behind.

## 4. State transitions

A registration order has **no lifecycle**. It is created final:

```text
(nothing) ──register──► PAID ──────────────────────► (terminal)
                         │
                         ├─ tickets issued  (after commit, async)
                         └─ email_sent      FALSE ─delivery ok─► TRUE
                                              └─ failure ─► stays FALSE, admin resend armed
```

It never becomes `PENDING`, is never swept by the expiry sweeper (no `payment_expires_at`), never
receives a webhook, and never restores quota. Cancelling or refunding a registration is out of scope.

The **ticket** it issues follows the ordinary lifecycle unchanged: `ACTIVE → USED` (irreversible),
with the admission-window outcomes from the ticket type's `event_start`/`event_end`.

## 5. Entity relationships (unchanged shape)

```text
events ──1:N── ticket_types ──(is_visible)
   │                 │
   │                 └──1:N── order_items ──N:1── orders (registration: derived)
   │                 └──1:N── attendees   ──N:1──────┘
   │                                │
   │                                └──1:1── tickets
   └──1:1── event_terms ◄── orders.event_terms_id
```

No new table, no new foreign key, no changed cardinality.

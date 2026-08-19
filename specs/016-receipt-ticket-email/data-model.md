# Phase 1 Data Model: Receipt + E-Ticket Email Attachments

**Feature**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md) | **Research**: [research.md](./research.md)

## Database

**No change.** No migration, no new column, no new table, no index. `SCHEMA.md` is not
touched — verified against the requirement set, not assumed: FR-036 forbids new stored
data, and every value the three surfaces render already has a column.

Where each rendered value comes from:

| Surface field | Source |
|---|---|
| Order number, status, total, subtotal | `orders.order_number`, `order_statuses.name`, `orders.total_amount`, `orders.subtotal` |
| Buyer name / email / phone | `orders.buyer_name` / `buyer_email` / `buyer_phone` |
| Order date | `orders.created_at` |
| Last updated | `orders.updated_at` (**newly read**, already stored — R-008) |
| Payment method, payment date | `payments.payment_type`, `payments.created_at` (**newly read** — R-002) |
| Event name / venue / address | `events.name` / `venue` / `address`, via `TicketOrderByNumber` |
| Line name, qty, unit price, subtotal | `order_items` + resolved display names |
| Line admission date | `PublicOrderItem.AdmissionStarts` (spec 015) |
| Line descriptor | `ticket_types.description` / `packages.description` (**newly read** — R-009) |
| Fee lines | `order_fees.name`, `order_fees.amount` (frozen) |
| Ticket code, holder name/email | `tickets.ticket_code`, `attendees.name` / `email` |
| Ticket type name, admission window | `ticket_types.name`, `event_start`, `event_end` |
| Branding | Configuration, not storage (R-004) |

## Domain structs

All types below live in `backend/internal/notification/`. Per Principle III they are the
domain's own; no `sqlc` or sibling-domain type appears in any field. Per Principle II they
name only Go primitives, `decimal.Decimal`, `time.Time`, and each other — which is what
keeps `cmd/api/architecture_test.go` green.

### `OrderDelivery` — widened

Existing fields are unchanged; the delivery path's contract for what an order *is* simply
grows to cover the two new documents.

```go
type OrderDelivery struct {
    // --- unchanged ---
    ID          uuid.UUID
    OrderNumber string
    BuyerName   string
    BuyerEmail  string
    Status      string
    TotalAmount decimal.Decimal
    Subtotal    *decimal.Decimal   // nil on pre-fee orders → breakdown collapses (FR-013)
    Items       []ReceiptLine
    Fees        []ReceiptFee

    // --- new ---
    BuyerPhone  string             // "" when unrecorded → the row is omitted, not blanked (FR-027)
    CreatedAt   time.Time          // "Order Date" in the email body (FR-024)
    UpdatedAt   time.Time          // "Last updated" on the receipt (FR-008, R-008)
    Event       DeliveryEvent      // FR-025
    Payment     PaymentSummary     // FR-010
}
```

**Validation / invariants**

- `Status` must be `PAID` before any document is rendered — already enforced at the top of
  `SendTicketEmail`, unchanged.
- `BuyerEmail` non-empty — already enforced, unchanged.
- `Subtotal == nil` ⟹ neither the receipt nor the body emits a subtotal row or any fee row
  (FR-013). This is the pre-fee-order path and it must stay exercised by a test.
- `TotalAmount` is the charged amount and the only figure guaranteed present (FR-014).

### `DeliveryEvent` — new

```go
type DeliveryEvent struct {
    Name    string
    Venue   string
    Address string
}
```

The event's *own* identity. Deliberately carries **no dates**: per constitution, a ticket
names its ticket type's admission window and an event names the event's — and every date
on these three surfaces is a ticket-type or per-line date (FR-020, FR-011). Omitting the
fields makes that impossible to get wrong by reaching for the nearest available date.

### `PaymentSummary` — new

```go
type PaymentSummary struct {
    Method string    // "QRIS"; falls back to provider name, then the literal "QRIS"
    Status string    // display form, e.g. "Paid"
    PaidAt time.Time // the settling payment row's created_at
}
```

`Method` resolution order (R-002): `payments.payment_type` → `orders.payment_provider` →
`"QRIS"`. The fallback chain exists so a null column never renders an empty cell in a
financial document.

### `ReceiptLine` — widened

```go
type ReceiptLine struct {
    // --- unchanged ---
    Name      string
    Quantity  int32
    UnitPrice decimal.Decimal
    Subtotal  decimal.Decimal

    // --- new ---
    AdmissionStarts []time.Time  // one per distinct admission day; a bundle may have several
    Descriptor      string       // the line's own description; "" omits the separator (R-009)
}
```

**Rendering rule for the sub-line** (FR-011, FR-026):

| `AdmissionStarts` | `Descriptor` | Sub-line |
|---|---|---|
| `[26 Apr]` | `"Expo Entrance Ticket"` | `26 Apr 2026 • Expo Entrance Ticket` |
| `[26 Apr]` | `""` | `26 Apr 2026` |
| `[26 Apr, 27 Apr]` | `"Two-day pass"` | `26 Apr 2026, 27 Apr 2026 • Two-day pass` |
| `[]` | `""` | *(sub-line omitted entirely)* |

A bundle spanning several days lists each day rather than collapsing to the earliest —
collapsing is precisely the defect spec 015 introduced `AdmissionStarts` to fix, and
re-introducing it on the receipt would undo that work in a new place.

### `ReceiptFee` — unchanged

```go
type ReceiptFee struct {
    Name   string           // frozen at booking, percentage baked in: "PPN (11%)"
    Amount decimal.Decimal
}
```

Printed verbatim (spec Assumptions). The design's varied labels — "Tax", "Platform Fee",
"Application Fee (Admin Fee)" — are mock text; the order's frozen names are authoritative,
because a receipt that renames a fee no longer reconciles with the total the buyer approved
at checkout.

### `TicketDetail` — widened

```go
type TicketDetail struct {
    // --- unchanged ---
    TicketCode     string
    AttendeeName   string
    AttendeeEmail  string       // now PRINTED, in full (FR-018, FR-034) — previously unprinted
    TicketTypeName string
    EventName      string
    Venue          string
    EventStart     time.Time    // the TICKET TYPE's window, never the event's (FR-020)
    EventEnd       time.Time
}
```

No new field. The change of note is semantic: `AttendeeEmail`'s existing doc comment says
*"not printed — it is the per-holder delivery address the sender groups by"*. Both halves
are now stale — grouping was removed when delivery collapsed to the buyer, and FR-018 puts
the address on the page. The comment must be corrected in the same change; a wrong comment
on a field about to change meaning is how the next reader gets misled.

### `Branding` — new

```go
type Branding struct {
    SiteName     string  // "JIVE"
    SiteURL      string  // "https://www.jive.co.id"
    SupportEmail string  // "help@manjo.com"
    LegalEntity  string  // "PT Manjo Teknologi Indonesia"
    Attribution  string  // "Powered By Manjo"
    LogoPath     string  // optional; empty or unreadable → text wordmark (R-005)
}
```

Injected into `NewService`, identical for every order (FR-035). Not stored, not per-event
(FR-036).

### `Attachment` / `Message` — unchanged

`Message.Attachments` is already `[]Attachment`
([smtp.go:19-24](../../backend/internal/notification/smtp.go#L19)) and `Compose` already
loops over it. Carrying two files needs no change to the mail layer at all.

## Derived values (computed at render time, never stored)

| Value | Rule | Requirement |
|---|---|---|
| Masked email | keep first 5 of local part + whole domain; `<5` → mask from char 2 | FR-033 |
| Masked phone | keep leading 5 + trailing 4; too short → mask from char 2 | FR-033 |
| `Ticket N of M` | N = 1-based page index, M = `len(tickets)` | FR-016 |
| Admission window text | existing `FormatTicketWindow` | FR-020 |
| Money | existing `formatIDR` — `Rp 550.000`, no cents | FR-037 |
| QR PNG | existing `RenderQR(ticket.TicketCode)`, in memory, never persisted | FR-017 |
| Attachment filenames | `receipt-<order_number>.pdf`, `tickets-<order_number>.pdf` | FR-004 |

## Cross-boundary mapping

`backend/cmd/api/adapters.go` is the only place a sibling domain's type touches these
structs.

```text
order.OrderRecord        ──┐
  BuyerPhone, CreatedAt,   │
  UpdatedAt (new read)     ├──► notification.OrderDelivery
                           │
order.TicketOrderDetail  ──┤      Event      ◄── PublicOrderEvent{Name,Venue,Address}
  (already fetched at      │      Items[]    ◄── PublicOrderItem{…,AdmissionStarts,Description}
   adapters.go:318)        │      Fees[]     ◄── PublicOrderFee
                           │      Subtotal   ◄── *money.Money
payment row (new read)   ──┘      Payment    ◄── PaymentSummary

ticket.FullDetail        ─────► notification.TicketDetail   (unchanged mapping)
```

**The one place the blast radius leaves `internal/notification`**: `PublicOrderItem` has no
description field today, so R-009 requires widening that DTO and the `lineDisplays` query
behind it. Additive and read-only, but it is a change to `internal/order`'s wire contract —
`PublicOrderItem` is serialised to guests on `GET /ticket/order/:order_id` — so it adds a
nullable JSON field there. Flag it in review.

## State transitions

None. This feature renders existing state and mutates nothing except the already-existing
`orders.email_sent` write, whose semantics are unchanged: set only after a successful
`Send`, and never reached if either document fails to render (FR-006).

```text
order PAID ──► render receipt ──► render e-tickets ──► Send ──► MarkEmailSent
                    │                    │               │
                    └── error ───────────┴───────────────┴──► no email, email_sent stays FALSE,
                                                              resend stays armed (FR-006)
```

---

# Revision 2 — Design-Review Deltas (2026-08-12)

## Database

**Still no change.** Verified again, not assumed: the corrections are presentation-only.
Every timestamp column involved is already `TIMESTAMP WITH TIME ZONE`, so the stored
instants are unambiguous and the day-boundary defect is purely a rendering bug —
`SCHEMA.md` and `migrations/` stay untouched.

## Struct deltas

| Type | Delta | Why |
|---|---|---|
| `Message` | `+ Inline []Attachment` | The body references images by content ID; those are message parts, not documents (FR-001, FR-023a) |
| `Branding` | `LogoPath` becomes required in practice; `+ PinPath` | FR-023a promotes the logo from ornament to requirement; `PinPath` is pending the FR-025 decision |
| `buildEmailBody` | returns `(html string, inline []Attachment)` | Makes a `cid:` reference with no matching part unrepresentable |

No change to `OrderDelivery`, `ReceiptLine`, `TicketDetail`, `DeliveryEvent` or
`PaymentSummary`. The corrections are about how existing values are *rendered*.

## New package-level value

```go
// jakarta is the display zone for every time on every surface.
var jakarta = sync.OnceValue(func() *time.Location { ... })  // FixedZone("WIB", 7*3600) fallback
```

Hardcoded rather than configured: `pkg/config`'s precedent is operator-tunable values, and
a wrong display zone would silently misstate a financial document.

## Derived values — updated

| Value | Rule | Changed |
|---|---|---|
| Money | `IDR 550.000` / `IDR 15.000,92` — `IDR` prefix, dot thousands, comma decimal, fraction only when non-zero, never truncated | **Yes** (was `Rp 550.000`) |
| Admission window | `26 Apr 2026 @ 10:00 - 21:00 WIB`, converted to Jakarta | **Yes** (was `Mon, 02 Jan 2006 15:04 MST`) |
| Order stamp (email) | `04 Jul 2026 06:56 WIB` | **Yes** — no comma |
| Transaction stamp (receipt) | `20 Apr 2026, 14:30 WIB` | **Yes** — with comma; must no longer share a formatter with the above |
| Receipt sub-line date | `27 Apr 2026`, converted to Jakarta | **Yes** — this is the day-boundary fix |
| Band colour | `#151A26` | **Yes** (was `#141B2D`) — must equal the logo's opaque background |
| Masking, page count, filenames, QR | unchanged | No |

## Invariant added

`SetLineCapStyle` / `SetLineJoinStyle` are sticky `Fpdf` state. Any function that sets them
restores `"butt"` / `"miter"` before returning — otherwise every later rule on the page
inherits round ends, which passes every assertion and still looks wrong.

---

# Revision 3 — Brand Refresh (2026-08-19)

**No change to this document.** Recorded explicitly so the absence is a finding rather than
an omission.

Revision 3 replaces a brand asset, resizes three header bands, re-aligns the e-ticket
footer and changes two configuration defaults. It adds no entity, widens no struct, alters
no field type, and touches no database object — so `SCHEMA.md` is untouched and no migration
exists.

`Branding` (above) keeps its exact shape; only the **values** two of its fields default to
change (FR-035a). See [contracts/notification.md](./contracts/notification.md) §Revision 3.

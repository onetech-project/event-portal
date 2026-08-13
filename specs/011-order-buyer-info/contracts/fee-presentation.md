# Contract — Form-step fee presentation (Track C, rev. 4)

Covers spec 011 **FR-016, FR-016a, FR-016b, FR-016c** under constitution **v4.0.0**.

This document exists mainly to state what does **not** change. Track C's whole risk is that
a display edit quietly becomes a pricing edit, so the API side is asserted rather than
assumed.

---

## 1. API contract — unchanged, byte for byte

`GET /ticket/order/:order_id` is **not modified**. No field is added, removed, renamed, or
retyped. The three fields Track C reads have shipped since migration `000010`:

```jsonc
{
  "total_amount": "530000.00",   // frozen at booking; the amount charged
  "subtotal": "500000.00",       // pre-fee sum of the lines; null on pre-000010 orders
  "fees": [                      // frozen snapshot, names carry their computed rate
    { "name": "PPN (11%)", "amount": "55000.00" },
    { "name": "Admin Fee", "amount": "1200.00" }
  ]
}
```

Source of truth: `backend/internal/order/dto.go:330-336`; frontend mirror at
`frontend/lib/types.ts:246-250`.

**No other endpoint changes.** In particular the payment-creation path continues to send
`total_amount` as the gateway's gross amount, and the receipt continues to render the
itemized breakdown. Any backend diff in a Track C commit is out of scope.

---

## 2. UI contract — which figure each phase renders

The panel takes one prop, `phase` (research R26), replacing the `showFeeBreakdown`
boolean. There is no default: both call sites state their phase.

| | `phase="registration"` (holder forms) | `phase="payment"` (checkout) |
|---|---|---|
| Route | `/events/{slug}/orders/{orderNumber}` | `/events/{slug}/orders/{orderNumber}/checkout` |
| Itemized rows (`Subtotal (N items)` + one per fee) | **absent** | present, unless `subtotal` is `null` |
| Headline heading | `Total Payment` | `Total Payment` |
| Headline figure | `subtotal ?? total_amount` | `total_amount` |
| Headline sub-line | taxes and fees are added at the next step | includes all taxes and fees |

Both phases keep the heading `Total Payment` (FR-016a) — the sub-line carries the
distinction, not the heading. Renaming the registration heading to `Subtotal` was
considered and rejected in the 2026-08-12 clarification; the rejection is recorded in the
spec's Assumptions so it is not re-litigated.

### Rules a reviewer can check directly

1. The fee-inclusive figure MUST NOT appear anywhere on the registration route — not as
   the headline, not in a row, not in the QRIS instructions block.
2. The registration sub-line MUST NOT claim the figure includes taxes or fees.
3. `subtotal === "0.00"` MUST render as `Rp 0`, not fall back to `total_amount`
   (nullish coalescing, not falsy — research R28).
4. `subtotal === null` MUST render `total_amount` (FR-016c). Such orders predate fees, so
   the two values are equal and the fallback is exact.

---

## 3. Money invariant (SC-005a)

For every order, across this change:

```
charged_after == charged_before
gateway_gross_amount_after == gateway_gross_amount_before
receipt_total_after == receipt_total_before
order_fees rows: identical (count, names, amounts)
```

A test that only inspects the rendered screen cannot see a violation of this. The e2e
scenario therefore asserts the charged amount alongside the two displayed figures.

---

## 4. Test data requirement (research R27 — read this before writing any test)

**Every fixture that exercises Track C MUST have `subtotal ≠ total_amount`.** Otherwise
the assertions pass identically against fixed and unfixed code.

Two concrete traps, both verified:

- `frontend/components/order/order-summary-panel.test.tsx:25-27` sets
  `total_amount: "150000.00"`, `subtotal: "150000.00"`, `fees: []`. New cases must not
  reuse that fixture unmodified.
- `e2e/support/db.ts:44-45` **TRUNCATEs `fees`**. Migration `000010` seeds `PPN 11%` and
  `Admin Fee 1200`, but the per-run reset deletes them, so every e2e order books with an
  empty fee set and `total_amount == subtotal`.

The e2e scenario must therefore arrange a fee first, through the real admin API:

```
POST /api/v1/admin/fees      (backend/internal/order/admin_handler.go:34)
```

Never by writing the `fees` table directly — AGENTS.md requires arrangement through the
real API, and that route is also what keeps cached reads honest.

Per constitution Principle VIII the scenario MUST be observed failing against unfixed
code before the fix lands. On the current fixture data an unfixed run would pass, which is
exactly the failure mode this section exists to prevent.

## 5. Panel contents outside the money figures (FR-013 / FR-014, amended 2026-08-13)

Added in rev. 5. Sections 1–4 govern **which figure** the panel renders; this section
governs **what else is on the card**. The two were settled a week apart and had been in
open disagreement with the shipped component until now.

The clarification bent the spec to the shipped design (Figma `206-3145`), so this section
documents the component **as it already is**. There is no production change to make here —
the contract exists so the negative assertions have a written source rather than being
inferred from a comment in a test file.

| Element | On the card? | Governing requirement |
|---|---|---|
| Event name, event date | **Yes** | FR-013 |
| Ticket line: name, quantity badge, line subtotal | **Yes** | FR-014 |
| Ticket line: per-unit price | **No** | FR-014 (amended) |
| Booking ID | **No** | FR-013 (amended) |
| Payment method section, QRIS fixed | **Yes** | FR-015 |
| Closing money figure and fee rows | per §2 | FR-016, FR-016a–c |

Two constraints on the amendment, both load-bearing:

- **The unit price is removed from the display, not from the order.** `quantity × unit
  price` remains the derivation of the line subtotal (FR-014), and the wire payload is
  unchanged — no field is dropped from `GET /ticket/order/:order_id`. A reviewer checking
  this looks at the rendered card, not at the API response.
- **The Booking ID is relocated in emphasis, not withdrawn.** It remains disclosed on the
  confirmation screen and in the receipt email. Removing it from the panel must not become
  a reason to remove it from those, which are where the guest keeps it.

**Assertion shape.** Both are absence assertions, and an absence assertion passes when the
component fails to render at all. Each MUST be paired with a rendered positive on the same
card in the same test block (the event name and the QRIS radio are already asserted at both
sites). Choose the fixture so the derived unit price cannot collide with the line subtotal,
the grand total, or a fee amount — otherwise the assertion stops distinguishing anything.

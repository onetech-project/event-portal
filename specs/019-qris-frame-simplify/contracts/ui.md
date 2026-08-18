# UI Contract: the Scan to Pay card

**Feature**: `019-qris-frame-simplify` | **Date**: 2026-08-18 | **Phase**: 1

This feature exposes no HTTP interface. What it does expose — and what the acceptance
suite asserts against — is the card's element inventory. It is written as a contract
because the requirements are stated in both directions: certain things must be present,
and five specific things must be absent. An inventory that only lists what is shown
cannot express the second half.

Reference design: Figma node `203-1201` in file `kelTjyctfqKz5pJQ90U6BM`.
Component: [`frontend/components/order/qris-panel.tsx`](../../../frontend/components/order/qris-panel.tsx).

---

## MUST be present — payable order

Top to bottom, in this order. The frame is the 522 × 735 region; the total sits beneath it.

| # | Element | Nature | Source | Notes |
|---|---|---|---|---|
| 1 | QRIS lockup — "QR Code Standar Pembayaran Nasional" | image | `/brand/qris-lockup.png` | trademark; imaged, never redrawn |
| 2 | GPN mark | image | `/brand/gpn-logo.png` | trademark; imaged, never redrawn |
| 3 | "Scan to Pay" | **heading** | static | moves inside the frame (research D-2); must remain addressable by heading role |
| 4 | "Use any e-Wallet or Mobile Banking app supporting QRIS." | text | static | one line; replaced when the order has ended |
| 5 | The live QR code | image | `apiOrigin() + payment.qr_image_path` | rendered on demand by the API from the stored payload; never re-encoded or optimised |
| 6 | "SATU QRIS UNTUK SEMUA" | text | static | scheme furniture |
| 7 | "Cek aplikasi penyelenggara di: www.aspi-qris.id" | text | static | scheme furniture |
| 8 | "Cara bayar dengan QRIS:" + three step icons with captions | image + text | `/brand/qris-step-*.png` | sits in the red corner wedge |
| 9 | "Total amount due" + the formatted amount | text | `payment.amount` \| `totalAmount` | **below** the frame, not inside it |

Decorative and unlabelled, present but not asserted: the batik ground
(`/brand/qris-batik.png`), the red chevron bleeding off the left edge, and the red corner
wedge. All three are `aria-hidden`.

## MUST NOT be present — any order state

| Removed | How it read before | Was fed by |
|---|---|---|
| Merchant name | bold uppercase line under the lockups | `QRIS_MERCHANT_NAME` |
| Merchant registration number | `NMID : 936008580287697876` | `QRIS_MERCHANT_ID` |
| Terminal label | e.g. `659` | `QRIS_TERMINAL_LABEL` |
| Acquirer code | `Dicetak oleh : 93600008` | `QRIS_ACQUIRER_CODE` |
| Printed-layout version | `Versi Cetak : 1.0-2024.11.13` | `QRIS_PRINT_VERSION` |

The last two occupied a single absolutely-positioned block at the frame's lower left.
That block is removed in its entirety, not emptied.

**Absence is unconditional.** It does not depend on the environment leaving the five
values unset — the values are no longer read at all. The acceptance rig proves this by
setting them and asserting they do not appear (research D-3).

## MUST be present — ended order (`EXPIRED` / `CANCELLED`)

Elements 1–4 and 6–9 above are unchanged, with these substitutions:

| # | Element | Ended value |
|---|---|---|
| 4 | subtitle | "This order can no longer be paid." |
| 5 | QR code | replaced in place by a dashed placeholder carrying: `CANCELLED` → "This order was cancelled, so no code can be shown."; `EXPIRED` → "The payment time ran out, so this code is no longer valid." |
| 9 | total label | "Order total" instead of "Total amount due" |

The frame stays. It must not collapse to a void beside the end-of-journey dialog, and none
of the five removed fields may return in this state.

---

## Instruction contract: "How to pay with QRIS"

Component:
[`frontend/components/order/qris-instructions.tsx`](../../../frontend/components/order/qris-instructions.tsx).

**MUST**: the verification step names the exact formatted amount due, and instructs the
guest to stop without entering a PIN if their app shows a different amount.

**MUST NOT**: name a configured merchant; instruct the guest to compare against a merchant
name printed above, around, or on the card.

The component no longer imports from `@/lib/env`.

---

## Configuration contract

### Retired

`QRIS_MERCHANT_NAME`, `QRIS_MERCHANT_ID`, `QRIS_TERMINAL_LABEL`, `QRIS_ACQUIRER_CODE`,
`QRIS_PRINT_VERSION` — together with their `NEXT_PUBLIC_`-prefixed legacy forms.

Removed from `frontend/lib/runtime-config.ts`, `frontend/lib/env.ts`,
`frontend/.env.example`, `docker-compose.yml` and the `README.md` configuration table.

### Compatibility guarantee

A deployment that still exports any of the five starts normally and ignores them. There
is no warning, no deprecation notice and no failure — an unknown environment variable is
simply not read. This is asserted, not merely asserted-to-be-true: the acceptance rig
exports all five on every run.

### Remaining

`API_BASE_URL` (legacy `NEXT_PUBLIC_API_BASE_URL`) — unchanged, still the only
per-environment value the browser needs, still resolved at start-up so one image can be
promoted from UAT to production without a rebuild.

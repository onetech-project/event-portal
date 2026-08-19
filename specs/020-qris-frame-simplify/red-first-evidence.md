# Red-first evidence (SC-005, task T006)

**Date**: 2026-08-18
**Ran against**: `frontend/components/order/qris-panel.tsx` **unmodified** — the acceptance
rig (T001) wired, the assertions (T004, T005) written, no source change yet.

```
cd e2e && npm test -- guest-purchase \
  --grep "payment card shows the code|browses, books, pays"
```

## Result: 2 failed

```
2) [chromium] › specs/guest-purchase.spec.ts:528:3 › Guest purchase, end to end ›
   the payment card shows the code and none of the retired frame fields

  Error: payment card still renders E2E-MERCHANT-MUST-NOT-RENDER

  expect(received).not.toContain(expected) // indexOf

  Expected substring: not "E2E-MERCHANT-MUST-NOT-RENDER"
  Received string:        "Scan to PayUse any e-Wallet or Mobile Banking app supporting
  QRIS.E2E-MERCHANT-MUST-NOT-RENDERNMID : E2E-NMID-MUST-NOT-RENDERE2E-TERMINAL-MUST-NOT-
  RENDERSATU QRIS UNTUK SEMUACek aplikasi penyelenggaradi: www.aspi-qris.idDicetak oleh :
  E2E-ACQUIRER-MUST-NOT-RENDERVersi Cetak : E2E-VERSION-MUST-NOT-RENDERCara bayar dengan
  QRIS:Buka Aplikasi\nBerlogo QRISScan & CekBayarTotal amount dueRp 250.000"

     at ../support/journey.ts:256

  2 failed
    [chromium] › specs/guest-purchase.spec.ts:70:3  › browses, books, pays and receives tickets
    [chromium] › specs/guest-purchase.spec.ts:528:3 › the payment card shows the code and none
                                                      of the retired frame fields
```

## Why this is the evidence that was wanted

The received string is the whole payment card, and every one of the five retired fields is
in it — merchant name, `NMID : …`, terminal label, `Dicetak oleh : …`, `Versi Cetak : …`.
The assertion is looking at the right element and the element really was printing them.

Both the dedicated scenario and the main purchase journey failed, which confirms the check
in `expectAwaitingPayment()` reaches every scenario that passes through the payment screen,
not only the one written for it.

Without T001's env block this run would have been **green in CI** (no `frontend/.env`
there, so `runtimeConfigFromEnv()` resolves all five to `""` and the component already
renders nothing). See [research.md D-3](./research.md).

## Baseline (T003)

The full suite was green immediately before these assertions were added — `npm test`,
exit 0 — so the two failures above are attributable to the new assertion and nothing else.

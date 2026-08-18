# Data Model: QRIS Frame Simplification

**Feature**: `019-qris-frame-simplify` | **Date**: 2026-08-18 | **Phase**: 1

## Database: no change

No migration. No table, column, index, constraint or enum is added, altered or dropped.
[SCHEMA.md](../../SCHEMA.md) is **not** edited by this feature, and the
"`SCHEMA.md` changes in the same commit as any migration" rule from
[AGENTS.md](../../AGENTS.md) is not engaged because there is no migration.

## API contract: no change

No request or response shape moves. The payment card is rendered from data the frontend
already receives:

```ts
// frontend/lib/types.ts — unchanged, listed for reference
type PaymentInstruction = {
  method: string;
  provider: string;
  amount: string;
  expires_at: string;    // server-owned deadline; the countdown reads this
  qr_image_path: string; // the API renders the QR on demand from the stored payload
};
```

`qr_image_path` continues to resolve against the API origin and continues to be rendered
un-optimised, for the reason the component already records: re-encoding a QR risks
scannability, the one property it must keep. FR-008 makes this explicit — nothing about
what is encoded, requested from, or reported to the gateway changes.

## The one type that does change: `RuntimeConfig`

`frontend/lib/runtime-config.ts` defines the per-environment settings the server resolves
and injects into the document for the browser to read back. Five of its six fields are
retired.

### Before

| Field | Environment name (runtime / legacy) | Read by |
|---|---|---|
| `apiBaseUrl` | `API_BASE_URL` / `NEXT_PUBLIC_API_BASE_URL` | every browser fetch, and `apiOrigin()` for the QR image |
| `qrisMerchantName` | `QRIS_MERCHANT_NAME` / `NEXT_PUBLIC_…` | `qris-panel.tsx`, `qris-instructions.tsx` |
| `qrisMerchantId` | `QRIS_MERCHANT_ID` / `NEXT_PUBLIC_…` | `qris-panel.tsx` |
| `qrisTerminalLabel` | `QRIS_TERMINAL_LABEL` / `NEXT_PUBLIC_…` | `qris-panel.tsx` |
| `qrisAcquirerCode` | `QRIS_ACQUIRER_CODE` / `NEXT_PUBLIC_…` | `qris-panel.tsx` |
| `qrisPrintVersion` | `QRIS_PRINT_VERSION` / `NEXT_PUBLIC_…` | `qris-panel.tsx` |

### After

| Field | Environment name (runtime / legacy) | Read by |
|---|---|---|
| `apiBaseUrl` | `API_BASE_URL` / `NEXT_PUBLIC_API_BASE_URL` | every browser fetch, and `apiOrigin()` for the QR image |

`RuntimeConfig` becomes a one-field type. That is fine and should not tempt anyone to
collapse it into a bare string — the injection mechanism, the `<` escaping in
`runtimeConfigScript()`, and the runtime-over-build-time precedence are all still needed,
and the type is the natural place for the next per-environment value to land.

### Removal checklist for the five fields

| Location | What goes |
|---|---|
| `frontend/lib/runtime-config.ts` | five `RuntimeConfig` fields, five `read(…)` lines in `runtimeConfigFromEnv()`, and the doc-comment paragraph that uses the QRIS identity as its worked example |
| `frontend/lib/env.ts` | `qrisMerchantName`, `qrisMerchantId`, `qrisTerminalLabel`, `qrisAcquirerCode`, `qrisPrintVersion` and their two doc blocks |
| `frontend/lib/runtime-config.test.ts` | the five names from `RUNTIME_NAMES`, the five imports, and the QRIS-based precedence assertions (re-expressed per research D-5) |
| `frontend/.env.example` | five entries and their two comment blocks |
| `docker-compose.yml` | five `QRIS_*` service-env entries and the comment above them |
| `README.md` | two rows of the per-environment configuration table, and the sentence about the QRIS fields being left unset on purpose |

`apiBaseUrl` remains the marker for "the server really did inject this" in
`runtimeConfig()` — that logic is unaffected and must not be simplified away while the
field count drops.

### Invariants preserved

- **Empty is unset, not a value.** `runtimeConfigFromEnv()` treats `""` as absent and
  falls through to the legacy name and then the default. This is still tested, on
  `API_BASE_URL` (research D-5).
- **Injected wins, including when empty.** `runtimeConfig()` spreads the injected payload
  over the environment-derived one. Unchanged.
- **Unknown environment names are inert.** A deployment still exporting the five retired
  names is not consulted for them and does not fail (FR-013).

## The rendered card: an element inventory, not an entity

The payment card holds no state of its own; it is a projection of `PaymentInstruction`
plus two props. Because the acceptance criteria are stated as "shows exactly these
elements and none of those", the inventory is written down as a contract rather than as a
data model — see [contracts/ui.md](./contracts/ui.md).

Component inputs are unchanged:

```ts
type Props = {
  payment: PaymentInstruction | null;      // null once the order is no longer payable
  totalAmount: string;                     // so the frame can still name the sum
  endedStatus?: "EXPIRED" | "CANCELLED" | null;
};
```

`endedStatus` keeps its full meaning: the frame stays after an order ends and only the
scannable code is replaced (FR-009). The two ended messages are unchanged text.

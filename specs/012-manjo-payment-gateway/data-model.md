# Phase 1 Data Model: Manjo Payment Gateway Migration

**Feature**: [spec.md](./spec.md) | **Research**: [research.md](./research.md)

## Schema changes

**None.** No table, column, index, constraint, or migration. `SCHEMA.md` needs no update, which
satisfies the constitution's sync rule vacuously.

Two things that look like they need storage do not:

- **Gateway configuration** moved to the environment (R5), not the database — reversing the original
  request after consideration.
- **Reconciliation state** rides on the existing `payments` audit table (R4).

## Existing entities: how their meaning changes

### `orders`

| Column | Change |
| --- | --- |
| `payment_expires_at` | **Meaning changes.** Was a server-computed deadline (`now + PAYMENT_WINDOW`). Now it is the expiry the gateway returned (`qr_ea`), adopted verbatim. Falls back to the computed value only when no usable expiry came back, and that fallback raises a signal (FR-009b). |
| `payment_qr_string` | Unchanged in role. Now written exactly once per order — the refresh that used to overwrite it is withdrawn (FR-010). |
| `payment_url` | Now holds the gateway's hosted image address (`qr_u`) when present. Often empty; both samples return `""`. Never a page the guest is sent to. |
| `status` | Unchanged. Same five states, same transitions, same CHECK-constrained values. |
| `subtotal`, `total_amount` | Unchanged. FR-002b confirms the existing math rather than altering it. |

The deadline change carries a subtlety worth stating: `payment_expires_at` serves **both** the
booking hold and the payment window, and one sweeper query expires both. Only the payment-phase
value's *source* changes. The booking hold is still `now + BOOKING_HOLD`, still ours.

### `payments`

Structurally unchanged. Its role widens from "log of what the provider said" to "log of what the
provider **or an operator** said about this order", which the columns already support.

| Column | Use |
| --- | --- |
| `provider` | `"manjo"` for gateway-originated rows; unchanged in role. |
| `transaction_id` | The gateway's `nti` (network transaction id) on callbacks; `eri` on session open; for operator rows, the reference the operator supplied. `NOT NULL`, so operator rows must carry something — see marker table. |
| `payment_type` | `"qris"`. |
| `status` | Carries either the provider's raw status **or** one of the markers below. This is not a new technique: the retired `QR_REISSUED` marker used the same column the same way. |
| `raw_response` | Full inbound payload for gateway rows; the who/when/why envelope for operator and marker rows. |

#### Marker rows

| `status` | Written when | `transaction_id` | `raw_response` carries |
| --- | --- | --- | --- |
| `DISPUTED` | terminal-failure notification for an order already `PAID` (FR-016b) | the contradicting `nti` | the contradicting payload plus the settling row's id |
| `SETTLED_AFTER_EXPIRY` | a redelivered `Completed` revives an expired order (FR-019) | the `nti` | the prior status and the quota lines re-taken |
| `SETTLE_REFUSED_NO_QUOTA` | a redelivered `Completed` could not be honoured because the seats are short (FR-019c) | the `nti` | the shortfall per ticket type, so an operator can size the top-up |
| `SESSION_DUPLICATE` | session-open answered duplicate-reference (FR-007d) | the order's own reference | the error body and attempt count |

`ORPHAN_PAYMENT` and `MANUAL_RECONCILED` are **withdrawn**. The first meant "a payment we refuse to
honour", which is no longer an outcome for an expired order — that case now settles. The second
recorded a human asserting a payment, which FR-022d forbids outright.

The two replacements are not a rename. `SETTLED_AFTER_EXPIRY` records something that *did* happen and
answers the question an oversold event provokes — why does this ticket type's issued count exceed the
allocation it was given? `SETTLE_REFUSED_NO_QUOTA` records something that did *not* happen, and
carries the number the operator needs to make it happen next time.

Marker rows are **written in addition to**, never instead of, the audit row for the payload that
triggered them. FR-018 requires every notification recorded in its original form; a marker is a second
statement about it, not a replacement.

#### Why markers rather than a derived query

With the worklist withdrawn (FR-022h) the markers are no longer a queue to be worked — they are the
order's history, read when someone looks that order up. They are still written rather than derived,
and the reason has changed shape rather than disappeared.

A marker records **what was true at the moment a decision was taken**: the order's status when a
contradiction arrived, the quota lines re-taken when a settle succeeded, the shortfall when one was
refused. None of that is recoverable afterwards. Quota moves on, statuses move on, and a query that
re-derived "was this a contradiction?" from today's state would give a different answer next month —
and a different one again if the status mapping were ever changed.

The Principle II argument survives too: deriving any of this would mean joining `payments` to
`orders` across a domain boundary, where writing at detection time costs nothing because
`applyProviderResult` already holds the order's status.

## Domain types (Go, `internal/payment`)

These are in-memory shapes, not storage. Listed because they cross the `Gateway` boundary and
Principle V governs them.

### `Gateway` interface

```
Name() string
CreateTransaction(ctx, TransactionRequest) (PaymentSession, error)
VerifyWebhook(payload []byte, token string) (*WebhookResult, error)
```

`FetchStatus` is **removed** (FR-022). The remaining two are exactly the pair Principle V names.
`VerifyWebhook`'s second parameter changes meaning from *signature* to *presented bearer token*; the
method's role — authenticate an inbound notification and normalise it — is unchanged.

### `TransactionRequest` (unchanged shape, narrower use)

`OrderNumber`, `GrossAmount`, `CustomerName`, `CustomerEmail`, `CustomerPhone`, `Items`.

`Items` is now unused by the adapter — the Manjo contract has no line-item field, only a single
remark. Keep the field (it is the gateway-neutral shape, and a future provider may want it) but do not
send it. `CustomerEmail` and `CustomerPhone` likewise have no destination in this contract.

### `PaymentSession`

| Field | Source | Note |
| --- | --- | --- |
| `ProviderRef` | `d.eri` | also recoverable from the payload at tag `62`→`05` |
| `QRString` | `d.mr.qr_r` | must be non-empty or the session is treated as failed (FR-006) |
| `QRImageURL` | `d.mr.qr_u` | usually `""` |
| `ExpiresAt` | `d.mr.qr_ea` | **now authoritative** — parsed as an offset-bearing timestamp, stored as the instant it denotes (FR-009a) |
| `RedirectURL` | — | stays empty; QRIS never redirects |

### `WebhookResult`

| Field | Source | Note |
| --- | --- | --- |
| `OrderNumber` | `ri` | now a **direct** lookup — no suffix stripping (FR-010b) |
| `TransactionID` | `nti` | |
| `TransactionStatus` | `s` | the enum's name, preserved verbatim for audit (FR-018) |
| `FraudStatus` | — | unused; the contract has no fraud concept. Retained on the struct so `MapProviderStatus` keeps its shape. |
| `PaymentType` | `tt` / method | deposit-only; anything else is refused (FR-020) |
| `RawPayload` | whole body | |

### Contract module types

All wire shapes come from the contract module at `v0.0.0-20260810052544-9d11826f4864` — the request
(`paynet.TransactionRequest`), the method enum (`method.Method`), the QR response
(`method.QRResponse`, which publishes `qr_ea` as of that version), the status enum (`status.Status`),
and the callback (`callback.Request`). No locally-redefined wire struct, so there is one source of
truth per shape.

`method.QRResponse.ExpiredAt` is a `time.Time`, so the parse and the offset handling happen at the
contract boundary — FR-009a is carried by the type. The zero value is the catch: an absent `qr_ea`
decodes to a valid year-1 timestamp rather than to nothing, so the missing case must be detected by an
explicit zero check, not inferred from it looking expired (FR-009b).

## State transitions

**One transition is added** to the lifecycle this feature otherwise leaves alone: `EXPIRED → PAID`,
driven by a redelivered notification (FR-019). Everything else is as before.

```
                    Completed                         ┌─ DISPUTED marker + signal
        ┌──────────────────────────────► PAID ────────┤  (Reject/Cancel/Expired arriving after)
        │                                  ▲          └─ repeat Completed → quiet no-op
        │                                  │
   PENDING ──── Reject | Cancel ─────► CANCELLED      │  Completed arriving after
        │                                  │          │  ─► refused, recorded, signalled
        │                                  └──────────┘  (FR-019d: never revived)
        │
        │──── Expired | deadline ────► EXPIRED ───► Completed redelivered (FR-019)
        │                                             ├─ quota covers the hold  ─► PAID,
        │                                             │    seats re-taken, tickets issued
        │                                             └─ quota short (FR-019c) ─► unchanged,
        │                                                  recorded + signalled, answered 200
        │
        └──── Pending | Obscure | unknown ─► unchanged (Obscure/unknown also signal)
```

Three invariants the diagram encodes:

- **`PAID` is one-way.** Nothing moves an order out of it — not a later notification, not a person.
  The asymmetry with the new `EXPIRED → PAID` edge is deliberate: the gateway may tell us about a
  payment we missed, but it does not get to take one back once tickets are in a buyer's hands.
- **Only expiry is reversible.** A `CANCELLED` order is not revived by a later completion (FR-019d).
  Expiry is *our* verdict, reached because a notification never came; cancellation is the gateway's,
  or the consequence of a code that was never issued. Reversing our own verdict is honest; reversing
  the gateway's is contradicting the source of truth this design rests on.
- **Quota moves exactly once per transition**, restored by whichever path wins the guarded change —
  sweeper, callback, or duplicate-reference release — and re-taken in the same transaction as a
  settle, in full or not at all (FR-019b).

## Validation rules

| Rule | Source | Enforced where |
| --- | --- | --- |
| Reference ≤ 25 chars, guaranteed by construction not truncation | FR-010a | session-open, before the call |
| Amount is the frozen order total, unrecomputed | FR-002b | session-open |
| Empty `qr_r` ⇒ failed session | FR-006 | response parse |
| Expiry absent/unparseable/past ⇒ fallback + signal | FR-009b | response parse |
| Expiry materially longer than expected ⇒ adopt **and** signal | FR-009c/d | response parse |
| Callback token mismatch ⇒ non-200, nothing recorded as accepted | FR-012 | handler, before parse |
| Non-deposit transaction type ⇒ no order change, recorded, 200 | FR-020 | service |
| Unknown reference ⇒ no order change, recorded, signal, 200 | FR-013 | service |
| Already `PAID` + same outcome ⇒ quiet no-op, 200 | FR-016 | service |
| Already `PAID` + terminal failure ⇒ `DISPUTED`, 200 | FR-016b | service |
| `EXPIRED` + `Completed` + quota covers the hold ⇒ `PAID`, seats re-taken, tickets issued | FR-019, FR-019b | service |
| `EXPIRED` + `Completed` + quota short ⇒ unchanged, recorded, signalled, 200 with an error body | FR-019c | service |
| `CANCELLED` + `Completed` ⇒ never revived; recorded, signalled, 200 | FR-019d | service |

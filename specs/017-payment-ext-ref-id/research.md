# Phase 0 Research: Payment External Reference ID

**Feature**: [spec.md](spec.md) | **Plan**: [plan.md](plan.md) | **Date**: 2026-08-14

The spec left no `NEEDS CLARIFICATION` markers — both were resolved with the user before
planning began. What remained were design questions the spec deliberately deferred, and one
correctness trap that only appeared on close reading of the checkout path. This document
records them.

---

## Finding 0: What the code already does with the reference

Established before deciding anything, because it bounds every option below.

| Fact | Evidence |
|------|----------|
| The reference arrives only on the session-open answer | `sessionOpenPath = "/v1/manjo/transaction/incoming"` ([manjo.go:24](../../backend/internal/payment/manjo.go#L24)); `ProviderRef: parsed.Data.ExternalRefID` ([manjo.go:239](../../backend/internal/payment/manjo.go#L239)) |
| Inbound notifications do **not** carry it | `callback.Request` has `ri` (our order number), `nti` (network txn id), `s`, `td`, `tft`, `tt`, `ai.rrn` — no `eri` |
| It already reaches the order domain, provider-neutral | `order.PaymentSession.ProviderRef`, mapped in [adapters.go:155](../../backend/cmd/api/adapters.go#L155) |
| …and is then dropped | `ProviderRef` has **zero** readers outside the mapping itself and one test fixture |
| Nothing writes `payments` at session-open time today | `CreatePayment` has exactly two callers, both on the notification path ([service.go:360](../../backend/internal/payment/service.go#L360), [service.go:506](../../backend/internal/payment/service.go#L506)) |
| A payments row written *during checkout* is nonetheless established practice | `ReleaseDuplicateSession` writes `SESSION_DUPLICATE` from the composition root on the checkout path ([service.go:538](../../backend/internal/payment/service.go#L538), called from [adapters.go:140](../../backend/cmd/api/adapters.go#L140)) |
| The codebase already documents a session-open row that does not exist | "The session-open row carries the instrument; the settling callback carries the time." ([service.go:798](../../backend/internal/payment/service.go#L798)) |
| The e2e stub already emits `eri` and exposes an unused accessor for it | `eri: externalRefId` and `externalRefFor(refId)` ([gateway-stub.ts:137,166](../../e2e/support/gateway-stub.ts#L137)) — `externalRefFor` has no callers |

The last two matter more than they look. This feature is not bolting a new concept onto the
system; it is completing one the system was already written as though it had.

---

## Decision 1: The reference gets its own `payments` row, not a column on a notification row

**Decision**: Add `payments.ext_ref_id` and write **one new row per order** at session open,
with `status = 'SESSION_OPENED'` — a marker, in the sense the payment domain already uses
that word.

**Rationale**:

- `payments` is append-only by design ("Rows are never updated in place: the sequence of
  notifications is itself the audit trail" — [repository.go:37](../../backend/internal/payment/repository.go#L37)).
  Back-filling the column onto an existing row would be an in-place update of an audit
  record, which is the one thing this table refuses to do.
- There is no row to back-fill *onto* at session-open time. The first notification may never
  arrive — which is precisely the case this feature exists to serve.
- The row carries something a column alone cannot: **a timestamp for when the session
  opened**, which is the start of the support timeline.
- `SESSION_OPENED` is the exact mirror of the existing `SESSION_DUPLICATE`. One marker when
  the gateway issues a code, one when it refuses to. The pair reads as a single sentence in
  the payment history.
- It makes [service.go:798](../../backend/internal/payment/service.go#L798)'s stated
  rationale true. `SettlementForOrder` resolves the payment instrument from "the session-open
  row"; today it silently gets it from a notification row instead.

**Alternatives considered**:

| Alternative | Rejected because |
|-------------|------------------|
| Reuse `transaction_id` for the reference | That column means "the gateway's network transaction id, as delivered on a notification". The admin panel's **Transaction** column renders it under that meaning. Two different gateway identifiers in one column would make that column lie. |
| Add the column, write no row, back-fill on the first notification | The stranded order — no notification ever arrives — is the whole use case. It would store the reference exactly when it is not needed. |
| Store it only in `raw_response` JSON | Not queryable without a JSONB reach-in from a read that must stay simple, and invisible to the wire shape. |

**Consequence to accept**: the payment history table in the admin dialog gains one row per
order (`SESSION_OPENED`, badged "our note" like every other marker). Spec FR-003/FR-019 say
existing history must be unchanged — existing rows *are* unchanged, and checkout-time markers
already appear there today via `SESSION_DUPLICATE`, so no new kind of thing is introduced.
Called out here so it is a decision rather than a surprise.

---

## Decision 2: Where the write happens — and the race it has to survive

**Decision**: Write the row from the **composition root's payment adapter, immediately after
`CreateTransaction` succeeds** — before TX-P, not after it.

This was the hardest call in the plan, so both candidates are recorded in full.

### Candidate A — write immediately after the gateway answers (chosen)

`orderPaymentAdapter.CreateSession` already holds the payment service (it calls
`ReleaseDuplicateSession` on the failure branch). On the success branch it calls
`RecordSessionOpened` and then maps the session onto the order domain's shape.

- **The reference survives a TX-P failure.** If stamping the order fails, the code exists at
  the gateway but the order carries no QR. A retry re-opens under the same reference, the
  gateway refuses it as a duplicate, and the order is released as unpayable. That order is
  stranded *by definition* — and Candidate B would have recorded nothing for it. The one
  order that most needs a gateway reference is the one B loses.
- Symmetric with the existing failure branch: success writes `SESSION_OPENED`, refusal writes
  `SESSION_DUPLICATE`, both from the same adapter, both via the payment service.
- Outside every transaction, by construction — `CreateSession` is called between TX-D and
  TX-P.

### Candidate B — write after TX-P reports `stamped == true` (rejected)

- Would guarantee **exactly one** row per order, written by the checkout that actually won,
  eliminating the ambiguity below outright.
- Rejected for the bullet above: it drops the reference on precisely the stranded-order path,
  which inverts the feature's purpose.

### The race, stated honestly

Two concurrent checkouts both pass the `payment_qr_string != ""` guard and both call
`CreateTransaction`. If **both** somehow opened a session, two `SESSION_OPENED` rows would
exist with different references, and only one names the session whose QR was stamped on the
order. The read below takes the newest, which is not provably the winner.

This is unreachable with the current gateway: it refuses a second session for a reference it
has already issued, which is what `ErrDuplicateReference` /
`order.ErrGatewaySessionDuplicate` exist to handle — the second caller gets a refusal, its
order is cancelled and its seats released. The `!stamped` branch in checkout is defensive
against a gateway that behaves differently.

**Accepted as a documented limitation**, not designed around: a gateway that permits
duplicate references would need this revisited, and would need far more revisited than this.
Recorded here so the next reader finds it stated rather than discovers it.

**Failure handling**: a failed `RecordSessionOpened` is logged and swallowed — never
propagated. The gateway has already made the order payable; destroying a live payment session
because an audit write failed would harm the guest and the operator both. This is the same
call `writeMarker` already makes for the same reason ([service.go:514](../../backend/internal/payment/service.go#L514)).
It degrades to spec FR-004's "no reference recorded" state, loudly logged. Spec FR-002 is
read as binding the *design* — record at session open, never defer — not as a demand that an
audit failure abort a payable checkout.

---

## Decision 3: How the order domain reads it back

**Decision**: The order domain declares a `PaymentRecords` interface in its own package; the
composition root satisfies it over `payment.Service`.

**Why a read is needed at all**: spec FR-011 puts `ext_ref_id` on all three payload-bearing
checkout outcomes. Only one of them has the gateway's answer in hand:

| Outcome | Source of the reference |
|---------|-------------------------|
| Fresh session opened | `session.ProviderRef` — already in hand, no read |
| `409` payment-already-started ([service.go:400](../../backend/internal/order/service.go#L400)) | Read — this branch returns **before** any gateway call |
| Lost the TX-P race ([service.go:530](../../backend/internal/order/service.go#L530)) | Read — the caller's own `ProviderRef` names the *loser's* session and would be actively wrong |

That third row is the one that makes reading mandatory rather than convenient.

**Shape**:

```go
// Declared in internal/order, implemented in cmd/api. It does not talk to the
// gateway — this is about a row this system already wrote.
type PaymentRecords interface {
    ExternalRefForOrder(ctx context.Context, orderID uuid.UUID) (string, error)
}
```

**Read-only, deliberately.** Decision 2 puts the *write* in the composition root's adapter,
right where the gateway answers, so the order domain never asks for it to happen — it only
ever asks what was recorded. The payment-domain side of the write is
`payment.Service.RecordSessionOpened(ctx, orderNumber string, session PaymentSession) error`,
mirroring `ReleaseDuplicateSession(ctx, orderNumber string, cause error)`: same package, same
call site, same order-number-not-UUID argument, same look-the-order-up-internally shape. It
takes the whole `PaymentSession` because the adapter already holds one and the marker envelope
wants three of its fields.

**Rationale**: this is the pattern `ARCHITECTURE.md §3.2` prescribes and the codebase already
uses four times over (`order.EventProvider`, `order.PaymentGateway`,
`payment.OrderProvider`, `notification`'s settlement read). The consumer declares what it
needs; the composition root — the only package allowed to see every domain — adapts.
`TestNoDomainImportsAnotherDomain` enforces it.

**Alternatives considered**:

| Alternative | Rejected because |
|-------------|------------------|
| Extend `PaymentGateway` with the lookup | That interface means "talk to the provider". Reading our own stored rows through it would make the abstraction mean two things, and would put a database read behind a name that promises a network call. |
| Duplicate the reference onto `orders` so `qrResponseFor` gets it free | Two sources of truth for one fact, and they can disagree. The whole point of a support identifier is that it is not ambiguous. |
| Return `ext_ref_id` only on the fresh-session response | This is exactly the Q1 the user answered **A** to. Settled, not reopened. |
| Have the HTTP handler assemble it | Moves domain logic into transport and leaves the service returning an incomplete DTO. |

**Lookup query**: newest-first, non-null only —
`SELECT ext_ref_id FROM payments WHERE order_id = $1 AND ext_ref_id IS NOT NULL ORDER BY created_at DESC LIMIT 1`.
No index is added: `payments` is already read by `order_id` for the notification list, the
row counts per order are single digits, and speculative indexes are scope creep the
constitution's Principle VI discourages. A read that finds nothing returns the empty string,
never an error — spec FR-010 requires the field present-and-empty, so "no reference" must not
be an error condition.

---

## Decision 4: How the admin console gets an order-level value

**Decision**: Add `ext_ref_id` to the existing `NotificationResponse` row shape; the frontend
derives the order-level value from the list it already fetches.

**Rationale**:

- Spec FR-015 requires the value be shown **once for the order**, not per row. Deriving it in
  the panel (`notifications.find(n => n.ext_ref_id)?.ext_ref_id`) satisfies that without a
  third network call in a dialog that already makes two.
- Only the `SESSION_OPENED` row will ever carry a non-empty value, so the derivation is exact,
  not heuristic.
- No new route, no new query key, no new handler — spec FR-016 forbids a new page and this
  keeps the surface count flat.

**Alternatives considered**:

| Alternative | Rejected because |
|-------------|------------------|
| New endpoint `GET /admin/payment/order/:id/reference` | A third round trip for one string, and a new route to document, test, and version. |
| Change the notifications response from an array to an object with a header | Breaking wire change to a shape the frontend, the OpenAPI document, and its tests all consume. Disproportionate. |
| Add it to the admin **orders list** read | The admin domain owns that read and would have to reach into `payments` across a domain boundary — a whole new seam for a value the user asked to see in the payment view. Also directly contrary to spec FR-016. |
| Render it as a column in the notifications table | Repeats one order-level value down every row and leaves it blank on all but one. Rejected in the spec itself (FR-015). |

---

## Decision 5: Regression surface — what adding a row could break

Checked rather than assumed, because a new row in an append-only log is read by code that
did not expect it.

| Reader | Effect | Verdict |
|--------|--------|---------|
| `SettlementForOrder` ([service.go:801](../../backend/internal/payment/service.go#L801)) | Walks rows newest-first, takes the first non-empty `PaymentType` as the instrument and the first `Completed` row's time as `PaidAt`. The `SESSION_OPENED` row is the **oldest**, so a paid order's instrument still resolves from its notification rows — unchanged. An unpaid order would now resolve an instrument with a zero `PaidAt`, but the receipt only renders for paid orders. | No behavior change; assert it in the payment tests. |
| Admin notifications list | Gains one row per order. Marker badge ("our note") already exists and applies. | Intended; update `order-payment-panel.test.tsx` fixtures and any Go test asserting a row count. |
| `ListPaymentsByOrderID` consumers | Only the two above. | Bounded. |
| Cache | `payments` is not cached; the notification query runs `staleTime: 0`. | No invalidation needed, no `E2E_CACHE_ENABLED` difference. |

The instrument written on the `SESSION_OPENED` row is `"qris"`, matching what
`ReleaseDuplicateSession` already writes on its sibling row. This is the payment domain
naming its own gateway's instrument, which is where that knowledge belongs under Principle V —
not the order domain's business, and not the gateway interface's.

---

## Resolved: no open questions

Every `NEEDS CLARIFICATION` from the specify phase was answered by the user before planning
(Q1 → all three checkout outcomes; Q2 → order-level line in the existing payment dialog).
Nothing in Phase 0 surfaced a new one. Phase 1 proceeds.

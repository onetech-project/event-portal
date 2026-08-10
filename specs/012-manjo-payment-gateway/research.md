# Phase 0 Research: Manjo Payment Gateway Migration

**Feature**: [spec.md](./spec.md) | **Date**: 2026-08-10

Each item resolves an unknown from the plan's Technical Context. Items marked **OPEN** could not be
resolved from available evidence and carry a stated fallback so they do not block implementation.

---

## R1. Pin the contract module version that publishes `qr_ea`

**Decision**: Advance the dependency to `v0.0.0-20260810052544-9d11826f4864` and decode the QR method
response through the module's own `method.QRResponse`. No locally-defined decode struct.

```fish
cd backend
go get gitlab.pg-poppay.com/company/shared/cdtc/go@v0.0.0-20260810052544-9d11826f4864
go mod tidy && go mod vendor   # vendor/ is gitignored; regenerate per PRIVATE_MODULE_SETUP.md
```

**Rationale**: The field was published upstream during planning, then re-typed:

```go
type QRResponse struct {
    RawQR     string    `json:"qr_r"  msgpack:"qr_r"`
    URLQR     string    `json:"qr_u"  msgpack:"qr_u"`
    ExpiredAt time.Time `json:"qr_ea" msgpack:"qr_ea"`
}
```

Two upstream commits, each verified by full recursive diff against its predecessor, each touching
only this file: `…407938eeb78a` added `ExpiredAt string`, `…9d11826f4864` changed it to `time.Time`.
Nothing else in the module moved across either — no status value, no callback field, no request shape.

**What the timestamp type buys**: the parse happens at the contract boundary. An offset-bearing value
(`2026-08-10T10:26:46+07:00`) unmarshals into a `time.Time` carrying that offset, and comparisons are
instant-based regardless of location — so FR-009a's "preserve the instant, not the wall-clock reading"
is satisfied by the type rather than by our discipline. That removes a whole class of timezone bug.

**What it costs, and why FR-009b/e matter more now, not less**:

- **Absent no longer looks absent.** A missing `qr_ea` decodes to the zero `time.Time`, which is a
  *valid* timestamp in year 1 — not an error, not a null. It is only distinguishable from a real
  value by an explicit zero check. Treated as an ordinary expiry it reads as "already expired", which
  happens to route to the right fallback for the wrong reason; the signal FR-009b demands would be
  lost, and the operator would never learn the gateway stopped sending the field.
- **Malformed can now take the payload with it.** A non-RFC3339 value makes the timestamp's own
  unmarshal fail. Whether the surrounding struct survives that depends on the decoder's error
  handling, which is exactly the kind of thing not to rely on implicitly. FR-009e therefore states the
  required outcome directly: an unreadable expiry is a deadline problem, and must never be the reason
  a guest is denied a code that decoded perfectly well.

The older hazard is unchanged and still the worst one: decoding against a version *without* the field
drops it with no error whatsoever, yielding a healthy-looking QR beside a deadline the gateway never
agreed to.

**Alternatives considered**:

- *A local decode struct* — was the plan while the field was unpublished; now redundant duplication
  of a shape the module states correctly.
- *Keep the string form and parse ourselves* — rejected: hand-parsing is the timezone bug the type
  eliminates, and a second parse is a second place to get it wrong.
- *Stay on the old version and compute deadlines locally* — rejected: abandons FR-009 and reintroduces
  the two-sides-disagree problem the gateway's expiry exists to solve.

**Residual follow-up**: still no subdirectory-prefixed tag, so this remains a pseudo-version. Worth
asking the gateway team for `go/v1.x.y` (per `PRIVATE_MODULE_SETUP.md`) so services can pin something
readable — a shared contract repo where every consumer cites a commit hash makes "what are we
compiled against" needlessly hard to answer.

---

## R2. Callback authentication is a bearer token

**Decision**: Read `Authorization: Bearer <token>` and compare against the configured callback key
using a constant-time comparison. Reject with a non-200 when absent or mismatched; that is the only
case where a non-200 is correct (FR-012c).

**Rationale**: Determined from the gateway's own dispatch code, not from the contract:
`resty.New().SetBaseURL(cfg.CallbackHost).SetAuthToken(cfg.CallbackToken)`. Resty's `SetAuthToken`
emits precisely `Authorization: Bearer <token>`. The `callback.Request` struct carries no
authenticating field, confirming the token is the whole mechanism.

Constant-time comparison because a byte-by-byte early return leaks the token a character at a time to
anyone able to time the endpoint (FR-012a). `crypto/subtle.ConstantTimeCompare` is already used for
this purpose in the retired Midtrans signature check, so the pattern exists in-tree.

**Alternatives considered**: HMAC over the body — stronger, but the gateway does not send one.
Source-address allow-list — rejected during clarification; couples correctness to topology.

---

## R3. Status mapping is an integer enum, not a string

**Decision**: Map `status.Status` (a `uint8` enum) directly. Retain the existing `Outcome` shape and
the total-mapping discipline of `status.go`, replacing the string switch:

| `status.Status` | Value | Order outcome | Quota | Signal |
| --- | --- | --- | --- | --- |
| `Pending` | 0 | unchanged | held | no |
| `Reject` | 1 | `CANCELLED` | restored | no |
| `Cancel` | 2 | `CANCELLED` | restored | no |
| `Expired` | 3 | `EXPIRED` | restored | no |
| `Obscure` | 4 | unchanged | held | **yes** |
| `Completed` | 5 | `PAID` | consumed | no |
| any other value | — | unchanged | held | **yes** |

**Rationale**: This preserves the constitution's requirement that `expire`/`cancel`/`deny`/`failure`
atomically set `EXPIRED`/`CANCELLED` and restore quota — `Reject` and `Cancel` are Manjo's names for
deny/failure, `Expired` for expire. `Obscure` is documented upstream as "undefined status from
payment network or bank", which is precisely a case that must not be guessed at.

The **zero-value hazard** deserves stating: `Pending` is `0`, so a callback with a missing or
unparseable `s` field decodes as `Pending` — a silent "do nothing" rather than a visible error. The
mapping is still correct (an unknown status should change nothing), but the parse must distinguish
"absent" from "explicitly pending" for FR-018's audit record to be honest about what arrived.

**Alternatives considered**: Mapping via `Status.String()` and reusing the string switch — rejected:
converts a total, compiler-checkable mapping into a stringly-typed one for no benefit.

---

## R4. Reconciliation state needs no migration

**Decision**: Record reconciliation state as marker rows in the existing `payments` table, using
`payments.status` for the marker and `payments.raw_response` (JSONB) for its detail. No new table,
column, index, or migration.

**Rationale**: `payments.status` is a free-form `VARCHAR(50)` holding *the provider's raw status* —
and the codebase already writes a non-provider marker into it: `QR_REISSUED`, counted by
`CountReissuedQRs`. That precedent is being retired by this feature, which frees the technique for a
better use.

This also resolves the **Principle II risk**. A worklist defined as "paid orders with a contradicting
notification" would need a `payments`↔`orders` JOIN across domain boundaries. But contradictions are
*detected* inside `applyProviderResult`, where the order's status is already in hand — so the marker
can be written at detection time and the worklist becomes a pure `payments` query owned entirely by
the payment domain. Order details are then fetched through the existing order-provider interface, the
same way every other cross-domain read in this codebase works.

Markers:

| Marker | Written when | Feeds |
| --- | --- | --- |
| `DISPUTED` | a terminal-failure notification arrives for an order already `PAID` | FR-016b, FR-016c |
| `ORPHAN_PAYMENT` | a `Completed` notification arrives for a cancelled/expired order | FR-019 |
| `MANUAL_RECONCILED` | staff confirm a payment by hand | FR-022d, FR-022e |
| `SESSION_DUPLICATE` | session-open returns duplicate-reference | FR-007d |

**Alternatives considered**:

- *A `reconciliations` table* — rejected: a migration and a second audit trail for something the
  payments log already models. Every marker is, genuinely, "a thing the provider or an operator said
  about this order at a time", which is what that table is.
- *A boolean column on `orders`* — rejected: cross-domain write from the payment domain, and it
  cannot carry who/when/why.

**Accepted cost**: no index on `payments(order_id)` exists, so worklist and marker queries scan.
At MVP volume this is the same trade `CountReissuedQRs` already shipped with. If it bites, a partial
index is a one-line migration later — deliberately not taken now to keep this feature migration-free.

---

## R5. Configuration collapses from six variables to five

**Decision**:

| Variable | Status | Notes |
| --- | --- | --- |
| `PG_BASE_URL` | renamed, **required** | no default, no compiled-in hostname (FR-026) |
| `PG_SERVER_KEY` | renamed | → `ac.cr.client_secret` |
| `PG_CLIENT_KEY` | renamed | → `ac.cr.client_id` |
| `PG_CALLBACK_TOKEN` | **new** | inbound bearer token; no predecessor |
| `PAYMENT_WINDOW` | retained, re-purposed | FR-009b fallback + FR-009d expectation only |
| `MIDTRANS_IS_PRODUCTION` | **removed** | FR-026a |
| `PAYMENT_EXPIRY` | **removed** | collapsed into `PAYMENT_WINDOW` (FR-030) |
| `QR_REFRESH_AFTER` | **removed** | refresh withdrawn (FR-010) |

**Rationale**: Today `.env.example` documents `QR_REFRESH_AFTER=7m < PAYMENT_WINDOW=14m <
PAYMENT_EXPIRY=15m`, and startup enforces that ordering. After this feature none of the three means
what it did: the first is no longer sent, the second is no longer ours to decide, the third has
nothing to drive. **The interdependency validation must be deleted, not loosened** — a check
comparing values that no longer carry their old meaning is worse than no check, because it still
looks like it is protecting something.

`PAYMENT_SWEEP_INTERVAL` and `BOOKING_HOLD` are unaffected: the sweep and the booking hold are
independent of how the payment deadline is decided.

**OPEN — credential half mapping**. `PG_SERVER_KEY`→`client_secret` and `PG_CLIENT_KEY`→`client_id`
follows the Midtrans sense (server key private, client key public) but is unconfirmed. Because the
pair is vestigial — the gateway checks presence, not privilege (FR-028a) — a swap would not fail
loudly at first. *Fallback*: implement as stated and verify against a real session-open before
go-live; a swap is a one-line fix with no data consequences.

---

## R6. The liveness watchdog threshold is derivable

**Decision**: Treat the stream as failed when no frame and no keep-alive has arrived for **50
seconds** — twice the server's 25-second keep-alive interval (`defaultKeepAliveInterval`). On failure
start the fallback reads; on the next frame or keep-alive, stop them.

**Rationale**: The server emits a beat every 25 s unconditionally, so silence longer than two
intervals cannot be normal. One interval would false-positive on ordinary jitter; two gives a full
missed beat of margin while still detecting a silently buffered stream inside a minute — well within a
payment window.

This is the only mechanism that catches the failure the previous design kept polling for: an
intermediary holding the connection open and forwarding nothing produces `onopen` with no error, so
connection state alone reports health that isn't there.

**Supporting finding**: the per-IP concurrent-stream cap now reads `defaultStreamCap = 10` (it was
`5` when first surveyed). Since a refusal is an HTTP 429 rather than a dropped connection, the browser
will *not* retry it — `EventSource` only reconnects after a network-level drop, so a non-200 fails the
connection permanently. The fallback must therefore treat "refused" as a failure state rather than
waiting for a reconnect that will never come (FR-021i).

**Correction made during implementation — the beat had to become a named event.** This decision was
originally written around the existing comment frame (`: keep-alive`), on the reasoning that it already
carried the signal. It does not: the browser's `EventSource` **discards comments without dispatching
anything**, so a client watching for them sees nothing on a healthy stream. A watchdog built on that
would have fired on every single payment, since the server only writes a data frame when the status
actually changes — a pending order is legitimately silent for its whole window.

The server therefore now emits `event: keep-alive\ndata: {}` instead. The frame still nurses
intermediaries exactly as the comment did, and it is observable via `addEventListener`.

**Alternatives considered**: a data-carrying heartbeat with sequence numbers — rejected, and the
original objection to it still stands: an *unnamed* data frame reaches `onmessage` and would need
filtering out of the status path. A **named** event does not reach `onmessage` at all, which is
precisely why it is the right shape here and the unnamed one is not.

---

## R7. Outbound retry, and what a duplicate-reference error means

**Decision**: Retry session-open on timeout and network error, at most 2 further attempts with short
backoff, reusing the order number as the reference unchanged. Treat a duplicate-reference error as a
distinct terminal outcome: stop retrying, release the order's quota promptly, write a
`SESSION_DUPLICATE` marker, and tell the guest to start again.

**Rationale**: The gateway rejects a reference it has already issued a code for. Two consequences,
pulling in opposite directions:

- *Safe*: a retry can never create a second live code. FR-010's "one session per order" is enforced
  by the gateway, not merely by our discipline.
- *Unrecoverable*: the error carries no code, and no call returns an existing one (FR-022). A retry
  that meets it proves a code exists that this system will never hold. The order cannot be paid,
  because the guest is never shown anything to scan.

Releasing quota immediately rather than at the deadline follows directly: the guest sees no code, so
holding their seats for the full window cannot help anyone. FR-007f routes a payment that somehow
arrives anyway to the existing orphan path.

Retry pacing must be short — a person is watching a spinner. The gateway's own client waits 10 s
between attempts, which is correct for a background callback and unacceptable here.

**Alternatives considered**:

- *No retry* — rejected: turns a one-second blip into a failed checkout.
- *Retry with a fresh reference* — rejected: creates a second session for one order, breaking FR-010,
  and reintroduces the reference-suffix machinery this feature removes.
- *Retry only errors that never reached the gateway* — subsumed: those are retried unconditionally,
  and the duplicate-reference response makes timeout retries safe too.

---

## R8. Withdrawing three capabilities

**Decision**: Delete rather than deprecate.

| Withdrawn | Surface removed |
| --- | --- |
| Provider status query | `Gateway.FetchStatus`, `Service.RefreshStatus`, `POST /ticket/order/:id/payment/refresh`, `ErrProviderHasNoRecord`, `CodePaymentStatusUnavailable`, the rate-limited route group if it holds nothing else |
| QR refresh | `Service.ReissueQR`, `POST /ticket/checkout/:id/refresh-qr`, `CountReissuedQRs`, `QR_REISSUED`, `stripReissueSuffix` + `reissueSuffix`, `QRRefreshAfter` plumbing, `qr_refresh_after_seconds` in both DTOs, the frontend refresh timer |
| Midtrans adapter | `midtrans.go`, `midtrans_test.go`, `refresh_test.go`, `reissue_test.go`, and the `CoreAPI*BaseURL` constants |

**Rationale**: A deprecated-but-wired status query is worse than none — it would answer confidently
using a call that cannot report status. `stripReissueSuffix` in particular must go rather than remain
harmless: left in place it would silently truncate any future order number ending in `-R<digits>`.

**Note on `qr_refresh_after_seconds`**: it appears in two response DTOs and is consumed by the
frontend. Removing a field from a response the frontend reads is the one place this feature can break
a running page, so backend and frontend removal belong in the same change.

---

## R9. QRIS frame identity is configured, not parsed

**Decision**: Render the frame's merchant name, registration number, and terminal label as fixed
configured values. Do not parse the payload.

**Rationale**: Chosen during clarification with the trade understood. Everything the frame shows *is*
present in the payload — decoding both supplied samples confirms merchant name at tag `59`
(`Ayoborong`, `Pupuk Kalteng`), city at `60`, the 18-digit merchant id at `26`→`01`, terminal label at
`62`→`07`, and the gateway's own reference at `62`→`05`. So parsing is available later at low cost
(recorded as deferred work), but is not built now.

The consequence FR-021b exists for: the payment instructions tell the guest to verify the merchant
name before entering a PIN, so a fixed label that disagrees with the code defeats the check it
invites. Verification against a genuinely-issued code before go-live is a release step, not a test.

**Also confirmed from the same decode**: tag `01` is `12`, meaning these are *dynamic* QRs, and tag
`54` carries the amount. The payer cannot alter what they pay, which is what makes the callback's
missing amount field tolerable (FR-002b's edge case).

---

## R10. Fractional rupiah — **RESOLVED (observed 2026-08-10)**

**Question**: A percentage fee against an odd subtotal yields centavos (11% of 105,001 = 11,550.11).
Indonesian payment practice does not use them. Does the gateway reject such an amount, truncate it, or
round it differently than the page displayed?

**Answer: none of the three — it preserves the value verbatim.** One session-open was sent against a
live gateway with `a: 11550.11`, and the returned payload carries

```
tag 54 (transaction amount) = "11550.11"
```

so the amount the payer's app shows is exactly the amount the order summary shows. No rounding rule of
our own is needed, and FR-002b's "send the frozen total unchanged" turns out to be sufficient rather
than merely safe. The matching edge case has been removed from the spec.

**Also settled by the same call**, since all of it needed the same live observation:

| Question | Observation |
| --- | --- |
| Is the QR dynamic (amount fixed by the code)? | Yes — tag `01` is `12` and tag `54` carries the amount. Confirms R9 against this gateway rather than only against the supplied samples, which is what makes the callback's missing amount field tolerable. |
| What is the gateway's own default validity? | **~8 minutes** (`qr_ea` came back 8 minutes out), not the 15 the retired `PAYMENT_EXPIRY` assumed. Adopted verbatim per FR-009c. `PAYMENT_WINDOW=15m` remains a sane fallback and sits inside FR-009d's tolerance, so it raises no signal. |
| Frame identity | Merchant `Pupuk Kalteng` (tag 59), NMID `936008580287697876` (26→01), terminal `659` (62→07), city `JAKARTA BARAT` (60). The values first configured from the older sample (`Ayoborong` / `…176412711` / `A01`) were **wrong for this environment** and were corrected — exactly the mismatch FR-021b exists to prevent, caught by the release step rather than by a guest. |

---

## R11. `MERCHANT_NOT_AVAILABLE` is **not** the duplicate-reference error — WITHDRAWN and reversed

**Decision (current)**: do **not** treat `MERCHANT_NOT_AVAILABLE` as a duplicate reference. It falls
through to the generic failure path, where the order stays `PENDING` with its quota held and the
guest can retry (FR-007). We do not currently know what this gateway calls a duplicate reference;
the plainer wordings (`duplicate`, `already exist`, `already registered`, `ref id exist`) remain as
guesses, and none is confirmed.

**What was recorded here before, and why it was wrong.** This entry claimed the string was the
duplicate-reference refusal, "observed, not guessed": replaying a reference returned
`{"s":false,"e":"MERCHANT_NOT_AVAILABLE"}` while a fresh reference immediately before and after both
succeeded, so "the merchant is available and it is the *reference* that is taken; the name is simply
misleading."

The name is not misleading. It means what it says. Merchant availability on the gateway's side is
**scored and volume-capped**, so it changes between calls — its own boot log enumerates active and
inactive merchants and their scores. That is what made a three-call experiment look conclusive when
it was not: the fresh references succeeded because a merchant happened to be available at that
moment, not because the replayed reference was special.

**How it surfaced.** Every merchant went inactive, and a brand-new order —
`ORD-20260810-JGJFPA`, a reference the gateway had never seen — was refused with
`MERCHANT_NOT_AVAILABLE`. The system read that as "a code already exists for this reference",
released the seats, cancelled the order, and told the guest to book again. Booking again failed
identically. A transient merchant outage had been converted into a permanent, shop-wide outage in
which no guest could complete a purchase and each was told their order was dead.

**The accepted-ambiguity clause was the actual defect.** It said that if the string were also
returned for a genuine outage, treating it as terminal "is still right … the only cost is releasing
the seats early, which is the safe direction." Both halves were false. Retrying is not the question —
what matters is what the *guest* is told, and a terminal refusal tells them to start over on a path
that cannot succeed. And the cost is not one order's seats: it is every checkout, for as long as the
outage lasts.

**The asymmetry to reason from.** The two failure directions are not comparable:

| Mistake | Cost |
| --- | --- |
| A genuine duplicate read as a generic failure | One order holds its seats until its deadline (~8 min) instead of releasing at once. Bounded, self-healing, invisible to everyone else. |
| An availability error read as a duplicate | Every checkout is killed on arrival for the duration of the outage, and the remedy offered to the guest cannot work. |

So an unrecognised refusal must fall through to the generic path. Detecting a duplicate is an
optimisation on FR-007e's prompt release; being wrong about it is an outage.

**Still open**: what a real duplicate reference actually returns. Worth asking the gateway team
directly rather than inferring it from a third experiment — the first two inferences drawn from this
endpoint's error text were both wrong.

---

## R12. The credential mapping cannot be confirmed by observation

**Finding**: `PG_SERVER_KEY`→`client_secret` and `PG_CLIENT_KEY`→`client_id` remains an **assumption**,
and the live gateway cannot settle it. Three calls establish why:

| Sent | Result |
| --- | --- |
| Plausible values in the assumed positions | accepted |
| The two halves **swapped** | accepted |
| `client_id` empty | refused: `Network Account Credential field client_id is required` |

So the gateway checks **presence, not privilege** — confirming FR-028a directly — and a swap is
undetectable by construction. R5's fallback ("verify against a real session-open before go-live") is
therefore not achievable as stated; there is nothing to verify against. The mapping stays as
implemented, and the consequence is recorded rather than papered over: if it is backwards, nothing in
this system or in the gateway will say so.

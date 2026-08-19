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

> **Superseded by [spec 020](../020-qris-frame-simplify/spec.md).** The frame no longer displays a
> merchant name, registration number or terminal label, so the question this section answers — parse
> them or configure them — no longer arises, and the deferred parsing work is withdrawn rather than
> pending. Kept because the payload decoding below is still true of the payload, and because the
> mismatch this decision's release step actually caught (see the table under R15) is worth not
> forgetting.

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
| Frame identity | Merchant `Pupuk Kalteng` (tag 59), NMID `936008580287697876` (26→01), terminal `659` (62→07), city `JAKARTA BARAT` (60). The values first configured from the older sample (`Ayoborong` / `…176412711` / `A01`) were **wrong for this environment** and were corrected — exactly the mismatch FR-021b exists to prevent, caught by the release step rather than by a guest. *Spec 020 retired FR-021b by removing the display, which removes this failure mode rather than re-guarding it; the tags themselves are unchanged in the payload.* |

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

---

## R13. The cooldown cannot stay an Echo rate-limiter middleware

**Decision**: Replace the middleware on the guest resend route with a small keyed cooldown in
`pkg/httpx`, consulted from the handler. Its one method answers both questions at once —
`Take(key) (allowed bool, retryAfter time.Duration)` — and the handler puts the seconds into whichever
response it returns.

**Rationale**: two requirements make the middleware shape unworkable, and neither is satisfiable by
configuring it differently.

- FR-021j needs the remaining seconds on the **accepted** answer as well as the refused one. Echo's
  `middleware.RateLimiterStore` exposes only `Allow(identifier) (bool, error)`. A middleware can
  refuse a request but cannot hand the handler a number, and the accepted path never passes through
  the limiter's own response at all.
- FR-021k needs the body checked **before** any allowance is spent. A middleware runs first by
  definition, so an unreadable body has already been keyed and counted before the handler sees it.

Reading the remaining time is straightforward and non-mutating: `rate.Limiter.TokensAt(now)` returns
the fractional token count, so `wait = (1 - tokens) / limit` whenever it is below one. Go 1.26's
vendored `golang.org/x/time/rate` has it (`rate.go:86`). Called again immediately after a successful
take, the same expression yields the full window the acceptance has just started — which is exactly
what FR-021j asks the accepted response to carry, with no separate constant to keep in step.

**Alternatives considered**:

- *Keep the middleware and stash the remaining time in the Echo context.* Rejected: it addresses only
  the first requirement. The bucket is still spent before the body is validated, which is FR-021k's
  whole point.
- *`Reserve()` the next token and `Cancel()` it to read the delay.* Rejected: mutate-then-undo where a
  pure read exists, and `Cancel` restores the token only when no later reservation has intervened —
  a correctness footnote this needs no part of.
- *Read `Retry-After` from a header instead.* Rejected upstream, in the spec: a cross-origin page
  cannot read a header that is not named in `Access-Control-Expose-Headers`, and the envelope already
  carries per-error detail in `data` (`PAYMENT_ALREADY_STARTED` puts the QR payload there). The field
  costs nothing new.

**Eviction**: idle keys are dropped after the existing `rateLimitWindow` of 3 minutes. That must stay
strictly greater than the cooldown itself — evicting a key mid-cooldown would silently forgive it. At
3 minutes against a 60-second window there is ample margin, and the invariant belongs in a test rather
than in a comment.

**On the client side, do not reuse `ExpiryCountdown`.** It takes an absolute ISO deadline plus the
server's clock at the moment it sent one, and corrects for device skew — the right design for the
payment deadline, where the instant is what both sides must agree on. The cooldown returns a
*duration*, which is skew-proof by construction: there is no shared instant to disagree about. Reusing
the component would mean synthesising an `expiresAt`/`serverTime` pair to feed machinery that then
cancels itself out. A plain local ticker over the returned seconds is both smaller and more accurate
here, and the difference is a real one rather than a stylistic preference.

---

## R14. `RateLimitPerBodyField` is deleted, not repaired

**Decision**: Remove `RateLimitPerBodyField` and its `bodyField` helper from `pkg/httpx` entirely.
`RateLimitPerIP` and `RateLimitBy` stay; they have other callers and no equivalent flaw.

**Rationale**: the guest resend was its only user, and the behaviour that made it wrong is not a bug
inside it but its stated design — "a request whose body is missing, unparseable, or lacks the field
falls into a shared bucket rather than bypassing the limit". One shared bucket across every caller and
every order means a single malformed request throttles the whole system for the window. That is what
FR-021k forbids, and with the cooldown moved into the handler (R13) there is nothing left for the
middleware to do. `bodyField` goes with it: buffering and restoring the request body existed solely so
the middleware could peek at it before the handler bound it.

The shared bucket is also why the observed defect looked like a rate-limit problem rather than an
encoding one. A per-order key would have made the first malformed press harmless to everyone else.

**Alternatives considered**: keep the middleware for a future caller — rejected. Nothing else needs
per-body keying, and leaving a helper whose documented fallback is a system-wide bucket invites the
same failure the next time someone reaches for it.

---

## R15. The double-encoded request body

**Finding**: `useGuestResendTicketEmail` passed `body: JSON.stringify({ order_id: orderNumber })` while
`apiFetch` stringifies whatever it is given (`api-client.ts:82`). The request therefore carried a JSON
*string* — `"{\"order_id\":\"ORD-…\"}"` — not an object.

Every symptom follows from that one line:

| Observed | Cause |
| --- | --- |
| First press sent nothing, logged nothing | `c.Bind` fails on a string body; the handler's silent branch answers 202 |
| Second press refused | the unreadable first request was counted against the shared bucket (R14) |
| Refusal never cleared | the screen disables the button on a 429 with nothing to re-enable it |

**Decision**: pass the object. `apiFetch` owns serialisation, and this is the only site in the frontend
that pre-stringified — verified across `lib/`, `components/`, and `app/`, one match, now zero.

**Worth stating plainly**: this fix alone makes resend work again. Everything R13 and R14 describe is
about the next such bug being visible in a log line instead of costing an afternoon, and about one
caller's mistake staying one caller's problem. The requirements are not a workaround for this defect;
they are what would have made it a five-minute diagnosis.

**Guard**: `apiFetch`'s contract (`body?: unknown`, stringified internally) makes the mistake easy to
repeat and impossible to see at the call site. A test asserting the wire body of the resend request
parses to an object with `order_id` pins the behaviour where it actually broke.

---

## R16. The callback endpoint answers in the envelope, and the refusal needs a registered 200-band code

**Trigger** (clarification 2026-08-11): the callback endpoint answers a successful notification with
no body at all, while the one path that *does* answer with a body — the FR-019c settle refusal —
writes a bare object outside the envelope. Three paths, three shapes.

**Finding, and it narrows the work considerably**: the error paths are *already* correct.
`apperr.Error.Response()` renders `Body{Code: Numeric(status, code), Message, Data}` — the same
`{code, message, data}` envelope successes use — and `httpx.ErrorHandler` writes it for every returned
error. So the authentication refusal, the unreadable-body refusal, and the internal fault already
satisfy FR-012e without a line changing. Only two writes deviate, both in `payment/handler.go`:

| Line | Today | Required |
| --- | --- | --- |
| `c.NoContent(http.StatusOK)` | 200, no body | 200, `{code: 200000, message: "Success", data: null}` (FR-012f) |
| `c.JSON(200, toSettleRefusedResponse(refused))` | bare object, no envelope | 200, `{code: 200001, message: …, data: {shortfall}}` (FR-019e) |

**Decision — render the refusal through `apperr`, not through a second envelope writer.** Register
`CodeTicketsUnavailable = "TICKETS_UNAVAILABLE"` and add one arm to `apperr.Numeric`:

```
case CodeTicketsUnavailable:
    return 200001
```

then have the handler return `apperr.New(http.StatusOK, apperr.CodeTicketsUnavailable, msg).WithData(shortfall)`.

**Rationale**: `Numeric` is the single place a numeric envelope code is derived, and FR-019e's whole
point is that the code is what discriminates. Routing the refusal around it would create a second
derivation site and the invariant would hold only by everyone remembering. The shared handler also
emits the `request rejected` warn line for free, which is wanted here — a refused settle is exactly
what an operator should be able to find in the logs.

**The trap this closes, which is the reason the code must be *registered* rather than left to the
default.** `Numeric`'s fallback is `status * 1000`. An `apperr` carrying HTTP 200 and any unregistered
code therefore renders `200000` — byte-identical to `httpx.SuccessCode`. A refusal would announce
itself as a success and no test asserting on the status line would catch it, because the status line
is 200 in both cases by design (FR-019c). `200001` is free: no 200-band sub-code exists today, and the
scheme's `status × 1000 + sub-code` shape is preserved, so no envelope code contradicts the status it
sits behind.

**Alternatives considered**:

| Alternative | Rejected because |
| --- | --- |
| A new `httpx.RespondWithCode(c, status, code, …)` writing the envelope directly | A second place numeric codes are chosen, competing with `Numeric`. The registry is the thing that makes FR-019e checkable. |
| Keep `200000` and put the refusal only in `data` | Makes the refusal indistinguishable from an acknowledgement to anything reading the code, on a status line that is 200 either way. Defeats FR-019e outright. |
| Reuse `400002 INSUFFICIENT_QUOTA` inside the 200 | An envelope code whose leading digits contradict the status line — the per-endpoint quirk the envelope exists to prevent. |
| Report the applied outcome in `data` on every acknowledgement | Rejected at clarification (FR-012f). It duplicates what FR-018 records and FR-022c serves, in a transient body that can drift from the durable one. |

**Consequence for the `SettleRefusedResponse` DTO**: it loses `order_number`, `error`, and `message`
— the envelope's `code` and `message` now carry the last two, and the order number is already the
notification's own `ri`. What survives as `data` is the shortfall list, which is the part an operator
cannot derive from anywhere else (FR-022e).

**Watch item, not a blocker**: returning a non-nil error with a 200 status is unusual, and anything
that counts handler errors as failures — span status, an error-rate metric — will count this one. No
such middleware exists in this build today. If one is added, this path needs an explicit exemption
rather than a silent reclassification.

---

## R17. The signalled anomalies name themselves; the uneventful ones do not

**Trigger** (observed 2026-08-11, on a live server): a notification for `ORD-20260811-S8HsD72`
matched no order. The server logged it twice at ERROR — `unattributable payment notification`,
`unknown order reference` — and answered `{"code":200000,"message":"Success","data":null}`. A
notification that matched nothing reported success.

**What R16 got wrong.** FR-012f made *every* 200 identical, reasoning that the durable record
(FR-018) is the account of what happened and a transient body could drift from it. That reasoning
survives for the uneventful outcomes and fails for the anomalies: it made "we applied your payment"
and "we have no idea what order this is" the same bytes, so the only way to discover the second was
to go reading logs — which is exactly the position the resend defect of Increment 2 left an operator
in, for the same reason.

**Decision**: the line is the **operational signal**, not whether the order moved. Five outcomes raise
a signal and now name themselves in the envelope's code; everything else stays `200000`.

| Outcome | Code | Signal? |
| --- | --- | --- |
| Payment applied, pending, repeat of a recorded outcome | `200000` | no — routine |
| Settle refused, quota short (FR-019c) | `200001` | yes |
| Unknown reference (FR-013) | `200002` | yes |
| Not a deposit (FR-020) | `200003` | yes |
| Indeterminate/unrecognised status (FR-014) | `200004` | yes |
| Contradiction of a paid order (FR-016b) | `200005` | yes |
| Completion for a gateway-cancelled order (FR-019d) | `200006` | yes |

**Why the signal is the right line.** It already exists, and it already encodes the judgement "a
person should know about this" — FR-014's table has the column. Reusing it means one rule rather than
a per-case argument, and it keeps the repeat delivery quiet: at-least-once makes a duplicate the
*normal* case (FR-012c guarantees up to four), so flagging it would bury the five that matter in the
noise those retries generate. That is the same trap FR-016b already names for alerting, applied to
the response body.

**Mechanism**: the service returns sentinel errors (`ErrNotificationUnknownOrder` and four siblings);
the handler maps them to codes. Sentinels rather than a transport-shaped type keep `apperr` out of
the service, and they are what the service tests assert on — twelve existing assertions changed from
`require.NoError` to `require.ErrorIs`, which is the honest reading of what those tests always meant.

**Precedence**, which the live check surfaced and which is worth stating because more than one code
can apply: deposit check → order lookup → status mapping. An unknown reference carrying `s:99`
answers `200002`, not `200004`. The order is not arbitrary — a status cannot be mapped onto an order
that was never found.

**Messages carry no order-specific detail.** The endpoint is authenticated, so this is not the
enumeration concern governing the resend endpoint (FR-021l). It is that the durable record is the
account of what happened, and a response restating it could drift. What the caller gets is the *kind*
of problem — enough to tell a misconfigured gateway from a vanished order without a log search.

**Alternatives considered**:

| Alternative | Rejected because |
| --- | --- |
| Report only the unknown reference | The observed case, but withdrawal and unrecognised-status are the same defect a day later. "Always send the error" reads as a rule, not a patch. |
| Report anything that changed no order | Sweeps in pending and the duplicate retry. Retries are guaranteed and routine; flagging them is how the five that matter get buried. |
| Distinguish by HTTP status | Forbidden by FR-012c — a non-200 costs four deliveries and still loses the notification. |
| Put the detail in `data` | Duplicates the durable record in a transient body, the objection R16 raised and which still holds. The code names the kind; the record holds the specifics. |

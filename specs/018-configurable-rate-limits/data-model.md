# Phase 1 Data Model: Configurable Rate Limits

**Feature**: [spec.md](spec.md) · **Plan**: [plan.md](plan.md) · **Research**: [research.md](research.md)

> **No database involvement.** This feature persists nothing and adds no table, column or
> migration, so [SCHEMA.md](../../SCHEMA.md) is untouched. The "entities" here are
> configuration values held in memory for the life of the process. They are documented in
> this file because the validation rules between them are the substance of the work.

## Entities

### `ThrottlePolicy` — one rate-shaped surface

Four of the six surfaces share this shape.

| Field | Type | Meaning |
|-------|------|---------|
| `Enabled` | `bool` | This surface's own switch. Unset means enabled (FR-009). |
| `Rate` | `float64` | Sustained allowance, requests per second per key. |
| `Burst` | `int` | Short-term allowance above `Rate`. |

Derived:

- `Active(master bool) bool` → `master && Enabled`. The single place FR-009's precedence is
  expressed, so the two switches cannot be combined differently at different call sites.
- `RefillWindow() time.Duration` → `Burst / Rate` seconds. The time to refill an empty
  bucket; the quantity R9's retention invariant is checked against.

### `CooldownPolicy` — the guest ticket-email resend

Rate-shaped underneath, but configured as a window because that is what it means and what
the guest is shown (R1).

| Field | Type | Meaning |
|-------|------|---------|
| `Enabled` | `bool` | This surface's own switch. |
| `Window` | `time.Duration` | How long one order must wait between resends. |
| `Burst` | `int` | Sends allowed before the wait applies. |

Derived: `Rate() float64` → `1 / Window.Seconds()`, which is what `rate.Limit` receives.
With the default `Window=60s, Burst=1` this reproduces today's `guestResendRate = 1.0/60.0`
exactly.

### `StreamPolicy` — the live payment-status connection cap

Not a rate at all; a ceiling on concurrent held-open connections. Given its own type so it
cannot be handed an allowance and a burst it has no meaning for (FR-004).

| Field | Type | Meaning |
|-------|------|---------|
| `Enabled` | `bool` | This surface's own switch. |
| `MaxConns` | `int` | Concurrent SSE connections allowed per key. |

### `ThrottleConfig` — the whole subsystem

| Field | Type | Meaning |
|-------|------|---------|
| `Enabled` | `bool` | **Master switch** (FR-007). Off disables all six regardless of their own switches. |
| `IdleTTL` | `time.Duration` | Shared idle-retention for per-key state (FR-005). |
| `Book` | `ThrottlePolicy` | Booking requests. |
| `Availability` | `ThrottlePolicy` | The availability check in front of the T&C gate. |
| `TicketLookup` | `ThrottlePolicy` | Public ticket lookup. |
| `Checkout` | `ThrottlePolicy` | Checkout — costs an outbound gateway call. |
| `Resend` | `CooldownPolicy` | Guest ticket-email resend, keyed per order. |
| `StatusStream` | `StreamPolicy` | Live payment-status SSE connections. |

Hangs off `config.Config` as a single field, `Throttle ThrottleConfig`, so the whole
subsystem travels as one value into `main.go`.

## Defaults — these reproduce today's behaviour exactly (FR-006)

Every default below is the literal constant in force today. The middle column is where it
lives now and is deleted by this change.

| Setting | Today | Default after this change |
|---------|-------|---------------------------|
| Master switch | *(no such thing)* | `true` |
| Idle retention | [`rateLimitWindow`](../../backend/cmd/api/main.go#L38) `3m` | `3m` |
| Booking rate / burst | [`bookRate` `0.33` / `bookBurst` `5`](../../backend/cmd/api/main.go#L58-L59) | `0.33` / `5` |
| Availability rate / burst | [`availabilityRate` `1.0` / `availabilityBurst` `10`](../../backend/cmd/api/main.go#L68-L69) | `1.0` / `10` |
| Ticket lookup rate / burst | `TICKET_LOOKUP_RATE_LIMIT` `5` / `TICKET_LOOKUP_BURST` `10` *(already env)* | `5` / `10` |
| Checkout rate / burst | [`gatewayCallRate` `0.2` / `gatewayCallBurst` `3`](../../backend/cmd/api/main.go#L43-L44) | `0.2` / `3` |
| Resend window / burst | [`guestResendRate` `1.0/60.0` / `guestResendBurst` `1`](../../backend/cmd/api/main.go#L52-L53) | `60s` / `1` |
| Status stream cap | [`defaultStreamCap` `10`](../../backend/internal/payment/stream.go#L129) | `10` |
| Per-surface switches | *(no such thing)* | `true` ×6 |

**The `0.33` is deliberate and must not be tidied.** It is not `1/3`. Reproducing today's
behaviour means carrying `0.33` forward verbatim, which is the decisive argument for the
float idiom in R1.

## Validation rules

All run inside `config.Load()`, appending to the existing `loader.errs` slice so every
problem is reported in one pass rather than one per restart — the contract `Load()` already
documents and every other setting already follows.

| # | Rule | Applies to | Requirement | Failure it prevents |
|---|------|------------|-------------|---------------------|
| V1 | Value parses as its declared type | all | FR-013 | Typo silently taken as a default the operator did not choose |
| V2 | `Rate > 0` | enabled `ThrottlePolicy` | FR-014 | **Permanent lockout** — a bucket that spends its burst and never refills |
| V3 | `Window > 0` | enabled `CooldownPolicy` | FR-014 | Same, expressed as a window |
| V4 | `Burst >= 1` | enabled rate-shaped surfaces | FR-014 | A bucket that refuses the very first request forever |
| V5 | `MaxConns >= 1` | enabled `StreamPolicy` | FR-014 | An SSE endpoint that accepts nothing |
| V6 | `IdleTTL > RefillWindow()` | **every** enabled rate-shaped surface | FR-015, FR-016 | Silent forgiveness — an exhausted key evicted before it refills returns with a full bucket (R9) |
| V7 | `IdleTTL > 0` | when any surface is enabled | FR-013 | Eviction on every call, so no state survives to limit anything |

**V2 and V4 are only checked when the surface is enabled.** A disabled surface's numbers are
never used, and refusing to start over an unused value would make disabling a throttle harder
than leaving it on — the opposite of what a kill switch is for.

**V6 is the one rule that spans two settings.** It is the generalisation of the invariant
`cooldown.go` documents for the resend and `cooldown_test.go` asserts for today's wired
values. Once both sides are operator-settable, they can be inverted, and the resulting
failure is silent: the limit appears to work and quietly forgives everyone.

## State transitions

None persisted. Two worth naming because acceptance scenarios turn on them:

1. **Disabled → enabled across a restart.** All throttle state is per-process memory, so a
   restart discards every bucket. Traffic sent while a throttle was off cannot be charged
   afterwards because the record of it does not survive. This is what makes FR-011 true by
   construction rather than by an explicit reset step.
2. **Master switch versus per-surface switch.** The only combination rule is FR-009, and it
   is expressed once, in `Active()`. Master off wins over a surface's own `Enabled=true`;
   master on defers to it; an unset per-surface switch reads as `true`.

## Relationship to the API contract

No request or response shape changes. `PublicRetryAfter.retry_after_seconds` keeps its
current meaning; only the number it carries becomes configurable. When the resend cooldown is
disabled it carries `0`, which the frontend floors to a 5-second debounce — deliberately, and
unchanged by this feature (R6).

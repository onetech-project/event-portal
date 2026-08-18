# Contract: Throttle Configuration

**Feature**: [../spec.md](../spec.md) · **Plan**: [../plan.md](../plan.md) · **Model**: [../data-model.md](../data-model.md)

The interface this feature exposes is not an HTTP endpoint — it is the set of environment
variables the deployment supplies. This file is that contract: names, types, defaults,
units, and what each one protects. It is the source for the `backend/.env.example` block
FR-018 requires.

**Read once, at process start.** Changing any value requires a restart, never a rebuild
(spec Clarifications Q1). Absent or empty means the default. A malformed value refuses
startup and names itself (FR-013).

## Master switch and shared settings

| Variable | Type | Default | Meaning |
|----------|------|---------|---------|
| `RATE_LIMIT_ENABLED` | bool | `true` | **The kill switch.** `false` takes all six surfaces out of the request path. Overrides every per-surface switch (FR-009). |
| `RATE_LIMIT_IDLE_TTL` | duration | `3m` | How long untouched per-key state is retained. MUST exceed every enabled surface's full-burst refill time (V6). |

> **What `RATE_LIMIT_ENABLED=false` leaves exposed.** Booking creates rows and holds quota
> for an hour without payment. Checkout opens a real outbound payment-gateway session per
> press. The ticket-email resend sends real mail to a buyer's inbox. All three are
> unauthenticated, and remain so with throttling off. This is a deliberate operator
> decision, not a safe default (FR-018).

## Per-surface settings

Each surface takes an `_ENABLED` switch. Unset reads as `true` (FR-009).

### Booking — `POST /api/v1/.../book`

Creates rows and holds quota for an hour without payment.

| Variable | Type | Default | Meaning |
|----------|------|---------|---------|
| `RATE_LIMIT_BOOK_ENABLED` | bool | `true` | |
| `RATE_LIMIT_BOOK_RATE` | float (req/sec) | `0.33` | Roughly one booking every three seconds per client. |
| `RATE_LIMIT_BOOK_BURST` | int | `5` | Headroom for a genuine group organising itself. |

### Availability check — the gate in front of Terms & Conditions

Reads and creates nothing, so it is far looser than booking on purpose: a refused guest
adjusting their selection re-checks, and charging that to the booking budget would throttle
them out of the recovery path the check exists to offer.

| Variable | Type | Default | Meaning |
|----------|------|---------|---------|
| `RATE_LIMIT_AVAILABILITY_ENABLED` | bool | `true` | |
| `RATE_LIMIT_AVAILABILITY_RATE` | float (req/sec) | `1.0` | |
| `RATE_LIMIT_AVAILABILITY_BURST` | int | `10` | |

### Public ticket lookup — `GET /api/v1/tickets/:code`

Makes ticket-code enumeration impractical (spec 001 FR-020).

| Variable | Type | Default | Meaning |
|----------|------|---------|---------|
| `RATE_LIMIT_TICKET_LOOKUP_ENABLED` | bool | `true` | |
| `RATE_LIMIT_TICKET_LOOKUP_RATE` | float (req/sec) | `5` | Alias: `TICKET_LOOKUP_RATE_LIMIT` (deprecated, still honoured — R2). |
| `RATE_LIMIT_TICKET_LOOKUP_BURST` | int | `10` | Alias: `TICKET_LOOKUP_BURST` (deprecated, still honoured). |

> **Alias precedence**: the `RATE_LIMIT_`-prefixed name wins when both are set. When only
> the legacy name is set it is used, and startup logs a deprecation notice. A deployment
> that already sets the old names keeps working and is told to move.

### Checkout — opens a payment-gateway session

The tightest limit in the system, because each press costs a real outbound call.

| Variable | Type | Default | Meaning |
|----------|------|---------|---------|
| `RATE_LIMIT_CHECKOUT_ENABLED` | bool | `true` | |
| `RATE_LIMIT_CHECKOUT_RATE` | float (req/sec) | `0.2` | One every five seconds. |
| `RATE_LIMIT_CHECKOUT_BURST` | int | `3` | An impatient double-tap, no more. |

### Guest ticket-email resend — keyed per **order**, not per client

Keyed on the order because the inbox being protected is the buyer's: a per-client limit
would let a handful of hosts flood one buyer between them.

| Variable | Type | Default | Meaning |
|----------|------|---------|---------|
| `RATE_LIMIT_RESEND_ENABLED` | bool | `true` | |
| `RATE_LIMIT_RESEND_WINDOW` | duration | `60s` | **Guest-visible.** This is what the confirmation screen counts down from and what a refusal reports as `retry_after_seconds`. Changing it changes what guests are told (FR-019). |
| `RATE_LIMIT_RESEND_BURST` | int | `1` | |

### Live payment status — concurrent SSE connections

A concurrency ceiling, not a rate: the cost here is held-open connections. It takes no
allowance and no burst (FR-004).

| Variable | Type | Default | Meaning |
|----------|------|---------|---------|
| `RATE_LIMIT_STATUS_STREAM_ENABLED` | bool | `true` | |
| `RATE_LIMIT_STATUS_STREAM_MAX_CONNS` | int | `10` | Concurrent streams per client. |

## Client identification

Not a throttle setting, but the thing every per-client throttle depends on, and currently
broken (R7).

| Variable | Type | Default | Meaning |
|----------|------|---------|---------|
| `TRUSTED_PROXY_CIDRS` | comma-separated CIDRs | *(empty)* | Empty — the default — means the client address is taken from the connection and `X-Forwarded-For` is **ignored**. Set it only when the API genuinely sits behind a proxy you control; the header is then trusted from those addresses alone. |

> **Why this exists.** Today `e.IPExtractor` is never set, so Echo trusts a caller-supplied
> `X-Forwarded-For` from anyone. Five of the six surfaces key on that value, so all five can
> be bypassed by rotating one header. An empty default closes it; the variable exists so a
> future proxy deployment is a configuration change rather than a code change.

## Validation contract

Startup refuses, reporting **every** problem at once rather than the first:

| Rule | Message shape |
|------|---------------|
| Unparseable value | `RATE_LIMIT_BOOK_RATE must be a number, got "fast"` |
| Rate not positive while enabled | `RATE_LIMIT_BOOK_RATE must be positive when RATE_LIMIT_BOOK_ENABLED is true, got 0` |
| Burst below 1 while enabled | `RATE_LIMIT_BOOK_BURST must be at least 1, got 0` |
| Cap below 1 while enabled | `RATE_LIMIT_STATUS_STREAM_MAX_CONNS must be at least 1, got 0` |
| Retention too short | `RATE_LIMIT_IDLE_TTL (10s) must exceed the refill window of RATE_LIMIT_BOOK (15.2s)` |

A disabled surface's numbers are **not** validated. Turning a throttle off must not be made
harder by a value nobody will read.

## Startup report

One structured line, at `info`, naming the master switch and each surface's effective
values, plus a deprecation notice per legacy alias in use.

Deliberately **not** exposed on `/healthz` or any other endpoint: that route is public and
unauthenticated, and publishing exact thresholds converts a limit into a documented
allowance for anyone who asks (R8).

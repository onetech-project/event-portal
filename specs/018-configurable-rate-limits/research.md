# Phase 0 Research: Configurable Rate Limits

**Feature**: [spec.md](spec.md) · **Plan**: [plan.md](plan.md) · **Date**: 2026-08-18

No `NEEDS CLARIFICATION` markers survived `/speckit-specify` — the three that existed were
answered in the spec's Clarifications section. This document records the eight design
decisions the plan rests on, including two defects found by reading the code that the spec
did not anticipate.

---

## R1 — The configuration idiom follows each mechanism's shape, not one house style

**Decision**: Rate-shaped surfaces take a **float requests-per-second** plus an **integer
burst**. The resend cooldown takes a **duration window** plus a burst. The stream cap takes a
**single integer ceiling**.

**Rationale**: Three reasons, in order of weight.

1. **Exactness beats legibility here.** `bookRate` is `0.33`, not `1.0/3.0`
   ([main.go:57](../../backend/cmd/api/main.go#L58)). Re-expressing it as a duration would
   make it `3.0303…s`, and any rounding an operator would tolerate reading changes the
   default. FR-006 requires defaults reproduce today's behaviour exactly, and a float
   reproduces it exactly.
2. **One surface is already configured this way.** `TICKET_LOOKUP_RATE_LIMIT=5` is a float
   rps in `.env.example` today. Introducing a second idiom for the same concept, while the
   first stays, is worse than either idiom chosen consistently.
3. **The resend genuinely is not a rate.** It is a cooldown with burst 1, its value is shown
   to guests in seconds, and `1.0/60.0` is an unreadable way to say "one a minute". FR-004
   already establishes the principle that a mechanism must not be forced into a shape it has
   no meaning for; this is the same principle applied one surface over.

**Alternatives considered**: A duration-per-request idiom for everything (`BOOK_EVERY=3s`) —
more legible for operators, rejected because it cannot express `0.33` without changing the
default. Floats for everything including the resend — consistent, rejected because
`0.0166666` is a value no operator can verify by eye and the resend's window is the one
number a guest also sees.

---

## R2 — Names are `RATE_LIMIT_<SURFACE>_<FIELD>`, with the two existing names kept as aliases

**Decision**: New settings use the `RATE_LIMIT_` prefix. The two shipped names,
`TICKET_LOOKUP_RATE_LIMIT` and `TICKET_LOOKUP_BURST`, keep working: the new name wins if
set, the legacy name is used if only it is set, and using a legacy name logs a deprecation
notice at startup.

**Rationale**: Those two names are in `backend/.env.example`, in `backend/.env`, and
plausibly in a deployed environment. Silently ignoring a variable an operator has already
set is the single worst failure mode this feature can have — the operator believes they
configured a limit, and they did not. A loud alias costs about ten lines.

**Alternatives considered**: Hard rename with no alias — rejected, silently drops working
configuration. Keeping the old names as the scheme for all six surfaces — rejected, the
prefix would be `TICKET_LOOKUP_` for a booking limit.

---

## R3 — A disabled throttle is not mounted, rather than mounted and skipped

**Decision**: When a surface is inactive, its constructor returns a pass-through and no
token-bucket store is allocated. `Cooldown.Take` on a disabled cooldown returns
`(true, 0)` without touching the visitor map. `streamLimiter.acquire` returns a no-op
release and `true`.

**Rationale**: FR-010 requires that a disabled surface produce no throttle-related refusal,
and the Technical Context requires a disabled throttle cost nothing per request. Echo's
`RateLimiterConfig.Skipper` would satisfy the first but not the second — the store is still
allocated and its eviction goroutine still runs. Not mounting is also easier to prove: there
is no code path to the refusal, rather than a branch that must be shown never to be taken.

**Critical caveat — "not mounted" means the limiter, not the group.** A disabled surface
MUST still construct its group and register its route; only the limiter becomes a
pass-through. Echo's `Group.Use` registers two catch-all `RouteNotFound` routes whenever a
group carries middleware
([group.go:21-33](../../backend/vendor/github.com/labstack/echo/v4/group.go#L21-L33)), and
`main.go` builds five groups on the identical `/api/v1` prefix. Dropping a group entirely
would therefore change which middleware chain answers unmatched `/api/v1/*` paths, making
404 behaviour differ between throttling modes. The acceptance specs MUST assert that an
unmatched path answers identically with throttling on and off.

**Alternatives considered**: `Skipper: func(echo.Context) bool { return true }` — rejected
per above. Wiring `rate.Inf` as the limit — rejected because it leaves the store, the
eviction, and the memory growth in place while looking disabled.

---

## R4 — Throttle acceptance tests get their own API instance

**Decision**: `e2e/playwright.config.ts` gains a second API server on its own port,
configured with deliberately tight thresholds, used only by `e2e/specs/rate-limit.spec.ts`.
The main suite's API keeps today's default thresholds.

**Rationale**: Token buckets are keyed per IP and live in process memory shared across the
whole run. An exhaustion test needs a threshold small enough to trip in a second or two; the
guest-purchase journey needs one generous enough that a scripted browser never trips it.
Both cannot be true of one process, and every request in a local run arrives from the same
IP. Draining a bucket in one spec would refuse an unrelated spec for up to the refill time —
flakiness injected directly into the acceptance gate.

**Alternatives considered**:

- **Distinct `X-Forwarded-For` per test** to mint separate buckets. Rejected on principle: it
  works only because of the bypass R7 exists to close, so the suite would depend on the
  defect and break the moment it is fixed.
- **Short idle-TTL plus serial ordering.** Rejected — timing-dependent, and the failure mode
  is an intermittently red acceptance gate, which is worse than the cost of a second process.
- **Cover throttling only in Go tests.** Rejected — Principle VIII puts defects reachable
  through the assembled system in `e2e/`, and a 429 reaching a real browser is exactly that.

---

## R5 — The CI matrix is three runs, not four

**Decision**: Run the suite as (cache on, throttle on) — the baseline; (cache **off**,
throttle on) — Principle VII's obligation; and (cache on, throttle **off**) — FR-020's
obligation. Do not run the fourth cell.

**Rationale**: Principle VII requires both cache modes and FR-020 requires both throttle
modes. Taken as a cross product that is four full runs of a suite that drives a real browser.
The fourth cell (both off) tests an interaction that does not exist: no throttle reads or
writes the cache, and no cached read is throttled by anything the cache affects. Three runs
discharge both obligations with each toggle independently exercised.

**Alternatives considered**: All four — rejected as cost with no distinct failure it could
catch. Two (fold throttle-off into the cache-off run) — rejected because a failure in that
run would not say which toggle caused it.

---

## R6 — FR-012 needs a one-word amendment; the frontend does not change

**Decision**: Leave [order-confirmation.tsx](../../frontend/components/order/order-confirmation.tsx)
alone. Read FR-012 as *"the guest is not held behind the **enforced** cooldown"*, and amend
its wording in the spec accordingly. The 5-second client-side debounce floor stays in all
modes.

**Rationale**: This is the second discovery. With the cooldown disabled, the server returns
`retry_after_seconds: 0`, and the frontend deliberately floors zero to
`FALLBACK_COOLDOWN_SECONDS = 5`
([order-confirmation.tsx:152-159](../../frontend/components/order/order-confirmation.tsx#L152-L159)).
So FR-012 as written — *"MUST NOT present a countdown the guest is required to wait out"* —
cannot be satisfied without deleting that floor.

The floor should not be deleted. Its documented reason is that a live button plus a held key
produces a burst of requests. Disabling the *throttle* removes the refusals but not the
consequence: every press still sends a real email. A 5-second debounce with throttling off
is protecting the buyer's inbox, which is the same thing the cooldown protects, at a scale
appropriate to a UI affordance rather than a server limit.

The distinction that matters to a guest is 5 seconds of debounce versus 60 seconds of
enforced wait. FR-012's intent is that the second disappears. It does.

**Alternatives considered**: Delete the floor when disabled — rejected, reintroduces the
held-key mail flood the floor was written to stop. Add a field to the response saying
"throttling is off" — rejected, publishes the operational posture of the service to every
unauthenticated guest, which is the same objection as R8.

---

## R7 — The per-IP limiters do not currently limit per IP

**Decision**: Set `e.IPExtractor = echo.ExtractIPDirect()` in `cmd/api/main.go`, with a
documented path to `echo.ExtractIPFromXFFHeader(echo.TrustLinkLocal(), …)` for a deployment
that genuinely runs behind a trusted proxy.

**Rationale**: This is the first discovery, and the more serious one.
`cmd/api/main.go` never sets `e.IPExtractor`, and `pkg/`/`internal/` never set it either.
Echo's `Context.RealIP()` therefore falls through to its legacy behaviour: it returns the
first value of a caller-supplied `X-Forwarded-For` header, and only if that is absent does it
use the actual socket address
([context.go:309-331](../../backend/vendor/github.com/labstack/echo/v4/context.go#L309-L331)).

Every throttle keyed on `c.RealIP()` — booking, availability, ticket lookup, checkout, and
the SSE connection cap, which is five of the six surfaces in scope — is therefore bypassable
by sending a different `X-Forwarded-For` on each request. The ticket-code enumeration defence
that FR-020 of spec 001 and [ARCHITECTURE.md:160](../../ARCHITECTURE.md#L160) both claim is
not currently in force.

`ExtractIPDirect()` is correct for the deployment shape this project actually has, where the
API is reached directly. If a reverse proxy is introduced, the header becomes trustworthy
only from that proxy's address, which is what `ExtractIPFromXFFHeader` with an explicit trust
list expresses — and that is a configuration change, not a code change, so it belongs in the
same settings surface this feature is building.

**Why it belongs in this feature rather than a follow-up**: the spec's SC-004 and SC-005
require demonstrating that a surface refuses at its configured threshold and that a
per-surface switch works. Neither can be honestly demonstrated while any client can mint a
fresh bucket per request. Writing the e2e throttle specs first and fixing the bypass second
would mean authoring them against known-broken behaviour and rewriting them immediately.

**Principle VIII obligation**: this is a bugfix in a covered flow, so its scenario MUST be
seen red first — a rotated-header request escaping the booking limiter on unfixed code,
before the fix lands.

**Alternatives considered**: `ExtractIPFromXFFHeader()` with no trust options — rejected, it
still trusts an untrusted hop. Leaving it and documenting the limitation — rejected, see
Complexity Tracking in [plan.md](plan.md).

---

## R8 — The effective configuration is logged at startup and is *not* exposed on `/healthz`

**Decision**: One structured log line at startup naming the master switch and each surface's
effective values. Nothing is added to `/healthz` or any other endpoint.

**Rationale**: FR-017 requires an operator be able to confirm what is in force. A startup log
line does that and matches how this service already reports `payment expiry sweeper started`.

`/healthz` is public and unauthenticated. Publishing exact thresholds there hands an attacker
the precise budget to stay under — it converts a limit into a documented allowance. The
operator has log access; the internet does not, and should not.

**Alternatives considered**: An authenticated admin endpoint — rejected as scope the spec
explicitly excludes (no admin console surface). `/healthz` — rejected per above.

---

## R9 — The cooldown's retention invariant generalises to every surface

**Decision**: Validate `IdleTTL > burst / rate` for **every** enabled rate-shaped surface,
not just the resend cooldown.

**Rationale**: `cooldown.go` documents the invariant that idle retention MUST exceed the
window itself, because evicting a key mid-cooldown forgives it silently, and
`cooldown_test.go` asserts it for the values `main.go` currently wires. The same hazard
applies to every token bucket: a client throttled down to an empty bucket, evicted before it
refills, returns with a full one. Today one shared constant (`rateLimitWindow = 3m`) sits
comfortably above all six windows, so nobody has had to think about it. Once an operator can
set both sides, they can invert them.

Checked against today's defaults, every surface passes, which is a useful confirmation of
FR-006:

| Surface | Full-burst refill | Retention | Holds |
|---------|-------------------|-----------|-------|
| Booking | 5 / 0.33 ≈ 15.2s | 180s | ✓ |
| Availability | 10 / 1.0 = 10s | 180s | ✓ |
| Ticket lookup | 10 / 5 = 2s | 180s | ✓ |
| Checkout | 3 / 0.2 = 15s | 180s | ✓ |
| Resend cooldown | 1 / (1/60) = 60s | 180s | ✓ |
| Status stream | n/a — a concurrency cap has no refill | n/a | n/a |

**Alternatives considered**: Per-surface retention settings — rejected as configuration
surface with no demonstrated need; one shared retention with a validated floor is simpler and
covers the hazard. Checking only the resend — rejected, the hazard is general and the check
costs one loop.

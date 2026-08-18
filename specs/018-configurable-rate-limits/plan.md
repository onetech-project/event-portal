# Implementation Plan: Configurable Rate Limits

**Branch**: `fix/rate-limit` | **Date**: 2026-08-18 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/018-configurable-rate-limits/spec.md`

> **Branch note**: `setup-plan.sh` reports `BRANCH=018-configurable-rate-limits` because it
> derives the name from `.specify/feature.json`. The actual git branch is `fix/rate-limit`.
> The spec directory and the branch are independent by design; no rename is needed.

## Summary

Six throttling mechanisms are currently compiled into the API as Go constants. This change
moves every one of their numbers into `pkg/config`, adds a master switch plus a per-surface
switch, validates the combinations that would silently break the limiter, and reports the
effective configuration at startup. Nothing about *when* configuration is read changes: it
is read once at process start, exactly like every other setting this service has. "Not
build time" means no rebuild, not no restart (spec Clarifications Q1).

Two problems surfaced while reading the code that the spec did not anticipate, and both are
resolved here rather than discovered during implementation:

1. **The per-IP limiters do not actually limit per IP.** `e.IPExtractor` is never set, so
   Echo falls back to trusting a caller-supplied `X-Forwarded-For` header
   ([context.go:309-331](../../backend/vendor/github.com/labstack/echo/v4/context.go#L309-L331)).
   Every per-IP throttle in the system is bypassable by rotating one header. See
   [research.md](research.md) R7.
2. **`retry_after_seconds: 0` does not mean "no wait" to the frontend.** It floors to a
   5-second debounce
   ([order-confirmation.tsx:152-159](../../frontend/components/order/order-confirmation.tsx#L152-L159)),
   deliberately. FR-012 as written is therefore unsatisfiable without removing a guard that
   exists for a good reason. See [research.md](research.md) R6.

## Technical Context

**Language/Version**: Go 1.26.5 (backend), TypeScript / Next.js (frontend), TypeScript /
Playwright (e2e)

**Primary Dependencies**: `labstack/echo/v4` (routing + its `middleware.RateLimiter`),
`golang.org/x/time/rate` (token buckets, used directly by `httpx.Cooldown`). No new
dependency is introduced by this feature.

**Storage**: None. Throttle state is per-process memory and is deliberately not persisted or
shared; configuration is environment variables read once at startup. No migration, so
`SCHEMA.md` is untouched.

**Testing**: `backend/scripts/test.sh ./...` (Go unit + database-backed), `npx vitest run`
(frontend), `e2e/` (Playwright against the real stack).

**Target Platform**: Linux container, single deployable (`cmd/api`).

**Project Type**: Web service + web frontend + end-to-end acceptance suite.

**Performance Goals**: No change to request-path cost. A disabled throttle MUST allocate no
store and add no per-request work — it is not mounted at all, rather than mounted and
skipped.

**Constraints**: Defaults must reproduce today's behaviour byte-for-byte (FR-006).
Validation must run at startup and report every problem at once, matching the existing
`config.Load()` error-accumulation contract.

**Scale/Scope**: 6 throttled surfaces, ~18 new environment variables, 2 legacy variable
names preserved as aliases, 1 shared idle-retention setting.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
|-----------|---------|----------|
| I. Modular Monolith | **PASS** | Configuration lives in `pkg/config`; throttle construction in `pkg/httpx`; all wiring in `cmd/api/main.go`, which is already the only place domains meet. |
| II. Domain Isolation | **PASS** | No domain imports another. `notification` already receives `*httpx.Cooldown` as a parameter; `payment`'s stream cap becomes a parameter the same way. Enforced by `cmd/api/architecture_test.go`. |
| III. DTO Isolation | **PASS** | No DTO changes. `PublicRetryAfter` already carries `retry_after_seconds`; its meaning is unchanged. |
| IV. Transactional Integrity | **PASS** | Untouched. No throttle sits inside a transaction; the limiters run as middleware before any handler opens one. |
| V. Payment Gateway Abstraction | **PASS** | Untouched. The checkout limiter protects outbound gateway calls and does not reach into the gateway abstraction. |
| VI. Guest-First MVP Scope | **PASS** | No new product surface. Operator configuration only; no admin console screen (spec Out of Scope). |
| VII. Cache | **PASS, with a cost** | No cache interaction. But Principle VII already requires the suite pass with cache on *and* off, and FR-020 now requires it pass with throttling on *and* off. See the matrix decision in [research.md](research.md) R5 — this is 3 CI runs, not 4. |
| VIII. E2E Acceptance | **PASS, and it is the bulk of the work** | See the mandatory rows below. Principle VIII now also names `E2E_RATE_LIMIT_ENABLED` and requires throttle scenarios be isolated from one another. |
| IX. Request Throttling (**new, v4.2.0**) | **PASS by construction — this feature is its discharge** | Added by the amendment this feature triggered. The repository is knowingly non-compliant with its client-identity bullet until T005 lands; that is recorded in the constitution's Follow-up TODOs rather than softened. |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes. Every throttled surface sits
      on a covered flow: booking and checkout in `e2e/specs/guest-purchase.spec.ts`, ticket
      lookup in the same file, the resend cooldown in the spec-016 delivery scenarios, and
      the SSE status stream in the checkout journey. **These specs do not currently assert
      anything about rate limiting** — a four-way sweep of `e2e/` found zero coverage of
      429s or throttle behaviour. Files that must change: `e2e/support/env.ts`,
      `e2e/playwright.config.ts`, and a new `e2e/specs/rate-limit.spec.ts`.
- [x] **New user-visible flow?** No new flow. But throttle *refusals* are user-visible and
      have never been covered, so this change arrives with the coverage that was missing:
      FR-021 (refusal observed at the configured threshold, and absent when disabled) and
      FR-022 (per-surface switch — one off, another still in force).
- [x] **Bugfix in a covered flow?** Yes — R7, the `X-Forwarded-For` bypass, is a genuine
      defect in a covered flow. Per Principle VIII its scenario MUST be confirmed failing
      against the unfixed code before the fix lands: a request carrying a rotated
      `X-Forwarded-For` must be shown to escape the booking limiter today, and to be caught
      after. This ordering is a task-level obligation, recorded here so it cannot be
      quietly skipped.
- [x] **Behaviour change under `E2E_CACHE_ENABLED=false`?** None. Throttling and caching do
      not interact — no throttle reads or writes the cache. The suite must still pass in
      both cache modes, which the matrix in R5 preserves.

## Project Structure

### Documentation (this feature)

```text
specs/018-configurable-rate-limits/
├── plan.md              # This file
├── research.md          # Phase 0 — 8 decisions, including the two discoveries above
├── data-model.md        # Phase 1 — config types, defaults, validation rules
├── quickstart.md        # Phase 1 — how to verify the feature end to end
├── contracts/
│   └── configuration.md # Phase 1 — the environment-variable contract
├── checklists/
│   └── requirements.md  # From /speckit-specify — 16/16
└── tasks.md             # Phase 2 (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
backend/
├── cmd/api/
│   └── main.go                      # DELETE 6 const blocks; wire cfg.Throttle;
│                                    # set e.IPExtractor (R7); log effective config
├── pkg/config/
│   ├── config.go                    # ADD ThrottleConfig + policies + validation
│   └── config_test.go               # ADD defaults-reproduce-today, validation matrix
└── pkg/httpx/
    ├── rate_limit.go                # ADD disabled → no-op middleware, no store
    ├── rate_limit_test.go           # ADD disabled-path tests
    ├── cooldown.go                  # ADD enabled flag; disabled Take → (true, 0)
    └── cooldown_test.go             # GENERALISE the idle>window invariant assertion

backend/internal/payment/
└── stream.go                        # streamLimiter takes cap + enabled from wiring

frontend/
└── components/order/order-confirmation.tsx   # UNCHANGED — see research.md R6

e2e/
├── support/env.ts                   # ADD rateLimitEnabled
├── playwright.config.ts             # ADD throttle env to the api server; ADD a second
│                                    # api instance with tight limits for throttle specs
├── specs/rate-limit.spec.ts         # NEW — FR-021, FR-022, and the R7 bypass regression
└── README.md                        # DOCUMENT the new toggle alongside E2E_CACHE_ENABLED

backend/.env.example                 # DOCUMENT all new settings (FR-018)
ARCHITECTURE.md                      # line 160 says the lookup "is rate limited per IP" —
                                     # still true, but now configurable and disableable
```

**Structure Decision**: The repository is an established web application — a Go modular
monolith under `backend/`, a Next.js frontend under `frontend/`, and a Playwright acceptance
suite under `e2e/`. This feature adds no new module and no new layer. It moves constants
into the existing `pkg/config` loader, adapts the two existing throttle constructors in
`pkg/httpx`, parameterises one in-handler limiter in `internal/payment`, and adds the
acceptance coverage Principle VIII requires. The frontend is deliberately untouched.

## Complexity Tracking

> Filled because two items go beyond the spec's literal scope and need justification.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|--------------------------------------|
| ~~Setting `e.IPExtractor` (R7)~~ — **no longer a deviation.** Now required by spec FR-002a/FR-002b and Constitution Principle IX ("a per-client throttle MUST key on an identity the client cannot forge"), and explicitly authorised. Row retained for the audit trail. | Every per-IP throttle in scope is currently bypassable with one header. Shipping configurable limits without this delivers a dial connected to nothing, and the spec's own SC-004/SC-005 (per-surface enforcement) cannot be honestly demonstrated while any client can mint a fresh bucket per request. | Leaving it: rejected because the feature's central promise — that these limits are enforceable and tunable — would be false. Deferring to a follow-up: rejected because the e2e throttle specs written here would have to be authored against the broken behaviour and then rewritten. |
| ~~A second API instance in `e2e/playwright.config.ts` (R4)~~ — **no longer a deviation.** Principle VIII's new "Both throttling modes" bullet requires exactly this isolation. Row retained for the audit trail. | Exhaustion tests need thresholds small enough to trip in seconds; the rest of the suite needs thresholds generous enough not to trip at all. Per-IP buckets are process-wide and shared, so one instance cannot serve both without cross-test poisoning. | Distinct `X-Forwarded-For` per test: rejected — it depends on exactly the bypass R7 fixes. Short idle-TTL plus serial ordering: rejected as timing-dependent flakiness in the acceptance gate. Testing throttles only in Go unit tests: rejected — Principle VIII puts covered flows in `e2e/`. |

## Post-Design Constitution Re-Check

*Run after Phase 1. Gates re-evaluated against the design that actually emerged, not the
one anticipated before research.*

| Principle | Verdict | What changed during design |
|-----------|---------|----------------------------|
| I. Modular Monolith | **PASS** | Confirmed. `ThrottleConfig` hangs off `config.Config` in `pkg/`; no new package, no new layer. |
| II. Domain Isolation | **PASS** | Confirmed, and tightened. `internal/payment`'s stream cap becomes a wiring parameter rather than a package constant, which moves a decision *out* of a domain and into the composition root. `architecture_test.go` continues to enforce this. |
| III. DTO Isolation | **PASS** | Confirmed — no DTO field added, removed or re-typed. R6 turned on *not* changing the response shape. |
| IV. Transactional Integrity | **PASS** | Unchanged. |
| V. Payment Gateway Abstraction | **PASS** | Unchanged. |
| VI. Guest-First MVP Scope | **PASS** | Confirmed. R8 actively rejected the one design that would have added a surface (exposing thresholds on an endpoint). |
| VII. Cache | **PASS** | Confirmed independent. R5 discharges the both-modes obligation in three runs. |
| VIII. E2E Acceptance | **PASS, with two obligations recorded** | R4 settled *how* (a second API instance with tight thresholds). R7 added a red-first obligation that did not exist when the gate was first evaluated. |

**Two design decisions changed the spec rather than merely implementing it**, both recorded
in `research.md` and applied:

1. **FR-012 was amended** (R6). As written it required the guest-facing countdown to vanish
   when the cooldown is disabled. The frontend deliberately floors `retry_after_seconds: 0`
   to a 5-second debounce, and that floor should stay: disabling the *throttle* removes the
   refusals but not the mail. FR-012 now distinguishes the enforced minute from the UI
   debounce.
2. **R7 added work the spec did not ask for.** Justified in Complexity Tracking above. It is
   the difference between shipping a configurable limit and shipping a configurable limit
   that is actually enforced.

**No new violations.** The Complexity Tracking table is complete and both entries carry a
rejected simpler alternative.

**Gate result: PASS.** Ready for `/speckit-tasks`.

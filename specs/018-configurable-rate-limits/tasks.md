---
description: "Task list for Configurable Rate Limits"
---

# Tasks: Configurable Rate Limits

**Input**: Design documents from `/specs/018-configurable-rate-limits/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/configuration.md](contracts/configuration.md)

**Tests**: Included and NOT optional. [AGENTS.md](../../AGENTS.md) requires all three tiers, and Constitution **Principle VIII** makes `e2e/` an acceptance gate. FR-025 additionally requires one scenario be seen **red first**.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: Which user story this task belongs to

## Path Conventions

Web app: Go backend at `backend/`, Next.js frontend at `frontend/`, Playwright acceptance suite at `e2e/`.

---

## Phase 1: Setup

**Purpose**: Nothing to scaffold — no new package, no new dependency, no migration. This phase only pins the baseline that FR-006 is measured against.

- [X] T001 Record the current constant values as the baseline in `specs/018-configurable-rate-limits/data-model.md` — verify each of the seven defaults against source before writing any code (`backend/cmd/api/main.go:38,43-44,52-53,58-59,68-69`, `backend/pkg/config/config.go:233-234`, `backend/internal/payment/stream.go:129`)
- [X] T002 Confirm the baseline suite is green before any change: `cd backend && ./scripts/test.sh ./...` and `cd e2e && npm test`

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The client-identity repair (FR-002a/FR-002b) and the config type surface. **Every user story depends on both**: no throttle threshold is honestly demonstrable while the key it is enforced on is forgeable, and no story can be wired before the config types exist.

**⚠️ T003 MUST be seen failing before T005 lands (FR-025, Principle VIII).**

- [X] T003 Write the red-first bypass regression in `e2e/specs/rate-limit.spec.ts` — book repeatedly while rotating `X-Forwarded-For`, assert the limiter refuses; run it against unfixed code and **confirm it fails** (twenty 200s today), recording the failure output in the PR
- [X] T004 [P] Add `TrustedProxyCIDRs []string` to `Config` in `backend/pkg/config/config.go`, parsed from `TRUSTED_PROXY_CIDRS`, empty default, invalid CIDR appended to `loader.errs`
- [X] T005 Set `e.IPExtractor` in `backend/cmd/api/main.go` — `echo.ExtractIPDirect()` when `TRUSTED_PROXY_CIDRS` is empty, `echo.ExtractIPFromXFFHeader(echo.TrustIPRange(...)...)` otherwise; verify T003 now passes
- [X] T006 [P] Add `ThrottlePolicy`, `CooldownPolicy`, `StreamPolicy` and `ThrottleConfig` types to `backend/pkg/config/config.go` per [data-model.md](data-model.md), including `Active(master bool) bool` and `RefillWindow() time.Duration`
- [X] T007 Load all throttle settings in `config.Load()` in `backend/pkg/config/config.go` using the existing `l.float`/`l.integer`/`l.boolean`/`l.duration` helpers, with the exact defaults from [data-model.md](data-model.md) — `0.33` stays `0.33`, never `1.0/3.0`
- [X] T008 Implement the legacy alias path for `TICKET_LOOKUP_RATE_LIMIT` / `TICKET_LOOKUP_BURST` in `backend/pkg/config/config.go` (FR-006a): new name wins when both set, legacy honoured when alone, deprecation recorded for the startup report
- [X] T009a Ensure the FR-015 rejection message names the minimum retention that would make the configuration valid, so an operator raising a burst is told what to set (FR-015a)
- [X] T009 Implement validation rules V1–V7 in `config.Load()` in `backend/pkg/config/config.go` per [data-model.md](data-model.md) — appending to `loader.errs` so every problem is reported in one pass; V2/V4/V5 apply only to **enabled** surfaces; V6 compares the shared retention against the longest window across all enabled surfaces
- [X] T010 [P] Add `pkg/config` tests in `backend/pkg/config/config_test.go`: each default equals the constant it replaced (FR-006), the alias precedence matrix (FR-006a), and every failure shape in the [contracts/configuration.md](contracts/configuration.md) validation table (FR-013/FR-014/FR-015)

**Checkpoint**: config loads, validates and reports; per-client keys are no longer forgeable. No behaviour has changed for a deployment that sets nothing.

---

## Phase 3: User Story 1 — Tune a throttle without producing a new build (P1) 🎯 MVP

**Goal**: Every threshold currently welded into the artifact is settable from the deployment's configuration, and an unset deployment is byte-for-byte today's system.

**Independent Test**: Change one surface's allowance, restart, drive that surface to refusal, observe the new threshold. No build artifact produced.

- [X] T011 [US1] Delete the six constant blocks from `backend/cmd/api/main.go` (`rateLimitWindow`, `gatewayCall*`, `guestResend*`, `book*`, `availability*`) and wire all five limiter groups from `cfg.Throttle`
- [X] T012 [P] [US1] Extend `NewCooldown` in `backend/pkg/httpx/cooldown.go` to take its window as a duration, preserving `1.0/60.0` semantics exactly for the `60s` default
- [X] T013 [P] [US1] Parameterise the stream cap in `backend/internal/payment/stream.go` — `newStreamLimiter` receives its ceiling from wiring rather than `defaultStreamCap`; delete the constant
- [X] T014 [US1] Pass the stream cap from `backend/cmd/api/main.go` into the payment handler, keeping the domain free of the decision (Principle II)
- [X] T015 [P] [US1] Generalise the retention invariant assertion in `backend/pkg/httpx/cooldown_test.go` from the single wired pair to every enabled surface's configured pair (R9)
- [X] T016 [P] [US1] Add the throttle block to `backend/.env.example` from [contracts/configuration.md](contracts/configuration.md) — every setting with default, unit, and what it protects (FR-018)

**Checkpoint**: US1 is independently demonstrable. Quickstart steps 1 and 2 pass.

---

## Phase 4: User Story 2 — Turn throttling off (P1)

**Goal**: One switch removes all six throttles from the request path, with no other behavioural difference.

**Independent Test**: Start with the master switch off, drive every surface far past its threshold, observe no throttle refusal; restart with it on and the refusals return.

- [X] T017 [US2] Return a pass-through middleware from `RateLimitPerIP`/`RateLimitBy` in `backend/pkg/httpx/rate_limit.go` when the surface is inactive — allocating no store (R3)
- [X] T018 [P] [US2] Make `Cooldown.Take` in `backend/pkg/httpx/cooldown.go` return `(true, 0)` without touching the visitor map when disabled
- [X] T019 [P] [US2] Make `streamLimiter.acquire` in `backend/internal/payment/stream.go` return a no-op release and `true` when disabled
- [X] T020 [US2] Ensure every group in `backend/cmd/api/main.go` is still constructed and its routes still registered when its limiter is disabled — only the limiter becomes a pass-through (R3 caveat: Echo registers catch-all `NotFound` routes per group with middleware, and five groups share `/api/v1`)
- [X] T021 [P] [US2] Add `backend/pkg/httpx/rate_limit_test.go` cases proving a disabled limiter allocates no store and refuses nothing
- [X] T022 [P] [US2] Add a test asserting an unmatched `/api/v1/*` path answers identically with throttling on and off (guards the R3 caveat)

**Checkpoint**: US2 is independently demonstrable. Quickstart step 3 passes.

---

## Phase 5: User Story 3 — Switch one throttle off without switching them all off (P2)

**Goal**: Per-surface switches, with a precedence rule stated once so the two switches cannot contradict.

**Independent Test**: Disable exactly one surface; it refuses nothing while every other surface still refuses at its threshold.

- [X] T023 [US3] Implement FR-009 precedence solely inside `ThrottlePolicy.Active()` in `backend/pkg/config/config.go` — master off wins, master on defers, unset per-surface reads as enabled
- [X] T024 [US3] Route every mount decision in `backend/cmd/api/main.go` through `Active()` so precedence cannot be recombined differently at different call sites
- [X] T025 [P] [US3] Add the precedence matrix to `backend/pkg/config/config_test.go` — all four combinations of master × per-surface, including master-off-surface-on

**Checkpoint**: US3 is independently demonstrable. Quickstart step 4 passes.

---

## Phase 6: Observability & Documentation (Cross-Cutting)

- [X] T026 Emit the startup report in `backend/cmd/api/main.go` — one structured `info` line naming the master switch and every surface's effective values, plus a deprecation notice per legacy alias in use (FR-017)
- [X] T027 [P] Add a test asserting no throttle threshold appears in the `/healthz` payload — publishing them converts a limit into a documented allowance (R8)
- [X] T028 [P] Document the blast radius of each switch in `backend/.env.example` (FR-018): booking holds quota, checkout costs a gateway call, resend sends real mail, and the connection cap is the only bound on held-open connections and their periodic database reads
- [X] T029 [P] Update [ARCHITECTURE.md](../../ARCHITECTURE.md) line 160 — the lookup is still rate limited per IP, but now configurable and disableable
- [X] T029a [P] Update `api/openapi.yml:625-626` — it publishes `TICKET_LOOKUP_RATE_LIMIT` / `TICKET_LOOKUP_BURST` by name in the API contract; add the new names and mark the old ones deprecated (FR-001, FR-006a)
- [X] T029b [P] Document the FR-002a client-identity change in `backend/.env.example` — a proxied deployment that does not set `TRUSTED_PROXY_CIDRS` will see its clients collapse onto the proxy address (SC-003)

---

## Phase 7: End-to-End Acceptance (Principle VIII — NOT optional)

**Purpose**: Discharge FR-020 through FR-025. There is currently **zero** e2e coverage of rate limiting, so this phase is the bulk of the acceptance value.

- [X] T030 Add `rateLimitEnabled: env("E2E_RATE_LIMIT_ENABLED", "true") === "true"` to `e2e/support/env.ts`, mirroring `cacheEnabled` (FR-024)
- [X] T031 Pass the throttle settings into the API server env block in `e2e/playwright.config.ts` (~line 91-140), which currently passes none
- [X] T032 Add a second API instance on its own port to `e2e/playwright.config.ts` with deliberately tight thresholds, used only by the throttle specs (FR-023, R4) — the main suite's API keeps today's defaults
- [X] T033 [P] Complete `e2e/specs/rate-limit.spec.ts`: a surface driven past its configured threshold observes the refusal, and the same scenario with that throttle disabled observes none (FR-021); `test.skip` when throttling is off, mirroring `e2e/specs/cache-refresh.spec.ts:41`
- [X] T034 [P] Add the per-surface switch scenario to `e2e/specs/rate-limit.spec.ts` — one surface disabled while another remains in force (FR-022)
- [X] T035 [P] Document `E2E_RATE_LIMIT_ENABLED` in `e2e/README.md` alongside `E2E_CACHE_ENABLED` (line ~195)
- [X] T036 Run the three-mode matrix (R5): `npm test`, `E2E_CACHE_ENABLED=false npm test`, `E2E_RATE_LIMIT_ENABLED=false npm test` — all green, no scenario skipped in a mode where it should run

---

## Phase 8: Polish

- [X] T037 [P] Run the full three-tier suite: `cd backend && ./scripts/test.sh ./...`, `cd frontend && npx vitest run`, `cd e2e && npm test`
- [X] T038 [P] Walk [quickstart.md](quickstart.md) steps 1–8 by hand and confirm each stated expectation, including that a *disabled* surface's bad numbers do **not** refuse startup
- [X] T039 Confirm `backend/cmd/api/architecture_test.go` still passes — no domain gained an import of another (Principle II)
- [X] T040 Verify FR-006 one final time: check out `main`, record each surface's refusal threshold, then confirm the feature branch with no throttle variables set refuses at exactly the same points

---

## Dependencies & Execution Order

```text
Phase 1 (Setup)
      ↓
Phase 2 (Foundational) ← BLOCKS EVERYTHING; T003 red-first before T005
      ↓
      ├─→ Phase 3 (US1, P1) ──┐
      ├─→ Phase 4 (US2, P1) ──┤  US1/US2/US3 are independent of each other
      └─→ Phase 5 (US3, P2) ──┤  once Phase 2 lands
                              ↓
                    Phase 6 (Observability)
                              ↓
                    Phase 7 (E2E acceptance)
                              ↓
                    Phase 8 (Polish)
```

**Story independence**: US1, US2 and US3 touch overlapping files (`main.go`, `rate_limit.go`, `cooldown.go`), so they are independently *testable* but not safely *concurrent*. Land them in priority order.

**Within-phase parallelism**:

- Phase 2: T004 ∥ T006, then T010 ∥ once T007–T009 land
- Phase 3: T012 ∥ T013 ∥ T015 ∥ T016 (four distinct files)
- Phase 4: T018 ∥ T019 ∥ T021 ∥ T022
- Phase 6: T027 ∥ T028 ∥ T029
- Phase 7: T033 ∥ T034 ∥ T035 after T030–T032

---

## Implementation Strategy

**MVP = Phase 1 + Phase 2 + Phase 3 (US1).** That delivers the actual request — every threshold configurable, no rebuild — on a foundation where the limits are genuinely enforced. Phase 2 is not optional padding: without T005 the thresholds US1 makes tunable are bypassable by anyone sending a header.

**Second increment**: Phase 4 (US2). The kill switch is what makes tuning safe to attempt in production.

**Third**: Phase 5 (US3), then 6–8.

**Do not defer Phase 7.** Principle VIII makes it the acceptance gate, and this feature currently has no e2e throttle coverage at all — shipping Phases 1–6 without it means the kill switch has never been proven to work in the assembled system.

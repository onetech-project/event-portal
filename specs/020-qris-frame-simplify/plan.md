# Implementation Plan: QRIS Frame Simplification

**Branch**: `020-qris-frame-simplify` | **Date**: 2026-08-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/020-qris-frame-simplify/spec.md`

## Summary

Strip five printed fields from the Scan to Pay card — merchant name, NMID, terminal
label, acquirer code and printed-layout version — so the frame matches the design as
drawn (Figma `203-1201`), move the heading and subtitle into the space they vacate,
retire the configuration that fed them, and reword the one payment instruction that
pointed at a label which no longer exists.

Nothing server-side changes. No schema, no API contract, no gateway call, no cache key.
The whole change lives in four frontend files, their tests, the deployment templates, the
acceptance suite, and the governance documents that currently mandate the opposite.

The one genuinely difficult part is making the acceptance assertion *mean* something.
`frontend/.env` is gitignored, so on this machine the frame prints `JIVE` / NMID
`936008580287697876` / terminal `659` / acquirer `93600008` / version `1.0-2024.11.13`,
while in CI it prints nothing at all. A naive "the card must not show a merchant name"
assertion would therefore be red locally and green in CI *today*, before any code is
touched — a regression pin that pins nothing. The rig has to supply those five values
deliberately so that the pre-change code prints them and the assertion is red for the
right reason. That same deliberate supply is what proves FR-013.

## Technical Context

**Language/Version**: TypeScript 5 on React 19.2.4 / Next.js 16.2.12 (App Router).
Go 1.26.5 backend is **not touched**.

**Primary Dependencies**: Tailwind CSS v4 (container queries drive the frame's internal
scale), shadcn/ui `Card` primitives, TanStack Query for the order read. No new dependency.

**Storage**: None. No migration, no `SCHEMA.md` change, no Redis key.

**Testing**: Vitest 4 (`frontend`), Playwright 1.50+ (`e2e`). Go suite untouched but must
stay green.

**Target Platform**: Browser, guest-facing. The payment card renders in a two-column
checkout layout on wide viewports and stacked on narrow ones; the frame itself is sized
by a container query, so the layout it sits in is irrelevant to its internal proportions.

**Project Type**: Web application — `backend/` (Go), `frontend/` (Next.js), `e2e/`
(Playwright).

**Performance Goals**: Unchanged. The change removes DOM nodes and five runtime-config
fields; it cannot regress anything measurable.

**Constraints**: The QR image is served on demand by the API and must remain
un-re-encoded and un-resized in a way that risks scannability. The frame's brand artwork
(QRIS lockup, GPN mark, batik ground, step icons) is imaged, not redrawn, and stays
exactly as-is.

**Scale/Scope**: One guest-facing card. 4 frontend source files, 2 frontend test files,
1 e2e spec + 1 e2e support file + 1 e2e config, 3 deployment/doc files, 3 governance
files in `specs/012-manjo-payment-gateway/`.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Engaged? | Verdict |
|---|---|---|
| I. Modular Monolith | No | No backend change; no new deployable. |
| II. Domain Isolation | No | No Go package touched; `cmd/api/architecture_test.go` unaffected. |
| III. DTO Isolation | No | No DTO, no wire shape changes. The order detail response is read exactly as before. |
| IV. Transactional Integrity & Idempotency | No | No write path, no transaction, no gateway call. |
| V. Payment Gateway Abstraction | **Yes — and satisfied** | The change removes *displayed* gateway-adjacent identity. It touches nothing in the gateway port or adapter, and FR-008 forbids altering what is encoded, requested or reported. |
| VI. Guest-First MVP Scope Discipline | **Yes — and satisfied** | Purely subtractive on a guest surface already in scope. Nothing from the prohibited list is introduced. |
| VII. Cache as Disposable Read Accelerator | No | No cache key, no invalidation, no read path change. Behaviour is identical under `E2E_CACHE_ENABLED=false`. |
| VIII. End-to-End Acceptance Coverage | **Yes — the load-bearing gate** | See the block below. |
| IX. Request Throttling | No | No new endpoint, no throttled surface. Behaviour identical under `E2E_RATE_LIMIT_ENABLED=false`. |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes — the guest purchase
      journey, at the QRIS step. The specs that must change:
      - `e2e/specs/guest-purchase.spec.ts` — the `browses, books, pays and receives
        tickets` journey passes through the payment card and currently asserts only
        "waiting for payment". A card-contents assertion is added.
      - `e2e/support/journey.ts` — `expectAwaitingPayment()` is where that assertion
        belongs, so every scenario that reaches the payment screen gets it for free.
      - `e2e/playwright.config.ts` — the `frontend` web server must be given the five
        retired values, for the reason in the Summary. Without this the new assertion is
        green before the change and proves nothing.
- [x] **New user-visible flow?** No. An existing covered flow changes what it shows.
      The suite currently asserts nothing about the card's contents, which is precisely
      the "green because the suite never looked" condition Principle VIII names — so
      this change *adds* the coverage that was missing rather than merely adjusting it.
- [x] **Bugfix in a covered flow?** No — a deliberate design change. The
      fails-before-the-fix discipline is applied anyway, per spec SC-005: the new
      assertion MUST be run against unmodified `qris-panel.tsx` and seen red, with the
      failure showing the five values it found. A pass at that point means the rig is
      not supplying them and the test is worthless.
- [x] **Behaviour under `E2E_CACHE_ENABLED=false`?** No change. The card is rendered
      from the order detail read, which the cache serves identically in both modes. The
      suite is still run both ways, and under `E2E_RATE_LIMIT_ENABLED=false` as well.

**Governance obligation (Governance section + AGENTS.md).** This feature *contradicts*
requirements already ratified in `specs/012-manjo-payment-gateway/`. Those edits land in
the same change or the change is incomplete:

- `specs/012-manjo-payment-gateway/spec.md` — FR-021a superseded, FR-021b retired,
  FR-021c narrowed to the amount; the Clarifications entry, US-scenarios 1 and 4, and the
  deferred "derive the QRIS frame's merchant identity from the code" item all updated.
- `specs/012-manjo-payment-gateway/research.md` — the frame-identity decision (~L290) and
  the verification table row (~L332) annotated as superseded by spec 020.
- `specs/012-manjo-payment-gateway/quickstart.md` — the merchant-name verification
  paragraph (~L309) and the payload-tag → env-var table (~L325–327).
- `README.md` — the two rows in the per-environment configuration table and the paragraph
  explaining why the QRIS fields are deliberately left unset.

No constitution amendment is required: no principle changes. `ARCHITECTURE.md`, `PRD.md`
and `SCHEMA.md` need no edit — none of them mentions the frame's printed identity.

**Gate result: PASS.** No violations, so Complexity Tracking stays empty.

## Project Structure

### Documentation (this feature)

```text
specs/020-qris-frame-simplify/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── ui.md            # Phase 1 output — the card's element inventory + retired config
├── checklists/
│   └── requirements.md  # From /speckit-specify
└── tasks.md             # /speckit-tasks — NOT created here
```

### Source Code (repository root)

```text
frontend/
├── components/order/
│   ├── qris-panel.tsx            # CHANGED — remove 5 fields + footer block; heading
│   │                             #   and subtitle move inside the frame
│   ├── qris-panel.test.tsx       # NEW — component-level pin on the element inventory
│   └── qris-instructions.tsx     # CHANGED — step 4 verifies the amount only
├── lib/
│   ├── env.ts                    # CHANGED — 5 accessors deleted
│   ├── runtime-config.ts         # CHANGED — 5 fields off RuntimeConfig + env reader
│   └── runtime-config.test.ts    # CHANGED — precedence re-expressed on API_BASE_URL
└── .env.example                  # CHANGED — 5 entries removed

e2e/
├── playwright.config.ts          # CHANGED — frontend web server supplies the 5 retired
│                                 #   values on purpose (see research.md D-3)
├── support/journey.ts            # CHANGED — expectAwaitingPayment asserts card contents
└── specs/guest-purchase.spec.ts  # CHANGED — an explicit card-contents scenario

docker-compose.yml                # CHANGED — 5 service env entries + their comment
README.md                         # CHANGED — config table rows + explanatory paragraph

specs/012-manjo-payment-gateway/  # CHANGED — spec.md, research.md, quickstart.md
                                  #   (governance obligation above)

backend/                          # UNTOUCHED
```

**Structure Decision**: The existing three-tier layout (`backend/`, `frontend/`, `e2e/`)
is unchanged. This feature is confined to `frontend/` plus the acceptance suite and the
documents that describe deployment and prior intent. The one new file is a component test
beside the component it pins, matching the existing convention
(`expiry-countdown.test.tsx`, `order-summary-panel.test.tsx`, `payment-status-card.test.tsx`).

## Constitution Re-check (post-design)

Re-evaluated after Phase 1. **Still PASS**, and the design produced no new engagement:

- No artifact introduced a backend change, a schema change, a wire-contract change, a
  cache key, or a throttled surface. Principles I–IV, VII and IX remain untouched.
- Principle V holds: [contracts/ui.md](./contracts/ui.md) removes *displayed* identity
  only. The QR payload, the charge request and the callback handling are outside every
  file this plan names.
- Principle VIII is strengthened rather than merely satisfied. Design surfaced
  [research.md D-3](./research.md): `frontend/.env` is gitignored, so the naive assertion
  would be green in CI before the change. The rig now supplies the retired values on
  purpose, which makes the red-first step real and simultaneously discharges FR-013.
  [quickstart.md](./quickstart.md) step 2 is a hard stop — a pass there invalidates the
  whole run.
- Governance obligation unchanged and still in scope for the same commit: three files in
  `specs/012-manjo-payment-gateway/` plus `README.md`. No constitution amendment needed.

One thing worth stating plainly rather than burying: this feature **removes a payment
safety affordance**. The merchant-name comparison was the guest's only in-page defence
against a swapped code, and by explicit decision it goes. The amount check now carries
that weight alone, which is why FR-010 demands the exact formatted amount in the sentence
rather than a generic instruction, and why [research.md D-8](./research.md) recommends
confirming with the acquirer that printed identity is not required on a dynamic on-screen
code. That question does not block the work — nothing here alters the payload — but it
should be asked rather than assumed indefinitely.

## Complexity Tracking

> No Constitution Check violations. Nothing to justify.

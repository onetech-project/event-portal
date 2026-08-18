---

description: "Task list for 019-qris-frame-simplify"
---

# Tasks: QRIS Frame Simplification

**Input**: Design documents from `/specs/019-qris-frame-simplify/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/ui.md](./contracts/ui.md),
[quickstart.md](./quickstart.md)

**Tests**: **Not optional for this feature.** FR-014 requires acceptance coverage, SC-005
requires it be seen red first, and Constitution Principle VIII makes `e2e/` the gate.
[research.md D-7](./research.md) adds a component tier because `e2e/` "proves the
assembled system, not each part".

**Organization**: Grouped by user story. Note the one honest exception to story
independence, stated in Dependencies below: **US3 cannot complete before US1 and US2**,
because you cannot delete an accessor that still has callers.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1 / US2 / US3 from [spec.md](./spec.md)

## Path Conventions

Web application: `frontend/` (Next.js), `e2e/` (Playwright), `backend/` (Go — **untouched
by this feature**). Repository-root files: `docker-compose.yml`, `README.md`.

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: Make the acceptance rig capable of catching the regression, and get the real
design measurements, before anything is asserted or edited.

- [X] T001 Add the five retired values to the `frontend` web server env in `e2e/playwright.config.ts` (the block that currently sets only `API_BASE_URL`, ~L219). Use the **bare runtime names** — `QRIS_MERCHANT_NAME`, `QRIS_MERCHANT_ID`, `QRIS_TERMINAL_LABEL`, `QRIS_ACQUIRER_CODE`, `QRIS_PRINT_VERSION` — because `runtimeConfigFromEnv()` reads the bare name before the `NEXT_PUBLIC_` one, so these win over a developer's gitignored `frontend/.env`. Give them unmistakable sentinel values (`E2E-MERCHANT-MUST-NOT-RENDER` and siblings). Add a comment explaining that the rig supplies them **on purpose**, so a later reader does not "tidy up" the block that makes the assertion meaningful — cite [research.md D-3](./research.md) and FR-013.
- [X] T002 [P] Load the `figma-design-to-code` skill, then call `get_design_context` on node `203-1201` of file `kelTjyctfqKz5pJQ90U6BM`. Record the frame's vertical rhythm as fractions of its 522 px width — heading, subtitle, QR top edge, `SATU QRIS UNTUK SEMUA` baseline, `aspi-qris.id` block, wedge top edge — into a scratch note for T011. Do **not** use node `947-105`: it is the generic template raster and still shows all five fields ([research.md D-1](./research.md)).
- [X] T003 [P] Capture a green baseline before touching anything: `cd e2e && npm test`. A pre-existing failure must be resolved or recorded now, so that the red produced in T006 is unambiguously attributable to the new assertion.

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: The red-first gate. Nothing in Phase 3+ may begin until T006 has been seen
fail for the right reason.

**⚠️ CRITICAL**: T006 is a hard stop. A pass there means the rig is not supplying the
values and every later green in this feature is a false one.

- [X] T004 Extend `expectAwaitingPayment()` in `e2e/support/journey.ts` to assert the payment card's contents against [contracts/ui.md](./contracts/ui.md). Assert **two ways**: (a) the T001 sentinel strings are absent — the strong local pin; (b) the structural labels `NMID`, `Dicetak oleh` and `Versi Cetak` are absent — which stays meaningful in the UAT profile, where the rig cannot set environment variables but real values would appear. Also assert the positive inventory: the "Scan to Pay" heading by role, the subtitle, and the QR image. Six specs call this method (`guest-purchase` ×4, `admin-console` ×1, `uat/purchase` ×1), so all six gain the check.
- [X] T005 Add a dedicated scenario to `e2e/specs/guest-purchase.spec.ts` that drives an order to the payment screen and asserts the full card inventory from [contracts/ui.md](./contracts/ui.md) — present elements *and* the five absences — so the contract has one named home rather than living only as a side effect of a helper. Arrange through the real API only; no direct database writes (Principle VIII).
- [X] T006 **GATE — confirm RED.** With `frontend/components/order/qris-panel.tsx` still unmodified, run `cd e2e && npm test -- guest-purchase --grep "browses, books, pays"`. It MUST fail, and the failure MUST name the sentinel strings it found. Save the failure output — SC-005 asks for it, and it is the only evidence the assertion looks where it claims to. **If this passes, stop and fix T001.**

**Checkpoint**: The regression is provably catchable. Implementation may begin.

---

## Phase 3: User Story 1 — the frame shows only what the design draws (Priority: P1) 🎯 MVP

**Goal**: The Scan to Pay card matches Figma `203-1201`: no merchant name, no NMID, no
terminal label, no `Dicetak oleh`, no `Versi Cetak`, with the heading and subtitle
occupying the vacated space.

**Independent Test**: Load the payment page for an unpaid order. The code is present and
scannable, the frame matches the design, and none of the five fields appear — in the
payable state or either ended state.

### Tests for User Story 1 ⚠️

> Write these first and watch them fail.

- [X] T007 [P] [US1] Create `frontend/components/order/qris-panel.test.tsx` following the conventions of its siblings (`expiry-countdown.test.tsx`, `order-summary-panel.test.tsx`). Cover three states — payable, `endedStatus="EXPIRED"`, `endedStatus="CANCELLED"` — asserting for each that the five fields are absent and that the contract's present elements render. Pass the five values through the runtime-config global so the test proves the component *ignores* them rather than merely that they were unset. Confirm it fails against the unmodified component.

### Implementation for User Story 1

- [X] T008 [US1] In `frontend/components/order/qris-panel.tsx`, delete the three identity blocks — merchant name, `NMID : …`, terminal label — and their `qrisMerchantName()`/`qrisMerchantId()`/`qrisTerminalLabel()` reads (FR-002, FR-003, FR-004).
- [X] T009 [US1] In the same file, delete the absolutely-positioned lower-left footer block in its entirety, along with the `qrisAcquirerCode()`/`qrisPrintVersion()` reads (FR-005). Remove the block, do not empty it — an empty positioned div is a future rendering surprise.
- [X] T010 [US1] Move the `CardHeader` content — the "Scan to Pay" title and its one-line subtitle, including the ended-state substitution "This order can no longer be paid." — inside the frame, between the QRIS/GPN lockups and the code (FR-006, [research.md D-2](./research.md)). The heading MUST remain addressable by heading role; T004 and T007 both locate it that way.
- [X] T011 [US1] Re-derive the frame's internal vertical rhythm from the T002 measurements, keeping every value a container-query fraction of the frame's own width (`cqw`/percentage), exactly as the file already does. Do not switch to fixed pixels — the container query is what keeps the frame correct in both the stacked and two-column checkout layouts.
- [X] T012 [US1] Update the component's doc comment. It currently argues at length that the merchant name, registration number and terminal label are "rendered prominently" as an anti-swap check, and cites FR-021a. That rationale is now false and must be replaced with the current one, citing spec 019 and noting that the amount check in the instructions carries the verification weight.
- [X] T013 [US1] Remove the now-unused imports from `@/lib/env` in `qris-panel.tsx`, keeping `apiOrigin` — the QR image path still needs it.

**Checkpoint**: `qris-panel.test.tsx` green; the T006 e2e assertion now green. US1 is
independently demonstrable.

---

## Phase 4: User Story 2 — the instructions stop pointing at a label that is gone (Priority: P1)

**Goal**: The "How to pay with QRIS" verification step names the exact amount and no
merchant, and no longer refers to "the merchant name printed above the code".

**Independent Test**: Expand the instructions on the payment page and read every step. No
step names a merchant or refers to a printed merchant name; the amount check states the
actual formatted amount.

### Tests for User Story 2 ⚠️

- [X] T014 [P] [US2] Create `frontend/components/order/qris-instructions.test.tsx` asserting that the rendered steps contain the formatted amount and the stop-before-PIN instruction, and contain neither a configured merchant name nor the phrase "printed above the code". Supply a merchant name through the runtime-config global so the test proves the component ignores it. Confirm it fails against the unmodified component.

### Implementation for User Story 2

- [X] T015 [US2] Rewrite step 4 of `frontend/components/order/qris-instructions.tsx` as an amount-only check (FR-010): name the formatted amount explicitly and keep "If it differs, stop and do not enter your PIN." Delete the `merchantName ? … : "the merchant name printed above the code"` branch outright — that fallback is the sharpest instance of the problem this story fixes ([research.md D-6](./research.md)).
- [X] T016 [US2] Remove the `qrisMerchantName` import and the `const merchantName = …` read from the same file; the component no longer imports from `@/lib/env` at all.
- [X] T017 [US2] Update the component's doc comment, which currently states that naming the merchant "is the whole point" and that "'does this match?' is answerable". Replace it with the amount-check rationale and record plainly that the merchant comparison was removed by decision, so a future reader does not restore it as an apparent oversight.

**Checkpoint**: US1 and US2 both green. The guest-facing change is complete and
demonstrable; the configuration is now dead but still present.

---

## Phase 5: User Story 3 — deployments no longer carry QRIS frame configuration (Priority: P2)

**Goal**: The five values are gone from the type, the accessors, the deployment templates
and the operator documentation, and a deployment that still sets them starts normally and
ignores them.

**Independent Test**: Start with none of the five set — the payment page renders
identically. Search the deployment templates and operator docs — no reference remains.

**⚠️ Depends on US1 and US2.** Deleting these accessors while `qris-panel.tsx` or
`qris-instructions.tsx` still import them is a compile error. This is a real ordering
constraint, not a preference.

### Tests for User Story 3 ⚠️

- [X] T018 [US3] Re-express the three precedence cases in `frontend/lib/runtime-config.test.ts` on `API_BASE_URL`, which exercises every path: both names set → runtime wins; only `NEXT_PUBLIC_API_BASE_URL` set → build-time value; `API_BASE_URL=""` with the prefixed name set → empty is unset; neither set → `FALLBACK_API_BASE_URL`. **Do not simply delete the QRIS-based cases** — they are currently the only coverage of the runtime-over-build-time mechanism, which outlives this feature ([research.md D-5](./research.md)). Also trim `RUNTIME_NAMES` and the five imports.

### Implementation for User Story 3

- [X] T019 [US3] Remove the five fields from the `RuntimeConfig` type and the five `read(…)` lines from `runtimeConfigFromEnv()` in `frontend/lib/runtime-config.ts`. Rewrite the module doc-comment paragraph that uses the QRIS identity as its worked example for why `NEXT_PUBLIC_*` welds an image to one environment — keep the argument, change the example to `API_BASE_URL`. Leave `apiBaseUrl`'s role as the "the server really did inject this" marker in `runtimeConfig()` intact.
- [X] T020 [US3] Delete `qrisMerchantName`, `qrisMerchantId`, `qrisTerminalLabel`, `qrisAcquirerCode` and `qrisPrintVersion` from `frontend/lib/env.ts`, along with their two doc blocks.
- [X] T021 [P] [US3] Remove the five entries and their two comment blocks from `frontend/.env.example`.
- [X] T022 [P] [US3] Remove the five `QRIS_*` service-env entries from `docker-compose.yml` (~L144–148) and the comment above them about unset rendering "an empty frame rather than a wrong merchant name" — that advice describes a check that no longer exists.
- [X] T023 [P] [US3] Remove the two QRIS rows from the per-environment configuration table in `README.md` (~L130–131) and the following sentence explaining that the QRIS fields are left unset on purpose.
- [X] T024 [US3] Run `cd frontend && export PATH="$HOME/.nvm/versions/node/v26.5.1/bin:$PATH" && ./node_modules/.bin/next build`. This is the completeness check on the removal: five deleted exports mean any missed import is a compile error, not a silent survivor.

**Checkpoint**: All three stories complete. `RuntimeConfig` is a one-field type and the
rig still sets all five, proving FR-013.

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: Governance obligations, full verification, and the visual check no automated
test performs.

### Governance — required in the same commit (Constitution Governance + AGENTS.md)

- [X] T025 [P] Amend `specs/012-manjo-payment-gateway/spec.md`: mark **FR-021a** superseded by spec 019, **FR-021b** retired (with nothing displayed there is no fixed label that can disagree with the code, so the release-time re-verification step and its edge case go), **FR-021c** narrowed to the amount. Update the Clarifications entry, US-scenarios 1 and 4, and withdraw the deferred item "derive the QRIS frame's merchant identity from the code" — there is no longer a displayed identity to derive. Each edit carries a pointer to spec 019.
- [X] T026 [P] Annotate `specs/012-manjo-payment-gateway/research.md`: the frame-identity decision (~L290) and the frame-identity row of the verification table (~L332) are superseded by spec 019. Annotate rather than delete — the record of why the values were once configured, and of the mismatch that release step actually caught, is worth keeping.
- [X] T027 [P] Update `specs/012-manjo-payment-gateway/quickstart.md`: the merchant-name verification paragraph (~L309) and the payload-tag → environment-variable table (~L325–327). The payload tags (59, 26→01, 62→07) remain true of the payload; what changes is that nothing renders them.

### Full verification

- [X] T028 Run the frontend tier: `cd frontend && export PATH="$HOME/.nvm/versions/node/v26.5.1/bin:$PATH" && ./node_modules/.bin/vitest run && ./node_modules/.bin/eslint app components lib`.
- [X] T029 [P] Run the Go tier: `cd backend && ./scripts/test.sh ./...`. Expected untouched and green — a failure here means something leaked outside the intended scope.
- [X] T030 Grep-verify FR-012 across the tree: no `QRIS_MERCHANT_NAME`, `QRIS_MERCHANT_ID`, `QRIS_TERMINAL_LABEL`, `QRIS_ACQUIRER_CODE`, `QRIS_PRINT_VERSION`, `qrisMerchantName`, `qrisMerchantId`, `qrisTerminalLabel`, `qrisAcquirerCode` or `qrisPrintVersion` outside `e2e/playwright.config.ts` (deliberate, T001) and the annotated spec-012 documents. Exclude `.next/` build output and `node_modules/`.
- [X] T031 Note for the implementer's own machine: `frontend/.env` is gitignored and still sets the five `NEXT_PUBLIC_QRIS_*` values. It cannot be committed and is now inert, but clear the entries locally so a future reader is not misled into thinking they still do something.

### End-to-end acceptance (Constitution Principle VIII) — NOT optional

- [X] T032 Run the full suite green: `cd e2e && npm test`. The assertion that was red at T006 must now pass, with the rig still exporting all five values.
- [X] T033 Run it again with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test`.
- [X] T034 Run it again with throttling off: `cd e2e && E2E_RATE_LIMIT_ENABLED=false npm test`.

### The check no test performs

- [X] T035 Walk [quickstart.md](./quickstart.md) step 6 against the running app: stacking order (QRIS/GPN marks above the heading), no conspicuous gap, the three wedge checkpoints at 522 px frame width, step captions still on the red wedge at the narrowest supported viewport, and a real scan of the code confirming FR-008. Then exercise an ended order and confirm the frame survives with the total reading "Order total" and none of the five fields back.

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: no dependencies.
- **Foundational (Phase 2)**: needs T001. **Blocks everything** — T006 is the gate.
- **US1 (Phase 3)** and **US2 (Phase 4)**: both start after Phase 2; independent of each
  other and can run in parallel.
- **US3 (Phase 5)**: **requires US1 and US2 complete.** T019/T020 delete accessors that
  T013 and T016 remove the last callers of. Attempting US3 first is a compile error.
- **Polish (Phase 6)**: after all three stories.

### Within Each User Story

- Tests first, seen failing, then implementation. T007 before T008–T013; T014 before
  T015–T017; T018 alongside T019–T020 (the test edit and the type edit are one change —
  the test will not compile against the old type either way).

### Parallel Opportunities

- **Phase 1**: T002 and T003 in parallel after T001.
- **Phases 3 and 4**: entire stories in parallel — `qris-panel.tsx` and
  `qris-instructions.tsx` are different files with no shared symbol once T013 and T016
  land.
- **Phase 5**: T021, T022, T023 in parallel (three unrelated files) once T019/T020 are in.
- **Phase 6**: T025, T026, T027 in parallel; T029 in parallel with anything.
- **Not parallel**: T006 (a gate), T024 (needs every source edit), T032–T034 (sequential
  full-suite runs), T030 and T035 (whole-tree checks).

---

## Parallel Example: User Stories 1 and 2

```bash
# After the Phase 2 gate, two independent tracks:
Track A (US1): T007 → T008 → T009 → T010 → T011 → T012 → T013   # qris-panel.tsx
Track B (US2): T014 → T015 → T016 → T017                        # qris-instructions.tsx

# Then, and only then:
Track C (US3): T018 → T019 → T020 → (T021 ‖ T022 ‖ T023) → T024
```

---

## Implementation Strategy

### MVP scope

**US1 alone is a coherent, shippable increment** — the frame matches the design and the
five fields are gone from the payment surface.

But do not ship it alone. US2 is also P1 for a reason: US1 without US2 leaves a live
instruction telling guests to compare against "the merchant name printed above the code"
on a card that no longer prints one. That is a payment screen actively misdirecting the
one safety check it offers. **The real MVP is US1 + US2.**

US3 is genuinely deferrable — dead configuration is untidy, not harmful — but it is small,
it is already sequenced, and leaving it undone means `docker-compose.yml` keeps advising
operators about a check that no longer exists.

### Incremental delivery

1. Phase 1 → Phase 2. **Stop at T006.** If it is green, the rest of the plan is theatre.
2. Phases 3 and 4 in parallel → validate → this is the shippable increment.
3. Phase 5 → `next build` proves the removal is complete.
4. Phase 6 → governance in the same commit, then the three suite runs and the visual walk.

### One thing to keep in view

This change **removes a payment safety affordance** by explicit decision: the merchant
comparison was the guest's only in-page defence against a swapped code. T015 and T017 are
where that weight transfers to the amount check, which is why T015 requires the exact
formatted amount in the sentence rather than a generic instruction, and why T017 asks for
the removal to be *recorded* in the component rather than merely performed. A future
reader who finds an amount-only check and no explanation will reasonably assume the
merchant check was lost by accident and restore it.

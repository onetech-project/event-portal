---
description: "Task list for 019-buyer-form-expiry-redirect"
---

# Tasks: Bundle Form Labels & End-of-Journey Destination

**Input**: Design documents from `/specs/019-buyer-form-expiry-redirect/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md),
[data-model.md](./data-model.md), [contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: Test tasks are included and are **not** optional here. The spec's Assumptions
require coverage to land in the same change, and Constitution Principle VIII makes `e2e/`
an acceptance gate. This is a *behaviour change*, not a bugfix, so "red first" takes its
behaviour-change form: the existing assertions pass for the behaviour being replaced, so
each story retargets its assertions to the new behaviour and confirms them failing **before**
the source moves.

**Organization**: grouped by user story. The two stories share no source file, so either can
ship alone.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: can run in parallel — different files, no dependency on an incomplete task
- **[Story]**: `[US1]` = end-of-journey destination (P1), `[US2]` = bundle visitor number (P2)

## Path Conventions

Web application, per [plan.md](./plan.md): Go API in `backend/` (**untouched by this
feature**), Next.js app in `frontend/`, Playwright acceptance suite in `e2e/`.

> **Toolchain note, needed by almost every task below.** `node` is not on `PATH` and `npx`
> is intercepted. Prefix every frontend command with:
> `export PATH="$HOME/.nvm/versions/node/v26.5.1/bin:$PATH"`
> `frontend/package.json` has no `test` or `typecheck` script — invoke the local bins
> directly. The acceptance suite runs from `e2e/`, not through a `frontend/` script.

> **⚠️ Cross-story file conflicts.** Three files are edited by both stories or by several
> tasks, so those tasks are **not** parallel with each other even where they sit in
> different phases:
> - `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` — T003 (US1) and T016 (US2)
> - `e2e/specs/guest-purchase.spec.ts` — T012, T013 (US1) and T021 (US2)
> Run them sequentially, or let one story land completely before starting the other.

---

## Phase 1: Setup (Baseline Capture)

**Purpose**: establish what "green" means *before* any edit, so a later failure is
attributable. Principle VIII does not accept "unrelated failure" as an exemption, which is
only enforceable if the baseline was recorded.

- [X] T001 [P] Capture the frontend baseline on the unchanged tree: run `./node_modules/.bin/vitest run` and `./node_modules/.bin/next build` from `frontend/` and record that both are green (all six assertions listed in [plan.md](./plan.md) currently PASS — that is expected and is the point of this step)
- [X] T002 [P] Capture the acceptance baseline on the unchanged tree: `REDIS_PORT=6380 docker compose up -d postgres redis mailpit && docker compose run --rm migrate up` from the repo root, then `cd e2e && npm test`; record the result

**Checkpoint**: baseline recorded. Any failure from here is attributable to this feature.

---

## Phase 2: Foundational (Blocking Prerequisites)

**None.** Stated rather than omitted, so it is clear this was decided and not forgotten.

The two stories touch disjoint source files — US1 lives in the dialog, the two order pages,
and `lib/order-routes.ts`; US2 lives in `slot-groups.ts` and `visitor-form.tsx`. There is no
shared model, no migration, no new dependency, and no infrastructure to stand up
([data-model.md](./data-model.md)). Inventing a foundational task here would only add a
false barrier between two independent tracks.

**Checkpoint**: both user stories may begin immediately after Phase 1.

---

## Phase 3: User Story 1 - The dead end leads back to the event (Priority: P1) 🎯 MVP

**Goal**: the end-of-journey dialog keeps every behaviour spec 011 FR-022/FR-023/FR-024 gave
it; its single action leads to `/events/{slug}` and is labelled for that destination.

**Independent Test**: end a held order on each of the two order screens in turn, press the
dialog's single action, and land on that order's event detail page with the released seats
listed as available. Requires nothing from User Story 2.

### Tests for User Story 1 (write first, confirm RED)

- [X] T003 [P] [US1] Retarget the dialog-action assertions in `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx`: line 200 `toHaveAccessibleName(/return to home page/i)` → the new event-page label, and lines 224-226 (`shows a cancelled order the same dialog` test) `href` `"/"` → `"/events/jazz-night-2026"` (the fixture slug, `PENDING.event.slug`). Leave `expect(replace).not.toHaveBeenCalled()` at line 174 untouched — it is FR-005's regression barrier
- [X] T004 [P] [US1] Retarget the dialog-action assertion in `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.test.tsx` line 365 (`offers no close control at all` test): `href` `"/"` → `"/events/jazz-night-2026"`. Then ADD a CANCELLED-variant case to this file asserting the same destination, so FR-006 (both endings) × FR-008 (both screens) is pinned as a full 2×2 rather than three of four corners. Leave `expect(replace).not.toHaveBeenCalled()` at line 274 untouched
- [X] T005 [US1] Confirm RED: `cd frontend && ./node_modules/.bin/vitest run "app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx" "app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.test.tsx"` — expect failures on the retargeted label and the two/three retargeted `href` assertions. Do NOT proceed until seen failing

### Implementation for User Story 1

- [X] T006 [US1] Add `export function eventDetailPath(eventSlug: string): string` returning `/events/${encodeURIComponent(eventSlug)}` to `frontend/lib/order-routes.ts`, alongside its three existing siblings, with a doc comment stating why the event path belongs in the module that owns every order address (see [contracts/README.md](./contracts/README.md))
- [X] T007 [US1] Change `frontend/components/order/end-of-journey-dialog.tsx`: add a **required** `eventSlug: string` prop, point the single `<Link>` at `eventDetailPath(eventSlug)` instead of `"/"`, relabel it `Return to Event Page`, and update the component's doc comment at line 38 which currently names the `"Return to Home Page"` button as the accessibility escape hatch. Keep it a `Link` — no `router` call (FR-005). Keep exactly one action (FR-002, FR-009)
- [X] T008 [P] [US1] Pass `eventSlug={eventSlug}` to `<EndOfJourneyDialog>` at `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx:124`
- [X] T009 [P] [US1] Pass `eventSlug={eventSlug}` to `<EndOfJourneyDialog>` at `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.tsx:243`
- [X] T010 [US1] Confirm GREEN and confirm nothing loosened: re-run `cd frontend && ./node_modules/.bin/vitest run "app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx" "app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.test.tsx"`; verify the retargeted assertions now pass AND that both `expect(replace).not.toHaveBeenCalled()` guards still exist and still pass — a green suite with either weakened would mean an automatic redirect crept in (research R5)
- [X] T011 [US1] Run `cd frontend && ./node_modules/.bin/next build` clean. Because `eventSlug` is required, this is the proof that no `EndOfJourneyDialog` call site was missed — a third caller would be a compile error, not a silent fallback

### Acceptance coverage for User Story 1 (Principle VIII — not optional)

- [X] T012 [US1] Extend the existing `an expired payment returns the seats to the pool` scenario at `e2e/specs/guest-purchase.spec.ts:257`. It already arranges a real expiry via the signed gateway notification (`expireOrder`) and asserts only quota restoration — it never looks at the screen. Add: the dialog is visible, it contains exactly one link, that link's accessible name names the event page, and clicking it navigates to `/events/${event.slug}`
- [X] T013 [US1] Add a holder-forms hold-expiry scenario to `e2e/specs/guest-purchase.spec.ts` (suggested title: `an expired hold sends the guest back to the event from the holder forms`). Book, stay on the forms without calling `payWithQris()`, let the booking hold lapse, then assert the same four things as T012. **The wait MUST derive from the scaled `BOOKING_HOLD`** — prefer waiting on the dialog appearing with a timeout derived from that scaling over sleeping a fixed 30s; `E2E_SLOW_MO` grows the hold deliberately, so a hardcoded wait passes headless and hangs under `test:slow` (research R3). This is the only Track A surface T012 cannot reach, because its order has already started payment
- [X] T014 [US1] Run just these two scenarios green: `cd e2e && npm test -- --grep "expired payment|expired hold"`

**Checkpoint**: User Story 1 is complete and independently shippable. The dialog is unchanged
in every respect except where its one action leads and what it says.

---

## Phase 4: User Story 2 - Bundle holder cards carry no visitor number (Priority: P2)

**Goal**: no per-unit visitor number appears on any holder card — drawn, revealed in the
truncation tooltip, or announced — while card count, card order, titles, badges, and what is
submitted stay exactly as they are.

**Independent Test**: open the holder forms for an order containing two units of one bundle,
confirm two cards with no visitor number on either, then complete the order and confirm both
units' tickets carry the details typed. Requires nothing from User Story 1.

### Tests for User Story 2 (write first, confirm RED)

- [X] T015 [P] [US2] Update `frontend/components/order/slot-groups.test.ts`: change the assertion at line 125 (`groups.map((g) => g.unitLabel)).toEqual(["Visitor 1", "Visitor 2"])`) to pin what survives instead — two groups, correct `slotIds` split, both titled with the bundle name, and distinct `packageUnit` values (1 and 2), since `packageUnit` is retained and is what actually separates the units. DELETE the `leaves the unit label off when a bundle has only one purchased unit` test at lines 128-146: a field that no longer exists has no null case to assert
- [X] T016 [P] [US2] Invert the visitor-label assertions in `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` lines 943-944: `getByText("Visitor 1")` / `getByText("Visitor 2")` → assert both are absent (`queryByText(/^Visitor \d+$/)` finds nothing). In the same test, ADD assertions that the two cards still render with the bundle's name and their ticket-count badge (FR-012) and that there are still exactly two holder forms (FR-014) — otherwise this test would pass if the cards vanished entirely. Update the `// told apart by their visitor labels` comment above it, which will otherwise describe the opposite of what the test asserts
- [X] T017 [US2] Confirm RED: `cd frontend && ./node_modules/.bin/vitest run components/order/slot-groups.test.ts "app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx"` — expect failures on the inverted visitor-label assertions and the retargeted `slot-groups` expectation. Do NOT proceed until seen failing

### Implementation for User Story 2

- [X] T018 [US2] In `frontend/components/order/slot-groups.ts`: delete the `unitLabel` field from the `SlotGroup` type (line 29) and its two initialisers (lines 61, 83), and delete the whole second pass at lines 87-95 together with the `unitsPerPackage` map (lines 41, 65-70) that exists only to feed it. Leave the first pass — the grouping by `(package_id, package_unit)` — untouched: that is what FR-014 protects
- [X] T019 [US2] In `GroupTitle` in `frontend/components/order/visitor-form.tsx`: drop `group.unitLabel` from the `fullTitle` join (line 399) and delete the rendered `<span>` at lines 425-428. Keep `useIsTruncated(fullTitle)` and the tooltip — the heading can still be clipped, it just has one fewer part. Update the function's doc comment, which currently explains that a clipped heading hides "the unit label and the badge that trail behind it"
- [X] T020 [US2] Confirm GREEN and prove the survey was complete: re-run `cd frontend && ./node_modules/.bin/vitest run components/order/slot-groups.test.ts "app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx"`, then `cd frontend && ./node_modules/.bin/next build`. The build is load-bearing here — every remaining reader of `unitLabel` becomes a compile error, so a clean build is the proof that research R1's "exactly two consumers" finding held

### Acceptance coverage for User Story 2 (Principle VIII — not optional)

- [X] T021 [US2] Add a two-unit bundle scenario to `e2e/specs/guest-purchase.spec.ts` (suggested title: `a bundle bought twice shows two unnumbered holder cards`). Build an event with two ticket types and a package over both (`createSellableEvent`, `createTicketType`, `createPackage` — the pattern at line 383 already does this), then `selectQuantity(bundleName, 2)` and book. Assert two holder cards, the bundle name on both, no text matching `/^Visitor \d+$/` anywhere, then fill both with `fillHolder(0, …)` / `fillHolder(1, …)` and complete through `payWithQris()` so the one-card-fills-a-whole-unit fan-out is proven against the real API rather than a stubbed `fetch`. No scenario books a package with quantity > 1 today, so this shape has never been assembled in a real browser (research R4)
- [X] T022 [US2] Run just that scenario green: `cd e2e && npm test -- --grep "bundle bought twice"`

### Heading layout, added mid-implementation (spec FR-015, FR-016)

> Requested by the user after Phase 4 was under way, and recorded as tasks rather than
> folded in silently. Same heading, independent of the visitor-number removal.

- [X] T030 [US2] Replace `GroupTitle` in `frontend/components/order/visitor-form.tsx` with a wrapping heading: `flex flex-wrap` on the `CardTitle`, the name in a `min-w-0 break-words` span, and `ml-auto shrink-0` on the badge so it holds the right edge whether it shares the line or drops below. Remove the `Tooltip`/`TooltipTrigger`/`TooltipContent` wrapper — with nothing hidden it would only shadow text already on screen
- [X] T031 [US2] Delete the now-dead `useIsTruncated` hook from `frontend/components/order/visitor-form.tsx` and drop the imports it was the only user of (`Tooltip*`, `useState`, `useEffect`)
- [X] T032 [US2] Replace the two truncation tests in `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` — and the `stubCardTitleWidths` helper they needed — with coverage for the new behaviour: the whole heading renders, no `truncate` class, no tooltip on hover, and the badge carries `ml-auto shrink-0`
- [X] T033 [US2] Add `flex-1` to the `CardTitle` in `frontend/components/order/visitor-form.tsx` so the heading fills the width its header leaves it, and pin that class in the heading test. Found by measuring the real browser rather than by reasoning: without it the title box is only as wide as its text, so `ml-auto` pinned the badge to the end of the NAME — landing at x=488 on the first card and x=658 on the second, never at the card's edge (x=804). The first card correctly still stops short, because the delivery notice occupies that space

**Checkpoint**: both user stories are independently functional.

---

## Phase 5: Polish & Cross-Cutting Concerns

**Purpose**: the whole-system gates and the documentation that describes what changed. The
per-story acceptance scenarios were written inside their phases (above) rather than here, so
each story is independently *verified*, not merely independently *coded* — a stricter
placement than the template's, not a looser one. What remains here are the runs and checks
that span both stories.

- [X] T023 Run the full acceptance suite green: `cd e2e && npm test`. Compare against the T002 baseline — a failure outside the three touched scenarios is either a real regression or a broken test, and Principle VIII admits no third option
- [X] T024 Run the suite with the cache off: `cd e2e && E2E_CACHE_ENABLED=false npm test`. Expected to be uneventful — nothing in this feature reads or writes the cache — but an unexercised kill switch is an unverified one
- [X] T025 Run the suite with throttling off: `cd e2e && E2E_RATE_LIMIT_ENABLED=false npm test`. Also confirm the **enabled** run of T023 showed no throttle refusal: the three touched scenarios book three additional orders against the shipped per-IP `Book` allowance that every guest scenario shares (Principle IX check carried from [plan.md](./plan.md))
- [X] T026 [P] Lint clean: `cd frontend && ./node_modules/.bin/eslint app components lib`
- [X] T027 [P] Update the `guest-purchase.spec.ts` coverage row in `e2e/README.md` line 15: it currently says "hold expiry returning quota", which no longer describes everything that scenario group asserts — add the ended-order dialog destination and the multi-unit bundle holder form
- [X] T028 [P] *Optional tidy-up, safe to drop:* fold the two inline `/events/${eventSlug}` error-state links onto `eventDetailPath` — `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.tsx:82` and `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.tsx:114`. It removes exactly the hand-built-path drift `lib/order-routes.ts` exists to prevent, and it belongs to neither story
- [X] T029 Walk the six-step checklist in [quickstart.md](./quickstart.md). Steps 1-5 are covered by the acceptance scenarios added in T012/T013/T021 and the dismissal tests in the two page suites. Step 6's *visual* half — which no jsdom assertion can reach, since happy-dom measures every element as zero — was verified by driving the real browser and measuring the rendered heading: it found FR-016 was not actually holding and produced the `flex-1` fix (T033)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 (Setup)**: no dependencies — start immediately
- **Phase 2 (Foundational)**: empty; blocks nothing
- **Phase 3 (US1)** and **Phase 4 (US2)**: both depend only on Phase 1. Independent of each other in *source*, but see the file-conflict warning at the top — they share two test files
- **Phase 5 (Polish)**: depends on whichever stories are being shipped

### User Story Dependencies

- **US1 (P1)**: no dependency on US2. Shippable alone.
- **US2 (P2)**: no dependency on US1. Shippable alone.

### Within Each User Story

Tests retargeted and confirmed RED → source changed → GREEN + typecheck → acceptance
scenario → scenario green. The RED gate (T005, T017) is a hard stop, not a formality: these
assertions currently pass for the behaviour being replaced, so skipping the gate would leave
no evidence the new assertion tests anything.

### Parallel Opportunities

- T001 ‖ T002 (different tiers, no shared file)
- T003 ‖ T004 (different test files)
- T008 ‖ T009 (different page files; both need T007 first)
- T015 ‖ T016 (different test files)
- T026 ‖ T027 ‖ T028 (lint, docs, and an unrelated tidy-up)
- **NOT parallel**: T012, T013, T021 all edit `e2e/specs/guest-purchase.spec.ts`. T003 and T016 both edit `page.test.tsx`

---

## Parallel Example: User Story 1

```bash
# The two red-first test edits touch different files — do them together:
Task: "T003 Retarget dialog-action assertions in .../orders/[orderNumber]/page.test.tsx"
Task: "T004 Retarget dialog-action assertion + add CANCELLED case in .../checkout/page.test.tsx"

# After T007 lands the required prop, both call sites can be updated together:
Task: "T008 Pass eventSlug at .../orders/[orderNumber]/page.tsx:124"
Task: "T009 Pass eventSlug at .../orders/[orderNumber]/checkout/page.tsx:243"
```

---

## Implementation Strategy

### MVP First (User Story 1 only)

1. Phase 1 (T001-T002) — baseline
2. Phase 3 (T003-T014) — the destination change, with both acceptance scenarios
3. **STOP and VALIDATE**: end a held order on each screen, press the action, land on the
   event page
4. Phase 5's suite runs (T023-T025) — ship

US1 alone is a coherent release: it fixes the dead end that every abandoned purchase reaches,
and it is the half of this feature with a user-visible cost today.

### Incremental Delivery

1. Baseline → US1 → validate → ship (MVP)
2. US2 → validate → ship
3. Polish runs and docs after whichever set is shipping

### Parallel Team Strategy

Two developers can take one story each after T001-T002, provided they coordinate on the two
shared test files (`page.test.tsx`, `guest-purchase.spec.ts`). Given the total size — six
source files, one of them a three-line addition — sequential delivery by one person is
likely faster than the coordination overhead.

---

## Notes

- `[P]` = different files, no dependency on an incomplete task
- The `backend/`, `migrations/`, `SCHEMA.md`, and `api/openapi.yml` surfaces are **not
  touched** by any task here; if a task seems to need one, the design has drifted from
  [plan.md](./plan.md)
- Two assertions must survive this feature unchanged and passing —
  `expect(replace).not.toHaveBeenCalled()` in `page.test.tsx:174` and
  `checkout/page.test.tsx:274`. They are what stops an automatic redirect being reintroduced
  under a green suite
- Commit after each task or logical group; either checkpoint is a valid stopping point

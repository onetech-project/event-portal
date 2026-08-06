# Tasks: Admin Visual Icon Picker

**Input**: Design documents from `/specs/009-admin-icon-picker/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/content-icon-contract.md, quickstart.md

**Tests**: Included — the plan (R7) and contract define a mandatory test strategy (catalog-sync drift protection, 400004 contract, fallback rendering).

**Organization**: Grouped by user story; the catalog contract work is foundational because the picker offers the full 2,007-key catalog by default, so backend agreement (FR-006) must exist before any story ships.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies)
- **[Story]**: US1 (pick visually), US2 (search broad catalog), US3 (glyphs in admin lists)

## Phase 1: Setup (vendored picker component)

**Purpose**: Get the user-designated shadcn-iconpicker into the repo, adapted to the base-ui kit.

- [X] T001 Install the reference picker from its registry: from `frontend/`, run `npx shadcn@latest add "https://icon-picker.alan-courtois.fr/r/icon-picker"`; verify it created `frontend/components/ui/icon-picker.tsx` + `frontend/components/ui/icons-data.ts` and added `@tanstack/react-virtual`, `fuse.js`, `usehooks-ts` to `frontend/package.json` (`lucide-react` already present)
- [X] T002 Add the two missing registry deps in the project's `base-nova` style: from `frontend/`, run `npx shadcn@latest add tooltip skeleton`; verify `frontend/components/ui/tooltip.tsx` and `frontend/components/ui/skeleton.tsx` exist and are `@base-ui/react`-based like the rest of the kit
- [X] T003 Reconcile the vendored `frontend/components/ui/icon-picker.tsx` with this repo (research R1): imports resolve against the base-ui `popover`/`tooltip` APIs (no Radix-only props like `asChild`/`sideOffset` left unadapted); per R1, drop tooltips in favor of accessible names if they fight the virtualized grid; per `frontend/AGENTS.md`, read the relevant pages under `frontend/node_modules/next/dist/docs/` before editing (Next 16.2.12 differs from training data)

---

## Phase 2: Foundational (catalog contract — BLOCKS all user stories)

**Purpose**: One generated catalog consumed by both sides so everything the picker offers is accepted (FR-006) and legacy data stays valid (FR-007). See contracts/content-icon-contract.md §3.

**⚠️ CRITICAL**: No user story work can begin until this phase is complete — the picker offers 2,007 keys while the backend still accepts 20.

- [X] T004 Create `frontend/scripts/generate-icon-keys.mjs`: write `Object.keys(dynamicIconImports)` from `lucide-react/dynamic`, sorted, one per line, LF, to `backend/internal/event/iconkeys/icon_keys.txt` (format per contract §3); include a header comment stating when to re-run (any lucide-react version change)
- [X] T005 Run the generator and check in `backend/internal/event/iconkeys/icon_keys.txt` (expect ~2,007 lines; spot-check it contains all 20 legacy keys listed in contract §3)
- [X] T006 Create `backend/internal/event/iconkeys/iconkeys.go`: `//go:embed icon_keys.txt`, parse once at init into a set, expose `Valid(key string) bool` (and the set size for tests); doc comment explains the generated-file contract
- [X] T007 Modify `backend/internal/event/content.go`: delete the hardcoded `contentIcons` map, point `validateIcon` at `iconkeys.Valid`; semantics unchanged — nil/empty accepted, unknown key → `apperr.CodeUnknownIcon` (400004)
- [X] T008 [P] Extend `backend/internal/event/content_test.go`: legacy key (`music`) accepted; non-legacy catalog key (`anchor`) accepted; existing `TestUnknownIconKeyIsRefusedWith400004` still green; catalog sanity — embedded set ≥ 1,000 keys and contains all 20 legacy keys
- [X] T009 [P] Create the catalog-sync drift test `frontend/scripts/generate-icon-keys.test.ts` (vitest, node fs): lines of `backend/internal/event/iconkeys/icon_keys.txt` deep-equal `Object.keys(dynamicIconImports).sort()` — fails CI when lucide-react is upgraded without regenerating

**Checkpoint**: `cd backend && go test ./internal/event/...` and `cd frontend && npx vitest run scripts/` green — backend now accepts the full catalog.

---

## Phase 3: User Story 1 - Pick an icon visually (Priority: P1) 🎯 MVP

**Goal**: Activities and guidelines forms use a visual glyph picker with an explicit "no icon"/clear affordance; saved wire shape unchanged (`""` → `null`).

**Independent Test**: quickstart.md §US1 — open Add-activity, see glyph grid, pick one, save, glyph persists; clear works; editing a pre-feature block preselects its icon.

- [X] T010 [US1] Create `frontend/components/admin/icon-field.tsx`: `Field`-labeled wrapper around the picker loaded via `next/dynamic` (research R5 — `icons-data.ts`/`fuse.js` must not enter the admin page's initial bundle); trigger shows selected glyph + name or "No icon"; renders a clear button with accessible name "Remove icon" when a value is set (FR-004); props `{ value: string; onChange: (icon: string) => void; label: string }` matching today's `IconSelect`
- [X] T011 [US1] Replace `IconSelect` with `IconField` in `frontend/components/admin/content-block-form.tsx` (activities form + guidelines form); keep local state `""` = none and submit mapping `"" → null` exactly as today; delete the now-unused `IconSelect`
- [X] T012 [US1] Accessibility pass on `frontend/components/ui/icon-picker.tsx` per research R6 (FR-010): every grid option is a focusable `button` whose accessible name is the icon name; search input labeled; open → search → select → close operable by keyboard; fix in place (file is project-owned)
- [X] T013 [P] [US1] Create `frontend/components/admin/icon-field.test.tsx` (vitest + testing-library, picker mocked per R7): selecting sets the value; clear button appears only when set and resets to `""`; trigger reflects the chosen icon name
- [X] T014 [US1] Extend the admin form tests in `frontend/components/admin/content-block-form.test.tsx` (create if absent): submitting with no icon sends `icon: null`; submitting with a picked key sends that key verbatim

**Checkpoint**: US1 fully functional — run quickstart.md §US1 manually; `npx vitest run components/admin` green.

---

## Phase 4: User Story 2 - Search a broad icon catalog (Priority: P2)

**Goal**: Search-as-you-type over the full catalog with a no-match empty state; non-legacy icons round-trip to the guest page, which renders any catalog key.

**Independent Test**: quickstart.md §US2 — type `anch`, pick `anchor` (outside the legacy 20), save without 400004, see the glyph on the public event page; gibberish shows "no icons match".

- [X] T015 [US2] Rewrite `frontend/components/event/content-icon.tsx` per research R3: drop the fixed 20-entry `ICONS` map; if `name in dynamicIconImports` render `DynamicIcon` from `lucide-react/dynamic`, else statically render the existing `Info` fallback (FR-008 — no console noise for stale keys); keep the exact component API (`name: string | null`, `className`, `aria-hidden`, null for empty)
- [X] T016 [US2] Verify search behavior in `frontend/components/ui/icon-picker.tsx`: debounced filter narrows as you type, clearing restores the full catalog, and an explicit "No icons match" empty state exists (add it if the vendored version lacks one)
- [X] T017 [P] [US2] Create `frontend/components/event/content-icon.test.tsx`: known catalog key renders an svg; unknown key renders the Info fallback; `null`/`""` renders nothing
- [X] T018 [P] [US2] Create `frontend/components/ui/icon-picker.test.tsx` with a `ResizeObserver` stub (R7 risk mitigation): open picker → type a term → matching option appears → select fires `onValueChange`; gibberish term shows the empty state
- [X] T019 [US2] Update `frontend/lib/types.ts` per data-model.md: `CONTENT_ICON_KEYS` loses its whole-catalog role — keep it only as the legacy-20 fixture referenced by tests, or delete it if nothing references it after T011/T015

**Checkpoint**: US1 + US2 work; non-legacy icon round-trips admin → API → guest page (quickstart §US2).

---

## Phase 5: User Story 3 - Glyphs in the admin block lists (Priority: P3)

**Goal**: Admin activities/guidelines lists show the real glyph instead of the `(music)`-style text suffix; nothing shown for icon-less blocks.

**Independent Test**: quickstart.md §US3 — lists render glyphs, no raw key text, no placeholder when `icon` is null.

- [X] T020 [US3] In `frontend/components/admin/content-block-form.tsx`, replace both `({block.icon})` text suffixes (activities list, guidelines list) with the `ContentIcon` glyph (import from `@/components/event/content-icon`); render nothing when `icon` is null/empty (FR-009)
- [X] T021 [P] [US3] Extend `frontend/components/admin/content-block-form.test.tsx`: a listed block with an icon renders the glyph and not the raw `(key)` text; a block without an icon renders no glyph element

**Checkpoint**: All three stories independently validated per quickstart.md.

---

## Phase 6: Polish & Cross-Cutting

- [X] T022 [P] ~~Remove the unused `react-icons` dependency~~ — SKIPPED after the required grep: `react-icons/fa6` IS used by `frontend/components/layout/site-footer.tsx` and `site-header.tsx` for Font Awesome brand glyphs (WhatsApp etc.) that Lucide doesn't carry; the dependency stays (research R1's premise was wrong)
- [X] T023 Run the full quickstart.md validation — automated portions done: backend `go test ./...` 290 passed (includes the 400004 crafted-request contract), frontend vitest 222 passed in 28 files, lint 0 errors, `next build` clean. REMAINING FOR A HUMAN: the in-browser US1–US3 walkthrough and keyboard-only pass in quickstart.md (cannot be driven headlessly here)
- [X] T024 Performance spot-check per SC-005/FR-011 — verified structurally on the production build: `icons-data`/`fuse.js` are imported only by `icon-picker.tsx`, which is reachable only via `next/dynamic` in `icon-field.tsx`, and the build emitted them as separate lazy chunks outside the page bundles; the picker additionally mounts nothing until the first trigger click (asserted in icon-field.test.tsx). In-browser timing spot-check remains part of the human walkthrough (T023)

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: none — start immediately; T001 → T002 → T003 sequential (T003 edits what T001/T002 install)
- **Foundational (Phase 2)**: independent of Phase 1 (backend + script work; can run in parallel with Setup); within it T004 → T005 → T006 → T007, then T008/T009 in parallel
- **US1 (Phase 3)**: needs Phases 1 + 2 (picker exists and is accepted by backend)
- **US2 (Phase 4)**: needs Phase 2; T015/T017 don't touch the picker and could start any time after Phase 2, T016/T018 need Phase 1's vendored component
- **US3 (Phase 5)**: needs T015 (`ContentIcon` rewrite) and touches `content-block-form.tsx`, so schedule after T011 to avoid same-file conflicts
- **Polish (Phase 6)**: after all desired stories

### Same-file conflict warning

`content-block-form.tsx` is edited by T011 (US1), T020 (US3) and its test file by T014/T021 — do not parallelize those across stories.

### Parallel Opportunities

- Phase 1 (frontend setup) ∥ Phase 2 (backend catalog) — different trees
- T008 ∥ T009 (Go tests vs vitest sync test)
- T013 within US1; T017 ∥ T018 within US2; T021 within US3
- T022 ∥ T023/T024 in Polish

## Parallel Example: after Phase 2 checkpoint

```bash
# One developer / agent each, no file overlap:
Task: "T008 Extend backend/internal/event/content_test.go with legacy/new/sanity icon tests"
Task: "T009 Create frontend/scripts/generate-icon-keys.test.ts catalog-sync test"
# Meanwhile frontend setup continues: T003 reconcile icon-picker.tsx with base-ui kit
```

## Implementation Strategy

**MVP = Phases 1 + 2 + 3 (US1)**: visual picking over the full catalog with clear/no-icon, contract intact — ship/demo after the Phase 3 checkpoint. Note US2's search mostly rides along with the vendored component; the US2 phase is what *verifies* it and makes the guest page render arbitrary keys (T015) — so treat T015 as the first task picked up after MVP. Then US3 (small), then Polish.

## Notes

- Wire contract and DB schema are untouched throughout — no migration tasks exist on purpose (data-model.md).
- Commit T005's generated file together with T004's script so reviewers can re-run and diff.
- Every task above follows `- [ ] Txxx [P?] [Story?] description + explicit file path`.

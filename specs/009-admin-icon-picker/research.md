# Research: Admin Visual Icon Picker

**Feature**: 009-admin-icon-picker | **Date**: 2026-08-05

All decisions below were verified against the installed codebase (not assumed from
documentation): `frontend/package.json`, `frontend/node_modules/lucide-react` v1.28.0,
`frontend/components.json`, `backend/internal/event/content.go`, and the reference
component's live registry payload at `https://icon-picker.alan-courtois.fr/r/icon-picker`.

## R1 — Picker component: adopt the user-designated shadcn-iconpicker

**Decision**: Install [shadcn-iconpicker](https://github.com/alan-crts/shadcn-iconpicker)
through the shadcn registry (`npx shadcn@latest add "https://icon-picker.alan-courtois.fr/r/icon-picker"`)
and treat the installed files as project-owned code (standard shadcn model), adapted where
needed.

**What the registry item brings** (verified from the registry payload):

- Files: `components/ui/icon-picker.tsx` (component) and `components/ui/icons-data.ts`
  (icon name/tag/category metadata).
- npm deps: `@tanstack/react-virtual` (virtualized 5-column grid), `fuse.js` (fuzzy
  search over names/tags/categories, 100 ms debounce), `usehooks-ts`; `lucide-react` is
  already installed.
- shadcn registry deps: `button`, `input`, `popover` (already exist in
  `frontend/components/ui/`), `tooltip`, `skeleton` (missing — must be added).
- Loads glyphs via `lucide-react/dynamic` (`DynamicIcon` + `dynamicIconImports`) and
  filters its metadata to icons present in the installed lucide version — so the offered
  set is always a subset of what the installed library can render.
- Props cover the spec's needs: `value`/`onValueChange`, `open`/`onOpenChange`,
  `searchable`, `categorized`, `iconsList`, custom trigger via `children`, `modal`.

**Compatibility caveat found**: this project's shadcn components are built on
`@base-ui/react` (style `base-nova` per `components.json`), not Radix. The existing
`popover.tsx` exposes the standard `Popover/PopoverTrigger/PopoverContent` API, so the
picker's imports resolve, but the two missing registry deps (`tooltip`, `skeleton`) must
be added in the project's base-ui idiom (via `npx shadcn@latest add tooltip skeleton`
with the project's configured style, or hand-ported to match). Any Radix-specific prop
usage in the installed picker (`sideOffset`, `asChild`, …) must be reconciled against the
base-ui popover's props at implementation time. Tooltips are a nice-to-have here — if
base-ui tooltip integration fights the virtualized grid, drop tooltips and rely on
accessible names (R6) rather than grow the change.

**Alternatives considered**:
- Build a picker from scratch on `popover` + `input` + manual grid — more control, but
  re-implements virtualization, fuzzy search, and category data the reference already
  provides; rejected since the user explicitly designated the reference.
- `react-icons`-based pickers (the package is already installed but unused) — would break
  the existing guest-side rendering contract, which stores **lucide slugs** and renders
  them with lucide-react; mixing collections would force a second identifier namespace.
  Rejected; consider removing the unused `react-icons` dependency during implementation.

## R2 — Catalog source of truth and backend validation

**Decision**: One generated, checked-in catalog file consumed by both sides.

- A small Node script, `frontend/scripts/generate-icon-keys.mjs`, writes the sorted key
  list of `dynamicIconImports` (the installed lucide-react's full dynamic catalog —
  **2,007 names** at v1.28.0) to `backend/internal/event/iconkeys/icon_keys.txt`, one
  key per line.
- The backend replaces the hardcoded 20-entry `contentIcons` map in
  `internal/event/content.go` with a set loaded at init from that file via `go:embed`.
  `validateIcon` behavior is unchanged: nil/empty accepted, unknown key → HTTP 400 with
  `apperr.CodeUnknownIcon` (400004). The existing test
  `TestUnknownIconKeyIsRefusedWith400004` stays green.
- Drift protection (FR-006): a frontend vitest test reads `icon_keys.txt` from disk and
  asserts it equals `Object.keys(dynamicIconImports)` — so upgrading lucide-react without
  regenerating the file fails CI; a Go test asserts the embedded set is large (≥ 1,000)
  and contains all 20 legacy keys (FR-007 — all 20 were verified to be standard lucide
  slugs present in the 2,007).

Set relationships that make FR-006/FR-008 hold: picker offers (icons-data ∩ installed
lucide) ⊆ installed lucide = backend accepts = guest page can render.

**Alternatives considered**:
- Pattern-based validation (accept any kebab-case slug) — rejected: FR-006 explicitly
  requires rejecting identifiers outside the recognized catalog, and today's 400004
  contract is load-bearing (tested, documented).
- Backend serves the catalog over an API the picker consumes — rejected: the picker's
  metadata (tags, categories) lives client-side anyway; an endpoint adds a network
  dependency and DTO surface for no agreement gain over a shared generated file.
- Hand-maintaining a larger curated list — rejected: recreates the drift problem that
  motivated this feature's registry expansion.

## R3 — Rendering arbitrary catalog icons (guest page + admin lists)

**Decision**: Rewrite `frontend/components/event/content-icon.tsx` to drop the fixed
20-entry component map and render by name with `DynamicIcon` from
`lucide-react/dynamic`. Verified from the installed `DynamicIcon.mjs`: it lazy-loads the
icon module per name (per-icon chunks — the 2,007-icon catalog never enters the page
bundle), renders the `fallback` component (or null) while loading or on unknown name, and
logs unknown names via `console.error`. To keep FR-008's graceful degradation and avoid
noisy errors for stale keys, `ContentIcon` first checks `name in dynamicIconImports` and
statically renders the current `Info` fallback when absent — preserving today's
fallback semantics exactly. Admin block lists (FR-009 / US3) reuse this same
`ContentIcon` instead of the current `({key})` text suffix.

**Alternatives considered**: importing the full `icons` map (puts the entire catalog in
the client bundle — rejected on FR-011); keeping a hand-grown static map (defeats the
feature).

## R4 — Admin form integration and the "no icon" affordance

**Decision**: Replace `IconSelect` in
`frontend/components/admin/content-block-form.tsx` with a new
`IconField` wrapper (label via existing `Field`, the picker as trigger showing the
selected glyph + name, and an explicit clear ("no icon") button rendered next to the
trigger whenever a value is set). The reference picker itself has no built-in clear
affordance (FR-004), so the wrapper owns it. Form state keeps today's shape: `""` = no
icon in local state, submitted as `null` — the wire contract is untouched.

## R5 — Performance (FR-011, SC-005)

**Decision**: Load the picker lazily with `next/dynamic` inside `IconField` so
`icons-data.ts` (the largest new client asset) and `fuse.js` are fetched only when an
admin actually opens the content page's icon field, keeping the admin content page's
initial load unchanged. In-picker performance is already handled by the reference:
virtualized rows and 100 ms-debounced fuzzy search. Note: per the frontend AGENTS.md
warning, confirm `next/dynamic` usage against `node_modules/next/dist/docs/` (Next
16.2.12) before implementation.

## R6 — Accessibility (FR-010, SC-006)

**Decision**: Acceptance bar — trigger is a real button labeled "Icon" (via `Field`);
every icon option in the grid is a focusable `button` whose accessible name is the icon
name; search input is labeled; popover open/close and selection are keyboard-operable
(base-ui popover provides focus management). The clear button gets an explicit
accessible name ("Remove icon"). Vitest + testing-library assertions cover these.
If the installed component falls short (e.g., unlabeled buttons), fix it in place —
the files are project-owned after registry install.

## R7 — Testing strategy

- **Backend (`go test`, testify, existing test pool)**: existing 400004 refusal test
  unchanged; add — legacy key accepted, a new catalog key (e.g. `anchor`) accepted,
  embedded catalog sanity (≥ 1,000 keys, contains all 20 legacy keys).
- **Frontend (vitest + testing-library, happy-dom)**: catalog-sync test (R2);
  `ContentIcon` renders a known key and falls back on an unknown key; `IconField`
  interaction — pick sets the key, clear resets to none, submit sends `null` vs key.
  Known environment risk: `@tanstack/react-virtual` needs `ResizeObserver` /ranged
  `getBoundingClientRect`, which happy-dom stubs poorly — mitigate by testing `IconField`
  with the picker mocked, and covering the real picker's open/search path in a focused
  test with a `ResizeObserver` polyfill/stub. Virtualization itself is upstream-tested.

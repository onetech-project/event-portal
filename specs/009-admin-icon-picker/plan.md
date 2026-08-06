# Implementation Plan: Admin Visual Icon Picker

**Branch**: `009-admin-icon-picker` | **Date**: 2026-08-05 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/009-admin-icon-picker/spec.md`

## Summary

Replace the admin CMS's text-only icon `<select>` (20 hardcoded keys) with a visual,
searchable icon picker offering the full installed Lucide catalog (2,007 icons), using
the user-designated [shadcn-iconpicker](https://github.com/alan-crts/shadcn-iconpicker)
registry component adapted to this project's base-ui shadcn kit. The recognized-catalog
validation contract (unknown key → 400004) is preserved by generating the catalog key
list from the installed `lucide-react` into a file the Go backend embeds; guest-page and
admin-list rendering switch from a fixed 20-entry icon map to per-name lazy rendering
via `lucide-react/dynamic`. No database schema or API shape changes.

## Technical Context

**Language/Version**: Frontend — TypeScript 5, Next.js 16.2.12 (App Router), React
19.2.4. Backend — Go 1.24+, Echo v4.

**Primary Dependencies**: `lucide-react` 1.28.0 (installed; ships `lucide-react/dynamic`
with `DynamicIcon` + 2,007-key `dynamicIconImports`), shadcn registry item
`icon-picker` (adds `@tanstack/react-virtual`, `fuse.js`, `usehooks-ts`, plus `tooltip`
and `skeleton` ui components), existing shadcn/base-ui (`@base-ui/react`) ui kit.
Backend: standard library only (`embed`).

**Storage**: PostgreSQL — **no schema change**. `icon` stays a nullable text column on
`event_activities` / `event_guidelines`; only the accepted value set grows.

**Testing**: Frontend — vitest 4 + @testing-library/react (happy-dom). Backend —
`go test` + testify against the existing test pool (`content_test.go` extended).

**Target Platform**: Web; admin pages (desktop-first) + public event page.

**Project Type**: Web application (Next.js frontend + Go modular-monolith backend).

**Performance Goals**: Picker opens < 2 s; search results < 1 s (SC-005); admin content
page initial load unchanged (picker + icon metadata lazy-loaded, FR-011).

**Constraints**: No object storage / uploads (icons are named keys rendered
client-side); server keeps rejecting unknown keys with 400004 (FR-006); existing 20 keys
must remain valid with unchanged rendering (FR-007); keyboard + AT operable (FR-010).

**Scale/Scope**: Catalog 2,007 keys; two admin forms (activities, guidelines), two admin
list surfaces, one guest renderer; ~6 frontend files + 1 backend package touched.

## Constitution Check

*GATE: evaluated against constitution v1.1.1 — PASS (pre-Phase-0 and re-checked
post-Phase-1; no violations, Complexity Tracking empty).*

| Principle | Verdict | Notes |
|-----------|---------|-------|
| I. Modular Monolith | PASS | All backend change stays inside `internal/event` (validation + embedded catalog in a subpackage `internal/event/iconkeys`); no new domains. |
| II. Domain Isolation | PASS | No cross-domain imports or JOINs introduced. |
| III. DTO Isolation | PASS | DTO/request shapes in `content.go` untouched; `icon` remains `*string`. |
| IV. Transactional Integrity & Idempotency | PASS | Booking/checkout/webhook paths untouched. |
| V. Payment Gateway Abstraction | PASS | Not touched. |
| VI. Guest-First MVP Scope Discipline | PASS | Admin UX enhancement; not on the prohibited list; guest flow behavior unchanged; **no uploads/object storage** — catalog is named keys only, `banner_url`-style URL handling unaffected. |
| Tech Stack Requirements | PASS | Additions are frontend UI libraries consistent with the mandated Next.js/TypeScript stack; no schema change so SCHEMA.md needs no update; no new infrastructure. |
| Critical Data Flow Rules | PASS | Quota/ticket/validation flows untouched. |

## Project Structure

### Documentation (this feature)

```text
specs/009-admin-icon-picker/
├── plan.md              # This file
├── research.md          # Phase 0 output (R1–R7)
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── content-icon-contract.md   # Phase 1 output
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
frontend/
├── components/
│   ├── ui/
│   │   ├── icon-picker.tsx        # NEW — installed from shadcn-iconpicker registry, then adapted
│   │   ├── icons-data.ts          # NEW — installed icon metadata (names/tags/categories)
│   │   ├── tooltip.tsx            # NEW — registry dep (base-ui idiom)
│   │   └── skeleton.tsx           # NEW — registry dep
│   ├── admin/
│   │   ├── icon-field.tsx         # NEW — Field wrapper: lazy picker + clear button
│   │   └── content-block-form.tsx # MODIFIED — IconSelect → IconField; lists render glyphs
│   └── event/
│       └── content-icon.tsx       # MODIFIED — fixed 20-icon map → DynamicIcon by name
├── lib/
│   └── types.ts                   # MODIFIED — CONTENT_ICON_KEYS role changes (legacy set for tests)
└── scripts/
    └── generate-icon-keys.mjs     # NEW — dynamicIconImports keys → backend catalog file

backend/
└── internal/event/
    ├── iconkeys/
    │   ├── iconkeys.go            # NEW — go:embed icon_keys.txt, exposes the valid-key set
    │   └── icon_keys.txt          # NEW — generated, checked in (2,007 keys, sorted)
    ├── content.go                 # MODIFIED — contentIcons map → iconkeys set (validateIcon unchanged)
    └── content_test.go            # MODIFIED — legacy/new/unknown key + catalog sanity tests
```

**Structure Decision**: Existing two-part web-app layout (`frontend/` + `backend/`)
is kept; the only structural addition is the `internal/event/iconkeys` subpackage so the
generated file and its `go:embed` loader stay inside the owning domain (Principle I).

## Phase 0 — Research

Complete — see [research.md](research.md). Key decisions: adopt the designated registry
component as project-owned code, adapted to the base-ui kit (R1); one generated
catalog file embedded by Go with drift-protection tests on both sides (R2);
`DynamicIcon` per-name lazy rendering with the existing `Info` fallback preserved (R3);
`IconField` wrapper owns the clear/"no icon" affordance the reference lacks (R4);
`next/dynamic` lazy-load keeps the admin page's initial bundle unchanged (R5);
accessible-name and keyboard acceptance bar with in-place fixes if the vendored
component falls short (R6); test strategy including the happy-dom/virtualization risk
and its mitigation (R7). No NEEDS CLARIFICATION items remain.

## Phase 1 — Design & Contracts

Complete — artifacts:

- [data-model.md](data-model.md) — entities (unchanged storage; Icon Catalog as a
  generated artifact), validation rules, and set relationships that satisfy FR-006/007/008.
- [contracts/content-icon-contract.md](contracts/content-icon-contract.md) — the
  affected admin endpoints' `icon` field contract (unchanged shape, enlarged domain),
  the 400004 error contract, and the catalog-file format contract binding frontend and
  backend.
- [quickstart.md](quickstart.md) — runnable validation: regeneration, both test suites,
  and manual scenarios mapped to US1–US3 plus the crafted-request edge case.

## Risks & Mitigations

1. **Base-ui vs Radix mismatch** in the vendored picker (popover props, `asChild`) —
   reconcile at install time; component is project-owned after `shadcn add`; drop
   tooltips rather than grow the change if they fight the virtualized grid (R1/R6).
2. **Catalog drift on lucide-react upgrades** — vitest sync test fails CI until
   `generate-icon-keys.mjs` is re-run (R2).
3. **happy-dom vs virtualization** in tests — mock the picker for `IconField` tests;
   stub `ResizeObserver` for the one focused picker test (R7).
4. **Next 16 specifics** — per `frontend/AGENTS.md`, verify `next/dynamic` and client
   lazy-loading idioms against `node_modules/next/dist/docs/` before writing code (R5).

## Complexity Tracking

No constitution violations — table intentionally empty.

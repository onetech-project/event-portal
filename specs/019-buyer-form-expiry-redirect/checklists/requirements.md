# Specification Quality Checklist: Bundle Form Labels & End-of-Journey Destination

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-18
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

### Iteration 1 (2026-08-18)

Three [NEEDS CLARIFICATION] markers were raised on the expiry half: whether the guest is
told why they moved, whether cancellation redirects too, and whether an already-ended order
redirects. All three assumed an **automatic** redirect.

### Iteration 2 (2026-08-18) — all items pass

The premise was wrong, and the answer collapsed the scope rather than resolving three
separate questions. There is no automatic redirect: the end-of-journey modal keeps every
behaviour spec 011 FR-022/FR-023/FR-024 mandate, and only the destination and label of its
single action change. The three questions dissolved:

- **Silent redirect vs. explanation** — moot. The modal still carries the explanation, and
  the guest is never moved without pressing its action (FR-005).
- **Cancelled orders** — same destination, because the modal is the same modal (FR-006).
- **Already-ended orders** — same destination, for the same reason (FR-007).

The spec was rewritten rather than patched: US1 now describes a destination change inside a
preserved flow, and FR-001/FR-002 restate the behaviour that must **not** change, so a
reader cannot mistake this for the automatic-redirect feature the first draft described.

Three things were made explicit that the answer implied but did not state:

- **FR-004** — the label must name its destination. A button reading "Return to Home Page"
  that leads to the event page is a defect, so the label moves with the destination. The
  exact wording is recorded as an assumption ("Return to Event Page"), not a constraint.
- **FR-009** — this is not a reinstatement of the "Repeat Order" action spec 011 FR-024
  deleted. That one led to the ticket-selection step; this one leads to the event's landing
  page, and there is still exactly one action.
- **Back-button behaviour** — the action stays an ordinary navigation, so back returns to the
  ended order and its modal. Recorded in Edge Cases and Assumptions as deliberately
  unchanged, since the instruction was to keep the current logic.

The **Amends** note scopes the change against spec 011 FR-024 precisely: destination and
label only; the Figma `293-3` design, the body copy, the single-action rule, and FR-022 and
FR-023 in full all stand.

### Iteration 3 (2026-08-18) — during implementation

Two changes were made to this feature after planning, both recorded rather than absorbed
silently:

- **Renumbered 020 → 019.** The original number avoided a collision with
  `019-frontend-architecture-refactor` on the `refractor/fe` branch, which is not merged
  here. The user's call: this branch's numbering should follow its own history. The cost,
  if both ever merge, is that 019 stops being a unique key — the directories themselves do
  not collide, since the suffixes differ.
- **FR-015 and FR-016 added** — the card heading wraps instead of clipping to one line, and
  the badge holds the right edge with `ml-auto`. Requested mid-implementation. Net removal:
  the single-line heading needed overflow detection, which leaves no trace in the DOM, so
  it had carried a `ResizeObserver`, a font-loading callback, and a tooltip whose only job
  was to hand back what the ellipsis took. All of it is gone.

Both are reflected in [spec.md](../spec.md) and in [tasks.md](../tasks.md) (T030–T032).
The checklist above still passes: the added requirements are testable, bounded, and carry
acceptance scenarios.

### Environment findings (not defects in this feature)

The baseline tasks (T001, T002) exist to separate pre-existing breakage from this change,
and they earned their place twice:

- `frontend/node_modules` was out of sync with its own committed lockfile — `next-themes`
  and `sonner` were declared and pinned but not installed, so `next build` failed its
  typecheck on `components/ui/sonner.tsx`, a committed file nothing imports.
- `e2e/node_modules` did not exist at all, so `npm test` died with
  `playwright: command not found`.

Both were resolved with a plain `npm install` in each directory, installing already-pinned
versions; `package.json` and `package-lock.json` were verified unmodified afterwards in
both places, so no dependency was added and no version drifted.

The acceptance baseline itself recorded **40 passed, 1 failed, 2 skipped**. The failure —
`admin-console.spec.ts:268` — was classified rather than assumed: a re-run passed in 36s,
and the log shows Turbopack taking 23.5s to compile `/admin/events/[id]` on first visit,
consuming most of the 30s navigation timeout. It is a cold-compile flake on a surface this
feature does not touch, and it is recorded here so a later reviewer does not attribute it
to this change.

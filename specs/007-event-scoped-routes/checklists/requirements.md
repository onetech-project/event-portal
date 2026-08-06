# Specification Quality Checklist: Event-Scoped Guest Navigation

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-04
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

### Validation iteration 1 (2026-08-04)

Failures found and corrected before this version:

- **Implementation detail leak**: an early draft named concrete route paths and framework
  concepts (route groups, layout files) in requirements. Rewritten in terms of "shared
  framing", "event's area", and "addressed within their event" so the spec stays
  implementation-agnostic.
- **Unmeasurable success criteria**: replaced render-timing and component-level criteria
  with guest-observable outcomes (SC-001 … SC-006).
- **Missing edge cases**: added event-still-loading, countdown-crossing-zero,
  deep-linked mismatch, and stale-links-in-the-wild.

### Validation iteration 2 (2026-08-04)

Both open questions answered by the user and folded into the spec:

1. **Home-screen ticket lookup entry → option B**. A top-level code-entry *resolver* is
   kept: it determines the owning event from the code and forwards into that event's
   ticket screen. Captured as FR-017/FR-018, with FR-016 narrowed accordingly (the ban is
   on top-level addresses for a *specific* order or ticket, not on the resolver), and
   User Story 4's scenarios and SC-004 rewritten to match.
2. **Countdown on the order/payment screen → option B**. Both countdowns render, each
   explicitly labelled and visually distinguishable. Captured as FR-020 and SC-007; the
   competing-countdowns edge case now states the requirement instead of posing the
   question. Noted in Assumptions that the countdown's current heading is not an adequate
   label and must change.

No `[NEEDS CLARIFICATION]` markers remain. All checklist items pass; the spec is ready
for `/speckit-plan`.

### Validation iteration 3 — `/speckit-clarify` (2026-08-04)

The user re-scoped the feature after reviewing the plan's file tree. Five questions asked
and answered; the spec was rewritten rather than patched, because the change removed two
of the four original user stories.

**Removed**: event-scoped ticket lookup screens, the top-level ticket-code resolver, the
legacy `/orders/:orderNumber` forwarder, and the `event_slug` API field whose only
consumers were the first two. Iteration 2's resolution of "Question 1" is therefore
superseded — root ticket pages stay exactly as they are today.

**Added**: the four-step progress rail moves into the shared framing (it currently renders
on the first screen only, so stages 2–4 were never shown to anyone); a distinct
confirmation screen with a receipt, resend, and back-to-home; automatic forwarding from
any settled order to that screen; and a public rate-limited resend endpoint.

Re-validation against the rewritten spec: **16/16 items passing** — unchanged count, no
regressions. Two points worth recording rather than failing:

- FR-025 requires a resend frequency limit without naming a number. That is deliberate —
  the threshold is a policy decision for the plan, and SC-007 makes the behaviour testable
  either way.
- The Assumptions section now references an approved Figma frame for the confirmation
  screen. This is a source pointer, not a leaked implementation detail; the requirements
  still describe what the screen must convey, not how it looks.

`plan.md`, `research.md`, `data-model.md`, `contracts/`, `quickstart.md`, and `tasks.md`
were all regenerated against this spec in the same session — they described the removed
stories and would otherwise contradict it.

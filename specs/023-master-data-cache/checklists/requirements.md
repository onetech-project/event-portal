# Specification Quality Checklist: Master Data Read Cache

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-24
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

- Items marked incomplete require spec updates before `/speckit-clarify` or `/speckit-plan`.

### Validation record

Two iterations were needed.

**Iteration 1 failed three items.** The first draft named Redis, PostgreSQL, `pkg/cache`,
`ListActiveGenders`, `genderMaps`, `CACHE_TTL` and specific file paths throughout, failing *no
implementation details*, *written for non-technical stakeholders*, and *success criteria are
technology-agnostic*. That naming is accurate — it is where the investigation landed — but it
belongs in `plan.md`, not here. The requirements were rewritten in terms of "the accelerator",
"the database", "the active-only projection" and "the operator refresh", which left every
requirement still testable while removing the vocabulary that presumes the implementation. The
concrete anchors are preserved in the conversation that produced this spec and will be restored
in planning, where they are appropriate.

**Iteration 2 failed one item.** *Scope is clearly bounded* passed only weakly: the fee and order
status exclusions were stated in the Context section as findings, which reads as background
rather than as a rule. Anyone implementing from the requirements alone could have added them in
good faith. They were promoted to FR-003, FR-004 and FR-005 so the exclusion is a requirement
with a recorded reason, and mirrored in Out of Scope.

### Areas needing attention

All items pass, but two things are worth the reader's attention before planning:

1. ~~**FR-008's startup refresh is a design decision this spec makes on the user's behalf.**~~
   **RESOLVED in clarification 2026-08-24.** It was put to the user and confirmed: startup refresh
   is the correctness mechanism. The same session also reversed the no-expiry design it was
   written against, so the standard expiry now sits behind it as a backstop, and FR-008a covers
   the case where the refresh itself cannot run.
2. ~~**FR-020 through FR-022 make this feature gated by governance, not by code.**~~
   **RESOLVED 2026-08-24 — the amendment has landed.** Constitution **v6.1.0** admits the gender
   master list by name (FR-020a satisfied, not as a category), adds service start as the
   invalidation trigger for a surface with no write path (FR-022 satisfied), and syncs
   ARCHITECTURE.md §3.6a and PRD.md §1.2; SCHEMA.md was verified unaffected (FR-021 satisfied).
   FR-020's gate is therefore open and the feature may proceed to planning.

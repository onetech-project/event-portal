# Specification Quality Checklist: Configurable Rate Limits

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

**Iteration 1 (2026-08-18)** — 15/16. Three `[NEEDS CLARIFICATION]` markers remained, each
on a decision that changed scope rather than detail.

**Iteration 2 (2026-08-18)** — 16/16. All three answered and recorded in the spec's
Clarifications section:

| # | Decision | Answer | Effect on the spec |
|---|----------|--------|--------------------|
| Q1 | Does "at runtime" mean without a process restart? | No — read at startup, restart to apply, never a rebuild | Former US3 (hot reload) removed; hot reload named explicitly in Out of Scope; assumption added that "not build time" ≠ "no restart" |
| Q2 | Which throttles are in scope? | All six | FR-002 now enumerates them in a table with each one's shape; FR-004 added so the connection cap is not forced into an allowance/burst shape it has no meaning for |
| Q3 | Master switch only, or per-surface too? | Both, both from configuration | Former US4 promoted to US3 (P2); FR-008 and FR-009 added, FR-009 fixing precedence so the two switches cannot contradict; SC-005 and FR-022 added to cover it |

### Standing note on "No implementation details"

The Assumptions section names environment variables as the configuration source, and the
Clarifications section records that this was the requester's stated constraint
("configurable through the env"), not a design choice made here. Every functional
requirement stays phrased as "the deployment's configuration". This is a deliberate,
bounded exception and the item is marked passing on that basis.

### Independent verification — iteration 3 (2026-08-18)

A 9-agent workflow ran a four-way codebase sweep and a three-lens adversarial review
(constitution / spec-quality / refutation), then a consolidation pass and a verifying
synthesis. 61 raw findings; 21 survived verification, 5 of them blockers.

**The 16/16 recorded at iteration 2 was not warranted.** It was reached by grading the spec
against itself rather than against the code. Every blocker came from reading source the spec
had asserted things about:

| Verdict | Finding |
|---|---|
| ✅ **Confirmed correct** | FR-002's six surfaces are complete — no missed surface, none invented. The count was the one thing the sweep was launched to test, and it held. |
| ❌ Blocker | The client identity all five per-client throttles key on was never named in the spec, and is broken today (`e.IPExtractor` unset → `X-Forwarded-For` trusted from anyone) |
| ❌ Blocker | FR-001's premise was factually wrong for surface 3 — `TICKET_LOOKUP_*` is already env-settable and **published in `api/openapi.yml:625-626`** |
| ❌ Blocker | FR-012's amendment left three other statements contradicting it |
| ❌ Blocker | SC-006 forbade the skip FR-021 requires |
| ❌ Blocker | FR-014 caught one lockout shape of three — burst-0 refuses forever (`rate.go:362`) |
| ❌ Major | FR-015 blocked the exact tuning User Story 1 is built around (burst 5→100 needs 303s against 180s retention) |

27 corrections applied across spec.md, research.md, data-model.md and quickstart.md; 3 tasks
added. Four `data-model.md` source anchors were off by two lines and are fixed.

**Current honest status: the four Content Quality items and the Requirement Completeness
items above are re-graded as passing only after those 27 corrections.** The lesson worth
keeping is that a self-graded checklist cannot catch a spec that is confidently wrong about
the code — only reading the code catches that.

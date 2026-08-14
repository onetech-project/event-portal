# Specification Quality Checklist: Payment External Reference ID

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-14
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

**Iteration 1 (2026-08-14)** — Two open clarifications raised (checkout-outcome coverage,
admin placement). One checklist item failing.

**Iteration 2 (2026-08-14)** — Both answered and folded in; all items pass.

- **FR-011/FR-012** (was the open Q1): `ext_ref_id` carries the real reference on all three
  checkout outcomes that return a payment payload — fresh session, already-started refusal,
  lost-race answer. This makes the reference a value that is *read back*, not merely
  appended.
- **FR-015/FR-016** (was the open Q2): one order-level value inside the existing per-order
  payment view. No orders-list column, no new page.
- Source of the reference confirmed by the user: it arrives on the gateway's answer to the
  session-open call (the same answer carrying the QR payload), and on nothing else.

**Carried to planning, not a scope question.** The storage location is decided (the payment
records) and is not re-opened. But nothing writes to the payment records on the checkout
path today — they are written only from the notification path, by the payment domain — and
the two rebuilt checkout answers are assembled from the order's own stored payment details.
FR-012 therefore needs a read path that respects domain isolation. `/speckit-plan` owns that
design.

Spec is ready for `/speckit-plan`.

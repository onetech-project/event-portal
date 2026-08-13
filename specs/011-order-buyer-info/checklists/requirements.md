# Specification Quality Checklist: Order Page Buyer Information — Per-Ticket Holder Forms

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-06
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

- **Delivery rule, final**: the original input asked for per-holder delivery ("not just one"); the 2026-08-06 clarification reversed it — one email to the buyer (form 1) with all tickets and the receipt (FR-012). Constitution went 1.1.1 → 2.0.0 (fan-out) → 3.0.0 (back to one email); FR-016's form-step fee rule arrived in 2.1.0. The other holders' emails are still collected and stored as holder identity.
- Form 1 is both ticket holder 1 and the buyer; no separate buyer form exists (FR-001) and no extra label marks it — the delivery chip on that card is the marker (FR-011).
- Supersedes spec 010 FR-008 (buyer block preserved) and spec 008 FR-012 (buyer contact collection); noted inline in the spec.
- **UI/spec disagreement, RESOLVED 2026-08-13**: the order summary panel had been edited by hand to drop the Booking ID (FR-013) and the per-unit price on each ticket line (FR-014), matching Figma 206-3145, and the two assertions covering them sat suspended in `page.test.tsx` and `checkout/page.test.tsx` while the question went undecided through two plan revisions. The clarification session bent the spec to the shipped design rather than restoring the UI: FR-013 no longer asks for the Booking ID on the card (it stays on the confirmation screen and in the receipt) and FR-014 no longer asks for a per-unit price (quantity and line subtotal remain, and the unit price is still what the subtotal is derived from). Both suspended assertions were reinstated as negative checks, each paired with a rendered positive so absence cannot pass vacuously.
- **Judgment call on "no implementation details"**: FR-020 names the two URL addresses. Addresses are treated as product surface here, not implementation detail — they are user-visible, bookmarkable, and the distinction between them is precisely what the requirement is about. Spec 007 sets the same precedent ("distinct routes" in its clarifications). Item left checked deliberately rather than silently.

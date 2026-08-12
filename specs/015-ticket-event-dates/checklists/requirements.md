# Specification Quality Checklist: Per-Ticket Event Dates

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-11
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

All items pass — 16/16 before and after the 2026-08-12 clarification session, with no
state changes.

**The spec now diverges from the shipped implementation.** Four clarifications landed
after the feature was built, and two of them contradict working code: the package line
collapses to a single date where FR-021a–c now require every distinct day, and the
panel's Event box derives from the order's tickets where FR-009 now requires the event's
own dates. Re-run `/speckit-plan` (or go straight to the edits) before treating this spec
as describing what exists.

Resolved in the 2026-08-12 session:

- **FR-021a–c** — a package line names every distinct admission date its bundle carries;
  one or two shown exactly, three or more as a range between the earliest and latest
  admission **start** date. Counting is by distinct calendar date, not ticket count.
- **FR-012a/b** — all date and time rendering moves to English, day-first. Currency and
  visitor counts stay Indonesian, because `Rp 170.400` is correct for IDR.
- **FR-009** — reversed: the panel's Event box and its "Gate opens at" line read the
  event's own dates, not the order's tickets. The box is labelled "Event".
- Scope — the email receipt and the e-ticket are explicitly out of scope for the
  multi-date rule.

Resolved in the 2026-08-11 clarification session:

- **FR-005 / FR-005a–c** — containment is enforced at ticket-type save only. The event
  date edit stays unguarded and raises a non-blocking warning naming stranded ticket
  types, because enforcing both sides deadlocks a genuine reschedule: the event cannot
  move until its tickets do, and the tickets cannot move until the event does. Widening
  an event never strands anything.
- **FR-013** — the guest ticket-selection page keeps its current dateless design; the
  feature is confined to surfaces that already print a date.
- **FR-014** — validation uses the stated instants exactly, no grace period, both
  endpoints inclusive. Consequence recorded in FR-006 and Assumptions: the event start is
  the moment admission opens, not showtime.

Named entity/status vocabulary in the spec (`VALID`, `ALREADY_USED`, `INVALID`, "Order
Summary panel", "Gate opens at", "Dates" cell, "On sale") is existing user-visible
surface naming, retained deliberately so requirements anchor to what a stakeholder can
actually point at — not implementation leakage.

Two premises in the original request were corrected against the code and are reflected in
the Overview:

- The countdown is driven by the **event's** start date, not the sales window. "Not the
  countdown" is therefore honoured by leaving it on the event (FR-012), not by leaving it
  on a sales window it never used.
- The sales window prints no date anywhere on the guest side; it only gates row
  availability.

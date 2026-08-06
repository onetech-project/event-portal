# Specification Quality Checklist: Ticket Package Bundles in the Selection Step

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

## Validation Notes

**Iteration 1 — all items pass.** Details on the two items that needed judgement:

1. **"No implementation details"** — the specification names the existing `ticket_types`
   table once in Key Entities and once in Assumptions. This is an accepted, deliberate
   deviation: the request specifies a `tickets` table carrying `quota`, while this codebase
   already ships that concept as `ticket_types` and uses the name `tickets` for issued
   passes. Leaving the collision unstated would make every requirement ambiguous about which
   table it governs, which is a worse failure than the leak. Everywhere else the
   specification says "ticket" and "pass" in domain language. All SQL, Go, TypeScript,
   endpoints and transaction logic are confined to `contracts/`.

2. **Technology-agnostic success criteria** — SC-003/SC-004 quantify concurrency (200
   simultaneous attempts) and SC-005 quantifies page readiness (2 seconds). Both are stated
   as user-observable outcomes, not as database throughput or framework metrics, so they
   pass.

**Clarifications resolved before drafting** (asked and answered, so no markers were carried
into the spec):

- *How registration and passes work for a multi-ticket bundle* → **N forms → N passes**: the
  guest fills a separate registrant form per constituent, those registrants may be different
  people, and one pass is issued per attendee. This preserves the constitution's
  one-pass-per-attendee rule with no amendment and keeps `attendees.ticket_type_id` non-null.

**Scope confirmation received mid-authoring**: focus is the **ticket list selection** screen.
User Stories 1 and 2 are that screen; Story 3 covers only enough of checkout to make a
bundle-containing selection payable; Story 4 (admin authoring) is supporting and priced at
P3.

## Items for `/speckit-plan` to settle

These are not specification gaps — they are implementation decisions deliberately deferred,
recorded so they are not lost:

1. **Composition edits under open orders** (`contracts/checkout-transaction.md` §3): quota
   restoration reconstructs an order's hold from a package's *current* composition, so
   editing composition while `PENDING` orders exist would drift quota. Choose (a) reject such
   edits — recommended and already reflected in `contracts/api.md` — or (b) snapshot the
   expansion at checkout. Must not ship without one.
2. **Constitution clarification**: package-only orders leave `order_items.ticket_type_id`
   null, so the ticket-type delete-guard can no longer treat the `attendees` check as
   redundant. Principle VI's "associated" definition needs a wording pass
   (`contracts/schema.md` §6).
3. **PRD.md §1.5 is LOCKED** and predates packages; `/admin/packages` and the `packages[]`
   field on `GET /events/:slug` extend it. PRD.md and SCHEMA.md must be updated in the same
   change as the migration, per the constitution's Governance section.

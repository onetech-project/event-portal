# Specification Quality Checklist: Free Ticket Registration Form

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-20
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

**Re-validated 2026-08-21 after the EIGHTH `/speckit-clarify` session** (the review dialog laid out
against Figma, then its "Registration Info" section removed). Result: 16/16 → 16/16, no state
changes.

**The Figma connector was authorized for this session — the first time in this feature's life.**
Every prior pass carried "Unverified against the designs" as a standing risk, and this session is
what that risk cashing out looks like: the review dialog satisfied FR-017 completely while matching
nothing about the design, because FR-017 constrained its CONTENT and never its form. A requirement
can be fully testable, fully met, and still leave the screen wrong.

*No contradictory earlier statement remains* was the item that earned this session, twice over.
FR-017 required the review to show "the ticket type being claimed"; the design has no such row and
the user removed the section. FR-017 is amended rather than quietly contradicted, and **FR-017a
states the new rule as a prohibition** — "MUST NOT name the event or the ticket type" — because a
requirement satisfied by omission is one the next person reinstates as an improvement. Both the
Vitest and Playwright assertions were inverted to match: they now assert the section's ABSENCE.

**The accepted cost, on the record.** FR-017 existed so that nobody confirms a registration without
being shown which ticket they are claiming. That is gone. What carries the weight instead is that
the form behind the dialog identifies the event (FR-015) and the address is per-ticket-type — a
guest arrives by invitation to one specific ticket and cannot select another here. The review checks
their typing, not their selection. If a future change ever lets one form offer a CHOICE of ticket
types, FR-017a becomes wrong and must be revisited before that ships.

**Two places the Figma frame is stale, noted and NOT implemented.** The same screen draws a phone
placeholder of `0812XXXXXXXX` — eleven digits, which this product's own twelve-digit rule refuses
(FR-006, already recorded at spec.md's phone clarification) — and a date-of-birth placeholder
reading "Optional" against a field FR-021 makes required. The design was followed for layout and
typography, and deliberately not for content that contradicts a shipped rule.

**Re-validated 2026-08-21 after the SEVENTH `/speckit-clarify` session** (which removed the
read-through gate, keeping only the automatic tick). Result: 16/16 → 16/16, no state changes.

**This is the fourth reversal of an already-implemented decision, and the second in the same
week to reverse a decision made in *this* spec.** The gate FR-045 required — checkbox
uncheckable until the end of the document, Agree withheld until then — is now forbidden by the
same requirement number. Four clarifications settled what replaces it: Agree tracks the checkbox
(FR-014c), the form's box still opens the modal (FR-014a, unchanged), the automatic tick fires at
most once per opening and never overrides a deliberate untick (FR-045a, new), and a voided
consent re-presents the current document without requiring it to be read (FR-051, FR-014f).

*No contradictory earlier statement remains* is the item that earned this session. Gate language
had spread well beyond FR-045: it was load-bearing in **SC-011**, which measured "0% can record an
agreement without having reached the end" — a criterion the new behaviour fails by design and
which would have read as a regression rather than the intended change. SC-011 now measures that
the document was *presented*, plus the converse property the user actually asked for (nobody who
wants to agree is blocked). Twelve further statements were corrected in place: FR-014d, FR-014f,
FR-043c, FR-046, FR-047, FR-048, FR-051, FR-044, US5's title, rationale, Independent Test and six
of its scenarios, two edge cases, the Terms & Conditions entity note, SC-013, the Assumptions
line about the gate being a presentation rule, and the Out of Scope line. The two superseded
2026-08-20 clarifications are retained under explicit labels rather than deleted, as every prior
reversal in this file has been.

*Success criteria are measurable* was the second item re-checked hardest, for the reason above:
a success criterion that measures a rule the spec has just deleted still reads as passing quality
review, because it is well-formed. It has to be re-read for **subject**, not just for form.

**The specification is internally consistent; the code and the downstream artefacts are not.**
`plan.md` (17 hits), `tasks.md` (17) and `research.md` (13) still describe the gate, as do
`frontend/components/booking/terms-dialog.tsx`, `frontend/components/terms/terms-viewer.tsx`
and both their test files. FR-048 additionally puts the booking dialog inside a Principle VIII
covered flow, so `e2e/` scenarios asserting the gate will fail against the new requirements —
correctly. None of that is resolved here.

**Re-validated 2026-08-20 after the SIXTH `/speckit-clarify` session** (sharing the holder fields
and the T&C dialog shell between the booking flow and the registration form). Result: 16/16 →
16/16, no state changes.

*No implementation details* was the item to watch, because FR-043a/043b describe component
boundaries. They are kept because the REQUIREMENT is the sharing itself: "the two surfaces must not
diverge" is not testable without saying that one component renders both, and the previous wording —
which required only shared *behaviour* — was satisfied by two implementations that had already
drifted. FR-043b names the concrete evidence (a phone placeholder of eleven digits against a
twelve-digit rule) rather than asserting drift in the abstract.

**Unverified against the designs.** The Figma MCP server was disconnected for this session, so the
five design links could not be opened. The reuse was resolved from the code. Any remaining visual
difference between the built screens and the designs is therefore NOT established either way, and
this line is here so that gap is not mistaken for a check that passed.

**Re-validated 2026-08-20 after the FIFTH `/speckit-clarify` session** (the admin price field
following the on-sale checkbox). Result: 16/16 → 16/16, no state changes.

*Requirements are testable / no contradictory statement remains* was where this session earned its
keep. The first draft of FR-004b required a non-zero price on any ticket going on sale — which
directly contradicted FR-003's explicit allowance of a purchasable ticket priced zero, and was
caught by an existing schema test rather than by review. FR-004c now records the corrected rule and
why, so the same over-reach is not re-derived later: what is required is a price *typed*, not a
price *non-zero*.

FR-004d exists for the same reason FR-033b does — a rule whose whole purpose is that a human is not
misled has to state the negative case too, so the explanation cannot leak onto a create form where
nothing was cleared and the sentence would be a plain falsehood.

**Re-validated 2026-08-20 after the FOURTH `/speckit-clarify` session** (which removed
`orders.is_registration` in favour of deriving the distinction). Result: 16/16 → 16/16, no state
changes.

This is the third reversal of an already-implemented decision, and the first to require a
**constitution amendment** rather than merely a spec edit: Principle IV had made the stored marker
part of the permission that admitted a second origin for `PAID`, so removing it is a MAJOR bump
(v5.0.0 → v6.0.0), and SCHEMA.md, ARCHITECTURE.md and the migration moved in the same change as
Governance requires.

*No implementation detail leaks* was re-checked hardest here, and FR-033/033a/033b deliberately name
the derivation expression's shape. That is justified on the same ground as exception (1): the rule
is only testable if the spec says what "distinguishable" resolves to, and FR-033a exists precisely
to forbid two *cheaper* rules that would look equivalent to a reader who was not told.

*Requirements are testable* drove FR-033b, which requires a TEST rather than a comment for the
accepted retroactivity. A cost recorded only in prose is one nobody will find; the assertion in
`TestDerivedRegistrationFlagFlipsWhenTheTicketTypeBecomesPurchasable` is what makes a later
"fix" collide with the decision instead of quietly reversing it.

**Re-validated 2026-08-20 after the THIRD `/speckit-clarify` session** (the one that renamed
`ticket_types.is_registration_only` to `is_visible`). Result: 16/16 → 16/16, no state changes.

Like the previous session, this one **reverses an already-implemented decision** — and it is
the more dangerous of the two, because it looks like a rename and is not. `is_visible` is the
logical negation of `is_registration_only`: the default flips `FALSE` → `TRUE`, every filter
flips sense, and every write flips value. A find-and-replace across the 119 references would
compile, pass type-checking, and be exactly backwards. SC-009 was strengthened specifically to
make that failure detectable, because its symptom — every ticket type in the system silently
becoming registration-only — raises no error anywhere.

The specification remains internally consistent; the code does not yet match it.

**Re-validated 2026-08-20 after the SECOND `/speckit-clarify` session** (the one that removed
the per-event email-uniqueness rule). Result: 16/16 → 16/16, no state changes. Every item was
re-evaluated against the new text rather than carried forward.

This session is unusual and the fact is recorded here rather than left to be discovered:
**it reverses a decision that was already implemented.** FR-023 previously required a
duplicate-email refusal; it now forbids one. That is a spec change with a live code footprint —
a query, a lock, an index, an error code, a client message and a concurrency test all exist to
enforce a rule that no longer exists. The checklist passes because the *specification* is
internally consistent; it says nothing about the code, which at the moment contradicts it. See
the Completion Report for the enumerated removals.

**Re-validated 2026-08-20 after the FIRST `/speckit-clarify` session.** Result: 16/16 → 16/16,
no state changes. The spec grew from 42 to 65 functional requirements, from 10 to 16 success
criteria, and from 4 to 5 user stories; every checklist item was re-evaluated against the new
text rather than carried forward.

Four clarifications were resolved with the user before drafting, and five more during the
clarify session (Clarifications, session 2026-08-20), so no `[NEEDS CLARIFICATION]` markers were
carried into the spec.

**Deliberate, justified exceptions to "no implementation details":**

1. Column and table names (`ticket_types.is_visible`, `attendees`, `tickets`,
   `orders`, `ticket_types.quota`) appear in FR-001..FR-005, FR-026..FR-033 and the
   Clarifications. `SCHEMA.md` is the project's declared absolute source of truth for the
   database and the constitution requires it to change in the same commit as any migration,
   so the schema is a governed contract this specification is entitled to name — and the
   column name was itself the user's decision to make (twice: `is_registration_only`, then
   `is_visible`). Naming it here is what makes FR-001 testable. FR-001a additionally names the
   gap between what the column is called and what it governs, which is a specification concern
   rather than an implementation one: an admin toggling "visible" needs to know it also makes
   the type free to register for.
2. Status `PAID` appears in FR-027. It is the literal subject of the Principle IV deviation
   recorded in *Constitution Deviations*; the deviation cannot be stated without it.
3. The route `/events/[slug]/register/[ticketId]` appears in FR-010, the Clarifications and the
   Assumptions. It is a user-visible address the user specified explicitly, and a requirement to
   serve a form "somewhere" would not be testable without it.

4. `orders.terms_agreed_at` and `orders.event_terms_id` appear in FR-049 and the Clarifications.
   Same justification as (1): these are existing governed schema fields, and the decision made
   was specifically to reuse them rather than invent a parallel record.

5. FR-023a and the FR-023 clarification name mechanisms being *removed* — an address-scoped
   lock, an address-existence query, a `lower(email)` index, a dedicated refusal code. Naming
   them is what makes the reversal actionable: this rule is already built, so "the constraint is
   removed" is not a testable instruction unless the spec says which artefacts must stop
   existing. Each requirement still leads with the observable outcome (two submissions of one
   address both succeed and never contend), and the mechanism names follow as particulars.

**Risks carried forward, not resolved here:**

- FR-027 writes `PAID` outside a gateway webhook, contradicting a NON-NEGOTIABLE principle. The
  spec records this as requiring a constitution amendment before implementation rather than
  treating it as settled. `/speckit-plan` must not schedule implementation work ahead of that
  amendment.
- FR-044/FR-045 change the booking flow's Terms & Conditions dialog, which is inside a Principle
  VIII **covered flow**. FR-048 requires the affected `e2e/` scenarios to change in the same
  commit. A plan that treats this as frontend-only work is wrong.
- FR-039 ships copy asserting a delivery that has not completed, decided deliberately after the
  concern was raised. FR-039e, FR-038 and FR-053 are the compensating requirements; if any of
  the three is dropped during planning, an undelivered registration becomes unrecoverable and
  the guest is never told.

**Items re-examined most carefully this pass**, because the clarify session is where specs
usually acquire contradictions:

- *No contradictory earlier statement remains.* Four statements were **replaced, not
  supplemented**: the two-checkbox consent requirement (old FR-014), the plural-route assumption,
  the "confirmation must not assert delivery" clause (old FR-039), and "both guest and admin
  resend MUST work" (old FR-038). The superseded consent clarification is retained in the
  Clarifications with an explicit *superseded* label, which is a record of a decision reversal
  rather than live contradictory guidance.
- *Requirements are testable.* FR-039d previously deferred a behaviour to the plan; it now
  asserts the outcome and its accepted consequence, so nothing in the requirements defers a
  decision to a later phase.
- *No contradictory earlier statement remains, second session.* Eight further statements were
  **replaced, not supplemented**: FR-023/023a/023b, US1 acceptance scenario 6, the
  concurrent-same-address edge case, SC-002, SC-014, SC-016, the Attendee and Event entity
  notes, and the Assumptions line that listed the duplicate rule as one of the limits. Two
  rationales that survived only because of the deleted rule were corrected in place rather than
  left standing: the confirmation-page clarification no longer cites duplicate-error avoidance
  as a reason, and FR-039b no longer promises a reload will not raise a duplicate failure. The
  superseded duplicate-email clarification is retained under an explicit *WITHDRAWN* label, as a
  record of a reversal rather than live guidance.
- *Success criteria are measurable* was re-checked hardest on SC-016, which previously claimed
  no unauthenticated surface could cause mail to an address the caller does not control. That
  was already an overclaim — the unlisted link could always send one such mail — and removing
  the per-address rule makes it repeatable. SC-016 now states the true, measurable property (no
  address-existence disclosure, and refusal of everything beyond the per-source threshold)
  rather than the one that reads better.

**Verification of a re-checked item** — "Requirements are testable": FR-020 and FR-025 defer to
"the same rule / the same verbatim message as the existing holder forms" rather than restating
digits and lengths. This is testable (the two surfaces are compared against each other, which is
the actual requirement) and deliberately avoids creating a second copy of a rule that has already
been changed twice.

# Specification Quality Checklist: Receipt + E-Ticket Email Attachments

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-12
**Feature**: [spec.md](../spec.md)

## Content Quality

- [ ] No implementation details (languages, frameworks, APIs)
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
- [ ] No implementation details leak into specification

## Notes

**Iteration 1 (2026-08-12)** — 3 [NEEDS CLARIFICATION] markers raised: per-ticket price on
the e-ticket, masking of buyer contact details, and the source of branding.

**Iteration 2 (2026-08-12)** — all 3 resolved and recorded in the spec's Clarifications
section; 16/16 passing.

**Iteration 3 (2026-08-12, design review)** — the rendered implementation was compared
against Figma and 15 corrections were raised. Three carried real tradeoffs and were put to
the user; the rest were unambiguous and recorded directly. **16/16 → 14/16.**

### Two items regressed, both for the same reason, both accepted

- **"No implementation details"** and **"No implementation details leak into
  specification"** now fail. FR-031a names CSS flexbox and CSS grid; FR-023a names inline
  image parts referenced by content ID, and rules out remote URLs and `data:` URIs.

  This is deliberate and is not a defect to fix by rewording. The user was asked to choose
  the layout mechanism and the logo-delivery mechanism, and **the mechanism is the
  decision** — an email that uses flexbox renders differently for a large share of
  recipients, and a logo delivered by remote URL simply does not appear for most of them.
  Stating only the outcome ("renders correctly everywhere") would leave the spec unable to
  record what was actually decided, and would invite the same wrong implementation again.

  Both requirements carry an inline note saying why the mechanism is named, so a future
  reader does not "clean it up".

### Kept passing by rewriting rather than by accepting the leak

**"Success criteria are technology-agnostic"** was heading for a regression: the first
draft of SC-011 asserted the body "contains no `display:flex` and no `display:grid`
declaration", and SC-012 named Outlook, Gmail and Apple Mail. Both were rewritten to state
the user-visible outcome instead — no client shows a stacked fallback of a multi-column
block; the mark appears as an image for a recipient whose client blocks remote images. The
mechanical check moved into FR-031a, where an implementation constraint legitimately lives.

### New dependency worth flagging before planning

The **logo image is now a hard dependency** (FR-023a), not the optional ornament research
R-005 treated it as. So are a location-pin icon (FR-025) and an envelope icon (FR-015a).
Since this was written: the logo has been extracted and staged at
`backend/assets/brand/jive-logo.png`; the envelope will be drawn with vector primitives so
needs no asset; and the **pin cannot be obtained at all** — Figma node `750:2` is an emoji
text layer that exports blank. FR-025 is therefore unmet pending a decision, recorded in
plan.md.

### Governance follow-up owed by the implementation

FR-001 and SC-001 say "exactly two **document** attachments". This turned out to need less
work than feared: verified against a running Mailpit, inline CID parts are reported in a
separate `Inline` array, so `Attachments` stays at 2 and the e2e assertion is unaffected.
The constitution bullet and `PRD.md` §1.4 should still gain the word "document" for
precision, but no version bump is warranted.

**Iteration 4 (2026-08-12, currency decimals)** — the user reported that a stored
`15.000,92` must print its `92`. Answering it reversed part of iteration 3. **14/16 → 14/16;
no checklist item changed state.**

What changed, and why it was more than a formatting tweak:

- FR-037 was rewritten a second time. The documents now use Indonesian separators — dot
  thousands, comma decimal — differing from the site in the **prefix only**. Iteration 3
  had taken the email mock's comma-thousands styling; the site's own formatter carries a
  comment warning that exactly that styling "would misstate the sum they are about to pay"
  for these buyers, and the decimal case made the risk concrete.
- FR-037a (fraction shown only when non-zero) and FR-037b (never truncate) are new.
- **SC-013 was self-contradictory and is fixed.** It required "no dot-separated thousands
  anywhere", which the new FR-037 mandates. SC-013a is new: the printed rows must sum to
  the printed total.
- The superseded clarification bullet is marked in place rather than deleted — the
  reversal's reasoning only reads correctly against what it replaced.

**The substantive finding is not the separators.** `formatIDR` uses `IntPart()`, which
truncates. On an order whose fees compute to cents, the printed rows would not sum to the
printed total — a reconciliation defect on a financial document, and a direct conflict with
FR-014/SC-003 that existed before this iteration and was not previously noticed.

Items marked incomplete remain the two accepted implementation-detail deviations described
above. They do not block `/speckit-tasks`.

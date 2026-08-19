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

**Iteration 5 (2026-08-19, brand refresh + reported attachment count)** — a report that the
email carries four attachments, plus a new brand asset in two variants and a footer change
on the e-ticket. Five questions were put to the user; a sixth clarification came from the
user's own question about SVG. **14/16 → 14/16; no checklist item changed state.**

- **The reported defect was not one.** Mailpit's API returns the two PDFs in `Attachments`
  and the logo and pin in `Inline`, and its list endpoint reports `Attachments: 2`; its
  message *view* merges both into one downloadable strip because it is a MIME debugger.
  Gmail and Yopmail were checked by hand and list two. FR-001, FR-023a and SC-001 are
  confirmed rather than amended, and iteration 3's governance follow-up is now closed —
  `PRD.md` §1.4, the constitution bullet and the e2e assertion were all re-checked and
  already agree.
- **FR-025 was narrowed** from "inline SVG or an inline image part" to the image part alone.
  Gmail strips `<svg>` and Outlook's Word engine cannot draw it, so the SVG allowance would
  have failed the two largest client families. This is a real correction, not tidying: an
  implementer following the old wording could have shipped something invisible to most
  recipients.
- **FR-022a was amended** — the customer-service block is right-positioned with its contents
  left-aligned, not two lines flush right. FR-022d (envelope icon) and FR-022e (the rule must
  hold for any address length, not just the mock's) are new.
- **FR-023b and FR-023c are new**: the mark grows so the sponsor lockup's second line is
  legible, and exactly one asset serves all surfaces.

### Two items re-checked closely and kept passing

**"Success criteria are technology-agnostic"** was heading for the same regression
iteration 3 caught. SC-019's first draft asserted the retired mark "returns no matches in
the frontend's public assets, the backend's embedded assets, or any source reference" —
three implementation locations in one criterion. Rewritten to the outcome: every surface
shows the same mark, and the retired mark appears on none.

**"All acceptance scenarios are defined"** would have failed: FR-022a, FR-022d, FR-023b and
FR-023c were amended or added with nothing in User Story 2 exercising them. Scenarios 6 and
7 were added — the client lists two documents with the images rendering in the body, and
the mark's secondary line is legible at 100% zoom.

### Scope deliberately left out, recorded so it is not mistaken for an oversight

- **The receipt carries no brand mark and never has.** `drawBrandMark` is called only from
  the e-ticket renderer, yet FR-023a and FR-035 both describe the mark as being on "all
  three surfaces". Either the receipt gains a header band or those two requirements narrow
  to two surfaces. Settling it would redesign a document nobody asked to change, so it is a
  governance note rather than part of this change.
- **The frontend's `favicon.ico` is still the unmodified Next.js scaffold icon** and the
  document title is still "Event Ticketing". Neither is a logo call, so the "update every
  logo call" instruction does not reach them.

Items marked incomplete remain the two accepted implementation-detail deviations described
above. They do not block `/speckit-tasks`.

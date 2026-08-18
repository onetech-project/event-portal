# Feature Specification: QRIS Frame Simplification

**Feature Branch**: `019-qris-frame-simplify`

**Created**: 2026-08-18

**Status**: Draft

**Input**: User description: "i want to update the qris design to be like this https://www.figma.com/design/kelTjyctfqKz5pJQ90U6BM/Project---JIVE?node-id=947-105&m=dev — so drop the merchant name, nmid, terminal label, qris acquirer code, and print version"

## Clarifications

### Session 2026-08-18

- **Reference design confirmed**: the requester supplied the rendered target frame — the
  Scan to Pay card as drawn (Figma node `203-1201`), carrying the QRIS and GPN marks, the
  heading and its one-line subtitle, the live code, the two standard notices, the three
  pay-step icons, and the total below. No merchant identity block, no printed footer.
- Q: With the merchant name gone from the frame, what should the "How to pay with QRIS"
  verification step say? → **A: Drop the name — the guest verifies the amount and a
  merchant they recognise. All five configured values are retired.**

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A guest sees the payment code in the frame as designed (Priority: P1)

A guest reaches the payment page for an unpaid order. The Scan to Pay card presents the
QRIS and GPN marks, the "Scan to Pay" heading with its one-line subtitle, the live code
for this order, the two standard QRIS notices beneath it, the three pay-step icons in the
corner wedge, and the total due below the frame. Nothing else — no merchant name, no
registration number, no terminal label, and no printed-footer lines.

**Why this priority**: This is the entire feature. The payment surface is the most-seen
screen in the purchase journey, and it currently carries five fields the design does not
call for. Everything else here is consequence.

**Independent Test**: Load the payment page for an unpaid order and read the card. The
code is present and scannable, the frame matches the design, and none of the five removed
fields appear anywhere on the card.

**Acceptance Scenarios**:

1. **Given** an unpaid order with an issued code, **When** the payment page loads,
   **Then** the card shows the QRIS and GPN marks, the "Scan to Pay" heading, its
   subtitle, the live code, "SATU QRIS UNTUK SEMUA", the `www.aspi-qris.id` notice, the
   three pay-step icons, and the total due.
2. **Given** the same page, **When** the guest reads the card, **Then** no merchant name,
   no registration number ("NMID"), no terminal label, no "Dicetak oleh" line and no
   "Versi Cetak" line appear anywhere on it.
3. **Given** the same page, **When** the guest scans the code with a QRIS-capable app,
   **Then** the code resolves exactly as it did before this change — removing the printed
   labels changes nothing the app reads.
4. **Given** the space the removed identity block used to occupy, **When** the page
   renders, **Then** the heading and its subtitle occupy it, so the frame carries no
   conspicuous gap between the marks and the code.

---

### User Story 2 - The payment instructions no longer point at a label that is gone (Priority: P1)

A guest unfamiliar with QRIS opens the "How to pay with QRIS" instructions. The
verification step tells them to check the amount their app shows against the amount on
this page, and to stop if it differs. It does not name a merchant, and it does not refer
them to a merchant name printed above the code — because there is no longer one there.

**Why this priority**: Equal in priority to US1 and inseparable from it. Instructions that
tell a guest to compare against a label the page no longer prints are worse than no
instructions: they read as a safety check while offering nothing to check against.
Shipping US1 without US2 leaves a live, misleading instruction on a payment screen.

**Independent Test**: Open the instructions on the payment page and read every step. No
step names a merchant or refers to a merchant name printed on the card, and the amount
check is still stated explicitly with the actual amount.

**Acceptance Scenarios**:

1. **Given** the payment page, **When** the guest expands "How to pay with QRIS",
   **Then** the verification step names the exact amount due and instructs them to stop
   without entering a PIN if their app shows a different amount.
2. **Given** the same instructions, **When** the guest reads them end to end, **Then** no
   step names a configured merchant, and no step refers to a merchant name printed above
   or around the code.

---

### User Story 3 - A deployment no longer carries QRIS frame configuration (Priority: P2)

An operator deploying or promoting the application configures the settings for that
environment. The five QRIS frame values are not among them: they are absent from the
deployment configuration templates and from the operator documentation, and setting them
has no effect.

**Why this priority**: Lower than the visible change but genuinely part of it. Four of
these values existed only to be printed on the frame, and the fifth only to be named in
the instruction step; with both gone, leaving the knobs behind creates configuration that
looks load-bearing on a payment path and is not — the kind of thing an operator later
sets carefully and to no effect.

**Independent Test**: Start the application with none of the five values set and confirm the
payment page renders identically. Search the deployment configuration templates and the
operator documentation and find no reference to them.

**Acceptance Scenarios**:

1. **Given** a deployment with none of the five QRIS frame values configured, **When** the
   payment page loads, **Then** it renders exactly as it does with them set — the values
   are read by nothing.
2. **Given** the operator documentation and the deployment configuration templates,
   **When** an operator looks for what the application must be configured with, **Then**
   the five QRIS frame values are not listed.
3. **Given** an existing deployment that still sets the five values in its environment,
   **When** the new version starts, **Then** it starts normally and ignores them — a
   leftover value is not an error.

---

### Edge Cases

- **Order already expired or cancelled**: the frame stays and the code is replaced by the
  existing explanatory message. That behaviour is unchanged; the removed fields must not
  reappear in the ended state, and the message must still sit where the code was.
- **Very narrow viewport**: the frame scales as one piece. With the identity block gone
  the frame's internal proportions shift, and the heading, subtitle, code, notices and
  step captions must all remain legible and un-clipped at the narrowest supported width.
- **Leftover configured values**: an environment that still sets the retired values must
  not fail to start, and must not print them.
- **A guest who scanned a swapped code**: the amount check is now the only in-page check
  available. It must therefore state the exact amount, not a vague instruction to "check
  the amount".

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The payment card MUST present the issued code inside the standard QRIS frame
  showing only: the QRIS and GPN marks, the "Scan to Pay" heading, its one-line subtitle,
  the code, "SATU QRIS UNTUK SEMUA", the `www.aspi-qris.id` notice, and the three
  pay-step icons with their captions.
- **FR-002**: The payment card MUST NOT display a merchant name.
- **FR-003**: The payment card MUST NOT display a merchant registration number ("NMID").
- **FR-004**: The payment card MUST NOT display a terminal label.
- **FR-005**: The payment card MUST NOT display an acquirer code ("Dicetak oleh") or a
  printed-layout version ("Versi Cetak"). The frame's lower-left footer block is removed
  in its entirety.
- **FR-006**: The heading and subtitle MUST sit inside the frame between the QRIS/GPN
  marks and the code, occupying the space the removed identity block vacated, so the
  frame reads as a deliberate composition rather than one with a hole in it.
- **FR-007**: The total due MUST remain below the frame, labelled and unchanged.
- **FR-008**: The code itself MUST be unchanged — same source, same live payload, same
  on-demand rendering. This change is presentational only and MUST NOT alter what is
  encoded, requested from, or reported to the payment gateway.
- **FR-009**: The ended-order behaviour MUST be preserved: after an order expires or is
  cancelled the frame remains and the code is replaced by the existing explanation, with
  none of the removed fields returning.
- **FR-010**: The payment instructions MUST tell the guest to verify the exact amount due
  before confirming, and to stop without entering a PIN if their app shows a different
  amount.
- **FR-011**: The payment instructions MUST NOT name a configured merchant and MUST NOT
  instruct the guest to compare against a merchant name printed on the card.
- **FR-012**: The five QRIS frame values (merchant name, registration number, terminal
  label, acquirer code, printed-layout version) MUST be removed from the settings the
  application reads at start-up, from the deployment configuration templates, and from
  the operator documentation.
- **FR-013**: A deployment that still supplies any of the retired values MUST start
  normally and ignore them.
- **FR-014**: The end-to-end acceptance suite MUST assert the payment card's contents —
  what it shows and what it must not show — so a later reintroduction of the removed
  fields is caught, per constitution Principle VIII.

### Requirements superseded by this change

This feature amends spec `012-manjo-payment-gateway`, which mandated the removed fields.
The following MUST be updated in the same change:

- **FR-021a** (frame shows merchant name, registration number and terminal label as fixed
  configured values) — **superseded**. The frame shows none of them.
- **FR-021b** (those fixed values must be verified against a genuinely issued code before
  go-live and re-verified whenever the merchant account changes) — **retired**. With
  nothing displayed there is no fixed label that can disagree with the code, which
  removes the release step and the mismatch edge case it guarded.
- **FR-021c** (instructions must have the guest verify both merchant name and amount) —
  **narrowed to the amount**, per FR-010.
- Spec 012's acceptance scenarios naming the frame identity and the merchant-name check,
  and its deferred item "derive the QRIS frame's merchant identity from the code" — the
  latter is **withdrawn**, since there is no longer a displayed identity to derive.

### Key Entities

- **Payment card**: the Scan to Pay surface on the payment page. Composed of the QRIS
  frame (marks, heading, subtitle, live code, standard notices, pay-step icons) and the
  total due beneath it.
- **Payment instruction**: the live code and its amount, issued per order by the gateway.
  Unchanged by this feature.
- **QRIS frame configuration**: the five per-environment values retired by this feature.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: A guest viewing the payment page sees zero occurrences of the merchant name,
  registration number, terminal label, acquirer code and printed-layout version — five
  fields removed, none remaining, in both the payable and the ended state.
- **SC-002**: The rendered payment card matches the reference design's element inventory
  exactly: every element the design draws is present, and no element the design omits is
  shown.
- **SC-003**: Codes issued after this change are scanned and settled at the same success
  rate as before it — the change is presentational and MUST show no effect on completed
  payments.
- **SC-004**: A deployment requires five fewer configured settings, and starting with
  none of them set produces a payment page indistinguishable from one started with all of
  them set.
- **SC-005**: The acceptance suite fails if any removed field is reintroduced to the
  payment card, verified by confirming the new assertions go red against the current
  (pre-change) code.
- **SC-006**: The payment instructions state the exact amount due, and contain no
  reference to a merchant name, in 100% of renders.

## Assumptions

- The reference design is the Scan to Pay card as drawn at Figma node `203-1201`; node
  `947-105` is the generic QRIS template raster the requester linked to point at the
  frame artwork, and its printed identity fields are not the target.
- The frame's artwork — the batik ground, the red chevron and corner wedge, the QRIS and
  GPN marks, and the three step icons — is unchanged. Only text is removed, plus the
  footer block that held two of the removed lines.
- The QRIS frame's overall proportions and the code's relative size follow the reference
  design once the identity block is gone; the code does not shrink.
- Removing printed labels has no bearing on QRIS scheme compliance for a
  dynamically-issued, per-transaction code presented on screen. A printed static merchant
  standee is a different artefact under different rules, and none is affected here.
- No stored data changes. No API contract changes. No backend change is expected beyond
  whatever the acceptance suite needs.
- The existing payment countdown, live status updates, and end-of-journey dialog are
  untouched.
- Guests are the only audience for this surface; there is no admin view of the QRIS frame.

## Dependencies

- The payment page and its live code issuance (spec `012-manjo-payment-gateway`) must be
  working; this feature edits its presentation and amends three of its requirements.
- Governance updates required in the same change: spec `012` FR-021a–c and the related
  scenarios and deferred item, plus the operator documentation table of per-environment
  configuration.

## Out of Scope

- Any change to how the code is issued, encoded, rendered, or verified.
- Any change to the payment gateway integration, callbacks, or settlement.
- Any change to the total-due presentation, the countdown, or live status updates.
- Deriving merchant identity from the code payload — withdrawn, not deferred.
- The receipt and ticket email, which do not carry the QRIS frame.

# Research: QRIS Frame Simplification

**Feature**: `020-qris-frame-simplify` | **Date**: 2026-08-18 | **Phase**: 0

The Technical Context in [plan.md](./plan.md) carries no NEEDS CLARIFICATION markers —
this is a subtractive change to an existing, well-understood component in a stack the
repository already runs. What follows is therefore not technology selection but the
decisions that were genuinely open, and the two findings that change how the work must be
sequenced.

---

## D-1: Which Figma node is the target

**Decision**: Build against node `203-1201`, the Scan to Pay card as composed. Node
`947-105`, which the request linked, is not the target.

**Rationale**: `947-105` is a single flat raster named `image 4`, 522 × 735 — the generic
QRIS template artwork. Rendering it shows a merchant name (`INSTITUTE OF COMPUTER
SCIENCE`), an NMID, a terminal label `A01`, `Dicetak oleh : 93600008` and
`Versi Cetak : 1.0-2024.11.13`: every field this feature removes. Taken literally it
would be an instruction to keep them. The requester then supplied the rendered card and
named it "the new design", and that render is `203-1201` — the node the current component
already cites as its source, and the one that carries none of the five fields. The linked
node was pointing at the frame *artwork*, not at its printed text.

**Alternatives considered**: Treating `947-105` literally — rejected, it contradicts both
the explicit "drop the merchant name, nmid, terminal label, qris acquirer code, and print
version" and the confirmed render. Asking again — unnecessary; the render settled it.

**Consequence for implementation**: the implementer MUST load the `figma-design-to-code`
skill and call `get_design_context` on `203-1201` before editing markup, per the Figma
MCP contract. Do not re-derive from `947-105`.

---

## D-2: Where the heading and subtitle live

**Decision**: `Scan to Pay` and its one-line subtitle move *inside* the frame, between the
QRIS/GPN lockups and the code. The `CardHeader` above the frame goes away.

**Rationale**: In the reference render the QRIS and GPN marks sit *above* the heading.
Those marks are part of the frame — they are inside the 522 × 735 artwork. A heading that
renders above them, as today's `CardHeader` does, cannot produce that stacking order. And
simply deleting the three identity lines without filling the gap leaves roughly a fifth of
the frame's height empty between the marks and the code, which reads as a rendering fault
rather than as a clean design. FR-006 exists for that reason.

**Alternatives considered**:

- *Keep the `CardHeader` and let the frame open with a gap* — rejected; contradicts the
  render and looks broken.
- *Keep the `CardHeader` and enlarge the QR to absorb the space* — rejected; the render
  shows the code at essentially its current relative width, and growing it would also
  push the two standard notices and the step wedge out of position.

**Note**: the accessible heading must survive the move. `CardTitle` renders a heading
element that the e2e and unit assertions can address by role; whatever replaces it inside
the frame must still expose "Scan to Pay" as a heading, not as a styled paragraph.

---

## D-3: Making the acceptance assertion actually fail first — *the finding that matters*

**Decision**: `e2e/playwright.config.ts` MUST supply all five retired values to the
`frontend` web server. The new card-contents assertion is then written against a rig that
is actively trying to print them.

**Rationale**: This is the one place the obvious plan is wrong.

`frontend/.env` is gitignored (`frontend/.gitignore:34`, `.env*`). On this developer's
machine it sets `NEXT_PUBLIC_QRIS_MERCHANT_NAME=JIVE`, `…_MERCHANT_ID=936008580287697876`,
`…_TERMINAL_LABEL=659`, `…_ACQUIRER_CODE=93600008`, `…_PRINT_VERSION=1.0-2024.11.13`, and
Next's dev server loads it. In CI there is no such file, and `runtimeConfigFromEnv()`
resolves all five to `""`, which the component already renders as *nothing*
(`{merchantName ? … : null}` and siblings).

So an assertion of the form "the payment card must not contain `JIVE`" behaves like this:

| | before the change | after the change |
|---|---|---|
| locally (`.env` present) | **red** ✓ | green ✓ |
| CI (no `.env`) | **green** ✗ | green ✓ |

A test that is green in CI before the fix is not a regression pin. It would go on passing
if someone reinstated all five fields tomorrow, because CI's environment never gives the
component anything to print. Principle VIII's "green because the suite never looked" is
exactly this shape, and spec SC-005 forbids it.

Supplying the values in the rig collapses the table to red-then-green everywhere, and it
buys a second thing for free: after the change the component no longer reads those
variables at all, so the rig setting them *is* the FR-013 scenario — "a deployment that
still supplies retired values starts normally and ignores them". One env block, two
requirements proven.

**Alternatives considered**:

- *Assert against whatever the environment happens to hold* — rejected, per the table.
- *Commit a `frontend/.env` for the suite* — rejected; `.env*` is gitignored deliberately
  and un-ignoring it invites secrets into the repository for an unrelated reason.
- *Set the values in `e2e/support/env.ts` as configurable knobs* — rejected as
  over-general. These are fixed literals whose only job is to be conspicuous and
  un-rendered; a knob implies someone might want to vary them, and varying them weakens
  the assertion.

**Implementation note**: use the runtime (bare) names — `QRIS_MERCHANT_NAME`,
`QRIS_MERCHANT_ID`, `QRIS_TERMINAL_LABEL`, `QRIS_ACQUIRER_CODE`, `QRIS_PRINT_VERSION`.
`runtimeConfigFromEnv()` reads the bare name first, so bare names dominate any `.env`
the developer's machine also loads, making the rig identical on both. Give them values
that cannot occur by accident (`E2E-MERCHANT-MUST-NOT-RENDER` and similar) rather than
plausible ones — a distinctive string turns a failure message into a diagnosis.

---

## D-4: Retire the configuration, or leave it unread?

**Decision**: Retire all five from `RuntimeConfig`, `runtimeConfigFromEnv()`, `env.ts`,
`frontend/.env.example`, `docker-compose.yml` and `README.md`.

**Rationale**: The requester chose "drop the name, verify amount only", which removes the
last reader of the fifth value as well as the four that only ever fed the frame. Config
that nothing reads on a payment path is worse than absent: `docker-compose.yml` currently
carries a comment explaining that unset renders "an empty frame rather than a wrong
merchant name, which is the safe direction for a field the guest is told to check before
entering a PIN" — advice about a check that will no longer exist. An operator who reads
that and sets the values carefully has been misled by the repository.

**Alternatives considered**: Keeping the fields on `RuntimeConfig` as deprecated — rejected;
there is no consumer to deprecate for, and the type is small enough that removal is
cheaper than a deprecation cycle. Keeping only `qrisMerchantName` — rejected by the
clarification decision.

**Backward compatibility**: unchanged behaviour for existing deployments. `runtimeConfig()`
merges injected fields over the environment and reads unknown keys as absent; an
environment that still exports the five names is simply not consulted for them. Nothing
throws. FR-013 covers this and D-3's rig proves it.

---

## D-5: What tests the runtime-over-build-time precedence after the QRIS fields go

**Decision**: Re-express `frontend/lib/runtime-config.test.ts`'s three precedence cases on
`API_BASE_URL` alone.

**Rationale**: The QRIS fields are currently the *vehicle* for the mechanism's tests —
"prefers the runtime variable over the build-time one" uses `QRIS_MERCHANT_NAME`, "falls
back to the build-time variable" uses `QRIS_TERMINAL_LABEL`, "treats an empty runtime
variable as unset" uses `QRIS_MERCHANT_ID`. Deleting the fields would silently delete the
coverage of a mechanism this repository considers load-bearing (it is why one image can
be promoted from UAT to production unchanged). `API_BASE_URL` exercises all three paths:
both names set → runtime wins; only `NEXT_PUBLIC_API_BASE_URL` set → build-time value;
`API_BASE_URL=""` with the prefixed name set → empty is unset and the prefixed value is
used; neither set → `FALLBACK_API_BASE_URL`.

**Alternatives considered**: Deleting the precedence tests along with the fields —
rejected; that is coverage loss disguised as cleanup, and the mechanism outlives this
feature. Introducing a test-only config field — rejected; a field that exists only for
its own test proves nothing about the real one.

---

## D-6: The rewritten instruction step

**Decision**: Step 4 of `qris-instructions.tsx` becomes an amount-only check, stating the
formatted amount explicitly and keeping the stop-instruction: check that the app shows the
amount `IDR …`, and if it differs, stop and do not enter the PIN.

**Rationale**: FR-010/FR-011, from the requester's decision. The wording constraint is
that the *exact* amount must appear in the sentence, because the amount check is now the
only in-page verification a guest can perform — "check the amount is correct" gives them
nothing to compare against, while "check your app shows IDR 550.000" does. The component
already receives and formats the amount, so this is a copy change, not a data change.

The `qrisMerchantName` import and the `merchantName ? … : "the merchant name printed above
the code"` fallback both go: the fallback is a direct reference to text the card will no
longer contain, and is the sharpest instance of the problem US2 exists to fix.

**Alternatives considered**: "check the merchant is one you recognise" — kept as optional
softening only; it is not a *check* in the comparative sense, so it may accompany the
amount check but MUST NOT replace it.

---

## D-7: Component test as well as acceptance test

**Decision**: Add `frontend/components/order/qris-panel.test.tsx` pinning the element
inventory, alongside the e2e assertion.

**Rationale**: Principle VIII is explicit that `e2e/` "proves the assembled system, not
each part", and that the lower tiers remain required where they already apply. Three
sibling components in the same directory already carry `.test.tsx` files, so this is the
established convention rather than a new tier. The component test is also the only cheap
place to pin the *ended* states (FR-009): reaching an expired and a cancelled order
through the real API in Playwright is slow, and the panel takes `endedStatus` as a prop.

**Division of labour**: the component test pins what the panel renders given props,
including both ended states. The e2e assertion pins that the assembled page, fed real
retired config, still shows none of the five. Neither subsumes the other.

---

## D-8: Is removing the printed identity permissible under the QRIS scheme?

**Decision**: Yes for this surface, and the question is out of scope for the code change.

**Rationale**: The printed merchant identity block belongs to the *static* QRIS artefact —
the merchant standee or printed card, which a payer must be able to attribute without any
other context. What this page presents is a dynamic, per-transaction code, issued by the
gateway for one order, valid for one payment window, displayed inside the merchant's own
authenticated checkout. The payer's app decodes and displays the merchant identity from
the payload itself, which is unchanged by this feature (FR-008). The frame here is brand
furniture around a live code, not a printed instrument.

**Recorded as an assumption, not a finding**: the spec's Assumptions section states this,
and it is the kind of claim that should be confirmed with the acquirer rather than settled
by a developer. It does not block the work — nothing in this change alters the payload —
but if the acquirer requires printed identity on dynamic on-screen codes, that is a
business constraint that would reverse the decision, and it should be asked.

---

## D-9: Deriving the new frame proportions

**Decision**: Re-derive the frame's internal vertical rhythm from the `203-1201` render at
its native 522 × 735, keeping every measurement expressed as a container-query fraction of
the frame's own width, exactly as the component does today.

**Rationale**: The existing implementation is already built this way, with a comment
explaining why — "every measurement below is a fraction of the frame's own width, taken
from the design (522 × 735), so the whole frame scales as one piece in either column
layout". That property is what keeps the frame correct in both the stacked and the
two-column checkout layout, and it must survive. Removing three text blocks and the
footer changes only which fractions apply, not the technique.

**Verification method**: screenshot the implemented card at 522 CSS px frame width and
compare against the Figma render at the same width. The checkpoints that matter are the
top of the QR, the baseline of `SATU QRIS UNTUK SEMUA`, and the top edge of the red corner
wedge — if those three land, the rhythm between them is right. The step-caption block is
sized to the wedge rather than to its own content (the existing comment warns that a
content-sized block spills onto the white), so it must be re-checked at the narrowest
supported viewport rather than assumed.

**Alternatives considered**: Switching to fixed pixel sizing now that there is less
content — rejected; it would break the two-column layout the container query exists to
serve, for no benefit.

---

## Open questions carried into implementation

1. **Acquirer confirmation on printed identity (D-8)** — worth asking, does not block.
2. **Exact fractional measurements (D-9)** — resolved at implementation time against the
   Figma node, verified by screenshot comparison, not guessable from this document.

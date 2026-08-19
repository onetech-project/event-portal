# Implementation Plan: Bundle Form Labels & End-of-Journey Destination

**Branch**: `019-buyer-form-expiry-redirect` | **Date**: 2026-08-18 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/019-buyer-form-expiry-redirect/spec.md`

## Summary

Two small, unrelated frontend changes to the same pair of order screens.

**Track A — the end-of-journey dialog's destination.** The dialog that opens over an ended
order keeps every behaviour spec 011 FR-022/FR-023/FR-024 gave it; only its single action
changes, from `/` labelled "Return to Home Page" to `/events/{slug}` labelled "Return to
Event Page". The dialog is one shared component used from both order screens, so the change
lands in one file plus a new prop threaded from its two call sites. No automatic navigation
is added: the guest still presses the button (FR-005), which means no `router` call, no
history rewriting, and no new effect.

**Track B — the bundle card's visitor number.** `SlotGroup.unitLabel` and the pass that
computes it are deleted, and with them the span and the tooltip fragment that rendered it.
Nothing else consumes the field, so the removal is total rather than a hidden-but-computed
label.

Neither track touches the Go API, the database, or `api/openapi.yml`. Both are provable
from the existing three-tier test setup, and both break existing assertions on the way in —
which is the starting signal Principle VIII requires.

## Technical Context

**Language/Version**: TypeScript 5.x, React 19, Next.js App Router (frontend only). No Go
change, no SQL change.

**Primary Dependencies**: existing only — Next.js `Link`, Base UI dialog (already wrapped by
`components/ui/dialog`), TanStack Query, Tailwind. **No new dependency is added.**

**Storage**: N/A. No migration, no schema change, `SCHEMA.md` untouched. Verified rather
than assumed: the feature reads nothing new and writes nothing.

**Testing**: frontend Vitest (`frontend/`) and the Playwright acceptance suite (`e2e/`). The
Go tiers are unaffected. Vitest and the Next toolchain are invoked through the local bins
with node prepended to PATH — there is no `test` script in `frontend/package.json` (see
Quickstart).

**Target Platform**: browser, both order screens of the guest purchase journey.

**Project Type**: web application — Go API (`backend/`) plus Next.js frontend
(`frontend/`), with `e2e/` as the whole-system acceptance gate. This change is confined to
`frontend/` and `e2e/`.

**Performance Goals**: none new, and none at risk. Track A swaps one link's `href` and its
text. Track B removes a loop over already-materialised groups and one rendered span. Poll
and countdown cadences are untouched.

**Constraints**:

- **No automatic navigation** (FR-005). The action must stay an ordinary `Link`. Any
  `router.replace`/`push` reaction to an ended order would violate the clarified intent and
  would also contradict FR-001's requirement that the screen stay rendered behind the
  dialog.
- **Exactly one action** (FR-002, FR-009). The dialog must not gain a second control, and in
  particular this is not the deleted "Repeat Order" link returning.
- **Label follows destination** (FR-004). The two must move together; a button naming the
  home page that leads to the event page is the defect this requirement exists to prevent.

**Scale/Scope**: 6 source files, 3 Vitest files, 1 Playwright spec. No new route, no new
component, no new state.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Basis |
|---|---|---|
| I. Modular Monolith | **N/A** | No backend change. No package boundary is crossed. |
| II. Domain Isolation | **N/A** | No Go domain is touched; `cmd/api/architecture_test.go` has nothing to police here. |
| III. DTO Isolation | **N/A** | No DTO, no wire shape, no `api/openapi.yml` change. `TicketOrderDetail` is read exactly as today. |
| IV. Transactional Integrity & Idempotency | **N/A** | No write path. Seat release and the order's status transitions are untouched (FR-010). |
| V. Payment Gateway Abstraction | **N/A** | No gateway interaction. The payment-screen expiry signal already in place is reused unchanged. |
| VI. Guest-First MVP Scope Discipline | **PASS** | No new dependency, no new infrastructure, no new surface. Both tracks reduce code. |
| VII. Cache as a Disposable Read Accelerator | **PASS** | No cache read, write, or invalidation. The destination page is served by an already-cached public read whose behaviour is identical in both modes; nothing here is cache-sensitive, so the `E2E_CACHE_ENABLED=false` run exercises the same paths. |
| VIII. End-to-End Acceptance Coverage | **PASS with required suite changes** | See the gate rows below. |
| IX. Request Throttling | **PASS, with one check** | No throttle is added, removed, or retuned. The new scenarios book orders and so consume the shipped per-IP `Book` allowance shared by the guest specs; the suite runs serially (`workers: 1`), and the additions are three orders across the whole file. Confirmed as a check in Quickstart rather than assumed. |

**Governance-document sync**: none required, and this was verified rather than asserted —
`PRD.md` and `ARCHITECTURE.md` contain no mention of the end-of-journey dialog, its
destination, or the bundle card's visitor numbering. `SCHEMA.md` is untouched because there
is no migration. Spec 011's FR-024 is amended, and that amendment is recorded in this
feature's spec under **Amends**; per-feature specs are historical records and are not
rewritten in place.

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes, two of them, both named
      explicitly in Principle VIII's covered-flow list: **hold expiry** (Track A) and the
      **holder forms** step of the guest purchase journey (Track B). The spec that must
      change is [`e2e/specs/guest-purchase.spec.ts`](../../e2e/specs/guest-purchase.spec.ts).
- [x] **New user-visible flow?** No new flow. Two existing ones change what the guest sees,
      and both changes are asserted in the same commit (three scenarios — see below).
- [x] **Bugfix in a covered flow?** Not a bugfix; this is requested behaviour change. The
      red-first obligation is still met and is not weaker here: six existing assertions go
      red against the unchanged code, listed under *Red-first signal* below, and each must be
      seen failing before the change lands.
- [x] **Behaviour under `E2E_CACHE_ENABLED=false`?** Unchanged. Nothing in either track
      reads or writes the cache. The suite must still be run in both modes, because that is
      how the kill switch stays verified — not because this feature is suspected of
      breaking it.

**Required `e2e/` changes** (all in `guest-purchase.spec.ts`):

1. **Extend** the existing `an expired payment returns the seats to the pool` scenario
   (line 257). It arranges a genuine payment expiry through the signed gateway
   notification and today asserts only quota restoration — it never looks at the screen.
   Add: the dialog is up, it offers exactly one action, that action names the event page,
   and pressing it lands on `/events/{slug}`.
2. **Add** a holder-forms hold-expiry scenario. Book, stay on the forms, let the booking
   hold lapse, then assert the same four things. This is the only *new slow* scenario
   (~35s of wall clock, see research R3) and it is not optional: the forms screen is the
   one Track A surface the extended scenario above never reaches, because that order has
   already started payment.
3. **Add** a two-unit bundle scenario. `selectQuantity(bundle, 2)` is booked nowhere in the
   suite today, so the multi-unit holder form — the *only* shape that ever showed a visitor
   number — is currently assembled only in jsdom. Assert two cards, no visitor number on
   either, then complete the order so the fan-out is proven through the real API.

**Red-first signal** — six existing assertions that must be seen failing before the change:

| File | Line | Assertion that goes red |
|---|---|---|
| `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` | 200 | `toHaveAccessibleName(/return to home page/i)` |
| `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` | 224 | `getByRole("link", { name: /return to home page/i })` → `href` `"/"` (CANCELLED variant) |
| `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` | 943 | `getByText("Visitor 1")` |
| `frontend/app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx` | 944 | `getByText("Visitor 2")` |
| `frontend/app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.test.tsx` | 365 | `actions[0]` → `href` `"/"` |
| `frontend/components/order/slot-groups.test.ts` | 125 | `groups.map(g => g.unitLabel)` → `["Visitor 1", "Visitor 2"]` |

A seventh, `slot-groups.test.ts:146` (`groups[0].unitLabel).toBeNull()`), stops compiling
once the field is deleted; it is removed rather than rewritten, because a field that no
longer exists has no null case to assert.

## Project Structure

### Documentation (this feature)

```text
specs/019-buyer-form-expiry-redirect/
├── plan.md              # This file
├── spec.md              # Feature specification
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   └── README.md        # Phase 1 output — internal UI contracts; no API contract changes
├── checklists/
│   └── requirements.md  # Spec quality checklist
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
frontend/
├── app/(public)/events/[slug]/orders/[orderNumber]/
│   ├── page.tsx                  # Track A: pass the event slug to the dialog
│   ├── page.test.tsx             # Tracks A+B: 4 assertions change
│   └── checkout/
│       ├── page.tsx              # Track A: pass the event slug to the dialog
│       └── page.test.tsx         # Track A: 1 assertion changes
├── components/order/
│   ├── end-of-journey-dialog.tsx # Track A: the whole change — prop, href, label, doc comment
│   ├── slot-groups.ts            # Track B: delete unitLabel + the pass that computes it
│   ├── slot-groups.test.ts       # Track B: 1 assertion changes, 1 test is deleted
│   └── visitor-form.tsx          # Track B: drop the label from GroupTitle; then (FR-015/016)
│                                 #   rewrite it to wrap, deleting the tooltip + useIsTruncated
└── lib/
    └── order-routes.ts           # Track A: add eventDetailPath(slug)

e2e/
└── specs/
    └── guest-purchase.spec.ts    # Principle VIII: 1 scenario extended, 2 added
```

**Structure Decision**: the repository's existing web-application split is used as is —
`backend/` (Go API), `frontend/` (Next.js), `e2e/` (Playwright acceptance gate). This
feature adds no directory and no module. Track A's one architectural choice is *where the
destination is built*: `frontend/lib/order-routes.ts` already owns every address an order
occupies and exists precisely so a path built by hand in one place cannot drift from a
path matched by a regex in another. The event detail path joins it as `eventDetailPath`,
rather than being interpolated inside the dialog, so both order screens and the dialog
resolve one definition. The two screens already interpolate `/events/${eventSlug}` inline
for their error-state "Back to the event" links; folding those onto the same helper is a
two-line tidy-up that removes the drift the helper exists to prevent, and it is listed
separately in tasks so it can be dropped without affecting either track.

## Complexity Tracking

> No Constitution Check violations. Nothing to justify.

Both tracks are net code removals or single-line substitutions, add no dependency, and
introduce no new abstraction. The one new export (`eventDetailPath`) is a three-line
function placed in the module that already owns its siblings.

## Constitution Check — post-design re-evaluation

*Re-run after Phase 1. Nothing in the design moved a verdict; two rows gained evidence they
did not have before the research, and one correction was made.*

| Principle | Before design | After design | What changed |
|---|---|---|---|
| I–V | N/A | **N/A, confirmed** | Phase 1 produced no migration, no DTO, no Go file, and no gateway call. `data-model.md` records the absence explicitly rather than by omission. |
| VI. MVP Scope Discipline | PASS | **PASS, strengthened** | The design adds one three-line exported helper and deletes a field, a derivation pass, a map, a span, and a test. Net removal. |
| VII. Cache | PASS | **PASS, confirmed** | No cache key is read, written, or invalidated anywhere in the design. The `E2E_CACHE_ENABLED=false` run stays a required step regardless — an unexercised mode is an unverified one. |
| VIII. E2E Acceptance | PASS with required suite changes | **PASS, with the arrangement now proven reachable** | Research R3 was the gate. Before it, the forms-screen hold expiry was only *assumed* arrangeable without a forbidden database write. Reading `ListOrdersDueForExpiry` and `SCHEMA.md:130` established that the booking hold and the payment window share `payment_expires_at`, so the existing sweeper expires an untouched hold and the scenario can be arranged entirely through the real system. Had that been false, this row would have failed the gate. |
| IX. Throttling | PASS, with one check | **PASS, check carried into Quickstart** | Design adds three booked orders to a serially-run suite against the shipped per-IP allowance. Carried as an explicit step (Quickstart step 6), not closed on reasoning alone. |

**One correction made during Phase 1**, recorded because it would otherwise mislead whoever
runs the verification: the acceptance suite on this branch lives in `e2e/` and is driven
with `cd e2e && npm test`. The `AGENTS.md` text describing `frontend/__test__/` and
`npm run test:e2e` belongs to a parallel refactor branch (`refractor/fe`, spec 019) that is
not merged here. Quickstart uses the commands that actually work on `fix/buyer` and says so.

**Gate result: PASS.** No violation, so the Complexity Tracking table stays empty.

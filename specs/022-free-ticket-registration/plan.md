# Implementation Plan: Free Ticket Registration Form

**Branch**: `022-free-ticket-registration` | **Date**: 2026-08-20, **revised 2026-08-21** | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/022-free-ticket-registration/spec.md`

## Summary

An invited guest opens `/events/[slug]/register/[ticketId]`, fills one holder form, opens the
event's Terms & Conditions and accepts them, confirms in a review dialog, and is emailed a free
e-ticket. No payment, no fees, no receipt.

The technical approach is **reuse over invention**. A registration is composed from the exact
rows a purchase produces — a zero-total `orders` row at `PAID`, one `order_items` line, one
`attendees` row, one `tickets` row — written by a new `order`-domain service method that mirrors
`bookOnce` and then dispatches issuance and delivery through the same `TicketIssuer` /
`TicketDeliverer` interfaces the payment webhook uses. Because the rows are ordinary, ticket
lookup, the e-ticket PDF, admin lists, ticket validation, resend and the delete guards all keep
working with **no changes to their read paths**.

Three things genuinely change rather than compose:

1. `ticket_types` gains `is_visible` (default TRUE; FALSE means registration-only), which removes a type from every guest purchase
   surface and from package composition.
2. The notification service learns a second delivery shape — e-ticket alone, no receipt — selected
   from a marker on the order.
3. The Terms & Conditions presentation is extracted into one shared component and gains an
   automatic tick when the reader reaches the end, which changes the **existing booking dialog**
   and therefore the guest purchase journey in `e2e/`.

   *(Revised 2026-08-21. This originally read "and gains a read-to-the-end **gate**": the checkbox
   was uncheckable and Agree withheld until the guest reached the bottom. The clarification of
   2026-08-21 removed the gate and kept only the tick — the guest may agree without reading, and
   Agree now follows the checkbox. The extraction is unaffected; what changed is what the
   end-of-document signal authorises. See [contracts/terms-gate.md](./contracts/terms-gate.md).)*

## Technical Context

**Language/Version**: Go 1.26.5 (backend) · TypeScript 5 / React 19.2.4 / Next.js 16.2.12 (frontend)

**Primary Dependencies**: Echo v4.15.4, pgx/v5 v5.10.0, sqlc (generated queries), shopspring/decimal,
go-qrcode · TanStack Query v5, Zod v4, Tailwind v4, base-ui dialog primitives · Playwright ^1.50

**Storage**: PostgreSQL (single source of truth) · Redis (Principle VII read cache only, disposable)

**Testing**: Go unit + database-backed (`backend/scripts/test.sh`), Vitest (`frontend`), Playwright
acceptance suite (`e2e/`) against real app + real API + real Postgres/Redis + Mailpit, with only the
payment gateway substituted at the network boundary

**Target Platform**: Linux server (containerised API + Next.js), modern desktop and mobile browsers

**Project Type**: Web application — Go modular-monolith API + Next.js App Router frontend

**Performance Goals**: No regression to checkout throughput. The registration write path holds the
same quota row lock booking does, so it inherits Principle IV's absolute ban on network and cache
calls between `BEGIN` and `COMMIT`.

**Constraints**: No cache or gateway call inside an order-writing transaction · quota never negative ·
domains never import one another (`cmd/api/architecture_test.go` enforces) · `SCHEMA.md` changes in
the same commit as any migration · suite green with cache on **and** off, and throttling on **and** off

**Scale/Scope**: One new public endpoint, one new admin field, one migration, two new frontend routes,
one extracted shared component, one changed existing dialog, and the `e2e/` scenarios that follow.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence / action |
|---|---|---|
| **I. Modular Monolith** | PASS | No new domain. Registration lives in `internal/order/` — it writes `orders`, `order_items`, `attendees` and deducts quota, which is precisely that domain's existing job. |
| **II. Domain Isolation** | PASS | Quota via the existing `order.EventProvider.CheckAndDeductQuota`; terms via existing `EventProvider.CurrentTerms`; issuance and delivery via **new** narrow interfaces declared *in* `order` and satisfied by adapters in `cmd/api/adapters.go`, exactly as `payment.TicketIssuer` / `payment.TicketDeliverer` already are. No domain gains an import of another. |
| **III. DTO Isolation** | PASS | New `RegisterRequest` / `RegisterResponse` in `order/dto.go`. No `sqlc` struct on the wire, none in the cache. |
| **IV. Transactional Integrity** | ⚠️ **DEVIATION** | Two parts. (a) The single-transaction and no-network-call-inside rules are **honoured** — see Phase 1. (b) The registration writes `PAID` outside a gateway webhook, which the principle forbids absolutely. **Requires amendment. See Complexity Tracking.** |
| **V. Payment Gateway Abstraction** | PASS | The registration path never reaches the gateway. `Gateway` is untouched. |
| **VI. Guest-First MVP Scope** | PASS | Unauthenticated guest flow, no account. Delete guards keep working unchanged because a registration produces real `order_items` and `attendees` rows — the two things the guard already checks. No new Redis use. |
| **VII. Cache as Disposable Accelerator** | PASS | `is_visible` filters *within* already-cached `ticket_types` list surfaces; no new cacheable surface. Invalidation is `InvalidateAfterCommit` after the registration transaction, scoped to `cache.Orders()` and `cache.Event(eventID)` — identical to `bookOnce`. |
| **VIII. E2E Acceptance Coverage** | ⚠️ **WORK REQUIRED** | Both a new flow *and* a change to a covered flow. See the mandatory rows below. |
| **IX. Request Throttling** | PASS with work | New `RATE_LIMIT_REGISTER_*` policy following the existing `ThrottlePolicy` shape, joined to `ratePolicies()` so it inherits validation and the startup report. |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** **Yes.** Changing how the booking Terms
      & Conditions dialog's checkbox behaves changes the guest purchase journey. Specs that must
      change: **`e2e/specs/guest-purchase.spec.ts`**, which needs two scenarios — the auto-tick
      against a long document, and the manual tick with no scrolling.
      `e2e/specs/cache-refresh.spec.ts` and `admin-console.spec.ts` also change — ticket-type lists
      and package composition gain a new field and a new exclusion.
      **`e2e/support/journey.ts` needs no code change** (revised 2026-08-21): its two helpers,
      `agreeToTermsAndBook()` (line 149) and `agreeExpectingRefusal()` (line 182), already scroll to
      the end before pressing Agree, and scrolling still ticks the box. Only the comment inside the
      former, which asserts the checkbox is no longer a control, is now false. The ~14 call sites
      routing through it are untouched.
- [x] **New user-visible flow — which new scenarios cover it?** The registration journey and the
      containment boundary, enumerated in [research.md](./research.md) and
      [quickstart.md](./quickstart.md). At minimum: end-to-end registration → Mailpit assertion of a
      **one**-attachment email → ticket validates at the door; **both** agreement routes against a
      **long** document (auto-tick on scroll, and manual tick with no scrolling); the FR-045a latch;
      a purchasable ticket id refused by the endpoint; a registration-only type absent
      from the guest list and the package picker; a repeat email ACCEPTED (FR-023); quota exhaustion refused.
- [x] **Bugfix in a covered flow?** No. This is a feature plus a deliberate behaviour change — now
      two of them, since 2026-08-21 reverses part of the first before it shipped. Principle VIII's
      red-before-green rule attaches to the **manual-tick** scenario: it is the one that fails
      against the current dialog, whose checkbox carries `onCheckedChange={() => {}}`. The auto-tick
      scenario passes before and after, so it is a regression guard and proves nothing on its own.
- [x] **Behaviour under `E2E_CACHE_ENABLED=false`?** No difference. The new filter is a SQL
      predicate, not a cache concern, and invalidation follows the existing post-commit pattern. The
      suite must still be run in both modes, and in both throttle modes, because the new endpoint is
      throttled.

> ⚠️ **A trap this plan names explicitly, and which the revision does not remove.** Existing e2e
> fixtures author terms as `putTerms(token, event.id, "<p>terms</p>")` — a document far shorter than
> the dialog's reading area. Under FR-014d a non-scrollable document is ticked the instant the modal
> opens, so **every existing scenario stays green without exercising either route**. That is
> precisely the "green because nobody looked" failure Principle VIII forbids. Both new scenarios
> **must** author a long document — the manual one especially, since a short document would tick the
> box before the test could prove the tick came from the click.

## Project Structure

### Documentation (this feature)

```text
specs/022-free-ticket-registration/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── api.md           # HTTP contract for the registration endpoint
│   └── terms-gate.md    # Shared T&C dialog contract (both surfaces; name is historical)
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
backend/
├── migrations/
│   ├── 000016_ticket_type_registration_only.up.sql      # ADD
│   └── 000016_ticket_type_registration_only.down.sql    # ADD
├── internal/
│   ├── event/          # ticket_types: new column through queries, DTOs, admin CRUD,
│   │                   # guest-list exclusion, package-composition refusal
│   ├── order/          # registration_service.go (ADD) — the write path;
│   │                   # dto.go, event_provider.go (interfaces), handler.go (route)
│   ├── ticket/         # UNCHANGED — IssueTicketsForOrder is reused as is
│   └── notification/   # service.go: second delivery shape (e-ticket, no receipt)
├── pkg/config/
│   └── throttle.go     # Register ThrottlePolicy + RATE_LIMIT_REGISTER_*
└── cmd/api/
    ├── main.go         # registerGroup wiring
    └── adapters.go     # order→ticket, order→notification adapters

frontend/
├── app/(public)/events/[slug]/register/[ticketId]/
│   ├── page.tsx                 # ADD — the form
│   └── success/page.tsx         # ADD — the confirmation
├── components/terms/            # terms-viewer.tsx (document + end-of-document signal)
│                                # terms-dialog-shell.tsx (MODIFY — checkbox becomes a
│                                # control again; useReadThroughGate → useTermsAgreement)
├── components/booking/
│   └── terms-dialog.tsx         # MODIFY — consume the shell's new agreed/onAgreedChange
└── lib/                         # queries.ts, api-client, schemas: new endpoint + validators

e2e/
├── support/journey.ts           # comment fix only — the two helpers already scroll
└── specs/
    ├── guest-purchase.spec.ts   # MODIFY — both terms scenarios, long-document fixture
    └── free-registration.spec.ts # ADD — the registration journey + containment
```

**Structure Decision**: The existing Go modular monolith (`backend/internal/<domain>/`) plus the
Next.js App Router frontend, unchanged. The new route sits under the existing plural
`app/(public)/events/[slug]/` segment, the same one every other guest route uses.

*(Reversed 2026-08-20. This originally read as a structural judgement in favour of a **singular**
`app/(public)/event/` segment, justified by the API already using singular — `GET /event`,
`GET /event/:id`, `GET /ticket/:event_id`. That argument still holds on its own terms and is
recorded rather than deleted; it was outweighed by not wanting two sibling top-level segments
differing by one character.*

*The reversal is not free, and the cost is structural rather than cosmetic: under the plural
segment the route inherits `events/[slug]/layout.tsx`, which wraps it in the purchase journey's
countdown and progress rail. That rail names a **Payment** step, on a surface FR-015 forbids from
showing one. FR-010a therefore excludes the registration route inside `EventFrame` — the App Router
gives a nested route no way to opt out of a parent layout, so this is the only mechanism available
at this address.)*

### Post-Phase-1 re-check

Re-evaluated after `research.md`, `data-model.md`, `contracts/` and `quickstart.md` were written.
**No verdict changed.** Four items were sharpened by the design work, and one earlier claim in this
plan was found wrong and corrected:

- **Principle II** — strengthened, not weakened. The design routes the new flag through
  `event.TicketTypeRow → eventProviderAdapter → order.TicketTypeInfo` rather than a direct import,
  and the (now removed) FR-023 duplicate read would have JOINed `ticket_types` because Principle II restricts cross-domain
  JOINs to *write/update* paths; the write still reaches quota only through `CheckAndDeductQuota`.
- **Principle IV** — the no-network-and-no-cache-inside-the-transaction rule is enforced at runtime,
  not merely by review: `pkg/cache` returns `ErrInTransaction` when `db.InTransaction(ctx)` is true.
  An inlining refactor fails loudly. The `PAID` deviation is unchanged and still requires amendment.
- **Principle VII** — one new operational obligation surfaced: cache keys carry no schema or deploy
  version, so a warm entry outlives the deploy and can still serve a registration-only type. A cache
  flush becomes a deploy step (hazard 9). This is a runbook item, not a principle violation.
- **Principle IX** — **correction.** This plan's first draft repeated the constitution's Follow-up
  TODO that no first-party file sets `IPExtractor`. That is stale: `main.go:333-345` sets
  `echo.ExtractIPDirect()` by default and honours `X-Forwarded-For` only from `TRUSTED_PROXY_CIDRS`.
  Spec 018 closed it. The per-IP throttle this feature adds therefore rests on an unforgeable
  identity and may honestly be documented as enforced.
- **Principle VIII** — unchanged in verdict, sharper in scope: the fix centralises in two helper
  methods in `e2e/support/journey.ts`, and the short-fixture trap is now a named requirement rather
  than a warning.

### Revision 2026-08-21 — the read-through gate removed

A seventh `/speckit-clarify` session reversed part of this plan's own design before it shipped: the
gate is gone, the checkbox is a control again, and Agree follows the checkbox. Four clarifications
settled the replacement (spec Clarifications, Session 2026-08-21). Re-evaluated against the amended
spec, **no Constitution Check verdict changed** — the reversal is confined to one component's
interaction model and touches no principle. What did change:

- **Principle VIII** — same verdict, different evidence. The claim that `e2e/support/journey.ts`
  must change is now **wrong** and has been corrected above: its helpers scroll and press Agree,
  which still works. The real obligation moved to adding a *manual-tick* scenario, because that is
  the only one that fails against the current dialog and therefore the only one that can be seen red
  first. A task list that ships the auto-tick scenario alone would satisfy the letter of FR-048 and
  none of its purpose.
- **Scope shrank, and the sequencing improved with it.** The 2026-08-20 design's widest-blast-radius
  change — inverting the checkbox from input to indicator — is withdrawn, so the booking dialog ends
  up closer to where it started than the previous plan had it. `terms-viewer.tsx` needs **no change
  at all**: it reports one fact and never owned what the fact authorised, which is precisely why the
  reversal is cheap. The whole revision lands in `terms-dialog-shell.tsx`, one hook rename, and the
  two callers.
- **Verification got easier, once.** The manual route and the FR-045a latch need no layout, so they
  are unit-testable in a way the gate never was (D10). That is worth spending: the latch is the
  property most likely to be lost to a later refactor, and it can be pinned in Vitest cheaply.
- **One new client obligation** — FR-051's automatic re-presentation of a superseded document. It is
  the only place in this feature where the app opens the terms modal rather than the guest, which is
  hazard 14.

**Not re-opened:** the two Principle IV / Critical-Data-Flow deviations in Complexity Tracking. They
concern the `PAID` write path and the receipt attachment and are untouched by anything in this
revision. The amendments are still required first.

## Implementation hazards worth naming in the plan

Full detail and evidence in [research.md](./research.md). These are the ones that would produce a
feature that looks finished and is not:

| # | Hazard | Where it bites |
|---|---|---|
| 1 | **`UpdateTicketType` is an absolute full replace.** An admin PUT omitting `is_visible` would HIDE the type — off sale and free to register for — on an edit of any unrelated field. Polarity made this worse than the original hazard, so the request field is a pointer and absent means visible. | D16 |
| 2 | **Enforce FR-008 at `TicketTypeForCheckout`/`expandTicket`, not in `Book`.** Otherwise `POST /ticket/availability` still answers "available", and the refusal lands *after* the T&C dialog instead of before it. | D15 |
| 3 | **Do not filter `ListTicketTypeIDsByEventID`.** It feeds the `DeleteEvent` guard; filtering it turns a required `400` into a raw FK constraint violation. | D17 |
| 4 | ~~**The duplicate-email check does not serialise on its own.**~~ RETIRED: FR-023 removed the rule and the advisory lock with it. The inverse is now the hazard — a lock left behind would silently serialise two unrelated registrations. | D7 |
| 5 | **The scroll path cannot be tested in Vitest.** happy-dom returns `0` for scroll metrics and registers *inert* `IntersectionObserver`/`ResizeObserver` stubs, so a test passes while asserting nothing. The manual route and the FR-045a latch *are* unit-testable — driving `onReachedEnd` as an input needs no layout — and should be pinned there. | D10 |
| 6 | **The `tabIndex={0}` on the scroll region now looks like dead weight.** It is what lets `End`/`PageDown` reach the region (FR-046). A reviewer who knows the guest can just tick the box may delete it; the severity dropped from "cannot consent" to "loses the auto-tick", but the requirement did not. | [terms-gate.md §7](./contracts/terms-gate.md) |
| 7 | **`orders.buyer_email` must be on the INSERT.** `UpdateOrderBuyer` is guarded on `PENDING` and cannot patch an order inserted at `PAID`; an empty snapshot makes delivery fail silently after the guest was already told it succeeded. | D5, [data-model.md §2.1](./data-model.md) |
| 8 | **The email *body* renders money, not just the attachments.** FR-036 needs the body branch too, or a registrant receives "Total Payment IDR 0". | D5 |
| 9 | **A warm Redis entry outlives the deploy** and keeps serving registration-only types until a write or the TTL. SC-004 needs a cache flush as a deploy step. | D19 |
| 10 | **`SiteNav` hides on `pathname.startsWith("/events")`** ([site-nav.tsx:35](../../frontend/components/layout/site-nav.tsx#L35)) — the singular `/event` segment does not match, so the registration page would render the site nav that every purchase page hides. |  |
| 11 | **`sqlc generate` is a zero-diff no-op** until a query names the column. A task asserting otherwise is unsatisfiable. | D21 |
| 12 | **The auto-tick must latch, not track (FR-045a).** A refactor that "re-syncs the checkbox with the scroll position" re-ticks a box the guest deliberately unticked — an automatic action silently overriding a deliberate one, about consent. The shipped `firedRef` latch in `TermsViewer` already does the right thing; the hazard is losing it. | [terms-gate.md §2.1](./contracts/terms-gate.md) |
| 13 | **`onCheckedChange={() => {}}` must be replaced, not supplemented.** It is what currently makes the box unclickable. Adding an `onClick` elsewhere while leaving it produces a checkbox that answers a mouse and not a keyboard `Space` — FR-046's failure in a new costume. | [terms-gate.md §7](./contracts/terms-gate.md) |
| 14 | **FR-051 now requires the client to drive the dialog.** On `TERMS_CHANGED` the form must clear its checkbox and reopen the document *itself*, not wait for a click. Every other modal opening in this feature is guest-initiated, so this is the one inverted case and the one a reader will implement backwards. | [terms-gate.md §5](./contracts/terms-gate.md) |

## Complexity Tracking

> Two deviations from a NON-NEGOTIABLE principle, both decided deliberately and recorded in the
> spec's *Constitution Deviations* section. **Neither may be implemented before the constitution is
> amended in the same change**, together with `PRD.md`, `ARCHITECTURE.md` and `SCHEMA.md` as
> Governance requires. `/speckit-tasks` must sequence the amendment first.

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| **Principle IV** — an order reaches `PAID` without a gateway webhook | Ticket issuance, ticket lookup, e-ticket rendering, resend, admin listing and validation are all keyed on `PAID`. `notification.SendTicketEmail` refuses anything else outright ([service.go:173](../../backend/internal/notification/service.go#L173)). | A new terminal status (`REGISTERED`) was considered and rejected: it would require touching every one of those read paths and leave a second "issued" state each future path must remember. The cost accepted instead is that `PAID` stops meaning "a gateway confirmed money moved" — so the amendment must redefine that invariant, keep webhook-only absolute for orders that *have* a payment session, and FR-033's stored marker must keep the two origins distinguishable. |
| **Critical Data Flow Rules** — an e-ticket email sent without the mandatory receipt attachment | A receipt for a zero-amount registration is a proof of payment for a payment that never happened. | Sending a zero-value receipt was rejected as actively misleading. The rule is really about *paid* orders; the amendment scopes it that way. The supporting constraint that the e-ticket PDF carries no monetary figure is already satisfied, which is what makes the separation clean. |

*No third violation is claimed.* The transaction rules, domain isolation, cache rules and throttling
rules are all satisfied by composition rather than exception — see the Constitution Check table.

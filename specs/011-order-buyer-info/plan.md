# Implementation Plan: Order Page Buyer Information — Per-Ticket Holder Forms

**Branch**: `feat/buyer` (spec directory `011-order-buyer-info`) | **Date**: 2026-08-19 (rev. 6 — restoring saved holder details) | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/011-order-buyer-info/spec.md`

## Summary

FR-001 – FR-021 are **delivered** (tasks T001–T030, commits `e189e19`…`01ce8fe`): the buyer card is gone, holder forms are the only identity forms, the topmost holder is the primary contact, phone is a digits-only free-text field validated on length alone, the summary and rail behave, the QR screen has its own address, and delivery settled at exactly one email to the buyer (constitution v3.0.0). Migration `000012` shipped with it.

Rev. 3's two tracks are **also delivered** (tasks T031–T051), and are retained below as the design record:

**Track A — End-of-journey modal (FR-022 – FR-024).** Frontend only, no API change. Today an EXPIRED or CANCELLED order replaces the entire screen with a full-page "Time's Up" card, destroying the context the guest most needs at that moment. Both order screens instead keep their own layout rendered and raise one unclosable Base UI dialog over it — no X, no Escape, no backdrop dismissal, page behind inert — rebuilt to Figma `293-3` with a single "Return to Home Page" action. On the QR screen it opens the instant the client countdown reaches zero, replacing the inline expired notice.

**Track B — Schema revision (FR-025 – FR-029).** Backend + admin, invisible to guests. One new migration `000013`: both master lists move from uuid keys to integer identity with widened names and `created_by`/`updated_by` audit columns; `orders.status` becomes `status_id` with the name mapped back inside SQL so not one Go comparison changes; `attendees.gender_id` converts uuid → smallint through its old key; `packages.status` becomes an `is_active` boolean that surfaces on the admin wire as a toggle. `tickets.status` is explicitly untouched.

**Track C — Form-step fee presentation (FR-016, FR-016a – FR-016c), added 2026-08-12.** Frontend only, no API change, no backend change of any kind. The form-filling step's Order Summary shows the fee-inclusive `total_amount`; it must show the pre-fee `subtotal`, and its "Includes all taxes and fees" note must become a statement that fees are added at the next step. Both values already arrive on the same payload, so this is a render change at two lines of one component. Constitution **v4.0.0** was amended for it — the only track of the three that needed an amendment, and the amendment is already landed.

Rev. 4's Track C is **also delivered** (tasks T052–T060): the form-filling step renders the pre-fee `subtotal` behind an unchanged "Total Payment" heading, the note states fees arrive at the next step, and the e2e scenario arranges a real fee so the assertion is not vacuous (research R27).

Rev. 5 added two tracks from the **2026-08-13 clarification session**:

**Track D — Phone floor 10 → 12 (FR-006).** Backend + frontend, no schema change. The validator narrows from `^[0-9]{10,15}$` to `^[0-9]{12,15}$` on both sides of the wire, and the one canonical error string moves with it. Length alone; no prefix branch is introduced, so the floor is counted on the value exactly as typed — an 11-digit local number is refused in `08…` form and accepted as `628123456789`. The typing mask keeps its 15-digit ceiling and gains no floor. **Nothing at rest is re-validated**: `attendees.phone` stays `VARCHAR(50)` with no CHECK, and stored 10- and 11-digit numbers remain valid, displayable, and gateway-deliverable permanently (research R29).

**Track E — FR-013 / FR-014 amend to the shipped panel.** No production change at all. The summary card was hand-edited to Figma `206-3145` — no Booking ID, no per-unit price on the ticket line — and the spec has now been amended to match rather than the panel restored. The work is converting the two suspended comment blocks in `page.test.tsx` and `checkout/page.test.tsx` into live negative assertions, deleting the stale NOTE in the component, and clearing the "open disagreement" record from the quality checklist (research R30).

Rev. 6 adds one further track from the **2026-08-19 clarification session**, and Tracks D, E and F are the outstanding work in this feature:

**Track F — Restoring saved holder details (FR-030 – FR-033), added 2026-08-19.** Frontend, plus one narrowly-scoped acceptance change on the checkout endpoint. No schema change, no migration, no response-shape change. A guest whose payment fails is told by the API — in those words — that their details are saved, and is then returned to a screen with every field blank. The details really are saved: TX-D commits before the gateway is called, and a gateway failure compensates nothing ([service.go:383, :504](../../backend/internal/order/service.go)). The forms discard them because `visitor-form.tsx:125` hardcodes empty `defaultValues` on the reasoning *"the server holds nothing to prefill from"* — true under spec 008's Option B, and invalidated by this feature's own checkout call. The fix is to seed `defaultValues` from `order.slots`, which the page already fetches (research R31, R32). Two consequences carry real design weight: the seed must happen **once** and never re-sync, because the order is re-read on every window focus and a `reset()` effect would wipe a guest's typing on their way back from their banking app (R33); and a restored gender that has since been retired must be accepted back by the checkout, which today refuses it and would write a zero-value foreign key (R34).

**Neither Track D nor Track E needs a constitution amendment, and neither does Track F.** v4.0.0's Critical Data Flow Rules govern the panel's *money figures* (Track C's territory) and say nothing about phone format, the Booking ID, or a per-unit price. Track D is the first change in this feature to *narrow* a validation rule, which is why its at-rest question needed answering before it could be planned.

## Technical Context

**Language/Version**: Go 1.24+ (backend), TypeScript / Next.js App Router (frontend)

**Primary Dependencies**: Backend — Echo v4, pgx, sqlc-managed SQL in `internal/*/queries/*.sql`. Frontend — React Hook Form + zodResolver, Zod, TanStack Query, TailwindCSS, and **Base UI** `@base-ui/react` (the `dialog.tsx` wrapper in `components/ui/` is over Base UI, not Radix — verified against the installed package; Track A depends on its `modal` / `disablePointerDismissal` semantics).

**Storage**: PostgreSQL. **Track D: none, and that is a requirement rather than a convenience** — the 2026-08-13 clarification makes stored short numbers valid at rest permanently, so `attendees.phone` keeps `VARCHAR(50)` with no CHECK. Adding one is the failure mode: it would reject exactly the legacy rows the clarification protects, and would fail at migration time on any environment holding real orders. **Track E: none.** Track A: none. Track C: none — `subtotal` and `fees` have been on `GET /ticket/order/:order_id` since migration `000010`, so Track C reads a field the wire already carries (research R25). Track B: **one migration** — `000013_master_list_identity_and_flags.{up,down}.sql`. `000012` is left exactly as committed (research R18) because it is already applied in environments built from this branch; editing an applied migration makes `schema_migrations` lie.

**Testing**: Go table tests per domain (`cd backend && ./scripts/test.sh ./...`). Frontend vitest 4.1.10 — **no `test` script in `package.json`**; invoke `./node_modules/.bin/vitest run` directly, node is not on PATH (project memory `frontend-toolchain-invocation.md`). Track A rewrites the two "Time's Up" tests at `page.test.tsx:157` and `checkout/page.test.tsx:236` — both currently assert a `repeat order` link that FR-024 deletes, so they fail until updated. Track B's regression surface is the existing suites: nothing should change behaviour, which is the point. Track C breaks exactly one assertion (`page.test.tsx:541`, `/includes all taxes and fees/i`) and — more importantly — is **not currently provable by any test**, because every fixture that reaches the panel sets `subtotal === total_amount` and the e2e database has no fees at all (research R27). Every Track C test is therefore written against a fixture where the two figures genuinely differ, or it asserts nothing. **Track D repeats that trap exactly**: the e2e holder fixture `081298765432` (`guest-purchase.spec.ts:98`) is 12 digits and passes under both the old floor and the new one, so no existing test goes red on its own and a scenario that merely books an order proves nothing. Its acceptance scenario must drive an **11-digit** value and be seen failing against `{10,15}` first. The error string is asserted verbatim in five places (`checkout_forms_test.go:164`; `page.test.tsx:600`, `:621`, `:629`; plus the two source constants) and all must move together, or the suite reports a copy mismatch instead of a rule change. **Track E's signal is the inverse** — nothing goes red, because no production code changes; its whole deliverable is that two suspended comment blocks become assertions.

**Target Platform**: Linux server (Docker Compose) + modern browsers (guest flow is mobile-first responsive)

**Project Type**: Web application — Go modular-monolith backend + Next.js frontend, one release train

**Performance Goals**: Unchanged. Track B keeps the expiry sweeper's partial index hot by rebuilding `idx_orders_payment_expiry` on the literal seeded id (research R20).

**Scale/Scope (rev. 5)**: Track D — 2 source files (one Go, one TSX), 2 test files, 1 e2e spec; zero migrations, zero SQL, zero DTO shape changes. Track E — 0 source files, 2 test files, 1 comment deletion, 1 checklist note.

**Scale/Scope (rev. 6)**: Track F — 4 frontend source files, 3 Go files, 1 new SQL query under `sqlc generate`, ~4 test files, 1 e2e spec + 1 e2e helper. **Zero migrations, zero schema changes, zero response-shape changes.**

**Storage (rev. 6)**: **None, and no new column.** Every value Track F restores is already stored and already returned — `ListAttendeeSlotsByOrderID` selects `name`, `email`, `phone`, `dob` and the gender NAME today, and `TicketOrderSlot` already types all five ([data-model.md](data-model.md) §9). The single storage-adjacent change is a new read: checkout stops sharing `ListActiveGenders` with `GET /ticket/genders` and gains `ListGenders` (`id, name, is_active`), because the endpoint must keep offering active genders only while the checkout must be able to resolve a retired one. `genders` keeps the shape §7.1 gave it.

**Testing (rev. 6)**: **Track F is the third instance of the R27 trap, and the most deceptive of the three.** Nothing goes red on its own. Every holder-form fixture in the suite has empty slots, so "the forms are empty" is simultaneously what the tests assert, what unfixed code does, and what fixed code must still do for an unsubmitted order. `page.test.tsx:482` asserts every input is empty on a two-slot fixture and is the assertion holding the defect in place: it needs an explicit empty-slots fixture to keep covering FR-030's last clause, plus a new filled-slots case beside it. The e2e gate needs an order whose forms saved and whose payment did not start, which a successful checkout cannot reach (`payment_started` flips true and the page forwards to `/checkout`); `POST /__stub/fail-next-session` ([gateway-stub.ts:80](../../e2e/support/gateway-stub.ts)) reaches it exactly, and **no spec calls it today** — the helper is part of this track (research R35). The clobber rule (FR-032) needs a test of its own that re-renders with a changed order object and asserts the fields do not move, because nothing a user can see distinguishes a correct implementation from one that re-syncs until someone is mid-type.

**Constraints**: Constitution **v4.0.0**. Tracks A, B, **D and E** need no amendment; Track C required one and **already has it** — the fee-presentation bullet was redefined on 2026-08-12 (3.4.0 → 4.0.0, MAJOR, because a reversal is a backward-incompatible redefinition). DTO isolation (Track B's whole design is "storage moves, wire does not"); domain isolation (no domain imports another's repository; the name↔id mapping lives in SQL, not in a shared Go map that would cross domains); SCHEMA.md must be updated in the same change as any schema change (Technology Stack Requirements) — Track C triggers none of that, having no schema change.

**Scale/Scope**: Track A — 3 frontend files + 2 test files. Track B — 1 migration, **2** domains' `queries/*.sql` under `sqlc generate` (`order` and `event` — verified as the only two whose SQL touches the affected tables), ~4 event Go files, 3 frontend admin files, plus SCHEMA.md. Track C — **1 component + 2 call sites**, 3 test files, 1 e2e spec, 1 e2e helper. Zero backend files, zero migrations, zero SQL.

## Constitution Check

*GATE: Tracks A–E were evaluated against constitution **v4.0.0**; rev. 6 re-evaluates Track F against **v4.2.0**, the current text — it has gained Principle IX and a throttling clause on Principle VIII since rev. 5, both of which bear on Track F's e2e scenario and are answered in the rev. 6 rows below. Tracks A, B, D and E require no amendment — v4.0.0's Critical Data Flow Rules govern the panel's money figures, ticket generation and delivery, quota, admission windows, and cache coherence, and were re-read on 2026-08-13 against both new tracks: no bullet mentions phone format, the Booking ID, or a per-unit price. Track C required one and it is already landed — the fee-presentation bullet was redefined on 2026-08-12, so Track C is written against the amended rule rather than proposing one.*

| Principle | Verdict | Evidence |
|---|---|---|
| I — Modular Monolith | PASS | Track A is frontend-only. Track B stays inside `internal/order`, `internal/event`, and the `queries/` + `migrations/` sources sqlc generates from. No new packages, no new domains. |
| II — Domain Isolation | PASS — and the blast radius is smaller than it looks | A survey of `internal/*/queries/*.sql` found **only `order`'s SQL touches the `orders` table at all**; `payment/queries/payment.sql` names `orders.status` in a comment only, and `ticket/queries/ticket.sql` never references `orders`. `payment`, `ticket`, and `notification` receive order state through order-domain adapters (`OrderRef`, `OrderDelivery`, wired in `cmd/api/adapters.go`), so R19's SQL-side mapping means they keep receiving the status **name** with no change of any kind. No domain imports `order`'s repository, and no shared Go enum map is introduced that would couple them. `order_statuses`/`genders` are joined only within the order domain's own statements — the same intra-database referential integrity `000012` established. |
| III — DTO Isolation (rev. 5) | PASS | Track D changes a **validation rule**, not a shape: `CheckoutFormsRequest`'s `attendees[i].phone` keeps its type, its JSON name, and its position, and no response DTO is touched. Track E changes no contract at all — [contracts/fee-presentation.md](contracts/fee-presentation.md) §5 documents the panel as it already ships, and explicitly records that no field is dropped from `GET /ticket/order/:order_id`: the unit price leaves the *display*, not the payload. |
| IV — Transactional Integrity (rev. 5) | PASS | Track D validates before TX-D opens, exactly where the current phone check already runs; no transaction boundary, gateway call, or quota path moves. Track E touches no code. |
| III — DTO Isolation | PASS | Track B's governing constraint. FR-026 keeps `PENDING`/`PAID`/`CANCELLED`/`EXPIRED` on the wire while storage moves to `status_id`; the `TicketOrderDetail`, admin order, and SSE shapes are byte-identical. The one deliberate wire change — `packages.status` → `is_active` (FR-029) — is a DTO edit in `internal/event/admin_dto.go`, not a leaked sqlc struct. |
| IV — Transactional Integrity & Idempotency | PASS | No track touches booking TX-B, checkout TX-D, the gateway-call-after-commit rule, the webhook idempotency short-circuit, or `email_sent`. Track A's countdown-zero rule is explicitly client-side: the server's expiry sweep is unchanged and its later status arrival is a no-op on screen (contract §6). Track C touches no transaction at all — the totals it chooses between are both frozen at booking, before it ever runs. |
| V — Payment Gateway Abstraction | PASS | `Gateway` interface untouched; no payment code changes in any of the three tracks. Track C explicitly leaves the gateway's gross amount reading `total_amount` — asserted, not assumed, in [contracts/fee-presentation.md](contracts/fee-presentation.md) §3. |
| VI — Guest-First MVP Scope | PASS | Track A simplifies a guest dead end. Track B is corrective normalization, adding no capability — and master-list CRUD is **deliberately not built** (research R22), which is where the scope pressure would otherwise be. Track C removes a claim the screen could not support rather than adding a surface. |
| VIII — e2e Acceptance Gate (rev. 5) | **PASS only if Track D ships its scenario** | Holder-form validation is a covered flow (`e2e/specs/guest-purchase.spec.ts`), and Track D changes it, so the gate binds identically to Track C's. The trap is also identical and is why this verdict is conditional: the existing holder fixture is 12 digits, so **the whole suite stays green against unfixed code** and a naive scenario would prove nothing (research R29). The scenario MUST drive an 11-digit value through the real form and MUST be observed red under `{10,15}` before the regex moves. Track E needs no e2e scenario — it changes no behaviour, and its gate obligation is discharged at the component-test level by replacing two suspended comments with assertions. |
| VIII — e2e Acceptance Gate (rev. 4) | PASS — delivered | The form-filling step is a covered flow, so the gate binds: a change to it arrives with `e2e/specs/` updated in the same change. There is a trap here, which R27 records — the suite TRUNCATEs `fees` (`e2e/support/db.ts:44-45`), so every e2e order has `total_amount == subtotal` and a naive assertion would pass identically against fixed and unfixed code. The scenario MUST arrange a fee through the real admin API first (`POST /api/v1/admin/fees`), never by writing the table, per AGENTS.md. It MUST be seen failing against unfixed code. |
| Critical Data Flow Rules | PASS | The one-email-to-the-buyer rule, quota semantics, and ticket generation are untouched by all three tracks. Track A removes a duplicate expiry message; it does not change when an order expires. **Track C is the fee-presentation bullet**, and it implements the v4.0.0 text exactly: pre-fee subtotal on the form step, no fee-inclusive figure before the awaiting-payment step, the replaced note, the shared "Total Payment" heading, and the null-subtotal fallback. The bullet's display-only clause is the constraint Track C is most at risk of breaking, so it is asserted directly — SC-005a and quickstart Scenario J check the charged amount is unchanged, not merely that the screen looks right. |
| Critical Data Flow Rules — cache coherence | PASS | Track C writes nothing and invalidates nothing. It changes which field an already-cached read is rendered from; the order read itself is unchanged, so no cache key, scope, or invalidation path is touched. |
| Governance sync (rev. 5) | PASS — no document edit required | Neither track changes the schema, so the "SCHEMA.md in the same change" clause does not fire — and for Track D that is a positive finding rather than an omission: adding a CHECK would have triggered it, and the at-rest clarification forbids one. `PRD.md` and `ARCHITECTURE.md` describe neither phone length nor summary-card contents. The one document that **does** need an edit is not a governance document: [checklists/requirements.md](checklists/requirements.md) still records the FR-013/FR-014 disagreement as open, and Track E closes it. |
| Governance sync | **PASS with a required follow-up (Track B only)** | Track B changes the schema, so the Technology Stack clause ("Any schema change MUST update `SCHEMA.md` in the same change") binds: SCHEMA.md must carry migration `000013`, the two master-list tables' new shape, `orders.status_id`, `attendees.gender_id`'s new type, and `packages.is_active` **in the same commit**. ARCHITECTURE.md and PRD.md need no edit — no flow or product-scope change. Track C's own governance sync is already discharged: the v4.0.0 report verified `grep -i fee` returns nothing in ARCHITECTURE.md or PRD.md and that SCHEMA.md's `fees`/`order_fees` tables are untouched by a display rule. |
| I — Modular Monolith (rev. 6) | PASS | Track F is frontend plus `internal/order` only. No new package, no new domain, no new endpoint. |
| II — Domain Isolation (rev. 6) | PASS | The gender master list is read by the order domain's own SQL, exactly as today; `ListGenders` joins nothing across a domain boundary and no other domain learns of it. No domain imports another's repository. |
| III — DTO Isolation (rev. 6) | PASS | **The governing constraint, and it holds in both directions.** `GET /ticket/order/:order_id` is asserted byte-identical — every field Track F restores already ships ([contracts/form-restore.md](contracts/form-restore.md) §1), so the fix adds no field and renames none. `CheckoutFormsRequest` keeps its shape too: `attendees[i].gender` keeps its name, type and position, and only the set of *accepted values* widens (§3). |
| IV — Transactional Integrity (rev. 6) | PASS | No transaction boundary moves. The new slot-allowance check runs beside the existing `matchVisitorsToSlots` refusal, before TX-D opens; no cache call and no gateway call is introduced anywhere, let alone inside a transaction. The restore itself writes nothing. |
| V — Payment Gateway Abstraction (rev. 6) | PASS | The `Gateway` interface is untouched. Track F changes what the screen shows *after* a gateway failure, not the failure handling — the 502 path and its message stay exactly as they are, and [contracts/form-restore.md](contracts/form-restore.md) §5 asserts it. |
| VI — Guest-First MVP Scope (rev. 6) | PASS | It removes a re-typing tax from the guest's retry path and adds no surface — FR-033 explicitly forbids adding even a notice. |
| VIII — e2e Acceptance Gate (rev. 6) | **PASS only if Track F ships Scenario M** | The holder forms are a covered flow and this changes them, so the gate binds as it did for Tracks C and D — and the trap is the same one a third time, in its most deceptive form. Every existing fixture has empty slots, so **the whole suite stays green against unfixed code** and even a well-meaning scenario proves nothing unless it first *saves* details and then returns. The arrangement must force a gateway failure through `/__stub/fail-next-session` and be observed red before the fix (research R35). Per AGENTS.md the details are saved through the real checkout call, never written into `attendees` by the test. **v4.2.0 adds a second obligation this scenario is unusually exposed to**: the suite must pass with throttling enabled *and* disabled, and throttle scenarios must be isolated so one draining an allowance cannot refuse an unrelated one. Scenario M is the **first scenario in the suite to call `POST /ticket/checkout/:order_id` twice for one order**, and `RATE_LIMIT_CHECKOUT` defaults to rate `0.2`/s with burst `3` — two of three consumed, refilling one per five seconds. It must therefore book its own order, run under its own client identity, and not share a worker's allowance with another checkout-calling scenario. |
| IX — Request Throttling (rev. 6) | PASS — no throttle changes, one scenario obligation | Track F adds, removes and retunes no throttle: `RATE_LIMIT_CHECKOUT` keeps its policy, and no new endpoint means no new limiter. The principle's relevance here is entirely through Principle VIII's acceptance clause above — Scenario M's second checkout call is real traffic against a real allowance, so the scenario is written to survive `E2E_RATE_LIMIT_ENABLED` in both states rather than assuming the throttle is off. |
| Critical Data Flow Rules (rev. 6) | PASS | Quota, ticket generation, the one-email-to-the-buyer rule, admission windows and payment state are all untouched. The fee-presentation bullet is untouched: Track F changes no money figure. |
| Critical Data Flow Rules — cache coherence (rev. 6) | PASS | Track F writes nothing and invalidates nothing. It renders fields already present in a response the page fetches today, so no cache key, scope or invalidation path is involved. Verified against the E2E_CACHE_ENABLED=false requirement: with the cache off the behaviour is identical, because no cached read is consulted. |
| Governance sync (rev. 6) | PASS — no document edit required | No schema change, so the "SCHEMA.md in the same change" clause does not fire — and as with Track D that is a positive finding: the temptation to "finish the job" here is a column or a constraint, and §9 records that none is needed. `PRD.md` and `ARCHITECTURE.md` describe neither form seeding nor gender lifecycle. **One non-governance document does need editing in the same change**: spec `008-e2e-purchase-flow` asserts empty-on-revisit in four places (`contracts/booking-flow.md:78`, `contracts/api.md:133`, `quickstart.md:122`, `data-model.md:151`), and a superseded rule left standing in a contract document is precisely what let this behaviour survive to be reported. |

**Post-Phase-1 re-check (rev. 6)**: PASS — [research.md](research.md) R31–R35, [data-model.md](data-model.md) §9, [contracts/form-restore.md](contracts/form-restore.md), and quickstart Scenarios M–O introduce no new violation: no endpoint, no migration, no schema change, no cross-domain access, no DTO shape change, no outstanding amendment. Two conditional items are carried into [Complexity Tracking](#complexity-tracking) rather than left in a comment — Principle VIII on Scenario M, and the FR-032 clobber rule, which is a design constraint a reasonable implementation breaks by accident.

**Post-Phase-1 re-check (rev. 5)**: PASS — [research.md](research.md) R29–R30, the narrowed phone rule in [data-model.md](data-model.md) §2, [contracts/checkout-and-delivery.md](contracts/checkout-and-delivery.md) §1, [contracts/fee-presentation.md](contracts/fee-presentation.md) §5, and quickstart Scenarios K–L introduce no new violation: no endpoint, no schema change, no cross-domain access, no DTO shape change, no outstanding amendment. The single conditional verdict is Principle VIII on Track D — a task obligation, carried into [Complexity Tracking](#complexity-tracking) below so it cannot be quietly dropped, exactly as Track C's was.

**Post-Phase-1 re-check (rev. 3/4)**: PASS — [research.md](research.md) R17–R24 (rev. 3) and R25–R28 (rev. 4), [data-model.md](data-model.md) §7 and §8, and [contracts/schema-revision.md](contracts/schema-revision.md) + [contracts/fee-presentation.md](contracts/fee-presentation.md) introduce no new violation. No new endpoint, no cross-domain repository access, no outstanding constitution amendment; Track B's single governance obligation (SCHEMA.md) is named and owned, and Track C's is discharged. The one conditional verdict is Principle VIII on Track C, which is a task obligation rather than a design defect — it is carried into [Complexity Tracking](#complexity-tracking) so it cannot be quietly dropped.

## Project Structure

### Documentation (this feature)

```text
specs/011-order-buyer-info/
├── plan.md              # This file (rev. 6)
├── research.md          # Phase 0 — R1–R16 (delivered) + R17–R24 (rev. 3)
│                        #   + R25–R28 (rev. 4) + R29–R30 (rev. 5) + R31–R35 (rev. 6)
├── data-model.md        # Phase 1 — §1–6 (migration 000012) + §7 (migration 000013);
│                        #   §2's phone rule narrowed to {12,15} (rev. 5, no schema change);
│                        #   §9 restoring saved details (rev. 6, no schema change)
├── quickstart.md        # Phase 1 — scenarios A–F (delivered) + G–I (rev. 3) + J (rev. 4)
│                        #   + K (phone floor, incl. legacy at rest) + L (panel contents)
│                        #   + M–O (restore, no-clobber, retired gender) (rev. 6)
├── contracts/
│   ├── checkout-and-delivery.md   # delivered — API deltas vs specs 008/010;
│   │                              #   §1 phone rule narrowed to 12-15 (rev. 5)
│   ├── schema-revision.md         # rev. 3 — package wire break; everything else asserted unchanged
│   ├── fee-presentation.md        # rev. 4 — UI contract per phase; API asserted byte-identical
│   │                              #   + §5 panel contents outside the money figures (rev. 5)
│   └── form-restore.md            # rev. 6 — screen contract + the one checkout
│                                  #   acceptance change; both reads asserted unchanged
└── tasks.md             # Phase 2 (/speckit-tasks) — T001–T060 delivered (rev. 1–4);
                         #   T061–T072 Tracks D & E delivered (rev. 5);
                         #   T073–T090 Track F, phases 18–19 (rev. 6) — outstanding
```

### Source Code — Track F (restoring saved holder details)

```text
frontend/
├── components/order/
│   ├── slot-groups.ts                 # SlotGroup gains the seed values, read from
│   │                                  # the group's FIRST slot (a unit's slots are
│   │                                  # written identically by the fan-out, so no
│   │                                  # tie-break is specified — spec Assumptions)
│   ├── slot-groups.test.ts            # grouping now carries values: assert the seed
│   │                                  # travels with the group, incl. the pre-010
│   │                                  # one-slot-per-card shape
│   ├── visitor-form.tsx               # :125 defaultValues "" → seeded from the group
│   │                                  # (delete the stale Option B comment, do NOT
│   │                                  # add a reset() effect — FR-032, research R33);
│   │                                  # new isoToDob, the inverse of dobToIso :559;
│   │                                  # :266 options={genders} → per-card list widened
│   │                                  # by that card's own retired gender (FR-031).
│   │                                  # GenderSelect already takes options as a prop,
│   │                                  # so this is a call-site change; a retired entry
│   │                                  # has no master id, so its React key is its name
│   └── visitor-form.test.tsx          # NEW — seeding, the no-clobber re-render
│                                      # (FR-032), and the widened option list
└── app/(public)/events/[slug]/orders/[orderNumber]/
    ├── page.tsx                       # :119 delete the stale Option B comment.
    │                                  # NO code change: OrderForms already mounts
    │                                  # with data in hand (isPending guard), which
    │                                  # is what makes seed-once correct (R33)
    └── page.test.tsx                  # :482 asserts every input empty on a TWO-SLOT
                                       # fixture — the assertion holding the defect in
                                       # place. Re-point it at an explicit empty-slots
                                       # fixture (FR-030's last clause) and add a
                                       # filled-slots case beside it

backend/internal/order/
├── queries/order.sql                  # NEW ListGenders :many — id, name, is_active.
│                                      # ListActiveGenders :288 UNCHANGED; GET
│                                      # /ticket/genders must keep serving active only
├── repository.go                      # ListGenders wrapper beside ListActiveGenders
├── dto.go                             # :292 Validate's gender membership checks the
│                                      # FULL list. Position unchanged — it still runs
│                                      # before the order is loaded, so 400-before-404
│                                      # precedence survives (research R34)
├── service.go                         # allGenders() beside activeGenders(); new
│                                      # per-slot allowance after matchVisitorsToSlots;
│                                      # :452 GenderID resolves from the full map — the
│                                      # active-only map yields 0 for a retired name,
│                                      # an invalid FK rather than a refusal
└── checkout_forms_test.go             # retired gender accepted on its own slot,
                                       # refused on any other, unknown name still 400001

e2e/
├── support/gateway-stub.ts            # export a helper for POST /__stub/fail-next-session
│                                      # (:80 exists; nothing calls it today — R35)
└── specs/guest-purchase.spec.ts       # Scenario M — save forms, force the gateway to
                                       # fail, return, assert every field restored.
                                       # MUST be seen red first (Principle VIII)

specs/008-e2e-purchase-flow/           # supersession, same change (spec FR-030 note):
├── contracts/booking-flow.md          # :78 "revisit shows empty forms"
├── contracts/api.md                   # :133 Option B heading
├── quickstart.md                      # :122 "revisit shows empty slots"
└── data-model.md                      # :151 Option B note
```

**Migrations**: none. **Response shapes**: unchanged. **Endpoints**: none added.

### Source Code — Track D (phone floor 10 → 12)

```text
backend/
├── internal/order/dto.go                  # :226 comment (cites "10-15 digits"), :231
│                                          # visitorPhonePattern → ^[0-9]{12,15}$,
│                                          # :233 visitorPhoneMessage → "…12-15 digits."
└── internal/order/checkout_forms_test.go  # :138 comment, :164 verbatim message assert;
                                           # boundary cases move 10→12 and gain the
                                           # 11-digit case that is the whole change

frontend/
├── components/order/visitor-form.tsx      # :81-85 comment, :86 PHONE_MESSAGE,
│                                          # :88 phoneSchema regex. NOT :642 — the
│                                          # slice(0, 15) typing mask is the ceiling
│                                          # and stays; there is no typing floor
└── app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx
                                           # :410 fixture comment; :600, :621, :629
                                           # verbatim message asserts

e2e/
└── specs/guest-purchase.spec.ts           # :98 fixture is 12 digits and STAYS VALID —
                                           # add a scenario typing 11 digits and
                                           # asserting refusal (Principle VIII, R29)
```

**Migrations**: none, by requirement — see Storage above.

### Source Code — Track E (FR-013 / FR-014 amendment)

```text
frontend/
├── components/order/order-summary-panel.tsx    # :63-66 delete the NOTE recording the
│                                               # disagreement. NO behaviour change.
└── app/(public)/events/[slug]/orders/[orderNumber]/
    ├── page.test.tsx                           # :529-531 suspended comment → live
    │                                           # negative assertions (no Booking ID,
    │                                           # no per-unit price), each paired with
    │                                           # a rendered positive already in the block
    └── checkout/page.test.tsx                  # :125-126 same

specs/011-order-buyer-info/
└── checklists/requirements.md                  # Notes: the "Open UI/spec disagreement …
                                                # flagged for a decision" bullet is now
                                                # stale — it is the last place the
                                                # disagreement is recorded as open
```

**Backend**: no file. **Frontend production behaviour**: unchanged.

### Source Code — Track C (form-step fee presentation)

```text
frontend/
├── components/order/
│   ├── order-summary-panel.tsx        # :171 total_amount → the phase's figure;
│   │                                  # :167 "Includes all taxes and fees" → the
│   │                                  # next-step wording. The existing
│   │                                  # `showFeeBreakdown` boolean is renamed to
│   │                                  # `phase: "registration" | "payment"` (R26):
│   │                                  # it now governs the headline figure as well
│   │                                  # as the rows, and a boolean named for the
│   │                                  # rows alone would misdescribe it.
│   └── order-summary-panel.test.tsx   # new cases — both phases, on a fixture where
│                                      # subtotal ≠ total_amount (R27), plus the
│                                      # null-subtotal fallback (FR-016c)
├── components/order/visitor-form.tsx  # :314 showFeeBreakdown={false} → phase="registration"
├── app/(public)/events/[slug]/orders/[orderNumber]/
│   ├── checkout/page.tsx              # :222 add phase="payment" (was the default)
│   ├── page.test.tsx                  # :541 asserts the old note — rewrite
│   └── checkout/page.test.tsx         # unchanged behaviour; assertion re-pinned to the phase prop

e2e/
├── support/api.ts                     # new helper: create a fee via POST /api/v1/admin/fees
└── specs/guest-purchase.spec.ts       # new scenario — form step shows subtotal, checkout shows
                                       # subtotal + fees, charged amount unchanged (Principle VIII)
```

**Backend**: no file. `subtotal` and `fees` already ship on `GET /ticket/order/:order_id` ([dto.go:334-336](../../backend/internal/order/dto.go)), so Track C adds no endpoint, field, query, or migration.

### Source Code — Track A (end-of-journey modal)

```text
frontend/
├── components/order/
│   └── expired-state.tsx    # full-page <main> card, 2 links  →  EndOfJourneyDialog:
│                            # controlled open (never lowered), disablePointerDismissal,
│                            # showCloseButton={false}, modal (focus trap + scroll lock),
│                            # Figma 293-3 card, ONE "Return to Home Page" action,
│                            # copy without the "repeat your order" sentence
├── app/(public)/events/[slug]/orders/[orderNumber]/
│   ├── page.tsx             # :100-102 stop early-returning; render OrderForms + dialog
│   ├── page.test.tsx        # :157 rewrite — forms still present behind; no repeat link
│   └── checkout/
│       ├── page.tsx         # :158-160 same; `ended` includes locallyExpired; delete the
│       │                    # inline "payment code has expired" StatusAlert (:211-213)
│       │                    # and suppress QR + StatusChecker when ended
│       └── page.test.tsx    # :236 rewrite + countdown-zero case
```

### Source Code — Track B (schema revision)

```text
backend/
├── migrations/
│   └── 000013_master_list_identity_and_flags.{up,down}.sql
│                            # master lists uuid→identity (+ widened names, created_by/updated_by);
│                            # orders.status→status_id (seeded ids 1..4 fixed, R20);
│                            # attendees.gender_id uuid→smallint via the old key;
│                            # packages.status→is_active; rebuild idx_orders_payment_expiry
├── internal/order/queries/order.sql      # THE status file: reads alias `os.name AS status`;
│                                         # writes/filters resolve by name subquery (R19)
├── internal/event/queries/event.sql      # p.status='ACTIVE' → p.is_active; package column lists
│                                         #   →  run `sqlc generate` after these TWO
├── internal/payment/queries/payment.sql  # NO query change — verified: it never selects from or
│                                         # updates `orders`. `payments.status` is the provider's
│                                         # raw transaction status, a different column. One stale
│                                         # COMMENT at :4 cites `orders.status`; reword only.
├── internal/ticket/queries/ticket.sql    # NO change — never references `orders`; `tickets.status`
│                                         # is excluded by FR-029 and must stay 3-state
├── internal/event/admin_dto.go           # PackageStatusActive/Inactive consts → is_active bool
├── internal/event/{service,handler}.go   # package create/update paths follow the DTO
└── SCHEMA.md (repo root)                 # REQUIRED same-commit sync (Governance)

frontend/
├── lib/types.ts             # Package status union → is_active: boolean (OrderStatus UNCHANGED)
├── lib/schemas.ts           # z.enum(["ACTIVE","INACTIVE"]) → z.boolean()
└── components/admin/package-form.tsx(+.test.tsx)   # <select> → toggle, default true
```

**Structure Decision (rev. 5)**: Existing two-app layout, unchanged again. Track D edits two source files and no structure — the validator already exists on both sides, so this is a constant moving, not a rule arriving; the deliberate non-edit is `visitor-form.tsx:642`, where the typing mask enforces the ceiling and must not grow a floor. Track E edits no source file at all: its deliverable lives entirely in test files, one deleted comment, and a checklist note. Between them they add zero files, zero endpoints, zero migrations, and zero SQL, which is why rev. 5's plan is shorter than its predecessors and why the whole of its risk sits in Principle VIII rather than in design.

**Structure Decision (rev. 3/4)**: Track C adds no file to either app — it changes which of two already-delivered fields one component renders, which is why its plan is dominated by tests rather than by code. Track A adds no file and deletes one screen-level branch per page. Track B adds one migration and no module, route, or table — every Go-side change is a consequence of the SQL, which is why research R19's "map inside SQL" choice is load-bearing: it is what keeps a storage change to five tables from becoming a 29-file Go edit. A file-by-file survey narrowed Track B further than first assumed: only `order` and `event` own SQL against the affected tables, so the sqlc regeneration is two query files, not four.

## Complexity Tracking

No outstanding constitution violations to justify. Track C needed an amendment and got one before planning began (v4.0.0), rather than shipping against a rule it contradicted; Tracks D and E need none, verified against v4.0.0's bullets rather than assumed.

Seven items are carried openly rather than resolved silently — one of which (FR-013/FR-014) rev. 5 finally closes:

| Item | Status |
|---|---|
| **Track B is separable** | The spec's own scope note (below FR-021) says the order-status and package requirements "can be split into their own spec without altering any decision recorded above". They are here because they were decided here. Track A ships on its own with zero dependency on Track B; if the modal is wanted before a five-table storage change, split B out — the design artifacts (research R18–R23, data-model §7, contracts/schema-revision.md) move with it intact, and Track A's (R17, R24) stay put. **Recommended** if the modal is time-sensitive. |
| **Track C's e2e scenario is the whole gate** | Track C's code change is two lines, and its risk is entirely that nobody proves it. The unit fixtures all set `subtotal === total_amount`, and the e2e suite truncates `fees`, so the naive versions of every test pass against unfixed code (R27). The scenario must arrange a real fee through `POST /api/v1/admin/fees` and must be seen red before the fix. **If that scenario is dropped, Track C ships unverified and Principle VIII is breached** — this is the one item in this plan that cannot be traded away for scope. |
| **FR-013 / FR-014 vs. the shipped summary** | **CLOSED 2026-08-13 — this is now Track E.** Carried openly through rev. 3 and rev. 4 as a decision nobody had made: the panel was hand-edited to drop the Booking ID and the per-unit price to match Figma `206-3145`, and the two covering assertions were suspended in place. The clarification session bent the spec to the design, so no UI is restored and no code changes; the outstanding work is un-suspending the two assertions as negative checks and clearing the stale "open disagreement" note from the checklist. It is no longer a planning gap held open — it is a track with tasks. |
| **Track D's e2e scenario is the whole gate, again** | Track D's code change is one regex digit on each side of the wire, and — exactly as with Track C — its risk is entirely that nobody proves it. The e2e holder fixture is 12 digits, so **every existing test passes against unfixed code** (research R29). The scenario must drive an 11-digit value and be seen red under `{10,15}` before the regex moves. **If it is dropped, Track D ships unverified and Principle VIII is breached.** The second, quieter failure mode is moving the regex without moving the five verbatim assertions of the message string, which produces a suite failure that reads like a copy nit and is actually the rule and the guest-facing text disagreeing. |
| **Track D narrows a rule for the first time** | Every previous change to the phone rule widened it, so no stored value ever became invalid. This one makes real rows non-conforming, and the clarification's answer is that they stay valid at rest permanently. The temptation a future reader will feel is to "finish the job" with a CHECK constraint or a backfill sweep; both are explicitly out of scope and would fail on any environment holding real orders. Recorded here rather than only in research R29 because it is the kind of tidy-up that gets added without being planned. |
| **Track F's e2e scenario is the whole gate, a third time** | The pattern is now three for three, and this instance is the most deceptive. Track C's fixtures set `subtotal == total_amount`; Track D's fixture was 12 digits; Track F's fixtures have **empty slots**, so "the forms are empty" is what the suite asserts, what unfixed code does, and what fixed code must still do for an order nobody has submitted. A scenario that opens the forms and looks at them proves nothing. It must save details through the real checkout call, force the gateway to fail so `payment_started` stays false, return, and assert the values are back — and be seen red first (research R35). **If it is dropped, Track F ships unverified and Principle VIII is breached.** |
| **FR-032 is a constraint a good implementation breaks by accident** | The natural way to write "prefill from the server" is an effect that syncs the form whenever the order data changes. It reads as more correct than seeding once, and on this screen it is actively harmful: `useOrderDetail` refetches on window focus and reconnect, and returning from a banking app is the normal path here — so the effect would wipe a guest's typing exactly when they came back to finish. Seeding once needs no mechanism at all; RHF's `defaultValues` are captured at mount and `OrderForms` mounts with data in hand (R33). Recorded here because the failure has no visible symptom until a real guest is mid-type, and because a later reader "fixing" the missing sync is the most likely way this regresses. |

One accepted trade-off is recorded in full at research R17: FR-023 forbids the close control that Base UI's docs recommend keeping inside a modal popup for touch screen-reader users. The single "Return to Home Page" button is that escape — focusable, inside the trap, and a genuine exit — so the guidance's intent is met even though its literal shape is not.

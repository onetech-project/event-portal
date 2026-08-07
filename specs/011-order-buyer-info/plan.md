# Implementation Plan: Order Page Buyer Information — Per-Ticket Holder Forms

**Branch**: `feat/buyer` (spec directory `011-order-buyer-info`) | **Date**: 2026-08-07 (rev. 3 — end-of-journey modal + schema revision) | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/011-order-buyer-info/spec.md`

## Summary

FR-001 – FR-021 are **delivered** (tasks T001–T030, commits `e189e19`…`01ce8fe`): the buyer card is gone, holder forms are the only identity forms, the topmost holder is the primary contact, phone is a digits-only free-text field validated on length alone, the summary and rail behave, the QR screen has its own address, and delivery settled at exactly one email to the buyer (constitution v3.0.0). Migration `000012` shipped with it.

Two requirement groups remain, added by the 2026-08-07 clarifications and independent of each other:

**Track A — End-of-journey modal (FR-022 – FR-024).** Frontend only, no API change. Today an EXPIRED or CANCELLED order replaces the entire screen with a full-page "Time's Up" card, destroying the context the guest most needs at that moment. Both order screens instead keep their own layout rendered and raise one unclosable Base UI dialog over it — no X, no Escape, no backdrop dismissal, page behind inert — rebuilt to Figma `293-3` with a single "Return to Home Page" action. On the QR screen it opens the instant the client countdown reaches zero, replacing the inline expired notice.

**Track B — Schema revision (FR-025 – FR-029).** Backend + admin, invisible to guests. One new migration `000013`: both master lists move from uuid keys to integer identity with widened names and `created_by`/`updated_by` audit columns; `orders.status` becomes `status_id` with the name mapped back inside SQL so not one Go comparison changes; `attendees.gender_id` converts uuid → smallint through its old key; `packages.status` becomes an `is_active` boolean that surfaces on the admin wire as a toggle. `tickets.status` is explicitly untouched.

The two tracks share no file. Track A is small, guest-visible, and ready; Track B is a storage change across five tables, though narrower in code than it first appears — only two domains own SQL against them. The spec's own scope note permits Track B to be split into its own spec — see [Complexity Tracking](#complexity-tracking).

## Technical Context

**Language/Version**: Go 1.24+ (backend), TypeScript / Next.js App Router (frontend)

**Primary Dependencies**: Backend — Echo v4, pgx, sqlc-managed SQL in `internal/*/queries/*.sql`. Frontend — React Hook Form + zodResolver, Zod, TanStack Query, TailwindCSS, and **Base UI** `@base-ui/react` (the `dialog.tsx` wrapper in `components/ui/` is over Base UI, not Radix — verified against the installed package; Track A depends on its `modal` / `disablePointerDismissal` semantics).

**Storage**: PostgreSQL. Track A: none. Track B: **one migration** — `000013_master_list_identity_and_flags.{up,down}.sql`. `000012` is left exactly as committed (research R18) because it is already applied in environments built from this branch; editing an applied migration makes `schema_migrations` lie.

**Testing**: Go table tests per domain (`cd backend && ./scripts/test.sh ./...`). Frontend vitest 4.1.10 — **no `test` script in `package.json`**; invoke `./node_modules/.bin/vitest run` directly, node is not on PATH (project memory `frontend-toolchain-invocation.md`). Track A rewrites the two "Time's Up" tests at `page.test.tsx:157` and `checkout/page.test.tsx:236` — both currently assert a `repeat order` link that FR-024 deletes, so they fail until updated. Track B's regression surface is the existing suites: nothing should change behaviour, which is the point.

**Target Platform**: Linux server (Docker Compose) + modern browsers (guest flow is mobile-first responsive)

**Project Type**: Web application — Go modular-monolith backend + Next.js frontend, one release train

**Performance Goals**: Unchanged. Track B keeps the expiry sweeper's partial index hot by rebuilding `idx_orders_payment_expiry` on the literal seeded id (research R20).

**Constraints**: Constitution v3.0.0 — **no amendment needed by either track**. DTO isolation (Track B's whole design is "storage moves, wire does not"); domain isolation (no domain imports another's repository; the name↔id mapping lives in SQL, not in a shared Go map that would cross domains); SCHEMA.md must be updated in the same change as any schema change (Technology Stack Requirements).

**Scale/Scope**: Track A — 3 frontend files + 2 test files. Track B — 1 migration, **2** domains' `queries/*.sql` under `sqlc generate` (`order` and `event` — verified as the only two whose SQL touches the affected tables), ~4 event Go files, 3 frontend admin files, plus SCHEMA.md.

## Constitution Check

*GATE: evaluated against constitution v3.0.0 (current; neither track requires an amendment — a first for this feature).*

| Principle | Verdict | Evidence |
|---|---|---|
| I — Modular Monolith | PASS | Track A is frontend-only. Track B stays inside `internal/order`, `internal/event`, and the `queries/` + `migrations/` sources sqlc generates from. No new packages, no new domains. |
| II — Domain Isolation | PASS — and the blast radius is smaller than it looks | A survey of `internal/*/queries/*.sql` found **only `order`'s SQL touches the `orders` table at all**; `payment/queries/payment.sql` names `orders.status` in a comment only, and `ticket/queries/ticket.sql` never references `orders`. `payment`, `ticket`, and `notification` receive order state through order-domain adapters (`OrderRef`, `OrderDelivery`, wired in `cmd/api/adapters.go`), so R19's SQL-side mapping means they keep receiving the status **name** with no change of any kind. No domain imports `order`'s repository, and no shared Go enum map is introduced that would couple them. `order_statuses`/`genders` are joined only within the order domain's own statements — the same intra-database referential integrity `000012` established. |
| III — DTO Isolation | PASS | Track B's governing constraint. FR-026 keeps `PENDING`/`PAID`/`CANCELLED`/`EXPIRED` on the wire while storage moves to `status_id`; the `TicketOrderDetail`, admin order, and SSE shapes are byte-identical. The one deliberate wire change — `packages.status` → `is_active` (FR-029) — is a DTO edit in `internal/event/admin_dto.go`, not a leaked sqlc struct. |
| IV — Transactional Integrity & Idempotency | PASS | Neither track touches booking TX-B, checkout TX-D, the gateway-call-after-commit rule, the webhook idempotency short-circuit, or `email_sent`. Track A's countdown-zero rule is explicitly client-side: the server's expiry sweep is unchanged and its later status arrival is a no-op on screen (contract §6). |
| V — Payment Gateway Abstraction | PASS | `Gateway` interface untouched; no payment code changes in either track. |
| VI — Guest-First MVP Scope | PASS | Track A simplifies a guest dead end. Track B is corrective normalization, adding no capability — and master-list CRUD is **deliberately not built** (research R22), which is where the scope pressure would otherwise be. |
| Critical Data Flow Rules | PASS | The one-email-to-the-buyer rule, quota semantics, ticket generation, and the form-step fee rule are all untouched. Track A removes a duplicate expiry message; it does not change when an order expires. |
| Governance sync | **PASS with a required follow-up** | Track B changes the schema, so the Technology Stack clause ("Any schema change MUST update `SCHEMA.md` in the same change") binds: SCHEMA.md must carry migration `000013`, the two master-list tables' new shape, `orders.status_id`, `attendees.gender_id`'s new type, and `packages.is_active` **in the same commit**. ARCHITECTURE.md and PRD.md need no edit — no flow or product-scope change. |

**Post-Phase-1 re-check**: PASS — [research.md](research.md) R17–R24, [data-model.md](data-model.md) §7, and [contracts/schema-revision.md](contracts/schema-revision.md) introduce no new violation. No new endpoint, no cross-domain repository access, no constitution amendment; the single governance obligation (SCHEMA.md) is named and owned.

## Project Structure

### Documentation (this feature)

```text
specs/011-order-buyer-info/
├── plan.md              # This file (rev. 3)
├── research.md          # Phase 0 — R1–R16 (delivered) + R17–R24 (rev. 3)
├── data-model.md        # Phase 1 — §1–6 (migration 000012) + §7 (migration 000013)
├── quickstart.md        # Phase 1 — scenarios A–F (delivered) + G–I (rev. 3)
├── contracts/
│   ├── checkout-and-delivery.md   # delivered — API deltas vs specs 008/010
│   └── schema-revision.md         # rev. 3 — package wire break; everything else asserted unchanged
└── tasks.md             # Phase 2 (/speckit-tasks) — still lists T001–T030 only; rev. 3 tasks not yet generated
```

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

**Structure Decision**: Existing two-app layout. Track A adds no file and deletes one screen-level branch per page. Track B adds one migration and no module, route, or table — every Go-side change is a consequence of the SQL, which is why research R19's "map inside SQL" choice is load-bearing: it is what keeps a storage change to five tables from becoming a 29-file Go edit. A file-by-file survey narrowed Track B further than first assumed: only `order` and `event` own SQL against the affected tables, so the sqlc regeneration is two query files, not four.

## Complexity Tracking

No constitution violations to justify — the first revision of this feature that needs no amendment.

Two items are carried openly rather than resolved silently:

| Item | Status |
|---|---|
| **Track B is separable** | The spec's own scope note (below FR-021) says the order-status and package requirements "can be split into their own spec without altering any decision recorded above". They are here because they were decided here. Track A ships on its own with zero dependency on Track B; if the modal is wanted before a five-table storage change, split B out — the design artifacts (research R18–R23, data-model §7, contracts/schema-revision.md) move with it intact, and Track A's (R17, R24) stay put. **Recommended** if the modal is time-sensitive. |
| **FR-013 / FR-014 vs. the shipped summary** | Flagged in [checklists/requirements.md](checklists/requirements.md) and still open: the summary panel was hand-edited to drop the Booking ID (FR-013) and the per-unit price on each ticket line (FR-014) to match Figma `206-3145`, and the two assertions covering them are suspended with in-place comments in `page.test.tsx` and `checkout/page.test.tsx`. This is a spec-vs-design disagreement needing a decision, not a planning gap: either restore the UI or amend FR-013/FR-014. Neither track touches it, and it is **not** silently absorbed into this plan. |

One accepted trade-off is recorded in full at research R17: FR-023 forbids the close control that Base UI's docs recommend keeping inside a modal popup for touch screen-reader users. The single "Return to Home Page" button is that escape — focusable, inside the trap, and a genuine exit — so the guidance's intent is met even though its literal shape is not.

# Implementation Plan: Per-Ticket Event Dates

**Branch**: `015-ticket-event-dates` | **Date**: 2026-08-12 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/015-ticket-event-dates/spec.md`

**Revision 2.** Revision 1 shipped: the migration, the admin authoring surface, per-line
ticket dates, and the five-outcome gate check are implemented, tested, and green in all
three tiers. The 2026-08-12 clarification session then produced four answers, three of
which **contradict the shipped code**. This revision plans only that delta. Revision 1's
constitution analysis still holds except where noted, and nothing already shipped is
being torn out beyond the three corrections below.

## Summary

Three corrections and one addition, all confined to how the Order Summary panel and the
frontend formatters render dates. No schema change, no validation change, no change to
anything the gate does.

1. **Package lines lose information today.** A bundle admitting on Day 1 *and* Day 2
   renders as one date, because the wire carries a single collapsed `event_start` per
   line and the panel prints it. The screenshot that triggered this shows
   "Test - Day 1 & 2 → 30 Sep 2026" with Day 2 nowhere. The line must name every distinct
   day the bundle admits on.
2. **The Event box was pointed at the wrong thing.** Revision 1 derived the panel's date
   range and "Gate opens at" line from the order's ticket windows. The box is labelled
   "Event"; it must name the event's own dates. This reverts a piece of shipped work.
3. **Dates render in Indonesian.** `id-ID` produces "1 Okt 2026". Every date and time in
   both interfaces moves to English, day-first. Currency and visitor counts stay
   Indonesian.
4. **Out of scope, explicitly**: the email receipt and the e-ticket. The receipt's order
   lines stay dateless; each e-ticket keeps printing its own single window.

## Technical Context

**Language/Version**: Go 1.x (backend), TypeScript with Next.js 16.2.12 / React 19.2.4

**Primary Dependencies**: unchanged from revision 1. No new dependency.

**Storage**: **No change.** `ticket_types.event_start` / `event_end` already exist
(migration `000014`). Nothing in this revision touches the schema, so `SCHEMA.md` is
untouched and no migration `000015` is created.

**Testing**: the three tiers as before. This revision's changes are concentrated in
frontend rendering, so Vitest carries most of the new assertions, with e2e proving the
package line end to end.

**Target Platform**: Linux server; guest web + admin console

**Project Type**: Web application — Go modular monolith backend + Next.js frontend

**Performance Goals**: no new N+1. The package constituent dates come from one extra
batched query over ids already in hand.

**Constraints**: the wire shape of `PublicOrderItem` changes, so backend and frontend
move together. Frontend work must consult `node_modules/next/dist/docs/` first
(`frontend/AGENTS.md`).

**Scale/Scope**: 1 SQL query added, 1 changed; 4 Go structs; 6 frontend files; 5 date
formatters relocalised; ~6 existing tests to correct.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Assessment | Verdict |
|---|---|---|
| **I. Modular Monolith** | Work stays in `internal/event`, `internal/order`, and `frontend/`. | PASS |
| **II. Domain Isolation** | The new constituent-dates query joins `packages` → `package_tickets` → `ticket_types`, all event-domain tables. `EventLookup` keeps its four-method signature; only `PackageDisplay` widens. | PASS |
| **III. DTO Isolation** | The new field is added to hand-written DTOs; no sqlc struct reaches transport. | PASS |
| **IV. Transactional Integrity** | Untouched. No order-writing transaction, no quota logic, no gateway call. | PASS |
| **V. Payment Gateway Abstraction** | Untouched. | PASS |
| **VI. Guest-First MVP Scope** | Display-only. Adds no capability; the deliberate exclusion of the email receipt keeps the change small. | PASS |
| **VII. Cache as Disposable Accelerator** | **`PublicOrderItem` is NOT cached** — order detail is a single-record read and Principle VII forbids caching those. The cached surface is the ticket-type *list* DTO, which this revision does not touch. So unlike revision 1, no flush is required. | PASS |
| **VIII. E2E Acceptance Coverage** | See below. | PASS |

**End-to-end acceptance (Principle VIII) — always applicable, never omit this row:**

- [x] **Does this feature touch a flow covered by `e2e/`?** Yes.
      `e2e/specs/guest-purchase.spec.ts` covers the Order Summary panel on the
      registration and payment steps. The existing scenario "each order line shows its own
      ticket's date, not the event's" stays valid but must gain a package assertion.
- [x] **New user-visible behaviour → new scenarios.** The package multi-date line is new
      and user-visible; it needs a scenario buying a bundle whose constituents sit on
      different days and asserting both days appear on the one line.
- [x] **Bugfix in a covered flow → a scenario that fails first.** The collapsed package
      line is a defect in a covered flow. Its scenario MUST be confirmed red before the
      fix — against current code the line renders one date, so asserting two fails.
- [x] **Behaviour under `E2E_CACHE_ENABLED=false`?** None: order detail is uncached in
      both modes. The suite must still pass both ways, as always.

**Complexity Tracking**: no violations to justify — the table is omitted.

## Project Structure

### Documentation (this feature)

```text
specs/015-ticket-event-dates/
├── plan.md              # This file (revision 2)
├── research.md          # Phase 0 — D-001..D-008 (rev 1) + D-009..D-012 (rev 2)
├── data-model.md        # Phase 1 — updated for the wire change
├── quickstart.md        # Phase 1 — updated scenarios
├── DEPLOY.md            # Revision 1's mandatory cache flush (still applies)
├── contracts/api.md     # Phase 1 — updated
├── checklists/
└── tasks.md             # Phase 2 — regenerate with /speckit-tasks
```

### Source Code (repository root)

Only these files change. Everything else shipped in revision 1 stays as it is.

```text
backend/
├── internal/event/
│   ├── queries/event.sql        # ListPackageDisplaysByIDs loses MIN/MAX;
│   │                            # ListPackageAdmissionStartsByIDs added
│   ├── eventsql/                # regenerated
│   └── package_repository.go    # PackageDisplayRecord carries a date list
├── internal/order/
│   ├── admin_service.go         # PackageDisplay / TicketTypeDisplay
│   ├── dto.go                   # PublicOrderItem: admission_starts replaces the pair
│   └── public_service.go        # publicItems fills the list
└── cmd/api/adapters.go          # the two display copy loops

frontend/
├── lib/
│   ├── format.ts                # 5 date formatters id-ID → en-GB; currency/counts stay
│   ├── admission-dates.ts       # NEW — the 1 / 2 / range rule, pure and testable
│   └── types.ts                 # PublicOrderItem
└── components/order/
    └── order-summary-panel.tsx  # Event box reverts to event dates; line uses the rule

e2e/
├── support/api.ts               # a bundle-seeding helper for the new scenario
└── specs/guest-purchase.spec.ts # package multi-date scenario
```

**Structure Decision**: unchanged from revision 1 — the existing web-application split.
The one new file is `frontend/lib/admission-dates.ts`, which exists so the 1 / 2 / range
rule is unit-testable without rendering a panel, in the same spirit as
`lib/event-window.ts` from revision 1.

## Phase 0 — Research

Complete. Revision 2 adds four decisions to [research.md](./research.md):

- **D-009** — the wire carries a **list of admission starts per line**, replacing the
  single `event_start`/`event_end` pair. A ticket line has one entry; a package has one
  per distinct constituent.
- **D-010** — **the client dedupes by rendered date, not the server.** "Distinct calendar
  date" depends on the timezone the date is rendered in, so deduplicating anywhere other
  than at the point of formatting can produce two entries that print identically.
- **D-011** — the constituent starts come from a **second batched query**, not
  `array_agg`, keeping sqlc's generated types to plain `time.Time`.
- **D-012** — the locale change is **five formatters in one file**, and deliberately
  excludes currency and counts.

No `NEEDS CLARIFICATION` remains: the 2026-08-12 session resolved all four questions it
raised, and the fifth was withdrawn as out of scope.

## Phase 1 — Design & Contracts

Complete.

- [data-model.md](./data-model.md) — the revised `PublicOrderItem`, the package
  constituent query, and what the panel derives from each.
- [contracts/api.md](./contracts/api.md) — the order-detail wire delta and the corrected
  statement that the Event box reads the event.
- [quickstart.md](./quickstart.md) — scenarios updated for the package line, the Event
  box reversal, and English dates.

### Post-design Constitution re-check

No gate moved. Two observations the initial check could not have made:

- **Principle VII is easier here than in revision 1.** Order detail is explicitly
  *not* a cacheable surface, so the deploy hazard that dominated revision 1 does not
  recur. `DEPLOY.md` still applies to the revision 1 columns, but this revision adds no
  new flush requirement.
- **Principle VIII gains a genuinely new red-first case.** The collapsed package line is
  a defect a user can see in a covered flow, so it earns a scenario that must be observed
  failing. That is the one piece of this revision that cannot be hurried.

## Ordering constraints for Phase 2

1. The `PublicOrderItem` shape change lands backend-first, then frontend — the wire is
   the contract.
2. `sqlc generate` after the query edits; `git diff --exit-code internal/` must be clean.
3. The package e2e scenario is written and **seen red** before `publicItems` starts
   filling the list.
4. The Event box reversal deletes `admissionSpanOf` and the two Vitest cases that assert
   the span behaviour. Those two tests are not "broken" — they encode a decision that has
   been reversed, and they must be replaced rather than patched.
5. The locale change breaks exactly two assertions
   (`format.test.ts` `/Okt/`, `order-summary-panel.test.tsx` `/1 - 3 Agu/i`). Both are
   asserting the old language, not old behaviour.
6. No migration, no `SCHEMA.md` change, no cache flush beyond what `DEPLOY.md` already
   requires for revision 1.

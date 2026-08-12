# Quickstart: Validating Per-Ticket Event Dates

**Feature**: [spec.md](./spec.md) | **Contracts**: [contracts/api.md](./contracts/api.md)

How to prove this feature works. Everything below runs against the real stack — real Go
API, real PostgreSQL, real Redis, real browser — per constitution Principle VIII.

## Prerequisites

Postgres and Redis are not owned by the test runner; start them first.

```bash
REDIS_PORT=6380 docker compose up -d postgres redis
docker compose run --rm migrate up          # must reach 000014
docker compose run --rm migrate version     # expect: 14
```

Regenerate sqlc after the migration and the query edits, and confirm the output is
reproducible:

```bash
cd backend && sqlc generate && git diff --exit-code internal/
```

A non-empty diff here means generated code was hand-edited or the queries drifted.

## The three test tiers

```bash
cd backend  && ./scripts/test.sh ./...      # Go: unit + database-backed
cd frontend && npx vitest run               # TypeScript: logic + components
cd e2e      && npm test                     # Playwright, headless
cd e2e      && npm run test:slow            # headed, slowed for a human to watch
```

The suite must also pass with the cache off — this is how Principle VII's kill switch is
actually verified, not merely asserted:

```bash
cd e2e && E2E_CACHE_ENABLED=false npm test
```

## Migration sanity: the backfill must be total

Before trusting anything else, confirm no row escaped step 2 of the migration.

```sql
-- Expect 0. NOT NULL should make this impossible; check anyway, because a
-- backfill that silently missed rows is the failure this feature cannot absorb.
SELECT count(*) FROM ticket_types WHERE event_start IS NULL OR event_end IS NULL;

-- Expect 0. Every pre-existing ticket type must equal its parent event exactly
-- (FR-004) — that is what makes the feature invisible to events that don't use it.
SELECT count(*)
FROM ticket_types tt JOIN events e ON e.id = tt.event_id
WHERE tt.event_start <> e.start_date OR tt.event_end <> e.end_date;
```

The second query is only meaningful **immediately** after migrating, before any admin
edits a window.

## MANDATORY post-deploy step: flush the cache

Not optional, and not covered by any test. The public ticket list is cached as the DTO
with no schema version in the key, so entries written by the previous binary decode
cleanly into the new shape with `0001-01-01T00:00:00Z` for both new fields — silently
wrong data, not a decode error, for up to `CACHE_TTL` (default 10m) and for the whole of a
rolling deploy. Full reasoning in [research.md](./research.md) D-002.

```bash
curl -X POST -H "Authorization: Bearer $ADMIN_JWT" \
  http://localhost:8080/api/v1/admin/cache/refresh
# {"status":"flushed","entries_cleared":N}   — or {"status":"disabled"} if the cache is off
```

Safe to run unconditionally: it returns 200 with `"disabled"` rather than erroring when
caching is switched off.

## Scenario 1 — a buyer sees the date of the ticket they picked (US1)

Sets up the motivating case: one event, two days, two passes.

1. Create an event running 1–3 August.
2. Create **Day 1 Pass** with `event_start` 1 Aug 09:00, `event_end` 1 Aug 23:00.
3. Create **Day 2 Pass** with `event_start` 2 Aug 09:00, `event_end` 2 Aug 23:00.
4. As a guest, buy one Day 2 Pass and reach the holder-details step.

**Expect** the Order Summary panel to show 2 August — not 1 August, and not the 1–3 August
event range. The "Gate opens at" line reads 09:00 WIB of 2 August.

5. Buy both passes in one order.

**Expect** each ticket line under "Tickets" to carry its own date, and the panel's range to
span 1–2 August (the ticket span, not the event's 1–3).

6. Settle the order and open the issued ticket PDF and email.

**Expect** each ticket printed with its own window (FR-011).

7. Open the events list, the event landing page's "Dates" cell, and the countdown.

**Expect** all three unchanged, still showing the event's 1–3 August (FR-012). The
countdown still counts to the event's start.

## Scenario 2 — the admin authors and reschedules (US2)

1. Open the admin ticket-type form.

**Expect** Event start / Event end fields, labelled distinctly from Sales start / Sales
end, with the labelling making clear that event start is when **admission opens**, not
showtime (FR-006).

2. Submit with `event_end` before `event_start` → refused, `INVALID_DATE_RANGE`.
3. Submit a window outside the parent event's dates → refused, `INVALID_DATE_RANGE`, with
   the event's own bounds named.
4. Widen the event from 1–3 August to 1–4 August.

**Expect** success, no warning, and no ticket-type window altered (FR-005c).

5. Move the event from 1–3 August to 5–7 August.

**Expect** the save to **succeed** (FR-005a), a non-blocking warning naming both stranded
ticket types (FR-005b), and their windows unchanged. Then move each ticket type into 5–7
August — each now saves cleanly.

This ordering is the whole point of FR-005a: try it the other way (tickets first) and it
is impossible, which is why the event edit is deliberately unguarded.

6. Edit a ticket type's window and reload the guest order page.

**Expect** the new dates immediately, with no manual flush (FR-008). This is the
cache-coherence check — run it in both cache modes.

## Scenario 3 — the gate refuses the wrong day (US3)

Boundary behaviour matters more than the happy path here, because FR-014 admits **no**
tolerance.

| Setup | Validate at | Expect |
|---|---|---|
| window covers now | now | `Valid`, "Mark used" present, transition works as before |
| window starts tomorrow | now | `NOT_YET_VALID`, window named, **no** "Mark used" |
| window ended yesterday | now | `EXPIRED`, window named, **no** "Mark used" |
| window covers now, ticket already used | now | `Already used` — precedence beats the window (FR-018) |
| unknown code | now | `Invalid`, no window disclosed (FR-019) |
| window starts at T | exactly T | `Valid` — endpoints are inclusive |
| window ends at T | exactly T | `Valid` — endpoints are inclusive |
| window starts at T | one moment before T | refused — no grace period |

Also confirm the direct-invocation guard (FR-017): call
`POST /admin/tickets/:code/use` for an out-of-window ticket and expect **409**, with the
ticket still `ACTIVE` afterwards. The UI hiding the button is not the enforcement.

## Known breakage this feature must fix, not work around

The existing e2e validation scenarios **will fail** until the seeding helpers are updated.
`createEvent` seeds every event at `+30d/+31d` ([api.ts:110-111](../../e2e/support/api.ts)),
so once ticket windows default to the parent event's range and validation enforces them
strictly, `admin-console.spec.ts:89` — which validates *now* and asserts `Valid` — gets
`NOT_YET_VALID`.

FR-005 containment means the ticket window alone cannot be fixed: the seeded **event** has
to span now too. Give `createEvent`, `createTicketType`, and `createSellableEvent` optional
date overrides and seed the validation scenarios with a window covering now.

Per Principle VIII, the new out-of-window scenario must be **confirmed failing against
unfixed code** before the fix lands. A regression test never seen red proves nothing.

## Definition of done

- [ ] `migrate version` reports 14; both backfill queries return 0
- [ ] `sqlc generate` leaves no diff
- [ ] All three test tiers green
- [ ] e2e green with `E2E_CACHE_ENABLED=false` as well
- [ ] `SCHEMA.md` updated in the same commit as the migration
- [ ] Cache flushed post-deploy
- [ ] The out-of-window e2e scenario was seen failing before the fix

---

# Quickstart — Revision 2 (2026-08-12)

No migration and no cache flush are added by this revision: it changes only the
order-detail read and frontend rendering, and order detail is not a cacheable surface
under Principle VII. [DEPLOY.md](./DEPLOY.md) still applies to revision 1's columns.

```bash
cd backend  && sqlc generate && git diff --exit-code internal/
cd backend  && ./scripts/test.sh ./...
cd frontend && npx vitest run
cd e2e      && npm test
cd e2e      && E2E_CACHE_ENABLED=false npm test
```

## Scenario 4 — a bundle names every day it admits on

This is the defect scenario. Confirm it fails before the fix.

1. Create an event running 1–3 August.
2. Create **Day 1 Pass** (admits 1 Aug) and **Day 2 Pass** (admits 2 Aug).
3. Create a **Day 1 & 2 Bundle** composed of both.
4. Buy one bundle and reach the holder-details step.

**Expect** the bundle's line to read `1 Aug 2026, 2 Aug 2026`. Against unfixed code it
reads `1 Aug 2026` alone, and the second day is invisible — which is exactly what the
screenshot that triggered this revision showed.

5. Add a **Day 3 Pass** to the bundle and rebuy.

**Expect** three distinct dates to collapse to a range: `1 - 3 Aug 2026`.

6. Compose a bundle from two ticket types that both admit on 1 August.

**Expect** one date, not the same date twice — deduplication is by rendered date.

## Scenario 5 — the Event box names the event

1. Open any order whose ticket admits on a narrower window than its parent event.

**Expect** the Event box's range to be the **event's** 1–3 August, and "Gate opens at" to
be the event's start time — not the ticket's. Revision 1 had these derived from the order's
tickets; FR-009 reversed that.

**Expect** the ticket line beneath it to still show the ticket's own day. The box and the
lines answer different questions and now visibly do.

## Scenario 6 — dates read in English

1. Open the events list, an event landing page, the order summary, and the admin console.

**Expect** English month names throughout — `1 Oct 2026`, never `1 Okt 2026` — in
day-first order, so nothing shifts position.

**Expect** `Rp 170.400` to be unchanged, with a dot thousands separator. If it reads
`Rp 170,400` the currency formatter was relocalised by mistake and the amount now misreads
to an Indonesian buyer.

## Revision 2 definition of done

- [ ] The bundle scenario was seen failing before the fix
- [ ] `admission_starts` is on the wire; `event_start`/`event_end` are gone from items
- [ ] `admissionSpanOf` is deleted and the Event box reads `order.event.*`
- [ ] Both Indonesian-month test assertions updated (`format.test.ts`, panel test)
- [ ] Currency still renders `Rp 170.400`
- [ ] All three tiers green, e2e in both cache modes

# Quickstart: Validating the Booking Availability Gate

**Feature**: 013-booking-availability-gate · **Date**: 2026-08-11

How to prove this feature works, and — for the scenario Principle VIII requires to be seen
red — how to prove it was broken first.

## Prerequisites

The e2e runner does not own Postgres or Redis; bring them up yourself first.

```bash
REDIS_PORT=6380 docker compose up -d postgres redis
docker compose run --rm migrate up
```

No new migration is introduced by this feature, so this is the same schema `main` runs.

## The red-first step (do this before implementing)

Principle VIII: *"A regression test never seen red proves nothing."* The reproduction is
the sold-out scenario — against unfixed code the terms dialog opens regardless of
availability, and the failure only surfaces at Agree.

1. Write **only** the sold-out scenario in `e2e/specs/guest-purchase.spec.ts` — the one
   that presses Buy Ticket on a selection whose quota has been exhausted and asserts
   `await expect(page.getByRole("dialog")).toBeHidden()`.
2. Run it against the current code:

   ```bash
   cd e2e && npx playwright test guest-purchase --grep "sold out"
   ```

3. **Confirm it fails**, and confirm it fails for the right reason: the dialog is visible.
   A failure caused by a bad selector or a seeding mistake proves nothing.
4. Only then implement.

Exhaust the quota **through the real API** — book and settle an order via `support/api.ts`
and `support/payment.ts`. AGENTS.md: *"Never write order status, tickets, or payment state
straight into the database from a test"* — those writes are also what invalidate the
cache, so going around them makes the setup lie.

## Backend

```bash
cd backend && ./scripts/test.sh ./...
```

Must cover:

| What | Why |
|------|-----|
| Every reason code in [contracts/availability.md](contracts/availability.md) §1 | FR-012 — each is reachable and distinct |
| A selection with two independently-broken lines returns **two** reasons | FR-006 — every offending line, not the first |
| A bundle + a standalone ticket on the same ticket type, affordable alone but not together, is refused | FR-005 — aggregation, the oversell `aggregateDemand` exists to stop |
| An event with no authored terms yields `TERMS_MISSING` with `item_index: null` | FR-004, D5 |
| A malformed request (nil `event_id`, `quantity: 0`, both ids set) is a **400**, not a decision | D1 — malformed input has no availability answer |
| Concurrent booking of the last seat: exactly one success, rest `INSUFFICIENT_QUOTA`, quota lands at 0 | §6 guard — the oversell regression `QuotaRemaining` could reintroduce |
| `cmd/api/architecture_test.go` still green | Principle II — `internal/event` still never imported |

Sanity check by hand, with the API running:

```bash
curl -s -X POST localhost:8080/api/v1/ticket/availability \
  -H 'content-type: application/json' \
  -d '{"event_id":"<uuid>","items":[{"ticket_type_id":"<uuid>","quantity":2}]}' | jq
```

A refused selection must come back **HTTP 200** with `code: 200000` and
`data.available: false`. If you see a 4xx for a sold-out ticket, the decision has been
modelled as an error and [research.md](research.md) D1 was not followed.

## Frontend

```bash
cd frontend && npx vitest run
```

> `node` is not on PATH here and `npx` is intercepted — see the
> `frontend-toolchain-invocation` memory for how to invoke these.

| What | Why |
|------|-----|
| A clean decision opens the terms dialog | FR-002 |
| A refused decision does **not** open it, and shows the reason's message | FR-002, FR-012 |
| Quantities are unchanged after a refusal | FR-007 |
| A second press while checking issues no second request | FR-008 |
| Closing the dialog and pressing again issues a **fresh** check | FR-009 — no reused decision |
| A transport failure shows "could not check" and leaves the dialog shut | FR-012, FR-002 |

`terms-dialog.test.tsx` must be migrated to mount with `open={true}` rather than clicking
the trigger, which no longer exists (D6). Its assertions about terms fetching, agreeing,
booking, and the `TERMS_CHANGED` refetch should otherwise survive verbatim — if they need
rewriting, the dialog's internals were changed further than this feature intends.

## End-to-end

```bash
cd e2e && npm test                        # headless
cd e2e && npm run test:slow               # headed, slowed down to watch
E2E_CACHE_ENABLED=false npm test          # Principle VII kill switch
```

Both cache modes must pass. This feature never reads the cache, so any difference between
the two runs is a real finding, not noise.

Scenarios required in `e2e/specs/guest-purchase.spec.ts`:

1. **Sold out is refused at Buy Ticket** — the red-first reproduction above. The dialog
   never opens; the message names the shortfall.
2. **A closed sale window is refused the same way.**
3. **An event with no authored terms is refused** before an empty dialog can render.
4. **Recovery** — a refused guest lowers the offending quantity, presses again, and
   completes the purchase through to issued tickets. This is the one that proves FR-007
   and FR-009 together in a real browser.
5. **The existing happy path passes unchanged.** If it needed editing beyond the
   `journey.ts` helper split, the gate has changed behavior it was not meant to touch.

`e2e/support/journey.ts` needs `agreeToTermsAndBook()` split: a `buyTicket()` that presses
the button and awaits the check, and an `expectRefusedAtBuy(reason)` for the refusal
scenarios. The existing doc comment — *"'Buy Ticket' opens the terms gate rather than
navigating"* — stops being true and must be rewritten, not left as a stale explanation of
a flow that no longer exists.

## Governance sync (same commit)

- **`ARCHITECTURE.md`** — the flow chart at ~line 166 sends the guest straight from
  selection to "Agree to the T&C", and the sequence diagram at ~200–230 shows
  `POST /ticket/book` as the booking step's first server call. Both must gain the
  availability step.
- **`PRD.md`** §1.5 "Public APIs" — add `POST /api/v1/ticket/availability` immediately
  before `POST /api/v1/ticket/book`.
- **`SCHEMA.md`** — no change. No migration exists to pair with it.

## What "done" looks like

- The three test tiers are green, and e2e is green in **both** cache modes.
- Scenario 1 was observed failing against unfixed code before the fix landed.
- A sold-out selection returns HTTP 200 with `available: false` — not a 4xx.
- `bookOnce` is unchanged. Every refusal path it had, it still has (FR-011). If the diff
  touches it, that needs explaining.

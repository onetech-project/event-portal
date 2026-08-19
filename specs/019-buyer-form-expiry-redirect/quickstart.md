# Quickstart: Bundle Form Labels & End-of-Journey Destination

**Feature**: `019-buyer-form-expiry-redirect` | **Date**: 2026-08-18

How to run this feature's verification, in the order that makes failures meaningful. The
red-first steps come first on purpose: Principle VIII's obligation is not "tests exist at
the end", it is "the assertion was seen failing against the unchanged code".

## Prerequisites

The frontend toolchain is not on `PATH` and `npx` is intercepted in this environment. Every
frontend command below assumes this export first:

```bash
export PATH="$HOME/.nvm/versions/node/v26.5.1/bin:$PATH"
```

`frontend/package.json` defines only `dev`, `build`, `start`, and `lint` — there is no
`test` or `typecheck` script, so the local bins are invoked directly.

The acceptance suite does not own its infrastructure. Start it first:

```bash
REDIS_PORT=6380 docker compose up -d postgres redis mailpit
docker compose run --rm migrate up
```

Mailpit is not optional even though this feature sends no mail — the delivery specs in the
same suite read real messages off it.

## Step 1 — See the red (before touching any source)

Six assertions must fail against unchanged code. Run them first and keep the output; a
regression test that was never seen red proves nothing about what it pins.

```bash
cd frontend
./node_modules/.bin/vitest run \
  "app/(public)/events/[slug]/orders/[orderNumber]/page.test.tsx" \
  "app/(public)/events/[slug]/orders/[orderNumber]/checkout/page.test.tsx" \
  components/order/slot-groups.test.ts
```

**Expected once the test files are updated but before the source is**: failures at

| File | What fails |
|---|---|
| `page.test.tsx` | the dialog action's accessible name; the CANCELLED variant's `href`; the two `Visitor 1` / `Visitor 2` assertions |
| `checkout/page.test.tsx` | the dialog action's `href` |
| `slot-groups.test.ts` | the `unitLabel` expectation |

**Expected before either is updated**: all green. That is the point — the current suite
passes for the behaviour being replaced, which is what makes step 1 a real gate rather
than a formality.

## Step 2 — Unit and component tier

```bash
cd frontend
./node_modules/.bin/vitest run
```

Green. Watch specifically that these two survive untouched — they are FR-005's regression
barrier, and a passing suite with either weakened would mean an automatic redirect crept in:

- `page.test.tsx` — `expect(replace).not.toHaveBeenCalled()` for an expired order
- `checkout/page.test.tsx` — the same assertion on the payment screen

## Step 3 — Types and lint

```bash
cd frontend
./node_modules/.bin/next build          # the typecheck of record
./node_modules/.bin/eslint app components lib
```

`next build` rather than a bare `tsc --noEmit`: the latter also surfaces stale
`.next/dev/types/validator.ts` errors pointing at route files that no longer exist, which
`next build` regenerates away.

Deleting `SlotGroup.unitLabel` is what makes this step load-bearing — every remaining
reader of the field becomes a compile error, so a green build is the proof that R1's "only
two consumers" survey was complete.

## Step 4 — Acceptance suite

```bash
cd e2e && npm test          # headless
cd e2e && npm run test:slow # headed, slowed, for watching the flow
```

The suite lives in `e2e/` on this branch and owns its own `package.json`, so it is driven
from that directory — not through a `frontend/` script. (Note for anyone reading a stale
copy of `AGENTS.md`: a parallel refactor branch moves this suite to `frontend/__test__/`
with `npm run test:e2e`. On `fix/buyer` the commands above are the working ones.)

Three scenarios in `e2e/specs/guest-purchase.spec.ts` carry this feature:

1. **`an expired payment returns the seats to the pool`** (extended). Already arranges a
   real expiry through the signed gateway notification. Now also asserts the dialog is up,
   offers exactly one action, that the action names the event page, and that pressing it
   lands on `/events/{slug}`.
2. **holder-forms hold expiry** (new). Book, stay on the forms, let the booking hold lapse,
   then assert the same four things.
3. **two-unit bundle holder forms** (new). `selectQuantity(bundle, 2)`, assert two cards
   with no visitor number on either, then complete the order so the fan-out is proven
   through the real API rather than a stubbed `fetch`.

**The one trap in scenario 2**: the wait must be derived from the same scaling
`playwright.config.ts` applies to `BOOKING_HOLD`, not hardcoded. `E2E_SLOW_MO` grows that
hold deliberately, so a literal 30-second wait passes headless and then hangs under
`test:slow`. Prefer waiting on the observable outcome — the dialog appearing — with a
timeout derived from the scaled hold, over sleeping for a fixed duration.

Scenario 2 costs roughly 35 seconds of wall clock. That is a real addition to the run and
is not hidden: it is the only way to reach the forms-screen dialog, because the order in
scenario 1 has already started payment.

## Step 5 — Both cache modes

Principle VII's kill switch is verified by exercise, not by assertion:

```bash
cd e2e && E2E_CACHE_ENABLED=false npm test
```

Nothing in this feature reads or writes the cache, so this run is expected to be
uneventful. Run it anyway — a mode that is only ever claimed to work is a mode nobody is
checking.

## Step 6 — Both throttling modes

```bash
cd e2e && E2E_RATE_LIMIT_ENABLED=false npm test
```

Also expected to be uneventful. The one thing to confirm in the **enabled** run: the three
scenarios above book three additional orders against the shipped per-IP `Book` allowance
that every guest scenario shares. The suite runs serially (`workers: 1`), so this should be
comfortable — but "should be" is why it is a step rather than a footnote.

## Manual walkthrough (optional, for the visual check)

With the dev stack up, in a browser:

1. Book any order and stop on the holder forms. Wait out the booking hold.
   → The forms, the progress rail, and the order summary stay on screen. The dialog opens
   over them. Nothing moves you.
2. Read the dialog's single button. → It names the event page, not the home page.
3. Try to dismiss it — Escape, a backdrop click, look for an X. → All three fail, and the
   page behind cannot be scrolled or typed into.
4. Press the button. → You land on `/events/{slug}`, and the seats the order held are
   listed as available again.
5. Repeat from the payment screen by letting the countdown reach `0 : 00`. → Same dialog,
   same destination, and the QR is gone from behind it.
6. Book two units of one bundle and open the holder forms. → Two cards, each titled with
   the bundle's name and badged with its ticket count, and no `Visitor 1` / `Visitor 2`
   anywhere — including in the tooltip you get by hovering a clipped heading.

## References

- Requirements and acceptance scenarios: [spec.md](./spec.md)
- Why each approach was chosen, and what was rejected: [research.md](./research.md)
- The shapes that change: [data-model.md](./data-model.md),
  [contracts/README.md](./contracts/README.md)

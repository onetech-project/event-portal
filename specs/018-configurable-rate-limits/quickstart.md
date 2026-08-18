# Quickstart: Validating Configurable Rate Limits

**Feature**: [spec.md](spec.md) · **Plan**: [plan.md](plan.md) · **Contract**: [contracts/configuration.md](contracts/configuration.md)

How to prove this feature works, in the order the proofs build on each other. Every step
maps to a numbered success criterion in the spec.

## Prerequisites

The runner does not own the infrastructure. Bring it up first — Mailpit is not optional,
the delivery specs read real messages off it.

```bash
REDIS_PORT=6380 docker compose up -d postgres redis mailpit
docker compose run --rm migrate up
```

No schema change ships with this feature, so `migrate up` is a no-op here; it is listed
because a fresh checkout still needs it.

---

## 1. Defaults reproduce today's behaviour — SC-003

The single most important check, and the one worth running first: a deployment that
configures nothing must be indistinguishable from the current system.

```bash
cd backend && ./scripts/test.sh ./...
```

**Expected**: green, including a new `pkg/config` test asserting each default equals the
constant it replaced. `0.33` must be `0.33` — not `1.0/3.0`, not rounded (see
[data-model.md](data-model.md), Defaults).

Then the real proof, with no throttle variables set at all:

```bash
cd e2e && npm test
```

**Expected**: green, with no scenario skipped and no test adjusted to accommodate the
change. If a guest-purchase scenario needed loosening to pass, a default drifted.

---

## 2. A limit changes without a rebuild — SC-001, SC-002

```bash
cd backend
RATE_LIMIT_BOOK_RATE=0.05 RATE_LIMIT_BOOK_BURST=1 go run ./cmd/api
```

Book twice in quick succession from the same client. **Expected**: the first is accepted,
the second refused with `429`. Restart without the two variables and both are accepted.

No `go build` of a release artifact happened in between — that is the whole claim of SC-001.

---

## 3. The master switch — SC-004

```bash
cd backend && RATE_LIMIT_ENABLED=false go run ./cmd/api
```

Drive any throttled surface far past its default threshold — at least 100 consecutive
requests, which is what SC-004 asks for.

**Expected**: no `429` from any of the six surfaces. Booking, availability, ticket lookup
and checkout all accept; the SSE endpoint accepts more than 10 concurrent streams; two
resends in a row both succeed.

**Also expected, and worth seeing with your own eyes**: the confirmation screen still
disables the resend button for ~5 seconds after a press. That is the client-side debounce,
not the cooldown, and it is correct — see [research.md](research.md) R6.

---

## 4. One surface off, the others still on — SC-005

```bash
cd backend && RATE_LIMIT_CHECKOUT_ENABLED=false go run ./cmd/api
```

**Expected**: checkout accepts 100 consecutive presses. Booking still refuses past `0.33/s`
with a burst of 5. This is the per-surface switch (FR-008) doing exactly one thing.

Then confirm the precedence rule (FR-009):

```bash
RATE_LIMIT_ENABLED=false RATE_LIMIT_CHECKOUT_ENABLED=true go run ./cmd/api
```

**Expected**: checkout is still unthrottled. The master switch wins.

---

## 5. Bad configuration is refused, and names itself — SC-007

Each of these must refuse to start:

```bash
cd backend
RATE_LIMIT_BOOK_RATE=fast   go run ./cmd/api   # unparseable
RATE_LIMIT_BOOK_RATE=-1     go run ./cmd/api   # negative
RATE_LIMIT_BOOK_RATE=0      go run ./cmd/api   # permanent lockout: burst spends, never refills
RATE_LIMIT_IDLE_TTL=10s     go run ./cmd/api   # shorter than the booking refill window
```

**Expected**: startup fails, naming the offending variable. The last one is the subtle case
and the reason V6 exists — a 10-second retention against a ~15-second refill silently
forgives every exhausted client, so the limiter appears to work while enforcing nothing
([research.md](research.md) R9).

Set several bad values at once. **Expected**: every problem reported in one pass, matching
how `config.Load()` already treats the rest of the configuration — one restart tells you
everything that is wrong, not the first thing.

The inverse also matters: a *disabled* surface's bad numbers must **not** refuse startup.

```bash
RATE_LIMIT_BOOK_ENABLED=false RATE_LIMIT_BOOK_RATE=0 go run ./cmd/api
```

**Expected**: starts fine. Turning a throttle off must not be made harder by a value nobody
will read.

---

## 6. The configuration in force is readable — SC-008

```bash
cd backend && RATE_LIMIT_CHECKOUT_RATE=0.5 go run ./cmd/api 2>&1 | head -20
```

**Expected**: one structured `info` line reporting the master switch and every surface's
effective values, with checkout showing `0.5`.

Then confirm the alias path (R2):

```bash
TICKET_LOOKUP_RATE_LIMIT=9 go run ./cmd/api 2>&1 | head -20
```

**Expected**: the lookup rate reports `9`, plus a deprecation notice pointing at
`RATE_LIMIT_TICKET_LOOKUP_RATE`. A deployment already setting the old name keeps working and
is told to move — never silently ignored.

**Expected NOT to happen**: none of these values appear on `/healthz`. Confirm:

```bash
curl -s localhost:8080/healthz | grep -i -E 'rate|limit|burst' || echo "clean — no thresholds exposed"
```

`/healthz` is public; publishing thresholds there hands an attacker the budget to stay under
([research.md](research.md) R8).

---

## 7. The per-IP bypass is closed — R7

**This one must be seen red first.** Principle VIII requires a regression scenario be
confirmed failing against the unfixed code, and this is a bugfix in a covered flow.

Against **unfixed** code, book repeatedly while rotating a header:

```bash
for i in $(seq 1 20); do
  curl -s -o /dev/null -w '%{http_code}\n' \
    -H "X-Forwarded-For: 10.0.0.$i" \
    -X POST localhost:8080/api/v1/... ;
done
```

**Expected before the fix**: twenty `200`s — every request mints a fresh bucket, and the
booking limiter is not in force at all.

**Expected after the fix**: the same sequence trips the limiter, because the client address
now comes from the connection and the header is ignored
([contracts/configuration.md](contracts/configuration.md), Client identification).

---

## 8. The acceptance gate, in every required mode — SC-006, FR-020

Three runs, not four ([research.md](research.md) R5):

```bash
cd e2e
npm test                                          # baseline: cache on, throttle on
E2E_CACHE_ENABLED=false npm test                  # Principle VII's obligation
E2E_RATE_LIMIT_ENABLED=false npm test             # FR-020's obligation
```

**Expected**: green in all three, with no scenario skipped in a mode where it should run.
`e2e/specs/rate-limit.spec.ts` skips itself when throttling is off, mirroring how
[cache-refresh.spec.ts:41](../../e2e/specs/cache-refresh.spec.ts#L41) already skips when the
cache is off.

The fourth cell — both off — is deliberately not run. No throttle reads or writes the cache,
so it tests an interaction that does not exist.

To watch a throttle refusal happen in a real browser at human speed:

```bash
cd e2e && npm run test:slow -- rate-limit
```

---

## What "done" looks like

| # | Criterion | Proven by |
|---|-----------|-----------|
| SC-001 | Every threshold changeable without a build | Step 2 |
| SC-002 | A change is in force in under 5 minutes | Step 2 |
| SC-003 | Unconfigured deployment is unchanged | Step 1 |
| SC-004 | Master switch: 100 requests, no refusals | Step 3 |
| SC-005 | One surface off, others in force | Step 4 |
| SC-006 | Both acceptance modes green | Step 8 |
| SC-007 | Every bad shape refused and named | Step 5 |
| SC-008 | Configuration in force is readable | Step 6 |
| R7 | Per-IP limits are actually per-IP | Step 7 |

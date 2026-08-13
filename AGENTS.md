# Working in this repository

## Read the governance documents before changing behavior

[.specify/memory/constitution.md](.specify/memory/constitution.md) is binding and
supersedes the others on process. [PRD.md](PRD.md) owns product scope,
[ARCHITECTURE.md](ARCHITECTURE.md) owns architectural rules, [SCHEMA.md](SCHEMA.md) is
the absolute source of truth for the database. Per-feature specs live in
[specs/](specs/). An amendment to the constitution MUST update the other three in the
same change.

## There is an end-to-end acceptance suite. Use it.

[`e2e/`](e2e/README.md) is a Playwright suite that drives a **real browser** against the
real Next.js app, the real Go API, and real PostgreSQL and Redis. Only the payment
provider is substituted, and even that at the network boundary, so settlement arrives as
a genuinely signed webhook the production handler verifies.

It covers the guest purchase journey end to end (browse → book → holder forms → QRIS →
settlement → issued tickets, plus ticket lookup, hold expiry, callback-token
rejection, replay), the admin console (auth, orders, ticket validation, delete
guards), and cache coherence.

Constitution **Principle VIII** makes this an acceptance gate, not an optional tier:

- **Changing a covered flow?** Update `e2e/specs/` in the *same* change. Green-because-
  nobody-looked is not verification.
- **Fixing a bug in a covered flow?** Add a scenario, and confirm it fails against the
  unfixed code before you fix it. A regression test never seen red proves nothing.
- **Adding a new user-visible flow?** It arrives with coverage, or with an explicit
  written justification for why not.
- **Never write order status, tickets, or payment state straight into the database from
  a test.** Arrange through the real API — those writes are also what invalidate the
  cache, so going around them makes the setup lie.

```bash
# Postgres, Redis and Mailpit first — the runner does not own them. Mailpit is not
# optional: the delivery specs read the real message off it (spec 016).
REDIS_PORT=6380 docker compose up -d postgres redis mailpit && docker compose run --rm migrate up

cd e2e && npm test          # headless
cd e2e && npm run test:slow # headed, slowed down so a human can follow the flow
```

The suite must also pass with the cache off (`E2E_CACHE_ENABLED=false`) — that is how
Principle VII's kill switch is actually verified.

## The other two test tiers still apply

```bash
cd backend && ./scripts/test.sh ./...   # Go: unit + database-backed
cd frontend && npx vitest run           # TypeScript: logic + components
```

`e2e/` proves the assembled system; it does not replace these. Conversely, a defect only
reachable through the assembled system belongs in `e2e/`, not approximated in a unit
test.

## Things that bite

- **`ticket_types.quota` is REMAINING quota**, never an original allocation.
- **No cache call inside an order-writing transaction** — the quota `UPDATE` holds a row
  lock, and a Redis round trip inside it serializes every concurrent buyer.
- **No gateway call inside a transaction either**, for the same reason.
- **Domains never import one another.** `cmd/api/architecture_test.go` enforces it.
- **`SCHEMA.md` changes in the same commit as any migration.**

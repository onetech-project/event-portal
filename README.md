# Event Ticketing MVP

Guests browse events, buy tickets without an account, pay through a gateway, and
receive QR tickets by email. Admins manage the catalog, watch sales, and validate
tickets at the door.

The authoritative specifications live in [PRD.md](PRD.md),
[ARCHITECTURE.md](ARCHITECTURE.md), [SCHEMA.md](SCHEMA.md), and
[.specify/memory/constitution.md](.specify/memory/constitution.md), with per-feature
specs under [specs/](specs/).

## Layout

```
backend/     Go modular monolith (Echo v4, sqlc, pgx)
  cmd/api/          entry point, routing, and the ONLY place domains meet
  cmd/seedadmin/    creates a local admin account
  internal/         one package per domain: admin, event, order, payment,
                    ticket, notification
  pkg/              shared non-domain utilities (db, config, logger, money, …)
  migrations/       golang-migrate up/down pairs, schema per SCHEMA.md
frontend/    Next.js App Router, TypeScript, TailwindCSS, TanStack Query
e2e/         Playwright acceptance suite — a real browser against the real stack
```

Each domain owns its tables and exposes `dto.go`, `repository.go`, `service.go`,
and `handler.go`. Domains never import one another: a domain declares the
interface it needs from its neighbours, and `cmd/api/adapters.go` connects them.
`cmd/api/architecture_test.go` enforces that automatically.

## Running it

Everything in containers:

```bash
docker compose up -d          # Postgres, API (:8080), frontend (:3000), Mailpit (:8025)
docker compose exec api /app/seedadmin -email admin@example.com -password 'a-strong-password'
```

Or the app locally against a containerised database:

```bash
docker compose up -d postgres migrate     # migrate exits once the schema is current
cp backend/.env.example backend/.env      # loaded automatically by godotenv
cd backend
go run ./cmd/seedadmin -email admin@example.com -password 'a-strong-password'
go run ./cmd/api                          # :8080

cd ../frontend
npm install
cp .env.example .env.local
npm run dev                               # :3000
```

Guests start at `/events`; admins sign in at `/admin/login`.

Host ports are configurable — `API_PORT`, `FRONTEND_PORT`, `POSTGRES_PORT` — so the
stack can coexist with a server you are already running locally.

## Observability

```bash
docker compose -f docker-compose.yml -f docker-compose.observability.yml up -d
```

| | |
|---|---|
| Grafana | http://localhost:3001 (anonymous admin, local only) |
| Prometheus | http://localhost:9090 |
| Alloy pipeline UI | http://localhost:12345 |

Grafana arrives with all three datasources and a **Ticketing API — Overview**
dashboard already provisioned.

```
API  --OTLP-->  Alloy  -->  Tempo         traces (HTTP spans + every SQL query)
API  --stdout-->  Alloy  -->  Loki        JSON logs, labelled by level and service
API  <--scrape--  Prometheus              RED metrics + Go runtime stats
```

The three signals are joined, which is the point of running all of them:

- Every log line emitted during a request carries `trace_id` and `span_id`, so a
  Loki line links straight to its trace in Tempo.
- Tempo links back to the log lines for a trace, and to its request-rate metrics.
- Prometheus exemplars carry a trace id, so a latency spike on a graph is one
  click from the trace that caused it.

Traces cover the database too: `pool.acquire` and per-query spans nest under the
HTTP span, so a slow checkout is attributed to a statement rather than guessed at.

Without this stack the app runs unchanged — `OTEL_EXPORTER_OTLP_ENDPOINT` is empty
by default, tracing installs a no-op provider, and incoming trace context is still
propagated.

## Docker images

Both are multi-stage.

- **Backend** — static binary built with `CGO_ENABLED=0`, shipped on
  `distroless/static` as a non-root user. No shell, no package manager, ~37 MB.
- **Frontend** — Next.js `output: "standalone"`, so the runtime stage carries only
  the traced `node_modules` and `server.js` and never runs an install. Note that
  `NEXT_PUBLIC_API_BASE_URL` is inlined at *build* time, so changing it needs a
  rebuild, not a restart.

## Tests

```bash
cd backend && ./scripts/test.sh ./...   # Go: unit + database-backed
cd frontend && npx vitest run           # TypeScript: logic + components
cd e2e && npm test                      # Playwright: the whole system, in a browser
```

**The e2e suite is the acceptance gate, not an optional extra** (Constitution
Principle VIII). It drives a real browser against the real Next.js app, the real Go
API, and real PostgreSQL and Redis — the payment provider is the only substitution, and
even that is replaced at the network boundary so settlement still arrives as a signed
webhook the production handler verifies. If you change the purchase journey, the admin
console, or anything the cache serves, update `e2e/specs/` in the same change; if you
fix a bug in one of those flows, add a scenario that fails against the unfixed code.
It needs Postgres and Redis up first — [`e2e/README.md`](e2e/README.md) has the
prerequisites, the coverage table, and `E2E_SLOW_MO` for watching a run at human speed.

The Go suite talks to a real PostgreSQL. `scripts/test.sh` points at a
`ticketing_test` database and runs packages one at a time, since each truncates the
shared schema on setup. Database-backed tests skip themselves when
`TEST_DATABASE_URL` is unset, so the suite still runs on a bare checkout:

```bash
cd backend && ./scripts/setup-test-db.sh   # recreates it and applies every migration
```

## Migrations

[golang-migrate](https://github.com/golang-migrate/migrate), versioned in
`backend/migrations/` as `{version}_{name}.up.sql` + `.down.sql`. Applied versions
are recorded in a `schema_migrations` table, so a migration is applied exactly once
and the database can say which version it is on.

`docker compose up` runs them: a one-shot `migrate` service that the API
`depends_on` with `service_completed_successfully`, so the API never starts against
a stale schema. Nothing else applies migrations — the API binary does not, and the
runtime image no longer even ships them.

To run them by hand, install the CLI once:

```bash
go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
export TICKETING_DB="postgres://ticketing:ticketing@localhost:5433/ticketing?sslmode=disable"

migrate -path backend/migrations -database "$TICKETING_DB" up        # apply everything
migrate -path backend/migrations -database "$TICKETING_DB" version   # current version
migrate -path backend/migrations -database "$TICKETING_DB" down 1    # roll back one
```

Or without installing anything, via the same image Compose uses:

```bash
docker compose run --rm migrate version
```

### Adding a migration

```bash
migrate create -ext sql -dir backend/migrations -seq -digits 6 add_something
```

Both files are required, and the `down` must actually reverse the `up` — that is
the whole point of committing it. `sqlc.yaml` points at the directory rather than a
file list, so a new migration is picked up by `sqlc generate` with no config change
(sqlc replays the `.up.sql` files in order and ignores the `.down.sql` ones).

### Adopting a database that predates version tracking

Databases created before this change carry the schema but no `schema_migrations`
row, so a plain `up` tries to re-create existing tables, fails, and leaves the
version marked *dirty*. Stamp it at the version it is already on instead:

```bash
migrate -path backend/migrations -database "$TICKETING_DB" force 2   # both migrations already applied
migrate -path backend/migrations -database "$TICKETING_DB" up        # "no change"
```

`force` sets the version without running any SQL — it is also how you clear a dirty
flag after a failed migration, once you have checked by hand what did and did not
apply.

## Things worth knowing before changing anything

- **`ticket_types.quota` is the REMAINING quota**, not an original allocation.
  Checkout decrements it; cancel, expire, deny, and failure restore it. There is
  no stored total, and `sold` is always derived from `SUM(order_items.quantity)`.
- **Booking and payment are separate calls since spec 008.** `POST /ticket/book`
  commits order, items, empty attendee slots, and the quota deduction together
  with a 1-hour hold; `POST /ticket/checkout/:order_id` saves the visitor forms
  and only then calls the provider, outside any transaction (holding a quota row
  lock across a network round trip would serialize every concurrent buyer). A
  failed provider call keeps the hold and the saved forms — the guest retries
  from the same order.
- **The webhook is idempotent.** An already-`PAID` order short-circuits, and every
  status change is guarded on the order still being `PENDING`, so a replayed
  notification cannot restore quota twice.
- **Three paths can settle an order**, and they all go through that same guarded
  transition: the provider webhook, the guest pressing "check payment status"
  (which reconciles against the provider), and the in-process expiry sweeper. That
  is what lets them race without restoring quota twice or issuing two sets of
  tickets.
- **The guest never leaves the site to pay.** Checkout opens a QRIS charge and
  routes to `/events/{slug}/orders/{order_number}`, which renders the QR from the stored payload
  on demand, counts down to the server's deadline, and polls until the status is
  final.
- **No object storage exists.** `tickets.qr_code_url` stays NULL and QR images are
  rendered on demand from `ticket_code`; `events.banner_url` is a plain URL an
  admin supplies.
- **SCHEMA.md is locked.** Ticket-code lookups are exact matches against the
  existing index — normalize the input, never wrap the column in `UPPER()`.
- **`e2e/` covers all of the above, and expects to be updated with them.** The
  purchase journey, the admin console, and the cache are under acceptance test. A
  change to any of them ships with its `e2e/specs/` change; a bugfix in any of them
  ships with a scenario that was red before the fix. See Constitution Principle VIII.

## Payment configuration

| Variable | Default | Notes |
|---|---|---|
| `MIDTRANS_BASE_URL` | derived from `MIDTRANS_IS_PRODUCTION` | **Core API** host — `https://api.sandbox.midtrans.com`, not the `app.sandbox` host SNAP used. Pointing it at `app.*` fails with a confusing 404. |
| `PAYMENT_EXPIRY` | `15m` | How long a QRIS code stays payable. 15 minutes is the provider's own default *and* its documented floor: below it the provider's expiry scheduler is unreliable, so startup rejects the value. |
| `PAYMENT_SWEEP_INTERVAL` | `30s` | How often abandoned orders past their deadline are expired and their quota returned. |

## Local end-to-end runs

This section is about driving the flow *by hand*. For the automated version, see
[`e2e/`](e2e/README.md) — it does all of the below without you clicking anything.

`MIDTRANS_BASE_URL` overrides the Core API endpoint, so the whole purchase flow —
checkout, webhook, ticket generation, email — can be exercised against a stub
gateway and a local SMTP catcher without touching the network.

Against the real sandbox, the provider cannot reach a webhook on `localhost`. Pay
the transaction at the QRIS simulator
(<https://simulator.sandbox.midtrans.com/qris/index>) and press **Check payment
status** on the order page: it reconciles directly with the provider through the
same code path the webhook uses, so the flow completes end to end without a public
URL.

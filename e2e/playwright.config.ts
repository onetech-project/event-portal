import { defineConfig, devices } from "@playwright/test";

import { config as env } from "./support/env";

/**
 * The suite brings up the whole stack itself: a Manjo stub, the Go API, and
 * the Next.js dev server, all on ports offset from the normal dev ones so a run
 * never collides with a stack you already have open.
 *
 * PostgreSQL and Redis are NOT started here — they are containers, and owning
 * their lifecycle from a test runner makes failures much harder to read. Bring
 * them up first:
 *
 *   docker compose up -d postgres redis && docker compose run --rm migrate up
 */

const backendDir = "../backend";
const frontendDir = "../frontend";

/**
 * E2E_SLOW_MO pauses before every browser operation so a headed run can be
 * watched. Each pause is spent inside the test, so the timeouts have to grow
 * with it — at the 500ms default of `npm run test:slow` a spec does a few
 * hundred operations, which is minutes, not seconds.
 */
const slowMo = env.slowMo;
const slowFactor = slowMo > 0 ? 1 + slowMo / 125 : 1;
const scaled = (ms: number) => Math.round(ms * slowFactor);

export default defineConfig({
  testDir: "./specs",
  outputDir: "./test-results",

  // Every spec truncates and re-seeds the database, so they cannot share one.
  // Serial execution is the honest choice here rather than a performance bug.
  fullyParallel: false,
  workers: 1,

  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,

  // Generous: a cold `next dev` compiles routes on first visit, and the
  // post-payment chain (tickets, PDF, SMTP) is asynchronous by design.
  timeout: scaled(90_000),
  expect: { timeout: scaled(15_000) },

  reporter: process.env.CI
    ? [["github"], ["html", { open: "never" }], ["list"]]
    : [["html", { open: "never" }], ["list"]],

  use: {
    baseURL: env.frontendURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    actionTimeout: scaled(15_000),
    navigationTimeout: scaled(30_000),
    launchOptions: { slowMo },
  },

  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],

  webServer: env.manageServers
    ? [
        {
          name: "gateway-stub",
          command: "node --experimental-strip-types support/gateway-stub.ts",
          url: `${env.gatewayStubURL}/__stub/health`,
          reuseExistingServer: !process.env.CI,
          timeout: 30_000,
          env: { E2E_GATEWAY_PORT: "8101" },
          stdout: "pipe",
          stderr: "pipe",
        },
        {
          name: "api",
          // `go run` rebuilds on every start, which keeps the suite honest about
          // testing the working tree rather than a stale binary.
          command: "go run ./cmd/api",
          cwd: backendDir,
          url: `${env.apiRootURL}/healthz`,
          reuseExistingServer: !process.env.CI,
          timeout: 120_000,
          stdout: "pipe",
          stderr: "pipe",
          env: {
            APP_PORT: "8100",
            APP_BASE_URL: env.apiRootURL,
            FRONTEND_URL: env.frontendURL,
            DATABASE_URL: env.databaseURL,
            JWT_SECRET: env.jwtSecret,
            LOG_LEVEL: "warn",

            // Payment goes through the stub, never a real gateway.
            PG_BASE_URL: env.gatewayStubURL,
            PG_SERVER_KEY: env.pgServerKey,
            PG_CLIENT_KEY: env.pgClientKey,
            // The whole of the callback's authentication: Manjo notifications
            // carry no signature, so the suite impersonates the gateway by
            // presenting this as a bearer token.
            PG_CALLBACK_TOKEN: env.pgCallbackToken,

            // Short windows so hold-expiry scenarios do not take an hour, but
            // still above PAYMENT_EXPIRY's documented 15-minute floor.
            PAYMENT_EXPIRY: "15m",
            PAYMENT_WINDOW: "14m",
            // Grows with E2E_SLOW_MO: a watched checkout takes far longer than
            // 30s to click through, and an expired hold would fail the run for
            // a reason that has nothing to do with the code under test.
            BOOKING_HOLD: `${Math.round(scaled(30_000) / 1000)}s`,
            PAYMENT_SWEEP_INTERVAL: "3s",

            // Pinned, and load-bearing (spec 016 T067). The API otherwise
            // inherits the developer's zone. Every displayed time is rendered in
            // Asia/Jakarta by explicit conversion, so a suite running on a
            // Jakarta machine would pass whether or not that conversion exists —
            // which is exactly the bug this pin exists to keep visible. UTC also
            // matches the deployed container, where time.Local is UTC.
            TZ: "UTC",

            REDIS_URL: env.cacheEnabled ? env.redisURL : "",
            CACHE_ENABLED: String(env.cacheEnabled),

            // Principle IX. The main API keeps the shipped thresholds so the
            // guest journey is exercised against what actually ships; only the
            // master switch follows the run's mode.
            RATE_LIMIT_ENABLED: String(env.rateLimitEnabled),
            CACHE_TTL: "10m",
            METRICS_ENABLED: "true",

            // Mailpit, and it is REQUIRED — not merely tolerated as it was
            // while nothing inspected the message. The delivery specs read the
            // real MIME back off it (spec 016), so a refused connection is a
            // failure, not a shrug. Bring it up with the rest:
            //   docker compose up -d postgres redis mailpit
            SMTP_HOST: process.env.E2E_SMTP_HOST ?? "localhost",
            SMTP_PORT: process.env.E2E_SMTP_PORT ?? "1025",
            SMTP_FROM: "uat@example.com",
          },
        },
        {
          // The throttle rig: a second API on its own port, with thresholds
          // small enough that a scenario can trip a limit in a second or two.
          //
          // It is a separate PROCESS on purpose. Throttle state is keyed per
          // client and lives in one process's memory, and every request in a
          // local run arrives from the same address — so draining a bucket on
          // the main API would refuse an unrelated scenario until it refilled.
          // Isolating it here is what Principle VIII's "Both throttling modes"
          // bullet asks for.
          name: "api-throttle",
          command: "go run ./cmd/api",
          cwd: backendDir,
          url: `${env.throttleApiURL}/healthz`,
          reuseExistingServer: !process.env.CI,
          timeout: 120_000,
          stdout: "pipe",
          stderr: "pipe",
          env: {
            APP_PORT: "8102",
            APP_BASE_URL: env.throttleApiURL,
            FRONTEND_URL: env.frontendURL,
            DATABASE_URL: env.databaseURL,
            JWT_SECRET: env.jwtSecret,
            LOG_LEVEL: "warn",
            PG_BASE_URL: env.gatewayStubURL,
            PG_SERVER_KEY: env.pgServerKey,
            PG_CLIENT_KEY: env.pgClientKey,
            PG_CALLBACK_TOKEN: env.pgCallbackToken,
            PAYMENT_EXPIRY: "15m",
            PAYMENT_WINDOW: "14m",
            BOOKING_HOLD: `${Math.round(scaled(30_000) / 1000)}s`,
            PAYMENT_SWEEP_INTERVAL: "3s",
            TZ: "UTC",
            REDIS_URL: "",
            CACHE_ENABLED: "false",
            METRICS_ENABLED: "false",
            SMTP_HOST: process.env.E2E_SMTP_HOST ?? "localhost",
            SMTP_PORT: process.env.E2E_SMTP_PORT ?? "1025",
            SMTP_FROM: "uat@example.com",

            // The whole point of this instance. Follows the run's mode so the
            // throttling-off run exercises the disabled path on the same rig.
            RATE_LIMIT_ENABLED: String(env.rateLimitEnabled),
            // Two lookups then refuse: small enough to trip quickly, and a
            // retention comfortably above the 2/1 = 2s refill window.
            RATE_LIMIT_TICKET_LOOKUP_RATE: "1",
            RATE_LIMIT_TICKET_LOOKUP_BURST: "2",
            RATE_LIMIT_IDLE_TTL: "30s",
            // The retention above must clear the LONGEST window it retains, and
            // the resend's default 60s would exceed it — startup refuses, which
            // is how this rig first proved the rule works. Shortened here rather
            // than raising retention, so the rig stays quick.
            RATE_LIMIT_RESEND_WINDOW: "5s",
            // Left ON at shipped values so the per-surface switch has something
            // to contrast against (FR-022).
            RATE_LIMIT_AVAILABILITY_ENABLED: "true",
            // Switched OFF individually, to prove one surface can be relieved
            // while another stays in force.
            RATE_LIMIT_BOOK_ENABLED: "false",
          },
        },
        {
          name: "frontend",
          // Dev mode on purpose: a run rebuilds nothing and picks up a source
          // edit immediately. The API URL is no longer a reason — it is read at
          // start-up now, so a production build could equally be pointed here.
          command: "npm run dev -- --port 3100",
          cwd: frontendDir,
          url: env.frontendURL,
          reuseExistingServer: !process.env.CI,
          timeout: 180_000,
          stdout: "pipe",
          stderr: "pipe",
          env: {
            API_BASE_URL: env.apiURL,

            // Five values the payment frame USED to print, supplied here on
            // purpose. Do not delete them as dead configuration — they are what
            // makes the "the card shows none of this" assertion mean something
            // (spec 019 research D-3).
            //
            // `frontend/.env` is gitignored, so a developer's machine sets these
            // via NEXT_PUBLIC_* and CI sets nothing at all. Without this block
            // the assertion is red locally and GREEN IN CI before the change —
            // a regression pin that pins nothing, which is exactly the
            // "green because the suite never looked" failure Principle VIII
            // names. Supplying them here makes the check red-then-green in both
            // places.
            //
            // The bare names are deliberate: runtimeConfigFromEnv() reads the
            // bare name before the NEXT_PUBLIC_ one, so these win over whatever
            // .env a developer happens to have. Sentinel values rather than
            // plausible ones, so a failure reads as a diagnosis.
            //
            // After spec 019 nothing reads them, which makes this block the
            // FR-013 scenario too: a deployment still exporting the retired
            // names starts normally and ignores them.
            QRIS_MERCHANT_NAME: "E2E-MERCHANT-MUST-NOT-RENDER",
            QRIS_MERCHANT_ID: "E2E-NMID-MUST-NOT-RENDER",
            QRIS_TERMINAL_LABEL: "E2E-TERMINAL-MUST-NOT-RENDER",
            QRIS_ACQUIRER_CODE: "E2E-ACQUIRER-MUST-NOT-RENDER",
            QRIS_PRINT_VERSION: "E2E-VERSION-MUST-NOT-RENDER",
          },
        },
      ]
    : undefined,
});

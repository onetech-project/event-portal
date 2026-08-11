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

            REDIS_URL: env.cacheEnabled ? env.redisURL : "",
            CACHE_ENABLED: String(env.cacheEnabled),
            CACHE_TTL: "10m",
            METRICS_ENABLED: "true",

            // Mailpit if it is up; a refused connection only fails the send,
            // which the suite asserts on via orders.email_sent rather than SMTP.
            SMTP_HOST: process.env.E2E_SMTP_HOST ?? "localhost",
            SMTP_PORT: process.env.E2E_SMTP_PORT ?? "1025",
            SMTP_FROM: "uat@example.com",
          },
        },
        {
          name: "frontend",
          // Dev mode on purpose: NEXT_PUBLIC_* is baked at build time, so a
          // production build could not be pointed at this run's API port.
          command: "npm run dev -- --port 3100",
          cwd: frontendDir,
          url: env.frontendURL,
          reuseExistingServer: !process.env.CI,
          timeout: 180_000,
          stdout: "pipe",
          stderr: "pipe",
          env: {
            NEXT_PUBLIC_API_BASE_URL: env.apiURL,
          },
        },
      ]
    : undefined,
});

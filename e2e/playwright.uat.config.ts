import { defineConfig, devices } from "@playwright/test";

import { config as env } from "./support/env";

/**
 * The deployment profile. Run it with `npm run test:uat`, which sets
 * E2E_TARGET=uat so support/env.ts reads `.env.uat` instead of `.env`.
 *
 * Two things are deliberately absent compared to playwright.config.ts:
 *
 *   - No `webServer`. The API and the frontend are already running and are not
 *     this runner's to start, stop, or configure.
 *   - No database. Every assertion goes through the API, and support/db.ts
 *     refuses to open a pool at all under this profile.
 *
 * Everything else — the page objects, the API client, the notification builder —
 * is shared with the local suite, so a scenario that passes here is running the
 * same code against a different address rather than a second implementation
 * that could drift.
 */

if (!env.remote) {
  throw new Error(
    "playwright.uat.config.ts requires E2E_TARGET=uat. Use `npm run test:uat`.",
  );
}

const slowMo = env.slowMo;
const slowFactor = slowMo > 0 ? 1 + slowMo / 125 : 1;
// Deployments answer over the public internet, behind TLS and a proxy, on
// hardware shared with other traffic. Everything is simply slower than
// localhost, so the baselines start higher than the local suite's before any
// slow-mo scaling.
const scaled = (ms: number) => Math.round(ms * slowFactor);

export default defineConfig({
  testDir: "./uat",
  outputDir: "./test-results-uat",

  // One at a time, as locally — but for a different reason. Nothing here
  // truncates; the constraint is that this run is a guest on a shared
  // environment, and a fan-out of browsers booking real inventory is not a
  // neighbourly way to smoke-test somebody else's deployment.
  fullyParallel: false,
  workers: 1,

  forbidOnly: !!process.env.CI,
  // A retry is worth having against a network the runner does not control, but
  // note what it costs here: a retried purchase books a second real order,
  // because the first attempt's residue cannot be truncated away.
  retries: 1,

  timeout: scaled(180_000),
  expect: { timeout: scaled(30_000) },

  reporter: process.env.CI
    ? [["github"], ["html", { open: "never", outputFolder: "playwright-report-uat" }], ["list"]]
    : [["html", { open: "never", outputFolder: "playwright-report-uat" }], ["list"]],

  use: {
    baseURL: env.frontendURL,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
    video: "retain-on-failure",
    actionTimeout: scaled(30_000),
    navigationTimeout: scaled(60_000),
    launchOptions: { slowMo },
  },

  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],
});

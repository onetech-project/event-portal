import { expect, test } from "@playwright/test";

import { health, publicEventList } from "../support/api";
import { config } from "../support/env";
import { AdminConsole } from "../support/journey";
import { PaymentStatus, deliverNotification } from "../support/payment";
import { assertRemoteProfile, runId, uatAdminToken } from "../support/uat";

/**
 * Reads only. Nothing here creates, books or pays.
 *
 * Its job is to fail first and fail clearly when the target is wrong or down,
 * so that the purchase spec's failures are about the product rather than about
 * an unreachable host or a stale password. Run it alone (`--grep @preflight`)
 * as a zero-residue smoke check of a fresh deploy.
 */

test.describe("@preflight Deployment is reachable and signed into", () => {
  test.beforeAll(() => {
    assertRemoteProfile();
  });

  test("the API answers /healthz", async () => {
    const body = await health();
    // The field is reported rather than asserted: a deployment may legitimately
    // run with the cache off (Principle VII's kill switch), and this suite has
    // no business dictating which.
    expect(body.status, `healthz said ${JSON.stringify(body)}`).toBe("ok");
    test.info().annotations.push({ type: "cache", description: body.cache ?? "unreported" });
  });

  test("the public catalogue is served", async () => {
    const events = await publicEventList();
    expect(Array.isArray(events)).toBe(true);
  });

  test("the guest catalogue page renders in a browser", async ({ page }) => {
    await page.goto("/events");
    // Any heading will do. The point is that the frontend is up, served the
    // route, and reached its API — not what this deployment happens to be
    // selling today.
    await expect(page.getByRole("heading").first()).toBeVisible();
  });

  /**
   * The safety property this whole profile rests on, asserted rather than
   * assumed: support/db.ts empties tables for a living, and a deployment's
   * database is not this suite's to empty. A future spec that reaches for a
   * database helper out of habit fails here first, loudly, instead of
   * TRUNCATEing a shared environment.
   */
  test("the database is unreachable from this profile", async () => {
    const { db } = await import("../support/db");
    expect(() => db()).toThrow(/unavailable when E2E_TARGET=uat/);
  });

  /**
   * That E2E_PG_CALLBACK_TOKEN is actually this deployment's token.
   *
   * Aimed at an order number that cannot exist, because authentication happens
   * before the body is looked at: a 401 means the token is wrong, and anything
   * else means it authenticated — without touching a real order either way.
   *
   * Worth its place in a read-only preflight because the alternative is finding
   * out mid-journey, after a real event, a real order and real held inventory
   * have already been created on somebody else's environment.
   */
  test("the configured callback token authenticates", async () => {
    const status = await deliverNotification({
      orderNumber: `ORD-E2E-PREFLIGHT-NOSUCH-${runId}`,
      status: PaymentStatus.Completed,
    });

    expect(
      status,
      "E2E_PG_CALLBACK_TOKEN is not this deployment's PG_CALLBACK_TOKEN — settlement cannot be driven with it",
    ).not.toBe(401);
  });

  test("the configured admin can sign in, by API and in the console", async ({ page }) => {
    const token = await uatAdminToken();
    expect(token.length).toBeGreaterThan(0);

    const console_ = new AdminConsole(page);
    await console_.signIn(config.admin.email, config.admin.password);
    await expect(page).toHaveURL(/\/admin\/events/);
  });
});

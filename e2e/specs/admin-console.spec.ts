import { expect, test } from "@playwright/test";

import { adminLogin, adminOrders, createSellableEvent } from "../support/api";
import {
  orderStatusOf,
  quotaOf,
  resetDatabase,
  seedAdmin,
  ticketCodesFor,
  ticketStatusOf,
  waitFor,
} from "../support/db";
import { AdminConsole, defaultHolder, GuestJourney } from "../support/journey";
import { settleOrder } from "../support/payment";
import { config } from "../support/env";
import { orderRow } from "../support/db";

/**
 * The other half of the product: the operator's side. These scenarios pick up
 * where the guest journey leaves off — a real, paid order exists, and the admin
 * has to see it and validate the ticket at the door.
 */

let token: string;

test.beforeEach(async () => {
  await resetDatabase();
  await seedAdmin();
  token = await adminLogin();
});

test.describe("Admin console", () => {
  test("signs in and reaches the event list", async ({ page }) => {
    await createSellableEvent(token, { slug: "uat-admin-signin", name: "Admin Sign In" });

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);

    await expect(page.getByText("Admin Sign In")).toBeVisible();
  });

  test("rejects a wrong password without signing in", async ({ page }) => {
    await page.goto("/admin/login");

    const form = page.getByRole("form", { name: /sign in/i });
    await form.getByLabel(/email/i).fill(config.admin.email);
    await form.getByLabel(/password/i).fill("definitely-not-the-password");
    await form.getByRole("button", { name: /sign in/i }).click();

    await expect(page.getByRole("alert").first()).toBeVisible();
    await expect(page).toHaveURL(/\/admin\/login/);
  });

  test("an unauthenticated visitor cannot reach an admin page", async ({ page }) => {
    await page.goto("/admin/orders");
    await page.waitForURL(/\/admin\/login/, { timeout: 20_000 });
  });

  test("a paid order appears in the admin order list", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-admin-orders",
      quota: 5,
    });

    // A real purchase, driven through the browser.
    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();
    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();

    const order = await orderRow(orderNumber);
    await settleOrder(orderNumber, String(Math.trunc(Number(order.total_amount))));
    await waitFor(
      () => orderStatusOf(orderNumber),
      (s) => s === "PAID",
      { what: "the order to be paid" },
    );

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openOrders();

    // No cache flush, no reload loop: the order list was invalidated when the
    // payment committed, so the first load already has it.
    await expect(page.getByText(orderNumber)).toBeVisible();
    await expect(page.getByText(defaultHolder.name).first()).toBeVisible();
  });

  test("validating a ticket marks it used, and a second attempt is refused", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-validation",
      quota: 5,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();
    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();

    const order = await orderRow(orderNumber);
    await settleOrder(orderNumber, String(Math.trunc(Number(order.total_amount))));

    const [code] = await waitFor(
      () => ticketCodesFor(orderNumber),
      (c) => c.length === 1,
      { what: "the ticket to be issued" },
    );

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openValidation();

    const form = page.getByRole("form", { name: /validate ticket/i });
    const codeInput = form.getByRole("textbox");
    const check = form.getByRole("button", { name: "Check ticket" });

    // First lookup: the ticket is valid but not yet admitted.
    await codeInput.fill(code);
    await check.click();
    await expect(page.getByText("Valid", { exact: true })).toBeVisible();
    expect(await ticketStatusOf(code)).toBe("ACTIVE");

    // Admitting is a separate, deliberate action — and irreversible through
    // this flow, which is why it is not folded into the lookup.
    await page.getByRole("button", { name: "Mark used" }).click();
    await waitFor(
      () => ticketStatusOf(code),
      (s) => s === "USED",
      { what: "the ticket to be marked used" },
    );

    // Second lookup of the same code must now read as already used.
    //
    // The exact label matters here: an earlier version of this test asserted
    // /used/i, which matched the "Mark used" BUTTON on the still-Valid card and
    // passed without the transition ever happening.
    await codeInput.fill(code);
    await check.click();
    await expect(page.getByText("Already used", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Mark used" })).toBeHidden();
  });

  test("an unknown ticket code is reported as invalid", async ({ page }) => {
    await createSellableEvent(token, { slug: "uat-unknown-code", quota: 1 });

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openValidation();

    const form = page.getByRole("form", { name: /validate ticket/i });
    await form.getByRole("textbox").fill("TIXDOESNOTEXIST");
    await form.getByRole("button", { name: "Check ticket" }).click();

    await expect(page.getByText("Invalid", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Mark used" })).toBeHidden();
  });

  test("an event with orders cannot be deleted", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-delete-guard",
      quota: 3,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    await guest.agreeToTermsAndBook();

    // The hold alone is enough to block deletion (Principle VI).
    expect(await quotaOf(ticketType.id)).toBe(2);

    const response = await fetch(`${config.apiURL}/admin/events/${event.id}`, {
      method: "DELETE",
      headers: { authorization: `Bearer ${token}` },
    });
    expect(response.status).toBe(400);

    // And it is still listed for the admin.
    const orders = await adminOrders(token, { eventId: event.id });
    expect(orders.length).toBe(1);
  });
});

import { expect, test } from "@playwright/test";

import {
  adminLogin,
  adminOrders,
  createSellableEvent,
  isoHoursFromNow,
  updateEvent,
  updateTicketTypeWindow,
} from "../support/api";
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

    await settleOrder(orderNumber);
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
    // The event is RUNNING: validation admits no tolerance (spec 015 FR-014),
    // so the default +30d seed would make this ticket NOT_YET_VALID and the
    // scenario would be testing the window rather than the admit transition.
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-validation",
      quota: 5,
      startDate: isoHoursFromNow(-1),
      endDate: isoHoursFromNow(6),
      eventStart: isoHoursFromNow(-1),
      eventEnd: isoHoursFromNow(6),
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();
    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();

    await settleOrder(orderNumber);

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


  /**
   * Spec 015 US3. A ticket admits on the day its ticket type says, and the gate
   * refuses it on any other. Validation admits NO tolerance (FR-014), so the
   * event window an admin sets is the moment admission opens, not showtime.
   *
   * These fail against unfixed code, where validation consults no date at all
   * and reports a plain "Valid" whenever the code exists and is unused.
   */
  test("a ticket whose day has not arrived is refused at the gate", async ({ page }) => {
    // The event — and the ticket — sit entirely in the future.
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-not-yet",
      quota: 3,
      startDate: isoHoursFromNow(24),
      endDate: isoHoursFromNow(72),
      eventStart: isoHoursFromNow(48),
      eventEnd: isoHoursFromNow(60),
    });

    const code = await issueOneTicket(page, event.slug, ticketType.name);

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openValidation();

    const form = page.getByRole("form", { name: /validate ticket/i });
    await form.getByRole("textbox").fill(code);
    await form.getByRole("button", { name: "Check ticket" }).click();

    await expect(page.getByText("Not yet valid", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Mark used" })).toBeHidden();

    // Refused, and still admissible on the right day.
    expect(await ticketStatusOf(code)).toBe("ACTIVE");
  });

  test("a ticket whose day has passed is refused at the gate", async ({ page }) => {
    // Sales are still open — the two windows are independent (FR-003) — but the
    // day this ticket admits to is behind us.
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-expired-window",
      quota: 3,
      startDate: isoHoursFromNow(-72),
      endDate: isoHoursFromNow(72),
      eventStart: isoHoursFromNow(-48),
      eventEnd: isoHoursFromNow(-24),
    });

    const code = await issueOneTicket(page, event.slug, ticketType.name);

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openValidation();

    const form = page.getByRole("form", { name: /validate ticket/i });
    await form.getByRole("textbox").fill(code);
    await form.getByRole("button", { name: "Check ticket" }).click();

    await expect(page.getByText("Expired", { exact: true })).toBeVisible();
    await expect(page.getByRole("button", { name: "Mark used" })).toBeHidden();
    expect(await ticketStatusOf(code)).toBe("ACTIVE");
  });

  // FR-017: hiding the button is not the enforcement. The endpoint must refuse
  // an out-of-window ticket even when called directly.
  test("marking an out-of-window ticket used is refused by the endpoint", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-direct-admit",
      quota: 3,
      startDate: isoHoursFromNow(24),
      endDate: isoHoursFromNow(72),
      eventStart: isoHoursFromNow(48),
      eventEnd: isoHoursFromNow(60),
    });

    const code = await issueOneTicket(page, event.slug, ticketType.name);

    const response = await fetch(
      `${config.apiURL}/admin/tickets/${encodeURIComponent(code)}/use`,
      { method: "POST", headers: { authorization: `Bearer ${token}` } },
    );

    expect(response.status).toBe(409);
    expect(await ticketStatusOf(code)).toBe("ACTIVE");
  });


  /**
   * Spec 015 FR-005a/b. Containment is enforced in ONE direction: a ticket type
   * cannot be saved outside its event, but the event can be moved out from under
   * its ticket types. That asymmetry is deliberate — enforcing both ways
   * deadlocks a reschedule, because neither the event nor its tickets could move
   * first. So the event edit succeeds and the console warns instead.
   */
  test("rescheduling an event warns about stranded ticket types instead of refusing", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-reschedule",
      quota: 4,
      startDate: isoHoursFromNow(24),
      endDate: isoHoursFromNow(96),
      eventStart: isoHoursFromNow(48),
      eventEnd: isoHoursFromNow(60),
    });

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);

    // No warning while every window sits inside the event.
    await admin.openEvent(event.id);
    await expect(page.getByText(/admits on days the event no longer runs/i)).toBeHidden();

    // Widening can never strand anything.
    const widened = await updateEvent(token, event.id, {
      name: `UAT ${event.slug}`,
      slug: event.slug,
      status: "PUBLISHED",
      start_date: isoHoursFromNow(24),
      end_date: isoHoursFromNow(120),
    });
    expect(widened.status).toBe(200);

    await admin.openEvent(event.id);
    await expect(page.getByText(/admits on days the event no longer runs/i)).toBeHidden();

    // Moving the event away does — and is still allowed.
    const moved = await updateEvent(token, event.id, {
      name: `UAT ${event.slug}`,
      slug: event.slug,
      status: "PUBLISHED",
      start_date: isoHoursFromNow(240),
      end_date: isoHoursFromNow(312),
    });
    expect(moved.status).toBe(200);

    await admin.openEvent(event.id);
    await expect(page.getByText(/admits on days the event no longer runs/i)).toBeVisible();
    await expect(page.getByText(ticketType.name, { exact: false }).first()).toBeVisible();

    // And the recovery direction now works: the ticket follows the event.
    await updateTicketTypeWindow(token, ticketType.id, {
      eventId: event.id,
      name: ticketType.name,
      price: "150000.00",
      quota: 4,
      salesStart: isoHoursFromNow(-1),
      salesEnd: isoHoursFromNow(300),
      eventStart: isoHoursFromNow(264),
      eventEnd: isoHoursFromNow(288),
    });

    await admin.openEvent(event.id);
    await expect(page.getByText(/admits on days the event no longer runs/i)).toBeHidden();
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

  /**
   * Spec 017. The reason an operator opens this dialog at all: it is the only
   * handle that matches an order in this system to a transaction in the
   * gateway's own records. Without it a payment that never notified has no
   * thread to pull.
   *
   * The payment dialog had no coverage before this — nothing in this file
   * opened it.
   */
  test("the payment view shows the gateway reference for a real order", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-admin-ext-ref",
      quota: 5,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();
    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();
    await guest.expectAwaitingPayment();

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openOrders();

    await page
      .getByRole("row", { name: new RegExp(orderNumber) })
      .getByRole("button", { name: /payment history/i })
      .click();

    // The stub mints its reference from the order number, so the expected value
    // is knowable without reaching into the stub's memory.
    const expected = `stub-eri-${orderNumber}`;
    await expect(page.getByText(expected)).toBeVisible();

    // Stated once for the order, not repeated down the notification rows: it is
    // an order-level fact and only one row carries it.
    await expect(page.getByText(expected)).toHaveCount(1);

    // And in full — an abbreviated identifier cannot be pasted into the
    // gateway's search, which is the entire use.
    await expect(page.getByText(expected)).toHaveText(expected);
  });

  // An order that never reached checkout has no reference and none can be
  // recovered. The dialog still has to render (spec 017 FR-018, SC-006).
  test("the payment view degrades cleanly for an order with no payment session", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-admin-no-ext-ref",
      quota: 5,
    });

    // Booked and left there: no holder forms, so checkout never runs and no
    // session is ever opened.
    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openOrders();

    await page
      .getByRole("row", { name: new RegExp(orderNumber) })
      .getByRole("button", { name: /payment history/i })
      .click();

    await expect(page.getByText(/no payment session was opened/i)).toBeVisible();
    await expect(page.getByText(/seats held vs\. remaining/i)).toBeVisible();
    await expect(page.getByText(/stub-eri-/)).toHaveCount(0);
  });

});

/**
 * Drives the real purchase journey to a settled order and returns its one
 * issued ticket code. Arranged entirely through the UI and the real webhook —
 * writing a ticket row directly would skip the cache invalidation the rest of
 * the system depends on (Principle VIII).
 */
async function issueOneTicket(
  page: import("@playwright/test").Page,
  slug: string,
  ticketName: string,
): Promise<string> {
  const guest = new GuestJourney(page);
  await guest.openTicketSelection(slug);
  await guest.selectQuantity(ticketName, 1);
  const orderNumber = await guest.agreeToTermsAndBook();
  await guest.fillHolder(0, defaultHolder);
  await guest.payWithQris();

  await settleOrder(orderNumber);

  const [code] = await waitFor(
    () => ticketCodesFor(orderNumber),
    (c) => c.length === 1,
    { what: "the ticket to be issued" },
  );
  return code;
}

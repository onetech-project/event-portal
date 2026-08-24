import { expect, test } from "@playwright/test";

import {
  adminLogin,
  adminOrders,
  adminOrdersPage,
  bookAsAnotherGuest,
  createRegistrationEvent,
  createSellableEvent,
  isoHoursFromNow,
  request,
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
 * Pagination across the admin console (spec 021).
 *
 * These run at a deliberately small `page_size`. The alternative — seeding
 * twenty-odd orders to overflow the default page — would take minutes per
 * scenario, and the shortcut that makes it fast (inserting rows straight into
 * the database) is exactly the one Principle VIII forbids: direct writes do not
 * invalidate the cache, so a paging test seeded that way could pass against a
 * stale cache and prove nothing at all. Every order below is booked through the
 * real API.
 */
test.describe("Admin console pagination", () => {
  /**
   * Books n real orders against one event and returns their order numbers.
   *
   * Paced to the shipped booking throttle rather than around it. Booking is
   * limited to 0.33/s with a burst of 5 (Principle IX), and that allowance is
   * per client and process-local — so every scenario in a local run draws on the
   * same bucket, and a scenario that simply looped would be refused because of
   * what an unrelated one spent. Waiting for the refill is what Principle VIII
   * means by isolating throttle-sensitive scenarios; disabling the limit for the
   * main API would stop exercising what actually ships.
   */
  async function bookOrders(
    eventId: string,
    ticketTypeId: string,
    n: number,
  ): Promise<string[]> {
    const numbers: string[] = [];
    for (let i = 0; i < n; i++) {
      numbers.push(await bookOnce(eventId, ticketTypeId));
    }
    return numbers;
  }

  async function bookOnce(eventId: string, ticketTypeId: string): Promise<string> {
    // 0.33/s refills a token every ~3s; six attempts covers a fully drained
    // bucket without hiding a genuine failure, which still throws.
    for (let attempt = 0; ; attempt++) {
      try {
        const { order_id } = await bookAsAnotherGuest(eventId, ticketTypeId, 1);
        return order_id;
      } catch (err) {
        const throttled = err instanceof Error && err.message.includes("429");
        if (!throttled || attempt >= 6) throw err;
        await new Promise((resolve) => setTimeout(resolve, 3_200));
      }
    }
  }

  test("walking pages two at a time repeats nothing and skips nothing", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-paging-disjoint",
      quota: 20,
    });
    const booked = await bookOrders(event.id, ticketType.id, 3);

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await page.goto(`/admin/orders?event_id=${event.id}&page_size=2`);
    await expect(page.getByRole("navigation", { name: /pagination/i })).toBeVisible();

    // Walk forward with the real Next button, collecting what each page shows.
    const seen: string[] = [];
    const { totalPages } = await admin.currentPage();
    // Three orders at two a page: a full page then a partial one.
    expect(totalPages).toBe(2);

    for (let n = 1; n <= totalPages; n++) {
      expect((await admin.currentPage()).page).toBe(n);
      seen.push(...(await admin.visibleRowKeys()));
      if (n < totalPages) {
        await admin.goToNextPage();
        await expect.poll(async () => (await admin.currentPage()).page).toBe(n + 1);
      }
    }

    // The property worth having: each order exactly once across the whole walk.
    // A tie in the ordering would show one twice here and hide another.
    expect(seen).toHaveLength(3);
    expect(new Set(seen).size).toBe(3);
    expect([...seen].sort()).toEqual([...booked].sort());
    expect(await admin.reportedTotal()).toBe(3);
  });

  test("the last page offers no way further forward", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-paging-last",
      quota: 20,
    });
    await bookOrders(event.id, ticketType.id, 3);

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);

    await page.goto(`/admin/orders?event_id=${event.id}&page_size=2&page=1`);
    expect(await admin.canGoBack()).toBe(false);
    expect(await admin.canGoForward()).toBe(true);

    await admin.goToNextPage();
    await expect
      .poll(async () => (await admin.currentPage()).page, { message: "reached page 2" })
      .toBe(2);
    expect(await admin.canGoForward()).toBe(false);
    expect(await admin.canGoBack()).toBe(true);
  });

  test("changing a filter returns to the first page", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-paging-filter",
      quota: 20,
    });
    await bookOrders(event.id, ticketType.id, 3);

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await page.goto(`/admin/orders?event_id=${event.id}&page_size=2&page=2`);
    expect((await admin.currentPage()).page).toBe(2);

    // Every booked order is PENDING, so filtering to PAID empties the list — and
    // page 3 of an empty list is not a place to leave an operator.
    await page.getByLabel("Status").click();
    await page.getByRole("option", { name: "PAID" }).click();

    await expect.poll(() => page.url()).not.toContain("page=2");
    await expect(page.getByText(/no orders match/i)).toBeVisible();
  });

  test("an out-of-range page in the address lands on the last page", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-paging-clamp",
      quota: 20,
    });
    await bookOrders(event.id, ticketType.id, 3);

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);

    // The shape of a bookmark taken before rows were deleted.
    await page.goto(`/admin/orders?event_id=${event.id}&page_size=2&page=999`);

    await expect
      .poll(async () => (await admin.currentPage()).page, {
        message: "the last page, not an error and not an empty table",
      })
      .toBe(2);
    // expect.poll, not a bare await: currentPage() above settles as soon as the
    // PAGINATION reports page 2, but the table body re-renders a tick later. A
    // one-shot read lands on the empty intermediate table and fails with
    // "expected 1, received 0" — under load often enough to be a real flake, and
    // this assertion is the one that catches it.
    await expect
      .poll(async () => (await admin.visibleRowKeys()).length, {
        message: "the last page carries the single remaining row",
      })
      .toBe(1);
    // And the address corrects itself, so it no longer claims page 999.
    await expect.poll(() => page.url()).toContain("page=2");
  });

  test("a copied address reopens the same page with the same filter", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-paging-shared",
      quota: 20,
    });
    await bookOrders(event.id, ticketType.id, 3);

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await page.goto(`/admin/orders?event_id=${event.id}&page_size=2&page=2`);

    const onPageTwo = await admin.visibleRowKeys();
    const shared = page.url();

    // What a colleague opening the link sees.
    await page.goto("/admin/events");
    await page.goto(shared);

    expect((await admin.currentPage()).page).toBe(2);
    expect(await admin.visibleRowKeys()).toEqual(onPageTwo);
  });

  test("the event filter still lists every event once the event list is paginated", async ({
    page,
  }) => {
    // The regression pagination introduces if the filter is fed from one page of
    // the admin event list: events past the first page silently stop being
    // filterable, with nothing to indicate they exist.
    const slugs = Array.from({ length: 4 }, (_, i) => `uat-paging-opt-${i}`);
    for (const slug of slugs) {
      await createSellableEvent(token, { slug, name: `Filterable ${slug}` });
    }

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);

    // One event per page in the events table — the filter must not follow suit.
    await page.goto("/admin/orders?page_size=1");
    await page.getByLabel("Event").click();

    for (const slug of slugs) {
      await expect(page.getByRole("option", { name: `Filterable ${slug}` })).toBeVisible();
    }
  });

  test("resizing a page keeps the row an operator was reading", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-paging-resize",
      quota: 20,
    });
    await bookOrders(event.id, ticketType.id, 3);

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await page.goto(`/admin/orders?event_id=${event.id}&page_size=2&page=2`);

    const firstRowOfPageTwo = (await admin.visibleRowKeys())[0];

    await admin.setPageSize(20);

    // Everything fits on one page now, and the row that was on screen still is.
    await expect.poll(async () => (await admin.currentPage()).totalPages).toBe(1);
    expect(await admin.visibleRowKeys()).toContain(firstRowOfPageTwo);
  });

  test("every list menu paginates the same way", async ({ page }) => {
    // SC-005: one interaction to learn, not four.
    await createSellableEvent(token, { slug: "uat-paging-menus", quota: 5 });

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);

    for (const path of ["/admin/events", "/admin/orders", "/admin/attendees", "/admin/fees"]) {
      await page.goto(path);
      // Fees and attendees may be empty, in which case the control correctly
      // renders nothing; what must never happen is a page that lists rows and
      // offers no way to move through them.
      const rows = await page.locator("tbody tr").count();
      const cards = await page.locator('[data-slot="card"]').count();
      if (rows > 0 || (path === "/admin/events" && cards > 0)) {
        await expect(
          page.getByRole("navigation", { name: /pagination/i }),
          `${path} lists rows but offers no paging control`,
        ).toBeVisible();
      }
    }
  });

  test("the API reports the total matching the filter, not the table", async () => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-paging-total",
      quota: 20,
    });
    await bookOrders(event.id, ticketType.id, 3);

    const paged = await adminOrdersPage(token, {
      eventId: event.id,
      page: 1,
      pageSize: 2,
    });

    expect(paged.items).toHaveLength(2);
    expect(paged.total).toBe(3);
    expect(paged.total_pages).toBe(2);
    expect(paged.page).toBe(1);
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

/**
 * Spec 022 T076. The operator's side of free registration.
 *
 * All four scenarios go through the browser rather than the API, because what is
 * under test is what an operator can SEE and DO — a correct JSON payload behind a
 * table that never renders it is not the requirement.
 */
test.describe("Free registration in the admin console", () => {
  test("a ticket type can be made invitation-only, and the form says what that costs", async ({
    page,
  }) => {
    const { event } = await createRegistrationEvent(token, { slug: "admin-invitation-field" });

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openEvent(event.id);

    // FR-004: the admin table distinguishes invitation types at a glance.
    await expect(page.getByText("Invitation only").first()).toBeVisible();

    // FR-001a: the control is named for its consequence. "Visible" alone reads as
    // a listing preference, and an admin tidying a list would be publishing a
    // free ticket without being told.
    await page.getByRole("button", { name: /^edit/i }).first().click();
    await expect(page.getByText(/free of charge/i)).toBeVisible();
    await expect(page.getByRole("checkbox").first()).toBeVisible();
  });

  // FR-007: the picker must not offer a choice the server will reject. Offering
  // one is how an operator concludes the refusal is a bug.
  test("an invitation-only type is absent from the package composition picker", async ({
    page,
  }) => {
    const { event, purchasable } = await createRegistrationEvent(token, {
      slug: "admin-invitation-package",
    });

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openEvent(event.id);

    await page.getByRole("button", { name: /new package|add package/i }).first().click();
    await page.getByLabel(/ticket types/i).click();

    await expect(page.getByRole("option", { name: purchasable.name })).toBeVisible();
    await expect(page.getByRole("option", { name: "Invitation Access" })).toHaveCount(0);
  });

  // FR-033 / US4 scenario 1, now DERIVED rather than stored (Principle IV v6.0.0).
  // PAID no longer implies money moved, so the row has to say which kind it is —
  // otherwise a free invitation reads as revenue on the operator's own screen.
  test("a registration is listed as a registration, at no charge", async ({ page }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "admin-invitation-order",
    });
    await registerViaApi(event.slug, registration.id, "listed@example.com");

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openOrders();

    const row = page.getByRole("row").filter({ hasText: "PAID" }).first();
    await expect(row).toContainText("Registration");
    await expect(row).toContainText("Free");
  });

  // FR-053 + FR-038a together: the ONLY recovery route for undelivered
  // registration mail. The registrant is never shown the address their ticket
  // went to, so an operator has a name and at best a guess at the address — and
  // then has to be able to resend from what they found.
  test("an operator finds a registrant by partial name and resends the e-ticket", async ({
    page,
  }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "admin-invitation-lookup",
    });
    await registerViaApi(event.slug, registration.id, "findme@example.com");

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);

    await page.goto("/admin/attendees");
    await page.getByLabel(/find someone/i).fill("Halo");

    const row = page.getByRole("row").filter({ hasText: "Halo Registrant" });
    await expect(row).toBeVisible();
    // Legible before the resend: a registration has no receipt to send.
    await expect(row).toContainText("Registered");

    // And the resend itself works for an order with no payment and no receipt.
    await admin.openOrders();
    await page.getByRole("button", { name: /resend email/i }).first().click();
    await expect(page.getByText(/email resent/i)).toBeVisible();
  });
});

/** Registers through the real endpoint — the same call the form makes. */
async function registerViaApi(slug: string, ticketTypeId: string, email: string): Promise<void> {
  const { data } = await request<{ updated_at: string }>(
    `/ticket/terms-condition/${encodeURIComponent(slug)}`,
  );
  const { status } = await request(`/ticket/register/${ticketTypeId}`, {
    method: "POST",
    body: JSON.stringify({
      slug,
      name: "Halo Registrant",
      email,
      phone: "628125567820",
      dob: "1996-04-12",
      gender: "MALE",
      agreed: true,
      event_terms_updated_at: data.updated_at,
    }),
  });
  if (status !== 201) throw new Error(`registration failed with ${status}`);
}

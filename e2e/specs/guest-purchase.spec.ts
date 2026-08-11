import { expect, test } from "@playwright/test";

import {
  adminLogin,
  adminOrders,
  bookAsAnotherGuest,
  createEvent,
  createSellableEvent,
  createTicketType,
  isoDaysFromNow,
  publicTicketTypes,
  putTerms,
  updateTicketTypeWindow,
} from "../support/api";
import {
  attendeeCountFor,
  orderRow,
  orderStatusOf,
  quotaOf,
  resetDatabase,
  seedAdmin,
  ticketCodesFor,
  waitFor,
} from "../support/db";
import { defaultHolder, GuestJourney, type Holder } from "../support/journey";
import { deliverNotification, expireOrder, settleOrder } from "../support/payment";

/**
 * The journey this product exists for: an unauthenticated guest goes from the
 * catalogue to a paid, issued ticket, without ever creating an account
 * (Constitution Principle VI).
 *
 * Everything here runs against the real stack. The only substitution is the
 * payment provider, and even that is substituted at the network boundary — the
 * API still speaks the Core API protocol to it, and settlement still arrives as
 * a correctly signed webhook that the real handler verifies.
 */

let token: string;

test.beforeEach(async () => {
  await resetDatabase();
  await seedAdmin();
  token = await adminLogin();
});

test.describe("Guest purchase, end to end", () => {
  test("browses, books, pays and receives tickets", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-full-journey",
      name: "UAT Full Journey",
      quota: 10,
      price: "150000.00",
    });

    const guest = new GuestJourney(page);

    // --- Browse -------------------------------------------------------------
    await guest.openCatalogue();
    await expect(page.getByText("UAT Full Journey")).toBeVisible();

    await guest.openEvent(event.slug);
    await guest.goToTicketSelection();

    // --- Select and agree ---------------------------------------------------
    await guest.selectQuantity(ticketType.name, 2);

    expect(await quotaOf(ticketType.id)).toBe(10); // nothing held yet

    const orderNumber = await guest.agreeToTermsAndBook();
    expect(orderNumber).toMatch(/^ORD-/);

    // Agreement is what takes the seats, inside the booking transaction.
    expect(await quotaOf(ticketType.id)).toBe(8);
    expect(await orderStatusOf(orderNumber)).toBe("PENDING");
    expect(await attendeeCountFor(orderNumber)).toBe(2);

    // The guest-facing list reflects the hold immediately — the cache was
    // invalidated when the booking committed, not when a TTL elapsed.
    const afterBooking = await publicTicketTypes(event.slug);
    expect(afterBooking[0].quota_remaining).toBe(8);

    // --- Holder forms -------------------------------------------------------
    // Two tickets, two holders. The first form is the buyer and the only address
    // that receives the tickets (Constitution 3.0.0).
    const second: Holder = {
      name: "Siti Rahayu",
      email: "siti@example.com",
      phone: "081298765432",
      gender: "Female",
      dob: "02/03/1998",
    };

    await guest.fillHolder(0, defaultHolder);
    await guest.fillHolder(1, second);

    // --- Payment ------------------------------------------------------------
    await guest.payWithQris();
    await guest.expectAwaitingPayment();

    const order = await orderRow(orderNumber);
    expect(order.buyer_email).toBe(defaultHolder.email);
    expect(order.buyer_name).toBe(defaultHolder.name);

    // The provider settles. The order moves because the real webhook handler
    // verified a real signature — nothing here writes to the database.
    const grossAmount = String(Math.trunc(Number(order.total_amount)));
    expect(await settleOrder(orderNumber, grossAmount)).toBe(200);

    // --- Confirmation -------------------------------------------------------
    // The SSE stream pushes the transition; the page moves on its own.
    await guest.expectConfirmation();

    expect(await orderStatusOf(orderNumber)).toBe("PAID");

    // One ticket per attendee, issued by the non-blocking post-payment work.
    const codes = await waitFor(
      () => ticketCodesFor(orderNumber),
      (c) => c.length === 2,
      { what: "two tickets to be issued" },
    );
    expect(codes).toHaveLength(2);
    expect(new Set(codes).size).toBe(2); // codes are unique

    // Paying does not return quota: the seats are sold, not released.
    expect(await quotaOf(ticketType.id)).toBe(8);

    // And the admin sees the order without any manual refresh of the cache.
    const orders = await adminOrders(token, { status: "PAID" });
    expect(orders.map((o) => o.order_number)).toContain(orderNumber);
  });

  test("a guest can look up an issued ticket by its code", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-lookup",
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

    await page.goto(`/tickets/${encodeURIComponent(code)}`);
    await expect(page.getByText(code)).toBeVisible();
  });

  test("an expired payment returns the seats to the pool", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-expiry",
      quota: 6,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 3);
    const orderNumber = await guest.agreeToTermsAndBook();

    expect(await quotaOf(ticketType.id)).toBe(3);

    await guest.fillHolder(0, defaultHolder);
    await guest.fillHolder(1, { ...defaultHolder, name: "Second Holder" });
    await guest.fillHolder(2, { ...defaultHolder, name: "Third Holder" });
    await guest.payWithQris();

    const order = await orderRow(orderNumber);
    const grossAmount = String(Math.trunc(Number(order.total_amount)));

    expect(await expireOrder(orderNumber, grossAmount)).toBe(200);

    await waitFor(
      () => orderStatusOf(orderNumber),
      (s) => s === "EXPIRED",
      { what: "the order to expire" },
    );

    // Quota is back, and the guest-facing list says so on the next read.
    expect(await quotaOf(ticketType.id)).toBe(6);
    const listed = await publicTicketTypes(event.slug);
    expect(listed[0].quota_remaining).toBe(6);
  });

  test("a webhook with a bad signature is rejected and changes nothing", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-bad-signature",
      quota: 4,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();

    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();

    const order = await orderRow(orderNumber);
    const status = await deliverNotification({
      orderId: orderNumber,
      transactionStatus: "settlement",
      grossAmount: String(Math.trunc(Number(order.total_amount))),
      tamperSignature: true,
    });

    expect(status).toBe(401);
    expect(await orderStatusOf(orderNumber)).toBe("PENDING");
    expect(await ticketCodesFor(orderNumber)).toHaveLength(0);
  });

  /**
   * Spec 013. The guest chose while the seats were there and pressed Buy Ticket
   * after they were gone.
   *
   * Before the availability gate this was only caught inside the booking
   * transaction, fired by Agree — so the guest read the entire Terms &
   * Conditions document, ticked the box, and was refused one press from
   * finishing. The gate has to catch it at the button.
   */
  test("a selection that sold out while choosing is refused before the terms", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-sold-out-gate",
      quota: 2,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);

    // Selected while the seats genuinely existed — this is the whole point. A
    // row that already read "Sold out" could not have been selected at all, so
    // seeding an exhausted quota up front would test nothing.
    await guest.selectQuantity(ticketType.name, 2);

    // Somebody else takes them, through the real booking path.
    await bookAsAnotherGuest(event.id, ticketType.id, 2);
    expect(await quotaOf(ticketType.id)).toBe(0);

    await guest.buyTicket();

    // Assert the refusal is SHOWN before asserting the dialog is not: a bare
    // toBeHidden() would pass on the first poll if it happened to run before the
    // dialog rendered, which is a false green on precisely the bug under test.
    await expect(guest.refusals()).toContainText(/remain/i);
    await expect(page.getByRole("dialog")).toBeHidden();

    // Nothing was held: the check reserves nothing, and the guest never reached
    // the call that would have.
    expect(await quotaOf(ticketType.id)).toBe(0);
  });

  /**
   * The same gate, a different reason. An admin closing a sale window while the
   * guest is mid-selection is indistinguishable, from the guest's side, from the
   * seats running out — and both have to be caught at the button.
   */
  test("a ticket whose sales window closed while choosing is refused before the terms", async ({
    page,
  }) => {
    const event = await createEvent(token, {
      slug: "uat-window-gate",
      name: "UAT Window Gate",
    });
    await putTerms(token, event.id, "<p>terms</p>");
    const open = await createTicketType(token, {
      eventId: event.id,
      name: "Regular",
      price: "150000.00",
      quota: 10,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(open.name, 1);

    // The window shuts behind them — a full replace, as the endpoint requires.
    await updateTicketTypeWindow(token, open.id, {
      eventId: event.id,
      name: open.name,
      price: "150000.00",
      quota: 10,
      salesStart: isoDaysFromNow(-3),
      salesEnd: isoDaysFromNow(-1),
    });

    await guest.buyTicket();

    await expect(guest.refusals()).toContainText(/not currently on sale/i);
    await expect(page.getByRole("dialog")).toBeHidden();
  });

  /**
   * An event with no authored Terms & Conditions has nothing to agree to.
   * Booking already refused it (409001), but only after the guest had been shown
   * a dialog whose body read "not available yet" and whose Agree button could
   * never usefully be pressed. The check stops that dialog existing.
   */
  test("an event with no authored terms is refused before an empty dialog can render", async ({
    page,
  }) => {
    const event = await createEvent(token, {
      slug: "uat-no-terms-gate",
      name: "UAT No Terms Gate",
    });
    const ticketType = await createTicketType(token, {
      eventId: event.id,
      name: "Regular",
      price: "150000.00",
      quota: 10,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    await guest.buyTicket();

    await expect(guest.refusals()).toContainText(/terms & conditions/i);
    await expect(page.getByRole("dialog")).toBeHidden();
  });

  /**
   * Spec 013 User Story 2. An early refusal is worth little if it is a dead end:
   * the guest keeps their selection, adjusts the offending line, and goes on to
   * a paid ticket without reloading or starting over.
   */
  test("a refused guest can adjust the quantity and complete the purchase", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-refusal-recovery",
      quota: 3,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 3);

    // Another buyer leaves exactly one seat, so 3 no longer fits but 1 does.
    await bookAsAnotherGuest(event.id, ticketType.id, 2);
    expect(await quotaOf(ticketType.id)).toBe(1);

    await guest.buyTicket();
    await expect(guest.refusals()).toContainText(/remain/i);

    // The selection survived the refusal — this is the recovery, not a restart.
    await guest.setQuantity(ticketType.name, 1);

    const orderNumber = await guest.agreeToTermsAndBook();
    expect(orderNumber).toMatch(/^ORD-/);
    expect(await quotaOf(ticketType.id)).toBe(0);

    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();
    await guest.expectAwaitingPayment();

    const order = await orderRow(orderNumber);
    const grossAmount = String(Math.trunc(Number(order.total_amount)));
    expect(await settleOrder(orderNumber, grossAmount)).toBe(200);

    await guest.expectConfirmation();
    const codes = await waitFor(
      () => ticketCodesFor(orderNumber),
      (c) => c.length === 1,
      { what: "the recovered purchase's ticket to be issued" },
    );
    expect(codes).toHaveLength(1);
  });

  test("a replayed settlement is idempotent", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-replay",
      quota: 4,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();

    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();

    const order = await orderRow(orderNumber);
    const grossAmount = String(Math.trunc(Number(order.total_amount)));

    expect(await settleOrder(orderNumber, grossAmount)).toBe(200);
    await waitFor(
      () => ticketCodesFor(orderNumber),
      (c) => c.length === 1,
      { what: "the ticket to be issued" },
    );

    // Providers retry. The second delivery must be accepted and do nothing.
    expect(await settleOrder(orderNumber, grossAmount)).toBe(200);

    expect(await orderStatusOf(orderNumber)).toBe("PAID");
    expect(await ticketCodesFor(orderNumber)).toHaveLength(1);
  });
});

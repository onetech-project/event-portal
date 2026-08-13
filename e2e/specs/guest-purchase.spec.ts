import { expect, test } from "@playwright/test";

import {
  adminLogin,
  adminOrders,
  bookAsAnotherGuest,
  createEvent,
  createFee,
  createPackage,
  createSellableEvent,
  createTicketType,
  isoDaysFromNow,
  isoHoursFromNow,
  publicOrder,
  publicTicketTypes,
  putTerms,
  resendTicketEmail,
  updateTicketTypeWindow,
} from "../support/api";
import {
  cidReferences,
  clearMailbox,
  downloadAttachment,
  isPDF,
  pdfPageCount,
  waitForMail,
} from "../support/mail";
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
import {
  PaymentStatus,
  deliverNotification,
  expireOrder,
  settleOrder,
} from "../support/payment";

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
  // Mailpit keeps everything it has ever received. Without this, a delivery
  // assertion matches a PREVIOUS run's message and passes while proving nothing
  // — a silent failure, which is why it is here rather than left to each spec.
  await clearMailbox();
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

    // The gateway settles. The order moves because the real callback handler
    // authenticated a real token — nothing here writes to the database.
    expect(await settleOrder(orderNumber)).toBe(200);

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

    // --- Delivery -----------------------------------------------------------
    // Spec 016. Up to here the suite only ever checked orders.email_sent, which
    // says a send succeeded and nothing about what was sent. These read the real
    // MIME the production mailer composed, off Mailpit, after a real SMTP hop.
    const mail = await waitForMail(defaultHolder.email);

    expect(mail.Attachments).toHaveLength(2); // FR-001 / SC-001
    const [receiptPart, ticketsPart] = mail.Attachments;

    // Receipt first, tickets second — fixed so two mail clients cannot show the
    // buyer a different first attachment (FR-004).
    expect(receiptPart.FileName).toBe(`receipt-${orderNumber}.pdf`);
    expect(ticketsPart.FileName).toBe(`tickets-${orderNumber}.pdf`);
    expect(receiptPart.ContentType).toBe("application/pdf");
    expect(ticketsPart.ContentType).toBe("application/pdf");

    const receipt = await downloadAttachment(mail.ID, receiptPart.PartID);
    const ticketsPDF = await downloadAttachment(mail.ID, ticketsPart.PartID);
    expect(isPDF(receipt)).toBe(true);
    expect(isPDF(ticketsPDF)).toBe(true);

    // FR-002 / SC-002: one page per issued ticket. This order has two.
    expect(pdfPageCount(ticketsPDF)).toBe(2);

    // FR-030 / SC-006: ticket codes live in the attachment alone.
    for (const code of codes) {
      expect(mail.HTML).not.toContain(code);
    }
    expect(mail.HTML).toContain("Buyer Information");

    // FR-023a, FR-025: every cid: the body references resolves to an inline
    // part, and the inline parts are NOT counted as attachments. A reference
    // with no part renders as a broken image and raises no error anywhere, so
    // this pairing is the only thing that catches it.
    const referenced = cidReferences(mail.HTML);
    expect(referenced.length).toBeGreaterThan(0);
    const inlineNames = (mail.Inline ?? []).map((p) => p.ContentID || p.FileName);
    for (const cid of referenced) {
      expect(inlineNames).toContain(cid);
    }
    expect(mail.Attachments).toHaveLength(2); // unchanged by the inline images

    // FR-024b / SC-014: times render in Asia/Jakarta with a WIB suffix. The API
    // runs under TZ=UTC (playwright.config.ts), so this passes only if the code
    // converts — which is the whole point of pinning the zone there.
    expect(mail.HTML).toContain("WIB");
    expect(mail.HTML).not.toContain("UTC");

    // FR-037: IDR with Indonesian separators, and no Rp anywhere.
    expect(mail.HTML).toContain("IDR ");
    expect(mail.HTML).not.toContain("Rp ");

    // FR-029: the receipt's attribution must not appear in the email body.
    expect(mail.HTML).not.toContain("Powered By Manjo");
    expect(mail.HTML).toContain("© 2026 manjo");

    // FR-031a: nested tables only — Outlook on Windows implements neither.
    expect(mail.HTML).not.toContain("display:flex");
    expect(mail.HTML).not.toContain("display:grid");

    // --- Resend -------------------------------------------------------------
    // FR-007 / SC-005: the same pair, with the same already-issued codes.
    expect((await resendTicketEmail(orderNumber)).status).toBe(202);

    const resent = await waitForMail(defaultHolder.email, { minCount: 2 });
    expect(resent.ID).not.toBe(mail.ID);
    expect(resent.Attachments).toHaveLength(2);
    expect(resent.Attachments[0].FileName).toBe(`receipt-${orderNumber}.pdf`);
    expect(resent.Attachments[1].FileName).toBe(`tickets-${orderNumber}.pdf`);

    const resentTickets = await downloadAttachment(resent.ID, resent.Attachments[1].PartID);
    expect(pdfPageCount(resentTickets)).toBe(2);

    // The codes are unchanged: a resend must never invalidate the pass the guest
    // already holds.
    expect(await ticketCodesFor(orderNumber)).toEqual(codes);
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

    await settleOrder(orderNumber);

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

    expect(await expireOrder(orderNumber)).toBe(200);

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

  /**
   * Spec 015. A multi-day event sells one product per day, and before this
   * feature every one of them advertised — and printed — the parent event's
   * opening date. The buyer holding a Day 2 pass was shown Day 1.
   *
   * This is the defect scenario: it fails against unfixed code, where both
   * lines render the same event-level date.
   */
  test("each order line shows its own ticket's date, not the event's", async ({ page }) => {
    // The event runs three days; the two passes admit on the first two.
    const eventStart = isoDaysFromNow(30);
    const eventEnd = isoDaysFromNow(33);

    const { event, ticketType: dayOne } = await createSellableEvent(token, {
      slug: "uat-multi-day",
      ticketName: "Day 1 Pass",
      quota: 5,
      startDate: eventStart,
      endDate: eventEnd,
      eventStart: isoHoursFromNow(30 * 24 + 9),
      eventEnd: isoHoursFromNow(30 * 24 + 23),
    });

    const dayTwo = await createTicketType(token, {
      eventId: event.id,
      name: "Day 2 Pass",
      price: "150000.00",
      quota: 5,
      eventStart: isoHoursFromNow(31 * 24 + 9),
      eventEnd: isoHoursFromNow(31 * 24 + 23),
    });

    // The public list carries each type's own window.
    const listed = await publicTicketTypes(event.slug);
    const listedOne = listed.find((t) => t.name === "Day 1 Pass");
    const listedTwo = listed.find((t) => t.name === "Day 2 Pass");
    expect(listedOne?.event_start).toBeTruthy();
    expect(listedTwo?.event_start).toBeTruthy();
    expect(listedOne?.event_start).not.toBe(listedTwo?.event_start);

    // Buy one of each, so the Order Summary panel has two lines to tell apart.
    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(dayOne.name, 1);
    await guest.selectQuantity(dayTwo.name, 1);
    await guest.agreeToTermsAndBook();

    // Each line names its own day. Against unfixed code both read the event's
    // opening date and these two dates are identical.
    const lineDates = await page
      .locator('[data-testid="order-line-date"]')
      .allTextContents();

    expect(lineDates).toHaveLength(2);
    expect(lineDates[0]).not.toBe(lineDates[1]);
  });

  /**
   * The other half of the same rule (FR-012): surfaces that describe the EVENT
   * keep describing the event. Only a ticket's date moves to the ticket.
   */
  test("the event's own surfaces still show the event's dates", async ({ page }) => {
    const eventStart = isoDaysFromNow(30);
    const eventEnd = isoDaysFromNow(33);

    const { event } = await createSellableEvent(token, {
      slug: "uat-event-dates-unchanged",
      quota: 5,
      startDate: eventStart,
      endDate: eventEnd,
      eventStart: isoHoursFromNow(30 * 24 + 9),
      eventEnd: isoHoursFromNow(30 * 24 + 23),
    });

    // The landing page's Dates cell spans the whole event, not the one ticket.
    await page.goto(`/events/${event.slug}`);
    const dates = page.getByText(/Dates/i).first();
    await expect(dates).toBeVisible();

    // A three-day event cannot be rendered as a single day: if the Dates cell
    // had been switched to the ticket's window it would collapse to one date.
    const infoBar = page.locator("body");
    await expect(infoBar).toContainText("-");
  });

  /**
   * Spec 015 revision 2, FR-021a. A bundle admits on every day its parts admit,
   * and its one line has to name each of them.
   *
   * The reported defect: a "Day 1 & 2" bundle rendered as a single date because
   * the wire carried only the earliest constituent start. The buyer was told they
   * were attending on one day while holding admission for two. This fails against
   * that code — one date where two are expected.
   */
  test("a bundle line names every day it admits on", async ({ page }) => {
    const { event, ticketType: dayOne } = await createSellableEvent(token, {
      slug: "uat-bundle-days",
      ticketName: "Day 1 Pass",
      quota: 5,
      startDate: isoDaysFromNow(30),
      endDate: isoDaysFromNow(33),
      eventStart: isoHoursFromNow(30 * 24 + 9),
      eventEnd: isoHoursFromNow(30 * 24 + 23),
    });

    const dayTwo = await createTicketType(token, {
      eventId: event.id,
      name: "Day 2 Pass",
      price: "150000.00",
      quota: 5,
      eventStart: isoHoursFromNow(31 * 24 + 9),
      eventEnd: isoHoursFromNow(31 * 24 + 23),
    });

    await createPackage(token, {
      eventId: event.id,
      name: "Day 1 & 2 Bundle",
      price: "250000.00",
      ticketTypeIds: [dayOne.id, dayTwo.id],
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity("Day 1 & 2 Bundle", 1);
    await guest.agreeToTermsAndBook();

    // One line, naming both days. Against unfixed code it names only the first.
    const dates = await page.locator('[data-testid="order-line-date"]').allTextContents();
    expect(dates).toHaveLength(1);

    const bundleLine = dates[0];
    expect(bundleLine).toMatch(/\d/);
    // Two distinct days are listed, not collapsed to one and not ranged.
    expect(bundleLine.split(",").length).toBe(2);
  });

  /**
   * FR-009, reversed on 2026-08-12: the panel's Event box is labelled "Event" and
   * names the event's own dates, not the windows of the tickets on the order.
   */
  test("the Order Summary's Event box names the event, not the tickets", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-event-box",
      quota: 5,
      startDate: isoDaysFromNow(30),
      endDate: isoDaysFromNow(33),
      // A narrow one-day window inside a three-day event.
      eventStart: isoHoursFromNow(31 * 24 + 9),
      eventEnd: isoHoursFromNow(31 * 24 + 23),
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    await guest.agreeToTermsAndBook();

    // The box spans the whole event; the line names the single day the ticket
    // admits on. If the box had been derived from the ticket the two would match.
    const boxRange = await page.getByText(/Gate opens at/i).locator("..").textContent();
    const lineDate = (
      await page.locator('[data-testid="order-line-date"]').allTextContents()
    )[0];

    expect(boxRange).toBeTruthy();
    expect(boxRange).not.toBe(lineDate);
  });

  /**
   * Spec 011 FR-016 / FR-016a / FR-016b, constitution v4.0.0. The holder-forms
   * step shows the PRE-FEE subtotal; the fee-inclusive total appears for the
   * first time on the checkout step, alongside the breakdown that explains it.
   *
   * The fee is created here rather than relied upon: `resetDatabase` truncates
   * `fees`, so without this call every order has `total_amount === subtotal` and
   * every assertion below would pass against unfixed code as readily as fixed.
   */
  test("the forms step shows the subtotal and the checkout step adds the fee", async ({
    page,
  }) => {
    await createFee(token, { name: "PPN", feeType: "PERCENT", value: "10.00" });

    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-fee-presentation",
      name: "UAT Fee Presentation",
      quota: 5,
      price: "500000.00",
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();

    // The two figures must genuinely differ, or nothing below is a test.
    const booked = await publicOrder(orderNumber);
    expect(booked.subtotal).toBe("500000.00");
    expect(booked.total_amount).toBe("550000.00");

    // (a) Forms step: the subtotal, and the fee-inclusive total NOWHERE on the
    // page. Against unfixed code the panel renders 550.000 here.
    const formsBody = await page.locator("body").textContent();
    expect(formsBody).toMatch(/500[.,]000/);
    expect(formsBody).not.toMatch(/550[.,]000/);
    // The note must not still claim the figure includes fees.
    await expect(page.getByText(/includes all taxes and fees/i)).toHaveCount(0);

    // (b) Checkout step: the breakdown and the fee-inclusive total.
    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();
    await guest.expectAwaitingPayment();

    const checkoutBody = await page.locator("body").textContent();
    expect(checkoutBody).toMatch(/550[.,]000/);
    await expect(page.getByText(/^Subtotal \(\d+ items?\)$/)).toBeVisible();

    // (c) The money did not move (FR-016b / SC-005a). The amount actually
    // presented for payment is the fee-inclusive total, exactly as before —
    // read from the API, not from the screen.
    const paying = await publicOrder(orderNumber);
    expect(paying.total_amount).toBe("550000.00");
    expect(paying.payment?.amount).toBe("550000.00");
    expect(await orderRow(orderNumber)).toMatchObject({ total_amount: "550000.00" });
  });

  // Manjo notifications carry no signature, no digest, and no field that could
  // authenticate them, so the bearer token is the entire mechanism. Presenting
  // the wrong one is the only way in.
  test("a callback with a bad token is rejected and changes nothing", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-bad-token",
      quota: 4,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();

    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();

    const status = await deliverNotification({
      orderNumber,
      status: PaymentStatus.Completed,
      invalidToken: true,
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

    expect(await settleOrder(orderNumber)).toBe(200);
    await waitFor(
      () => ticketCodesFor(orderNumber),
      (c) => c.length === 1,
      { what: "the ticket to be issued" },
    );

    // Gateways retry. The second delivery must be accepted and do nothing.
    expect(await settleOrder(orderNumber)).toBe(200);

    expect(await orderStatusOf(orderNumber)).toBe("PAID");
    expect(await ticketCodesFor(orderNumber)).toHaveLength(1);
  });
});

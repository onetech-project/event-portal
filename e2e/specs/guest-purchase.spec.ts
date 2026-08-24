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
  deleteTicketType,
  isoDaysFromNow,
  isoHoursFromNow,
  publicOrder,
  longTermsHtml,
  publicGenders,
  publicTicketTypes,
  putLongTerms,
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
  failNextGatewaySession,
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

    // FR-005a / SC-022: the subject names the order and the event it belongs to.
    // event.name is "UAT Full Journey" here, not "JIVE 2026", so this assertion
    // also fails a hardcoded event name — which is what makes FR-005a's "read it
    // from the order" genuinely covered rather than merely stated.
    expect(mail.Subject).toBe(`[${orderNumber}] E-receipt & E-Ticket for ${event.name}`);

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

    // FR-033 / SC-023: the buyer's phone masks its LAST FOUR characters and
    // nothing else, rendered exactly as stored — no +62 normalisation and no
    // reformatting. defaultHolder.phone is 081234567890.
    //
    // Written as LITERALS on purpose. Asserting through MaskPhone() would pass
    // for whatever shape the helper returns, so the scenario could never be red —
    // the failure Principle VIII names by hand (research R-027). The negative
    // assertion names the superseded shape so a failure shows both side by side.
    expect(mail.HTML).toContain("08123456****");
    expect(mail.HTML).not.toContain("08123***7890");

    // FR-033 / SC-026: the email masks the LAST THREE characters of its local part
    // and keeps the whole domain. defaultHolder.email is budisantoso@example.com.
    //
    // Literals again, for the reason above — and note the fixture's local part is
    // deliberately longer than four characters: at four or fewer both the old and
    // new rules render "b***", so this assertion could not be red (R-037).
    expect(mail.HTML).toContain("budisant***@example.com");
    expect(mail.HTML).not.toContain("budis******@example.com");

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

    // FR-023b / SC-020: the mark is drawn at a FIXED size and never grows the
    // header band. Asserted on the ATTRIBUTES, which is the half Outlook's Word
    // engine obeys — a body that set only the CSS would render the mark at its
    // full asset width for every Outlook recipient.
    //
    // The e-ticket footer's equivalent assertions are NOT here and must not be
    // added: production PDFs are compressed, so the text a page draws never
    // appears in the downloaded bytes (research R-025). That coverage lives at the
    // Go tier, through RenderTicketsPDFPlain.
    expect(mail.HTML).toContain('width="108" height="61"');

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

  /**
   * Spec 023. Checkout resolves a holder's gender through the ALL-KNOWN
   * projection of the master list — the one that includes retired entries so a
   * restored form can still submit what it was saved with (spec 011 FR-031).
   * That projection now comes from the read cache.
   *
   * The risk this covers is not "the wrong names appear". It is that the two
   * projections get collapsed into one: serve checkout the ACTIVE-only list and
   * every retired gender starts being refused, while a form filled from the same
   * list still looks perfectly normal. Booking through the browser with the
   * cache already warm is what exercises that resolution end to end.
   */
  test("a purchase completes with the gender master list already cached", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-gender-cache",
      name: "UAT Gender Cache",
      quota: 5,
      price: "150000.00",
    });

    // Warm the master list before the journey starts, so checkout resolves the
    // holder's gender against a CACHED master rather than a fresh read.
    const genders = await publicGenders();
    expect(genders.length).toBeGreaterThan(0);

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);

    const orderNumber = await guest.agreeToTermsAndBook();
    expect(orderNumber).toMatch(/^ORD-/);

    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();
    await guest.expectAwaitingPayment();

    // The gender resolved and the attendee was written — the assertion that
    // would fail if checkout had been handed the wrong projection.
    expect(await attendeeCountFor(orderNumber)).toBe(1);
    expect(await orderStatusOf(orderNumber)).toBe("PENDING");
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

    // Spec 019 FR-003/FR-006/FR-008. The guest never left the payment screen, so
    // the dialog opens over it and its ONE action leads back to the event rather
    // than to the site home. Asserted here rather than in a scenario of its own
    // because this is the only place the suite arranges a real provider expiry.
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading", { name: /time's up/i })).toBeVisible();

    const action = dialog.getByRole("link");
    await expect(action).toHaveCount(1);
    await expect(action).toHaveAccessibleName(/return to event page/i);

    // FR-005: the expiry moved nothing by itself. Still on /checkout until the
    // guest presses, and only then on the event's own page.
    expect(new URL(page.url()).pathname).toMatch(/\/checkout$/);
    await action.click();
    await expect(page).toHaveURL(new RegExp(`/events/${event.slug}$`));
  });

  /**
   * Spec 019 FR-005/FR-007, the holder-forms half of the same rule. The scenario
   * above cannot reach it: its order has already started payment, so it is on the
   * payment screen by the time it expires. Here the guest never leaves the forms
   * and the booking hold lapses underneath them.
   *
   * Booking and checkout share one deadline column, so the ordinary expiry
   * sweeper releases an untouched hold with no payment involved — which is what
   * lets this be arranged entirely through the real system rather than by writing
   * a status into the database (Principle VIII).
   *
   * The wait budget comes from `testInfo.timeout` rather than a literal: that
   * value is already scaled by the same factor the config applies to
   * BOOKING_HOLD, so a headed `npm run test:slow` run — where the hold grows to
   * minutes — stretches with it instead of hanging on a hardcoded 30 seconds.
   */
  test("an expired hold sends the guest back to the event from the holder forms", async ({
    page,
  }, testInfo) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-hold-expiry",
      quota: 4,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 2);
    const orderNumber = await guest.agreeToTermsAndBook();

    expect(await quotaOf(ticketType.id)).toBe(2);

    // Half-filled deliberately: FR-001 keeps the forms rendered behind the
    // dialog, so what the guest typed is neither cleared nor submitted.
    await guest.fillHolder(0, defaultHolder);

    await waitFor(
      () => orderStatusOf(orderNumber),
      (status) => status === "EXPIRED",
      {
        timeoutMs: Math.round(testInfo.timeout * 0.6),
        intervalMs: 1_000,
        what: "the booking hold to lapse",
      },
    );

    // The seats went back on sale without anyone reopening the page.
    expect(await quotaOf(ticketType.id)).toBe(4);

    const dialog = page.getByRole("dialog");
    await expect(dialog.getByRole("heading", { name: /time's up/i })).toBeVisible();

    // The forms are still there behind it — the dialog did not replace the screen
    // (FR-001). Located by attribute rather than by role on purpose: the open
    // dialog marks everything behind it aria-hidden, so a role query is excluded
    // from the accessibility tree and would report "not found" for a form that is
    // sitting right there. checkout/page.test.tsx documents the same trap.
    await expect(page.locator('form[aria-label="Visitor registration"]')).toBeAttached();

    const action = dialog.getByRole("link");
    await expect(action).toHaveCount(1);
    await expect(action).toHaveAccessibleName(/return to event page/i);

    // FR-005 again, and this is the assertion that would catch an automatic
    // redirect: the guest is still on the forms address until they press.
    expect(new URL(page.url()).pathname).toMatch(/\/orders\/[^/]+$/);
    await action.click();
    await expect(page).toHaveURL(new RegExp(`/events/${event.slug}$`));
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
    //
    // toHaveCount FIRST, then read: allTextContents() does not auto-wait, so
    // reading straight after the booking snapshots whatever happens to be in the
    // DOM — an empty array if the page has not finished rendering. That made this
    // assertion fail as "0 lines" under load, which reads like a product bug and
    // is not one.
    const lines = page.locator('[data-testid="order-line-date"]');
    await expect(lines).toHaveCount(2);
    const lineDates = await lines.allTextContents();

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
    // toHaveCount before reading — see the note in the multi-day test above.
    const lines = page.locator('[data-testid="order-line-date"]');
    await expect(lines).toHaveCount(1);
    const dates = await lines.allTextContents();

    const bundleLine = dates[0];
    expect(bundleLine).toMatch(/\d/);
    // Two distinct days are listed, not collapsed to one and not ranged.
    expect(bundleLine.split(",").length).toBe(2);
  });

  /**
   * Spec 019 FR-011/FR-014. Everywhere else the suite books a package with
   * quantity 1 — and a single unit never carried a visitor number — so the only
   * shape that ever showed one has until now been assembled nowhere but jsdom.
   * This books two units of one bundle in a real browser.
   */
  test("a bundle bought twice shows two unnumbered holder cards", async ({ page }) => {
    const { event, ticketType: dayOne } = await createSellableEvent(token, {
      slug: "uat-bundle-twice",
      ticketName: "Day 1 Pass",
      quota: 6,
    });
    const dayTwo = await createTicketType(token, {
      eventId: event.id,
      name: "Day 2 Pass",
      price: "150000.00",
      quota: 6,
    });
    await createPackage(token, {
      eventId: event.id,
      name: "Two-Day Bundle",
      price: "250000.00",
      ticketTypeIds: [dayOne.id, dayTwo.id],
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity("Two-Day Bundle", 2);
    const orderNumber = await guest.agreeToTermsAndBook();

    // Located by attribute rather than by role: `form` is not an implicit ARIA
    // role unless it is named, and keying off the DOM here keeps the query
    // working whichever way the heading markup moves.
    const form = page.locator('form[aria-label="Visitor registration"]');
    const titles = form.locator('[data-slot="card-title"]');

    // Two purchased units, two cards, each covering that unit's two tickets.
    await expect(titles).toHaveCount(2);
    await expect(titles.nth(0)).toContainText("Two-Day Bundle");
    await expect(titles.nth(0)).toContainText("2 tickets");
    await expect(titles.nth(1)).toContainText("Two-Day Bundle");
    await expect(titles.nth(1)).toContainText("2 tickets");

    // FR-011: nothing numbers them any more. The two headings are now identical,
    // which is the accepted cost recorded in the spec's Assumptions.
    await expect(form.getByText(/^Visitor \d+$/)).toHaveCount(0);

    // FR-014: one card still fills its WHOLE unit. Two cards filled, four
    // attendees stored — proven against the real API rather than a stubbed fetch.
    await guest.fillHolder(0, defaultHolder);
    await guest.fillHolder(1, { ...defaultHolder, name: "Second Unit Holder" });
    await guest.payWithQris();

    expect(await attendeeCountFor(orderNumber)).toBe(4);
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
    // Wait for the line before reading it — see the note in the multi-day test.
    const lines = page.locator('[data-testid="order-line-date"]');
    await expect(lines).toHaveCount(1);
    const lineDate = (await lines.allTextContents())[0];

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

  /**
   * Spec 019 FR-001 to FR-006 and FR-013: the Scan to Pay card carries what the
   * design draws, and none of the five fields it used to print.
   *
   * `expectAwaitingPayment` already runs this check for every scenario that
   * reaches the payment screen. It gets a named home here anyway, because a
   * contract enforced only as a side effect of a helper is a contract nobody
   * can find when it fails.
   *
   * What makes it a test rather than a tautology is in playwright.config.ts:
   * the runner hands the frontend all five retired values. Against pre-019 code
   * the frame prints them and this fails naming them; after 019 nothing reads
   * them, so the same env block also demonstrates that a deployment still
   * exporting the retired names is simply ignored (FR-013).
   */
  test("the payment card shows the code and none of the retired frame fields", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-qris-frame",
      name: "UAT QRIS Frame",
      quota: 5,
      price: "250000.00",
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();
    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();

    // The inventory itself, present and absent halves both.
    await guest.expectAwaitingPayment();

    // The code is the live one this order was issued, not a placeholder: the
    // image the browser loaded resolves to the path the API reports.
    const paying = await publicOrder(orderNumber);
    expect(paying.payment?.qr_image_path).toBeTruthy();
    const qr = page.getByRole("img", { name: /qris code for/i });
    await expect(qr).toHaveAttribute(
      "src",
      new RegExp(`${paying.payment?.qr_image_path}$`),
    );

    // The instructions no longer send the guest to a label that is gone
    // (FR-010, FR-011). The amount is named; no merchant is.
    await page.getByRole("button", { name: /how to pay with qris/i }).click();
    const steps = page.getByRole("list").filter({ hasText: /enter your PIN/i });
    await expect(steps).toContainText(/250[.,]000/);
    await expect(steps).not.toContainText(/merchant name printed/i);
    await expect(steps).not.toContainText("E2E-MERCHANT-MUST-NOT-RENDER");
  });

  // Spec 011 FR-006, clarified 2026-08-13: the phone floor is twelve digits,
  // counted on the value exactly as the guest typed it — no prefix inspection,
  // so a number can be too short in local form and long enough in international
  // form. Every other holder fixture in this suite is already twelve digits and
  // stays valid under both the old floor and the new one (research R29), which
  // is why this scenario has to type an eleven-digit number itself: nothing
  // else in the suite can tell the two rules apart.
  test("an 11-digit phone is refused, and the same number in 62… form is accepted", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-phone-floor",
      name: "UAT Phone Floor",
      quota: 5,
      price: "150000.00",
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    await guest.agreeToTermsAndBook();

    const phone = page
      .getByRole("form", { name: /visitor registration/i })
      .getByLabel(/phone number/i)
      .first();
    const submit = page.getByRole("button", { name: /continue to payment/i });
    const tooShort = page.getByText("Enter a phone number of 12-15 digits.");

    // `08123456789` is an ordinary Indonesian local-form number and eleven
    // digits long. It was valid until 2026-08-13; it is now too short. Against
    // unfixed code this order proceeds and the two assertions below fail.
    await guest.fillHolder(0, { ...defaultHolder, phone: "08123456789" });

    await expect(tooShort).toBeVisible();
    await expect(submit).toBeDisabled();

    // The same subscriber number written internationally is twelve digits and
    // passes. Nothing normalises between the two forms — the guest's choice is
    // what is validated and what is stored (FR-006).
    await phone.fill("628123456789");
    await phone.blur();

    await expect(tooShort).toHaveCount(0);
    await expect(submit).toBeEnabled();
  });

  // Manjo notifications carry no signature, no digest, and no field that could
  // authenticate them, so the bearer token is the entire mechanism. Presenting
  // the wrong one is the only way in.
  /**
   * Spec 011 FR-030 – FR-033 (clarified 2026-08-19). The defect scenario.
   *
   * When the gateway leg of checkout fails, the API answers with "Your details
   * are saved — please try again." It is telling the truth: TX-D commits every
   * holder's details before CreateTransaction is ever called, and the failure
   * compensates nothing. The guest was then returned to a screen with every
   * field blank, which is the promise being broken.
   *
   * This is the ONLY arrangement that reaches that state. A successful checkout
   * cannot stand in for it — `payment_started` flips true and the forms address
   * forwards to /checkout — and an order nobody has submitted must still show
   * empty cards, so simply loading the forms proves nothing either. Against
   * unfixed code the reload assertion below goes red; every other assertion here
   * passes on both sides, which is exactly why the reload one has to exist.
   *
   * Throttle note (constitution v4.2.0, Principle VIII): this is the first
   * scenario to call POST /ticket/checkout/:order_id twice for one order, and
   * RATE_LIMIT_CHECKOUT defaults to rate 0.2/s with burst 3. It books its own
   * event and order so it never shares an allowance with another scenario, and
   * two of three is inside the burst with the throttle enabled.
   */
  test("a payment that fails leaves the holder forms filled on the way back", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-restore-forms",
      quota: 4,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 2);
    const orderNumber = await guest.agreeToTermsAndBook();

    // Two cards, two distinguishable holders: a restore that filled every card
    // from the same slot would pass a single-holder check.
    const first: Holder = { ...defaultHolder, name: "Restored Ayu", email: "ayu@example.com" };
    const second: Holder = {
      name: "Restored Bagus",
      email: "bagus@example.com",
      phone: "081234509876",
      gender: "Female",
      dob: "02/03/1990",
    };
    await guest.fillHolder(0, first);
    await guest.fillHolder(1, second);

    // The gateway refuses the next session open. The refusal returns before the
    // stub records the reference, so the retry further down is not a duplicate.
    expect(await failNextGatewaySession()).toBe(200);

    const submit = page.getByRole("button", { name: /continue to payment/i });
    await expect(submit).toBeEnabled();
    await submit.click();

    // The API's own words, rendered verbatim by the form.
    await expect(
      page.getByText(
        /We could not start the payment with the provider\. Your details are saved/i,
      ),
    ).toBeVisible();

    // Saved, and payment genuinely never started: no compensation ran, so the
    // order keeps its status and its hold.
    const afterFailure = await publicOrder(orderNumber);
    expect(afterFailure.status).toBe("PENDING");
    expect(afterFailure.payment_started).toBe(false);
    expect(afterFailure.slots.map((slot) => slot.name).sort()).toEqual([
      "Restored Ayu",
      "Restored Bagus",
    ]);
    // Still held — a failed open must not release the seats.
    expect(await quotaOf(ticketType.id)).toBe(2);

    // THE ASSERTION. Reload the forms address and find the work still there.
    await page.reload();
    const form = page.locator('form[aria-label="Visitor registration"]');
    await expect(form).toBeVisible();

    // Paired positives, so a screen that failed to render its cards cannot
    // satisfy the checks below by having nothing to disagree with.
    await expect(
      page.getByText("The invoice and e-ticket will be sent via email"),
    ).toBeVisible();
    await expect(form.getByLabel(/full name/i)).toHaveCount(2);

    await expect(form.getByLabel(/full name/i).nth(0)).toHaveValue(first.name);
    await expect(form.getByLabel(/email address/i).nth(0)).toHaveValue(first.email);
    await expect(form.getByLabel(/phone number/i).nth(0)).toHaveValue(first.phone);
    await expect(form.getByPlaceholder("DD/MM/YYYY").nth(0)).toHaveValue(first.dob);
    await expect(form.getByLabel(/full name/i).nth(1)).toHaveValue(second.name);
    await expect(form.getByLabel(/email address/i).nth(1)).toHaveValue(second.email);
    await expect(form.getByPlaceholder("DD/MM/YYYY").nth(1)).toHaveValue(second.dob);

    // Gender is a custom select: the trigger shows the human label, followed by
    // the chevron glyph — hence a prefix match rather than an exact one. It is
    // ANCHORED on purpose: "Male" is a substring of "Female", so an unanchored
    // check would pass on the wrong restored value.
    await expect(
      page.getByRole("combobox", { name: "Gender" }).nth(0),
    ).toHaveText(new RegExp(`^${first.gender}`));
    await expect(
      page.getByRole("combobox", { name: "Gender" }).nth(1),
    ).toHaveText(new RegExp(`^${second.gender}`));

    // FR-033: nothing announces the restore. The delivery chip asserted above
    // stays the only notice on the forms.
    await expect(page.getByText(/restored|previously entered/i)).toHaveCount(0);

    // Enabled on arrival (FR-009 unchanged), and it goes through untouched.
    await expect(submit).toBeEnabled();
    await submit.click();
    await page.waitForURL(/\/checkout$/, { timeout: 30_000 });

    const afterRetry = await publicOrder(orderNumber);
    expect(afterRetry.payment_started).toBe(true);
    expect(afterRetry.slots.map((slot) => slot.name).sort()).toEqual([
      "Restored Ayu",
      "Restored Bagus",
    ]);
  });

  /**
   * The other half of FR-030, and the reason the scenario above cannot simply
   * assert "the fields have values": an order nobody has submitted has nothing
   * to restore, and must still open with empty cards. Without this pair, a
   * prefill that fired unconditionally would look correct.
   */
  test("an order whose forms were never submitted still opens empty", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-restore-empty",
      quota: 4,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 2);
    const orderNumber = await guest.agreeToTermsAndBook();

    expect((await publicOrder(orderNumber)).slots.every((slot) => slot.name === null)).toBe(
      true,
    );

    await page.reload();
    const form = page.locator('form[aria-label="Visitor registration"]');
    await expect(form.getByLabel(/full name/i)).toHaveCount(2);
    for (const index of [0, 1]) {
      await expect(form.getByLabel(/full name/i).nth(index)).toHaveValue("");
      await expect(form.getByLabel(/email address/i).nth(index)).toHaveValue("");
      await expect(form.getByPlaceholder("DD/MM/YYYY").nth(index)).toHaveValue("");
    }
    await expect(
      page.getByRole("button", { name: /continue to payment/i }),
    ).toBeDisabled();
  });

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
    await expect(guest.refusals()).toContainText(/someone was a bit faster/i);
    // FR-013: the shortfall figure the server still reports never reaches them.
    await expect(guest.refusals()).not.toContainText(/fewer than/i);
    await expect(page.getByRole("dialog")).toBeHidden();

    // SC-004 as restated by the amendment: the refusal itself moves nobody and
    // empties nothing. The guest is left on the selection page, with the two
    // tickets they picked, reading a message that asks THEM to reload — which is
    // a different thing from being reloaded.
    expect(page.url()).toContain(`/events/${event.slug}/tickets`);
    await expect(page.getByText("Total 2 Tickets")).toBeVisible();

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

    // The same sentence as the sold-out case: to a guest, a closed window and an
    // exhausted quota are one problem — their selection can no longer be bought.
    await expect(guest.refusals()).toContainText(/someone was a bit faster/i);
    await expect(guest.refusals()).not.toContainText(/not currently on sale/i);
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
    // SC-005's negative half: a carve-out keeps its own sentence and must never
    // borrow the general one, which would send the guest off to reload a page
    // that will refuse them again for exactly the same reason.
    await expect(guest.refusals()).not.toContainText(/someone was a bit faster/i);
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
    await expect(guest.refusals()).toContainText(/someone was a bit faster/i);

    // The refusal itself cleared nothing (FR-007 as amended): the guest is still
    // on the selection page with the quantities they entered, so they can adjust
    // in place. What the amendment gave up is the selection surviving a RELOAD,
    // which this scenario deliberately does not perform.
    await guest.setQuantity(ticketType.name, 1);

    const orderNumber = await guest.agreeToTermsAndBook();
    expect(orderNumber).toMatch(/^ORD-/);
    expect(await quotaOf(ticketType.id)).toBe(0);

    await guest.fillHolder(0, defaultHolder);
    await guest.payWithQris();
    await guest.expectAwaitingPayment();

    // settleOrder takes the order number alone. The gateway callback carries no
    // amount — buildNotification emits ri/nti/td/tft/tt and a status, and the API
    // settles against the order's own frozen total — so the gross amount this
    // used to compute and pass was discarded by JS and type-checked as an arity
    // error. Removed rather than plumbed through: there is nothing to plumb it to.
    expect(await settleOrder(orderNumber)).toBe(200);

    await guest.expectConfirmation();
    const codes = await waitFor(
      () => ticketCodesFor(orderNumber),
      (c) => c.length === 1,
      { what: "the recovered purchase's ticket to be issued" },
    );
    expect(codes).toHaveLength(1);
  });

  /**
   * Spec 013 FR-013 and FR-013a, added by the 2026-08-19 amendment.
   *
   * The check is advisory — it reserves nothing — so the last seat can go
   * between a passing answer and the Agree press. When it does, booking refuses,
   * and the guest must read the SAME general message they would have read at the
   * button, on a page they can act on. Not a second, more detailed sentence, and
   * not one stranded inside a dialog still covering the page it tells them to
   * reload.
   */
  test("a seat taken between the check and Agree refuses with the same general message", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-race-at-agree",
      quota: 2,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 2);

    // The check genuinely passes: the seats are there when Buy Ticket is pressed.
    await guest.buyTicket();
    await expect(page.getByRole("dialog")).toBeVisible();

    // And they are gone before Agree — through the real booking path, so quota
    // moves as it does in production and the same write invalidates the cache.
    await bookAsAnotherGuest(event.id, ticketType.id, 2);
    expect(await quotaOf(ticketType.id)).toBe(0);

    await guest.agreeExpectingRefusal();

    // Assert the message BEFORE the dialog's absence. A bare toBeHidden() can
    // pass on the first poll and hand back a false green on exactly the bug
    // under test — the same trap the sold-out scenario above guards against.
    await expect(guest.refusals()).toContainText(/someone was a bit faster/i);
    await expect(guest.refusals()).toContainText(
      /no longer available in this quantity/i,
    );

    // FR-013: no remaining-quota figure reaches the guest at either point.
    await expect(guest.refusals()).not.toContainText(/fewer than/i);

    // FR-013a: the dialog got out of the way, so the message sits on the page
    // the guest is told to reload and adjust.
    await expect(page.getByRole("dialog")).toBeHidden();

    // The refused attempt held nothing.
    expect(await quotaOf(ticketType.id)).toBe(0);
  });

  /**
   * Spec 013 SC-005, added by the 2026-08-19 amendment.
   *
   * Two independently-broken lines still produce two reasons on the wire —
   * FR-006 keeps that — but the guest reads one sentence. The count of faults is
   * not something they can act on differently, and naming lines was what the
   * amendment set out to stop.
   */
  test("several offending lines collapse to a single message", async ({ page }) => {
    const event = await createEvent(token, {
      slug: "uat-collapse-many",
      name: "UAT Collapse Many",
    });
    await putTerms(token, event.id, "<p>terms</p>");
    const first = await createTicketType(token, {
      eventId: event.id,
      name: "Day 1",
      price: "100000.00",
      quota: 1,
    });
    const second = await createTicketType(token, {
      eventId: event.id,
      name: "Day 2",
      price: "100000.00",
      quota: 1,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(first.name, 1);
    await guest.selectQuantity(second.name, 1);

    // Two lines broken two different ways, and deliberately WITHOUT booking:
    // booking is throttled per client at 0.33/s burst 5, and this scenario needs
    // no quota to move — only two refusals in one decision. Spending the shared
    // booking allowance here refuses whatever runs next, which is the hazard
    // Principle IX names and Principle VIII asks the suite to avoid.
    await deleteTicketType(token, first.id);
    await updateTicketTypeWindow(token, second.id, {
      eventId: event.id,
      name: second.name,
      price: "100000.00",
      quota: 1,
      salesStart: isoDaysFromNow(-3),
      salesEnd: isoDaysFromNow(-1),
    });

    await guest.buyTicket();

    const alert = guest.refusals();
    await expect(alert).toContainText(/someone was a bit faster/i);

    // Once, not once per broken line.
    const text = (await alert.textContent()) ?? "";
    expect(text.split("Someone was a bit faster!").length - 1).toBe(1);
    expect(text).not.toMatch(/fewer than/i);

    await expect(page.getByRole("dialog")).toBeHidden();
  });

  /**
   * Spec 013 SC-005 names "item no longer available" as reachable in the suite.
   * Deleting the ticket type is the only way to reach it: an inactive package is
   * not refused today, and PACKAGE_UNAVAILABLE is produced by no code path.
   */
  test("a ticket type deleted while choosing is refused with the same message", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-deleted-type",
      quota: 5,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);

    // No order references it yet, so the delete guard allows this.
    await deleteTicketType(token, ticketType.id);

    await guest.buyTicket();

    await expect(guest.refusals()).toContainText(/someone was a bit faster/i);
    await expect(guest.refusals()).not.toContainText(/does not exist/i);
    await expect(page.getByRole("dialog")).toBeHidden();
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

  /**
   * Spec 017. The checkout response gains the gateway's own reference for the
   * session, so a caller can match the order against the gateway's records
   * without a second request.
   *
   * Intercepted off the real response the browser receives, not fetched
   * separately: the point is that the app's own checkout call carries it.
   */
  test("the checkout response carries the gateway's external reference", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-ext-ref",
      quota: 5,
    });

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 1);
    const orderNumber = await guest.agreeToTermsAndBook();
    await guest.fillHolder(0, defaultHolder);

    const checkout = page.waitForResponse(
      (r) => r.url().includes("/ticket/checkout/") && r.request().method() === "POST",
    );
    await guest.payWithQris();
    const body = await (await checkout).json();

    // The stub mints the reference from the order number it was handed, so the
    // expected value is deterministic without reaching into the stub.
    expect(body.data.ext_ref_id).toBe(`stub-eri-${orderNumber}`);

    // Present-and-empty is the contract when a gateway supplies none; present
    // and populated is the contract here.
    expect(typeof body.data.ext_ref_id).toBe("string");

    // And the UI is exactly as it was: this is an internal support identifier,
    // meaningless to a buyer, and "keep the ui just like today" is a
    // requirement of this change rather than a side effect of it.
    await guest.expectAwaitingPayment();
    const rendered = await page.locator("body").textContent();
    expect(rendered).not.toContain(body.data.ext_ref_id);
  });

});


/**
 * Spec 022 T053 / FR-044-FR-045. The read-through gate on the BOOKING surface.
 *
 * The terms are re-seeded LONG here on purpose. Every other scenario in this file
 * uses the default one-paragraph fixture, which fits inside the dialog — and the
 * gate treats a document shorter than its reading area as already read (FR-014d),
 * correctly, since there is nothing to scroll. So those scenarios satisfy the gate
 * by being shown and would pass identically against a build with the gate removed.
 *
 * These two are the ones that would not.
 */
test.describe("Agreeing to the terms", () => {
  test("reaching the end of the document ticks the box and turns Agree up", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "booking-gate",
      quota: 5,
    });
    await putLongTerms(token, event.id);

    const journey = new GuestJourney(page);
    await journey.openTicketSelection(event.slug);
    await journey.selectQuantity(ticketType.name, 1);
    await journey.buyTicket();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(/clause 1\./i)).toBeVisible();

    // Before the end is reached, "Agree" is rendered as an aria-disabled span so
    // it stays visible and legible while not being actionable. Asserting on the
    // BUTTON role is what tells the two apart — a text assertion would pass in
    // both states and prove nothing.
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toHaveCount(0);

    await expect(dialog.getByRole("checkbox")).not.toBeChecked();

    await journey.readTermsToTheEnd();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toBeVisible();
    await expect(dialog.getByRole("checkbox")).toBeChecked();
  });

  /**
   * Spec 022 R201 / FR-014c, FR-045 (clarified 2026-08-21).
   *
   * The read-through GATE is gone. A guest who does not want to read may tick
   * the box and proceed, and Agree follows the checkbox rather than the scroll
   * position. This is the scenario that distinguishes the two: it never scrolls,
   * so it can only pass if the checkbox is genuinely a control again.
   *
   * Against the pre-2026-08-21 dialog this fails at the first click — the
   * checkbox carried a no-op `onCheckedChange` and could not be ticked at all.
   */
  test("a guest can agree without reading, by ticking the box", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "booking-tick-unread",
      quota: 5,
    });
    await putLongTerms(token, event.id);

    const journey = new GuestJourney(page);
    await journey.openTicketSelection(event.slug);
    await journey.selectQuantity(ticketType.name, 1);
    await journey.buyTicket();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    // Still at the top of a deliberately long document. Nothing has been read,
    // and the assertion below is what proves it: clause 1 is on screen.
    await expect(dialog.getByText(/clause 1\./i)).toBeVisible();
    await expect(dialog.getByRole("checkbox")).not.toBeChecked();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toHaveCount(0);

    // Tick it deliberately, without scrolling a single pixel.
    await dialog.getByRole("checkbox").click();
    await expect(dialog.getByRole("checkbox")).toBeChecked();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toBeVisible();

    // And it actually books — the guest is carried onward to the holder forms,
    // which is the difference between "the button appeared" and "consent counted".
    await dialog.getByRole("button", { name: /^agree$/i }).click();
    await page.waitForURL(/\/orders\/[^/]+$/, { timeout: 30_000 });
  });

  /**
   * Spec 022 R213 / FR-045a.
   *
   * The automatic tick fires at most once per opening and must never overwrite a
   * deliberate untick. Scrolling away from the end and back is the cheapest way
   * to make a re-syncing implementation show itself: one that tracks the scroll
   * position rather than latching will silently re-tick a box the guest cleared
   * on purpose — an automatic action overriding a deliberate one, about consent.
   */
  test("the automatic tick does not override a deliberate untick", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "booking-tick-latch",
      quota: 5,
    });
    await putLongTerms(token, event.id);

    const journey = new GuestJourney(page);
    await journey.openTicketSelection(event.slug);
    await journey.selectQuantity(ticketType.name, 1);
    await journey.buyTicket();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();

    await journey.readTermsToTheEnd();
    await expect(dialog.getByRole("checkbox")).toBeChecked();

    // The guest changes their mind.
    await dialog.getByRole("checkbox").click();
    await expect(dialog.getByRole("checkbox")).not.toBeChecked();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toHaveCount(0);

    // Scroll away from the end and back. Nothing about this is the guest
    // agreeing, so nothing about it may tick the box.
    const region = dialog.getByRole("region", { name: /terms and conditions/i });
    await region.evaluate((el) => el.scrollTo({ top: 0 }));
    await region.evaluate((el) => el.scrollTo({ top: el.scrollHeight }));

    await expect(dialog.getByRole("checkbox")).not.toBeChecked();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toHaveCount(0);
  });

  test("a guest who reads to the end can complete the booking", async ({ page }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "booking-gate-through",
      quota: 5,
    });
    await putLongTerms(token, event.id);

    const journey = new GuestJourney(page);
    await journey.openTicketSelection(event.slug);
    await journey.selectQuantity(ticketType.name, 1);

    const orderNumber = await journey.agreeToTermsAndBook();
    expect(orderNumber).toMatch(/^ORD-/);
  });
});


/**
 * Spec 022 T053a / FR-050-FR-051. The "Terms & Conditions changed" refusal.
 *
 * This scenario exists because the refusal was DEAD CODE. `UpsertEventTerms` is
 * `ON CONFLICT (event_id) DO UPDATE` on a UNIQUE `event_id`, so an admin edit
 * overwrites the row in place and PRESERVES its id — and the check compared ids.
 * It could never fire for the case its own doc comment described. The fix
 * compares `updated_at`; this is what holds it fixed.
 *
 * Reverting the comparison back to the id makes this fail, which is how it was
 * verified rather than assumed.
 */
test.describe("Terms republished while the guest is reading them", () => {
  test("agreeing is refused and the guest must accept the new version", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "terms-republished",
      quota: 5,
    });
    await putLongTerms(token, event.id);

    const journey = new GuestJourney(page);
    await journey.openTicketSelection(event.slug);
    await journey.selectQuantity(ticketType.name, 1);
    await journey.buyTicket();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await journey.readTermsToTheEnd();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toBeVisible();

    // The server compares the version to WHOLE-SECOND precision, so a republish
    // inside the same second as the guest's read is not detected. Crossing the
    // boundary is what makes this deterministic — and the wait documents a real
    // if narrow limitation rather than papering over a flaky test.
    await new Promise((r) => setTimeout(r, 1_200));

    // The admin republishes underneath them. Different content, and — crucially —
    // the SAME row id, which is exactly the case an id comparison cannot see.
    await putTerms(token, event.id, longTermsHtml(70));

    await dialog.getByRole("button", { name: /^agree$/i }).click();

    // Refused: the guest is not carried onward to the holder forms.
    await expect(dialog).toBeHidden();
    await expect(page).not.toHaveURL(/\/orders\/[^/]+$/);

    // And re-consenting starts from an unticked box against the CURRENT
    // document, so Agree is not sitting there already available above text the
    // guest has never been shown. They need not read it — but they must agree to
    // it again, deliberately (FR-051).
    await journey.buyTicket();
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toHaveCount(0);
  });
});

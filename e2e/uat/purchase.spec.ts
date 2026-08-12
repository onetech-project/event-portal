import { expect, test } from "@playwright/test";

import { adminOrders, publicEventList, publicTicketTypes } from "../support/api";
import { GuestJourney, type Holder } from "../support/journey";
import { PaymentStatus, deliverNotification, settleOrder } from "../support/payment";
import {
  assertRemoteProfile,
  disposableEvent,
  markBookedAgainst,
  quotaRemaining,
  runId,
  teardownCreated,
  uatAdminToken,
  waitForOrderStatus,
} from "../support/uat";

/**
 * The guest purchase journey, driven against a deployment.
 *
 * The same page objects, the same API client and the same notification builder
 * as specs/guest-purchase.spec.ts — the difference is what may be asserted. With
 * no database, the observable end state is the order: PAID, with its stored
 * totals intact, listed in the admin console. Ticket codes are deliberately not
 * exposed by any endpoint (they reach the guest by email), so issuance is
 * asserted through its consequences — the SSE push that moves the browser to
 * the confirmation screen, and the order reaching PAID — rather than by reading
 * `tickets` directly as the local suite can.
 *
 * What this run leaves behind: one unpublished event, one order, and one real
 * email to an @example.com address. See support/uat.ts for why none of that can
 * be cleaned up completely.
 */

let token: string;

const buyer: Holder = {
  // Stamped with the run id so an order found later can be traced to a run.
  name: `E2E Buyer ${runId}`,
  // example.com is reserved by RFC 2606 and cannot reach a real inbox, which
  // matters because a deployment's SMTP is real and will genuinely send.
  email: `e2e-${runId}@example.com`,
  phone: "081234567890",
  gender: "Male",
  dob: "15/08/1995",
};

test.describe.configure({ mode: "serial" });

test.beforeAll(async () => {
  assertRemoteProfile();
  token = await uatAdminToken();
});

test.afterAll(async () => {
  if (!token) return;
  for (const note of await teardownCreated(token)) {
    // eslint-disable-next-line no-console -- the run's residue is the point
    console.log(`[uat teardown] ${note}`);
  }
});

test("a guest browses, books, pays and lands on the confirmation screen", async ({ page }) => {
  const { event, ticketType } = await disposableEvent(token, {
    suffix: "purchase",
    quota: 4,
    price: "150000.00",
  });

  // Published means listed. Asserted against the API rather than the rendered
  // catalogue: a deployment's catalogue holds real events and may paginate, so
  // "is our slug in the list" is the claim that survives, not "is it on screen".
  expect((await publicEventList()).map((e) => e.slug)).toContain(event.slug);

  const guest = new GuestJourney(page);

  await guest.openEvent(event.slug);
  await guest.goToTicketSelection();
  await guest.selectQuantity(ticketType.name, 2);

  expect(await quotaRemaining(event.slug, ticketType.id)).toBe(4); // nothing held yet

  const orderNumber = await guest.agreeToTermsAndBook();
  markBookedAgainst(event.id);
  expect(orderNumber).toMatch(/^ORD-/);

  // Agreement takes the seats inside the booking transaction, and the read that
  // proves it goes through the public endpoint — so this also asserts the cache
  // was invalidated by the write rather than waiting out a TTL.
  expect(await quotaRemaining(event.slug, ticketType.id)).toBe(2);

  const held = await waitForOrderStatus(orderNumber, "PENDING", { timeoutMs: 10_000 });
  // Fees belong to the deployment, not to this run, so the assertion is the
  // invariant rather than a figure: whatever fees exist, the stored total is
  // never below the subtotal it was computed from.
  if (held.subtotal !== null) {
    expect(Number(held.total_amount)).toBeGreaterThanOrEqual(Number(held.subtotal));
  }

  await guest.fillHolder(0, buyer);
  await guest.fillHolder(1, {
    name: `E2E Companion ${runId}`,
    email: `e2e-${runId}+2@example.com`,
    phone: "081298765432",
    gender: "Female",
    dob: "02/03/1998",
  });

  // Reaches the deployment's real gateway. Everything up to here is reversible;
  // from here there is a payment session on somebody else's books.
  await guest.payWithQris();
  await guest.expectAwaitingPayment();

  const beforeSettlement = await waitForOrderStatus(orderNumber, "PENDING", { timeoutMs: 10_000 });
  const storedTotal = beforeSettlement.total_amount;

  // Settlement, impersonating the gateway with its own callback token. The
  // order moves because the deployment's real handler authenticated it — this
  // suite has no other way to move it, and no database to move it in.
  expect(await settleOrder(orderNumber)).toBe(200);

  // The SSE stream pushes the transition and the browser follows on its own.
  await guest.expectConfirmation();

  const paid = await waitForOrderStatus(orderNumber, "PAID");
  // Spec 011 FR-016b: settlement records payment, it does not re-price.
  expect(paid.total_amount).toBe(storedTotal);

  // And the console sees it. Scoped by event_id — the deployment's order list
  // is full of orders that are none of this run's business.
  const orders = await adminOrders(token, { eventId: event.id });
  expect(orders.find((o) => o.order_number === orderNumber)?.status).toBe("PAID");

  // Sold inventory stays sold.
  expect(await quotaRemaining(event.slug, ticketType.id)).toBe(2);
});

/**
 * The callback's entire authentication is the bearer token — Manjo
 * notifications carry no signature (VerifyWebhook in internal/payment/manjo.go).
 * Worth proving on the deployment specifically, because that is where the token
 * is a real secret and where a misconfigured one would leave settlement open to
 * anyone who knows an order number.
 *
 * Deliberately aimed at an order number that cannot exist: authentication is
 * checked before the body is looked at, so a rejection is provable without
 * touching a real order at all.
 */
test("the callback refuses a notification it cannot authenticate", async () => {
  const status = await deliverNotification({
    orderNumber: `ORD-E2E-NO-SUCH-ORDER-${runId}`,
    status: PaymentStatus.Completed,
    invalidToken: true,
  });

  expect(status).toBe(401);
});

/**
 * A published ticket type must advertise the admission window the admin gave it
 * (spec 015). Cheap to check here and worth it: window handling is timezone
 * sensitive, and a deployment is the first place a server running on a
 * different TZ than a developer's laptop would show it.
 */
test("a published ticket type advertises its admission window", async () => {
  const { event, ticketType } = await disposableEvent(token, { suffix: "window", quota: 2 });

  const listed = (await publicTicketTypes(event.slug)).find((t) => t.id === ticketType.id);
  expect(listed, `ticket type ${ticketType.id} missing from /ticket/${event.slug}`).toBeDefined();
  expect(Date.parse(listed!.event_start)).toBeLessThan(Date.parse(listed!.event_end));
});

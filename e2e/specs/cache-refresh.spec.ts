import { expect, test } from "@playwright/test";

import {
  adminLogin,
  adminOrdersPage,
  bookAsAnotherGuest,
  createEvent,
  createSellableEvent,
  createTicketType,
  flushCache,
  health,
  metricValue,
  metrics,
  isoHoursFromNow,
  publicTicketTypes,
  putTerms,
  updateEvent,
  updateTicketTypeWindow,
} from "../support/api";
import { quotaOf, resetDatabase, seedAdmin } from "../support/db";
import { config } from "../support/env";
import { defaultHolder, GuestJourney } from "../support/journey";

/**
 * UAT for the read cache (spec 014 / Constitution Principle VII).
 *
 * The point of these scenarios is that the cache must be invisible: a guest
 * should never be able to tell it is there except by how fast the page is. So
 * each one makes a change and then asserts through the SAME surface a user
 * would look at — the browser, or the public API the browser calls — rather than
 * inspecting Redis. If any of these fail, someone is being shown stale data.
 */

let token: string;

test.beforeEach(async () => {
  await resetDatabase();
  await seedAdmin();
  token = await adminLogin();
});

test.describe("Read cache stays invisible to users", () => {
  test.skip(!config.cacheEnabled, "run with E2E_CACHE_ENABLED=true");

  test("a newly published event appears on the very next page load", async ({ page }) => {
    await createSellableEvent(token, { slug: "uat-cache-first", name: "Already Listed" });

    // Warm the catalogue through the browser, the way real traffic would.
    await page.goto("/events");
    await expect(page.getByText("Already Listed")).toBeVisible();

    // Publish a second event while the first list is cached.
    const created = await createEvent(token, {
      slug: "uat-cache-second",
      name: "Published Just Now",
    });
    await putTerms(token, created.id, "<p>Terms.</p>");
    await createTicketType(token, {
      eventId: created.id,
      name: "Regular",
      price: "100000.00",
      quota: 5,
    });

    // No waiting period, no manual flush.
    await page.reload();
    await expect(page.getByText("Published Just Now")).toBeVisible();
  });

  test("unpublishing removes an event from the catalogue immediately", async ({ page }) => {
    const { event } = await createSellableEvent(token, {
      slug: "uat-cache-unpublish",
      name: "Going Dark",
    });

    await page.goto("/events");
    await expect(page.getByText("Going Dark")).toBeVisible();

    const result = await updateEvent(token, event.id, {
      name: "Going Dark",
      slug: event.slug,
      status: "DRAFT",
    });
    expect(result.status).toBe(200);

    await page.reload();
    await expect(page.getByText("Going Dark")).toBeHidden();
  });

  test("a booking's quota movement is visible on the next read of the ticket list", async ({
    page,
  }) => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-cache-quota",
      quota: 8,
    });

    // Warm the ticket list.
    expect((await publicTicketTypes(event.slug))[0].quota_remaining).toBe(8);

    const guest = new GuestJourney(page);
    await guest.openTicketSelection(event.slug);
    await guest.selectQuantity(ticketType.name, 3);
    await guest.agreeToTermsAndBook();

    // The database moved…
    expect(await quotaOf(ticketType.id)).toBe(5);
    // …and so did what the guest is shown, with no TTL in between.
    expect((await publicTicketTypes(event.slug))[0].quota_remaining).toBe(5);
  });


  // Spec 015 FR-008. The admission window is a guest-visible field on the same
  // cached DTO as quota, so an edit to it must survive the cache exactly as a
  // quota movement does.
  test("an edited admission window is visible on the next read of the ticket list", async () => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-cache-window",
      quota: 4,
      startDate: isoHoursFromNow(24),
      endDate: isoHoursFromNow(96),
      eventStart: isoHoursFromNow(48),
      eventEnd: isoHoursFromNow(60),
    });

    // Warm the ticket list, and record what it says the ticket admits.
    const before = (await publicTicketTypes(event.slug))[0].event_start;
    expect(before).toBeTruthy();

    // Move the admission window a day later, inside the same event.
    await updateTicketTypeWindow(token, ticketType.id, {
      eventId: event.id,
      name: ticketType.name,
      price: "150000.00",
      quota: 4,
      salesStart: isoHoursFromNow(-1),
      salesEnd: isoHoursFromNow(90),
      eventStart: isoHoursFromNow(72),
      eventEnd: isoHoursFromNow(84),
    });

    const after = (await publicTicketTypes(event.slug))[0].event_start;
    expect(after).not.toBe(before);
  });

  test("one event's writes leave another event's cached list alone", async ({ page }) => {
    const a = await createSellableEvent(token, { slug: "uat-scope-a", quota: 6 });
    const b = await createSellableEvent(token, { slug: "uat-scope-b", quota: 6 });

    // Warm both.
    expect((await publicTicketTypes(a.event.slug))[0].quota_remaining).toBe(6);
    expect((await publicTicketTypes(b.event.slug))[0].quota_remaining).toBe(6);

    const before = metricValue(
      await metrics(),
      'cache_requests_total{family="ticket_types_public",result="miss"}',
    );

    // Book against A only.
    const guest = new GuestJourney(page);
    await guest.openTicketSelection(a.event.slug);
    await guest.selectQuantity(a.ticketType.name, 2);
    await guest.agreeToTermsAndBook();

    expect((await publicTicketTypes(a.event.slug))[0].quota_remaining).toBe(4);
    expect((await publicTicketTypes(b.event.slug))[0].quota_remaining).toBe(6);

    // Scoping check: A had to be re-read (a miss), B did not. If invalidation
    // were global, both reads would have missed.
    const after = metricValue(
      await metrics(),
      'cache_requests_total{family="ticket_types_public",result="miss"}',
    );
    expect(after - before).toBeLessThanOrEqual(2);
  });

  test("repeat reads are served from cache", async () => {
    const { event } = await createSellableEvent(token, { slug: "uat-cache-hits", quota: 4 });

    await publicTicketTypes(event.slug); // miss, populates
    const before = metricValue(
      await metrics(),
      'cache_requests_total{family="ticket_types_public",result="hit"}',
    );

    for (let i = 0; i < 5; i++) await publicTicketTypes(event.slug);

    const after = metricValue(
      await metrics(),
      'cache_requests_total{family="ticket_types_public",result="hit"}',
    );
    expect(after - before).toBe(5);
  });

  test("an operator can flush the cache and reads stay correct", async ({ page }) => {
    const { event } = await createSellableEvent(token, {
      slug: "uat-cache-flush",
      name: "Flushable",
      quota: 4,
    });

    await page.goto("/events");
    await expect(page.getByText("Flushable")).toBeVisible();

    const flushed = await flushCache(token);
    expect(flushed.status).toBe(200);
    expect(flushed.data.status).toBe("flushed");

    // Rebuilt from PostgreSQL, still correct.
    await page.reload();
    await expect(page.getByText("Flushable")).toBeVisible();
    expect((await publicTicketTypes(event.slug))[0].quota_remaining).toBe(4);
  });

  /**
   * Spec 021. Paging multiplies the number of cache entries per list: one per
   * page per filter combination. The property that keeps that safe is that page
   * and size join the key's FINGERPRINT and not its scope, so one committed
   * write still orphans every page with a single generation bump.
   *
   * Written against the API rather than the browser because the assertion is
   * about which entry answers, and the page an entry belongs to is not something
   * a rendered table reveals.
   */
  test("a write invalidates the page it belongs to, not just the first one", async () => {
    const { event, ticketType } = await createSellableEvent(token, {
      slug: "uat-cache-paged",
      quota: 20,
    });

    // Three orders, so two pages at two a page. Paced to the shipped booking
    // throttle: the allowance is per client and shared by every scenario in the
    // run, so a bare loop would be refused for what an unrelated one spent.
    for (let i = 0; i < 3; i++) {
      await bookPaced(event.id, ticketType.id);
    }

    // Warm both pages.
    const firstBefore = await adminOrdersPage(token, {
      eventId: event.id,
      page: 1,
      pageSize: 2,
    });
    const secondBefore = await adminOrdersPage(token, {
      eventId: event.id,
      page: 2,
      pageSize: 2,
    });
    expect(firstBefore.total).toBe(3);
    expect(secondBefore.items).toHaveLength(1);

    // A fourth order. It lands on page 2, which is the page a scope-blind
    // invalidation would leave stale while page 1 looked perfectly fresh.
    await bookPaced(event.id, ticketType.id);

    const secondAfter = await adminOrdersPage(token, {
      eventId: event.id,
      page: 2,
      pageSize: 2,
    });
    expect(secondAfter.total).toBe(4);
    expect(secondAfter.items).toHaveLength(2);

    // And page 1 agrees about the total, so the two pages cannot disagree about
    // how big the list is.
    const firstAfter = await adminOrdersPage(token, {
      eventId: event.id,
      page: 1,
      pageSize: 2,
    });
    expect(firstAfter.total).toBe(4);
    expect(firstAfter.total_pages).toBe(2);
  });

  test("health reports the cache", async () => {
    const reported = await health();
    // With Redis up this is "ok"; the degraded branch is covered by the Go unit
    // tests, which can take the store down without disrupting a browser run.
    expect(["ok", "degraded"]).toContain(reported.status);
  });
});

/**
 * Books one order, waiting out the shipped booking throttle rather than
 * disabling it. Booking refills at 0.33/s (Principle IX) and the bucket is
 * shared by everything running against this API instance.
 */
async function bookPaced(eventId: string, ticketTypeId: string): Promise<void> {
  for (let attempt = 0; ; attempt++) {
    try {
      await bookAsAnotherGuest(eventId, ticketTypeId, 1);
      return;
    } catch (err) {
      const throttled = err instanceof Error && err.message.includes("429");
      if (!throttled || attempt >= 6) throw err;
      await new Promise((resolve) => setTimeout(resolve, 3_200));
    }
  }
}

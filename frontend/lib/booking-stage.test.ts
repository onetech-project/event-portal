import { describe, expect, it } from "vitest";

import { BOOKING_STEPS } from "@/components/booking/booking-steps";
import { bookingStageFromPathname } from "./booking-stage";

const SLUG = "jive-2026";
const ORDER = "ORD-20260801-A1B2C3D4";

describe("bookingStageFromPathname", () => {
  it("maps the event page to Booking", () => {
    expect(bookingStageFromPathname(`/events/${SLUG}`)).toBe("Booking");
  });

  it("maps the ticket selection to Booking", () => {
    expect(bookingStageFromPathname(`/events/${SLUG}/tickets`)).toBe("Booking");
  });

  it("maps the order page to Payment", () => {
    expect(bookingStageFromPathname(`/events/${SLUG}/orders/${ORDER}`)).toBe("Payment");
  });

  it("maps the confirmation page to Done, not Payment", () => {
    // The order route is a prefix of the done route, so a naive prefix check
    // reports Payment here and the rail never reaches its fourth stage.
    expect(bookingStageFromPathname(`/events/${SLUG}/orders/${ORDER}/done`)).toBe("Done");
  });

  it("ignores a trailing slash", () => {
    expect(bookingStageFromPathname(`/events/${SLUG}/orders/${ORDER}/done/`)).toBe("Done");
    expect(bookingStageFromPathname(`/events/${SLUG}/tickets/`)).toBe("Booking");
    expect(bookingStageFromPathname(`/events/${SLUG}/`)).toBe("Booking");
  });

  it("is not confused by a slug or order number containing a stage name", () => {
    expect(bookingStageFromPathname("/events/checkout")).toBe("Booking");
    expect(bookingStageFromPathname("/events/done/orders/done")).toBe("Payment");
  });

  it("falls back to Booking for an unrecognised path under an event", () => {
    expect(bookingStageFromPathname(`/events/${SLUG}/something-else`)).toBe("Booking");
  });

  it("only ever returns a declared stage", () => {
    const paths = [
      `/events/${SLUG}`,
      `/events/${SLUG}/checkout`,
      `/events/${SLUG}/orders/${ORDER}`,
      `/events/${SLUG}/orders/${ORDER}/done`,
      "/",
      "",
    ];

    for (const path of paths) {
      expect(BOOKING_STEPS).toContain(bookingStageFromPathname(path));
    }
  });
});

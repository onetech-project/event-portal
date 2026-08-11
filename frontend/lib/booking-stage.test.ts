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

  it("maps the ticket holder forms to Registration", () => {
    // Spec 011 FR-020: the forms and the QR screen used to share this address,
    // which forced the rail to report Payment here — reading as "Registration
    // finished" while the guest was still filling it in.
    expect(bookingStageFromPathname(`/events/${SLUG}/orders/${ORDER}`)).toBe("Registration");
  });

  it("maps the QR screen to Payment", () => {
    expect(bookingStageFromPathname(`/events/${SLUG}/orders/${ORDER}/checkout`)).toBe(
      "Payment",
    );
  });

  it("maps the confirmation page to Done, not Registration", () => {
    // The order route is a prefix of both nested routes, so a naive prefix
    // check stops at the forms and the rail never reaches its last two stages.
    expect(bookingStageFromPathname(`/events/${SLUG}/orders/${ORDER}/success`)).toBe("Done");
  });

  it("gives each of the four stages exactly one address", () => {
    const stageOf = (path: string) => bookingStageFromPathname(path);
    expect([
      stageOf(`/events/${SLUG}/tickets`),
      stageOf(`/events/${SLUG}/orders/${ORDER}`),
      stageOf(`/events/${SLUG}/orders/${ORDER}/checkout`),
      stageOf(`/events/${SLUG}/orders/${ORDER}/success`),
    ]).toEqual([...BOOKING_STEPS]);
  });

  it("ignores a trailing slash", () => {
    expect(bookingStageFromPathname(`/events/${SLUG}/orders/${ORDER}/success/`)).toBe("Done");
    expect(bookingStageFromPathname(`/events/${SLUG}/tickets/`)).toBe("Booking");
    expect(bookingStageFromPathname(`/events/${SLUG}/`)).toBe("Booking");
  });

  it("is not confused by a slug or order number containing a stage name", () => {
    expect(bookingStageFromPathname("/events/checkout")).toBe("Booking");
    expect(bookingStageFromPathname("/events/success/orders/success")).toBe("Registration");
    expect(bookingStageFromPathname("/events/checkout/orders/checkout")).toBe(
      "Registration",
    );
  });

  it("falls back to Booking for an unrecognised path under an event", () => {
    expect(bookingStageFromPathname(`/events/${SLUG}/something-else`)).toBe("Booking");
  });

  it("only ever returns a declared stage", () => {
    const paths = [
      `/events/${SLUG}`,
      `/events/${SLUG}/checkout`,
      `/events/${SLUG}/orders/${ORDER}`,
      `/events/${SLUG}/orders/${ORDER}/checkout`,
      `/events/${SLUG}/orders/${ORDER}/success`,
      "/",
      "",
    ];

    for (const path of paths) {
      expect(BOOKING_STEPS).toContain(bookingStageFromPathname(path));
    }
  });
});

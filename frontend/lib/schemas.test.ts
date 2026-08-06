import { describe, expect, it } from "vitest";
import { eventFormSchema, loginSchema, ticketTypeFormSchema } from "./schemas";

// The spec-001 checkoutSchema and its bundle attendee-split tests were removed
// by spec 010: the registration step now collects one visitor per bundle unit
// (components/order/slot-groups.ts + visitor-form.tsx), and the server owns
// the per-slot contract.

describe("loginSchema", () => {
  it("accepts valid credentials", () => {
    expect(
      loginSchema.safeParse({ email: "admin@example.com", password: "secret123" }).success,
    ).toBe(true);
  });

  it("rejects a malformed email", () => {
    expect(loginSchema.safeParse({ email: "admin", password: "secret123" }).success).toBe(
      false,
    );
  });

  it("rejects an empty password", () => {
    expect(
      loginSchema.safeParse({ email: "admin@example.com", password: "" }).success,
    ).toBe(false);
  });
});

describe("eventFormSchema", () => {
  function validEvent() {
    return {
      name: "Jazz Night",
      slug: "jazz-night",
      description: "",
      venue: "Balai Sarbini",
      address: "Jl. Sudirman",
      startDate: "2026-09-01T19:00",
      endDate: "2026-09-01T23:00",
      bannerUrl: "",
      status: "DRAFT" as const,
    };
  }

  it("accepts a well-formed event", () => {
    expect(eventFormSchema.safeParse(validEvent()).success).toBe(true);
  });

  it("rejects a slug with spaces or capitals", () => {
    for (const slug of ["Jazz Night", "JAZZ", "jazz_night", "-jazz"]) {
      expect(eventFormSchema.safeParse({ ...validEvent(), slug }).success).toBe(false);
    }
  });

  it("rejects an end date before the start date", () => {
    const result = eventFormSchema.safeParse({
      ...validEvent(),
      endDate: "2026-08-01T19:00",
    });

    expect(result.success).toBe(false);
    expect(JSON.stringify(result.error?.issues)).toContain("endDate");
  });

  it("rejects an unknown status", () => {
    expect(
      eventFormSchema.safeParse({ ...validEvent(), status: "ARCHIVED" }).success,
    ).toBe(false);
  });

  it("accepts an empty banner URL but rejects a malformed one", () => {
    expect(eventFormSchema.safeParse({ ...validEvent(), bannerUrl: "" }).success).toBe(true);
    expect(
      eventFormSchema.safeParse({ ...validEvent(), bannerUrl: "not a url" }).success,
    ).toBe(false);
  });
});

describe("ticketTypeFormSchema", () => {
  function validTicketType() {
    return {
      name: "Regular",
      price: "150000",
      quota: 100,
      salesStart: "2026-07-01T00:00",
      salesEnd: "2026-08-31T00:00",
    };
  }

  it("accepts a well-formed ticket type", () => {
    expect(ticketTypeFormSchema.safeParse(validTicketType()).success).toBe(true);
  });

  // quota is the REMAINING quota; zero means sold out, which is legitimate.
  it("accepts a zero quota but rejects a negative one", () => {
    expect(ticketTypeFormSchema.safeParse({ ...validTicketType(), quota: 0 }).success).toBe(
      true,
    );
    expect(ticketTypeFormSchema.safeParse({ ...validTicketType(), quota: -1 }).success).toBe(
      false,
    );
  });

  it("accepts a free ticket but rejects a negative price", () => {
    expect(ticketTypeFormSchema.safeParse({ ...validTicketType(), price: "0" }).success).toBe(
      true,
    );
    expect(
      ticketTypeFormSchema.safeParse({ ...validTicketType(), price: "-5" }).success,
    ).toBe(false);
  });

  it("rejects a non-numeric price", () => {
    expect(
      ticketTypeFormSchema.safeParse({ ...validTicketType(), price: "free" }).success,
    ).toBe(false);
  });

  it("rejects a sales window that ends before it starts", () => {
    const result = ticketTypeFormSchema.safeParse({
      ...validTicketType(),
      salesEnd: "2026-06-01T00:00",
    });

    expect(result.success).toBe(false);
    expect(JSON.stringify(result.error?.issues)).toContain("salesEnd");
  });
});

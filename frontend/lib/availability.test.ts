import { describe, expect, it } from "vitest";

import { ApiError } from "./api-client";
import { CHECK_FAILED, failureMessage, reasonMessage, reasonMessages } from "./availability";
import type { AvailabilityReason } from "./types";

function reason(overrides: Partial<AvailabilityReason> = {}): AvailabilityReason {
  return {
    item_index: 0,
    ticket_type_id: null,
    package_id: null,
    code: "INSUFFICIENT_QUOTA",
    message: "Only fewer than 2 ticket(s) remain.",
    ...overrides,
  };
}

describe("reasonMessage", () => {
  // Every code in the contract arrives already worded for a guest, and each
  // sentence differs from the others — which is what FR-012 asks for. The
  // client's job is to show them, not to re-word them: a second copy here is
  // where the check and the booking refusal would drift apart (FR-013).
  const codes: Array<[string, string]> = [
    ["INSUFFICIENT_QUOTA", "Only fewer than 2 ticket(s) remain."],
    ["TICKET_TYPE_NOT_ON_SALE", 'Ticket type "Early Bird" is not currently on sale.'],
    ["PACKAGE_NOT_ON_SALE", 'Package "Weekend Pass" is not currently on sale.'],
    ["TICKET_TYPE_NOT_FOUND", "Ticket type 0c2a does not exist."],
    ["PACKAGE_NOT_FOUND", "Package 7e55 does not exist."],
    ["VALIDATION_ERROR", "All items must belong to the event being booked."],
    ["TERMS_MISSING", "This event has no Terms & Conditions to agree to yet."],
  ];

  it.each(codes)("passes the server's sentence through for %s", (code, message) => {
    expect(reasonMessage(reason({ code, message }))).toBe(message);
  });

  it("gives each code its own distinct sentence", () => {
    const messages = codes.map(([code, message]) => reasonMessage(reason({ code, message })));
    expect(new Set(messages).size).toBe(codes.length);
  });

  it("falls back rather than showing a guest an empty line", () => {
    expect(reasonMessage(reason({ message: "" }))).toContain("no longer available");
    expect(reasonMessage(reason({ message: "   " }))).toContain("no longer available");
  });

  it("never leaks a raw code onto the screen", () => {
    const shown = reasonMessage(reason({ code: "SOME_FUTURE_CODE", message: "" }));
    expect(shown).not.toContain("SOME_FUTURE_CODE");
  });

  it("keeps the server's order when wording a whole decision", () => {
    expect(
      reasonMessages([reason({ message: "first" }), reason({ message: "second" })]),
    ).toEqual(["first", "second"]);
  });
});

describe("failureMessage", () => {
  it("echoes the server's own sentence, so the check and booking agree", () => {
    const error = new ApiError(400, 400002, "Only fewer than 3 ticket(s) remain.");
    expect(failureMessage(error)).toBe("Only fewer than 3 ticket(s) remain.");
  });

  it("words a transport failure as unknown, not as a refusal", () => {
    // status 0 is the API client's marker for "the server never answered".
    expect(failureMessage(new ApiError(0, 0, "Network request failed"))).toBe(CHECK_FAILED);
  });

  it("falls back for anything that is not an ApiError", () => {
    expect(failureMessage(new TypeError("boom"))).toBe(CHECK_FAILED);
    expect(failureMessage(null)).toBe(CHECK_FAILED);
    expect(failureMessage(undefined)).toBe(CHECK_FAILED);
  });
});

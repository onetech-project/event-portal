import { describe, expect, it } from "vitest";

import { API_CODES, ApiError } from "./api-client";
import {
  CHECK_FAILED,
  GENERAL_REFUSAL,
  THROTTLED,
  failureMessage,
  isAvailabilityRefusal,
  refusalMessages,
} from "./availability";
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

/**
 * The five codes that mean "what you chose can no longer be bought". FR-012
 * collapses all of them onto one sentence, so the server's own wording — which
 * still arrives, and which FR-006 keeps as a record — is never what a guest
 * reads.
 */
const AVAILABILITY_CODES: Array<[string, string]> = [
  ["INSUFFICIENT_QUOTA", "Only fewer than 2 ticket(s) remain."],
  ["TICKET_TYPE_NOT_ON_SALE", 'Ticket type "Early Bird" is not currently on sale.'],
  ["PACKAGE_NOT_ON_SALE", 'Package "Weekend Pass" is not currently on sale.'],
  ["TICKET_TYPE_NOT_FOUND", "Ticket type 0c2a does not exist."],
  ["PACKAGE_NOT_FOUND", "Package 7e55 does not exist."],
];

describe("refusalMessages — the FR-012 collapse", () => {
  it.each(AVAILABILITY_CODES)(
    "shows the one general message for %s, not the server's sentence",
    (code, message) => {
      expect(refusalMessages([reason({ code, message })])).toEqual([GENERAL_REFUSAL]);
    },
  );

  it("says it exactly as FR-012 quotes it", () => {
    // SC-005 asks for a character-for-character match, so pin the characters.
    expect(GENERAL_REFUSAL.title).toBe("Someone was a bit faster!");
    expect(GENERAL_REFUSAL.body).toBe(
      "One of your selected tickets is no longer available in this quantity. " +
        "Please refresh the page and adjust your order.",
    );
  });

  it("shows it once however many lines are at fault", () => {
    // The server emits one reason per contributing line, so a bundle and a
    // standalone drawing on one short ticket type produce several. The count of
    // faults is not the guest's business.
    const many = [
      reason({ item_index: 0 }),
      reason({ item_index: 1 }),
      reason({ item_index: 2, code: "TICKET_TYPE_NOT_ON_SALE", message: "gone" }),
    ];
    expect(refusalMessages(many)).toEqual([GENERAL_REFUSAL]);
  });

  it("never lets a remaining-quota figure through", () => {
    // FR-013's negative half, at the one place the number could escape.
    const shown = refusalMessages([reason()]);
    const text = shown.map((m) => `${m.title ?? ""} ${m.body}`).join(" ");
    expect(text).not.toMatch(/fewer than/i);
    expect(text).not.toMatch(/\d+ ticket/i);
  });

  it("treats a code it has never heard of as an availability refusal", () => {
    // The safe polarity: a decision the client cannot classify is still a
    // refusal, and the general message is a true thing to say about one.
    const shown = refusalMessages([reason({ code: "SOME_FUTURE_CODE", message: "" })]);
    expect(shown).toEqual([GENERAL_REFUSAL]);
    expect(JSON.stringify(shown)).not.toContain("SOME_FUTURE_CODE");
  });

  it("shows nothing for a decision with no reasons", () => {
    expect(refusalMessages([])).toEqual([]);
  });
});

describe("refusalMessages — the FR-012a carve-outs", () => {
  it("keeps the terms sentence, and never the general one", () => {
    const shown = refusalMessages([
      reason({
        code: "TERMS_MISSING",
        item_index: null,
        message: "This event has no Terms & Conditions to agree to yet.",
      }),
    ]);
    expect(shown).toEqual([{ body: "This event has no Terms & Conditions to agree to yet." }]);
    expect(shown).not.toContainEqual(GENERAL_REFUSAL);
  });

  it("keeps its own sentence for a malformed selection", () => {
    // The spec's Assumptions block: an item from another event is a bad request,
    // not a ticket somebody else got first.
    const shown = refusalMessages([
      reason({ code: "VALIDATION_ERROR", message: "All items must belong to the event being booked." }),
    ]);
    expect(shown).toEqual([{ body: "All items must belong to the event being booked." }]);
  });

  it("carries an availability refusal and a carve-out together", () => {
    // EvaluateAvailability appends its terms verdict whether or not the lines
    // were refused, so both can arrive in one decision.
    const shown = refusalMessages([
      reason(),
      reason({ code: "TERMS_MISSING", item_index: null, message: "No terms yet." }),
    ]);
    expect(shown).toEqual([GENERAL_REFUSAL, { body: "No terms yet." }]);
  });

  it("shows a repeated carve-out once", () => {
    const shown = refusalMessages([
      reason({ code: "VALIDATION_ERROR", message: "Bad line." }),
      reason({ code: "VALIDATION_ERROR", message: "Bad line." }),
    ]);
    expect(shown).toHaveLength(1);
  });

  it("falls back rather than showing a guest an empty line", () => {
    const shown = refusalMessages([reason({ code: "TERMS_MISSING", message: "   " })]);
    expect(shown[0].body).toContain("could not be checked");
  });
});

describe("isAvailabilityRefusal — booking classification (research.md D8)", () => {
  it.each([
    ["insufficient quota", API_CODES.insufficientQuota],
    ["a closed sale window, which shares 400001", API_CODES.validation],
    ["a ticket type or package that is gone", API_CODES.notFound],
  ])("counts %s as availability", (_label, code) => {
    expect(isAvailabilityRefusal(new ApiError(400, code, "whatever"))).toBe(true);
  });

  it.each([
    ["no authored terms", API_CODES.termsMissing],
    ["a throttled booking", API_CODES.rateLimited],
    ["an internal fault", API_CODES.internal],
    ["a transport failure", API_CODES.network],
  ])("does not count %s as availability", (_label, code) => {
    expect(isAvailabilityRefusal(new ApiError(500, code, "whatever"))).toBe(false);
  });

  it("does not count something that is not an ApiError", () => {
    expect(isAvailabilityRefusal(new TypeError("boom"))).toBe(false);
    expect(isAvailabilityRefusal(null)).toBe(false);
  });
});

describe("failureMessage — the check path", () => {
  it("words a transport failure as unknown, not as a refusal", () => {
    // status 0 is the API client's marker for "the server never answered".
    expect(failureMessage(new ApiError(0, API_CODES.network, "Network request failed"))).toEqual(
      CHECK_FAILED,
    );
  });

  it("words a throttled check itself, rather than leaking the limiter's prose", () => {
    const throttled = new ApiError(429, API_CODES.rateLimited, "rate limit exceeded");
    expect(failureMessage(throttled)).toEqual(THROTTLED);
    expect(failureMessage(throttled).body).not.toContain("rate limit exceeded");
  });

  it("never echoes the server's sentence", () => {
    // This is the regression that mattered: the echo is how the quota figure
    // used to reach the guest.
    const quota = new ApiError(400, API_CODES.insufficientQuota, "Only fewer than 3 ticket(s) remain.");
    expect(failureMessage(quota).body).not.toContain("fewer than");
  });

  it("falls back for anything that is not an ApiError", () => {
    expect(failureMessage(new TypeError("boom"))).toEqual(CHECK_FAILED);
    expect(failureMessage(null)).toEqual(CHECK_FAILED);
    expect(failureMessage(undefined)).toEqual(CHECK_FAILED);
  });
});

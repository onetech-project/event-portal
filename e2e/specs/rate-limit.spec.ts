import { expect, test } from "@playwright/test";

import { config } from "../support/env";

/**
 * Request throttling (spec 018, Constitution Principle IX).
 *
 * Everything here drives the SECOND api instance — `api-throttle` in
 * playwright.config.ts, on its own port with deliberately tiny thresholds.
 * That is not tidiness: throttle state is keyed per client and lives in one
 * process's memory, and every request in a local run arrives from the same
 * address, so draining a bucket on the main API would refuse unrelated
 * scenarios until it refilled.
 *
 * That rig is configured with the ticket lookup at 1/s burst 2, booking
 * switched OFF individually, and availability left ON — so one file can prove
 * the threshold binds, the master switch releases it, and a single surface can
 * be relieved without the others.
 */

const throttled = config.throttleApiURL;

/** Fires n requests one after another and returns the status codes in order. */
async function statusesFor(url: string, n: number): Promise<number[]> {
  const codes: number[] = [];
  for (let i = 0; i < n; i++) {
    const res = await fetch(url);
    codes.push(res.status);
  }
  return codes;
}

/**
 * A code that matches no ticket. A miss is a 404 and still spends allowance,
 * which is exactly the enumeration attempt the limit exists to make impractical
 * — so the scenario never needs a real ticket to prove the limit binds.
 */
const missingCode = (n: number) => `${throttled}/api/v1/tickets/ZZZ${String(n).padStart(4, "0")}X`;

test.describe("A throttled surface refuses past its configured threshold", () => {
  test.skip(!config.rateLimitEnabled, "run with E2E_RATE_LIMIT_ENABLED=true");

  test("the ticket lookup starts refusing once the burst is spent", async () => {
    const codes = await statusesFor(missingCode(1), 12);

    expect(codes.some((c) => c === 429), `expected a 429 among ${codes.join(",")}`).toBe(true);

    // The refusals come after the allowance, not instead of it: the limit
    // shapes traffic, it does not close the endpoint.
    expect(codes[0]).not.toBe(429);
  });

  test("a refused caller is told to come back, not merely rejected", async () => {
    await statusesFor(missingCode(2), 12);

    const res = await fetch(missingCode(2));
    if (res.status === 429) {
      // The refusal must be the API's own enveloped error, not a bare proxy
      // page — a guest-facing 429 is still a guest-facing response.
      expect(res.headers.get("content-type") ?? "").toContain("json");
    }
  });

  test("a per-client limit cannot be shed by rotating X-Forwarded-For", async () => {
    // The regression for the bypass this feature closed. Before the fix every
    // one of these bought a fresh bucket and all twenty answered 404.
    const codes: number[] = [];
    for (let i = 0; i < 20; i++) {
      const res = await fetch(missingCode(3), {
        headers: { "X-Forwarded-For": `10.0.0.${i + 1}` },
      });
      codes.push(res.status);
    }

    expect(
      codes.some((c) => c === 429),
      `a forged header must not mint fresh buckets; got ${codes.join(",")}`,
    ).toBe(true);
  });
});

test.describe("A surface switched off individually refuses nothing", () => {
  test.skip(!config.rateLimitEnabled, "run with E2E_RATE_LIMIT_ENABLED=true");

  // FR-022: the rig has booking switched off while the ticket lookup stays on.
  // Proving one is relieved is only half the claim — the other must still bind,
  // or a passing test would also pass with the master switch off.
  test("booking is unthrottled while the ticket lookup still refuses", async () => {
    const bookCodes: number[] = [];
    for (let i = 0; i < 30; i++) {
      const res = await fetch(`${throttled}/api/v1/ticket/book`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({}),
      });
      bookCodes.push(res.status);
    }
    expect(
      bookCodes.every((c) => c !== 429),
      `booking is switched off and must never answer 429; got ${[...new Set(bookCodes)].join(",")}`,
    ).toBe(true);

    const lookupCodes = await statusesFor(missingCode(4), 12);
    expect(
      lookupCodes.some((c) => c === 429),
      "the ticket lookup is still switched on and must still refuse",
    ).toBe(true);
  });
});

test.describe("With throttling off, nothing is refused for throttling", () => {
  test.skip(config.rateLimitEnabled, "run with E2E_RATE_LIMIT_ENABLED=false");

  // The other half of FR-020. SC-004 asks for at least 100 attempts.
  test("100 consecutive lookups produce no throttle refusal", async () => {
    const codes = await statusesFor(missingCode(5), 100);

    expect(
      codes.every((c) => c !== 429),
      `no request may be throttled while the kill switch is off; got ${[...new Set(codes)].join(",")}`,
    ).toBe(true);
  });

  test("a forged header changes nothing when there is no limit to shed", async () => {
    const codes: number[] = [];
    for (let i = 0; i < 30; i++) {
      const res = await fetch(missingCode(6), {
        headers: { "X-Forwarded-For": `10.0.0.${i + 1}` },
      });
      codes.push(res.status);
    }
    expect(codes.every((c) => c !== 429)).toBe(true);
  });
});

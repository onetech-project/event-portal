import { describe, expect, it } from "vitest";
import {
  formatCurrency,
  formatDateRange,
  formatDateTime,
  formatVisitorCount,
  toApiDateTime,
} from "./format";

describe("formatCurrency", () => {
  it("renders a decimal string as rupiah", () => {
    const formatted = formatCurrency("150000.00");

    expect(formatted).toContain("150.000");
    expect(formatted).toMatch(/Rp/);
  });

  it("does not show cents for whole-rupiah amounts", () => {
    expect(formatCurrency("150000.00")).not.toContain(",00");
  });

  it("renders a free ticket as zero rather than as blank", () => {
    expect(formatCurrency("0.00")).toContain("0");
  });

  it("falls back to the raw value when it is not a number", () => {
    expect(formatCurrency("not-a-number")).toBe("not-a-number");
  });
});

describe("formatDateTime", () => {
  it("renders an RFC3339 timestamp in a readable form", () => {
    const formatted = formatDateTime("2026-09-01T19:00:00Z");

    expect(formatted).toContain("2026");
    expect(formatted).not.toBe("");
  });

  it("returns a dash for a null timestamp", () => {
    expect(formatDateTime(null)).toBe("—");
  });

  it("returns the raw value when it cannot be parsed", () => {
    expect(formatDateTime("tomorrow")).toBe("tomorrow");
  });
});

describe("formatDateRange", () => {
  // Midday instants so the local zone cannot flip the calendar day.
  it("collapses a same-month range to one month and year", () => {
    expect(formatDateRange("2026-09-26T05:00:00Z", "2026-09-27T05:00:00Z")).toMatch(
      /^26 - 27 Sep(t)? 2026$/,
    );
  });

  it("shows a single date when start and end fall on the same day", () => {
    expect(formatDateRange("2026-09-26T05:00:00Z", "2026-09-26T10:00:00Z")).toMatch(
      /^26 Sept? 2026$/,
    );
  });

  it("spells the month on both sides when the range crosses one", () => {
    const formatted = formatDateRange("2026-09-28T05:00:00Z", "2026-10-02T05:00:00Z");

    expect(formatted).toMatch(/Sep/);
    expect(formatted).toMatch(/Oct/);
  });

  it("spells the year on both sides when the range crosses one", () => {
    const formatted = formatDateRange("2026-12-30T05:00:00Z", "2027-01-02T05:00:00Z");

    expect(formatted).toContain("2026");
    expect(formatted).toContain("2027");
  });
});

describe("formatVisitorCount", () => {
  it("dot-groups counts below one million", () => {
    expect(formatVisitorCount(100_000)).toBe("100.000");
    expect(formatVisitorCount(30_000)).toBe("30.000");
  });

  it("compacts millions, billions, and trillions", () => {
    expect(formatVisitorCount(1_000_000)).toBe("1M");
    expect(formatVisitorCount(1_500_000)).toBe("1,5M");
    expect(formatVisitorCount(2_000_000_000)).toBe("2B");
    expect(formatVisitorCount(3_000_000_000_000)).toBe("3T");
  });
});

describe("toApiDateTime", () => {
  // <input type="datetime-local"> yields a local wall-clock string with no zone;
  // the API needs an absolute RFC3339 instant.
  it("converts a datetime-local value to an ISO instant", () => {
    const iso = toApiDateTime("2026-09-01T19:00");

    expect(iso).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/);
    expect(new Date(iso).getTime()).not.toBeNaN();
  });

  it("round-trips through the datetime-local formatter", () => {
    const iso = toApiDateTime("2026-09-01T19:00");

    expect(toApiDateTime(toDateTimeLocalForTest(iso))).toBe(iso);
  });
});

function toDateTimeLocalForTest(iso: string): string {
  const d = new Date(iso);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// Spec 015 FR-012b. Dates moved to English; money did not, and must not — a
// locale sweep that catches this formatter renders "Rp 170,400" for an amount
// Indonesian buyers read as "Rp 170.400", misstating what they are about to pay.
describe("formatCurrency stays Indonesian", () => {
  it("uses a dot as the thousands separator", () => {
    const formatted = formatCurrency("170400.00");

    expect(formatted).toContain("170.400");
    expect(formatted).not.toContain("170,400");
  });

  it("still renders whole rupiah without decimals", () => {
    expect(formatCurrency("150000.00")).toContain("150.000");
  });
});

import { describe, expect, it } from "vitest";
import { formatCurrency, formatDateTime, toApiDateTime } from "./format";

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

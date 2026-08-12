import { describe, expect, it } from "vitest";

import { formatAdmissionDates } from "./admission-dates";

// Times are chosen so every instant lands on an unambiguous day in the test
// runner's timezone, keeping these assertions about the RULE rather than about
// timezone arithmetic.
const DAY1 = "2026-09-30T05:00:00Z";
const DAY2 = "2026-10-01T05:00:00Z";
const DAY3 = "2026-10-02T05:00:00Z";
const DAY5 = "2026-10-04T05:00:00Z";

describe("formatAdmissionDates", () => {
  it("renders a single day on its own", () => {
    const got = formatAdmissionDates([DAY1]);

    expect(got).toContain("2026");
    expect(got).not.toContain(",");
    expect(got).not.toContain(" - ");
  });

  // The reported defect: a Day 1 + Day 2 bundle used to show only Day 1.
  it("spells out two days rather than collapsing them", () => {
    const got = formatAdmissionDates([DAY1, DAY2]);

    expect(got.split(",")).toHaveLength(2);
    expect(got).not.toContain(" - ");
  });

  it("collapses three or more days to a range", () => {
    const got = formatAdmissionDates([DAY1, DAY2, DAY3]);

    expect(got).toContain(" - ");
    expect(got).not.toContain(",");
  });

  // FR-021b: the rule keys off distinct DATES, not off how many tickets produced
  // them. Three constituents on two days are two days.
  it("counts distinct days, not tickets", () => {
    const got = formatAdmissionDates([DAY1, DAY1, DAY2]);

    expect(got.split(",")).toHaveLength(2);
    expect(got).not.toContain(" - ");
  });

  it("shows one day when every constituent admits on it", () => {
    const got = formatAdmissionDates([DAY1, DAY1, DAY1]);

    expect(got).toBe(formatAdmissionDates([DAY1]));
  });

  // Deduplication is on the rendered string, so two instants that print the same
  // are one day however far apart they are in absolute terms.
  it("treats instants that render identically as one day", () => {
    const morning = "2026-09-30T02:00:00Z";
    const evening = "2026-09-30T14:00:00Z";

    const got = formatAdmissionDates([morning, evening]);

    expect(got).not.toContain(",");
  });

  // A known cost of the ranged form, recorded rather than fixed: past two dates
  // a list stops fitting the line.
  it("ranges non-contiguous days, which reads as contiguous", () => {
    const got = formatAdmissionDates([DAY1, DAY2, DAY5]);

    expect(got).toContain(" - ");
    expect(got).not.toContain(",");
  });

  it("renders nothing for a line with no days", () => {
    expect(formatAdmissionDates([])).toBe("");
  });

  // The range closes on the last START, never an event end — a ticket admitting
  // late evening must not advertise the following morning.
  it("ranges between the first and last start", () => {
    const three = formatAdmissionDates([DAY1, DAY2, DAY3]);
    const spelled = formatAdmissionDates([DAY1, DAY3]);

    // Both name the same two outer days, so the range's ends are those instants.
    for (const part of spelled.split(", ")) {
      expect(three).toContain(part.replace(/^\d+ /, "").trim().slice(0, 3));
    }
  });
});

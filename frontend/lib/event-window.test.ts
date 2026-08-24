import { describe, expect, it } from "vitest";

import { strandedTicketTypes } from "./event-window";
import type { TicketTypeAdminView } from "./types";

function ticketType(
  name: string,
  event_start: string,
  event_end: string,
): TicketTypeAdminView {
  return {
    id: `tt-${name}`,
    event_id: "ev-1",
    name,
    description: null,
    price: "150000.00",
    quota: 10,
    sold: 0,
    sales_start: "2026-07-01T00:00:00Z",
    sales_end: "2026-08-31T00:00:00Z",
    event_start,
    event_end,
    is_visible: true,
  };
}

// The event runs 1-3 August; the two passes sit on 1 August and 2 August.
const event = { start_date: "2026-08-01T00:00:00Z", end_date: "2026-08-03T23:59:00Z" };
const day1 = ticketType("Day 1", "2026-08-01T09:00:00Z", "2026-08-01T23:00:00Z");
const day2 = ticketType("Day 2", "2026-08-02T09:00:00Z", "2026-08-02T23:00:00Z");

describe("strandedTicketTypes", () => {
  it("reports nothing when every window sits inside the event", () => {
    expect(strandedTicketTypes(event, [day1, day2])).toEqual([]);
  });

  // Widening cannot strand anything — this is the common edit and must stay quiet.
  it("stays quiet when the event is widened", () => {
    const widened = { start_date: "2026-08-01T00:00:00Z", end_date: "2026-08-04T23:59:00Z" };

    expect(strandedTicketTypes(widened, [day1, day2])).toEqual([]);
  });

  it("reports every ticket type when the event is moved away", () => {
    const moved = { start_date: "2026-08-05T00:00:00Z", end_date: "2026-08-07T23:59:00Z" };

    expect(strandedTicketTypes(moved, [day1, day2]).map((t) => t.name)).toEqual([
      "Day 1",
      "Day 2",
    ]);
  });

  it("reports only the ticket types the shortened event no longer covers", () => {
    const shortened = { start_date: "2026-08-01T00:00:00Z", end_date: "2026-08-01T23:59:00Z" };

    expect(strandedTicketTypes(shortened, [day1, day2]).map((t) => t.name)).toEqual([
      "Day 2",
    ]);
  });

  it("treats a window exactly equal to the event as contained", () => {
    const exact = ticketType("Whole event", event.start_date, event.end_date);

    expect(strandedTicketTypes(event, [exact])).toEqual([]);
  });

  it("reports nothing for an event with no ticket types", () => {
    expect(strandedTicketTypes(event, [])).toEqual([]);
  });

  // An unparseable date is not evidence of a stranded window, and warning about
  // it would give the admin nothing to act on.
  it("ignores an unparseable window rather than warning about it", () => {
    const broken = ticketType("Broken", "not-a-date", "also-not-a-date");

    expect(strandedTicketTypes(event, [broken])).toEqual([]);
  });
});

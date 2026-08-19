import { describe, expect, it } from "vitest";

import { groupOrderSlots } from "./slot-groups";
import type { TicketOrderSlot } from "@/lib/types";

function slot(overrides: Partial<TicketOrderSlot> & { id: string }): TicketOrderSlot {
  return {
    ticket_type_name: "Regular",
    package_name: null,
    package_id: null,
    package_unit: null,
    name: null,
    email: null,
    phone: null,
    dob: null,
    gender: null,
    ...overrides,
  };
}

const PKG = "9f2c1111-1111-1111-1111-111111111111";

describe("groupOrderSlots", () => {
  it("keeps each standalone slot as its own group titled by ticket type", () => {
    const groups = groupOrderSlots([
      slot({ id: "s1" }),
      slot({ id: "s2", ticket_type_name: "VIP" }),
    ]);

    expect(groups).toHaveLength(2);
    expect(groups[0]).toMatchObject({
      key: "s1",
      slotIds: ["s1"],
      title: "Regular",
      ticketCount: 1,
      isBundle: false,
      packageBadge: null,
    });
    expect(groups[1].title).toBe("VIP");
  });

  it("collapses slots sharing (package_id, package_unit) into one bundle-titled group", () => {
    const groups = groupOrderSlots([
      slot({
        id: "b1",
        ticket_type_name: "Day 1 Pass",
        package_name: "2-Day Bundle",
        package_id: PKG,
        package_unit: 1,
      }),
      slot({
        id: "b2",
        ticket_type_name: "Day 2 Pass",
        package_name: "2-Day Bundle",
        package_id: PKG,
        package_unit: 1,
      }),
    ]);

    expect(groups).toHaveLength(1);
    // The bundle's title replaces the constituent ticket titles (spec 010 FR-005).
    expect(groups[0]).toMatchObject({
      key: `${PKG}:1`,
      slotIds: ["b1", "b2"],
      title: "2-Day Bundle",
      ticketCount: 2,
      isBundle: true,
    });
  });

  it("falls back to one pre-010 card for a bundle slot without a unit", () => {
    const groups = groupOrderSlots([
      slot({
        id: "legacy1",
        ticket_type_name: "Day 1 Pass",
        package_name: "2-Day Bundle",
        package_id: PKG,
        package_unit: null,
      }),
      slot({
        id: "legacy2",
        ticket_type_name: "Day 2 Pass",
        package_name: "2-Day Bundle",
        package_id: PKG,
        package_unit: null,
      }),
    ]);

    expect(groups).toHaveLength(2);
    // Old rendering: ticket-type title with the package name as a badge.
    expect(groups[0]).toMatchObject({
      slotIds: ["legacy1"],
      title: "Day 1 Pass",
      isBundle: false,
      packageBadge: "2-Day Bundle",
    });
    expect(groups[1].title).toBe("Day 2 Pass");
  });

  it("keeps one form per unit when the same bundle was purchased more than once", () => {
    const unitSlot = (id: string, unit: number, day: string) =>
      slot({
        id,
        ticket_type_name: day,
        package_name: "2-Day Bundle",
        package_id: PKG,
        package_unit: unit,
      });
    const groups = groupOrderSlots([
      unitSlot("u1-d1", 1, "Day 1 Pass"),
      unitSlot("u1-d2", 1, "Day 2 Pass"),
      unitSlot("u2-d1", 2, "Day 1 Pass"),
      unitSlot("u2-d2", 2, "Day 2 Pass"),
    ]);

    // One form per purchased unit (spec 010 US3). Spec 019 FR-011 removed the
    // "Visitor <n>" label that used to tell the two apart, so what is pinned here
    // is the grouping itself — which FR-014 requires to be untouched. The two
    // cards are now identical to look at, and that is accepted rather than
    // accidental: card order is stable and any holder may go on any card.
    expect(groups).toHaveLength(2);
    expect(groups.map((g) => g.slotIds)).toEqual([
      ["u1-d1", "u1-d2"],
      ["u2-d1", "u2-d2"],
    ]);
    expect(groups.map((g) => g.title)).toEqual(["2-Day Bundle", "2-Day Bundle"]);
    // packageUnit survives the label's removal — it is what actually separates
    // unit 1's slots from unit 2's, and the label was only ever derived from it.
    expect(groups.map((g) => g.packageUnit)).toEqual([1, 2]);
    expect(groups.map((g) => g.ticketCount)).toEqual([2, 2]);
  });

  it("keeps a mixed order in slot order: bundle group plus standalone groups", () => {
    const groups = groupOrderSlots([
      slot({ id: "s1", ticket_type_name: "Regular" }),
      slot({
        id: "b1",
        ticket_type_name: "Day 1 Pass",
        package_name: "2-Day Bundle",
        package_id: PKG,
        package_unit: 1,
      }),
      slot({
        id: "b2",
        ticket_type_name: "Day 2 Pass",
        package_name: "2-Day Bundle",
        package_id: PKG,
        package_unit: 1,
      }),
    ]);

    expect(groups.map((g) => g.slotIds)).toEqual([["s1"], ["b1", "b2"]]);
    expect(groups.map((g) => g.title)).toEqual(["Regular", "2-Day Bundle"]);
  });
});

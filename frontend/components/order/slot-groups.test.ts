import { describe, expect, it } from "vitest";

import { groupOrderSlots, isoToDob } from "./slot-groups";
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

/**
 * Spec 011 FR-030 (clarified 2026-08-19). The card's starting values travel with
 * the group, so the form does not have to go back to the raw slot list to find
 * them — and so a bundle unit, which is many slots and one card, has a single
 * answer for what its fields hold.
 */
describe("groupOrderSlots — the seed each card starts from", () => {
  const FILLED = {
    name: "Heather Higgins",
    email: "heather@example.com",
    phone: "144650550532",
    dob: "1990-10-09",
    gender: "FEMALE",
  };

  it("carries a standalone slot's stored details onto its own group", () => {
    const [group] = groupOrderSlots([slot({ id: "s1", ...FILLED })]);

    expect(group.seed).toEqual({
      name: "Heather Higgins",
      email: "heather@example.com",
      phone: "144650550532",
      dob: "09/10/1990",
      gender: "FEMALE",
    });
  });

  it("renders an unfilled slot as empty strings rather than leaking null into the form", () => {
    const [group] = groupOrderSlots([slot({ id: "s1" })]);

    // FR-030's last clause: a slot with nothing saved is indistinguishable from
    // one the guest has not touched, which is what an empty input holds.
    expect(group.seed).toEqual({ name: "", email: "", phone: "", dob: "", gender: "" });
  });

  it("seeds a bundle unit's single card from the unit's slots", () => {
    const [group] = groupOrderSlots([
      slot({ id: "b1", package_name: "2-Day Bundle", package_id: PKG, package_unit: 1, ...FILLED }),
      slot({ id: "b2", package_name: "2-Day Bundle", package_id: PKG, package_unit: 1, ...FILLED }),
    ]);

    // One card for two slots (spec 010), and one answer for what it holds. The
    // submit-time fan-out writes a unit's slots identically, so they cannot
    // disagree and no tie-break is specified.
    expect(group.slotIds).toEqual(["b1", "b2"]);
    expect(group.seed.name).toBe("Heather Higgins");
    expect(group.seed.dob).toBe("09/10/1990");
  });

  it("seeds each purchased unit of the same bundle independently", () => {
    const groups = groupOrderSlots([
      slot({ id: "u1", package_name: "2-Day Bundle", package_id: PKG, package_unit: 1, ...FILLED }),
      slot({
        id: "u2",
        package_name: "2-Day Bundle",
        package_id: PKG,
        package_unit: 2,
        ...FILLED,
        name: "Second Holder",
      }),
    ]);

    expect(groups.map((group) => group.seed.name)).toEqual([
      "Heather Higgins",
      "Second Holder",
    ]);
  });

  it("seeds a pre-010 bundle slot, which still renders one card per slot", () => {
    const groups = groupOrderSlots([
      slot({ id: "legacy", package_name: "Old Bundle", package_id: PKG, package_unit: null, ...FILLED }),
    ]);

    expect(groups).toHaveLength(1);
    expect(groups[0].packageBadge).toBe("Old Bundle");
    expect(groups[0].seed.name).toBe("Heather Higgins");
  });
});

describe("isoToDob", () => {
  it("converts the wire's date-only value to the field's format", () => {
    expect(isoToDob("1990-10-09")).toBe("09/10/1990");
  });

  it("round-trips a date the form would submit back unchanged", () => {
    // The pair that matters: a restored date submitted untouched must store the
    // date it came from. dobToIso is the other half, in visitor-form.tsx.
    const iso = "2000-01-31";
    const [day, month, year] = isoToDob(iso).split("/");
    expect(`${year}-${month}-${day}`).toBe(iso);
  });

  it("keeps a four-digit year that is not in the recent past", () => {
    // Real stored data: the reported order carried 1010-10-10.
    expect(isoToDob("1010-10-10")).toBe("10/10/1010");
  });

  it("returns empty for null and for anything that is not a plain ISO date", () => {
    expect(isoToDob(null)).toBe("");
    expect(isoToDob("")).toBe("");
    expect(isoToDob("09/10/1990")).toBe("");
    expect(isoToDob("1990-10-09T00:00:00Z")).toBe("");
  });
});

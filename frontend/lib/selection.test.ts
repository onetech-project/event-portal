import { describe, expect, it } from "vitest";

import {
  availabilityOf,
  buildSelectableItems,
  checkoutItems,
  maxQuantityFor,
  selectionLines,
  selectionTotal,
  totalUnits,
} from "./selection";
import type { PackageSummary, TicketTypeSummary } from "./types";

const DAY1 = "11111111-1111-1111-1111-111111111111";
const DAY2 = "22222222-2222-2222-2222-222222222222";
const BUNDLE = "33333333-3333-3333-3333-333333333333";

// The sales window (open) plus the admission window every ticket type now
// carries (spec 015). Neither constrains the other.
const OPEN = {
  sales_start: "2020-01-01T00:00:00Z",
  sales_end: "2030-01-01T00:00:00Z",
  event_start: "2026-04-26T09:00:00Z",
  event_end: "2026-04-26T23:00:00Z",
};
const NOW = new Date("2026-04-01T00:00:00Z").getTime();

function ticket(overrides: Partial<TicketTypeSummary> = {}): TicketTypeSummary {
  return {
    id: DAY1,
    name: "Jive (Day 1) - 26 Apr 2026",
    description: null,
    price: "35000.00",
    quota_remaining: 100,
    ...OPEN,
    ...overrides,
  };
}

function bundle(overrides: Partial<PackageSummary> = {}): PackageSummary {
  return {
    id: BUNDLE,
    name: "Jive (Day 1 and 2) - 26 - 27 Apr 2026",
    description: null,
    price: "50000.00",
    ...OPEN,
    available_units: 100,
    purchasable: true,
    components: [
      { ticket_type_id: DAY1, ticket_type_name: "Day 1", quantity_per_unit: 1 },
      { ticket_type_id: DAY2, ticket_type_name: "Day 2", quantity_per_unit: 1 },
    ],
    ...overrides,
  };
}

/**
 * The two split reads the selection page feeds into buildSelectableItems since
 * 008 (clarification 2026-08-05): EventDetail itself is content-only now.
 */
type Catalog = { ticket_types: TicketTypeSummary[]; packages: PackageSummary[] };

function event(overrides: Partial<Catalog> = {}): Catalog {
  return {
    ticket_types: [ticket(), ticket({ id: DAY2, name: "Jive (Day 2) - 27 Apr 2026" })],
    packages: [bundle()],
    ...overrides,
  };
}

/** Applies a catalog to the two-argument builder. */
function build(catalog: Catalog) {
  return buildSelectableItems(catalog.ticket_types, catalog.packages);
}

describe("buildSelectableItems", () => {
  it("merges tickets and bundles into one list ordered by price", () => {
    const items = build(event());

    expect(items).toHaveLength(3);
    expect(items.map((i) => i.kind)).toEqual(["ticket", "ticket", "package"]);
    // The design lists the Rp50.000 bundle after the two Rp35.000 days.
    expect(items[2].id).toBe(BUNDLE);
  });

  it("keeps a bundle in the list even when it is not purchasable", () => {
    const items = build(event({ packages: [bundle({ available_units: 0, purchasable: false })] }),
    );

    expect(items.map((i) => i.id)).toContain(BUNDLE);
  });
});

describe("maxQuantityFor", () => {
  it("caps a ticket at its remaining quota", () => {
    expect(maxQuantityFor({ kind: "ticket", id: DAY1, ticket: ticket({ quota_remaining: 3 }) })).toBe(3);
  });

  it("caps a bundle at its derived available units, not any stored figure", () => {
    expect(maxQuantityFor({ kind: "package", id: BUNDLE, pkg: bundle({ available_units: 2 }) })).toBe(2);
  });

  it("follows the actual limit with no artificial ceiling", () => {
    expect(maxQuantityFor({ kind: "package", id: BUNDLE, pkg: bundle({ available_units: 999 }) })).toBe(999);
    expect(maxQuantityFor({ kind: "ticket", id: DAY1, ticket: ticket({ quota_remaining: 100 }) })).toBe(100);
  });

  it("is zero when a bundle is sold out", () => {
    expect(maxQuantityFor({ kind: "package", id: BUNDLE, pkg: bundle({ available_units: 0 }) })).toBe(0);
  });
});

describe("availabilityOf", () => {
  it("reports a bundle unavailable when a constituent has run out", () => {
    // The server folds every gate into purchasable; the client does not re-derive it.
    const result = availabilityOf(
      { kind: "package", id: BUNDLE, pkg: bundle({ available_units: 0, purchasable: false }) },
      NOW,
    );

    expect(result.available).toBe(false);
    expect(result.label).toBe("Sold out");
  });

  it("reports a bundle unavailable when the server says so despite units remaining", () => {
    // e.g. a constituent's sales window closed while the bundle's own is open.
    const result = availabilityOf(
      { kind: "package", id: BUNDLE, pkg: bundle({ available_units: 5, purchasable: false }) },
      NOW,
    );

    expect(result.available).toBe(false);
  });

  it("reports a ticket whose sales have closed", () => {
    const result = availabilityOf(
      {
        kind: "ticket",
        id: DAY1,
        ticket: ticket({ sales_end: "2021-01-01T00:00:00Z" }),
      },
      NOW,
    );

    expect(result.available).toBe(false);
    expect(result.label).toBe("Sales have closed");
  });
});

describe("selectionLines", () => {
  const items = build(event());

  it("is empty when nothing is selected", () => {
    expect(selectionLines(items, {})).toEqual([]);
  });

  it("shows a selected bundle as ONE line at its own price", () => {
    const lines = selectionLines(items, { [BUNDLE]: 1 });

    expect(lines).toHaveLength(1);
    expect(lines[0].unitPrice).toBe("50000.00");
    expect(selectionTotal(lines)).toBe(50000);
  });

  it("totals two individually selected days", () => {
    const lines = selectionLines(items, { [DAY1]: 1, [DAY2]: 1 });

    expect(lines).toHaveLength(2);
    expect(totalUnits(lines)).toBe(2);
    expect(selectionTotal(lines)).toBe(70000);
  });

  it("drops a row stepped back down to zero", () => {
    expect(selectionLines(items, { [DAY1]: 0 })).toEqual([]);
  });
});

describe("checkoutItems", () => {
  it("emits exactly one id per line, matching the server's XOR", () => {
    const items = build(event());
    const payload = checkoutItems(selectionLines(items, { [DAY1]: 2, [BUNDLE]: 1 }));

    expect(payload).toEqual([
      { ticket_type_id: DAY1, quantity: 2 },
      { package_id: BUNDLE, quantity: 1 },
    ]);
  });
});

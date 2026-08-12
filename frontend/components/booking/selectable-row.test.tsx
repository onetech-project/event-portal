import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { SelectableRow } from "./selectable-row";
import type { PackageSummary, SelectableItem, TicketTypeSummary } from "@/lib/types";

const DAY1 = "11111111-1111-1111-1111-111111111111";
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

function ticketItem(overrides: Partial<TicketTypeSummary> = {}): SelectableItem {
  return {
    kind: "ticket",
    id: DAY1,
    ticket: {
      id: DAY1,
      name: "Day 1",
      description: null,
      price: "35000.00",
      quota_remaining: 100,
      ...OPEN,
      ...overrides,
    },
  };
}

function bundleItem(overrides: Partial<PackageSummary> = {}): SelectableItem {
  return {
    kind: "package",
    id: BUNDLE,
    pkg: {
      id: BUNDLE,
      name: "Day 1 and 2",
      description: null,
      price: "50000.00",
      ...OPEN,
      available_units: 100,
      purchasable: true,
      components: [
        { ticket_type_id: DAY1, ticket_type_name: "Day 1", quantity_per_unit: 1 },
      ],
      ...overrides,
    },
  };
}

describe("SelectableRow", () => {
  it("shows an Add button when nothing is selected", () => {
    render(<SelectableRow item={ticketItem()} now={NOW} quantity={0} onChange={vi.fn()} />);

    expect(screen.getByRole("button", { name: /add day 1/i })).toBeTruthy();
    expect(screen.queryByLabelText(/quantity for/i)).toBeNull();
  });

  it("turns into a stepper at 1 once added", async () => {
    const onChange = vi.fn();
    render(<SelectableRow item={ticketItem()} now={NOW} quantity={0} onChange={onChange} />);

    await userEvent.click(screen.getByRole("button", { name: /add day 1/i }));

    expect(onChange).toHaveBeenCalledWith(1);
  });

  it("renders the stepper with the current quantity when selected", () => {
    render(<SelectableRow item={ticketItem()} now={NOW} quantity={2} onChange={vi.fn()} />);

    expect(screen.getByLabelText(/quantity for day 1/i).textContent).toBe("2");
    expect(screen.queryByRole("button", { name: /add day 1/i })).toBeNull();
  });

  it("returns to the Add state when decremented from 1", async () => {
    const onChange = vi.fn();
    render(<SelectableRow item={ticketItem()} now={NOW} quantity={1} onChange={onChange} />);

    await userEvent.click(screen.getByRole("button", { name: /decrease day 1/i }));

    // 0 is what drops the row from the summary and restores its Add button.
    expect(onChange).toHaveBeenCalledWith(0);
  });

  it("badges a bundle row and leaves a ticket row unbadged", () => {
    const { unmount } = render(
      <SelectableRow item={bundleItem()} now={NOW} quantity={0} onChange={vi.fn()} />,
    );
    expect(screen.getByText("Bundle")).toBeTruthy();
    unmount();

    render(<SelectableRow item={ticketItem()} now={NOW} quantity={0} onChange={vi.fn()} />);
    expect(screen.queryByText("Bundle")).toBeNull();
  });

  it("caps a bundle's stepper at its derived available units", () => {
    render(
      <SelectableRow
        item={bundleItem({ available_units: 2 })}
        now={NOW}
        quantity={2}
        onChange={vi.fn()}
      />,
    );

    // A bundle owns no inventory: 2 is what the server derived from the scarcest
    // constituent, and the stepper must not offer a third.
    expect(
      screen.getByRole("button", { name: /increase day 1 and 2/i }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("caps a ticket's stepper at its remaining quota", () => {
    render(
      <SelectableRow
        item={ticketItem({ quota_remaining: 3 })}
        now={NOW}
        quantity={3}
        onChange={vi.fn()}
      />,
    );

    expect(
      screen.getByRole("button", { name: /increase day 1/i }).hasAttribute("disabled"),
    ).toBe(true);
  });

  it("cannot be added when the bundle is sold out", () => {
    render(
      <SelectableRow
        item={bundleItem({ available_units: 0, purchasable: false })}
        now={NOW}
        quantity={0}
        onChange={vi.fn()}
      />,
    );

    expect(
      screen.getByRole("button", { name: /add day 1 and 2/i }).hasAttribute("disabled"),
    ).toBe(true);
    expect(screen.getByText("Sold out")).toBeTruthy();
  });

  it("cannot be added when the server marks the bundle unpurchasable despite stock", () => {
    // e.g. a constituent's sales window closed while the bundle's own is open.
    render(
      <SelectableRow
        item={bundleItem({ available_units: 5, purchasable: false })}
        now={NOW}
        quantity={0}
        onChange={vi.fn()}
      />,
    );

    expect(
      screen.getByRole("button", { name: /add day 1 and 2/i }).hasAttribute("disabled"),
    ).toBe(true);
  });
});

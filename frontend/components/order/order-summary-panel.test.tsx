import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { OrderSummaryPanel } from "./order-summary-panel";
import type { PublicOrderItem, TicketOrderDetail } from "@/lib/types";

function line(overrides: Partial<PublicOrderItem> = {}): PublicOrderItem {
  return {
    kind: "ticket",
    ticket_type_name: "Day 1 Pass",
    package_name: null,
    quantity: 1,
    unit_price: "150000.00",
    subtotal: "150000.00",
    admission_starts: ["2026-08-01T02:00:00Z"],
    ...overrides,
  };
}

// The event runs three days; the tickets sold against it admit on single days.
//
// `money` exists because the default below sets total_amount === subtotal with
// no fees, which is the state that makes a fee assertion pass whatever the code
// does. Any test about WHICH figure renders must override it (spec 011 FR-016).
function order(
  items: PublicOrderItem[],
  money: Partial<Pick<TicketOrderDetail, "total_amount" | "subtotal" | "fees">> = {},
): TicketOrderDetail {
  return {
    order_id: "ORD-20260801-A1B2C3D4",
    status: "PENDING",
    total_amount: "150000.00",
    subtotal: "150000.00",
    fees: [],
    ...money,
    expires_at: null,
    terms_agreed_at: null,
    payment_started: false,
    event: {
      name: "Jazz Festival",
      slug: "jazz-festival",
      venue: "Balai Sarbini",
      address: "Jl. Jend. Sudirman",
      // Midday UTC on both ends so neither slides into a neighbouring day in
      // the runner's timezone — these cases are about which SOURCE the box
      // reads, not about timezone arithmetic.
      start_date: "2026-08-01T00:00:00Z",
      end_date: "2026-08-03T10:00:00Z",
    },
    items,
    slots: [],
    server_time: "2026-07-01T00:00:00Z",
    payment: null,
  };
}

describe("OrderSummaryPanel admission windows", () => {
  // Spec 015 FR-010. Before this feature every line rendered the parent event's
  // opening date, so a Day 2 pass told its buyer "1 August".
  it("shows each line's own date, not one date for the whole order", () => {
    render(
      <OrderSummaryPanel
        phase="registration"
        order={order([
          line({ ticket_type_name: "Day 1 Pass" }),
          line({
            ticket_type_name: "Day 2 Pass",
            admission_starts: ["2026-08-02T02:00:00Z"],
          }),
        ])}
      />,
    );

    const dates = screen.getAllByTestId("order-line-date").map((n) => n.textContent);

    expect(dates).toHaveLength(2);
    expect(dates[0]).not.toBe(dates[1]);
  });

  // FR-009, reversed on 2026-08-12: the box is labelled "Event" and names the
  // EVENT's dates. An earlier revision derived it from the order's tickets;
  // these two cases replace the ones that asserted that.
  it("shows the event's own range in the Event box, not the tickets'", () => {
    render(<OrderSummaryPanel phase="registration" order={order([line()])} />);

    // The event runs 1-3 August while the only ticket admits on 1 August.
    expect(screen.getByText(/1 - 3 Aug 2026/i)).toBeInTheDocument();
  });

  it("opens the gate at the event's start, not the earliest line's", () => {
    render(
      <OrderSummaryPanel
        phase="registration"
        order={order([
          line({
            ticket_type_name: "Day 2 Pass",
            admission_starts: ["2026-08-02T05:00:00Z"],
          }),
          line({ ticket_type_name: "Day 1 Pass" }),
        ])}
      />,
    );

    // The event starts 00:00Z = 07:00 in Jakarta. Neither line starts then, so
    // this can only have come from the event.
    expect(screen.getByText(/Gate opens at 07:00 WIB/i)).toBeInTheDocument();
  });

  // Spec 015 FR-021a: the reported defect. A bundle admitting on two days used
  // to render one date, and the second day vanished.
  it("names both days of a two-day bundle on its one line", () => {
    render(
      <OrderSummaryPanel
        phase="registration"
        order={order([
          line({
            kind: "package",
            ticket_type_name: null,
            package_name: "Day 1 & 2 Bundle",
            admission_starts: ["2026-08-01T02:00:00Z", "2026-08-02T02:00:00Z"],
          }),
        ])}
      />,
    );

    const dates = screen.getAllByTestId("order-line-date");
    expect(dates).toHaveLength(1);
    expect(dates[0].textContent?.split(",")).toHaveLength(2);
  });

  it("ranges a bundle admitting on three or more days", () => {
    render(
      <OrderSummaryPanel
        phase="registration"
        order={order([
          line({
            kind: "package",
            ticket_type_name: null,
            package_name: "Weekend Bundle",
            admission_starts: [
              "2026-08-01T02:00:00Z",
              "2026-08-02T02:00:00Z",
              "2026-08-03T02:00:00Z",
            ],
          }),
        ])}
      />,
    );

    expect(screen.getByTestId("order-line-date").textContent).toContain(" - ");
  });

  // A bundle carries the span across its constituents (FR-021), which the panel
  // renders like any other line.
  it("renders a package line's derived span", () => {
    render(
      <OrderSummaryPanel
        phase="registration"
        order={order([
          line({
            kind: "package",
            ticket_type_name: null,
            package_name: "Weekend Bundle",
            admission_starts: ["2026-08-01T02:00:00Z", "2026-08-02T02:00:00Z"],
          }),
        ])}
      />,
    );

    expect(screen.getByText("Weekend Bundle")).toBeInTheDocument();
    expect(screen.getAllByTestId("order-line-date")).toHaveLength(1);
  });

  // An order with no lines must still render a range rather than an empty one.
  it("falls back to the event's dates when there are no lines", () => {
    render(<OrderSummaryPanel phase="registration" order={order([])} />);

    expect(screen.getByText("Jazz Festival")).toBeInTheDocument();
    expect(screen.queryAllByTestId("order-line-date")).toHaveLength(0);
  });
});

/**
 * Spec 011 FR-016 / FR-016a / FR-016c, constitution v4.0.0.
 *
 * Every case here sets `subtotal` and `total_amount` APART. The module's default
 * fixture sets them equal, which is the state in which a test of "which figure
 * renders" passes no matter what the component does.
 */
describe("OrderSummaryPanel fee presentation", () => {
  // Rp 500.000 of tickets, Rp 50.000 of tax, Rp 550.000 charged.
  const priced = {
    subtotal: "500000.00",
    total_amount: "550000.00",
    fees: [{ name: "PPN (10%)", amount: "50000.00" }],
  };

  it("shows the pre-fee subtotal on the registration step, never the total", () => {
    render(<OrderSummaryPanel phase="registration" order={order([line()], priced)} />);

    expect(screen.getByTestId("order-total-figure")).toHaveTextContent("Rp 500.000");
    // The defect this reverses: the forms step showed the fee-inclusive figure.
    expect(screen.queryByText("Rp 550.000")).not.toBeInTheDocument();
    // ...and no itemization either — the step carries no fee in any form.
    expect(screen.queryByText(/^Subtotal \(/)).not.toBeInTheDocument();
    expect(screen.queryByText("PPN (10%)")).not.toBeInTheDocument();
  });

  it("keeps the Total payment heading but changes the note under it", () => {
    render(<OrderSummaryPanel phase="registration" order={order([line()], priced)} />);

    // FR-016a: the heading is shared across both steps; the note is what
    // distinguishes them, because the old one lied above a fee-free number.
    expect(screen.getByText(/total payment/i)).toBeInTheDocument();
    expect(screen.getByText(/Excludes taxes and fees/i)).toBeInTheDocument();
    expect(screen.queryByText(/includes all taxes and fees/i)).not.toBeInTheDocument();
  });

  it("shows the fee-inclusive total and the breakdown on the payment step", () => {
    render(<OrderSummaryPanel phase="payment" order={order([line()], priced)} />);

    expect(screen.getByTestId("order-total-figure")).toHaveTextContent("Rp 550.000");
    expect(screen.getByText(/^Subtotal \(\d+ items?\)$/)).toBeInTheDocument();
    expect(screen.getByText("PPN (10%)")).toBeInTheDocument();
    expect(screen.getByText(/includes all taxes and fees/i)).toBeInTheDocument();
  });

  // FR-016c: orders predating fees carry no subtotal of their own. Their stored
  // total already excludes fees, so falling back to it is exact.
  it("falls back to the stored total when the order predates fees", () => {
    render(
      <OrderSummaryPanel
        phase="registration"
        order={order([line()], { subtotal: null, total_amount: "150000.00", fees: [] })}
      />,
    );

    expect(screen.getByTestId("order-total-figure")).toHaveTextContent("Rp 150.000");
  });

  // The reason the implementation uses `??` rather than `||`. A free order has a
  // genuine "0.00" subtotal; a falsy check would skip it and bill the guest the
  // fee-inclusive total on a screen that promised nothing.
  it("renders a zero subtotal as Rp 0 rather than falling through to the total", () => {
    render(
      <OrderSummaryPanel
        phase="registration"
        order={order([line()], {
          subtotal: "0.00",
          total_amount: "1200.00",
          fees: [{ name: "Admin Fee", amount: "1200.00" }],
        })}
      />,
    );

    expect(screen.getByTestId("order-total-figure")).toHaveTextContent("Rp 0");
    expect(screen.queryByText("Rp 1.200")).not.toBeInTheDocument();
  });
});

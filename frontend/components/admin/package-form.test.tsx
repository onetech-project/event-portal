import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";

import { PackageForm } from "./package-form";
import type { PackageAdminView, TicketTypeAdminView } from "@/lib/types";

function renderWithQuery(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

const DAY_1 = "11111111-1111-4111-8111-111111111111";
const DAY_2 = "22222222-2222-4222-8222-222222222222";
const EVENT_ID = "33333333-3333-4333-8333-333333333333";

const ticketTypes: TicketTypeAdminView[] = [
  {
    id: DAY_1,
    event_id: EVENT_ID,
    name: "Day 1",
    description: null,
    price: "150000.00",
    quota: 7,
    sold: 3,
    sales_start: "2026-07-01T00:00:00Z",
    sales_end: "2026-08-31T00:00:00Z",
  },
  {
    id: DAY_2,
    event_id: EVENT_ID,
    name: "Day 2",
    description: null,
    price: "150000.00",
    quota: 4,
    sold: 0,
    sales_start: "2026-07-01T00:00:00Z",
    sales_end: "2026-08-31T00:00:00Z",
  },
];

const existing: PackageAdminView = {
  id: "44444444-4444-4444-8444-444444444444",
  event_id: EVENT_ID,
  name: "Two-Day Pass",
  description: null,
  price: "250000.00",
  sales_start: "2026-07-01T00:00:00Z",
  sales_end: "2026-08-31T00:00:00Z",
  status: "ACTIVE",
  components: [
    { ticket_type_id: DAY_1, ticket_type_name: "Day 1", quantity_per_unit: 1 },
    { ticket_type_id: DAY_2, ticket_type_name: "Day 2", quantity_per_unit: 1 },
  ],
  available_units: 4,
  sold: 0,
  created_at: null,
  updated_at: null,
};

describe("PackageForm", () => {
  // FR-036: a package holds no inventory. Offering a quota input here would
  // both contradict the model and be rejected by the server, which refuses any
  // quota-like key rather than ignoring it.
  it("offers no quota input, because a package owns no inventory", () => {
    renderWithQuery(
      <PackageForm eventId={EVENT_ID} ticketTypes={ticketTypes} onDone={vi.fn()} />,
    );

    expect(screen.queryByLabelText(/quota/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/stock|inventory|capacity/i)).not.toBeInTheDocument();
  });

  it("lists every ticket type of this event as a possible constituent", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <PackageForm eventId={EVENT_ID} ticketTypes={ticketTypes} onDone={vi.fn()} />,
    );

    await user.click(screen.getByRole("combobox", { name: /ticket types/i }));

    expect(await screen.findByRole("option", { name: /day 1/i })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /day 2/i })).toBeInTheDocument();
  });

  // A bundle always grants exactly one of each selected ticket type, so the
  // composition is a multiselect with no per-type quantity to enter.
  it("prefills the composition when editing", () => {
    renderWithQuery(
      <PackageForm
        eventId={EVENT_ID}
        ticketTypes={ticketTypes}
        pkg={existing}
        onDone={vi.fn()}
      />,
    );

    expect(screen.getByLabelText(/name/i)).toHaveValue("Two-Day Pass");
    expect(screen.getByLabelText(/ticket types/i)).toHaveTextContent("Day 1");
    expect(screen.getByLabelText(/ticket types/i)).toHaveTextContent("Day 2");
  });

  // The bundle's availability is the floor of its constituents' remaining
  // quota: Day 1 has 7 left, Day 2 has 4, so the bundle can sell 4 more.
  it("shows the bundle's derived availability as the minimum constituent quota", () => {
    renderWithQuery(
      <PackageForm
        eventId={EVENT_ID}
        ticketTypes={ticketTypes}
        pkg={existing}
        onDone={vi.fn()}
      />,
    );

    expect(screen.getByText(/can be sold up to 4 more unit/i)).toBeInTheDocument();
  });

  it("refuses a bundle with no constituents", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <PackageForm eventId={EVENT_ID} ticketTypes={ticketTypes} onDone={vi.fn()} />,
    );

    await user.type(screen.getByLabelText(/name/i), "Empty Bundle");
    await user.type(screen.getByLabelText(/price/i), "250000");
    fireEvent.change(screen.getByLabelText(/sales start/i), {
      target: { value: "2026-07-01T00:00" },
    });
    fireEvent.change(screen.getByLabelText(/sales end/i), {
      target: { value: "2026-08-31T00:00" },
    });

    // Dispatched directly: clicking a submit button does not reach React's
    // onSubmit under happy-dom, though it does in a real browser.
    fireEvent.submit(screen.getByRole("form", { name: /package/i }));

    const alerts = await screen.findAllByRole("alert");
    expect(alerts.map((alert) => alert.textContent).join(" ")).toMatch(
      /Select at least one ticket type/i,
    );
  });

  it("cannot be submitted for an event with no ticket types", () => {
    renderWithQuery(<PackageForm eventId={EVENT_ID} ticketTypes={[]} onDone={vi.fn()} />);

    expect(screen.getByRole("button", { name: /create package/i })).toBeDisabled();
    expect(screen.getByText(/no ticket types yet/i)).toBeInTheDocument();
  });
});

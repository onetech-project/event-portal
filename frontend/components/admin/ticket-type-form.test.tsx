import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement } from "react";
import { describe, expect, it, vi } from "vitest";

import { TicketTypeForm } from "./ticket-type-form";
import type { TicketTypeAdminView } from "@/lib/types";

function renderWithQuery(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

const existing: TicketTypeAdminView = {
  id: "tt-1",
  event_id: "ev-1",
  name: "Regular",
  price: "150000.00",
  quota: 7,
  sold: 3,
  sales_start: "2026-07-01T00:00:00Z",
  sales_end: "2026-08-31T00:00:00Z",
};

describe("TicketTypeForm", () => {
  // The constitution requires this field be labelled as REMAINING quota. Calling
  // it "Total" invites an admin to reset it above the true remainder, silently
  // creating tickets that were never allocated.
  it("labels the quota field as remaining quota, never as a total", () => {
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    const label = screen.getByText(/Sisa Kuota \/ Remaining Quota/i);

    expect(label).toBeInTheDocument();
    expect(screen.queryByText(/total quota/i)).not.toBeInTheDocument();
  });

  it("explains that the value is set absolutely", () => {
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    expect(screen.getByText(/set absolutely/i)).toBeInTheDocument();
  });

  it("shows the read-only sold count beside the quota when editing", () => {
    renderWithQuery(
      <TicketTypeForm eventId="ev-1" ticketType={existing} onDone={vi.fn()} />,
    );

    expect(screen.getByText(/3 sold/i)).toBeInTheDocument();
  });

  it("does not offer a sold input, since sold is derived", () => {
    renderWithQuery(
      <TicketTypeForm eventId="ev-1" ticketType={existing} onDone={vi.fn()} />,
    );

    expect(screen.queryByLabelText(/^sold$/i)).not.toBeInTheDocument();
  });

  it("prefills the existing values when editing", () => {
    renderWithQuery(
      <TicketTypeForm eventId="ev-1" ticketType={existing} onDone={vi.fn()} />,
    );

    expect(screen.getByLabelText(/name/i)).toHaveValue("Regular");
    expect(screen.getByLabelText(/Sisa Kuota/i)).toHaveValue(7);
  });

  it("rejects a negative remaining quota before submitting", async () => {
    const user = userEvent.setup();
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    await user.type(screen.getByLabelText(/name/i), "Regular");
    await user.type(screen.getByLabelText(/price/i), "100000");
    await user.clear(screen.getByLabelText(/Sisa Kuota/i));
    await user.type(screen.getByLabelText(/Sisa Kuota/i), "-5");

    // Dispatched directly: clicking a submit button does not reach React's
    // onSubmit under happy-dom, though it does in a real browser.
    fireEvent.submit(screen.getByRole("form", { name: /ticket type/i }));

    const alerts = await screen.findAllByRole("alert");
    expect(alerts.map((a) => a.textContent).join(" ")).toMatch(
      /Remaining quota must not be negative/i,
    );
  });
});

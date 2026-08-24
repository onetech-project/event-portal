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
  description: null,
  price: "150000.00",
  quota: 7,
  sold: 3,
  sales_start: "2026-07-01T00:00:00Z",
  sales_end: "2026-08-31T00:00:00Z",
  event_start: "2026-09-02T02:00:00Z",
  event_end: "2026-09-02T16:00:00Z",
  is_visible: true,
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

  // Spec 022 FR-004 / FR-001a. These three pin the DIRECTION of the flag, which is
  // the thing the is_registration_only -> is_visible inversion made easy to get
  // backwards: a form that quietly defaults to false takes every new ticket type
  // off sale, and one that quietly defaults to true republishes every invitation
  // ticket at price 0. Neither raises an error.
  it("defaults a NEW ticket type to being sold", () => {
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    // getByRole, not getByLabelText: the control is named both by its own
    // aria-label and by the <label> wrapping it, so a text query matches twice.
    expect(screen.getByRole("checkbox")).toBeChecked();
  });

  it("round-trips an invitation-only type as NOT sold", () => {
    renderWithQuery(
      <TicketTypeForm
        eventId="ev-1"
        ticketType={{ ...existing, is_visible: false }}
        onDone={vi.fn()}
      />,
    );

    expect(screen.getByRole("checkbox")).not.toBeChecked();
  });

  // The control is named for its consequence, not for the column. "Visible"
  // alone reads as a listing preference, and an admin tidying a list would be
  // publishing a free ticket without being told.
  it("warns that unticking also makes the ticket free", () => {
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    expect(screen.getByText(/invitation-only/i)).toBeInTheDocument();
    expect(screen.getByText(/free of charge/i)).toBeInTheDocument();
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

  // Spec 015: the two windows must be tellable apart at a glance. An admin who
  // reads "Event start" as "Sales start" sets a ticket that admits nobody.
  it("offers an event window labelled distinctly from the sales window", () => {
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    expect(screen.getByLabelText(/event start/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/event end/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/sales start/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/sales end/i)).toBeInTheDocument();
  });

  // Validation admits no tolerance, so an admin who reads "Event start" as
  // showtime rather than gate-open turns away every early arrival.
  it("says the event start is when admission opens, not showtime", () => {
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    expect(screen.getByText(/not showtime/i)).toBeInTheDocument();
  });

  it("prefills the existing event window when editing", () => {
    renderWithQuery(
      <TicketTypeForm eventId="ev-1" ticketType={existing} onDone={vi.fn()} />,
    );

    expect(screen.getByLabelText(/event start/i)).toHaveValue("2026-09-02T09:00");
    expect(screen.getByLabelText(/event end/i)).toHaveValue("2026-09-02T23:00");
  });

  it("rejects an event window that ends before it starts", async () => {
    const user = userEvent.setup();
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    await user.type(screen.getByLabelText(/name/i), "Day 2 Pass");
    await user.type(screen.getByLabelText(/price/i), "100000");
    await user.type(screen.getByLabelText(/sales start/i), "2026-07-01T00:00");
    await user.type(screen.getByLabelText(/sales end/i), "2026-08-31T00:00");
    await user.type(screen.getByLabelText(/event start/i), "2026-09-02T09:00");
    await user.type(screen.getByLabelText(/event end/i), "2026-09-01T09:00");

    fireEvent.submit(screen.getByRole("form", { name: /ticket type/i }));

    const alerts = await screen.findAllByRole("alert");
    expect(alerts.map((a) => a.textContent).join(" ")).toMatch(
      /must not end before it starts/i,
    );
  });
});

// Spec 022 FR-004a / FR-004b. The price follows the on-sale decision, in BOTH
// directions — and the second direction is the one that matters.
//
// Unticking zeroes the price. FR-005 only blocks that flip for package members, so
// an ordinary ticket priced 150,000 can be made invitation-only and lose its price.
// Ticking the box again must then demand a real one: carrying the zero forward
// would republish the type onto the guest purchase list at no charge, which is the
// US2 leak arriving through the price field rather than through a missing filter.
describe("TicketTypeForm — price follows the on-sale decision", () => {
  it("disables the price and zeroes it when the ticket is taken off sale", async () => {
    const user = userEvent.setup();
    renderWithQuery(<TicketTypeForm eventId="ev-1" ticketType={existing} onDone={vi.fn()} />);

    const price = screen.getByLabelText(/price/i);
    expect(price).toBeEnabled();
    expect(price).toHaveValue("150000.00");

    await user.click(screen.getByRole("checkbox"));

    expect(price).toBeDisabled();
    expect(price).toHaveValue("0");
  });

  it("explains that an invitation ticket is not charged", async () => {
    const user = userEvent.setup();
    renderWithQuery(<TicketTypeForm eventId="ev-1" ticketType={existing} onDone={vi.fn()} />);

    await user.click(screen.getByRole("checkbox"));

    expect(screen.getByText(/not charged/i)).toBeInTheDocument();
  });

  it("empties the price and re-enables it when the ticket goes back on sale", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <TicketTypeForm
        eventId="ev-1"
        ticketType={{ ...existing, is_visible: false, price: "0.00" }}
        onDone={vi.fn()}
      />,
    );

    const price = screen.getByLabelText(/price/i);
    expect(price).toBeDisabled();

    await user.click(screen.getByRole("checkbox"));

    expect(price).toBeEnabled();
    // Empty, not the old price: that price is already gone from the database, so
    // prefilling anything would show a number the server does not have.
    expect(price).toHaveValue("");
  });

  // FR-004b: the emptied field must SAY why it is empty. An empty required box with
  // no explanation is exactly the state this rule exists to prevent — the admin
  // has no way to know the price was destroyed rather than never set.
  //
  // Note what is NOT asserted: that zero is refused. A purchasable ticket priced
  // zero is legal (FR-003 permits a genuine giveaway), so the rule is that a price
  // must be TYPED, not that it must be non-zero. An earlier draft of this banned
  // zero outright and contradicted `schemas.test.ts` — which was right and this
  // was wrong.
  it("explains why the price is empty after going back on sale", async () => {
    const user = userEvent.setup();
    renderWithQuery(
      <TicketTypeForm
        eventId="ev-1"
        ticketType={{ ...existing, is_visible: false, price: "0.00" }}
        onDone={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("checkbox"));

    expect(screen.getByText(/cleared when this ticket was made invitation-only/i))
      .toBeInTheDocument();
    expect(screen.getByText(/0 is allowed, but it has to be deliberate/i)).toBeInTheDocument();
  });

  // A brand-new ticket type also has an empty price, and must NOT be told it "was
  // cleared" — that would be a plain falsehood about something that never happened.
  it("does not claim the price was cleared on a new ticket type", () => {
    renderWithQuery(<TicketTypeForm eventId="ev-1" onDone={vi.fn()} />);

    expect(screen.queryByText(/cleared when this ticket was made invitation-only/i)).toBeNull();
  });

  // "An invitation ticket saves at zero" is NOT retested here. It is a schema
  // rule, and lib/schemas.test.ts already pins it ("accepts a free ticket but
  // rejects a negative price"). A form-level duplicate would only re-prove it
  // through a mocked network, where a stub that diverges from adminFetch fails
  // for reasons that have nothing to do with the rule.
});

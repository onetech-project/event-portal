import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OrderDoneView } from "./page";
import type { TicketOrderDetail } from "@/lib/types";

const replace = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace, push: vi.fn() }),
}));

const PAID: TicketOrderDetail = {
  order_id: "ORD-20260801-A1B2C3D4",
  status: "PAID",
  total_amount: "550000.00",
  subtotal: "500000.00",
  fees: [{ name: "PPN (10%)", amount: "50000.00" }],
  terms_agreed_at: "2026-08-01T10:00:30Z",
  expires_at: "2026-08-01T10:15:00Z",
  payment_started: true,
  slots: [],
  event: {
    name: "Jazz Night 2026",
    slug: "jazz-night-2026",
    venue: "Balai Sarbini",
    address: "Jakarta, Indonesia",
    start_date: "2026-09-26T08:00:00Z",
    end_date: "2026-09-27T14:00:00Z",
  },
  items: [
    {
      kind: "ticket",
      ticket_type_name: "Regular",
      package_name: null,
      quantity: 2,
      unit_price: "200000.00",
      subtotal: "400000.00",
    },
    {
      kind: "package",
      ticket_type_name: null,
      package_name: "2-Day Bundle",
      quantity: 1,
      unit_price: "150000.00",
      subtotal: "150000.00",
    },
  ],
  server_time: "2026-08-01T10:03:00Z",
  payment: null,
};

const EXPIRED: TicketOrderDetail = { ...PAID, status: "EXPIRED" };
const CANCELLED: TicketOrderDetail = { ...PAID, status: "CANCELLED" };

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** Success bodies arrive wrapped in the API envelope (spec 008). */
function envelope(data: unknown) {
  return jsonResponse({ code: 200000, message: "Success", data });
}

/** A fresh client per test so no cached order leaks between them. */
function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return Wrapper;
}

function renderDone(eventSlug = PAID.event.slug) {
  return render(
    <OrderDoneView eventSlug={eventSlug} orderNumber={PAID.order_id} />,
    { wrapper: wrapper() },
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
  replace.mockClear();
});

// The mirror of the earlier screens' forwards: nothing has finished yet, so a
// guest who lands here early belongs back on the step they actually stopped at
// — the QR screen once payment has started, the holder forms before that
// (spec 011 FR-021).
describe("confirmation — an order still awaiting payment", () => {
  it("sends an order that is already paying back to the QR screen", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope({ ...PAID, status: "PENDING" })));

    renderDone();

    await waitFor(() =>
      expect(replace).toHaveBeenCalledWith(
        `/events/${PAID.event.slug}/orders/${PAID.order_id}/checkout`,
      ),
    );
  });

  it("sends an order whose payment never started back to the holder forms", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        envelope({ ...PAID, status: "PENDING", payment_started: false, payment: null }),
      ),
    );

    renderDone();

    await waitFor(() =>
      expect(replace).toHaveBeenCalledWith(
        `/events/${PAID.event.slug}/orders/${PAID.order_id}`,
      ),
    );
  });

  it("never calls a pending order cancelled on the way", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope({ ...PAID, status: "PENDING" })));

    renderDone();

    await waitFor(() => expect(replace).toHaveBeenCalled());
    expect(screen.queryByRole("heading", { name: /order cancelled/i })).not.toBeInTheDocument();
  });

  it("does not forward a pending order reached through the wrong event", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope({ ...PAID, status: "PENDING" })));

    renderDone("some-other-event");

    await screen.findByRole("heading", { name: /order not found for this event/i });
    expect(replace).not.toHaveBeenCalled();
  });
});

describe("confirmation — a paid order", () => {
  it("confirms the payment and names the event", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderDone();

    expect(
      await screen.findByRole("heading", { name: /payment successful/i }),
    ).toBeInTheDocument();
    expect(screen.getByText("Jazz Night 2026")).toBeInTheDocument();
  });

  it("shows the order number and the total paid", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderDone();

    expect(await screen.findByText(PAID.order_id)).toBeInTheDocument();
    expect(screen.getByText(/Rp\s?550[.,]000/)).toBeInTheDocument();
  });

  it("lists every purchased item with its quantity", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderDone();

    expect(await screen.findByText("Regular")).toBeInTheDocument();
    expect(screen.getByText(/Quantity: 2 tickets/i)).toBeInTheDocument();

    // A bundle is named by its package, not by a constituent ticket type.
    expect(screen.getByText("2-Day Bundle")).toBeInTheDocument();
    expect(screen.getByText(/Quantity: 1 ticket$/i)).toBeInTheDocument();
  });

  it("says a confirmation email was sent, and offers a way home", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderDone();

    expect(await screen.findByText(/email confirmation has been sent/i)).toBeInTheDocument();

    // Button renders its anchor with role="button", as elsewhere in the app.
    expect(screen.getByRole("button", { name: /back to home/i })).toHaveAttribute(
      "href",
      "/",
    );
  });
});

describe("confirmation — resending the email", () => {
  it("reports success once the resend goes through", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(
          url.includes("/resend-email")
            ? jsonResponse({ message: "If that order exists, its ticket email has been sent again." }, 202)
            : envelope(PAID),
        ),
      ),
    );

    renderDone();
    await userEvent.click(await screen.findByRole("button", { name: /resend email/i }));

    expect(await screen.findByText(/sent again/i)).toBeInTheDocument();
  });

  it("sends only the order number — never an address", async () => {
    const fetchSpy = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        url.includes("/resend-email")
          ? jsonResponse({ message: "ok" }, 202)
          : envelope(PAID),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);

    renderDone();
    await userEvent.click(await screen.findByRole("button", { name: /resend email/i }));

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find((args) =>
        String(args[0]).includes("/resend-email"),
      );
      expect(call).toBeDefined();
      // The order number travels in the body's order_id field, not the URL.
      expect(String(call?.[1]?.body)).toContain(PAID.order_id);
      // No address travels at all — delivery is per holder (spec 011), and the
      // server reads every recipient from the order itself.
      expect(JSON.stringify(call?.[1] ?? {})).not.toContain("@");
    });
  });

  // A 429 is "too soon", not "broken" — the endpoint allows one send a minute.
  it("shows a cooldown rather than an error when asked too often", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((url: string) =>
        Promise.resolve(
          url.includes("/resend-email")
            ? jsonResponse({ error_code: "RATE_LIMITED", message: "Too many requests." }, 429)
            : envelope(PAID),
        ),
      ),
    );

    renderDone();
    await userEvent.click(await screen.findByRole("button", { name: /resend email/i }));

    expect(await screen.findByText(/please wait a minute/i)).toBeInTheDocument();
    expect(screen.queryByText(/could not send/i)).not.toBeInTheDocument();
  });
});

// FR-020: the journey ends here even when it did not end in a purchase.
describe("confirmation — an order that never got paid", () => {
  it("reports an expired order instead of congratulating the guest", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderDone();

    expect(await screen.findByRole("heading", { name: /order expired/i })).toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: /payment successful/i }),
    ).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: /back to the event/i })).toHaveAttribute(
      "href",
      "/events/jazz-night-2026",
    );
  });

  it("reports a cancelled order distinctly from an expired one", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(CANCELLED)));

    renderDone();

    expect(await screen.findByRole("heading", { name: /order cancelled/i })).toBeInTheDocument();
  });

  it("offers no resend for an order with no tickets", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderDone();
    await screen.findByRole("heading", { name: /order expired/i });

    expect(screen.queryByRole("button", { name: /resend email/i })).not.toBeInTheDocument();
  });
});

// FR-013: a confirmation discloses as much as the order itself.
describe("confirmation — wrong event", () => {
  it("refuses an order belonging to another event and shows none of it", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderDone("some-other-event");

    expect(
      await screen.findByRole("heading", { name: /order not found for this event/i }),
    ).toBeInTheDocument();
    expect(screen.queryByText(PAID.order_id)).not.toBeInTheDocument();
    expect(screen.queryByText("Jazz Night 2026")).not.toBeInTheDocument();
    expect(screen.queryByText(/Rp\s?550[.,]000/)).not.toBeInTheDocument();
  });
});

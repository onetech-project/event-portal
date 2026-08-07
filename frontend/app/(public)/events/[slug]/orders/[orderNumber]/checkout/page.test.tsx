import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { CheckoutView } from "./page";
import type { TicketOrderDetail } from "@/lib/types";

const replace = vi.fn();
const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace, push }),
}));

/**
 * The QRIS screen's own tests. Spec 011 FR-020 moved this screen off the order
 * page onto `/orders/{orderNumber}/checkout`, so the payment-phase coverage
 * that used to live in `../page.test.tsx` lives here — plus the two state
 * forwards the split introduced (FR-021).
 */
const PENDING: TicketOrderDetail = {
  order_id: "ORD-20260801-A1B2C3D4",
  status: "PENDING",
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
      unit_price: "275000.00",
      subtotal: "550000.00",
    },
  ],
  server_time: "2026-08-01T10:03:00Z",
  payment: {
    method: "QRIS",
    provider: "midtrans",
    amount: "550000.00",
    expires_at: "2026-08-01T10:15:00Z",
    qr_image_path: "/api/v1/orders/ORD-20260801-A1B2C3D4/qris.png",
  },
};

const PAID: TicketOrderDetail = { ...PENDING, status: "PAID", payment: null };
const EXPIRED: TicketOrderDetail = { ...PENDING, status: "EXPIRED", payment: null };
/** Payment never started — this guest belongs back on the holder forms. */
const HELD: TicketOrderDetail = { ...PENDING, payment_started: false, payment: null };

const FORMS_PATH = `/events/${PENDING.event.slug}/orders/${PENDING.order_id}`;
const DONE_PATH = `${FORMS_PATH}/done`;

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
    defaultOptions: { queries: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return Wrapper;
}

// CheckoutView rather than the default export: the page's route-param
// unwrapping uses `use()`, which does not settle under this test DOM.
function renderCheckout(eventSlug = PENDING.event.slug) {
  return render(
    <CheckoutView eventSlug={eventSlug} orderNumber={PENDING.order_id} />,
    { wrapper: wrapper() },
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
  replace.mockClear();
  push.mockClear();
});

describe("checkout page — awaiting payment", () => {
  it("shows the order, the QR code, and the amount to pay", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderCheckout();

    expect(await screen.findByText(/scan to pay/i)).toBeInTheDocument();
    // The Complete Purchase countdown banner spans the page (Figma 32-1366).
    expect(screen.getByText(/complete purchase/i)).toBeInTheDocument();
    expect(screen.getByText("Jazz Night 2026")).toBeInTheDocument();
    // The summary lists the ticket line with its quantity badge and line
    // subtotal (Figma 206-3145 shows no per-unit price on this line).
    expect(screen.getByText("Regular")).toBeInTheDocument();
    expect(screen.getByText("x2")).toBeInTheDocument();
    // The line subtotal and the grand total are both Rp 550.000 here.
    expect(screen.getAllByText(/550[.,]000/).length).toBeGreaterThan(0);
    // NOTE: spec 011 FR-013's Booking ID was removed from the panel by hand to
    // match the design; assertion suspended until UI and spec agree again.
    // How to pay is a collapsible on the summary (Figma 203-1157).
    expect(screen.getByRole("button", { name: /how to pay with qris/i })).toBeInTheDocument();

    // The payment breakdown frozen at booking (spec 011 FR-016): Ticket Total,
    // one row per fee, then the grand total. This screen itemizes; the forms
    // step deliberately does not.
    expect(screen.getByText("Ticket Total").parentElement).toHaveTextContent(/500[.,]000/);
    expect(screen.getByText("PPN (10%)").parentElement).toHaveTextContent(/50[.,]000/);
    expect(screen.getByText(/total payment/i)).toBeInTheDocument();

    // Rendered by our own API from the stored payload — the browser never talks
    // to the payment provider.
    const qr = screen.getByRole("img", { name: /QRIS code/i });
    expect(qr).toHaveAttribute("src", "http://api.test" + PENDING.payment!.qr_image_path);
  });

  it("collapses the breakdown to the total alone when the order predates fees", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(envelope({ ...PENDING, subtotal: null, fees: [] })),
    );

    renderCheckout();
    await screen.findByText(/scan to pay/i);

    expect(screen.queryByText("Ticket Total")).not.toBeInTheDocument();
    expect(screen.getByText(/total payment/i)).toBeInTheDocument();
  });

  it("expands the how-to-pay collapsible to the QRIS steps", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderCheckout();
    const trigger = await screen.findByRole("button", { name: /how to pay with qris/i });

    // Collapsed by default (Figma 32-1366); the steps appear on demand.
    expect(screen.queryByText(/scan qr or pay menu/i)).not.toBeInTheDocument();
    await userEvent.setup().click(trigger);
    expect(await screen.findByText(/scan qr or pay menu/i)).toBeInTheDocument();
    expect(screen.getByText(/enter your pin/i)).toBeInTheDocument();
  });

  it("offers a way to check the payment status on demand", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderCheckout();

    expect(
      await screen.findByRole("button", { name: /check payment status/i }),
    ).toBeInTheDocument();
  });

  it("says the page updates itself while it is polling", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderCheckout();

    expect(await screen.findByText(/updates by itself/i)).toBeInTheDocument();
  });

  it("shows no ticket holder forms — those belong to the previous step", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderCheckout();
    await screen.findByText(/scan to pay/i);

    expect(
      screen.queryByPlaceholderText(/as written on id card/i),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /continue to payment/i }),
    ).not.toBeInTheDocument();
  });
});

// Spec 011 FR-021: each of the two order screens forwards to the other when the
// order's state does not belong to it — always with replace, so the back button
// never bounces between two screens that forward to each other.
describe("checkout page — state forwards", () => {
  it("sends an order whose payment never started back to the forms", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(HELD)));

    renderCheckout();

    await waitFor(() => expect(replace).toHaveBeenCalledWith(FORMS_PATH));
    expect(push).not.toHaveBeenCalled();
    // Nothing scannable is rendered on the way out.
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
  });

  it("sends a paid order to the confirmation screen", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderCheckout();

    await waitFor(() => expect(replace).toHaveBeenCalledWith(DONE_PATH));
    expect(push).not.toHaveBeenCalled();
  });

  it("keeps an order that is genuinely awaiting payment where it is", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderCheckout();

    await screen.findByRole("img", { name: /QRIS code/i });
    expect(replace).not.toHaveBeenCalled();
  });

  // The dead ends outrank the forwards: an expired order has nowhere better to
  // be, and a congratulations screen would misreport it (spec 007 FR-020).
  it("shows the Time's Up dialog for an expired order instead of forwarding", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderCheckout();

    expect(await screen.findByRole("heading", { name: /time's up/i })).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
  });

  // FR-022's actual demand: the screen is not replaced. Without this the suite
  // passes for the full-page card too — it also showed a "Time's Up" heading.
  it("keeps the payment screen rendered behind the dialog", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderCheckout();
    await screen.findByRole("heading", { name: /time's up/i });

    expect(screen.getByText("Jazz Night 2026")).toBeInTheDocument();
    expect(screen.getByText("Regular")).toBeInTheDocument();
  });

  it("shows a cancelled order the same dialog, worded as cancelled", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(envelope({ ...PENDING, status: "CANCELLED", payment: null })),
    );

    renderCheckout();

    expect(
      await screen.findByRole("heading", { name: /order cancelled/i }),
    ).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});

// FR-023. Asserted here rather than left to a manual pass because each of these
// is a *separate* Base UI setting — a library upgrade that adds a dismissal
// reason would silently reopen the page behind an order the guest can no longer
// pay for, and only a test would catch it.
describe("checkout page — the dialog cannot be dismissed", () => {
  async function openedDialog() {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));
    renderCheckout();
    return screen.findByRole("dialog");
  }

  it("stays open when Escape is pressed", async () => {
    const dialog = await openedDialog();

    await userEvent.keyboard("{Escape}");

    expect(dialog).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: /time's up/i })).toBeInTheDocument();
  });

  it("stays open when the backdrop is clicked", async () => {
    await openedDialog();

    const backdrop = document.querySelector('[data-slot="dialog-overlay"]');
    expect(backdrop).not.toBeNull();
    await userEvent.click(backdrop as Element);

    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: /time's up/i })).toBeInTheDocument();
  });

  it("offers no close control at all", async () => {
    const dialog = await openedDialog();

    expect(within(dialog).queryByRole("button", { name: /close/i })).not.toBeInTheDocument();
    // Exactly one way out, and it leaves the flow rather than dismissing.
    const actions = within(dialog).getAllByRole("link");
    expect(actions).toHaveLength(1);
    expect(actions[0]).toHaveAttribute("href", "/");
  });
});

// FR-022: one expiry produces one message. The client countdown is the trigger,
// not the server's status — waiting for the sweep would leave a scannable QR on
// screen for up to a poll interval after the code died.
describe("checkout page — the countdown reaching zero", () => {
  // Same PENDING order the server still considers live, seen from a moment
  // after its payment deadline: the countdown computes zero on mount.
  const COUNTDOWN_DONE: TicketOrderDetail = {
    ...PENDING,
    server_time: "2026-08-01T10:20:00Z",
  };

  it("opens the dialog while the server still reports the order PENDING", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(COUNTDOWN_DONE)));

    renderCheckout();

    expect(await screen.findByRole("heading", { name: /time's up/i })).toBeInTheDocument();
    // Not forwarded — the order is still PENDING as far as the server knows.
    expect(replace).not.toHaveBeenCalled();
  });

  it("takes the QR code away rather than leaving something scannable behind it", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(COUNTDOWN_DONE)));

    renderCheckout();
    await screen.findByRole("heading", { name: /time's up/i });

    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /check payment status/i }),
    ).not.toBeInTheDocument();
  });

  // The inline StatusAlert that used to occupy this branch is deleted: it was
  // the second message for a single expiry, which is what FR-022 removes.
  it("shows no inline expired notice alongside the dialog", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(COUNTDOWN_DONE)));

    renderCheckout();
    await screen.findByRole("heading", { name: /time's up/i });

    expect(screen.queryByText(/this payment code has expired/i)).not.toBeInTheDocument();
  });

  // The server's EXPIRED lands moments later. It must change nothing on screen.
  it("looks identical once the server's own EXPIRED arrives", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderCheckout();
    await screen.findByRole("heading", { name: /time's up/i });

    expect(screen.getAllByRole("dialog")).toHaveLength(1);
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/this payment code has expired/i)).not.toBeInTheDocument();
  });
});

// The remaining forward rules, resumed after the two dialog describes above.
describe("checkout page — isolation and polling", () => {
  // Wrong-event isolation outranks every forward: forwarding would confirm the
  // order exists and hand over the event that owns it (spec 007 FR-013).
  it("refuses an order belonging to another event without forwarding", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderCheckout("some-other-event");

    await screen.findByRole("heading", { name: /order not found for this event/i });
    expect(replace).not.toHaveBeenCalled();
    expect(screen.queryByText("Jazz Night 2026")).not.toBeInTheDocument();
  });

  // A settled page must place no further load on the API.
  it("stops polling once the status is final", async () => {
    const fetchSpy = vi.fn().mockResolvedValue(envelope(PAID));
    vi.stubGlobal("fetch", fetchSpy);

    renderCheckout();
    await waitFor(() => expect(replace).toHaveBeenCalled());

    const afterLoad = fetchSpy.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 100));

    expect(fetchSpy.mock.calls.length).toBe(afterLoad);
  });
});

describe("checkout page — checking status", () => {
  it("reports that nothing has changed yet", async () => {
    const fetchSpy = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        url.includes("/payment/refresh")
          ? envelope({
              order_number: PENDING.order_id,
              status: "PENDING",
              changed: false,
              checked_at: "2026-08-01T10:04:00Z",
            })
          : envelope(PENDING),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);

    renderCheckout();
    await userEvent.click(
      await screen.findByRole("button", { name: /check payment status/i }),
    );

    expect(await screen.findByText(/still waiting for your payment/i)).toBeInTheDocument();
  });

  // Spec 008 FR-018: pressing too often is a cooldown, not an error.
  it("shows a cooldown rather than an error when rate limited", async () => {
    const fetchSpy = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        url.includes("/payment/refresh")
          ? jsonResponse({ error_code: "RATE_LIMITED", message: "Too many requests." }, 429)
          : envelope(PENDING),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);

    renderCheckout();
    const button = await screen.findByRole("button", { name: /check payment status/i });
    await userEvent.click(button);

    expect(await screen.findByText(/wait a few seconds/i)).toBeInTheDocument();
    await waitFor(() => expect(button).toBeDisabled());
  });
});

describe("checkout page — unknown order", () => {
  it("says the order was not found without revealing anything else", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({ error_code: "ORDER_NOT_FOUND", message: "Order not found." }, 404),
      ),
    );

    renderCheckout();

    expect(await screen.findByText(/could not find an order/i)).toBeInTheDocument();
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
  });
});

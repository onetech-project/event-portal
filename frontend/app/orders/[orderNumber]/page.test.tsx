import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OrderView } from "./page";
import type { PublicOrderDetail } from "@/lib/types";

const PENDING: PublicOrderDetail = {
  order_number: "ORD-20260801-A1B2C3D4",
  status: "PENDING",
  total_amount: "550000.00",
  buyer_name: "Siti Rahayu",
  buyer_email: "siti@example.com",
  created_at: "2026-08-01T10:00:00Z",
  event: { name: "Jazz Night 2026", slug: "jazz-night-2026" },
  items: [
    {
      ticket_type_name: "Regular",
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

const PAID: PublicOrderDetail = { ...PENDING, status: "PAID", payment: null };
const EXPIRED: PublicOrderDetail = { ...PENDING, status: "EXPIRED", payment: null };

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** A fresh client per test so no cached order leaks between them. */
function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

// OrderView rather than the default export: the page's route-param unwrapping
// uses `use()`, which does not settle under this test DOM.
function renderOrder() {
  return render(<OrderView orderNumber={PENDING.order_number} />, { wrapper: wrapper() });
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("order page — awaiting payment", () => {
  it("shows the order, the QR code, and the amount to pay", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(PENDING)));

    renderOrder();

    expect(await screen.findByText(/complete your payment/i)).toBeInTheDocument();
    expect(screen.getByText(PENDING.order_number, { exact: false })).toBeInTheDocument();
    expect(screen.getByText("Jazz Night 2026")).toBeInTheDocument();
    expect(screen.getByText(/Regular × 2/)).toBeInTheDocument();
    expect(screen.getByText("siti@example.com")).toBeInTheDocument();

    // Rendered by our own API from the stored payload — the browser never talks
    // to the payment provider.
    const qr = screen.getByRole("img", { name: /QRIS code/i });
    expect(qr).toHaveAttribute("src", "http://api.test" + PENDING.payment!.qr_image_path);
  });

  it("offers a way to check the payment status on demand", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(PENDING)));

    renderOrder();

    expect(
      await screen.findByRole("button", { name: /check payment status/i }),
    ).toBeInTheDocument();
  });

  it("says the page updates itself while it is polling", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(PENDING)));

    renderOrder();

    expect(await screen.findByText(/updates by itself/i)).toBeInTheDocument();
  });
});

describe("order page — settled", () => {
  it("shows the success state and no QR once paid", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(PAID)));

    renderOrder();

    expect(
      await screen.findByRole("heading", { name: /payment successful/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(/on their way to/i)).toBeInTheDocument();
    expect(screen.getAllByText("siti@example.com").length).toBeGreaterThan(0);
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /check payment status/i }),
    ).not.toBeInTheDocument();
  });

  it("explains an expired order and links back to the event", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse(EXPIRED)));

    renderOrder();

    expect(await screen.findByText(/expired before it was paid/i)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /back to the event/i })).toHaveAttribute(
      "href",
      "/events/jazz-night-2026",
    );
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
  });

  // FR-020: a settled page must place no further load on the API.
  it("stops polling once the status is final", async () => {
    const fetchSpy = vi.fn().mockResolvedValue(jsonResponse(PAID));
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await screen.findByRole("heading", { name: /payment successful/i });

    const afterLoad = fetchSpy.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 100));

    expect(fetchSpy.mock.calls.length).toBe(afterLoad);
  });
});

describe("order page — checking status", () => {
  it("reports that nothing has changed yet", async () => {
    const fetchSpy = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        url.includes("/payment/refresh")
          ? jsonResponse({
              order_number: PENDING.order_number,
              status: "PENDING",
              changed: false,
              checked_at: "2026-08-01T10:04:00Z",
            })
          : jsonResponse(PENDING),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await userEvent.click(
      await screen.findByRole("button", { name: /check payment status/i }),
    );

    expect(await screen.findByText(/still waiting for your payment/i)).toBeInTheDocument();
  });

  // FR-018: pressing too often is a cooldown, not an error.
  it("shows a cooldown rather than an error when rate limited", async () => {
    const fetchSpy = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        url.includes("/payment/refresh")
          ? jsonResponse(
              { error_code: "RATE_LIMITED", message: "Too many requests." },
              429,
            )
          : jsonResponse(PENDING),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    const button = await screen.findByRole("button", { name: /check payment status/i });
    await userEvent.click(button);

    expect(await screen.findByText(/wait a few seconds/i)).toBeInTheDocument();
    await waitFor(() => expect(button).toBeDisabled());
  });
});

describe("order page — unknown order", () => {
  it("says the order was not found without revealing anything else", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({ error_code: "ORDER_NOT_FOUND", message: "Order not found." }, 404),
      ),
    );

    renderOrder();

    expect(await screen.findByText(/order not found/i)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /browse events/i })).toBeInTheDocument();
  });
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OrderPaymentPanel } from "./order-payment-panel";
import { storeToken } from "@/lib/auth";

const ORDER_ID = "44444444-4444-4444-4444-444444444444";
const TICKET_ID = "55555555-5555-5555-5555-555555555555";
const OTHER_TICKET_ID = "66666666-6666-6666-6666-666666666666";

/** Success bodies arrive wrapped in the API envelope (spec 008). */
function envelope(data: unknown) {
  return new Response(JSON.stringify({ code: 200000, message: "Success", data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

const NOTIFICATIONS = [
  {
    id: "n2",
    provider: "manjo",
    transaction_id: "A48593Completed",
    status: "SETTLE_REFUSED_NO_QUOTA",
    is_marker: true,
    payment_type: "qris",
    raw_payload: {},
    received_at: "2026-08-10T10:20:00Z",
  },
  {
    id: "n1",
    provider: "manjo",
    transaction_id: "A48593Expired",
    status: "Expired",
    is_marker: false,
    payment_type: "qris",
    raw_payload: {},
    received_at: "2026-08-10T10:00:00Z",
  },
];

function stubApi(holds: unknown) {
  const spy = vi.fn().mockImplementation((rawUrl: string | URL) => {
    const url = String(rawUrl);
    if (url.includes("/notifications")) return Promise.resolve(envelope(NOTIFICATIONS));
    if (url.includes("/holds")) return Promise.resolve(envelope(holds));
    return Promise.reject(new Error(`unexpected fetch: ${url}`));
  });
  vi.stubGlobal("fetch", spy);
  return spy;
}

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

function renderPanel(orderStatus = "EXPIRED") {
  return render(
    <OrderPaymentPanel
      orderId={ORDER_ID}
      orderNumber="ORD-20260810-A7K2QX"
      orderStatus={orderStatus}
      open
      onOpenChange={() => {}}
    />,
    { wrapper },
  );
}

describe("OrderPaymentPanel", () => {
  beforeEach(() => {
    vi.unstubAllGlobals();
    localStorage.clear();
    // Every route here is admin-only, and adminFetch refuses outright without a
    // live session rather than letting the request reach the network.
    storeToken("admin-token", new Date(Date.now() + 60 * 60 * 1000).toISOString());
  });

  it("shows every notification, including the ones this system refused", async () => {
    stubApi([{ ticket_type_id: TICKET_ID, ticket_type_name: "Regular", held: 3, remaining: 7 }]);
    renderPanel();

    // The refusal is often the whole explanation for why an order is stuck, so a
    // history that showed only accepted notifications would hide the reason.
    await waitFor(() =>
      expect(screen.getByText("SETTLE_REFUSED_NO_QUOTA")).toBeTruthy(),
    );
    expect(screen.getByText("Expired")).toBeTruthy();
  });

  it("separates this system's own conclusions from what the gateway said", async () => {
    stubApi([{ ticket_type_id: TICKET_ID, ticket_type_name: "Regular", held: 3, remaining: 7 }]);
    renderPanel();

    // Both live in the same field on the wire; letting a marker read as a gateway
    // status would be misleading on exactly the screen where it matters most.
    await waitFor(() => expect(screen.getByText("our note")).toBeTruthy());
  });

  it("reports seats held against seats remaining", async () => {
    stubApi([{ ticket_type_id: TICKET_ID, ticket_type_name: "Regular", held: 3, remaining: 7 }]);
    renderPanel();

    await waitFor(() => expect(screen.getByText("Regular")).toBeTruthy());
    expect(screen.getByText("3")).toBeTruthy();
    expect(screen.getByText("7")).toBeTruthy();
  });

  it("names the top-up an expired order needs before a resend can settle it", async () => {
    stubApi([{ ticket_type_id: TICKET_ID, ticket_type_name: "Regular", held: 3, remaining: 1 }]);
    renderPanel("EXPIRED");

    await waitFor(() => expect(screen.getByText("2 more")).toBeTruthy());
    expect(
      screen.getByText(/one ticket type is short/i),
    ).toBeTruthy();
  });

  it("counts every short constituent of a bundle, because the settle is all-or-nothing", async () => {
    stubApi([
      { ticket_type_id: TICKET_ID, ticket_type_name: "Day 1", held: 3, remaining: 1 },
      { ticket_type_id: OTHER_TICKET_ID, ticket_type_name: "Day 2", held: 3, remaining: 0 },
    ]);
    renderPanel("EXPIRED");

    // Topping up only the first would earn a second refusal on the same order.
    await waitFor(() => expect(screen.getByText(/2 ticket types are short/i)).toBeTruthy());
  });

  it("raises no shortfall warning when the quota covers the hold", async () => {
    stubApi([{ ticket_type_id: TICKET_ID, ticket_type_name: "Regular", held: 3, remaining: 7 }]);
    renderPanel("EXPIRED");

    await waitFor(() => expect(screen.getByText("Regular")).toBeTruthy());
    expect(screen.queryByText(/short/i)).toBeNull();
  });

  it("offers no way to record that a payment happened", async () => {
    stubApi([{ ticket_type_id: TICKET_ID, ticket_type_name: "Regular", held: 3, remaining: 7 }]);
    renderPanel();

    await waitFor(() => expect(screen.getByText("Regular")).toBeTruthy());

    // FR-022d. Ticket issuance has exactly one trigger: a notification from the
    // gateway. A button here would be a second source of payment truth, and the
    // one least exercised at the moment it matters.
    expect(screen.queryByRole("button", { name: /confirm|mark.*paid|settle/i })).toBeNull();
  });
});

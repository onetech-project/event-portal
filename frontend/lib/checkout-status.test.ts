import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { useCheckoutStatus } from "./checkout-status";
import { queryKeys } from "./queries";
import type { TicketOrderDetail } from "./types";

const ORDER_ID = "ORD-20260805-A1B2C3D4";

/** The minimal EventSource surface the hook touches. */
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  closed = false;

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  close() {
    this.closed = true;
  }

  emit(data: unknown) {
    this.onmessage?.({ data: JSON.stringify(data) });
  }
}

function harness() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  client.setQueryData<TicketOrderDetail>(queryKeys.order(ORDER_ID), {
    order_id: ORDER_ID,
    status: "PENDING",
    total_amount: "100000.00",
    subtotal: null,
    fees: [],
    buyer_name: null,
    buyer_email: null,
    expires_at: null,
    terms_agreed_at: null,
    payment_started: true,
    event: {
      name: "Jive",
      slug: "jive-2026",
      venue: "Stadion Madya GBK",
      address: "Jakarta, Indonesia",
      start_date: "2026-09-26T08:00:00Z",
      end_date: "2026-09-27T14:00:00Z",
    },
    items: [],
    slots: [],
    server_time: "2026-08-05T10:00:00Z",
    payment: null,
  });
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client }, children);
  return { client, wrapper };
}

afterEach(() => {
  FakeEventSource.instances = [];
  vi.unstubAllGlobals();
});

describe("useCheckoutStatus", () => {
  it("reports not-live when the runtime has no EventSource (polling carries it)", () => {
    const { wrapper } = harness();

    const { result } = renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    expect(result.current.live).toBe(false);
  });

  it("opens the stream against the checkout status path", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const { wrapper } = harness();

    renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    expect(FakeEventSource.instances).toHaveLength(1);
    expect(FakeEventSource.instances[0].url).toContain(
      `/ticket/checkout/${ORDER_ID}/status`,
    );
  });

  it("patches the cached order the moment a frame arrives", async () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const { client, wrapper } = harness();
    renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    FakeEventSource.instances[0].emit({ order_id: ORDER_ID, status: "PAID" });

    await waitFor(() => {
      const cached = client.getQueryData<TicketOrderDetail>(queryKeys.order(ORDER_ID));
      expect(cached?.status).toBe("PAID");
    });
  });

  it("closes the stream after a terminal status", async () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const { wrapper } = harness();
    renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    FakeEventSource.instances[0].emit({ order_id: ORDER_ID, status: "EXPIRED" });

    await waitFor(() => expect(FakeEventSource.instances[0].closed).toBe(true));
  });

  it("opens nothing when disabled", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const { wrapper } = harness();

    renderHook(() => useCheckoutStatus(ORDER_ID, false), { wrapper });

    expect(FakeEventSource.instances).toHaveLength(0);
  });
});

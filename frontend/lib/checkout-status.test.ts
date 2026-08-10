import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  DEGRADED_POLL_INTERVAL_MS,
  STREAM_LIVENESS_TIMEOUT_MS,
  useCheckoutStatus,
} from "./checkout-status";
import { queryKeys } from "./queries";
import type { TicketOrderDetail } from "./types";

const ORDER_ID = "ORD-20260805-A1B2C3D4";

/**
 * The minimal EventSource surface the hook touches.
 *
 * Note what it does NOT have: any way for a comment frame to reach the page.
 * That is faithful — the browser discards SSE comments without dispatching
 * anything, which is exactly why the server's liveness beat is a named event
 * rather than the comment it used to be.
 */
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  closed = false;
  private listeners = new Map<string, Set<() => void>>();

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, handler: () => void) {
    const set = this.listeners.get(type) ?? new Set();
    set.add(handler);
    this.listeners.set(type, set);
  }

  removeEventListener(type: string, handler: () => void) {
    this.listeners.get(type)?.delete(handler);
  }

  close() {
    this.closed = true;
  }

  /** The server's named liveness beat. */
  beat() {
    for (const handler of this.listeners.get("keep-alive") ?? []) handler();
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
  vi.useRealTimers();
});

describe("useCheckoutStatus", () => {
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

    act(() => FakeEventSource.instances[0].emit({ order_id: ORDER_ID, status: "PAID" }));

    await waitFor(() => {
      const cached = client.getQueryData<TicketOrderDetail>(queryKeys.order(ORDER_ID));
      expect(cached?.status).toBe("PAID");
    });
  });

  it("closes the stream after a terminal status", async () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const { wrapper } = harness();
    renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    act(() =>
      FakeEventSource.instances[0].emit({ order_id: ORDER_ID, status: "EXPIRED" }),
    );

    await waitFor(() => expect(FakeEventSource.instances[0].closed).toBe(true));
  });

  it("opens nothing when disabled", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const { wrapper } = harness();

    renderHook(() => useCheckoutStatus(ORDER_ID, false), { wrapper });

    expect(FakeEventSource.instances).toHaveLength(0);
  });

  // A runtime with no EventSource has a permanently failed stream, not a
  // transient one, so it must degrade rather than sit silent. It is flagged on
  // the next tick rather than during the first render, so the initial paint is
  // not a cascading re-render.
  it("degrades when the runtime has no EventSource", async () => {
    const { wrapper } = harness();

    const { result } = renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    await waitFor(() => expect(result.current.degraded).toBe(true));
    expect(result.current.live).toBe(false);
  });
});

describe("useCheckoutStatus liveness watchdog", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  it("is live and not degraded once the stream opens", () => {
    const { wrapper } = harness();
    const { result } = renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    act(() => FakeEventSource.instances[0].onopen?.());

    expect(result.current.live).toBe(true);
    expect(result.current.degraded).toBe(false);
  });

  // The silent-buffering case, and the whole reason the watchdog exists: an
  // intermediary accepts the connection and forwards nothing, so `onopen` fires,
  // no error is ever raised, and connection state reports health that is not
  // there.
  it("declares a silent stream failed and starts falling back", () => {
    const { wrapper } = harness();
    const { result } = renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    act(() => FakeEventSource.instances[0].onopen?.());
    expect(result.current.live).toBe(true);

    act(() => vi.advanceTimersByTime(STREAM_LIVENESS_TIMEOUT_MS + 1_000));

    expect(result.current.live).toBe(false);
    expect(result.current.degraded).toBe(true);
  });

  // A healthy idle stream sends no data frames at all — only the beat — so the
  // beat must be what keeps the watchdog satisfied. If it did not, every single
  // payment would false-positive into degraded mode within a minute.
  it("stays live while the keep-alive beat arrives", () => {
    const { wrapper } = harness();
    const { result } = renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    act(() => FakeEventSource.instances[0].onopen?.());

    for (let i = 0; i < 4; i++) {
      act(() => vi.advanceTimersByTime(STREAM_LIVENESS_TIMEOUT_MS - 5_000));
      act(() => FakeEventSource.instances[0].beat());
    }

    expect(result.current.live).toBe(true);
    expect(result.current.degraded).toBe(false);
  });

  // Covers both a dropped connection and a refused one (the 429 at the per-address
  // stream cap). EventSource does not retry a non-200, so waiting for a reconnect
  // that will never come would leave the page permanently silent.
  it("degrades on error rather than waiting for a reconnect", () => {
    const { wrapper } = harness();
    const { result } = renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    act(() => FakeEventSource.instances[0].onopen?.());
    act(() => FakeEventSource.instances[0].onerror?.());

    expect(result.current.live).toBe(false);
    expect(result.current.degraded).toBe(true);
  });

  it("leaves degraded mode as soon as the stream speaks again", () => {
    const { wrapper } = harness();
    const { result } = renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    act(() => FakeEventSource.instances[0].onopen?.());
    act(() => FakeEventSource.instances[0].onerror?.());
    expect(result.current.degraded).toBe(true);

    act(() => FakeEventSource.instances[0].onopen?.());

    expect(result.current.live).toBe(true);
    expect(result.current.degraded).toBe(false);
  });

  // SC-015: zero repeating requests while the stream is healthy. This is the
  // property the standing 3-second poll was traded away for, so it is asserted
  // directly rather than inferred.
  it("issues no repeating reads while the stream is healthy", () => {
    const { client, wrapper } = harness();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    act(() => FakeEventSource.instances[0].onopen?.());
    act(() => vi.advanceTimersByTime(DEGRADED_POLL_INTERVAL_MS * 6));

    expect(invalidate).not.toHaveBeenCalled();
  });

  it("reads periodically only while degraded, and stops when it recovers", () => {
    const { client, wrapper } = harness();
    const invalidate = vi.spyOn(client, "invalidateQueries");
    renderHook(() => useCheckoutStatus(ORDER_ID, true), { wrapper });

    act(() => FakeEventSource.instances[0].onopen?.());
    act(() => FakeEventSource.instances[0].onerror?.());

    act(() => vi.advanceTimersByTime(DEGRADED_POLL_INTERVAL_MS * 3));
    const whileDegraded = invalidate.mock.calls.length;
    expect(whileDegraded).toBeGreaterThan(0);

    act(() => FakeEventSource.instances[0].onopen?.());
    act(() => vi.advanceTimersByTime(DEGRADED_POLL_INTERVAL_MS * 5));

    expect(invalidate.mock.calls.length).toBe(whileDegraded);
  });
});

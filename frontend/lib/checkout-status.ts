"use client";

import { useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { apiBaseUrl } from "./env";
import { isFinalOrderStatus, queryKeys } from "./queries";
import type { CheckoutStatusEvent, TicketOrderDetail } from "./types";

/**
 * How long the stream may stay silent before it is presumed dead.
 *
 * Twice the server's 25-second keep-alive interval. One interval would
 * false-positive on ordinary jitter; two leaves a full missed beat of margin and
 * still notices inside a minute, which is well within any payment window.
 */
export const STREAM_LIVENESS_TIMEOUT_MS = 50_000;

/**
 * How often the page re-reads the order while the stream is known to be failed.
 *
 * This is NOT a standing poll. It runs only in degraded mode and stops the
 * moment the stream is healthy again, which is what makes "zero repeating
 * requests while the stream is healthy" an observable property rather than a
 * claim.
 */
export const DEGRADED_POLL_INTERVAL_MS = 5_000;

/** The server's named liveness beat. Never reaches `onmessage`. */
const KEEP_ALIVE_EVENT = "keep-alive";

/**
 * `connecting` — opened, nothing heard yet; neither live nor degraded.
 * `live` — spoke within the timeout.
 * `failed` — errored, refused, or silent past the timeout.
 */
type StreamState = "connecting" | "live" | "failed";

export type CheckoutStatusState = {
  /** True while the stream is connected AND has spoken within the timeout. */
  live: boolean;
  /**
   * True while the page is falling back to periodic reads because the stream is
   * failed. Exposed so the screen can say so rather than looking identical to a
   * healthy one.
   */
  degraded: boolean;
};

/**
 * Live checkout status over SSE (GET /ticket/checkout/:order_id/status).
 *
 * Frames land in the shared order query, so every consumer — the QR panel, the
 * redirect effect, the countdown — reacts through one code path.
 *
 * The standing 3-second poll that used to run underneath this is gone. It was
 * there as a fallback for intermediaries that buffer streaming responses, and
 * that risk is real — but paying for it on every healthy page was the wrong
 * trade, because the failure it guarded against is *detectable*. Two mechanisms
 * replace it:
 *
 *   - a liveness watchdog, because a proxy that accepts the connection and
 *     forwards nothing produces `onopen` and no error at all, so connection
 *     state alone reports health that is not there;
 *   - treating a refused connection as failed rather than waiting, because
 *     EventSource only retries after a network-level drop — a non-200 (the 429
 *     at the per-address stream cap) fails it permanently and no reconnect is
 *     ever attempted.
 *
 * The server closes the stream after a terminal status; a page that knows the
 * order is settled does not reopen it.
 */
export function useCheckoutStatus(
  orderNumber: string,
  enabled: boolean,
): CheckoutStatusState {
  const queryClient = useQueryClient();
  const [state, setState] = useState<StreamState>("connecting");

  // Held in a ref so re-arming the watchdog on every beat does not re-run the
  // effect and tear down the connection it is measuring.
  const watchdog = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (!enabled || orderNumber === "") return;

    // jsdom (and very old browsers) have no EventSource. That is a permanently
    // failed stream, not a transient one — but it is flagged on the next tick
    // rather than during this render, so the first paint is not a cascading
    // re-render.
    if (typeof EventSource === "undefined") {
      const timer = setTimeout(() => setState("failed"), 0);
      return () => clearTimeout(timer);
    }

    const source = new EventSource(
      `${apiBaseUrl()}/ticket/checkout/${encodeURIComponent(orderNumber)}/status`,
    );

    const clearWatchdog = () => {
      if (watchdog.current !== null) {
        clearTimeout(watchdog.current);
        watchdog.current = null;
      }
    };

    // Every beat and every frame re-arms it. Silence past the timeout is the
    // only thing that trips it.
    const armWatchdog = () => {
      clearWatchdog();
      watchdog.current = setTimeout(() => setState("failed"), STREAM_LIVENESS_TIMEOUT_MS);
    };

    const markHealthy = () => {
      setState("live");
      armWatchdog();
    };

    source.onopen = markHealthy;
    source.addEventListener(KEEP_ALIVE_EVENT, markHealthy);

    // Covers both a dropped connection (the browser will retry) and a refused
    // one (it will not). They are indistinguishable here and need not be
    // distinguished: either way the stream is not delivering, so the fallback
    // engages. If the browser does reconnect, `onopen` turns it off again.
    source.onerror = () => {
      clearWatchdog();
      setState("failed");
    };

    source.onmessage = (message: MessageEvent<string>) => {
      markHealthy();

      let event: CheckoutStatusEvent;
      try {
        event = JSON.parse(message.data) as CheckoutStatusEvent;
      } catch {
        return; // a malformed frame is not worth breaking the stream over
      }

      // Patch the cached order so the UI flips instantly, then refetch for the
      // full authoritative detail (payment fields, server_time).
      queryClient.setQueryData<TicketOrderDetail>(
        queryKeys.order(orderNumber),
        (current) =>
          current === undefined
            ? current
            : { ...current, status: event.status as TicketOrderDetail["status"] },
      );
      void queryClient.invalidateQueries({ queryKey: queryKeys.order(orderNumber) });

      if (isFinalOrderStatus(event.status)) {
        source.close();
        clearWatchdog();
        setState("connecting");
      }
    };

    return () => {
      source.removeEventListener(KEEP_ALIVE_EVENT, markHealthy);
      source.close();
      clearWatchdog();
      setState("connecting");
    };
  }, [orderNumber, enabled, queryClient]);

  const degraded = enabled && orderNumber !== "" && state === "failed";

  // Degraded-mode reads. Deliberately a separate effect keyed on `degraded`, so
  // it starts and stops with the mode and cannot outlive it — the property that
  // makes "no repeating requests while healthy" hold.
  useEffect(() => {
    if (!degraded) return;

    const timer = setInterval(() => {
      void queryClient.invalidateQueries({ queryKey: queryKeys.order(orderNumber) });
    }, DEGRADED_POLL_INTERVAL_MS);

    return () => clearInterval(timer);
  }, [degraded, orderNumber, queryClient]);

  return {
    live: enabled && orderNumber !== "" && state === "live",
    degraded,
  };
}

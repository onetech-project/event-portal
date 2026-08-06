"use client";

import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { apiBaseUrl } from "./env";
import { isFinalOrderStatus, queryKeys } from "./queries";
import type { CheckoutStatusEvent, TicketOrderDetail } from "./types";

/**
 * Live checkout status over SSE (GET /ticket/checkout/:order_id/status).
 *
 * Frames land in the shared order query, so every consumer — the QR panel, the
 * redirect effect, the countdown — reacts through the one code path the 3-second
 * poll already feeds. The poll in `useOrderDetail` never stops running, which IS
 * the mandated fallback: when the stream drops (returns false here), the page
 * degrades to 3-second polling with no mode switch, and the browser's own
 * EventSource retry brings the stream back.
 *
 * The server closes the stream after a terminal status; a page that knows the
 * order is settled doesn't reopen it.
 */
export function useCheckoutStatus(orderNumber: string, enabled: boolean): { live: boolean } {
  const queryClient = useQueryClient();
  const [live, setLive] = useState(false);

  useEffect(() => {
    // jsdom (and very old browsers) have no EventSource: polling carries it.
    // No state reset needed here — a previous streaming run's cleanup below
    // already set live back to false before this run started.
    if (!enabled || orderNumber === "" || typeof EventSource === "undefined") {
      return;
    }

    const source = new EventSource(
      `${apiBaseUrl()}/ticket/checkout/${encodeURIComponent(orderNumber)}/status`,
    );

    source.onopen = () => setLive(true);
    // The browser retries on its own; `live` only reports honestly meanwhile.
    source.onerror = () => setLive(false);

    source.onmessage = (message: MessageEvent<string>) => {
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
        setLive(false);
      }
    };

    return () => {
      source.close();
      setLive(false);
    };
  }, [orderNumber, enabled, queryClient]);

  return { live };
}

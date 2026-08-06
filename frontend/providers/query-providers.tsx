"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { useState, type ReactNode } from "react";

/**
 * Wraps the app in a TanStack Query client.
 *
 * The client is created inside state so each browser session gets exactly one,
 * and a server render never shares a cache between requests.
 */
export function QueryProviders({ children }: { children: ReactNode }) {
  const [client] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            // Quotas are live inventory: a stale count would offer a guest
            // tickets that are already gone.
            staleTime: 0,
            refetchOnWindowFocus: true,
            retry: 1,
          },
        },
      }),
  );

  return (
    <QueryClientProvider client={client}>
      {children}
      <ReactQueryDevtools />
    </QueryClientProvider>
  );
}

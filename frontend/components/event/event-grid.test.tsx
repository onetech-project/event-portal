import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { EventGrid } from "./event-grid";
import type { EventSummary } from "@/lib/types";

const EVENTS: EventSummary[] = [
  {
    id: "11111111-1111-1111-1111-111111111111",
    name: "Jive Jakarta 2026",
    slug: "jive-2026",
    venue: "JIExpo",
    address: "Kemayoran",
    start_date: "2026-08-20T12:00:00Z",
    end_date: "2026-08-21T12:00:00Z",
    banner_url: "https://cdn.example.com/banner.png",
  },
  {
    id: "22222222-2222-2222-2222-222222222222",
    name: "Rock Fest",
    slug: "rock-fest",
    venue: "GBK",
    address: "Senayan",
    start_date: "2026-09-01T12:00:00Z",
    end_date: "2026-09-01T20:00:00Z",
    banner_url: null,
  },
];

function envelope(data: unknown) {
  return new Response(JSON.stringify({ code: 200000, message: "Success", data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function renderGrid() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return render(<EventGrid />, { wrapper: Wrapper });
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("EventGrid", () => {
  it("renders every published event as a card linking to its detail page", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(envelope(EVENTS));

    renderGrid();

    expect(await screen.findByText("Jive Jakarta 2026")).toBeTruthy();
    expect(screen.getByText("Rock Fest")).toBeTruthy();
    expect(screen.getByText("JIExpo")).toBeTruthy();

    const links = screen.getAllByRole("link");
    expect(links.map((l) => l.getAttribute("href"))).toEqual([
      "/events/jive-2026",
      "/events/rock-fest",
    ]);
  });

  it("says so when nothing is on sale", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(envelope([]));

    renderGrid();

    expect(await screen.findByText(/no events are on sale/i)).toBeTruthy();
  });
});

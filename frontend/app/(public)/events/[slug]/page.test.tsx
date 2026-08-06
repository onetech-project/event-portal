import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { EventDetailView } from "./page";
import type { EventDetail } from "@/lib/types";

const SLUG = "jive-2026";

const EVENT: EventDetail = {
  id: "11111111-1111-1111-1111-111111111111",
  name: "Jive Jakarta 2026",
  slug: SLUG,
  venue: "JIExpo Hall 1A & 1B",
  address: "Jakarta, Indonesia",
  start_date: "2026-09-26T10:00:00Z",
  end_date: "2026-09-27T22:00:00Z",
  banner_url: "https://cdn.example.com/banner.png",
  description: "<p>The most anticipated <strong>B2B expo</strong>.</p>",
  scale: 30000,
  activities: [
    { id: "a1", title: "Vape Competitions", description: "Cloud chasing.", icon: "star", position: 1 },
    { id: "a2", title: "Music & Entertainment", description: "Main stage.", icon: "music", position: 2 },
  ],
  guest_stars: [{ id: "g1", name: "DJ Nova", position: 1 }],
  guidelines: [
    { id: "gl1", description: "Strictly 17+ only.", icon: "id-card", position: 1 },
  ],
  has_terms: true,
};

function envelope(data: unknown) {
  return new Response(JSON.stringify({ code: 200000, message: "Success", data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return render(<EventDetailView slug={SLUG} />, { wrapper: Wrapper });
}

beforeEach(() => {
  vi.restoreAllMocks();
});

describe("event detail page", () => {
  it("renders the CMS content: description HTML, activities, guests, guidelines", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(envelope(EVENT));

    renderPage();

    // The WYSIWYG description arrives as sanitized HTML and renders as markup.
    expect(await screen.findByText("B2B expo")).toBeInTheDocument();

    expect(screen.getByText("Vape Competitions")).toBeInTheDocument();
    expect(screen.getByText("Music & Entertainment")).toBeInTheDocument();
    expect(screen.getByText("DJ Nova")).toBeInTheDocument();
    expect(screen.getByText("Strictly 17+ only.")).toBeInTheDocument();
    // The venue appears in the info bar and again on the guidelines card.
    expect(screen.getAllByText("JIExpo Hall 1A & 1B").length).toBeGreaterThan(0);
  });

  it("shows no ticket or package data — tickets live on their own route", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(envelope(EVENT));

    renderPage();

    const buy = await screen.findByRole("link", { name: /buy tickets/i });
    expect(buy).toHaveAttribute("href", `/events/${SLUG}/tickets`);
    // One fetch: the detail endpoint. No /ticket/:slug, no /packages/:slug.
    const urls = vi.mocked(globalThis.fetch).mock.calls.map((c) => String(c[0]));
    expect(urls.every((u) => u.includes(`/event/${SLUG}`))).toBe(true);
  });

  it("disables Buy Tickets with a notice while no terms are authored", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      envelope({ ...EVENT, has_terms: false }),
    );

    renderPage();

    expect(await screen.findByText(/sales not open yet/i)).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /buy tickets/i })).not.toBeInTheDocument();
  });
});

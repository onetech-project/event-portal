import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { EventFrame } from "./event-frame";
import type { EventDetail } from "@/lib/types";

const SLUG = "jive-2026";

// Mutable so each test can choose which screen of the journey it frames. The
// detail page is the default; journey screens set a deeper path.
let pathname = `/events/${SLUG}`;
vi.mock("next/navigation", () => ({
  usePathname: () => pathname,
}));

const EVENT: EventDetail = {
  id: "11111111-1111-1111-1111-111111111111",
  name: "Jive Jakarta 2026",
  slug: SLUG,
  venue: "JIExpo",
  address: "Kemayoran",
  start_date: new Date(Date.now() + 3 * 86_400_000).toISOString(),
  end_date: new Date(Date.now() + 4 * 86_400_000).toISOString(),
  banner_url: null,
  description: null,
  scale: null,
  activities: [],
  guest_stars: [],
  guidelines: [],
  has_terms: true,
};

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

/** A fresh client per test so no cached event leaks between them. */
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

function renderFrame() {
  return render(
    <EventFrame slug={SLUG}>
      <p>nested screen</p>
    </EventFrame>,
    { wrapper: wrapper() },
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
  pathname = `/events/${SLUG}`;
});

describe("EventFrame — while the event is loading", () => {
  it("shows no countdown at all rather than a zeroed placeholder", async () => {
    // A never-settling fetch holds the query pending for the whole assertion.
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderFrame();

    expect(screen.queryByText("Days")).toBeNull();
    expect(screen.queryByText(/event starts in/i)).toBeNull();
    // The zeros a skeleton would show read as an event starting right now.
    expect(screen.queryByText("00")).toBeNull();
  });

  it("still renders the progress rail on journey screens, which does not depend on the event", async () => {
    pathname = `/events/${SLUG}/tickets`;
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderFrame();

    expect(screen.getByText("Booking")).toBeTruthy();
  });

  it("shows no rail on the detail page — it is a landing page, not a step (Figma 4-5)", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderFrame();

    expect(screen.queryByText("Booking")).toBeNull();
  });

  it("does not block the nested screen from rendering its own loading state", async () => {
    vi.spyOn(globalThis, "fetch").mockReturnValue(new Promise(() => {}));

    renderFrame();

    expect(screen.getByText("nested screen")).toBeTruthy();
  });
});

describe("EventFrame — unknown event", () => {
  it("suppresses all nested content and says the event was not found", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ message: "Event not found." }, 404),
    );

    renderFrame();

    await waitFor(() => {
      expect(screen.getByText(/event not found/i)).toBeTruthy();
    });

    expect(screen.queryByText("nested screen")).toBeNull();
    expect(screen.queryByText("Days")).toBeNull();
  });

  it("offers a way back to the events list", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      jsonResponse({ message: "Event not found." }, 404),
    );

    renderFrame();

    await waitFor(() => {
      const link = screen.getByRole("link", { name: /events/i });
      expect(link.getAttribute("href")).toBe("/events");
    });
  });
});

describe("EventFrame — resolved event", () => {
  it("renders the countdown, the rail, and the nested screen together", async () => {
    pathname = `/events/${SLUG}/tickets`;
    vi.spyOn(globalThis, "fetch").mockResolvedValue(envelope(EVENT));

    renderFrame();

    await waitFor(() => {
      expect(screen.getByText(/event starts in/i)).toBeTruthy();
    });

    expect(screen.getByText("Booking")).toBeTruthy();
    expect(screen.getByText("nested screen")).toBeTruthy();
  });

  it("renders the rail and the nested screen but no countdown once the event has started", async () => {
    pathname = `/events/${SLUG}/tickets`;
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      envelope({ ...EVENT, start_date: new Date(Date.now() - 86_400_000).toISOString() }),
    );

    renderFrame();

    await waitFor(() => {
      expect(screen.getByText("nested screen")).toBeTruthy();
    });

    expect(screen.queryByText(/event starts in/i)).toBeNull();
    expect(screen.getByText("Booking")).toBeTruthy();
  });
});

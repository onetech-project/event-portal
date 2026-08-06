import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { TermsDialog } from "./terms-dialog";
import type { SelectionLine } from "@/lib/types";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
}));

const EVENT_ID = "11111111-1111-1111-1111-111111111111";
const TERMS_ID = "22222222-2222-2222-2222-222222222222";
const TICKET_ID = "33333333-3333-3333-3333-333333333333";
const ORDER_ID = "ORD-20260805-A1B2C3D4";

const LINES: SelectionLine[] = [
  { kind: "ticket", id: TICKET_ID, name: "Day 1", unitPrice: "35000.00", quantity: 2 },
];

const TERMS = {
  id: TERMS_ID,
  content: "<ol><li>All ticket sales are final.</li></ol>",
  updated_at: "2026-08-01T00:00:00Z",
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** Success bodies arrive wrapped in the API envelope (spec 008). */
function envelope(data: unknown, status = 200) {
  return jsonResponse({ code: 200000, message: "Success", data }, status);
}

/**
 * The dialog's three calls, routed by URL. Overrides let a test fail one leg.
 */
function stubApi(overrides: { book?: Response; agreement?: Response } = {}) {
  const spy = vi.fn().mockImplementation((rawUrl: string | URL) => {
    const url = String(rawUrl);
    if (url.includes("/ticket/book")) {
      return Promise.resolve(
        overrides.book ??
          envelope(
            {
              order_id: ORDER_ID,
              status: "PENDING",
              total_amount: "70000.00",
              expires_at: "2026-08-05T11:00:00Z",
            },
            201,
          ),
      );
    }
    if (url.includes(`/ticket/terms-condition/${ORDER_ID}`)) {
      return Promise.resolve(overrides.agreement ?? envelope(null));
    }
    // GET /ticket/terms-condition/:event_slug
    return Promise.resolve(envelope(TERMS));
  });
  vi.stubGlobal("fetch", spy);
  return spy;
}

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return Wrapper;
}

function setup() {
  return render(
    <TermsDialog
      eventId={EVENT_ID}
      eventSlug="jive-2026"
      eventName="Jive Indonesia 2026"
      lines={LINES}
      triggerClassName="buy"
    />,
    { wrapper: wrapper() },
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
  push.mockClear();
});

describe("TermsDialog", () => {
  it("keeps the terms closed until Buy Ticket is pressed, then shows the CMS document", async () => {
    stubApi();
    setup();

    expect(screen.queryByText(/i agree to terms/i)).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));

    expect(screen.getByText(/i agree to terms/i)).toBeTruthy();
    // The event is named as the subject of the terms, not just the page title.
    expect(screen.getByText("Jive Indonesia 2026")).toBeTruthy();
    // The clause comes from the fetched document, not a static copy.
    expect(await screen.findByText(/all ticket sales are final/i)).toBeTruthy();
  });

  it("withholds Agree until the guest ticks the box", async () => {
    stubApi();
    setup();
    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);

    expect(screen.queryByRole("button", { name: /^agree$/i })).toBeNull();

    await userEvent.click(screen.getByRole("checkbox"));

    expect(screen.getByRole("button", { name: /^agree$/i })).toBeTruthy();
  });

  it("books, records the agreement, and routes to the order page on Agree", async () => {
    const fetchSpy = stubApi();
    setup();
    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);
    await userEvent.click(screen.getByRole("checkbox"));

    await userEvent.click(screen.getByRole("button", { name: /^agree$/i }));

    await waitFor(() =>
      expect(push).toHaveBeenCalledWith(`/events/jive-2026/orders/${ORDER_ID}`),
    );

    // Routing is asynchronous and the dialog outlives it, so a guest whose
    // booking worked must never be shown the failure affordance while the order
    // page loads — nothing failed and there is nothing to retry.
    expect(screen.queryByRole("button", { name: /retry/i })).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("button", { name: /opening your order/i })).toBeTruthy();

    // Call order: book first, then the agreement carrying the shown terms id.
    const calls = fetchSpy.mock.calls.map((args) => String(args[0]));
    const bookIndex = calls.findIndex((u) => u.includes("/ticket/book"));
    const agreeIndex = calls.findIndex((u) => u.includes(`/ticket/terms-condition/${ORDER_ID}`));
    expect(bookIndex).toBeGreaterThanOrEqual(0);
    expect(agreeIndex).toBeGreaterThan(bookIndex);
    const agreementBody = JSON.parse(String(fetchSpy.mock.calls[agreeIndex][1]?.body));
    expect(agreementBody).toEqual({ agreed: true, event_terms_id: TERMS_ID });
  });

  it("retries only the agreement after it fails, never booking twice", async () => {
    const fetchSpy = stubApi({
      agreement: jsonResponse({ code: 500000, message: "boom", data: null }, 500),
    });
    setup();
    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);
    await userEvent.click(screen.getByRole("checkbox"));

    await userEvent.click(screen.getByRole("button", { name: /^agree$/i }));

    // The failure surfaces and the button becomes a retry.
    expect(await screen.findByRole("alert")).toBeTruthy();
    const retry = await screen.findByRole("button", { name: /retry/i });
    expect(push).not.toHaveBeenCalled();

    // Second attempt succeeds — and must not open a second hold.
    stubApiSecondAttempt(fetchSpy);
    await userEvent.click(retry);

    await waitFor(() =>
      expect(push).toHaveBeenCalledWith(`/events/jive-2026/orders/${ORDER_ID}`),
    );
    const bookCalls = fetchSpy.mock.calls.filter((args) =>
      String(args[0]).includes("/ticket/book"),
    );
    expect(bookCalls).toHaveLength(1);
  });

  it("forgets the agreement once the dialog is closed", async () => {
    stubApi();
    setup();
    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);
    await userEvent.click(screen.getByRole("checkbox"));
    await userEvent.click(screen.getByRole("button", { name: /cancel/i }));

    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));

    // Reopening starts unticked, so an old tick cannot carry a new selection
    // through to booking.
    expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe("false");
    expect(screen.queryByRole("button", { name: /^agree$/i })).toBeNull();
  });
});

/** Rewires the shared spy so the agreement leg now succeeds. */
function stubApiSecondAttempt(spy: ReturnType<typeof vi.fn>) {
  spy.mockImplementation((rawUrl: string | URL) => {
    const url = String(rawUrl);
    if (url.includes(`/ticket/terms-condition/${ORDER_ID}`)) {
      return Promise.resolve(envelope(null));
    }
    return Promise.resolve(envelope(TERMS));
  });
}

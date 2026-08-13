import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render as rtlRender, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactElement, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { SelectionSummary } from "./selection-summary";
import type { AvailabilityReason, SelectionLine } from "@/lib/types";

// The nested TermsDialog reaches for the router and the query client; neither
// affects what this suite asserts, so both are the thinnest possible stand-ins.
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}));

function render(ui: ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return rtlRender(ui, { wrapper: Wrapper });
}

const DAY1 = "11111111-1111-1111-1111-111111111111";
const DAY2 = "22222222-2222-2222-2222-222222222222";
const BUNDLE = "33333333-3333-3333-3333-333333333333";

const day1: SelectionLine = {
  kind: "ticket",
  id: DAY1,
  name: "Jive (Day 1) - 26 Apr 2026",
  unitPrice: "35000.00",
  quantity: 1,
};

const day2: SelectionLine = { ...day1, id: DAY2, name: "Jive (Day 2) - 27 Apr 2026" };

const bundle: SelectionLine = {
  kind: "package",
  id: BUNDLE,
  name: "Jive (Day 1 and 2) - 26 - 27 Apr 2026",
  unitPrice: "50000.00",
  quantity: 1,
};

/** Envelope-shaped fetch stub for POST /ticket/availability. */
function stubAvailability(
  decision: { available: boolean; reasons: AvailabilityReason[] },
): ReturnType<typeof vi.fn> {
  const fetchMock = vi.fn(async () =>
    new Response(JSON.stringify({ code: 200000, message: "OK", data: decision }), {
      status: 200,
      headers: { "content-type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function refusal(overrides: Partial<AvailabilityReason> = {}): AvailabilityReason {
  return {
    item_index: 0,
    ticket_type_id: DAY1,
    package_id: null,
    code: "INSUFFICIENT_QUOTA",
    message: "Only fewer than 2 ticket(s) remain.",
    ...overrides,
  };
}

function renderSelection(lines: SelectionLine[] = [day1]) {
  return render(
    <SelectionSummary
      lines={lines}
      eventId="11111111-0000-0000-0000-000000000000"
      eventSlug="jive-2026"
      eventName="Jive Indonesia 2026"
    />,
  );
}

const pressBuyTicket = () =>
  userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));

beforeEach(() => {
  vi.restoreAllMocks();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("SelectionSummary", () => {
  it("shows its placeholder and disables Buy Ticket when empty", () => {
    render(
      <SelectionSummary
        lines={[]}
        eventId="11111111-0000-0000-0000-000000000000"
        eventSlug="jive-2026"
        eventName="Jive Indonesia 2026"
      />,
    );

    expect(screen.getByText(/selected ticket will appear here/i)).toBeTruthy();
    // A <span> stand-in rather than a control: there is nothing to check out yet.
    expect(screen.queryByRole("button", { name: /buy ticket/i })).toBeNull();
  });

  it("enables Buy Ticket as soon as one row is selected", () => {
    render(
      <SelectionSummary
        lines={[day1]}
        eventId="11111111-0000-0000-0000-000000000000"
        eventSlug="jive-2026"
        eventName="Jive Indonesia 2026"
      />,
    );

    // A button, not a link: it opens the Terms & Conditions gate first.
    expect(screen.getByRole("button", { name: /buy ticket/i })).toBeTruthy();
    expect(screen.queryByRole("link", { name: /buy ticket/i })).toBeNull();
    expect(screen.queryByText(/selected ticket will appear here/i)).toBeNull();
  });

  it("totals two individually selected days", () => {
    render(<SelectionSummary lines={[day1, day2]} eventId="11111111-0000-0000-0000-000000000000" eventSlug="jive-2026" eventName="Jive Indonesia 2026" />);

    expect(screen.getByText("Total 2 Tickets")).toBeTruthy();
    expect(screen.getByText(/Rp\s?70[.,]000/)).toBeTruthy();
  });

  it("renders a selected bundle as ONE line at its own price", () => {
    render(<SelectionSummary lines={[bundle]} eventId="11111111-0000-0000-0000-000000000000" eventSlug="jive-2026" eventName="Jive Indonesia 2026" />);

    // Not two day lines, and not Rp70.000: the guest bought the bundle.
    expect(screen.getByText(bundle.name)).toBeTruthy();
    expect(screen.queryByText(/Day 1\) - 26 Apr/)).toBeNull();
    // Twice over: once as the line subtotal, once as the grand total.
    expect(screen.getAllByText(/Rp\s?50[.,]000/)).toHaveLength(2);
  });

  it("lists a bundle and a ticket as independent lines", () => {
    render(<SelectionSummary lines={[day1, bundle]} eventId="11111111-0000-0000-0000-000000000000" eventSlug="jive-2026" eventName="Jive Indonesia 2026" />);

    expect(screen.getByText(day1.name)).toBeTruthy();
    expect(screen.getByText(bundle.name)).toBeTruthy();
    expect(screen.getByText(/Rp\s?85[.,]000/)).toBeTruthy();
  });

  it("multiplies a line by its quantity", () => {
    render(
      <SelectionSummary lines={[{ ...bundle, quantity: 3 }]} eventId="11111111-0000-0000-0000-000000000000" eventSlug="jive-2026" eventName="Jive Indonesia 2026" />,
    );

    // 3 × Rp50.000, as both the line subtotal and the grand total.
    expect(screen.getAllByText(/Rp\s?150[.,]000/)).toHaveLength(2);
    // Every line counts in "Ticket(s)", bundles included, and the count is
    // pluralised. 59b2293 had introduced a separate "Bundle" unit label here
    // and unpluralised the total; this commit's own change reverted the total
    // but left the line label behind, so these two assertions disagreed with
    // each other's panel and stayed red on main. The line and the total must
    // count the same things in the same words.
    expect(screen.getByText("3 Tickets")).toBeTruthy();
    expect(screen.getByText("Total 3 Tickets")).toBeTruthy();
  });
});

/**
 * Spec 013. Buy Ticket asks the server whether the selection can still be
 * bought, and the Terms & Conditions gate opens only on a clean answer.
 */
describe("SelectionSummary availability gate", () => {
  it("opens the terms only after the check comes back available", async () => {
    const fetchMock = stubAvailability({ available: true, reasons: [] });
    renderSelection();

    expect(screen.queryByText(/i agree to terms/i)).toBeNull();

    await pressBuyTicket();

    await waitFor(() => expect(screen.getByText(/i agree to terms/i)).toBeTruthy());
    expect(fetchMock.mock.calls[0][0]).toContain("/ticket/availability");
  });

  it("refuses without opening the terms, and says why", async () => {
    stubAvailability({ available: false, reasons: [refusal()] });
    renderSelection();

    await pressBuyTicket();

    await waitFor(() =>
      expect(screen.getByRole("alert", { name: /why this selection cannot be bought/i }).textContent).toContain(
        "Only fewer than 2 ticket(s) remain.",
      ),
    );
    // The whole point: the guest never reaches the document.
    expect(screen.queryByText(/i agree to terms/i)).toBeNull();
  });

  it("reports every offending line, not just the first", async () => {
    stubAvailability({
      available: false,
      reasons: [
        refusal(),
        refusal({
          item_index: 1,
          ticket_type_id: DAY2,
          code: "TICKET_TYPE_NOT_ON_SALE",
          message: 'Ticket type "Jive (Day 2) - 27 Apr 2026" is not currently on sale.',
        }),
      ],
    });
    renderSelection([day1, day2]);

    await pressBuyTicket();

    await waitFor(() => expect(screen.getByRole("alert", { name: /why this selection cannot be bought/i })).toBeTruthy());
    const alert = screen.getByRole("alert", { name: /why this selection cannot be bought/i }).textContent ?? "";
    expect(alert).toContain("Only fewer than 2 ticket(s) remain.");
    expect(alert).toContain("is not currently on sale");
  });

  it("leaves the selection untouched when refused", async () => {
    stubAvailability({ available: false, reasons: [refusal()] });
    renderSelection([{ ...day1, quantity: 2 }]);

    await pressBuyTicket();
    await waitFor(() => expect(screen.getByRole("alert", { name: /why this selection cannot be bought/i })).toBeTruthy());

    // Still the guest's own choice, to adjust as they see fit (FR-007).
    expect(screen.getByText("2 Tickets")).toBeTruthy();
    expect(screen.getByText("Total 2 Tickets")).toBeTruthy();
  });

  it("drops the refusal as soon as the guest changes the selection", async () => {
    stubAvailability({ available: false, reasons: [refusal()] });
    const { rerender } = renderSelection([day1]);

    await pressBuyTicket();
    await waitFor(() =>
      expect(screen.getByRole("alert", { name: /why this selection cannot be bought/i })).toBeTruthy(),
    );

    // The guest edits the selection. The message described the old one.
    rerender(
      <SelectionSummary
        lines={[{ ...day1, quantity: 2 }]}
        eventId="11111111-0000-0000-0000-000000000000"
        eventSlug="jive-2026"
        eventName="Jive Indonesia 2026"
      />,
    );

    expect(
      screen.queryByRole("alert", { name: /why this selection cannot be bought/i }),
    ).toBeNull();
  });

  it("does not resurrect a refusal after the selection is emptied and rebuilt", async () => {
    stubAvailability({ available: false, reasons: [refusal()] });
    const { rerender } = renderSelection([day1]);

    await pressBuyTicket();
    await waitFor(() =>
      expect(screen.getByRole("alert", { name: /why this selection cannot be bought/i })).toBeTruthy(),
    );

    const show = (lines: SelectionLine[]) =>
      rerender(
        <SelectionSummary
          lines={lines}
          eventId="11111111-0000-0000-0000-000000000000"
          eventSlug="jive-2026"
          eventName="Jive Indonesia 2026"
        />,
      );

    // Remove every row: the panel returns to its placeholder, which merely
    // stops RENDERING the refusal...
    show([]);
    expect(screen.getByText(/selected ticket will appear here/i)).toBeTruthy();

    // ...and pressing Add again must not bring the old message back with it.
    show([day1]);
    expect(
      screen.queryByRole("alert", { name: /why this selection cannot be bought/i }),
    ).toBeNull();
  });

  it("issues no second check while one is in flight", async () => {
    let release: (value: Response) => void = () => {};
    const fetchMock = vi.fn(
      () => new Promise<Response>((resolve) => {
        release = resolve;
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    renderSelection();

    await pressBuyTicket();
    await waitFor(() => expect(screen.getByRole("button", { name: /checking/i })).toBeTruthy());

    // The button is busy, so the press cannot land again (FR-008).
    expect(screen.queryByRole("button", { name: /buy ticket/i })).toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(1);

    release(
      new Response(JSON.stringify({ code: 200000, message: "OK", data: { available: true, reasons: [] } }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
    await waitFor(() => expect(screen.getByText(/i agree to terms/i)).toBeTruthy());
  });

  it("checks again on every press rather than reusing the last answer", async () => {
    const fetchMock = stubAvailability({ available: false, reasons: [refusal()] });
    renderSelection();

    await pressBuyTicket();
    await waitFor(() => expect(screen.getByRole("alert", { name: /why this selection cannot be bought/i })).toBeTruthy());

    await pressBuyTicket();

    // FR-009: a decision describes one instant, and is never carried forward.
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));
  });

  it("treats a check that never answered as unknown, not as permission", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => {
      throw new TypeError("network down");
    }));
    renderSelection();

    await pressBuyTicket();

    await waitFor(() =>
      expect(screen.getByRole("alert", { name: /why this selection cannot be bought/i }).textContent).toContain("could not check availability"),
    );
    // A failed check is not a passing one.
    expect(screen.queryByText(/i agree to terms/i)).toBeNull();
  });
});

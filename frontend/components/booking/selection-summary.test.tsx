import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render as rtlRender, screen } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";

import { SelectionSummary } from "./selection-summary";
import type { SelectionLine } from "@/lib/types";

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

    expect(screen.getByText("Total 2 Ticket")).toBeTruthy();
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
    expect(screen.getByText("3 Bundle")).toBeTruthy();
    expect(screen.getByText("Total 3 Ticket")).toBeTruthy();
  });
});

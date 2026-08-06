import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { SalesCountdown } from "./sales-countdown";

/** An ISO timestamp `days` days from now, so the countdown has real values. */
function inDays(days: number): string {
  return new Date(Date.now() + days * 86_400_000).toISOString();
}

afterEach(() => {
  vi.useRealTimers();
});

describe("SalesCountdown", () => {
  it("renders nothing when the event has no start date", () => {
    const { container } = render(<SalesCountdown startDate={null} />);

    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing once the event has started", () => {
    // Counting into the negative would be worse than silence: the rows already
    // say sales have closed.
    const { container } = render(<SalesCountdown startDate={inDays(-1)} />);

    expect(container).toBeEmptyDOMElement();
  });

  it("renders nothing for an unparseable start date", () => {
    const { container } = render(<SalesCountdown startDate={"not-a-date"} />);

    expect(container).toBeEmptyDOMElement();
  });

  it("shows days, hours, minutes and seconds for a future event", () => {
    render(<SalesCountdown startDate={inDays(3)} />);

    for (const unit of ["Days", "Hours", "Min", "Sec"]) {
      expect(screen.getByText(unit)).toBeTruthy();
    }
  });

  it("labels what it is counting down to", () => {
    // FR-017: the payment screen shows this alongside the payment-expiry timer,
    // so a label naming a date rather than a duration is not enough.
    render(<SalesCountdown startDate={inDays(3)} />);

    expect(screen.getByText(/event starts in/i)).toBeTruthy();
  });

  it("does not cap the day count at two digits", () => {
    // The unit blocks are two characters wide by default; an event a year out
    // must still show its real day count rather than a truncated one.
    render(<SalesCountdown startDate={inDays(400)} />);

    const days = screen.getByText("Days").previousElementSibling;
    expect(days?.textContent).toMatch(/^\d{3}$/);
  });
});

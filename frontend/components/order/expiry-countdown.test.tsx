import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  clockOffsetMs,
  ExpiryCountdown,
  formatRemaining,
  remainingMs,
} from "./expiry-countdown";

/** Mirrors what the component does: measure the offset once, then count down. */
const remainingFrom = (expiresAt: string, serverTime: string, now: number) =>
  remainingMs(expiresAt, clockOffsetMs(serverTime, now), now);

afterEach(() => {
  vi.useRealTimers();
});

describe("remainingMs", () => {
  it("counts down from the server's deadline", () => {
    const now = Date.parse("2026-08-01T10:00:00Z");

    expect(remainingFrom("2026-08-01T10:05:00Z", "2026-08-01T10:00:00Z", now)).toBe(
      5 * 60_000,
    );
  });

  // The offset belongs to the response it was measured from. Re-measuring it
  // against a later local clock would subtract the elapsed time twice over and
  // freeze the display at its starting value.
  it("keeps counting down as the local clock advances past the response", () => {
    const responseAt = Date.parse("2026-08-01T10:00:00Z");
    const offset = clockOffsetMs("2026-08-01T10:00:00Z", responseAt);

    expect(remainingMs("2026-08-01T10:05:00Z", offset, responseAt + 2000)).toBe(
      4 * 60_000 + 58_000,
    );
  });

  /**
   * SC-005: a device clock an hour fast would otherwise show the code as long
   * expired. The offset is measured against the same response the deadline came
   * in, so the displayed time stays true.
   */
  it("corrects for a device clock running an hour fast", () => {
    const deviceNow = Date.parse("2026-08-01T11:00:00Z");

    expect(remainingFrom("2026-08-01T10:05:00Z", "2026-08-01T10:00:00Z", deviceNow)).toBe(
      5 * 60_000,
    );
  });

  it("corrects for a device clock running behind", () => {
    const deviceNow = Date.parse("2026-08-01T08:30:00Z");

    expect(remainingFrom("2026-08-01T10:05:00Z", "2026-08-01T10:00:00Z", deviceNow)).toBe(
      5 * 60_000,
    );
  });

  it("never goes negative once the deadline has passed", () => {
    const now = Date.parse("2026-08-01T10:06:00Z");

    expect(remainingFrom("2026-08-01T10:05:00Z", "2026-08-01T10:06:00Z", now)).toBe(0);
  });

  it("treats an unparseable timestamp as expired rather than throwing", () => {
    expect(remainingFrom("not-a-date", "2026-08-01T10:00:00Z", Date.now())).toBe(0);
  });
});

describe("formatRemaining", () => {
  it.each([
    [900_000, "15:00"],
    [61_000, "1:01"],
    [9_000, "0:09"],
    [0, "0:00"],
  ])("formats %ims as %s", (ms, expected) => {
    expect(formatRemaining(ms)).toBe(expected);
  });
});

describe("ExpiryCountdown", () => {
  it("shows the remaining time and ticks down", () => {
    vi.useFakeTimers();
    const now = Date.parse("2026-08-01T10:00:00Z");
    vi.setSystemTime(now);

    render(
      <ExpiryCountdown
        expiresAt="2026-08-01T10:05:00Z"
        serverTime="2026-08-01T10:00:00Z"
      />,
    );

    expect(screen.getByText("5:00")).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(2000));
    expect(screen.getByText("4:58")).toBeInTheDocument();
  });

  it("reports expiry once, when the countdown reaches zero", () => {
    vi.useFakeTimers();
    const now = Date.parse("2026-08-01T10:00:00Z");
    vi.setSystemTime(now);
    const onExpired = vi.fn();

    render(
      <ExpiryCountdown
        expiresAt="2026-08-01T10:00:03Z"
        serverTime="2026-08-01T10:00:00Z"
        onExpired={onExpired}
      />,
    );

    expect(onExpired).not.toHaveBeenCalled();

    act(() => vi.advanceTimersByTime(4000));

    expect(onExpired).toHaveBeenCalled();
    expect(screen.getByText(/expired/i)).toBeInTheDocument();
  });

  // What the banner is for is the clock, so the clock is what has to survive the
  // deadline. Replacing it with the word "Expired" removed it at the one moment
  // the guest was looking straight at it.
  it("holds the digits at zero rather than replacing them once time runs out", () => {
    vi.useFakeTimers();
    vi.setSystemTime(Date.parse("2026-08-01T10:00:00Z"));

    render(
      <ExpiryCountdown
        variant="digits"
        expiresAt="2026-08-01T10:00:03Z"
        serverTime="2026-08-01T10:00:00Z"
      />,
    );

    expect(screen.getByText("0 : 03")).toBeInTheDocument();

    act(() => vi.advanceTimersByTime(4000));

    expect(screen.getByText("0 : 00")).toBeInTheDocument();
    expect(screen.queryByText(/^expired$/i)).not.toBeInTheDocument();
  });

  // An order that ended before the page opened has no deadline left to count.
  // The clock still shows, because a banner that vanishes reads as a broken
  // page rather than as a closed window.
  it("shows a zeroed clock when there is no deadline left", () => {
    const onExpired = vi.fn();

    render(
      <ExpiryCountdown
        variant="digits"
        expiresAt={null}
        serverTime="2026-08-01T10:00:00Z"
        onExpired={onExpired}
      />,
    );

    expect(screen.getByText("0 : 00")).toBeInTheDocument();
    // Nothing expired while the guest watched, so nothing is reported.
    expect(onExpired).not.toHaveBeenCalled();
  });
});

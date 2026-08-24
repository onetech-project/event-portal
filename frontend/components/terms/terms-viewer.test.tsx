import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { useTermsAgreement } from "@/components/terms/terms-viewer";

/**
 * What this file does NOT test, and why that is deliberate.
 *
 * The scrolling itself is untestable at this tier. The test DOM is happy-dom,
 * where:
 *
 *   - `scrollHeight` / `clientHeight` are getter-only, PropertySymbol-backed and
 *     initialised to 0, so they cannot be stubbed by assignment; and
 *   - `IntersectionObserver` and `ResizeObserver` ARE registered on `window`, so
 *     a feature-detect finds them, but every method is an empty
 *     `// TODO: Not implemented` stub — the callback never fires and never throws.
 *
 * A test that scrolled a container and asserted the box ticked would therefore
 * evaluate `0 <= 0 + TOLERANCE`, conclude the document is non-scrollable, report
 * the end reached, and pass — having rendered nothing, measured nothing and
 * verified nothing. Worse, the observer path cannot even be detected as inert,
 * because the constructor exists.
 *
 * So the scroll is proved in Playwright, against a real browser and a
 * deliberately LONG document (e2e/specs/free-registration.spec.ts and
 * guest-purchase.spec.ts). Here we drive `onReachedEnd` directly as an input and
 * test the state rules around it, which are real logic and need no layout.
 */
describe("useTermsAgreement", () => {
  it("starts unagreed", () => {
    const { result } = renderHook(() => useTermsAgreement(true));
    expect(result.current.agreed).toBe(false);
  });

  it("agrees when the viewer reports the end was reached", () => {
    const { result } = renderHook(() => useTermsAgreement(true));
    act(() => result.current.onReachedEnd());
    expect(result.current.agreed).toBe(true);
  });

  // FR-045, clarified 2026-08-21: reading is a convenience, not a condition.
  it("agrees when the guest ticks the box, having read nothing", () => {
    const { result } = renderHook(() => useTermsAgreement(true));
    act(() => result.current.setAgreed(true));
    expect(result.current.agreed).toBe(true);
  });

  it("withdraws when the guest unticks the box", () => {
    const { result } = renderHook(() => useTermsAgreement(true));
    act(() => result.current.setAgreed(true));
    act(() => result.current.setAgreed(false));
    expect(result.current.agreed).toBe(false);
  });

  /**
   * FR-045a — the property most likely to be lost to a later refactor.
   *
   * A reasonable-looking change that "re-syncs the checkbox with the scroll
   * position" would re-tick a box the guest deliberately cleared: an automatic
   * action overwriting a deliberate one, about consent. TermsViewer's `firedRef`
   * already makes a second `onReachedEnd` unlikely, but that is one caller's ref
   * discipline. This asserts the rule as a property of the state, which is what
   * survives the viewer being rewritten.
   */
  it("does not re-tick a box the guest deliberately cleared", () => {
    const { result } = renderHook(() => useTermsAgreement(true));

    act(() => result.current.onReachedEnd());
    expect(result.current.agreed).toBe(true);

    act(() => result.current.setAgreed(false));
    expect(result.current.agreed).toBe(false);

    // The viewer fires again — a resize, a scroll away and back, an observer
    // that double-fires. The guest's choice stands until the dialog closes.
    act(() => result.current.onReachedEnd());
    expect(result.current.agreed).toBe(false);
  });

  it("re-arms the automatic tick on the next opening", () => {
    const { result, rerender } = renderHook(({ open }) => useTermsAgreement(open), {
      initialProps: { open: true },
    });

    act(() => result.current.setAgreed(false));
    act(() => result.current.onReachedEnd());
    expect(result.current.agreed).toBe(false);

    rerender({ open: false });
    rerender({ open: true });

    // A fresh opening is a fresh decision: the guest's untick from last time
    // must not disable the convenience for the rest of the session.
    act(() => result.current.onReachedEnd());
    expect(result.current.agreed).toBe(true);
  });

  // Agreement is deliberately not remembered across openings, so a guest is
  // never carried onward on an acceptance they abandoned. This is the rule the
  // booking dialog already applied to its checkbox.
  it("resets when the surface closes, so reopening starts unchecked", () => {
    const { result, rerender } = renderHook(({ open }) => useTermsAgreement(open), {
      initialProps: { open: true },
    });

    act(() => result.current.onReachedEnd());
    expect(result.current.agreed).toBe(true);

    rerender({ open: false });
    expect(result.current.agreed).toBe(false);

    rerender({ open: true });
    expect(result.current.agreed).toBe(false);
  });

  it("stays agreed while the surface stays open, so scrolling back up does not revoke it", () => {
    const { result, rerender } = renderHook(({ open }) => useTermsAgreement(open), {
      initialProps: { open: true },
    });
    act(() => result.current.onReachedEnd());
    rerender({ open: true });
    expect(result.current.agreed).toBe(true);
  });
});

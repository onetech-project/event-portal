import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { IconPicker } from "@/components/ui/icon-picker";

// happy-dom has no ResizeObserver and no real layout, which the virtualized
// grid needs (spec 009 R7); the stub plus generous rect sizes let the
// virtualizer produce rows.
beforeEach(() => {
  // The virtualizer only learns its viewport from ResizeObserver callbacks,
  // so the stub must fire once per observe() like a real browser does.
  vi.stubGlobal(
    "ResizeObserver",
    class {
      cb: ResizeObserverCallback;
      constructor(cb: ResizeObserverCallback) {
        this.cb = cb;
      }
      observe(el: Element) {
        this.cb(
          [{ target: el, contentRect: el.getBoundingClientRect() } as ResizeObserverEntry],
          this as unknown as ResizeObserver,
        );
      }
      unobserve() {}
      disconnect() {}
    },
  );
  // TanStack Virtual sizes its viewport from offsetWidth/offsetHeight, which
  // happy-dom always reports as 0 — without these the virtualizer computes an
  // empty visible range and renders no rows at all.
  Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get: () => 400,
  });
  Object.defineProperty(HTMLElement.prototype, "offsetWidth", {
    configurable: true,
    get: () => 300,
  });
});

describe("IconPicker (vendored, adapted to base-ui)", () => {
  it("opens, searches, and fires onValueChange for a match", async () => {
    const onValueChange = vi.fn();
    const user = userEvent.setup();
    render(<IconPicker onValueChange={onValueChange} />);

    await user.click(screen.getByRole("button", { name: "Select an icon" }));
    await user.type(await screen.findByLabelText("Search icons"), "anchor");

    // 100 ms debounce + fuzzy search; the option button carries the icon name.
    const option = await screen.findByRole("button", { name: "anchor" });
    await user.click(option);

    expect(onValueChange).toHaveBeenCalledWith("anchor");
  });

  it("shows the empty state for a term matching nothing", async () => {
    const user = userEvent.setup();
    render(<IconPicker />);

    await user.click(screen.getByRole("button", { name: "Select an icon" }));
    await user.type(await screen.findByLabelText("Search icons"), "zzzzqqqq");

    await waitFor(() => expect(screen.getByText("No icons match")).toBeInTheDocument());
  });
});

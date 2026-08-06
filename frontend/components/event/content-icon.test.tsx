import { render, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ContentIcon } from "@/components/event/content-icon";

describe("ContentIcon", () => {
  it("renders a known catalog key (including non-legacy ones) as an svg", async () => {
    const { container } = render(<ContentIcon name="anchor" className="size-4" />);
    // DynamicIcon resolves the per-icon chunk asynchronously.
    await waitFor(() => expect(container.querySelector("svg")).toBeInTheDocument());
  });

  it("falls back to the Info glyph, statically, for a stale key", () => {
    const { container } = render(<ContentIcon name="definitely-not-real" />);
    expect(container.querySelector("svg.lucide-info")).toBeInTheDocument();
  });

  it("renders nothing for null or empty names", () => {
    const { container: withNull } = render(<ContentIcon name={null} />);
    expect(withNull).toBeEmptyDOMElement();

    const { container: withEmpty } = render(<ContentIcon name="" />);
    expect(withEmpty).toBeEmptyDOMElement();
  });
});

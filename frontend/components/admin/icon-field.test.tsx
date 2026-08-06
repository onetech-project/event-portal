import { useState } from "react";

import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { IconField } from "@/components/admin/icon-field";

// The real picker is virtualized and lazy-loaded (spec 009 R7); IconField's
// own contract — activation, value display, clear — is tested against a stub.
vi.mock("@/components/ui/icon-picker", () => ({
  IconPicker: ({
    onValueChange,
    children,
  }: {
    onValueChange?: (icon: string) => void;
    children?: React.ReactNode;
  }) => (
    <div>
      {children}
      <button type="button" onClick={() => onValueChange?.("anchor")}>
        pick-anchor
      </button>
    </div>
  ),
}));

function Harness({ initial = "" }: { initial?: string }) {
  const [icon, setIcon] = useState(initial);
  return <IconField value={icon} onChange={setIcon} label="Icon" />;
}

describe("IconField", () => {
  it("shows 'No icon' and no clear button when empty", () => {
    render(<Harness />);
    expect(screen.getByRole("button", { name: "Icon: none" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove icon" })).not.toBeInTheDocument();
  });

  it("mounts the picker on first click and applies the selection", async () => {
    const user = userEvent.setup();
    render(<Harness />);

    // Nothing picker-related is mounted before activation (lazy-load contract).
    expect(screen.queryByText("pick-anchor")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Icon: none" }));
    await user.click(await screen.findByText("pick-anchor"));

    expect(screen.getByText("anchor")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Remove icon" })).toBeInTheDocument();
  });

  it("clears back to no icon via the Remove icon button", async () => {
    const user = userEvent.setup();
    render(<Harness initial="music" />);

    expect(screen.getByText("music")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Remove icon" }));

    expect(screen.getByRole("button", { name: "Icon: none" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Remove icon" })).not.toBeInTheDocument();
  });
});

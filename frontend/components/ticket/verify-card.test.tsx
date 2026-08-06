import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { VerifyCard } from "./verify-card";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
}));

beforeEach(() => {
  push.mockClear();
});

describe("VerifyCard", () => {
  it("routes to the lookup page with the normalized code", async () => {
    render(<VerifyCard />);

    await userEvent.type(screen.getByLabelText(/ticket code/i), "  abc234defg ");
    await userEvent.click(screen.getByRole("button", { name: /check ticket/i }));

    expect(push).toHaveBeenCalledWith("/tickets/ABC234DEFG");
  });

  it("does nothing while the code box is empty", () => {
    render(<VerifyCard />);

    expect(screen.getByRole("button", { name: /check ticket/i })).toBeDisabled();
    expect(push).not.toHaveBeenCalled();
  });
});

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ValidationResultCard } from "./validation-result-card";
import type { ValidationResult } from "@/lib/types";

const valid: ValidationResult = {
  result: "VALID",
  ticket_code: "ABC234DEFG",
  attendee_name: "Budi Santoso",
  ticket_type_name: "Regular",
  event_name: "Jazz Night 2026",
};

const alreadyUsed: ValidationResult = { ...valid, result: "ALREADY_USED" };

const invalid: ValidationResult = {
  result: "INVALID",
  ticket_code: "GHOST234AB",
  attendee_name: null,
  ticket_type_name: null,
  event_name: null,
};

describe("ValidationResultCard", () => {
  it("shows the attendee, ticket type, and event for a valid ticket", () => {
    render(<ValidationResultCard result={valid} onMarkUsed={vi.fn()} isMarking={false} />);

    expect(screen.getByText(/valid/i)).toBeInTheDocument();
    expect(screen.getByText("Budi Santoso")).toBeInTheDocument();
    expect(screen.getByText("Regular")).toBeInTheDocument();
    expect(screen.getByText("Jazz Night 2026")).toBeInTheDocument();
    expect(screen.getByText("ABC234DEFG")).toBeInTheDocument();
  });

  // Marking a ticket used is irreversible, so the action only appears for a
  // ticket that can actually be admitted.
  it("offers Mark Used only for a valid ticket", () => {
    const { rerender } = render(
      <ValidationResultCard result={valid} onMarkUsed={vi.fn()} isMarking={false} />,
    );
    expect(screen.getByRole("button", { name: /mark used/i })).toBeInTheDocument();

    rerender(
      <ValidationResultCard result={alreadyUsed} onMarkUsed={vi.fn()} isMarking={false} />,
    );
    expect(screen.queryByRole("button", { name: /mark used/i })).not.toBeInTheDocument();

    rerender(
      <ValidationResultCard result={invalid} onMarkUsed={vi.fn()} isMarking={false} />,
    );
    expect(screen.queryByRole("button", { name: /mark used/i })).not.toBeInTheDocument();
  });

  it("calls onMarkUsed with the ticket code", async () => {
    const onMarkUsed = vi.fn();
    const { default: userEvent } = await import("@testing-library/user-event");
    const user = userEvent.setup();

    render(<ValidationResultCard result={valid} onMarkUsed={onMarkUsed} isMarking={false} />);
    await user.click(screen.getByRole("button", { name: /mark used/i }));

    expect(onMarkUsed).toHaveBeenCalledWith("ABC234DEFG");
  });

  it("disables the action while the request is in flight", () => {
    render(<ValidationResultCard result={valid} onMarkUsed={vi.fn()} isMarking />);

    expect(screen.getByRole("button", { name: /admitting/i })).toBeDisabled();
  });

  it("shows Already Used with the attendee still visible", () => {
    render(
      <ValidationResultCard result={alreadyUsed} onMarkUsed={vi.fn()} isMarking={false} />,
    );

    expect(screen.getByText(/already used/i)).toBeInTheDocument();
    expect(screen.getByText("Budi Santoso")).toBeInTheDocument();
  });

  // An unknown code must disclose nothing beyond the verdict.
  it("shows only the verdict for an invalid ticket", () => {
    render(<ValidationResultCard result={invalid} onMarkUsed={vi.fn()} isMarking={false} />);

    expect(screen.getByText(/invalid/i)).toBeInTheDocument();
    expect(screen.queryByText(/attendee/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/event/i)).not.toBeInTheDocument();
  });
});

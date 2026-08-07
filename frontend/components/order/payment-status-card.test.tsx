import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PaymentStatusCard } from "./payment-status-card";

/**
 * Spec 011 FR-012: everything goes to the buyer — the first holder form's
 * address — so the PAID copy points there. The card never shows the address
 * itself; the guest order read does not expose it.
 */
describe("PaymentStatusCard", () => {
  it("points the PAID copy at the first ticket holder form's email", () => {
    render(<PaymentStatusCard status="PAID" eventSlug="jazz-night-2026" />);

    expect(screen.getByText(/payment successful/i)).toBeInTheDocument();
    expect(
      screen.getByText(/email address on the\s+first ticket holder form/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/one qr code per attendee/i)).toBeInTheDocument();
    // The address itself is never rendered — there is none to show.
    expect(screen.queryByText(/@/)).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: /back to the event/i })).toHaveAttribute(
      "href",
      "/events/jazz-night-2026",
    );
  });

  it("words an expired order as released, not as a success", () => {
    render(<PaymentStatusCard status="EXPIRED" eventSlug="jazz-night-2026" />);

    expect(screen.getByText(/expired before it was paid/i)).toBeInTheDocument();
    expect(screen.queryByText(/payment successful/i)).not.toBeInTheDocument();
  });

  it("words a cancelled order as never charged", () => {
    render(<PaymentStatusCard status="CANCELLED" eventSlug="jazz-night-2026" />);

    expect(screen.getByText(/nothing was charged/i)).toBeInTheDocument();
  });
});

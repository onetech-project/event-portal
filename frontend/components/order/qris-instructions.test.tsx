import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import { QrisInstructions } from "./qris-instructions";

/**
 * Spec 019 FR-010 / FR-011: the verification step checks the amount, and only
 * the amount.
 *
 * The merchant name it used to check went with the frame that printed it, by
 * explicit decision. That leaves the amount carrying the whole weight of the
 * one comparison a guest can actually perform against a swapped code, which is
 * why the assertions below insist on the *exact* figure rather than merely on
 * the word "amount": "check the amount is right" gives a guest nothing to
 * compare against.
 *
 * A merchant name is configured here on purpose. Asserting its absence against
 * an unset variable would prove only that it was unset.
 */

const MERCHANT = "UNIT-MERCHANT-MUST-NOT-RENDER";

beforeEach(() => {
  process.env.QRIS_MERCHANT_NAME = MERCHANT;
});

afterEach(() => {
  delete process.env.QRIS_MERCHANT_NAME;
  delete process.env.NEXT_PUBLIC_QRIS_MERCHANT_NAME;
});

async function openInstructions(amount: string): Promise<void> {
  render(<QrisInstructions amount={amount} />);
  await userEvent.click(screen.getByRole("button", { name: /how to pay with qris/i }));
}

describe("QrisInstructions", () => {
  it("has the guest verify the exact amount before entering a PIN", async () => {
    await openInstructions("250000.00");

    expect(screen.getByText("Rp 250.000")).toBeInTheDocument();
    expect(screen.getByText(/do not enter your PIN/i)).toBeInTheDocument();
  });

  it("names no merchant, and does not send the guest to a label that is gone", async () => {
    await openInstructions("250000.00");

    const text = document.body.textContent ?? "";
    expect(text, "instructions still name a configured merchant").not.toContain(
      MERCHANT,
    );
    // The old fallback pointed at frame text that no longer exists — the
    // sharpest version of the problem this change fixes.
    expect(text).not.toMatch(/merchant name printed/i);
    expect(text).not.toMatch(/merchant name/i);
  });

  it("still walks through scanning, so the amount check is not the only step", async () => {
    await openInstructions("250000.00");

    expect(screen.getByText(/open your m-banking or e-wallet app/i)).toBeInTheDocument();
    expect(screen.getByText(/point the camera at the QR code/i)).toBeInTheDocument();
    expect(screen.getByText(/save your payment receipt/i)).toBeInTheDocument();
  });
});

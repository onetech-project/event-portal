import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";

import type { PaymentInstruction } from "@/lib/types";

import { QrisPanel } from "./qris-panel";

/**
 * Spec 019: the Scan to Pay frame carries what the design draws, and none of
 * the five fields it used to print.
 *
 * The sentinels below are the point of this file. Asserting the fields are
 * absent while nothing is configured would prove only that nothing was
 * configured — the component always rendered `null` for an empty value, so such
 * a test passes just as readily against the pre-019 component. Setting the
 * retired variables first turns it into a real claim: the frame does not print
 * them *even when an environment supplies them*, because nothing reads them any
 * more. That is also FR-013, checked here at the cheapest tier.
 *
 * The ended states live here rather than in `e2e/` for the reason research D-7
 * gives: reaching an expired and a cancelled order through the real API is slow,
 * and this panel takes the state as a prop.
 */

const RETIRED = {
  QRIS_MERCHANT_NAME: "UNIT-MERCHANT-MUST-NOT-RENDER",
  QRIS_MERCHANT_ID: "UNIT-NMID-MUST-NOT-RENDER",
  QRIS_TERMINAL_LABEL: "UNIT-TERMINAL-MUST-NOT-RENDER",
  QRIS_ACQUIRER_CODE: "UNIT-ACQUIRER-MUST-NOT-RENDER",
  QRIS_PRINT_VERSION: "UNIT-VERSION-MUST-NOT-RENDER",
} as const;

const payment: PaymentInstruction = {
  method: "qris",
  provider: "manjo",
  amount: "250000.00",
  expires_at: "2026-08-18T12:00:00Z",
  qr_image_path: "/ticket/order/ORD-1/qris.png",
};

beforeEach(() => {
  for (const [name, value] of Object.entries(RETIRED)) {
    process.env[name] = value;
  }
});

afterEach(() => {
  for (const name of Object.keys(RETIRED)) {
    delete process.env[name];
    delete process.env[`NEXT_PUBLIC_${name}`];
  }
});

/** Every retired field, plus the captions that used to introduce them. */
function expectNoRetiredFields(): void {
  const frame = screen.getByText(/satu qris untuk semua/i).closest("div");
  const text = document.body.textContent ?? "";

  expect(frame).not.toBeNull();
  for (const value of Object.values(RETIRED)) {
    expect(text, `frame still renders ${value}`).not.toContain(value);
  }
  for (const label of ["NMID", "Dicetak oleh", "Versi Cetak"]) {
    expect(text, `frame still renders the "${label}" caption`).not.toContain(label);
  }
}

describe("QrisPanel", () => {
  it("shows the live code and the design's own text, and none of the retired fields", () => {
    render(<QrisPanel payment={payment} totalAmount="250000.00" />);

    expectNoRetiredFields();

    expect(screen.getByRole("heading", { name: "Scan to Pay" })).toBeInTheDocument();
    expect(
      screen.getByText(/use any e-wallet or mobile banking app supporting qris/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/satu qris untuk semua/i)).toBeInTheDocument();
    expect(screen.getByText(/www\.aspi-qris\.id/i)).toBeInTheDocument();
    expect(screen.getByText(/total amount due/i)).toBeInTheDocument();
    expect(screen.getByText("Rp 250.000")).toBeInTheDocument();

    // The code is the API's on-demand render of this order's stored payload.
    expect(screen.getByRole("img", { name: /qris code for/i })).toHaveAttribute(
      "src",
      "http://api.test/ticket/order/ORD-1/qris.png",
    );
  });

  it("keeps the frame but drops the code once the payment window has run out", () => {
    render(
      <QrisPanel payment={null} totalAmount="250000.00" endedStatus="EXPIRED" />,
    );

    expectNoRetiredFields();

    // The frame survives — an ended order must not leave a void beside the
    // end-of-journey dialog — but nothing scannable is left in it.
    expect(screen.getByRole("heading", { name: "Scan to Pay" })).toBeInTheDocument();
    expect(screen.getByText(/satu qris untuk semua/i)).toBeInTheDocument();
    expect(screen.queryByRole("img", { name: /qris code for/i })).not.toBeInTheDocument();

    expect(screen.getByText(/this order can no longer be paid/i)).toBeInTheDocument();
    expect(screen.getByText(/the payment time ran out/i)).toBeInTheDocument();
    expect(screen.getByText(/order total/i)).toBeInTheDocument();
    expect(screen.queryByText(/total amount due/i)).not.toBeInTheDocument();
  });

  it("says a cancelled order was cancelled, not that it timed out", () => {
    render(
      <QrisPanel payment={null} totalAmount="250000.00" endedStatus="CANCELLED" />,
    );

    expectNoRetiredFields();

    expect(screen.getByText(/this order was cancelled/i)).toBeInTheDocument();
    expect(screen.queryByText(/the payment time ran out/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("img", { name: /qris code for/i })).not.toBeInTheDocument();
  });
});

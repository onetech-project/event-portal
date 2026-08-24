import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { TermsDialogShell } from "./terms-dialog-shell";

/**
 * The MANUAL route to agreement (spec 022 FR-014c/FR-045, clarified 2026-08-21).
 *
 * This file exists because the manual route needs no layout at all, and so is
 * the one half of this feature that belongs at the unit tier. The other half —
 * reaching the end of a long document — cannot be tested here and is not
 * attempted: happy-dom reports every element as zero-height and registers inert
 * observers, so the end always reads as already reached. That half is proved in
 * e2e/specs/guest-purchase.spec.ts against a real browser.
 *
 * `agreed` is driven as a PROP here rather than through TermsViewer, precisely
 * so happy-dom's phantom auto-tick cannot make these assertions pass for the
 * wrong reason.
 */

const TERMS = {
  id: "22222222-2222-2222-2222-222222222222",
  content: "<ol><li>All ticket sales are final.</li></ol>",
  updated_at: "2026-08-01T00:00:00Z",
};

function envelope(data: unknown) {
  return new Response(JSON.stringify({ code: 200000, message: "Success", data }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return Wrapper;
}

/** Holds `agreed` the way both real callers do, via useTermsAgreement. */
function Harness({
  onAction = () => {},
  ready,
}: {
  onAction?: () => void;
  ready?: boolean;
}) {
  const [agreed, setAgreed] = useState(false);
  return (
    <TermsDialogShell
      open
      onOpenChange={() => {}}
      eventSlug="jive-2026"
      eventName="Jive Indonesia 2026"
      agreed={agreed}
      onAgreedChange={setAgreed}
      onReachedEnd={() => {}}
      action={{ label: "Agree", onClick: onAction, ready }}
    />
  );
}

/**
 * `Agree` is NOT a disabled button while unavailable — it is swapped for an
 * aria-disabled <span>. So `getByRole("button", …)` throws rather than returning
 * a disabled node, and `toBeDisabled()` would fail as an error instead of an
 * assertion. Query for absence of the role.
 */
function agreeButton() {
  return screen.queryByRole("button", { name: /^agree$/i });
}

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(() => Promise.resolve(envelope(TERMS))),
  );
});

describe("TermsDialogShell", () => {
  it("starts unticked, with Agree unavailable", async () => {
    render(<Harness />, { wrapper: wrapper() });
    await screen.findByText(/all ticket sales are final/i);

    expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe("false");
    expect(agreeButton()).toBeNull();
  });

  // The heart of the 2026-08-21 clarification: the checkbox is a control again.
  it("lets the guest tick the box by hand, which turns Agree up", async () => {
    render(<Harness />, { wrapper: wrapper() });
    await screen.findByText(/all ticket sales are final/i);

    await userEvent.click(screen.getByRole("checkbox"));

    expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe("true");
    expect(agreeButton()).not.toBeNull();
  });

  it("takes Agree away again when the guest unticks", async () => {
    render(<Harness />, { wrapper: wrapper() });
    await screen.findByText(/all ticket sales are final/i);

    await userEvent.click(screen.getByRole("checkbox"));
    await userEvent.click(screen.getByRole("checkbox"));

    expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe("false");
    expect(agreeButton()).toBeNull();
  });

  it("runs the caller's handler when Agree is pressed", async () => {
    const onAction = vi.fn();
    render(<Harness onAction={onAction} />, { wrapper: wrapper() });
    await screen.findByText(/all ticket sales are final/i);

    await userEvent.click(screen.getByRole("checkbox"));
    await userEvent.click(screen.getByRole("button", { name: /^agree$/i }));

    expect(onAction).toHaveBeenCalledTimes(1);
  });

  /**
   * US5 scenario 3: nothing tells the guest they must read first.
   *
   * The label carried "(please read the document)" while the read-through gate
   * existed. With the gate withdrawn that sentence is not merely redundant, it
   * is false — the guest is under no such obligation.
   */
  it("does not tell the guest to read the document first", async () => {
    render(<Harness />, { wrapper: wrapper() });
    await screen.findByText(/all ticket sales are final/i);

    expect(screen.queryByText(/please read/i)).toBeNull();
    expect(screen.getByText(/i agree to terms & conditions/i)).toBeTruthy();
  });

  /**
   * `ready` is about the DOCUMENT, not the guest, and survived the loosening
   * untouched: agreeing to terms that failed to load would record consent to
   * nothing. Ticking the box must not be enough on its own.
   */
  it("still withholds Agree when the document is not ready, even once ticked", async () => {
    render(<Harness ready={false} />, { wrapper: wrapper() });
    await screen.findByText(/all ticket sales are final/i);

    await userEvent.click(screen.getByRole("checkbox"));

    expect(screen.getByRole("checkbox").getAttribute("aria-checked")).toBe("true");
    expect(agreeButton()).toBeNull();
  });

  // FR-044: the dialog must look exactly as it did. `disabled` on a base-ui
  // checkbox would dim it (disabled:opacity-50) and drop it from the tab order.
  it("never renders the checkbox disabled", async () => {
    render(<Harness />, { wrapper: wrapper() });
    await screen.findByText(/all ticket sales are final/i);

    const box = screen.getByRole("checkbox");
    expect(box.hasAttribute("disabled")).toBe(false);
    expect(box.getAttribute("aria-disabled")).toBeNull();
  });
});

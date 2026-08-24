import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { TermsDialog } from "./terms-dialog";
import { API_CODES } from "@/lib/api-client";
import { GENERAL_REFUSAL, THROTTLED, type RefusalMessage } from "@/lib/availability";
import type { SelectionLine } from "@/lib/types";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
}));

const EVENT_ID = "11111111-1111-1111-1111-111111111111";
const TERMS_ID = "22222222-2222-2222-2222-222222222222";
const TICKET_ID = "33333333-3333-3333-3333-333333333333";
const ORDER_ID = "ORD-20260805-A1B2C3D4";

const LINES: SelectionLine[] = [
  { kind: "ticket", id: TICKET_ID, name: "Day 1", unitPrice: "35000.00", quantity: 2 },
];

const TERMS_UPDATED_AT = "2026-08-01T00:00:00Z";

const TERMS = {
  id: TERMS_ID,
  content: "<ol><li>All ticket sales are final.</li></ol>",
  updated_at: TERMS_UPDATED_AT,
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** Success bodies arrive wrapped in the API envelope (spec 008). */
function envelope(data: unknown, status = 200) {
  return jsonResponse({ code: 200000, message: "Success", data }, status);
}

/**
 * The dialog's three calls, routed by URL. Overrides let a test fail one leg.
 */
function stubApi(overrides: { book?: Response; agreement?: Response } = {}) {
  const spy = vi.fn().mockImplementation((rawUrl: string | URL) => {
    const url = String(rawUrl);
    if (url.includes("/ticket/book")) {
      return Promise.resolve(
        overrides.book ??
          envelope(
            {
              order_id: ORDER_ID,
              status: "PENDING",
              total_amount: "70000.00",
              expires_at: "2026-08-05T11:00:00Z",
            },
            201,
          ),
      );
    }
    if (url.includes(`/ticket/terms-condition/${ORDER_ID}`)) {
      return Promise.resolve(overrides.agreement ?? envelope(null));
    }
    // GET /ticket/terms-condition/:event_slug
    return Promise.resolve(envelope(TERMS));
  });
  vi.stubGlobal("fetch", spy);
  return spy;
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

/**
 * The dialog is controlled since spec 013 — it no longer owns a Buy Ticket
 * trigger, because the press now runs a server availability check first and
 * only a clean answer opens the gate.
 *
 * This harness stands in for that parent with the smallest thing that still
 * makes these tests mean what they meant: a button that opens it. The check
 * itself is SelectionSummary's job and is covered there; what is under test
 * here is everything the dialog does once open.
 */
function Harness({
  onAvailabilityRefusal = () => {},
}: {
  onAvailabilityRefusal?: (message: RefusalMessage) => void;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <button type="button" onClick={() => setOpen(true)}>
        Buy Ticket
      </button>
      <TermsDialog
        eventId={EVENT_ID}
        eventSlug="jive-2026"
        eventName="Jive Indonesia 2026"
        lines={LINES}
        open={open}
        onOpenChange={setOpen}
        onAvailabilityRefusal={onAvailabilityRefusal}
      />
    </>
  );
}

function setup(props: { onAvailabilityRefusal?: (message: RefusalMessage) => void } = {}) {
  return render(<Harness {...props} />, { wrapper: wrapper() });
}

/** An error envelope as the API emits it: {code, message, data}, never a string code. */
function apiError(status: number, code: number, message: string) {
  return jsonResponse({ code, message, data: null }, status);
}

/**
 * Opens the dialog and presses Agree.
 *
 * It does not tick the box, and does not need to: under happy-dom every element
 * reports zero height, so the end of the document counts as reached the moment
 * the terms render (FR-014d) and the automatic tick has already fired. That is a
 * happy-dom artefact, not a behaviour — see the note below.
 */
async function agree(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /buy ticket/i }));
  await screen.findByText(/all ticket sales are final/i);
  await user.click(screen.getByRole("button", { name: /^agree$/i }));
}

beforeEach(() => {
  vi.restoreAllMocks();
  push.mockClear();
});

describe("TermsDialog", () => {
  it("keeps the terms closed until Buy Ticket is pressed, then shows the CMS document", async () => {
    stubApi();
    setup();

    expect(screen.queryByText(/i agree to terms/i)).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));

    expect(screen.getByText(/i agree to terms/i)).toBeTruthy();
    // The event is named as the subject of the terms, not just the page title.
    expect(screen.getByText("Jive Indonesia 2026")).toBeTruthy();
    // The clause comes from the fetched document, not a static copy.
    expect(await screen.findByText(/all ticket sales are final/i)).toBeTruthy();
  });

  // WHAT THIS TIER CANNOT TEST, stated so nobody adds a test that only appears
  // to cover it. Under happy-dom scrollHeight and clientHeight are getter-only
  // zeroes and IntersectionObserver is an inert stub, so the end of the document
  // reads as reached the instant the terms render: `0 <= 0` means "this document
  // does not scroll, so it has been read" (FR-014d). A test asserting "Agree is
  // withheld before scrolling" would be asserting a happy-dom artefact. The
  // automatic tick is proved in e2e/specs/guest-purchase.spec.ts against a real
  // browser and a deliberately LONG document.
  //
  // What IS testable here is the manual route, which needs no layout: the
  // checkbox is a CONTROL (FR-045, clarified 2026-08-21). It briefly was not —
  // this test previously asserted that clicking it did nothing, which is now the
  // opposite of the requirement. The shell's own coverage is in
  // components/terms/terms-dialog-shell.test.tsx; this asserts the wiring
  // reaches it through the real dialog.
  it("lets the guest untick and re-tick the agreement box by hand", async () => {
    stubApi();
    setup();
    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);

    // Already ticked, by the phantom auto-tick described above.
    const box = screen.getByRole("checkbox");
    expect(box.getAttribute("aria-checked")).toBe("true");

    // Withdrawing is the guest's, and takes Agree away with it.
    await userEvent.click(box);
    expect(box.getAttribute("aria-checked")).toBe("false");
    expect(screen.queryByRole("button", { name: /^agree$/i })).toBeNull();

    // And they can put it back, without touching the document.
    await userEvent.click(box);
    expect(box.getAttribute("aria-checked")).toBe("true");
    expect(screen.queryByRole("button", { name: /^agree$/i })).not.toBeNull();
  });

  it("books, records the agreement, and routes to the order page on Agree", async () => {
    const fetchSpy = stubApi();
    setup();
    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);

    await userEvent.click(screen.getByRole("button", { name: /^agree$/i }));

    await waitFor(() =>
      expect(push).toHaveBeenCalledWith(`/events/jive-2026/orders/${ORDER_ID}`),
    );

    // Routing is asynchronous and the dialog outlives it, so a guest whose
    // booking worked must never be shown the failure affordance while the order
    // page loads — nothing failed and there is nothing to retry.
    expect(screen.queryByRole("button", { name: /retry/i })).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.getByRole("button", { name: /opening your order/i })).toBeTruthy();

    // Call order: book first, then the agreement carrying BOTH identifiers of
    // the document actually shown — the id (which document) and updated_at
    // (which version). The version is the load-bearing one: an admin edit
    // preserves the id, so the id alone could never report a mid-flow change
    // (spec 022).
    const calls = fetchSpy.mock.calls.map((args) => String(args[0]));
    const bookIndex = calls.findIndex((u) => u.includes("/ticket/book"));
    const agreeIndex = calls.findIndex((u) => u.includes(`/ticket/terms-condition/${ORDER_ID}`));
    expect(bookIndex).toBeGreaterThanOrEqual(0);
    expect(agreeIndex).toBeGreaterThan(bookIndex);
    const agreementBody = JSON.parse(String(fetchSpy.mock.calls[agreeIndex][1]?.body));
    expect(agreementBody).toEqual({
      agreed: true,
      event_terms_id: TERMS_ID,
      event_terms_updated_at: TERMS_UPDATED_AT,
    });
  });

  it("retries only the agreement after it fails, never booking twice", async () => {
    const fetchSpy = stubApi({
      agreement: jsonResponse({ code: 500000, message: "boom", data: null }, 500),
    });
    setup();
    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);

    await userEvent.click(screen.getByRole("button", { name: /^agree$/i }));

    // The failure surfaces and the button becomes a retry.
    expect(await screen.findByRole("alert")).toBeTruthy();
    const retry = await screen.findByRole("button", { name: /retry/i });
    expect(push).not.toHaveBeenCalled();

    // Second attempt succeeds — and must not open a second hold.
    stubApiSecondAttempt(fetchSpy);
    await userEvent.click(retry);

    await waitFor(() =>
      expect(push).toHaveBeenCalledWith(`/events/jive-2026/orders/${ORDER_ID}`),
    );
    const bookCalls = fetchSpy.mock.calls.filter((args) =>
      String(args[0]).includes("/ticket/book"),
    );
    expect(bookCalls).toHaveLength(1);
  });

  // The reset itself — reopening starts ungated — is proved against the hook in
  // components/terms/terms-viewer.test.tsx, where it is real logic rather than a
  // layout measurement. Here we prove the dialog re-evaluates on reopen rather
  // than carrying a booked order or an error across.
  it("starts a fresh agreement each time it is opened", async () => {
    stubApi();
    setup();
    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);
    await userEvent.click(screen.getByRole("button", { name: /cancel/i }));

    await userEvent.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);

    expect(screen.queryByRole("button", { name: /retry/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

/** Rewires the shared spy so the agreement leg now succeeds. */
function stubApiSecondAttempt(spy: ReturnType<typeof vi.fn>) {
  spy.mockImplementation((rawUrl: string | URL) => {
    const url = String(rawUrl);
    if (url.includes(`/ticket/terms-condition/${ORDER_ID}`)) {
      return Promise.resolve(envelope(null));
    }
    return Promise.resolve(envelope(TERMS));
  });
}

/**
 * Spec 013 FR-013a, added by the 2026-08-19 amendment.
 *
 * Until then this file never failed the `book` leg at all — the only failure it
 * injected was the agreement call — so nothing anywhere observed what a guest
 * sees when booking refuses at Agree.
 */
describe("TermsDialog — booking refuses at Agree", () => {
  const AVAILABILITY: Array<[string, number, string]> = [
    ["a quota shortfall", API_CODES.insufficientQuota, "Only fewer than 2 ticket(s) remain."],
    ["a closed sale window", API_CODES.validation, 'Ticket type "Day 1" is not currently on sale.'],
    ["a ticket type that is gone", API_CODES.notFound, "Ticket type 33333 does not exist."],
  ];

  it.each(AVAILABILITY)(
    "closes and reports the general message upward for %s",
    async (_label, code, serverSentence) => {
      stubApi({ book: apiError(400, code, serverSentence) });
      const onAvailabilityRefusal = vi.fn();
      const user = userEvent.setup();
      setup({ onAvailabilityRefusal });

      await agree(user);

      await waitFor(() => expect(onAvailabilityRefusal).toHaveBeenCalledWith(GENERAL_REFUSAL));
      // FR-013a: out of the way, so the message lands on a page the guest can act on.
      await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
      // FR-013: the server's sentence is a record, not copy.
      expect(document.body.textContent).not.toContain(serverSentence);
    },
  );

  it("leaves no ticked box, no Retry and no stale sentence for the next opening", async () => {
    // research.md D9: the reset only runs if the dialog closes through its OWN
    // handleOpenChange. A parent flipping `open` would skip it and leave all
    // three behind.
    stubApi({ book: apiError(400, API_CODES.insufficientQuota, "Only fewer than 2 ticket(s) remain.") });
    const user = userEvent.setup();
    setup();

    await agree(user);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    stubApi();
    await user.click(screen.getByRole("button", { name: /buy ticket/i }));
    await screen.findByText(/all ticket sales are final/i);

    // The ticked-box half of this assertion moved to the hook test: under
    // happy-dom the gate re-satisfies on reopen, so the box legitimately reads
    // checked here and asserting otherwise would be asserting the artefact.
    expect(screen.queryByRole("button", { name: /retry/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  // The server sentence matters here: these codes carry no inventory figure, so
  // the dialog is free to show the server's own words, and does for anything it
  // has nothing better to say about.
  const NON_AVAILABILITY: Array<[string, number, number, string, RegExp]> = [
    [
      "absent terms",
      409,
      API_CODES.termsMissing,
      "This event has no Terms & Conditions to agree to yet.",
      /terms & conditions/i,
    ],
    ["a throttled booking", 429, API_CODES.rateLimited, "rate limit exceeded", /too many attempts/i],
    ["an internal fault", 500, API_CODES.internal, "", /went wrong/i],
  ];

  it.each(NON_AVAILABILITY)(
    "keeps its own in-dialog alert for %s",
    async (_label, status, code, serverSentence, expected) => {
      stubApi({ book: apiError(status, code, serverSentence) });
      const onAvailabilityRefusal = vi.fn();
      const user = userEvent.setup();
      setup({ onAvailabilityRefusal });

      await agree(user);

      // FR-013a's converse: these are not an availability race, so the dialog
      // stays and words them where it always did.
      await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(expected));
      expect(screen.getByRole("dialog")).toBeInTheDocument();
      expect(onAvailabilityRefusal).not.toHaveBeenCalled();
    },
  );

  it("words a throttled booking with the sentence the check also uses", async () => {
    // One condition, one story at both points (FR-012a).
    stubApi({ book: apiError(429, API_CODES.rateLimited, "rate limit exceeded") });
    const user = userEvent.setup();
    setup();

    await agree(user);

    await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent(THROTTLED.body));
    expect(screen.getByRole("alert")).not.toHaveTextContent(/rate limit exceeded/i);
  });
});

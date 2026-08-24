import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { RegistrationView } from "./page";

const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
}));

const SLUG = "jazz-night-2026";
const TICKET_ID = "33333333-3333-3333-3333-333333333333";
const TERMS_UPDATED_AT = "2026-08-01T00:00:00Z";

const PREREQS = {
  event: { name: "Jazz Night 2026", slug: SLUG },
  ticket_type_name: "Invitation Access",
  event_terms_updated_at: TERMS_UPDATED_AT,
  genders: [
    { id: 2, name: "MALE" },
    { id: 1, name: "FEMALE" },
  ],
};

const TERMS = {
  id: "22222222-2222-2222-2222-222222222222",
  content: "<ol><li>Wristbands are non-transferable.</li></ol>",
  updated_at: TERMS_UPDATED_AT,
};

function envelope(data: unknown, status = 200) {
  return new Response(JSON.stringify({ code: 200000, message: "Success", data }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function failure(code: number, message: string, data: unknown = null, status = 400) {
  return new Response(JSON.stringify({ code, message, data }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** The view's two GETs and its POST, routed by URL. */
function stubApi(overrides: { submit?: Response } = {}) {
  const spy = vi.fn().mockImplementation((rawUrl: string | URL, init?: RequestInit) => {
    const url = String(rawUrl);
    if (url.includes("/ticket/terms-condition/")) {
      return Promise.resolve(envelope(TERMS));
    }
    if (url.includes("/ticket/register/") && init?.method === "POST") {
      return Promise.resolve(overrides.submit ?? envelope({ registered: true }, 201));
    }
    if (url.includes("/ticket/register/")) {
      return Promise.resolve(envelope(PREREQS));
    }
    throw new Error(`unstubbed fetch: ${url}`);
  });
  vi.stubGlobal("fetch", spy);
  return spy;
}

function renderView(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

async function fillValidForm(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText(/full name/i), "Halo Registrant");
  await user.type(screen.getByLabelText(/email address/i), "halo@example.com");
  await user.type(screen.getByLabelText(/phone number/i), "628125567820");
  await user.type(screen.getByLabelText(/date of birth/i), "12/04/1996");

  // Gender is the design-system Select: choosing means opening it and clicking
  // an option. The exact label matters — /male/i also matches "Female".
  await user.click(screen.getAllByRole("combobox")[0]);
  await user.click(await screen.findByRole("option", { name: "Male" }));
}

describe("RegistrationView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  // Anti-drift (clarified 2026-08-20). The holder fields are SHARED with the
  // booking flow; asserting the shared placeholder here means re-inlining a
  // field on this page fails rather than quietly diverging from the other
  // surface, which is what happened before they were extracted.
  it("renders the shared holder fields, not a copy of them", async () => {
    stubApi();
    renderView(<RegistrationView slug={SLUG} ticketId={TICKET_ID} />);

    await screen.findByRole("form", { name: /invitation registration/i });

    expect(screen.getByLabelText(/phone number/i)).toHaveAttribute(
      "placeholder",
      "081234567890",
    );
    expect(screen.getByLabelText(/full name/i)).toHaveAttribute(
      "placeholder",
      "As written on ID card",
    );
  });

  it("shows no price, payment step or receipt anywhere (FR-015)", async () => {
    stubApi();
    renderView(<RegistrationView slug={SLUG} ticketId={TICKET_ID} />);

    await screen.findByRole("form", { name: /invitation registration/i });

    expect(screen.queryByText(/\bRp\b|IDR|receipt|payment|total/i)).not.toBeInTheDocument();
  });

  // The submit is gated on BOTH conditions, and the second is the one a guest can
  // satisfy by accident if the checkbox is an ordinary checkbox.
  it("keeps the submit disabled until the form is valid AND the terms are accepted", async () => {
    const user = userEvent.setup();
    stubApi();
    renderView(<RegistrationView slug={SLUG} ticketId={TICKET_ID} />);

    await screen.findByRole("form", { name: /invitation registration/i });
    const submit = screen.getByRole("button", { name: /confirm registration/i });
    expect(submit).toBeDisabled();

    await fillValidForm(user);
    await waitFor(() => expect(submit).toBeDisabled());

    await user.click(screen.getByRole("checkbox"));
    await user.click(await screen.findByRole("button", { name: /^agree$/i }));

    await waitFor(() => expect(submit).toBeEnabled());
  });

  // FR-014a. Consent is the act of reading, so the control does not toggle — it
  // opens the document. If this regresses, a guest can agree to terms they were
  // never shown and the whole gate is decorative.
  it("opens the terms instead of ticking the box when the box is clicked", async () => {
    const user = userEvent.setup();
    stubApi();
    renderView(<RegistrationView slug={SLUG} ticketId={TICKET_ID} />);

    await screen.findByRole("form", { name: /invitation registration/i });
    const box = screen.getByRole("checkbox");

    await user.click(box);

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(box).not.toBeChecked();
  });

  // FR-043a. Reported from a screenshot: this dialog rendered a single small
  // right-aligned "Accept" where the booking dialog has an agreement checkbox and
  // two full-width actions — visibly a different dialog on the same product.
  //
  // The footer now lives in the shared shell, so it cannot diverge again. This
  // asserts the three things that were missing, which is what a reader comparing
  // the two screens would notice first.
  it("renders the same footer as the booking dialog", async () => {
    const user = userEvent.setup();
    stubApi();
    renderView(<RegistrationView slug={SLUG} ticketId={TICKET_ID} />);

    await screen.findByRole("form", { name: /invitation registration/i });
    await user.click(screen.getByRole("checkbox"));

    const dialog = await screen.findByRole("dialog");

    expect(within(dialog).getByText(/i agree to terms & conditions/i)).toBeInTheDocument();
    expect(within(dialog).getByText(/^cancel$/i)).toBeInTheDocument();
    expect(await within(dialog).findByRole("button", { name: /^agree$/i })).toBeInTheDocument();
    // And the old bare Accept is gone, not merely joined by the rest.
    expect(within(dialog).queryByRole("button", { name: /^accept$/i })).toBeNull();
  });

  it("shows back exactly what was typed before anything is recorded (FR-017)", async () => {
    const user = userEvent.setup();
    const fetchSpy = stubApi();
    renderView(<RegistrationView slug={SLUG} ticketId={TICKET_ID} />);

    await screen.findByRole("form", { name: /invitation registration/i });
    await fillValidForm(user);
    await user.click(screen.getByRole("checkbox"));
    await user.click(await screen.findByRole("button", { name: /^agree$/i }));
    await user.click(screen.getByRole("button", { name: /confirm registration/i }));

    const review = await screen.findByRole("dialog");
    expect(review).toHaveTextContent("Halo Registrant");
    expect(review).toHaveTextContent("halo@example.com");
    expect(review).toHaveTextContent("628125567820");
    expect(review).toHaveTextContent("12/04/1996");

    // The review shows the ENTRIES and nothing else. The "Registration Info"
    // section that named the event and the ticket type was removed 2026-08-21
    // (Figma 764:659) and FR-017 amended to match, so asserting their absence is
    // what stops it being reinstated by accident.
    expect(review).not.toHaveTextContent("Jazz Night 2026");
    expect(review).not.toHaveTextContent("Invitation Access");

    expect(
      fetchSpy.mock.calls.filter(([, init]) => (init as RequestInit)?.method === "POST"),
    ).toHaveLength(0);
  });

  it("surfaces a server refusal without claiming the registration succeeded", async () => {
    const user = userEvent.setup();
    stubApi({
      submit: failure(400002, "There are no places left for this registration."),
    });
    renderView(<RegistrationView slug={SLUG} ticketId={TICKET_ID} />);

    await screen.findByRole("form", { name: /invitation registration/i });
    await fillValidForm(user);
    await user.click(screen.getByRole("checkbox"));
    await user.click(await screen.findByRole("button", { name: /^agree$/i }));
    await user.click(screen.getByRole("button", { name: /confirm registration/i }));
    await user.click(await screen.findByRole("button", { name: /confirm & submit/i }));

    expect(await screen.findByText(/no places left/i)).toBeInTheDocument();
    expect(push).not.toHaveBeenCalled();
  });

  // FR-011/FR-012: every reason the registration is unavailable arrives as ONE
  // refusal and renders as one sentence. A per-condition branch here would rebuild
  // the enumeration oracle the server deliberately collapsed.
  it("renders one refusal panel for an unavailable registration", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(failure(404001, "This registration is not available.", null, 404)),
    );
    renderView(<RegistrationView slug={SLUG} ticketId={TICKET_ID} />);

    expect(await screen.findByText(/registration unavailable/i)).toBeInTheDocument();
    expect(screen.queryByRole("form")).not.toBeInTheDocument();
  });
});

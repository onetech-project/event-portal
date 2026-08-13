import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { OrderView } from "./page";
import type { TicketOrderDetail } from "@/lib/types";

const replace = vi.fn();
const push = vi.fn();

vi.mock("next/navigation", () => ({
  useRouter: () => ({ replace, push }),
}));

const PENDING: TicketOrderDetail = {
  order_id: "ORD-20260801-A1B2C3D4",
  status: "PENDING",
  total_amount: "550000.00",
  subtotal: "500000.00",
  fees: [{ name: "PPN (10%)", amount: "50000.00" }],
  terms_agreed_at: "2026-08-01T10:00:30Z",
  expires_at: "2026-08-01T10:15:00Z",
  payment_started: true,
  slots: [],
  event: {
    name: "Jazz Night 2026",
    slug: "jazz-night-2026",
    venue: "Balai Sarbini",
    address: "Jakarta, Indonesia",
    start_date: "2026-09-26T08:00:00Z",
    end_date: "2026-09-27T14:00:00Z",
  },
  items: [
    {
      kind: "ticket",
      ticket_type_name: "Regular",
      package_name: null,
      quantity: 2,
      // Coherent with the order's money above: 2 x 250.000 = the 500.000
      // subtotal, + the 50.000 fee = the 550.000 total that is charged. It
      // previously read 2 x 275.000 = 550.000, which made the lines sum to the
      // grand total and left the order's own subtotal unaccounted for — and
      // put a stray "Rp 550.000" on the forms step that had nothing to do with
      // the fee-inclusive figure.
      unit_price: "250000.00",
      subtotal: "500000.00",
      admission_starts: ["2026-09-01T12:00:00Z"],
    },
  ],
  server_time: "2026-08-01T10:03:00Z",
  payment: {
    method: "QRIS",
    provider: "manjo",
    amount: "550000.00",
    expires_at: "2026-08-01T10:15:00Z",
    qr_image_path: "/api/v1/orders/ORD-20260801-A1B2C3D4/qris.png",
  },
};

const PAID: TicketOrderDetail = { ...PENDING, status: "PAID", payment: null };
const EXPIRED: TicketOrderDetail = { ...PENDING, status: "EXPIRED", payment: null };

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** Success bodies arrive wrapped in the API envelope (spec 008). */
function envelope(data: unknown) {
  return jsonResponse({ code: 200000, message: "Success", data });
}

/** A fresh client per test so no cached order leaks between them. */
function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  Wrapper.displayName = "TestQueryWrapper";
  return Wrapper;
}

// OrderView rather than the default export: the page's route-param unwrapping
// uses `use()`, which does not settle under this test DOM.
function renderOrder(eventSlug = PENDING.event.slug) {
  return render(
    <OrderView eventSlug={eventSlug} orderNumber={PENDING.order_id} />,
    { wrapper: wrapper() },
  );
}

beforeEach(() => {
  vi.restoreAllMocks();
  replace.mockClear();
  push.mockClear();
});

// FR-013: an order reached through the wrong event is a dead end, not a
// redirect. Forwarding to the owning event would make a wrong URL work.
describe("order page — wrong event", () => {
  it("refuses an order that belongs to another event", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder("some-other-event");

    expect(
      await screen.findByRole("heading", { name: /order not found for this event/i }),
    ).toBeInTheDocument();
  });

  it("shows none of the order's details", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder("some-other-event");
    await screen.findByRole("heading", { name: /order not found for this event/i });

    expect(screen.queryByText(PENDING.order_id)).not.toBeInTheDocument();
    expect(screen.queryByText("Regular")).not.toBeInTheDocument();
    expect(screen.queryByText("x2")).not.toBeInTheDocument();
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
    // Above all, not the owning event's name — that alone would leak which
    // event the order belongs to.
    expect(screen.queryByText("Jazz Night 2026")).not.toBeInTheDocument();
  });

  it("links back to the event in the URL, never to the one that owns the order", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder("some-other-event");
    await screen.findByRole("heading", { name: /order not found for this event/i });

    expect(screen.getByRole("link", { name: /back to the event/i })).toHaveAttribute(
      "href",
      "/events/some-other-event",
    );
  });
});

// Spec 011 FR-021: this page is only ever the ticket-holder-forms screen.
// Anything further along belongs elsewhere — a settled order on the
// confirmation, an order already paying on the QR screen — and every hop is a
// replace so the back button cannot bounce between screens that forward to
// each other.
describe("order page — settled and started", () => {
  const DONE_PATH = `/events/${PENDING.event.slug}/orders/${PENDING.order_id}/success`;
  const CHECKOUT_PATH = `/events/${PENDING.event.slug}/orders/${PENDING.order_id}/checkout`;

  it("sends a paid order to the confirmation screen", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderOrder();

    await waitFor(() => expect(replace).toHaveBeenCalledWith(DONE_PATH));
  });

  // FR-020 / FR-022: an expired order is a dead end shown HERE, not a
  // confirmation — forwarding to /success would congratulate a failed purchase.
  it("shows the Time's Up dialog for an expired order instead of forwarding", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderOrder();

    expect(
      await screen.findByRole("heading", { name: /time's up/i }),
    ).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
  });

  // The assertion that actually distinguishes FR-022 from the full-page card it
  // replaced. Without it, this suite passes for either design: the old card
  // also showed a "Time's Up" heading — it just destroyed everything else on
  // the way. The guest keeps the screen they were on.
  it("keeps the order's own screen rendered behind the dialog", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderOrder();
    await screen.findByRole("heading", { name: /time's up/i });

    expect(screen.getByText("Jazz Night 2026")).toBeInTheDocument();
    expect(screen.getByText("Regular")).toBeInTheDocument();
  });

  // FR-023/FR-024: one way out, and no way to dismiss it into a page the guest
  // can no longer submit.
  it("offers exactly one action and no way to dismiss itself", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderOrder();
    const dialog = await screen.findByRole("dialog");

    const actions = within(dialog).getAllByRole("link");
    expect(actions).toHaveLength(1);
    expect(actions[0]).toHaveAccessibleName(/return to home page/i);
    expect(actions[0]).toHaveAttribute("href", "/");

    // The Repeat Order link is gone (FR-024) — and with it the copy that told
    // the guest to repeat an order nothing offers to repeat.
    expect(screen.queryByRole("link", { name: /repeat order/i })).not.toBeInTheDocument();
    expect(screen.queryByText(/please repeat your order/i)).not.toBeInTheDocument();
    // No close (X) control anywhere on it.
    expect(within(dialog).queryByRole("button", { name: /close/i })).not.toBeInTheDocument();
  });

  it("shows a cancelled order the same dialog, worded as cancelled", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(envelope({ ...PENDING, status: "CANCELLED", payment: null })),
    );

    renderOrder();

    expect(
      await screen.findByRole("heading", { name: /order cancelled/i }),
    ).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    // Same single action as the expiry variant.
    expect(screen.getByRole("link", { name: /return to home page/i })).toHaveAttribute(
      "href",
      "/",
    );
    expect(screen.queryByRole("link", { name: /repeat order/i })).not.toBeInTheDocument();
  });

  it("replaces rather than pushes, so back does not bounce off this screen", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderOrder();

    await waitFor(() => expect(replace).toHaveBeenCalled());
    expect(push).not.toHaveBeenCalled();
  });

  it("shows no payment instructions on the way out", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderOrder();

    await waitFor(() => expect(replace).toHaveBeenCalled());
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /check payment status/i }),
    ).not.toBeInTheDocument();
  });

  it("does not forward an order belonging to another event", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderOrder("some-other-event");

    await screen.findByRole("heading", { name: /order not found for this event/i });
    // Forwarding here would confirm the order exists and hand over its event.
    expect(replace).not.toHaveBeenCalled();
  });

  it("sends an order whose payment already started to the QR screen", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder();

    // The hop Continue to Payment takes, and the one a bookmark of this
    // address takes after the fact (spec 011 FR-021).
    await waitFor(() => expect(replace).toHaveBeenCalledWith(CHECKOUT_PATH));
    expect(push).not.toHaveBeenCalled();
    // No forms are rendered on the way out — they are no longer fillable.
    expect(
      screen.queryByPlaceholderText(/as written on id card/i),
    ).not.toBeInTheDocument();
  });

  // A settled page must place no further load on the API.
  it("stops polling once the status is final", async () => {
    const fetchSpy = vi.fn().mockResolvedValue(envelope(PAID));
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await waitFor(() => expect(replace).toHaveBeenCalled());

    const afterLoad = fetchSpy.mock.calls.length;
    await new Promise((resolve) => setTimeout(resolve, 100));

    expect(fetchSpy.mock.calls.length).toBe(afterLoad);
  });
});

describe("order page — unknown order", () => {
  it("says the order was not found without revealing anything else", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({ error_code: "ORDER_NOT_FOUND", message: "Order not found." }, 404),
      ),
    );

    renderOrder();

    // The design keeps this screen heading-free; the alert carries the words.
    expect(
      await screen.findByText(/could not find an order with that number/i),
    ).toBeInTheDocument();

    // FR-014: even a dead end keeps the guest inside the event they came from,
    // rather than dropping them back at the full event list.
    expect(screen.getByRole("link", { name: /back to the event/i })).toHaveAttribute(
      "href",
      `/events/${PENDING.event.slug}`,
    );
  });
});

// --- The registration phase (spec 011, superseding 008 US5) ------------------
//
// One visitor card per slot group and nothing else: the buyer form is gone,
// and delivery is per ticket holder.

const HELD: TicketOrderDetail = {
  ...PENDING,
  payment_started: false,
  payment: null,
  terms_agreed_at: "2026-08-01T10:00:30Z",
  expires_at: "2026-08-01T11:00:00Z",
  slots: [
    {
      id: "51111111-1111-1111-1111-111111111111",
      ticket_type_name: "Regular",
      package_name: null,
      package_id: null,
      package_unit: null,
      name: null,
      email: null,
      phone: null,
      dob: null,
      gender: null,
    },
    {
      id: "52222222-2222-2222-2222-222222222222",
      ticket_type_name: "Regular",
      package_name: null,
      package_id: null,
      package_unit: null,
      name: null,
      email: null,
      phone: null,
      dob: null,
      gender: null,
    },
  ],
};

/**
 * A held order for ONE bundle unit containing two constituent tickets
 * (spec 010 US1): the registration step must show a single bundle-titled
 * visitor form whose values fan out to both slots.
 */
const BUNDLE_PACKAGE_ID = "9f2c1111-1111-1111-1111-111111111111";
const BUNDLE_HELD: TicketOrderDetail = {
  ...HELD,
  items: [
    {
      kind: "package",
      ticket_type_name: null,
      package_name: "2-Day Bundle",
      quantity: 1,
      unit_price: "550000.00",
      subtotal: "550000.00",
      admission_starts: ["2026-09-01T12:00:00Z"],
    },
  ],
  slots: [
    {
      id: "61111111-1111-1111-1111-111111111111",
      ticket_type_name: "Day 1 Pass",
      package_name: "2-Day Bundle",
      package_id: BUNDLE_PACKAGE_ID,
      package_unit: 1,
      name: null,
      email: null,
      phone: null,
      dob: null,
      gender: null,
    },
    {
      id: "62222222-2222-2222-2222-222222222222",
      ticket_type_name: "Day 2 Pass",
      package_name: "2-Day Bundle",
      package_id: BUNDLE_PACKAGE_ID,
      package_unit: 1,
      name: null,
      email: null,
      phone: null,
      dob: null,
      gender: null,
    },
  ],
};

/** The gender master list (GET /ticket/genders) the form's options load from. */
const GENDERS = [
  { id: "g1111111-1111-1111-1111-111111111111", name: "FEMALE" },
  { id: "g2222222-2222-2222-2222-222222222222", name: "MALE" },
];

/**
 * A valid spec-011 phone (FR-006, clarified 2026-08-07, floor raised
 * 2026-08-13): 12-15 digits, stored exactly as typed. The local `08…` form is
 * what this fixture enters, so it is also what the payload carries — the form
 * normalises nothing. Twelve digits is now the minimum, which this value meets
 * exactly.
 */
const PHONE = "081234567890";

/**
 * URL-routed fetch stub for the registration phase: the gender master list, a
 * successful checkout, and the given order for everything else. A fresh
 * Response per call, so no body is ever consumed twice.
 */
function registrationFetch(order: TicketOrderDetail) {
  return vi.fn().mockImplementation((url: string) => {
    if (String(url).includes("/ticket/genders")) {
      return Promise.resolve(envelope(GENDERS));
    }
    if (String(url).includes("/ticket/checkout/")) {
      return Promise.resolve(
        envelope({
          order_id: order.order_id,
          qr_string: "QR",
          expires_at: "2026-08-01T10:17:00Z",
          qr_image_url: `/api/v1/ticket/order/${order.order_id}/qris.png`,
        }),
      );
    }
    return Promise.resolve(envelope(order));
  });
}

/** Fills visitor card `index` (name/email/phone/dob/gender) with valid values. */
async function fillCard(
  user: ReturnType<typeof userEvent.setup>,
  index: number,
  person: { name: string; email: string; dob?: string },
) {
  const names = screen.getAllByPlaceholderText(/as written on id card/i);
  const emails = screen.getAllByPlaceholderText("name@example.com");
  const phones = screen.getAllByPlaceholderText("081234567890");
  const dobs = screen.getAllByPlaceholderText("DD/MM/YYYY");
  const genders = screen.getAllByRole("combobox");
  await user.type(names[index], person.name);
  await user.type(emails[index], person.email);
  await user.type(phones[index], PHONE);
  // Fixtures keep the wire's ISO date; the field is typed as DD/MM/YYYY and
  // the form converts back to ISO on submit.
  const [year, month, day] = (person.dob ?? "2000-01-31").split("-");
  await user.type(dobs[index], `${day}/${month}/${year}`);
  // Gender is the design-system Select: choosing means opening it and
  // clicking an option.
  await user.click(genders[index]);
  await user.click(await screen.findByRole("option", { name: "Female" }));
}

/** Waits for the registration phase to mount and returns its submit button. */
async function findContinueButton() {
  return await screen.findByRole("button", { name: /continue to payment/i });
}

describe("order page — registration phase (payment not started)", () => {
  it("shows one empty visitor card per slot and no buyer card", async () => {
    vi.stubGlobal("fetch", registrationFetch(HELD));

    renderOrder();

    // Two slots → two cards; every input empty (Option B revisit).
    const nameInputs = await screen.findAllByPlaceholderText(/as written on id card/i);
    expect(nameInputs).toHaveLength(2);
    for (const input of nameInputs) {
      expect(input).toHaveValue("");
    }
    // Spec 011: the separate buyer form is gone — the first card's holder is
    // the primary contact.
    expect(screen.queryByText(/buyer contact/i)).not.toBeInTheDocument();
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /continue to payment/i }),
    ).toBeInTheDocument();
  });

  it("pins the delivery notice to the first holder card only", async () => {
    vi.stubGlobal("fetch", registrationFetch(HELD));

    renderOrder();

    // Two cards, ONE chip (FR-011, clarified 2026-08-06): it rides the FIRST
    // card's header, top right (Figma 206-1804).
    const nameInputs = await screen.findAllByPlaceholderText(/as written on id card/i);
    expect(nameInputs).toHaveLength(2);
    const chip = screen.getByText("The invoice and e-ticket will be sent via email");
    expect(nameInputs[0].closest<HTMLElement>('[data-slot="card"]')).toContainElement(chip);
    expect(nameInputs[1].closest<HTMLElement>('[data-slot="card"]')).not.toContainElement(chip);
    // The page-top banner is gone — the chip is the only delivery notice.
    expect(screen.queryByText(/emailed to each ticket holder/i)).not.toBeInTheDocument();
  });

  it("marks every field of every card as required", async () => {
    vi.stubGlobal("fetch", registrationFetch(HELD));

    renderOrder();
    await screen.findAllByPlaceholderText(/as written on id card/i);

    // 2 cards × 5 fields, each label carrying the visual asterisk plus the
    // screen-reader-only "(required)" (spec 011 US1-AS3).
    expect(screen.getAllByText("*")).toHaveLength(10);
    expect(screen.getAllByText("(required)")).toHaveLength(10);
  });

  it("renders the Figma 12-4456 summary: event box, QRIS radio, no hold countdown", async () => {
    vi.stubGlobal("fetch", registrationFetch(HELD));

    renderOrder();
    await screen.findAllByPlaceholderText(/as written on id card/i);

    // Event box carries venue, address, and gate-open time from the API.
    expect(screen.getByText("Balai Sarbini")).toBeInTheDocument();
    expect(screen.getByText("Jakarta, Indonesia")).toBeInTheDocument();
    expect(screen.getByText(/gate opens at .* WIB/i)).toBeInTheDocument();

    // FR-013 and FR-014 as amended 2026-08-13: the card carries neither the
    // Booking ID nor a per-unit price. Both are absence assertions, so each
    // leans on the rendered positives above (venue, gate time) and the QRIS
    // radio below — a panel that failed to render would otherwise satisfy them
    // both at once (research R30).
    expect(screen.queryByText(PENDING.order_id)).not.toBeInTheDocument();
    expect(screen.queryByText(/ORD-/)).not.toBeInTheDocument();

    // HELD's line is Regular x2 at 250.000, subtotal 500.000. The unit price is
    // the one figure that must NOT appear, and 250.000 collides with none of
    // the line subtotal (500.000), the fee (50.000), or the total (550.000).
    // "Regular" also names each holder card on this step, so the panel's copy
    // is one of several — getAllByText, not getByText.
    expect(screen.getAllByText("Regular").length).toBeGreaterThan(0);
    expect(screen.getByText("x2")).toBeInTheDocument();
    expect(screen.queryByText(/Rp\s?250[.,]000/)).not.toBeInTheDocument();

    // QRIS is a pre-selected radio — the sole payment method.
    expect(screen.getByRole("radio", { name: /pay with qris/i })).toBeChecked();

    // The order-hold countdown box is gone from this step.
    expect(screen.queryByText(/complete purchase/i)).not.toBeInTheDocument();

    // No fee reaches this step in ANY form (spec 011 FR-016, constitution
    // v4.0.0): not as itemized rows, and not folded into the closing figure.
    // HELD's subtotal (500.000) and total (550.000) differ and it carries a
    // real fee, so each assertion below is the rule doing the work rather than
    // missing data — on equal figures they would all pass regardless.
    expect(HELD.subtotal).not.toBeNull();
    expect(HELD.subtotal).not.toBe(HELD.total_amount);

    expect(screen.queryByText("Ticket Total")).not.toBeInTheDocument();
    expect(screen.queryByText(/^Subtotal \(/)).not.toBeInTheDocument();
    expect(screen.queryByText("PPN (10%)")).not.toBeInTheDocument();

    // The pre-fee subtotal, under a heading that still reads Total payment
    // (FR-016a) and a note that no longer claims the fees are already in it.
    expect(screen.getByText(/total payment/i)).toBeInTheDocument();
    expect(screen.getByTestId("order-total-figure")).toHaveTextContent("Rp 500.000");
    expect(screen.getByText(/Excludes taxes and fees/i)).toBeInTheDocument();
    expect(screen.queryByText(/includes all taxes and fees/i)).not.toBeInTheDocument();

    // The fee-inclusive figure appears nowhere on this step — the assertion
    // that would have caught the reported defect.
    expect(screen.queryByText("Rp 550.000")).not.toBeInTheDocument();
  });

  it("enables Continue only once every field of every card is valid", async () => {
    vi.stubGlobal("fetch", registrationFetch(HELD));

    renderOrder();
    const button = await findContinueButton();

    // FR-009: gated from the start.
    expect(button).toBeDisabled();

    const user = userEvent.setup();
    await fillCard(user, 0, { name: "Siti Rahayu", email: "siti@example.com" });
    // One card done, the other still empty: still gated.
    expect(button).toBeDisabled();

    await fillCard(user, 1, { name: "Budi Visitor", email: "budi@example.com" });
    await waitFor(() => expect(button).toBeEnabled());

    // Clearing any field re-arms the gate.
    await user.clear(screen.getAllByPlaceholderText(/as written on id card/i)[0]);
    await waitFor(() => expect(button).toBeDisabled());
  });

  it("reveals errors on untouched fields when the gate is clicked, and posts nothing", async () => {
    const fetchSpy = registrationFetch(HELD);
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await findContinueButton();

    // The wrapper around the disabled button catches the click and runs
    // trigger() — the "attempted to proceed" moment (US2-AS5).
    const user = userEvent.setup();
    await user.click(screen.getByTestId("continue-gate"));

    // Untouched fields now show their errors…
    expect(await screen.findAllByText("Select a gender.")).toHaveLength(2);
    expect(screen.getAllByText("Full name is required.")).toHaveLength(2);
    expect(screen.getAllByText("Enter a phone number of 12-15 digits.")).toHaveLength(2);
    expect(screen.getAllByText("Date of birth is required.")).toHaveLength(2);
    // …and nothing was submitted.
    expect(
      fetchSpy.mock.calls.some((args) => String(args[0]).includes("/ticket/checkout/")),
    ).toBe(false);
  });

  it("rejects a phone number outside 12-15 digits with the exact message", async () => {
    vi.stubGlobal("fetch", registrationFetch(HELD));

    renderOrder();
    const phones = await screen.findAllByPlaceholderText("081234567890");
    // The numeric mobile keyboard (FR-006); the mask and schema do the rest.
    expect(phones[0]).toHaveAttribute("inputmode", "numeric");

    const user = userEvent.setup();
    // Eleven digits — an ordinary local-form Indonesian number, and the case
    // the 2026-08-13 floor added. Under the old 10-digit floor this passed, so
    // it is the input that distinguishes the two rules; a shorter value would
    // have been rejected either way and would prove nothing.
    await user.type(phones[0], "08123456789");
    await user.tab(); // mode "onTouched": the error appears on leaving the field.

    expect(
      await screen.findByText("Enter a phone number of 12-15 digits."),
    ).toBeInTheDocument();

    // The same subscriber number in international form is twelve digits and
    // clears it. Nothing normalises between the two spellings — the floor is
    // counted on what was typed.
    await user.clear(phones[0]);
    await user.type(phones[0], "628123456789");
    await waitFor(() =>
      expect(
        screen.queryByText("Enter a phone number of 12-15 digits."),
      ).not.toBeInTheDocument(),
    );
  });

  // The old field was a bare `type="tel"` input, which is only a keyboard hint:
  // letters typed straight through and sat there until the schema complained on
  // blur. The mask is what actually keeps them out (clarified 2026-08-07).
  it("drops everything that is not a digit as it is typed", async () => {
    vi.stubGlobal("fetch", registrationFetch(HELD));

    renderOrder();
    const phones = await screen.findAllByPlaceholderText("081234567890");
    const user = userEvent.setup();

    await user.type(phones[0], "abc0812-345 def6789");
    // Eleven digits: the mask filters characters, it does NOT enforce the
    // 12-digit floor. There is deliberately no typing floor — a guest cannot be
    // stopped mid-number at digit 11 — so the length is checked on submit and
    // this value is legitimately still too short at this point.
    expect(phones[0]).toHaveValue("08123456789");

    // FR-006 keeps whichever form the guest chose, so the local and
    // international spellings stay DIFFERENT values — nothing is normalised.
    await user.clear(phones[0]);
    await user.type(phones[0], "+62 812-3456-789");
    expect(phones[0]).toHaveValue("628123456789");

    // 15 digits is the ceiling; typing past it is ignored, not rejected.
    await user.clear(phones[0]);
    await user.type(phones[0], "08123456789012345");
    expect(phones[0]).toHaveValue("081234567890123");
  });

  it("types date of birth as masked DD/MM/YYYY with no calendar control", async () => {
    vi.stubGlobal("fetch", registrationFetch(HELD));

    renderOrder();
    const dobs = await screen.findAllByPlaceholderText("DD/MM/YYYY");
    // A text field, not the native picker (clarified 2026-08-06).
    expect(dobs[0]).toHaveAttribute("inputmode", "numeric");
    expect(dobs[0]).not.toHaveAttribute("type", "date");

    // Digits alone produce the separators.
    const user = userEvent.setup();
    await user.type(dobs[0], "31121999");
    expect(dobs[0]).toHaveValue("31/12/1999");

    // A date that does not exist on the calendar is rejected…
    await user.clear(dobs[0]);
    await user.type(dobs[0], "31022000");
    await user.tab();
    expect(await screen.findByText("Enter a real calendar date.")).toBeInTheDocument();

    // …as is a future one, while TODAY is accepted (spec edge case).
    const now = new Date();
    const pad = (value: number) => String(value).padStart(2, "0");
    const today = `${pad(now.getDate())}${pad(now.getMonth() + 1)}${now.getFullYear()}`;
    await user.clear(dobs[0]);
    await user.type(dobs[0], `${pad(now.getDate())}${pad(now.getMonth() + 1)}${now.getFullYear() + 1}`);
    await user.tab();
    expect(
      await screen.findByText("Date of birth cannot be in the future."),
    ).toBeInTheDocument();

    await user.clear(dobs[0]);
    await user.type(dobs[0], today);
    await user.tab();
    await waitFor(() =>
      expect(
        screen.queryByText("Date of birth cannot be in the future."),
      ).not.toBeInTheDocument(),
    );
  });

  it("submits one attendee per slot and no buyer keys", async () => {
    const fetchSpy = registrationFetch(HELD);
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    const button = await findContinueButton();

    const user = userEvent.setup();
    await fillCard(user, 0, {
      name: "Siti Rahayu",
      email: "siti@example.com",
      dob: "1995-05-05",
    });
    await fillCard(user, 1, {
      name: "Budi Visitor",
      email: "budi@example.com",
      dob: "2000-01-31",
    });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find((args) =>
        String(args[0]).includes(`/ticket/checkout/${HELD.order_id}`),
      );
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.[1]?.body));
      // Spec 011: attendees only — the server derives the primary contact
      // from the topmost form via canonical slot order.
      expect(Object.keys(body)).toEqual(["attendees"]);
      expect(body.attendees).toHaveLength(2);
      expect(body.attendees[0]).toMatchObject({
        id: HELD.slots[0].id,
        name: "Siti Rahayu",
        email: "siti@example.com",
        phone: PHONE,
        dob: "1995-05-05",
        gender: "FEMALE",
      });
      expect(body.attendees[1]).toMatchObject({
        id: HELD.slots[1].id,
        name: "Budi Visitor",
        email: "budi@example.com",
        dob: "2000-01-31",
      });
    });
  });

  it("shows no forms once payment has started — that step is over", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder();

    // Spec 011 FR-020: the QR now has an address of its own, so this page has
    // nothing left to render for a started order and forwards instead.
    await waitFor(() => expect(replace).toHaveBeenCalled());
    expect(
      screen.queryByText(/invoice and e-ticket will be sent via email/i),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /continue to payment/i }),
    ).not.toBeInTheDocument();
    expect(screen.queryByText(/scan to pay/i)).not.toBeInTheDocument();
  });
});

// --- Spec 010 US1: a bundle unit collapses into one visitor form -------------

describe("order page — registration phase with a bundle", () => {
  it("collapses a bundle unit into ONE form titled with the bundle name", async () => {
    vi.stubGlobal("fetch", registrationFetch(BUNDLE_HELD));

    renderOrder();

    // A single bundle card — not one card per constituent slot.
    expect(await screen.findAllByPlaceholderText(/as written on id card/i)).toHaveLength(1);
    // The card's title is the bundle's ("2-Day Bundle" also sits in the order
    // summary, so scope to card titles), with the pass count as its badge;
    // constituent ticket-type names do not appear as card titles.
    const cardTitles = Array.from(
      document.querySelectorAll('[data-slot="card-title"]'),
    ).map((el) => el.textContent ?? "");
    expect(cardTitles.some((title) => title.includes("2-Day Bundle"))).toBe(true);
    expect(screen.getByText(/2 tickets/i)).toBeInTheDocument();
    expect(screen.queryByText("Day 1 Pass")).not.toBeInTheDocument();
    expect(screen.queryByText("Day 2 Pass")).not.toBeInTheDocument();
  });

  it("fans the single bundle form out to one payload entry per slot", async () => {
    const fetchSpy = registrationFetch(BUNDLE_HELD);
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    const button = await findContinueButton();

    const user = userEvent.setup();
    await fillCard(user, 0, {
      name: "Bundle Visitor",
      email: "visitor@example.com",
      dob: "2000-01-31",
    });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find((args) =>
        String(args[0]).includes(`/ticket/checkout/${BUNDLE_HELD.order_id}`),
      );
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.[1]?.body));
      // One form, two slots: identical visitor data on both entries (FR-003),
      // each carrying its own slot id — and nothing but attendees in the body.
      expect(Object.keys(body)).toEqual(["attendees"]);
      expect(body.attendees).toHaveLength(2);
      expect(body.attendees.map((a: { id: string }) => a.id)).toEqual([
        BUNDLE_HELD.slots[0].id,
        BUNDLE_HELD.slots[1].id,
      ]);
      for (const attendee of body.attendees) {
        expect(attendee.name).toBe("Bundle Visitor");
        expect(attendee.email).toBe("visitor@example.com");
        expect(attendee.dob).toBe("2000-01-31");
        expect(attendee.gender).toBe("FEMALE");
      }
    });
  });

  it("keeps one form per standalone ticket next to the collapsed bundle (mixed order)", async () => {
    // Standalone slots come first on the wire (ORDER BY package_id NULLS FIRST).
    const MIXED_HELD: TicketOrderDetail = {
      ...BUNDLE_HELD,
      slots: [HELD.slots[0], ...BUNDLE_HELD.slots],
    };
    const fetchSpy = registrationFetch(MIXED_HELD);
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    const button = await findContinueButton();

    // Standalone card + ONE bundle card = 2 forms for 3 slots.
    expect(screen.getAllByPlaceholderText(/as written on id card/i)).toHaveLength(2);
    const cardTitles = Array.from(
      document.querySelectorAll('[data-slot="card-title"]'),
    ).map((el) => el.textContent ?? "");
    expect(cardTitles.some((title) => title.includes("Regular"))).toBe(true);
    expect(cardTitles.some((title) => title.includes("2-Day Bundle"))).toBe(true);

    const user = userEvent.setup();
    await fillCard(user, 0, {
      name: "Solo Visitor",
      email: "solo@example.com",
      dob: "1999-09-09",
    });
    await fillCard(user, 1, {
      name: "Bundle Visitor",
      email: "bundle@example.com",
      dob: "2000-01-31",
    });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find((args) =>
        String(args[0]).includes(`/ticket/checkout/${MIXED_HELD.order_id}`),
      );
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.[1]?.body));
      // 3 slots on the wire: the standalone entry independent, the bundle
      // pair identical.
      expect(body.attendees).toHaveLength(3);
      const byId = new Map(
        body.attendees.map((a: { id: string; name: string; email: string }) => [a.id, a]),
      );
      expect(byId.get(HELD.slots[0].id)).toMatchObject({
        name: "Solo Visitor",
        email: "solo@example.com",
      });
      for (const slot of BUNDLE_HELD.slots) {
        expect(byId.get(slot.id)).toMatchObject({
          name: "Bundle Visitor",
          email: "bundle@example.com",
        });
      }
    });
  });

  it("shows one labeled form per unit when the same bundle is purchased twice", async () => {
    const unitSlot = (id: string, unit: number, day: string) => ({
      id,
      ticket_type_name: day,
      package_name: "2-Day Bundle",
      package_id: BUNDLE_PACKAGE_ID,
      package_unit: unit,
      name: null,
      email: null,
      phone: null,
      dob: null,
      gender: null,
    });
    const TWO_UNITS_HELD: TicketOrderDetail = {
      ...BUNDLE_HELD,
      items: [{ ...BUNDLE_HELD.items[0], quantity: 2, subtotal: "1100000.00" }],
      slots: [
        unitSlot("71111111-1111-1111-1111-111111111111", 1, "Day 1 Pass"),
        unitSlot("72222222-2222-2222-2222-222222222222", 1, "Day 2 Pass"),
        unitSlot("73333333-3333-3333-3333-333333333333", 2, "Day 1 Pass"),
        unitSlot("74444444-4444-4444-4444-444444444444", 2, "Day 2 Pass"),
      ],
    };
    const fetchSpy = registrationFetch(TWO_UNITS_HELD);
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    const button = await findContinueButton();

    // One form per purchased unit (4 slots → 2 forms), told apart by their
    // visitor labels.
    expect(screen.getAllByPlaceholderText(/as written on id card/i)).toHaveLength(2);
    expect(screen.getByText("Visitor 1")).toBeInTheDocument();
    expect(screen.getByText("Visitor 2")).toBeInTheDocument();

    const user = userEvent.setup();
    await fillCard(user, 0, {
      name: "First Guest",
      email: "first@example.com",
      dob: "2000-01-31",
    });
    await fillCard(user, 1, {
      name: "Second Guest",
      email: "second@example.com",
      dob: "2001-02-01",
    });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find((args) =>
        String(args[0]).includes(`/ticket/checkout/${TWO_UNITS_HELD.order_id}`),
      );
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.[1]?.body));
      // 4 payload entries: unit 1's pair carries the first guest, unit 2's the
      // second — each unit internally identical.
      expect(body.attendees).toHaveLength(4);
      const byId = new Map(
        body.attendees.map((a: { id: string; email: string }) => [a.id, a.email]),
      );
      expect(byId.get(TWO_UNITS_HELD.slots[0].id)).toBe("first@example.com");
      expect(byId.get(TWO_UNITS_HELD.slots[1].id)).toBe("first@example.com");
      expect(byId.get(TWO_UNITS_HELD.slots[2].id)).toBe("second@example.com");
      expect(byId.get(TWO_UNITS_HELD.slots[3].id)).toBe("second@example.com");
    });
  });

  it("routes a server error on a fanned-out entry back onto the bundle form", async () => {
    const message = "All tickets in the same bundle must use the same visitor information.";
    const fetchSpy = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes("/ticket/genders")) {
        return Promise.resolve(envelope(GENDERS));
      }
      if (String(url).includes("/ticket/checkout/")) {
        // The server indexes the fanned-out payload: entry 1 is the bundle's
        // second slot, which the UI shows as the one bundle card.
        return Promise.resolve(
          jsonResponse(
            {
              code: 400001,
              message: "The visitor forms do not match the order's tickets.",
              data: { "attendees[1].dob": message },
            },
            400,
          ),
        );
      }
      return Promise.resolve(envelope(BUNDLE_HELD));
    });
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    const button = await findContinueButton();

    const user = userEvent.setup();
    await fillCard(user, 0, {
      name: "Bundle Visitor",
      email: "visitor@example.com",
      dob: "2000-01-31",
    });
    await waitFor(() => expect(button).toBeEnabled());
    await user.click(button);

    // Payload index 1 remaps to the single visible bundle card's dob field.
    expect(await screen.findByText(message)).toBeInTheDocument();
  });
});

// --- A card title that does not fit hands its text to a tooltip --------------

/**
 * happy-dom lays nothing out — every element measures zero — so the ellipsis a
 * real browser draws has to be simulated. Reports the given widths for card
 * titles only, which is what the overflow check compares.
 */
function stubCardTitleWidths(scrollWidth: number, clientWidth: number) {
  for (const [prop, value] of [
    ["scrollWidth", scrollWidth],
    ["clientWidth", clientWidth],
  ] as const) {
    Object.defineProperty(HTMLElement.prototype, prop, {
      configurable: true,
      get(this: HTMLElement) {
        return this.getAttribute("data-slot") === "card-title" ? value : 0;
      },
    });
  }
}

describe("order page — a card title too long for its card", () => {
  afterEach(() => {
    // The stubs are own properties of the prototype; deleting them uncovers
    // happy-dom's own accessors again for the next file.
    const proto = HTMLElement.prototype as unknown as Record<string, unknown>;
    delete proto.scrollWidth;
    delete proto.clientWidth;
  });

  it("offers the whole heading as a tooltip once the line is clipped", async () => {
    stubCardTitleWidths(400, 100);
    vi.stubGlobal("fetch", registrationFetch(BUNDLE_HELD));

    renderOrder();
    await findContinueButton();

    const title = document.querySelector('[data-slot="card-title"]');
    expect(title).not.toBeNull();

    await userEvent.setup().hover(title as HTMLElement);

    // Title, unit label and badge — everything the ellipsis swallowed — read
    // back in one line.
    expect(
      await screen.findByText("2-Day Bundle · 2 tickets"),
    ).toBeInTheDocument();
  });

  it("stays quiet when the heading fits", async () => {
    stubCardTitleWidths(100, 100);
    vi.stubGlobal("fetch", registrationFetch(BUNDLE_HELD));

    renderOrder();
    await findContinueButton();

    const title = document.querySelector('[data-slot="card-title"]');
    await userEvent.setup().hover(title as HTMLElement);

    // Nothing is hidden, so a tooltip would only shadow text already on screen.
    await waitFor(() =>
      expect(screen.queryByText("2-Day Bundle · 2 tickets")).toBeNull(),
    );
  });
});

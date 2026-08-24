import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { OrderForms } from "./visitor-form";
import type { TicketOrderDetail, TicketOrderSlot } from "@/lib/types";

/**
 * Spec 011 FR-030 – FR-033 (clarified 2026-08-19): the holder forms start from
 * whatever the order already holds.
 *
 * The rule these tests exist to pin is not "the fields have values" — it is that
 * they are seeded ONCE. The order is re-read on every window focus and
 * reconnect, and the natural way to write a prefill (an effect that syncs the
 * form whenever the order changes) reads as more correct while silently wiping a
 * guest's typing the moment they come back from their banking app. That failure
 * has no visible symptom until it happens to a real person mid-form, so it gets
 * a test rather than a comment.
 */

function jsonResponse(body: unknown) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

function envelope(data: unknown) {
  return jsonResponse({ code: 200000, message: "Success", data });
}

const GENDERS = [
  // Numeric ids, matching the wire since migration 0013 narrowed them. The
  // uuid-shaped placeholders these replace were harmless while the NAME was the
  // key and became wrong the moment the id started being submitted (FR-034).
  { id: 1, name: "FEMALE" },
  { id: 2, name: "MALE" },
];

function slot(overrides: Partial<TicketOrderSlot> & { id: string }): TicketOrderSlot {
  return {
    ticket_type_name: "Regular",
    package_name: null,
    package_id: null,
    package_unit: null,
    name: null,
    email: null,
    phone: null,
    dob: null,
    gender_id: null,
    gender: null,
    ...overrides,
  };
}

function order(slots: TicketOrderSlot[]): TicketOrderDetail {
  return {
    order_id: "ORD-20260801-A1B2C3D4",
    status: "PENDING",
    total_amount: "550000.00",
    subtotal: "500000.00",
    fees: [],
    terms_agreed_at: "2026-08-01T10:00:30Z",
    expires_at: "2026-08-01T11:00:00Z",
    payment_started: false,
    slots,
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
        quantity: slots.length,
        unit_price: "250000.00",
        subtotal: "500000.00",
        admission_starts: ["2026-09-01T12:00:00Z"],
      },
    ],
    server_time: "2026-08-01T10:03:00Z",
    payment: null,
  };
}

const SAVED = {
  name: "Heather Higgins",
  email: "heather@example.com",
  phone: "144650550532",
  dob: "1990-10-09",
  gender_id: 1,
  gender: "FEMALE",
};

const FILLED = order([slot({ id: "s1", ...SAVED })]);
const EMPTY = order([slot({ id: "s1" })]);

function stubFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation((url: string) => {
      if (String(url).includes("/ticket/genders")) {
        return Promise.resolve(envelope(GENDERS));
      }
      return Promise.resolve(envelope({}));
    }),
  );
}

function wrapper() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

const nameInput = () => screen.getByPlaceholderText(/as written on id card/i);
const emailInput = () => screen.getByPlaceholderText("name@example.com");
const phoneInput = () => screen.getByPlaceholderText("081234567890");
const dobInput = () => screen.getByPlaceholderText("DD/MM/YYYY");

beforeEach(() => {
  vi.restoreAllMocks();
  stubFetch();
});

describe("OrderForms — seeding from the order's stored details (FR-030)", () => {
  it("fills every field of a card from its slot", async () => {
    render(<OrderForms order={FILLED} />, { wrapper: wrapper() });

    expect(nameInput()).toHaveValue("Heather Higgins");
    expect(emailInput()).toHaveValue("heather@example.com");
    expect(phoneInput()).toHaveValue("144650550532");
    // Stored date-only, displayed in the field's own format.
    expect(dobInput()).toHaveValue("09/10/1990");
    expect(await screen.findByRole("combobox", { name: "Gender" })).toHaveTextContent(
      "Female",
    );
  });

  it("leaves a card empty when its slot holds nothing", () => {
    render(<OrderForms order={EMPTY} />, { wrapper: wrapper() });

    // FR-030's last clause. This is the pair that stops an unconditional
    // prefill from looking correct.
    expect(nameInput()).toHaveValue("");
    expect(emailInput()).toHaveValue("");
    expect(dobInput()).toHaveValue("");
  });

  it("enables Continue to Payment on arrival for a fully restored card", async () => {
    render(<OrderForms order={FILLED} />, { wrapper: wrapper() });

    // FR-009 unchanged: the gate is "every field of every form is valid" and
    // nothing else, so a restored card satisfies it without being touched.
    expect(
      await screen.findByRole("button", { name: /continue to payment/i }),
    ).toBeEnabled();
  });

  it("shows no error on a field merely for having been restored", () => {
    render(<OrderForms order={FILLED} />, { wrapper: wrapper() });

    expect(screen.queryByText(/is required/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/enter a phone number/i)).not.toBeInTheDocument();
  });

  it("seeds each card from its own slot", () => {
    render(
      <OrderForms
        order={order([
          slot({ id: "s1", ...SAVED }),
          slot({ id: "s2", ...SAVED, name: "Second Holder", email: "second@example.com" }),
        ])}
      />,
      { wrapper: wrapper() },
    );

    const names = screen.getAllByPlaceholderText(/as written on id card/i);
    expect(names[0]).toHaveValue("Heather Higgins");
    expect(names[1]).toHaveValue("Second Holder");
  });
});

describe("OrderForms — the seed is applied once and never re-applied (FR-032)", () => {
  it("does not overwrite an edited field when the order is re-read", async () => {
    const user = userEvent.setup();
    const Wrapper = wrapper();
    const { rerender } = render(<OrderForms order={FILLED} />, { wrapper: Wrapper });

    await user.clear(nameInput());
    await user.type(nameInput(), "Edited By Guest");

    // A refetch delivering the SAME order: a new object, identical contents,
    // which is exactly what refetchOnWindowFocus produces.
    rerender(<OrderForms order={order([slot({ id: "s1", ...SAVED })])} />);

    expect(nameInput()).toHaveValue("Edited By Guest");
  });

  it("does not refill a field the guest deliberately cleared", async () => {
    const user = userEvent.setup();
    const Wrapper = wrapper();
    const { rerender } = render(<OrderForms order={FILLED} />, { wrapper: Wrapper });

    await user.clear(emailInput());
    rerender(<OrderForms order={order([slot({ id: "s1", ...SAVED })])} />);

    // The clearing has to survive, or the guest cannot correct a stored typo.
    expect(emailInput()).toHaveValue("");
  });

  it("does not move an untouched field when the stored details themselves change", () => {
    const Wrapper = wrapper();
    const { rerender } = render(<OrderForms order={FILLED} />, { wrapper: Wrapper });

    rerender(
      <OrderForms order={order([slot({ id: "s1", ...SAVED, name: "Server Says Otherwise" })])} />,
    );

    // Seeded once. The saved details are a starting point, not a live feed —
    // the screen is not a mirror of the order.
    expect(nameInput()).toHaveValue("Heather Higgins");
  });
});

describe("OrderForms — a restored gender that has since been retired (FR-031)", () => {
  const RETIRED = order([slot({ id: "s1", ...SAVED, gender_id: 99, gender: "PREFER_NOT_TO_SAY" })]);

  it("shows the held gender even though the master list no longer offers it", async () => {
    render(<OrderForms order={RETIRED} />, { wrapper: wrapper() });

    expect(await screen.findByRole("combobox", { name: "Gender" })).toHaveTextContent(
      "Prefer_not_to_say",
    );
  });

  it("offers the retired value in that card's own list", async () => {
    const user = userEvent.setup();
    render(<OrderForms order={RETIRED} />, { wrapper: wrapper() });

    await user.click(await screen.findByRole("combobox", { name: "Gender" }));
    expect(
      await screen.findByRole("option", { name: "Prefer_not_to_say" }),
    ).toBeInTheDocument();
  });

  it("does not offer it on a card that does not hold it", async () => {
    const user = userEvent.setup();
    render(
      <OrderForms
        order={order([
          slot({ id: "s1", ...SAVED, gender_id: 99, gender: "PREFER_NOT_TO_SAY" }),
          slot({ id: "s2", ...SAVED }),
        ])}
      />,
      { wrapper: wrapper() },
    );

    const selects = await screen.findAllByRole("combobox", { name: "Gender" });
    await user.click(selects[1]);

    // The widening is per card, not an amnesty on the master list.
    expect(screen.queryByRole("option", { name: "Prefer_not_to_say" })).toBeNull();
    expect(await screen.findByRole("option", { name: "Female" })).toBeInTheDocument();
  });

  it("drops the retired option once the guest picks an active one", async () => {
    const user = userEvent.setup();
    render(<OrderForms order={RETIRED} />, { wrapper: wrapper() });

    const select = await screen.findByRole("combobox", { name: "Gender" });
    await user.click(select);
    await user.click(await screen.findByRole("option", { name: "Male" }));
    expect(select).toHaveTextContent("Male");

    await user.click(select);
    expect(screen.queryByRole("option", { name: "Prefer_not_to_say" })).toBeNull();
  });

  it("leaves the option list untouched for an ordinary active gender", async () => {
    const user = userEvent.setup();
    render(<OrderForms order={FILLED} />, { wrapper: wrapper() });

    await user.click(await screen.findByRole("combobox", { name: "Gender" }));
    expect(await screen.findAllByRole("option")).toHaveLength(GENDERS.length);
  });
});

describe("OrderForms — nothing announces the restore (FR-033)", () => {
  it("adds no notice, and leaves the delivery chip the only one on the forms", () => {
    render(<OrderForms order={FILLED} />, { wrapper: wrapper() });

    expect(screen.queryByText(/restored/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/previously entered/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/we filled/i)).not.toBeInTheDocument();
    // The one notice the cards are meant to carry (FR-011) is still there, so
    // the absences above are not passing because the form failed to render.
    expect(
      screen.getByText("The invoice and e-ticket will be sent via email"),
    ).toBeInTheDocument();
  });
});

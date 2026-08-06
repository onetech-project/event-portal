import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

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
  buyer_name: "Siti Rahayu",
  buyer_email: "siti@example.com",
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
      unit_price: "275000.00",
      subtotal: "550000.00",
    },
  ],
  server_time: "2026-08-01T10:03:00Z",
  payment: {
    method: "QRIS",
    provider: "midtrans",
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

describe("order page — awaiting payment", () => {
  it("shows the order, the QR code, and the amount to pay", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder();

    expect(await screen.findByText(/scan to pay/i)).toBeInTheDocument();
    // The Complete Purchase countdown banner spans the page (Figma 32-1366).
    expect(screen.getByText(/complete purchase/i)).toBeInTheDocument();
    expect(screen.getByText("Jazz Night 2026")).toBeInTheDocument();
    // The summary lists the ticket line with its quantity badge.
    expect(screen.getByText("Regular")).toBeInTheDocument();
    expect(screen.getByText("x2")).toBeInTheDocument();
    // How to pay is a collapsible on the summary (Figma 203-1157).
    expect(screen.getByRole("button", { name: /how to pay with qris/i })).toBeInTheDocument();

    // The fee breakdown frozen at booking (Figma 32-1366).
    expect(screen.getByText(/subtotal \(2 items\)/i)).toBeInTheDocument();
    expect(screen.getByText("PPN (10%)")).toBeInTheDocument();

    // Rendered by our own API from the stored payload — the browser never talks
    // to the payment provider.
    const qr = screen.getByRole("img", { name: /QRIS code/i });
    expect(qr).toHaveAttribute("src", "http://api.test" + PENDING.payment!.qr_image_path);
  });

  it("expands the how-to-pay collapsible to the QRIS steps", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder();
    const trigger = await screen.findByRole("button", { name: /how to pay with qris/i });

    // Collapsed by default (Figma 32-1366); the steps appear on demand.
    expect(screen.queryByText(/scan qr or pay menu/i)).not.toBeInTheDocument();
    await userEvent.setup().click(trigger);
    expect(await screen.findByText(/scan qr or pay menu/i)).toBeInTheDocument();
    expect(screen.getByText(/enter your pin/i)).toBeInTheDocument();
  });

  it("offers a way to check the payment status on demand", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder();

    expect(
      await screen.findByRole("button", { name: /check payment status/i }),
    ).toBeInTheDocument();
  });

  it("says the page updates itself while it is polling", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder();

    expect(await screen.findByText(/updates by itself/i)).toBeInTheDocument();
  });
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

    expect(screen.queryByText("siti@example.com")).not.toBeInTheDocument();
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

// FR-018: this page is only ever the paying screen. Anything settled belongs on
// the confirmation, whether it settled while the guest watched or was already
// settled when they opened a saved link.
describe("order page — settled", () => {
  const DONE_PATH = `/events/${PENDING.event.slug}/orders/${PENDING.order_id}/done`;

  it("sends a paid order to the confirmation screen", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PAID)));

    renderOrder();

    await waitFor(() => expect(replace).toHaveBeenCalledWith(DONE_PATH));
  });

  // FR-020 / Figma 288-2295: an expired order is a dead end shown HERE, not a
  // confirmation — forwarding to /done would congratulate a failed purchase.
  it("shows the Time's Up state for an expired order instead of forwarding", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(EXPIRED)));

    renderOrder();

    expect(
      await screen.findByRole("heading", { name: /time's up/i }),
    ).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();

    // The way back to repeat the order stays inside the event.
    expect(screen.getByRole("link", { name: /repeat order/i })).toHaveAttribute(
      "href",
      `/events/${PENDING.event.slug}/tickets`,
    );
    expect(screen.getByRole("link", { name: /return to home page/i })).toHaveAttribute(
      "href",
      "/",
    );
  });

  it("shows a cancelled order the same dead end, worded as cancelled", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(envelope({ ...PENDING, status: "CANCELLED", payment: null })),
    );

    renderOrder();

    expect(
      await screen.findByRole("heading", { name: /order cancelled/i }),
    ).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
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

  it("does not forward an order that is still pending", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder();

    await screen.findByRole("img", { name: /QRIS code/i });
    expect(replace).not.toHaveBeenCalled();
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

describe("order page — checking status", () => {
  it("reports that nothing has changed yet", async () => {
    const fetchSpy = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        url.includes("/payment/refresh")
          ? envelope({
              order_number: PENDING.order_id,
              status: "PENDING",
              changed: false,
              checked_at: "2026-08-01T10:04:00Z",
            })
          : envelope(PENDING),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await userEvent.click(
      await screen.findByRole("button", { name: /check payment status/i }),
    );

    expect(await screen.findByText(/still waiting for your payment/i)).toBeInTheDocument();
  });

  // FR-018: pressing too often is a cooldown, not an error.
  it("shows a cooldown rather than an error when rate limited", async () => {
    const fetchSpy = vi.fn().mockImplementation((url: string) =>
      Promise.resolve(
        url.includes("/payment/refresh")
          ? jsonResponse(
              { error_code: "RATE_LIMITED", message: "Too many requests." },
              429,
            )
          : envelope(PENDING),
      ),
    );
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    const button = await screen.findByRole("button", { name: /check payment status/i });
    await userEvent.click(button);

    expect(await screen.findByText(/wait a few seconds/i)).toBeInTheDocument();
    await waitFor(() => expect(button).toBeDisabled());
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

// --- US5 (spec 008): the registration phase, Option B ------------------------

const HELD: TicketOrderDetail = {
  ...PENDING,
  payment_started: false,
  payment: null,
  buyer_name: null,
  buyer_email: null,
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

describe("order page — registration phase (payment not started)", () => {
  it("shows empty visitor forms, one card per slot, plus the buyer block", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(HELD)));

    renderOrder();

    // No page heading on this step (Figma 12-4456) — the buyer card leads.
    expect(await screen.findByText(/buyer contact/i)).toBeInTheDocument();
    // Buyer block + two visitor cards; every input empty (Option B revisit).
    const nameInputs = screen.getAllByPlaceholderText(/as written on id card/i);
    expect(nameInputs).toHaveLength(3);
    for (const input of nameInputs) {
      expect(input).toHaveValue("");
    }
    expect(screen.queryByRole("img", { name: /QRIS code/i })).not.toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /continue to payment/i }),
    ).toBeInTheDocument();
  });

  it("renders the Figma 12-4456 summary: badge, event box, QRIS radio, no hold countdown", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(HELD)));

    renderOrder();
    await screen.findByText(/buyer contact/i);

    // Blue info badge beside the buyer card title, not a helper paragraph.
    expect(
      screen.getByText(/the invoice and e-ticket will be sent via email/i),
    ).toBeInTheDocument();

    // Event box carries venue, address, and gate-open time from the API.
    expect(screen.getByText("Balai Sarbini")).toBeInTheDocument();
    expect(screen.getByText("Jakarta, Indonesia")).toBeInTheDocument();
    expect(screen.getByText(/gate opens at .* WIB/i)).toBeInTheDocument();

    // QRIS is a pre-selected radio — the sole payment method.
    expect(screen.getByRole("radio", { name: /pay with qris/i })).toBeChecked();

    // The order-hold countdown box is gone from this step.
    expect(screen.queryByText(/complete purchase/i)).not.toBeInTheDocument();
  });

  it("submits the forms to the checkout endpoint with the slot ids", async () => {
    const fetchSpy = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes("/ticket/genders")) {
        return Promise.resolve(envelope(GENDERS));
      }
      if (String(url).includes("/ticket/checkout/")) {
        return Promise.resolve(
          envelope({
            order_id: HELD.order_id,
            qr_string: "QR",
            expires_at: "2026-08-01T10:17:00Z",
            qr_image_url: `/api/v1/ticket/order/${HELD.order_id}/qris.png`,
            qr_refresh_after_seconds: 420,
          }),
        );
      }
      return Promise.resolve(envelope(HELD));
    });
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await screen.findByText(/buyer contact/i);

    const user = userEvent.setup();
    // Index 0 of every repeated input belongs to the buyer block; visitor
    // cards follow in slot order.
    const names = screen.getAllByPlaceholderText(/as written on id card/i);
    const emails = screen.getAllByPlaceholderText("name@example.com");
    const phones = screen.getAllByPlaceholderText("+62812XXXXXXX");
    await user.type(names[0], "Siti Rahayu");
    await user.type(emails[0], "siti@example.com");
    await user.type(phones[0], "+628123456789");

    // Buyer form carries gender + dob too (Figma 12-4456): index 0 of the
    // repeated controls belongs to the buyer, visitors follow. Gender is the
    // design-system Select, so choosing means opening it and clicking an option.
    const chooseGender = async (combobox: HTMLElement) => {
      await user.click(combobox);
      await user.click(await screen.findByRole("option", { name: "Female" }));
    };
    const dobs = document.querySelectorAll('input[type="date"]');
    const genders = screen.getAllByRole("combobox");
    await user.type(dobs[0] as HTMLElement, "1995-05-05");
    await chooseGender(genders[0]);
    // The trigger shows the human label, not the stored value.
    expect(genders[0]).toHaveTextContent("Female");
    expect(genders[0]).not.toHaveTextContent("FEMALE");
    for (let i = 0; i < 2; i++) {
      await user.type(names[i + 1], `Visitor ${i}`);
      await user.type(emails[i + 1], `v${i}@example.com`);
      await user.type(phones[i + 1], "+62812345678");
      await user.type(dobs[i + 1] as HTMLElement, "2000-01-31");
      await chooseGender(genders[i + 1]);
    }

    await user.click(screen.getByRole("button", { name: /continue to payment/i }));

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find((args) =>
        String(args[0]).includes(`/ticket/checkout/${HELD.order_id}`),
      );
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.[1]?.body));
      expect(body.buyer_email).toBe("siti@example.com");
      expect(body.buyer_dob).toBe("1995-05-05");
      expect(body.buyer_gender).toBe("FEMALE");
      expect(body.attendees).toHaveLength(2);
      expect(body.attendees[0].id).toBe(HELD.slots[0].id);
      expect(body.attendees[0].gender).toBe("FEMALE");
      expect(body.attendees[0].dob).toBe("2000-01-31");
    });
  });

  it("keeps the QR phase for an order whose payment already started", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(PENDING)));

    renderOrder();

    expect(await screen.findByText(/scan to pay/i)).toBeInTheDocument();
    expect(screen.queryByText(/buyer contact/i)).not.toBeInTheDocument();
  });
});

// --- Spec 010 US1: a bundle unit collapses into one visitor form -------------

describe("order page — registration phase with a bundle", () => {
  /** Fills one visitor-form row (name/email/phone/dob/gender) at `index`. */
  async function fillPerson(
    user: ReturnType<typeof userEvent.setup>,
    index: number,
    person: { name: string; email: string; dob: string },
  ) {
    const names = screen.getAllByPlaceholderText(/as written on id card/i);
    const emails = screen.getAllByPlaceholderText("name@example.com");
    const phones = screen.getAllByPlaceholderText("+62812XXXXXXX");
    const dobs = document.querySelectorAll('input[type="date"]');
    const genders = screen.getAllByRole("combobox");
    await user.type(names[index], person.name);
    await user.type(emails[index], person.email);
    await user.type(phones[index], "+628123456789");
    await user.type(dobs[index] as HTMLElement, person.dob);
    await user.click(genders[index]);
    await user.click(await screen.findByRole("option", { name: "Female" }));
  }

  it("collapses a bundle unit into ONE form titled with the bundle name", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(envelope(BUNDLE_HELD)));

    renderOrder();
    await screen.findByText(/buyer contact/i);

    // Buyer block + a single bundle card — not one card per constituent slot.
    expect(screen.getAllByPlaceholderText(/as written on id card/i)).toHaveLength(2);
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
    const fetchSpy = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes("/ticket/genders")) {
        return Promise.resolve(envelope(GENDERS));
      }
      if (String(url).includes("/ticket/checkout/")) {
        return Promise.resolve(
          envelope({
            order_id: BUNDLE_HELD.order_id,
            qr_string: "QR",
            expires_at: "2026-08-01T10:17:00Z",
            qr_image_url: `/api/v1/ticket/order/${BUNDLE_HELD.order_id}/qris.png`,
            qr_refresh_after_seconds: 420,
          }),
        );
      }
      return Promise.resolve(envelope(BUNDLE_HELD));
    });
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await screen.findByText(/buyer contact/i);

    const user = userEvent.setup();
    await fillPerson(user, 0, {
      name: "Siti Rahayu",
      email: "siti@example.com",
      dob: "1995-05-05",
    });
    await fillPerson(user, 1, {
      name: "Bundle Visitor",
      email: "visitor@example.com",
      dob: "2000-01-31",
    });

    await user.click(screen.getByRole("button", { name: /continue to payment/i }));

    await waitFor(() => {
      const call = fetchSpy.mock.calls.find((args) =>
        String(args[0]).includes(`/ticket/checkout/${BUNDLE_HELD.order_id}`),
      );
      expect(call).toBeDefined();
      const body = JSON.parse(String(call?.[1]?.body));
      // One form, two slots: identical visitor data on both entries (FR-003),
      // each carrying its own slot id.
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
    const fetchSpy = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes("/ticket/genders")) {
        return Promise.resolve(envelope(GENDERS));
      }
      if (String(url).includes("/ticket/checkout/")) {
        return Promise.resolve(
          envelope({
            order_id: MIXED_HELD.order_id,
            qr_string: "QR",
            expires_at: "2026-08-01T10:17:00Z",
            qr_image_url: `/api/v1/ticket/order/${MIXED_HELD.order_id}/qris.png`,
            qr_refresh_after_seconds: 420,
          }),
        );
      }
      return Promise.resolve(envelope(MIXED_HELD));
    });
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await screen.findByText(/buyer contact/i);

    // Buyer + standalone card + ONE bundle card = 3 forms for 3 slots.
    expect(screen.getAllByPlaceholderText(/as written on id card/i)).toHaveLength(3);
    const cardTitles = Array.from(
      document.querySelectorAll('[data-slot="card-title"]'),
    ).map((el) => el.textContent ?? "");
    expect(cardTitles.some((title) => title.includes("Regular"))).toBe(true);
    expect(cardTitles.some((title) => title.includes("2-Day Bundle"))).toBe(true);

    const user = userEvent.setup();
    await fillPerson(user, 0, {
      name: "Siti Rahayu",
      email: "siti@example.com",
      dob: "1995-05-05",
    });
    await fillPerson(user, 1, {
      name: "Solo Visitor",
      email: "solo@example.com",
      dob: "1999-09-09",
    });
    await fillPerson(user, 2, {
      name: "Bundle Visitor",
      email: "bundle@example.com",
      dob: "2000-01-31",
    });
    await user.click(screen.getByRole("button", { name: /continue to payment/i }));

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
    const fetchSpy = vi.fn().mockImplementation((url: string) => {
      if (String(url).includes("/ticket/genders")) {
        return Promise.resolve(envelope(GENDERS));
      }
      if (String(url).includes("/ticket/checkout/")) {
        return Promise.resolve(
          envelope({
            order_id: TWO_UNITS_HELD.order_id,
            qr_string: "QR",
            expires_at: "2026-08-01T10:17:00Z",
            qr_image_url: `/api/v1/ticket/order/${TWO_UNITS_HELD.order_id}/qris.png`,
            qr_refresh_after_seconds: 420,
          }),
        );
      }
      return Promise.resolve(envelope(TWO_UNITS_HELD));
    });
    vi.stubGlobal("fetch", fetchSpy);

    renderOrder();
    await screen.findByText(/buyer contact/i);

    // Buyer + one form per purchased unit (4 slots → 2 forms), told apart by
    // their visitor labels.
    expect(screen.getAllByPlaceholderText(/as written on id card/i)).toHaveLength(3);
    expect(screen.getByText("Visitor 1")).toBeInTheDocument();
    expect(screen.getByText("Visitor 2")).toBeInTheDocument();

    const user = userEvent.setup();
    await fillPerson(user, 0, {
      name: "Siti Rahayu",
      email: "siti@example.com",
      dob: "1995-05-05",
    });
    await fillPerson(user, 1, {
      name: "First Guest",
      email: "first@example.com",
      dob: "2000-01-31",
    });
    await fillPerson(user, 2, {
      name: "Second Guest",
      email: "second@example.com",
      dob: "2001-02-01",
    });
    await user.click(screen.getByRole("button", { name: /continue to payment/i }));

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
    await screen.findByText(/buyer contact/i);

    const user = userEvent.setup();
    await fillPerson(user, 0, {
      name: "Siti Rahayu",
      email: "siti@example.com",
      dob: "1995-05-05",
    });
    await fillPerson(user, 1, {
      name: "Bundle Visitor",
      email: "visitor@example.com",
      dob: "2000-01-31",
    });
    await user.click(screen.getByRole("button", { name: /continue to payment/i }));

    // Payload index 1 remaps to the single visible bundle card's dob field.
    expect(await screen.findByText(message)).toBeInTheDocument();
  });
});

/**
 * Page objects for the guest purchase journey, written against what the UI
 * actually exposes to a screen reader: labels, roles and accessible names.
 *
 * The app has almost no test ids, and that is fine — driving it by accessible
 * name means a change that breaks these selectors is usually a change that
 * broke accessibility too, which is a signal worth having rather than routing
 * around with brittle CSS paths.
 */

import { expect, type Page } from "@playwright/test";

/** One holder's details. The first form is the buyer (Constitution 3.0.0). */
export type Holder = {
  name: string;
  email: string;
  phone: string;
  /** "Male" | "Female" — the human label the select renders. */
  gender: "Male" | "Female";
  /** DD/MM/YYYY, as the input expects. */
  dob: string;
};

export const defaultHolder: Holder = {
  name: "Budi Santoso",
  email: "budi@example.com",
  phone: "081234567890",
  gender: "Male",
  dob: "15/08/1995",
};

export class GuestJourney {
  constructor(private readonly page: Page) {}

  /** The public catalogue. */
  async openCatalogue(): Promise<void> {
    await this.page.goto("/events");
  }

  async openEvent(slug: string): Promise<void> {
    await this.page.goto(`/events/${slug}`);
  }

  /** Detail page → ticket selection. */
  async goToTicketSelection(): Promise<void> {
    await this.page.getByRole("link", { name: /buy tickets/i }).click();
    await this.page.waitForURL(/\/tickets$/);
  }

  async openTicketSelection(slug: string): Promise<void> {
    await this.page.goto(`/events/${slug}/tickets`);
  }

  /**
   * Bumps a ticket's quantity using the stepper's accessible controls, which is
   * how a keyboard user would do it.
   */
  async selectQuantity(ticketName: string, quantity: number): Promise<void> {
    // The row starts collapsed behind an "Add <name>" control and only becomes a
    // stepper once the first one is added.
    //
    // Waiting explicitly rather than probing isVisible() matters: on a cold
    // `next dev` the row can still be hydrating, and a probe would quietly
    // report "not visible", skip the click, and leave the stepper that never
    // appears to time out fifteen seconds later with a misleading message.
    const add = this.page.getByRole("button", { name: `Add ${ticketName}` });
    await expect(add).toBeVisible();
    await add.click();

    // The counter is an aria-live <span>, not an input, so its text is the
    // assertion — and reading it after each click keeps the loop from racing
    // ahead of React's re-render.
    const stepper = this.page.getByLabel(`Quantity for ${ticketName}`);
    await expect(stepper).toHaveText("1");

    const increase = this.page.getByRole("button", { name: `Increase ${ticketName}` });
    for (let i = 1; i < quantity; i++) {
      await increase.click();
      await expect(stepper).toHaveText(String(i + 1));
    }
  }

  /**
   * Adjusts an already-selected row to an exact quantity, in either direction.
   *
   * Unlike selectQuantity this assumes the stepper is already showing, which is
   * what makes it the right helper for a guest correcting a refused selection
   * rather than building a new one.
   */
  async setQuantity(ticketName: string, quantity: number): Promise<void> {
    const stepper = this.page.getByLabel(`Quantity for ${ticketName}`);
    await expect(stepper).toBeVisible();

    const increase = this.page.getByRole("button", { name: `Increase ${ticketName}` });
    const decrease = this.page.getByRole("button", { name: `Decrease ${ticketName}` });

    // Read between clicks rather than computing a click count up front: the
    // counter is the source of truth, and stepping to it cannot drift.
    for (let guard = 0; guard < 20; guard++) {
      const current = Number((await stepper.textContent()) ?? "0");
      if (current === quantity) return;
      await (current < quantity ? increase : decrease).click();
      await expect(stepper).toHaveText(String(current < quantity ? current + 1 : current - 1));
    }

    throw new Error(`could not step ${ticketName} to ${quantity}`);
  }

  /**
   * The refusal panel the availability gate renders (spec 013).
   *
   * Named rather than located by bare role: the App Router keeps its own
   * always-present `role="alert"` route announcer in the DOM, so an unqualified
   * getByRole("alert") is a strict-mode violation on every page.
   */
  refusals() {
    return this.page.getByRole("alert", { name: /why this selection cannot be bought/i });
  }

  /** Reads the remaining-quota figure the row advertises, if it shows one. */
  async visibleQuotaText(ticketName: string): Promise<string> {
    const row = this.page.locator("section", { hasText: ticketName }).first();
    return (await row.textContent()) ?? "";
  }

  /**
   * Presses "Buy Ticket" and waits for the availability check it fires to
   * settle (spec 013).
   *
   * The button does not open the terms itself. It asks the server whether this
   * exact selection can still be bought, and only a clean answer opens the
   * gate — so a caller expecting the dialog must wait for the round trip, and a
   * caller expecting a refusal has somewhere to assert instead.
   *
   * This only presses. Waiting belongs to the caller's assertion — `toBeVisible`
   * on the dialog, or `toContainText` on the refusal — both of which retry, so
   * they absorb the round trip without a sleep. Waiting here instead would have
   * to re-find the button, and an open Radix dialog marks the page behind it
   * `aria-hidden`, so by role it is no longer there to find.
   */
  async buyTicket(): Promise<void> {
    const button = this.page.getByRole("button", { name: /buy ticket/i });
    await expect(button).toBeEnabled();
    await button.click();
  }

  /**
   * The full gate: press Buy Ticket, pass the availability check, agree to the
   * terms. Agreement is what books the order, so this is the moment quota is
   * actually taken.
   */
  async agreeToTermsAndBook(): Promise<string> {
    await this.buyTicket();

    const dialog = this.page.getByRole("dialog");
    await expect(dialog).toBeVisible();

    // The checkbox is unlabelled-by-text in the DOM sense; it sits next to the
    // "I agree to" copy inside the dialog.
    await dialog.getByRole("checkbox").click();
    await dialog.getByRole("button", { name: /^agree$/i }).click();

    // Booking + agreement are two calls; the dialog closes and the app lands on
    // the holder-forms step, whose URL carries the order number.
    await this.page.waitForURL(/\/orders\/[^/]+$/, { timeout: 30_000 });
    return this.orderNumberFromUrl();
  }

  orderNumberFromUrl(): string {
    const match = this.page.url().match(/\/orders\/([^/?#]+)/);
    if (!match) throw new Error(`no order number in URL: ${this.page.url()}`);
    return decodeURIComponent(match[1]);
  }

  /** Fills the nth holder form (0-based). Form 0 is the buyer. */
  async fillHolder(index: number, holder: Holder): Promise<void> {
    const form = this.page.getByRole("form", { name: /visitor registration/i });

    await form.getByLabel(/full name/i).nth(index).fill(holder.name);
    await form.getByLabel(/email address/i).nth(index).fill(holder.email);
    await form.getByLabel(/phone number/i).nth(index).fill(holder.phone);
    await form.getByPlaceholder("DD/MM/YYYY").nth(index).fill(holder.dob);

    // A custom select rendered in a portal, so the options are NOT inside the
    // form — they must be located from the page.
    //
    // exact:true is load-bearing: Playwright's name matching is substring by
    // default, and "Male" matches "Female" too.
    await this.page.getByRole("combobox", { name: "Gender" }).nth(index).click();
    const listbox = this.page.getByRole("listbox");
    await listbox.getByRole("option", { name: holder.gender, exact: true }).click();
    await expect(listbox).toBeHidden();
  }

  /**
   * Submits the forms and starts payment. This is the call that reaches the
   * gateway, so it is where the stub earns its keep.
   */
  async payWithQris(): Promise<void> {
    // "Pay with QRIS" is the (sr-only, pre-checked) payment-method radio, not
    // the action. The action is the submit inside the continue gate, whose
    // wrapper catches the click and surfaces field errors while the form is
    // still invalid.
    const submit = this.page.getByRole("button", { name: /continue to payment/i });
    await expect(submit).toBeEnabled();
    await submit.click();

    await this.page.waitForURL(/\/checkout$/, { timeout: 30_000 });
  }

  /** The QR screen, waiting on the provider. */
  async expectAwaitingPayment(): Promise<void> {
    await expect(this.page.getByText(/waiting for payment/i)).toBeVisible();
  }

  /**
   * After settlement the SSE stream pushes the transition and the app moves to
   * the confirmation screen on its own — no reload.
   */
  async expectConfirmation(): Promise<void> {
    await this.page.waitForURL(/\/done$/, { timeout: 45_000 });
    await expect(this.page.getByText(/thank you for your purchase/i)).toBeVisible();
  }
}

export class AdminConsole {
  constructor(private readonly page: Page) {}

  async signIn(email: string, password: string): Promise<void> {
    await this.page.goto("/admin/login");
    const form = this.page.getByRole("form", { name: /sign in/i });
    await form.getByLabel(/email/i).fill(email);
    await form.getByLabel(/password/i).fill(password);
    await form.getByRole("button", { name: /sign in/i }).click();
    await this.page.waitForURL(/\/admin\/events/, { timeout: 30_000 });
  }

  async openOrders(): Promise<void> {
    await this.page.goto("/admin/orders");
  }

  async openValidation(): Promise<void> {
    await this.page.goto("/admin/validate");
  }

  async openEvents(): Promise<void> {
    await this.page.goto("/admin/events");
  }
}

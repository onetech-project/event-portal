/**
 * Page objects for the guest purchase journey, written against what the UI
 * actually exposes to a screen reader: labels, roles and accessible names.
 *
 * The app has almost no test ids, and that is fine — driving it by accessible
 * name means a change that breaks these selectors is usually a change that
 * broke accessibility too, which is a signal worth having rather than routing
 * around with brittle CSS paths.
 */

import { expect, type Locator, type Page } from "@playwright/test";

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
  // Local part deliberately longer than four characters. At four or fewer the
  // email mask renders identically under the old and new FR-033 rules, so an
  // assertion on the masked address could never be red (research R-037).
  email: "budisantoso@example.com",
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

    // Spec 022 FR-045: reaching the end of the document ticks the agreement box
    // on the guest's behalf, which is what turns Agree up. This helper takes the
    // reading route deliberately; the guest may equally tick the box themselves
    // without scrolling (clarified 2026-08-21), which guest-purchase.spec.ts
    // asserts separately — the two must not be wired to one condition.
    await this.readTermsToTheEnd();
    await dialog.getByRole("button", { name: /^agree$/i }).click();

    // Booking + agreement are two calls; the dialog closes and the app lands on
    // the holder-forms step, whose URL carries the order number.
    await this.page.waitForURL(/\/orders\/[^/]+$/, { timeout: 30_000 });
    return this.orderNumberFromUrl();
  }

  /**
   * Ticks the agreement box and presses Agree on an ALREADY-OPEN terms dialog,
   * expecting booking to refuse rather than navigate (spec 013 FR-013a).
   *
   * A sibling of `agreeToTermsAndBook` rather than a flag on it: that one blocks
   * on `waitForURL`, and a refused booking never produces the URL it waits for.
   * It also has a long tail of happy-path callers that must not be disturbed.
   *
   * Deliberately does NOT press Buy Ticket. The caller owns the gap between the
   * passing check and this press, because that gap is where the race under test
   * happens. Pressing it here would also fail outright: an open dialog marks the
   * page behind it `aria-hidden`, so the button is no longer there to find.
   *
   * Presses only. The assertion belongs to the caller — what a refusal looks
   * like is exactly what the amendment changed.
   */
  async agreeExpectingRefusal(): Promise<void> {
    const dialog = this.page.getByRole("dialog");
    await expect(dialog).toBeVisible();

    await this.readTermsToTheEnd();
    await dialog.getByRole("button", { name: /^agree$/i }).click();
  }

  /**
   * Scrolls the open Terms & Conditions to the bottom and waits for the gate to
   * open (spec 022 FR-014b).
   *
   * Scrolls the region rather than pressing End, because the two are different
   * assertions: this one is "the guest read it", and pressing keys is what the
   * keyboard-only scenario tests deliberately and separately.
   *
   * Harmless on a SHORT document — the end counts as reached the moment it is
   * shown (FR-014d), so the box is already ticked, this scrolls nothing and
   * returns. That is what lets every existing purchase scenario keep its short
   * fixture unchanged. It is also exactly why a scenario that means to test the
   * automatic tick must seed `longTermsHtml()`: against the short fixture this
   * helper would pass even if the automatic tick were deleted.
   */
  async readTermsToTheEnd(): Promise<void> {
    const dialog = this.page.getByRole("dialog");
    const region = dialog.getByRole("region", { name: /terms and conditions/i });
    await expect(region).toBeVisible();

    // Scroll, then let the observer fire. Repeated because a document whose
    // images or fonts settle late can grow after the first scroll, leaving the
    // sentinel just out of view.
    await expect(async () => {
      await region.evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
      await expect(dialog.getByRole("button", { name: /^agree$/i })).toBeVisible({
        timeout: 1_000,
      });
    }).toPass({ timeout: 15_000 });
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
    await this.expectQrisFrameContents();
  }

  /**
   * The Scan to Pay card carries what the design draws and nothing else
   * (spec 020, contracts/ui.md).
   *
   * Asserted two ways because the two profiles can supply different things.
   *
   * The sentinels are the strong pin, and they only work locally: the runner
   * hands the frontend the five retired values (playwright.config.ts), so
   * before spec 020 the frame printed them and this method failed naming them.
   * A deployment cannot be made to export those, so under the uat profile the
   * sentinel half passes vacuously.
   *
   * The label half is what still bites there. `NMID`, `Dicetak oleh` and
   * `Versi Cetak` are the literal captions the frame used to print beside the
   * real values, so they catch a reintroduction in an environment carrying
   * genuine merchant configuration, which is the only kind a deployment has.
   */
  async expectQrisFrameContents(): Promise<void> {
    // Located by its text rather than by heading role, deliberately: the role
    // only holds AFTER spec 020 (the title used to be a styled div), and a
    // locator that cannot resolve against the old markup would fail this method
    // on a timeout instead of on the thing it exists to catch. The absences are
    // asserted before the positives for the same reason — the first failure a
    // reader sees should name the field that came back.
    const card = this.page
      .getByText("Scan to Pay", { exact: true })
      .locator("xpath=ancestor::*[@data-slot='card'][1]");
    await expect(card).toBeVisible();

    const text = (await card.textContent()) ?? "";

    // Absent: the five the runner is actively trying to make it print.
    for (const sentinel of [
      "E2E-MERCHANT-MUST-NOT-RENDER",
      "E2E-NMID-MUST-NOT-RENDER",
      "E2E-TERMINAL-MUST-NOT-RENDER",
      "E2E-ACQUIRER-MUST-NOT-RENDER",
      "E2E-VERSION-MUST-NOT-RENDER",
    ]) {
      expect(text, `payment card still renders ${sentinel}`).not.toContain(sentinel);
    }

    // Absent: the captions, which bite wherever real values are configured.
    for (const label of ["NMID", "Dicetak oleh", "Versi Cetak"]) {
      expect(text, `payment card still renders the "${label}" label`).not.toContain(
        label,
      );
    }

    // Present: the heading as a real heading, its subtitle, and the live code.
    await expect(card.getByRole("heading", { name: "Scan to Pay" })).toBeVisible();
    await expect(
      card.getByText(/use any e-wallet or mobile banking app supporting qris/i),
    ).toBeVisible();
    await expect(card.getByRole("img", { name: /qris code for/i })).toBeVisible();
  }

  /**
   * After settlement the SSE stream pushes the transition and the app moves to
   * the confirmation screen on its own — no reload.
   */
  async expectConfirmation(): Promise<void> {
    await this.page.waitForURL(/\/success$/, { timeout: 45_000 });
    // The heading, not loose body text: this screen also renders "Payment" in
    // its summary, and a regex broad enough to match that would pass on the
    // still-pending checkout page too.
    await expect(
      this.page.getByRole("heading", { name: /payment successful/i }),
    ).toBeVisible();
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

  async openEvent(eventId: string): Promise<void> {
    await this.page.goto(`/admin/events/${eventId}`);
  }

  // --- Pagination (spec 021) -----------------------------------------------
  //
  // Every admin list shares one control, so these work on all of them. The
  // scenarios drive the real buttons rather than the address bar wherever they
  // can, because what is under test is what an operator can actually do.

  /** The paging control, scoped so a page with one list has one match. */
  private pagination(): Locator {
    return this.page.getByRole("navigation", { name: /pagination/i });
  }

  async goToNextPage(): Promise<void> {
    await this.pagination().getByRole("button", { name: /next page/i }).click();
  }

  async goToPreviousPage(): Promise<void> {
    await this.pagination().getByRole("button", { name: /previous page/i }).click();
  }

  async goToPage(n: number): Promise<void> {
    await this.pagination().getByRole("button", { name: `Page ${n}` }).click();
  }

  async setPageSize(size: number): Promise<void> {
    await this.pagination().getByLabel(/rows per page/i).click();
    await this.page.getByRole("option", { name: String(size) }).click();
  }

  /** Whether a further page exists in that direction, as the controls report it. */
  async canGoForward(): Promise<boolean> {
    return this.pagination().getByRole("button", { name: /next page/i }).isEnabled();
  }

  async canGoBack(): Promise<boolean> {
    return this.pagination().getByRole("button", { name: /previous page/i }).isEnabled();
  }

  /** "Page 3 of 7" → { page: 3, totalPages: 7 }. */
  async currentPage(): Promise<{ page: number; totalPages: number }> {
    const text = (await this.pagination().getByText(/page \d+ of \d+/i).innerText()).trim();
    const [, page, totalPages] = /page (\d+) of (\d+)/i.exec(text) ?? [];
    return { page: Number(page), totalPages: Number(totalPages) };
  }

  /** The total the list reports for the current filters, from "Showing X–Y of Z". */
  async reportedTotal(): Promise<number> {
    const text = await this.pagination().getByText(/showing/i).innerText();
    const [, total] = /of (\d+)/.exec(text) ?? [];
    return Number(total);
  }

  /**
   * The first cell of every visible row — the identifier a paging walk collects
   * to prove it saw each record exactly once.
   */
  async visibleRowKeys(): Promise<string[]> {
    const cells = this.page.locator("tbody tr td:first-child");
    return (await cells.allInnerTexts()).map((t) => t.trim());
  }
}

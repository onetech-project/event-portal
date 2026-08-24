import { expect, test } from "@playwright/test";

import {
  adminLogin,
  createRegistrationEvent,
  putLongTerms,
  publicTicketTypes,
  request,
  requestRaw,
} from "../support/api";
import { db, quotaOf, resetDatabase, seedAdmin, ticketStatusOf } from "../support/db";
import { config } from "../support/env";
import { clearMailbox, downloadAttachment, isPDF, waitForMail } from "../support/mail";
import { AdminConsole } from "../support/journey";

/**
 * UAT for free ticket registration (spec 022).
 *
 * The invitation journey end to end: open the link, fill the form, read the Terms
 * & Conditions, agree to them, review, submit, and receive an e-ticket — with no
 * payment, no price and no receipt anywhere.
 *
 * Two things here are deliberately awkward, and both are the point.
 *
 * The terms are seeded LONG (`createRegistrationEvent` uses `longTermsHtml`). The
 * default fixture is one short paragraph, which fits inside the dialog, and a
 * document shorter than its viewport counts as already reached — correctly, since
 * there is nothing to scroll, so the box ticks itself the moment the modal opens.
 * Against that fixture these scenarios would pass with the automatic tick deleted.
 *
 * And the mail is read off Mailpit rather than trusted from a 201. The whole
 * recovery story for this feature rests on delivery actually happening: the guest
 * is told the ticket was sent before it has been, and is shown no address, so a
 * silent delivery failure is invisible to them forever.
 */

let token: string;

test.beforeEach(async () => {
  await resetDatabase();
  await seedAdmin();
  token = await adminLogin();
  await clearMailbox();
});

const HOLDER = {
  name: "Halo Registrant",
  email: "halo.registrant@example.com",
  phone: "628125567820",
  dob: "12/04/1996",
};

async function fillRegistrationForm(page: import("@playwright/test").Page, email: string) {
  const form = page.getByRole("form", { name: /invitation registration/i });
  await expect(form).toBeVisible();

  await form.getByLabel(/full name/i).fill(HOLDER.name);
  await form.getByLabel(/email address/i).fill(email);
  await form.getByLabel(/phone number/i).fill(HOLDER.phone);
  await form.getByLabel(/date of birth/i).fill(HOLDER.dob);

  await form.getByRole("combobox").first().click();
  // exact: true — Playwright matches accessible names by SUBSTRING, so "Male"
  // also matches "Female" and the click is a strict-mode violation.
  await page.getByRole("option", { name: "Male", exact: true }).click();
}

/** Scrolls the open terms to the bottom and waits for Accept to become pressable. */
async function readTermsToTheEnd(page: import("@playwright/test").Page) {
  const dialog = page.getByRole("dialog");
  const region = dialog.getByRole("region", { name: /terms and conditions/i });
  await expect(region).toBeVisible();

  await expect(async () => {
    await region.evaluate((el) => el.scrollTo({ top: el.scrollHeight }));
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toBeVisible({
      timeout: 1_000,
    });
  }).toPass({ timeout: 15_000 });
}

/**
 * The confirmation page's title.
 *
 * Located by CardTitle's `data-slot`, deliberately, NOT by `getByRole("heading")`.
 * CardTitle renders a plain <div>, so the title carries no heading role and a
 * role-based query cannot see it. Matching the component's own stable hook is
 * what keeps this honest: it asserts the title the page actually renders rather
 * than a semantic it does not claim.
 *
 * Do not "fix" this back to getByRole — it will fail, and it failed once already
 * (2026-08-21) when the page moved from a plain <h1> to CardTitle.
 */
function successTitle(page: import("@playwright/test").Page) {
  return page.locator('[data-slot="card-title"]').filter({ hasText: /registration complete/i });
}

/**
 * The FORM's agreement checkbox, not the modal's.
 *
 * Once the modal has been opened, `getByRole("checkbox")` matches two elements
 * and is a strict-mode violation — the dialog's own box stays mounted. The two
 * are told apart by their labels: the form reads "I agree to the Terms &
 * Conditions", the modal "I agree to terms & conditions".
 */
function formAgreementBox(page: import("@playwright/test").Page) {
  return page.getByRole("checkbox", { name: /I agree to the Terms/ });
}

async function acceptTerms(page: import("@playwright/test").Page) {
  await page.getByRole("checkbox").click();
  await readTermsToTheEnd(page);
  await page.getByRole("button", { name: /^agree$/i }).click();
}

test.describe("An invited guest registers and receives a free e-ticket", () => {
  test("the full journey: form, terms, review, e-ticket in the inbox", async ({ page }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-journey",
    });
    const before = await quotaOf(registration.id);

    await page.goto(`${config.frontendURL}/events/${event.slug}/register/${registration.id}`);

    await fillRegistrationForm(page, HOLDER.email);
    await acceptTerms(page);

    // FR-017: the review shows back exactly what was typed, before anything is
    // recorded.
    await page.getByRole("button", { name: /confirm registration/i }).click();
    const review = page.getByRole("dialog");
    await expect(review).toContainText(HOLDER.name);
    await expect(review).toContainText(HOLDER.email);
    // The review shows the guest's ENTRIES only. The section naming the event
    // and the ticket type was removed 2026-08-21 (Figma 764:659, FR-017
    // amended), so this asserts its absence rather than its content.
    await expect(review).not.toContainText(/registration info/i);

    await review.getByRole("button", { name: /confirm & submit/i }).click();

    await page.waitForURL(/\/register\/[^/]+\/success$/, { timeout: 30_000 });
    await expect(successTitle(page)).toBeVisible();

    // FR-015 / SC-008: no money anywhere on the guest's path.
    const body = await page.locator("body").innerText();
    expect(body).not.toMatch(/\bRp\b|IDR|receipt|payment/i);

    // Exactly one place taken (FR-026, SC-006).
    expect(await quotaOf(registration.id)).toBe(before - 1);

    // SC-003 / FR-035: ONE message, ONE attachment, and it is the e-ticket.
    const mail = await waitForMail(HOLDER.email);
    expect(mail.Attachments).toHaveLength(1);
    expect(mail.Subject).not.toMatch(/receipt/i);

    const pdf = await downloadAttachment(mail.ID, mail.Attachments[0].PartID);
    expect(isPDF(pdf)).toBe(true);

    // FR-036: no monetary figure in the message body either.
    const html = mail.HTML ?? "";
    expect(html).not.toMatch(/\bRp\b|IDR|Subtotal|Total/i);
  });

  // FR-023, reversed from the original spec: an address may register as many
  // times as remaining quota allows. Asserted through the UI rather than the API
  // because the refusal that used to exist was a guest-visible one.
  test("the same email may register again and receives a second ticket", async ({ page }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-repeat",
    });
    const before = await quotaOf(registration.id);
    const url = `${config.frontendURL}/events/${event.slug}/register/${registration.id}`;

    for (const attempt of [1, 2]) {
      await page.goto(url);
      await fillRegistrationForm(page, HOLDER.email);
      await acceptTerms(page);
      await page.getByRole("button", { name: /confirm registration/i }).click();
      await page
        .getByRole("dialog")
        .getByRole("button", { name: /confirm & submit/i })
        .click();
      await page.waitForURL(/\/register\/[^/]+\/success$/, { timeout: 30_000 });
      expect(await quotaOf(registration.id), `after attempt ${attempt}`).toBe(before - attempt);
    }

    // Two separate deliveries to the same inbox, each with its own e-ticket.
    const second = await waitForMail(HOLDER.email, { minCount: 2 });
    expect(second.Attachments).toHaveLength(1);
  });

  // FR-039b / SC-014: the confirmation is a distinct address and is reload-safe.
  test("reloading the confirmation re-shows it and registers nothing more", async ({ page }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-reload",
    });

    await page.goto(`${config.frontendURL}/events/${event.slug}/register/${registration.id}`);
    await fillRegistrationForm(page, HOLDER.email);
    await acceptTerms(page);
    await page.getByRole("button", { name: /confirm registration/i }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: /confirm & submit/i })
      .click();
    await page.waitForURL(/\/register\/[^/]+\/success$/, { timeout: 30_000 });

    const afterFirst = await quotaOf(registration.id);
    await page.reload();

    await expect(successTitle(page)).toBeVisible();
    await expect(page.getByRole("form", { name: /invitation registration/i })).toHaveCount(0);
    expect(await quotaOf(registration.id)).toBe(afterFirst);
  });
});

test.describe("Agreement on the registration form", () => {
  // Spec 022 FR-014a/FR-045. The document is still put in front of every guest —
  // that part did NOT change on 2026-08-21 — and reaching its end still ticks the
  // box for them. The terms are seeded long so this cannot pass by accident.
  test("reaching the end of the terms ticks the box and turns Agree up", async ({
    page,
  }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-gate",
    });

    await page.goto(`${config.frontendURL}/events/${event.slug}/register/${registration.id}`);
    await page.getByRole("checkbox").click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(/clause 1\./i)).toBeVisible();

    // While the modal's box is unticked there is no Agree BUTTON — the affordance
    // is rendered as an aria-disabled span, so it stays visible and legible while
    // not being actionable. Asserting on the role is what distinguishes the two.
    await expect(dialog.getByRole("checkbox")).not.toBeChecked();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toHaveCount(0);

    await readTermsToTheEnd(page);
    await expect(dialog.getByRole("checkbox")).toBeChecked();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toBeVisible();
  });

  /**
   * Spec 022 R214 / FR-014c, FR-045 (clarified 2026-08-21).
   *
   * The registration surface must offer the same manual route booking does, or
   * the two have drifted on what agreeing means — which is the whole subject of
   * User Story 5. This never scrolls, against a long document.
   */
  test("a guest can agree without reading, by ticking the box in the modal", async ({
    page,
  }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-tick-unread",
    });

    await page.goto(`${config.frontendURL}/events/${event.slug}/register/${registration.id}`);
    await page.getByRole("checkbox").click();

    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    // Still at the top of a long document. Nothing has been read.
    await expect(dialog.getByText(/clause 1\./i)).toBeVisible();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toHaveCount(0);

    await dialog.getByRole("checkbox").click();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toBeVisible();

    // Accepting carries back to the form with its own box ticked (FR-014e).
    await dialog.getByRole("button", { name: /^agree$/i }).click();
    await expect(dialog).toBeHidden();
    await expect(formAgreementBox(page)).toBeChecked();
  });

  /**
   * Spec 022 R214 / FR-051 (clarified 2026-08-21).
   *
   * When an admin republishes the terms under a guest who already accepted, the
   * submission is refused — and the form RE-PRESENTS the current document by
   * itself, without waiting to be asked. This is the one place in the feature
   * where the app opens the modal rather than the guest, and it is deliberate:
   * they did not choose to lose their consent, so they are shown what changed
   * rather than left to work out why the box emptied.
   *
   * They still need not read it. Ticking and agreeing again is enough — what is
   * required is a fresh, deliberate acceptance of the CURRENT version.
   */
  test("a republished document re-opens itself and must be agreed to again", async ({
    page,
  }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-reopens",
    });
    const before = await quotaOf(registration.id);

    await page.goto(`${config.frontendURL}/events/${event.slug}/register/${registration.id}`);
    await fillRegistrationForm(page, HOLDER.email);
    await acceptTerms(page);
    await expect(formAgreementBox(page)).toBeChecked();

    // The server compares the version to WHOLE-SECOND precision, so a republish
    // inside the same second as the guest's acceptance is not detected. Crossing
    // the boundary is what makes this deterministic.
    await new Promise((r) => setTimeout(r, 1_200));
    await putLongTerms(token, event.id);

    await page.getByRole("button", { name: /confirm registration/i }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: /confirm & submit/i })
      .click();

    // Refused: nothing registered, no quota moved, and the guest is still here.
    await expect(page).not.toHaveURL(/\/register\/[^/]+\/success$/);
    expect(await quotaOf(registration.id)).toBe(before);

    // And the terms are back on screen without the guest asking for them.
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByText(/clause 1\./i)).toBeVisible();
    await expect(dialog.getByRole("checkbox")).not.toBeChecked();

    // Agreeing again — by ticking, without reading — lets the registration through.
    await dialog.getByRole("checkbox").click();
    await dialog.getByRole("button", { name: /^agree$/i }).click();
    await page.getByRole("button", { name: /confirm registration/i }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: /confirm & submit/i })
      .click();
    await page.waitForURL(/\/register\/[^/]+\/success$/, { timeout: 30_000 });
    expect(await quotaOf(registration.id)).toBe(before - 1);
  });

  // FR-046 / SC-012: the automatic tick must fire for a keyboard-only guest. An
  // implementation that listens only for pointer scrolling would deny them the
  // convenience every mouse user gets, and would look finished to a mouse user.
  test("a keyboard-only guest can reach the end and accept", async ({ page }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-keyboard",
    });

    await page.goto(`${config.frontendURL}/events/${event.slug}/register/${registration.id}`);
    await page.getByRole("checkbox").click();

    const dialog = page.getByRole("dialog");
    const region = dialog.getByRole("region", { name: /terms and conditions/i });
    await expect(region).toBeVisible();
    await expect(dialog.getByRole("button", { name: /^agree$/i })).toHaveCount(0);

    // Focus the scroll region and drive it with the keyboard alone. tabIndex={0}
    // on that div is what makes this possible; without it End never reaches the
    // element and this scenario is the only thing that would notice.
    await region.focus();
    await expect(async () => {
      await page.keyboard.press("End");
      await expect(dialog.getByRole("button", { name: /^agree$/i })).toBeVisible({
        timeout: 1_000,
      });
    }).toPass({ timeout: 15_000 });
  });
});

test.describe("The registration endpoint refuses what the form would not offer", () => {
  // US3 scenario 2 / SC-005: hiding a form is not enforcement. Every refusal is
  // the SAME refusal (FR-012), so a caller cannot enumerate ticket types.
  test("a purchasable ticket type is refused, identically to an unknown one", async () => {
    const { event, purchasable } = await createRegistrationEvent(token, {
      slug: "invitation-seam",
    });
    const before = await quotaOf(purchasable.id);

    const unknown = "00000000-0000-4000-8000-000000000000";
    const body = JSON.stringify({
      slug: event.slug,
      name: HOLDER.name,
      email: HOLDER.email,
      phone: HOLDER.phone,
      dob: "1996-04-12",
      gender: "MALE",
      agreed: true,
      event_terms_updated_at: new Date().toISOString(),
    });

    // requestRaw, not request: request unwraps the envelope to `data`, which is
    // null on every 404, so comparing through it would pass even if the code and
    // message gave the answer away.
    const paid = await requestRaw(`/ticket/register/${purchasable.id}`, { method: "POST", body });
    const missing = await requestRaw(`/ticket/register/${unknown}`, { method: "POST", body });

    expect(paid.status).toBe(404);
    expect(missing.status).toBe(404);
    expect(paid.body).toEqual(missing.body);

    // Nothing written, no quota moved.
    expect(await quotaOf(purchasable.id)).toBe(before);
  });

  // US2 / SC-004: containment holds on the guest purchase surface, in both cache
  // modes — this assertion runs identically with E2E_CACHE_ENABLED=false.
  test("a registration-only type never appears on the guest ticket list", async ({ page }) => {
    const { event, purchasable, registration } = await createRegistrationEvent(token, {
      slug: "invitation-hidden",
    });

    const listed = await publicTicketTypes(event.slug);
    const ids = listed.map((t) => t.id);
    expect(ids).toContain(purchasable.id);
    expect(ids).not.toContain(registration.id);

    // The selection page is where ticket types are listed; the detail page is a
    // landing page and lists none, so asserting there would pass vacuously.
    await page.goto(`${config.frontendURL}/events/${event.slug}/tickets`);
    await expect(page.getByText(purchasable.name).first()).toBeVisible();
    await expect(page.getByText("Invitation Access")).toHaveCount(0);
  });

  // FR-007: and it cannot be bundled. The server refuses the composition even
  // though the admin picker no longer offers it.
  test("a registration-only type cannot be added to a package", async () => {
    const { event, purchasable, registration } = await createRegistrationEvent(token, {
      slug: "invitation-package",
    });

    const { status } = await request("/admin/packages", {
      method: "POST",
      token,
      body: JSON.stringify({
        event_id: event.id,
        name: "Bundle With An Invitation",
        price: "150000.00",
        sales_start: new Date(Date.now() - 86_400_000).toISOString(),
        sales_end: new Date(Date.now() + 86_400_000).toISOString(),
        is_active: true,
        components: [
          { ticket_type_id: purchasable.id },
          { ticket_type_id: registration.id },
        ],
      }),
    });

    expect(status).toBe(400);
  });
});

// The registration BURST default is deliberately not asserted here.
//
// It was, and it destabilised this file. The limiter keys on the client address,
// so every scenario in here shares one allowance: 10 burst refilling at 0.2/s. A
// scenario spending five of those left the rest of the file racing the refill,
// and an unrelated assertion would intermittently get a 429 where it expected a
// 201 or a 404 — which is precisely what tasks.md T080 warns against, a drained
// allowance refusing a scenario that was not testing throttling.
//
// The shipped default is pinned in the Go tier instead
// (pkg/config: TestRegistrationBurstAdmitsAWholeGroup, and
// TestRefillWindowMatchesTheShippedValues for the FR-034a retention invariant),
// where it is a configuration fact and needs no HTTP round trips to establish.
// What is left here is the behaviour only the assembled system can show.

/** The version token the form would have read, fetched the way the form does. */
async function currentTermsUpdatedAt(slug: string): Promise<string> {
  const { data } = await request<{ updated_at: string }>(
    `/ticket/terms-condition/${encodeURIComponent(slug)}`,
  );
  return data.updated_at;
}


test.describe("A registration's ticket is an ordinary ticket", () => {
  // SC-003 / FR-028. The code in the e-ticket must open the door. A registration
  // that produced a document nobody could scan would satisfy every other
  // assertion in this file and still be useless.
  test("the issued code validates as Valid at the door", async ({ page }) => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-validates",
      // The ticket must be INSIDE its admission window, or validation correctly
      // answers NOT_YET_VALID and this scenario would be asserting the wrong
      // thing about a working system (spec 015 FR-014: no tolerance).
      admittingNow: true,
    });

    await page.goto(`${config.frontendURL}/events/${event.slug}/register/${registration.id}`);
    await fillRegistrationForm(page, HOLDER.email);
    await acceptTerms(page);
    await page.getByRole("button", { name: /confirm registration/i }).click();
    await page
      .getByRole("dialog")
      .getByRole("button", { name: /confirm & submit/i })
      .click();
    await page.waitForURL(/\/register\/[^/]+\/success$/, { timeout: 30_000 });

    // Issuance runs after the response, so the code appears shortly afterwards.
    const code = await waitForRegistrationTicketCode(HOLDER.email);
    expect(await ticketStatusOf(code)).toBe("ACTIVE");

    const admin = new AdminConsole(page);
    await admin.signIn(config.admin.email, config.admin.password);
    await admin.openValidation();

    const form = page.getByRole("form", { name: /validate ticket/i });
    await form.getByRole("textbox").fill(code);
    await form.getByRole("button", { name: "Check ticket" }).click();
    await expect(page.getByText("Valid", { exact: true })).toBeVisible();
  });

  // FR-030 / SC-006: quota is the real bound now that no per-address rule remains.
  test("registration is refused once quota is exhausted, with nothing written", async () => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-quota",
      registrationQuota: 1,
    });

    const body = (email: string) =>
      JSON.stringify({
        slug: event.slug,
        name: HOLDER.name,
        email,
        phone: HOLDER.phone,
        dob: "1996-04-12",
        gender: "MALE",
        agreed: true,
        event_terms_updated_at: undefined,
      });

    const updatedAt = await currentTermsUpdatedAt(event.slug);
    const withTerms = (email: string) =>
      JSON.stringify({ ...JSON.parse(body(email)), event_terms_updated_at: updatedAt });

    const first = await requestRaw(`/ticket/register/${registration.id}`, {
      method: "POST",
      body: withTerms("first@example.com"),
    });
    expect(first.status).toBe(201);
    expect(await quotaOf(registration.id)).toBe(0);

    const second = await requestRaw(`/ticket/register/${registration.id}`, {
      method: "POST",
      body: withTerms("second@example.com"),
    });

    // 404, not 400 INSUFFICIENT_QUOTA — and that is correct.
    //
    // "No places left" is one of the conditions FR-011 refuses to render a form
    // for, and FR-012 makes every one of those refusals identical so an
    // unauthenticated caller cannot enumerate ticket types. A quota already at
    // zero is therefore resolved away before the transaction is reached.
    //
    // The contract's 400 INSUFFICIENT_QUOTA covers the other case: quota
    // exhausted BETWEEN page load and submit, which only the transaction can
    // detect. Both are refusals that write nothing; only their timing differs.
    expect(second.status).toBe(404);
    expect(await quotaOf(registration.id)).toBe(0);
  });

  // FR-050/FR-051: a submission carrying a superseded terms version is refused
  // rather than silently binding the guest to text they never saw.
  test("a superseded terms version is refused", async () => {
    const { event, registration } = await createRegistrationEvent(token, {
      slug: "invitation-stale-terms",
    });
    const stale = await currentTermsUpdatedAt(event.slug);

    // The version comparison truncates to WHOLE SECONDS on the server, so a
    // republish landing in the same second as the original read is not detected.
    // Waiting past the second boundary is what makes this scenario deterministic
    // rather than a coin toss — and the wait is documenting a real, if narrow,
    // limitation of the version check rather than working around a flaky test.
    await new Promise((r) => setTimeout(r, 1_200));

    // Republish. The row is updated in place and KEEPS its id, which is exactly
    // why the version token is `updated_at` and not the id.
    await putLongTerms(token, event.id);

    const { status } = await requestRaw(`/ticket/register/${registration.id}`, {
      method: "POST",
      body: JSON.stringify({
        slug: event.slug,
        name: HOLDER.name,
        email: HOLDER.email,
        phone: HOLDER.phone,
        dob: "1996-04-12",
        gender: "MALE",
        agreed: true,
        event_terms_updated_at: stale,
      }),
    });

    expect(status).toBe(409);
    expect(await quotaOf(registration.id)).toBe(5);
  });
});

/** The ticket code issued for a registration, read once issuance has committed. */
async function waitForRegistrationTicketCode(email: string): Promise<string> {
  const deadline = Date.now() + 30_000;
  while (Date.now() < deadline) {
    const { rows } = await db().query<{ ticket_code: string }>(
      `SELECT t.ticket_code
         FROM tickets t
         JOIN attendees a ON a.id = t.attendee_id
        WHERE lower(a.email) = lower($1)
        LIMIT 1`,
      [email],
    );
    if (rows.length > 0) return rows[0].ticket_code;
    await new Promise((r) => setTimeout(r, 300));
  }
  throw new Error(`no ticket issued for ${email} within 30s`);
}

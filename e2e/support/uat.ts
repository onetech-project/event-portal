/**
 * What a run needs when it drives a deployment it does not own.
 *
 * The local suite arranges state by emptying a database and refilling it. None
 * of that is available here, and the difference is not merely mechanical — it
 * changes what a test is allowed to assume:
 *
 *   - The catalogue is not empty and never will be. Nothing may assert on list
 *     lengths, "the first event", or totals; every read is scoped to the slug
 *     this run created.
 *   - Fees, admins and master data already exist and belong to somebody else.
 *     They are read, never created or deleted.
 *   - Whatever this run creates outlives it. An event with orders against it is
 *     undeletable by design, so teardown unpublishes rather than promising a
 *     clean slate it cannot deliver.
 *
 * Everything created here is prefixed and stamped with a run id so the residue
 * is identifiable months later by someone who was not here.
 */

import { expect } from "@playwright/test";

import {
  adminLogin,
  createSellableEvent,
  deleteEvent,
  deleteTicketType,
  publicOrder,
  publicTicketTypes,
  updateEvent,
  type PublicOrder,
  type SeededEvent,
  type SeededTicketType,
} from "./api";
import { config } from "./env";

/**
 * Identifies one run's data. Time-ordered so residue sorts chronologically, with
 * a random tail because two runs can start inside the same second.
 */
export const runId = `${new Date().toISOString().slice(2, 16).replace(/[-:T]/g, "")}-${Math.random()
  .toString(36)
  .slice(2, 6)}`;

/** e.g. `uat-e2e-2608121530-k3f9-purchase`. */
export function runSlug(suffix: string): string {
  return `${config.uatPrefix}-${runId}-${suffix}`;
}

type Created = {
  event: SeededEvent;
  ticketType: SeededTicketType;
  /** Set once anything has been booked against it, which makes it undeletable. */
  hasOrders: boolean;
};

const created: Created[] = [];

/**
 * Signs in as the deployment's existing admin.
 *
 * Fails loudly and specifically: on a deployment a 401 here means the account
 * does not exist or the password is stale, which is a configuration problem the
 * runner cannot fix by retrying, and every later failure would be a confusing
 * echo of it.
 */
export async function uatAdminToken(): Promise<string> {
  try {
    return await adminLogin();
  } catch (cause) {
    throw new Error(
      `Could not sign in to ${config.apiURL} as ${config.admin.email}. ` +
        `The uat profile never creates admins — this account must already exist there. ` +
        `Check E2E_ADMIN_EMAIL / E2E_ADMIN_PASSWORD in e2e/.env.uat.\n  ${String(cause)}`,
    );
  }
}

/**
 * Creates one published event with terms and a single ticket type, tracked for
 * teardown.
 *
 * The quota is deliberately tiny. It is real inventory on a shared environment,
 * and a run that reserves ten thousand seats to buy two is rude to whoever
 * looks at the numbers tomorrow.
 */
export async function disposableEvent(
  token: string,
  options: { suffix: string; quota?: number; price?: string; ticketName?: string } = {
    suffix: "run",
  },
): Promise<{ event: SeededEvent; ticketType: SeededTicketType }> {
  const slug = runSlug(options.suffix);

  const seeded = await createSellableEvent(token, {
    slug,
    name: `[E2E ${runId}] ${options.suffix}`,
    quota: options.quota ?? 4,
    price: options.price ?? "150000.00",
    ticketName: options.ticketName ?? "Regular",
    status: "PUBLISHED",
  });

  created.push({ ...seeded, hasOrders: false });
  return seeded;
}

/**
 * Records that inventory has been taken against an event, so teardown does not
 * bother attempting a delete the server will refuse.
 */
export function markBookedAgainst(eventId: string): void {
  const entry = created.find((c) => c.event.id === eventId);
  if (entry) entry.hasOrders = true;
}

/**
 * Best-effort cleanup, in the only order the guards allow: ticket type first,
 * then event.
 *
 * Deletion is attempted only where nothing has been bought. Everywhere else the
 * event is unpublished, which is the honest end state — the rows must stay for
 * referential integrity, but `status='PUBLISHED'` is what the public catalogue
 * filters on (ListPublishedEvents), so an unpublished event is invisible to
 * anyone browsing the deployment.
 *
 * Never throws. A failed teardown must not turn a green run red or mask the
 * real failure of a red one; it reports and moves on.
 */
export async function teardownCreated(token: string): Promise<string[]> {
  const notes: string[] = [];

  for (const entry of created.splice(0)) {
    const { event, ticketType, hasOrders } = entry;

    try {
      if (!hasOrders) {
        const ticketTypeResult = await deleteTicketType(token, ticketType.id);
        const eventResult = await deleteEvent(token, event.id);

        if (eventResult.status === 204) {
          notes.push(`deleted ${event.slug}`);
          continue;
        }
        notes.push(
          `delete refused for ${event.slug} (ticket type ${ticketTypeResult.status}, ` +
            `event ${eventResult.status}) — unpublishing instead`,
        );
      }

      const unpublished = await updateEvent(token, event.id, {
        name: `[E2E ${runId} spent] ${event.name}`,
        slug: event.slug,
        status: "DRAFT",
      });
      notes.push(
        unpublished.status === 200
          ? `unpublished ${event.slug}`
          : `COULD NOT unpublish ${event.slug} (${unpublished.status}) — still visible publicly`,
      );
    } catch (cause) {
      notes.push(`teardown error for ${event.slug}: ${String(cause)}`);
    }
  }

  return notes;
}

// --- Reads that stand in for the database ----------------------------------

/**
 * Remaining quota as the public catalogue reports it.
 *
 * This is `ticket_types.quota` — already the remaining figure, not an
 * allocation — read through the same endpoint a guest's browser reads, which
 * means it also proves the cache was invalidated rather than merely that the
 * row changed.
 */
export async function quotaRemaining(slug: string, ticketTypeId: string): Promise<number> {
  const types = await publicTicketTypes(slug);
  const found = types.find((t) => t.id === ticketTypeId);
  if (!found) throw new Error(`ticket type ${ticketTypeId} is not listed under ${slug}`);
  return found.quota_remaining;
}

/**
 * Polls the guest order read until it reaches `status`.
 *
 * Ticket issuance, the PDF and the email run in a goroutine after the callback
 * has already answered 200, so the status is eventually consistent by design
 * and polling is the honest way to observe it. A deployment is also across a
 * network from here, which is the other reason not to sleep a guess.
 */
export async function waitForOrderStatus(
  orderNumber: string,
  status: string,
  { timeoutMs = 60_000, intervalMs = 1_000 } = {},
): Promise<PublicOrder> {
  const deadline = Date.now() + timeoutMs;
  let last: PublicOrder | undefined;

  while (Date.now() < deadline) {
    last = await publicOrder(orderNumber);
    if (last.status === status) return last;
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }

  throw new Error(
    `order ${orderNumber} was still ${last?.status ?? "unreadable"} after ${timeoutMs}ms, expected ${status}`,
  );
}

/**
 * Fails the run before it can do damage if the profile is wrong.
 *
 * Called from the uat setup. Without it, a `.env.uat` still holding localhost
 * would run the whole suite against a dev stack and report the deployment
 * healthy — the same class of mistake in the opposite direction as pointing the
 * local suite at a deployment.
 */
export function assertRemoteProfile(): void {
  expect(
    config.remote,
    "the uat suite must run with E2E_TARGET=uat (use `npm run test:uat`)",
  ).toBe(true);
}

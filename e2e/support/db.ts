/**
 * Direct database access, used for exactly two things:
 *
 *   1. resetting to a known-empty state before a run, and
 *   2. asserting on state the UI and API deliberately do not expose
 *      (quota counters, email_sent, ticket_code).
 *
 * It is NOT used to fabricate application state. Orders are created by booking
 * through the UI, and payments move by delivering a signed webhook — because a
 * UAT that reaches into the database to set up its own preconditions stops
 * testing the paths it claims to cover.
 */

import pg from "pg";

import { assertDisposableDatabase, config } from "./env";

const { Pool } = pg;

let pool: pg.Pool | undefined;

export function db(): pg.Pool {
  if (config.remote) {
    // Not a missing feature. The first thing anything here does is TRUNCATE, and
    // a deployment shares its database with data nobody wants back. The uat
    // profile therefore has no connection string at all (support/env.ts), and
    // this refusal makes an accidental import fail loudly at the call rather
    // than quietly at `new URL(undefined)` three frames down.
    throw new Error(
      "support/db.ts is unavailable when E2E_TARGET=uat: the suite has no database access " +
        "to a deployment. Assert through the API instead — see uat/README or support/uat.ts.",
    );
  }

  if (!pool) {
    assertDisposableDatabase(config.databaseURL);
    pool = new Pool({ connectionString: config.databaseURL, max: 4 });
  }
  return pool;
}

export async function closeDb(): Promise<void> {
  await pool?.end();
  pool = undefined;
}

/**
 * Empties every application table.
 *
 * `genders` is deliberately absent: it is master data seeded by migration 0012,
 * and the visitor form's gender select would be empty without it. Same for
 * `schema_migrations`, which is golang-migrate's bookkeeping.
 */
export async function resetDatabase(): Promise<void> {
  await db().query(`
    TRUNCATE tickets, payments, attendees, order_fees, order_items, orders,
             package_tickets, packages, ticket_types, events, admins, fees
    RESTART IDENTITY CASCADE
  `);
}

/** Seeds the one admin the suite signs in as. */
export async function seedAdmin(): Promise<void> {
  await db().query(
    `INSERT INTO admins (email, password_hash)
     VALUES ($1, $2)
     ON CONFLICT (email) DO UPDATE SET password_hash = EXCLUDED.password_hash`,
    [config.admin.email, config.admin.passwordHash],
  );
}

// --- Assertions the API does not expose ------------------------------------

export async function quotaOf(ticketTypeId: string): Promise<number> {
  const { rows } = await db().query<{ quota: number }>(
    `SELECT quota FROM ticket_types WHERE id = $1`,
    [ticketTypeId],
  );
  if (rows.length === 0) throw new Error(`no ticket type ${ticketTypeId}`);
  return Number(rows[0].quota);
}

/**
 * Order status lives in a master list since migration 0013: `orders.status_id`
 * references `order_statuses`. Every read here joins rather than assuming the
 * ids, so a reordered master list cannot silently turn PAID into CANCELLED.
 */
export async function orderStatusOf(orderNumber: string): Promise<string> {
  const { rows } = await db().query<{ status: string }>(
    `SELECT s.name AS status
       FROM orders o
       JOIN order_statuses s ON s.id = o.status_id
      WHERE o.order_number = $1`,
    [orderNumber],
  );
  if (rows.length === 0) throw new Error(`no order ${orderNumber}`);
  return rows[0].status;
}

export async function orderRow(orderNumber: string) {
  const { rows } = await db().query(
    `SELECT o.id, o.order_number, s.name AS status, o.email_sent,
            o.buyer_name, o.buyer_email, o.total_amount
       FROM orders o
       JOIN order_statuses s ON s.id = o.status_id
      WHERE o.order_number = $1`,
    [orderNumber],
  );
  if (rows.length === 0) throw new Error(`no order ${orderNumber}`);
  return rows[0];
}

/** One ticket is issued per attendee on PAID; these are what the guest receives. */
export async function ticketCodesFor(orderNumber: string): Promise<string[]> {
  const { rows } = await db().query<{ ticket_code: string }>(
    `SELECT t.ticket_code
       FROM tickets t
       JOIN orders o ON o.id = t.order_id
      WHERE o.order_number = $1
      ORDER BY t.ticket_code`,
    [orderNumber],
  );
  return rows.map((r) => r.ticket_code);
}

export async function attendeeCountFor(orderNumber: string): Promise<number> {
  const { rows } = await db().query<{ count: string }>(
    `SELECT count(*)::text AS count
       FROM attendees a
       JOIN orders o ON o.id = a.order_id
      WHERE o.order_number = $1`,
    [orderNumber],
  );
  return Number(rows[0].count);
}

/**
 * Waits for a condition the API completes asynchronously — ticket issuance and
 * email dispatch run in a non-blocking goroutine after the webhook answers 200,
 * so polling is the honest way to observe them rather than sleeping a guess.
 */
export async function waitFor<T>(
  probe: () => Promise<T>,
  predicate: (value: T) => boolean,
  { timeoutMs = 15_000, intervalMs = 200, what = "condition" } = {},
): Promise<T> {
  const deadline = Date.now() + timeoutMs;
  let last: T | undefined;

  while (Date.now() < deadline) {
    last = await probe();
    if (predicate(last)) return last;
    await new Promise((resolve) => setTimeout(resolve, intervalMs));
  }

  throw new Error(`timed out after ${timeoutMs}ms waiting for ${what}; last value: ${JSON.stringify(last)}`);
}

/** ACTIVE | USED | REVOKED — the door's view of a ticket. */
export async function ticketStatusOf(code: string): Promise<string> {
  const { rows } = await db().query<{ status: string }>(
    `SELECT status FROM tickets WHERE ticket_code = $1`,
    [code],
  );
  if (rows.length === 0) throw new Error(`no ticket ${code}`);
  return rows[0].status;
}

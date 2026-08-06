/**
 * Booking-step selection logic, kept free of React so it can be tested directly.
 *
 * The server remains the authority on both price and availability: everything
 * here is presentational or a client-side convenience. A figure shown in the list
 * may already be stale by the time the guest checks out, which is why checkout
 * re-validates and re-prices from stored data.
 */

import type {
  CheckoutItemInput,
  PackageSummary,
  SelectableItem,
  SelectionLine,
  TicketTypeSummary,
} from "./types";

/**
 * Merges an event's tickets and bundles into the single list the designs show,
 * ordered by price then name. Bundles are peers of tickets here, not a separate
 * section — only their badge distinguishes them.
 */
export function buildSelectableItems(
  ticketTypes: TicketTypeSummary[],
  packages: PackageSummary[],
): SelectableItem[] {
  const items: SelectableItem[] = [
    ...ticketTypes.map(
      (ticket): SelectableItem => ({ kind: "ticket", id: ticket.id, ticket }),
    ),
    ...packages.map((pkg): SelectableItem => ({ kind: "package", id: pkg.id, pkg })),
  ];

  return items.sort((a, b) => {
    const priceDelta = Number(priceOf(a)) - Number(priceOf(b));
    if (priceDelta !== 0) return priceDelta;
    return nameOf(a).localeCompare(nameOf(b));
  });
}

export function nameOf(item: SelectableItem): string {
  return item.kind === "ticket" ? item.ticket.name : item.pkg.name;
}

export function priceOf(item: SelectableItem): string {
  return item.kind === "ticket" ? item.ticket.price : item.pkg.price;
}

/**
 * The notice line for a row: the admin-authored description when there is one,
 * otherwise null and the row shows no notice at all.
 *
 * Blank-but-present descriptions are treated as absent — the server already
 * normalises whitespace-only input to null, and this guards the case where an
 * older row slipped through.
 */
export function noticeOf(item: SelectableItem): string | null {
  const description =
    item.kind === "ticket" ? item.ticket.description : item.pkg.description;

  return description?.trim() ? description.trim() : null;
}

/**
 * The stepper ceiling for a row: remaining quota for a ticket, and whole
 * available sets for a bundle. A bundle owns no inventory, so `available_units`
 * is the server's derived figure, not a stored one. No artificial ceiling is
 * applied — the real limit is whatever the server last reported.
 */
export function maxQuantityFor(item: SelectableItem): number {
  const supply =
    item.kind === "ticket" ? item.ticket.quota_remaining : item.pkg.available_units;
  return Math.max(0, supply);
}

export type Availability =
  | { available: true; label: string }
  | { available: false; label: string };

/**
 * Why a row can or cannot be selected right now.
 *
 * For a package the server has already folded every gate into `purchasable`
 * (units, status, its own window, and every constituent's window), so the client
 * reads one boolean rather than re-deriving a rule it could get subtly wrong.
 */
export function availabilityOf(item: SelectableItem, now: number): Availability {
  if (item.kind === "package") {
    return packageAvailability(item.pkg);
  }
  return ticketAvailability(item.ticket, now);
}

function packageAvailability(pkg: PackageSummary): Availability {
  if (pkg.available_units <= 0) {
    return { available: false, label: "Sold out" };
  }
  if (!pkg.purchasable) {
    return { available: false, label: "Not available" };
  }
  return { available: true, label: `${pkg.available_units} bundle(s) remaining` };
}

function ticketAvailability(ticket: TicketTypeSummary, now: number): Availability {
  if (ticket.quota_remaining <= 0) {
    return { available: false, label: "Sold out" };
  }
  if (new Date(ticket.sales_start).getTime() > now) {
    return { available: false, label: "Sales have not opened yet" };
  }
  if (new Date(ticket.sales_end).getTime() < now) {
    return { available: false, label: "Sales have closed" };
  }
  return { available: true, label: `${ticket.quota_remaining} remaining` };
}

/** Selection state: row id → chosen quantity. Absent or 0 means unselected. */
export type Quantities = Record<string, number>;

/**
 * The summary panel's lines, in list order.
 *
 * A selected bundle contributes exactly ONE line under its own name and price —
 * never two lines for the days inside it. That is what the guest bought.
 */
export function selectionLines(
  items: SelectableItem[],
  quantities: Quantities,
): SelectionLine[] {
  return items
    .filter((item) => (quantities[item.id] ?? 0) > 0)
    .map((item) => ({
      kind: item.kind,
      id: item.id,
      name: nameOf(item),
      unitPrice: priceOf(item),
      quantity: quantities[item.id],
    }));
}

/**
 * The displayed total. Presentational only — the server recomputes the charged
 * amount from its own stored prices and ignores anything the client sends.
 */
export function selectionTotal(lines: SelectionLine[]): number {
  return lines.reduce((sum, line) => sum + Number(line.unitPrice) * line.quantity, 0);
}

/** Selected units across all lines. A bundle counts as its own units, not its parts. */
export function totalUnits(lines: SelectionLine[]): number {
  return lines.reduce((sum, line) => sum + line.quantity, 0);
}

/** The checkout request's `items`, with the server's XOR preserved. */
export function checkoutItems(lines: SelectionLine[]): CheckoutItemInput[] {
  return lines.map((line) =>
    line.kind === "package"
      ? { package_id: line.id, quantity: line.quantity }
      : { ticket_type_id: line.id, quantity: line.quantity },
  );
}

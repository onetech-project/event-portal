/**
 * The three addresses an order occupies inside its event's journey.
 *
 * Spec 011 FR-020 gave the QR payment screen an address of its own so each
 * progress stage maps to exactly one address — while the forms and the QR
 * shared `/orders/{orderNumber}`, the rail could only name one of the two
 * stages and always named the wrong one on the forms.
 *
 * They live together here because three screens forward to one another
 * (FR-021) and `bookingStageFromPathname` has to recognise the same shapes:
 * a path built by hand in one place and matched by a regex in another is how
 * the stage drifts from the address again.
 */

const eventBase = (eventSlug: string, orderNumber: string) =>
  `/events/${encodeURIComponent(eventSlug)}/orders/${encodeURIComponent(orderNumber)}`;

/** Ticket holder forms — the Registration stage. */
export function orderFormsPath(eventSlug: string, orderNumber: string): string {
  return eventBase(eventSlug, orderNumber);
}

/** The QRIS screen — the Payment stage. */
export function orderCheckoutPath(eventSlug: string, orderNumber: string): string {
  return `${eventBase(eventSlug, orderNumber)}/checkout`;
}

/** The confirmation — the Success stage. */
export function orderDonePath(eventSlug: string, orderNumber: string): string {
  return `${eventBase(eventSlug, orderNumber)}/success`;
}

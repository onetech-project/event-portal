/**
 * The addresses an event's purchase journey occupies: the three an order moves
 * between, plus the event's own page they all fall back to.
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

/**
 * The event's own page.
 *
 * Not an order address, but it belongs beside them rather than interpolated at
 * each use, for the same reason the rest of this module exists: it already has
 * more readers than writers. `bookingStageFromPathname` matches this shape to
 * decide a path is the Booking stage, and `EventFrame` keys its "no progress
 * rail here" case off it. Spec 019 FR-003 added a third reader — where the
 * end-of-journey dialog sends a guest whose order ended without a purchase.
 */
export function eventDetailPath(eventSlug: string): string {
  return `/events/${encodeURIComponent(eventSlug)}`;
}

const eventBase = (eventSlug: string, orderNumber: string) =>
  `${eventDetailPath(eventSlug)}/orders/${encodeURIComponent(orderNumber)}`;

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

import { type BookingStep } from "@/components/booking/booking-steps";

/**
 * Which stage of the purchase journey a path represents.
 *
 * Derived from the URL rather than declared by each screen, so a screen cannot
 * show a stage that contradicts its own address (spec FR-005). The previous
 * arrangement — one hardcoded `current="Booking"` on the event page — is exactly
 * the drift this prevents: stages 2 to 4 were never shown to anyone.
 *
 * Only paths under `/events/{slug}` are meaningful here, since that is the only
 * subtree the event layout wraps. Anything else falls back to the first stage.
 */
export function bookingStageFromPathname(pathname: string): BookingStep {
  // Everything after `/events/{slug}`. The slug itself is skipped rather than
  // matched, so a slug like "checkout" or "done" cannot masquerade as a stage.
  const match = /^\/events\/[^/]+(\/.*)?$/.exec(stripTrailingSlash(pathname));
  if (!match) return "Booking";

  const rest = match[1] ?? "";

  // Order matters: the bare order path is a prefix of both nested ones, so
  // testing for it first would leave the last two stages unreachable.
  if (/^\/orders\/[^/]+\/done$/.test(rest)) return "Done";
  // Spec 011 FR-020: the QR screen has its own address, so Registration and
  // Payment are finally distinguishable. Before that both phases lived at the
  // bare order path and the rail had to call them all Payment — which read as
  // "Registration finished" while the guest was still filling the forms.
  if (/^\/orders\/[^/]+\/checkout$/.test(rest)) return "Payment";
  if (/^\/orders\/[^/]+$/.test(rest)) return "Registration";

  // "" is the detail page, "/tickets" the selection — both are Booking.
  return "Booking";
}

function stripTrailingSlash(pathname: string): string {
  return pathname.length > 1 && pathname.endsWith("/") ? pathname.slice(0, -1) : pathname;
}

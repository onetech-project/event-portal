/**
 * Wording for a refused purchase, kept out of React so it can be tested
 * directly — the same reason `selection.ts` is a plain module.
 *
 * The guiding rule is that the SERVER's sentence wins. It is produced by the
 * same code that produces booking's sentence for the same condition, so a guest
 * refused at the Buy Ticket check and refused again at Agree reads one story
 * rather than two (spec 013 FR-013). Re-wording it here would reintroduce
 * exactly the drift that guarantee exists to prevent.
 *
 * What is left for the client is the case the server cannot word: the check
 * that never reached it.
 */

import { ApiError } from "./api-client";
import type { AvailabilityReason } from "./types";

/**
 * Last-resort wording. Reached only if the server sends a reason with no
 * message — which the contract does not allow — so it exists to keep a raw
 * code off the screen rather than as a real branch.
 */
const UNWORDED_REASON = "This item is no longer available. Please adjust your selection.";

/** A check that never got an answer is not a refusal — it is an unknown. */
export const CHECK_FAILED =
  "We could not check availability just now. Please try again.";

/**
 * The sentence to show for one refusal.
 *
 * Deliberately a pass-through: every code in the contract
 * (INSUFFICIENT_QUOTA, TICKET_TYPE_NOT_ON_SALE, PACKAGE_NOT_ON_SALE,
 * TICKET_TYPE_NOT_FOUND, PACKAGE_NOT_FOUND, VALIDATION_ERROR, TERMS_MISSING)
 * arrives already worded for a guest, and each one differs from the others —
 * which is what FR-012 asks for. A client-side switch over those codes would
 * add a second place for the wording to live and a silent gap the day the
 * server grows a code this file has not heard of.
 */
export function reasonMessage(reason: AvailabilityReason): string {
  const message = reason.message?.trim();
  return message ? message : UNWORDED_REASON;
}

/** Every refusal in a decision, in the order the server reported them. */
export function reasonMessages(reasons: AvailabilityReason[]): string[] {
  return reasons.map(reasonMessage);
}

/**
 * Wording for a thrown failure — the availability check or the booking call.
 *
 * Prefers the server's own message for the same reason `reasonMessage` does:
 * the API's refusals are already guest-facing, and echoing them keeps the
 * booking-time message identical to the check-time one. A transport failure
 * (`status: 0` — the server never answered) is the one case with no server
 * sentence to echo.
 */
export function failureMessage(error: unknown): string {
  if (error === null || error === undefined) return CHECK_FAILED;

  if (error instanceof ApiError) {
    if (error.status === 0) return CHECK_FAILED;
    const message = error.message?.trim();
    if (message) return message;
  }

  return CHECK_FAILED;
}

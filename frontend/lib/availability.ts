/**
 * Wording for a refused purchase, kept out of React so it can be tested
 * directly — the same reason `selection.ts` is a plain module.
 *
 * The rule, since the 2026-08-19 amendment (spec 013 FR-012, FR-013): the guest
 * reads ONE fixed sentence for every availability refusal, and the server's own
 * sentence is never rendered. The server still reports every offending line with
 * its code and its message, and FR-006 keeps that payload deliberately — as the
 * record of why a selection was refused, not as copy. In particular
 * "Only fewer than N ticket(s) remain." must never reach a guest.
 *
 * Three refusals are not an availability race and keep their own wording
 * (FR-012a): no authored terms, a check that never reached the server, and a
 * throttled request. A fourth, a request the selection page could not have
 * produced, keeps its own too (spec Assumptions).
 *
 * Both the pre-check and the booking path resolve through this module, which is
 * what stops the two points telling a guest two different stories.
 */

import { API_CODES, ApiError } from "./api-client";
import type { AvailabilityReason } from "./types";

/**
 * One message as the guest reads it.
 *
 * `title` is optional because only FR-012's message is written as a heading and
 * an explanation; the carve-outs are a single sentence each. The render maps
 * this onto `AlertTitle` + `AlertDescription`.
 */
export type RefusalMessage = { title?: string; body: string };

/**
 * FR-012's message, verbatim and in one place.
 *
 * SC-005 requires it to match character for character wherever it appears, which
 * is only achievable if it appears exactly once.
 */
export const GENERAL_REFUSAL: RefusalMessage = {
  title: "Someone was a bit faster!",
  body:
    "One of your selected tickets is no longer available in this quantity. " +
    "Please refresh the page and adjust your order.",
};

/** FR-012a: the check never got an answer. Not a refusal — an unknown. */
export const CHECK_FAILED: RefusalMessage = {
  body: "We could not check availability just now. Please try again.",
};

/**
 * FR-012a: the guest is being asked to slow down.
 *
 * Worded here rather than left to the server because the rate limiter answers
 * with its middleware's own prose ("rate limit exceeded"), which is not
 * something to show a buyer. The same sentence is used for a throttled booking,
 * so one condition reads one way at both points.
 */
export const THROTTLED: RefusalMessage = {
  body: "Too many attempts. Please wait a moment and try again.",
};

/**
 * The stable string codes that are NOT an availability race.
 *
 * Everything else — `INSUFFICIENT_QUOTA`, `TICKET_TYPE_NOT_ON_SALE`,
 * `PACKAGE_NOT_ON_SALE`, `TICKET_TYPE_NOT_FOUND`, `PACKAGE_NOT_FOUND` — collapses
 * to `GENERAL_REFUSAL`.
 *
 * The polarity matters: an unrecognised code falls through to the general
 * message on purpose. A decision the client cannot classify is still a refusal,
 * and "one of your tickets is no longer available" is the safe thing to say
 * about one — safer than echoing a sentence written for an operator, and safer
 * than staying silent. The day the server grows a code this file has not heard
 * of, the guest is told something true rather than nothing.
 */
const OWN_WORDING_CODES: ReadonlySet<string> = new Set([
  "TERMS_MISSING",
  "VALIDATION_ERROR",
  "INTERNAL_ERROR",
]);

/** Last-resort wording for a carve-out the server sent with an empty message. */
const UNWORDED_REASON = "This selection could not be checked. Please try again.";

/**
 * Every message a refused decision should show, in server order.
 *
 * At most ONE `GENERAL_REFUSAL`, however many lines are at fault — FR-012 makes
 * the count of faults invisible to the guest — plus each distinct carve-out
 * once. A decision can legitimately carry both: `EvaluateAvailability` appends
 * its terms verdict whether or not the lines were refused.
 */
export function refusalMessages(reasons: AvailabilityReason[]): RefusalMessage[] {
  const messages: RefusalMessage[] = [];
  const seenCarveOuts = new Set<string>();
  let generalShown = false;

  for (const reason of reasons) {
    const code = reason.code?.trim();

    if (!code || !OWN_WORDING_CODES.has(code)) {
      if (!generalShown) {
        messages.push(GENERAL_REFUSAL);
        generalShown = true;
      }
      continue;
    }

    if (seenCarveOuts.has(code)) continue;
    seenCarveOuts.add(code);

    const body = reason.message?.trim();
    messages.push({ body: body ? body : UNWORDED_REASON });
  }

  return messages;
}

/**
 * The numeric envelope codes that mean "availability" on a booking refusal.
 *
 * The booking path has no stable string code to read: `apperr.Body` is
 * `{code, message, data}` and the string is never serialised. So this branches
 * on numbers, and `apperr.Numeric` is lossy — see research.md D8 for the whole
 * argument, and for why 400001 is counted as availability despite also carrying
 * the VALIDATION_ERROR family. The short version: the sale-window race is
 * reachable by a real guest and every competing validation error needs a request
 * body the selection page cannot construct.
 */
const BOOKING_AVAILABILITY_CODES: ReadonlySet<number> = new Set([
  API_CODES.validation, // 400001 — the two on-sale codes land here
  API_CODES.insufficientQuota, // 400002
  API_CODES.notFound, // 404001 — ticket type or package gone
]);

/**
 * Whether a thrown booking failure is an availability refusal (FR-013).
 *
 * `TermsDialog` uses this to decide whether to close and hand the refusal up to
 * the selection page, or to keep its own in-dialog alert as it always has.
 */
export function isAvailabilityRefusal(error: unknown): boolean {
  return error instanceof ApiError && BOOKING_AVAILABILITY_CODES.has(error.code);
}

/**
 * Wording for a thrown failure on the CHECK path.
 *
 * A throw here is never an availability verdict — the check reports those in its
 * decision body with `available: false`, and reaches this function only when it
 * got no decision at all. So the answer is one of FR-012a's two "we could not
 * ask" cases, never `GENERAL_REFUSAL`.
 *
 * Deliberately no longer echoes `error.message`. That echo is how the rate
 * limiter's middleware prose reached the alert, and on the booking path it is
 * how "Only fewer than N ticket(s) remain." reached the guest at Agree.
 */
export function failureMessage(error: unknown): RefusalMessage {
  if (error instanceof ApiError && error.code === API_CODES.rateLimited) {
    return THROTTLED;
  }
  return CHECK_FAILED;
}

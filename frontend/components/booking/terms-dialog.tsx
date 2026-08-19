"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
} from "@/components/ui/dialog";
import { API_CODES, ApiError } from "@/lib/api-client";
import {
  GENERAL_REFUSAL,
  THROTTLED,
  type RefusalMessage,
  isAvailabilityRefusal,
} from "@/lib/availability";
import { useBookOrder, useEventTerms, useRecordAgreement } from "@/lib/queries";
import { checkoutItems } from "@/lib/selection";
import type { SelectionLine } from "@/lib/types";

/**
 * The Terms & Conditions gate in front of booking (spec 008).
 *
 * It is a CONTROLLED dialog (spec 013): it no longer owns the Buy Ticket
 * button. The press now runs a server availability check first, and only a
 * clean answer opens this — so the component that awaits that answer is the one
 * that holds `open`. Owning a trigger here would mean blocking its own opening
 * for the duration of a round trip, i.e. lying about its own state.
 *
 * Once open, nothing below changed. The dialog fetches the event's current
 * CMS-authored terms; the Agree click is two calls back-to-back —
 * POST /ticket/book creates the held PENDING order (quota locked, 1-hour hold),
 * then POST /ticket/terms-condition/:order_id records the agreement — and only
 * then does the guest move on to the order page.
 *
 * If either call fails, the order sits held but unagreed: Agree turns into a
 * retry that re-fires only the agreement (the server call is idempotent). A
 * dialog abandoned in that state is reclaimed by the 1-hour sweeper. On the
 * success path the button must never show that retry — see `navigating`.
 *
 * Agreement is deliberately not remembered across openings — closing the dialog
 * discards it, so the guest is never carried onward on a tick they made for an
 * earlier selection.
 */
export function TermsDialog({
  eventId,
  eventSlug,
  eventName,
  lines,
  open,
  onOpenChange,
  onAvailabilityRefusal,
}: {
  /** The event's UUID — what POST /ticket/book identifies the event by. */
  eventId: string;
  /** The event's slug — what the terms read and the order route use. */
  eventSlug: string;
  eventName: string;
  lines: SelectionLine[];
  /** Owned by the caller, which opens this only on a clean availability check. */
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /**
   * Called when booking refuses for an availability reason (spec 013 FR-013a).
   *
   * This dialog closes ITSELF first and then reports upward, so the caller can
   * render the refusal in the same region the pre-check uses. The caller must
   * not close this by flipping `open`: base-ui fires `onOpenChange` only from
   * its own `setOpen`, so a controlled-prop flip would skip `handleOpenChange`
   * and leave `agreed`, the booked order id and the mutations' error state
   * behind for the next opening.
   */
  onAvailabilityRefusal: (message: RefusalMessage) => void;
}) {
  const router = useRouter();
  const [agreed, setAgreed] = useState(false);
  // Set once book succeeds, so a failed agreement retries against the same
  // order instead of booking (and holding quota) twice.
  const [bookedOrderId, setBookedOrderId] = useState<string | null>(null);
  // Both calls have landed and we are on our way to the order page. The routing
  // is asynchronous and this dialog stays mounted until the new segment
  // commits, so without this latch the button would drop out of its busy state
  // mid-navigation and offer a retry for work that already succeeded.
  const [navigating, setNavigating] = useState(false);

  const terms = useEventTerms(eventSlug, open);
  const book = useBookOrder();
  const agreement = useRecordAgreement();

  const busy = book.isPending || agreement.isPending || navigating;
  // Only a real failure earns the retry affordance — not the mere existence of
  // a booked order, which is also true all the way through the success path.
  const failed = book.isError || agreement.isError;

  function handleOpenChange(next: boolean) {
    onOpenChange(next);
    if (!next) {
      setAgreed(false);
      // A booked-but-unagreed order left behind here is intentionally
      // abandoned: checkout refuses it and the hold sweeper reclaims it.
      setBookedOrderId(null);
      setNavigating(false);
      book.reset();
      agreement.reset();
    }
  }

  async function handleAgree() {
    if (!terms.data || busy) return;
    try {
      let orderId = bookedOrderId;
      if (orderId === null) {
        const booked = await book.mutateAsync({
          event_id: eventId,
          items: checkoutItems(lines),
        });
        orderId = booked.order_id;
        setBookedOrderId(orderId);
      }
      await agreement.mutateAsync({ orderId, eventTermsId: terms.data.id });
      // Latched before the push, not after: the mutations are settled from here
      // on, and this is the only thing keeping the button out of its idle state
      // while the order page loads.
      setNavigating(true);
      router.push(
        `/events/${encodeURIComponent(eventSlug)}/orders/${encodeURIComponent(orderId)}`,
      );
    } catch (error) {
      setNavigating(false);
      // The document changed while the guest was reading: show the current
      // version and make them tick again for what they will actually agree to.
      if (error instanceof ApiError && error.code === API_CODES.termsChanged) {
        setAgreed(false);
        void terms.refetch();
        return;
      }
      // The seat went between the passing check and this press (FR-013a). The
      // guest is told to reload and adjust, which they cannot do while this
      // dialog covers the page — so it gets out of the way first, through its
      // own handleOpenChange so the reset actually runs, and hands the refusal
      // to the selection page. bookedOrderId is still null here: an availability
      // refusal throws from `book` itself, before there is an order to abandon.
      if (isAvailabilityRefusal(error)) {
        handleOpenChange(false);
        onAvailabilityRefusal(GENERAL_REFUSAL);
        return;
      }
      // Other failures stay visible through the mutations' error state.
    }
  }

  const errorMessage = agreementErrorMessage(book.error) ?? agreementErrorMessage(agreement.error);

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="flex max-h-[calc(100dvh-6rem)] max-w-144.5 flex-col gap-0 rounded-2xl p-0 sm:max-w-3xl">
        {/* An empty bar the close button sits in, so the rule below it clears
            the button rather than running under it. */}
        <div className="h-15 shrink-0" />

        <div className="flex min-h-0 flex-1 flex-col border-t px-4 pt-4 pb-4">
          <div className="pb-3">
            <p className="text-base leading-6 text-terms-heading">
              Terms &amp; Conditions for:
            </p>
            <DialogTitle className="text-base leading-6 font-bold text-terms-heading">
              {eventName}
            </DialogTitle>
          </div>

          <hr className="border-t" />

          <div className="min-h-0 flex-1 overflow-y-auto py-3.5 text-sm leading-5 text-terms-ink">
            {terms.isPending ? (
              <p className="text-muted-foreground">Loading terms…</p>
            ) : terms.isError ? (
              <p className="text-muted-foreground">
                {terms.error instanceof ApiError &&
                terms.error.code === API_CODES.termsNotAuthored
                  ? "This event's Terms & Conditions are not available yet. Please try again later."
                  : "We could not load the Terms & Conditions. Please close this dialog and try again."}
              </p>
            ) : (
              // Sanitized server-side on write (bluemonday); rendered verbatim.
              <div
                className="terms-content [&_ol]:list-decimal [&_ol]:space-y-3.5 [&_ol]:pl-10 [&_ul]:list-disc [&_ul]:pl-6 [&_p]:pb-3.5"
                dangerouslySetInnerHTML={{ __html: terms.data.content }}
              />
            )}
          </div>

          <hr className="border-t" />

          {errorMessage ? (
            <p role="alert" className="pt-3 text-sm leading-5 text-destructive">
              {errorMessage}
            </p>
          ) : null}

          <label className="flex items-center gap-2 pt-3 text-sm leading-5 text-terms-ink">
            <Checkbox checked={agreed} onCheckedChange={setAgreed} />I agree to
            terms &amp; conditions
          </label>

          <div className="flex gap-8 pt-3">
            <DialogClose className="flex h-12 flex-1 items-center justify-center rounded-lg border border-brand bg-background text-base font-bold text-brand transition-colors hover:bg-brand-surface">
              Cancel
            </DialogClose>

            {agreed && terms.isSuccess ? (
              <button
                type="button"
                onClick={handleAgree}
                disabled={busy}
                className="flex h-12 flex-1 items-center justify-center rounded-lg bg-brand text-base font-bold text-brand-foreground transition-opacity hover:opacity-90 disabled:opacity-60"
              >
                {busy
                  ? navigating
                    ? "Opening your order…"
                    : "Booking…"
                  : failed
                    ? "Retry"
                    : "Agree"}
              </button>
            ) : (
              <span
                aria-disabled="true"
                className="flex h-12 flex-1 items-center justify-center rounded-lg bg-muted text-base font-bold text-muted-foreground"
              >
                Agree
              </span>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/**
 * Words a booking failure for the dialog; null when there is none.
 *
 * Availability refusals never reach here: `handleAgree` intercepts them, closes
 * the dialog and hands them to the selection page (spec 013 FR-013a). So none of
 * the codes below carries an inventory figure, and the server's own sentence is
 * safe to show for the ones the client has nothing better to say about.
 *
 * The two worded here are the ones where the client knows something the server's
 * sentence does not: that the document on screen has been replaced and must be
 * re-read, and that the thing to do about a throttle is wait. The throttle
 * sentence is shared with the availability check, so one condition reads one way
 * at both points (FR-012a).
 */
function agreementErrorMessage(error: unknown): string | null {
  if (error === null || error === undefined) return null;
  if (error instanceof ApiError) {
    if (error.code === API_CODES.termsChanged) {
      return "The Terms & Conditions were updated. Please review the new version and agree again.";
    }
    if (error.code === API_CODES.rateLimited) {
      return THROTTLED.body;
    }
    const message = error.message?.trim();
    if (message) return message;
  }
  return "Something went wrong. Please try again.";
}

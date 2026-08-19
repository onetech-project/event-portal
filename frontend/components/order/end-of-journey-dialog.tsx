"use client";

import Link from "next/link";
import { TriangleAlert } from "lucide-react";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { eventDetailPath } from "@/lib/order-routes";

/**
 * The dead end for an order that ended without a purchase (Figma 293-3).
 *
 * This does NOT replace the screen. Both order screens keep rendering their own
 * layout — progress rail, the half-filled holder forms or the payment panel,
 * and the order summary — and raise this dialog over it (spec 011 FR-022). The
 * guest needs that context most at the moment the order dies: which order,
 * which step, what they had typed.
 *
 * It cannot be dismissed (FR-023), and Base UI has three separate ways out that
 * each have to be closed off:
 *
 *   - the close (X) control     → `showCloseButton={false}`
 *   - backdrop / outside press  → `disablePointerDismissal`
 *   - Escape                    → a controlled `open` that is never lowered
 *
 * The last is why `onOpenChange` is a no-op rather than an inspection of
 * `eventDetails.reason`: ignoring the callback ignores *every* close reason,
 * including any a future Base UI version adds. `modal` is left at its default
 * `true`, which is what makes the page behind inert — focus trapped, document
 * scroll locked, outside pointer events disabled — so none of that is
 * hand-rolled here.
 *
 * Base UI's docs suggest keeping a `Dialog.Close` inside a modal popup so touch
 * screen-reader users have an escape. FR-023 forbids a close control, so the
 * single "Return to Event Page" button is that escape: focusable, inside the
 * trap, and a real way out. Browser navigation is never blocked — this blocks
 * the page, not the browser.
 *
 * Spec 019 FR-003 changed where that button leads and, with it, what it says.
 * It used to deposit the guest on the site home — several steps from the event
 * they were buying for, and with no sign that the seats they just lost were
 * back on sale. It now leads to the event's own page. Nothing else here moved:
 * the guest is still never navigated automatically (FR-005), so this stays a
 * plain link rather than gaining a router call.
 */
export function EndOfJourneyDialog({
  status,
  eventSlug,
}: {
  status: "EXPIRED" | "CANCELLED";
  /**
   * The event whose page the single action leads to.
   *
   * Required rather than optional on purpose (FR-008): an optional slug would
   * let a call site forget it and silently inherit some fallback destination,
   * which is the per-screen divergence this feature had to rule out. Both order
   * screens hold this value already, so there is nothing to thread.
   */
  eventSlug: string;
}) {
  const expired = status === "EXPIRED";

  return (
    <Dialog open onOpenChange={noop} disablePointerDismissal>
      <DialogContent
        showCloseButton={false}
        className="gap-0 rounded-2xl p-8 text-center"
      >
        <span className="mx-auto flex size-16 items-center justify-center rounded-full bg-brand-surface">
          <TriangleAlert aria-hidden className="size-7 text-brand" />
        </span>

        <DialogTitle className="pt-5 text-2xl font-bold tracking-tight">
          {expired ? "Time's Up" : "Order Cancelled"}
        </DialogTitle>

        <DialogDescription className="pt-2 text-sm leading-5">
          {expired
            ? "Sorry, your payment time has expired. Your seats have been released."
            : "This order was cancelled. Your seats have been released."}
        </DialogDescription>

        {/* Still exactly one action (FR-024, spec 019 FR-009). The old "Repeat
            Order" link is gone and is NOT what this is: that one jumped into
            the event's ticket SELECTION, and the copy above promised a repeat
            this dialog does not offer. This leads to the event's landing page
            instead — one step short of selection — so the guest arrives where
            the released seats are listed without being told an order was
            repeated for them. */}
        <Link
          href={eventDetailPath(eventSlug)}
          className="mt-6 flex h-11 w-full items-center justify-center rounded-lg bg-brand text-base font-bold text-brand-foreground transition-opacity hover:opacity-90 focus:outline-none focus:ring-2 focus:ring-brand focus:ring-offset-2 disabled:opacity-60"
        >
          Return to Event Page
        </Link>
      </DialogContent>
    </Dialog>
  );
}

/** Swallows every close request, whatever its reason. See the note above. */
function noop() {}

"use client";

import Link from "next/link";
import { TriangleAlert } from "lucide-react";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";

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
 * single "Return to Home Page" button is that escape: focusable, inside the
 * trap, and a real way out. Browser navigation is never blocked — this blocks
 * the page, not the browser.
 */
export function EndOfJourneyDialog({
  status,
}: {
  status: "EXPIRED" | "CANCELLED";
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
            ? "Sorry, your payment time has expired. Please repeat your order."
            : "This order was cancelled. Please repeat your order."}
        </DialogDescription>

        {/* Exactly one action (FR-024). The old "Repeat Order" link into the
            event's ticket selection is gone, and with it the copy above that
            used to tell the guest to repeat — promising an action this dialog
            does not offer. */}
        <Link
          href="/"
          className="mt-6 flex h-11 w-full items-center justify-center rounded-lg bg-brand text-base font-bold text-brand-foreground transition-opacity hover:opacity-90 focus:outline-none focus:ring-2 focus:ring-brand focus:ring-offset-2 disabled:opacity-60"
        >
          Return to Home Page
        </Link>
      </DialogContent>
    </Dialog>
  );
}

/** Swallows every close request, whatever its reason. See the note above. */
function noop() {}

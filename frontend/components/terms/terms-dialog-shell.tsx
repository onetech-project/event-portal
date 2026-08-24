"use client";

import type { ReactNode } from "react";

import { TermsViewer } from "@/components/terms/terms-viewer";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
} from "@/components/ui/dialog";

/**
 * The Terms & Conditions dialog — the WHOLE dialog, footer included.
 *
 * Both surfaces that show terms render this: the booking flow and the free
 * registration form. They are pixel-identical by construction (spec 022 FR-043a,
 * tightened 2026-08-20), because anything left to each caller is something the
 * two can drift on.
 *
 * The footer used to be the exception, on the reasoning that booking needs
 * checkbox + Cancel + Agree while registration only needs to accept. That was
 * wrong, and it showed: registration rendered a single small right-aligned
 * button where booking has an agreement checkbox and two full-width actions —
 * visibly a different dialog. What varies between the surfaces is only what
 * pressing Agree DOES, so only that is injected.
 *
 * What each caller still owns is `action.onClick` and, while a press is in
 * flight, `action.label` — booking says "Booking…" and then "Retry" on failure,
 * because it is placing an order behind this button. Registration has nothing to
 * wait for and always reads "Agree".
 */
export function TermsDialogShell({
  open,
  onOpenChange,
  eventSlug,
  eventName,
  agreed,
  onAgreedChange,
  onReachedEnd,
  error,
  action,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  eventSlug: string;
  eventName: string;
  /**
   * Whether the guest has agreed. This is what the Agree action follows —
   * NOT the scroll position (FR-014c, clarified 2026-08-21).
   */
  agreed: boolean;
  /** The guest ticking or unticking the box themselves. */
  onAgreedChange: (next: boolean) => void;
  /** Reaching the end of the document, which ticks the box on their behalf. */
  onReachedEnd: () => void;
  /** Shown above the actions. Null when there is nothing to report. */
  error?: string | null;
  action: {
    /** What the button reads right now — callers vary this while busy. */
    label: ReactNode;
    onClick: () => void;
    /** A press is in flight. */
    disabled?: boolean;
    /**
     * An additional precondition beyond the checkbox. Booking passes
     * `terms.isSuccess`, because agreeing to a document that failed to load would
     * record consent to nothing. Defaults to true.
     *
     * This survived the 2026-08-21 loosening untouched, and deliberately: it is a
     * fact about the DOCUMENT, not a demand on the guest.
     */
    ready?: boolean;
  };
}) {
  const actionable = agreed && action.ready !== false;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[calc(100dvh-6rem)] max-w-144.5 flex-col gap-0 rounded-2xl p-0 sm:max-w-3xl">
        {/* An empty bar the close button sits in, so the rule below it clears
            the button rather than running under it. */}
        <div className="h-15 shrink-0" />

        <div className="flex min-h-0 flex-1 flex-col border-t px-4 pt-4 pb-4">
          <DialogTitle className="sr-only">
            Terms &amp; Conditions for {eventName}
          </DialogTitle>

          <TermsViewer
            eventSlug={eventSlug}
            eventName={eventName}
            enabled={open}
            onReachedEnd={onReachedEnd}
          />

          <hr className="border-t" />

          {error ? (
            <p role="alert" className="pt-3 text-sm leading-5 text-destructive">
              {error}
            </p>
          ) : null}

          {/*
            An ordinary checkbox, and that is the point (spec 022 FR-045,
            clarified 2026-08-21). The guest may tick it at any moment without
            having read anything; reaching the end of the document ticks it for
            them, as a convenience rather than a condition.

            It briefly was not one: an earlier design made this an INDICATOR with
            a no-op `onCheckedChange`, so that reaching the end was the only way
            to consent. That gate is withdrawn.

            Still never `disabled`: base-ui renders a <button role="checkbox">
            carrying `disabled:opacity-50`, so disabling it would dim the control
            and drop it from the tab order — a VISIBLE change to a dialog FR-044
            requires to look exactly as it did. There is now no state in which it
            should be disabled at all.
          */}
          <label className="flex items-center gap-2 pt-3 text-sm leading-5 text-terms-ink cursor-pointer select-none">
            <Checkbox checked={agreed} onCheckedChange={onAgreedChange} />
            <p>I agree to terms &amp; conditions</p>
          </label>

          <div className="flex gap-8 pt-3">
            <DialogClose className="flex h-12 flex-1 items-center justify-center rounded-lg border border-brand bg-background text-base font-bold text-brand transition-colors hover:bg-brand-surface">
              Cancel
            </DialogClose>

            {actionable ? (
              <button
                type="button"
                onClick={action.onClick}
                disabled={action.disabled}
                className="flex h-12 flex-1 items-center justify-center rounded-lg bg-brand text-base font-bold text-brand-foreground transition-opacity hover:opacity-90 disabled:opacity-60"
              >
                {action.label}
              </button>
            ) : (
              // Not a disabled <button>: an aria-disabled span keeps the
              // affordance visible and legible while making clear it is not yet
              // actionable. Its absence from the button role is also what the
              // gate scenarios assert on, so the two states are distinguishable.
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

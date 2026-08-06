"use client";

import Link from "next/link";
import { TriangleAlert } from "lucide-react";

/**
 * The dead-end screen for an order that never got paid (Figma 288-2295):
 * a "Time's Up" card telling the guest their hold or payment window ran out
 * and the seats went back on sale, with the way back to start over.
 *
 * Rendered in place of the payment panel (spec FR-020) — the journey ended,
 * just not with a purchase, and a repeat means booking again from the ticket
 * selection because availability may have changed.
 */
export function ExpiredState({
  status,
  eventSlug,
}: {
  status: "EXPIRED" | "CANCELLED";
  eventSlug: string;
}) {
  const expired = status === "EXPIRED";

  return (
    <main className="mx-auto flex w-full max-w-2xl flex-1 items-center justify-center px-6 py-10">
      <section
        aria-live="polite"
        className="w-full max-w-md rounded-2xl bg-card p-8 text-center shadow-sm"
      >
        <span className="mx-auto flex size-16 items-center justify-center rounded-full bg-brand-surface">
          <TriangleAlert aria-hidden className="size-7 text-brand" />
        </span>

        <h1 className="pt-5 text-2xl font-bold tracking-tight">
          {expired ? "Time's Up" : "Order Cancelled"}
        </h1>

        <p className="pt-2 text-sm leading-5 text-muted-foreground">
          {expired
            ? "Sorry, your payment time has expired. Please repeat your order."
            : "This order was cancelled. Your tickets were released back on sale."}
        </p>

        <div className="space-y-3 pt-6">
          <Link
            href={`/events/${encodeURIComponent(eventSlug)}/tickets`}
            className="flex h-11 w-full items-center justify-center rounded-lg bg-brand text-base font-bold text-brand-foreground transition-opacity hover:opacity-90"
          >
            Repeat Order
          </Link>
          <Link
            href="/"
            className="flex h-11 w-full items-center justify-center rounded-lg border border-brand text-base font-bold text-brand transition-colors hover:bg-brand-surface"
          >
            Return to Home Page
          </Link>
        </div>
      </section>
    </main>
  );
}

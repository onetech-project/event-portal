"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { BookingSteps } from "@/components/booking/booking-steps";
import { SalesCountdown } from "@/components/booking/sales-countdown";
import { StatusAlert } from "@/components/ui/feedback";
import { bookingStageFromPathname } from "@/lib/booking-stage";
import { useEventBySlug } from "@/lib/queries";

/**
 * The chrome every screen in an event's purchase journey sits inside.
 *
 * It lives in the layout, not in each page, for one structural reason: the App
 * Router keeps a layout mounted while only the leaf changes, so the countdown's
 * interval is never torn down as the guest moves selection → checkout → payment
 * → confirmation. Rendering it per page would restart the timer on every hop,
 * which is the reset spec SC-001 forbids.
 *
 * The event is read with the same `useEventBySlug` the detail page uses, so the
 * two share one query key. That deduplicates the request without extending any
 * cache lifetime — quota stays live inventory under the app-wide `staleTime: 0`.
 */
export function EventFrame({
  slug,
  children,
}: Readonly<{ slug: string; children: ReactNode }>) {
  const { data: event, isPending, isError } = useEventBySlug(slug);
  const pathname = usePathname();
  const stage = bookingStageFromPathname(pathname);

  // The detail page is a landing page, not a step of the journey (Figma 4-5,
  // clarification 2026-08-05): the rail appears from the selection page onward.
  const isDetailPage = /^\/events\/[^/]+\/?$/.test(pathname);

  // An event nobody can resolve has no journey to frame, so nothing nested is
  // rendered behind the message (spec FR-007). Doing this here rather than in
  // each page means it cannot be forgotten on one of them.
  if (isError || (!isPending && !event)) {
    return (
      <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
        <StatusAlert>
          Event not found. It may have been removed, or the link may be wrong.
        </StatusAlert>
        <p className="mt-4 text-sm">
          <Link href="/events" className="underline">
            Browse all events
          </Link>
        </p>
      </main>
    );
  }

  return (
    <div className="flex flex-1 flex-col">
      {/* No countdown until the start date is genuinely known: a zeroed skeleton
          reads as an event starting this second (spec FR-008). The rail has no
          such dependency — it comes from the URL — so it renders immediately and
          the guest keeps their sense of progress while the event loads. */}
      {event ? <SalesCountdown startDate={event.start_date} /> : null}
      {isDetailPage ? null : <BookingSteps current={stage} />}
      {children}
    </div>
  );
}

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

  // Free registration (spec 022 FR-010a) is not a purchase journey at all.
  //
  // It lives under this layout only because it shares the `/events/[slug]`
  // segment, and the App Router gives a nested route no way to opt out of a
  // parent layout — so the exclusion has to happen here. Left inherited, an
  // invited guest would be shown a progress rail whose steps include **Payment**
  // on a surface FR-015 forbids from presenting any payment step, and a countdown
  // for a sale they are not part of.
  //
  // Matched on the URL like the stage itself, so a page cannot contradict its own
  // address. The event chrome around it is deliberately kept: the registrant is
  // still looking at one event, and should see whose event it is.
  const isRegistration = /^\/events\/[^/]+\/register(\/|$)/.test(pathname);

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
      {event && !isRegistration ? <SalesCountdown startDate={event.start_date} /> : null}
      {isDetailPage || isRegistration ? null : <BookingSteps current={stage} />}
      {children}
    </div>
  );
}

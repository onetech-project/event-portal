"use client";

import Link from "next/link";
import { Image as ImageIcon } from "lucide-react";

import { Card, CardContent } from "@/components/ui/card";
import { EmptyState, Loading, StatusAlert } from "@/components/ui/feedback";
import { formatDateTime } from "@/lib/format";
import { usePublishedEvents } from "@/lib/queries";

/**
 * The homepage event grid (spec 008 US3): every published event as a card
 * linking to its detail page. Extracted from the old /events listing, which now
 * redirects here.
 */
export function EventGrid() {
  const { data: events, isPending, error } = usePublishedEvents();

  if (isPending) return <Loading label="Loading events…" />;
  if (error) return <StatusAlert>{error.message}</StatusAlert>;

  if (events !== undefined && events.length === 0) {
    return <EmptyState>No events are on sale right now. Check back soon.</EmptyState>;
  }

  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
      {events?.map((event) => (
        <Link key={event.id} href={`/events/${event.slug}`} className="block">
          {/* pt-0 + overflow-hidden: the card's flush-media treatment only
              auto-applies to a first-child <img>; the gray placeholder div
              needs the same, or it floats below the card's top padding. */}
          <Card className="h-full overflow-hidden pt-0 transition hover:ring-foreground/25">
            {event.banner_url !== null ? (
              // A plain admin-supplied URL: the platform never uploads or hosts
              // banner images, so this is rendered as an ordinary remote image.
              // eslint-disable-next-line @next/next/no-img-element
              <img src={event.banner_url} alt={event.name} className="h-36 w-full object-cover" />
            ) : (
              // No banner authored: a gray placeholder keeps every card the
              // same shape instead of collapsing the image area
              // (clarification 2026-08-05).
              <div
                aria-hidden
                className="flex h-36 w-full items-center justify-center bg-muted"
              >
                <ImageIcon className="size-8 text-muted-foreground/60" />
              </div>
            )}
            <CardContent>
              <h2 className="text-lg font-semibold">{event.name}</h2>
              <p className="mt-1 text-sm text-muted-foreground">{event.venue}</p>
              <p className="mt-2 text-sm text-muted-foreground">
                {formatDateTime(event.start_date)}
              </p>
            </CardContent>
          </Card>
        </Link>
      ))}
    </div>
  );
}

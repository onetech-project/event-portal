"use client";

import Link from "next/link";

import { Card, CardContent } from "@/components/ui/card";
import { EmptyState, Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import { formatDateTime } from "@/lib/format";
import { usePublishedEvents } from "@/lib/queries";

export default function EventsPage() {
  const { data: events, isPending, error } = usePublishedEvents();

  return (
    <main className="mx-auto w-full max-w-4xl flex-1 px-6 py-10">
      <PageHeading title="Upcoming events" subtitle="Pick an event to see its tickets." />

      {isPending ? <Loading label="Loading events…" /> : null}
      {error ? <StatusAlert>{error.message}</StatusAlert> : null}

      {events && events.length === 0 ? (
        <EmptyState>No events are on sale right now. Check back soon.</EmptyState>
      ) : null}

      <div className="grid gap-4 sm:grid-cols-2">
        {events?.map((event) => (
          <Link key={event.id} href={`/events/${event.slug}`} className="block">
            <Card className="h-full transition hover:ring-foreground/25">
              {event.banner_url ? (
                // A plain admin-supplied URL: the platform never uploads or hosts
                // banner images, so this is rendered as an ordinary remote image.
                // eslint-disable-next-line @next/next/no-img-element
                <img
                  src={event.banner_url}
                  alt=""
                  className="h-36 w-full object-cover"
                />
              ) : null}
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
    </main>
  );
}

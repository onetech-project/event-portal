"use client";

import Link from "next/link";
import { use } from "react";
import {
  CalendarDays,
  Image as ImageIcon,
  MapPin,
  MoveRight,
  Users,
} from "lucide-react";

import { ContentIcon } from "@/components/event/content-icon";
import { Loading, StatusAlert } from "@/components/ui/feedback";
import { formatDateRange, formatVisitorCount } from "@/lib/format";
import { useEventBySlug } from "@/lib/queries";
import type { EventDetail } from "@/lib/types";
import { Card, CardContent, CardHeader } from "@/components/ui/card";

/**
 * The guest event detail page (spec 008 US4, Figma 4-5): banner, schedule and
 * venue bar, CMS-authored description and content blocks. Tickets are NOT here
 * (clarification 2026-08-05) — Buy Tickets leads to /events/[slug]/tickets,
 * which is the only screen paying for live-quota reads.
 */
export default function EventDetailPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = use(params);
  return <EventDetailView slug={slug} />;
}

/**
 * The page itself, separated from route-param unwrapping so it can be rendered
 * with a plain slug (`use()` does not settle under the test DOM).
 */
export function EventDetailView({ slug }: { slug: string }) {
  const { data: event, isPending, error } = useEventBySlug(slug);

  if (isPending) return <Loading label="Loading event…" />;

  if (error) {
    return (
      <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-10">
        <StatusAlert>{error.message}</StatusAlert>
        <p className="mt-4 text-sm">
          <Link href="/" className="underline">
            Back to all events
          </Link>
        </p>
      </main>
    );
  }

  if (!event) return null;

  return (
    <div className="flex-1">
      {/* Hero (Figma 4-5): full-bleed square banner. The white content sheet
          below rises over the image's foot with rounded TOP corners — the
          rounding belongs to the sheet, not the image — and the dark info bar
          straddles the sheet's top edge (clarification 2026-08-05). */}
      <section>
        {event.banner_url !== null ? (
          // Admin-supplied URL; nothing is uploaded or hosted here.
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={event.banner_url}
            alt=""
            className="h-80 w-full object-cover sm:h-112"
          />
        ) : (
          // No banner authored: gray block + image icon, never a bare gap
          // (clarification 2026-08-05).
          <div
            aria-hidden
            className="flex h-64 w-full items-center justify-center bg-muted sm:h-80"
          >
            <ImageIcon className="size-12 text-muted-foreground/60" />
          </div>
        )}

        {/* pt-px keeps the bar's negative margin from collapsing through this
            sheet and dragging the sheet itself up instead of the bar. */}
        <div className="relative -mt-10 rounded-t-3xl bg-background pt-px">
          <div className="mx-auto w-full max-w-6xl px-6">
            <div className="-mt-10 flex flex-wrap items-center justify-between gap-4 rounded-xl bg-foreground px-6 py-4 text-background shadow-lg">
              <h1 className="sr-only">{event.name}</h1>
              <InfoCell
                icon={<CalendarDays aria-hidden className="size-5" />}
                label="Dates"
              >
                {formatDateRange(event.start_date, event.end_date)}
              </InfoCell>
              {event.scale !== null && event.scale > 0 ? (
                <>
                  <CellSeparator />
                  <InfoCell
                    icon={<Users aria-hidden className="size-5" />}
                    label="Scale"
                  >
                    {formatVisitorCount(event.scale)}+ Visitors
                  </InfoCell>
                </>
              ) : null}
              <CellSeparator />
              <InfoCell
                icon={<MapPin aria-hidden className="size-5" />}
                label="Venue"
              >
                {event.venue}
                <span className="block text-xs opacity-70">
                  {event.address}
                </span>
              </InfoCell>

              {event.has_terms ? (
                <Link
                  href={`/events/${encodeURIComponent(event.slug)}/tickets`}
                  className="flex h-11 items-center gap-2 rounded-lg bg-brand px-6 text-sm font-bold text-brand-foreground uppercase transition-opacity hover:opacity-90"
                >
                  Buy Tickets <MoveRight aria-hidden className="size-4" />
                </Link>
              ) : (
                // No authored T&C = nothing to agree to = booking closed (409001
                // server-side); the disabled control says why instead of 404ing.
                <span
                  aria-disabled="true"
                  title="Ticket sales open once the organizer publishes the Terms & Conditions."
                  className="flex h-11 items-center rounded-lg bg-muted px-6 text-sm font-bold text-muted-foreground uppercase"
                >
                  Sales not open yet
                </span>
              )}
            </div>
          </div>
        </div>
      </section>

      <main className="mx-auto w-full max-w-6xl px-6 pb-14">
        <div className="mt-10 grid gap-8 lg:grid-cols-[minmax(0,1fr)_320px] lg:items-start">
          <div className="space-y-10">
            {event.description !== null && event.description !== "" ? (
              <section aria-label="About the event">
                <h2 className="text-2xl font-bold tracking-tight">
                  About the event
                </h2>
                {/* Sanitized server-side on write (bluemonday); rendered verbatim. */}
                <div
                  className="prose-content mt-3 text-sm leading-6 text-muted-foreground [&_a]:underline [&_h2]:mt-4 [&_h2]:text-lg [&_h2]:font-semibold [&_h3]:mt-3 [&_h3]:font-semibold [&_ol]:list-decimal [&_ol]:pl-6 [&_p]:mb-3 [&_strong]:text-foreground [&_ul]:list-disc [&_ul]:pl-6"
                  dangerouslySetInnerHTML={{ __html: event.description }}
                />
              </section>
            ) : null}

            {event.activities.length > 0 ? (
              <section aria-label="Main event activities">
                <h2 className="text-2xl font-bold tracking-tight">
                  Main event activities
                </h2>
                <div className="mt-4 grid gap-4 sm:grid-cols-2">
                  {event.activities.map((activity) => (
                    <Card key={activity.id} className="gap-0 bg-muted/20">
                      <CardHeader>
                        <span className="flex size-10 items-center justify-center rounded-full bg-brand-surface text-brand">
                          <ContentIcon
                            name={activity.icon ?? "info"}
                            className="size-5"
                          />
                        </span>
                      </CardHeader>
                      <CardContent>
                        <h3 className="pt-3 font-semibold">{activity.title}</h3>
                        <p className="pt-1 text-sm leading-5 text-muted-foreground">
                          {activity.description}
                        </p>
                      </CardContent>
                    </Card>
                  ))}
                </div>
              </section>
            ) : null}

            {event.guest_stars.length > 0 ? (
              <section aria-label="Featured guests">
                <h2 className="text-2xl font-bold tracking-tight">
                  Featured guests
                </h2>
                <ul className="mt-4 flex flex-wrap gap-2">
                  {event.guest_stars.map((star) => (
                    <li
                      key={star.id}
                      className="rounded-md border bg-card px-3 py-1.5 text-sm font-medium"
                    >
                      {star.name}
                    </li>
                  ))}
                </ul>
              </section>
            ) : null}
          </div>

          <aside className="space-y-4 lg:sticky lg:top-6">
            <VenueGuidelinesCard event={event} />
          </aside>
        </div>
      </main>
    </div>
  );
}

/**
 * The thin vertical divider between the info bar's cells (Figma 4-5). Hidden
 * when the bar wraps on small screens — a vertical line between stacked rows
 * would read as noise.
 */
function CellSeparator() {
  return (
    <span
      aria-hidden
      className="hidden h-10 w-px self-center bg-background/20 sm:block"
    />
  );
}

function InfoCell({
  icon,
  label,
  children,
}: {
  icon: React.ReactNode;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-3">
      <span className="flex size-10 items-center justify-center rounded-full bg-brand text-brand-foreground">
        {icon}
      </span>
      <div>
        <p className="text-xs uppercase tracking-wide opacity-70">{label}</p>
        <p className="text-sm font-semibold">{children}</p>
      </div>
    </div>
  );
}

function VenueGuidelinesCard({ event }: { event: EventDetail }) {
  if (event.guidelines.length === 0) return null;
  return (
    <section
      aria-label="Event guidelines"
      className="overflow-hidden rounded-xl border bg-card"
    >
      <div className="relative border-b bg-foreground text-background">
        {event.banner_url ? (
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={event.banner_url}
            alt={event.name}
            className="h-56 w-full object-cover"
          />
        ) : (
          <div
            aria-hidden
            className="flex h-56 w-full items-center justify-center bg-muted"
          >
            <ImageIcon className="size-8 text-muted-foreground/60" />
          </div>
        )}
        <div className="absolute bottom-0 px-5 py-3 w-full bg-linear-to-t from-black to-transparent">
          <p className="font-semibold">{event.venue}</p>
          <p className="text-xs opacity-70">{event.address}</p>
        </div>
      </div>
      <div className="px-5 py-4 bg-muted/20">
        <h2 className="text-xs font-bold uppercase tracking-wide text-muted-foreground">
          Event guidelines
        </h2>
        <ul className="mt-3 space-y-3">
          {event.guidelines.map((guideline) => (
            <li
              key={guideline.id}
              className="flex items-start gap-2 text-sm leading-5"
            >
              <span className="mt-0.5 shrink-0 text-brand">
                <ContentIcon
                  name={guideline.icon ?? "info"}
                  className="size-4"
                />
              </span>
              {guideline.description}
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

"use client";

import Link from "next/link";
import { use, useMemo, useState } from "react";

import { SelectableRow } from "@/components/booking/selectable-row";
import { SelectionSummary } from "@/components/booking/selection-summary";
import { EmptyState, Loading, StatusAlert } from "@/components/ui/feedback";
import { useEventBySlug, useEventPackages, useEventTicketTypes } from "@/lib/queries";
import {
  buildSelectableItems,
  selectionLines,
  type Quantities,
} from "@/lib/selection";

/** Params is a Promise in this Next.js version and must be unwrapped. */
export default function EventDetailPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = use(params);
  const { data: event, isPending, error } = useEventBySlug(slug);
  // Ticket and package data come from their own endpoints since 008
  // (clarification 2026-08-05) — the detail response is content-only.
  const ticketsQuery = useEventTicketTypes(slug);
  const packagesQuery = useEventPackages(slug);
  const [quantities, setQuantities] = useState<Quantities>({});
  // Read once, in a lazy initializer, so the sales-window checks stay pure and do
  // not shift between renders.
  const [now] = useState(() => Date.now());

  // Tickets and bundles are one list, ordered by price. A bundle is a peer
  // product here, not a section of its own — only its badge sets it apart.
  const items = useMemo(
    () => buildSelectableItems(ticketsQuery.data ?? [], packagesQuery.data ?? []),
    [ticketsQuery.data, packagesQuery.data],
  );
  const lines = useMemo(() => selectionLines(items, quantities), [items, quantities]);

  if (isPending || ticketsQuery.isPending || packagesQuery.isPending)
    return <Loading label="Loading event…" />;

  if (error) {
    return (
      <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-10">
        <StatusAlert>{error.message}</StatusAlert>
        <p className="mt-4 text-sm">
          <Link href="/events" className="underline">
            Back to all events
          </Link>
        </p>
      </main>
    );
  }

  if (!event) return null;

  return (
    <div className="flex-1">
      {/* The countdown and the progress rail come from the event layout, which
          keeps them mounted across the whole journey. */}

      {/* No event title or banner here: the design opens straight onto the
          ticket list, and each row already names the event and its date. */}
      <main className="mx-auto w-full max-w-6xl px-6 pt-2 pb-14">
        <h1 className="sr-only">{event.name}</h1>

        <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_362px] lg:items-start">
          <section aria-label="Tickets and bundles" className="space-y-6">
            {items.length === 0 ? (
              <EmptyState>No tickets have been published for this event yet.</EmptyState>
            ) : null}

            {items.map((item) => (
              <SelectableRow
                key={`${item.kind}:${item.id}`}
                item={item}
                now={now}
                quantity={quantities[item.id] ?? 0}
                onChange={(quantity) =>
                  setQuantities((current) => ({ ...current, [item.id]: quantity }))
                }
              />
            ))}
          </section>

          <SelectionSummary
            lines={lines}
            eventId={event.id}
            eventSlug={slug}
            eventName={event.name}
          />
        </div>
      </main>
    </div>
  );
}

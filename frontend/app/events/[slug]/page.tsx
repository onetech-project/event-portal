"use client";

import Link from "next/link";
import { use, useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { EmptyState, Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { formatCurrency, formatDateTime } from "@/lib/format";
import { useEventBySlug } from "@/lib/queries";
import type { TicketTypeSummary } from "@/lib/types";

/** Params is a Promise in this Next.js version and must be unwrapped. */
export default function EventDetailPage({
  params,
}: {
  params: Promise<{ slug: string }>;
}) {
  const { slug } = use(params);
  const { data: event, isPending, error } = useEventBySlug(slug);
  const [quantities, setQuantities] = useState<Record<string, number>>({});
  // Read once, in a lazy initializer, so the sales-window checks below stay pure
  // and do not shift between renders.
  const [now] = useState(() => Date.now());

  const selection = useMemo(
    () => Object.entries(quantities).filter(([, quantity]) => quantity > 0),
    [quantities],
  );

  const total = useMemo(() => {
    if (!event) return 0;
    return selection.reduce((sum, [ticketTypeId, quantity]) => {
      const ticketType = event.ticket_types.find((t) => t.id === ticketTypeId);
      return sum + (ticketType ? Number(ticketType.price) * quantity : 0);
    }, 0);
  }, [event, selection]);

  const checkoutHref = `/checkout?slug=${encodeURIComponent(slug)}&${selection
    .map(([id, quantity]) => `t=${id}:${quantity}`)
    .join("&")}`;

  if (isPending) return <Loading label="Loading event…" />;

  if (error) {
    return (
      <main className="mx-auto w-full max-w-3xl flex-1 px-6 py-10">
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
    <main className="mx-auto w-full max-w-3xl flex-1 px-6 py-10">
      {event.banner_url ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={event.banner_url}
          alt=""
          className="mb-6 h-52 w-full rounded-xl object-cover"
        />
      ) : null}

      <PageHeading
        title={event.name}
        subtitle={`${event.venue} · ${formatDateTime(event.start_date)}`}
      />

      {event.description ? (
        <p className="mb-6 whitespace-pre-line">{event.description}</p>
      ) : null}
      <p className="mb-8 text-sm text-muted-foreground">{event.address}</p>

      <h2 className="mb-3 text-lg font-semibold">Tickets</h2>

      {event.ticket_types.length === 0 ? (
        <EmptyState>No tickets have been published for this event yet.</EmptyState>
      ) : null}

      <div className="space-y-3">
        {event.ticket_types.map((ticketType) => (
          <TicketTypeRow
            key={ticketType.id}
            ticketType={ticketType}
            now={now}
            quantity={quantities[ticketType.id] ?? 0}
            onChange={(quantity) =>
              setQuantities((current) => ({ ...current, [ticketType.id]: quantity }))
            }
          />
        ))}
      </div>

      {selection.length > 0 ? (
        <Card className="mt-6">
          <CardContent className="flex items-center justify-between gap-4">
            <div>
              <p className="text-sm text-muted-foreground">Estimated total</p>
              <p className="text-xl font-semibold">{formatCurrency(String(total))}</p>
              <p className="mt-1 text-xs text-muted-foreground">
                The final amount is recalculated by the server at checkout.
              </p>
            </div>
            <Button asChild size="lg">
              <Link href={checkoutHref}>Continue to checkout</Link>
            </Button>
          </CardContent>
        </Card>
      ) : null}
    </main>
  );
}

function TicketTypeRow({
  ticketType,
  now,
  quantity,
  onChange,
}: {
  ticketType: TicketTypeSummary;
  now: number;
  quantity: number;
  onChange: (quantity: number) => void;
}) {
  const soldOut = ticketType.quota_remaining === 0;
  const notYetOpen = new Date(ticketType.sales_start).getTime() > now;
  const closed = new Date(ticketType.sales_end).getTime() < now;
  const unavailable = soldOut || notYetOpen || closed;

  // Never offer more than the quota the server last reported. It can still
  // change under us, which is why checkout is the real authority.
  const max = Math.min(ticketType.quota_remaining, 10);

  let availability: string;
  if (soldOut) {
    availability = "Sold out";
  } else if (notYetOpen) {
    availability = `Sales open ${formatDateTime(ticketType.sales_start)}`;
  } else if (closed) {
    availability = "Sales have closed";
  } else {
    availability = `${ticketType.quota_remaining} remaining`;
  }

  return (
    <Card>
      <CardContent className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="font-medium">{ticketType.name}</p>
          <p className="text-sm text-muted-foreground">
            {formatCurrency(ticketType.price)}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">{availability}</p>
        </div>

        <Select
          disabled={unavailable}
          value={String(quantity)}
          onValueChange={(value) => onChange(Number(value))}
        >
          <SelectTrigger aria-label={`Quantity for ${ticketType.name}`} className="w-24">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {Array.from({ length: max + 1 }, (_, i) => (
              <SelectItem key={i} value={String(i)}>
                {i}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </CardContent>
    </Card>
  );
}

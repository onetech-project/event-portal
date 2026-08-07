"use client";

import Link from "next/link";

import { Card, CardContent } from "@/components/ui/card";
import { StatusAlert } from "@/components/ui/feedback";
import type { OrderStatus } from "@/lib/types";

type Props = {
  status: OrderStatus;
  eventSlug: string;
};

/**
 * What the guest sees once their order has settled — or, for a cancelled or
 * expired one, why it cannot be paid and what to do instead.
 *
 * Spec 011 FR-012: everything goes to the buyer — the first holder form's
 * address. The copy says so without naming it, since the guest order read no
 * longer exposes the buyer email.
 */
export function PaymentStatusCard({ status, eventSlug }: Readonly<Props>) {
  if (status === "PAID") {
    return (
      <Card>
        <CardContent className="space-y-4">
          <StatusAlert tone="success">
            <span className="font-semibold">Payment successful.</span> Your invoice and
            every e-ticket in this order are on their way to the email address on the
            first ticket holder form, with one QR code per attendee.
          </StatusAlert>

          <p className="text-sm text-muted-foreground">
            Keep your order number handy for support. If the email has not arrived after
            a few minutes, check your spam folder — the tickets remain valid either way.
          </p>

          <p className="text-sm">
            <Link
              href={eventSlug ? `/events/${eventSlug}` : "/events"}
              className="underline"
            >
              {eventSlug ? "Back to the event" : "Browse events"}
            </Link>
          </p>
        </CardContent>
      </Card>
    );
  }

  const expired = status === "EXPIRED";

  return (
    <Card>
      <CardContent className="space-y-4">
        <StatusAlert>
          {expired
            ? "This order expired before it was paid, so the tickets were released back on sale."
            : "This order was cancelled and can no longer be paid. Nothing was charged."}
        </StatusAlert>

        <p className="text-sm text-muted-foreground">
          You can start a new order for the same event — availability may have changed.
        </p>

        <p className="text-sm">
          <Link href={eventSlug ? `/events/${eventSlug}` : "/events"} className="underline">
            {eventSlug ? "Back to the event" : "Browse events"}
          </Link>
        </p>
      </CardContent>
    </Card>
  );
}

"use client";

import Link from "next/link";
import { use } from "react";

import { Card, CardContent } from "@/components/ui/card";
import { PageHeading, StatusAlert } from "@/components/ui/feedback";

/**
 * Post-checkout landing page.
 *
 * The guest is normally redirected straight to the payment provider from
 * checkout; this page is where the provider's own "back to merchant" link lands,
 * so it explains what happens next rather than polling for a status the webhook
 * owns.
 */
export default function OrderStatusPage({
  params,
}: {
  params: Promise<{ orderNumber: string }>;
}) {
  const { orderNumber } = use(params);

  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
      <PageHeading title="Thanks for your order" subtitle={`Order ${orderNumber}`} />

      <Card>
        <CardContent className="space-y-4">
          <StatusAlert tone="info">
            Once your payment is confirmed we email your tickets as a PDF, with one QR
            code per attendee. This usually takes a moment.
          </StatusAlert>

          <p className="text-sm text-muted-foreground">
            Keep your order number handy for support. If the email has not arrived after
            a few minutes, check your spam folder — the tickets remain valid either way.
          </p>

          <p className="text-sm">
            <Link href="/events" className="underline">
              Browse more events
            </Link>
          </p>
        </CardContent>
      </Card>
    </main>
  );
}

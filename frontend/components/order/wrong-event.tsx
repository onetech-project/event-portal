import Link from "next/link";

import { PageHeading, StatusAlert } from "@/components/ui/feedback";

/**
 * Shown when an order is opened under an event that does not own it.
 *
 * Deliberately a dead end rather than a redirect to the owning event: sending
 * the guest onward would make a wrong URL work, which defeats the point of
 * scoping orders to their event in the first place (spec FR-013). It is also
 * worded differently from the plain "order not found" case, so support can tell
 * a mistyped order number from a mismatched link.
 */
export function WrongEvent({ eventSlug }: Readonly<{ eventSlug: string }>) {
  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
      <PageHeading title="Order not found for this event" />
      <StatusAlert>
        That order belongs to a different event. Check the link from your confirmation
        email, or browse this event&apos;s tickets instead.
      </StatusAlert>
      <p className="mt-4 text-sm">
        <Link href={`/events/${eventSlug}`} className="underline">
          Back to the event
        </Link>
      </p>
    </main>
  );
}

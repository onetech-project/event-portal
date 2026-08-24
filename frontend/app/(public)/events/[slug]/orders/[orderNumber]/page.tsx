"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect } from "react";

import { EndOfJourneyDialog } from "@/components/order/end-of-journey-dialog";
import { OrderForms } from "@/components/order/visitor-form";
import { WrongEvent } from "@/components/order/wrong-event";
import { Loading, StatusAlert } from "@/components/ui/feedback";
import { ApiError } from "@/lib/api-client";
import { useCheckoutStatus } from "@/lib/checkout-status";
import { eventDetailPath, orderCheckoutPath, orderDonePath } from "@/lib/order-routes";
import { isFinalOrderStatus, useOrderDetail } from "@/lib/queries";

/**
 * The ticket holder forms — the Registration stage of the rail.
 *
 * Spec 011 FR-020: this address used to serve the QRIS screen too, switching
 * on `payment_started`. One address for two stages meant the rail could name
 * only one of them, so it named Payment on both and told guests they had
 * finished Registration while they were still filling it in. The QR screen now
 * lives at `./checkout` and this page is the forms alone.
 */
export default function OrderPage({
  params,
}: Readonly<{ params: Promise<{ slug: string; orderNumber: string }> }>) {
  const { slug, orderNumber } = use(params);
  return <OrderView eventSlug={slug} orderNumber={orderNumber} />;
}

/**
 * The page itself, separated from route-param unwrapping so it can be rendered
 * with a plain order number.
 */
export function OrderView({
  eventSlug,
  orderNumber,
}: Readonly<{ eventSlug: string; orderNumber: string }>) {
  const router = useRouter();
  const { data, isPending, isError, error } = useOrderDetail(orderNumber);

  // Live status over SSE; the 3-second poll stays running as the mandated
  // fallback, so a dropped stream degrades with no mode switch. It matters here
  // too: an order can settle or expire while the forms sit open.
  useCheckoutStatus(orderNumber, !isFinalOrderStatus(data?.status));

  // Two forwards, both `replace` (spec 011 FR-021). A PAID order belongs on the
  // confirmation — it may have been paid in another tab, or the link reopened
  // later. An order whose payment has already started belongs on the QR screen;
  // this is also the hop Continue to Payment takes, since submitting the forms
  // flips `payment_started` on the refetched order.
  //
  // replace, not push: the two screens forward to each other by state, so a
  // history entry here would leave the back button bouncing between them.
  const paid = data?.status === "PAID";
  const started = data?.payment_started === true;
  const ownedByThisEvent = data?.event.slug === eventSlug;
  const live = data?.status === "PENDING";

  useEffect(() => {
    if (!ownedByThisEvent) return;
    if (paid) {
      router.replace(orderDonePath(eventSlug, orderNumber));
    } else if (started && live) {
      router.replace(orderCheckoutPath(eventSlug, orderNumber));
    }
  }, [paid, started, live, ownedByThisEvent, router, eventSlug, orderNumber]);

  if (isPending) return <Loading label="Loading your order…" />;

  if (isError) {
    const notFound = error instanceof ApiError && error.status === 404;
    return (
      <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
        <StatusAlert>
          {notFound
            ? "We could not find an order with that number. Check the link from your confirmation, or start a new order."
            : "We could not load this order right now. Please try again in a moment."}
        </StatusAlert>
        <p className="mt-4 text-sm">
          <Link href={eventDetailPath(eventSlug)} className="underline">
            Back to the event
          </Link>
        </p>
      </main>
    );
  }

  // An order reached through the wrong event is refused outright rather than
  // redirected to the one that owns it: forwarding would quietly turn a wrong
  // URL into a working one, which is exactly the isolation this route exists to
  // provide (spec 007 FR-013). It outranks the state forwards above.
  if (data.event.slug !== eventSlug) {
    return <WrongEvent eventSlug={eventSlug} />;
  }

  // The order ran out of time (or was cancelled). Spec 011 FR-022: this no
  // longer replaces the page — the forms, the rail, and the summary keep
  // rendering below and the dialog opens over them, so the guest can still see
  // which order died and what they had typed into it.
  //
  // Narrowed to the union rather than kept as a boolean so the dialog's prop
  // type is checked rather than asserted.
  const endedStatus =
    data.status === "EXPIRED" || data.status === "CANCELLED"
      ? data.status
      : null;

  // Both forwards above are one frame away; rendering the forms in between
  // would only flicker. An ended order forwards nowhere, so it must NOT be held
  // on the loader — `payment_started` stays true on an order that expired after
  // checkout, and without this guard that order would show a spinner forever
  // instead of its dialog.
  if (endedStatus === null && (paid || data.payment_started)) {
    return <Loading label="Loading your order…" />;
  }

  // The forms seed themselves from whatever the order already holds (spec 011
  // FR-030). Nothing extra is needed here: the isPending guard above means
  // OrderForms only ever mounts with data in hand, which is what makes seeding
  // once — and never re-syncing — correct.
  return (
    <main className="mx-auto w-full max-w-6xl flex-1 px-6 py-10">
      <OrderForms order={data} />
      {endedStatus && <EndOfJourneyDialog status={endedStatus} eventSlug={eventSlug} />}
    </main>
  );
}

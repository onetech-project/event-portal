"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useEffect } from "react";

import { OrderConfirmation } from "@/components/order/order-confirmation";
import { PaymentStatusCard } from "@/components/order/payment-status-card";
import { WrongEvent } from "@/components/order/wrong-event";
import { Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import { ApiError } from "@/lib/api-client";
import { useOrderDetail } from "@/lib/queries";

/**
 * Where the purchase journey ends.
 *
 * Reached automatically from the order screen the moment an order settles, so it
 * covers every settled outcome — not just a successful payment. An order that
 * expired or was cancelled arrives here too and is told so plainly.
 *
 * It reads the order through the same query the order screen already warmed, so
 * arriving here costs no extra round-trip.
 */
export default function OrderDonePage({
  params,
}: Readonly<{ params: Promise<{ slug: string; orderNumber: string }> }>) {
  const { slug, orderNumber } = use(params);
  return <OrderDoneView eventSlug={slug} orderNumber={orderNumber} />;
}

/**
 * The screen itself, separated from route-param unwrapping so it can be rendered
 * with plain strings.
 */
export function OrderDoneView({
  eventSlug,
  orderNumber,
}: Readonly<{ eventSlug: string; orderNumber: string }>) {
  const router = useRouter();
  const { data, isPending, isError, error } = useOrderDetail(orderNumber);

  // The mirror of the payment screen's forward. An order still awaiting payment
  // has not finished anything, so a guest who reaches this address early — by
  // editing the URL, or from a bookmark saved before paying — belongs back on
  // the payment screen rather than reading a confirmation of nothing.
  const stillPayable = data?.status === "PENDING";
  const ownedByThisEvent = data?.event.slug === eventSlug;

  useEffect(() => {
    if (stillPayable && ownedByThisEvent) {
      router.replace(
        `/events/${encodeURIComponent(eventSlug)}/orders/${encodeURIComponent(orderNumber)}`,
      );
    }
  }, [stillPayable, ownedByThisEvent, router, eventSlug, orderNumber]);

  if (isPending) return <Loading label="Loading your order…" />;

  if (isError) {
    const notFound = error instanceof ApiError && error.status === 404;
    return (
      <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
        <PageHeading title={notFound ? "Order not found" : "Something went wrong"} />
        <StatusAlert>
          {notFound
            ? "We could not find an order with that number. Check the link from your confirmation email."
            : "We could not load this order right now. Please try again in a moment."}
        </StatusAlert>
        <p className="mt-4 text-sm">
          <Link href={`/events/${eventSlug}`} className="underline">
            Back to the event
          </Link>
        </p>
      </main>
    );
  }

  // Same guard as the payment screen, for the same reason: a confirmation is as
  // much of a disclosure as the order itself (spec FR-013).
  if (data.event.slug !== eventSlug) {
    return <WrongEvent eventSlug={eventSlug} />;
  }

  if (data.status === "PAID") {
    return <OrderConfirmation order={data} />;
  }

  // On its way back to the payment screen; see the effect above.
  if (stillPayable) return <Loading label="Loading your order…" />;

  // Expired and cancelled land here too — the journey ended, just not with a
  // purchase. PaymentStatusCard already words both cases and points back to the
  // event, where availability may have changed (spec FR-020).
  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
      <PageHeading
        title={data.status === "EXPIRED" ? "Order expired" : "Order cancelled"}
        subtitle={`Order ${data.order_id}`}
      />
      <PaymentStatusCard
        status={data.status}
        buyerEmail={data.buyer_email}
        eventSlug={eventSlug}
      />
    </main>
  );
}

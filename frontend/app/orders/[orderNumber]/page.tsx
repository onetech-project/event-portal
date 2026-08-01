"use client";

import Link from "next/link";
import { use, useCallback, useEffect, useState } from "react";

import { PaymentStatusCard } from "@/components/order/payment-status-card";
import { QrisPanel } from "@/components/order/qris-panel";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import { ApiError } from "@/lib/api-client";
import { formatCurrency } from "@/lib/format";
import { isFinalOrderStatus, useOrderDetail, useRefreshPaymentStatus } from "@/lib/queries";
import type { PublicOrderDetail } from "@/lib/types";

/**
 * The guest's order page.
 *
 * This is where checkout lands: the order, a QRIS code while it is unpaid, a
 * countdown, and a status that updates itself within seconds of the payment
 * provider confirming — without the guest reloading anything.
 */
export default function OrderPage({
  params,
}: Readonly<{ params: Promise<{ orderNumber: string }> }>) {
  const { orderNumber } = use(params);
  return <OrderView orderNumber={orderNumber} />;
}

/**
 * The page itself, separated from route-param unwrapping so it can be rendered
 * with a plain order number.
 */
export function OrderView({ orderNumber }: Readonly<{ orderNumber: string }>) {
  const { data, isPending, isError, error, isFetching, fetchStatus } =
    useOrderDetail(orderNumber);

  // Once the countdown hits zero the code is dead even though the server's own
  // status flip follows within the sweep interval. Hiding it here keeps a page
  // left open overnight from showing something scannable.
  const [locallyExpired, setLocallyExpired] = useState(false);
  const onExpired = useCallback(() => setLocallyExpired(true), []);

  // A fresh instruction (a new deadline) means the page is live again.
  useEffect(() => {
    if (data?.payment) setLocallyExpired(false);
  }, [data?.payment?.expires_at]);

  if (isPending) return <Loading label="Loading your order…" />;

  if (isError) {
    const notFound = error instanceof ApiError && error.status === 404;
    return (
      <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
        <PageHeading title={notFound ? "Order not found" : "Something went wrong"} />
        <StatusAlert>
          {notFound
            ? "We could not find an order with that number. Check the link from your confirmation, or start a new order."
            : "We could not load this order right now. Please try again in a moment."}
        </StatusAlert>
        <p className="mt-4 text-sm">
          <Link href="/events" className="underline">
            Browse events
          </Link>
        </p>
      </main>
    );
  }

  const showPayment = data.payment !== null && !locallyExpired;

  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
      <PageHeading
        title={data.status === "PAID" ? "Payment successful" : "Complete your payment"}
        subtitle={`Order ${data.order_number}`}
      />

      <div className="space-y-6">
        {showPayment && data.payment ? (
          <>
            <QrisPanel
              payment={data.payment}
              serverTime={data.server_time}
              onExpired={onExpired}
            />
            <StatusChecker
              orderNumber={data.order_number}
              isPolling={fetchStatus !== "paused"}
              isFetching={isFetching}
            />
          </>
        ) : (
          <PaymentStatusCard
            status={locallyExpired && data.status === "PENDING" ? "EXPIRED" : data.status}
            buyerEmail={data.buyer_email}
            eventSlug={data.event.slug}
          />
        )}

        <OrderSummaryCard order={data} />
      </div>
    </main>
  );
}

function OrderSummaryCard({ order }: Readonly<{ order: PublicOrderDetail }>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{order.event.name || "Your order"}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <ul className="space-y-1 text-sm">
          {order.items.map((item) => (
            <li key={item.ticket_type_name} className="flex justify-between gap-4">
              <span>
                {item.ticket_type_name} × {item.quantity}
              </span>
              <span>{formatCurrency(item.subtotal)}</span>
            </li>
          ))}
        </ul>

        <div className="flex justify-between border-t pt-3 text-sm font-semibold">
          <span>Total</span>
          <span>{formatCurrency(order.total_amount)}</span>
        </div>

        <dl className="grid gap-1 text-sm text-muted-foreground">
          <div className="flex justify-between gap-4">
            <dt>Buyer</dt>
            <dd className="text-foreground">{order.buyer_name}</dd>
          </div>
          <div className="flex justify-between gap-4">
            <dt>Email</dt>
            <dd className="text-foreground">{order.buyer_email}</dd>
          </div>
          <div className="flex justify-between gap-4">
            <dt>Status</dt>
            <dd className="text-foreground">{order.status}</dd>
          </div>
        </dl>
      </CardContent>
    </Card>
  );
}

/**
 * The "check payment status" control, plus an honest report of whether the page
 * is currently updating itself.
 */
function StatusChecker({
  orderNumber,
  isPolling,
  isFetching,
}: Readonly<{ orderNumber: string; isPolling: boolean; isFetching: boolean }>) {
  const refresh = useRefreshPaymentStatus(orderNumber);

  // A 429 means "too soon", not "failed": show it as a cooldown the guest can
  // wait out rather than an error (spec FR-018).
  const rateLimited =
    refresh.error instanceof ApiError && refresh.error.status === 429
      ? refresh.error
      : null;
  const failed = refresh.error instanceof ApiError && !rateLimited ? refresh.error : null;

  return (
    <Card>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          <Button
            onClick={() => refresh.mutate()}
            disabled={refresh.isPending || rateLimited !== null}
            variant="outline"
          >
            {refresh.isPending ? "Checking…" : "Check payment status"}
          </Button>

          <p className="text-xs text-muted-foreground">
            {isPolling
              ? "This page updates by itself as soon as your payment is confirmed."
              : "Not updating live right now — we keep retrying, and it recovers on its own."}
            {isFetching ? " Checking…" : ""}
          </p>
        </div>

        {rateLimited ? (
          <StatusAlert tone="info">
            You checked very recently — please wait a few seconds and try again.
          </StatusAlert>
        ) : null}

        {failed ? (
          <StatusAlert>
            We could not reach the payment provider just now. Your payment is unaffected
            — the page keeps checking.
          </StatusAlert>
        ) : null}

        {refresh.isSuccess && !refresh.data.changed ? (
          <StatusAlert tone="info">
            Still waiting for your payment. If you have just paid, it can take a few
            seconds to reach us.
          </StatusAlert>
        ) : null}

        {refresh.isSuccess && refresh.data.changed && !isFinalOrderStatus(refresh.data.status) ? (
          <StatusAlert tone="info">Payment status updated.</StatusAlert>
        ) : null}
      </CardContent>
    </Card>
  );
}

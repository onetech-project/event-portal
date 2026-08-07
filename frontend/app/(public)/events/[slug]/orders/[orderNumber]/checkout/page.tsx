"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";

import { AlarmClock, ChevronDown, QrCode } from "lucide-react";

import { EndOfJourneyDialog } from "@/components/order/end-of-journey-dialog";
import { ExpiryCountdown } from "@/components/order/expiry-countdown";
import { OrderSummaryPanel } from "@/components/order/order-summary-panel";
import { QrisPanel } from "@/components/order/qris-panel";
import { WrongEvent } from "@/components/order/wrong-event";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Loading, StatusAlert } from "@/components/ui/feedback";
import { ApiError } from "@/lib/api-client";
import { useCheckoutStatus } from "@/lib/checkout-status";
import { formatCurrency } from "@/lib/format";
import { orderDonePath, orderFormsPath } from "@/lib/order-routes";
import {
  isFinalOrderStatus,
  QR_REFRESH_BEFORE_EXPIRY_MS,
  useOrderDetail,
  useRefreshPaymentStatus,
  useRefreshQR,
} from "@/lib/queries";

/**
 * The QRIS payment screen — the Payment stage of the rail.
 *
 * Spec 011 FR-020 split it out of the order page, which used to serve both the
 * ticket holder forms and this screen at one address. Sharing an address meant
 * the rail could not tell the two stages apart and marked Registration
 * finished while the guest was still typing into it.
 *
 * What lives here is everything that is only true once payment has started:
 * the QRIS code and its 7-minute refresh, the Complete Purchase countdown, and
 * the status poll. The forms page keeps everything that is only true before.
 */
export default function CheckoutPage({
  params,
}: Readonly<{ params: Promise<{ slug: string; orderNumber: string }> }>) {
  const { slug, orderNumber } = use(params);
  return <CheckoutView eventSlug={slug} orderNumber={orderNumber} />;
}

/**
 * The screen itself, separated from route-param unwrapping so it can be
 * rendered with a plain order number.
 */
export function CheckoutView({
  eventSlug,
  orderNumber,
}: Readonly<{ eventSlug: string; orderNumber: string }>) {
  const router = useRouter();
  const { data, isPending, isError, error, isFetching, fetchStatus } =
    useOrderDetail(orderNumber);

  // Live status over SSE; the 3-second poll above stays running as the
  // mandated fallback, so a dropped stream degrades with no mode switch.
  useCheckoutStatus(orderNumber, !isFinalOrderStatus(data?.status));

  // The 7-minute QR refresh (spec 008 FR-015): a QRIS payload's practical
  // scan-life is shorter than the payment window, so a fresh code is fetched
  // before the old one goes stale. The moment is derived from the server-owned
  // deadline — QR_REFRESH_AFTER (7m) into a PAYMENT_WINDOW (14m) is 7 minutes
  // before it — using server_time so a wrong device clock cannot skew it. On
  // failure the old QR stays on screen; the server kept it live too.
  const refreshQR = useRefreshQR(orderNumber);
  const [qrVersion, setQrVersion] = useState(0);
  const paymentExpiresAt = data?.payment?.expires_at;
  const serverTime = data?.server_time;
  useEffect(() => {
    if (paymentExpiresAt === undefined || serverTime === undefined) return;
    const delay =
      Date.parse(paymentExpiresAt) - Date.parse(serverTime) - QR_REFRESH_BEFORE_EXPIRY_MS;
    if (delay <= 0) return;
    const timer = setTimeout(() => {
      refreshQR.mutate(undefined, { onSuccess: () => setQrVersion((v) => v + 1) });
    }, delay);
    return () => clearTimeout(timer);
    // refreshQR is a stable mutation handle; keying on the deadline re-arms
    // the timer exactly when a new instruction appears.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [paymentExpiresAt, serverTime]);

  // Once the countdown hits zero the code is dead even though the server's own
  // status flip follows within the sweep interval. Hiding it here keeps a page
  // left open overnight from showing something scannable.
  //
  // What is remembered is *which* deadline ran out, not a bare flag, so a fresh
  // instruction (a new deadline) makes the page live again on its own — no
  // effect needed to undo the flag.
  const expiresAt = data?.payment?.expires_at ?? null;
  const [expiredDeadline, setExpiredDeadline] = useState<string | null>(null);
  const onExpired = useCallback(() => setExpiredDeadline(expiresAt), [expiresAt]);
  const locallyExpired = expiredDeadline !== null && expiredDeadline === expiresAt;

  // Two forwards, both `replace` (spec 011 FR-021). A PAID order belongs on the
  // confirmation, whether it settled while the guest watched — the poll notices
  // within seconds — or was already settled when they opened a saved link. An
  // order whose payment never started has no QR to show and belongs back on the
  // forms. Using replace rather than push is what stops the back button
  // bouncing between two screens that forward to each other.
  //
  // EXPIRED and CANCELLED are deliberately not forwarded: the journey ended
  // without a purchase, and a congratulations screen would misreport that.
  const paid = data?.status === "PAID";
  const notStarted = data !== undefined && !data.payment_started;
  const ownedByThisEvent = data?.event.slug === eventSlug;
  const live = data?.status === "PENDING";

  useEffect(() => {
    if (!ownedByThisEvent) return;
    if (paid) {
      router.replace(orderDonePath(eventSlug, orderNumber));
    } else if (notStarted && live) {
      router.replace(orderFormsPath(eventSlug, orderNumber));
    }
  }, [paid, notStarted, live, ownedByThisEvent, router, eventSlug, orderNumber]);

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
          <Link href={`/events/${eventSlug}`} className="underline">
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
  // longer replaces the screen — the countdown banner and the order summary
  // keep rendering, and the dialog opens over them, so the guest still sees
  // which order ended and where they were.
  //
  // `locallyExpired` is folded in deliberately: it is what opens the dialog the
  // instant the countdown reaches zero rather than one poll later. When the
  // server's own EXPIRED lands moments after, `data.payment` goes null, that
  // latch falls back to false, and the first term takes over — the screen does
  // not change. One expiry, one message.
  const endedStatus =
    data.status === "EXPIRED" || data.status === "CANCELLED"
      ? data.status
      : locallyExpired
        ? ("EXPIRED" as const)
        : null;

  // Both forwards above are one frame away; a panel rendered in between would
  // only flicker. An ended order forwards nowhere, so it must NOT be held on
  // the loader — a CANCELLED order whose payment never started would otherwise
  // spin forever instead of showing its dialog.
  if (endedStatus === null && (paid || !data.payment_started)) {
    return <Loading label="Loading your order…" />;
  }

  // Nothing scannable is left behind the dialog: on an ended order the QR panel
  // and the status checker are not rendered at all (FR-022).
  const showPayment = data.payment !== null && endedStatus === null;

  // Figma 32-1366 / 203-1157: the Complete Purchase countdown banner spans the
  // page; below it, Scan to Pay on the left and the shared Order Summary stub
  // panel — with the collapsible how-to-pay — on the right.
  return (
    <main className="mx-auto w-full max-w-6xl flex-1 px-6 py-10">
      {showPayment && data.payment ? (
        <div className="flex flex-wrap items-center justify-between gap-4 rounded-xl bg-brand-surface px-5 py-4">
          <div className="flex items-center gap-3">
            <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-background text-brand">
              <AlarmClock aria-hidden className="size-5" />
            </span>
            <div>
              <p className="font-semibold">Complete Purchase</p>
              <p className="text-xs text-muted-foreground">
                The order will be automatically canceled if the time expires.
              </p>
            </div>
          </div>
          <ExpiryCountdown
            variant="digits"
            expiresAt={data.payment.expires_at}
            serverTime={data.server_time}
            onExpired={onExpired}
          />
        </div>
      ) : null}

      <div className="mt-6 grid gap-8 lg:grid-cols-[minmax(0,1fr)_380px] lg:items-start">
        <div className="space-y-6">
          {/* The inline "This payment code has expired" notice that used to sit
              in this branch is gone (FR-022): the dialog below is the single
              message for an ended order, and two of them for one expiry was
              exactly the duplication the requirement exists to remove. */}
          {showPayment && data.payment ? (
            <>
              <QrisPanel payment={data.payment} cacheBust={qrVersion} />
              <StatusChecker
                orderNumber={data.order_id}
                isPolling={fetchStatus !== "paused"}
                isFetching={isFetching}
              />
            </>
          ) : null}
        </div>

        <aside className="lg:sticky lg:top-6">
          <OrderSummaryPanel
            order={data}
            afterTotal={
              <>
                <HowToPay amount={data.payment !== null ? data.payment.amount : data.total_amount} />
                <Button size="lg" disabled className="w-full">
                  Waiting for payment…
                </Button>
              </>
            }
          />
        </aside>
      </div>

      {endedStatus && <EndOfJourneyDialog status={endedStatus} />}
    </main>
  );
}

/**
 * The "How to pay with QRIS" collapsible (Figma 32-1366 collapsed /
 * 203-1157 expanded), sitting between the total and the status button.
 */
function HowToPay({ amount }: Readonly<{ amount: string }>) {
  return (
    <Collapsible className="border-y py-3">
      <CollapsibleTrigger className="group flex w-full items-center justify-between gap-2 text-sm font-medium">
        <span className="flex items-center gap-2">
          <QrCode aria-hidden className="size-4" />
          How to pay with QRIS
        </span>
        <ChevronDown
          aria-hidden
          className="size-4 transition-transform group-data-panel-open:rotate-180"
        />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <ol className="list-decimal space-y-2 pt-3 pl-5 text-sm text-muted-foreground">
          <li>
            Open your m-banking or e-wallet app (Gopay, OVO, Dana, LinkAja, BCA
            mobile, Livin&apos;, etc.).
          </li>
          <li>Select the Scan QR or Pay menu.</li>
          <li>Point the camera at the QR Code shown on the left.</li>
          <li>
            Ensure the merchant name matches the organizer and the payment
            amount is correct ({formatCurrency(amount)}).
          </li>
          <li>Confirm the payment and enter your PIN.</li>
          <li>Save your payment receipt.</li>
        </ol>
      </CollapsibleContent>
    </Collapsible>
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
  // wait out rather than an error (spec 008 FR-018).
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

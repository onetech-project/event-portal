"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { use, useCallback, useEffect, useState } from "react";

import { Clock } from "lucide-react";

import { EndOfJourneyDialog } from "@/components/order/end-of-journey-dialog";
import { ExpiryCountdown } from "@/components/order/expiry-countdown";
import { OrderSummaryPanel } from "@/components/order/order-summary-panel";
import { QrisInstructions } from "@/components/order/qris-instructions";
import { QrisPanel } from "@/components/order/qris-panel";
import { WrongEvent } from "@/components/order/wrong-event";
import { Button, buttonVariants } from "@/components/ui/button";
import { Loading, StatusAlert } from "@/components/ui/feedback";
import { ApiError } from "@/lib/api-client";
import { useCheckoutStatus } from "@/lib/checkout-status";
import { orderDonePath, orderFormsPath } from "@/lib/order-routes";
import { isFinalOrderStatus, useOrderDetail } from "@/lib/queries";
import { cn } from "@/lib/utils";

/**
 * The QRIS payment screen — the Payment stage of the rail.
 *
 * Spec 011 FR-020 split it out of the order page, which used to serve both the
 * ticket holder forms and this screen at one address. Sharing an address meant
 * the rail could not tell the two stages apart and marked Registration
 * finished while the guest was still typing into it.
 *
 * What lives here is everything that is only true once payment has started: the
 * QRIS code, the Complete Purchase countdown, and the live status stream.
 *
 * Two things this screen used to have are gone, and their absence is the point.
 * The mid-window QR refresh: one order now gets one code for one window, and
 * that window is the gateway's own. The "check payment status" button: there is
 * no call behind it any more — the gateway offers nothing that reads a
 * transaction's status — and a button that only re-read our own database would
 * be useless in exactly the case it existed for.
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
  const { data, isPending, isError, error } = useOrderDetail(orderNumber);

  // Live status over SSE, and nothing repeating underneath it. The hook watches
  // the stream's own liveness and only falls back to periodic reads while it is
  // genuinely failing, so a healthy page makes no repeating requests at all.
  const { degraded } = useCheckoutStatus(
    orderNumber,
    !isFinalOrderStatus(data?.status),
  );

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

  // Nothing scannable is left behind the dialog (FR-022, US5 scenario 7) — but
  // the panel itself stays. The server stops issuing a payment instruction the
  // moment an order ends, so `data.payment` is already null here and the frame
  // renders its expired face: merchant identity intact, no code. Blanking the
  // whole column left the page a void beside the dialog, which read as broken
  // rather than as finished.
  const showPayment = data.payment !== null && endedStatus === null;

  // Figma 32-1366 / 203-1157: the Complete Purchase countdown banner spans the
  // page; below it, Scan to Pay on the left and the shared Order Summary stub
  // panel — with the collapsible how-to-pay — on the right.
  return (
    <main className="mx-auto w-full max-w-6xl flex-1 px-6 py-10">
      {/* The banner outlives the deadline it counts. Removing it at zero took
          the countdown off the screen at the one moment the guest was watching
          it, which read as the page breaking; leaving it at 0 : 00 shows the
          window closing rather than the page losing its nerve. It adds no
          second expiry message — the dialog is still the only one (FR-022). */}
      <div className="flex flex-wrap items-center justify-between gap-4 rounded-xl bg-brand-surface px-5 py-4 border border-destructive/20">
        <div className="flex items-center gap-3">
          <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-background text-brand">
            <Clock aria-hidden className="size-5" />
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
          // Null once the order has ended: the server stops issuing an
          // instruction, so there is no deadline left to count and the clock
          // holds at zero.
          expiresAt={showPayment && data.payment ? data.payment.expires_at : null}
          serverTime={data.server_time}
          onExpired={onExpired}
        />
      </div>

      <div className="mt-6 grid gap-8 lg:grid-cols-[minmax(0,1fr)_380px] lg:items-start">
        <div className="space-y-6">
          {/* The inline "This payment code has expired" notice that used to sit
              in this branch is gone (FR-022): the dialog below is the single
              message for an ended order, and two of them for one expiry was
              exactly the duplication the requirement exists to remove. */}
          <QrisPanel
            payment={showPayment ? data.payment : null}
            totalAmount={data.total_amount}
            endedStatus={endedStatus}
          />
          {/* Said only when it is true. A page that always claimed to be
              updating live would be indistinguishable from one that had
              silently stopped — which is the failure the watchdog exists to
              make visible, so hiding it here would give the detection away
              again. */}
          {showPayment && degraded ? (
            <StatusAlert tone="info">
              Live updates are not getting through right now, so we are checking
              every few seconds instead. Your payment is unaffected — this page
              still updates itself once it is confirmed.
            </StatusAlert>
          ) : null}
        </div>

        <aside className="lg:sticky lg:top-6">
          <OrderSummaryPanel
            order={data}
            afterTotal={
              <>
                <QrisInstructions
                  amount={data.payment !== null ? data.payment.amount : data.total_amount}
                />
                <PrimaryAction
                  status={data.status}
                  href={orderDonePath(eventSlug, orderNumber)}
                />
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
 * The panel's primary action, which is state-driven rather than static.
 *
 * While the order is pending there is nothing for a guest to press — payment
 * happens in their banking app, not here — so the control is inert and says what
 * the page is waiting for. Once the payment lands it becomes the way on to the
 * tickets. The page also forwards there on its own; the button is for the case
 * where a guest is looking at the screen rather than at a redirect.
 */
function PrimaryAction({
  status,
  href,
}: Readonly<{ status: string; href: string }>) {
  if (status === "PAID") {
    return (
      <Link href={href} className={cn(buttonVariants({ size: "lg" }), "w-full")}>
        View your tickets
      </Link>
    );
  }

  return (
    <Button size="lg" disabled className="w-full disabled:bg-muted disabled:text-muted-foreground">
      Waiting for payment…
    </Button>
  );
}

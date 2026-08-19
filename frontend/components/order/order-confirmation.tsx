"use client";

import { Check, RefreshCw, Ticket } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { formatCurrency } from "@/lib/format";
import { useGuestResendTicketEmail } from "@/lib/queries";
import { ApiError } from "@/lib/api-client";
import type { ResendRetryAfter, TicketOrderDetail } from "@/lib/types";

/**
 * The end of the purchase journey: the guest's receipt.
 *
 * Deliberately reads as a record rather than a status page — the order screen
 * they came from was the status page, and by the time they arrive here nothing
 * about the order can change again.
 */
export function OrderConfirmation({
  order,
  event
}: Readonly<{ order: TicketOrderDetail; event: string }>) {
  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
      <Card className="items-center gap-6 py-6">
        <CardContent className="flex w-full flex-col items-center gap-6">
          <div className="flex flex-col items-center gap-3 text-center">
            <span
              aria-hidden
              className="flex size-20 items-center justify-center rounded-full bg-emerald-50 dark:bg-emerald-950"
            >
              <Check size={36} strokeWidth={3} className="text-emerald-500" />
            </span>

            <h1 className="text-2xl font-extrabold tracking-tight">
              Payment Successful!
            </h1>

            <p className="text-base text-muted-foreground">
              Thank you for your purchase. Your payment for{" "}
              <span className="font-semibold text-foreground">
                {order.event.name}
              </span>{" "}
              has been successfully processed.
            </p>
          </div>

          <Receipt order={order} />

          <Button render={<Link href={`/events/${event}`} />} variant="outline" size="lg">
            Back to home
          </Button>
        </CardContent>
      </Card>
    </main>
  );
}

function Receipt({ order }: Readonly<{ order: TicketOrderDetail }>) {
  return (
    <section
      aria-label="Order receipt"
      className="w-full rounded-2xl border bg-muted/40 p-5"
    >
      <div className="flex flex-wrap items-start justify-between gap-4 border-b pb-4">
        <div className="space-y-1">
          <h2 className="text-xs font-semibold tracking-[0.6px] text-muted-foreground uppercase">
            Booking ID
          </h2>
          {/* The real order number, not a prettier short code: it is what the
              guest quotes to support and what their email carries. */}
          <p className="font-mono text-lg font-bold break-all">
            {order.order_id}
          </p>
        </div>

        <div className="space-y-1 text-right">
          <h2 className="text-xs font-semibold tracking-[0.6px] text-muted-foreground uppercase">
            Total paid
          </h2>
          <p className="text-2xl font-extrabold text-brand">
            {formatCurrency(order.total_amount)}
          </p>
        </div>
      </div>

      <ul className="space-y-5 border-b py-5">
        {order.items.map((item, index) => (
          <li key={index} className="flex items-center gap-4">
            <Ticket
              aria-hidden
              size={18}
              className="shrink-0 text-muted-foreground"
            />
            <div className="min-w-0 space-y-1">
              <p className="font-bold wrap-break-word">
                {item.kind === "package"
                  ? item.package_name
                  : item.ticket_type_name}
              </p>
              <p className="text-sm text-muted-foreground">
                Quantity: {item.quantity}{" "}
                {item.quantity === 1 ? "ticket" : "tickets"}
              </p>
            </div>
          </li>
        ))}
      </ul>

      <ResendRow orderNumber={order.order_id} />
    </section>
  );
}

/**
 * The recovery path for a confirmation email that never arrived.
 *
 * The address is never shown or asked for — the server sends to the buyer on
 * record, and this button carries only the order number.
 */
function ResendRow({ orderNumber }: Readonly<{ orderNumber: string }>) {
  const resend = useGuestResendTicketEmail(orderNumber);

  // Seconds left before another resend will be accepted, held here and nowhere
  // else. Deliberately not persisted and not shared between tabs (FR-021p): the
  // cooldown lives in the server's memory and does not survive a restart, so a
  // deadline the browser remembered could outlast the limit it describes and
  // hold a guest back from a resend the server would have accepted. A fresh load
  // arms the button; the first press resolves the truth either way.
  const [secondsLeft, setSecondsLeft] = useState(0);

  // A 429 means "too soon", not "failed" — opposite remedies, so they must not
  // read alike (FR-021q).
  const rateLimited =
    resend.error instanceof ApiError && resend.error.status === 429;
  const failed = resend.error !== null && !rateLimited;

  // The wait always comes from the server, never from this device's clock or a
  // window compiled into the page (FR-021j). Both answers carry it: the
  // acceptance reports the window it just started, the refusal what is left of
  // the one already running. Seeded from the mutation's own callbacks rather
  // than from an effect watching its result — the countdown is a reaction to an
  // event, not state to be kept in sync with a render.
  const press = () =>
    resend.mutate(undefined, {
      onSuccess: (data) => startCountdown(data.retry_after_seconds),
      onError: (error) => startCountdown(retryAfterSeconds(error)),
    });

  // A server that names no wait gets the floor, not a free button. Zero seconds
  // would leave the control live and turn a held key into a burst of refusals —
  // the exact loop the countdown exists to break, wearing a different hat. The
  // floor is not a guess at the real window: it is short on purpose, so the next
  // press asks the server again rather than stranding a guest behind a number
  // this page invented.
  const startCountdown = (seconds: number) => {
    setSecondsLeft(seconds > 0 ? seconds : FALLBACK_COOLDOWN_SECONDS);
  };

  // Ticking down re-arms the button on its own (FR-021o). Without this the guest
  // whose email genuinely never arrived is left with a dead control and no way
  // to ask again short of reloading.
  useEffect(() => {
    if (secondsLeft <= 0) return;
    const timer = setTimeout(() => setSecondsLeft((s) => s - 1), 1000);
    return () => clearTimeout(timer);
  }, [secondsLeft]);

  const waiting = secondsLeft > 0;

  return (
    <div className="flex flex-wrap items-center justify-between gap-4 pt-4">
      <div className="min-w-0 flex-1 space-y-1">
        <p className="text-sm text-muted-foreground">
          An email confirmation has been sent to your registered email address.
        </p>

        {/* The acceptance carries the window it just started, so the wait is
            shown here rather than held back until a refusal reveals it
            (FR-021j). Seeding the countdown without displaying it would leave
            the guest with a button that is disabled for no stated reason. */}
        {resend.isSuccess ? (
          <p role="status" className="text-sm font-medium text-emerald-600">
            Email sent again. Please check your inbox and spam folder.
            {waiting ? ` You can ask again in ${secondsLeft}s.` : ""}
          </p>
        ) : null}

        {/* Gated on the countdown, not on the error alone: once the wait is over
            the refusal is history, and leaving it on screen beside a live button
            tells the guest not to press the thing they may now press. */}
        {rateLimited && waiting ? (
          <p
            role="status"
            className="text-sm font-medium text-muted-foreground"
          >
            That email was just sent. You can ask again in {secondsLeft}s.
          </p>
        ) : null}

        {failed ? (
          <p role="status" className="text-sm font-medium text-destructive">
            We could not send it just now. Please try again shortly.
          </p>
        ) : null}
      </div>

      <Button
        onClick={press}
        disabled={resend.isPending || waiting}
        size="lg"
        className="bg-brand-surface text-brand hover:bg-brand-line/40"
      >
        <RefreshCw
          aria-hidden
          size={20}
          className={resend.isPending ? "animate-spin" : ""}
        />
        {resend.isPending ? "Sending…" : "Resend email"}
      </Button>
    </div>
  );
}

/**
 * How long to hold the button when the server named no wait at all.
 *
 * Deliberately far shorter than any real window: it is a floor that stops a
 * burst, not an estimate of the cooldown. Whatever the true remaining time is,
 * the next press learns it from the server.
 */
const FALLBACK_COOLDOWN_SECONDS = 5;

/** The seconds a 429 reports, from the error envelope's detail payload. */
function retryAfterSeconds(error: unknown): number {
  if (!(error instanceof ApiError)) return 0;
  const data = error.data as ResendRetryAfter | null;
  return typeof data?.retry_after_seconds === "number"
    ? data.retry_after_seconds
    : 0;
}

"use client";

import { Check, RefreshCw, Ticket } from "lucide-react";
import Link from "next/link";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { formatCurrency } from "@/lib/format";
import { useGuestResendTicketEmail } from "@/lib/queries";
import { ApiError } from "@/lib/api-client";
import type { TicketOrderDetail } from "@/lib/types";

/**
 * The end of the purchase journey: the guest's receipt.
 *
 * Deliberately reads as a record rather than a status page — the order screen
 * they came from was the status page, and by the time they arrive here nothing
 * about the order can change again.
 */
export function OrderConfirmation({ order }: Readonly<{ order: TicketOrderDetail }>) {
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

            <h1 className="text-2xl font-extrabold tracking-tight">Payment Successful!</h1>

            <p className="text-base text-muted-foreground">
              Thank you for your purchase. Your payment for{" "}
              <span className="font-semibold text-foreground">{order.event.name}</span> has
              been successfully processed.
            </p>
          </div>

          <Receipt order={order} />

          <Button render={<Link href="/" />} variant="outline" size="lg">
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
          <p className="font-mono text-lg font-bold break-all">{order.order_id}</p>
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
            <Ticket aria-hidden size={18} className="shrink-0 text-muted-foreground" />
            <div className="min-w-0 space-y-1">
              <p className="font-bold wrap-break-word">
                {item.kind === "package" ? item.package_name : item.ticket_type_name}
              </p>
              <p className="text-sm text-muted-foreground">
                Quantity: {item.quantity} {item.quantity === 1 ? "ticket" : "tickets"}
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

  // A 429 means "too soon", not "failed": the endpoint allows one send per
  // minute per order, so this is a cooldown the guest can wait out.
  const rateLimited = resend.error instanceof ApiError && resend.error.status === 429;
  const failed = resend.error !== null && !rateLimited;

  return (
    <div className="flex flex-wrap items-center justify-between gap-4 pt-4">
      <div className="min-w-0 flex-1 space-y-1">
        <p className="text-sm text-muted-foreground">
          An email confirmation has been sent to your registered email address.
        </p>

        {resend.isSuccess ? (
          <p role="status" className="text-sm font-medium text-emerald-600">
            Sent again — check your inbox in a moment.
          </p>
        ) : null}

        {rateLimited ? (
          <p role="status" className="text-sm font-medium text-muted-foreground">
            Just sent. Please wait a minute before asking again.
          </p>
        ) : null}

        {failed ? (
          <p role="status" className="text-sm font-medium text-destructive">
            We could not send it just now. Please try again shortly.
          </p>
        ) : null}
      </div>

      <Button
        onClick={() => resend.mutate()}
        disabled={resend.isPending || rateLimited}
        size="lg"
        className="bg-brand-surface text-brand hover:bg-brand-line/40"
      >
        <RefreshCw aria-hidden size={20} className={resend.isPending ? "animate-spin" : ""} />
        {resend.isPending ? "Sending…" : "Resend email"}
      </Button>
    </div>
  );
}

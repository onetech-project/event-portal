"use client";

import { CalendarDays, Lock, MapPin, ReceiptText } from "lucide-react";

import { TicketNotch } from "@/components/booking/ticket-notch";
import { formatCurrency, formatDateRange } from "@/lib/format";
import type { TicketOrderDetail } from "@/lib/types";

/**
 * The Order Summary ticket-stub panel shared by the registration and payment
 * phases (Figma 206-3145 / 32-1366): icon-chip header, event box, TICKETS,
 * a notched dashed tear, then the total. What sits around the total differs
 * per phase, so it arrives through the two slots:
 *
 * - `beforeTotal` — between the tear and the total (registration: the QRIS
 *   payment-method radio).
 * - `afterTotal` — between the total and the SECURE CHECKOUT line (the phase's
 *   action button; payment adds the how-to-pay collapsible).
 *
 * No `overflow-hidden` on the panel: the notches straddle the border by 1px
 * and would be clipped.
 */
export function OrderSummaryPanel({
  order,
  beforeTotal,
  afterTotal,
}: {
  order: TicketOrderDetail;
  beforeTotal?: React.ReactNode;
  afterTotal?: React.ReactNode;
}) {
  return (
    <div className="rounded-xl border bg-card">
      {/* Icon chip + title only — no booking id line (Figma 206-3145). */}
      <header className="flex items-center gap-3 px-4 py-4">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-muted">
          <ReceiptText aria-hidden className="size-4" />
        </span>
        <h2 className="text-2xl font-bold">Order Summary</h2>
      </header>

      <div className="space-y-4 px-4.5">
        {/* Event box (Figma 12-4456): venue, address, dates, gate-open time
            from the extended GET /ticket/order/:order_id event object. */}
        <div className="rounded-lg bg-slate-50 p-3">
          <p className="text-xs font-semibold uppercase text-muted-foreground">Event</p>
          <p className="pt-1 font-semibold">{order.event.name}</p>
          <div className="mt-2 space-y-2 text-sm">
            <div className="flex items-start gap-2">
              <MapPin aria-hidden className="mt-0.5 size-4 shrink-0 text-brand" />
              <span>
                <span className="block font-medium">{order.event.venue}</span>
                <span className="block text-xs text-muted-foreground">
                  {order.event.address}
                </span>
              </span>
            </div>
            <div className="flex items-start gap-2">
              <CalendarDays aria-hidden className="mt-0.5 size-4 shrink-0 text-brand" />
              <span>
                <span className="block font-medium">
                  {formatDateRange(order.event.start_date, order.event.end_date)}
                </span>
                <span className="block text-xs text-muted-foreground">
                  Gate opens at {gateTime(order.event.start_date)}
                </span>
              </span>
            </div>
          </div>
        </div>

        <div>
          <p className="text-xs font-semibold uppercase text-muted-foreground">Tickets</p>
          <ul className="mt-2 space-y-3 text-sm">
            {order.items.map((item, index) => (
              <li key={index} className="flex items-start justify-between gap-3">
                <span className="font-semibold">
                  {item.kind === "package" ? item.package_name : item.ticket_type_name}
                </span>
                <span className="flex shrink-0 flex-col items-end gap-1">
                  <span className="font-semibold">{formatCurrency(item.subtotal)}</span>
                  <span className="rounded bg-brand-surface px-1.5 py-0.5 text-xs font-medium text-brand">
                    x{item.quantity}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </div>
      </div>

      {/* The stub tear: a dashed rule notched into both edges of the panel,
          same as the selection summary (Figma 12-4456). */}
      <div className="relative my-4 px-6">
        <TicketNotch side="left" />
        <TicketNotch side="right" />
        <hr className="border-t border-dashed" />
      </div>

      <div className="space-y-4 px-4.5 pb-4">
        {beforeTotal}

        <div className="flex items-baseline justify-between">
          <div>
            <p className="text-xs font-semibold uppercase text-muted-foreground">
              Total payment
            </p>
            <p className="text-xs text-muted-foreground">Includes all taxes and fees</p>
          </div>
          <p className="text-xl font-bold text-brand">{formatCurrency(order.total_amount)}</p>
        </div>

        {afterTotal}

        <p className="flex items-center justify-center gap-1.5 text-xs font-medium uppercase tracking-wide text-muted-foreground">
          <Lock aria-hidden className="size-3.5" /> Secure checkout
        </p>
      </div>
    </div>
  );
}

/** The order's total unit count across its lines, for "Subtotal (N items)". */
function totalQuantity(order: TicketOrderDetail): number {
  return order.items.reduce((sum, item) => sum + item.quantity, 0);
}

/** The event's gate-open wall-clock time in Jakarta, e.g. "15:00 WIB". */
function gateTime(startDate: string): string {
  const date = new Date(startDate);
  if (Number.isNaN(date.getTime())) return "—";
  const clock = new Intl.DateTimeFormat("en-GB", {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
    timeZone: "Asia/Jakarta",
  }).format(date);
  return `${clock} WIB`;
}

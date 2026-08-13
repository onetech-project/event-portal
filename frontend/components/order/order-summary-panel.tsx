"use client";

import { CalendarDays, Lock, MapPin, ReceiptText } from "lucide-react";

import { TicketNotch } from "@/components/booking/ticket-notch";
import { formatAdmissionDates } from "@/lib/admission-dates";
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
  phase,
}: {
  order: TicketOrderDetail;
  beforeTotal?: React.ReactNode;
  afterTotal?: React.ReactNode;
  /**
   * Which step is rendering. This selects BOTH the itemized fee rows and the
   * figure on the closing line, because after spec 011 FR-016 the two are one
   * decision (constitution v4.0.0, fee presentation):
   *
   * - `registration` — the pre-fee subtotal, no fee rows, and a note saying the
   *   fees arrive at the next step.
   * - `payment` — the fee-inclusive total, itemized above it.
   *
   * Deliberately has no default. Its predecessor was a `showFeeBreakdown`
   * boolean, and the moment the flag also began selecting the headline figure
   * that name described half of what it did.
   */
  phase: "registration" | "payment";
}) {
  // FR-016: the forms step shows the pre-fee subtotal, and the fee-inclusive
  // total first appears on the payment step, where the breakdown explaining it
  // appears with it.
  //
  // `??` and not `||`: "0.00" is a real subtotal — a free order must render
  // Rp 0, not fall through to the total. Only a null subtotal takes the
  // fallback, and those orders predate fees, so their stored total already
  // excludes fees and the fallback is exact rather than approximate (FR-016c).
  const headline =
    phase === "registration"
      ? (order.subtotal ?? order.total_amount)
      : order.total_amount;

  return (
    <div className="rounded-xl border bg-card">
      {/* Icon chip + title (Figma 206-3145). The card deliberately carries no
          Booking ID (FR-013) and no per-unit price on its ticket lines
          (FR-014), both amended to this design on 2026-08-13. The Booking ID is
          disclosed on the confirmation screen and in the receipt; the unit price
          stays the figure each line subtotal is derived from. */}
      <header className="flex items-center gap-3 px-4 py-4">
        <span className="flex size-9 shrink-0 items-center justify-center rounded-full bg-muted">
          <ReceiptText aria-hidden className="size-4" />
        </span>
        <div>
          <h2 className="text-2xl font-bold">Order Summary</h2>
        </div>
      </header>

      <div className="space-y-4 px-4.5">
        {/* Event box (Figma 12-4456): venue, address, dates, gate-open time
            from the extended GET /ticket/order/:order_id event object. */}
        <div className="rounded-lg bg-slate-50 p-3">
          <p className="text-[10px] font-semibold uppercase text-muted-foreground tracking-widest">
            Event
          </p>
          <p className="pt-1 font-semibold">{order.event.name}</p>
          <div className="mt-2 space-y-2 text-sm">
            <div className="flex items-start gap-2">
              <MapPin
                aria-hidden
                className="mt-0.5 size-4 shrink-0 text-brand"
              />
              <span>
                <span className="block font-medium">{order.event.venue}</span>
                <span className="block text-xs text-muted-foreground">
                  {order.event.address}
                </span>
              </span>
            </div>
            <div className="flex items-start gap-2">
              <CalendarDays
                aria-hidden
                className="mt-0.5 size-4 shrink-0 text-brand"
              />
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
          <p className="text-[10px] font-semibold uppercase text-muted-foreground tracking-widest">
            Tickets
          </p>
          <ul className="flex flex-col gap-3 text-sm">
            {order.items.map((item, index) => (
              <li key={index} className="flex items-start justify-between gap-3">
                <span className="flex flex-col font-semibold">
                  <p>
                    {item.kind === "package"
                      ? item.package_name
                      : item.ticket_type_name}
                  </p>
                  {/* Every day this line admits on (spec 015 FR-010, FR-021a).
                      Two lines of one order can differ — a Day 1 pass and a Day 2
                      pass — and a bundle names both of its days on one line,
                      which reading a single collapsed date used to hide. */}
                  <p
                    data-testid="order-line-date"
                    className="text-muted-foreground text-xs font-normal"
                  >
                    {formatAdmissionDates(item.admission_starts)}
                  </p>
                </span>
                <span className="flex shrink-0 flex-col items-end gap-1">
                  <span className="font-semibold">
                    {formatCurrency(item.subtotal)}
                  </span>
                  <span className="rounded bg-brand-surface px-1.5 py-0.5 text-xs font-medium text-brand">
                    x{item.quantity}
                  </span>
                </span>
              </li>
            ))}
          </ul>
        </div>
        {/* Payment breakdown (spec 011 FR-016): Ticket Total then the frozen
            per-fee rows (collectively the "Tax & Service Fee"). This belongs to
            the payment phase alone — the registration phase shows no fee in any
            form, neither itemized here nor folded into the closing figure. An
            order predating fees (null subtotal) has nothing to itemize either,
            and collapses to its total. */}
        {phase === "payment" && order.subtotal !== null ? (
          <dl className="space-y-1.5 pb-3 text-sm bg-slate-50 p-3 rounded-lg border-slate-500">
            <div className="flex items-baseline justify-between">
              <dt className="text-muted-foreground">Subtotal ({order.items.length} items)</dt>
              <dd className="font-medium">{formatCurrency(order.subtotal)}</dd>
            </div>
            {order.fees.map((fee) => (
              <div key={fee.name} className="flex items-baseline justify-between">
                <dt className="text-muted-foreground">{fee.name}</dt>
                <dd className="font-medium">{formatCurrency(fee.amount)}</dd>
              </div>
            ))}
          </dl>
        ) : null}
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
            {/* The heading above stays "Total payment" on both phases
                (FR-016a); this line is what distinguishes them. On the forms
                step the old "Includes all taxes and fees" would sit above a
                fee-free number and understate what the guest is about to be
                charged. */}
            <p className="text-xs text-muted-foreground">
              {phase === "registration"
                ? "Excludes taxes and fees"
                : "Includes all taxes and fees"}
            </p>
          </div>
          <p data-testid="order-total-figure" className="text-xl font-bold text-brand">
            {formatCurrency(headline)}
          </p>
        </div>

        {afterTotal}

        <p className="flex items-center justify-center gap-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
          <Lock aria-hidden className="size-3.5" /> Secure checkout
        </p>
      </div>
    </div>
  );
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

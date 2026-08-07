"use client";

import { TermsDialog } from "@/components/booking/terms-dialog";
import { TicketNotch } from "@/components/booking/ticket-notch";
import { formatCurrency } from "@/lib/format";
import { selectionTotal, totalUnits } from "@/lib/selection";
import type { SelectionLine } from "@/lib/types";
import TicketIcon from "../icons/ticket";

/** Shared by the live trigger and its inert stand-in, so the two cannot drift. */
const BUY_TICKET_CLASS =
  "flex h-10 w-full items-center justify-center rounded-lg bg-brand text-base font-bold text-brand-foreground uppercase transition-opacity";

/**
 * The "Selected Ticket" panel.
 *
 * It is always present, showing a placeholder while nothing is selected, because
 * the designs treat it as a fixture of the page rather than something that
 * appears once a selection exists.
 *
 * A selected bundle renders as ONE line under its own name and price — never
 * decomposed into the days inside it. That is what the guest chose and what the
 * order will record.
 */
export function SelectionSummary({
  lines,
  eventId,
  eventSlug,
  eventName,
}: {
  lines: SelectionLine[];
  /** The event's UUID, which the booking call identifies the event by. */
  eventId: string;
  /** The event's slug, used by the terms read and the order route. */
  eventSlug: string;
  /** Named in the Terms & Conditions the guest must accept to go on. */
  eventName: string;
}) {
  const isEmpty = lines.length === 0;
  const total = selectionTotal(lines);
  const units = totalUnits(lines);

  // No `overflow-hidden` on the panel: the notches straddle the border by 1px
  // and would be clipped, leaving the arc off the card edge, not cut into it.
  return (
    <aside className="sticky top-6 rounded-xl border bg-card">
      <header className="border-b px-4 py-4">
        <h2 className="text-2xl font-semibold">Selected Ticket</h2>
      </header>

      <div className="px-4.5 pt-3">
        {isEmpty ? (
          <div className="flex flex-col gap-2 items-center py-5">
            <TicketIcon />
            <p className="text-center text-sm text-muted-foreground">
              Selected ticket will appear here
            </p>
          </div>
        ) : (
          <>
            <div className="space-y-4 border-b pb-2">
              <h3 className="text-lg font-bold text-ticket-ink">
                Booking Detail
              </h3>

              <ul className="space-y-3">
                {lines.map((line) => (
                  <li key={`${line.kind}:${line.id}`} className="space-y-1">
                    <p className="flex items-start gap-1 text-base leading-5 text-ticket-muted">
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img
                        src="/icons/ticket.svg"
                        alt=""
                        width={16}
                        height={16}
                        className="mt-0.5 size-4 shrink-0"
                      />
                      {line.name}
                    </p>
                    <div className="flex items-center justify-between gap-4 text-sm leading-5">
                      <span className="text-ticket-muted">
                        {line.quantity}{" "}
                        {`Ticket${line.quantity > 1 ? "s" : ""}`}
                      </span>
                      <span className="font-bold text-ticket-ink">
                        {/* Presentational only: the server recomputes what is charged. */}
                        {formatCurrency(
                          String(Number(line.unitPrice) * line.quantity),
                        )}
                      </span>
                    </div>
                  </li>
                ))}
              </ul>
            </div>

            <div className="flex items-center justify-between gap-4 pt-3 text-base leading-5">
              <span className="text-ticket-muted">Total {units} {`Ticket${units > 1 ? "s" : ""}`}</span>
              <span className="font-bold text-ticket-ink">
                {formatCurrency(String(total))}
              </span>
            </div>
          </>
        )}
      </div>

      {/* The stub tear: a dashed rule notched into both edges of the panel. */}
      <div className="relative my-3 px-6">
        <TicketNotch side="left" />
        <TicketNotch side="right" />
        <hr className="border-t border-dashed" />
      </div>

      <div className="px-4.5 pb-4">
        {isEmpty ? (
          <span aria-disabled="true" className={`${BUY_TICKET_CLASS} opacity-50`}>
            Buy Ticket
          </span>
        ) : (
          // Booking is gated: this opens the Terms & Conditions rather than
          // navigating, and only agreement books the order and moves on.
          <TermsDialog
            eventId={eventId}
            eventSlug={eventSlug}
            eventName={eventName}
            lines={lines}
            triggerClassName={`${BUY_TICKET_CLASS} hover:opacity-90`}
          />
        )}
      </div>
    </aside>
  );
}

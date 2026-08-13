"use client";

import { useState } from "react";

import { TermsDialog } from "@/components/booking/terms-dialog";
import { TicketNotch } from "@/components/booking/ticket-notch";
import { failureMessage, reasonMessages } from "@/lib/availability";
import { formatCurrency } from "@/lib/format";
import { useCheckAvailability } from "@/lib/queries";
import { checkoutItems, selectionTotal, totalUnits } from "@/lib/selection";
import type { SelectionLine } from "@/lib/types";
import TicketIcon from "../icons/ticket";
import { Alert } from "../ui/alert";
import { Button } from "../ui/button";

/** Shared by the live trigger and its inert stand-in, so the two cannot drift. */
const BUY_TICKET_CLASS =
  "flex h-10 w-full items-center justify-center rounded-lg bg-brand text-base hover:bg-brand font-bold text-brand-foreground uppercase transition-opacity";

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

  // The gate lives here rather than in TermsDialog because this is the
  // component that awaits the server's answer, and the answer is what decides
  // whether the dialog opens at all (spec 013 FR-001/FR-002).
  const [termsOpen, setTermsOpen] = useState(false);
  const [refusals, setRefusals] = useState<string[]>([]);
  const check = useCheckAvailability();

  // A refusal describes ONE selection at one moment. The moment the guest
  // changes what they picked it is talking about something that no longer
  // exists, so it is discarded here rather than left to be re-shown.
  //
  // Without this, emptying the selection only HID the message — the panel stops
  // rendering it — and adding a row back brought the stale one straight back,
  // still naming a shortfall for a selection the guest had already abandoned.
  //
  // Adjusted during render rather than in an effect: this is derived state
  // catching up with a prop, and an effect would paint the stale message for a
  // frame before clearing it.
  const selectionKey = lines
    .map((line) => `${line.kind}:${line.id}:${line.quantity}`)
    .join("|");
  const [checkedSelection, setCheckedSelection] = useState(selectionKey);
  if (checkedSelection !== selectionKey) {
    setCheckedSelection(selectionKey);
    if (refusals.length > 0) setRefusals([]);
  }

  async function handleBuyTicket() {
    // FR-008: one check at a time. A guest pressing twice must not fire two.
    if (check.isPending) return;

    // FR-009: a decision is never reused. Clearing here also means a refusal
    // from the previous press cannot linger next to a fresh answer.
    setRefusals([]);

    try {
      const decision = await check.mutateAsync({
        event_id: eventId,
        items: checkoutItems(lines),
      });

      if (decision.available) {
        setTermsOpen(true);
        return;
      }
      // FR-006: every offending line, not the first. FR-007 is satisfied by
      // omission — nothing here touches the guest's quantities.
      setRefusals(reasonMessages(decision.reasons));
    } catch (error) {
      // Not a refusal: the check never got an answer, so the gate stays shut
      // rather than guessing in either direction (FR-002, FR-012).
      setRefusals([failureMessage(error)]);
    }
  }

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
                        {/* A bundle counts as itself, not as the days inside it —
                            the same reason it renders as one line at its own
                            price rather than decomposed. The unit noun stays
                            "Ticket(s)" for every kind of line, so this count and
                            the panel's own "Total N Tickets" below are counting
                            the same things in the same words. */}
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
              <span className="text-ticket-muted">
                Total {units} {`Ticket${units > 1 ? "s" : ""}`}
              </span>
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

      <div className="flex flex-col gap-1 px-4.5 pb-4">
        {isEmpty ? (
          <span
            aria-disabled="true"
            className={`${BUY_TICKET_CLASS} opacity-50`}
          >
            Buy Ticket
          </span>
        ) : (
          <>
            {/* Announced, not merely shown: a refusal arrives after the press
                with no other visible change, so a screen reader user would
                otherwise be left waiting on silence. */}
            {refusals.length > 0 ? (
              // The name goes on Alert, which already carries role="alert" — a
              // second one on the list nested a live region inside a live
              // region and left two unnamed-vs-named alerts in the tree. The
              // <li>s matter too: AlertTitle renders a <div>, and a <ul> whose
              // children are <div>s is invalid markup that costs the list its
              // semantics, so a screen reader stops announcing "3 items".
              <Alert
                variant="destructive"
                aria-label="Why this selection cannot be bought"
              >
                <ul className="space-y-1 text-sm leading-5">
                  {refusals.map((message) => (
                    <li key={message} className="font-medium">
                      {message}
                    </li>
                  ))}
                </ul>
              </Alert>
            ) : null}

            {/* Booking is gated twice over: this press confirms the selection is
                still buyable, and only then does the Terms & Conditions dialog
                open. Agreement is what actually books the order. */}
            <Button
              type="button"
              onClick={handleBuyTicket}
              disabled={check.isPending}
              className={`${BUY_TICKET_CLASS} hover:opacity-90 disabled:opacity-60`}
            >
              {check.isPending ? "Checking…" : "Buy Ticket"}
            </Button>

            <TermsDialog
              eventId={eventId}
              eventSlug={eventSlug}
              eventName={eventName}
              lines={lines}
              open={termsOpen}
              onOpenChange={setTermsOpen}
            />
          </>
        )}
      </div>
    </aside>
  );
}

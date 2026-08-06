"use client";

import { useEffect, useState } from "react";

/**
 * A live countdown to the event's start date.
 *
 * It counts down to when the event begins, not to when any individual ticket
 * stops selling: the deadline the guest actually cares about is the doors
 * opening, and it stays fixed while ticket windows come and go beneath it.
 *
 * It renders nothing once the event has started rather than counting into the
 * negative — by then the rows say "Sales have closed" on their own, which is the
 * honest message.
 */
export function SalesCountdown({ startDate }: { startDate: string | null }) {
  const target = startDate ? new Date(startDate).getTime() : NaN;
  const [remaining, setRemaining] = useState(() => remainingFrom(target, Date.now()));

  useEffect(() => {
    if (Number.isNaN(target)) return;

    const id = setInterval(() => setRemaining(remainingFrom(target, Date.now())), 1000);
    return () => clearInterval(id);
  }, [target]);

  if (!remaining) return null;

  return (
    <section
      aria-label="Time remaining until the event starts"
      className="border-b bg-background py-3"
    >
      <div className="mx-auto flex max-w-5xl flex-wrap items-center justify-center gap-4 px-6">
        {/* Names the duration, not a date: the payment screen shows this beside
            the payment-expiry timer, and a guest must never mistake one for the
            other (spec FR-017). */}
        <p className="text-sm text-muted-foreground">Event starts in</p>

        <div className="flex items-start gap-4">
          <Unit value={remaining.days} label="Days" />
          <Separator />
          <Unit value={remaining.hours} label="Hours" />
          <Separator />
          <Unit value={remaining.minutes} label="Min" />
          <Separator />
          <Unit value={remaining.seconds} label="Sec" />
        </div>
      </div>
    </section>
  );
}

function Unit({ value, label }: { value: number; label: string }) {
  return (
    <div className="flex flex-col items-center">
      <span className="w-14 rounded bg-brand px-3 py-1.5 text-center text-xl font-bold text-brand-foreground tabular-nums">
        {String(value).padStart(2, "0")}
      </span>
      <span className="pt-1 text-xs font-semibold tracking-[0.6px] text-muted-foreground uppercase">
        {label}
      </span>
    </div>
  );
}

function Separator() {
  return (
    // h-10 matches the digit box height (text-xl line + py-1.5) so the colon
    // centers on the red boxes, not the box-plus-label column.
    <span aria-hidden className="flex h-10 items-center text-xl font-bold text-muted-foreground">
      :
    </span>
  );
}

type Remaining = { days: number; hours: number; minutes: number; seconds: number };

function remainingFrom(target: number, now: number): Remaining | null {
  if (Number.isNaN(target)) return null;

  const ms = target - now;
  if (ms <= 0) return null;

  const seconds = Math.floor(ms / 1000);
  return {
    days: Math.floor(seconds / 86400),
    hours: Math.floor((seconds % 86400) / 3600),
    minutes: Math.floor((seconds % 3600) / 60),
    seconds: seconds % 60,
  };
}

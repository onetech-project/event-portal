"use client";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import type { ValidationResult } from "@/lib/types";

const VERDICTS = {
  VALID: {
    label: "Valid",
    className:
      "bg-emerald-100 text-emerald-900 dark:bg-emerald-950/60 dark:text-emerald-100",
  },
  ALREADY_USED: {
    label: "Already used",
    className: "bg-amber-100 text-amber-900 dark:bg-amber-950/60 dark:text-amber-100",
  },
  INVALID: {
    label: "Invalid",
    className: "bg-destructive/15 text-destructive",
  },
} as const;

/**
 * The door-side verdict for a scanned or typed ticket code.
 *
 * "Mark used" appears only for a VALID ticket: the transition is irreversible, so
 * the action is never offered where it could not legitimately apply.
 */
export function ValidationResultCard({
  result,
  onMarkUsed,
  isMarking,
}: {
  result: ValidationResult;
  onMarkUsed: (code: string) => void;
  isMarking: boolean;
}) {
  const verdict = VERDICTS[result.result];

  return (
    <Card>
      <CardContent className="space-y-4">
        <div
          className={cn(
            "rounded-lg px-4 py-3 text-lg font-semibold",
            verdict.className,
          )}
        >
          {verdict.label}
        </div>

        <p className="font-mono text-lg tracking-widest">{result.ticket_code}</p>

        {result.result !== "INVALID" ? (
          <dl className="space-y-2 text-sm">
            <div className="flex justify-between gap-4">
              <dt className="text-muted-foreground">Attendee</dt>
              <dd className="font-medium">{result.attendee_name}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-muted-foreground">Ticket type</dt>
              <dd className="font-medium">{result.ticket_type_name}</dd>
            </div>
            <div className="flex justify-between gap-4">
              <dt className="text-muted-foreground">Event</dt>
              <dd className="font-medium">{result.event_name}</dd>
            </div>
          </dl>
        ) : (
          <p className="text-sm text-muted-foreground">
            No ticket matches this code. Do not admit the holder.
          </p>
        )}

        {result.result === "VALID" ? (
          <div>
            <Button onClick={() => onMarkUsed(result.ticket_code)} disabled={isMarking}>
              {isMarking ? "Admitting…" : "Mark used"}
            </Button>
            <p className="mt-2 text-xs text-muted-foreground">
              This cannot be undone — admit the holder before confirming.
            </p>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

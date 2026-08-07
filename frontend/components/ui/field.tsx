"use client";

import type { ReactNode } from "react";

import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/**
 * A labelled form control with an optional hint and validation message.
 *
 * The control is nested inside the label, which associates the two without the
 * caller having to thread an id through every field. The error is announced
 * with role="alert" because it appears only once the guest has been in the
 * field (or attempted to proceed).
 *
 * `required` renders the mandatory marker (spec 011 US1-AS3): a visual
 * asterisk plus screen-reader text inside the label, so the control is
 * announced as required without reaching into `children`.
 */
export function Field({
  label,
  error,
  hint,
  required,
  children,
  className,
}: {
  label: string;
  error?: string;
  hint?: ReactNode;
  required?: boolean;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Label className={cn("flex flex-col items-stretch gap-1.5 font-normal", className)}>
      <span className="text-sm font-medium">
        {label}
        {required ? (
          <>
            <span aria-hidden className="ml-0.5 text-destructive">
              *
            </span>
            <span className="sr-only"> (required)</span>
          </>
        ) : null}
      </span>

      {children}

      {hint ? <span className="text-xs font-normal text-muted-foreground">{hint}</span> : null}

      {error ? (
        <span role="alert" className="text-xs font-normal text-destructive">
          {error}
        </span>
      ) : null}
    </Label>
  );
}

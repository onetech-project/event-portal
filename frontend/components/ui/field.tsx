"use client";

import type { ReactNode } from "react";

import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/**
 * A labelled form control with an optional hint and validation message.
 *
 * The control is nested inside the label, which associates the two without the
 * caller having to thread an id through every field. The error is announced with
 * role="alert" because it appears only after a submit attempt.
 */
export function Field({
  label,
  error,
  hint,
  children,
  className,
}: {
  label: string;
  error?: string;
  hint?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <Label className={cn("flex flex-col items-stretch gap-1.5 font-normal", className)}>
      <span className="text-sm font-medium">{label}</span>

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

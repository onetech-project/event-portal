"use client";

import type { ReactNode } from "react";

import { Alert, AlertDescription } from "@/components/ui/alert";
import { cn } from "@/lib/utils";

/**
 * App-level status messages.
 *
 * shadcn's Alert ships `default` and `destructive`; this adds the success and
 * info tones the purchase and admin flows need, while keeping the same element,
 * role, and slots so it stays visually consistent with the rest of the kit.
 */
export function StatusAlert({
  tone = "error",
  children,
  className,
}: {
  tone?: "error" | "success" | "info";
  children: ReactNode;
  className?: string;
}) {
  const tones = {
    error: "",
    success: "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900/50 dark:bg-emerald-950/40 dark:text-emerald-100",
    info: "border-border bg-muted text-foreground",
  } as const;

  return (
    <Alert
      variant={tone === "error" ? "destructive" : "default"}
      className={cn(tones[tone], className)}
    >
      <AlertDescription className={tone === "error" ? undefined : "text-current"}>
        {children}
      </AlertDescription>
    </Alert>
  );
}

export function PageHeading({ title, subtitle }: { title: string; subtitle?: string }) {
  return (
    <div className="mb-6">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      {subtitle ? <p className="mt-1 text-sm text-muted-foreground">{subtitle}</p> : null}
    </div>
  );
}

export function Loading({ label = "Loading…" }: { label?: string }) {
  return (
    <p role="status" className="py-8 text-center text-sm text-muted-foreground">
      {label}
    </p>
  );
}

export function EmptyState({ children }: { children: ReactNode }) {
  return <p className="py-8 text-center text-sm text-muted-foreground">{children}</p>;
}

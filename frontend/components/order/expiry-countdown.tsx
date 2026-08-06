"use client";

import { useEffect, useRef, useState } from "react";

type Props = {
  /** The server's payment deadline, ISO 8601. */
  expiresAt: string;
  /** The server's clock at the moment it sent `expiresAt`, ISO 8601. */
  serverTime: string;
  /** Called once when the countdown reaches zero. */
  onExpired?: () => void;
  /**
   * "inline" (default) reads "Pay within m:ss"; "digits" is the Complete
   * Purchase banner's bare mm : ss readout (Figma 32-1366) — the banner text
   * around it carries the context.
   */
  variant?: "inline" | "digits";
};

/**
 * How far this device's clock is from the server's, in milliseconds.
 *
 * Measured once per response. It must NOT be recomputed on every tick: the
 * server's timestamp is a fixed snapshot, so re-measuring it against a moving
 * local clock cancels out exactly the elapsed time the countdown is supposed to
 * show, and the display freezes.
 */
export function clockOffsetMs(serverTime: string, now: number): number {
  const serverNow = Date.parse(serverTime);
  return Number.isNaN(serverNow) ? 0 : serverNow - now;
}

/**
 * Milliseconds left until the deadline, corrected for a wrong device clock, so
 * a laptop set an hour fast still shows the true remaining time (SC-005).
 */
export function remainingMs(expiresAt: string, offsetMs: number, now: number): number {
  const deadline = Date.parse(expiresAt);
  if (Number.isNaN(deadline)) return 0;

  return Math.max(0, deadline - (now + offsetMs));
}

/** Formats a duration as m:ss, which is how a payment window reads. */
export function formatRemaining(ms: number): string {
  const totalSeconds = Math.ceil(ms / 1000);
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  return `${minutes}:${seconds.toString().padStart(2, "0")}`;
}

export function ExpiryCountdown({
  expiresAt,
  serverTime,
  onExpired,
  variant = "inline",
}: Readonly<Props>) {
  // null until the first measurement: reading the clock is a side effect, so it
  // happens in the effect below rather than during render.
  const offset = useRef<number | null>(null);
  const [remaining, setRemaining] = useState<number | null>(null);

  useEffect(() => {
    // Re-synced on every response, so a slow local clock self-corrects rather
    // than drifting over a 15-minute window.
    offset.current = clockOffsetMs(serverTime, Date.now());

    const update = () =>
      setRemaining(remainingMs(expiresAt, offset.current ?? 0, Date.now()));
    update();
    const tick = setInterval(update, 1000);
    return () => clearInterval(tick);
  }, [expiresAt, serverTime]);

  useEffect(() => {
    if (remaining === 0) onExpired?.();
  }, [remaining, onExpired]);

  // Blank for one frame at most, until the effect above takes its first
  // measurement.
  if (remaining === null) return null;

  const urgent = remaining > 0 && remaining < 60_000;

  if (variant === "digits") {
    return (
      <p aria-live="polite" className="font-mono text-2xl font-bold text-brand">
        {remaining > 0 ? (
          formatRemaining(remaining).replace(":", " : ")
        ) : (
          <span className="text-base font-semibold text-destructive">Expired</span>
        )}
      </p>
    );
  }

  return (
    <p className="text-sm text-muted-foreground">
      {remaining > 0 ? (
        <>
          {/* Named for the deadline it imposes, not for the code it belongs to:
              the event-start countdown sits on this same screen, and mistaking
              one for the other costs the guest their order (spec FR-017). */}
          Pay within{" "}
          <span
            // aria-live so a screen reader hears the time run out rather than
            // only the sighted user seeing it.
            aria-live="polite"
            className={
              urgent ? "font-mono font-semibold text-destructive" : "font-mono font-semibold"
            }
          >
            {formatRemaining(remaining)}
          </span>
        </>
      ) : (
        <span className="font-semibold text-destructive">This code has expired.</span>
      )}
    </p>
  );
}

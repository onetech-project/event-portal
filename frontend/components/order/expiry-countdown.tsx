"use client";

import { useEffect, useRef, useState } from "react";

type Props = {
  /** The server's payment deadline, ISO 8601. */
  expiresAt: string;
  /** The server's clock at the moment it sent `expiresAt`, ISO 8601. */
  serverTime: string;
  /** Called once when the countdown reaches zero. */
  onExpired?: () => void;
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

export function ExpiryCountdown({ expiresAt, serverTime, onExpired }: Readonly<Props>) {
  // Re-synced on every response, so a slow local clock self-corrects rather
  // than drifting over a 15-minute window.
  const offset = useRef(clockOffsetMs(serverTime, Date.now()));
  const [remaining, setRemaining] = useState(() =>
    remainingMs(expiresAt, offset.current, Date.now()),
  );

  useEffect(() => {
    offset.current = clockOffsetMs(serverTime, Date.now());
    setRemaining(remainingMs(expiresAt, offset.current, Date.now()));

    const tick = setInterval(() => {
      setRemaining(remainingMs(expiresAt, offset.current, Date.now()));
    }, 1000);
    return () => clearInterval(tick);
  }, [expiresAt, serverTime]);

  useEffect(() => {
    if (remaining === 0) onExpired?.();
  }, [remaining, onExpired]);

  const urgent = remaining > 0 && remaining < 60_000;

  return (
    <p className="text-sm text-muted-foreground">
      {remaining > 0 ? (
        <>
          This code expires in{" "}
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

"use client";

import { BrowserQRCodeReader, type IScannerControls } from "@zxing/browser";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { StatusAlert } from "@/components/ui/feedback";

/**
 * Camera QR capture for the door flow.
 *
 * Scanning is the secondary input path — manual code entry is primary (PRD §1.4)
 * — so the camera stays off until an admin turns it on, and any failure degrades
 * to the manual field rather than blocking check-in.
 *
 * The decoded text is handed to the same validate call as a typed code; this
 * component never talks to the API itself.
 */
export function QrScanner({ onScan }: Readonly<{ onScan: (code: string) => void }>) {
  const videoRef = useRef<HTMLVideoElement>(null);
  const controlsRef = useRef<IScannerControls | null>(null);
  const [active, setActive] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!active) return;

    let cancelled = false;
    const reader = new BrowserQRCodeReader();

    void reader
      .decodeFromVideoDevice(undefined, videoRef.current ?? undefined, (result) => {
        if (result) {
          // Stop immediately: without this the same badge decodes repeatedly and
          // fires a burst of identical lookups.
          controlsRef.current?.stop();
          setActive(false);
          onScan(result.getText());
        }
      })
      .then((controls) => {
        if (cancelled) {
          controls.stop();
          return;
        }
        controlsRef.current = controls;
      })
      .catch(() => {
        setError(
          "Could not start the camera. Check the browser's camera permission, or type the code instead.",
        );
        setActive(false);
      });

    return () => {
      cancelled = true;
      controlsRef.current?.stop();
      controlsRef.current = null;
    };
  }, [active, onScan]);

  return (
    <div className="space-y-3">
      <Button
        type="button"
        variant="outline"
        onClick={() => {
          setError(null);
          setActive((on) => !on);
        }}
      >
        {active ? "Stop camera" : "Scan QR code"}
      </Button>

      {error ? <StatusAlert>{error}</StatusAlert> : null}

      {active ? (
        <video
          ref={videoRef}
          className="w-full rounded-lg border bg-black"
          muted
          playsInline
        >
          <track kind="captions" />
        </video>
      ) : null}
    </div>
  );
}

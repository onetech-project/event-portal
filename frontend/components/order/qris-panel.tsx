"use client";

import { Card, CardContent } from "@/components/ui/card";
import { apiOrigin } from "@/lib/env";
import { formatCurrency } from "@/lib/format";
import type { PaymentInstruction } from "@/lib/types";

type Props = {
  /** Null once the order is no longer payable — the server stops issuing it. */
  payment: PaymentInstruction | null;
  /** The order's total, so the frame can still name the sum after expiry. */
  totalAmount: string;
  /** Set when the order ended; the frame stays, the scannable code does not. */
  endedStatus?: "EXPIRED" | "CANCELLED" | null;
};

/** The three steps printed along the bottom of every QRIS frame. */
const PAY_STEPS = [
  { src: "/brand/qris-step-open.png", label: "Buka Aplikasi\nBerlogo QRIS" },
  { src: "/brand/qris-step-scan.png", label: "Scan & Cek" },
  { src: "/brand/qris-step-pay.png", label: "Bayar" },
] as const;

/**
 * The Scan to Pay card (Figma 203-1201): the live code inside the standard
 * Indonesian QRIS frame, and the exact amount due. The payment countdown lives
 * in the Complete Purchase banner above the columns, not here.
 *
 * The frame carries what the design draws and nothing else. Spec 020 removed
 * the merchant name, the registration number, the terminal label and the two
 * printed-footer lines — the design masks that whole band out and puts the
 * heading and its subtitle in the space instead, which is why the title sits
 * inside the frame here rather than in a card header above it.
 *
 * Worth knowing before restoring any of it: the merchant name was there as a
 * check. A QRIS code is scanned by an app the guest trusts, and comparing the
 * name on screen against the name the app shows was the one defence available
 * against a swapped code. That comparison was removed deliberately, not lost —
 * the amount check in [qris-instructions.tsx](./qris-instructions.tsx) now
 * carries the whole verification weight, which is why it names the exact sum
 * rather than saying "check the amount". Spec 020 supersedes spec 012's
 * FR-021a and retires FR-021b.
 *
 * The Figma node for this frame is one flat raster with a mockup QR baked into
 * it, so it is rebuilt here as markup: the QR has to be the live one this order
 * was issued, rendered by the API on demand from the stored payload. Only the
 * artwork is imaged — the QRIS and GPN marks, the batik ground, and the three
 * step icons — because those are trademarks and illustration. Redrawing a brand
 * mark by hand would undermine the very frame it belongs to.
 *
 * After the order ends the frame stays and only the code goes (US5 scenario 7).
 * An expired order must not present something scannable, but blanking the whole
 * panel left the page a void beside a modal, which read as broken rather than
 * as finished.
 */
export function QrisPanel({
  payment,
  totalAmount,
  endedStatus = null,
}: Readonly<Props>) {
  const ended = endedStatus !== null;
  const amount = payment !== null ? payment.amount : totalAmount;

  return (
    <Card>
      <CardContent className="space-y-5">
        {/*
          A container query, not viewport breakpoints: every measurement below is
          a fraction of the frame's own width, taken from the design (522 × 735),
          so the whole frame scales as one piece in either column layout.
        */}
        <div className="@container w-full">
          <div
            className="relative aspect-522/735 overflow-hidden rounded-xl border bg-white text-neutral-900"
          >
            {/* eslint-disable @next/next/no-img-element -- fixed artwork; the
                optimizer would only re-encode small static PNGs. */}

            {/* The batik ground, lifted from the design as one layer so its
                motif and its diagonal masking stay exactly as drawn.

                One hazard in this asset: it was produced by erasing the
                artwork's overlaid text, and the eraser took the motif with it,
                leaving letter-shaped holes wherever text used to sit. The
                footer band was repaired by hand when spec 020 removed the text
                that had been hiding it. The holes under "SATU QRIS UNTUK SEMUA"
                and the aspi-qris.id lines are still there, invisible only
                because the live text below sits exactly on top of them. Move or
                shorten that copy and the ghost of the old wording appears. */}
            <img
              src="/brand/qris-batik.png"
              alt=""
              aria-hidden="true"
              className="pointer-events-none absolute inset-0 size-full object-cover"
            />

            {/* Decorative: the chevron bleeding off the left edge. */}
            <div
              aria-hidden="true"
              className="absolute left-0 top-[26%] h-[33.5%] w-[18.2%] bg-[#EF2731]"
              style={{ clipPath: "polygon(0% 0%, 100% 34%, 100% 64%, 0% 100%)" }}
            />
            {/* Decorative: the wedge filling the bottom-right corner. */}
            <div
              aria-hidden="true"
              className="absolute bottom-0 right-0 h-[25.5%] w-[40%] bg-[#EF2731]"
              style={{ clipPath: "polygon(100% 0%, 100% 100%, 0% 100%)" }}
            />

            <div className="relative flex h-full flex-col items-center px-[10%] pt-[6.6%]">
              <div className="flex w-full items-start justify-between">
                <img
                  src="/brand/qris-lockup.png"
                  alt="QRIS — QR Code Standar Pembayaran Nasional"
                  width={525}
                  height={89}
                  className="h-auto w-[62%]"
                />
                <img
                  src="/brand/gpn-logo.png"
                  alt="GPN — Gerbang Pembayaran Nasional"
                  width={100}
                  height={122}
                  className="mt-[-1.5%] h-auto w-[11.8%]"
                />
              </div>

              {/*
                The title and its subtitle, inside the frame rather than above
                it: the design puts them below the QRIS and GPN marks, which are
                part of the frame artwork, and a card header could never produce
                that order. The three margins here are measured off the design at
                its native 522 × 735 — heading top at 122px, code top at 249px —
                and expressed in cqw so the whole block scales with the frame.
              */}
              <h2 className="mt-[6.36cqw] text-center text-[3.83cqw]/[1.4] font-bold text-[#111]">
                Scan to Pay
              </h2>
              <p className="mt-[1.53cqw] text-center text-[2.49cqw]/[1.5] text-[#6b7280]">
                {ended
                  ? "This order can no longer be paid."
                  : "Use any e-Wallet or Mobile Banking app supporting QRIS."}
              </p>

              {payment !== null ? (
                /*
                  Rendered on demand by the API from the live payload — no file
                  is stored anywhere and the browser never talks to the gateway.
                  Left unoptimized deliberately: re-encoding a QR risks its
                  scannability, the one property it has to keep.
                */
                <img
                  src={apiOrigin() + payment.qr_image_path}
                  alt={`QRIS code for ${formatCurrency(amount)}`}
                  width={627}
                  height={627}
                  className="mt-[13.7cqw] aspect-square w-[68%] object-contain"
                />
              ) : (
                <div className="mt-[13.7cqw] flex aspect-square w-[68%] items-center justify-center rounded-[2cqw] border border-dashed border-neutral-300 bg-white/70 px-[4%]">
                  <p className="text-center text-[3.6cqw]/[1.35] font-medium text-neutral-500">
                    {endedStatus === "CANCELLED"
                      ? "This order was cancelled, so no code can be shown."
                      : "The payment time ran out, so this code is no longer valid."}
                  </p>
                </div>
              )}

              <p className="mt-[4%] text-center text-[3.4cqw]/[1.2] tracking-[0.06em] text-neutral-400">
                SATU QRIS UNTUK SEMUA
              </p>
              <p className="mt-[3%] text-center text-[3.4cqw]/[1.35] text-neutral-600">
                Cek aplikasi penyelenggara
                <br />
                di: www.aspi-qris.id
              </p>
            </div>

            {/*
              Sized to the wedge, not to its own content. The triangle's left
              edge climbs as it rises, so at the caption's height it offers only
              about 27% of the card's width — anything wider spills onto the
              white, which is exactly what a content-sized block did.
            */}
            <div className="absolute bottom-[0.6%] right-[1.9%] w-[25.5cqw] text-white">
              <p className="text-right text-[1.7cqw]/[1.3] whitespace-nowrap">
                Cara bayar dengan QRIS:
              </p>
              <ol className="mt-[0.9cqw] flex items-start">
                {PAY_STEPS.map((step) => (
                  <li
                    key={step.src}
                    className="flex w-[8.5cqw] flex-col items-center"
                  >
                    <img
                      src={step.src}
                      alt=""
                      aria-hidden="true"
                      width={72}
                      height={80}
                      className="h-auto w-[6cqw]"
                    />
                    <span className="mt-[0.5cqw] whitespace-pre-line text-center text-[1.25cqw]/[1.2]">
                      {step.label}
                    </span>
                  </li>
                ))}
              </ol>
            </div>
            {/* eslint-enable @next/next/no-img-element */}
          </div>
        </div>

        <div className="rounded-lg bg-muted/40 px-4 py-3 text-center">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            {ended ? "Order total" : "Total amount due"}
          </p>
          <p className="pt-1 text-2xl font-bold">{formatCurrency(amount)}</p>
        </div>
      </CardContent>
    </Card>
  );
}

"use client";

import { Card, CardContent } from "@/components/ui/card";
import { apiOrigin } from "@/lib/env";
import { formatCurrency } from "@/lib/format";
import type { PaymentInstruction } from "@/lib/types";

type Props = {
  payment: PaymentInstruction;
  /**
   * Bumped after a QR re-issue so the <img> bypasses its short browser cache
   * and shows the fresh code.
   */
  cacheBust?: number;
};

/**
 * The Scan to Pay card (Figma 32-1366): the QRIS code the guest scans and the
 * exact amount due. The payment countdown lives in the Complete Purchase
 * banner above the columns, not here.
 *
 * The image is rendered by the API on demand from the stored payload — there is
 * no file anywhere, and the browser never talks to the payment provider.
 */
export function QrisPanel({ payment, cacheBust = 0 }: Readonly<Props>) {
  return (
    <Card>
      <CardContent className="space-y-5 pt-2 text-center">
        <div>
          <h2 className="text-xl font-bold">Scan to Pay</h2>
          <p className="pt-1 text-sm text-muted-foreground">
            Use any e-Wallet or Mobile Banking app supporting QRIS.
          </p>
        </div>

        {/* Rendered on demand by the API from the live QR payload; next/image
            optimization would only re-encode a code that must stay scannable. */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={apiOrigin() + payment.qr_image_path + (cacheBust > 0 ? `?v=${cacheBust}` : "")}
          alt={`QRIS code for ${formatCurrency(payment.amount)}`}
          width={320}
          height={320}
          className="mx-auto h-80 w-80 max-w-full rounded-lg border bg-white p-3"
        />

        <div className="rounded-lg bg-muted/40 px-4 py-3">
          <p className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
            Total amount due
          </p>
          <p className="pt-1 text-2xl font-bold">{formatCurrency(payment.amount)}</p>
        </div>
      </CardContent>
    </Card>
  );
}

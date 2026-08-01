"use client";

import { ExpiryCountdown } from "@/components/order/expiry-countdown";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { apiOrigin } from "@/lib/env";
import { formatCurrency } from "@/lib/format";
import type { PaymentInstruction } from "@/lib/types";

type Props = {
  payment: PaymentInstruction;
  serverTime: string;
  onExpired: () => void;
};

/**
 * The QRIS code the guest scans, the exact amount to pay, and how long the code
 * is still good for.
 *
 * The image is rendered by the API on demand from the stored payload — there is
 * no file anywhere, and the browser never talks to the payment provider.
 */
export function QrisPanel({ payment, serverTime, onExpired }: Readonly<Props>) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Scan to pay</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Open any QRIS-compatible banking or e-wallet app, scan this code, and confirm
          the payment.
        </p>

        <div className="flex flex-col items-center gap-3">
          <img
            src={apiOrigin() + payment.qr_image_path}
            alt={`QRIS code for ${formatCurrency(payment.amount)}`}
            width={256}
            height={256}
            className="h-64 w-64 rounded-lg border bg-white p-2"
          />

          <p className="text-lg font-semibold">{formatCurrency(payment.amount)}</p>

          <ExpiryCountdown
            expiresAt={payment.expires_at}
            serverTime={serverTime}
            onExpired={onExpired}
          />
        </div>
      </CardContent>
    </Card>
  );
}

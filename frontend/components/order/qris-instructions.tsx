"use client";

import { ChevronDown, QrCode } from "lucide-react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { qrisMerchantName } from "@/lib/env";
import { formatCurrency } from "@/lib/format";

type Props = {
  /** The amount the guest should see in their app before confirming. */
  amount: string;
};

/**
 * The "How to pay with QRIS" collapsible (Figma 32-1366 collapsed / 203-1157
 * expanded), sitting between the total and the primary action.
 *
 * Step 4 is the one that matters. A QRIS payment is authorised inside an app
 * this page cannot see, so the only check available to the guest is comparing
 * what their app shows against what this screen says — and it has to name both
 * halves, the merchant and the amount, or it is not a check at all. Naming the
 * merchant explicitly rather than saying "the organizer" is the whole point:
 * "does this match?" is answerable, "is this right?" is not.
 */
export function QrisInstructions({ amount }: Readonly<Props>) {
  const merchantName = qrisMerchantName();

  return (
    <Collapsible className="border-y py-3">
      <CollapsibleTrigger className="group flex w-full items-center justify-between gap-2 text-sm font-medium">
        <span className="flex items-center gap-2">
          <QrCode aria-hidden className="size-4" />
          How to pay with QRIS
        </span>
        <ChevronDown
          aria-hidden
          className="size-4 transition-transform group-data-panel-open:rotate-180"
        />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <ol className="list-decimal space-y-2 pt-3 pl-5 text-sm text-muted-foreground">
          <li>
            Open your m-banking or e-wallet app (Gopay, OVO, Dana, LinkAja, BCA
            mobile, Livin&apos;, etc.).
          </li>
          <li>Select the Scan QR or Pay menu.</li>
          <li>Point the camera at the QR Code shown on the left.</li>
          <li>
            Check that your app shows{" "}
            {merchantName ? (
              <span className="font-medium text-foreground">{merchantName}</span>
            ) : (
              "the merchant name printed above the code"
            )}{" "}
            and the amount{" "}
            <span className="font-medium text-foreground">{formatCurrency(amount)}</span>.
            If either differs, stop and do not enter your PIN.
          </li>
          <li>Confirm the payment and enter your PIN.</li>
          <li>Save your payment receipt.</li>
        </ol>
      </CollapsibleContent>
    </Collapsible>
  );
}

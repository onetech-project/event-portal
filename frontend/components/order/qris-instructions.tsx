"use client";

import { ChevronDown, QrCode } from "lucide-react";

import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
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
 * what their app shows against what this screen says. It names the exact amount
 * for that reason: "does this match?" is answerable, "is this right?" is not.
 *
 * It used to name the merchant too, and that half was removed deliberately by
 * spec 020, not lost — the frame it told the guest to read the name off no
 * longer prints one, and an instruction pointing at absent text is worse than
 * no instruction, because it reads as a safety check while offering nothing to
 * check against. Spec 012's FR-021c is narrowed to the amount accordingly.
 *
 * The consequence is worth stating where someone will find it: the amount is
 * now the whole of the guest's defence against a swapped code. Do not soften it
 * to "check the amount is correct" — the figure has to be on screen to be
 * compared against.
 */
export function QrisInstructions({ amount }: Readonly<Props>) {
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
            Check that your app shows the amount{" "}
            <span className="font-medium text-foreground">{formatCurrency(amount)}</span>.
            If it differs, stop and do not enter your PIN.
          </li>
          <li>Confirm the payment and enter your PIN.</li>
          <li>Save your payment receipt.</li>
        </ol>
      </CollapsibleContent>
    </Collapsible>
  );
}

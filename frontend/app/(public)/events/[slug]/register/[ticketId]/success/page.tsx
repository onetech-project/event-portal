"use client";

import { CheckIcon } from "lucide-react";
import { use } from "react";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useRegistrationPrereqs } from "@/lib/queries";

/**
 * The registration confirmation (spec 022 FR-039).
 *
 * A STATIC panel, carrying no registration reference and no email address. That
 * is the approved design and it was chosen deliberately after the trade-off was
 * raised, so two consequences are designed in rather than discovered:
 *
 *  - It asserts a delivery that has not completed when this renders — issuance
 *    and the email run after the response. A registrant whose delivery bounces
 *    has been told it succeeded, so ADMIN RESEND is the only recovery route
 *    (FR-038), and an operator must be able to find them by name or partial
 *    email (FR-053).
 *  - Because it holds no reference, it cannot tell a guest who just registered
 *    from anyone who typed the address, and it MUST NOT try (FR-039d) — no
 *    guessing from history, referrer, or client state a reload would lose. It
 *    renders the same panel to both. Nothing is disclosed by that: the panel
 *    contains no registrant data, only the event's name, which the event's own
 *    public pages already carry.
 *
 * Reload-safe by construction (FR-039b): there is nothing here to re-submit, and
 * the form is behind them, so refreshing never produces an empty form or a
 * duplicate-registration error for a guest who succeeded.
 */
export default function RegistrationSuccessPage({
  params,
}: {
  params: Promise<{ slug: string; ticketId: string }>;
}) {
  const { slug, ticketId } = use(params);

  // Only to name the event in the copy. A failure here degrades the sentence,
  // never the page: someone who has just registered must not be shown an error.
  const prereqs = useRegistrationPrereqs(slug, ticketId);
  const eventName = prereqs.data?.event.name ?? "this event";

  return (
    <main className="flex flex-col items-center justify-center mx-auto w-full max-w-3xl flex-1 px-6 py-16">
      <Card className="mx-auto max-w-lg py-8">
        <CardHeader className="flex flex-col gap-2 items-center justify-center">
          <div
            aria-hidden
            className="mx-auto flex size-14 items-center justify-center rounded-full bg-emerald-50 text-emerald-600"
          >
            <CheckIcon className="size-7" />
          </div>

          <CardTitle className="text-xl font-bold w-fit">Registration Complete!</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4 text-center">
          <p className="text-sm text-muted-foreground">
            Thank you for registering. Your complimentary invitation e-ticket
            for {eventName} has been successfully generated and sent to your
            email.
          </p>
        </CardContent>
      </Card>
    </main>
  );
}

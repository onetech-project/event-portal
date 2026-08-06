import { VerifyCard } from "@/components/ticket/verify-card";
import { PageHeading } from "@/components/ui/feedback";

/**
 * Ticket validation: a code box that routes to /tickets/:code.
 *
 * This is where verification lives now — it used to be an aside on the
 * homepage, duplicating the form this page already had. One copy, one address,
 * linked from the header nav.
 */
export default function TicketLookupPage() {
  return (
    <main className="mx-auto w-full max-w-md flex-1 px-6 py-10">
      <PageHeading
        title="Validate a ticket"
        subtitle="Enter the code printed on your ticket."
      />

      <VerifyCard />
    </main>
  );
}

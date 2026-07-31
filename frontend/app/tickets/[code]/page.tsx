"use client";

import Link from "next/link";
import { use } from "react";

import { Card, CardContent } from "@/components/ui/card";
import { Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import { ApiError } from "@/lib/api-client";
import { useTicketByCode } from "@/lib/queries";

const STATUS_LABELS: Record<string, { label: string; tone: "success" | "info" | "error" }> = {
  ACTIVE: { label: "Valid", tone: "success" },
  USED: { label: "Already used", tone: "info" },
  REVOKED: { label: "Not valid", tone: "error" },
};

export default function TicketPage({ params }: { params: Promise<{ code: string }> }) {
  const { code } = use(params);
  const { data: ticket, isPending, error } = useTicketByCode(code);

  const apiError = error instanceof ApiError ? error : null;

  return (
    <main className="mx-auto w-full max-w-md flex-1 px-6 py-10">
      <PageHeading title="Ticket status" />

      {isPending ? <Loading label="Checking ticket…" /> : null}

      {apiError ? (
        <StatusAlert>
          {apiError.code === "RATE_LIMITED"
            ? "Too many lookups. Please wait a moment and try again."
            : apiError.message}
        </StatusAlert>
      ) : null}

      {ticket ? (
        <Card>
          <CardContent className="space-y-4">
            <p className="font-mono text-lg tracking-widest">{ticket.ticket_code}</p>

            <StatusAlert tone={STATUS_LABELS[ticket.status]?.tone ?? "info"}>
              {STATUS_LABELS[ticket.status]?.label ?? ticket.status}
            </StatusAlert>

            <dl className="space-y-2 text-sm">
              <div className="flex justify-between">
                <dt className="text-muted-foreground">Event</dt>
                <dd className="font-medium">{ticket.event_name}</dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-muted-foreground">Attendee</dt>
                <dd className="font-medium">{ticket.attendee_name}</dd>
              </div>
            </dl>
          </CardContent>
        </Card>
      ) : null}

      <p className="mt-6 text-sm">
        <Link href="/tickets" className="underline">
          Check another ticket
        </Link>
      </p>
    </main>
  );
}

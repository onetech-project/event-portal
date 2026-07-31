"use client";

import { useCallback, useState } from "react";

import { QrScanner } from "@/components/admin/qr-scanner";
import { ValidationResultCard } from "@/components/admin/validation-result-card";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { PageHeading, StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api-client";
import { useMarkTicketUsed, useValidateTicket } from "@/lib/queries";
import type { ValidationResult } from "@/lib/types";

export default function ValidatePage() {
  const [code, setCode] = useState("");
  const [result, setResult] = useState<ValidationResult | null>(null);

  const validate = useValidateTicket();
  const markUsed = useMarkTicketUsed();

  const check = useCallback(
    async (raw: string) => {
      const trimmed = raw.trim();
      if (!trimmed) return;

      markUsed.reset();
      setCode(trimmed);
      setResult(await validate.mutateAsync(trimmed));
    },
    [markUsed, validate],
  );

  const admit = useCallback(
    async (ticketCode: string) => {
      await markUsed.mutateAsync(ticketCode);
      // Re-validate so the card reflects the ticket's new state rather than a
      // locally-assumed one.
      setResult(await validate.mutateAsync(ticketCode));
    },
    [markUsed, validate],
  );

  let error: ApiError | null = null;
  if (validate.error instanceof ApiError) {
    error = validate.error;
  } else if (markUsed.error instanceof ApiError) {
    error = markUsed.error;
  }

  return (
    <main className="mx-auto w-full max-w-md flex-1 px-6 py-8">
      <PageHeading
        title="Validate tickets"
        subtitle="Type the code, or scan the QR on the attendee's ticket."
      />

      <Card>
        <CardContent className="space-y-5">
          <form
            onSubmit={(e) => {
              e.preventDefault();
              void check(code);
            }}
            aria-label="Validate ticket"
            className="space-y-4"
          >
            <Field label="Ticket code">
              <Input
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder="ABC234DEFG"
                autoFocus
                autoCapitalize="characters"
                className="font-mono tracking-widest"
              />
            </Field>

            <Button type="submit" disabled={validate.isPending || code.trim() === ""}>
              {validate.isPending ? "Checking…" : "Check ticket"}
            </Button>
          </form>

          <div className="border-t pt-5">
            <QrScanner onScan={(scanned) => void check(scanned)} />
          </div>
        </CardContent>
      </Card>

      {error ? (
        <div className="mt-4">
          <StatusAlert>{error.message}</StatusAlert>
        </div>
      ) : null}

      {result ? (
        <div className="mt-4">
          <ValidationResultCard
            result={result}
            onMarkUsed={(ticketCode) => void admit(ticketCode)}
            isMarking={markUsed.isPending}
          />
        </div>
      ) : null}
    </main>
  );
}

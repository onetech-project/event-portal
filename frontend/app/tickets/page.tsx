"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { PageHeading } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";

/** Entry point for the public ticket lookup — a code box that routes to /tickets/:code. */
export default function TicketLookupPage() {
  const router = useRouter();
  const [code, setCode] = useState("");

  return (
    <main className="mx-auto w-full max-w-md flex-1 px-6 py-10">
      <PageHeading
        title="Look up a ticket"
        subtitle="Enter the code printed on your ticket."
      />

      <Card>
        <CardContent>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const normalized = code.trim().toUpperCase();
              if (normalized) router.push(`/tickets/${encodeURIComponent(normalized)}`);
            }}
            className="space-y-4"
          >
            <Field label="Ticket code">
              <Input
                value={code}
                onChange={(e) => setCode(e.target.value)}
                placeholder="ABC234DEFG"
                autoCapitalize="characters"
                className="font-mono tracking-widest"
              />
            </Field>
            <Button type="submit" disabled={code.trim() === ""}>
              Check ticket
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}

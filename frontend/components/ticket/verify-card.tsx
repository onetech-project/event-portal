"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

/**
 * The verify-ticket form on /tickets: a code box that routes to the existing
 * GET /tickets/:code lookup screen. Verification stays reachable without
 * picking an event first — the header nav links straight to it.
 *
 * No title of its own: the page it sits on supplies the heading.
 */
export function VerifyCard() {
  const router = useRouter();
  const [code, setCode] = useState("");

  return (
    <Card>
      <CardContent>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const normalized = code.trim().toUpperCase();
            if (normalized !== "") {
              router.push(`/tickets/${encodeURIComponent(normalized)}`);
            }
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
          <Button type="submit" disabled={code.trim() === ""} className="w-full">
            Check ticket
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

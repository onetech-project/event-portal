import Link from "next/link";

import { Button } from "@/components/ui/button";

export default function Home() {
  return (
    <main className="mx-auto flex max-w-3xl flex-1 flex-col justify-center gap-6 px-6 py-16">
      <h1 className="text-3xl font-semibold tracking-tight">Event Ticketing</h1>
      <p className="text-muted-foreground">
        Browse upcoming events and buy tickets as a guest — no account needed. Your
        tickets arrive by email as a PDF with a QR code.
      </p>
      <div className="flex flex-wrap gap-3">
        <Button asChild size="lg">
          <Link href="/events">Browse events</Link>
        </Button>
        <Button asChild size="lg" variant="outline">
          <Link href="/tickets">Look up a ticket</Link>
        </Button>
      </div>
    </main>
  );
}

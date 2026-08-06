import { EventGrid } from "@/components/event/event-grid";

/**
 * The homepage (spec 008 US3): the event grid IS the landing page. Ticket
 * verification used to sit beside it in a two-column split; it now lives on
 * /tickets, reachable from the header nav, so the grid gets the full width.
 */
export default function Home() {
  return (
    <main className="mx-auto w-full max-w-[calc(100dvw-10rem)] flex-1 px-6 py-10">
      <header className="mb-8">
        <h1 className="text-3xl font-semibold tracking-tight">Upcoming events</h1>
        <p className="mt-1 text-muted-foreground">
          Buy tickets as a guest — no account needed. Your tickets arrive by email as
          a PDF with a QR code.
        </p>
      </header>

      <section aria-label="Events">
        <EventGrid />
      </section>
    </main>
  );
}

import type { ReactNode } from "react";

import { EventFrame } from "@/components/event/event-frame";

/**
 * Wraps every screen in one event's purchase journey — selection, checkout,
 * payment, confirmation — in that event's shared chrome.
 *
 * This stays a server component so `params` can be awaited here and nested pages
 * are not dragged across the client boundary by association. The client work
 * (fetching the event, ticking the countdown, reading the pathname) belongs to
 * EventFrame.
 */
export default async function EventLayout({
  children,
  params,
}: Readonly<{ children: ReactNode; params: Promise<{ slug: string }> }>) {
  const { slug } = await params;

  return <EventFrame slug={slug}>{children}</EventFrame>;
}

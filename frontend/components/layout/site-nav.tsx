"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

import { cn } from "@/lib/utils";

/**
 * `match` decides the active state: the events entry owns the homepage listing
 * *and* every event detail/purchase screen underneath /events, so a plain
 * `startsWith` on "/" would light it up everywhere.
 */
const LINKS = [
  {
    href: "/",
    label: "Events",
    match: (path: string) =>
      path === "/" || path.startsWith("/events"),
  },
  {
    href: "/tickets",
    label: "Validate ticket",
    match: (path: string) => path.startsWith("/tickets"),
  },
];

/**
 * The guest navigation inside SiteHeader — the two places a visitor can go
 * without an event in hand: the event list and ticket validation.
 *
 * Client-side because the active link is read from the pathname; SiteHeader
 * itself stays a server component.
 */
export function SiteNav() {
  const pathname = usePathname();
  // Both segments, deliberately. Purchase screens live under the PLURAL
  // "/events"; free registration (spec 022) lives under the SINGULAR "/event",
  // matching the API's own `GET /event` naming. A `startsWith("/events")` alone
  // silently stops matching the moment the singular route exists, so the
  // registration page would render the nav every purchase page hides.
  const isEventScoped =
    pathname.startsWith("/events");

  if (isEventScoped) return null;

  return (
    <nav aria-label="Main" className="flex items-center gap-1">
      {LINKS.map((link) => {
        const active = link.match(pathname);
        return (
          <Link
            key={link.href}
            href={link.href}
            aria-current={active ? "page" : undefined}
            className={cn(
              "rounded-lg px-3 py-2 text-base font-bold tracking-[0.4px] transition",
              active
                ? "bg-white/12 text-white"
                : "text-white/70 hover:bg-white/8 hover:text-white",
            )}
          >
            {link.label}
          </Link>
        );
      })}
    </nav>
  );
}

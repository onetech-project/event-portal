"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";

import { Button } from "@/components/ui/button";
import { clearToken } from "@/lib/auth";
import { cn } from "@/lib/utils";

const LINKS = [
  { href: "/admin/events", label: "Events" },
  { href: "/admin/orders", label: "Orders" },
  { href: "/admin/attendees", label: "Attendees" },
  { href: "/admin/fees", label: "Fees" },
  { href: "/admin/validate", label: "Validate" },
];

/** Shared navigation across every /admin/* page. */
export function AdminNav() {
  const pathname = usePathname();
  const router = useRouter();

  return (
    <header className="border-b bg-card">
      <nav className="mx-auto flex max-w-5xl items-center gap-1 px-6 py-3">
        <span className="mr-4 font-semibold">Admin</span>

        {LINKS.map((link) => {
          const active = pathname.startsWith(link.href);
          return (
            <Link
              key={link.href}
              href={link.href}
              aria-current={active ? "page" : undefined}
              className={cn(
                "rounded-lg px-3 py-1.5 text-sm transition",
                active
                  ? "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:bg-muted hover:text-foreground",
              )}
            >
              {link.label}
            </Link>
          );
        })}

        <Button
          variant="ghost"
          className="ml-auto"
          onClick={() => {
            // clearToken notifies this tab as well as the others, so the guard
            // above stops rendering admin content immediately rather than
            // waiting for the navigation to land.
            clearToken("signed-out");
            router.replace("/admin/login");
          }}
        >
          Sign out
        </Button>
      </nav>
    </header>
  );
}

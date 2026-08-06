import Link from "next/link";
import { FaWhatsapp } from "react-icons/fa6";

import { SiteNav } from "@/components/layout/site-nav";

/**
 * The guest-facing JIVE header: logo and navigation left, Help link right, on
 * the dark brand bar.
 *
 * Admin screens do not use this — they have their own chrome in
 * app/admin/layout.tsx.
 */
export function SiteHeader() {
  return (
    <header className="bg-ink">
      <div className="mx-auto flex h-24.25 max-w-330 items-center justify-between gap-6 px-6">
        <div className="flex items-center gap-6">
          <Link href="/" aria-label="JIVE home" className="shrink-0">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/brand/jive-logo.png"
              alt="JIVE"
              width={98}
              height={55}
              className="h-13.75 w-24.5 object-contain"
            />
          </Link>

          <SiteNav />
        </div>

        <a
          href="https://wa.me/"
          target="_blank"
          rel="noreferrer"
          className="flex items-center gap-2 text-base font-bold tracking-[0.4px] text-white transition-opacity hover:opacity-80"
        >
          {/* WhatsApp green, straight from the design. */}
          <FaWhatsapp aria-hidden size={24} className="text-[#25d366]" />
          Help
        </a>
      </div>
    </header>
  );
}

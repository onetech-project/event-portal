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
            {/*
              Capped by HEIGHT, with width following the asset's own ratio, so the
              bar can never grow to accommodate a mark (spec 016 FR-023b). A 200px
              mark in a 130px bar was tried on 2026-08-19 and reversed — it took
              too much of the viewport before any content showed.

              w-auto rather than a fixed width: swapping the asset for one of a
              different ratio then changes the mark's width and nothing else. The
              width/height attributes carry the current ratio to reserve the right
              box before the image loads.

              Still a plain <img>: next/image for one static above-the-fold asset
              trades a lint suppression for a loader and a layout shift, and
              nothing here needs either.
            */}
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img
              src="/brand/jive-logo-white.png"
              alt="JIVE"
              width={97}
              height={55}
              className="h-13.75 w-auto object-contain"
            />
          <SiteNav />
        </div>

        <a
          href="https://wa.me/6282245122667"
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

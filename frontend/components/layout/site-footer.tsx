import type { IconType } from "react-icons";
import {
  FaFacebookF,
  FaInstagram,
  FaTiktok,
  FaTwitter,
  FaYoutube,
} from "react-icons/fa6";

/**
 * The social row from the design, in its drawn order.
 *
 * These come from react-icons/fa6 — the same Font Awesome 6 Brands set the
 * design draws them from, so the marks match glyph for glyph rather than being
 * approximated. The design uses the classic bird (), not the X mark.
 */
const SOCIALS: { label: string; href: string; Icon: IconType }[] = [
  { label: "Instagram", href: "https://instagram.com/", Icon: FaInstagram },
  { label: "Twitter", href: "https://twitter.com/", Icon: FaTwitter },
  { label: "YouTube", href: "https://youtube.com/", Icon: FaYoutube },
  { label: "TikTok", href: "https://tiktok.com/", Icon: FaTiktok },
  { label: "Facebook", href: "https://facebook.com/", Icon: FaFacebookF },
];

/**
 * The guest-facing JIVE footer on the dark brand bar. Admin screens do not use
 * it — they have their own chrome.
 */
export function SiteFooter() {
  return (
    <footer className="mt-auto border-t bg-ink">
      <div className="mx-auto flex min-h-24.25 max-w-7xl flex-wrap items-center justify-between gap-4 px-8 py-8">
        <p className="text-sm font-medium text-white">
          © {new Date().getFullYear()} Jive. All rights reserved.
        </p>

        <ul className="flex items-center gap-4.5">
          {SOCIALS.map(({ label, href, Icon }) => (
            <li key={label}>
              <a
                href={href}
                target="_blank"
                rel="noreferrer"
                aria-label={label}
                className="block text-white transition-opacity hover:opacity-70"
              >
                <Icon aria-hidden size={24} />
              </a>
            </li>
          ))}
        </ul>
      </div>
    </footer>
  );
}

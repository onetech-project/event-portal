"use client";

import { Info } from "lucide-react";
import { DynamicIcon, dynamicIconImports } from "lucide-react/dynamic";
import type { IconName } from "lucide-react/dynamic";

/**
 * Renders a CMS content-block icon by its catalog key (spec 009). Any key in
 * the generated lucide catalog resolves via DynamicIcon's per-icon lazy
 * chunks, so the full 2,000+ set never enters the page bundle. A stale key —
 * possible only if the catalog shrinks in a future upgrade — falls back to
 * Info statically, keeping the page intact without console noise; the server
 * refuses unknown keys at write time (400004), so this is a read-side guard.
 */
export function ContentIcon({
  name,
  className,
}: {
  name: string | null;
  className?: string;
}) {
  if (name === null || name === "") return null;
  if (!(name in dynamicIconImports)) {
    return <Info aria-hidden className={className} />;
  }
  // No `fallback` prop: it would also render during the icon's lazy load,
  // flashing Info before every glyph. Rendering nothing until loaded is calmer,
  // and load failures (network-level only, since membership is pre-checked)
  // then omit the icon rather than break the page.
  return <DynamicIcon name={name as IconName} aria-hidden className={className} />;
}

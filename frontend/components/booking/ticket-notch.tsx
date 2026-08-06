/**
 * The semicircular bite taken out of a ticket stub's left and right edges.
 *
 * It is a small bordered box that straddles the card edge and is rounded on its
 * inner side only, so it reads as a punched hole rather than a pill sitting on
 * top. `bg-background` is what makes it look cut out — it must match whatever is
 * behind the card, not the card's own fill.
 */
export function TicketNotch({
  side,
  className,
}: {
  side: "left" | "right";
  className?: string;
}) {
  const edge =
    side === "left"
      ? "-left-px rounded-r-full border-y border-r"
      : "-right-px rounded-l-full border-y border-l";

  return (
    <span
      aria-hidden
      className={`pointer-events-none absolute top-1/2 h-6.5 w-3 -translate-y-1/2 bg-background ${edge} ${className ?? ""}`}
    />
  );
}

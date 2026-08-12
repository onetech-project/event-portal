import { formatDate, formatDateRange } from "@/lib/format";

/**
 * Renders the days an order line admits on (spec 015 FR-021a–c).
 *
 * A ticket line carries one day. A bundle carries one per constituent, and
 * naming only the first was the defect this exists to fix: a "Day 1 & 2" bundle
 * told its buyer they were attending on one day while they held admission for
 * two.
 *
 * The rule, in the buyer's terms:
 *   - one or two distinct days  → both spelled out, "30 Sep 2026, 1 Oct 2026"
 *   - three or more             → a range, "30 Sep - 2 Oct 2026"
 *
 * **Deduplication happens on the rendered string, not on the instant**, and that
 * is deliberate. A calendar date is not a property of an instant; it is a
 * property of an instant read in a timezone. 2026-09-30T22:00Z and
 * 2026-10-01T02:00Z are two distinct instants and two distinct UTC dates, but in
 * Jakarta they are both 1 October — one day to the buyer. Comparing what is
 * actually printed makes "distinct dates" true by construction, whoever is
 * reading and wherever they are.
 *
 * The range's endpoints are the first and last **start** instants, never an
 * event end (FR-021c): a ticket admitting 22:00–02:00 ends on the following
 * calendar day, and closing the range there would advertise a day on which
 * nothing in the bundle admits anyone.
 *
 * A bundle spanning non-contiguous days — Day 1, Day 2, Day 5 — renders as a
 * range and so reads as though every day between is included. That is a known
 * cost of the ranged form, accepted because past two dates a list stops fitting
 * the line; the exact days remain on each issued ticket.
 */
export function formatAdmissionDates(starts: readonly string[]): string {
  if (starts.length === 0) return "";

  const rendered: string[] = [];
  const kept: string[] = [];
  for (const start of starts) {
    const text = formatDate(start);
    if (rendered.includes(text)) continue;
    rendered.push(text);
    kept.push(start);
  }

  if (rendered.length <= 2) return rendered.join(", ");

  return formatDateRange(kept[0], kept[kept.length - 1]);
}

import type { TicketTypeAdminView } from "@/lib/types";

/**
 * Which of an event's ticket types now admit on days the event itself does not
 * run (spec 015 FR-005b).
 *
 * This exists because containment is enforced in one direction only. A ticket
 * type cannot be saved with a window outside its parent event, but the event
 * CAN be moved out from under its ticket types — deliberately, because enforcing
 * both directions deadlocks a reschedule: the event cannot move while its
 * tickets sit on the old dates, and the tickets cannot move while the event
 * still sits on them. Neither could go first.
 *
 * So the event edit stays open and warns instead. The warning is derived here
 * rather than returned by the API because the admin event payload already
 * carries both halves of the comparison.
 *
 * Widening an event can never strand anything, so this returns empty for the
 * common edit.
 */
export function strandedTicketTypes(
  event: { start_date: string; end_date: string },
  ticketTypes: readonly TicketTypeAdminView[],
): TicketTypeAdminView[] {
  const eventStart = new Date(event.start_date).getTime();
  const eventEnd = new Date(event.end_date).getTime();
  if (Number.isNaN(eventStart) || Number.isNaN(eventEnd)) return [];

  return ticketTypes.filter((ticketType) => {
    const start = new Date(ticketType.event_start).getTime();
    const end = new Date(ticketType.event_end).getTime();
    // An unparseable window is not evidence of a problem; leave it alone rather
    // than raising a warning nobody can act on.
    if (Number.isNaN(start) || Number.isNaN(end)) return false;
    return start < eventStart || end > eventEnd;
  });
}

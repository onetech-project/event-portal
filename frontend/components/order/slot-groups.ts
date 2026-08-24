import type { TicketOrderSlot } from "@/lib/types";

/**
 * One visitor form's worth of slots (spec 010): a bundle unit collapses to a
 * single form that fills every slot in the unit with the same visitor, while
 * standalone tickets keep one form each.
 */
export type SlotGroup = {
  /** Stable key: the slot id for solo groups, `packageId:unit` for bundle units. */
  key: string;
  /** Every slot this group's single form fills (identical visitor data). */
  slotIds: string[];
  /** Card title: the bundle's name for bundle groups, the ticket type's otherwise. */
  title: string;
  /** slotIds.length — drives the "N tickets" badge on bundle forms. */
  ticketCount: number;
  /** True only for unit-grouped bundle forms (title is the package name). */
  isBundle: boolean;
  /** Package origin; null for standalone slots. */
  packageId: string | null;
  /** The purchased unit this group covers; null for solo groups. */
  packageUnit: number | null;
  /**
   * Pre-010 rendering aid: a bundle slot booked before units existed renders
   * solo with its package name as a badge (exactly the old card).
   */
  packageBadge: string | null;
  /**
   * What this card's fields start out holding (spec 011 FR-030, clarified
   * 2026-08-19). Empty strings when the slot has never been filled, which is
   * every slot until checkout saves them.
   *
   * Read from the group's FIRST slot: a bundle unit's slots are written
   * identically by the submit-time fan-out, so they cannot disagree and no
   * tie-break between them is specified.
   */
  seed: SlotSeed;
};

/** One card's worth of stored holder details, ready for the form. */
export type SlotSeed = {
  name: string;
  email: string;
  phone: string;
  /** DD/MM/YYYY, converted from the wire's date-only YYYY-MM-DD. */
  dob: string;
  /** The gender master row's NAME, which may since have been deactivated. */
  gender: string;
};

/**
 * The stored details of one slot as form values. A slot the guest has never
 * filled carries nulls in every field, and an empty string is what an untouched
 * input holds — so the two states render identically, which is FR-030's last
 * clause.
 */
function seedFrom(slot: TicketOrderSlot): SlotSeed {
  return {
    name: slot.name ?? "",
    email: slot.email ?? "",
    phone: slot.phone ?? "",
    dob: isoToDob(slot.dob),
    gender: slot.gender ?? "",
  };
}

/**
 * The wire's date-only `YYYY-MM-DD` as the field's `DD/MM/YYYY`. The inverse of
 * `dobToIso` in visitor-form.tsx, and it must round-trip: a restored date
 * submitted unchanged has to store the date it came from.
 *
 * Anything that is not a plain ISO date comes back empty rather than partly
 * converted — a half-formed value in a masked field is worse than an empty one,
 * because the guest cannot tell what it was.
 */
export function isoToDob(iso: string | null): string {
  if (iso === null) return "";
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!match) return "";
  const [, year, month, day] = match;
  return `${day}/${month}/${year}`;
}

/**
 * Groups an order's slots into visitor forms. Slots sharing a non-null
 * (package_id, package_unit) become one form; a standalone slot — or a bundle
 * slot from an order booked before spec 010 (package_unit null) — is its own
 * form, keeping legacy orders on the old one-form-per-slot behavior.
 */
export function groupOrderSlots(slots: TicketOrderSlot[]): SlotGroup[] {
  const groups: SlotGroup[] = [];
  const bundleGroups = new Map<string, SlotGroup>();

  for (const slot of slots) {
    if (slot.package_id !== null && slot.package_unit !== null) {
      const key = `${slot.package_id}:${slot.package_unit}`;
      const existing = bundleGroups.get(key);
      if (existing) {
        existing.slotIds.push(slot.id);
        existing.ticketCount = existing.slotIds.length;
        continue;
      }
      const group: SlotGroup = {
        key,
        slotIds: [slot.id],
        title: slot.package_name ?? slot.ticket_type_name,
        ticketCount: 1,
        isBundle: true,
        packageId: slot.package_id,
        packageUnit: slot.package_unit,
        packageBadge: null,
        seed: seedFrom(slot),
      };
      bundleGroups.set(key, group);
      groups.push(group);
      continue;
    }

    groups.push({
      key: slot.id,
      slotIds: [slot.id],
      title: slot.ticket_type_name,
      ticketCount: 1,
      isBundle: false,
      packageId: slot.package_id,
      packageUnit: null,
      packageBadge: slot.package_name,
      seed: seedFrom(slot),
    });
  }

  return groups;
}

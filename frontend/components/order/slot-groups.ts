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
};

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
    });
  }

  return groups;
}

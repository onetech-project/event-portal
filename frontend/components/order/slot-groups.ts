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
  /** "Visitor <n>" when the same package appears with more than one unit. */
  unitLabel: string | null;
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
  const unitsPerPackage = new Map<string, Set<number>>();

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
        unitLabel: null,
      };
      bundleGroups.set(key, group);
      groups.push(group);
      let units = unitsPerPackage.get(slot.package_id);
      if (!units) {
        units = new Set();
        unitsPerPackage.set(slot.package_id, units);
      }
      units.add(slot.package_unit);
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
      unitLabel: null,
    });
  }

  // Multiple purchased units of one package need distinguishable forms (US3).
  for (const group of groups) {
    if (!group.isBundle || group.packageId === null || group.packageUnit === null) {
      continue;
    }
    if ((unitsPerPackage.get(group.packageId)?.size ?? 0) > 1) {
      group.unitLabel = `Visitor ${group.packageUnit}`;
    }
  }

  return groups;
}

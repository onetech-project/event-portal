"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Info } from "lucide-react";
import { useMemo } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { OrderSummaryPanel } from "@/components/order/order-summary-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { HolderFields } from "@/components/order/holder-fields";
import { StatusAlert } from "@/components/ui/feedback";
import { groupOrderSlots, type SlotGroup } from "@/components/order/slot-groups";
import { API_CODES, ApiError } from "@/lib/api-client";
import {
  dobSchema,
  dobToIso,
  genderSchema,
  holderEmailSchema,
  holderNameSchema,
  phoneSchema,
} from "@/lib/holder-fields";
import { useGenders, useStartCheckout } from "@/lib/queries";
import type { TicketOrderDetail } from "@/lib/types";
import QrisIcon from "../icons/qris";

/**
 * The registration phase of the order page (spec 011, superseding 008 US5):
 * ONE visitor card per slot GROUP and nothing else — there is no separate
 * buyer form. The topmost card's holder becomes the order's primary contact
 * (derived server-side from canonical slot order). Everything is submitted in
 * the single Continue to Payment call (Option B —
 * POST /ticket/checkout/:order_id), and nothing is persisted before it.
 *
 * That rule is about when details are SAVED, not about what a revisit shows
 * (spec 011 FR-030, clarified 2026-08-19). The call saves every holder before it
 * reaches the gateway and a gateway failure compensates nothing, so an order can
 * sit PENDING with its forms stored and no payment started. The cards therefore
 * render whatever the slots hold; only a slot that has never been filled comes
 * up empty.
 *
 * Spec 010: a bundle unit's slots collapse into ONE card titled with the
 * bundle's name; on submit the card's values fan out to one payload entry per
 * slot, so every ticket in the unit stores the same visitor and the wire
 * contract stays per-slot.
 */

const visitorSchema = z.object({
  /** The slot ids this card fills (a whole bundle unit, or one standalone slot). */
  slot_ids: z.array(z.string()).min(1),
  name: holderNameSchema,
  email: holderEmailSchema,
  phone: phoneSchema,
  dob: dobSchema,
  gender: genderSchema,
});

const formsSchema = z.object({
  attendees: z.array(visitorSchema),
});

export type OrderFormsValues = z.infer<typeof formsSchema>;

export function OrderForms({ order }: { order: TicketOrderDetail }) {
  const checkout = useStartCheckout(order.order_id);
  // The gender master list (clarified 2026-08-05); every gender select on the
  // page shares this one fetch.
  const { data: genders } = useGenders();

  // One card per group (spec 010): a bundle unit's slots share a single form.
  const groups = useMemo(() => groupOrderSlots(order.slots), [order.slots]);

  const {
    control,
    handleSubmit,
    setError,
    trigger,
    // mode "onTouched" keeps isValid live for the button gate (FR-009) while
    // only showing a field's error once the guest has actually been in it.
    formState: { errors, isValid },
  } = useForm<OrderFormsValues>({
    resolver: zodResolver(formsSchema),
    mode: "onTouched",
    // Seeded from whatever the order already holds (FR-030, clarified
    // 2026-08-19). The presence of stored details is the ONLY condition — this
    // never inspects why the guest came back, because the case it exists for
    // (a checkout whose forms saved and whose gateway leg then failed) reports
    // payment_started false and is indistinguishable from a plain reload.
    //
    // Seeded ONCE, and deliberately so: useForm captures defaultValues at mount,
    // and OrderForms only mounts with data in hand. Do NOT add an effect that
    // resets the form when `order` changes, and do NOT switch to RHF's `values`
    // prop — the order is re-read on every window focus and reconnect, so either
    // would wipe a guest's typing on their way back from their banking app
    // (FR-032).
    defaultValues: {
      attendees: groups.map((group) => ({
        slot_ids: group.slotIds,
        ...group.seed,
      })),
    },
  });

  const onSubmit = handleSubmit(async (values) => {
    // The wire contract stays one entry per slot: each card fans out to all of
    // its slot ids with identical values, and payloadGroupIndex remembers which
    // card produced each entry so server errors can find their way back.
    const payloadGroupIndex: number[] = [];
    const attendees = values.attendees.flatMap((entry, groupIndex) =>
      entry.slot_ids.map((slotId) => {
        payloadGroupIndex.push(groupIndex);
        return {
          id: slotId,
          name: entry.name,
          email: entry.email,
          phone: entry.phone,
          // The schema has proven the DD/MM/YYYY value converts; the wire
          // contract stays ISO.
          dob: dobToIso(entry.dob) ?? entry.dob,
          gender: entry.gender,
        };
      }),
    );

    try {
      // Spec 011: no buyer_* fields — the server derives the primary contact
      // from the topmost form via canonical slot order.
      await checkout.mutateAsync({ attendees });
      // Success flips payment_started on the invalidated order query; the
      // page re-renders into the QR phase on its own.
    } catch (error) {
      // 400001 carries a field→message map as data; mark the exact inputs.
      if (
        error instanceof ApiError &&
        error.code === API_CODES.validation &&
        typeof error.data === "object" &&
        error.data !== null
      ) {
        for (const [field, message] of Object.entries(
          error.data as Record<string, string>,
        )) {
          // Server paths index the fanned-out payload (attendees[3].dob) —
          // remap onto the visible card, then rewrite to RHF's attendees.0.dob.
          const rhfPath = field
            .replace(/^attendees\[(\d+)\]/, (match, index) => {
              const groupIndex = payloadGroupIndex[Number(index)];
              return groupIndex === undefined
                ? match
                : `attendees[${groupIndex}]`;
            })
            .replace(/\[(\d+)\]/g, ".$1") as keyof OrderFormsValues;
          setError(rhfPath, { type: "server", message });
        }
      }
    }
  });

  const blockedError =
    checkout.error instanceof ApiError &&
    checkout.error.code !== API_CODES.validation
      ? checkout.error
      : null;

  return (
    <form
      onSubmit={onSubmit}
      aria-label="Visitor registration"
      className="space-y-6"
    >
      <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_340px] lg:items-start">
        <div className="space-y-6">
          {blockedError ? (
            <StatusAlert>{checkoutErrorMessage(blockedError)}</StatusAlert>
          ) : null}

          {groups.map((group, index) => (
            <Card key={group.key}>
              <CardHeader className="flex flex-wrap-reverse items-center justify-between gap-2 min-w-0">
                <GroupTitle group={group} />
                {/* FR-011 (clarified 2026-08-06): the delivery notice rides the
                  FIRST card only, top right of its header (Figma 206-1804). */}
                {index === 0 ? (
                  <Badge className="ml-auto h-fit bg-sky-50 px-2.5 py-1 text-xs font-medium text-sky-700">
                    <Info aria-hidden className="size-3.5" />
                    The invoice and e-ticket will be sent via email
                  </Badge>
                ) : null}
              </CardHeader>
              <CardContent className="grid gap-4 sm:grid-cols-2">
                <HolderFields
                  control={control}
                  fields={{
                    name: `attendees.${index}.name`,
                    email: `attendees.${index}.email`,
                    phone: `attendees.${index}.phone`,
                    gender: `attendees.${index}.gender`,
                    dob: `attendees.${index}.dob`,
                  }}
                  errors={{
                    name: errors.attendees?.[index]?.name?.message,
                    email: errors.attendees?.[index]?.email?.message,
                    phone: errors.attendees?.[index]?.phone?.message,
                    gender: errors.attendees?.[index]?.gender?.message,
                    dob: errors.attendees?.[index]?.dob?.message,
                  }}
                  genders={genders}
                />
              </CardContent>
            </Card>
          ))}
        </div>

        <OrderSummary
          order={order}
          submitting={checkout.isPending}
          canSubmit={isValid}
          onBlockedClick={() => void trigger()}
        />
      </div>
    </form>
  );
}

function OrderSummary({
  order,
  submitting,
  canSubmit,
  onBlockedClick,
}: {
  order: TicketOrderDetail;
  submitting: boolean;
  canSubmit: boolean;
  onBlockedClick: () => void;
}) {
  // No order-hold countdown on this step (Figma 12-4456, clarified
  // 2026-08-05): the shared header's event countdown is the only timer here.
  // The hold still expires server-side.
  return (
    <aside className="lg:sticky lg:top-6">
      <OrderSummaryPanel
        order={order}
        // Form step: the pre-fee subtotal, no fees in any form — neither
        // itemized rows nor a fee-inclusive figure (constitution v4.0.0 fee
        // presentation, spec 011 FR-016).
        phase="registration"
        beforeTotal={
          <div>
            <p className="text-xs font-semibold uppercase text-muted-foreground">
              Payment method
            </p>
            {/* QRIS is the only method, so the radio is pre-selected and
                read-only — but it still LOOKS like the choice it is in the
                design (Figma 12-4456). */}
            <label className="mt-2 flex items-center justify-between gap-3 rounded-lg border border-brand bg-brand-surface px-3 py-2.5 text-sm">
              <span className="flex items-center gap-3">
                {/* Stand-in for the QRIS mark — no logo asset is bundled. */}
                <span
                  aria-hidden
                  className="flex p-1 aspect-square shrink-0 items-center justify-center rounded bg-background text-[9px] font-black tracking-tight"
                >
                  <QrisIcon className="size-8" />
                </span>
                <span>
                  <span className="block font-semibold">QRIS</span>
                  <span className="block text-xs text-muted-foreground">
                    Scan to pay
                  </span>
                </span>
              </span>
              {/* Custom ring-and-dot radio (Figma 206-3145); the sr-only input
                  keeps the real role and checked state for assistive tech. */}
              <span className="relative flex shrink-0 items-center">
                <input
                  type="radio"
                  name="payment_method"
                  checked
                  readOnly
                  aria-label="Pay with QRIS"
                  className="sr-only"
                />
                <span
                  aria-hidden
                  className="flex size-5 items-center justify-center rounded-full border-2 border-brand"
                >
                  <span className="size-2.5 rounded-full bg-brand" />
                </span>
              </span>
            </label>
          </div>
        }
        afterTotal={
          <div
            onClick={canSubmit ? undefined : onBlockedClick}
            data-testid="continue-gate"
          >
            <Button
              size="lg"
              type="submit"
              disabled={!canSubmit || submitting}
              className="w-full bg-brand hover:bg-brand/90 text-brand-foreground transition-all disabled:pointer-events-none"
            >
              {submitting ? "Starting payment…" : "Continue to Payment"}
            </Button>
          </div>
        }
      />
    </aside>
  );
}

/**
 * The card's heading: the group's name, and a badge for what the card covers.
 *
 * The heading WRAPS rather than ellipsizing. It used to be pinned to a single
 * line, and because `truncate` leaves no trace in the DOM that line dragged a
 * whole measuring apparatus behind it — a ResizeObserver, a font-loading
 * callback, and a tooltip — whose only job was to hand back the text the
 * ellipsis had taken away. Letting the line wrap shows the name outright, and
 * all of that goes with it.
 *
 * The badge keeps `ml-auto`, so it sits against the right edge whether it
 * shares the first line with the name or drops onto its own.
 *
 * Spec 019 FR-011 removed the per-unit "Visitor <n>" label that used to sit
 * between the name and the badge. Two units of one bundle therefore now carry
 * identical headings; that is accepted, not overlooked — nothing binds a
 * particular holder to a particular unit, so the guest fills the cards in the
 * order shown and either order submits correctly.
 */
function GroupTitle({ group }: { group: SlotGroup }) {
  // A bundle card badges its ticket count; a pre-010 bundle slot renders solo
  // and badges its package name instead (see SlotGroup.packageBadge). The two
  // are mutually exclusive, so one badge covers both.
  const badge = group.isBundle
    ? `${group.ticketCount} ${group.ticketCount === 1 ? "ticket" : "tickets"}`
    : group.packageBadge;

  return (
    <CardTitle
      // data-slot is what [data-slot="card-title"] styling and queries key off.
      data-slot="card-title"
      // flex-1 so the heading fills the width its header leaves it. Without it
      // the CardTitle is only as wide as its text, so `ml-auto` would pin the
      // badge to the end of the NAME rather than to the edge of the card — a
      // different place on every card, which is the opposite of what a
      // right-aligned badge is for.
      className="flex flex-1 flex-wrap gap-1 items-center gap-y-1 text-lg font-bold lg:w-full"
    >
      {/* min-w-0 with wrap-break-word so a bundle named as one unbroken token wraps
          inside the card rather than pushing past its edge — a flex item will
          not otherwise shrink below its own content. */}
      <span className="min-w-0 wrap-break-word">{group.title}</span>
      {badge !== null ? (
        <Badge className="ml-auto shrink-0 rounded bg-brand-surface px-2 py-0.5 text-xs font-medium text-brand">
          {badge}
        </Badge>
      ) : null}
    </CardTitle>
  );
}


/**
 * A gender select fed by the master list (GET /ticket/genders). The design
 * system Select holds its own value rather than exposing a native input, so it
 * goes through a Controller instead of register().
 *
 * The master list serves ACTIVE genders only, while a slot keeps whatever gender
 * it was saved with — deactivating an entry never rewrites a stored reference.
 * So a restored card can hold a value this list does not offer, and a select can
 * only show a value it has an option for. FR-031: the held value is added to
 * THIS card's options so it displays, and to no other card's.
 *
 * The widening tracks the live field value rather than the seed, which is what
 * makes the retired option leave the list the moment the guest picks something
 * else — a list widened from the seed would keep offering it forever.
 */
/** Words a non-field checkout failure for the guest. */
function checkoutErrorMessage(error: ApiError): string {
  if (error.code === API_CODES.termsNotRecorded) {
    return "Your Terms & Conditions agreement was not recorded. Please go back and book again.";
  }
  if (error.code === API_CODES.orderExpired) {
    return "This order has expired. Please book again.";
  }
  if (error.code === API_CODES.paymentInitiationFailed) {
    return "We could not start the payment with the provider. Your details are saved — please try again.";
  }
  if (error.code === API_CODES.rateLimited) {
    return "Too many attempts. Please wait a moment and try again.";
  }
  return error.message;
}

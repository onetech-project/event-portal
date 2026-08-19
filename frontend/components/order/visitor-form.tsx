"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Info } from "lucide-react";
import { useMemo } from "react";
import {
  Controller,
  useForm,
  type Control,
  type FieldPath,
} from "react-hook-form";
import { z } from "zod";

import { OrderSummaryPanel } from "@/components/order/order-summary-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { groupOrderSlots, type SlotGroup } from "@/components/order/slot-groups";
import { API_CODES, ApiError } from "@/lib/api-client";
import { useGenders, useStartCheckout } from "@/lib/queries";
import type { GenderOption, TicketOrderDetail } from "@/lib/types";
import QrisIcon from "../icons/qris";

/**
 * The registration phase of the order page (spec 011, superseding 008 US5):
 * ONE visitor card per slot GROUP and nothing else — there is no separate
 * buyer form. The topmost card's holder becomes the order's primary contact
 * (derived server-side from canonical slot order). Everything is submitted in
 * the single Continue to Payment call (Option B —
 * POST /ticket/checkout/:order_id); nothing is persisted before that call,
 * which is why a revisit always shows empty forms.
 *
 * Spec 010: a bundle unit's slots collapse into ONE card titled with the
 * bundle's name; on submit the card's values fan out to one payload entry per
 * slot, so every ticket in the unit stores the same visitor and the wire
 * contract stays per-slot.
 */

// DOB is typed as DD/MM/YYYY text — no native calendar control (clarified
// 2026-08-06) — and crosses the wire as ISO YYYY-MM-DD via dobToIso.
const DOB_PATTERN = /^\d{2}\/\d{2}\/\d{4}$/;

const dobSchema = z
  .string()
  .min(1, "Date of birth is required.")
  .regex(DOB_PATTERN, "Enter a date as DD/MM/YYYY.")
  .refine((value) => !DOB_PATTERN.test(value) || dobToIso(value) !== null, {
    message: "Enter a real calendar date.",
  })
  .refine(
    (value) => {
      const iso = dobToIso(value);
      // Compared as ISO strings against the guest's LOCAL today (both are
      // zero-padded, so lexical order is chronological). Parsing to a Date
      // would read the value as UTC midnight and reject today's date for
      // anyone east of UTC during their morning — today must be accepted.
      return iso === null || iso <= todayIso();
    },
    { message: "Date of birth cannot be in the future." },
  );

// The gender option set comes from GET /ticket/genders (master data), so the
// schema only requires a choice; the server checks membership.
const genderSchema = z.string().min(1, "Select a gender.");

// Spec 011 FR-006 (clarified 2026-08-07, floor raised 2026-08-13) — 12-15
// digits, and nothing but digits. Length is the whole rule: no prefix is
// required, and the digits are counted on the value exactly as typed. The guest
// may enter `628123456789` or `081234567890` and whichever they chose is what
// gets stored — but a shorter local-form number like `08123456789` is eleven
// digits and is refused, which is the one case the raised floor changes. The
// message matches the server's word-for-word: both surface on the same inline
// field slot.
const PHONE_MESSAGE = "Enter a phone number of 12-15 digits.";

const phoneSchema = z.string().regex(/^[0-9]{12,15}$/, PHONE_MESSAGE);

const visitorSchema = z.object({
  /** The slot ids this card fills (a whole bundle unit, or one standalone slot). */
  slot_ids: z.array(z.string()).min(1),
  name: z.string().trim().min(1, "Full name is required."),
  email: z.email("Enter a valid email address."),
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
    register,
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
    // Option B: always empty — the server holds nothing to prefill from.
    defaultValues: {
      attendees: groups.map((group) => ({
        slot_ids: group.slotIds,
        name: "",
        email: "",
        phone: "",
        dob: "",
        gender: "",
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
                <div className="sm:col-span-2">
                  <Field
                    label="Full name"
                    required
                    error={errors.attendees?.[index]?.name?.message}
                  >
                    <Input
                      className="h-auto px-3 py-4"
                      placeholder="As written on ID card"
                      {...register(`attendees.${index}.name`)}
                    />
                  </Field>
                </div>
                <Field
                  label="Email address"
                  required
                  error={errors.attendees?.[index]?.email?.message}
                >
                  <Input
                    className="h-auto px-3 py-4"
                    placeholder="name@example.com"
                    {...register(`attendees.${index}.email`)}
                  />
                </Field>
                <Field
                  label="Phone number"
                  required
                  error={errors.attendees?.[index]?.phone?.message}
                >
                  <PhoneInput
                    control={control}
                    name={`attendees.${index}.phone`}
                  />
                </Field>
                <Field
                  label="Gender"
                  required
                  error={errors.attendees?.[index]?.gender?.message}
                >
                  <GenderSelect
                    control={control}
                    name={`attendees.${index}.gender`}
                    options={genders}
                  />
                </Field>
                <Field
                  label="Date of birth"
                  required
                  error={errors.attendees?.[index]?.dob?.message}
                >
                  <DobInput control={control} name={`attendees.${index}.dob`} />
                </Field>
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
      // lg:w-full so the heading claims the whole header row from lg up. Its
      // header is flex-wrap-reverse, so that pushes the first card's delivery
      // notice onto a second line which wrap-reverse renders ABOVE — which is
      // what puts the notice top-right of the header (Figma 206-1804). It is
      // deliberately not flex-1: that would grow the heading but leave the
      // notice on the same line, beside the ticket-count badge.
      //
      // Below lg no width applies, so the heading is only as wide as its text
      // and this badge sits at the end of the NAME rather than at the card
      // edge. That is how the brand refresh shipped it, not an oversight
      // carried over — the narrow layout stacks rather than aligning to edges.
      className="flex flex-wrap gap-1 items-center gap-y-1 text-lg font-bold lg:w-full"
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
 */
function GenderSelect({
  control,
  name,
  options,
}: {
  control: Control<OrderFormsValues>;
  name: FieldPath<OrderFormsValues>;
  options: GenderOption[] | undefined;
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Select
          value={field.value === "" ? null : (field.value as string)}
          onValueChange={(value) => field.onChange(value ?? "")}
        >
          <SelectTrigger
            aria-label="Gender"
            className="px-3 py-4 w-full data-[size=default]:h-fit "
          >
            {/* The trigger shows the human label, never the stored value. */}
            <SelectValue>
              {(value: string | null) =>
                value === null ? "Select" : genderLabel(value)
              }
            </SelectValue>
          </SelectTrigger>
          <SelectContent align="start" alignItemWithTrigger={false}>
            {(options ?? []).map((option) => (
              <SelectItem key={option.id} value={option.name} className="p-3">
                {genderLabel(option.name)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    />
  );
}

/**
 * Phone as a free-text, digits-only field (FR-006, clarified 2026-08-07). No
 * country code is supplied by the interface: the guest types the whole number
 * in whichever form they think in, `62…` or `08…`, and it is stored exactly
 * that way. The only thing the mask does is keep non-digits out, because
 * `type="tel"` and `inputMode="numeric"` are keyboard hints rather than filters
 * — a physical keyboard types letters straight through both — so without it the
 * field would accept anything until the schema complained on blur. Controlled
 * through a Controller so the masked value is what RHF validates and submits.
 */
function PhoneInput({
  control,
  name,
}: {
  control: Control<OrderFormsValues>;
  name: FieldPath<OrderFormsValues>;
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Input
          className="h-auto px-3 py-4"
          inputMode="numeric"
          type="tel"
          // Twelve digits: the placeholder must not advertise an example the
          // field would refuse (FR-006's floor, raised 2026-08-13).
          placeholder="081234567890"
          value={field.value as string}
          onChange={(event) => field.onChange(phoneDigits(event.target.value))}
          onBlur={field.onBlur}
          name={field.name}
          ref={field.ref}
        />
      )}
    />
  );
}

/**
 * Date of birth as DD/MM/YYYY text — no native calendar control (clarified
 * 2026-08-06). The mask feeds the slashes in as the guest types digits, which
 * is what lets the numeric mobile keypad (no "/" key) still produce the
 * format. Controlled through a Controller so the masked value is what RHF
 * validates.
 */
function DobInput({
  control,
  name,
}: {
  control: Control<OrderFormsValues>;
  name: FieldPath<OrderFormsValues>;
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Input
          className="h-auto px-3 py-4"
          placeholder="DD/MM/YYYY"
          inputMode="numeric"
          maxLength={10}
          value={field.value as string}
          onChange={(event) => field.onChange(maskDob(event.target.value))}
          onBlur={field.onBlur}
          name={field.name}
          ref={field.ref}
        />
      )}
    />
  );
}

/** "FEMALE" → "Female": the master list stores the canonical uppercase value. */
function genderLabel(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1).toLowerCase();
}

/** "31/12/1999" → "1999-12-31"; null when not a real calendar date. */
function dobToIso(value: string): string | null {
  if (!DOB_PATTERN.test(value)) return null;
  const day = Number(value.slice(0, 2));
  const month = Number(value.slice(3, 5));
  const year = Number(value.slice(6));
  // Round-trip through Date to reject e.g. 31/02/2000 (Date rolls it over).
  const date = new Date(Date.UTC(year, month - 1, day));
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return null;
  }
  return `${value.slice(6)}-${value.slice(3, 5)}-${value.slice(0, 2)}`;
}

/** The guest's local calendar date as ISO, e.g. "2026-08-06". */
function todayIso(): string {
  const now = new Date();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${now.getFullYear()}-${month}-${day}`;
}

/**
 * Whatever was typed or pasted → the digits of it, capped at the 15 the server
 * accepts. Nothing else is touched: a leading 0 stays a leading 0 and a leading
 * 62 stays a leading 62, because FR-006 keeps whichever form the guest chose.
 * A pasted "+62 812-3456-789" therefore lands on "628123456789" — the
 * separators and the "+" fall away, the number itself does not change form.
 */
function phoneDigits(raw: string): string {
  return raw.replace(/\D/g, "").slice(0, 15);
}

/** Keeps only digits and re-inserts the slashes: "31121999" → "31/12/1999". */
function maskDob(raw: string): string {
  const digits = raw.replace(/\D/g, "").slice(0, 8);
  return [digits.slice(0, 2), digits.slice(2, 4), digits.slice(4)]
    .filter((part) => part !== "")
    .join("/");
}

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

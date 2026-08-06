"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Info } from "lucide-react";
import { useMemo } from "react";
import { Controller, useForm, type Control, type FieldPath } from "react-hook-form";
import { z } from "zod";

import { OrderSummaryPanel } from "@/components/order/order-summary-panel";
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
import { groupOrderSlots } from "@/components/order/slot-groups";
import { API_CODES, ApiError } from "@/lib/api-client";
import { useGenders, useStartCheckout } from "@/lib/queries";
import type { GenderOption, TicketOrderDetail } from "@/lib/types";
import { Badge } from "../ui/badge";

/**
 * The registration phase of the order page (spec 008 US5, Figma 244-7184):
 * buyer contact plus one visitor card per slot GROUP, all submitted in the
 * single Continue to Payment call (Option B — POST /ticket/checkout/:order_id).
 * Nothing is persisted before that call, which is why a revisit always shows
 * empty forms.
 *
 * Spec 010: a bundle unit's slots collapse into ONE card titled with the
 * bundle's name; on submit the card's values fan out to one payload entry per
 * slot, so every ticket in the unit stores the same visitor and the wire
 * contract stays per-slot.
 */

const dobSchema = z
  .string()
  .regex(/^\d{4}-\d{2}-\d{2}$/, "Date of birth is required.")
  .refine((value) => Date.parse(value) <= Date.now(), {
    message: "Date of birth cannot be in the future.",
  });

// The gender option set comes from GET /ticket/genders (master data), so the
// schema only requires a choice; the server checks membership.
const genderSchema = z.string().min(1, "Select a gender.");

const visitorSchema = z.object({
  /** The slot ids this card fills (a whole bundle unit, or one standalone slot). */
  slot_ids: z.array(z.string()).min(1),
  name: z.string().trim().min(1, "Full name is required."),
  email: z.email("Enter a valid email address."),
  phone: z.string().trim().min(6, "Enter a valid phone number."),
  dob: dobSchema,
  gender: genderSchema,
});

const formsSchema = z.object({
  buyer_name: z.string().trim().min(1, "Buyer name is required."),
  buyer_email: z.string().trim().email("Enter a valid email address."),
  buyer_phone: z.string().trim().min(6, "Enter a valid phone number."),
  buyer_dob: dobSchema,
  buyer_gender: genderSchema,
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
    formState: { errors },
  } = useForm<OrderFormsValues>({
    resolver: zodResolver(formsSchema),
    // Option B: always empty — the server holds nothing to prefill from.
    defaultValues: {
      buyer_name: "",
      buyer_email: "",
      buyer_phone: "",
      buyer_dob: "",
      buyer_gender: "",
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
          dob: entry.dob,
          gender: entry.gender,
        };
      }),
    );

    try {
      await checkout.mutateAsync({
        buyer_name: values.buyer_name,
        buyer_email: values.buyer_email,
        buyer_phone: values.buyer_phone,
        buyer_dob: values.buyer_dob,
        buyer_gender: values.buyer_gender,
        attendees,
      });
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
        for (const [field, message] of Object.entries(error.data as Record<string, string>)) {
          // Server paths index the fanned-out payload (attendees[3].dob) —
          // remap onto the visible card, then rewrite to RHF's attendees.0.dob.
          const rhfPath = field
            .replace(/^attendees\[(\d+)\]/, (match, index) => {
              const groupIndex = payloadGroupIndex[Number(index)];
              return groupIndex === undefined ? match : `attendees[${groupIndex}]`;
            })
            .replace(/\[(\d+)\]/g, ".$1") as keyof OrderFormsValues;
          setError(rhfPath, { type: "server", message });
        }
      }
    }
  });

  const blockedError =
    checkout.error instanceof ApiError && checkout.error.code !== API_CODES.validation
      ? checkout.error
      : null;

  return (
    <form onSubmit={onSubmit} aria-label="Visitor registration" className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_340px] lg:items-start">
      <div className="space-y-6">
        {blockedError ? (
          <StatusAlert>{checkoutErrorMessage(blockedError)}</StatusAlert>
        ) : null}

        <Card>
          <CardHeader className="flex flex-wrap items-center justify-between gap-2">
            <CardTitle>Buyer contact</CardTitle>
            <Badge className=" bg-sky-50 px-2.5 py-1 text-xs font-medium text-sky-700">
              <Info aria-hidden className="size-3.5" />
              The invoice and e-ticket will be sent via email
            </Badge>
          </CardHeader>
          <CardContent className="grid gap-4 sm:grid-cols-2">
            {/* Same field set as a visitor card (Figma 12-4456): name spans the
                row, then email + phone, then gender + date of birth. */}
            <div className="sm:col-span-2">
              <Field label="Full name" error={errors.buyer_name?.message}>
                <Input placeholder="As written on ID card" {...register("buyer_name")} />
              </Field>
            </div>
            <Field label="Email address" error={errors.buyer_email?.message}>
              <Input placeholder="name@example.com" {...register("buyer_email")} />
            </Field>
            <Field label="Phone number" error={errors.buyer_phone?.message}>
              <Input placeholder="+62812XXXXXXX" {...register("buyer_phone")} />
            </Field>
            <Field label="Gender" error={errors.buyer_gender?.message}>
              <GenderSelect control={control} name="buyer_gender" options={genders} />
            </Field>
            <Field label="Date of birth" error={errors.buyer_dob?.message}>
              <Input type="date" {...register("buyer_dob")} />
            </Field>
          </CardContent>
        </Card>

        {groups.map((group, index) => (
          <Card key={group.key}>
            <CardHeader>
              <CardTitle className="text-lg font-bold">
                {/* Bundle cards carry the bundle's title, never a constituent
                    ticket's (spec 010 FR-005); the badge says how many passes
                    this one form registers. */}
                {group.title}
                {group.unitLabel !== null ? (
                  <span className="ml-2 text-sm font-medium text-muted-foreground">
                    {group.unitLabel}
                  </span>
                ) : null}
                {group.isBundle ? (
                  <span className="ml-2 rounded bg-brand-surface px-2 py-0.5 text-xs font-medium text-brand">
                    {group.ticketCount} {group.ticketCount === 1 ? "ticket" : "tickets"}
                  </span>
                ) : group.packageBadge !== null ? (
                  <span className="ml-2 rounded bg-brand-surface px-2 py-0.5 text-xs font-medium text-brand">
                    {group.packageBadge}
                  </span>
                ) : null}
              </CardTitle>
            </CardHeader>
            <CardContent className="grid gap-4 sm:grid-cols-2">
              {/* slot_ids ride in form state from defaultValues — no hidden
                  input; the fan-out on submit reads them per card. */}
              <div className="sm:col-span-2">
                <Field label="Full name" error={errors.attendees?.[index]?.name?.message}>
                  <Input placeholder="As written on ID card" {...register(`attendees.${index}.name`)} />
                </Field>
              </div>
              <Field label="Email address" error={errors.attendees?.[index]?.email?.message}>
                <Input placeholder="name@example.com" {...register(`attendees.${index}.email`)} />
              </Field>
              <Field label="Phone number" error={errors.attendees?.[index]?.phone?.message}>
                <Input placeholder="+62812XXXXXXX" {...register(`attendees.${index}.phone`)} />
              </Field>
              <Field label="Gender" error={errors.attendees?.[index]?.gender?.message}>
                <GenderSelect
                  control={control}
                  name={`attendees.${index}.gender`}
                  options={genders}
                />
              </Field>
              <Field label="Date of birth" error={errors.attendees?.[index]?.dob?.message}>
                <Input type="date" {...register(`attendees.${index}.dob`)} />
              </Field>
            </CardContent>
          </Card>
        ))}
      </div>

      <OrderSummary order={order} submitting={checkout.isPending} />
    </form>
  );
}

function OrderSummary({
  order,
  submitting,
}: {
  order: TicketOrderDetail;
  submitting: boolean;
}) {
  // No order-hold countdown on this step (Figma 12-4456, clarified
  // 2026-08-05): the shared header's event countdown is the only timer here.
  // The hold still expires server-side.
  return (
    <aside className="lg:sticky lg:top-6">
      <OrderSummaryPanel
        order={order}
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
                  className="flex h-8 w-10 shrink-0 items-center justify-center rounded bg-background text-[9px] font-black tracking-tight"
                >
                  QRIS
                </span>
                <span>
                  <span className="block font-semibold">QRIS</span>
                  <span className="block text-xs text-muted-foreground">Scan to pay</span>
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
          <Button size="lg" type="submit" disabled={submitting} className="w-full bg-brand hover:bg-brand/90 text-brand-foreground transition-all">
            {submitting ? "Starting payment…" : "Continue to Payment"}
          </Button>
        }
      />
    </aside>
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
          <SelectTrigger aria-label="Gender" className="h-9 w-full">
            {/* The trigger shows the human label, never the stored value. */}
            <SelectValue>
              {(value: string | null) => (value === null ? "Select" : genderLabel(value))}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {(options ?? []).map((option) => (
              <SelectItem key={option.id} value={option.name}>
                {genderLabel(option.name)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    />
  );
}

/** "FEMALE" → "Female": the master list stores the canonical uppercase value. */
function genderLabel(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1).toLowerCase();
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

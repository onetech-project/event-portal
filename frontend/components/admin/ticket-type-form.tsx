"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Field } from "@/components/ui/field";
import { StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import { toApiDateTime, toDateTimeLocal } from "@/lib/format";
import { useCreateTicketType, useUpdateTicketType } from "@/lib/queries";
import { ticketTypeFormSchema, type TicketTypeForm as FormValues } from "@/lib/schemas";
import type { TicketTypeAdminView } from "@/lib/types";

/**
 * Create/edit form for a ticket type.
 *
 * The quota field is the REMAINING quota — the same live counter guest checkout
 * decrements and cancellations restore. It is labelled and explained as such, and
 * the value submitted replaces the remainder outright; the server never
 * re-subtracts past sales from it (constitution, Critical Data Flow Rules).
 *
 * `sold` is derived and read-only, shown for context only.
 */
export function TicketTypeForm({
  eventId,
  ticketType,
  onDone,
}: {
  eventId: string;
  ticketType?: TicketTypeAdminView;
  onDone: () => void;
}) {
  const isEditing = ticketType !== undefined;
  const create = useCreateTicketType(eventId);
  const update = useUpdateTicketType(eventId);
  const mutation = isEditing ? update : create;

  const {
    register,
    control,
    handleSubmit,
    reset,
    setValue,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(ticketTypeFormSchema),
    defaultValues: {
      name: ticketType?.name ?? "",
      description: ticketType?.description ?? "",
      price: ticketType?.price ?? "",
      quota: ticketType?.quota ?? 0,
      salesStart: toDateTimeLocal(ticketType?.sales_start),
      salesEnd: toDateTimeLocal(ticketType?.sales_end),
      eventStart: toDateTimeLocal(ticketType?.event_start),
      eventEnd: toDateTimeLocal(ticketType?.event_end),
      // `?? true` is load-bearing. A new type must default to on sale, and an
      // existing one must round-trip whatever it already is — a fallback of
      // false would take every edited ticket off sale silently.
      isVisible: ticketType?.is_visible ?? true,
    },
  });

  // Spec 022 FR-004a/FR-004b. The price follows the on-sale decision.
  //
  // Watched rather than read from the checkbox's own handler because the field is
  // also set by `reset` and by defaultValues; a handler-only rule would leave the
  // price editable on an invitation type loaded straight from the server.
  // useWatch, not watch(): the latter returns a fresh function the React Compiler
  // cannot memoize, so it opts this whole component out of compilation — the same
  // trap package-form.tsx already documents.
  const isVisible = useWatch({ control, name: "isVisible" });

  // Whether the price field was emptied by putting this ticket back on sale, as
  // opposed to simply being blank on a new form. Only that case gets the
  // explanation; telling someone creating a fresh ticket type that their price
  // "was cleared" would be false.
  const [priceClearedByToggle, setPriceClearedByToggle] = useState(false);

  /**
   * Turning the ticket off sale clears its price; turning it back on empties the
   * field so a real one must be typed.
   *
   * Emptying — rather than restoring the old price — is the point. The old price
   * is already gone from the database by then, so prefilling anything here would
   * show a number the server does not have. An empty required field is the only
   * honest state, and the schema refuses to save until it is filled.
   */
  function handleOnSaleChange(next: boolean) {
    setValue("isVisible", next, { shouldValidate: false });
    setValue("price", next ? "" : "0", { shouldValidate: false, shouldDirty: true });
    setPriceClearedByToggle(next);
  }

  const onSubmit = handleSubmit(async (values) => {
    const body = {
      // event_id is immutable on update and ignored by the server there.
      event_id: eventId,
      name: values.name,
      // Sent even when blank so clearing the field clears the stored remark; the
      // server normalises whitespace-only input back to null.
      description: values.description ?? "",
      price: values.price,
      quota: values.quota,
      sales_start: toApiDateTime(values.salesStart),
      sales_end: toApiDateTime(values.salesEnd),
      event_start: toApiDateTime(values.eventStart),
      event_end: toApiDateTime(values.eventEnd),
      // Sent on EVERY write. Update is an absolute full replace on the server,
      // so a body that omits this leaves the decision to the server's default
      // rather than to the admin looking at the form.
      is_visible: values.isVisible,
    };

    if (isEditing) {
      await update.mutateAsync({ id: ticketType.id, body });
    } else {
      await create.mutateAsync(body);
      reset();
    }
    onDone();
  });

  const error = mutation.error instanceof ApiError ? mutation.error : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{isEditing ? `Edit ${ticketType.name}` : "New ticket type"}</CardTitle>
      </CardHeader>

      <CardContent className="space-y-4">
        {error ? <StatusAlert>{error.message}</StatusAlert> : null}

        <form
          onSubmit={onSubmit}
          aria-label={isEditing ? "Edit ticket type" : "New ticket type"}
          className="grid gap-4 sm:grid-cols-2"
        >
          <Field label="Name" error={errors.name?.message}>
            <Input {...register("name")} />
          </Field>

          <Field
            label="Price (IDR)"
            error={errors.price?.message}
            hint={
              isVisible
                ? priceClearedByToggle
                  ? // FR-004b: an empty required field with no explanation is the
                    // thing this exists to avoid. Zero is still a legal price for
                    // a ticket on sale (FR-003 permits a genuine giveaway) — what
                    // is refused is inheriting one nobody typed.
                    "Cleared when this ticket was made invitation-only. Enter the price it should sell for — 0 is allowed, but it has to be deliberate."
                  : undefined
                : "Not charged. An invitation ticket is free whatever is stored, so the price is held at 0 while it is off sale."
            }
          >
            <Input
              inputMode="decimal"
              disabled={!isVisible}
              {...register("price")}
            />
          </Field>

          <Field
            label="Remark"
            error={errors.description?.message}
            hint="Shown on the booking card instead of the standard non-refundable notice. Leave blank to keep that wording."
          >
            <Textarea rows={2} {...register("description")} />
          </Field>

          <div />

          <Field
            label="Sisa Kuota / Remaining Quota"
            error={errors.quota?.message}
            hint={
              <>
                {isEditing ? `${ticketType.sold} sold so far. ` : null}
                This value is set absolutely — it replaces the remaining quota and is
                not reduced by past sales. Avoid editing it during active sales.
              </>
            }
          >
            <Input type="number" min={0} {...register("quota", { valueAsNumber: true })} />
          </Field>

          <div />

          <Field label="Sales start" error={errors.salesStart?.message}>
            <Input type="datetime-local" {...register("salesStart")} />
          </Field>

          <Field label="Sales end" error={errors.salesEnd?.message}>
            <Input type="datetime-local" {...register("salesEnd")} />
          </Field>

          <Field
            label="Event start"
            error={errors.eventStart?.message}
            hint="When this ticket ADMITS its holder — the moment the gate opens for it, not showtime. Validation refuses a ticket presented even a moment earlier. Separate from the sales window above."
          >
            <Input type="datetime-local" {...register("eventStart")} />
          </Field>

          <Field
            label="Event end"
            error={errors.eventEnd?.message}
            hint="The last moment this ticket admits its holder. Must fall inside the event's own dates."
          >
            <Input type="datetime-local" {...register("eventEnd")} />
          </Field>

          {/*
            FR-001a: the control is named for what it DOES, not for the column.
            "Visible" alone would read as a listing preference, and an admin who
            unticked it to tidy a list would be publishing a free ticket.
          */}
          <div className="sm:col-span-2">
            <Controller
              control={control}
              name="isVisible"
              render={({ field }) => (
                <label className="flex items-start gap-3 rounded-lg border p-3">
                  <Checkbox
                    checked={field.value}
                    onCheckedChange={(next) => handleOnSaleChange(Boolean(next))}
                    aria-label="Sell this ticket type"
                  />
                  <span className="text-sm">
                    <span className="font-medium">Sell this ticket type</span>
                    <span className="mt-1 block text-xs text-muted-foreground">
                      On by default. Untick to make it an{" "}
                      <strong>invitation-only</strong> ticket: it disappears from the
                      event page, cannot be bought or bundled into a package, and is
                      obtained only through its registration link — free of charge,
                      whatever amount is set above.
                    </span>
                  </span>
                </label>
              )}
            />
          </div>

          <div className="flex gap-2 sm:col-span-2">
            <Button type="submit" disabled={mutation.isPending}>
              {isEditing ? "Save changes" : "Create ticket type"}
            </Button>
            <Button type="button" variant="outline" onClick={onDone}>
              Cancel
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

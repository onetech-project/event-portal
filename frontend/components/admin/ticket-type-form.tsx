"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useForm } from "react-hook-form";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
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
    handleSubmit,
    reset,
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
    },
  });

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

          <Field label="Price (IDR)" error={errors.price?.message}>
            <Input inputMode="decimal" {...register("price")} />
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

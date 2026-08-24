"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Controller, useForm, useWatch } from "react-hook-form";

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
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import { toApiDateTime, toDateTimeLocal } from "@/lib/format";
import { useCreatePackage, useUpdatePackage } from "@/lib/queries";
import { packageFormSchema, type PackageForm as FormValues } from "@/lib/schemas";
import type { PackageAdminView, TicketTypeAdminView } from "@/lib/types";

/**
 * Create/edit form for a package (bundle).
 *
 * A bundle grants exactly one of each selected ticket type per unit, so the
 * composition picker is a plain multiselect — there is no per-type quantity to
 * enter. There is also no quota input, deliberately: a package owns no
 * inventory, and the server rejects any quota-like field outright rather than
 * ignoring it (FR-036). What a bundle can sell is derived from its
 * constituents' remaining quota, and the derived line below the picker shows
 * that floor (the minimum remaining quota across the selection).
 *
 * The picker lists every ticket type of THIS event only — a bundle cannot span
 * events, which the composite foreign key enforces in the database too.
 */
export function PackageForm({
  eventId,
  ticketTypes,
  pkg,
  onDone,
}: Readonly<{
  eventId: string;
  ticketTypes: TicketTypeAdminView[];
  pkg?: PackageAdminView;
  onDone: () => void;
}>) {
  const isEditing = pkg !== undefined;
  const create = useCreatePackage(eventId);
  const update = useUpdatePackage(eventId);
  const mutation = isEditing ? update : create;

  const {
    register,
    control,
    handleSubmit,
    formState: { errors },
  } = useForm<FormValues>({
    resolver: zodResolver(packageFormSchema),
    defaultValues: {
      name: pkg?.name ?? "",
      description: pkg?.description ?? "",
      price: pkg?.price ?? "",
      salesStart: toDateTimeLocal(pkg?.sales_start),
      salesEnd: toDateTimeLocal(pkg?.sales_end),
      isActive: pkg?.is_active ?? true,
      components: (pkg?.components ?? []).map((component) => component.ticket_type_id),
    },
  });

  // Spec 022 FR-007: invitation-only types are not bundleable, so they never
  // reach the picker. The server refuses them anyway — this is the affordance,
  // not the enforcement — but offering a choice that will be rejected on submit
  // is how an admin ends up thinking the refusal is a bug.
  //
  // Deliberately NOT applied to `selectedTypes` below: an existing package that
  // somehow contains one must still show it, or the quota maths silently omits a
  // constituent and the bundle claims more availability than it has.
  const bundleable = ticketTypes.filter((ticketType) => ticketType.is_visible);

  // useWatch rather than watch(): the latter returns a fresh function the React
  // Compiler cannot memoize, so it opts the whole component out of compilation.
  const selectedIds = useWatch({ control, name: "components" }) ?? [];

  const selectedTypes = ticketTypes.filter((ticketType) =>
    selectedIds.includes(ticketType.id),
  );
  const minRemainingQuota =
    selectedTypes.length > 0
      ? Math.min(...selectedTypes.map((ticketType) => ticketType.quota))
      : null;

  const onSubmit = handleSubmit(async (values) => {
    const body = {
      // event_id is immutable on update and ignored by the server there.
      event_id: eventId,
      name: values.name,
      // Sent even when blank so clearing the field clears the stored remark.
      description: values.description ?? "",
      price: values.price,
      sales_start: toApiDateTime(values.salesStart),
      sales_end: toApiDateTime(values.salesEnd),
      is_active: values.isActive,
      // A bundle always grants exactly one of each selected ticket type.
      components: values.components.map((ticketTypeId) => ({
        ticket_type_id: ticketTypeId,
        quantity_per_unit: 1,
      })),
    };

    if (isEditing) {
      await update.mutateAsync({ id: pkg.id, body });
    } else {
      await create.mutateAsync(body);
    }
    onDone();
  });

  const error = mutation.error instanceof ApiError ? mutation.error : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{isEditing ? `Edit ${pkg.name}` : "New package"}</CardTitle>
      </CardHeader>

      <CardContent className="space-y-4">
        {error ? <StatusAlert>{error.message}</StatusAlert> : null}

        <form
          onSubmit={onSubmit}
          aria-label={isEditing ? "Edit package" : "New package"}
          className="grid gap-4 sm:grid-cols-2"
        >
          <Field label="Name" error={errors.name?.message}>
            <Input {...register("name")} />
          </Field>

          <Field
            label="Price (IDR)"
            error={errors.price?.message}
            hint="The all-in bundle price. It is not checked against the sum of its constituents — a bundle is normally cheaper."
          >
            <Input inputMode="decimal" {...register("price")} />
          </Field>

          <Field
            label="Remark"
            error={errors.description?.message}
            hint="Shown on the booking card instead of the standard non-refundable notice. Leave blank to keep that wording."
          >
            <Textarea rows={2} {...register("description")} />
          </Field>

          <Field label="Active" error={errors.isActive?.message}>
            {/* A plain checkbox: the field is a boolean now, so the two-option
                select it replaced had nothing left to choose between. */}
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                {...register("isActive")}
                className="size-4 rounded border-input accent-primary"
              />
              Available for purchase
            </label>
          </Field>

          <Field label="Sales start" error={errors.salesStart?.message}>
            <Input type="datetime-local" {...register("salesStart")} />
          </Field>

          <Field label="Sales end" error={errors.salesEnd?.message}>
            <Input type="datetime-local" {...register("salesEnd")} />
          </Field>

          <fieldset className="sm:col-span-2">
            <legend className="text-sm font-medium">What this bundle contains</legend>
            <p className="mt-1 text-xs text-muted-foreground">
              A bundle grants exactly one of each selected ticket type. It has no
              quota of its own — it can be sold as long as every constituent below
              still has enough remaining quota.
            </p>

            {bundleable.length === 0 ? (
              <p className="mt-3 text-sm text-muted-foreground">
                This event has no ticket types yet. Add one before creating a bundle.
              </p>
            ) : (
              <div className="mt-3">
                <Controller
                  control={control}
                  name="components"
                  render={({ field }) => (
                    <Select
                      multiple
                      value={field.value}
                      onValueChange={field.onChange}
                      items={Object.fromEntries(
                        bundleable.map((ticketType) => [ticketType.id, ticketType.name]),
                      )}
                    >
                      <SelectTrigger aria-label="Ticket types" className="w-full">
                        <SelectValue placeholder="Select ticket types..." />
                      </SelectTrigger>
                      <SelectContent align="start">
                        {bundleable.map((ticketType) => (
                          <SelectItem key={ticketType.id} value={ticketType.id}>
                            {ticketType.name}
                            <span className="ml-auto text-xs text-muted-foreground">
                              {ticketType.quota} left
                            </span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                />
                {errors.components ? (
                  <p role="alert" className="mt-2 text-xs text-destructive">
                    {errors.components.message}
                  </p>
                ) : null}
              </div>
            )}

            {minRemainingQuota !== null ? (
              <p className="mt-3 text-xs text-muted-foreground">
                This bundle can be sold up to {minRemainingQuota} more unit(s),
                limited by the selected ticket type with the least remaining quota.
              </p>
            ) : null}
          </fieldset>

          <div className="flex gap-2 sm:col-span-2">
            <Button type="submit" disabled={mutation.isPending || ticketTypes.length === 0}>
              {isEditing ? "Save changes" : "Create package"}
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

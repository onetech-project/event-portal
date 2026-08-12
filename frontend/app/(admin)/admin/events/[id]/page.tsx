"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useRouter } from "next/navigation";
import { use, useState } from "react";
import { Controller, useForm } from "react-hook-form";

import { EventStatusField } from "@/components/admin/event-status-field";
import { PackageForm } from "@/components/admin/package-form";
import { TicketTypeForm } from "@/components/admin/ticket-type-form";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { EmptyState, Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { EventContentManager } from "@/components/admin/content-block-form";
import { RichTextEditor } from "@/components/admin/rich-text-editor";
import { ApiError } from "@/lib/api-client";
import { strandedTicketTypes } from "@/lib/event-window";
import { formatCurrency, formatDateTime, toApiDateTime, toDateTimeLocal } from "@/lib/format";
import {
  useAdminEvent,
  useAdminPackages,
  useDeleteEvent,
  useDeletePackage,
  useDeleteTicketType,
  useUpdateEvent,
} from "@/lib/queries";
import { eventFormSchema, type EventForm } from "@/lib/schemas";
import type { EventAdminDetail, PackageAdminView, TicketTypeAdminView } from "@/lib/types";

export default function AdminEventDetailPage({
  params,
}: Readonly<{ params: Promise<{ id: string }> }>) {
  const { id } = use(params);
  const { data: event, isPending, error } = useAdminEvent(id);

  if (isPending) return <Loading />;
  if (error) {
    return (
      <main className="mx-auto w-full max-w-4xl flex-1 px-6 py-8">
        <StatusAlert>{error.message}</StatusAlert>
      </main>
    );
  }
  if (!event) return null;

  return (
    <main className="mx-auto w-full max-w-4xl flex-1 space-y-8 px-6 py-8">
      <PageHeading title={event.name} subtitle={`/${event.slug}`} />
      <EventEditor event={event} />
      {/* CMS content (spec 008 US4): T&C + the guest detail page's blocks. */}
      <EventContentManager eventId={event.id} />
      <TicketTypesSection event={event} />
      <PackagesSection event={event} />
      <DangerZone event={event} />
    </main>
  );
}

function EventEditor({ event }: Readonly<{ event: EventAdminDetail }>) {
  const update = useUpdateEvent(event.id);
  const [saved, setSaved] = useState(false);

  const {
    register,
    control,
    handleSubmit,
    formState: { errors },
  } = useForm<EventForm>({
    resolver: zodResolver(eventFormSchema),
    defaultValues: {
      name: event.name,
      slug: event.slug,
      description: event.description ?? "",
      venue: event.venue,
      address: event.address,
      startDate: toDateTimeLocal(event.start_date),
      endDate: toDateTimeLocal(event.end_date),
      bannerUrl: event.banner_url ?? "",
      scale: event.scale !== null ? String(event.scale) : "",
      status: event.status,
    },
  });

  const onSubmit = handleSubmit(async (values) => {
    await update.mutateAsync({
      name: values.name,
      slug: values.slug,
      description: values.description || null,
      venue: values.venue,
      address: values.address,
      start_date: toApiDateTime(values.startDate),
      end_date: toApiDateTime(values.endDate),
      banner_url: values.bannerUrl ? values.bannerUrl : null,
      scale: values.scale ? Number(values.scale) : null,
      status: values.status,
    });
    setSaved(true);
  });

  const error = update.error instanceof ApiError ? update.error : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Event details</CardTitle>
      </CardHeader>

      <CardContent className="space-y-4">
        {error ? <StatusAlert>{error.message}</StatusAlert> : null}
        {saved && !error ? <StatusAlert tone="success">Saved.</StatusAlert> : null}

        <form onSubmit={onSubmit} aria-label="Edit event" className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" error={errors.name?.message}>
            <Input {...register("name")} />
          </Field>
          <Field label="Slug" error={errors.slug?.message}>
            <Input {...register("slug")} />
          </Field>
          <Field label="Venue" error={errors.venue?.message}>
            <Input {...register("venue")} />
          </Field>
          <Field label="Address" error={errors.address?.message}>
            <Input {...register("address")} />
          </Field>
          <Field label="Starts" error={errors.startDate?.message}>
            <Input type="datetime-local" {...register("startDate")} />
          </Field>
          <Field label="Ends" error={errors.endDate?.message}>
            <Input type="datetime-local" {...register("endDate")} />
          </Field>
          <Field
            label="Banner URL"
            error={errors.bannerUrl?.message}
            hint="A link to an existing image — there is no upload here."
          >
            <Input {...register("bannerUrl")} />
          </Field>
          <Field
            label="Scale"
            error={errors.scale?.message}
            hint="Expected visitors — shown on the event page as e.g. “30.000+ Visitors”."
          >
            <Input inputMode="numeric" placeholder="30000" {...register("scale")} />
          </Field>
          <EventStatusField control={control} error={errors.status?.message} />
          <div className="sm:col-span-2">
            <Field label="Description" error={errors.description?.message}>
              {/* WYSIWYG (spec 008 US4): HTML value; the server sanitizes on write. */}
              <Controller
                control={control}
                name="description"
                render={({ field }) => (
                  <RichTextEditor
                    value={field.value ?? ""}
                    onChange={field.onChange}
                    ariaLabel="Event description editor"
                  />
                )}
              />
            </Field>
          </div>
          <div className="sm:col-span-2">
            <Button type="submit" disabled={update.isPending}>
              {update.isPending ? "Saving…" : "Save changes"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function TicketTypesSection({ event }: Readonly<{ event: EventAdminDetail }>) {
  const [editing, setEditing] = useState<TicketTypeAdminView | null>(null);
  const [creating, setCreating] = useState(false);
  const remove = useDeleteTicketType(event.id);

  const error = remove.error instanceof ApiError ? remove.error : null;
  const stranded = strandedTicketTypes(event, event.ticket_types);

  return (
    <section>
      <div className="mb-3 flex items-center justify-between">
        <h2 className="font-medium">Ticket types</h2>
        <Button
          variant="outline"
          onClick={() => {
            setEditing(null);
            setCreating((open) => !open);
          }}
        >
          {creating ? "Cancel" : "Add ticket type"}
        </Button>
      </div>

      {error ? (
        <div className="mb-4">
          <StatusAlert>{error.message}</StatusAlert>
        </div>
      ) : null}

      {/* Stranded admission windows (spec 015 FR-005b). Moving or shortening an
          event is deliberately NOT refused — refusing it would deadlock a
          reschedule, since the tickets cannot move ahead of the event either.
          So the save goes through and this names what to fix next. It persists
          until every window is back inside the event's dates. */}
      {stranded.length > 0 ? (
        <div className="mb-4">
          <StatusAlert tone="info">
            {stranded.length === 1 ? "This ticket type admits" : "These ticket types admit"}{" "}
            on days the event no longer runs (
            {formatDateTime(event.start_date)} — {formatDateTime(event.end_date)}):{" "}
            <strong>{stranded.map((t) => t.name).join(", ")}</strong>. Edit{" "}
            {stranded.length === 1 ? "it" : "each of them"} so the admission window falls
            inside the event.
          </StatusAlert>
        </div>
      ) : null}

      {creating ? (
        <div className="mb-4">
          <TicketTypeForm eventId={event.id} onDone={() => setCreating(false)} />
        </div>
      ) : null}

      {editing ? (
        <div className="mb-4">
          <TicketTypeForm
            eventId={event.id}
            ticketType={editing}
            onDone={() => setEditing(null)}
          />
        </div>
      ) : null}

      {event.ticket_types.length === 0 && !creating ? (
        <EmptyState>No ticket types yet — add one so this event can sell.</EmptyState>
      ) : null}

      <div className="space-y-3">
        {event.ticket_types.map((ticketType) => (
          <Card key={ticketType.id}>
            <CardContent className="flex flex-wrap items-center justify-between gap-4">
              <div>
                <p className="font-medium">{ticketType.name}</p>
                <p className="text-sm text-muted-foreground">
                  {formatCurrency(ticketType.price)}
                </p>
                {/* The remark the booking card shows in place of the standard
                    non-refundable notice; absent means that wording stands. */}
                <p className="mt-1 max-w-prose text-xs whitespace-pre-line text-muted-foreground">
                  {ticketType.description ?? (
                    <span className="italic">Standard non-refundable notice</span>
                  )}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Sisa Kuota / Remaining Quota: <strong>{ticketType.quota}</strong> ·{" "}
                  {ticketType.sold} sold
                </p>
                <p className="text-xs text-muted-foreground">
                  On sale {formatDateTime(ticketType.sales_start)} —{" "}
                  {formatDateTime(ticketType.sales_end)}
                </p>
                {/* The admission window, kept visually distinct from the sales
                    line above: confusing the two is the whole failure mode this
                    labelling guards against (spec 015 FR-007). */}
                <p className="text-xs text-muted-foreground">
                  Admits {formatDateTime(ticketType.event_start)} —{" "}
                  {formatDateTime(ticketType.event_end)}
                </p>
              </div>

              <div className="flex gap-2">
                <Button
                  variant="outline"
                  onClick={() => {
                    setCreating(false);
                    setEditing(ticketType);
                  }}
                >
                  Edit
                </Button>
                <Button
                  variant="destructive"
                  disabled={remove.isPending}
                  onClick={() => {
                    if (
                      window.confirm(
                        `Delete "${ticketType.name}"? This is blocked if it has already been ordered.`,
                      )
                    ) {
                      remove.mutate(ticketType.id);
                    }
                  }}
                >
                  Delete
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
}

/**
 * Packages (bundles) for this event.
 *
 * It lives on the event page rather than under its own top-level route because
 * the composition picker needs that event's ticket types in scope — a bundle can
 * only be built from them.
 *
 * Note there is no quota column here, and none in the form: availability is
 * derived from the constituents on every read, so it is shown as a read-only
 * "available" figure alongside the composition that produced it.
 */
function PackagesSection({ event }: Readonly<{ event: EventAdminDetail }>) {
  const { data: packages, isPending, error: loadError } = useAdminPackages(event.id);
  const [editing, setEditing] = useState<PackageAdminView | null>(null);
  const [creating, setCreating] = useState(false);
  const remove = useDeletePackage(event.id);

  const removeError = remove.error instanceof ApiError ? remove.error : null;

  return (
    <section>
      <div className="mb-3 flex items-center justify-between">
        <h2 className="font-medium">Packages</h2>
        <Button
          variant="outline"
          onClick={() => {
            setEditing(null);
            setCreating((open) => !open);
          }}
        >
          {creating ? "Cancel" : "Add package"}
        </Button>
      </div>

      {loadError ? (
        <div className="mb-4">
          <StatusAlert>{loadError.message}</StatusAlert>
        </div>
      ) : null}

      {removeError ? (
        <div className="mb-4">
          <StatusAlert>{removeError.message}</StatusAlert>
        </div>
      ) : null}

      {creating ? (
        <div className="mb-4">
          <PackageForm
            eventId={event.id}
            ticketTypes={event.ticket_types}
            onDone={() => setCreating(false)}
          />
        </div>
      ) : null}

      {editing ? (
        <div className="mb-4">
          <PackageForm
            eventId={event.id}
            ticketTypes={event.ticket_types}
            pkg={editing}
            onDone={() => setEditing(null)}
          />
        </div>
      ) : null}

      {isPending ? <Loading label="Loading packages…" /> : null}

      {packages?.length === 0 && !creating ? (
        <EmptyState>
          No packages yet — bundle existing ticket types to sell them as one item.
        </EmptyState>
      ) : null}

      <div className="space-y-3">
        {packages?.map((pkg) => (
          <Card key={pkg.id}>
            <CardContent className="flex flex-wrap items-center justify-between gap-4">
              <div>
                <p className="font-medium">{pkg.name}</p>
                <p className="text-sm text-muted-foreground">{formatCurrency(pkg.price)}</p>
                <p className="mt-1 max-w-prose text-xs whitespace-pre-line text-muted-foreground">
                  {pkg.description ?? (
                    <span className="italic">Standard non-refundable notice</span>
                  )}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  Contains{" "}
                  {pkg.components
                    .map(
                      (component) =>
                        `${component.quantity_per_unit}× ${component.ticket_type_name}`,
                    )
                    .join(" + ")}
                </p>
                {/* Derived from the constituents' remaining quota — a package
                    stores no inventory of its own. */}
                <p className="mt-1 text-xs text-muted-foreground">
                  Available: <strong>{pkg.available_units}</strong> bundle(s) · {pkg.sold}{" "}
                  sold · {pkg.is_active ? "Active" : "Inactive"}
                </p>
                <p className="text-xs text-muted-foreground">
                  On sale {formatDateTime(pkg.sales_start)} — {formatDateTime(pkg.sales_end)}
                </p>
              </div>

              <div className="flex gap-2">
                <Button
                  variant="outline"
                  onClick={() => {
                    setCreating(false);
                    setEditing(pkg);
                  }}
                >
                  Edit
                </Button>
                <Button
                  variant="destructive"
                  disabled={remove.isPending}
                  onClick={() => {
                    if (
                      window.confirm(
                        `Delete "${pkg.name}"? This is blocked if it has already been ordered.`,
                      )
                    ) {
                      remove.mutate(pkg.id);
                    }
                  }}
                >
                  Delete
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </section>
  );
}

function DangerZone({ event }: Readonly<{ event: EventAdminDetail }>) {
  const router = useRouter();
  const remove = useDeleteEvent();
  const error = remove.error instanceof ApiError ? remove.error : null;

  return (
    <Card className="ring-destructive/30">
      <CardHeader>
        <CardTitle className="text-destructive">Delete this event</CardTitle>
      </CardHeader>

      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">
          Deleting the event also deletes all {event.ticket_types.length} of its ticket
          types. It is rejected if any of them has already been ordered.
        </p>

        {error ? <StatusAlert>{error.message}</StatusAlert> : null}

        <Button
          variant="destructive"
          disabled={remove.isPending}
          onClick={() => {
            if (
              window.confirm(
                `Delete "${event.name}" and its ${event.ticket_types.length} ticket type(s)?`,
              )
            ) {
              remove.mutate(event.id, {
                onSuccess: () => router.push("/admin/events"),
              });
            }
          }}
        >
          {remove.isPending ? "Deleting…" : "Delete event"}
        </Button>
      </CardContent>
    </Card>
  );
}

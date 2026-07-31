"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";
import { useState } from "react";
import { useForm } from "react-hook-form";

import { EventStatusField } from "@/components/admin/event-status-field";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { EmptyState, Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { ApiError } from "@/lib/api-client";
import { formatDateTime, toApiDateTime } from "@/lib/format";
import { useAdminEvents, useCreateEvent } from "@/lib/queries";
import { eventFormSchema, type EventForm } from "@/lib/schemas";

export default function AdminEventsPage() {
  const { data: events, isPending, error } = useAdminEvents();
  const [showForm, setShowForm] = useState(false);

  return (
    <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-8">
      <div className="flex items-center justify-between">
        <PageHeading title="Events" subtitle="All events, in every status." />
        <Button onClick={() => setShowForm((open) => !open)} variant="outline">
          {showForm ? "Cancel" : "New event"}
        </Button>
      </div>

      {showForm ? (
        <div className="mb-8">
          <CreateEventCard onCreated={() => setShowForm(false)} />
        </div>
      ) : null}

      {isPending ? <Loading /> : null}
      {error ? <StatusAlert>{error.message}</StatusAlert> : null}
      {events?.length === 0 ? (
        <EmptyState>No events yet. Create one to start selling.</EmptyState>
      ) : null}

      <div className="space-y-3">
        {events?.map((event) => (
          <Card key={event.id}>
            <CardContent className="flex flex-wrap items-center justify-between gap-4">
              <div>
                <p className="font-medium">{event.name}</p>
                <p className="text-sm text-muted-foreground">
                  /{event.slug} · {event.venue}
                </p>
                <p className="mt-1 text-xs text-muted-foreground">
                  {formatDateTime(event.start_date)} — {formatDateTime(event.end_date)}
                </p>
              </div>

              <div className="flex items-center gap-3">
                <Badge variant="secondary">{event.status}</Badge>
                <Button asChild variant="outline">
                  <Link href={`/admin/events/${event.id}`}>Manage</Link>
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </main>
  );
}

function CreateEventCard({ onCreated }: Readonly<{ onCreated: () => void }>) {
  const create = useCreateEvent();

  const {
    register,
    control,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<EventForm>({
    resolver: zodResolver(eventFormSchema),
    defaultValues: {
      name: "",
      slug: "",
      description: "",
      venue: "",
      address: "",
      startDate: "",
      endDate: "",
      bannerUrl: "",
      status: "DRAFT",
    },
  });

  const onSubmit = handleSubmit(async (values) => {
    await create.mutateAsync({
      name: values.name,
      slug: values.slug,
      description: values.description || null,
      venue: values.venue,
      address: values.address,
      start_date: toApiDateTime(values.startDate),
      end_date: toApiDateTime(values.endDate),
      banner_url: values.bannerUrl ? values.bannerUrl : null,
      status: values.status,
    });
    reset();
    onCreated();
  });

  const error = create.error instanceof ApiError ? create.error : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>New event</CardTitle>
      </CardHeader>

      <CardContent className="space-y-4">
        {error ? <StatusAlert>{error.message}</StatusAlert> : null}

        <form onSubmit={onSubmit} aria-label="New event" className="grid gap-4 sm:grid-cols-2">
          <Field label="Name" error={errors.name?.message}>
            <Input {...register("name")} />
          </Field>
          <Field label="Slug" error={errors.slug?.message} hint="Used in the public URL.">
            <Input {...register("slug")} placeholder="jazz-night-2026" />
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
            <Input {...register("bannerUrl")} placeholder="https://…" />
          </Field>
          <EventStatusField control={control} error={errors.status?.message} />
          <div className="sm:col-span-2">
            <Field label="Description" error={errors.description?.message}>
              <Textarea {...register("description")} />
            </Field>
          </div>
          <div className="sm:col-span-2">
            <Button type="submit" disabled={create.isPending}>
              {create.isPending ? "Creating…" : "Create event"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

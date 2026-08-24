"use client";

import { useEffect } from "react";

import { Card, CardContent } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { EmptyState, Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Pagination } from "@/components/ui/pagination";
import { useAdminAttendees, useAdminEventOptions } from "@/lib/queries";
import { useListParams } from "@/lib/use-list-params";

const ALL = "__all__";

export default function AdminAttendeesPage() {
  // Page, size and the event filter all live in the URL, so a shared address
  // reopens this exact view rather than page N of some other result set.
  const list = useListParams();
  const eventId = list.filters.event_id ?? ALL;
  const search = list.filters.search ?? "";

  const { data: events } = useAdminEventOptions();
  const {
    data: attendees,
    isPending,
    error,
  } = useAdminAttendees(
    undefined,
    eventId === ALL ? undefined : eventId,
    search || undefined,
    list.params,
  );

  // Adopt the page the server actually served, so an out-of-range bookmark
  // corrects itself instead of displaying a position the rows did not come from.
  const servedPage = attendees?.page;
  const { syncServedPage } = list;
  useEffect(() => {
    if (servedPage !== undefined) syncServedPage(servedPage);
  }, [servedPage, syncServedPage]);

  return (
    <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-8">
      <PageHeading
        title="Attendees"
        subtitle="Everyone holding a ticket — bought or registered for."
      />

      <Card className="mb-6">
        <CardContent>
          <Field label="Event">
            <Select
              value={eventId}
              onValueChange={(value) =>
                list.setFilter("event_id", !value || value === ALL ? undefined : value)
              }
            >
              <SelectTrigger aria-label="Event">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>All events</SelectItem>
                {events?.map((event) => (
                  <SelectItem key={event.id} value={event.id}>
                    {event.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          {/*
            Spec 022 FR-053. This is a RECOVERY route, not a convenience. A free
            registrant is never shown the address their e-ticket went to, so when
            one calls to say nothing arrived, an operator has a name and at best a
            guess at the address — a lookup demanding the exact address would
            leave them nothing to do.
          */}
          <Field
            label="Find someone"
            hint="Part of a name, an email address, or an order number. Use this when a guest says their ticket never arrived."
          >
            <Input
              type="search"
              placeholder="e.g. Halo, or example.com"
              defaultValue={search}
              onChange={(e) => {
                const next = e.currentTarget.value.trim();
                list.setFilter("search", next === "" ? undefined : next);
              }}
            />
          </Field>
        </CardContent>
      </Card>

      {isPending ? <Loading /> : null}
      {error ? <StatusAlert>{error.message}</StatusAlert> : null}
      {attendees?.items.length === 0 ? <EmptyState>No attendees match.</EmptyState> : null}

      {attendees && attendees.items.length > 0 ? (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Email</TableHead>
                <TableHead>Ticket type</TableHead>
                <TableHead>Order</TableHead>
                <TableHead>Origin</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {attendees.items.map((attendee) => (
                <TableRow key={`${attendee.order_number}-${attendee.email}-${attendee.name}`}>
                  <TableCell>{attendee.name}</TableCell>
                  <TableCell className="text-muted-foreground">{attendee.email}</TableCell>
                  <TableCell>{attendee.ticket_type_name}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {attendee.order_number}
                  </TableCell>
                  {/* Legible before a resend: a registration has no receipt. */}
                  <TableCell className="text-xs">
                    {attendee.is_registration ? "Registered" : "Purchased"}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : null}

      {attendees ? (
        <Pagination
          page={attendees.page}
          pageSize={attendees.page_size}
          total={attendees.total}
          totalPages={attendees.total_pages}
          onPageChange={list.setPage}
          onPageSizeChange={list.setPageSize}
        />
      ) : null}
    </main>
  );
}

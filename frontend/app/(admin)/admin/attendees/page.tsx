"use client";

import { useEffect } from "react";

import { Card, CardContent } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
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

  const { data: events } = useAdminEventOptions();
  const {
    data: attendees,
    isPending,
    error,
  } = useAdminAttendees(undefined, eventId === ALL ? undefined : eventId, list.params);

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
        subtitle="Everyone a ticket has been purchased for."
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

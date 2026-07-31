"use client";

import { useState } from "react";

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
import { useAdminAttendees, useAdminEvents } from "@/lib/queries";

const ALL = "__all__";

export default function AdminAttendeesPage() {
  const [eventId, setEventId] = useState(ALL);

  const { data: events } = useAdminEvents();
  const {
    data: attendees,
    isPending,
    error,
  } = useAdminAttendees(undefined, eventId === ALL ? undefined : eventId);

  return (
    <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-8">
      <PageHeading
        title="Attendees"
        subtitle="Everyone a ticket has been purchased for."
      />

      <Card className="mb-6">
        <CardContent>
          <Field label="Event">
            <Select value={eventId} onValueChange={setEventId}>
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
      {attendees?.length === 0 ? <EmptyState>No attendees match.</EmptyState> : null}

      {attendees && attendees.length > 0 ? (
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
              {attendees.map((attendee) => (
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
    </main>
  );
}

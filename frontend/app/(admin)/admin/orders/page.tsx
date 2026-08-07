"use client";

import { useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { ApiError } from "@/lib/api-client";
import { formatCurrency, formatDateTime } from "@/lib/format";
import { useAdminEvents, useAdminOrders, useResendTicketEmail } from "@/lib/queries";

const STATUSES = ["PENDING", "PAID", "CANCELLED", "EXPIRED"] as const;
const ALL = "__all__";

export default function AdminOrdersPage() {
  const [status, setStatus] = useState(ALL);
  const [eventId, setEventId] = useState(ALL);

  const { data: events } = useAdminEvents();
  const {
    data: orders,
    isPending,
    error,
  } = useAdminOrders(
    status === ALL ? undefined : status,
    eventId === ALL ? undefined : eventId,
  );

  const resend = useResendTicketEmail();
  const resendError = resend.error instanceof ApiError ? resend.error : null;

  return (
    <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-8">
      <PageHeading title="Orders" subtitle="Read-only view of every order placed." />

      <Card className="mb-6">
        <CardContent className="grid gap-4 sm:grid-cols-2">
          <Field label="Status">
            <Select
              value={status}
              onValueChange={(value) => setStatus(value ?? ALL)}
            >
              <SelectTrigger aria-label="Status">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={ALL}>All statuses</SelectItem>
                {STATUSES.map((s) => (
                  <SelectItem key={s} value={s}>
                    {s}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>

          <Field label="Event">
            <Select
              value={eventId}
              onValueChange={(value) => setEventId(value ?? ALL)}
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

      {resend.isSuccess ? (
        <div className="mb-4">
          <StatusAlert tone="success">
            {/* One recipient: the order's buyer (spec 011 FR-012). */}
            {resend.data.message} to {resend.data.sent_to}.
          </StatusAlert>
        </div>
      ) : null}
      {resendError ? (
        <div className="mb-4">
          <StatusAlert>{resendError.message}</StatusAlert>
        </div>
      ) : null}

      {isPending ? <Loading /> : null}
      {error ? <StatusAlert>{error.message}</StatusAlert> : null}
      {orders?.length === 0 ? <EmptyState>No orders match.</EmptyState> : null}

      {orders && orders.length > 0 ? (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Order</TableHead>
                <TableHead>Buyer</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Total</TableHead>
                <TableHead>Placed</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {orders.map((order) => (
                <TableRow key={order.id}>
                  <TableCell className="font-mono text-xs">{order.order_number}</TableCell>
                  <TableCell>
                    <div>{order.buyer_name}</div>
                    <div className="text-xs text-muted-foreground">{order.buyer_email}</div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="secondary">{order.status}</Badge>
                  </TableCell>
                  <TableCell>{formatCurrency(order.total_amount)}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatDateTime(order.created_at)}
                  </TableCell>
                  <TableCell>
                    {/* Only a PAID order has tickets to resend. */}
                    {order.status === "PAID" ? (
                      <Button
                        variant="outline"
                        disabled={resend.isPending}
                        onClick={() => resend.mutate(order.id)}
                      >
                        Resend email
                      </Button>
                    ) : null}
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

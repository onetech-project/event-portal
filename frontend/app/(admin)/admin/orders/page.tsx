"use client";

import { useEffect, useState } from "react";

import { OrderPaymentPanel } from "@/components/admin/order-payment-panel";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
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
import { ApiError } from "@/lib/api-client";
import { formatCurrency, formatDateTime } from "@/lib/format";
import { Pagination } from "@/components/ui/pagination";
import { useListParams } from "@/lib/use-list-params";
import { useAdminEventOptions, useAdminOrders, useResendTicketEmail } from "@/lib/queries";

const STATUSES = ["PENDING", "PAID", "CANCELLED", "EXPIRED"] as const;
const ALL = "__all__";

export default function AdminOrdersPage() {
  // Filters live in the URL alongside the page, not in component state: a link
  // shared from page 3 of a filtered list has to reopen on page 3 of that same
  // filtered list, or the position is preserved and its meaning is lost.
  const list = useListParams();
  const status = list.filters.status ?? ALL;
  const eventId = list.filters.event_id ?? ALL;
  const search = list.filters.search ?? "";

  // The order whose payment story is open, if any. Reached by order number from
  // this list, because that is what a guest hands support when they call.
  const [inspecting, setInspecting] = useState<{
    id: string;
    number: string;
    status: string;
  } | null>(null);

  const { data: events } = useAdminEventOptions();
  const {
    data: orders,
    isPending,
    error,
  } = useAdminOrders(
    status === ALL ? undefined : status,
    eventId === ALL ? undefined : eventId,
    search || undefined,
    list.params,
  );

  // The server clamps an out-of-range page to the last one; adopting what it
  // actually served is what makes a stale bookmark correct itself in the address
  // bar rather than showing a position the rows did not come from.
  const servedPage = orders?.page;
  const { syncServedPage } = list;
  useEffect(() => {
    if (servedPage !== undefined) syncServedPage(servedPage);
  }, [servedPage, syncServedPage]);

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
              onValueChange={(value) =>
                list.setFilter("status", !value || value === ALL ? undefined : value)
              }
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

          {/* FR-053: partial name, email or order number — the operator's lookup. */}
          <Field label="Find an order" className="sm:col-span-2">
            <Input
              type="search"
              placeholder="Order number, buyer name, or part of an email"
              defaultValue={search}
              onChange={(e) => {
                const next = e.currentTarget.value.trim();
                list.setFilter("search", next === "" ? undefined : next);
              }}
            />
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
      {orders?.items.length === 0 ? <EmptyState>No orders match.</EmptyState> : null}

      {orders && orders.items.length > 0 ? (
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
              {orders.items.map((order) => (
                <TableRow key={order.id}>
                  <TableCell className="font-mono text-xs">{order.order_number}</TableCell>
                  <TableCell>
                    <div>{order.buyer_name}</div>
                    <div className="text-xs text-muted-foreground">{order.buyer_email}</div>
                  </TableCell>
                  <TableCell className="space-y-1">
                    <Badge variant="secondary">{order.status}</Badge>
                    {/*
                      FR-033. PAID no longer implies money moved: a registration
                      is written straight to PAID with a zero total. Without this
                      the two are indistinguishable on the row an operator reads.
                    */}
                    {order.is_registration ? (
                      <div>
                        <Badge variant="outline">Registration</Badge>
                      </div>
                    ) : null}
                  </TableCell>
                  <TableCell>
                    {order.is_registration ? (
                      <span className="text-muted-foreground">Free</span>
                    ) : (
                      formatCurrency(order.total_amount)
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {formatDateTime(order.created_at)}
                  </TableCell>
                  <TableCell className="flex gap-2">
                    <Button
                      variant="outline"
                      onClick={() =>
                        setInspecting({
                          id: order.id,
                          number: order.order_number,
                          status: order.status,
                        })
                      }
                    >
                      Payment history
                    </Button>
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

      {orders ? (
        <Pagination
          page={orders.page}
          pageSize={orders.page_size}
          total={orders.total}
          totalPages={orders.total_pages}
          onPageChange={list.setPage}
          onPageSizeChange={list.setPageSize}
        />
      ) : null}

      {inspecting ? (
        <OrderPaymentPanel
          orderId={inspecting.id}
          orderNumber={inspecting.number}
          orderStatus={inspecting.status}
          open
          onOpenChange={(next) => {
            if (!next) setInspecting(null);
          }}
        />
      ) : null}
    </main>
  );
}

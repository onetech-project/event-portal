"use client";

import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogTitle,
} from "@/components/ui/dialog";
import { EmptyState, Loading, StatusAlert } from "@/components/ui/feedback";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { formatDateTime } from "@/lib/format";
import { useOrderHolds, useOrderNotifications } from "@/lib/queries";

/**
 * One order's payment story: every notification recorded against it, and what it
 * holds against what remains.
 *
 * This is the whole of this app's part in rescuing a payment whose notification
 * never arrived. Ops reads it, checks the order against the gateway's own
 * dashboard, tops up quota if the seats were resold, and then asks the *gateway*
 * to resend that transaction's notification — which settles the order through
 * the same path every ordinary purchase takes.
 *
 * There is deliberately no "mark as paid" here, and there never should be. A
 * second way to turn a payment into tickets would be the path least exercised
 * and most trusted at the moment it matters.
 */
export function OrderPaymentPanel({
  orderId,
  orderNumber,
  orderStatus,
  open,
  onOpenChange,
}: {
  orderId: string;
  orderNumber: string;
  orderStatus: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  // Both queries are gated on `orderId`, and the dialog only mounts with one, so
  // nothing fetches until an operator actually opens a row.
  const notifications = useOrderNotifications(open ? orderId : "");
  const holds = useOrderHolds(open ? orderId : "");

  const shortfall = (holds.data ?? []).filter((hold) => hold.remaining < hold.held);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-3xl">
        <DialogTitle className="font-mono text-base">{orderNumber}</DialogTitle>
        <DialogDescription>
          Payment history and seat holds for this order. Everything here is
          read-only — a stranded payment is recovered by asking the gateway to
          resend its notification, never by recording the payment here.
        </DialogDescription>

        <section className="mt-2">
          <h3 className="mb-2 text-sm font-semibold">Seats held vs. remaining</h3>

          {holds.isPending ? <Loading /> : null}
          {holds.error ? <StatusAlert>{holds.error.message}</StatusAlert> : null}
          {holds.data?.length === 0 ? (
            <EmptyState>This order holds no seats.</EmptyState>
          ) : null}

          {holds.data && holds.data.length > 0 ? (
            <>
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Ticket type</TableHead>
                      <TableHead>Held</TableHead>
                      <TableHead>Remaining</TableHead>
                      <TableHead>Top-up needed</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {holds.data.map((hold) => {
                      const missing = hold.held - hold.remaining;
                      return (
                        <TableRow key={hold.ticket_type_id}>
                          <TableCell>{hold.ticket_type_name}</TableCell>
                          <TableCell>{hold.held}</TableCell>
                          <TableCell>{hold.remaining}</TableCell>
                          <TableCell>
                            {missing > 0 ? (
                              <Badge variant="secondary">{missing} more</Badge>
                            ) : (
                              <span className="text-xs text-muted-foreground">—</span>
                            )}
                          </TableCell>
                        </TableRow>
                      );
                    })}
                  </TableBody>
                </Table>
              </div>

              {/*
                The refusal an operator would otherwise only discover by asking for
                a resend and watching it bounce. Every short type has to be topped
                up: the settle is all-or-nothing across the whole order.
              */}
              {orderStatus === "EXPIRED" && shortfall.length > 0 ? (
                <div className="mt-3">
                  <StatusAlert>
                    A resent notification cannot settle this order yet —{" "}
                    {shortfall.length === 1
                      ? "one ticket type is"
                      : `${shortfall.length} ticket types are`}{" "}
                    short. Add the missing quota to every one of them first; a
                    partial settle is not possible.
                  </StatusAlert>
                </div>
              ) : null}
            </>
          ) : null}
        </section>

        <section className="mt-4">
          <h3 className="mb-2 text-sm font-semibold">Notifications</h3>

          {notifications.isPending ? <Loading /> : null}
          {notifications.error ? (
            <StatusAlert>{notifications.error.message}</StatusAlert>
          ) : null}
          {notifications.data?.length === 0 ? (
            <EmptyState>
              No notification has ever been recorded against this order.
            </EmptyState>
          ) : null}

          {notifications.data && notifications.data.length > 0 ? (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Status</TableHead>
                    <TableHead>Transaction</TableHead>
                    <TableHead>Arrived</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {notifications.data.map((row) => (
                    <TableRow key={row.id}>
                      <TableCell>
                        {/*
                          A marker is this system's own conclusion, not something
                          the gateway said. Both live in the same field, and
                          letting one read as the other would be actively
                          misleading on the screen where it matters most.
                        */}
                        <Badge variant={row.is_marker ? "outline" : "secondary"}>
                          {row.status}
                        </Badge>
                        {row.is_marker ? (
                          <span className="ml-2 text-xs text-muted-foreground">
                            our note
                          </span>
                        ) : null}
                      </TableCell>
                      <TableCell className="font-mono text-xs">
                        {row.transaction_id || "—"}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {formatDateTime(row.received_at)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : null}
        </section>
      </DialogContent>
    </Dialog>
  );
}

"use client";

import { useState } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Field } from "@/components/ui/field";
import { EmptyState, Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
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
import { formatCurrency } from "@/lib/format";
import { useAdminFees, useCreateFee, useDeleteFee, useUpdateFee } from "@/lib/queries";
import type { FeeAdminView } from "@/lib/types";

/**
 * The fee master admin page (clarified 2026-08-05): the rows booking applies
 * to every new order's subtotal. Editing here never changes existing orders —
 * their fee lines were frozen at booking.
 */
export default function AdminFeesPage() {
  const { data: fees, isPending, error } = useAdminFees();
  const [editing, setEditing] = useState<FeeAdminView | null>(null);
  const remove = useDeleteFee();

  return (
    <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-8">
      <PageHeading
        title="Fees"
        subtitle="Applied to the order subtotal at booking; existing orders keep the fees they were booked with."
      />

      {error ? <StatusAlert>{error.message}</StatusAlert> : null}
      {remove.error instanceof ApiError ? (
        <StatusAlert>{remove.error.message}</StatusAlert>
      ) : null}

      <div className="mt-6 grid gap-6 lg:grid-cols-[minmax(0,1fr)_340px] lg:items-start">
        <Card>
          <CardContent>
            {isPending ? (
              <Loading label="Loading fees…" />
            ) : fees === undefined || fees.length === 0 ? (
              <EmptyState>No fees yet — orders are charged the ticket subtotal only.</EmptyState>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Name</TableHead>
                    <TableHead>Type</TableHead>
                    <TableHead>Value</TableHead>
                    <TableHead>Active</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {fees.map((fee) => (
                    <TableRow key={fee.id}>
                      <TableCell className="font-medium">{fee.name}</TableCell>
                      <TableCell>
                        <Badge variant="outline">{fee.fee_type}</Badge>
                      </TableCell>
                      <TableCell>
                        {fee.fee_type === "PERCENT"
                          ? `${trimPercent(fee.value)}%`
                          : formatCurrency(fee.value)}
                      </TableCell>
                      <TableCell>{fee.is_active ? "Yes" : "No"}</TableCell>
                      <TableCell className="space-x-2 text-right">
                        <Button variant="outline" size="sm" onClick={() => setEditing(fee)}>
                          Edit
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          disabled={remove.isPending}
                          onClick={() => remove.mutate(fee.id)}
                        >
                          Delete
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        <FeeForm
          key={editing?.id ?? "new"}
          fee={editing}
          onDone={() => setEditing(null)}
        />
      </div>
    </main>
  );
}

/** Create form, or the edit form for the row picked in the table. */
function FeeForm({ fee, onDone }: { fee: FeeAdminView | null; onDone: () => void }) {
  const create = useCreateFee();
  const update = useUpdateFee(fee?.id ?? "");
  const mutation = fee !== null ? update : create;

  const [name, setName] = useState(fee?.name ?? "");
  const [feeType, setFeeType] = useState<"PERCENT" | "FIXED">(fee?.fee_type ?? "PERCENT");
  const [value, setValue] = useState(fee !== null ? trimPercent(fee.value) : "");
  const [isActive, setIsActive] = useState(fee?.is_active ?? true);

  const submit = (e: React.FormEvent) => {
    e.preventDefault();
    mutation.mutate(
      {
        name,
        fee_type: feeType,
        value: value === "" ? "0" : value,
        position: fee?.position ?? 0,
        is_active: isActive,
      },
      { onSuccess: onDone },
    );
  };

  const apiError = mutation.error instanceof ApiError ? mutation.error : null;
  const fieldErrors =
    apiError !== null && typeof apiError.data === "object" && apiError.data !== null
      ? (apiError.data as Record<string, string>)
      : {};

  return (
    <Card>
      <CardHeader>
        <CardTitle>{fee !== null ? `Edit ${fee.name}` : "New fee"}</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="space-y-4">
          {apiError !== null && Object.keys(fieldErrors).length === 0 ? (
            <StatusAlert>{apiError.message}</StatusAlert>
          ) : null}

          <Field label="Name" error={fieldErrors.name}>
            <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="PPN" />
          </Field>

          <Field label="Type" error={fieldErrors.fee_type}>
            <Select
              value={feeType}
              onValueChange={(v) => setFeeType((v as "PERCENT" | "FIXED") ?? "PERCENT")}
            >
              <SelectTrigger aria-label="Fee type" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="PERCENT">Percent of subtotal</SelectItem>
                <SelectItem value="FIXED">Fixed amount</SelectItem>
              </SelectContent>
            </Select>
          </Field>

          <Field
            label={feeType === "PERCENT" ? "Percentage" : "Amount (Rp)"}
            error={fieldErrors.value}
          >
            <Input
              inputMode="decimal"
              value={value}
              onChange={(e) => setValue(e.target.value)}
              placeholder={feeType === "PERCENT" ? "11" : "1200"}
            />
          </Field>

          <label className="flex items-center gap-2 text-sm">
            <Checkbox
              checked={isActive}
              onCheckedChange={(checked) => setIsActive(checked === true)}
            />
            Active — applied to new bookings
          </label>

          <div className="flex gap-2">
            <Button type="submit" disabled={mutation.isPending}>
              {mutation.isPending ? "Saving…" : fee !== null ? "Save changes" : "Add fee"}
            </Button>
            {fee !== null ? (
              <Button type="button" variant="ghost" onClick={onDone}>
                Cancel
              </Button>
            ) : null}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

/** "11.00" → "11", "2.50" → "2.5" for display and for the edit input. */
function trimPercent(value: string): string {
  const n = Number(value);
  return Number.isNaN(n) ? value : String(n);
}

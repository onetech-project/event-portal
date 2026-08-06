"use client";

import { Controller, type Control } from "react-hook-form";

import { Field } from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { EventForm } from "@/lib/schemas";

const STATUSES = ["DRAFT", "PUBLISHED", "COMPLETED"] as const;

/**
 * The event status picker, shared by the create and edit forms.
 *
 * BaseUI's Select holds its own value rather than exposing a native input, so it
 * goes through a Controller instead of register().
 */
export function EventStatusField({
  control,
  error,
}: Readonly<{ control: Control<EventForm>; error?: string }>) {
  return (
    <Controller
      control={control}
      name="status"
      render={({ field }) => (
        <Field label="Status" error={error}>
          <Select value={field.value} onValueChange={field.onChange}>
            <SelectTrigger aria-label="Status">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STATUSES.map((status) => (
                <SelectItem key={status} value={status}>
                  {status}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      )}
    />
  );
}

"use client";

import { Controller, type Control, type FieldPath, type FieldValues } from "react-hook-form";

import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { genderLabel } from "@/lib/holder-fields";
import type { GenderOption } from "@/lib/types";

/**
 * The identity of one ticket holder — name, email, phone, gender, date of birth.
 *
 * ONE component, rendered by both surfaces that collect this: the booking flow's
 * per-attendee cards and the free registration form (spec 022). They previously
 * shared only their validation rules (`lib/holder-fields.ts`) while duplicating
 * the markup, which is precisely where two screens drift — a label, a
 * placeholder or a field order corrected on one stayed wrong on the other, and
 * nothing failed to say so.
 *
 * Generic over the form's shape rather than tied to one, because the two callers
 * address these fields differently: booking writes `attendees.3.email`, and
 * registration writes a flat `email`. The paths are passed in, so neither caller
 * has to bend its schema to the other's.
 *
 * Every field goes through a `Controller`, including the two that could have used
 * `register`. Uniformity is the point: the phone and DOB inputs MUST be
 * controlled — their masks are what RHF validates and submits — and a mixed
 * approach invites the next field to be added the wrong way.
 */
export function HolderFields<T extends FieldValues>({
  control,
  fields,
  errors,
  genders,
}: {
  control: Control<T>;
  /** Where each value lives in the caller's own form shape. */
  fields: {
    name: FieldPath<T>;
    email: FieldPath<T>;
    phone: FieldPath<T>;
    gender: FieldPath<T>;
    dob: FieldPath<T>;
  };
  /** Already-resolved messages: the two callers index their errors differently. */
  errors: {
    name?: string;
    email?: string;
    phone?: string;
    gender?: string;
    dob?: string;
  };
  genders: GenderOption[] | undefined;
}) {
  return (
    <>
      <div className="sm:col-span-2">
        <Field label="Full name" required error={errors.name}>
          <Controller
            control={control}
            name={fields.name}
            render={({ field }) => (
              <Input
                className="h-auto px-3 py-4"
                placeholder="As written on ID card"
                value={(field.value as string) ?? ""}
                onChange={field.onChange}
                onBlur={field.onBlur}
                name={field.name}
                ref={field.ref}
              />
            )}
          />
        </Field>
      </div>

      <Field label="Email address" required error={errors.email}>
        <Controller
          control={control}
          name={fields.email}
          render={({ field }) => (
            <Input
              className="h-auto px-3 py-4"
              placeholder="name@example.com"
              value={(field.value as string) ?? ""}
              onChange={field.onChange}
              onBlur={field.onBlur}
              name={field.name}
              ref={field.ref}
            />
          )}
        />
      </Field>

      <Field label="Phone number" required error={errors.phone}>
        <PhoneInput control={control} name={fields.phone} />
      </Field>

      <Field label="Gender" required error={errors.gender}>
        <GenderSelect control={control} name={fields.gender} options={genders} />
      </Field>

      <Field label="Date of birth" required error={errors.dob}>
        <DobInput control={control} name={fields.dob} />
      </Field>
    </>
  );
}

function GenderSelect<T extends FieldValues>({
  control,
  name,
  options,
}: {
  control: Control<T>;
  name: FieldPath<T>;
  options: GenderOption[] | undefined;
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Select
          value={field.value === "" ? null : (field.value as string)}
          onValueChange={(value) => field.onChange(value ?? "")}
        >
          <SelectTrigger
            aria-label="Gender"
            className="px-3 py-4 w-full data-[size=default]:h-fit "
          >
            {/* The trigger shows the human label, never the stored value. */}
            <SelectValue>
              {(value: string | null) => (value === null ? "Select" : genderLabel(value))}
            </SelectValue>
          </SelectTrigger>
          <SelectContent align="start" alignItemWithTrigger={false}>
            {(options ?? []).map((option) => (
              <SelectItem key={option.id} value={option.name} className="p-3">
                {genderLabel(option.name)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    />
  );
}

/**
 * Phone as a free-text, digits-only field (spec 011 FR-006, clarified
 * 2026-08-07). No country code is supplied by the interface: the guest types the
 * whole number in whichever form they think in, `62…` or `08…`, and it is stored
 * exactly that way. The only thing the mask does is keep non-digits out, because
 * `type="tel"` and `inputMode="numeric"` are keyboard hints rather than filters —
 * a physical keyboard types letters straight through both — so without it the
 * field would accept anything until the schema complained on blur. Controlled
 * through a Controller so the masked value is what RHF validates and submits.
 */
function PhoneInput<T extends FieldValues>({
  control,
  name,
}: {
  control: Control<T>;
  name: FieldPath<T>;
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Input
          className="h-auto px-3 py-4"
          inputMode="numeric"
          type="tel"
          // Twelve digits: the placeholder must not advertise an example the
          // field would refuse (FR-006's floor, raised 2026-08-13).
          placeholder="081234567890"
          value={(field.value as string) ?? ""}
          onChange={(event) => field.onChange(phoneDigits(event.target.value))}
          onBlur={field.onBlur}
          name={field.name}
          ref={field.ref}
        />
      )}
    />
  );
}

/**
 * Date of birth as DD/MM/YYYY text — no native calendar control (clarified
 * 2026-08-06). The mask feeds the slashes in as the guest types digits, which is
 * what lets the numeric mobile keypad (no "/" key) still produce the format.
 * Controlled through a Controller so the masked value is what RHF validates.
 *
 * `type="text"`, and it has to stay that way. Two independent reasons:
 *
 * - A native `type="date"` holds ONLY `YYYY-MM-DD`; anything else is sanitised
 *   to the empty string. `dobSchema`, `dobToIso` and the server all speak
 *   DD/MM/YYYY, so the masked value written back would blank the field the
 *   instant the guest completed a date.
 * - Its calendar button cannot be hidden in Firefox at all. There is no
 *   `::-moz-` counterpart to `::-webkit-calendar-picker-indicator` and
 *   `::picker-icon` is unimplemented there (Bugzilla 1830890), so the control
 *   would look like a different field per browser.
 */
function DobInput<T extends FieldValues>({
  control,
  name,
}: {
  control: Control<T>;
  name: FieldPath<T>;
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => (
        <Input
          className="h-auto px-3 py-4"
          type="text"
          placeholder="DD/MM/YYYY"
          inputMode="numeric"
          maxLength={10}
          value={(field.value as string) ?? ""}
          onChange={(event) => field.onChange(maskDob(event.target.value))}
          onBlur={field.onBlur}
          name={field.name}
          ref={field.ref}
        />
      )}
    />
  );
}

/**
 * Whatever was typed or pasted → the digits of it, capped at the 15 the server
 * accepts. Nothing else is touched: a leading 0 stays a leading 0 and a leading
 * 62 stays a leading 62, because FR-006 keeps whichever form the guest chose.
 * A pasted "+62 812-3456-789" therefore lands on "628123456789" — the separators
 * and the "+" fall away, the number itself does not change form.
 */
function phoneDigits(raw: string): string {
  return raw.replace(/\D/g, "").slice(0, 15);
}

/** Keeps only digits and re-inserts the slashes: "31121999" → "31/12/1999". */
function maskDob(raw: string): string {
  const digits = raw.replace(/\D/g, "").slice(0, 8);
  return [digits.slice(0, 2), digits.slice(2, 4), digits.slice(4)]
    .filter((part) => part !== "")
    .join("/");
}

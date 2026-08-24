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
  heldGender,
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
  /**
   * The gender this card already holds, as (id, name). Needed only when that
   * entry has since been retired — the active master list no longer carries it,
   * so its display name cannot be looked up (spec 011 FR-031, FR-035).
   */
  heldGender?: { id: number; name: string };
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
        <GenderSelect
          control={control}
          name={fields.gender}
          options={genders}
          held={heldGender}
        />
      </Field>

      <Field label="Date of birth" required error={errors.dob}>
        <DobInput control={control} name={fields.dob} />
      </Field>
    </>
  );
}

/**
 * A gender select fed by the master list (GET /ticket/genders). The design
 * system Select holds its own value rather than exposing a native input, so it
 * goes through a Controller instead of register().
 *
 * The value exchanged is the master entry's IDENTIFIER, stringified because the
 * Select deals in strings (spec 011 FR-034). The NAME is only ever rendered.
 *
 * The master list serves ACTIVE genders only, while a slot keeps whatever gender
 * it was saved with — deactivating an entry never rewrites a stored reference.
 * So a restored card can hold a value this list does not offer, and a select can
 * only show a value it has an option for. FR-031: the held value is added to
 * THIS card's options so it displays, and to no other card's.
 *
 * That widening needs the retired entry's NAME, which the active master list by
 * definition does not have — which is why `held` carries the (id, name) pair the
 * readback supplies (FR-035) rather than just the id. Under the old name-based
 * wire the name was self-sufficient and no such pair was needed; it is the one
 * place the move to identifiers cost something rather than simplifying.
 */
function GenderSelect<T extends FieldValues>({
  control,
  name,
  options,
  held,
}: {
  control: Control<T>;
  name: FieldPath<T>;
  options: GenderOption[] | undefined;
  held?: { id: number; name: string };
}) {
  return (
    <Controller
      control={control}
      name={name}
      render={({ field }) => {
        const value = field.value === "" ? null : (field.value as string);
        const choices = optionsIncluding(options, held, value);
        return (
          <Select value={value} onValueChange={(v) => field.onChange(v ?? "")}>
            <SelectTrigger
              aria-label="Gender"
              className="px-3 py-4 w-full data-[size=default]:h-fit "
            >
              {/* The trigger shows the human label, never the submitted id. */}
              <SelectValue>
                {(current: string | null) => {
                  if (current === null) return "Select";
                  const match = choices.find((o) => String(o.id) === current);
                  // A value with no matching option cannot be labelled — which
                  // is precisely why the readback carries the name alongside the
                  // id (FR-035) and why `held` is threaded down here.
                  return match ? genderLabel(match.name) : "Select";
                }}
              </SelectValue>
            </SelectTrigger>
            <SelectContent align="start" alignItemWithTrigger={false}>
              {choices.map((option) => (
                <SelectItem key={option.id} value={String(option.id)} className="p-3">
                  {genderLabel(option.name)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        );
      }}
    />
  );
}

/**
 * The offered genders, plus `held` when it is set and the list does not already
 * contain it (FR-031). Returns the master list untouched in every ordinary case,
 * so the widening costs nothing on a card holding an active gender.
 *
 * The widening tracks the LIVE field value rather than the seed, which is what
 * makes the retired option leave the list the moment the guest picks something
 * else — a list widened from the seed would keep offering it forever.
 */
function optionsIncluding(
  options: GenderOption[] | undefined,
  held: { id: number; name: string } | undefined,
  current: string | null,
): GenderOption[] {
  const list = options ?? [];
  if (!held) return list;
  // Tracks the LIVE field value, not the seed: once the guest picks something
  // else the retired option leaves the list and cannot be chosen again (FR-031).
  if (String(held.id) !== current) return list;
  if (list.some((option) => option.id === held.id)) return list;
  return [...list, held];
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

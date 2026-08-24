import { z } from "zod";

/**
 * The holder-identity field rules, shared by every surface that collects them.
 *
 * Extracted from `components/order/visitor-form.tsx` when free registration
 * (spec 022) added a second such surface. They live here rather than being
 * copied because FR-020 and FR-025 require the identical input to fail
 * identically on both forms, with the identical message — two different phone
 * rules on two forms of one product is a defect, and a copy drifts the first
 * time either is tuned. This rule has already been tuned twice.
 *
 * The messages are also shared with the SERVER word for word
 * (`backend/internal/order/dto.go`), so the same failure reads the same
 * whichever side catches it.
 */

// DOB is typed as DD/MM/YYYY text — no native calendar control (clarified
// 2026-08-06) — and crosses the wire as ISO YYYY-MM-DD via dobToIso.
export const DOB_PATTERN = /^\d{2}\/\d{2}\/\d{4}$/;

/** "31/12/1999" → "1999-12-31"; null when not a real calendar date. */
export function dobToIso(value: string): string | null {
  if (!DOB_PATTERN.test(value)) return null;
  const day = Number(value.slice(0, 2));
  const month = Number(value.slice(3, 5));
  const year = Number(value.slice(6));
  // Round-trip through Date to reject e.g. 31/02/2000 (Date rolls it over).
  const date = new Date(Date.UTC(year, month - 1, day));
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return null;
  }
  return `${value.slice(6)}-${value.slice(3, 5)}-${value.slice(0, 2)}`;
}

/** The guest's local calendar date as ISO, e.g. "2026-08-06". */
export function todayIso(): string {
  const now = new Date();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${now.getFullYear()}-${month}-${day}`;
}

export const dobSchema = z
  .string()
  .min(1, "Date of birth is required.")
  .regex(DOB_PATTERN, "Enter a date as DD/MM/YYYY.")
  .refine((value) => !DOB_PATTERN.test(value) || dobToIso(value) !== null, {
    message: "Enter a real calendar date.",
  })
  .refine(
    (value) => {
      const iso = dobToIso(value);
      // Compared as ISO strings against the guest's LOCAL today (both are
      // zero-padded, so lexical order is chronological). Parsing to a Date
      // would read the value as UTC midnight and reject today's date for
      // anyone east of UTC during their morning — today must be accepted.
      return iso === null || iso <= todayIso();
    },
    { message: "Date of birth cannot be in the future." },
  );

// The gender option set comes from master data, so the schema only requires a
// choice; the server checks membership against the active list.
export const genderSchema = z.string().min(1, "Select a gender.");

// Spec 011 FR-006 (clarified 2026-08-07, floor raised 2026-08-13) — 12-15
// digits, and nothing but digits. Length is the whole rule: no prefix is
// required, and the digits are counted on the value exactly as typed. So
// `628123456789` passes while the same subscriber number written `08123456789`
// is eleven digits and does not.
//
// The Figma comp for the registration form shows "+628125567820" in its filled
// state. That value is REFUSED — the `+` is not a digit — and that is
// deliberate: the rule is inherited, not re-negotiated per surface.
export const PHONE_MESSAGE = "Enter a phone number of 12-15 digits.";

export const phoneSchema = z.string().regex(/^[0-9]{12,15}$/, PHONE_MESSAGE);

export const holderNameSchema = z.string().trim().min(1, "Full name is required.");

export const holderEmailSchema = z.email("Enter a valid email address.");

/** Title-cases a master-list gender name for display ("MALE" → "Male"). */
export function genderLabel(name: string): string {
  return name.charAt(0).toUpperCase() + name.slice(1).toLowerCase();
}

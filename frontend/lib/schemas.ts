import { z } from "zod";

/**
 * Browser-side validation schemas.
 *
 * These mirror the server's rules so a guest gets an immediate, field-level
 * message instead of a round trip. The server remains the authority — it
 * revalidates everything and recomputes prices and totals itself.
 */

const trimmedRequired = (label: string) =>
  z
    .string()
    .trim()
    .min(1, `${label} is required.`);

export const loginSchema = z.object({
  email: z.email("Enter a valid email address."),
  password: z.string().min(1, "Password is required."),
});

export type LoginForm = z.infer<typeof loginSchema>;

const slugPattern = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

export const eventFormSchema = z
  .object({
    name: trimmedRequired("Name"),
    slug: z
      .string()
      .trim()
      .min(1, "Slug is required.")
      .regex(slugPattern, 'Use lowercase letters, digits, and single hyphens (e.g. "jazz-night").'),
    description: z.string().optional(),
    venue: trimmedRequired("Venue"),
    address: trimmedRequired("Address"),
    startDate: trimmedRequired("Start date"),
    endDate: trimmedRequired("End date"),
    // A plain URL string: this MVP has no upload endpoint and no object storage.
    bannerUrl: z.union([z.literal(""), z.url("Enter a valid URL.")]).optional(),
    // Expected visitor count for the detail page's info bar; digits only.
    scale: z
      .union([z.literal(""), z.string().regex(/^\d+$/, "Enter a whole number of visitors.")])
      .optional(),
    status: z.enum(["DRAFT", "PUBLISHED", "COMPLETED"]),
  })
  .superRefine((value, ctx) => {
    if (new Date(value.endDate) < new Date(value.startDate)) {
      ctx.addIssue({
        code: "custom",
        path: ["endDate"],
        message: "End date must not be before the start date.",
      });
    }
  });

export type EventForm = z.infer<typeof eventFormSchema>;

export const ticketTypeFormSchema = z
  .object({
    name: trimmedRequired("Name"),
    // Optional remark shown on the booking card in place of the standard
    // non-refundable notice. Blank keeps the standard wording.
    description: z.string().optional(),
    price: z
      .string()
      .trim()
      .refine((value) => value !== "" && !Number.isNaN(Number(value)), "Enter a valid amount.")
      .refine((value) => Number(value) >= 0, "Price must not be negative."),
    // Remaining quota, not an original allocation. Zero means sold out.
    quota: z.number().int().min(0, "Remaining quota must not be negative."),
    salesStart: trimmedRequired("Sales start"),
    salesEnd: trimmedRequired("Sales end"),
    // The admission window, independent of the sales window above (spec 015
    // FR-003). Containment against the parent event's dates is the server's
    // call — this form has no access to them.
    eventStart: trimmedRequired("Event start"),
    eventEnd: trimmedRequired("Event end"),
    // Spec 022 FR-001a. NOT a listing preference: false means the type is
    // obtained by registering rather than by buying — off every guest purchase
    // surface, refused by booking and checkout, and ineligible for packages.
    // Defaults true, matching the column, so a form that never touches it
    // cannot take a ticket off sale.
    isVisible: z.boolean(),
  })
  .superRefine((value, ctx) => {
    if (new Date(value.salesEnd) < new Date(value.salesStart)) {
      ctx.addIssue({
        code: "custom",
        path: ["salesEnd"],
        message: "Sales must not end before they start.",
      });
    }
    if (new Date(value.eventEnd) < new Date(value.eventStart)) {
      ctx.addIssue({
        code: "custom",
        path: ["eventEnd"],
        message: "The event must not end before it starts.",
      });
    }

  });

export type TicketTypeForm = z.infer<typeof ticketTypeFormSchema>;

/**
 * A package (bundle) grants exactly one of each constituent ticket type per
 * unit. The composition picker therefore only records which ticket types are
 * in the bundle; there is no per-type quantity to enter.
 *
 * A bundle owns no inventory — what it can sell is derived from its
 * constituents' remaining quota (FR-036).
 */
export const packageFormSchema = z
  .object({
    name: trimmedRequired("Name"),
    description: z.string().optional(),
    price: z
      .string()
      .trim()
      .refine((value) => value !== "" && !Number.isNaN(Number(value)), "Enter a valid amount.")
      .refine((value) => Number(value) >= 0, "Price must not be negative."),
    salesStart: trimmedRequired("Sales start"),
    salesEnd: trimmedRequired("Sales end"),
    // A package's availability is a two-state flag, not a word (spec 011
    // FR-029). Unlike the order status — whose names come from a master list
    // and must round-trip — this one has no list behind it, so a boolean says
    // everything the ACTIVE/INACTIVE strings did.
    isActive: z.boolean(),
    components: z.array(z.guid("Select a valid ticket type.")),
  })
  .superRefine((value, ctx) => {
    if (new Date(value.salesEnd) < new Date(value.salesStart)) {
      ctx.addIssue({
        code: "custom",
        path: ["salesEnd"],
        message: "Sales must not end before they start.",
      });
    }

    if (value.components.length === 0) {
      ctx.addIssue({
        code: "custom",
        path: ["components"],
        message: "Select at least one ticket type — a bundle needs constituents.",
      });
    }
  });

export type PackageForm = z.infer<typeof packageFormSchema>;

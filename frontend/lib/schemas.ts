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

const checkoutItemSchema = z.object({
  ticketTypeId: z.guid("Select a valid ticket type."),
  quantity: z.number().int().min(1),
});

const checkoutAttendeeSchema = z.object({
  ticketTypeId: z.guid("Select a valid ticket type."),
  name: trimmedRequired("Attendee name"),
  email: z.email("Enter a valid email address."),
});

export const checkoutSchema = z
  .object({
    buyerName: trimmedRequired("Your name"),
    buyerEmail: z.email("Enter a valid email address."),
    buyerPhone: trimmedRequired("Phone number"),
    items: z.array(checkoutItemSchema).min(1, "Select at least one ticket."),
    attendees: z.array(checkoutAttendeeSchema),
  })
  .superRefine((value, ctx) => {
    const total = value.items.reduce((sum, item) => sum + item.quantity, 0);

    if (total !== value.attendees.length) {
      ctx.addIssue({
        code: "custom",
        path: ["attendees"],
        message: `Enter details for all ${total} attendee(s).`,
      });
      return;
    }

    // The grand total matching is not enough: each ticket type's attendee count
    // must match its own quantity, or tickets would be issued for the wrong type.
    const provided = new Map<string, number>();
    for (const attendee of value.attendees) {
      provided.set(attendee.ticketTypeId, (provided.get(attendee.ticketTypeId) ?? 0) + 1);
    }

    for (const item of value.items) {
      if ((provided.get(item.ticketTypeId) ?? 0) !== item.quantity) {
        ctx.addIssue({
          code: "custom",
          path: ["attendees"],
          message: "Each ticket type needs exactly as many attendees as tickets ordered.",
        });
        return;
      }
    }
  });

export type CheckoutForm = z.infer<typeof checkoutSchema>;

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
    price: z
      .string()
      .trim()
      .refine((value) => value !== "" && !Number.isNaN(Number(value)), "Enter a valid amount.")
      .refine((value) => Number(value) >= 0, "Price must not be negative."),
    // Remaining quota, not an original allocation. Zero means sold out.
    quota: z.number().int().min(0, "Remaining quota must not be negative."),
    salesStart: trimmedRequired("Sales start"),
    salesEnd: trimmedRequired("Sales end"),
  })
  .superRefine((value, ctx) => {
    if (new Date(value.salesEnd) < new Date(value.salesStart)) {
      ctx.addIssue({
        code: "custom",
        path: ["salesEnd"],
        message: "Sales must not end before they start.",
      });
    }
  });

export type TicketTypeForm = z.infer<typeof ticketTypeFormSchema>;

"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useEffect, useMemo } from "react";
import { useForm } from "react-hook-form";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Loading, PageHeading, StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api-client";
import { formatCurrency } from "@/lib/format";
import { useCheckout, useEventBySlug } from "@/lib/queries";
import { checkoutSchema, type CheckoutForm } from "@/lib/schemas";

export default function CheckoutPage() {
  // useSearchParams needs a Suspense boundary in the App Router.
  return (
    <Suspense fallback={<Loading />}>
      <CheckoutForm />
    </Suspense>
  );
}

/** Parses the `t=<ticketTypeId>:<quantity>` pairs the event page links with. */
function parseSelection(params: URLSearchParams): { ticketTypeId: string; quantity: number }[] {
  return params
    .getAll("t")
    .map((pair) => {
      const [ticketTypeId, quantity] = pair.split(":");
      return { ticketTypeId, quantity: Number(quantity) };
    })
    .filter((item) => item.ticketTypeId && Number.isInteger(item.quantity) && item.quantity > 0);
}

function CheckoutForm() {
  const params = useSearchParams();
  const router = useRouter();
  const slug = params.get("slug") ?? "";
  const selection = useMemo(() => parseSelection(params), [params]);

  const { data: event, isPending } = useEventBySlug(slug);
  const checkout = useCheckout();

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
  } = useForm<CheckoutForm>({
    resolver: zodResolver(checkoutSchema),
    defaultValues: {
      buyerName: "",
      buyerEmail: "",
      buyerPhone: "",
      items: selection,
      // One attendee entry per ticket: this is the dynamic part of the form, and
      // the count must match the order exactly or the server rejects it.
      attendees: selection.flatMap((item) =>
        Array.from({ length: item.quantity }, () => ({
          ticketTypeId: item.ticketTypeId,
          name: "",
          email: "",
        })),
      ),
    },
  });

  // The guest stays on the site: the order page shows the QRIS code and updates
  // itself when the payment lands (spec FR-009).
  useEffect(() => {
    if (checkout.data?.order_number) {
      router.replace(`/orders/${encodeURIComponent(checkout.data.order_number)}`);
    }
  }, [checkout.data, router]);

  const nameOf = (ticketTypeId: string) =>
    event?.ticket_types.find((t) => t.id === ticketTypeId)?.name ?? "Ticket";

  const attendeeRows = selection.flatMap((item, itemIndex) =>
    Array.from({ length: item.quantity }, (_, seat) => ({
      ticketTypeId: item.ticketTypeId,
      label: `${nameOf(item.ticketTypeId)} — attendee ${seat + 1}`,
      index: selection
        .slice(0, itemIndex)
        .reduce((sum, previous) => sum + previous.quantity, 0) + seat,
    })),
  );

  if (selection.length === 0) {
    return (
      <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
        <StatusAlert tone="info">
          No tickets selected.{" "}
          <Link href="/events" className="underline">
            Browse events
          </Link>{" "}
          to pick some.
        </StatusAlert>
      </main>
    );
  }

  if (isPending) return <Loading label="Loading ticket details…" />;

  const onSubmit = handleSubmit((values) =>
    checkout.mutateAsync({
      buyer_name: values.buyerName,
      buyer_email: values.buyerEmail,
      buyer_phone: values.buyerPhone,
      items: values.items.map((item) => ({
        ticket_type_id: item.ticketTypeId,
        quantity: item.quantity,
      })),
      attendees: values.attendees.map((attendee) => ({
        ticket_type_id: attendee.ticketTypeId,
        name: attendee.name,
        email: attendee.email,
      })),
    }),
  );

  const error = checkout.error instanceof ApiError ? checkout.error : null;

  return (
    <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
      <PageHeading
        title="Checkout"
        subtitle="No account needed — we email your tickets after payment."
      />

      <Card className="mb-6">
        <CardHeader>
          <CardTitle>Your order</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="space-y-1 text-sm">
            {selection.map((item) => {
              const ticketType = event?.ticket_types.find((t) => t.id === item.ticketTypeId);
              return (
                <li key={item.ticketTypeId} className="flex justify-between">
                  <span>
                    {nameOf(item.ticketTypeId)} × {item.quantity}
                  </span>
                  <span>
                    {ticketType
                      ? formatCurrency(String(Number(ticketType.price) * item.quantity))
                      : ""}
                  </span>
                </li>
              );
            })}
          </ul>
        </CardContent>
      </Card>

      {error ? (
        <div className="mb-6">
          <StatusAlert>
            {error.message}
            {error.code === "PAYMENT_INITIATION_FAILED" ? (
              <span className="mt-1 block">
                Nothing was charged and your tickets were released — you can safely try
                again.
              </span>
            ) : null}
          </StatusAlert>
        </div>
      ) : null}

      <form onSubmit={onSubmit} className="space-y-6">
        <Card>
          <CardHeader>
            <CardTitle>Buyer details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <Field label="Full name" error={errors.buyerName?.message}>
              <Input {...register("buyerName")} autoComplete="name" />
            </Field>
            <Field
              label="Email"
              error={errors.buyerEmail?.message}
              hint="Your tickets are sent here."
            >
              <Input type="email" {...register("buyerEmail")} autoComplete="email" />
            </Field>
            <Field label="Phone" error={errors.buyerPhone?.message}>
              <Input {...register("buyerPhone")} autoComplete="tel" />
            </Field>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Attendees</CardTitle>
          </CardHeader>
          <CardContent className="space-y-6">
            <p className="text-sm text-muted-foreground">
              One ticket is issued per attendee, each with its own QR code.
            </p>

            {attendeeRows.map((row) => (
              <fieldset key={row.index} className="space-y-3">
                <legend className="text-sm font-medium">{row.label}</legend>
                <input
                  type="hidden"
                  value={row.ticketTypeId}
                  {...register(`attendees.${row.index}.ticketTypeId`)}
                />
                <Field label="Name" error={errors.attendees?.[row.index]?.name?.message}>
                  <Input {...register(`attendees.${row.index}.name`)} />
                </Field>
                <Field label="Email" error={errors.attendees?.[row.index]?.email?.message}>
                  <Input type="email" {...register(`attendees.${row.index}.email`)} />
                </Field>
              </fieldset>
            ))}

            {errors.attendees?.message ? (
              <p role="alert" className="text-xs text-destructive">
                {errors.attendees.message}
              </p>
            ) : null}
          </CardContent>
        </Card>

        <Button type="submit" size="lg" disabled={isSubmitting || checkout.isPending}>
          {checkout.isPending ? "Starting payment…" : "Pay now"}
        </Button>
      </form>
    </main>
  );
}

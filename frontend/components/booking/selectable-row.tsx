"use client";

import { TicketNotch } from "@/components/booking/ticket-notch";
import { formatCurrency } from "@/lib/format";
import { availabilityOf, maxQuantityFor, nameOf, noticeOf, priceOf } from "@/lib/selection";
import type { SelectableItem } from "@/lib/types";
import { Info, Minus, Plus } from "lucide-react";
import { Button } from "../ui/button";

/**
 * One row of the booking list, drawn as a ticket stub: dashed rules with a notch
 * bitten out of each edge between them.
 *
 * Tickets and bundles share this component deliberately. The spec requires
 * identical selection mechanics for both, and a shared row is what stops them
 * drifting apart; only the Bundle badge tells them apart visually.
 */
export function SelectableRow({
  item,
  now,
  quantity,
  onChange,
}: {
  item: SelectableItem;
  now: number;
  quantity: number;
  onChange: (quantity: number) => void;
}) {
  const availability = availabilityOf(item, now);
  const max = maxQuantityFor(item);
  const isBundle = item.kind === "package";
  const name = nameOf(item);
  const notice = noticeOf(item);
  const selected = quantity > 0;

  // Selected stubs fill with the brand tint and carry the brand rule colour;
  // unselected ones stay on the neutral card surface.
  const surface = selected
    ? "border-brand-line bg-brand-surface"
    : "border-border bg-card";
  const rule = selected ? "border-brand-line" : "border-border";

  return (
    <article
      data-selected={selected ? "true" : undefined}
      className={`relative rounded-xl border px-6 py-5 transition-colors ${surface}`}
    >
      {isBundle ? (
        <p className="mb-2 inline-flex items-center rounded-md border border-destructive bg-destructive/5 px-2 py-1 text-[10px] font-bold tracking-[0.25px] text-destructive">
          Bundle
        </p>
      ) : null}

      <h3 className="text-xl font-bold tracking-tight text-ticket-title sm:text-2xl">
        {name}
      </h3>

      <hr className={`my-4 border-t border-dashed ${rule}`} />

        {/* The notches sit on this band, level with the notice line. */}
        <div className="relative -mx-6 px-6">
          <TicketNotch side="left" className={rule} />
          <TicketNotch side="right" className={rule} />

          {/* The organiser's own remark. No description, no notice line at all. */}
          {notice ? (
            <p className="flex items-center gap-1 text-xs whitespace-pre-line text-ticket-muted">
              <Info className="size-4" />
              {notice}
            </p>
          ) : null}
        </div>

      <hr className={`my-4 border-t border-dashed ${rule}`} />

      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <p className="text-xl font-extrabold tracking-[-0.5px] text-ticket-ink">
            {formatCurrency(priceOf(item))}
          </p>

          {!availability.available ? (
            <p className="mt-1 text-xs font-medium text-ticket-muted">
              {availability.label}
            </p>
          ) : null}
        </div>

        {selected ? (
          <Stepper name={name} quantity={quantity} max={max} onChange={onChange} />
        ) : (
          <Button
            type="button"
            disabled={!availability.available}
            onClick={() => onChange(1)}
            aria-label={`Add ${name}`}
            className="rounded-lg bg-brand px-4 text-sm font-bold text-brand-foreground transition-opacity hover:opacity-90 hover:bg-brand disabled:pointer-events-none disabled:opacity-50"
          >
            Add <span aria-hidden>+</span>
          </Button>
        )}
      </div>
    </article>
  );
}

/**
 * The quantity control a selected row turns into.
 *
 * Decrementing from 1 returns the row to its Add state rather than sitting at 0,
 * and the ceiling is whatever the server last reported — remaining quota for a
 * ticket, whole available sets for a bundle. It can still change under us, which
 * is why checkout is the real authority.
 */
function Stepper({
  name,
  quantity,
  max,
  onChange,
}: {
  name: string;
  quantity: number;
  max: number;
  onChange: (quantity: number) => void;
}) {
  const button =
    "aspect-square bg-transparent rounded-lg border border-brand hover:bg-brand hover:text-brand-foreground transition-all disabled:pointer-events-none disabled:opacity-40";

  return (
    <div className="flex items-center">
      <Button
        type="button"
        variant="outline"
        aria-label={`Decrease ${name}`}
        onClick={() => onChange(quantity - 1)}
        className={button}
      >
        <Minus size={20} />
      </Button>

      <span
        aria-live="polite"
        aria-label={`Quantity for ${name}`}
        className="w-8 text-center text-base text-ticket-ink"
      >
        {quantity}
      </span>

      <Button
        type="button"
        aria-label={`Increase ${name}`}
        variant="outline"
        disabled={quantity >= max}
        onClick={() => onChange(quantity + 1)}
        className={button}
      >
        <Plus size={20} />
      </Button>
    </div>
  );
}

"use client";

import {
  ChevronLeftIcon,
  ChevronRightIcon,
  ChevronsLeftIcon,
  ChevronsRightIcon,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { PAGE_SIZES } from "@/lib/types";
import { cn } from "@/lib/utils";

/**
 * The paging control every admin list shares (spec 021).
 *
 * One component rather than four is what makes SC-005 true — an operator who has
 * used Orders needs no second explanation for Attendees. It is presentational:
 * it reports what was clicked and renders what it is told, and never holds the
 * current page itself. That state lives in the URL (`useListParams`), because a
 * position an operator cannot share or return to is only half a position.
 */

export type PaginationProps = {
  /** The page currently being displayed — what the server served, not what was asked for. */
  page: number;
  pageSize: number;
  /** Rows matching the current filters, across all pages. */
  total: number;
  totalPages: number;
  onPageChange: (page: number) => void;
  onPageSizeChange: (size: number) => void;
  className?: string;
};

export function Pagination({
  page,
  pageSize,
  total,
  totalPages,
  onPageChange,
  onPageSizeChange,
  className,
}: PaginationProps) {
  // Nothing to page through: the list renders its own empty state, and controls
  // here would be furniture around an absence (FR-016).
  if (total <= 0 || totalPages <= 0) return null;

  const first = (page - 1) * pageSize + 1;
  // The last page is usually partial, so this is bounded by the total rather
  // than assuming a full page.
  const last = Math.min(page * pageSize, total);

  const atStart = page <= 1;
  const atEnd = page >= totalPages;

  return (
    <nav
      aria-label="Pagination"
      className={cn(
        "mt-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between",
        className,
      )}
    >
      <div className="flex items-center gap-4">
        <p className="text-sm text-muted-foreground">
          Showing {first}–{last} of {total}
        </p>

        <div className="flex items-center gap-2">
          <Select
            value={String(pageSize)}
            onValueChange={(value) => {
              if (value) onPageSizeChange(Number(value));
            }}
          >
            <SelectTrigger size="sm" aria-label="Rows per page" className="w-20">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {sizeOptions(pageSize).map((size) => (
                <SelectItem key={size} value={String(size)}>
                  {size}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="flex items-center gap-1">
        {/*
          The live region carries the position for anyone who cannot see the
          highlighted number. It is also the only place the total page count is
          stated in words.
        */}
        <span aria-live="polite" className="mr-2 text-sm text-muted-foreground">
          Page {page} of {totalPages}
        </span>

        <Button
          variant="outline"
          size="icon"
          aria-label="First page"
          disabled={atStart}
          onClick={() => onPageChange(1)}
        >
          <ChevronsLeftIcon className="size-4" />
        </Button>
        <Button
          variant="outline"
          size="icon"
          aria-label="Previous page"
          disabled={atStart}
          onClick={() => onPageChange(page - 1)}
        >
          <ChevronLeftIcon className="size-4" />
        </Button>

        {pageWindow(page, totalPages).map((n, i) =>
          n === ELLIPSIS ? (
            <span
              key={`gap-${i}`}
              aria-hidden="true"
              className="px-1 text-sm text-muted-foreground"
            >
              …
            </span>
          ) : (
            <Button
              key={n}
              variant={n === page ? "default" : "outline"}
              size="icon"
              aria-label={`Page ${n}`}
              aria-current={n === page ? "page" : undefined}
              onClick={() => onPageChange(n)}
            >
              {n}
            </Button>
          ),
        )}

        <Button
          variant="outline"
          size="icon"
          aria-label="Next page"
          disabled={atEnd}
          onClick={() => onPageChange(page + 1)}
        >
          <ChevronRightIcon className="size-4" />
        </Button>
        <Button
          variant="outline"
          size="icon"
          aria-label="Last page"
          disabled={atEnd}
          onClick={() => onPageChange(totalPages)}
        >
          <ChevronsRightIcon className="size-4" />
        </Button>
      </div>
    </nav>
  );
}

/**
 * The sizes to offer. An address may legitimately carry a size the menu does not
 * list — the API accepts 1..100 — and leaving it out would show a trigger with
 * nothing selected while that size was plainly in effect.
 */
function sizeOptions(current: number): number[] {
  const sizes = [...PAGE_SIZES] as number[];
  return sizes.includes(current) ? sizes : [current, ...sizes].sort((a, b) => a - b);
}

const ELLIPSIS = -1;

/**
 * The numbered buttons to show: always the first and last page, always a
 * neighbour either side of the current one, and a gap marker where numbers were
 * left out. Bounded so a list of 300 pages does not render 300 buttons.
 */
function pageWindow(page: number, totalPages: number): number[] {
  if (totalPages <= 7) {
    return Array.from({ length: totalPages }, (_, i) => i + 1);
  }

  const pages = new Set<number>([1, totalPages, page - 1, page, page + 1]);
  const shown = [...pages].filter((n) => n >= 1 && n <= totalPages).sort((a, b) => a - b);

  const out: number[] = [];
  shown.forEach((n, i) => {
    if (i > 0 && n - shown[i - 1] > 1) out.push(ELLIPSIS);
    out.push(n);
  });
  return out;
}

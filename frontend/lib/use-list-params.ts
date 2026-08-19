"use client";

import { useCallback, useMemo } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";

import { DEFAULT_PAGE_SIZE, PAGE_SIZES, type ListParams } from "./types";

/**
 * Paging and filter state for an admin list, held in the URL rather than in
 * component state (spec 021 FR-011).
 *
 * Filters live here too, not just the page. If they stayed in `useState`, a
 * shared link would land a colleague on page 3 of a *different* result set — the
 * position preserved and its meaning lost. One place for both is also what makes
 * "changing a filter returns to page 1" a single operation instead of two pieces
 * of state to keep in step (FR-010).
 */

/** The filters an admin list can carry. All optional, all plain strings. */
export type ListFilters = Record<string, string | undefined>;

export type ListState = {
  page: number;
  pageSize: number;
  filters: ListFilters;
};

/** Query keys that are paging, not filters. */
const PAGE_KEY = "page";
const SIZE_KEY = "page_size";

const MAX_PAGE_SIZE = PAGE_SIZES[PAGE_SIZES.length - 1];

/**
 * Parses a positive integer, rejecting anything a person might type that is not
 * one — blank, negative, fractional, non-numeric, or past what a number can hold
 * exactly. Returns null so the caller decides the fallback.
 */
function positiveInt(raw: string | null): number | null {
  if (raw === null) return null;
  const trimmed = raw.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const n = Number(trimmed);
  if (!Number.isSafeInteger(n) || n < 1) return null;
  return n;
}

/**
 * Reads list state out of a query string, correcting rather than rejecting.
 *
 * The server clamps too, and is authoritative — this exists so the controls
 * render sensibly on the first paint, before any response has arrived.
 */
export function readListParams(search: URLSearchParams): ListState {
  const page = positiveInt(search.get(PAGE_KEY)) ?? 1;

  // Any size in range is honoured, not just the ones the control offers. The
  // server accepts 1..100, and a client that silently substituted its own
  // default for an off-menu value would display twenty rows under an address
  // claiming two — the address and the list have to agree about what is on
  // screen, or neither can be trusted.
  const requestedSize = positiveInt(search.get(SIZE_KEY));
  const pageSize =
    requestedSize === null
      ? DEFAULT_PAGE_SIZE
      : Math.min(requestedSize, MAX_PAGE_SIZE);

  const filters: ListFilters = {};
  search.forEach((value, key) => {
    if (key === PAGE_KEY || key === SIZE_KEY) return;
    // `?status=` is not a status filter — it is an empty box.
    if (value !== "") filters[key] = value;
  });

  return { page, pageSize, filters };
}

/**
 * Renders list state as a query string for the address bar, omitting defaults so
 * a first visit stays clean and a shared address carries only what was chosen.
 */
export function toSearchString(state: ListState): string {
  const out = new URLSearchParams();
  if (state.page > 1) out.set(PAGE_KEY, String(state.page));
  if (state.pageSize !== DEFAULT_PAGE_SIZE) out.set(SIZE_KEY, String(state.pageSize));
  for (const [key, value] of Object.entries(state.filters)) {
    if (value) out.set(key, value);
  }
  return out.toString();
}

/**
 * Renders list state as a query string for the API. Unlike the address bar, this
 * always sends the paging explicitly: the client's idea of the default and the
 * server's are two separate constants, and relying on them agreeing is a bug
 * waiting for one of them to be retuned.
 */
export function buildListQuery(state: ListState): string {
  const out = new URLSearchParams();
  out.set(PAGE_KEY, String(state.page));
  out.set(SIZE_KEY, String(state.pageSize));
  for (const [key, value] of Object.entries(state.filters)) {
    if (value) out.set(key, value);
  }
  return out.toString();
}

/**
 * The page holding the first row of the old page, after a resize.
 *
 * Resizing without this drops an operator somewhere unrelated — going from page
 * 5 of 20 to a size of 100 would leave them on page 5 of 100, five hundred rows
 * past what they were reading.
 */
export function pageForNewSize(page: number, oldSize: number, newSize: number): number {
  const safePage = page < 1 ? 1 : page;
  if (oldSize === newSize) return safePage;
  const firstRow = (safePage - 1) * oldSize;
  return Math.floor(firstRow / newSize) + 1;
}

export type ListParamsController = ListState & {
  /** Just the paging, for passing to a query hook. */
  params: ListParams;
  /** The query string to send to the API. */
  query: string;
  setPage: (page: number) => void;
  setPageSize: (size: number) => void;
  /** Setting any filter returns to the first page (FR-010). */
  setFilter: (key: string, value: string | undefined) => void;
  /**
   * Accepts the server's clamped page as authoritative. Called with what the
   * response actually served, so a stale bookmark corrects itself in the address
   * bar instead of showing a position the rows did not come from (FR-013).
   */
  syncServedPage: (served: number) => void;
};

export function useListParams(): ListParamsController {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const state = useMemo(
    () => readListParams(new URLSearchParams(searchParams.toString())),
    [searchParams],
  );

  const write = useCallback(
    (next: ListState) => {
      const query = toSearchString(next);
      // replace, not push: walking ten pages should not bury the previous screen
      // under ten history entries, while Back still leaves the list.
      router.replace(query ? `${pathname}?${query}` : pathname, { scroll: false });
    },
    [pathname, router],
  );

  const setPage = useCallback(
    (page: number) => write({ ...state, page: page < 1 ? 1 : page }),
    [state, write],
  );

  const setPageSize = useCallback(
    (size: number) =>
      write({
        ...state,
        page: pageForNewSize(state.page, state.pageSize, size),
        pageSize: size,
      }),
    [state, write],
  );

  const setFilter = useCallback(
    (key: string, value: string | undefined) =>
      write({ ...state, page: 1, filters: { ...state.filters, [key]: value } }),
    [state, write],
  );

  const syncServedPage = useCallback(
    (served: number) => {
      if (served >= 1 && served !== state.page) write({ ...state, page: served });
    },
    [state, write],
  );

  return {
    ...state,
    params: { page: state.page, pageSize: state.pageSize },
    query: buildListQuery(state),
    setPage,
    setPageSize,
    setFilter,
    syncServedPage,
  };
}

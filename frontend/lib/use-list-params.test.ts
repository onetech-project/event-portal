import { describe, expect, it } from "vitest";

import {
  buildListQuery,
  pageForNewSize,
  readListParams,
  toSearchString,
} from "./use-list-params";
import { DEFAULT_PAGE_SIZE } from "./types";

/**
 * The pure half of the paging hook. Everything that decides *what* the URL says
 * lives here so it can be tested without a router; the hook itself only reads
 * `useSearchParams` and calls `router.replace`.
 */

const params = (query: string) => new URLSearchParams(query);

describe("readListParams — correcting what a person could type", () => {
  it("defaults when nothing was asked for", () => {
    const p = readListParams(params(""));
    expect(p.page).toBe(1);
    expect(p.pageSize).toBe(DEFAULT_PAGE_SIZE);
  });

  it("reads a valid page and size", () => {
    const p = readListParams(params("page=3&page_size=50"));
    expect(p.page).toBe(3);
    expect(p.pageSize).toBe(50);
  });

  it("honours a size the control does not offer, rather than substituting its own", () => {
    // The server accepts 1..100. Substituting the default here would render
    // twenty rows under an address claiming two.
    expect(readListParams(params("page_size=2")).pageSize).toBe(2);
    expect(readListParams(params("page_size=37")).pageSize).toBe(37);
  });

  it.each([
    ["page=0", 1],
    ["page=-3", 1],
    ["page=abc", 1],
    ["page=", 1],
    ["page=1.9", 1],
    ["page=99999999999999999999", 1],
  ])("corrects %s to page %i", (query, want) => {
    expect(readListParams(params(query)).page).toBe(want);
  });

  it.each([
    ["page_size=0", DEFAULT_PAGE_SIZE],
    ["page_size=abc", DEFAULT_PAGE_SIZE],
    ["page_size=-10", DEFAULT_PAGE_SIZE],
    ["page_size=5000", 100],
  ])("corrects %s to size %i", (query, want) => {
    expect(readListParams(params(query)).pageSize).toBe(want);
  });

  it("reads the filters alongside the paging", () => {
    const p = readListParams(params("page=2&status=PAID&event_id=e1"));
    expect(p.page).toBe(2);
    expect(p.filters.status).toBe("PAID");
    expect(p.filters.event_id).toBe("e1");
  });

  it("reports an absent filter as undefined rather than an empty string", () => {
    // An empty string would be sent to the API as `status=`, which is a
    // different request from "no status filter".
    const p = readListParams(params("status="));
    expect(p.filters.status).toBeUndefined();
  });
});

describe("toSearchString — what actually lands in the address bar", () => {
  it("omits the defaults so a first visit has a clean address", () => {
    expect(toSearchString({ page: 1, pageSize: DEFAULT_PAGE_SIZE, filters: {} })).toBe("");
  });

  it("carries a non-default page", () => {
    expect(toSearchString({ page: 4, pageSize: DEFAULT_PAGE_SIZE, filters: {} })).toBe("page=4");
  });

  it("carries a non-default size", () => {
    expect(toSearchString({ page: 1, pageSize: 50, filters: {} })).toBe("page_size=50");
  });

  it("carries filters so a shared address reproduces the view, not just the position", () => {
    const s = toSearchString({
      page: 2,
      pageSize: 50,
      filters: { status: "PAID", event_id: "e1" },
    });
    expect(s).toContain("page=2");
    expect(s).toContain("page_size=50");
    expect(s).toContain("status=PAID");
    expect(s).toContain("event_id=e1");
  });

  it("drops an unset filter entirely", () => {
    const s = toSearchString({
      page: 1,
      pageSize: DEFAULT_PAGE_SIZE,
      filters: { status: undefined, event_id: "e1" },
    });
    expect(s).toBe("event_id=e1");
  });

  it("round-trips through readListParams", () => {
    const before = { page: 3, pageSize: 100, filters: { status: "PENDING" } };
    expect(readListParams(params(toSearchString(before)))).toMatchObject(before);
  });
});

describe("pageForNewSize — keeping the row you were looking at", () => {
  it("keeps the first row visible when the page grows", () => {
    // Page 3 of 20 starts at row 40; at 50 per page that row is on page 1.
    expect(pageForNewSize(3, 20, 50)).toBe(1);
  });

  it("keeps the first row visible when the page shrinks", () => {
    // Page 2 of 50 starts at row 50; at 20 per page that row is on page 3.
    expect(pageForNewSize(2, 50, 20)).toBe(3);
  });

  it("stays on the first page when it was already the first", () => {
    expect(pageForNewSize(1, 20, 100)).toBe(1);
    expect(pageForNewSize(1, 100, 20)).toBe(1);
  });

  it("is a no-op when the size did not change", () => {
    expect(pageForNewSize(7, 50, 50)).toBe(7);
  });

  it("never returns a page below the first", () => {
    expect(pageForNewSize(0, 20, 20)).toBeGreaterThanOrEqual(1);
    expect(pageForNewSize(-5, 20, 20)).toBeGreaterThanOrEqual(1);
  });
});

describe("buildListQuery — what the API is asked for", () => {
  it("always sends the paging, because the server's defaults are not the client's business", () => {
    const q = buildListQuery({ page: 1, pageSize: DEFAULT_PAGE_SIZE, filters: {} });
    expect(q).toContain("page=1");
    expect(q).toContain(`page_size=${DEFAULT_PAGE_SIZE}`);
  });

  it("sends set filters and omits unset ones", () => {
    const q = buildListQuery({
      page: 2,
      pageSize: 20,
      filters: { status: "PAID", event_id: undefined },
    });
    expect(q).toContain("status=PAID");
    expect(q).not.toContain("event_id");
  });
});

describe("pageForNewSize — across the combinations an operator can actually pick", () => {
  // PAGE_SIZES is 20/50/100, so these are every transition the control offers.
  it.each([
    [1, 20, 50, 1],
    [1, 20, 100, 1],
    [2, 20, 50, 1], // row 20 → page 1 of 50
    [3, 20, 50, 1], // row 40 → page 1 of 50
    [4, 20, 50, 2], // row 60 → page 2 of 50
    [6, 20, 100, 2], // row 100 → page 2 of 100
    [2, 50, 20, 3], // row 50 → page 3 of 20
    [3, 50, 20, 6], // row 100 → page 6 of 20
    [2, 100, 20, 6], // row 100 → page 6 of 20
    [2, 100, 50, 3], // row 100 → page 3 of 50
  ])("page %i at %i per page becomes page %i at %i", (page, oldSize, newSize, want) => {
    expect(pageForNewSize(page, oldSize, newSize)).toBe(want);
  });

  it("keeps the row an operator was looking at inside the new page", () => {
    // The property the table above is sampling: the first row of the old page is
    // still on the page this returns.
    for (const page of [1, 2, 3, 5, 9]) {
      for (const oldSize of [20, 50, 100]) {
        for (const newSize of [20, 50, 100]) {
          const firstRow = (page - 1) * oldSize;
          const next = pageForNewSize(page, oldSize, newSize);
          const from = (next - 1) * newSize;
          expect(firstRow).toBeGreaterThanOrEqual(from);
          expect(firstRow).toBeLessThan(from + newSize);
        }
      }
    }
  });
});

describe("the address after the server clamped", () => {
  it("rewrites to the page that was actually served", () => {
    // A bookmark for page 999 of a list that now has 7 pages: the server serves
    // page 7, and the address has to agree or it displays a position the rows did
    // not come from.
    const stale = readListParams(params("page=999&status=PAID"));
    const corrected = { ...stale, page: 7 };

    expect(toSearchString(corrected)).toContain("page=7");
    expect(toSearchString(corrected)).toContain("status=PAID");
  });

  it("drops the page from the address entirely when the server served the first one", () => {
    const stale = readListParams(params("page=999"));
    expect(toSearchString({ ...stale, page: 1 })).toBe("");
  });
});

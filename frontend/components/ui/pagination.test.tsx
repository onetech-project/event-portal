import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { Pagination } from "./pagination";
import { DEFAULT_PAGE_SIZE } from "@/lib/types";

/**
 * The one control every admin list uses. Its job is to be identical everywhere
 * (spec 021 SC-005), to never offer a move that does not exist (FR-006), and to
 * be operable and legible without a mouse (FR-017).
 */

const noop = () => {};

function renderPagination(overrides: Partial<React.ComponentProps<typeof Pagination>> = {}) {
  return render(
    <Pagination
      page={1}
      pageSize={DEFAULT_PAGE_SIZE}
      total={137}
      totalPages={7}
      onPageChange={noop}
      onPageSizeChange={noop}
      {...overrides}
    />,
  );
}

describe("what it reports", () => {
  it("says which rows of how many are on screen", () => {
    renderPagination({ page: 2, pageSize: 20, total: 137, totalPages: 7 });
    expect(screen.getByText(/21.*40.*137/)).toBeInTheDocument();
  });

  it("does not claim a full page on the last, partial one", () => {
    // 137 rows at 20 per page: the last page holds 17, not 20.
    renderPagination({ page: 7, pageSize: 20, total: 137, totalPages: 7 });
    expect(screen.getByText(/121.*137.*137/)).toBeInTheDocument();
  });

  it("announces the position to assistive technology", () => {
    renderPagination({ page: 3, totalPages: 7 });
    const nav = screen.getByRole("navigation", { name: /pagination/i });
    expect(nav).toHaveAttribute("aria-label", expect.stringMatching(/pagination/i));
    expect(screen.getByText(/page 3 of 7/i)).toBeInTheDocument();
  });
});

describe("what it offers", () => {
  it("does not offer a previous page from the first", () => {
    renderPagination({ page: 1, totalPages: 7 });
    expect(screen.getByRole("button", { name: /previous page/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /first page/i })).toBeDisabled();
  });

  it("does not offer a next page from the last", () => {
    renderPagination({ page: 7, totalPages: 7 });
    expect(screen.getByRole("button", { name: /next page/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /last page/i })).toBeDisabled();
  });

  it("offers both directions in the middle", () => {
    renderPagination({ page: 4, totalPages: 7 });
    expect(screen.getByRole("button", { name: /previous page/i })).toBeEnabled();
    expect(screen.getByRole("button", { name: /next page/i })).toBeEnabled();
  });

  it("renders nothing at all when there is nothing to page through", () => {
    // FR-016: an empty list keeps its own empty state; "page 1 of 0" is not a
    // thing an operator should ever be shown.
    const { container } = renderPagination({ page: 1, total: 0, totalPages: 0 });
    expect(container).toBeEmptyDOMElement();
  });

  it("offers no movement when everything fits on one page", () => {
    renderPagination({ page: 1, total: 12, totalPages: 1 });
    expect(screen.getByRole("button", { name: /next page/i })).toBeDisabled();
    expect(screen.getByRole("button", { name: /previous page/i })).toBeDisabled();
  });

  it("marks the current page for assistive technology", () => {
    renderPagination({ page: 3, totalPages: 7 });
    expect(screen.getByRole("button", { name: "Page 3" })).toHaveAttribute(
      "aria-current",
      "page",
    );
  });
});

describe("what it does", () => {
  it("moves forward and back by one", async () => {
    const onPageChange = vi.fn();
    renderPagination({ page: 4, totalPages: 7, onPageChange });

    await userEvent.click(screen.getByRole("button", { name: /next page/i }));
    expect(onPageChange).toHaveBeenCalledWith(5);

    await userEvent.click(screen.getByRole("button", { name: /previous page/i }));
    expect(onPageChange).toHaveBeenCalledWith(3);
  });

  it("jumps to the first and last page", async () => {
    const onPageChange = vi.fn();
    renderPagination({ page: 4, totalPages: 7, onPageChange });

    await userEvent.click(screen.getByRole("button", { name: /first page/i }));
    expect(onPageChange).toHaveBeenCalledWith(1);

    await userEvent.click(screen.getByRole("button", { name: /last page/i }));
    expect(onPageChange).toHaveBeenCalledWith(7);
  });

  it("jumps to a numbered page", async () => {
    const onPageChange = vi.fn();
    renderPagination({ page: 1, totalPages: 7, onPageChange });

    await userEvent.click(screen.getByRole("button", { name: "Page 3" }));
    expect(onPageChange).toHaveBeenCalledWith(3);
  });

  it("is reachable and operable by keyboard alone", async () => {
    const onPageChange = vi.fn();
    renderPagination({ page: 1, totalPages: 7, onPageChange });

    const next = screen.getByRole("button", { name: /next page/i });
    next.focus();
    expect(next).toHaveFocus();

    await userEvent.keyboard("{Enter}");
    expect(onPageChange).toHaveBeenCalledWith(2);
  });
});

describe("page size", () => {
  it("shows the current size", () => {
    renderPagination({ pageSize: 50 });
    expect(screen.getByLabelText(/rows per page/i)).toBeInTheDocument();
    expect(screen.getByText("50")).toBeInTheDocument();
  });

  it("reports a new size to the caller", async () => {
    const onPageSizeChange = vi.fn();
    renderPagination({ pageSize: 20, onPageSizeChange });

    await userEvent.click(screen.getByLabelText(/rows per page/i));
    await userEvent.click(await screen.findByRole("option", { name: "50" }));

    expect(onPageSizeChange).toHaveBeenCalledWith(50);
  });

  it("offers every size the API accepts, and no more", async () => {
    renderPagination();
    await userEvent.click(screen.getByLabelText(/rows per page/i));

    const options = await screen.findAllByRole("option");
    expect(options.map((o) => o.textContent)).toEqual(["20", "50", "100"]);
  });
});

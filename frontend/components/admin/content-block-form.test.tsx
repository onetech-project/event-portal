import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { EventContentManager } from "@/components/admin/content-block-form";
import { storeToken } from "@/lib/auth";

// The T&C editor drags in tiptap and is exercised by its own test file; the
// icon picker is virtualized and lazy-loaded and is stubbed per spec 009 R7.
vi.mock("@/components/admin/rich-text-editor", () => ({
  RichTextEditor: () => <div data-testid="rich-text-editor" />,
}));

vi.mock("@/components/ui/icon-picker", () => ({
  IconPicker: ({
    onValueChange,
    children,
  }: {
    onValueChange?: (icon: string) => void;
    children?: React.ReactNode;
  }) => (
    <div>
      {children}
      <button type="button" onClick={() => onValueChange?.("anchor")}>
        pick-anchor
      </button>
    </div>
  ),
}));

const EVENT_ID = "55555555-5555-4555-8555-555555555555";

function envelope(data: unknown, status = 200) {
  return new Response(JSON.stringify({ code: 200000, message: "Success", data }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

/** Captures every POST body; answers reads with an authored-terms world. */
function stubApi(activities: unknown[] = []) {
  const posts: { url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = init?.method ?? "GET";
      if (method === "POST") {
        posts.push({ url, body: JSON.parse(String(init?.body)) });
        return envelope({ id: "66666666-6666-4666-8666-666666666666" }, 201);
      }
      if (url.includes("terms")) {
        return envelope({ id: "t", content: "<p>rules</p>", updated_at: null });
      }
      if (url.includes("/activities")) {
        return envelope(activities);
      }
      return envelope([]);
    }),
  );
  return posts;
}

function renderManager() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <EventContentManager eventId={EVENT_ID} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  storeToken("test-token", new Date(Date.now() + 3_600_000).toISOString());
});

describe("EventContentManager icon submission", () => {
  it("submits icon: null when no icon is chosen", async () => {
    const posts = stubApi();
    const user = userEvent.setup();
    renderManager();

    const form = await screen.findByRole("form", { name: /add activity/i });
    await user.type(within(form).getByLabelText(/title/i), "Harbor Tour");
    await user.type(within(form).getByLabelText(/description/i), "By the docks.");

    // Dispatched directly: clicking a submit button does not reach React's
    // onSubmit under happy-dom, though it does in a real browser.
    fireEvent.submit(form);

    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0].url).toContain(`/admin/events/${EVENT_ID}/activities`);
    expect(posts[0].body).toMatchObject({ title: "Harbor Tour", icon: null });
  });

  it("submits the picked catalog key verbatim", async () => {
    const posts = stubApi();
    const user = userEvent.setup();
    renderManager();

    const form = await screen.findByRole("form", { name: /add activity/i });
    await user.type(within(form).getByLabelText(/title/i), "Harbor Tour");
    await user.type(within(form).getByLabelText(/description/i), "By the docks.");

    await user.click(within(form).getByRole("button", { name: "Icon: none" }));
    await user.click(await within(form).findByText("pick-anchor"));

    fireEvent.submit(form);

    await waitFor(() => expect(posts).toHaveLength(1));
    expect(posts[0].body).toMatchObject({ icon: "anchor" });
  });

  // US3 (FR-009): the admin list shows the real glyph, not the raw key text.
  it("renders stored icons in the block list as glyphs, not key suffixes", async () => {
    stubApi([
      {
        id: "77777777-7777-4777-8777-777777777777",
        title: "Live Music",
        description: "Main stage.",
        icon: "music",
        position: 1,
      },
      {
        id: "88888888-8888-4888-8888-888888888888",
        title: "Quiet Zone",
        description: "No performances.",
        icon: null,
        position: 2,
      },
    ]);
    renderManager();

    const withIcon = (await screen.findByText("Live Music")).closest("li")!;
    // DynamicIcon resolves its per-icon chunk asynchronously.
    await waitFor(() => expect(withIcon.querySelector("svg")).toBeInTheDocument());
    expect(withIcon.textContent).not.toContain("(music)");

    const withoutIcon = screen.getByText("Quiet Zone").closest("li")!;
    expect(withoutIcon.querySelector("svg")).not.toBeInTheDocument();
  });
});

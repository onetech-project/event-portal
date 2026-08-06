import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it, vi } from "vitest";

import { RichTextEditor } from "./rich-text-editor";

function Harness({ initial = "<p>Hello</p>", onChange = () => {} }: {
  initial?: string;
  onChange?: (html: string) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <RichTextEditor
      value={value}
      onChange={(html) => {
        setValue(html);
        onChange(html);
      }}
    />
  );
}

describe("RichTextEditor", () => {
  it("renders the formatting toolbar and the initial HTML", async () => {
    render(<Harness />);

    await waitFor(() => expect(screen.getByRole("toolbar")).toBeInTheDocument());
    for (const label of ["Bold", "Italic", "Heading 2", "Bullet list", "Numbered list", "Link"]) {
      expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    }
    expect(await screen.findByText("Hello")).toBeInTheDocument();
  });

  it("emits HTML when a mark is toggled", async () => {
    const onChange = vi.fn();
    render(<Harness onChange={onChange} />);
    await waitFor(() => expect(screen.getByRole("toolbar")).toBeInTheDocument());

    // Select-all inside the editable region, then bold it.
    const editable = document.querySelector('[contenteditable="true"]');
    expect(editable).not.toBeNull();
    await userEvent.click(editable as HTMLElement);
    await userEvent.keyboard("{Control>}a{/Control}");
    await userEvent.click(screen.getByRole("button", { name: "Bold" }));

    await waitFor(() => {
      expect(onChange).toHaveBeenCalled();
      const html = onChange.mock.calls.at(-1)?.[0] as string;
      expect(html).toContain("<strong>");
    });
  });
});

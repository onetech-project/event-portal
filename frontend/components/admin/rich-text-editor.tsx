"use client";

import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import Link from "@tiptap/extension-link";
import StarterKit from "@tiptap/starter-kit";
import {
  Bold,
  Heading2,
  Heading3,
  Italic,
  Link as LinkIcon,
  List,
  ListOrdered,
} from "lucide-react";
import { useEffect } from "react";

/**
 * The admin WYSIWYG editor (spec 008 US4, research: TipTap MIT StarterKit only
 * — no Cloud/Pro extensions). Value in and out is an HTML string; the server
 * sanitizes on write, so this component stays a plain editing surface.
 *
 * The toolbar is built from ordinary buttons styled with the app's tokens —
 * no TipTap UI package.
 */
export function RichTextEditor({
  value,
  onChange,
  ariaLabel = "Rich text editor",
}: {
  value: string;
  onChange: (html: string) => void;
  ariaLabel?: string;
}) {
  const editor = useEditor({
    extensions: [
      StarterKit,
      Link.configure({
        openOnClick: false,
        autolink: true,
        defaultProtocol: "https",
      }),
    ],
    content: value,
    // SSR-safe: render only after mount (TipTap v3 requirement in the App Router).
    immediatelyRender: false,
    editorProps: {
      attributes: {
        "aria-label": ariaLabel,
        class:
          "min-h-40 rounded-b-lg border border-t-0 px-3 py-2 text-sm focus:outline-none " +
          "[&_ol]:list-decimal [&_ol]:pl-6 [&_ul]:list-disc [&_ul]:pl-6 " +
          "[&_h2]:text-xl [&_h2]:font-semibold [&_h3]:text-lg [&_h3]:font-semibold " +
          "[&_a]:underline [&_p]:mb-2",
      },
    },
    onUpdate: ({ editor: current }) => onChange(current.getHTML()),
  });

  // An outside value change (loading a saved document) replaces the content;
  // emitUpdate false so it does not echo back through onChange.
  useEffect(() => {
    if (editor !== null && value !== editor.getHTML()) {
      editor.commands.setContent(value, { emitUpdate: false });
    }
  }, [value, editor]);

  return (
    <div>
      <Toolbar editor={editor} />
      <EditorContent editor={editor} />
    </div>
  );
}

function Toolbar({ editor }: { editor: Editor | null }) {
  if (editor === null) return null;

  const items = [
    {
      label: "Bold",
      icon: Bold,
      active: editor.isActive("bold"),
      run: () => editor.chain().focus().toggleBold().run(),
    },
    {
      label: "Italic",
      icon: Italic,
      active: editor.isActive("italic"),
      run: () => editor.chain().focus().toggleItalic().run(),
    },
    {
      label: "Heading 2",
      icon: Heading2,
      active: editor.isActive("heading", { level: 2 }),
      run: () => editor.chain().focus().toggleHeading({ level: 2 }).run(),
    },
    {
      label: "Heading 3",
      icon: Heading3,
      active: editor.isActive("heading", { level: 3 }),
      run: () => editor.chain().focus().toggleHeading({ level: 3 }).run(),
    },
    {
      label: "Bullet list",
      icon: List,
      active: editor.isActive("bulletList"),
      run: () => editor.chain().focus().toggleBulletList().run(),
    },
    {
      label: "Numbered list",
      icon: ListOrdered,
      active: editor.isActive("orderedList"),
      run: () => editor.chain().focus().toggleOrderedList().run(),
    },
    {
      label: "Link",
      icon: LinkIcon,
      active: editor.isActive("link"),
      run: () => {
        if (editor.isActive("link")) {
          editor.chain().focus().unsetLink().run();
          return;
        }
        // A prompt keeps the toolbar dependency-free; the admin pastes a URL.
        const url = window.prompt("Link URL");
        if (url) {
          editor.chain().focus().setLink({ href: url }).run();
        }
      },
    },
  ];

  return (
    <div role="toolbar" aria-label="Formatting" className="flex flex-wrap gap-1 rounded-t-lg border bg-muted/40 p-1">
      {items.map((item) => (
        <button
          key={item.label}
          type="button"
          aria-label={item.label}
          aria-pressed={item.active}
          onClick={item.run}
          className={`flex size-8 items-center justify-center rounded-md text-sm transition-colors hover:bg-muted ${
            item.active ? "bg-muted text-foreground" : "text-muted-foreground"
          }`}
        >
          <item.icon aria-hidden className="size-4" />
        </button>
      ))}
    </div>
  );
}

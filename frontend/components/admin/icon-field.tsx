"use client";

import dynamic from "next/dynamic";
import { useState } from "react";

import { X } from "lucide-react";

import { ContentIcon } from "@/components/event/content-icon";
import { Button } from "@/components/ui/button";
import { Field } from "@/components/ui/field";
import type { IconName } from "@/components/ui/icon-picker";

// The picker drags in the full icon metadata catalog and fuzzy search, so it
// stays out of the admin page bundle (spec 009 R5): nothing loads until the
// admin first clicks the trigger, at which point the real picker mounts
// already open.
const IconPicker = dynamic(
  () => import("@/components/ui/icon-picker").then((m) => ({ default: m.IconPicker })),
  {
    ssr: false,
    loading: () => (
      <Button type="button" variant="outline" className="flex-1 justify-start font-normal" disabled>
        Loading icons…
      </Button>
    ),
  },
);

function TriggerContent({ value }: { value: string }) {
  if (value === "") return <>No icon</>;
  return (
    <>
      <ContentIcon name={value} className="size-4" />
      {value}
    </>
  );
}

/**
 * The icon control for CMS content-block forms: a visual picker over the full
 * catalog plus the explicit clear affordance the picker itself lacks (FR-004).
 * Value contract matches the old IconSelect: "" means no icon; the form maps
 * it to null on submit.
 */
export function IconField({
  value,
  onChange,
  label,
}: {
  value: string;
  onChange: (icon: string) => void;
  label: string;
}) {
  const [activated, setActivated] = useState(false);

  const triggerClass = "flex-1 justify-start gap-2 font-normal";
  // The Field's <label> would otherwise name every button in it just "Icon";
  // an explicit name keeps the current value announced (FR-010).
  const triggerLabel = value === "" ? "Icon: none" : `Icon: ${value}`;

  return (
    <Field label={label}>
      <div className="flex items-center gap-2">
        {activated ? (
          <IconPicker
            value={value === "" ? undefined : (value as IconName)}
            onValueChange={(icon) => onChange(icon)}
            defaultOpen
          >
            <Button type="button" variant="outline" aria-label={triggerLabel} className={triggerClass}>
              <TriggerContent value={value} />
            </Button>
          </IconPicker>
        ) : (
          <Button
            type="button"
            variant="outline"
            aria-label={triggerLabel}
            className={triggerClass}
            onClick={() => setActivated(true)}
          >
            <TriggerContent value={value} />
          </Button>
        )}

        {value !== "" ? (
          <Button
            type="button"
            variant="outline"
            size="icon"
            aria-label="Remove icon"
            onClick={() => onChange("")}
          >
            <X aria-hidden className="size-4" />
          </Button>
        ) : null}
      </div>
    </Field>
  );
}

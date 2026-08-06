"use client";

import { useState } from "react";

import { IconField } from "@/components/admin/icon-field";
import { RichTextEditor } from "@/components/admin/rich-text-editor";
import { ContentIcon } from "@/components/event/content-icon";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Loading, StatusAlert } from "@/components/ui/feedback";
import { Input } from "@/components/ui/input";
import { ApiError } from "@/lib/api-client";
import {
  useAdminContentBlocks,
  useAdminEventTerms,
  useCreateContentBlock,
  useDeleteContentBlock,
  useUpdateContentBlock,
  useUpsertEventTerms,
  type ContentBlockKind,
} from "@/lib/queries";
import {
  type ActivityBlock,
  type GuestStarBlock,
  type GuidelineBlock,
} from "@/lib/types";

/**
 * The admin CMS content surface for one event (spec 008 US4): the T&C
 * document (WYSIWYG) and the three content-block collections shown on the
 * guest detail page. Icons come from the fixed documented set — a key outside
 * it earns a 400004 from the server, so the picker only offers valid ones.
 */
export function EventContentManager({ eventId }: { eventId: string }) {
  return (
    <div className="space-y-6">
      <TermsEditorCard eventId={eventId} />
      <ActivitiesCard eventId={eventId} />
      <GuestStarsCard eventId={eventId} />
      <GuidelinesCard eventId={eventId} />
    </div>
  );
}

// --- Terms -------------------------------------------------------------------

function TermsEditorCard({ eventId }: { eventId: string }) {
  const terms = useAdminEventTerms(eventId);
  const upsert = useUpsertEventTerms(eventId);
  const [draft, setDraft] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  const notAuthored =
    terms.error instanceof ApiError && terms.error.status === 404;

  if (terms.isPending) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Terms &amp; Conditions</CardTitle>
        </CardHeader>
        <CardContent>
          <Loading label="Loading terms…" />
        </CardContent>
      </Card>
    );
  }

  const value = draft ?? terms.data?.content ?? "";

  return (
    <Card>
      <CardHeader>
        <CardTitle>Terms &amp; Conditions</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {notAuthored && draft === null ? (
          <StatusAlert tone="info">
            No Terms &amp; Conditions yet. Guests cannot book this event until
            they are published here.
          </StatusAlert>
        ) : null}
        {terms.error && !notAuthored ? (
          <StatusAlert>{terms.error.message}</StatusAlert>
        ) : null}
        {upsert.error ? <StatusAlert>{upsert.error.message}</StatusAlert> : null}
        {saved && !upsert.error ? (
          <StatusAlert tone="success">Terms saved.</StatusAlert>
        ) : null}

        <RichTextEditor
          value={value}
          onChange={(html) => {
            setDraft(html);
            setSaved(false);
          }}
          ariaLabel="Terms and conditions editor"
        />

        <Button
          onClick={() =>
            upsert.mutate(value, { onSuccess: () => setSaved(true) })
          }
          disabled={upsert.isPending || value === ""}
        >
          {upsert.isPending ? "Saving…" : "Save terms"}
        </Button>
      </CardContent>
    </Card>
  );
}

// --- Shared pieces -----------------------------------------------------------

function useBlockMutations(eventId: string, kind: ContentBlockKind) {
  return {
    create: useCreateContentBlock(eventId, kind),
    update: useUpdateContentBlock(eventId, kind),
    remove: useDeleteContentBlock(eventId, kind),
  };
}

function MutationErrors({
  errors,
}: {
  errors: (Error | null)[];
}) {
  const first = errors.find((e) => e !== null);
  if (first === undefined || first === null) return null;
  return <StatusAlert>{first.message}</StatusAlert>;
}

// --- Activities --------------------------------------------------------------

function ActivitiesCard({ eventId }: { eventId: string }) {
  const list = useAdminContentBlocks<ActivityBlock>(eventId, "activities");
  const { create, update, remove } = useBlockMutations(eventId, "activities");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [icon, setIcon] = useState("");

  return (
    <Card>
      <CardHeader>
        <CardTitle>Activities</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <MutationErrors errors={[create.error, update.error, remove.error]} />

        {list.isPending ? <Loading label="Loading activities…" /> : null}

        <ul className="space-y-2">
          {list.data?.map((block) => (
            <li key={block.id} className="flex items-start justify-between gap-3 rounded-lg border p-3 text-sm">
              <div>
                <p className="flex items-center gap-2 font-medium">
                  <ContentIcon name={block.icon} className="size-4 shrink-0 text-muted-foreground" />
                  {block.title}
                </p>
                <p className="text-muted-foreground">{block.description}</p>
              </div>
              <div className="flex shrink-0 gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() =>
                    update.mutate({
                      id: block.id,
                      body: {
                        title: block.title,
                        description: block.description,
                        icon: block.icon,
                        position: block.position - 1,
                      },
                    })
                  }
                >
                  Move up
                </Button>
                <Button size="sm" variant="outline" onClick={() => remove.mutate(block.id)}>
                  Delete
                </Button>
              </div>
            </li>
          ))}
        </ul>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(
              {
                title,
                description,
                icon: icon === "" ? null : icon,
                position: (list.data?.length ?? 0) + 1,
              },
              {
                onSuccess: () => {
                  setTitle("");
                  setDescription("");
                  setIcon("");
                },
              },
            );
          }}
          aria-label="Add activity"
          className="grid gap-3 sm:grid-cols-3"
        >
          <Field label="Title">
            <Input value={title} onChange={(e) => setTitle(e.target.value)} />
          </Field>
          <Field label="Description">
            <Input value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <IconField value={icon} onChange={setIcon} label="Icon" />
          <div className="sm:col-span-3">
            <Button type="submit" disabled={create.isPending || title === "" || description === ""}>
              Add activity
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

// --- Guest stars -------------------------------------------------------------

function GuestStarsCard({ eventId }: { eventId: string }) {
  const list = useAdminContentBlocks<GuestStarBlock>(eventId, "guest-stars");
  const { create, remove, update } = useBlockMutations(eventId, "guest-stars");
  const [name, setName] = useState("");

  return (
    <Card>
      <CardHeader>
        <CardTitle>Guest stars</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <MutationErrors errors={[create.error, update.error, remove.error]} />
        {list.isPending ? <Loading label="Loading guest stars…" /> : null}

        <ul className="space-y-2">
          {list.data?.map((block) => (
            <li key={block.id} className="flex items-center justify-between gap-3 rounded-lg border p-3 text-sm">
              <span className="font-medium">{block.name}</span>
              <Button size="sm" variant="outline" onClick={() => remove.mutate(block.id)}>
                Delete
              </Button>
            </li>
          ))}
        </ul>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(
              { name, position: (list.data?.length ?? 0) + 1 },
              { onSuccess: () => setName("") },
            );
          }}
          aria-label="Add guest star"
          className="flex items-end gap-3"
        >
          <div className="flex-1">
            <Field label="Name">
              <Input value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
          </div>
          <Button type="submit" disabled={create.isPending || name === ""}>
            Add guest star
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}

// --- Guidelines --------------------------------------------------------------

function GuidelinesCard({ eventId }: { eventId: string }) {
  const list = useAdminContentBlocks<GuidelineBlock>(eventId, "guidelines");
  const { create, remove, update } = useBlockMutations(eventId, "guidelines");
  const [description, setDescription] = useState("");
  const [icon, setIcon] = useState("");

  return (
    <Card>
      <CardHeader>
        <CardTitle>Guidelines</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <MutationErrors errors={[create.error, update.error, remove.error]} />
        {list.isPending ? <Loading label="Loading guidelines…" /> : null}

        <ul className="space-y-2">
          {list.data?.map((block) => (
            <li key={block.id} className="flex items-center justify-between gap-3 rounded-lg border p-3 text-sm">
              <span className="flex items-center gap-2">
                <ContentIcon name={block.icon} className="size-4 shrink-0 text-muted-foreground" />
                {block.description}
              </span>
              <Button size="sm" variant="outline" onClick={() => remove.mutate(block.id)}>
                Delete
              </Button>
            </li>
          ))}
        </ul>

        <form
          onSubmit={(e) => {
            e.preventDefault();
            create.mutate(
              {
                description,
                icon: icon === "" ? null : icon,
                position: (list.data?.length ?? 0) + 1,
              },
              {
                onSuccess: () => {
                  setDescription("");
                  setIcon("");
                },
              },
            );
          }}
          aria-label="Add guideline"
          className="grid gap-3 sm:grid-cols-2"
        >
          <Field label="Description">
            <Input value={description} onChange={(e) => setDescription(e.target.value)} />
          </Field>
          <IconField value={icon} onChange={setIcon} label="Icon" />
          <div className="sm:col-span-2">
            <Button type="submit" disabled={create.isPending || description === ""}>
              Add guideline
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

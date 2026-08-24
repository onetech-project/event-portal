"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { XIcon } from "lucide-react";
import { use, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";

import { HolderFields } from "@/components/order/holder-fields";
import { TermsDialogShell } from "@/components/terms/terms-dialog-shell";
import { useTermsAgreement } from "@/components/terms/terms-viewer";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
} from "@/components/ui/dialog";
import { StatusAlert } from "@/components/ui/feedback";
import { API_CODES, ApiError } from "@/lib/api-client";
import {
  dobSchema,
  dobToIso,
  genderLabel,
  genderSchema,
  holderEmailSchema,
  holderNameSchema,
  phoneSchema,
} from "@/lib/holder-fields";
import { useRegistrationPrereqs, useSubmitRegistration } from "@/lib/queries";
import { useRouter } from "next/navigation";
import { Lock } from "lucide-react";

/**
 * Free ticket registration (spec 022).
 *
 * One holder, one ticket, no payment and no price anywhere on the surface
 * (FR-015). The guest fills the form, reads the event's Terms & Conditions
 * THROUGH TO THE END and accepts them, reviews exactly what they typed, and
 * confirms.
 *
 * Every gate this page applies is re-applied by the server. Hiding a form is not
 * enforcement: the submit endpoint refuses a caller who never loaded this page
 * (FR-024, US3 scenario 2), and the two must not disagree.
 */
const registrationSchema = z.object({
  name: holderNameSchema,
  email: holderEmailSchema,
  phone: phoneSchema,
  dob: dobSchema,
  gender: genderSchema,
});

type RegistrationValues = z.infer<typeof registrationSchema>;

export default function RegistrationPage({
  params,
}: {
  params: Promise<{ slug: string; ticketId: string }>;
}) {
  const { slug, ticketId } = use(params);
  return <RegistrationView slug={slug} ticketId={ticketId} />;
}

/**
 * The form itself, separated from route-param unwrapping so it can be rendered
 * directly in tests — the same split `CheckoutView` already uses. `use(params)`
 * suspends, which a component test would otherwise have to stage around.
 */
export function RegistrationView({
  slug,
  ticketId,
}: {
  slug: string;
  ticketId: string;
}) {
  const router = useRouter();

  const prereqs = useRegistrationPrereqs(slug, ticketId);
  const submit = useSubmitRegistration(ticketId);

  const [termsOpen, setTermsOpen] = useState(false);
  const [reviewOpen, setReviewOpen] = useState(false);
  const [agreed, setAgreed] = useState(false);
  // The VERSION the guest actually read, captured at Accept. Not the id: an
  // admin edit overwrites the terms row in place and preserves its id, so an id
  // could never report that the document changed underneath them.
  const [acceptedVersion, setAcceptedVersion] = useState<string | null>(null);

  // The MODAL's agreement, which is not the form's. `agreed` above is the form
  // checkbox — the outcome of a completed acceptance, governed by FR-014a. This
  // one is the box inside the dialog, which the guest may tick at any moment
  // (FR-045) and which resets every time the dialog closes.
  const {
    agreed: modalAgreed,
    setAgreed: setModalAgreed,
    onReachedEnd,
  } = useTermsAgreement(termsOpen);

  const form = useForm<RegistrationValues>({
    resolver: zodResolver(registrationSchema),
    mode: "onTouched",
    defaultValues: { name: "", email: "", phone: "", dob: "", gender: "" },
  });

  // FR-011/FR-012: every reason this registration might be unavailable — unknown
  // ticket type, a purchasable one, another event's, a closed window, no places
  // left — arrives as ONE refusal and is rendered as one panel and one sentence.
  // A per-condition branch here would rebuild the enumeration oracle the server
  // deliberately collapsed.
  if (prereqs.isError) {
    const noTerms =
      prereqs.error instanceof ApiError &&
      prereqs.error.code === API_CODES.termsMissing;
    return (
      <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
        <Card>
          <CardContent className="py-10 text-center">
            <h1 className="text-xl font-bold">Registration unavailable</h1>
            <p className="mt-2 text-sm text-muted-foreground">
              {noTerms
                ? "This event's Terms & Conditions are not ready yet. Please try again later."
                : "This registration is not available."}
            </p>
          </CardContent>
        </Card>
      </main>
    );
  }

  if (prereqs.isPending) {
    return (
      <main className="mx-auto w-full max-w-2xl flex-1 px-6 py-10">
        <p className="text-sm text-muted-foreground text-center">Loading…</p>
      </main>
    );
  }

  const data = prereqs.data;
  const values = form.getValues();

  function handleAccept() {
    setAgreed(true);
    setAcceptedVersion(data.event_terms_updated_at);
    setTermsOpen(false);
  }

  // FR-014a: interacting with the UNCHECKED control opens the document rather
  // than ticking the box. Consent is the act of reading, not of clicking.
  function handleAgreementToggle(next: boolean) {
    if (next) {
      setTermsOpen(true);
      return;
    }
    // FR-014f: clearing withdraws consent. Re-consenting requires the document
    // to be OPENED again — the `next` branch above does that — but not read.
    setAgreed(false);
    setAcceptedVersion(null);
  }

  async function handleConfirm() {
    if (acceptedVersion === null) return;
    try {
      await submit.mutateAsync({
        slug,
        name: values.name.trim(),
        email: values.email.trim(),
        phone: values.phone.trim(),
        dob: dobToIso(values.dob) ?? values.dob,
        gender: values.gender,
        agreed: true,
        event_terms_updated_at: acceptedVersion,
      });
      router.push(
        `/events/${encodeURIComponent(slug)}/register/${encodeURIComponent(ticketId)}/success`,
      );
    } catch (error) {
      setReviewOpen(false);
      // FR-051: the document moved under them. Clear the agreement so a
      // superseded acceptance cannot be resubmitted by retrying, and RE-PRESENT
      // the current document without waiting to be asked.
      //
      // Opening it ourselves is deliberate and is the one place this feature
      // drives the dialog rather than the guest (clarified 2026-08-21): they did
      // not choose to lose their consent — an admin edit voided it — so they are
      // shown what changed rather than left to work out why the box emptied.
      // They still need not read it; ticking and accepting again is enough.
      if (error instanceof ApiError && error.code === API_CODES.termsChanged) {
        setAgreed(false);
        setAcceptedVersion(null);
        void prereqs.refetch();
        setTermsOpen(true);
      }
      // Everything else stays visible through the mutation's error state.
    }
  }

  const canReview = form.formState.isValid && agreed && !submit.isPending;

  return (
    <main className="flex flex-col justify-center items-center mx-auto w-full max-w-2xl flex-1 px-6 py-10">
      <Card>
        <CardContent className="space-y-6 py-8">
          <header className="space-y-2 text-center">
            <h1 className="text-xl font-bold">
              Exclusive Invitation Registration
            </h1>
            <p className="text-sm text-muted-foreground">
              For invited guests of {data.event.name} only. Please fill in your
              details to register your invitation. Your complimentary invitation
              e-ticket will be sent via email.
            </p>
          </header>

          {submit.isError ? (
            <StatusAlert>{submissionMessage(submit.error)}</StatusAlert>
          ) : null}

          <form
            aria-label="Invitation registration"
            className="space-y-4"
            onSubmit={(e) => {
              e.preventDefault();
              if (canReview) setReviewOpen(true);
            }}
          >
            {/*
              The SAME holder fields the booking flow renders (spec 022,
              clarified 2026-08-20). Sharing the markup — not just the validators
              in lib/holder-fields.ts — is what stops the two surfaces drifting:
              a label, placeholder or field order corrected on one used to stay
              wrong on the other, silently.

              The registration form is one holder, so the paths are flat where
              booking's are `attendees.N.*`.
            */}
            <div className="grid gap-4 sm:grid-cols-2">
              <HolderFields
                control={form.control}
                fields={{
                  name: "name",
                  email: "email",
                  phone: "phone",
                  gender: "gender",
                  dob: "dob",
                }}
                errors={{
                  name: form.formState.errors.name?.message,
                  email: form.formState.errors.email?.message,
                  phone: form.formState.errors.phone?.message,
                  gender: form.formState.errors.gender?.message,
                  dob: form.formState.errors.dob?.message,
                }}
                genders={data.genders}
              />
            </div>

            <label className="flex items-center gap-2 text-sm cursor-pointer">
              <Checkbox
                checked={agreed}
                onCheckedChange={handleAgreementToggle}
              />
              <span>
                I agree to the{" "}
                <button
                  type="button"
                  className="font-medium text-brand underline"
                  onClick={() => setTermsOpen(true)}
                >
                  Terms &amp; Conditions
                </button>
                .
              </span>
            </label>

            <Button
              size="lg"
              type="submit"
              disabled={!canReview}
              className="w-full h-10 bg-brand hover:bg-brand/90 text-brand-foreground transition-all disabled:pointer-events-none"
            >
              Confirm Registration
            </Button>

            <p className="flex items-center justify-center gap-1.5 text-[10px] font-medium uppercase tracking-wide text-muted-foreground">
              <Lock aria-hidden className="size-3.5" /> Secure checkout
            </p>
          </form>
        </CardContent>
      </Card>

      {/*
        The Terms & Conditions, gated on reading them through (FR-014b-e).

        The SAME dialog shell the booking flow renders (clarified 2026-08-20), so
        the two surfaces cannot differ in width, padding, header spacing or the
        rule above the actions. What is still this page's own is the footer: a
        single Accept, because a registration has no order to hold at this moment.
      */}
      <TermsDialogShell
        open={termsOpen}
        onOpenChange={setTermsOpen}
        eventSlug={slug}
        eventName={data.event.name}
        agreed={modalAgreed}
        onAgreedChange={setModalAgreed}
        onReachedEnd={onReachedEnd}
        action={{
          // Identical footer to the booking dialog by construction — checkbox,
          // Cancel, Agree. Only the handler differs: this records the acceptance
          // and closes, where booking also places the order.
          label: "Agree",
          onClick: handleAccept,
        }}
      />

      {/*
        FR-017: show back exactly what was typed, before anything is recorded.

        Laid out against Figma node 764:488 (2026-08-21). The dialog's own close
        button is suppressed and re-rendered inside the header row, because the
        design puts the title and the dismiss on one ruled line — the default
        `absolute top-2 right-2` cannot sit on that line and clears the rule
        instead of centring against it.
      */}
      <Dialog open={reviewOpen} onOpenChange={setReviewOpen}>
        <DialogContent
          showCloseButton={false}
          className="gap-0 rounded-xl p-0 sm:max-w-md max-h-[calc(100dvh-6rem)]"
        >
          <div className="flex items-center justify-between border-b px-6 py-4">
            <DialogTitle className="leading-normal font-bold">
              Registration Details
            </DialogTitle>
            <DialogClose
              aria-label="Close"
              className="flex size-8 items-center justify-center rounded-md text-muted-foreground transition-colors hover:text-foreground"
            >
              <XIcon aria-hidden className="size-3.5" />
            </DialogClose>
          </div>

          <div className="p-8 flex flex-col gap-3">
            <SectionHeading>Guest Details</SectionHeading>
            <dl className="flex flex-col gap-3">
              <Row label="Full Name" value={values.name} />
              <Row label="Email" value={values.email} />
              <Row label="Phone Number" value={values.phone} />
              <Row label="Gender" value={genderLabel(values.gender)} />
              <Row label="Date of Birth" value={values.dob} />
            </dl>

            <div className="flex flex-col gap-2.5 pt-6">
              <Button
                type="button"
                className="h-11 w-full rounded-lg bg-brand text-sm font-semibold text-brand-foreground transition-all hover:bg-brand/90"
                onClick={handleConfirm}
                disabled={submit.isPending}
              >
                {submit.isPending ? "Submitting…" : "Confirm & Submit"}
              </Button>
              {/*
                A caption, NOT a control. It was previously a `DialogClose`, which
                made this sentence the dismiss affordance and gave the close
                action an accessible name describing email delivery. The design
                shows plain text under the button, and the × in the header is the
                only way out other than Cancel.
              */}
              <p className="text-center text-xs leading-4 text-muted-foreground">
                The information will be sent via email
              </p>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </main>
  );
}

/** A section label inside the review dialog — 10px, uppercase, widely tracked. */
function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="text-[10px] font-bold tracking-[1px] text-muted-foreground/80 uppercase">
      {children}
    </h3>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-4">
      <dt className="font-medium text-muted-foreground">{label}</dt>
      <dd className="text-right font-semibold">{value}</dd>
    </div>
  );
}

/**
 * Words a submission failure.
 *
 * The duplicate-email refusal is worded here rather than shown as a field error:
 * the address is well-formed, so marking the input invalid would tell the guest
 * to fix a typo that does not exist.
 */
function submissionMessage(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === API_CODES.termsChanged) {
      return "The Terms & Conditions were updated. Please review the new version and agree again.";
    }
    if (error.code === API_CODES.rateLimited) {
      return "Too many attempts. Please wait a moment and try again.";
    }
    const message = error.message?.trim();
    if (message) return message;
  }
  return "Something went wrong. Please try again.";
}

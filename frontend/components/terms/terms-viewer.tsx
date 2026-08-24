"use client";

import { useCallback, useEffect, useRef, useState } from "react";

import { API_CODES, ApiError } from "@/lib/api-client";
import { useEventTerms } from "@/lib/queries";

/**
 * The event's Terms & Conditions, plus the end-of-document signal (spec 022).
 *
 * ONE component serves both surfaces — the booking dialog and the free
 * registration form — so the two cannot drift apart in wording, in behaviour, or
 * in what agreeing is taken to mean (FR-043).
 *
 * This component deliberately owns NOTHING about consent. It renders the
 * document and reports, exactly once per opening, that the reader reached the
 * end. What that fact authorises is the caller's business — and that separation
 * is why this file needed no change when the read-through GATE was withdrawn on
 * 2026-08-21. What moved was the meaning of the signal, from "unlock the Agree
 * button" to "tick the checkbox for them"; the signal itself did not move.
 */
export function TermsViewer({
  eventSlug,
  eventName,
  enabled = true,
  onReachedEnd,
}: {
  /** What `useEventTerms` fetches by — the event's public identifier. */
  eventSlug: string;
  eventName: string;
  /** Gates the query, the same way `useEventTerms(slug, open)` already does. */
  enabled?: boolean;
  /** Fires ONCE, when the read-through condition is first satisfied. */
  onReachedEnd: () => void;
}) {
  const terms = useEventTerms(eventSlug, enabled);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  // Latched in a ref, not state: this must fire exactly once, and a ref cannot
  // be lost to a render between the observer firing and the state committing.
  const firedRef = useRef(false);

  const fire = useCallback(() => {
    if (firedRef.current) return;
    firedRef.current = true;
    onReachedEnd();
  }, [onReachedEnd]);

  // Re-arm the latch when the surface closes.
  //
  // Without this the automatic tick never fires again on a SECOND opening
  // whenever the host keeps this component mounted: useTermsAgreement resets
  // `agreed` to false, but `firedRef` would still be true, so nothing would ever
  // call onReachedEnd again. A ref mutation, not setState, so no cascading render.
  useEffect(() => {
    if (!enabled) firedRef.current = false;
  }, [enabled]);

  /**
   * Has the reader reached the end?
   *
   * TOLERANCE is non-zero on purpose (FR-047). Fractional device pixel ratios,
   * browser zoom and sub-pixel layout routinely leave scrollTop + clientHeight a
   * fraction short of scrollHeight, so exact equality would refuse a guest who
   * has visibly reached the bottom — permanently, since there is nothing further
   * to scroll.
   */
  const evaluate = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const TOLERANCE = 4;
    // Nothing to scroll: the document is shorter than its reading area, so it
    // has been read by being shown (FR-014d). Without this a short document
    // would make acceptance impossible.
    if (el.scrollHeight <= el.clientHeight + TOLERANCE) {
      fire();
      return;
    }
    if (el.scrollTop + el.clientHeight >= el.scrollHeight - TOLERANCE) {
      fire();
    }
  }, [fire]);

  // The PRIMARY mechanism is the sentinel, not the scroll handler.
  //
  // FR-046 requires the gate to open for every way a person reads: pointer
  // scrolling, PageDown and End, and assistive technology. An IntersectionObserver
  // on a trailing sentinel satisfies all of those, because it reports that the end
  // of the document became visible however that happened. A scroll listener alone
  // would make this a pointer-only affordance — a screen-reader user who read the
  // whole document might never generate a scroll event, and could never consent.
  //
  // The scroll and resize handlers below are a secondary trigger for the cases the
  // observer does not cover, not the other way round.
  useEffect(() => {
    if (!terms.isSuccess) return;
    const root = scrollRef.current;
    const sentinel = sentinelRef.current;
    if (!root || !sentinel) return;

    // Content just rendered: a short document is already "read" and must not wait
    // for an event that will never come.
    evaluate();

    if (typeof IntersectionObserver === "undefined") return;
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) fire();
      },
      { root, threshold: 0 },
    );
    io.observe(sentinel);
    return () => io.disconnect();
  }, [terms.isSuccess, evaluate, fire]);

  // Re-evaluate when the reading area changes size (FR-047): a window enlarged
  // after the end was reached must not un-satisfy the gate, and one shrunk must
  // be re-measured rather than left stale.
  useEffect(() => {
    if (!terms.isSuccess) return;
    const el = scrollRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => evaluate());
    ro.observe(el);
    return () => ro.disconnect();
  }, [terms.isSuccess, evaluate]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="pb-3">
        <p className="text-base leading-6 text-terms-heading">
          Terms &amp; Conditions for:
        </p>
        <p className="text-base leading-6 font-bold text-terms-heading">{eventName}</p>
      </div>

      <hr className="border-t" />

      {/*
        tabIndex={0} is load-bearing, not decoration. A plain overflow-y-auto div
        cannot receive focus, so PageDown and End never reach it and a
        keyboard-only guest can never satisfy the gate — FR-046 would fail
        outright while the feature looked finished to a mouse user.
      */}
      <div
        ref={scrollRef}
        onScroll={evaluate}
        tabIndex={0}
        role="region"
        aria-label={`Terms and Conditions for ${eventName}`}
        className="min-h-0 flex-1 overflow-y-auto py-3.5 text-sm leading-5 text-terms-ink focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring"
      >
        {terms.isPending ? (
          <p className="text-muted-foreground">Loading terms…</p>
        ) : terms.isError ? (
          <p className="text-muted-foreground">
            {terms.error instanceof ApiError &&
            terms.error.code === API_CODES.termsNotAuthored
              ? "This event's Terms & Conditions are not available yet. Please try again later."
              : "We could not load the Terms & Conditions. Please close this dialog and try again."}
          </p>
        ) : (
          <>
            {/* Sanitized server-side on write (bluemonday); rendered verbatim. */}
            <div
              className="terms-content [&_ol]:list-decimal [&_ol]:space-y-3.5 [&_ol]:pl-10 [&_ul]:list-disc [&_ul]:pl-6 [&_p]:pb-3.5"
              dangerouslySetInnerHTML={{ __html: terms.data.content }}
            />
            {/* The trailing sentinel the observer watches. aria-hidden: it is a
                measurement device, not content. */}
            <div ref={sentinelRef} aria-hidden="true" className="h-px w-full" />
          </>
        )}
      </div>

      <hr className="border-t" />
    </div>
  );
}

/**
 * Holds whether the guest has agreed, and resets on close.
 *
 * Split out so both surfaces reset identically. The reset matters: agreement is
 * deliberately not remembered across openings, so a guest is never carried
 * onward on an acceptance they abandoned.
 *
 * Two ways in, and they are not equal. `setAgreed` is the guest's own — they may
 * tick or untick at any moment, having read nothing (FR-045). `onReachedEnd` is
 * the automatic tick, fired by TermsViewer when the reader reaches the bottom.
 *
 * The automatic one can only ever fire ONCE per opening, because TermsViewer
 * latches it in a ref and re-arms only on close. That latch is FR-045a and it is
 * load-bearing: it is the whole reason scrolling cannot re-tick a box the guest
 * deliberately cleared. Nothing here may "re-sync" this state with the scroll
 * position — an automatic action must not overwrite a deliberate one, least of
 * all one about consent.
 */
export function useTermsAgreement(open: boolean) {
  const [agreed, setAgreedState] = useState(false);
  // Has the guest touched the checkbox themselves during this opening? Once they
  // have, the automatic tick is spent for good — see setAgreed below.
  const touchedRef = useRef(false);

  // Reset DURING RENDER, not in an effect. This is React's documented
  // "adjusting state when a prop changes" pattern: an effect would reset one
  // render too late, briefly showing a reopened dialog as already-agreed, and it
  // would cascade an extra render to do it.
  const [wasOpen, setWasOpen] = useState(open);
  if (wasOpen !== open) {
    setWasOpen(open);
    if (!open) {
      setAgreedState(false);
      touchedRef.current = false;
    }
  }

  const setAgreed = useCallback((next: boolean) => {
    touchedRef.current = true;
    setAgreedState(next);
  }, []);

  // The automatic tick, and the second half of FR-045a.
  //
  // TermsViewer already latches this to fire once per opening, so in practice it
  // arrives at most once. The `touchedRef` guard here is deliberately redundant:
  // it makes the rule a property of the STATE rather than of one caller's ref
  // discipline, so a future refactor of the viewer — an added ResizeObserver
  // pass, a re-sync on scroll, an observer that fires twice — cannot re-tick a
  // box the guest deliberately cleared. An automatic action must never overwrite
  // a deliberate one, least of all one about consent.
  const onReachedEnd = useCallback(() => {
    if (touchedRef.current) return;
    setAgreedState(true);
  }, []);

  return { agreed, setAgreed, onReachedEnd };
}

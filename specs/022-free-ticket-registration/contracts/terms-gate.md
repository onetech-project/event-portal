# Component Contract: Shared Terms & Conditions Dialog

**Feature**: 022-free-ticket-registration | **Date**: 2026-08-20, **revised 2026-08-21**

*(Filename is historical. This contract described a read-through **gate** until the clarification of
2026-08-21 removed it; what survives is an automatic tick. The file keeps its name so the links from
[plan.md](../plan.md) and [research.md](../research.md) do not rot.)*

Serves **both** surfaces (FR-043). This is a UI contract, not an HTTP one — but it is a contract,
because two independent callers depend on identical behaviour and a Principle VIII covered flow is
one of them.

> ## What changed on 2026-08-21, in one paragraph
>
> The checkbox is a **control** again, not an indicator. The guest may tick it at any moment without
> having read anything, and Agree follows the checkbox rather than the scroll position (FR-014c,
> FR-045). Reaching the end of the document still ticks the box for them — but only as a
> convenience, at most once per opening, and never over the top of a deliberate untick (FR-045a).
> Nothing about the *document* changed: it is still presented, still extracted into one shared
> component, still reachable by keyboard and assistive technology.

## 1. The extraction boundary

`frontend/components/booking/terms-dialog.tsx` mixed three separable things. Only the first two are
shared:

| Concern | Originally | After |
|---|---|---|
| **Presentation** — header (*"Terms & Conditions for:"* + event name), scrollable sanitized body, loading and error states | inline in `TermsDialog` | **extracted** → `components/terms/terms-viewer.tsx` |
| **The whole dialog frame** — width, padding, header spacing, checkbox, Cancel, Agree | inline in `TermsDialog` | **extracted** → `components/terms/terms-dialog-shell.tsx` (FR-043a) |
| **End-of-document signal** — has the reader reached the bottom | did not exist | **in the shared viewer**, reported to the shell |
| **Booking orchestration** — `useBookOrder`, `useRecordAgreement`, navigation, retry, availability refusal | inline in `TermsDialog` | **stays** in `TermsDialog`, untouched |

The registration form does **not** inherit the booking orchestration. It has no order to hold at
that moment: acceptance is local state, and the whole registration is written by one later call.

## 2. Shared viewer

```ts
type TermsViewerProps = {
  eventSlug: string;          // what useEventTerms fetches by
  eventName: string;
  enabled: boolean;           // gates the query, as useEventTerms(slug, open) already does
  onReachedEnd: () => void;   // fires AT MOST ONCE per opening, when the end is reached
};
```

**This type is unchanged by the 2026-08-21 revision, and so is the component.** The viewer never
owned the checkbox or what agreeing means — it reports one fact and lets the caller decide what the
fact authorises. That separation is exactly why the reversal costs nothing here: what the signal
*does* moved from "opens the gate" to "ticks the box", and the signal itself did not move at all.

### 2.1 The end-of-document condition

Satisfied when **any** of these is true:

1. `scrollTop + clientHeight >= scrollHeight - TOLERANCE`
2. The content cannot scroll at all: `scrollHeight <= clientHeight` (FR-014d)
3. A sentinel element placed after the last content node has intersected the viewport

**`TOLERANCE` must be non-zero.** Exact equality is the wrong test: fractional device pixel ratios,
browser zoom and sub-pixel layout routinely leave `scrollTop + clientHeight` a fraction short of
`scrollHeight`, and a guest who has visibly reached the end would never get the tick (FR-047). A
small pixel tolerance is the conventional fix.

**Prefer the sentinel (option 3) as the primary mechanism.** An `IntersectionObserver` on a trailing
sentinel satisfies FR-046 in a way a `scroll` handler cannot: it fires for keyboard paging and the
End key, for programmatic scrolling, and for assistive technology that moves focus through the
document without generating scroll events. Keep the scroll computation as a secondary trigger for
browsers or conditions where the observer does not fire.

*(This argument is materially weaker than it was — a guest the observer fails is no longer trapped,
because they can simply tick the box. It is kept because FR-046 still requires it, and because
"the accessible path is the one that silently degrades" is not a defensible place to land.)*

**Re-evaluate on resize** (FR-047), but only while the tick is still pending: `ResizeObserver` on
the scroll container, and a latch that has already fired must not fire again.

**Fires at most once per opening (FR-045a).** Once it has fired, scrolling back up does not untick,
scrolling back down does not re-tick, and a resize does not re-tick. This is what stops the
automatic behaviour from overriding a guest who deliberately unticked the box — the single most
important property added on 2026-08-21, and the one most easily lost to a well-meaning refactor
that "re-syncs" the checkbox with the scroll position.

**It resets when the dialog closes** — matching the existing rule that agreement is never remembered
across openings ([terms-dialog.tsx](../../../frontend/components/booking/terms-dialog.tsx), `handleOpenChange`),
so a guest is never carried onward on an acceptance they abandoned (spec Edge Cases).

## 3. Shared dialog shell

```ts
type TermsDialogShellProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  eventSlug: string;
  eventName: string;
  agreed: boolean;                        // WAS: reachedEnd
  onAgreedChange: (next: boolean) => void; // NEW — the checkbox is a control again
  onReachedEnd: () => void;
  error?: string | null;
  action: { label: ReactNode; onClick: () => void; disabled?: boolean; ready?: boolean };
};
```

Three changes from the shipped shell, and no others:

1. `reachedEnd` becomes `agreed`, and gains `onAgreedChange`. The checkbox renders
   `checked={agreed} onCheckedChange={onAgreedChange}` instead of `checked={reachedEnd}
   onCheckedChange={() => {}}`.
2. `actionable` is computed from `agreed && action.ready !== false` — **not** from `reachedEnd`.
   The shell no longer receives `reachedEnd` at all; it only forwards `onReachedEnd` to the viewer,
   and the caller's hook turns that into `agreed = true`.
3. The label's *"(please read the document)"* hint is **removed**. US5 scenario 3 requires that
   nothing tells the guest they must read first, and that parenthetical is the only thing on either
   surface that does.

`action.ready` keeps its meaning exactly: booking still passes `terms.isSuccess`, because agreeing
to a document that failed to load would record consent to nothing. That is a genuine precondition
and survives the loosening untouched — it is about the *document*, not about the guest.

### 3.1 The state hook

`useReadThroughGate(open)` returns `{ reachedEnd, onReachedEnd }` today. It becomes:

```ts
useTermsAgreement(open) → { agreed, setAgreed, onReachedEnd }
```

- `onReachedEnd` sets `agreed = true`. It is the auto-tick.
- `setAgreed` is the guest's own control, passed to the shell as `onAgreedChange`.
- The render-time reset on `open` transitioning to false is **kept verbatim**. It is the mechanism
  behind "reopening starts unchecked", it is already correct, and its comment explains why it is a
  render-phase reset rather than an effect. Do not touch it.

The rename is not cosmetic: `useReadThroughGate` names a thing this feature no longer has, and a
hook whose name promises a gate is where a future reader re-adds one.

## 4. Surface A — booking dialog (existing, modified)

**Layout is unchanged** (FR-044): same checkbox, same Cancel, same Agree, same positions, same
labels.

| State | Checkbox | Agree |
|---|---|---|
| Open, end not reached | unchecked, **clickable** | unavailable |
| Guest ticks it themselves | checked | **turns up**, no scrolling required |
| End reached, box untouched | **auto-ticks** | turns up |
| End reached, box already unticked by the guest | **stays unticked** (FR-045a) | stays unavailable |
| Guest unticks after either route | unchecked | unavailable again |
| Reopened | unchecked again | unavailable again |

The checkbox goes back to being an input. This *reverts* the widest-blast-radius change of the
2026-08-20 design rather than extending it — worth stating plainly, because a reader who knows the
earlier contract will expect the checkbox to be an indicator and will read the new code as a bug.

## 5. Surface B — registration form (new)

The checkbox lives on the **form**, not in the dialog, and FR-014a is unchanged by this revision.

```text
[ ] I agree to the Terms & Conditions.        ← click anywhere here opens the modal (FR-014a)
                     ^^^^^^^^^^^^^^^^^^ styled as the affordance

modal footer:  [ Agree ]   ← available as soon as the modal's own checkbox is ticked (FR-014c)
```

- Accept → ticks the form checkbox, closes the modal (FR-014e)
- Dismiss / Cancel / Escape → checkbox stays unchecked, nothing recorded (FR-014e)
- Clearing the form checkbox withdraws consent; re-checking reopens the document, but does not
  require reading it (FR-014f)
- The accepted `event_terms_id` is captured at Accept and submitted later (FR-050)
- **On a `TERMS_CHANGED` refusal the form clears the checkbox and reopens the modal *itself*,
  without waiting for a click (FR-051, new 2026-08-21).** This is the one place the client must
  drive the dialog rather than react to the guest: their consent was voided by an admin edit, not
  by their own choice, so they are shown what changed. Implementation note: the modal is already
  controlled by `open`/`onOpenChange`, so this is setting that state on the error branch of the
  submit mutation — not a new mechanism.

## 6. Testing this

> **The scroll path cannot be tested at the unit tier, and pretending otherwise is the failure
> mode here.**
> The test DOM is **happy-dom** (`@happy-dom/global-registrator`, [vitest.setup.ts](../../../frontend/vitest.setup.ts)),
> not jsdom, and it is worse for this purpose in a way that matters:
>
> - `scrollHeight` / `scrollWidth` are getter-only, backed by `PropertySymbol`, and initialised to
>   `0`. They cannot be stubbed by assignment.
> - `IntersectionObserver` and `ResizeObserver` **are registered on `window`** — so a feature-detect
>   finds them — but every method is an empty `// TODO: Not implemented` stub. The callback never
>   fires and never throws.
>
> The consequence is a test that fails silently upward: condition 2 evaluates `0 <= 0` as true, the
> auto-tick reports itself satisfied on a document that was never laid out, and the assertion passes
> while verifying a happy-dom artefact.

Split accordingly — and note that the revision moves work *down* a tier, which is the one place this
change makes verification easier rather than harder:

| Tier | Covers |
|---|---|
| **Vitest** | The whole manual route, which needs no layout at all: ticking the checkbox makes Agree available, unticking makes it unavailable, and no copy tells the guest to read first. Plus everything around the auto-tick with `onReachedEnd` driven directly as an input — that it ticks the box, that it fires at most once, that a fired-then-unticked box is not re-ticked, that closing resets. Do **not** assert on scroll metrics or observer callbacks. |
| **Playwright** (`e2e/`) | The auto-tick itself, in a real browser with real layout, against a **long** document — the only tier where scrolling means anything. FR-048 additionally requires the manual route asserted here, without scrolling, so the two are known not to have been wired to the same condition. |

## 7. Implementation traps in the existing dialog

Each is a real property of the code as it stands, and each would produce a plausible-looking wrong
implementation.

1. **The scroll region must stay focusable.** `TermsViewer` already sets `tabIndex={0}`, `role="region"`
   and an `aria-label` on the `overflow-y-auto` div. Without them `PageDown` and `End` never reach it
   and FR-046 fails. This is now easy to delete "as dead weight" — a reviewer who knows the guest can
   just tick the box may not see what it is for. It stays.

2. **Do not add `disabled` to the checkbox.** base-ui's `Checkbox` renders a `<button role="checkbox">`
   and the component's classes include `disabled:opacity-50`
   ([checkbox.tsx:8](../../../frontend/components/ui/checkbox.tsx#L8)), so a disabled state would dim
   it and drop it from the tab order — a *visible* change to a dialog FR-044 requires to look
   unchanged. There is now no state in which it should be disabled at all; the trap is only that
   someone re-adds one.

3. **The no-op `onCheckedChange={() => {}}` must be replaced, not supplemented.** It is what currently
   makes the box unclickable. Leaving it in place while adding an `onClick` elsewhere produces a
   checkbox that responds to a mouse and not to a keyboard `Space`, which is FR-046's failure in a
   new costume.

4. **`Agree` is not a disabled button — it is swapped for an `aria-disabled` `<span>`**
   ([terms-dialog-shell.tsx](../../../frontend/components/terms/terms-dialog-shell.tsx)).
   So `getByRole("button", { name: /^agree$/i })` **throws** rather than returning a disabled node.
   Any test asserting the unavailable state must query for the span or use `queryByRole(...)` and
   expect null; a naive `toBeDisabled()` fails as an error, not an assertion. This is unchanged, and
   it is now asserted from two directions instead of one.

5. **The agreement state must live inside the existing reset.** `TermsDialog` resets its state only
   when the dialog closes through its **own** `handleOpenChange` — a parent flipping the controlled
   `open` prop skips it entirely, which the component's doc comment calls out explicitly. State added
   outside that reset carries a stale acceptance into the next opening. `useTermsAgreement` keeps the
   render-phase reset that already handles this; the trap is adding a second piece of state beside it.

### 7.1 The e2e helpers: no change required, and that is the finding

`e2e/support/journey.ts` drives ~14 call sites through two helpers:

- `agreeToTermsAndBook()` — line 149, calls `readTermsToTheEnd()` then presses Agree
- `agreeExpectingRefusal()` — line 182, the same

Both scroll to the end and then press Agree. **Scrolling still ticks the box, so both keep working
against the revised behaviour unchanged.** The comment inside `agreeToTermsAndBook` that explains
why it does not click the checkbox is now wrong and must be corrected, but the code is not.

What FR-048 adds is a **second** route that nothing currently exercises: tick the checkbox without
scrolling, and assert Agree turns up anyway. Without it the suite cannot tell the difference between
"Agree follows the checkbox" and "Agree follows the scroll position, and the helper happens to
scroll" — which is the whole of this change.

> **The trap that survives the revision.** Existing fixtures author
> `putTerms(token, event.id, "<p>terms</p>")` — far shorter than the reading area, so condition 2
> ticks the box the instant the modal opens and **every existing scenario stays green without
> exercising either route**. That is "green because nobody looked" (Principle VIII). The auto-tick
> scenario must author a genuinely long document and assert the unticked→ticked transition; the
> manual scenario must assert it while still scrolled to the top.

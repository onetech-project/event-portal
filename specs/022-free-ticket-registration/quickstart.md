# Quickstart: Validating Free Ticket Registration

**Feature**: 022-free-ticket-registration | **Date**: 2026-08-20

How to prove this feature works end to end. Contracts are in [contracts/](./contracts/); the row
shapes are in [data-model.md](./data-model.md). This document is the run guide, not the
implementation.

## Prerequisites

The runner does not own the infrastructure. Bring it up first — **Mailpit is not optional**, because
the delivery assertion below reads the real message off it:

```bash
REDIS_PORT=6380 docker compose up -d postgres redis mailpit
docker compose run --rm migrate up
```

Then the three tiers:

```bash
cd backend  && ./scripts/test.sh ./...    # Go: unit + database-backed
cd frontend && npx vitest run             # TypeScript: logic + components
cd e2e      && npm test                   # Playwright, headless
cd e2e      && npm run test:slow          # headed and slowed, to watch the terms dialog behave
```

**Principle VIII requires all four matrix runs to pass**, not just the default one:

```bash
cd e2e
E2E_CACHE_ENABLED=false npm test          # Principle VII kill switch
E2E_RATE_LIMIT_ENABLED=false npm test     # Principle IX kill switch
```

## Before you start: the governance gate

This feature **must not merge** without the two constitution amendments recorded in the spec's
*Constitution Deviations* section and in [plan.md](./plan.md) Complexity Tracking. Each amendment
updates `PRD.md`, `ARCHITECTURE.md` and `SCHEMA.md` in the same change. If `.specify/memory/constitution.md`
has not moved, the implementation contradicts a NON-NEGOTIABLE principle and the checklist is not
satisfiable.

## Manual walkthrough

### 1. Arrange (through the admin console — never direct DB writes)

1. Create a published event with a Terms & Conditions document. **Author a long one** — long enough
   that the modal's reading area must scroll. A short document is ticked the instant the modal opens
   (FR-014d) and every check below passes without testing anything.
2. Add an ordinary paid ticket type.
3. Add a second ticket type, mark it **registration-only**, quota ≥ 2, sales window open.

### 2. Containment (User Story 2)

| Check | Expected |
|---|---|
| Guest ticket list for the event | Only the paid type. The registration-only type appears nowhere. |
| Admin ticket-type list | **Both**, with the registration-only one visibly distinguished. |
| Admin package composition picker | The registration-only type is not offered. |
| Add a package containing the registration-only type via the API directly | Refused, naming the type. |
| Mark a *package member* type registration-only | Refused, naming the blocking packages. |

### 3. The boundary (User Story 3)

Open `/event/<slug>/register/<paid-ticket-type-id>` — substituting the **paid** type's id.

Expected: no form, *"This registration is not available."* Then `POST` the same id directly, bypassing
the UI entirely. Expected: refused, and **no order, attendee, ticket or quota movement**. Hiding a
form is not enforcement (US3 scenario 2).

Repeat with a registration-only type belonging to a *different* event. Same refusal, indistinguishable.

### 4. Agreement (User Story 5) — the part most likely to be wrong

*(Revised 2026-08-21: there is no longer a read-through gate. Both routes below must work, and the
whole risk is that they get wired to the same condition and only one is really tested.)*

**Registration form.** Open `/events/<slug>/register/<registration-ticket-type-id>`.

1. Click the *"I agree to the Terms & Conditions"* checkbox → the modal opens; the form's box does
   **not** tick yet (FR-014a — unchanged).
2. **Without scrolling at all**, tick the checkbox inside the modal → `Agree` turns up immediately.
   Nothing on screen tells you to read first.
3. Untick it inside the modal → `Agree` goes away again.
4. Now scroll to the very end without touching the box → it **ticks itself** and `Agree` turns up.
5. Untick it, then scroll away from the end and back → it **stays unticked** (FR-045a). If it
   re-ticks, the auto-tick is tracking scroll position instead of latching, and it is overriding a
   deliberate choice about consent.
6. Agree → modal closes, the form's checkbox is now ticked.
7. Untick the form's box → `Confirm Registration` becomes unavailable. Re-tick → the modal opens
   again, unticked: acceptance is **not remembered** across openings.
8. **Keyboard only**: reopen, Tab into the reading area, press `End` → the box ticks itself (FR-046).
   Also confirm `Space` ticks the checkbox when focused, which is the same requirement from the
   other direction.

**Booking dialog** — the covered flow. On the same event, select the paid ticket and press Buy Ticket:

1. The dialog opens with its **existing layout** — checkbox, Cancel, Agree, unmoved (FR-044).
2. The checkbox is unchecked and `Agree` unavailable — but the checkbox **is clickable**. Tick it
   without scrolling: `Agree` turns up (FR-045).
3. Cancel, reopen, and this time scroll to the end → the checkbox ticks itself and `Agree` turns up.
4. Both routes must work. If only one does, they are wired to the same condition.

### 5. Register (User Story 1)

Fill the form. Confirm each rule refuses, and that a submission with several bad fields reports **all
of them at once** (FR-024):

| Field | Rejected |
|---|---|
| Full name | empty or whitespace |
| Email | malformed |
| Phone | `+628125567820` — the `+` is not a digit. Digits only, 12–15. Same message as the holder forms. |
| Date of birth | a future date |

Then submit a valid form → the review dialog shows back exactly what you typed → `Confirm & Submit`.

**Expected**: land on `/event/<slug>/register/<ticketId>/success` reading *"Registration Complete!"*
and *"…has been successfully generated and sent to your email."* **Reload it** — it must re-show the
confirmation, not an empty form (FR-039b).

### 6. Delivery — assert on Mailpit, not on a flag

Open Mailpit. The message must have:

- **Exactly ONE attachment**: the e-ticket. **No receipt.** This is the single most important
  assertion in the feature and the one the constitution amendment exists for.
- **No monetary figure anywhere** — not in the body, not in the PDF.
- Recipient = the registrant's address.

> `orders.email_sent` going true is **not** evidence of any of this. Read the message.

### 7. The ticket is an ordinary ticket

- Public lookup by its ticket code resolves it.
- Admin ticket validation inside the admission window → `Valid`, and `Used` is offered and
  irreversible.
- Outside the window → `Not yet valid` / `Expired`, naming the window. Free issuance changes nothing
  about validation.

### 8. Duplicate and exhaustion

| Check | Expected |
|---|---|
| Register again with the same email for this event | **Accepted** (FR-023). A second order, attendee and e-ticket, and quota down by one more. |
| Same email, **different** event | Allowed. The rule is event-scoped. |
| Email belonging to an **expired/cancelled** order's attendee | Allowed — an abandoned checkout must not lock an address out. |
| Drain quota to 0, then register | Refused. Quota never goes negative. |

### 9. Admin (User Story 4)

The registration appears in the order list with a **zero total** and its registration origin legible,
and in the attendee list with the same detail as a purchased attendee. Deleting the event or the
ticket type is refused exactly as for a purchase.

**Then the recovery path**, which FR-039 makes load-bearing: given only the registrant's *name* or a
*partial* email — not the exact address, which the guest was never shown — an operator must be able
to find the registration and resend (FR-053). If admin search requires an exact address, an
undelivered registration is unrecoverable and the feature is not finished.

## Automated coverage this feature must add

New spec file `e2e/specs/free-registration.spec.ts`:

1. Full journey → **one**-attachment email off Mailpit → ticket validates at the door.
2. **Auto-tick** against a **long** document: unticked → scroll to the end → ticked, Agree available.
   Both surfaces.
3. **Manual tick** against the same long document, with **no scrolling**: tick → Agree available.
   This is the scenario that fails against the current code and therefore the one that proves the
   change (FR-048).
4. The FR-045a latch: auto-tick fires, guest unticks, guest scrolls away and back → still unticked.
5. Keyboard-only auto-tick (`End` key).
6. A paid ticket type id refused by the endpoint, with nothing written.
7. Registration-only type absent from the guest list and the package picker.
8. A repeat email **accepted** — FR-023 removed the duplicate rule; a scenario asserting a refusal
   here is asserting a rule that no longer exists.
9. Quota exhaustion refused.
10. Stale `event_terms_id` refused; the form clears its checkbox and **re-opens the document itself**
    (FR-051).
11. Success page reload safe.

Changed: `guest-purchase.spec.ts` (both terms scenarios + long-document fixture),
`admin-console.spec.ts` and `cache-refresh.spec.ts` (new field, new exclusion).
**Not changed: `e2e/support/journey.ts`.** Its two helpers scroll to the end and press Agree, which
still works — only the now-false comment inside `agreeToTermsAndBook` needs correcting. See
[research.md D11](./research.md).

## Done when

- [ ] All three tiers green, and `e2e/` green in **all four** cache × throttle combinations
- [ ] The delivered email was **read** and carries exactly one attachment
- [ ] Both constitution amendments landed, with `PRD.md`, `ARCHITECTURE.md` and `SCHEMA.md` updated
- [ ] `SCHEMA.md` changed in the same commit as the migration
- [ ] Both agreement routes were verified against a long document — not a `<p>terms</p>` fixture
- [ ] The manual-tick scenario was seen **failing** against the current dialog before the change

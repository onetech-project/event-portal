# API Contract: Free Ticket Registration

**Feature**: 022-free-ticket-registration | **Date**: 2026-08-20

All responses use the project's standard envelope via `httpx.Respond` / `apperr`. Codes below are
existing `pkg/apperr` constants unless marked **NEW**.

---

## 1. `GET /api/v1/ticket/register/:ticket_type_id` — form prerequisites

Public, unauthenticated. Answers whether this registration is open and returns what the form needs
to render. The page calls it on load.

> **Route placement note.** `register` is a static segment and Echo routes static before param, so
> it cannot be shadowed by `GET /ticket/:event_id`. This is the same reasoning that already protects
> `/ticket/terms-condition/:event_id` ([event/handler.go:32-35](../../../backend/internal/event/handler.go#L32-L35)).

**Query**: `?slug=<event-slug>` — required. The ticket type must belong to this event.

**200**

```json
{
  "ticket_type_id": "uuid",
  "ticket_type_name": "Invitation Access",
  "event": { "id": "uuid", "name": "Jive Jakarta 2026", "slug": "jive-jakarta-2026" },
  "event_terms_id": "uuid",
  "event_terms_updated_at": "2026-08-20T04:12:33Z",
  "genders": [{ "id": 1, "name": "Man" }]
}
```

No price, no fee, no quota figure is returned — FR-015, and a quota number would be a live inventory
value the page must not display or gate on (Principle VII).

**Refusals** — FR-011/FR-012 require these to be *indistinguishable*. Every one of the following
answers **`404 TICKET_TYPE_NOT_FOUND`** with one identical message, *"This registration is not
available."*:

- unknown ticket type id
- ticket type belongs to a different event than `slug` names
- ticket type is `is_visible` (i.e. purchasable, not registration-only)
- event unknown, or not visible to guests
- sales window not open, or closed
- remaining quota is zero

> Collapsing six conditions onto one code is deliberate, not lazy. FR-012 forbids disclosing which
> ticket types or events exist; a distinct code per condition is an oracle. The trade-off — an
> operator debugging a broken link gets no detail from the response — is paid on the server side,
> where each refusal is logged with its actual reason.

**`409 TERMS_MISSING`** is the one refusal that is *not* collapsed: the event exists and the
registration is otherwise open, but no Terms & Conditions document is authored (FR-014g). It is
distinguishable because it discloses nothing an unauthenticated caller cannot already learn from the
booking path, which answers the same code for the same condition
([order/service.go:117-123](../../../backend/internal/order/service.go#L117-L123)).

---

## 2. `POST /api/v1/ticket/register/:ticket_type_id` — submit

Public, unauthenticated. **Throttled** — see §4.

**Request**

```json
{
  "slug": "jive-jakarta-2026",
  "name": "Halo Registrant",
  "email": "halo@example.com",
  "phone": "628125567820",
  "dob": "1996-04-12",
  "gender_id": 1,
  "agreed": true,
  "event_terms_updated_at": "2026-08-20T04:12:33Z"
}
```

**201**

```json
{ "registered": true }
```

Deliberately minimal. FR-039 settles that the confirmation page shows no address and no reference,
so returning either would put data on the wire that nothing may render — and an order number in a
response body is a support-grade identifier that the guest has no authenticated way to use.

**Refusals**

| Status | Code | Condition |
|---|---|---|
| 400 | `VALIDATION_ERROR` | Any field-shape failure. `data` is a `field → message` map carrying **every** offending field in one pass (FR-024). Messages are byte-identical to the client's (FR-025). |
| 400 | `TERMS_NOT_ACCEPTED` | `agreed` is false. |
| 404 | `TICKET_TYPE_NOT_FOUND` | Every §1 refusal, re-checked. The endpoint refuses independently of the UI (FR-024, US3 scenario 2). |
| 409 | `TERMS_CHANGED` | `event_terms_updated_at` is not the current document's (FR-050). Client clears its agreement checkbox and re-opens the modal **itself**, without waiting for the guest to click (FR-051, revised 2026-08-21); it does not require the document to be read again. **Compare `updated_at`, never the id** — an edit preserves the id, so an id comparison can never fire (see [research.md D8](../research.md)). |
| 409 | `TERMS_MISSING` | The document is gone entirely. |
| 400 | `INSUFFICIENT_QUOTA` | Quota exhausted between page load and submit. |
| 429 | `RATE_LIMITED` | Throttle — and now the ONLY volume bound (FR-023b). Reported to the guest as a temporary "wait and retry", never as a form error, so a legitimate group registrant is not sent back to edit correct fields. |

> **There is deliberately no duplicate-address refusal.** An earlier draft returned
> `409 EMAIL_ALREADY_REGISTERED` when the address already held a place at the event. FR-023 removed
> that rule: an address may register as many times as remaining quota allows, and each submission
> produces its own order, attendee and e-ticket. The numeric code `409007` is **retired, not
> reassigned** — a client or fixture still keyed on it must get no match rather than silently start
> matching an unrelated conflict.
>
> A side effect worth naming: with no address-keyed check anywhere, this endpoint can no longer be
> used to discover whether a given address holds a ticket (SC-016), which it previously could.

**Nothing is written on any refusal.** No order, no attendee, no ticket, no quota movement (FR-051).

### 2.1 Server-side sequence

```text
 1. validate shape .......................... no DB
 2. resolve ticket type + event ............. read-only, collapses to 404
 3. resolve current terms ................... read-only; compare updated_at → 409
 4. resolve active genders .................. read-only
 5. (no duplicate-address check — FR-023 removed it; see data-model.md §3.1)
 ── BEGIN ────────────────────────────────────────────────────────────────────
 6. CheckAndDeductQuota (row-locked, atomic) — the FIRST statement in the tx.
    No address-scoped advisory lock precedes it: two submissions of one address
    are two ordinary registrations and must not contend (FR-023a).
 7. INSERT orders (total 0, PAID, terms_agreed_at, event_terms_id) — NO origin marker;
    the registration/purchase distinction is derived from the line inserted next
 8. INSERT order_items (qty 1, unit_price 0)
 9. INSERT attendees (fully filled — no empty-slot phase)
10. register post-commit cache invalidation .. cache.InvalidateAfterCommit(Orders, Event)
 ── COMMIT ───────────────────────────────────────────────────────────────────
11. respond 201
12. async, off the request path (mirrors payment.fulfillAsync):
      ticket.IssueTicketsForOrder(orderID)
      notification.SendTicketEmail(orderID)   → registration shape: e-ticket only
```

Steps 6–10 contain **no network call and no cache command** — Principle IV and Principle VII's
transaction-boundary rule. `cache.InvalidateAfterCommit` registers the work; it does not perform it,
and the cache client refuses outright on a transaction context, so an inlining refactor fails loudly.

Step 12 uses `context.WithTimeout(context.WithoutCancel(requestCtx), …)` and a `WaitGroup` for
graceful drain, exactly as [payment/service.go:831-861](../../../backend/internal/payment/service.go#L831-L861) does.

---

## 3. Changed existing contracts

| Endpoint | Change |
|---|---|
| `GET /api/v1/ticket/:event_id` (guest ticket list) | Registration-only types **excluded** (FR-006). No response-shape change. |
| `GET /api/v1/admin/events/:id` and admin ticket-type reads | `is_visible` **added** to `TicketTypeAdminView` (FR-004). Defaults `true` on the wire; the admin projection is unfiltered, so `false` rows appear here and nowhere guest-facing. |
| `POST` / `PUT` admin ticket type | `is_visible` accepted on `TicketTypeRequest`, as an OPTIONAL boolean: absent means visible (see §3). Setting it `false` on a type that is a package member is refused `409 PACKAGE_COMPOSITION_LOCKED` (FR-005) — reusing the existing code, whose meaning already is "this package membership blocks what you asked". |
| `POST` / `PUT` admin package | A registration-only component is refused `400 VALIDATION_ERROR` naming the offending type (FR-007). |
| `POST /api/v1/ticket/book`, `POST /api/v1/ticket/checkout/:order_id`, `POST /api/v1/ticket/availability` | A registration-only ticket type is refused; no quota moves (FR-008). **All three at one seam** — see below. |

### 3.1 Enforce FR-008 at the seam, not at three call sites

Booking, checkout and availability all resolve a ticket type through **one** interface method:
`EventProvider.TicketTypeForCheckout` ([event_provider.go:97](../../../backend/internal/order/event_provider.go#L97)),
consumed by `expandTicket` ([order/demand.go:52](../../../backend/internal/order/demand.go#L52)).
Putting the refusal there covers all three endpoints *and* package expansion at once.

Putting it only in `Book` — the obvious reading of FR-008 — leaves `POST /ticket/availability`
answering "available" for a registration-only type. That matters: availability runs deliberately
*in front of* the Terms & Conditions dialog so a guest never opens a document for a purchase that
cannot happen (spec 013). A refusal that arrives one step later is the exact regression that spec
exists to prevent.

The flag reaches `order` the only way Principle II permits: `event.TicketTypeRow` →
`eventProviderAdapter` ([adapters.go:32](../../../backend/cmd/api/adapters.go#L32)) →
`order.TicketTypeInfo`. `event` must not import `order`, and vice versa; the adapter file is the one
place allowed to see both.

### 3.2 `UpdateTicketType` is an absolute full replace — a real leak if missed

`UPDATE ticket_types SET name = $2, description = $3, price = $4, quota = $5, …`
([event.sql:150-157](../../../backend/internal/event/queries/event.sql#L150-L157)) replaces every
column unconditionally; the query's own comment records this for `quota`.

Under the original `is_registration_only`, an admin PUT that omitted the field **cleared** it and
republished the type onto the guest list at price 0. Under `is_visible` the polarity inverts and so
does the failure: an omitted field would **hide** the type, taking it off sale and making it free to
register for — on an edit of any unrelated field.

The hiding direction is the worse of the two, so `TicketTypeRequest.IsVisible` is a **pointer**, and
absent means visible (matching the column default). Hiding must be an explicit `"is_visible": false`.
The field is still threaded through the repository call and the admin form and still sent on every
write, and a round-trip test still proves that editing an unrelated field preserves it — the pointer
is a floor under that discipline, not a replacement for it.

---

## 4. Throttling (Principle IX)

New surface, following `ThrottlePolicy` exactly ([pkg/config/throttle.go:119-140](../../../backend/pkg/config/throttle.go#L119-L140)):

| Setting | Default | Rationale |
|---|---|---|
| `RATE_LIMIT_REGISTER_ENABLED` | `true` | Per-surface switch. |
| `RATE_LIMIT_REGISTER_RATE` | `0.2` | Matches `RATE_LIMIT_CHECKOUT_RATE`. This surface consumes quota **and** sends real mail, so it is at least as expensive as checkout. |
| `RATE_LIMIT_REGISTER_BURST` | `10` | **Deliberately not checkout's 3.** FR-023 removed the one-address-per-event rule, so submitting this form repeatedly from one device became the intended way to register a group; at a burst of 3 the fourth guest is refused. Burst governs legitimate group size, rate governs bulk abuse — only the burst moved, so the 12/min ceiling is unchanged. Refill 50 s, still well under the 3-minute idle TTL. |

Must be added to `ThrottleConfig.ratePolicies()` so it automatically joins startup validation and the
startup report — omitting that is how a new throttle silently escapes both.

**Blast-radius documentation is mandatory** (Principle IX, final rule). The configuration docs must
state: *disabling this leaves an unauthenticated endpoint that deducts quota and sends real email,
and that since FR-023 removed the per-address rule, **nothing else bounds submission volume at
all**.*

The `GET` in §1 rides the unthrottled public group — it is a read that creates nothing, the same
judgement `RegisterAvailabilityRoute` documents for its own read-only endpoint.

**Client identity is sound — the constitution's Follow-up TODO on this is stale.** Verified rather
than assumed: [main.go:333-345](../../../backend/cmd/api/main.go#L333-L345) sets
`e.IPExtractor = echo.ExtractIPDirect()` by default and only honours `X-Forwarded-For` from
`TRUSTED_PROXY_CIDRS`. Spec 018 closed this. So the per-IP keying this throttle depends on satisfies
Principle IX's unforgeable-identity rule, and may honestly be described as enforced.

Two structural constraints the composition root imposes, both enforced by
`cmd/api/architecture_test.go`:

- **The group must be constructed in both modes.** A disabled throttle is *substituted* with a
  pass-through, never skipped — wrapping group construction in `if enabled` changes how an unmatched
  `/api/v1` path answers between modes, which Principle IX forbids in as many words. Use the existing
  `rateLimit(...)` helper, which already does this.
- **`cmd/api/ops.go` must not contain the strings `Throttle`, `RateLimit` or `RATE_LIMIT`** —
  `/healthz` is public and must not publish thresholds. Any ops or health work for this surface goes
  elsewhere.

**Validate the `IdleTTL` interaction before choosing the numbers.** Startup *refuses* unless
`RATE_LIMIT_IDLE_TTL` strictly exceeds the longest full-burst refill window across every enabled rate
surface ([throttle.go:248-278](../../../backend/pkg/config/throttle.go#L248-L278)). The defaults
above give a 15 s refill (3 ÷ 0.2) against a 3-minute TTL, so they are safe — but a slower rate such
as `0.1` with burst `3` would produce a 30 s window and, if it ever exceeded the configured TTL, would
refuse startup and take the whole Principle VIII suite down with it.

**No new numeric code is registered.** This warning originally applied to
`EMAIL_ALREADY_REGISTERED`: `apperr.Numeric`'s default arm is `status * 1000`
([apperr.go](../../../backend/pkg/apperr/apperr.go)), so an unregistered code would have rendered as
`409000`. FR-023 removed the refusal, so the constant and its `409007` mapping were both deleted. The
hazard the warning describes still applies to any *future* code added here.

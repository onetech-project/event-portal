# Phase 0 Research: Per-Ticket Event Dates

**Feature**: [spec.md](./spec.md) | **Date**: 2026-08-11

Every decision below was taken against the code as it stands, not against a
recollection of it. File and line references are the evidence.

---

## D-001: Where the two columns live and how existing rows are filled

**Decision.** Migration `000014_ticket_type_event_window` adds
`event_start timestamptz` and `event_end timestamptz` to `ticket_types`, backfills them
from the parent event, then applies `NOT NULL` and a
`ticket_types_event_window_chk CHECK (event_end >= event_start)`.

**Rationale.** The three-step *add nullable → `UPDATE … FROM` joined table → `SET NOT
NULL`* shape is the established house pattern, with the reasoning written into the
migration that introduced it:
`backend/migrations/000013_master_list_identity_and_flags.up.sql:61-75`, whose comment
reads "Backfilled BEFORE the NOT NULL, or the constraint fails on existing rows."
`000012_gender_fk_and_contact_snapshot.up.sql:14-18` is the same join-backfill against
another table. Recent migrations use no explicit `BEGIN;` (golang-migrate wraps each file
for Postgres), no `IF NOT EXISTS` on new work, a `--` header naming the spec, and a
`COMMENT ON COLUMN` where the semantics are non-obvious — all three of `000011`, `000012`,
`000013` follow that shape.

**The check is `>=`, not `>`, and this corrected the spec.** `packages_sales_window_chk`
uses strict `>`, which was the tempting model. But `EventRequest.Validate` refuses only
`EndDate.Before(StartDate)` (`backend/internal/event/admin_dto.go:113-115`) — an event
with `start_date == end_date` is legal and may exist. A strict check plus FR-004's
"backfill from the parent event" would then fail the `SET NOT NULL` step on real data.
Matching the event's own rule keeps the backfill total and keeps FR-005 containment
trivially satisfiable at rollout, because every ticket window starts out exactly equal to
its event's. FR-002 was amended accordingly.

**Alternatives rejected.**
- *Strict `>` with a defensive backfill* (`GREATEST(e.end_date, e.start_date + interval
  '1 day')`, in the spirit of `000013:48-57`). Rejected: it invents a date no admin chose,
  and that invented end would then sit outside a zero-length parent event, violating
  FR-005 the moment anyone edited the ticket type.
- *Nullable columns meaning "inherit the event"*. Rejected: it makes every read site
  branch, and FR-004 explicitly wants no ticket type without a window.

**Follow-on.** `SCHEMA.md` changes in the same commit (AGENTS.md, constitution
Governance). `sqlc.yaml` points at the `migrations` **directory**, so the new column is
picked up by `cd backend && sqlc generate` with no config edit (`README.md:185-187`).

---

## D-002: The cached ticket-type list will serve zero-value dates after deploy

**Decision.** Flush the cache as an explicit post-deploy step, via the existing
`POST /admin/cache/refresh`. No key-versioning scheme is introduced.

**Rationale — this is the sharpest operational hazard in the feature.** The public
ticket list is cached as the **DTO** `[]event.TicketTypeSummary`
(`backend/internal/event/service.go:171-191`), JSON-marshalled with no envelope and no
schema version (`backend/pkg/cache/through.go:67`). The key grammar is
`list:ticket_types_public:{event_uuid}:-:g{gen}`
(`backend/pkg/cache/cache.go:164-180`, `pkg/cache/surfaces.go:66-68`) and the only
variable part is a generation counter bumped **by writes, not by deploys**
(`pkg/cache/cache.go:106-111`).

`Through` does degrade gracefully on a decode failure — it counts
`cache_errors_total{op="decode"}` and falls through to the database
(`pkg/cache/through.go:48-57`), with a comment naming exactly this scenario. **But adding
fields will not trigger that path.** `encoding/json` ignores absent fields, so an entry
written by the old binary decodes cleanly into the new struct with
`event_start`/`event_end` left at `0001-01-01T00:00:00Z`. That is silently wrong data, not
an error, for up to `CACHE_TTL` (default 10m, `pkg/config/config.go:219-221`) — and for
the whole of a rolling deploy, since old and new pods share the same keys.

A flush is one call, already implemented, already tested
(`cmd/api/ops_test.go`, `e2e/specs/cache-refresh.spec.ts:157`), and returns
`{"status":"disabled"}` rather than erroring when the cache is off
(`cmd/api/ops.go:72-79`), so the step is safe to run unconditionally.

**Alternatives rejected.**
- *Add a schema-version segment to the key grammar.* Correct in the abstract but changes
  the published contract in `specs/014-redis-list-cache/contracts/cache-keys.md` for a
  one-off migration, and every future column addition would still need someone to
  remember to bump it.
- *Rely on the 10-minute TTL.* Rejected outright: Principle VII says TTL "MUST NOT be the
  mechanism by which the system becomes correct."
- *Make the fields pointers so absence is visible.* Rejected: it pushes a nil check into
  every consumer to paper over a deploy-ordering problem.

---

## D-003: The per-line window rides on `PublicOrderItem`, not on `PublicOrderEvent`

**Decision.** Add `event_start` / `event_end` to `PublicOrderItem`
(`backend/internal/order/dto.go:150-157`), populated in `publicItems`
(`backend/internal/order/public_service.go:286-312`). Leave `PublicOrderEvent.start_date`
/ `end_date` meaning what they say — the **event's** dates. The Order Summary panel
derives its date range and gate time from the items.

**Rationale.** `publicItems` already holds both the `OrderItemRecord` and its matching
display, per line, so the data is in hand at zero extra cost. `eventOf`
(`public_service.go:256-284`) deliberately collapses to the first resolvable line and is
the wrong seam — FR-010 needs each line to differ. Keeping `PublicOrderEvent` truthful
matters because FR-012 rests on the event's own dates continuing to mean the event.

The frontend cost is small and the formatter already cooperates: `formatDateRange`
collapses a same-day range to a single date (`frontend/lib/format.ts:48`), and `gateTime`
is a module-private helper taking a plain string (`order-summary-panel.tsx:180-191`), so a
per-line or min-across-lines value drops straight in.

**Alternatives rejected.**
- *Overwrite `PublicOrderEvent.start_date` with the ticket span.* Rejected: it makes a
  field named for the event carry something else, and any future consumer of
  `order.event.start_date` silently gets the wrong thing.
- *Add a second pair (`ticket_event_start`/`…_end`) to `PublicOrderEvent`.* Rejected: it
  duplicates data derivable from the items and would drift from them.

---

## D-004: Package windows are derived in SQL, and only where a package is displayed

**Decision.** Extend `ListPackageDisplaysByIDs`
(`backend/internal/event/queries/event.sql:291-299`) with
`MIN(tt.event_start)` / `MAX(tt.event_end)` joined through `package_tickets` →
`ticket_types`. Do **not** touch `PackageSummaryDTO`.

**Rationale.** FR-021 wants the span derived, never stored — the same reasoning the schema
already applies to package availability ("An inventory column here would be a second
source of truth", `SCHEMA.md:217-220`). Both tables belong to the event domain, so the
join stays inside the boundary, exactly as the existing comment on that query says.

Scope is narrower than it first appears: FR-013 keeps the ticket-selection page dateless,
so the **only** guest surface that shows a package's window is the Order Summary panel's
per-line date, which reads `PackageDisplay`. `PackageSummaryDTO`
(`backend/internal/event/dto.go:98-108`) needs nothing.

---

## D-005: Validation gains two result codes, not one

**Decision.** Add `ResultNotYetValid = "NOT_YET_VALID"` and `ResultExpired = "EXPIRED"` to
`backend/internal/ticket/dto.go:10-21`, both carrying `event_start` / `event_end` on
`ValidationResult`.

**Rationale.** FR-015 requires an outcome distinct from the existing three; FR-016
requires naming the window; US3 scenarios 2 and 3 want "does not apply yet" and "has
passed" worded differently. Two flat string constants match the existing style, keep the
frontend verdict map a pure lookup (`validation-result-card.tsx:8-22`), and — decisively —
put the not-yet/passed decision on the **server**, where the clock is authoritative. A
single `OUT_OF_WINDOW` code would force the result card to compare the window against the
validating device's clock, which is precisely the device most likely to be wrong at a
gate.

`Validate` already joins `ticket_types` and `events`
(`backend/internal/ticket/queries/ticket.sql:8-17`) and selects only names, so reaching
the window is a projection change, not a new join. `ListTicketDetailsByOrderID`
(`ticket.sql:36-48`) already selects `e.start_date` over the identical join path — the
precedent is in the same file.

**Precedence** (FR-018) is implemented in the existing switch at
`backend/internal/ticket/service.go:149-163`, with the window check inserted only on the
`ACTIVE` branch. That gives `INVALID` → `ALREADY_USED` → window → `VALID` for free, and
keeps `INVALID`'s field-nilling (`service.go:161-163`) intact so FR-019 holds.

**Alternatives rejected.**
- *One `OUT_OF_WINDOW` code with a server-computed `reason` discriminator.* Functionally
  identical but adds a field the frontend must switch on anyway — two codes say the same
  thing with less machinery.
- *Enforce the window inside `MarkUsed`'s guarded `UPDATE`.* Rejected as the primary
  mechanism: the single guarded statement's concurrency guarantee
  (`ticket.sql:22-28`) is load-bearing and should not grow a date predicate. FR-017's
  direct-invocation refusal is a service-level check before that statement runs.

---

## D-006: Containment is checked in the service; the event-edit warning is frontend-derived

**Decision.** FR-005 containment lives in `event.Service.CreateTicketType` /
`UpdateTicketType`. FR-005b's stranded-ticket-type warning is computed in the admin
frontend from data it already has. No API change for the warning.

**Rationale.** `TicketTypeRequest.Validate` is deliberately DB-free
(`admin_dto.go:143-165`) and has no access to the parent event's dates, and
`architecture_test.go:105-117` forbids `admin_dto.go` from importing `eventsql` anyway. The
service is the right layer, and `CreateTicketType` **already loads the event** at
`admin_service.go:244-250` and discards it into `_` — the dates are in hand for free.
`UpdateTicketType` is the asymmetric one: it loads nothing and learns `EventID` only from
the returned row (`admin_service.go:300`), so it needs a lookup added before the write.

For the warning: `EventAdminDetail` already embeds the event's dates *and* its ticket
types (`admin_dto.go:43-46`), so once `TicketTypeAdminView` carries the window, the admin
event page can derive "which of these sit outside" with no new endpoint, no new field, and
no server round trip. `UpdateEvent` stays a single-statement update with no cross-entity
check (`admin_service.go:71-92`), which is what FR-005a demands anyway.

**Error code**: reuse `apperr.CodeInvalidDateRange` (`pkg/apperr/apperr.go:61`, numeric
`400001`). It is already what both validators return for a malformed window, and
`apperr.go:11-12` warns that codes are a public contract not to be grown casually.
`(*Error).WithData` (`apperr.go:205-209`) carries which bound was violated.

---

## D-007: The e2e suite's seeded dates break under strict validation

**Decision.** Give `createEvent`, `createTicketType`, and `createSellableEvent`
(`e2e/support/api.ts:98-181`) optional date overrides, and seed the two validation
scenarios with an event window that spans **now**.

**Rationale.** This is a real breakage, not a precaution. `createEvent` seeds every event
at `+30d / +31d` (`e2e/support/api.ts:110-111`). Once ticket windows default to the parent
event's range (FR-004) and FR-014 enforces them with no tolerance, the existing
`admin-console.spec.ts:89` scenario — which validates a ticket *now* and asserts `Valid` —
gets `NOT_YET_VALID` instead. FR-005 containment means the fix cannot be the ticket window
alone: the seeded **event** has to span now too.

Roughly 28 seeding call sites exist across the three spec files, but all funnel through
those three helpers, so the change is contained to one file plus the two validation tests.

**Note for the tasks phase**: per Principle VIII the new out-of-window scenario must be
confirmed failing against unfixed code before the fix lands. The existing
`admin-console.spec.ts:89` and `:144` are the regression surface.

---

## D-008: Test-coverage gap this feature must not inherit

**Observation, not a decision.** There is **no Go test anywhere** that exercises
ticket-type create/update/delete cache invalidation. `internal/event/cache_integration_test.go`
covers only the event catalogue, and none of the ticket-type CRUD tests in
`admin_service_test.go:211-377` wire a cache at all. The only things standing between a
ticket-type column change and a stale cached list today are the per-event generation bump
on three admin paths and the 10-minute TTL.

Since this feature adds two fields to the cached ticket-type DTO and makes them
guest-visible, the tasks phase should close that gap rather than widen it.

---

## Resolved unknowns

| Unknown | Resolution |
|---|---|
| Where does "booking information" live? | The Order Summary panel, `frontend/components/order/order-summary-panel.tsx:83-89` (range + gate) and `:110` (per-line date). No literal "Booking Information" string exists. |
| Does the countdown read the sales window? | No — `event.start_date`, via `event-frame.tsx:62`. It stays on the event (FR-012). |
| Can validation reach the window? | Yes, the join already exists; projection change only (`ticket.sql:8-17`). |
| Highest migration | `000013`; this feature is `000014`. |
| sqlc regen | `cd backend && sqlc generate`; verify with `git diff --exit-code internal/`. No Makefile. |
| Cached value shape | The domain DTO, per Principle VII. Confirmed `[]TicketTypeSummary`. |

---

# Phase 0 Research — Revision 2

**Date**: 2026-08-12. Four decisions covering the 2026-08-12 clarification delta. Revision
1's D-001..D-008 above are unchanged and still describe shipped code.

---

## D-009: The wire carries a list of admission starts per line

**Decision.** Replace `PublicOrderItem.event_start` / `event_end` with
`admission_starts []time.Time` — the distinct admission start instants that line admits
on, sorted ascending. A ticket line carries exactly one; a package carries one per
distinct constituent.

**Rationale.** A single pair cannot express a Day 1 + Day 2 bundle, and that is precisely
the defect: `publicItems` fills a package line from `MIN(tt.event_start)`
(`backend/internal/event/queries/event.sql`, `ListPackageDisplaysByIDs`), so the panel
prints the earliest day and Day 2 vanishes. No amount of formatting recovers information
the wire never carried.

`event_end` is dropped rather than kept alongside because nothing reads it any more.
Revision 1 used it for the panel's range, and FR-009 has just reverted that range to the
event's own dates (D-013 below is not needed — FR-009 states it outright). The per-line
display names days, not durations, and FR-021c fixes the range's closing value as a
**start** date. Keeping an unread field on the wire invites a future consumer to read the
wrong thing.

**Alternatives rejected.**
- *Keep the pair and add the list.* Two representations of one fact, guaranteed to drift.
- *Send a pre-formatted string.* Puts locale and the 1/2/range rule on the server, where
  it cannot know the viewer's timezone — see D-010.

---

## D-010: The client dedupes by rendered date, not the server

**Decision.** The server sends distinct admission **instants**. The client formats each
one and deduplicates the resulting **strings**, then applies the 1 / 2 / range rule.

**Rationale — this is the subtle one.** FR-021b counts "distinct calendar dates", but a
calendar date is not a property of an instant; it is a property of an instant *rendered in
a timezone*. Two constituents starting 2026-09-30T22:00Z and 2026-10-01T02:00Z are two
distinct instants and two distinct UTC dates, but in Asia/Jakarta (UTC+7) they are
05:00 and 09:00 on **1 October** — one date.

If the server deduplicates, it must guess the viewer's timezone, and any mismatch shows
the buyer the same date printed twice. Deduplicating on the formatted string makes the
rule true by construction: "two distinct dates" means exactly "two distinct things the
buyer can see".

This also matters because `formatDate` renders in the **browser's** timezone (it passes
no `timeZone` option), while `gateTime` pins `Asia/Jakarta`. That inconsistency predates
this feature and is out of scope, but it is exactly why the dedupe cannot safely live on
the server.

**Alternatives rejected.**
- *Server dedupes by Asia/Jakarta date.* Correct for buyers in Jakarta, wrong for anyone
  else, and silently so.
- *Server sends `date` strings instead of instants.* Same problem, plus it discards the
  time of day the range logic and any future consumer might need.

---

## D-011: Constituent starts come from a second batched query, not `array_agg`

**Decision.** Drop the `MIN`/`MAX` aggregate from `ListPackageDisplaysByIDs` and add
`ListPackageAdmissionStartsByIDs`, returning `(package_id, event_start)` rows — one per
distinct constituent — assembled into a map in the repository.

**Rationale.** Evidence from revision 1: `MIN(tt.event_start)` over a `LEFT JOIN`
generated `TicketEventStart interface{}`, and only an explicit
`CAST(... AS TIMESTAMP WITH TIME ZONE)` restored `time.Time`. Aggregates lose type
information in this sqlc configuration. `array_agg(DISTINCT tt.event_start)` would push
that further, into array-of-timestamptz territory, for no gain.

Two plain queries keep every generated type a bare `time.Time`, keep the row shape
obvious to the next reader, and stay inside the event domain's own tables. Both are
batched over `= ANY($1)` on ids already in hand, so this is one extra round trip per order
read, not per line.

**Alternatives rejected.**
- *`array_agg(DISTINCT ...)`.* One query, but a generated type this configuration has
  already shown it handles poorly, and a `DISTINCT` on instants that D-010 says is the
  wrong place to deduplicate anyway.
- *Widen the existing query and let rows multiply.* Turns a one-row-per-package result
  into a fan-out the caller must re-collapse — the same work, hidden.

---

## D-012: The locale change is five formatters, and excludes money

**Decision.** Switch the five date/time `Intl` formatters in `frontend/lib/format.ts` from
`id-ID` to `en-GB`. Leave `currencyFormatter` and the two compact-count formatters on
`id-ID`.

**Rationale.** `en-GB` yields "1 Oct 2026" — day-first, matching the order `id-ID` already
produced, so nothing in the layout shifts. `en-US` would give "Oct 1, 2026" and move the
day.

Currency stays because `Intl.NumberFormat("id-ID", {currency:"IDR"})` renders
`Rp 170.400`, where the dot is a **thousands** separator. Under `en-GB` the same amount
becomes `Rp 170,400`. Both are legible to an English reader, but the screenshot's
Indonesian buyer reads dot-as-thousands, and switching it risks a buyer misreading the
amount they are about to pay. Visitor counts follow the same reasoning: "30.000+
Visitors" is the intended rendering.

`gateTime` in `order-summary-panel.tsx` already uses `en-GB`, so this brings the rest of
the app in line with a choice the panel had already made locally.

**Blast radius**: two test assertions encode Indonesian months —
`frontend/lib/format.test.ts` expects `/Okt/`, and
`frontend/components/order/order-summary-panel.test.tsx` expects `/1 - 3 Agu/i`. Both
assert the old language, not old behaviour.

**Alternatives rejected.**
- *Change only the Order Summary.* Leaves "1 Okt" on the events list and "1 Oct" on the
  order page — reads as a bug.
- *Everything to English including money.* Misstates rupiah amounts to the buyers this
  product serves.

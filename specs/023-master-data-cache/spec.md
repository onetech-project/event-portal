# Feature Specification: Master Data Read Cache

**Feature Branch**: `023-master-data-cache`

**Created**: 2026-08-24

**Status**: Draft

**Input**: User description: "i want it if an api needs master data, ex: genders, store the master data on redis instead since master data wont change that much" — followed by "no need TTL on the redis, because it wont change".

## Context

The request names master data generally and genders as its example. Investigation established
that **genders is the only master list the request can actually apply to**, and the two
exclusions are load-bearing rather than oversights — each is recorded as a requirement below so
a later reader does not "complete" the feature by adding them.

The gender master list is a two-row table read on four request paths, none of them inside a
transaction. Every read currently takes a database connection to return two rows that change
only when a migration changes them.

## Clarifications

### Session 2026-08-24

- **Q: Should the gender entries keep the project's standard expiry backstop, exactly as every
  other cached surface does?**
  → **A: yes — the standard backstop, with the startup refresh as the correctness mechanism.**
  This REVERSES the "no expiry" instruction the spec was first written to; FR-006 and FR-007 are
  rewritten accordingly. A surface with no expiry is technically possible, but only by widening
  the shared storage contract to carry a per-surface lifetime — changing infrastructure every
  domain uses so that one surface behaves unlike all the others. It is also unnecessary: the
  project already treats expiry as a backstop for a refresh that was missed, never as what makes
  data fresh. Keeping it therefore does not contradict "the list will not change"; it bounds the
  damage on the occasions the refresh does not happen.

- **Q: Should the Principle VII amendment admit the gender master list specifically, or
  master-data lists as a general category?**
  → **A: the gender master list specifically.** The principle enumerates concrete entity families
  — `events`, `ticket_types`, packages, `orders`, `attendees` — rather than categories, and that
  specificity is the discipline a closed list exists to enforce. Genders is in any case the only
  master list that qualifies (FR-003, FR-004), so a category grant would admit more than this
  feature needs or was reviewed for. A future master list gets its own review.

- **Q: What should happen if the store is unreachable when the service starts, so the startup
  refresh cannot run?**
  → **A: start and serve, log the degraded start, and let the expiry bound the staleness.**
  Refusing to start was rejected outright — Principle VII requires the API to start and serve with
  the store absent. This is exactly the case the expiry backstop exists for, which is what makes
  the first clarification of this session load-bearing rather than cosmetic: without an expiry,
  this failure mode would be unbounded and a pre-migration list could be served indefinitely.
  FR-008a states it.

- **Q: On a miss, does it read from the database and populate the store (a backfill)?**
  → **A: yes — and the spec did not actually say so, which is why FR-015a now exists.** The
  read-through path this feature reuses already does it: on a miss it reads the database, returns
  the value to the caller, and writes it back so the next read is served from the store. The gap
  was in the wording: the spec said a read "rebuilds from the database", which an implementer
  could satisfy by reading through on *every* miss and never storing anything — meeting every
  other requirement while delivering none of the benefit. **A miss populates; it does not
  invalidate.** The two are opposite operations: invalidation exists for data that CHANGED and
  orphans existing entries, so invalidating on a miss would find nothing to orphan and would
  discard the other projection's entry for no reason. FR-015c states that distinction because the
  question that prompted it conflated them, and an implementation could too.

- **Q: When a miss reads from the database and then fails to store the value back, what should
  happen?**
  → **A: best-effort — serve the caller, count the failure, let the next reader repeat the read.**
  A store can be readable and still reject a write, and the caller already holds the value they
  came for, so failing their request over a bookkeeping step would turn the accelerator into a
  liability. Surfacing an error was rejected outright as a violation of Principle VII's rule that
  no endpoint may return an error because of the cache. Retrying inline was rejected because it
  adds latency to the very request the accelerator exists to speed up. FR-015b states it, and the
  result is that a store failing writes degrades to no accelerator at all rather than to an outage.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - A guest fills in a form without waiting on master data (Priority: P1)

A guest opening a booking or registration form needs the list of gender options before the form
is usable. Today every such visit reads the master list from the database. The list is
identical for every guest and changes only on deployment, so the read is repeated work that
each guest waits on and that occupies a database connection other work needs under load.

**Why this priority**: it is the whole point of the feature, and it is the only story that
delivers value on its own. Everything else here protects it.

**Independent Test**: exercise the guest form paths with the accelerator enabled and confirm the
options presented are identical to those served without it, while the database is consulted for
the master list at most once per accelerator lifetime rather than once per visit.

**Acceptance Scenarios**:

1. **Given** the accelerator is warm, **When** a guest opens a registration or booking form,
   **Then** the gender options shown are exactly those the database holds, and no database read
   of the master list occurs.
2. **Given** the accelerator is cold, **When** two guests open a form simultaneously, **Then**
   both are served correctly and the database is read once, not twice.
3. **Given** the accelerator is disabled by configuration, **When** a guest opens a form,
   **Then** they see exactly the same options, produced by a direct database read.

---

### User Story 2 - An operator changes the master list and the change takes effect (Priority: P1)

The master list changes only by migration, and a migration is always accompanied by a
deployment. An operator who adds or retires a gender must be able to rely on the change being
visible afterwards, without knowing that an accelerator exists.

**Why this priority**: equal to P1 above because without it the feature is a correctness bug
rather than an accelerator. A stored copy with no refresh trigger would serve the pre-migration
list until its expiry lapsed — and would have served it indefinitely under the spec's original
no-expiry design. This story is what stops that.

**Independent Test**: change the master list, complete a deployment, and confirm the new list is
served without any manual cache operation.

**Acceptance Scenarios**:

1. **Given** a migration has added a gender and the service has been deployed, **When** a guest
   opens a form, **Then** the new gender is offered.
2. **Given** a migration has retired a gender and the service has been deployed, **When** a
   guest opens a form, **Then** the retired gender is no longer offered, and checkout still
   accepts it on a slot that already held it.
3. **Given** an operator has corrected the master list directly in the database without a
   deployment, **When** they invoke the existing operator refresh, **Then** the corrected list
   is served on the next read.

---

### User Story 3 - The accelerator fails and nobody notices (Priority: P2)

The accelerator becomes unreachable mid-traffic. Guests continue to book and register; nothing
they can see changes except response time.

**Why this priority**: P2 because it protects an already-working system rather than delivering
new value, but it is not optional — an accelerator that can take down the purchase path is a
worse trade than no accelerator.

**Independent Test**: make the store unreachable under load and confirm every affected path
still answers correctly from the database.

**Acceptance Scenarios**:

1. **Given** the store is unreachable, **When** a guest opens a form or submits a registration,
   **Then** the request succeeds using the database and no error is shown.
2. **Given** the store is unreachable, **When** an operator checks service health, **Then**
   health reports degraded rather than failing.

---

### Edge Cases

- **The stored copy cannot be decoded** — most likely a shape change deployed over a still-live
  entry. The stored value is discarded and the database is read, then stored again in the current
  shape so the bad entry is replaced rather than re-read forever; a guest never sees a partial or
  garbled option list.
- **The store accepts reads but rejects the write-back** — the guest is served from the database
  and sees nothing unusual, the failure is counted, and the next reader simply repeats the read
  (FR-015b). A store in this state degrades to no accelerator at all, never to an outage.
- **The store is emptied while traffic is flowing** — the next read of each projection rebuilds
  from the database. Nothing is lost, because nothing exists only in the store.
- **Two projections disagree** — the active-only list and the all-known list are derived from the
  same table and must never present a contradiction, such as a name absent from the all-known
  list but present in the active one.
- **A retired gender is submitted at checkout** — must continue to be accepted on a slot that
  already held it, exactly as today. Serving that projection from the accelerator must not
  narrow it to active entries.
- **A registration submits a retired gender** — must continue to be refused, exactly as today.
  Serving that projection from the accelerator must not widen it to retired entries.
- **The accelerator is enabled but was never warmed and the database is also unavailable** — the
  request fails as it does today. The accelerator adds no new guarantee here and must not be
  credited with one.
- **The store is unreachable at service start** — the automatic refresh does not run and nothing
  reports an error to a guest, so a pre-migration list can survive the deployment meant to replace
  it. The service starts anyway, the degraded start is recorded, and expiry is what bounds the
  window (FR-008a).

## Requirements *(mandatory)*

### Functional Requirements

**Scope**

- **FR-001**: The gender master list MUST be served from the shared read accelerator on every
  request path that reads it, rather than from the database on each request.
- **FR-002**: Both projections of the list MUST be served: the **active-only** projection that
  supplies form options and validates a registration, and the **all-known** projection,
  including retired entries, that checkout uses to resolve a value a restored form still holds.
  They MUST remain independently correct and MUST NOT be collapsed into one.
- **FR-003**: The fee master list MUST NOT be cached, and the reason MUST be recorded rather than
  left as an omission: it is read inside the order-writing transaction, where a call to the
  accelerator is forbidden and is actively refused. Moving that read out of the transaction to
  make it cacheable is out of scope and MUST NOT be done as part of this feature.
- **FR-004**: The order status master list MUST NOT be cached, and again the reason MUST be
  recorded: it is never read as a list, only resolved inside database queries. There is no read
  to accelerate without rewriting those queries, which is out of scope.
- **FR-005**: No master list other than genders MUST be added to the accelerator by this feature.
  A future master list MUST be admitted only by the same governance route this feature follows.

**Freshness**

- **FR-006**: The stored gender entries MUST use the same configured expiry as every other cached
  surface. This feature MUST NOT introduce a per-surface expiry mechanism and MUST NOT change the
  expiry of any existing surface (clarified 2026-08-24, reversing the original "no expiry"
  instruction).
- **FR-007**: That expiry MUST be treated as a BACKSTOP bounding the damage of a refresh that was
  missed, and MUST NOT be the mechanism by which the system becomes correct. A design whose only
  means of making a changed list visible is waiting for expiry MUST be rejected.
- **FR-008**: Correctness MUST come from an explicit, automatic refresh: the system MUST discard
  the stored gender entries when the service starts, so that any migration — the only thing that
  changes the list — is picked up by the deployment that carries it, with no operator action.
- **FR-008a**: If the store is unreachable when the service starts, the service MUST still start
  and serve. The missed refresh MUST be recorded as a degraded start rather than passed over in
  silence, and the expiry of FR-006 is what bounds how long a pre-migration list can be served
  (clarified 2026-08-24).
- **FR-009**: The refresh of FR-008 MUST be scoped to master data. Service startup MUST NOT
  discard other cached surfaces, whose contents are expensive to rebuild and are already kept
  correct by their own write-triggered invalidation.
- **FR-010**: The existing operator refresh MUST continue to clear these entries along with all
  others, so a direct database correction remains recoverable without a deployment.
- **FR-011**: If a write path for master data is ever built, it MUST invalidate these entries on
  commit, in the same way every other cached surface is invalidated. This feature MUST NOT be
  built in a way that makes adding that invalidation a redesign.

**Correctness and safety**

- **FR-012**: The database MUST remain the only source of truth. No gender datum may exist only
  in the accelerator, and emptying the accelerator entirely MUST leave the system fully correct
  at the cost of one rebuild.
- **FR-013**: An accelerator failure MUST NOT surface to a guest. Every affected read MUST fall
  back to the database and succeed, and no endpoint may return an error because the accelerator
  is unavailable.
- **FR-014**: Service health MUST report degraded, not failing, when the accelerator is
  unreachable and the database is healthy.
- **FR-015**: Concurrent misses for the same projection MUST collapse into a single database
  read, so a cold start under load does not multiply the work it exists to avoid.
- **FR-015a**: A read that misses MUST fall back to the database **and store the value it read**,
  so that the next read of that projection is served without touching the database. Reading
  through on every miss without storing the result MUST NOT be accepted as an implementation of
  this feature: it would satisfy every other requirement here while delivering none of the
  benefit (clarified 2026-08-24).
- **FR-015b**: That store-back MUST be best-effort. If the store rejects the write, the caller
  MUST still receive their value and MUST NOT see an error; the failure MUST be counted so it is
  visible to an operator afterwards; and the only consequence MUST be that the next reader repeats
  the database read (clarified 2026-08-24).
- **FR-015c**: Populating on a miss MUST NOT be conflated with invalidating. A miss MUST NOT
  trigger an invalidation — there is nothing stale to orphan, and invalidating would additionally
  discard the other projection's entry and defeat the feature. Invalidation happens only on the
  events of FR-008 and FR-011.
- **FR-016**: The values stored MUST be the domain's own representation, never the database
  layer's generated row types, so the storage schema is not smuggled into a second contract.
- **FR-017**: The accelerated reads MUST reach the store only through the existing shared
  interface, never a store client held directly by the domain.
- **FR-018**: A single configuration switch MUST disable the accelerator and return these reads
  to the database with no other behavioural difference.
- **FR-019**: The response a caller receives MUST be byte-identical whether it was served from
  the accelerator or the database.

**Governance**

- **FR-020**: This feature adds a surface to a list the constitution closes, and therefore MUST
  NOT be implemented before Principle VII is amended to admit it. The amendment MUST be part of
  this change, not a follow-up.
- **FR-020a**: The amendment MUST admit the **gender master list specifically**, named the way the
  existing families are named, and MUST NOT admit master data as a category. Any future master
  list is admitted only by its own amendment (clarified 2026-08-24).
- **FR-021**: The amendment MUST update the architecture, product and schema governance
  documents in the same change, per the repository's amendment rule. Where a document is
  genuinely unaffected, that MUST be verified and stated rather than assumed.
- **FR-022**: The amendment MUST address the constitution's existing rule that invalidation is
  triggered by commit and never by time. As revised by this session the feature now matches that
  rule's SHAPE — an explicit event is what makes it correct, and expiry is only a backstop behind
  that event — but the event is service start rather than a commit, because this data has no write
  path to commit. The amendment MUST recognise that case explicitly rather than leave the feature
  reading as a violation (revised 2026-08-24; the earlier wording, written when the surface had no
  expiry at all, said the feature satisfied neither branch).
- **FR-023**: The guest purchase and free registration journeys are covered flows, so acceptance
  coverage MUST travel with this change, and MUST pass with the accelerator both enabled and
  disabled.

### Key Entities

- **Gender master list**: the set of gender values the product recognises. Two projections are
  read: **active**, the values currently offered on a form, and **all-known**, every value ever
  recorded including retired ones. A retired value keeps working wherever it was already stored;
  it simply stops being offered.
- **Master data surface**: the accelerator's notion of a cached list, which this feature extends
  to cover master data. It carries the identity used to discard entries and the classification
  used to report accelerator activity.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Across any number of guest form visits between deployments, the master list is read
  from the database at most once per accelerator lifetime, down from once per visit.
- **SC-002**: 100% of gender option lists presented to guests are identical with the accelerator
  enabled and disabled, on every path that reads them.
- **SC-003**: A gender added or retired by migration is visible to guests immediately after the
  deployment completes, with zero manual operator steps.
- **SC-004**: 0 guest-facing requests fail because the accelerator is unavailable, measured with
  the store stopped under load.
- **SC-005**: 100% of checkout submissions carrying a retired gender on a slot that already held
  it continue to be accepted, and 100% of registrations carrying a retired gender continue to be
  refused — unchanged from before this feature.
- **SC-006**: A cold start serving concurrent first-time requests produces exactly one database
  read of each projection.
- **SC-007**: The full acceptance suite passes with the accelerator enabled and with it disabled.
- **SC-008**: 0 cached surfaces change their expiry behaviour as a result of this feature, and 0
  changes are made to the shared storage contract.
- **SC-009**: With the store unreachable at service start, 100% of requests still succeed, the
  degraded start is visible to an operator afterwards, and no pre-migration list is served for
  longer than the configured expiry.
- **SC-010**: Following a single miss of either projection, the next read of that projection is
  served without a database read — demonstrating that the miss populated the store rather than
  merely reading past it.
- **SC-011**: When the store rejects a write-back, 100% of affected requests still return the
  correct value and the failure is visible to an operator afterwards.

## Assumptions

- **Startup refresh is what makes a changed list take effect; expiry is the backstop behind it.**
  The list changes only by migration; a migration is always accompanied by a deployment; a
  deployment restarts the service. Discarding the entries at startup therefore ties freshness to
  the only event that can change the data, while the expiry bounds the damage when that refresh
  does not happen (FR-008a). Confirmed in clarification 2026-08-24, which restored the standard
  expiry the spec had originally been written without.
- **A deployment restarts at least one instance.** Weaker than it first appears, and deliberately
  so: the refresh discards entries centrally, so a single instance starting clears the list for
  every instance. A rolling deployment simply repeats a harmless discard. The earlier and stronger
  assumption — that a deployment restarts *every* instance — is not needed and is withdrawn.
- **The two-row size of the list is incidental, not load-bearing.** The design would be the same
  for a master list of a few hundred rows; nothing here depends on the list being tiny.
- **Existing accelerator infrastructure is reused.** The read-through path, miss collapsing,
  failure handling, metrics, the disable switch and the operator refresh all exist and are not
  rebuilt. What this feature adds is a surface, a freshness rule for it, and the governance to
  permit both.
- **No user-visible behaviour changes.** Success for this feature is that nothing changes except
  the number of database reads. Any observable difference in what a guest sees is a defect.

## Out of Scope

- Caching the fee master list, or moving its read out of the order-writing transaction to make it
  cacheable (FR-003).
- Caching the order status master list, or rewriting the queries that resolve it inline (FR-004).
- Building administrative create, update or delete for any master list. This feature must not
  block one, but does not deliver one.
- Caching anything that is not a master list. The constitution's existing closed surface list is
  otherwise unchanged by this feature.
- Any change to what the gender projections contain, to which paths read which projection, or to
  how a retired gender behaves at checkout or registration.
- Per-surface expiry, and any widening of the shared storage contract to support it. Considered
  and rejected in clarification (2026-08-24): exempting one surface from expiry would mean
  changing a contract every domain uses so that a single surface behaves unlike all the others.

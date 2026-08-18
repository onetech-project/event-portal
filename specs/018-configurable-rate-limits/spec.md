# Feature Specification: Configurable Rate Limits

**Feature Branch**: `fix/rate-limit`

**Created**: 2026-08-18

**Status**: Draft

**Input**: User description: "i want to make the rate limit to be configurable through the env, the we can turn it on and off and set the config through the env and we can change it on runtime not build time"

## Clarifications

### Session 2026-08-18

- Q: Does "change it on runtime" require changes to take effect without restarting the
  service process? → **A: No.** Configuration is read when the process starts. Changing a
  value means changing the deployment's environment and restarting the service — no new
  build artifact. Hot reload of a live process is explicitly out of scope.
- Q: Which throttles are in scope? → **A: All six.** The four per-client request
  limiters, the per-order guest ticket-email resend cooldown, and the per-client cap on
  concurrent live-status connections.
- Q: Master switch only, or per-surface switches too? → **A: Both.** One switch that
  disables all throttling, plus an individual switch per throttled surface. Both are
  supplied the same way as every other setting.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Tune a throttle without producing a new build (Priority: P1)

An operator watching production sees that guests organising a group booking are being
refused too aggressively. Today the numbers that decide this are fixed inside the shipped
artifact, so loosening them means a code change, a review, a rebuild and a redeploy —
hours, for a value that should take a minute. The operator wants to set the allowance and
the burst for each throttled surface from the deployment's own configuration, change it
there, and restart the service to pick it up.

**Why this priority**: This is the core of the request and the largest source of pain.
Without it, every throttle number stays welded to the artifact.

**Independent Test**: Change the configured allowance for one throttled surface, restart
the service, drive that surface until it refuses, and confirm the refusal arrives at the
newly configured threshold rather than the old one — with no new build artifact produced.

**Acceptance Scenarios**:

1. **Given** a deployment that sets no throttle configuration at all, **When** the service
   starts and each throttled surface is driven to its refusal point, **Then** every
   surface refuses at exactly the threshold it refuses at today.
2. **Given** a deployment that configures the booking surface to a higher allowance,
   **When** a client issues bookings at a pace that is refused under the default,
   **Then** the requests are accepted.
3. **Given** a deployment that configures the booking surface to a lower allowance,
   **When** a client issues bookings at a pace that is accepted under the default,
   **Then** the excess requests are refused with the same refusal response guests receive
   today.
4. **Given** a configured value that is not a number, or is negative, **When** the service
   starts, **Then** it refuses to start and reports which setting is wrong, rather than
   silently substituting a default.
5. **Given** the service is running with one set of values, **When** the configuration is
   changed and the service restarted, **Then** the new values are in force and no build
   artifact was produced in between.

---

### User Story 2 - Turn throttling off (Priority: P1)

An operator needs throttling out of the way — running a load test against a staging copy,
or clearing a production incident where the limiter itself is refusing legitimate traffic.
They want one switch that takes every throttle out of the path, and they want to put them
all back the same way.

**Why this priority**: A kill switch is what makes the rest safe to change. It is also
what this project demands of its other optional subsystem, the read cache. That parity is now
binding rather than aspirational: Constitution **Principle IX** requires the two-granularity
kill switch, and **Principle VIII** names `E2E_RATE_LIMIT_ENABLED` alongside
`E2E_CACHE_ENABLED` as an acceptance obligation.

**Independent Test**: Start the service with throttling switched off, issue a burst far
beyond every configured threshold against every throttled surface, and confirm nothing is
refused for throttling reasons; restart with it switched on and confirm the refusals
return.

**Acceptance Scenarios**:

1. **Given** throttling is switched off, **When** a client issues requests far above every
   configured threshold on every throttled surface, **Then** none are refused for
   exceeding a throttle.
2. **Given** throttling is switched off, **When** the guest journey is exercised end to
   end, **Then** behaviour is identical to a throttled deployment in every respect other
   than the absence of refusals.
3. **Given** throttling is switched off, **When** a guest requests a ticket-email resend
   twice in a row, **Then** both are accepted — no refusal is returned — and the only wait
   between them is the short client-side debounce that exists in every mode (FR-012).
4. **Given** throttling is switched back on — which means the process was restarted, since
   configuration is read at startup (Clarifications Q1) — **When** a client exceeds a
   threshold, **Then** the refusal returns, and traffic sent while throttling was off is not
   charged against that client, because no bucket survived the restart.

---

### User Story 3 - Switch one throttle off without switching them all off (Priority: P2)

An operator finds that one surface's throttle is the problem — checkout is refusing
legitimate buyers during a launch spike — and wants to relieve exactly that one, leaving
every other protection standing.

**Why this priority**: The incident case is real, and the master switch in US2 is a blunt
answer to it: killing every throttle to rescue one leaves unauthenticated, mail-sending
and money-costing surfaces bare for the length of the incident. Separable from US1 and
US2, so it can ship after them.

**Independent Test**: Switch off exactly one throttled surface, confirm it stops refusing,
and confirm every other throttled surface still refuses at its threshold.

**Acceptance Scenarios**:

1. **Given** one throttled surface is switched off individually, **When** it is driven far
   beyond its former threshold, **Then** nothing is refused on it.
2. **Given** the same deployment, **When** a different throttled surface is driven past
   its threshold, **Then** it still refuses.
3. **Given** the master switch is off, **When** an individual surface is switched on,
   **Then** that surface is still not throttled — the master switch wins.
4. **Given** the master switch is on and a surface's own switch is unset, **When** that
   surface is driven past its threshold, **Then** it refuses; an unset per-surface switch
   means enabled.

---

### Edge Cases

- **A value that creates a permanent lockout.** An allowance of zero paired with a
  non-zero burst produces a bucket that spends its burst and never refills, locking that
  client out until the process restarts. This must be refused as configuration, not
  discovered in production.
- **The guest resend window is user-visible.** The confirmation screen counts down from
  the resend allowance, and the refusal tells the guest how long is left. Reconfiguring
  that allowance changes what guests are *told*, not only what they are *allowed* — the
  displayed countdown must follow the configured value, never a second copy of it.
- **Retention shorter than the window it retains.** Per-client throttle state is forgotten
  after an idle period. If that idle period is configured shorter than the cooldown window
  itself, a waiting caller is silently forgiven mid-cooldown and the limit stops meaning
  anything. This relationship must hold across every combination an operator can
  configure, not only the defaults.
- **A setting present but empty.** An empty value means the documented default, consistent
  with every setting this service already reads, and must never be read as zero — which is
  the lockout shape above.
- **The connection cap has no burst.** The live-status limiter counts concurrent held-open
  connections, not a request rate, so it takes a single ceiling rather than an
  allowance-and-burst pair. Its configuration must not be forced into the wrong shape.
- **Master switch versus per-surface switch disagreeing.** Both can be set, and they can
  contradict. The precedence must be defined once and be the same for every surface.
- **Turning throttling off is not turning protection off.** The surfaces that are
  unauthenticated, send real mail, or cost a real outbound payment-gateway call are still
  all of those things when the switch is off. The live-status connection cap deserves its
  own mention: it is the only bound on held-open connections per client, and each held
  connection also issues a periodic database read, so with it off both are unbounded. Disabling throttling is an operator
  decision with a blast radius that must be stated where the operator will read it.
- **A deployment that configures nothing.** The overwhelmingly common case. It must be
  indistinguishable from today's system, not merely close to it.

## Requirements *(mandatory)*

### Functional Requirements

#### Scope of what becomes configurable

- **FR-001**: Every throttle threshold MUST be settable from the deployment's configuration,
  without producing a new build artifact. Five surfaces are compiled-in constants today; the
  public ticket lookup's allowance and burst are already environment-settable, and this
  feature brings them under one scheme rather than introducing configurability there (see
  FR-006a). Those two names are also published in the API contract at `api/openapi.yml`,
  which MUST be updated in the same change.
- **FR-002**: The six throttled surfaces in scope MUST be. Five of the six are keyed *per
  client*, so the identity used for that keying is part of this feature's scope, not an
  assumed given (see FR-002a):

  | # | Surface | Shape | Configurable values |
  |---|---------|-------|---------------------|
  | 1 | Booking request | per-client request rate | allowance, burst, enabled |
  | 2 | Availability check | per-client request rate | allowance, burst, enabled |
  | 3 | Public ticket lookup | per-client request rate | allowance, burst, enabled |
  | 4 | Checkout (costs an outbound gateway call) | per-client request rate | allowance, burst, enabled |
  | 5 | Guest ticket-email resend | per-order cooldown | **window**, burst, enabled |
  | 6 | Live payment status connections | per-client concurrent-connection cap | ceiling, enabled |

- **FR-002a**: The client address a per-client throttle is keyed on MUST be derived from the
  connection by default. A caller-supplied forwarding header MUST be honoured only from
  proxy addresses named in configuration. *(Today no such derivation is configured, so the
  header is trusted from anyone and all five per-client throttles are bypassable by rotating
  it — see research.md R7. Without this requirement, every threshold this feature makes
  configurable remains unenforceable.)*
- **FR-002b**: Deployments that legitimately sit behind a reverse proxy MUST be able to
  restore header-based client identification through configuration alone, without a code
  change.

- **FR-003**: Each rate-shaped surface MUST expose its sustained allowance and its
  short-term burst independently of one another. The resend cooldown is the exception: it
  MUST be expressed as the wait itself — a duration — because that duration is the number a
  guest is shown, and a fractional allowance is a value no operator can verify by eye.
- **FR-004**: The connection-cap surface MUST expose a single ceiling and MUST NOT be
  given an allowance/burst pair it has no meaning for. It also keeps no idle state — its
  accounting is released when the connection closes — so the retention setting in FR-005
  does not apply to it either.
- **FR-005**: Idle throttle state — per client for the rate-limited surfaces, per order for
  the resend cooldown — MUST be retained for a configurable period. This is a **single
  setting shared by every surface that retains state**, matching today's single shared
  value, not one setting per surface. The connection cap is excluded per FR-004.
- **FR-006**: Every setting MUST default to the value in force today, so a deployment that
  configures nothing behaves exactly as the current system does.
- **FR-006a**: Two settings are *already* operator-configurable today — the public ticket
  lookup's allowance and burst. They MUST keep working under the names a deployment may
  already have set. Where a new name is introduced for the same value, the existing name
  MUST continue to be honoured, the new name MUST win when both are set, and use of the old
  name MUST be reported at startup. *(A hard rename would satisfy FR-006 while silently
  reverting any deployment that has actually tuned them — the worst failure mode this
  feature can have.)*

#### On and off

- **FR-007**: A single master switch MUST disable all throttling in scope, returning the
  system to unthrottled behaviour with no other behavioural difference.
- **FR-008**: Each of the six surfaces MUST additionally carry its own switch, so one can
  be disabled while the others stay in force.
- **FR-009**: Precedence MUST be: master switch off disables every surface regardless of
  its own switch; master switch on defers to each surface's own switch; an unset
  per-surface switch means enabled. An **unset master switch** also means enabled, which is
  the state of every existing deployment on the day this ships.
- **FR-010**: With a surface disabled, no request to it MUST be refused for exceeding a
  throttle, and no throttle-related refusal MUST appear in any response from it.
- **FR-010a**: A disabled surface MUST be disabled by *substitution* — a pass-through that
  allocates no throttle state — not by leaving the throttle in place and skipping it. This
  mirrors the discharge [ARCHITECTURE.md](../../ARCHITECTURE.md) records for the read cache,
  where `CACHE_ENABLED=false` substitutes `cache.NoOp{}`. Routing MUST be otherwise
  unchanged: an unmatched path MUST answer identically with throttling on and off.
- **FR-011**: Disabling and re-enabling MUST be symmetric: traffic a client sent while a
  throttle was off MUST NOT be charged against it once the throttle is on again.
- **FR-012**: With the guest resend cooldown disabled, the guest MUST NOT be held behind
  the *enforced* cooldown — the minute-long wait disappears. A short client-side debounce
  (currently five seconds) remains in every mode: disabling the server-side throttle stops
  the refusals but not the consequence, since each press still sends a real email, and a
  held key would flood the buyer's inbox. *(Amended during planning — see research.md R6.
  The original wording was unsatisfiable without deleting a guard that exists for a
  documented reason.)*

#### Validation and safety

- **FR-013**: Invalid configuration MUST be rejected at startup, naming the offending
  setting, rather than silently substituting a default — matching how this service already
  treats most settings it reads. Note the two pre-existing rate-limit settings are currently
  *unvalidated*: a deployment that today starts with a zero or negative value will refuse to
  start after this change. That is intended, and MUST be called out in the documentation
  FR-018 requires.
- **FR-014**: Configuration that would produce a permanent lockout MUST be rejected. There
  are three distinct shapes, and all three MUST be caught: an allowance that never refills
  (rate ≤ 0) paired with a finite burst; a **burst of zero**, which refuses every request
  forever regardless of allowance because a token bucket grants only when the request size
  does not exceed the burst; and a connection ceiling of zero, which accepts no stream at
  all.
- **FR-015**: Configuration MUST be rejected where the shared idle-retention period is not
  strictly greater than the longest window it retains — computed per surface as burst
  divided by allowance for the rate-shaped surfaces, and as the cooldown window itself for
  the resend. The check MUST consider every *enabled* surface, since one shared retention
  serves them all. Retention shorter than a window silently forgives waiting callers: the
  limit appears to work while enforcing nothing.
- **FR-015a**: Because retention is shared, raising one surface's burst can push its refill
  window past the shared retention and make an otherwise reasonable tuning invalid — raising
  the booking burst from 5 to 100 at the default allowance needs 303s against a 180s
  retention. The rejection MUST therefore name the minimum retention that would make the
  configuration valid, so the operator is told what to set rather than only that they are
  wrong. *(Without this, FR-015 blocks the exact loosening User Story 1 is built around.)*
- **FR-016**: Every validation rule MUST be enforced against the *configured* values rather
  than the shipped defaults, and MUST be demonstrated by at least one rejected configuration
  per rule.

#### Observability and documentation

- **FR-017**: The throttle configuration actually in force MUST be written to the service's
  log at startup — the master switch, every surface's effective values, and a deprecation
  notice for each legacy name in use — so a deployment that believes it changed a limit can
  confirm that it did. It MUST NOT be exposed on any unauthenticated endpoint, including
  `/healthz`: publishing exact thresholds converts a limit into a documented allowance.
- **FR-018**: Every new setting MUST be documented alongside the project's other
  configuration settings, stating its default, its unit, what it protects, and — for the
  switches — what is left exposed when it is off.
- **FR-019**: The guest-facing wait shown after a resend MUST be derived from the enforced
  cooldown, so no configuration can make the displayed countdown disagree with the enforced
  one — except for the fixed client-side debounce named in FR-012, which is a send-guard
  rather than a report of the server's limit.

#### Acceptance coverage

- **FR-020**: The end-to-end acceptance suite MUST pass with throttling both enabled and
  disabled, in the same way it must already pass with the read cache both enabled and
  disabled.
- **FR-021**: The acceptance suite MUST include a scenario that drives a throttled surface
  past its configured threshold and observes the refusal, and the same scenario with that
  throttle disabled observing no refusal.
- **FR-022**: The acceptance suite MUST cover the per-surface switch: one surface disabled
  while another remains in force.
- **FR-023**: The acceptance suite MUST be able to exercise throttling without the buckets
  of one scenario affecting another — by driving a separately configured instance, or by an
  equivalent isolation the suite owns. Sharing one process's buckets across the whole run is
  not acceptable: throttle state is process-local and keyed per client, and every request in
  a local run arrives from one address, so a scenario that drains a bucket would refuse an
  unrelated scenario for as long as it takes to refill.
- **FR-024**: The suite MUST carry a throttling on/off switch of its own, supplied the same
  way the read-cache switch already is, so the both-modes obligation in FR-020 is runnable
  rather than aspirational.
- **FR-025**: The bypass named in FR-002a MUST arrive with a regression scenario that is
  confirmed failing against the unfixed code before the fix lands.
- **FR-026**: The retention setting (FR-005) and the guest-facing wait (FR-019) MUST each be
  covered by at least one acceptance scenario exercising a *valid* configured value — not
  only by the rejection rules that test invalid ones.

### Key Entities

- **Throttle Policy**: The named, per-surface set of values governing one throttled
  surface — its threshold (an allowance-and-burst pair, or a single ceiling for the
  connection cap), its idle-retention period, and whether it is active. Configuration
  only; nothing is persisted and nothing is shared between service instances.
- **Master Switch**: The single operator control that takes every throttle policy out of
  the request path at once, overriding each policy's own switch.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 100% of the throttle thresholds fixed in the artifact today can be changed
  by an operator without producing a new build artifact.
- **SC-002**: An operator can change any single throttle threshold and have it in force in
  under 5 minutes, down from a full build-and-release cycle.
- **SC-003**: A deployment that configures none of the new settings passes the full
  acceptance suite, and every threshold refuses at exactly the point it refuses today. The
  one deliberate behavioural difference is the client-identity repair in FR-002a: a
  deployment that today sits behind a proxy without declaring it will find its clients
  collapsing onto the proxy's address until `TRUSTED_PROXY_CIDRS` is set. This MUST be called
  out in the documentation FR-018 requires.
- **SC-009**: A client cannot obtain more than its configured allowance by varying request
  headers.
- **SC-010**: A deployment that sets only the two pre-existing ticket-lookup settings has
  exactly the thresholds it had before this change.
- **SC-004**: With throttling switched off, each throttled surface accepts at least 100
  attempts without a single throttle refusal — 100 sequential requests for the rate-limited
  surfaces, driven against enough distinct orders and remaining quota that no non-throttle
  refusal intervenes, and at least 100 simultaneously open connections for the connection
  cap.
- **SC-005**: With exactly one surface switched off, that surface produces no throttle
  refusal across at least 100 attempts (counted as in SC-004) while every other surface
  still refuses at its configured threshold.
- **SC-006**: The acceptance suite passes in both modes — throttling on and throttling off.
  Scenarios whose entire subject is a throttle refusal are expected to skip themselves in the
  throttling-off run, exactly as the cache-refresh scenarios already skip when the cache is
  off; every other scenario MUST run in both.
- **SC-007**: 100% of invalid configurations drawn from the documented failure shapes
  (non-numeric, negative, permanent-lockout, retention-shorter-than-window) are reported
  at startup with the offending setting named, and none of them reach a serving system.
- **SC-008**: An operator can read back the throttle configuration in force and confirm it
  matches what was deployed, for 100% of the settings.

## Assumptions

- The existing values become the defaults verbatim. This feature changes where the numbers
  come from, not what they are; retuning them is a separate, later decision by an operator.
- Configuration is supplied through the same environment-variable mechanism this service
  already uses for every other setting, including its existing local-file convenience and
  the rule that a value injected into the real environment always wins over that file.
- Configuration is read once, when the process starts. "Not build time" means no rebuild;
  it does not mean no restart.
- Refusing to start on invalid configuration matches this service's existing behaviour of
  validating all configuration up front and failing fast.
- Throttling remains per-instance. Each instance limits independently, which is the
  trade-off already accepted and documented for the current implementation.
- The set of throttled surfaces does not change. This feature makes existing throttles
  configurable; it neither adds throttling to anything unthrottled today nor removes it
  from anything throttled today. The *keying* of the existing per-client throttles does
  change, because it is currently broken (FR-002a) — that is a repair to a surface already
  in the list, not a new surface.
- The refusal response shape that guests and clients already receive is unchanged. Only
  *when* it is sent becomes configurable.
- No administrative console screen is added for tuning throttles; this is deployment
  configuration, not a product feature.
- The frontend requires no code change. Its behaviour does shift in one visible way when the
  resend cooldown is disabled: the countdown drops from the enforced window to the fixed
  five-second client-side debounce described in FR-012. That is a consequence of
  configuration, not an edit to the component.

## Out of Scope

- **Hot reload of a running process.** Applying a configuration change without a restart —
  by signal, by polling, or by an administrative endpoint — is explicitly excluded
  (Clarifications, Q1).
- Sharing throttle state across multiple service instances.
- Per-tenant, per-account or per-API-key throttle policies.
- An admin console screen for editing throttle policies.
- Adding throttling to surfaces that are not throttled today, or removing it from any that
  are.
- Changing the refusal response format or status code.

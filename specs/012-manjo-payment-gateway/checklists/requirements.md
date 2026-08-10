# Specification Quality Checklist: Manjo Payment Gateway Migration

**Purpose**: Validate specification completeness and quality before proceeding to planning
**Created**: 2026-08-10
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, APIs)
- [x] Focused on user value and business needs
- [x] Written for non-technical stakeholders
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic (no implementation details)
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover primary flows
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] No implementation details leak into specification

## Notes

- Items marked incomplete require spec updates before `/speckit-clarify` or `/speckit-plan`.

### Iteration 1 — 2026-08-10 (post-`/speckit-specify`)

**Failing**: "No [NEEDS CLARIFICATION] markers remain" — 3 markers open, all raised because the
supplied gateway contract did not answer them and no safe default existed:

| Marker | Requirement | Why it could not be defaulted |
| --- | --- | --- |
| Callback authentication | FR-012 | The notification contract carries no signature, token, or authenticating field. Guessing wrong leaves a payment-confirming endpoint open to forgery. |
| Transaction status query | FR-022 | The contract's inquiry response carries identifiers and payer details but no status. Whether the existing "check payment status" action and the reconciliation path survive depends on the answer. |
| Database configuration scope | FR-028 | "Add the url to the database" is explicit; whether credentials follow it is not, and credentials at rest need an encryption and access-control answer that environment configuration does not. |

### Iteration 2 — 2026-08-10 (post-`/speckit-clarify`)

**Result**: 15/16 → 16/16. All three markers resolved by direct answer; no regressions.

| Marker | Resolution |
| --- | --- |
| FR-012 | Gateway presents a bearer token; this project issues an API key and compares against it. |
| FR-022 | No status query exists. Notifications are the only source of truth; staff reconcile a lost one through a new admin surface (User Story 6). |
| FR-028 | Database holds the endpoint address only. The credential pair is vestigial and may be a constant; the callback API key stays in environment configuration. |

**Material facts discovered during clarification** (not answers to asked questions):

- Callback path is `/v1.0/callback/exec`, not the `/callbacl/exec` of the original description.
- The gateway waits 5 seconds, retries 3 times 10 seconds apart, and retries on any non-200 —
  so retries are certain, the budget is finite, and there is no dead-letter path.
- The QR session response now returns its own expiry (`qr_ea`), superseding the earlier
  assumption that the deadline had to be computed locally.
- The contract module version pinned in the backend build predates `qr_ea` and would discard it
  silently. Recorded as a blocking dependency, not an assumption.
- Amount composition confirmed as already-implemented behaviour, not a change.

**Sections added during clarification**: `## Clarifications`, User Story 5 (payment page, from the
supplied design), User Story 6 (staff reconciliation, from the FR-022 answer).

**Notes on borderline items**:

- The spec names concrete endpoint paths, JSON field names, timeout and retry values, and a
  character limit. These are properties of an external contract this feature must conform to —
  constraints on the solution, not choices within it — so they are recorded as requirements rather
  than counted as implementation leakage. Internal structure, language, and framework remain absent.

### Iteration 3 — 2026-08-10 (status delivery)

**Result**: 16/16 → 16/16. No checkbox changed state; no regressions.

One question asked and answered: with interval polling removed, the page falls back to periodic
reads only while the live stream is known to have failed, and a liveness watchdog makes the silent
buffering case detectable. Added FR-021d–i, US5 scenarios 9–12, four edge cases, and SC-015–017.

This narrows a decision taken in an earlier feature (polling as a standing fallback beneath the
stream) rather than reversing it. The original rationale — staying correct behind intermediaries
that buffer streaming responses — is preserved by FR-021e, which is what makes that failure
detectable instead of silent.

Requirements stayed protocol-neutral: no transport, framework, or client library is named.

### Iteration 4 — 2026-08-10 (duplicate and contradicting callbacks)

**Result**: 16/16 → 16/16. No checkbox changed state; no regressions.

One question asked and answered. A paid order is never moved automatically; a notification repeating
the recorded outcome passes quietly, while one contradicting it raises a distinguishable signal and
lands on a staff worklist for a decision. Added FR-016a–d, FR-022h, US2 scenarios 10–11, US6
scenarios 7–8, three edge cases, and SC-018–019. FR-019 was amended to route its mirror case to the
same worklist.

This closed the last open question embedded in the Edge Cases section ("A notification arrives for
an order that is already paid, with a *different* status … What is authoritative, and is a human
told?"). Every edge case is now a statement rather than an unresolved question.

### Iteration 5 — 2026-08-10 (single code, gateway-owned expiry)

**Result**: 16/16 → 16/16. No checkbox changed state; no regressions.

Three answers integrated: the mid-window code refresh is withdrawn (one order, one code, one
window); any usable expiry the gateway returns is adopted whatever its length, with a signal when it
departs from the configured expectation; and the request carries six required fields with no
requested expiry at all.

Added FR-002c, FR-009c–d, FR-010b–c, SC-020–021. Rewrote FR-002, FR-009b, FR-010, FR-010a, FR-021,
the Payment Session Request entity, and the deadline-ownership assumption. Removed the re-issue
suffix from every requirement, scenario, edge case, and assumption that referenced it.

**Consistency audit performed** (five clarification sessions have superseded a lot):

- 66 functional requirements and 21 success criteria, all sequential, no gaps or duplicate ids.
- FR-002's sub-items had drifted out of order (002, 002c, 002b, 002a) and were resequenced.
- Acceptance-scenario numbering verified contiguous in every user story.
- No surviving reference to a requested validity window, a re-issue suffix, or suffix stripping.
- Every remaining mention of "refresh" is either the deliberate withdrawal or an unrelated sense.

### Iteration 6 — 2026-08-10 (environment configuration, reversal)

**Result**: 16/16 → 16/16. No checkbox changed state; no regressions.

The database-configuration decision from Iteration 2 was reversed: no gateway configuration is stored
at all. User Story 4 was rewritten from "endpoint is configured in the database" to "connection is
configured through the environment", and its acceptance now turns on failing fast at startup rather
than on live reconfiguration. Provider-named variables are renamed to gateway-general ones; the
environment selector is dropped rather than renamed, and the base address becomes required with no
compiled-in default.

Rewrote FR-023 through FR-028b, SC-007, the configuration key entity, and two assumptions. Added
FR-023a, FR-026a, FR-030. Two edge cases added, one rewritten.

**Superseded content removed rather than left to contradict**:

- The Iteration 2 clarification bullet is annotated as superseded, keeping only the finding that
  survived it (the credential pair is vestigial).
- The deferred item "move the credential pair into configuration" was deleted — the rename does it.
- No requirement, scenario, entity, or assumption still claims configuration is stored, reloadable
  without restart, or switchable without a redeploy.

**Deliberate acceptances now recorded rather than left implicit**:

- SC-007 no longer promises a redeploy-free environment switch. That was the only measurable outcome
  database configuration bought, and it was traded for adding no table, column, or migration.
- With no environment selector, nothing can recognise a *stale* address. A development endpoint
  deployed to production starts cleanly and takes real orders; catching that belongs to deployment
  tooling, not to this system.

### Iteration 7 — 2026-08-10 (transport and outbound retry)

**Result**: 16/16 → 16/16. No checkbox changed state; no regressions.

Confirmed a premise rather than changing one: no vendor SDK is or ever was a dependency
(`go.mod` has no provider entry). The existing adapter already speaks the provider's HTTP interface
directly with an injectable endpoint and client, which is what makes offline testing possible.
Recorded as FR-008a so the capability is not lost while the adapter is replaced.

One question asked and answered: a failed session-open is retried a bounded number of times reusing
the same reference, rather than failing the guest's checkout on a transient blip. Added FR-007a–d
and FR-008a, three edge cases, and one dependency.

The gateway's behaviour on a repeated reference was then confirmed: it rejects it with a
duplicate-reference error once a code exists. That makes FR-010 enforced by the gateway rather than
only by our care — a retry can never produce a second live code. Added FR-007e–f for the state it
creates, rewrote FR-007b and FR-007d, replaced two edge cases, added one deferred item, and removed
the dependency the answer closed.

**The state the answer exposes**: a retry that meets the duplicate-reference error proves a code
exists while giving no way to reach it, because no call returns an existing code (FR-022). The order
is unpayable from birth. FR-007e therefore releases its seats promptly rather than holding them to a
deadline that cannot end in a payment — the guest never sees a code, so there is no reading under
which holding them helps. FR-007f routes a payment that somehow arrives anyway to the existing
worklist path rather than letting it vanish.

### Iteration 8 — 2026-08-10 (contract module: expiry field published, then re-typed)

**Result**: 16/16 → 16/16. No checkbox changed state; no regressions. **No questions asked** — the
change was a factual contract update with no decision left open.

Upstream published `qr_ea` mid-planning and then changed it from text to a timestamp type. Both
commits verified by full recursive diff; each touched one file and nothing else in the module moved.
The plan's pin advanced twice, ending at `v0.0.0-20260810052544-9d11826f4864`.

Added FR-009e and two edge cases. Amended FR-009a (instant preservation now spans parse, storage, and
every serving surface, since the type only covers the parse) and the version-trap clarification.

**Why this needed a requirement rather than just a version bump**: a timestamp type removes one class
of bug and introduces two.

- It settles the offset question — comparisons become instant-based, so FR-009a's timezone hazard is
  handled by the type rather than by discipline.
- But an **absent** value now decodes to a valid year-1 timestamp rather than to nothing. Treated as
  an ordinary expiry it reads as "already expired", which routes to the right fallback for the wrong
  reason and loses the signal FR-009b requires — the operator would never learn the field stopped
  arriving.
- And a **malformed** value can fail the surrounding decode, taking a perfectly good payload with it.
  FR-009e states the required outcome directly rather than leaving it to decoder error-handling
  subtleties: an unreadable timestamp affects the countdown, not whether the guest can pay.

**Open items carried into planning** (none block `/speckit-plan`):

- Fractional-rupiah totals: whether the gateway rejects, truncates, or rounds a total carrying
  centavos is unverified. Raised as an edge case; needs a decision during planning.
- The contract module version bump is a prerequisite for FR-009 and must be sequenced first.
- Two configuration names are unconfirmed: the callback key (suggested `PG_CALLBACK_TOKEN`, no
  predecessor to rename) and the surviving payment-window value after the FR-030 collapse.
- The credential mapping is assumed, not confirmed: `PG_SERVER_KEY` → `client_secret` and
  `PG_CLIENT_KEY` → `client_id`. Because the pair is vestigial, a swap would not fail loudly.
- Three deferred improvements are recorded under Assumptions: deriving the QRIS frame identity from
  the payload, moving the credential pair into configuration, and detecting paid-but-expired orders
  proactively.

### Iteration 9 — 2026-08-10 (recovery by redelivery, not by assertion)

**Result**: 16/16 → 16/16. No checkbox changed state; no regressions.

Three answers integrated, and one of them invalidated a researched "fact" that had already reached
two documents as a requirement.

**The correction that mattered most**: the spec asserted "no dead-letter path and no manual
redelivery". That was *inferred* from the gateway's automatic-retry code — which describes only what
the gateway does by itself — and stated as though it had been observed. The gateway does offer an
operator-invoked resend, per transaction. Everything User Story 6 was built on followed from the
wrong half of that inference.

**What changed as a result**:

- FR-019 inverts. A completed payment for an expired order now **settles** it instead of being
  refused; FR-019a–d add the redelivery framing, the in-full-or-not-at-all re-deduction, the
  quota-short refusal (answered 200 so the gateway does not burn its retry budget on an attempt that
  would fail identically), and the restriction to expired orders only.
- FR-022d reverses from "staff MUST be able to record a payment" to "the system MUST NOT offer any
  way for a person to record a payment". Ticket issuance keeps exactly one trigger.
- FR-022h reverses from "there MUST be a worklist" to "there MUST NOT be one", with the reasoning
  preserved rather than deleted — see below.
- FR-016a gains an explicit statement of the asymmetry it now creates with FR-019, because the pair
  reads like a contradiction until you notice that one revives and the other refuses to revoke.
- FR-021's "the order lifecycle MUST be unchanged" gains a carve-out. It was about to be quietly
  false, which is worse than an admitted exception.

**Two reversals, both recorded with their original reasoning intact** rather than edited to look like
they were always the current answer:

- The worklist was justified by "an order nobody knows to search for is not findable". The flaw was
  that the case it existed for — a notification that never arrived — writes no row at all and so
  never appeared on it. It listed only anomalies the system already shouts about, and once manual
  confirmation went, nothing removed an entry from it.
- `ORPHAN_PAYMENT` and `MANUAL_RECONCILED` are withdrawn and replaced by `SETTLED_AFTER_EXPIRY` and
  `SETTLE_REFUSED_NO_QUOTA`. Not a rename: the old pair recorded refusals and human assertions, the
  new pair records a settlement that happened and a settlement that could not.

**Deliberate acceptances now recorded rather than left implicit**:

- A redelivered notification may settle an order of **any age**, including one whose event has
  already happened. The operator has just examined that order and chosen to resend it, so a guard
  would be catching their mistake rather than the system's.
- SC-019 is rewritten. It promised every change to a paid order was "attributable to a named person";
  with no human path left there is no person to name, and the honest guarantee is stronger — nothing
  moves a paid order at all.

**Carried into planning, not resolved here** (both are operational hazards the audit surfaced, and
both are about the quota top-up in step 6 rather than about this system's payment logic):

- The ticket-type editor sets quota **absolutely**, not as a delta, and spec 002 already documents
  the read-modify-write race as an accepted risk with the advice "do not edit during active sales".
  Step 6 is by construction performed during active sales on a sold-out type. FR-022e now requires
  showing the operator the order's actual hold, which is necessary but not sufficient.
- Package orders hold quota in every constituent ticket type, and the hold is reconstructed from the
  package's *current* composition. A composition edit between expiry and redelivery would make the
  re-deduction differ from what expiry released.

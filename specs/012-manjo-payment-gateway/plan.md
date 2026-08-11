# Implementation Plan: Manjo Payment Gateway Migration

**Branch**: `012-manjo-payment-gateway` | **Date**: 2026-08-10 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/012-manjo-payment-gateway/spec.md`

## Summary

Replace the Midtrans payment adapter with one speaking the Manjo gateway's REST contract, and let the gateway own the payment deadline. A session is opened with six fields at `POST {PG_BASE_URL}/v1/manjo/transaction/incoming`; the response carries the QRIS payload and — new — the expiry the gateway will enforce, which becomes the order's deadline. Confirmation arrives at a new `POST /v1.0/callback/exec` endpoint authenticated by a bearer token this project owns, answered 200 within 5 seconds because the gateway retries any other answer three times.

Three capabilities are withdrawn because the new contract cannot support them or no longer needs them: the outbound status query (no such call exists), the guest's status-check button (nothing left for it to ask), and the mid-window QR refresh (the deadline is now the code's own lifetime). What compensates for the lost status query is not a capability of ours at all — the gateway can be asked to **redeliver** a notification, per transaction, so a lost one is recovered by replaying it through the path every ordinary purchase already uses. That adds one transition to the order lifecycle (`EXPIRED → PAID`) and two read-only admin views, and it deliberately adds no way for a person to record that a payment happened.

The whole feature is additive-and-subtractive within `internal/payment` plus a thin frontend change. **It requires no database migration**: gateway configuration moves to environment variables, and what this system concludes about a notification rides on marker rows in the existing `payments` audit table — the same technique the retired `QR_REISSUED` marker already used.

**Late addition (clarification 2026-08-11, second part)**: the callback endpoint answers in the same `{code, message, data}` envelope as the rest of the API. It currently speaks three shapes across its paths — an empty 200 on success, a bare object on the FR-019c refusal, and the proper envelope on errors (FR-012e). The error paths turn out to be already correct (`apperr.Error.Response()` renders that envelope and the shared handler writes it), so the change is two writes in `payment/handler.go` plus one registry arm in `pkg/apperr`. An acknowledgement carries **no data** (FR-012f) — what a notification did is recorded under FR-018 and read through the per-order history, not echoed back — which leaves the envelope's numeric code as the only thing separating a refusal from an acknowledgement, since both leave on a 200 status line. That makes `200001`/`TICKETS_UNAVAILABLE` load-bearing rather than cosmetic: `Numeric`'s fallback would render an unregistered code on a 200 as `200000`, identical to success. See [research.md R16](./research.md). No migration.

**Late addition (clarification 2026-08-11)**: the confirmation screen's ticket-email resend gains a cooldown the guest can see. The endpoint belongs to spec 008; what is built here is the contract that makes its limit legible — the remaining seconds on every answer, a validation refusal for a body that cannot be keyed, a log line per attempt, and a countdown that re-arms itself. It reaches `internal/notification` and `pkg/httpx` rather than `internal/payment`, and it carries a one-line frontend bug fix that must land ahead of it: the resend body is currently double-encoded, so the endpoint has been answering "accepted" while sending nothing. Still no migration.

## Technical Context

**Language/Version**: Go 1.26.5 (backend), TypeScript / Next.js App Router (frontend)

**Primary Dependencies**: Echo v4, pgx v5 + sqlc, shopspring/decimal, `gitlab.pg-poppay.com/company/shared/cdtc/go` — **pin `v0.0.0-20260810052544-9d11826f4864`**, which publishes the QR expiry field (`qr_ea`) as a timestamp type; the version currently in `go.mod` predates it and would drop the field silently. Frontend: TanStack Query, Zod, native `EventSource`. Outbound HTTP uses the standard library with an injectable `*http.Client` — no vendor SDK is or was used.

**Storage**: PostgreSQL. **No schema change, no migration.** Configuration moves to environment; reconciliation state rides on `payments.status` + `payments.raw_response`.

**Testing**: Go standard `testing` with `httptest` stub gateways (the existing `midtrans_test.go` pattern); frontend Vitest. The full purchase flow stays exercisable offline by pointing `PG_BASE_URL` at a stub (FR-008a).

**Target Platform**: Linux server under Docker Compose

**Project Type**: Web application — `backend/` (Go modular monolith) + `frontend/` (Next.js)

**Performance Goals**: Callback acknowledged within 5 s of arrival (SC-011 — the gateway's timeout, past which a duplicate delivery is guaranteed); confirmed payment visible on an open payment page within 5 s (SC-016); scannable code within 5 s of checkout for 95% of attempts (SC-002).

**Constraints**: No object storage (QR rendered on demand); no Redis/queue (Constitution VI); session-open must not run inside a DB transaction (Constitution IV); outbound retry bounded by guest patience, explicitly *not* the gateway's own 10-second pacing (FR-007c); reference ≤ 25 characters (FR-010a).

**Scale/Scope**: MVP single instance. ~1 backend domain touched (`payment`, with small `order`, `event`, and `config` edges), 1 frontend page, plus two read-only views on the existing admin orders surface.

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Verdict | Evidence |
| --- | --- | --- |
| I. Modular Monolith | **PASS** | All gateway work lands in `internal/payment/`, with edges in `internal/order/` (DTO fields) and `pkg/config/`. The resend cooldown touches `internal/notification/` and `pkg/httpx/` instead — a second existing domain and a shared transport helper. No new domain either way. |
| II. Domain Isolation | **PASS** — with a design constraint | The per-order payment history must not JOIN `payments` to `orders`, and neither must the seats-held-versus-remaining view. Resolved in Phase 1: what this system concludes about a notification is *recorded* where the order status is already in hand, so the history is a pure `payments` query; order details and remaining quota come through the order- and event-provider interfaces. See [data-model.md](./data-model.md). |
| III. DTO Isolation | **PASS** | New/changed shapes go in `payment/dto.go`, `order/dto.go`, and `notification/dto.go`; no sqlc struct crosses the wire. `retry_after_seconds` is a computed transport value with no storage behind it. |
| IV. Transactional Integrity & Idempotency | **PASS** — after a MINOR amendment | FR-008 keeps `CreateTransaction` outside any transaction. FR-016/FR-016a match the constitution's "already `PAID` → 200 without reprocessing" verbatim. Manjo's `Reject`/`Cancel`/`Expired` map onto the constitution's `deny`/`cancel`/`failure`/`expire` outcomes with the same atomic status-plus-quota transition. Post-payment work stays in the non-blocking goroutine — now load-bearing, because the gateway allows only 5 s. **FR-019 needed the principle extended**: it introduces the first webhook outcome that *deducts* quota rather than restoring it, which the original enumeration did not contemplate. Amended in 3.1.0 with the all-or-nothing re-deduction rule and the prohibition on any non-webhook route to `PAID`. |
| V. Payment Gateway Abstraction | **PASS** — interface narrows | `Gateway` keeps `CreateTransaction` and `VerifyWebhook` (the two the constitution names). `FetchStatus` is **removed** because the Manjo contract has no equivalent (FR-022). Removing a method the constitution does not require is not a deviation; the `order` domain is untouched by it. |
| VI. Guest-First MVP Scope | **PASS** | Guest flow stays account-free. Refunds are *not* implemented — refunding stays a human decision taken outside this system, which is the existing posture, not new scope. Nor is the recovery flow: the customer's proof, the operator's judgement, and the resend request all happen outside this app, which gains only two read-only views. The resend cooldown stays process-local for the same reason every other limiter here does: Redis is out of scope, so one instance limits independently. |
| Tech Stack: "initial concrete implementation is Midtrans SNAP Sandbox" | **DEVIATION — amendment required** | See Complexity Tracking. |
| Critical Data Flow: fee presentation, quota semantics, one-email delivery | **PASS** | Untouched. FR-002b confirms existing fee math rather than changing it. |
| SCHEMA.md is source of truth; schema changes update it | **PASS (vacuous)** | No schema change, so nothing to sync. |

### Post-Phase-1 re-check

Re-evaluated after design: **all gates still pass**, one of them only after an amendment. The design decision that could have broken Principle II — reading an order's payment history and what it holds — was resolved by recording conclusions at detection time and taking order and quota details through the provider interfaces, rather than deriving either from a cross-domain JOIN. Principle IV needed genuine extension rather than reinterpretation, and got it (3.1.0). No new violation appeared.

Re-checked after the 2026-08-11 callback-response clarification: **still all pass, no amendment needed, and one gate is satisfied more literally than before.** Principle IV already anticipated this shape — "the handler still answers `200 OK` (a body may carry the refusal; the status code may not, because a retry would fail identically)". The constitution granted the refusal a body; what the clarification adds is how that body identifies itself, which the principle left open. FR-019e fills the gap in the direction the principle points.

Principle III (DTO Isolation) is the other one touched, and the change moves toward it: the refusal stops being a bare domain-shaped object on the wire and becomes the envelope every other endpoint returns, while the acknowledgement needs no DTO at all. No sqlc struct crosses the wire, no domain boundary is crossed, no table moves. Worth stating rather than assuming: modelling a deliberate 200 as an `*apperr.Error` does not weaken Principle IV's idempotency guarantees — the refusal is the branch where *nothing* was applied, so there is no effect to duplicate, and it is reached only after the notification is durably recorded.

Re-checked again after the 2026-08-11 clarification: **still all pass, no amendment needed.** The cooldown is transport state in a shared helper and a handler decision in `notification`; it crosses no domain boundary, adds no table, and changes no order lifecycle. The one thing worth stating rather than assuming: moving the limit out of middleware and into the handler does not weaken it. The window is consulted before the order lookup, so every attempt past the body check still spends it (FR-021m), and the key is the order number exactly as before — what changes is that a request the server cannot key now costs nobody anything instead of costing everybody a window.

## Project Structure

### Documentation (this feature)

```text
specs/012-manjo-payment-gateway/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/
│   ├── gateway.md       # Outbound session-open + inbound callback (external contract)
│   └── api.md           # This system's own HTTP surface: added, changed, removed
├── checklists/
│   └── requirements.md  # Spec quality checklist (16/16)
└── tasks.md             # Phase 2 output (/speckit-tasks — NOT created here)
```

### Source Code (repository root)

```text
backend/
├── cmd/api/main.go                     # CHANGED: wire manjo gateway; drop refresh + status routes;
│                                       #          build the resend Cooldown, drop its route group
├── pkg/config/config.go                # CHANGED: PG_* vars; drop IS_PRODUCTION, QR_REFRESH_AFTER;
│                                       #          collapse PAYMENT_EXPIRY + PAYMENT_WINDOW
├── pkg/apperr/apperr.go                # CHANGED: + CodeTicketsUnavailable; Numeric → 200001 (R16)
├── pkg/httpx/
│   ├── cooldown.go                     # NEW: keyed bucket reporting its own remaining time
│   ├── cooldown_test.go                # NEW: seconds on both answers; eviction > window
│   ├── envelope.go                     # UNCHANGED — Respond(c, 200, nil) is the acknowledgement
│   ├── error_handler.go                # UNCHANGED — already envelopes every error path
│   └── rate_limit.go                   # CHANGED: RateLimitPerBodyField + bodyField DELETED
├── internal/notification/
│   ├── handler.go                      # CHANGED: bind → 400; cooldown → 429; log every outcome
│   ├── dto.go                          # CHANGED: PublicResendResponse gains retry_after_seconds
│   └── public_handler_test.go          # CHANGED: per-outcome table incl. the shared-bucket regression
├── internal/payment/
│   ├── manjo.go                        # NEW: Gateway impl — session open, callback verify+parse
│   ├── manjo_test.go                   # NEW: httptest stub, table-driven
│   ├── gateway.go                      # CHANGED: drop FetchStatus; VerifyWebhook takes a token
│   ├── dto.go                          # CHANGED: PaymentSession gains ExpiresAt semantics
│   ├── status.go                       # CHANGED: map cdtc status enum, not Midtrans strings
│   ├── service.go                      # CHANGED: retry+duplicate handling, contradiction markers,
│   │                                   #          settle-on-expiry routing; drop ReissueQR/RefreshStatus
│   ├── handler.go                      # CHANGED: /v1.0/callback/exec; drop refresh + reissue routes;
│   │                                   #          NoContent → enveloped ack; refusal via apperr
│   ├── reconcile.go                    # NEW: settle-after-expiry + the two admin read views
│   ├── reconcile_dto.go                # CHANGED: SettleRefusedResponse reduced to the shortfall list
│   │                                   #          (code + message move onto the envelope)
│   ├── repository.go / queries/        # CHANGED: marker queries; drop CountReissuedQRs
│   ├── midtrans.go + midtrans_test.go  # DELETED
│   ├── refresh_test.go, reissue_test.go# DELETED
│   └── stream.go                       # UNCHANGED (snapshot-on-connect already correct)
├── internal/order/
│   ├── dto.go                          # CHANGED: drop qr_refresh_after_seconds
│   └── service.go                      # CHANGED: drop QRRefreshAfter timer plumbing
└── migrations/                         # UNCHANGED — no migration in this feature

frontend/
├── lib/queries.ts                      # CHANGED: drop the 3s interval; keep the endpoint;
│                                       #          FIX the double-encoded resend body (land first)
├── lib/checkout-status.ts              # CHANGED: liveness watchdog + degraded-mode fallback
├── lib/types.ts                        # CHANGED: PublicResendResponse gains retry_after_seconds
├── components/order/order-confirmation.tsx  # CHANGED: ResendRow — server-driven countdown that
│                                       #          re-arms itself; wait ≠ failure wording
├── app/(public)/…/checkout/page.tsx    # CHANGED: QRIS frame, CTA states; drop refresh timer
└── app/(admin)/admin/orders/           # CHANGED: per-order payment history + seats held vs remaining
```

**Sequencing note**: the `queries.ts` body fix is independent of everything else here and restores a
broken user-facing capability by itself. It should land as its own change ahead of the cooldown work —
otherwise the new behaviour is built and tested on top of a call that never reaches the send path.

**Structure Decision**: Existing web-application layout. This feature is deliberately concentrated in `backend/internal/payment/` — the domain the constitution designates for gateway-specific logic — so that the `order` domain's only changes are the removal of a DTO field and a timer it no longer needs.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Constitution's Technology Stack names Midtrans SNAP Sandbox as the concrete payment implementation | This feature replaces it. The line is a statement of fact that stops being true on merge. | Leaving it would put the constitution in conflict with the code it governs. Governance requires a documented rationale, a version bump, and a Sync Impact Report update — a **PATCH** bump (3.0.0 → 3.0.1) since it is a factual correction, not a principle change. `ARCHITECTURE.md` and `PRD.md` mention Midtrans too and must be swept in the same change. |
| Principle V names `VerifyWebhook`; our gateway authenticates by bearer token rather than by verifying a signed payload | The Manjo contract carries no signature. The method keeps its name and role — authenticate an inbound notification and normalise it — but takes the presented token instead of a body signature. | Renaming the method would put the code at odds with the constitution's own wording for no behavioural gain. Keeping the name preserves the constitution's intent (a swappable gateway boundary) while the parameter reflects reality. |

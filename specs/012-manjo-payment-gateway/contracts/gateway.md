# Contract: Manjo Gateway (external)

**Feature**: [../spec.md](../spec.md) | **Research**: [../research.md](../research.md)

Two directions, two authentication schemes. Neither is negotiable from our side.

---

## 1. Outbound — open a payment session

```
POST {PG_BASE_URL}/v1/manjo/transaction/incoming
Content-Type: application/json
Accept: application/json
```

### Request

Six required fields. **No requested expiry** (FR-002c) — the gateway applies and reports its own.

```json
{
  "ri": "ORD-20260810-A7K2QX",
  "a": 117750,
  "c": "IDR",
  "m": 0,
  "ac": { "cr": { "client_id": "…", "client_secret": "…" } },
  "r": "Payment for goods",
  "pn": "John Doe"
}
```

| Field | Required | Source | Constraint |
| --- | --- | --- | --- |
| `ri` | yes | order number, unmodified | ≤ 25 chars; ours is 19 (FR-010a) |
| `a` | yes | frozen order total | bare JSON number; subtotal + percentage fees + flat fees (FR-002b) |
| `c` | yes | `"IDR"` | |
| `m` | yes | `0` (QR) | `method.Method` enum |
| `ac.cr` | yes | `PG_CLIENT_KEY` / `PG_SERVER_KEY` | presence-checked, grants nothing (FR-028a) |
| `r` | yes | human-readable remark | surfaces in the payload at tag `62`→`08` |
| `pn` | no | buyer name | sent when known — support matches payers on the gateway dashboard (User Story 6) |
| `mp` | no | — | **omitted** for QRIS; the method payload is an empty struct |
| `ac.an` | no | — | omitted; absent from every supplied sample |

The credential travels **in the body**, not in a header. It grants no privilege, so the body is not
secret — but it must not reach a guest-facing response (FR-002a).

### Response — success

```json
{
  "s": true,
  "e": null,
  "d": {
    "m": 0,
    "eri": "A485931237885664E26C",
    "mr": {
      "qr_r": "00020101021226620015ID.CO.MANJO.WWW…6304EB21",
      "qr_u": "",
      "qr_ea": "2026-08-10T10:26:46+07:00"
    }
  }
}
```

| Field | Maps to | Handling |
| --- | --- | --- |
| `s` | — | only `true` is a session; anything else is a failed open (FR-003) |
| `e` | — | untyped; log it, never show it to a guest |
| `d.eri` | `ProviderRef` | recorded for support tracing (FR-004) |
| `d.mr.qr_r` | `QRString` | empty ⇒ treat as failed, do not render (FR-006) |
| `d.mr.qr_u` | `QRImageURL` | usually `""` |
| `d.mr.qr_ea` | `ExpiresAt` | **authoritative deadline** (FR-009). Timestamp type, so the offset is preserved by the boundary. Zero value ⇒ treat as absent, fall back **and signal** (FR-009b); unreadable ⇒ fall back but still show the code (FR-009e); longer than expected ⇒ adopt as-is and signal (FR-009c/d) |

> **`qr_ea` requires contract module `v0.0.0-20260810052544-9d11826f4864` or newer**, where it is a
> timestamp type. Earlier versions define `qr_r` and `qr_u` only, and decoding through one of those
> drops the expiry **with no error** — yielding a working QR beside a deadline the gateway never
> agreed to. Two decode hazards follow from the type: an absent value becomes a valid year-1
> timestamp rather than nothing, and a malformed one can fail the surrounding decode. See
> [R1](../research.md).

### Response — duplicate reference

Returned when a code was already issued for that `ri`. This is **not** a generic failure:

- It proves a session exists that this system will never hold — no call returns an existing code.
- The order can never be paid; the guest is shown nothing.
- Handling: stop retrying, release quota promptly, write a `SESSION_DUPLICATE` marker, tell the guest
  to start again (FR-007d, FR-007e).

### Failure handling

| Condition | Retry? | Guest sees |
| --- | --- | --- |
| Connection refused, DNS failure | yes — provably never reached the gateway | nothing, unless the budget is exhausted |
| Timeout | yes — a duplicate-reference reply is the worst case, and it is safe | as above |
| Duplicate reference | **no** — terminal (FR-007d) | "attempt failed, please start again" |
| `s: false` with an error | no | actionable error; order stays unpaid with quota held (FR-007) |

Retries reuse `ri` unchanged (FR-007b), at most 2 further attempts, bounded in total elapsed time
because a guest is waiting. **Do not copy the gateway's own 10-second retry pacing** — correct for a
background callback, unacceptable in a checkout (FR-007c).

---

## 2. Inbound — payment notification

```
POST {our_host}/v1.0/callback/exec
Authorization: Bearer {PG_CALLBACK_TOKEN}
Content-Type: application/json
Accept: application/json
Accept-Language: en
```

The path is fixed by the gateway's dispatch code and is not ours to choose.

### Request

`callback.Request` from the contract module:

```json
{
  "ri": "ORD-20260810-A7K2QX",
  "nti": "A485931237885664E26C",
  "s": 5,
  "td": "2026-08-10T10:15:22+07:00",
  "tft": "…",
  "tt": 0,
  "ai": { "rrn": "…" }
}
```

| Field | Meaning | Handling |
| --- | --- | --- |
| `ri` | our order number | **direct** lookup, no suffix stripping (FR-010b) |
| `nti` | network transaction id | stored as `payments.transaction_id` |
| `s` | `status.Status` enum | mapped per the table below |
| `td` | transaction date | informational only; ordering uses our own records |
| `tft` | counterparty | stored in the raw payload |
| `tt` | `TrxType` — `0` deposit, `1` withdraw | non-deposit ⇒ no order change, recorded, signalled, **200** (FR-020) |
| `ai.rrn` | retrieval reference | optional |

There is **no amount field**. A wrong amount cannot be detected from the notification — tolerable
only because these are dynamic QRs carrying the amount at tag `54`, so the payer cannot alter it.

### Status mapping (total)

| `s` | Name | Order | Quota | Signal |
| --- | --- | --- | --- | --- |
| 0 | `Pending` | unchanged | held | no |
| 1 | `Reject` | `CANCELLED` | restored | no |
| 2 | `Cancel` | `CANCELLED` | restored | no |
| 3 | `Expired` | `EXPIRED` | restored | no |
| 4 | `Obscure` | unchanged | held | **yes** |
| 5 | `Completed` | `PAID` | consumed | no |
| other | — | unchanged | held | **yes** |

> **Zero-value hazard**: `Pending` is `0`, so a missing or unparseable `s` decodes as "pending" rather
> than erroring. The outcome is still correct, but the parse must distinguish absent from explicitly
> pending so the audit record is honest about what arrived.

### Response

| Case | Status | Envelope code | Data | Why |
| --- | --- | --- | --- | --- |
| Payment applied; pending; a repeat of an outcome already recorded | **200** | `200000` | `null` | uneventful — at-least-once makes the repeat the normal case (FR-012f) |
| Settle refused — quota no longer covers the hold (FR-019c) | **200** | `200001` | shortfall per ticket type | a retry would fail identically; the code marks it not-a-success (FR-019e) |
| Unknown reference (FR-013) | **200** | `200002` | `null` | retrying cannot make the order exist |
| Not a deposit (FR-020) | **200** | `200003` | `null` | a withdrawal on a deposit-only endpoint |
| Indeterminate or unrecognised status (FR-014) | **200** | `200004` | `null` | may be a status that should have released quota |
| Contradicts an order already paid (FR-016b) | **200** | `200005` | `null` | nothing reversed; a person decides |
| Completion for a gateway-cancelled order (FR-019d) | **200** | `200006` | `null` | never revived; not the lost-notification case |
| Missing or wrong bearer token | non-200 | `401001` | `null` | the only correct refusal (FR-012) |
| Transient internal fault where a retry genuinely helps | non-200 | `500000` | `null` | |

Every row carries a body in the `{code, message, data}` envelope — there is no empty-body answer
(FR-012e). **Every 200 row is indistinguishable by status, so the envelope code is the discriminator**
and is what anything asserting on this endpoint has to read.

The line between `200000` and the rest is the **operational signal**, not whether the order moved
(FR-012g): everything that raises a signal names itself to the caller, everything merely uneventful
does not. Precedence matters when more than one could apply — the deposit check runs first, then the
order lookup, then the status mapping — so an unknown reference carrying an unrecognised status
answers `200002`, not `200004`.

### Delivery guarantees

- **Timeout**: 5 s. Answer within it or a duplicate delivery is guaranteed (SC-011).
- **Retries**: up to 3, 10 s apart, on any non-200 **or** timeout.
- **Total automatic budget**: ~30 s, with no dead-letter queue. An outage longer than that means the
  notification will not arrive **by itself**.
- **Operator-invoked resend**: the gateway can be asked to redeliver a notification, **per
  transaction**, returning the order id and the status. This is what makes recovery possible at all
  (FR-019a) and is why nothing in this system asserts a payment on a person's word. An earlier
  revision of this document said "no manual redelivery"; that was inferred from the automatic-retry
  code and was wrong.
- **At-least-once**: an outcome must survive up to 4 deliveries with no duplicated effect (FR-012d).
  A resend is simply one more delivery, so the idempotency this requires is the same idempotency that
  makes recovery safe — there is no separate replay path to get right.

The response body is not read; only the status code matters. A refusal that must not be retried is
therefore expressed as a 200 whose body explains itself (FR-019c) — the body is for our own record
and for an operator reading it, not for the gateway.

That the gateway ignores the body is *why* the body can be shaped for us rather than for it, not a
reason to omit it (FR-012e). The consequence worth stating: since a refusal and an acknowledgement
are both 200, a stub gateway or a test that checks only the status line will pass on either. The
envelope code is what has to be asserted.

---

## 3. Capabilities the contract does not offer

| Wanted | Available? | Consequence |
| --- | --- | --- |
| **Redeliver a notification on request** | **Yes** — per transaction, returning the order id and status | The recovery path for a lost callback (FR-019a). Nothing in this system has to assert a payment on a person's word. |
| Query a transaction's status | **No.** `InquiryResponse` returns `t_i`, `c_n`, `i`, `r`, `r_f` — identifiers and payer details, no status. | No polling reconciliation. Largely moot: a resend delivers the status through the path that already handles it. |
| Fetch an existing code by reference | **No** | A lost session-open response is unrecoverable (FR-007d) |
| Amount on the callback | **No** | Paid-amount verification impossible from the notification |
| Cancel or extend a session | **No** | Deadline is the gateway's; nothing extends it (FR-010c) |

The resend capability is what makes the missing status query tolerable rather than serious: the two
would answer the same operational question, and only one of them needs a new code path on our side.
Fetching an existing code by reference remains genuinely unaddressed — worth raising with the gateway
team as a contract request rather than engineering around.

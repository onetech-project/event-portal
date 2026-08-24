# Contract — Restoring saved holder details (FR-030 – FR-033)

**Track F, rev. 6.** A UI contract plus one narrowly-scoped acceptance change on
`POST /ticket/checkout/:order_id`. **No endpoint is added. No response shape changes. No request
shape changes.**

---

## 1. `GET /ticket/order/:order_id` — asserted byte-identical

The restore consumes what this response already carries. Recorded as an assertion because the
temptation when "the form does not prefill" is to assume the data is missing and add a field.

```jsonc
{
  "slots": [
    {
      "id": "942444ee-…",           // already used — the card's slot_ids
      "ticket_type_name": "Regular",
      "package_name": null,
      "package_id": null,
      "package_unit": null,
      "name":   "Heather Higgins",   // ← restored to Full Name
      "email":  "…@mailinator.com",  // ← restored to Email
      "phone":  "144650550532",      // ← restored to Phone, verbatim
      "dob":    "1010-10-10",        // ← restored to DD/MM/YYYY
      "gender_id": 1,                // ← restored INTO the select (what it resubmits)
      "gender": "FEMALE"             // ← what the select DISPLAYS (FR-035, 2026-08-24)
    }
  ],
  "payment_started": false
}
```

- Every one of these five fields ships today and is nullable. **No field is added, renamed,
  retyped or reordered.**
- `gender` is the master row's NAME via `LEFT JOIN genders` — **not** filtered on `is_active`,
  so a retired gender already reaches the client. §3 is about accepting it back, not about
  sending it.
- `null` in a field means the slot was never filled. Its card renders empty (FR-030).

## 2. The screen contract

| State of the order | What the forms show |
|---|---|
| slots carry details, `payment_started: false` | every field filled from its slot |
| slots carry details, order EXPIRED / CANCELLED | every field filled, behind the unclosable modal (FR-022) |
| slots empty | every field empty — unchanged from today |
| a mix | each card follows its own slots |

**Seeded once.** The fields are populated when the card first mounts. A later arrival of the
same order — and this response is re-read on every window focus and reconnect — rewrites
nothing, edited or not (FR-032). The screen never inspects *why* the guest returned; the
presence of the data is the whole condition (FR-030).

**Nothing is added to the screen.** No restore notice, banner or badge; the first card's
delivery chip remains the only notice on the forms (FR-033). A restored field is presented
exactly like a typed one — same styling, no error shown merely for having been restored, and
Continue to Payment enabled as soon as every field is valid, which is FR-009 unchanged.

## 3. `POST /ticket/checkout/:order_id` — one acceptance change

**Request and response shapes are unchanged.** `attendees[i].gender` keeps its name, type and
position. What changes is which values are accepted:

| Submitted gender | Slot's stored gender | Before | After |
|---|---|---|---|
| active | anything | accepted | accepted — unchanged |
| **inactive** | **the same name** | `400001` "Select a valid gender." | **accepted** |
| inactive | a different name, or none | `400001` | `400001` — unchanged |
| not in the master list at all | anything | `400001` | `400001` — unchanged |

The error code, the field key (`attendees[i].gender`) and the message text are all unchanged;
only the set of accepted values widens, and only for a value the slot already held.

**Error precedence is unchanged.** The shape check still runs before the order is loaded, so a
malformed payload against an unknown order number still answers `400001` and not `404`
(research R34). The new slot allowance runs after slots are matched, alongside the existing
`matchVisitorsToSlots` and `validateBundleUnitConsistency` refusals.

**Stored `gender_id` is correct in every path**, including the retired one — which today would
resolve to the zero value and write an invalid foreign key.

## 4. `GET /ticket/genders` — asserted unchanged

Still active genders only. FR-031 widens **one card's option list on the client** so a select
can display the value it holds; it does not widen the master list, and no retired option is
offered on a card that does not already carry it.

## 5. What this contract does not change

- No change to when or whether details are persisted — checkout still saves them, once, in TX-D.
- No change to the gateway leg, its failure handling, or its messages. The 502
  `CodePaymentInitiationFailed` copy — *"Your details are saved — please try again."* — is
  already accurate and stays exactly as it is; this work makes the next screen agree with it.
- No change to hold or payment deadlines, quota, seat release, or order status.
- No change to the primary-contact derivation, which still reads the topmost slot in canonical
  order.
- No change to cache keys, scopes, or invalidation: the restore performs no write.

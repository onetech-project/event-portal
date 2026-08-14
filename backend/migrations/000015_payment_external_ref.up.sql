-- Spec 017: the payments log gains the gateway's OWN reference for the payment
-- session — the `eri` the provider returns when it issues a code.
--
-- It arrives exactly once, on the answer to the session-open call, and on
-- nothing else: an inbound callback carries our order number and the gateway's
-- network transaction id, never this. Session open is therefore the only moment
-- it can be captured, which is why it is written on its own SESSION_OPENED row
-- rather than back-filled onto a notification row that may never arrive.
--
-- Nullable and unconstrained, deliberately:
--
--   * NOT NULL is impossible — every existing row predates the column and no
--     value can be reconstructed for them, and notification rows written after
--     this legitimately carry none.
--   * No DEFAULT '' — an empty string could not be told apart from a gateway
--     that answered with a blank reference, and those are different facts.
--   * No UNIQUE — the reference is unique per GATEWAY, which this system cannot
--     assert across a provider swap (already exercised once, Midtrans -> Manjo).
--     A violation would fail a guest's checkout for an audit-record reason.
--   * No index — payments is only ever read by order_id, which the existing
--     queries already filter on, and a single order's rows are countable on one
--     hand.

ALTER TABLE payments ADD COLUMN ext_ref_id VARCHAR(255);

COMMENT ON COLUMN payments.ext_ref_id IS
    'The gateway''s own reference (eri) for the payment session, captured at session open. Set on the SESSION_OPENED marker row only; NULL on every notification row, because callbacks do not carry it. Opaque - never parsed, compared, or derived from.';

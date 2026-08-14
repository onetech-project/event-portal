-- name: CreatePayment :one
-- One row per notification received. `payments.status` stores the provider's RAW
-- transaction_status (so `deny` stays distinguishable from `failure` even though
-- both fold into the order's CANCELLED status), and raw_response keeps the full
-- payload for audit.
--
-- Deliberately untouched by migration 0013: this is the provider's own status
-- string, a different column with a different meaning from the order's status,
-- which became a reference to the order-status master list. No query in this
-- file reads or writes the `orders` table at all.
INSERT INTO payments (order_id, provider, transaction_id, payment_type, status, raw_response, ext_ref_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, order_id, provider, transaction_id, payment_type, status, created_at;


-- name: ListPaymentsByOrderID :many
-- Every notification recorded against one order, accepted or refused, newest
-- first (FR-022c). Marker rows appear alongside the payloads that triggered
-- them, which is the point: the sequence IS the audit trail.
SELECT id, order_id, provider, transaction_id, payment_type, status, raw_response, ext_ref_id, created_at
FROM payments
WHERE order_id = $1
ORDER BY created_at DESC;


-- name: GetExternalRefByOrderID :one
-- The gateway's own reference for this order's payment session (spec 017).
--
-- Only the SESSION_OPENED row ever carries one — a callback does not send it —
-- so this resolves to that row. The ORDER BY is defensive rather than load
-- bearing: the current gateway refuses to open a second session for a reference
-- it has already issued, so an order has at most one.
--
-- No rows is a NORMAL answer, not an error. An order whose session never opened,
-- and any order predating this column, simply has no reference; the caller turns
-- that into an empty value rather than a failure, because the checkout response
-- carries the field present-and-empty (FR-010).
SELECT ext_ref_id FROM payments
WHERE order_id = $1 AND ext_ref_id IS NOT NULL
ORDER BY created_at DESC
LIMIT 1;


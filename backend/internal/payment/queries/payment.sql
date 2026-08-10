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
INSERT INTO payments (order_id, provider, transaction_id, payment_type, status, raw_response)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, order_id, provider, transaction_id, payment_type, status, created_at;


-- name: ListPaymentsByOrderID :many
-- Every notification recorded against one order, accepted or refused, newest
-- first (FR-022c). Marker rows appear alongside the payloads that triggered
-- them, which is the point: the sequence IS the audit trail.
SELECT id, order_id, provider, transaction_id, payment_type, status, raw_response, created_at
FROM payments
WHERE order_id = $1
ORDER BY created_at DESC;


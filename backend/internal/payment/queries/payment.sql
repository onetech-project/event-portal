-- name: CreatePayment :one
-- One row per notification received. `status` stores the provider's RAW
-- transaction_status (so `deny` stays distinguishable from `failure` even though
-- both fold into orders.status = 'CANCELLED'), and raw_response keeps the full
-- payload for audit.
INSERT INTO payments (order_id, provider, transaction_id, payment_type, status, raw_response)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, order_id, provider, transaction_id, payment_type, status, created_at;

-- name: CountReissuedQRs :one
-- How many times the order's QR has been re-issued (spec 008 FR-015). Drives
-- the -R{n} suffix on the provider reference.
SELECT COUNT(*) FROM payments WHERE order_id = $1 AND status = 'QR_REISSUED';

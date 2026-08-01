-- Checkout writes (TX1) ----------------------------------------------------

-- name: CreateOrder :one
INSERT INTO orders (order_number, buyer_name, buyer_email, buyer_phone, total_amount, status)
VALUES ($1, $2, $3, $4, $5, 'PENDING')
RETURNING id, order_number, buyer_name, buyer_email, buyer_phone, total_amount, status,
          payment_provider, payment_url, email_sent, created_at, updated_at,
          payment_qr_string, payment_expires_at;

-- name: CreateOrderItem :one
INSERT INTO order_items (order_id, ticket_type_id, quantity, price)
VALUES ($1, $2, $3, $4)
RETURNING id, order_id, ticket_type_id, quantity, price;

-- name: CreateAttendee :one
INSERT INTO attendees (order_id, ticket_type_id, name, email)
VALUES ($1, $2, $3, $4)
RETURNING id, order_id, ticket_type_id, name, email;

-- name: OrderNumberExists :one
SELECT EXISTS (SELECT 1 FROM orders WHERE order_number = $1) AS taken;

-- Payment lifecycle --------------------------------------------------------

-- Stamps everything the guest's payment page needs: the provider's QR-image URL
-- (audit/fallback), the raw QRIS payload the image is rendered from, and the
-- deadline the countdown and the expiry sweeper both read.
-- name: UpdatePaymentDetails :execrows
UPDATE orders
SET payment_url = $2,
    payment_provider = $3,
    payment_qr_string = $4,
    payment_expires_at = $5,
    updated_at = now()
WHERE id = $1;

-- name: UpdateOrderStatusIfPending :execrows
-- Guarded on PENDING so a replayed webhook notification cannot apply the same
-- transition (and its quota restoration) twice.
UPDATE orders
SET status = $2, updated_at = now()
WHERE id = $1 AND status = 'PENDING';

-- name: SetEmailSent :execrows
UPDATE orders SET email_sent = TRUE, updated_at = now() WHERE id = $1;

-- Reads --------------------------------------------------------------------

-- The trailing two columns are listed in migration order (0002 appended them
-- after updated_at), which is what lets sqlc reuse the single Order struct
-- instead of emitting a near-identical row type per query.
-- name: GetOrderByID :one
SELECT id, order_number, buyer_name, buyer_email, buyer_phone, total_amount, status,
       payment_provider, payment_url, email_sent, created_at, updated_at,
       payment_qr_string, payment_expires_at
FROM orders
WHERE id = $1;

-- name: GetOrderByNumber :one
SELECT id, order_number, buyer_name, buyer_email, buyer_phone, total_amount, status,
       payment_provider, payment_url, email_sent, created_at, updated_at,
       payment_qr_string, payment_expires_at
FROM orders
WHERE order_number = $1;

-- Orders whose payment deadline has passed but which nothing has moved yet.
-- Oldest first, so the longest-held quota is released soonest. Uses the partial
-- index idx_orders_payment_expiry, which covers exactly this predicate.
-- name: ListOrdersDueForExpiry :many
SELECT id, order_number, status, payment_expires_at
FROM orders
WHERE status = 'PENDING'
  AND payment_expires_at IS NOT NULL
  AND payment_expires_at <= $1
ORDER BY payment_expires_at
LIMIT $2;

-- name: ListOrderItemsByOrderID :many
SELECT id, order_id, ticket_type_id, quantity, price
FROM order_items
WHERE order_id = $1;

-- name: ListAttendeesByOrderID :many
SELECT id, order_id, ticket_type_id, name, email
FROM attendees
WHERE order_id = $1
ORDER BY id ASC;

-- OrderChecker (delete guards + derived sold counts) ------------------------
-- Both FKs onto ticket_types are ON DELETE RESTRICT, so a guard that inspects only
-- order_items would surface a raw constraint violation instead of the required 400
-- (constitution, Principle VI).

-- name: HasOrdersForTicketType :one
SELECT (
    EXISTS (SELECT 1 FROM order_items oi WHERE oi.ticket_type_id = $1)
    OR
    EXISTS (SELECT 1 FROM attendees a WHERE a.ticket_type_id = $1)
) AS has_orders;

-- name: HasOrdersForTicketTypes :one
SELECT (
    EXISTS (SELECT 1 FROM order_items oi WHERE oi.ticket_type_id = ANY(sqlc.arg(ids)::uuid[]))
    OR
    EXISTS (SELECT 1 FROM attendees a WHERE a.ticket_type_id = ANY(sqlc.arg(ids)::uuid[]))
) AS has_orders;

-- name: SoldCountByTicketTypes :many
-- Derived, never stored (constitution, Critical Data Flow Rules). Batched over a
-- whole page of ticket types so admin listings issue one query, not N.
SELECT ticket_type_id, SUM(quantity)::bigint AS sold
FROM order_items
WHERE ticket_type_id = ANY(sqlc.arg(ids)::uuid[])
GROUP BY ticket_type_id;

-- Admin read-only views ----------------------------------------------------

-- name: ListOrdersAdmin :many
SELECT id, order_number, buyer_name, buyer_email, buyer_phone, total_amount, status,
       payment_provider, payment_url, email_sent, created_at, updated_at
FROM orders
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (
        sqlc.narg(ticket_type_ids)::uuid[] IS NULL
        OR EXISTS (
            SELECT 1 FROM order_items oi
            WHERE oi.order_id = orders.id
              AND oi.ticket_type_id = ANY(sqlc.narg(ticket_type_ids)::uuid[])
        )
      )
ORDER BY created_at DESC;

-- name: ListAttendeesAdmin :many
SELECT a.id, a.order_id, a.ticket_type_id, a.name, a.email, o.order_number
FROM attendees a
JOIN orders o ON o.id = a.order_id
WHERE (sqlc.narg(order_id)::uuid IS NULL OR a.order_id = sqlc.narg(order_id)::uuid)
  AND (
        sqlc.narg(ticket_type_ids)::uuid[] IS NULL
        OR a.ticket_type_id = ANY(sqlc.narg(ticket_type_ids)::uuid[])
      )
ORDER BY o.created_at DESC, a.name ASC;

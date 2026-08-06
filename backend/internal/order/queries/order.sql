-- Checkout writes (TX1) ----------------------------------------------------

-- name: CreateOrder :one
INSERT INTO orders (order_number, buyer_name, buyer_email, buyer_phone, total_amount, status)
VALUES ($1, $2, $3, $4, $5, 'PENDING')
RETURNING id, order_number, buyer_name, buyer_email, buyer_phone, total_amount, status,
          payment_provider, payment_url, email_sent, created_at, updated_at,
          payment_qr_string, payment_expires_at, terms_agreed_at, event_terms_id,
       buyer_dob, buyer_gender, subtotal;

-- name: CreateOrderItem :one
-- Exactly one of ticket_type_id / package_id is set (order_items_line_kind_chk). A
-- package line is stored ONCE at the package's own price rather than expanded into
-- per-constituent rows, so total_amount stays exactly SUM(quantity * price).
INSERT INTO order_items (order_id, ticket_type_id, package_id, quantity, price)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, order_id, ticket_type_id, package_id, quantity, price;

-- name: CreateAttendee :one
-- ticket_type_id is never null, including for bundle-derived registrants: that is
-- what keeps one pass per attendee true for packages. package_id records only the
-- bundle a slot originated in.
INSERT INTO attendees (order_id, ticket_type_id, package_id, name, email)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, order_id, ticket_type_id, package_id, name, email;

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
       payment_qr_string, payment_expires_at, terms_agreed_at, event_terms_id,
       buyer_dob, buyer_gender, subtotal
FROM orders
WHERE id = $1;

-- name: GetOrderByNumber :one
SELECT id, order_number, buyer_name, buyer_email, buyer_phone, total_amount, status,
       payment_provider, payment_url, email_sent, created_at, updated_at,
       payment_qr_string, payment_expires_at, terms_agreed_at, event_terms_id,
       buyer_dob, buyer_gender, subtotal
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
SELECT id, order_id, ticket_type_id, package_id, quantity, price
FROM order_items
WHERE order_id = $1;

-- name: ListAttendeesByOrderID :many
SELECT id, order_id, ticket_type_id, package_id, name, email
FROM attendees
WHERE order_id = $1
ORDER BY id ASC;

-- name: ListQuotaHoldsByOrderID :many
-- An order's total per-ticket-type hold, expanding package lines through the
-- junction. This is the exact inverse of the checkout aggregation, so restoring
-- returns precisely what deducting took.
--
-- ORDER BY ticket_type_id matches the deduction order, keeping restore on the same
-- deterministic lock sequence and out of deadlock range.
--
-- It reconstructs the hold from the package's CURRENT composition, which is why
-- composition edits are rejected while a PENDING order exists (research.md R-004).
SELECT ticket_type_id, SUM(qty)::int AS qty
FROM (
    SELECT oi.ticket_type_id, oi.quantity AS qty
    FROM order_items oi
    WHERE oi.order_id = sqlc.arg(order_id) AND oi.ticket_type_id IS NOT NULL

    UNION ALL

    SELECT pt.ticket_type_id, oi.quantity * pt.quantity AS qty
    FROM order_items oi
    JOIN package_tickets pt ON pt.package_id = oi.package_id
    WHERE oi.order_id = sqlc.arg(order_id) AND oi.package_id IS NOT NULL
) holds
GROUP BY ticket_type_id
ORDER BY ticket_type_id;

-- name: HasOrdersForPackage :one
-- Delete guard for a package. Both FKs are ON DELETE RESTRICT, so this must return
-- a clean 400 rather than letting a raw constraint violation surface.
SELECT (
    EXISTS (SELECT 1 FROM order_items oi WHERE oi.package_id = $1)
    OR
    EXISTS (SELECT 1 FROM attendees a WHERE a.package_id = $1)
) AS has_orders;

-- name: HasPendingOrdersForPackage :one
-- Composition-lock guard (research.md R-004): an open PENDING order holding this
-- package means its composition cannot change, or restoration after expiry would
-- put back a different amount than was deducted. PAID or terminal orders are not
-- a constraint — their hold is permanent.
SELECT EXISTS (
    SELECT 1 FROM order_items oi
    JOIN orders o ON o.id = oi.order_id
    WHERE oi.package_id = $1 AND o.status = 'PENDING'
) AS has_pending;

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
--
-- The UNION arm is not optional: a package line carries no ticket_type_id, so a
-- count reading only the first arm would under-report every bundled sale.
SELECT ticket_type_id, SUM(qty)::bigint AS sold
FROM (
    SELECT oi.ticket_type_id, oi.quantity AS qty
    FROM order_items oi
    WHERE oi.ticket_type_id = ANY(sqlc.arg(ids)::uuid[])

    UNION ALL

    SELECT pt.ticket_type_id, oi.quantity * pt.quantity AS qty
    FROM order_items oi
    JOIN package_tickets pt ON pt.package_id = oi.package_id
    WHERE oi.package_id IS NOT NULL
      AND pt.ticket_type_id = ANY(sqlc.arg(ids)::uuid[])
) sales
GROUP BY ticket_type_id;

-- name: SoldCountByPackage :many
-- Derived count of how many units of each package have been sold, used by the
-- admin package dashboard. A package line is stored once at the package's own
-- price, so the count is a straight SUM(quantity) over order_items, no expansion.
SELECT package_id, SUM(quantity)::bigint AS sold
FROM order_items
WHERE package_id = ANY(sqlc.arg(ids)::uuid[])
GROUP BY package_id;

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

-- Spec 008: two-phase booking ------------------------------------------------

-- name: CreateBookedOrder :one
-- Booking (TX-B): the order exists before any buyer identity — those columns
-- stay NULL until checkout. payment_expires_at carries the 1-hour hold.
INSERT INTO orders (order_number, total_amount, subtotal, status, payment_expires_at)
VALUES ($1, $2, $3, 'PENDING', $4)
RETURNING id, order_number, buyer_name, buyer_email, buyer_phone, total_amount, status,
          payment_provider, payment_url, email_sent, created_at, updated_at,
          payment_qr_string, payment_expires_at, terms_agreed_at, event_terms_id,
       buyer_dob, buyer_gender, subtotal;

-- name: CreateAttendeeSlot :one
-- An EMPTY slot: ticket-type-bound at booking, identity filled at checkout.
-- package_unit ties bundle slots to their purchased unit (spec 010).
INSERT INTO attendees (order_id, ticket_type_id, package_id, package_unit)
VALUES ($1, $2, $3, $4)
RETURNING id, order_id, ticket_type_id, package_id, package_unit, name, email, phone, dob, gender;

-- name: RecordTermsAgreement :execrows
-- Same "Agree" click as booking, its own call. Guarded on PENDING; idempotent
-- because re-stamping the same agreement is harmless.
UPDATE orders
SET terms_agreed_at = now(), event_terms_id = $2, updated_at = now()
WHERE id = $1 AND status = 'PENDING';

-- name: UpdateOrderBuyer :execrows
-- Checkout TX-D: buyer identity arrives with the visitor forms.
UPDATE orders
SET buyer_name = $2, buyer_email = $3, buyer_phone = $4,
    buyer_dob = $5, buyer_gender = $6, updated_at = now()
WHERE id = $1 AND status = 'PENDING';

-- name: ListActiveGenders :many
-- The gender master list (clarified 2026-08-05): the forms' options and the
-- values checkout accepts both come from here, never a hardcoded set.
SELECT id, name FROM genders WHERE is_active ORDER BY name;

-- name: UpdateAttendeeDetails :execrows
-- Checkout TX-D: fills one slot. order_id in the predicate stops a forged slot
-- id from writing into another order's attendee.
UPDATE attendees
SET name = $3, email = $4, phone = $5, dob = $6, gender = $7
WHERE id = $1 AND order_id = $2;

-- name: UpdatePaymentQR :execrows
-- QR re-issue (7-minute refresh): swaps the payload WITHOUT touching the
-- deadline — the 14-minute window never extends (FR-015).
UPDATE orders
SET payment_url = $2, payment_qr_string = $3, updated_at = now()
WHERE id = $1 AND status = 'PENDING';

-- name: UpdatePaymentDetailsIfUnstarted :execrows
-- Checkout TX-P: stamps the gateway session and the payment window, guarded so
-- a concurrent checkout cannot overwrite a live QR.
UPDATE orders
SET payment_url = $2,
    payment_provider = $3,
    payment_qr_string = $4,
    payment_expires_at = $5,
    updated_at = now()
WHERE id = $1 AND status = 'PENDING' AND payment_qr_string IS NULL;

-- name: GetOrderWithTermsByNumber :one
-- The 008 guest read: everything the order page needs to pick its screen.
SELECT id, order_number, buyer_name, buyer_email, buyer_phone, total_amount, status,
       payment_provider, payment_url, email_sent, created_at, updated_at,
       payment_qr_string, payment_expires_at, terms_agreed_at, event_terms_id,
       buyer_dob, buyer_gender, subtotal
FROM orders
WHERE order_number = $1;

-- name: ListAttendeeSlotsByOrderID :many
-- Slot list incl. the 008 detail columns and the human ticket-type name.
-- Ordering (spec 010): standalone slots first, then bundle slots contiguous
-- per (package_id, package_unit), so one visitor form maps to one unit.
SELECT a.id, a.order_id, a.ticket_type_id, a.package_id, a.package_unit,
       a.name, a.email, a.phone, a.dob, a.gender,
       tt.name AS ticket_type_name
FROM attendees a
JOIN ticket_types tt ON tt.id = a.ticket_type_id
WHERE a.order_id = $1
ORDER BY a.package_id NULLS FIRST, a.package_unit ASC, a.id ASC;

-- name: GetOrderEventID :one
-- The event an order belongs to, resolved through its attendee slots (every
-- booked order has at least one, and each slot binds a concrete ticket type).
SELECT tt.event_id
FROM attendees a
JOIN ticket_types tt ON tt.id = a.ticket_type_id
WHERE a.order_id = $1
LIMIT 1;

-- Fees (master + per-order snapshot; clarified 2026-08-05) -------------------

-- name: ListActiveFees :many
-- The fee master rows booking applies, in display order.
SELECT id, name, fee_type, value, position
FROM fees
WHERE is_active
ORDER BY position, name;

-- name: ListFees :many
-- Admin read: every fee, active or not.
SELECT id, name, fee_type, value, position, is_active, created_at, updated_at
FROM fees
ORDER BY position, name;

-- name: CreateFee :one
INSERT INTO fees (name, fee_type, value, position, is_active)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, fee_type, value, position, is_active, created_at, updated_at;

-- name: UpdateFee :one
UPDATE fees
SET name = $2, fee_type = $3, value = $4, position = $5, is_active = $6, updated_at = now()
WHERE id = $1
RETURNING id, name, fee_type, value, position, is_active, created_at, updated_at;

-- name: DeleteFee :execrows
DELETE FROM fees WHERE id = $1;

-- name: CreateOrderFee :exec
-- Booking (TX-B): freezes one computed fee line onto the order.
INSERT INTO order_fees (order_id, name, amount, position)
VALUES ($1, $2, $3, $4);

-- name: ListOrderFeesByOrderID :many
SELECT id, name, amount, position
FROM order_fees
WHERE order_id = $1
ORDER BY position, name;

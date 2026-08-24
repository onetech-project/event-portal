-- Checkout writes (TX1) ----------------------------------------------------

-- name: CreateOrderItem :one
-- Exactly one of ticket_type_id / package_id is set (order_items_line_kind_chk). A
-- package line is stored ONCE at the package's own price rather than expanded into
-- per-constituent rows, so total_amount stays exactly SUM(quantity * price).
INSERT INTO order_items (order_id, ticket_type_id, package_id, quantity, price)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, order_id, ticket_type_id, package_id, quantity, price;

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
--
-- Migration 0013 moved the column to a reference, but the caller still passes a
-- NAME: the resolution happens here, in the statement, which is what keeps every
-- Go status comparison in the codebase unchanged (spec 011 FR-026, research
-- R19). Still one atomic statement, so the idempotency guard is unweakened.
UPDATE orders
SET status_id = (SELECT ost.id FROM order_statuses ost WHERE ost.name = sqlc.arg(status)),
    updated_at = now()
WHERE orders.id = $1
  AND status_id = (SELECT ost.id FROM order_statuses ost WHERE ost.name = 'PENDING');

-- name: SetEmailSent :execrows
UPDATE orders SET email_sent = TRUE, updated_at = now() WHERE id = $1;

-- Reads --------------------------------------------------------------------

-- The trailing two columns are listed in migration order (0002 appended them
-- after updated_at), which is what lets sqlc reuse the single Order struct
-- instead of emitting a near-identical row type per query.
-- Every order read joins the status master list and aliases the NAME back to
-- `status`, so the generated Order struct still carries `Status string` holding
-- PENDING/PAID/CANCELLED/EXPIRED. That is the whole reason migration 0013 costs
-- no Go changes (research R19) — keep the alias if you touch these.
-- name: GetOrderByID :one
--
-- `is_registration` is DERIVED, not stored (spec 022, decision reversed
-- 2026-08-20). An order is a registration when it carries a line for a
-- registration-only ticket type — containment guarantees such a type can never be
-- bought or bundled, so one such line can only have come from the registration
-- path.
--
-- The JOIN reaches into the event domain's table. Principle II permits that on a
-- READ; the registration WRITE still reaches quota only through EventProvider.
--
-- KNOWN AND ACCEPTED CONSEQUENCE: this value is not stable over time. An admin who
-- makes an invitation ticket type purchasable again retroactively reclassifies
-- every order that used it, and a resend of an old registration would then render
-- a receipt for an order that never had a payment. That was raised and the
-- derivation was chosen deliberately over a stored marker.
SELECT o.id, o.order_number, o.buyer_name, o.buyer_email, o.buyer_phone, o.total_amount,
       os.name AS status,
       o.payment_provider, o.payment_url, o.email_sent, o.created_at, o.updated_at,
       o.payment_qr_string, o.payment_expires_at, o.terms_agreed_at, o.event_terms_id,
       o.subtotal,
       EXISTS (
           SELECT 1 FROM order_items oi
           JOIN ticket_types tt ON tt.id = oi.ticket_type_id
           WHERE oi.order_id = o.id AND NOT tt.is_visible
       ) AS is_registration
FROM orders o
JOIN order_statuses os ON os.id = o.status_id
WHERE o.id = $1;

-- name: GetOrderByNumber :one
SELECT o.id, o.order_number, o.buyer_name, o.buyer_email, o.buyer_phone, o.total_amount,
       os.name AS status,
       o.payment_provider, o.payment_url, o.email_sent, o.created_at, o.updated_at,
       o.payment_qr_string, o.payment_expires_at, o.terms_agreed_at, o.event_terms_id,
       o.subtotal,
       EXISTS (
           SELECT 1 FROM order_items oi
           JOIN ticket_types tt ON tt.id = oi.ticket_type_id
           WHERE oi.order_id = o.id AND NOT tt.is_visible
       ) AS is_registration
FROM orders o
JOIN order_statuses os ON os.id = o.status_id
WHERE o.order_number = $1;

-- Orders whose payment deadline has passed but which nothing has moved yet.
-- Oldest first, so the longest-held quota is released soonest. Uses the partial
-- index idx_orders_payment_expiry, which covers exactly this predicate.
--
-- THE ONE PLACE THE LITERAL ID IS REQUIRED. Everywhere else the status name is
-- resolved with a subquery; here it cannot be. A partial index predicate must
-- be immutable, so the index is defined `WHERE status_id = 1`, and PostgreSQL
-- only matches a partial index when the query predicate provably implies it at
-- PLAN time. A subquery is a runtime value, so `status_id = (SELECT …)` would
-- silently stop using the index and turn every sweeper tick into a sequential
-- scan of the whole order history. That is why migration 0013 pins the seeded
-- ids and SCHEMA.md documents 1 = PENDING as part of the contract (research R20).
-- name: ListOrdersDueForExpiry :many
SELECT o.id, o.order_number, os.name AS status, o.payment_expires_at
FROM orders o
JOIN order_statuses os ON os.id = o.status_id
WHERE o.status_id = 1 -- PENDING, pinned by migration 0013; see above
  AND o.payment_expires_at IS NOT NULL
  AND o.payment_expires_at <= $1
ORDER BY o.payment_expires_at
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
    WHERE oi.package_id = $1
      AND o.status_id = (SELECT ost.id FROM order_statuses ost WHERE ost.name = 'PENDING')
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

-- Spec 021: the admin lists are paginated. Each list read is a PAIR — a count
-- and a page — and the pair MUST share one WHERE clause verbatim. The count runs
-- first because clamping an out-of-range page to the last one needs the total
-- before the slice is taken, and a page read past the end comes back empty with
-- nothing to clamp against (spec 021 research R3).
--
-- Every ORDER BY here ends in a primary key. That is not decoration: without a
-- total order, PostgreSQL may return rows tied on the leading key in different
-- relative orders for two different OFFSETs, which shows an operator a duplicate
-- on page 2 while hiding a different record entirely (spec 021 research R4).

-- name: CountOrdersAdmin :one
-- Pairs with ListOrdersAdmin below. Keep the two WHERE clauses identical.
SELECT count(*)
FROM orders o
JOIN order_statuses os ON os.id = o.status_id
WHERE (sqlc.narg(status)::text IS NULL OR os.name = sqlc.narg(status)::text)
  AND (
        sqlc.narg(ticket_type_ids)::uuid[] IS NULL
        OR EXISTS (
            SELECT 1 FROM order_items oi
            WHERE oi.order_id = o.id
              AND oi.ticket_type_id = ANY(sqlc.narg(ticket_type_ids)::uuid[])
        )
      )
  AND (
        sqlc.narg(search)::text IS NULL
        OR o.order_number ILIKE '%' || sqlc.narg(search)::text || '%'
        OR o.buyer_name   ILIKE '%' || sqlc.narg(search)::text || '%'
        OR o.buyer_email  ILIKE '%' || sqlc.narg(search)::text || '%'
      );

-- name: ListOrdersAdmin :many
-- The `status` filter parameter is still the NAME the admin UI sends; it is
-- matched against the joined master row rather than a column on orders.
SELECT o.id, o.order_number, o.buyer_name, o.buyer_email, o.buyer_phone, o.total_amount,
       os.name AS status,
       o.payment_provider, o.payment_url, o.email_sent,
       EXISTS (
           SELECT 1 FROM order_items oi
           JOIN ticket_types tt ON tt.id = oi.ticket_type_id
           WHERE oi.order_id = o.id AND NOT tt.is_visible
       ) AS is_registration,
       o.created_at, o.updated_at
FROM orders o
JOIN order_statuses os ON os.id = o.status_id
WHERE (sqlc.narg(status)::text IS NULL OR os.name = sqlc.narg(status)::text)
  AND (
        sqlc.narg(ticket_type_ids)::uuid[] IS NULL
        OR EXISTS (
            SELECT 1 FROM order_items oi
            WHERE oi.order_id = o.id
              AND oi.ticket_type_id = ANY(sqlc.narg(ticket_type_ids)::uuid[])
        )
      )
  AND (
        sqlc.narg(search)::text IS NULL
        OR o.order_number ILIKE '%' || sqlc.narg(search)::text || '%'
        OR o.buyer_name   ILIKE '%' || sqlc.narg(search)::text || '%'
        OR o.buyer_email  ILIKE '%' || sqlc.narg(search)::text || '%'
      )
ORDER BY o.created_at DESC, o.id DESC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- name: CountAttendeesAdmin :one
-- Pairs with ListAttendeesAdmin below. Keep the two WHERE clauses identical.
SELECT count(*)
FROM attendees a
JOIN orders o ON o.id = a.order_id
WHERE (sqlc.narg(order_id)::uuid IS NULL OR a.order_id = sqlc.narg(order_id)::uuid)
  AND (
        sqlc.narg(ticket_type_ids)::uuid[] IS NULL
        OR a.ticket_type_id = ANY(sqlc.narg(ticket_type_ids)::uuid[])
      )
  AND (
        sqlc.narg(search)::text IS NULL
        OR a.name         ILIKE '%' || sqlc.narg(search)::text || '%'
        OR a.email        ILIKE '%' || sqlc.narg(search)::text || '%'
        OR o.order_number ILIKE '%' || sqlc.narg(search)::text || '%'
      );

-- name: ListAttendeesAdmin :many
-- a.id breaks the tie that `created_at, name` leaves open constantly: one
-- order's attendees all share created_at, and two people with the same name in
-- one order is ordinary rather than exotic.
-- Spec 022 FR-053: `search` is the operator's recovery route, not a convenience.
-- A registrant is never shown the address their e-ticket went to (FR-039), so an
-- operator helping someone who never received it cannot be given the exact
-- address to look up. Matching a PARTIAL name or email is therefore the only
-- thing that turns "I registered and nothing arrived" into a resend.
--
-- ILIKE with leading wildcards cannot use a btree index and will seq-scan. That
-- is accepted: this is an authenticated, low-frequency admin read, unlike the
-- registration write path where a scan sat inside a quota row lock.
SELECT a.id, a.order_id, a.ticket_type_id, a.name, a.email, o.order_number,
       EXISTS (
           SELECT 1 FROM order_items oi
           JOIN ticket_types tt ON tt.id = oi.ticket_type_id
           WHERE oi.order_id = o.id AND NOT tt.is_visible
       ) AS is_registration
FROM attendees a
JOIN orders o ON o.id = a.order_id
WHERE (sqlc.narg(order_id)::uuid IS NULL OR a.order_id = sqlc.narg(order_id)::uuid)
  AND (
        sqlc.narg(ticket_type_ids)::uuid[] IS NULL
        OR a.ticket_type_id = ANY(sqlc.narg(ticket_type_ids)::uuid[])
      )
  AND (
        sqlc.narg(search)::text IS NULL
        OR a.name         ILIKE '%' || sqlc.narg(search)::text || '%'
        OR a.email        ILIKE '%' || sqlc.narg(search)::text || '%'
        OR o.order_number ILIKE '%' || sqlc.narg(search)::text || '%'
      )
ORDER BY o.created_at DESC, a.name ASC, a.id ASC
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

-- Spec 008: two-phase booking ------------------------------------------------

-- name: CreateBookedOrder :one
-- Booking (TX-B): the order exists before any contact identity — those columns
-- stay NULL until checkout. payment_expires_at carries the 1-hour hold.
--
-- Wrapped in a CTE because RETURNING cannot join: the insert resolves PENDING to
-- its id, then the outer select joins the master list back so the caller still
-- receives `status` as the NAME, with the same NOT NULL string type as every
-- other order read (research R19).
WITH inserted AS (
    INSERT INTO orders (order_number, total_amount, subtotal, status_id, payment_expires_at)
    VALUES ($1, $2, $3, (SELECT ost.id FROM order_statuses ost WHERE ost.name = 'PENDING'), $4)
    RETURNING id, order_number, buyer_name, buyer_email, buyer_phone, total_amount,
              status_id, payment_provider, payment_url, email_sent, created_at,
              updated_at, payment_qr_string, payment_expires_at, terms_agreed_at,
              event_terms_id, subtotal
)
SELECT i.id, i.order_number, i.buyer_name, i.buyer_email, i.buyer_phone, i.total_amount,
       os.name AS status,
       i.payment_provider, i.payment_url, i.email_sent, i.created_at, i.updated_at,
       i.payment_qr_string, i.payment_expires_at, i.terms_agreed_at, i.event_terms_id,
       i.subtotal,
       EXISTS (
           SELECT 1 FROM order_items oi
           JOIN ticket_types tt ON tt.id = oi.ticket_type_id
           WHERE oi.order_id = i.id AND NOT tt.is_visible
       ) AS is_registration
FROM inserted i
JOIN order_statuses os ON os.id = i.status_id;

-- name: CreateRegistrationOrder :one
-- Spec 022: the free-registration order. Created FINAL — PAID, zero total — in one
-- statement.
--
-- NOTHING marks it as a registration. The distinction is DERIVED from the ticket
-- type its single line references (decision reversed 2026-08-20), so
-- `is_registration` in the row this returns is necessarily FALSE: the line is
-- inserted after this statement, so at this instant the order has no items to
-- derive from. No caller reads it from here — the delivery path re-reads through
-- GetOrderByID once the line exists — and it is returned only because the Go layer
-- casts this row to GetOrderByIDRow and the two column lists must match.
--
-- Everything is inline BECAUSE IT HAS TO BE. The two obvious "insert then patch"
-- queries are both guarded on PENDING and would silently affect zero rows here:
-- UpdateOrderBuyer and RecordTermsAgreement. Their repository wrappers map "0
-- rows" to false, which the service reports as 410 ORDER_EXPIRED — so a
-- registration that actually succeeded would be reported to the guest as gone.
--
-- buyer_email in particular is load-bearing: notification.SendTicketEmail reads
-- orders.buyer_email and refuses an empty snapshot outright, so a registration
-- that left it NULL would issue a ticket and then silently fail to deliver it —
-- after the guest has already been told it was sent (spec 022 FR-039e).
--
-- payment_expires_at is left NULL, not set: it is what every order read renders a
-- live countdown from, and what the expiry sweeper's partial index selects on.
-- A registration is already final and must never look payable or be swept.
--
-- No order_fees rows accompany this insert and no payments row ever will
-- (FR-027, FR-029). Constitution Principle IV v5.0.0 admits this as the one path
-- to PAID without a gateway.
--
-- Same CTE shape as CreateBookedOrder, and the same column list: the Go layer
-- casts this row to GetOrderByIDRow, so the two must stay identical.
WITH inserted AS (
    INSERT INTO orders (order_number, total_amount, subtotal, status_id,
                        buyer_name, buyer_email, buyer_phone,
                        terms_agreed_at, event_terms_id)
    VALUES ($1, 0, 0, (SELECT ost.id FROM order_statuses ost WHERE ost.name = 'PAID'),
            $2, $3, $4, now(), $5)
    RETURNING id, order_number, buyer_name, buyer_email, buyer_phone, total_amount,
              status_id, payment_provider, payment_url, email_sent, created_at,
              updated_at, payment_qr_string, payment_expires_at, terms_agreed_at,
              event_terms_id, subtotal
)
SELECT i.id, i.order_number, i.buyer_name, i.buyer_email, i.buyer_phone, i.total_amount,
       os.name AS status,
       i.payment_provider, i.payment_url, i.email_sent, i.created_at, i.updated_at,
       i.payment_qr_string, i.payment_expires_at, i.terms_agreed_at, i.event_terms_id,
       i.subtotal,
       EXISTS (
           SELECT 1 FROM order_items oi
           JOIN ticket_types tt ON tt.id = oi.ticket_type_id
           WHERE oi.order_id = i.id AND NOT tt.is_visible
       ) AS is_registration
FROM inserted i
JOIN order_statuses os ON os.id = i.status_id;

-- Spec 022 FR-023: there is deliberately NO per-address query here.
--
-- An earlier draft carried EmailHasIssuedTicketForEvent (does this address
-- already hold a place at this event?) and LockRegistrationIdentity (an advisory
-- xact lock keyed on event+lower(email), taken first so the check above was not
-- a read-then-write race). Both are gone: the rule they enforced was removed,
-- and an address may now register as many times as remaining quota allows.
--
-- Nothing in the registration write path may be keyed on the email address
-- (FR-023a). Two submissions of one address must not contend, which is why the
-- lock went with the check rather than being kept "just in case" — a lock with
-- no invariant to protect is pure serialisation of the hot path.

-- name: CreateAttendeeSlot :one
-- An EMPTY slot: ticket-type-bound at booking, identity filled at checkout.
-- package_unit ties bundle slots to their purchased unit (spec 010).
INSERT INTO attendees (order_id, ticket_type_id, package_id, package_unit)
VALUES ($1, $2, $3, $4)
RETURNING id, order_id, ticket_type_id, package_id, package_unit, name, email, phone, dob, gender_id;

-- name: RecordTermsAgreement :execrows
-- Same "Agree" click as booking, its own call. Guarded on PENDING; idempotent
-- because re-stamping the same agreement is harmless.
UPDATE orders
SET terms_agreed_at = now(), event_terms_id = $2, updated_at = now()
WHERE orders.id = $1 AND status_id = (SELECT ost.id FROM order_statuses ost WHERE ost.name = 'PENDING');

-- name: UpdateOrderBuyer :execrows
-- Checkout TX-D: the primary contact — a snapshot of the TOPMOST holder form
-- (first slot group in canonical slot order, spec 011). Only what downstream
-- consumes is kept: name/email/phone (gateway customer, admin list, delivery
-- fallback).
UPDATE orders
SET buyer_name = $2, buyer_email = $3, buyer_phone = $4, updated_at = now()
WHERE orders.id = $1 AND status_id = (SELECT ost.id FROM order_statuses ost WHERE ost.name = 'PENDING');

-- name: ListActiveGenders :many
-- The gender master list as the FORMS' OPTIONS (clarified 2026-08-05): active
-- entries only, and it must stay that way. Spec 011 FR-031 widens one card's
-- option list to show a gender that card already holds; it does not widen this.
SELECT id, name FROM genders WHERE is_active ORDER BY name;

-- name: ListGenders :many
-- Every gender, active or not, as checkout needs it (spec 011 FR-031, clarified
-- 2026-08-19). Deactivating an entry never rewrites a stored reference, so a
-- restored form can submit a gender this list still knows and ListActiveGenders
-- no longer offers. Checkout resolves gender_id from here — the active-only map
-- yields the zero value for a retired name, which is an invalid foreign key
-- rather than a refusal.
SELECT id, name, is_active FROM genders ORDER BY name;

-- name: UpdateAttendeeDetails :execrows
-- Checkout TX-D: fills one slot. order_id in the predicate stops a forged slot
-- id from writing into another order's attendee. gender_id references the
-- master row whose NAME the request carried (spec 011, FR-018).
UPDATE attendees
SET name = $3, email = $4, phone = $5, dob = $6, gender_id = $7
WHERE id = $1 AND order_id = $2;


-- name: UpdatePaymentDetailsIfUnstarted :execrows
-- Checkout TX-P: stamps the gateway session and the payment window, guarded so
-- a concurrent checkout cannot overwrite a live QR.
UPDATE orders
SET payment_url = $2,
    payment_provider = $3,
    payment_qr_string = $4,
    payment_expires_at = $5,
    updated_at = now()
WHERE orders.id = $1
  AND status_id = (SELECT ost.id FROM order_statuses ost WHERE ost.name = 'PENDING')
  AND payment_qr_string IS NULL;

-- name: GetOrderWithTermsByNumber :one
-- The 008 guest read: everything the order page needs to pick its screen. The
-- screen it picks is chosen from the status NAME, which the join preserves.
SELECT o.id, o.order_number, o.buyer_name, o.buyer_email, o.buyer_phone, o.total_amount,
       os.name AS status,
       o.payment_provider, o.payment_url, o.email_sent, o.created_at, o.updated_at,
       o.payment_qr_string, o.payment_expires_at, o.terms_agreed_at, o.event_terms_id,
       o.subtotal
FROM orders o
JOIN order_statuses os ON os.id = o.status_id
WHERE o.order_number = $1;

-- name: ListAttendeeSlotsByOrderID :many
-- Slot list incl. the 008 detail columns and the human ticket-type name.
-- Ordering (spec 010): standalone slots first, then bundle slots contiguous
-- per (package_id, package_unit), so one visitor form maps to one unit. The
-- FIRST row of this ordering is the order's primary contact (spec 011).
-- gender comes back as BOTH the master row's id and its NAME (spec 011 FR-035,
-- clarified 2026-08-24). Not redundancy: a form submits the id, a guest is shown
-- the name, and a RETIRED entry is absent from the active master list — so a
-- client given only one of the two could not resolve the other, and FR-031's
-- restored form would break in one direction or the other. LEFT JOIN: an
-- unfilled slot is NULL in both.
SELECT a.id, a.order_id, a.ticket_type_id, a.package_id, a.package_unit,
       a.name, a.email, a.phone, a.dob, a.gender_id, g.name AS gender,
       tt.name AS ticket_type_name
FROM attendees a
JOIN ticket_types tt ON tt.id = a.ticket_type_id
LEFT JOIN genders g ON g.id = a.gender_id
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

-- name: CountFees :one
-- Pairs with ListFees below.
SELECT count(*) FROM fees;

-- name: ListFees :many
-- Admin read: every fee, active or not. `id` last for the same total-ordering
-- reason as the lists above — two fees may share a position and a name.
SELECT id, name, fee_type, value, position, is_active, created_at, updated_at
FROM fees
ORDER BY position, name, id
LIMIT sqlc.arg(row_limit)::int OFFSET sqlc.arg(row_offset)::int;

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

-- name: UpdateOrderStatusFromReleased :execrows
-- Settle-after-expiry only (FR-019): move an order out of a RELEASED state into
-- PAID because the gateway redelivered a notification saying it was paid.
--
-- The query can express any from→to move; the payment domain cannot ask for one.
-- Its adapter binds both statuses at the call site to EXPIRED → PAID, which is
-- what makes FR-019d — never revive an order the gateway itself cancelled —
-- unreachable by construction rather than by a caller remembering the rule.
--
-- Guarded on the order still being in the state the caller saw, for the same
-- reason UpdateOrderStatusIfPending is guarded on PENDING: two deliveries of the
-- same resend must produce one transition, not two, or the re-deduction that
-- accompanies it would run twice and oversell.
--
-- Nothing here is reachable by a person asserting a payment. FR-022d forbids that
-- outright, and there is no endpoint, service method, or interface that offers it.
UPDATE orders
SET status_id = (SELECT ost.id FROM order_statuses ost WHERE ost.name = sqlc.arg(status)),
    updated_at = now()
WHERE orders.id = $1
  AND status_id = (SELECT ost.id FROM order_statuses ost WHERE ost.name = sqlc.arg(from_status));

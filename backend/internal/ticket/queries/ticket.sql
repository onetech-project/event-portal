-- name: CreateTicket :one
-- qr_code_url is deliberately left NULL: this MVP has no object storage and QR
-- images are rendered on demand from ticket_code (spec FR-022).
INSERT INTO tickets (ticket_code, order_id, attendee_id, status)
VALUES ($1, $2, $3, 'ACTIVE')
RETURNING id, ticket_code, order_id, attendee_id, status, created_at;

-- name: GetTicketDetailByCode :one
-- Exact match on the already-canonical stored code so idx_tickets_ticket_code is
-- used; never UPPER(ticket_code) = ..., which would force a sequential scan.
SELECT t.ticket_code, t.status, a.name AS attendee_name,
       tt.name AS ticket_type_name, e.name AS event_name
FROM tickets t
JOIN attendees a ON a.id = t.attendee_id
JOIN ticket_types tt ON tt.id = a.ticket_type_id
JOIN events e ON e.id = tt.event_id
WHERE t.ticket_code = $1;

-- name: GetTicketStatusByCode :one
SELECT status FROM tickets WHERE ticket_code = $1;

-- name: MarkTicketUsed :execrows
-- Single guarded UPDATE: two near-simultaneous calls cannot both succeed.
-- updated_at records when the ticket was consumed — SCHEMA.md is LOCKED and the
-- tickets table has no used_at column.
UPDATE tickets
SET status = 'USED', updated_at = now()
WHERE ticket_code = $1 AND status = 'ACTIVE';

-- name: ListTicketsByOrderID :many
SELECT t.id, t.ticket_code, t.attendee_id, t.status
FROM tickets t
WHERE t.order_id = $1
ORDER BY t.created_at ASC, t.ticket_code ASC;

-- name: ListTicketDetailsByOrderID :many
-- Everything the ticket PDF prints, for the initial delivery and every resend.
-- attendee_email is the per-holder delivery address (spec 011): notification
-- groups an order's tickets by it and sends each holder their own PDF.
SELECT t.ticket_code, t.status, a.name AS attendee_name, a.email AS attendee_email,
       tt.name AS ticket_type_name, e.name AS event_name,
       e.venue, e.start_date
FROM tickets t
JOIN attendees a ON a.id = t.attendee_id
JOIN ticket_types tt ON tt.id = a.ticket_type_id
JOIN events e ON e.id = tt.event_id
WHERE t.order_id = $1
ORDER BY t.created_at ASC, t.ticket_code ASC;

-- name: CountTicketsByOrderID :one
SELECT COUNT(*)::bigint AS total FROM tickets WHERE order_id = $1;

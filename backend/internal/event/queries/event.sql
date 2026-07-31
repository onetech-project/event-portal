-- Guest-facing reads -------------------------------------------------------

-- name: ListPublishedEvents :many
SELECT id, name, slug, description, venue, address, start_date, end_date, banner_url, status
FROM events
WHERE status = 'PUBLISHED'
ORDER BY start_date ASC;

-- name: GetPublishedEventBySlug :one
SELECT id, name, slug, description, venue, address, start_date, end_date, banner_url, status
FROM events
WHERE slug = $1 AND status = 'PUBLISHED';

-- name: ListTicketTypesByEventID :many
SELECT id, event_id, name, price, quota, sales_start, sales_end
FROM ticket_types
WHERE event_id = $1
ORDER BY price ASC, name ASC;

-- name: GetTicketTypeByID :one
SELECT id, event_id, name, price, quota, sales_start, sales_end
FROM ticket_types
WHERE id = $1;

-- name: ListTicketTypeIDsByEventID :many
SELECT id FROM ticket_types WHERE event_id = $1;

-- name: ListTicketTypeNamesByIDs :many
SELECT id, name FROM ticket_types WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- Quota mutation (EventProvider) -------------------------------------------
-- `quota` is the REMAINING quota (constitution, Critical Data Flow Rules): the live
-- counter checkout decrements and cancel/expire/deny/failure restore. The guarded
-- WHERE makes the deduction atomic under concurrency — zero rows affected means
-- insufficient quota, never a negative value.

-- name: CheckAndDeductQuota :one
UPDATE ticket_types
SET quota = quota - sqlc.arg(qty), updated_at = now()
WHERE id = sqlc.arg(id) AND quota >= sqlc.arg(qty)
RETURNING quota;

-- name: RestoreQuota :one
UPDATE ticket_types
SET quota = quota + sqlc.arg(qty), updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING quota;

-- Admin event CRUD ---------------------------------------------------------

-- name: ListEvents :many
SELECT id, name, slug, description, venue, address, start_date, end_date, banner_url, status, created_at, updated_at
FROM events
ORDER BY start_date DESC;

-- name: GetEventByID :one
SELECT id, name, slug, description, venue, address, start_date, end_date, banner_url, status, created_at, updated_at
FROM events
WHERE id = $1;

-- name: CreateEvent :one
INSERT INTO events (name, slug, description, venue, address, start_date, end_date, banner_url, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, name, slug, description, venue, address, start_date, end_date, banner_url, status, created_at, updated_at;

-- name: UpdateEvent :one
UPDATE events
SET name = $2, slug = $3, description = $4, venue = $5, address = $6,
    start_date = $7, end_date = $8, banner_url = $9, status = $10, updated_at = now()
WHERE id = $1
RETURNING id, name, slug, description, venue, address, start_date, end_date, banner_url, status, created_at, updated_at;

-- name: DeleteTicketTypesByEventID :execrows
DELETE FROM ticket_types WHERE event_id = $1;

-- name: DeleteEvent :execrows
DELETE FROM events WHERE id = $1;

-- Admin ticket-type CRUD ---------------------------------------------------

-- name: ListTicketTypesAdmin :many
SELECT id, event_id, name, price, quota, sales_start, sales_end, created_at, updated_at
FROM ticket_types
WHERE event_id = $1
ORDER BY created_at ASC;

-- name: GetTicketTypeAdmin :one
SELECT id, event_id, name, price, quota, sales_start, sales_end, created_at, updated_at
FROM ticket_types
WHERE id = $1;

-- name: CreateTicketType :one
INSERT INTO ticket_types (event_id, name, price, quota, sales_start, sales_end)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, event_id, name, price, quota, sales_start, sales_end, created_at, updated_at;

-- name: UpdateTicketType :one
-- `quota` is set ABSOLUTELY to the submitted remaining quota; past sales are never
-- re-subtracted here (contracts/api.md, Admin Management).
UPDATE ticket_types
SET name = $2, price = $3, quota = $4, sales_start = $5, sales_end = $6, updated_at = now()
WHERE id = $1
RETURNING id, event_id, name, price, quota, sales_start, sales_end, created_at, updated_at;

-- name: DeleteTicketType :execrows
DELETE FROM ticket_types WHERE id = $1;

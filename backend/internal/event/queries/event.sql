-- Guest-facing reads -------------------------------------------------------

-- name: ListPublishedEvents :many
SELECT id, name, slug, description, venue, address, start_date, end_date, banner_url, status, scale
FROM events
WHERE status = 'PUBLISHED'
ORDER BY start_date ASC;

-- name: GetPublishedEventBySlug :one
SELECT id, name, slug, description, venue, address, start_date, end_date, banner_url, status, scale
FROM events
WHERE slug = $1 AND status = 'PUBLISHED';

-- name: ListTicketTypesByEventID :many
SELECT id, event_id, name, description, price, quota, sales_start, sales_end
FROM ticket_types
WHERE event_id = $1
ORDER BY price ASC, name ASC;

-- name: GetTicketTypeByID :one
SELECT id, event_id, name, description, price, quota, sales_start, sales_end
FROM ticket_types
WHERE id = $1;

-- name: ListTicketTypeIDsByEventID :many
SELECT id FROM ticket_types WHERE event_id = $1;

-- name: ListTicketTypeNamesByIDs :many
SELECT id, name FROM ticket_types WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ListTicketTypeQuotasByIDs :many
-- Remaining quota per ticket type. `quota` is the REMAINING counter (constitution,
-- Critical Data Flow Rules), not the original allocation.
--
-- The payment domain reads this to size the shortfall when a redelivered
-- notification cannot settle an expired order (FR-019c), and to show an operator
-- what an order holds against what is left before they request a resend
-- (FR-022e). It is deliberately NOT the sold-count shown on the ticket-type
-- editor, which counts released orders and so overstates what has been sold.
SELECT id, name, quota FROM ticket_types WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- Both tables belong to this domain, so the JOIN stays inside the boundary the
-- order domain is not allowed to cross itself.
-- name: ListTicketTypeDisplaysByIDs :many
SELECT tt.id, tt.name, e.name AS event_name, e.slug AS event_slug,
       e.venue AS event_venue, e.address AS event_address,
       e.start_date AS event_start_date, e.end_date AS event_end_date
FROM ticket_types tt
JOIN events e ON e.id = tt.event_id
WHERE tt.id = ANY(sqlc.arg(ids)::uuid[]);

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
SELECT id, name, slug, description, venue, address, start_date, end_date, banner_url, status, created_at, updated_at, scale
FROM events
ORDER BY start_date DESC;

-- name: GetEventByID :one
SELECT id, name, slug, description, venue, address, start_date, end_date, banner_url, status, created_at, updated_at, scale
FROM events
WHERE id = $1;

-- name: CreateEvent :one
INSERT INTO events (name, slug, description, venue, address, start_date, end_date, banner_url, status, scale)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, name, slug, description, venue, address, start_date, end_date, banner_url, status, created_at, updated_at, scale;

-- name: UpdateEvent :one
UPDATE events
SET name = $2, slug = $3, description = $4, venue = $5, address = $6,
    start_date = $7, end_date = $8, banner_url = $9, status = $10, scale = $11, updated_at = now()
WHERE id = $1
RETURNING id, name, slug, description, venue, address, start_date, end_date, banner_url, status, created_at, updated_at, scale;

-- name: DeleteTicketTypesByEventID :execrows
DELETE FROM ticket_types WHERE event_id = $1;

-- name: DeleteEvent :execrows
DELETE FROM events WHERE id = $1;

-- Admin ticket-type CRUD ---------------------------------------------------

-- name: ListTicketTypesAdmin :many
SELECT id, event_id, name, description, price, quota, sales_start, sales_end, created_at, updated_at
FROM ticket_types
WHERE event_id = $1
ORDER BY created_at ASC;

-- name: GetTicketTypeAdmin :one
SELECT id, event_id, name, description, price, quota, sales_start, sales_end, created_at, updated_at
FROM ticket_types
WHERE id = $1;

-- name: CreateTicketType :one
INSERT INTO ticket_types (event_id, name, description, price, quota, sales_start, sales_end)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, event_id, name, description, price, quota, sales_start, sales_end, created_at, updated_at;

-- name: UpdateTicketType :one
-- `quota` is set ABSOLUTELY to the submitted remaining quota; past sales are never
-- re-subtracted here (contracts/api.md, Admin Management).
UPDATE ticket_types
SET name = $2, description = $3, price = $4, quota = $5, sales_start = $6, sales_end = $7, updated_at = now()
WHERE id = $1
RETURNING id, event_id, name, description, price, quota, sales_start, sales_end, created_at, updated_at;

-- name: DeleteTicketType :execrows
DELETE FROM ticket_types WHERE id = $1;

-- Packages (bundle offers) --------------------------------------------------
-- Owned by this domain: a package hangs off an event and joins only to that
-- event's ticket_types, so nothing here crosses a domain boundary.
--
-- There is deliberately no quota column to read or write. Availability is derived
-- at read time from the remaining quota of the constituents reached through
-- package_tickets; a stored figure would be a second source of truth.

-- name: ListPackagesWithAvailabilityByEventID :many
-- The booking-list query. Two queries serve the whole list (this one plus a batched
-- component read), never N+1 across packages.
--
-- available_units = MIN(quota / quantity_per_unit): integer division floors, so a
-- constituent with 5 remaining consumed 2 at a time yields 2 whole sets, not 2.5.
-- MIN because the scarcest constituent governs.
--
-- purchasable folds every gate into one boolean so the client cannot disagree with
-- the server about what is buyable: units > 0, at least one component, EVERY
-- constituent on sale (the binding constraint), ACTIVE, and the package's own window.
--
-- LEFT JOIN, not JOIN: a componentless package must still list to an administrator
-- as broken rather than silently vanish.
WITH component_stats AS (
    SELECT
        pt.package_id,
        MIN(tt.quota / pt.quantity)::int AS available_units,
        (ARRAY_AGG(tt.id ORDER BY (tt.quota / pt.quantity) ASC, tt.id ASC))[1]::uuid
            AS limiting_ticket_type_id,
        BOOL_AND(tt.sales_start <= now() AND tt.sales_end >= now())
            AS all_components_on_sale,
        COUNT(*) AS component_count
    FROM package_tickets pt
    JOIN ticket_types tt ON tt.id = pt.ticket_type_id
    GROUP BY pt.package_id
)
SELECT
    p.id, p.event_id, p.name, p.description, p.price,
    p.sales_start, p.sales_end, p.is_active, p.created_at, p.updated_at,
    COALESCE(cs.available_units, 0)::int AS available_units,
    cs.limiting_ticket_type_id,
    (
        COALESCE(cs.available_units, 0) > 0
        AND COALESCE(cs.component_count, 0) > 0
        AND COALESCE(cs.all_components_on_sale, FALSE)
        AND p.is_active
        AND p.sales_start <= now()
        AND p.sales_end   >= now()
    ) AS purchasable
FROM packages p
LEFT JOIN component_stats cs ON cs.package_id = p.id
WHERE p.event_id = sqlc.arg(event_id)
ORDER BY p.price ASC, p.name ASC;

-- name: GetPackageAvailability :one
-- Single-package form, for the admin availability diagnostic.
--
-- The aggregates are COALESCEd because a componentless package groups over zero
-- rows and would otherwise return NULLs that fail to scan. component_count = 0 is
-- the signal callers read as "no composition, therefore unavailable"; a NIL
-- limiting id means the same.
SELECT
    COALESCE(MIN(tt.quota / pt.quantity), 0)::int AS available_units,
    COALESCE(
        (ARRAY_AGG(tt.id ORDER BY (tt.quota / pt.quantity) ASC, tt.id ASC))[1],
        '00000000-0000-0000-0000-000000000000'::uuid
    )::uuid AS limiting_ticket_type_id,
    COUNT(*)::int AS component_count,
    COALESCE(BOOL_AND(tt.sales_start <= now() AND tt.sales_end >= now()), FALSE)::boolean
        AS all_components_on_sale
FROM package_tickets pt
JOIN ticket_types tt ON tt.id = pt.ticket_type_id
WHERE pt.package_id = sqlc.arg(package_id);

-- name: ListPackageComponentsByPackageIDs :many
-- Batched over the whole list so rendering "what is inside this bundle" stays one
-- query regardless of how many packages an event has.
SELECT pt.package_id, pt.ticket_type_id, pt.quantity AS quantity_per_unit,
       tt.name AS ticket_type_name, tt.price, tt.quota, tt.sales_start, tt.sales_end
FROM package_tickets pt
JOIN ticket_types tt ON tt.id = pt.ticket_type_id
WHERE pt.package_id = ANY(sqlc.arg(package_ids)::uuid[])
ORDER BY pt.package_id, tt.name;

-- name: ListPublishedPackagesByEventSlug :many
-- Guest-facing variant: only ACTIVE packages of a PUBLISHED event.
SELECT p.id
FROM packages p
JOIN events e ON e.id = p.event_id
WHERE e.slug = sqlc.arg(slug) AND e.status = 'PUBLISHED' AND p.is_active;

-- name: GetPackageByID :one
SELECT id, event_id, name, description, price, sales_start, sales_end, is_active,
       created_at, updated_at
FROM packages
WHERE id = $1;

-- name: CreatePackage :one
-- No quota column is written because none exists.
INSERT INTO packages (event_id, name, description, price, sales_start, sales_end, is_active)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, event_id, name, description, price, sales_start, sales_end, is_active,
          created_at, updated_at;

-- name: UpdatePackage :one
-- event_id is absent on purpose: a package never moves between events, since its
-- composition is bound to that event by the composite foreign keys.
UPDATE packages
SET name = $2, description = $3, price = $4, sales_start = $5, sales_end = $6,
    is_active = $7, updated_at = now()
WHERE id = $1
RETURNING id, event_id, name, description, price, sales_start, sales_end, is_active,
          created_at, updated_at;

-- name: DeletePackage :execrows
DELETE FROM packages WHERE id = $1;

-- name: DeletePackageComponents :execrows
DELETE FROM package_tickets WHERE package_id = $1;

-- name: CreatePackageComponent :one
-- event_id is supplied by the caller and is what the two composite foreign keys
-- both resolve against, making cross-event composition impossible to insert.
INSERT INTO package_tickets (package_id, ticket_type_id, event_id, quantity)
VALUES ($1, $2, $3, $4)
RETURNING id, package_id, ticket_type_id, event_id, quantity, created_at;

-- name: ListPackagesByTicketTypeID :many
-- Drives the ticket-type delete guard: a ticket inside a bundle cannot be deleted
-- until it is removed from that bundle. Returns names so the 400 can say which.
SELECT p.id, p.name
FROM package_tickets pt
JOIN packages p ON p.id = pt.package_id
WHERE pt.ticket_type_id = $1
ORDER BY p.name;

-- name: ListPackagesByTicketTypeIDs :many
-- Batched form used when deleting an event, which must consider every ticket type.
SELECT DISTINCT p.id, p.name
FROM package_tickets pt
JOIN packages p ON p.id = pt.package_id
WHERE pt.ticket_type_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY p.name;

-- name: CountPackagesByEventID :one
SELECT COUNT(*)::bigint AS total FROM packages WHERE event_id = $1;

-- name: PackageForCheckout :one
-- Server-side truth for a package at checkout: authoritative price, window, status
-- and owning event. Composition is read separately via
-- ListPackageComponentsByPackageIDs so both callers share one query.
SELECT id, event_id, name, price, sales_start, sales_end, is_active
FROM packages
WHERE id = $1;

-- name: ListPackageDisplaysByIDs :many
-- Both tables belong to this domain, so the JOIN stays inside the boundary the
-- order domain is not allowed to cross itself.
SELECT p.id, p.name, e.name AS event_name, e.slug AS event_slug,
       e.venue AS event_venue, e.address AS event_address,
       e.start_date AS event_start_date, e.end_date AS event_end_date
FROM packages p
JOIN events e ON e.id = p.event_id
WHERE p.id = ANY(sqlc.arg(ids)::uuid[]);

-- Spec 008: Terms & Conditions ----------------------------------------------

-- name: GetEventTermsByEventID :one
SELECT id, event_id, content, created_at, updated_at
FROM event_terms
WHERE event_id = $1;

-- name: GetEventTermsByEventSlug :one
-- Guest read: the dialog fetches terms by the event's public identifier.
SELECT t.id, t.event_id, t.content, t.created_at, t.updated_at
FROM event_terms t
JOIN events e ON e.id = t.event_id
WHERE e.slug = $1 AND e.status = 'PUBLISHED';

-- name: UpsertEventTerms :one
-- One live document per event (event_id UNIQUE): an edit overwrites in place.
INSERT INTO event_terms (event_id, content)
VALUES ($1, $2)
ON CONFLICT (event_id) DO UPDATE SET content = EXCLUDED.content, updated_at = now()
RETURNING id, event_id, content, created_at, updated_at;

-- name: EventHasTerms :one
SELECT EXISTS (SELECT 1 FROM event_terms WHERE event_id = $1) AS has_terms;

-- Spec 008: content blocks --------------------------------------------------

-- name: ListEventActivities :many
SELECT id, event_id, title, description, icon, position, created_at, updated_at
FROM event_activities
WHERE event_id = $1
ORDER BY position ASC, created_at ASC;

-- name: CreateEventActivity :one
INSERT INTO event_activities (event_id, title, description, icon, position)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, event_id, title, description, icon, position, created_at, updated_at;

-- name: UpdateEventActivity :execrows
UPDATE event_activities
SET title = $3, description = $4, icon = $5, position = $6, updated_at = now()
WHERE id = $1 AND event_id = $2;

-- name: DeleteEventActivity :execrows
DELETE FROM event_activities WHERE id = $1 AND event_id = $2;

-- name: ListEventGuestStars :many
SELECT id, event_id, name, position, created_at, updated_at
FROM event_guest_stars
WHERE event_id = $1
ORDER BY position ASC, created_at ASC;

-- name: CreateEventGuestStar :one
INSERT INTO event_guest_stars (event_id, name, position)
VALUES ($1, $2, $3)
RETURNING id, event_id, name, position, created_at, updated_at;

-- name: UpdateEventGuestStar :execrows
UPDATE event_guest_stars
SET name = $3, position = $4, updated_at = now()
WHERE id = $1 AND event_id = $2;

-- name: DeleteEventGuestStar :execrows
DELETE FROM event_guest_stars WHERE id = $1 AND event_id = $2;

-- name: ListEventGuidelines :many
SELECT id, event_id, description, icon, position, created_at, updated_at
FROM event_guidelines
WHERE event_id = $1
ORDER BY position ASC, created_at ASC;

-- name: CreateEventGuideline :one
INSERT INTO event_guidelines (event_id, description, icon, position)
VALUES ($1, $2, $3, $4)
RETURNING id, event_id, description, icon, position, created_at, updated_at;

-- name: UpdateEventGuideline :execrows
UPDATE event_guidelines
SET description = $3, icon = $4, position = $5, updated_at = now()
WHERE id = $1 AND event_id = $2;

-- name: DeleteEventGuideline :execrows
DELETE FROM event_guidelines WHERE id = $1 AND event_id = $2;

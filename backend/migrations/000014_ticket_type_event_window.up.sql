-- Spec 015: a ticket type gains its own EVENT window — when a ticket of this
-- type admits its holder — alongside the sales window that says only when it
-- can be bought. A three-day festival's Day 1 and Day 2 passes are used on
-- different days; before this column pair both advertised, and both printed,
-- the parent event's opening date.
--
-- Deliberately unconstrained against sales_start/sales_end in either direction
-- (FR-003): a ticket may legitimately stay on sale after the day it admits to.
--
-- Backfilled from the parent event BEFORE the NOT NULL, or the constraint fails
-- on existing rows. The backfill is total — event_id is NOT NULL REFERENCES
-- events(id), so every row has a parent — and it copies the event's own dates
-- verbatim (FR-004), which is the only value that leaves every pre-existing
-- ticket type displaying exactly what it displayed before.

-- Spelled TIMESTAMP WITH TIME ZONE, not timestamptz: sqlc's type override keys
-- on pg_catalog.timestamptz, and only the long spelling normalises to it. The
-- short one silently generates pgtype.Timestamptz instead of time.Time.
ALTER TABLE ticket_types
    ADD COLUMN event_start TIMESTAMP WITH TIME ZONE,
    ADD COLUMN event_end   TIMESTAMP WITH TIME ZONE;

UPDATE ticket_types tt
SET event_start = e.start_date,
    event_end   = e.end_date
FROM events e
WHERE e.id = tt.event_id;

ALTER TABLE ticket_types ALTER COLUMN event_start SET NOT NULL;
ALTER TABLE ticket_types ALTER COLUMN event_end   SET NOT NULL;

-- `>=`, not `>` as packages_sales_window_chk uses. An event whose start_date
-- equals its end_date is legal (EventRequest.Validate refuses only an end
-- BEFORE a start), so a strict check here would fail the backfill above on real
-- data. Matching the event's own rule keeps the backfill total.
ALTER TABLE ticket_types
    ADD CONSTRAINT ticket_types_event_window_chk CHECK (event_end >= event_start);

COMMENT ON COLUMN ticket_types.event_start IS
    'The instant admission OPENS for this ticket type - not showtime. Admin validation admits no tolerance (spec 015 FR-014), so a holder presented one moment earlier is refused. Distinct from sales_start, which governs only purchase.';

COMMENT ON COLUMN ticket_types.event_end IS
    'The last instant a holder can be admitted, inclusive. Distinct from sales_end, which governs only purchase.';

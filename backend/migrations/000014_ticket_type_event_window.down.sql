-- The CHECK constraint is dropped with the columns it references.
ALTER TABLE ticket_types
    DROP COLUMN event_start,
    DROP COLUMN event_end;
